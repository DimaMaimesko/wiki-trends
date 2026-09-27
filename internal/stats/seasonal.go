package stats

import (
	"math"
	"time"
)

// Seasonality holds multiplicative seasonal indices estimated by
// ratio-to-moving-average. Index 1.0 means "average"; 1.4 means that slot
// normally runs 40% above the local level.
type Seasonality struct {
	Weekly       [7]float64  // Sunday..Saturday
	Monthly      [12]float64 // January..December
	HasWeekly    bool
	HasAnnual    bool
	YearsCovered float64
	WeeklyAmp    float64
	AnnualAmp    float64
	// AnnualConsistency is the mean pairwise correlation between the monthly
	// shapes estimated from each 365-day block. It answers "does this pattern
	// actually repeat?", which peak-month agreement alone does not.
	AnnualConsistency float64
}

// EstimateSeasonality measures the weekly and annual rhythm of a daily series.
//
// Why this matters more than it sounds: Wikipedia traffic for anything
// education-adjacent follows the school year. A window that starts in July and
// ends in November shows large "growth" that is purely the academic calendar.
// Estimating the annual index lets the skill deseasonalise before fitting a
// trend, and refuse to claim seasonality it cannot actually see.
//
// The two cycles are estimated differently, for a reason found by getting it
// wrong first:
//
//   - Weekly, by ratio to a 7-day centred moving average. A window as short as a
//     month gives dozens of observations per weekday, and the MA needs only three
//     days of padding, so this is cheap and robust.
//
//   - Annual, on the log scale, as the residual from a 365-day centred mean
//     whose undefined half-year at each edge is filled by extrapolating the
//     mean's own local slope. A plain centred mean leaves the first and last
//     half-year undefined, so a two-year window observes each calendar month
//     only once; a single global log-linear detrend uses every day but
//     describes a steeply falling series badly. Both were tried on real data
//     and rejected; the detrending step below has the details.
//
// Both indices take the median per slot rather than the mean: one news spike
// would otherwise make its month look permanently popular.
func EstimateSeasonality(start time.Time, x []float64) Seasonality {
	var s Seasonality
	s.YearsCovered = float64(len(x)) / 365.25
	for i := range s.Weekly {
		s.Weekly[i] = 1
	}
	for i := range s.Monthly {
		s.Monthly[i] = 1
	}
	if len(x) < 28 {
		return s
	}

	// --- weekly
	ma7 := CentredMA(x, 7)
	wRatios := make([][]float64, 7)
	for i, v := range x {
		if math.IsNaN(v) || math.IsNaN(ma7[i]) || ma7[i] <= 0 {
			continue
		}
		d := int(start.AddDate(0, 0, i).Weekday())
		wRatios[d] = append(wRatios[d], v/ma7[i])
	}
	var wIdx [7]float64
	ok := true
	for d := 0; d < 7; d++ {
		if len(wRatios[d]) < 3 {
			ok = false
			break
		}
		wIdx[d] = Median(wRatios[d])
	}
	if ok {
		normalize(wIdx[:])
		s.Weekly = wIdx
		s.HasWeekly = true
		s.WeeklyAmp = amplitude(wIdx[:])
	}

	// --- annual
	if len(x) < 730 {
		return s
	}
	deweekly := x
	if s.HasWeekly {
		deweekly = make([]float64, len(x))
		for i, v := range x {
			if math.IsNaN(v) {
				deweekly[i] = math.NaN()
				continue
			}
			deweekly[i] = v / s.Weekly[int(start.AddDate(0, 0, i).Weekday())]
		}
	}
	// Detrend on the log scale against a 365-day centred mean, with the
	// undefined half-year at each edge filled by extrapolating the mean's own
	// local slope.
	//
	// Both simpler options were tried and both failed on real data. A plain
	// centred mean leaves the first and last 182 days undefined, so a two-year
	// window yields one observation per calendar month and the "index" absorbs
	// whatever else happened that month. Detrending with a single global
	// log-linear fit uses every day but describes a steeply falling series badly,
	// and its residuals map onto months by accident: on Ukrainian astronomy it
	// produced a 5.3x peak-to-trough ratio with February a peak and March a
	// trough. A local mean with extrapolated edges keeps the estimate local
	// without discarding a year of data.
	logx := Log1p(deweekly)
	level := extendEdges(CentredMA(logx, 365))
	resid := make([]float64, len(logx))
	for i := range logx {
		if math.IsNaN(logx[i]) || math.IsNaN(level[i]) {
			resid[i] = math.NaN()
			continue
		}
		resid[i] = logx[i] - level[i]
	}

	byMonth := make([][]float64, 12)
	for i, r := range resid {
		if math.IsNaN(r) {
			continue
		}
		m := int(start.AddDate(0, 0, i).Month()) - 1
		byMonth[m] = append(byMonth[m], r)
	}
	var mIdx [12]float64
	// Two observations per calendar month is the floor for a value to mean "this
	// month is usually like that" rather than "this month once was".
	const minDaysPerMonth = 50
	for m := 0; m < 12; m++ {
		if len(byMonth[m]) < minDaysPerMonth {
			return s
		}
		mIdx[m] = math.Exp(Median(byMonth[m]))
	}
	normalize(mIdx[:])

	// Consistency gate: a pattern that does not repeat is not seasonal.
	//
	// The monthly shape is re-estimated inside each 365-day block and the blocks
	// are correlated with each other. A first attempt only checked whether the
	// overall peak and trough months fell on the right side of each block's
	// median, which a single 60-day bump passes about half the time by chance.
	// Correlating the whole 12-month shape uses all the information and rejects
	// one-off events, which is the failure mode that matters: a news cycle in one
	// March must never be reported as "demand peaks in spring".
	s.AnnualConsistency = annualConsistency(start, resid)
	if s.AnnualConsistency < 0.35 {
		return s
	}

	s.Monthly = mIdx
	s.HasAnnual = true
	s.AnnualAmp = amplitude(mIdx[:])
	return s
}

