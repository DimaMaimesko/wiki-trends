// Package stats implements the estimators the skill uses to turn a pageview
// series into a defensible claim about interest.
//
// Every function here is NaN-aware: missing days are skipped, never treated as
// zero. Three properties of Wikipedia pageview data drive the choices made in
// this package:
//
//  1. The series is multiplicative. Doubling happens; adding a constant does
//     not. So trends are estimated on log(views+1) and reported as compound
//     % per year, which is comparable across wikis of wildly different size.
//
//  2. The series is strongly autocorrelated (weekly rhythm plus persistent
//     news cycles). Ordinary least-squares standard errors assume independence
//     and, on daily pageviews, understate uncertainty by a factor of 2-5. That
//     turns noise into "statistically significant growth". Newey-West HAC
//     errors are used instead.
//
//  3. The series is spiky. One news event can carry a year's apparent growth.
//     Every trend is therefore computed twice: as-is, and with spikes
//     winsorized. Disagreement between the two is reported, not averaged away.
package stats

import (
	"math"
	"sort"
)

// Clean returns the non-NaN values of x.
func Clean(x []float64) []float64 {
	out := make([]float64, 0, len(x))
	for _, v := range x {
		if !math.IsNaN(v) {
			out = append(out, v)
		}
	}
	return out
}

func Mean(x []float64) float64 {
	n, s := 0, 0.0
	for _, v := range x {
		if math.IsNaN(v) {
			continue
		}
		s += v
		n++
	}
	if n == 0 {
		return math.NaN()
	}
	return s / float64(n)
}

func Sum(x []float64) float64 {
	s := 0.0
	for _, v := range x {
		if !math.IsNaN(v) {
			s += v
		}
	}
	return s
}

func Quantile(x []float64, q float64) float64 {
	c := Clean(x)
	if len(c) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), c...)
	sort.Float64s(s)
	if len(s) == 1 {
		return s[0]
	}
	pos := q * float64(len(s)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return s[lo] + (s[hi]-s[lo])*(pos-float64(lo))
}

func Median(x []float64) float64 { return Quantile(x, 0.5) }

// MAD is the median absolute deviation scaled to be a consistent estimator of
// the standard deviation for normal data (the 1.4826 factor). It is used instead
// of the standard deviation everywhere because a single 50x spike would inflate
// an SD enough to hide every other anomaly.
func MAD(x []float64) float64 {
	m := Median(x)
	if math.IsNaN(m) {
		return math.NaN()
	}
	d := make([]float64, 0, len(x))
	for _, v := range x {
		if !math.IsNaN(v) {
			d = append(d, math.Abs(v-m))
		}
	}
	return 1.4826 * Median(d)
}

// RollingMedian returns a centred rolling median of odd width w. Positions
// without enough neighbours use the widest available window rather than NaN, so
// the baseline is defined at the edges too.
func RollingMedian(x []float64, w int) []float64 {
	if w%2 == 0 {
		w++
	}
	half := w / 2
	out := make([]float64, len(x))
	buf := make([]float64, 0, w)
	for i := range x {
		lo, hi := i-half, i+half
		if lo < 0 {
			lo = 0
		}
		if hi >= len(x) {
			hi = len(x) - 1
		}
		buf = buf[:0]
		for j := lo; j <= hi; j++ {
			if !math.IsNaN(x[j]) {
				buf = append(buf, x[j])
			}
		}
		out[i] = Median(buf)
	}
	return out
}

// CentredMA is a NaN-skipping centred moving average, used to detrend a series
// before measuring seasonality. Width 7 removes the weekly cycle; width 365
// removes the annual one.
func CentredMA(x []float64, w int) []float64 {
	half := w / 2
	out := make([]float64, len(x))
	for i := range x {
		lo, hi := i-half, i+half
		if lo < 0 || hi >= len(x) {
			out[i] = math.NaN() // refuse to fabricate an edge value
			continue
		}
		s, n := 0.0, 0
		for j := lo; j <= hi; j++ {
			if !math.IsNaN(x[j]) {
				s += x[j]
				n++
			}
		}
		if n < w/2 {
			out[i] = math.NaN()
			continue
		}
		out[i] = s / float64(n)
	}
	return out
}

// Winsorize clips values outside the given quantiles, preserving length and NaN
// positions. Used to ask "does the trend survive without its extremes?".
func Winsorize(x []float64, loQ, hiQ float64) []float64 {
	lo, hi := Quantile(x, loQ), Quantile(x, hiQ)
	out := make([]float64, len(x))
	for i, v := range x {
		switch {
		case math.IsNaN(v):
			out[i] = v
		case v < lo:
			out[i] = lo
		case v > hi:
			out[i] = hi
		default:
			out[i] = v
		}
	}
	return out
}

// Log1p maps a count series to the scale trends are estimated on.
func Log1p(x []float64) []float64 {
	out := make([]float64, len(x))
	for i, v := range x {
		if math.IsNaN(v) {
			out[i] = math.NaN()
		} else {
			out[i] = math.Log1p(math.Max(v, 0))
		}
	}
	return out
}

// NormCDF via the complementary error function; avoids a dependency and is
// accurate to ~1e-15.
func NormCDF(z float64) float64 { return 0.5 * math.Erfc(-z/math.Sqrt2) }

// TwoSidedP converts a z statistic to a two-sided p-value.
func TwoSidedP(z float64) float64 {
	p := 2 * (1 - NormCDF(math.Abs(z)))
	return math.Max(0, math.Min(1, p))
}
