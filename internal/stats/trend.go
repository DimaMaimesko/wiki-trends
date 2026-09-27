package stats

import (
	"math"
	"sort"
)

// LogLinearFit is the result of regressing log(views+1) on a day index.
type LogLinearFit struct {
	SlopePerDay float64
	StdErr      float64
	NWLag       int
	PValue      float64
	R2          float64
	N           int
	Intercept   float64
	Residuals   []float64 // aligned with the input, NaN where input was NaN
}

const daysPerYear = 365.25

// GrowthPctPerYear converts a per-day log slope into a compound annual percent
// change. exp(b*365.25)-1 rather than b*365.25*100 because the fit is
// multiplicative: a slope of 0.002/day is +107%/yr, not +73%/yr.
func GrowthPctPerYear(slopePerDay float64) float64 {
	return (math.Exp(slopePerDay*daysPerYear) - 1) * 100
}

// FitLogLinear regresses y (already on a log scale) on its own index using OLS
// with Newey-West heteroskedasticity- and autocorrelation-consistent standard
// errors.
//
// Why HAC and not plain OLS: consecutive days of Wikipedia traffic are highly
// correlated, so the ~730 daily observations in a two-year window carry far less
// independent information than 730. Plain OLS errors would treat them as
// independent and report implausibly tight confidence intervals — the single
// most common way a pageview "trend" turns out to be an artefact. The bandwidth
// follows Newey & West's rule L = floor(4*(n/100)^(2/9)).
func FitLogLinear(y []float64) LogLinearFit {
	type pt struct{ x, y float64 }
	pts := make([]pt, 0, len(y))
	for i, v := range y {
		if !math.IsNaN(v) {
			pts = append(pts, pt{float64(i), v})
		}
	}
	res := LogLinearFit{N: len(pts), SlopePerDay: math.NaN(), StdErr: math.NaN(), PValue: math.NaN(), R2: math.NaN()}
	if len(pts) < 10 {
		return res
	}
	var sx, sy float64
	for _, p := range pts {
		sx += p.x
		sy += p.y
	}
	n := float64(len(pts))
	mx, my := sx/n, sy/n
	var sxx, sxy float64
	for _, p := range pts {
		dx := p.x - mx
		sxx += dx * dx
		sxy += dx * (p.y - my)
	}
	if sxx == 0 {
		return res
	}
	b := sxy / sxx
	a := my - b*mx
	res.SlopePerDay, res.Intercept = b, a

	// residuals and R^2
	u := make([]float64, len(pts))
	var ssr, sst float64
	for i, p := range pts {
		f := a + b*p.x
		u[i] = p.y - f
		ssr += u[i] * u[i]
		sst += (p.y - my) * (p.y - my)
	}
	if sst > 0 {
		res.R2 = 1 - ssr/sst
	}

	// Newey-West long-run variance of sum((x-mx)*u)
	L := int(math.Floor(4 * math.Pow(n/100, 2.0/9.0)))
	if L < 1 {
		L = 1
	}
	if L > len(pts)/4 {
		L = len(pts) / 4
	}
	res.NWLag = L
	z := make([]float64, len(pts))
	for i, p := range pts {
		z[i] = (p.x - mx) * u[i]
	}
	S := 0.0
	for _, v := range z {
		S += v * v
	}
	for l := 1; l <= L; l++ {
		w := 1 - float64(l)/float64(L+1)
		acc := 0.0
		for i := l; i < len(z); i++ {
			acc += z[i] * z[i-l]
		}
		S += 2 * w * acc
	}
	if S < 0 {
		S = 0
	}
	res.StdErr = math.Sqrt(S) / sxx
	if res.StdErr > 0 {
		res.PValue = TwoSidedP(b / res.StdErr)
	}

	// scatter residuals back onto the original index positions
	res.Residuals = make([]float64, len(y))
	k := 0
	for i, v := range y {
		if math.IsNaN(v) {
			res.Residuals[i] = math.NaN()
			continue
		}
		res.Residuals[i] = u[k]
		k++
	}
	return res
}

