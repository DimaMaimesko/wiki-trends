package render

import (
	"fmt"
	"math"
	"time"
)

// ChartSeries is one line on a time chart.
type ChartSeries struct {
	Label  string
	Values []float64 // NaN = gap; the line breaks rather than interpolating
	Color  Color
	Dashed bool
	// Trend, when set, is drawn as a dashed straight line across the panel: the
	// fitted log-linear model made visible so a reader can judge for themselves
	// whether one line describes the data.
	TrendFrom, TrendTo float64
	HasTrend           bool
	// CILow/CIHigh draw the confidence band of the fitted trend. Showing the band
	// is the difference between "interest is growing 40%/yr" and an honest
	// picture of how much the data actually pins that down.
	CILowFrom, CILowTo   float64
	CIHighFrom, CIHighTo float64
	HasCI                bool
	// Spikes mark detected event days along the top of the panel.
	Spikes []int
}

// TimeChart draws one or more daily series against a calendar axis.
//
// Two rendering decisions do most of the work for legibility. Raw daily
// pageviews are drawn faintly and a 7-day centred mean drawn solid on top: the
// weekly cycle is real but it is never the thing being decided, and a chart of
// raw dailies reads as a grey band. And series are labelled at their right-hand
// end rather than in a legend box, so the eye never has to travel between a
// colour swatch and a line.
type TimeChart struct {
	Title    string
	Subtitle string
	YLabel   string
	Start    time.Time
	Series   []ChartSeries
	LogY     bool // set explicitly; callers use AutoLog to decide
	Footnote string
	Smooth   int // rolling-mean width for the emphasised line; 0 disables
}

