package stats

import (
	"math"
	"time"
)

// SpikeEvent is a run of consecutive anomalous days treated as one event: a news
// cycle lasts days, and counting each day separately would overstate how many
// independent things happened.
type SpikeEvent struct {
	StartIdx int
	EndIdx   int
	PeakIdx  int
	Date     string  // date of the peak day
	Views    float64 // peak day's value
	Baseline float64 // local baseline at the peak
	Z        float64 // peak robust z on the log scale
	Views7   float64 // total excess views attributable to the event
}

// DetectSpikes finds multiplicative anomalies against a local baseline.
//
// Method: baseline = 29-day centred rolling median; residual = log(v+1) -
// log(baseline+1); scale = MAD of those residuals; z = residual / scale.
//
// Three deliberate choices:
//   - Rolling *median*, not mean, so that the spike does not lift its own
//     baseline.
//   - Log residuals, so the threshold means "k times the usual level" rather
//     than "k views above it" — the same test then works for an article with 30
//     views/day and one with 300 000.
//   - MAD, not SD, so one huge event does not raise the bar high enough to hide
//     every other event.
//
// Only positive spikes are reported as events; sustained drops are handled by
// DetectLevelShifts, which is the right model for them (a step, not a spike).
func DetectSpikes(start time.Time, x []float64, z float64, window int) []SpikeEvent {
	if len(x) < 30 {
		return nil
	}
	base := RollingMedian(x, window)
	resid := make([]float64, len(x))
	for i := range x {
		if math.IsNaN(x[i]) || math.IsNaN(base[i]) {
			resid[i] = math.NaN()
			continue
		}
		resid[i] = math.Log1p(x[i]) - math.Log1p(base[i])
	}
	scale := MAD(resid)
	if math.IsNaN(scale) || scale <= 0 {
		return nil
	}
	flag := make([]bool, len(x))
	zs := make([]float64, len(x))
	for i := range resid {
		if math.IsNaN(resid[i]) {
			continue
		}
		zs[i] = resid[i] / scale
		flag[i] = zs[i] >= z
	}
	var out []SpikeEvent
	for i := 0; i < len(flag); i++ {
		if !flag[i] {
			continue
		}
		j := i
		// allow a one-day dip inside an event: decay is rarely monotone
		for j+1 < len(flag) && (flag[j+1] || (j+2 < len(flag) && flag[j+2])) {
			j++
		}
		ev := SpikeEvent{StartIdx: i, EndIdx: j, PeakIdx: i, Z: zs[i]}
		for k := i; k <= j; k++ {
			if zs[k] > ev.Z {
				ev.Z, ev.PeakIdx = zs[k], k
			}
			if !math.IsNaN(x[k]) && !math.IsNaN(base[k]) {
				ev.Views7 += x[k] - base[k]
			}
		}
		ev.Views = x[ev.PeakIdx]
		ev.Baseline = base[ev.PeakIdx]
		ev.Date = start.AddDate(0, 0, ev.PeakIdx).Format("2006-01-02")
		out = append(out, ev)
		i = j
	}
	return out
}

// SpikeViewShare is the fraction of all views in the window that sit inside
// spike events, i.e. how much of the topic's measured audience is one-off
// attention rather than baseline demand. A high share with a positive trend is
// the signature of "a thing happened", not "a market is forming".
func SpikeViewShare(x []float64, events []SpikeEvent) float64 {
	total := Sum(x)
	if total <= 0 {
		return 0
	}
	in := 0.0
	for _, e := range events {
		for k := e.StartIdx; k <= e.EndIdx && k < len(x); k++ {
			if !math.IsNaN(x[k]) {
				in += x[k]
			}
		}
	}
	return in / total
}