// CIPctPerYear returns the 95% confidence interval for the annual growth rate.
// The interval is built on the log slope and then exponentiated, which keeps it
// asymmetric in percent space — correct for a multiplicative process, and a
// useful signal in itself: [-5%, +90%] is not "+40% growth".
func (f LogLinearFit) CIPctPerYear(z float64) (lo, hi float64) {
	if math.IsNaN(f.StdErr) {
		return math.NaN(), math.NaN()
	}
	return GrowthPctPerYear(f.SlopePerDay - z*f.StdErr), GrowthPctPerYear(f.SlopePerDay + z*f.StdErr)
}

// TheilSen is the median of all pairwise slopes: a breakdown point of 29%, so it
// ignores spikes entirely. Used as an independent cross-check on the OLS slope.
// Disagreement between the two means the OLS estimate is being steered by a few
// days and the trend should not be reported as a fact.
func TheilSen(y []float64) float64 {
	type pt struct{ x, y float64 }
	pts := make([]pt, 0, len(y))
	for i, v := range y {
		if !math.IsNaN(v) {
			pts = append(pts, pt{float64(i), v})
		}
	}
	if len(pts) < 3 {
		return math.NaN()
	}
	slopes := make([]float64, 0, len(pts)*(len(pts)-1)/2)
	for i := 0; i < len(pts); i++ {
		for j := i + 1; j < len(pts); j++ {
			dx := pts[j].x - pts[i].x
			if dx == 0 {
				continue
			}
			slopes = append(slopes, (pts[j].y-pts[i].y)/dx)
		}
	}
	if len(slopes) == 0 {
		return math.NaN()
	}
	sort.Float64s(slopes)
	return Median(slopes)
}

// MannKendall is a rank-based test for monotonic trend. It assumes nothing about
// the distribution and is insensitive to outlier magnitude, which is why it is
// the headline "is this real?" number rather than the OLS p-value.
//
// It does assume serially independent observations, so callers must pass a
// *monthly* aggregate. Running it on daily data would inflate significance for
// exactly the reason HAC errors exist.
func MannKendall(y []float64) (tau, p float64) {
	c := Clean(y)
	n := len(c)
	if n < 8 {
		return math.NaN(), math.NaN()
	}
	S := 0.0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			switch {
			case c[j] > c[i]:
				S++
			case c[j] < c[i]:
				S--
			}
		}
	}
	nf := float64(n)
	varS := nf * (nf - 1) * (2*nf + 5) / 18

	// tie correction
	sorted := append([]float64(nil), c...)
	sort.Float64s(sorted)
	i := 0
	for i < n {
		j := i
		for j+1 < n && sorted[j+1] == sorted[i] {
			j++
		}
		if t := float64(j - i + 1); t > 1 {
			varS -= t * (t - 1) * (2*t + 5) / 18
		}
		i = j + 1
	}
	if varS <= 0 {
		return 0, 1
	}
	var z float64
	switch {
	case S > 0:
		z = (S - 1) / math.Sqrt(varS)
	case S < 0:
		z = (S + 1) / math.Sqrt(varS)
	}
	tau = S / (nf * (nf - 1) / 2)
	return tau, TwoSidedP(z)
}

// BenjaminiHochberg adjusts p-values for the number of wikis tested. Scanning 20
// language editions at alpha=0.05 yields one "significant" result by chance
// alone; without this correction a portfolio scan reliably recommends a language
// picked out of noise. Returns adjusted p-values in the input order.
func BenjaminiHochberg(p []float64) []float64 {
	type ip struct {
		i int
		p float64
	}
	valid := make([]ip, 0, len(p))
	for i, v := range p {
		if !math.IsNaN(v) {
			valid = append(valid, ip{i, v})
		}
	}
	out := make([]float64, len(p))
	for i := range out {
		out[i] = math.NaN()
	}
	m := len(valid)
	if m == 0 {
		return out
	}
	sort.Slice(valid, func(a, b int) bool { return valid[a].p < valid[b].p })
	prev := 1.0
	for k := m - 1; k >= 0; k-- {
		adj := valid[k].p * float64(m) / float64(k+1)
		if adj > prev {
			adj = prev
		}
		prev = adj
		out[valid[k].i] = math.Min(1, adj)
	}
	return out
}