// AutoLog picks a log y-axis when the series span more than one order of
// magnitude, which is the normal case when comparing a large and a small wiki.
// On a linear axis the smaller wiki would be pinned to the baseline and its
// growth made invisible.
func AutoLog(series []ChartSeries) bool {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, s := range series {
		for _, v := range s.Values {
			if math.IsNaN(v) || v <= 0 {
				continue
			}
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	if math.IsInf(lo, 1) || lo <= 0 {
		return false
	}
	return hi/lo > 15
}

type box struct{ X, Y, W, H float64 }

// Draw renders the chart into the given rectangle.
func (t TimeChart) Draw(c Canvas, x, y, w, h float64) {
	if t.Smooth == 0 {
		t.Smooth = 7
	}
	titleH := 0.0
	if t.Title != "" {
		c.Text(x, y+11, t.Title, TextStyle{Size: 10.5, Bold: true, Color: ColInk})
		titleH += 16
	}
	if t.Subtitle != "" {
		c.Text(x, y+titleH+9, t.Subtitle, TextStyle{Size: 8, Color: ColMuted})
		titleH += 13
	}
	footH := 0.0
	if t.Footnote != "" {
		footH = 12
	}

	// Right gutter holds the end-of-line labels.
	labelW := 0.0
	for _, s := range t.Series {
		labelW = math.Max(labelW, c.TextWidth(s.Label, TextStyle{Size: 8}))
	}
	if labelW > 0 {
		labelW += 8
	}
	if labelW > w*0.28 {
		labelW = w * 0.28
	}

	p := box{X: x + 42, Y: y + titleH + 4, W: w - 42 - labelW, H: h - titleH - footH - 20}
	if p.W < 40 || p.H < 30 {
		return
	}

	lo, hi := dataRange(t.Series, t.LogY)
	if math.IsInf(lo, 1) {
		c.Text(x+w/2, p.Y+p.H/2, "no data", TextStyle{Size: 9, Color: ColMuted, Align: AlignMiddle})
		return
	}
	toY := func(v float64) float64 {
		if t.LogY {
			if v <= 0 {
				return p.Y + p.H
			}
			return p.Y + p.H - (math.Log10(v)-math.Log10(lo))/(math.Log10(hi)-math.Log10(lo))*p.H
		}
		return p.Y + p.H - (v-lo)/(hi-lo)*p.H
	}
	n := seriesLen(t.Series)
	toX := func(i int) float64 {
		if n <= 1 {
			return p.X
		}
		return p.X + float64(i)/float64(n-1)*p.W
	}

	// panel
	c.Rect(p.X, p.Y, p.W, p.H, &ColPanel, nil, 0)

	// y gridlines + labels
	for _, tk := range yTicks(lo, hi, t.LogY) {
		yy := toY(tk)
		if yy < p.Y-0.5 || yy > p.Y+p.H+0.5 {
			continue
		}
		c.Line(p.X, yy, p.X+p.W, yy, ColGrid, 0.5, false)
		c.Text(p.X-5, yy+2.6, fmtNum(tk), TextStyle{Size: 7.5, Color: ColMuted, Align: AlignEnd})
	}
	if t.YLabel != "" {
		c.Text(x, p.Y-4, t.YLabel, TextStyle{Size: 7.5, Color: ColMuted})
	}

	// x ticks
	for _, tk := range xTicks(t.Start, n) {
		xx := toX(tk.idx)
		c.Line(xx, p.Y+p.H, xx, p.Y+p.H+3, ColAxis, 0.6, false)
		c.Text(xx, p.Y+p.H+12, tk.label, TextStyle{Size: 7.5, Color: ColMuted, Align: AlignMiddle})
	}
	c.Line(p.X, p.Y+p.H, p.X+p.W, p.Y+p.H, ColAxis, 0.7, false)

	// With more than a few series, the faint raw dailies stop being context and
	// become noise that hides the smoothed lines underneath — and they dominate
	// the file size. Above four series only the 7-day means are drawn.
	showRaw := len(t.Series) <= 4
	// Cap the number of drawn vertices at roughly two per horizontal point.
	// Beyond that the extra vertices are invisible but still cost bytes, which
	// matters once a window is measured in years rather than months.
	maxPts := int(p.W * 2)

	// series
	for _, s := range t.Series {
		if s.HasCI {
			band := []Pt{
				{toX(0), toY(s.CIHighFrom)}, {toX(n - 1), toY(s.CIHighTo)},
				{toX(n - 1), toY(s.CILowTo)}, {toX(0), toY(s.CILowFrom)},
			}
			c.Polygon(band, s.Color, 0.08)
		}
		// raw daily values, faint
		if showRaw {
			for _, seg := range segments(s.Values) {
				pts := make([]Pt, 0, len(seg.idx))
				for _, i := range seg.idx {
					pts = append(pts, Pt{toX(i), toY(s.Values[i])})
				}
				c.Polyline(decimate(pts, maxPts), s.Color, 0.45, false)
			}
		}
		// smoothed line, solid
		sm := rollingMean(s.Values, t.Smooth)
		for _, seg := range segments(sm) {
			pts := make([]Pt, 0, len(seg.idx))
			for _, i := range seg.idx {
				pts = append(pts, Pt{toX(i), toY(sm[i])})
			}
			c.Polyline(decimate(pts, maxPts), s.Color, 1.6, s.Dashed)
		}
		if s.HasTrend {
			c.Line(toX(0), toY(s.TrendFrom), toX(n-1), toY(s.TrendTo), s.Color, 1.1, true)
		}
		// spike markers as short ticks under the top edge
		for _, i := range s.Spikes {
			if i >= 0 && i < n {
				xx := toX(i)
				c.Line(xx, p.Y+1, xx, p.Y+6, ColWarn, 1.0, false)
			}
		}
		// end-of-line label
		if labelW > 0 && s.Label != "" {
			if li := lastValid(sm); li >= 0 {
				lbl := Ellipsis(s.Label, TextStyle{Size: 8}, labelW-8)
				c.Text(p.X+p.W+5, toY(sm[li])+2.8, lbl, TextStyle{Size: 8, Bold: true, Color: s.Color})
			}
		}
	}

	if t.Footnote != "" {
		c.Text(x, y+h-2, t.Footnote, TextStyle{Size: 7, Color: ColMuted})
	}
}

// BarRow is one row of a horizontal bar chart.
type BarRow struct {
	Label string
	Value float64
	Note  string
	Color Color
}

// BarChart draws signed horizontal bars around a zero line: the right shape for
// growth rates, where the sign is the headline and the baseline must be visible.
type BarChart struct {
	Title    string
	Rows     []BarRow
	Unit     string
	Footnote string
}

func (b BarChart) Draw(c Canvas, x, y, w, h float64) {
	top := y
	if b.Title != "" {
		c.Text(x, y+11, b.Title, TextStyle{Size: 10.5, Bold: true, Color: ColInk})
		top += 18
	}
	if len(b.Rows) == 0 {
		return
	}
	labelW := 0.0
	for _, r := range b.Rows {
		labelW = math.Max(labelW, c.TextWidth(r.Label, TextStyle{Size: 8, Bold: true}))
	}
	labelW = math.Min(labelW+8, w*0.3)

	noteW := 0.0
	for _, r := range b.Rows {
		if r.Note != "" {
			noteW = math.Max(noteW, c.TextWidth(r.Note, TextStyle{Size: 7.5}))
		}
	}
	noteW = math.Min(noteW+8, w*0.34)

	px := x + labelW
	pw := w - labelW - noteW
	rowH := (h - (top - y) - 10) / float64(len(b.Rows))
	if rowH > 26 {
		rowH = 26
	}

	maxAbs := 0.0
	for _, r := range b.Rows {
		if !math.IsNaN(r.Value) {
			maxAbs = math.Max(maxAbs, math.Abs(r.Value))
		}
	}
	if maxAbs == 0 {
		maxAbs = 1
	}
	hasNeg := false
	for _, r := range b.Rows {
		if r.Value < 0 {
			hasNeg = true
		}
	}
	zero := px
	scale := pw
	if hasNeg {
		zero = px + pw/2
		scale = pw / 2
	}

	for i, r := range b.Rows {
		cy := top + float64(i)*rowH + rowH/2
		c.Text(x, cy+2.8, Ellipsis(r.Label, TextStyle{Size: 8, Bold: true}, labelW-6),
			TextStyle{Size: 8, Bold: true, Color: ColInk})
		if math.IsNaN(r.Value) {
			c.Text(zero+4, cy+2.8, "not measurable", TextStyle{Size: 7.5, Color: ColMuted})
		} else {
			bl := r.Value / maxAbs * scale
			col := r.Color
			if col == (Color{}) {
				col = ColPos
				if r.Value < 0 {
					col = ColNeg
				}
			}
			bh := math.Min(rowH*0.5, 11)
			bx, bw := zero, bl
			if bl < 0 {
				bx, bw = zero+bl, -bl
			}
			c.Rect(bx, cy-bh/2, math.Max(bw, 0.6), bh, &col, nil, 0)
			lbl := fmt.Sprintf("%+.0f%s", r.Value, b.Unit)
			if bl >= 0 {
				c.Text(zero+bl+4, cy+2.8, lbl, TextStyle{Size: 7.5, Color: ColInk})
			} else {
				c.Text(zero+bl-4, cy+2.8, lbl, TextStyle{Size: 7.5, Color: ColInk, Align: AlignEnd})
			}
		}
		if r.Note != "" {
			c.Text(x+w, cy+2.8, Ellipsis(r.Note, TextStyle{Size: 7.5}, noteW-6),
				TextStyle{Size: 7.5, Color: ColMuted, Align: AlignEnd})
		}
	}
	c.Line(zero, top, zero, top+float64(len(b.Rows))*rowH, ColAxis, 0.7, false)
	if b.Footnote != "" {
		c.Text(x, y+h-1, b.Footnote, TextStyle{Size: 7, Color: ColMuted})
	}
}

// --- helpers

type seg struct{ idx []int }

// segments splits a series at gaps so a missing week leaves a visible break
// instead of a straight line implying data that was never measured.
func segments(v []float64) []seg {
	var out []seg
	var cur []int
	for i, x := range v {
		if math.IsNaN(x) {
			if len(cur) > 1 {
				out = append(out, seg{cur})
			}
			cur = nil
			continue
		}
		cur = append(cur, i)
	}
	if len(cur) > 1 {
		out = append(out, seg{cur})
	}
	return out
}

// decimate reduces a polyline to at most maxPts vertices while preserving its
// visible shape, by keeping the minimum and maximum of each horizontal bucket.
//
// Plain stride sampling would be simpler and wrong: dropping every other day of a
// spiky series can drop the spike, which is precisely the feature a reader is
// looking for. Min/max bucketing keeps the envelope exactly, so a one-day event
// remains visible at any zoom level. This is what makes a ten-year daily window
// renderable without a multi-megabyte file.
func decimate(pts []Pt, maxPts int) []Pt {
	if maxPts < 4 || len(pts) <= maxPts {
		return pts
	}
	buckets := maxPts / 2
	out := make([]Pt, 0, maxPts+2)
	out = append(out, pts[0])
	for b := 0; b < buckets; b++ {
		lo := b * len(pts) / buckets
		hi := (b + 1) * len(pts) / buckets
		if hi <= lo {
			continue
		}
		mn, mx := lo, lo
		for i := lo; i < hi; i++ {
			if pts[i].Y < pts[mn].Y {
				mn = i
			}
			if pts[i].Y > pts[mx].Y {
				mx = i
			}
		}
		// emit in x order so the line does not zig-zag backwards
		if mn <= mx {
			out = append(out, pts[mn], pts[mx])
		} else {
			out = append(out, pts[mx], pts[mn])
		}
	}
	return append(out, pts[len(pts)-1])
}

func rollingMean(v []float64, w int) []float64 {
	if w < 2 {
		return v
	}
	half := w / 2
	out := make([]float64, len(v))
	for i := range v {
		lo, hi := i-half, i+half
		if lo < 0 {
			lo = 0
		}
		if hi >= len(v) {
			hi = len(v) - 1
		}
		s, n := 0.0, 0
		for j := lo; j <= hi; j++ {
			if !math.IsNaN(v[j]) {
				s += v[j]
				n++
			}
		}
		if n < (hi-lo+1)/2 {
			out[i] = math.NaN()
			continue
		}
		out[i] = s / float64(n)
	}
	return out
}

func seriesLen(ss []ChartSeries) int {
	n := 0
	for _, s := range ss {
		if len(s.Values) > n {
			n = len(s.Values)
		}
	}
	return n
}

func lastValid(v []float64) int {
	for i := len(v) - 1; i >= 0; i-- {
		if !math.IsNaN(v[i]) {
			return i
		}
	}
	return -1
}

func dataRange(ss []ChartSeries, logY bool) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, s := range ss {
		sm := rollingMean(s.Values, 7)
		for i := range s.Values {
			for _, v := range []float64{s.Values[i], sm[i]} {
				if math.IsNaN(v) {
					continue
				}
				if logY && v <= 0 {
					continue
				}
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
		}
		if s.HasCI {
			for _, v := range []float64{s.CILowFrom, s.CILowTo, s.CIHighFrom, s.CIHighTo} {
				if math.IsNaN(v) || (logY && v <= 0) {
					continue
				}
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
		}
	}
	if math.IsInf(lo, 1) {
		return lo, hi
	}
	if logY {
		return lo / 1.4, hi * 1.4
	}
	if hi == lo {
		hi = lo + 1
	}
	pad := (hi - lo) * 0.08
	lo -= pad
	hi += pad
	if lo < 0 {
		lo = 0
	}
	return
}

func yTicks(lo, hi float64, logY bool) []float64 {
	var out []float64
	if logY {
		for e := math.Floor(math.Log10(lo)); e <= math.Ceil(math.Log10(hi)); e++ {
			for _, m := range []float64{1, 2, 5} {
				v := m * math.Pow(10, e)
				if v >= lo && v <= hi {
					out = append(out, v)
				}
			}
		}
		if len(out) > 8 {
			var thin []float64
			for _, v := range out {
				e := math.Log10(v)
				if e == math.Trunc(e) {
					thin = append(thin, v)
				}
			}
			if len(thin) >= 2 {
				return thin
			}
		}
		return out
	}
	step := niceStep(hi-lo, 5)
	for v := math.Ceil(lo/step) * step; v <= hi; v += step {
		out = append(out, v)
	}
	return out
}

func niceStep(span float64, target int) float64 {
	if span <= 0 || target <= 0 {
		return 1
	}
	raw := span / float64(target)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 2.5, 5} {
		if raw <= m*mag {
			return m * mag
		}
	}
	return 10 * mag
}

type xtick struct {
	idx   int
	label string
}

// xTicks chooses a calendar granularity that yields roughly six labels, so a
// three-month window is labelled by month and a five-year window by year.
func xTicks(start time.Time, n int) []xtick {
	if n <= 1 {
		return nil
	}
	end := start.AddDate(0, 0, n-1)
	months := int(end.Sub(start).Hours()/24/30.44) + 1
	var out []xtick
	add := func(d time.Time, label string) {
		i := int(d.Sub(start).Hours() / 24)
		if i >= 0 && i < n {
			out = append(out, xtick{i, label})
		}
	}
	switch {
	case months <= 8:
		for d := monthStart(start); !d.After(end); d = d.AddDate(0, 1, 0) {
			add(d, d.Format("Jan"))
		}
	case months <= 30:
		for d := monthStart(start); !d.After(end); d = d.AddDate(0, 3, 0) {
			add(d, d.Format("Jan 06"))
		}
	case months <= 84:
		for d := monthStart(start); !d.After(end); d = d.AddDate(0, 6, 0) {
			add(d, d.Format("Jan 06"))
		}
	default:
		for y := start.Year(); y <= end.Year(); y++ {
			add(time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC), fmt.Sprint(y))
		}
	}
	return out
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func fmtNum(v float64) string {
	a := math.Abs(v)
	switch {
	case a >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case a >= 1e4:
		return fmt.Sprintf("%.0fk", v/1e3)
	case a >= 1e3:
		return fmt.Sprintf("%.1fk", v/1e3)
	case a >= 10:
		return fmt.Sprintf("%.0f", v)
	case a >= 1:
		return fmt.Sprintf("%.1f", v)
	case a > 0:
		return fmt.Sprintf("%.2f", v)
	}
	return "0"
}