// MaskSpikes replaces spike days with the local baseline so a trend can be
// refitted without them. It returns a copy; the original is never mutated,
// because both versions are reported side by side.
func MaskSpikes(x []float64, events []SpikeEvent, window int) []float64 {
	out := append([]float64(nil), x...)
	if len(events) == 0 {
		return out
	}
	base := RollingMedian(x, window)
	for _, e := range events {
		for k := e.StartIdx; k <= e.EndIdx && k < len(out); k++ {
			if !math.IsNaN(base[k]) {
				out[k] = base[k]
			}
		}
	}
	return out
}

// LevelShift is a persistent step in the series that the trend does not explain.
type LevelShift struct {
	Idx      int
	Date     string
	Before   float64
	After    float64
	RatioPct float64
	Z        float64
}

// DetectLevelShifts looks for a persistent step that the trend does not explain.
//
// Model, fitted jointly for every candidate break date t:
//
//	log(v_i) = a + b*i + c*1{i >= t} + u_i
//
// Estimating the slope b and the step c *together* is essential and was the
// result of getting it wrong first: if you fit a trend and then hunt for a step
// in the residuals, ordinary least squares has already tilted the line to
// straddle the step, and a 40% drop shows up as a 13% one. The joint fit keeps
// the two effects separate. The break date is chosen by minimum residual sum of
// squares, then the winning step gets Newey-West standard errors so its
// significance is not inflated by autocorrelation.
//
// Why the skill cares: after trend and seasonality are accounted for, a
// surviving discontinuity is usually *measurement*, not demand — a page move or
// merge, a redirect retarget, a change in Wikimedia's bot classification, a new
// mobile app release. These are reported for interpretation rather than modelled
// away, because the right response is to check the article history, not to
// adjust the number.
//
// logx must be the (optionally seasonally adjusted) log series, not residuals.
func DetectLevelShifts(start time.Time, logx []float64, minSeg int, minRatioPct float64) []LevelShift {
	n := len(logx)
	if n < 2*minSeg+1 || minSeg < 10 {
		return nil
	}
	// Coarse-to-fine search: a 7-day grid first, then a local refinement. A full
	// per-day sweep is affordable too, but this keeps a multi-year daily scan of
	// 20 wikis comfortably interactive.
	bestT, bestSSR := -1, math.Inf(1)
	eval := func(t int) *LM {
		X := make([][]float64, n)
		for i := 0; i < n; i++ {
			step := 0.0
			if i >= t {
				step = 1
			}
			X[i] = []float64{1, float64(i), step}
		}
		return FitOLS(X, logx, 0)
	}
	for t := minSeg; t <= n-minSeg; t += 7 {
		if m := eval(t); m != nil && m.SSR < bestSSR {
			bestSSR, bestT = m.SSR, t
		}
	}
	if bestT < 0 {
		return nil
	}
	for t := bestT - 6; t <= bestT+6; t++ {
		if t < minSeg || t > n-minSeg {
			continue
		}
		if m := eval(t); m != nil && m.SSR < bestSSR {
			bestSSR, bestT = m.SSR, t
		}
	}

	// refit the winner with HAC errors
	X := make([][]float64, n)
	for i := 0; i < n; i++ {
		step := 0.0
		if i >= bestT {
			step = 1
		}
		X[i] = []float64{1, float64(i), step}
	}
	m := FitOLS(X, logx, NWLag(len(Clean(logx))))
	if m == nil || math.IsNaN(m.SE[2]) || m.SE[2] <= 0 {
		return nil
	}
	c := m.Beta[2]
	ratioPct := (math.Exp(c) - 1) * 100
	z := c / m.SE[2]
	// Two gates: the step must be large enough to matter to a product decision,
	// and distinguishable from noise once autocorrelation is priced in.
	if math.Abs(ratioPct) < minRatioPct || math.Abs(z) < 3 {
		return nil
	}
	return []LevelShift{{
		Idx:      bestT,
		Date:     start.AddDate(0, 0, bestT).Format("2006-01-02"),
		Before:   0,
		After:    c,
		RatioPct: ratioPct,
		Z:        z,
	}}
}