// extendEdges fills the leading and trailing NaN runs of a moving average by
// extrapolating the slope of its own first and last 90 defined points.
func extendEdges(ma []float64) []float64 {
	out := append([]float64(nil), ma...)
	first, last := -1, -1
	for i, v := range out {
		if !math.IsNaN(v) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 || last-first < 30 {
		return out
	}
	const span = 90
	slopeAt := func(from, to int) float64 {
		var n, sx, sy, sxy, sxx float64
		for i := from; i <= to; i++ {
			if math.IsNaN(out[i]) {
				continue
			}
			x := float64(i)
			n++
			sx += x
			sy += out[i]
			sxy += x * out[i]
			sxx += x * x
		}
		if n < 3 || n*sxx-sx*sx == 0 {
			return 0
		}
		return (n*sxy - sx*sy) / (n*sxx - sx*sx)
	}
	head := slopeAt(first, mini(first+span, last))
	for i := first - 1; i >= 0; i-- {
		out[i] = out[i+1] - head
	}
	tail := slopeAt(maxi(first, last-span), last)
	for i := last + 1; i < len(out); i++ {
		out[i] = out[i-1] + tail
	}
	return out
}

// annualConsistency returns the mean pairwise Pearson correlation between the
// monthly shapes of each 365-day block. 1.0 means every year has the same shape;
// values near 0 mean the "seasonality" is noise or a one-off event.
func annualConsistency(start time.Time, resid []float64) float64 {
	blocks := len(resid) / 365
	if blocks < 2 {
		return 0
	}
	shapes := make([][]float64, 0, blocks)
	for b := 0; b < blocks; b++ {
		lo, hi := b*365, (b+1)*365
		byMonth := make([][]float64, 12)
		for i := lo; i < hi && i < len(resid); i++ {
			if math.IsNaN(resid[i]) {
				continue
			}
			m := int(start.AddDate(0, 0, i).Month()) - 1
			byMonth[m] = append(byMonth[m], resid[i])
		}
		shape := make([]float64, 12)
		ok := 0
		for m := 0; m < 12; m++ {
			if len(byMonth[m]) < 10 {
				shape[m] = math.NaN()
				continue
			}
			shape[m] = Median(byMonth[m])
			ok++
		}
		if ok >= 8 {
			shapes = append(shapes, shape)
		}
	}
	if len(shapes) < 2 {
		return 0
	}
	sum, n := 0.0, 0
	for i := 0; i < len(shapes); i++ {
		for j := i + 1; j < len(shapes); j++ {
			if r, ok := corr(shapes[i], shapes[j]); ok {
				sum += r
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// corr is Pearson correlation over positions where both inputs are defined.
func corr(a, b []float64) (float64, bool) {
	var xs, ys []float64
	for i := range a {
		if math.IsNaN(a[i]) || math.IsNaN(b[i]) {
			continue
		}
		xs = append(xs, a[i])
		ys = append(ys, b[i])
	}
	if len(xs) < 6 {
		return 0, false
	}
	mx, my := Mean(xs), Mean(ys)
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx <= 0 || syy <= 0 {
		return 0, false
	}
	return sxy / math.Sqrt(sxx*syy), true
}

func argMax(v []float64) int {
	best := 0
	for i := range v {
		if v[i] > v[best] {
			best = i
		}
	}
	return best
}

func argMin(v []float64) int {
	best := 0
	for i := range v {
		if v[i] < v[best] {
			best = i
		}
	}
	return best
}

func mini(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func normalize(v []float64) {
	m := Mean(v)
	if m == 0 || math.IsNaN(m) {
		return
	}
	for i := range v {
		v[i] /= m
	}
}

func amplitude(v []float64) float64 {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, x := range v {
		lo, hi = math.Min(lo, x), math.Max(hi, x)
	}
	if lo <= 0 {
		return math.NaN()
	}
	return hi / lo
}

// Adjust divides out the seasonal indices that were actually estimated. Indices
// that were not identifiable are left at 1.0, so this is always safe to call.
func (s Seasonality) Adjust(start time.Time, x []float64) []float64 {
	out := make([]float64, len(x))
	for i, v := range x {
		if math.IsNaN(v) {
			out[i] = math.NaN()
			continue
		}
		d := start.AddDate(0, 0, i)
		f := 1.0
		if s.HasWeekly {
			f *= s.Weekly[int(d.Weekday())]
		}
		if s.HasAnnual {
			f *= s.Monthly[int(d.Month())-1]
		}
		if f <= 0 {
			out[i] = math.NaN()
			continue
		}
		out[i] = v / f
	}
	return out
}

// PeakTrough returns calendar months (1-12) more than 10% above / below average.
func (s Seasonality) PeakTrough() (peaks, troughs []int) {
	if !s.HasAnnual {
		return nil, nil
	}
	for m, v := range s.Monthly {
		switch {
		case v >= 1.10:
			peaks = append(peaks, m+1)
		case v <= 0.90:
			troughs = append(troughs, m+1)
		}
	}
	return
}

// LooksLikeSchoolYear reports the signature of academic-calendar demand.
//
// For a founder this is directly actionable — it changes launch timing, content
// calendars and paid-acquisition windows — and it is also the main reason naive
// window comparisons mislead.
//
// The first version of this test averaged four hardcoded "term" months and
// compared them with July and August. It missed the clearest real example
// available: Ukrainian astronomy, where September alone runs at 2.2x the annual
// average and July at 0.41x, because March and April happened to sit just below
// average and dragged the four-month mean to 1.00. The test now asks the question
// that actually defines the pattern — is the summer clearly quiet, is most of the
// academic year clearly busier, and is the gap large — rather than fixing on
// particular months, which differ by country anyway.
func (s Seasonality) LooksLikeSchoolYear() bool {
	if !s.HasAnnual {
		return false
	}
	summer := (s.Monthly[6] + s.Monthly[7]) / 2 // Jul, Aug
	if summer >= 0.85 {
		return false
	}
	term := []int{8, 9, 10, 11, 1, 2, 3, 4} // Sep-Dec and Feb-May
	above, sum := 0, 0.0
	for _, m := range term {
		sum += s.Monthly[m]
		if s.Monthly[m] > 1.0 {
			above++
		}
	}
	mean := sum / float64(len(term))
	return above >= 5 && summer > 0 && mean/summer >= 1.4
}

// PeakMonth and TroughMonth return the strongest and weakest calendar months
// (1-12), for reports that need one month rather than a list.
func (s Seasonality) PeakMonth() int   { return argMax(s.Monthly[:]) + 1 }
func (s Seasonality) TroughMonth() int { return argMin(s.Monthly[:]) + 1 }

// MonthlyMeans collapses a daily series to one mean-per-day value per calendar
// month, dropping partial months at both ends. This is the input for
// Mann-Kendall: monthly values are close enough to independent for the test's
// assumptions to hold, and mean-per-day (not the month total) removes the
// 28-vs-31-day artefact.
func MonthlyMeans(start time.Time, x []float64) (vals []float64, labels []string) {
	if len(x) == 0 {
		return nil, nil
	}
	type acc struct {
		sum float64
		n   int
		key string
		y   int
		m   time.Month
	}
	var groups []*acc
	var cur *acc
	for i, v := range x {
		d := start.AddDate(0, 0, i)
		if cur == nil || cur.y != d.Year() || cur.m != d.Month() {
			cur = &acc{key: d.Format("2006-01"), y: d.Year(), m: d.Month()}
			groups = append(groups, cur)
		}
		if !math.IsNaN(v) {
			cur.sum += v
			cur.n++
		}
	}
	for _, g := range groups {
		days := daysInMonth(g.y, g.m)
		// Require most of the month to be present, else the mean is not
		// comparable with a full month.
		if g.n < days*3/4 {
			continue
		}
		vals = append(vals, g.sum/float64(g.n))
		labels = append(labels, g.key)
	}
	return
}

func daysInMonth(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// YoY compares the most recent 365 days with the 365 days immediately before,
// i.e. the same calendar window one year earlier.
//
// This is the most robust growth number available and the one to quote to a
// non-technical stakeholder: because both windows cover every season exactly
// once, it cannot be faked by seasonality, and it needs no model. Its cost is
// that it uses only two numbers, so it says nothing about the shape of the
// change — that is what the fitted trend is for.
func YoY(x []float64) (pct, recent, prior float64, ok bool) {
	n := len(x)
	if n < 730 {
		return math.NaN(), math.NaN(), math.NaN(), false
	}
	rec := x[n-365:]
	pri := x[n-730 : n-365]
	// Both halves need enough real observations for the means to be comparable.
	if len(Clean(rec)) < 300 || len(Clean(pri)) < 300 {
		return math.NaN(), math.NaN(), math.NaN(), false
	}
	recent, prior = Mean(rec), Mean(pri)
	if prior <= 0 {
		return math.NaN(), recent, prior, false
	}
	return (recent/prior - 1) * 100, recent, prior, true
}
