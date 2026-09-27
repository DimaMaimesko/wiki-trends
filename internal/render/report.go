package render

import (
	"fmt"
	"math"
	"time"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
	"github.com/dmytromaimesko/wiki-trends/internal/tseries"
)

// Report composes the shareable outputs from a dataset and its analysis.
//
// The one-page constraint is a feature, not a limitation: the audience is a
// founder deciding where to spend the next quarter, and the page has to carry
// the answer, the evidence and the caveats together. A chart that travels
// without its reliability grade and its limitations is worse than no chart,
// because it will be quoted.
type Report struct {
	Dataset  *model.Dataset
	Analysis *model.Analysis
	Title    string
	Question string // the user's actual question, echoed so the page is self-explaining
}

const (
	margin = 38.0
	pageW  = A4W
	pageH  = A4H
	bodyW  = pageW - 2*margin
)

// PDF renders the one-page report.
func (r Report) PDF() []byte {
	p := NewPDF(pageW, pageH)
	r.drawPage(p)
	return p.Bytes()
}

func (r Report) drawPage(c Canvas) {
	y := margin

	// ---------------- header
	title := r.Title
	if title == "" {
		title = r.topicName()
	}
	c.Text(margin, y+13, title, TextStyle{Size: 16, Bold: true, Color: ColInk})
	c.Text(pageW-margin, y+12, "wikitrends · "+time.Now().UTC().Format("2006-01-02"),
		TextStyle{Size: 7.5, Color: ColMuted, Align: AlignEnd})
	y += 22
	c.Text(margin, y+8, r.headerLine(), TextStyle{Size: 8.5, Color: ColMuted})
	y += 16
	if r.Question != "" {
		for _, ln := range wrapText("Question: "+r.Question, TextStyle{Size: 8}, bodyW) {
			c.Text(margin, y+8, ln, TextStyle{Size: 8, Color: ColMuted})
			y += 11
		}
	}
	c.Line(margin, y+3, pageW-margin, y+3, ColGrid, 0.8, false)
	y += 12

	// ---------------- verdict panel
	y = r.drawVerdicts(c, y)

	// ---------------- main chart
	series, start := r.chartSeries()
	metric := "Share of the edition's pageviews (per million)"
	if r.Analysis.Options.Metric == "absolute" {
		metric = "Pageviews per day"
	}
	tc := TimeChart{
		Title:    metric,
		Subtitle: "thin line: daily values · thick line: 7-day mean · dashed: fitted trend with 95% band · orange ticks: detected event spikes",
		Start:    start,
		Series:   series,
		LogY:     AutoLog(series),
	}
	tc.Draw(c, margin, y, bodyW, 208)
	y += 216

	// ---------------- comparison bars
	if rows := r.growthRows(); len(rows) > 0 {
		bh := math.Min(float64(len(rows))*24+24, 150)
		BarChart{
			Title: "Growth in attention share, % per year (spike-free estimate where growth is event-driven)",
			Rows:  rows,
			Unit:  "%",
		}.Draw(c, margin, y, bodyW, bh)
		y += bh + 6
	}

	// ---------------- metrics table
	y = r.drawTable(c, y)

	// ---------------- limitations
	y += 6
	c.Line(margin, y, pageW-margin, y, ColGrid, 0.8, false)
	y += 10
	c.Text(margin, y, "Assumptions and limitations", TextStyle{Size: 8.5, Bold: true, Color: ColInk})
	y += 11
	st := TextStyle{Size: 6.8, Color: ColMuted}
	for _, l := range r.Analysis.Limitations {
		if y > pageH-margin-8 {
			break
		}
		for i, ln := range wrapText("— "+l, st, bodyW) {
			if y > pageH-margin-8 {
				break
			}
			x := margin
			if i > 0 {
				x += 7
			}
			c.Text(x, y, ln, st)
			y += 8.2
		}
	}
}

func (r Report) topicName() string {
	t := r.Analysis.Topic
	if t.LabelEn != "" {
		return t.LabelEn
	}
	if t.Query != "" {
		return t.Query
	}
	return t.QID
}

func (r Report) headerLine() string {
	a := r.Analysis
	wikis := ""
	for i, w := range a.Wikis {
		if i > 0 {
			wikis += ", "
		}
		wikis += w.Wiki
	}
	qid := a.Topic.QID
	if qid == "" {
		qid = "explicit titles"
	}
	return fmt.Sprintf("%s · %s to %s · editions: %s · agent=%s access=%s",
		qid, a.Window.Start, a.Window.End, wikis, a.Params.Agent, a.Params.Access)
}

func (r Report) drawVerdicts(c Canvas, y float64) float64 {
	for _, w := range r.Analysis.Wikis {
		h := 0.0
		lines := wrapText(w.Verdict, TextStyle{Size: 8.2}, bodyW-70)
		reasonLines := []string{}
		for _, rs := range topReasons(w, 2) {
			reasonLines = append(reasonLines, wrapText("· "+rs, TextStyle{Size: 7}, bodyW-70)...)
		}
		h = 14 + float64(len(lines))*9.6 + float64(len(reasonLines))*8
		c.Rect(margin, y, bodyW, h, &ColPanel, nil, 0)

		gradeCol := gradeColor(w.Reliability.Grade)
		c.Rect(margin, y, 3, h, &gradeCol, nil, 0)

		c.Text(margin+10, y+11, w.Wiki+".wikipedia", TextStyle{Size: 9, Bold: true, Color: ColInk})
		c.Text(pageW-margin-8, y+11, "reliability "+w.Reliability.Grade,
			TextStyle{Size: 8, Bold: true, Color: gradeCol, Align: AlignEnd})
		yy := y + 22
		for _, ln := range lines {
			c.Text(margin+10, yy, ln, TextStyle{Size: 8.2, Color: ColInk})
			yy += 9.6
		}
		for _, ln := range reasonLines {
			c.Text(margin+10, yy, ln, TextStyle{Size: 7, Color: ColMuted})
			yy += 8
		}
		y += h + 5
	}
	return y
}

// topReasons surfaces the caveats that would most change a decision. Blockers
// come first because they mean the number should not be used at all.
func topReasons(w model.WikiAnalysis, n int) []string {
	var out []string
	out = append(out, w.Reliability.Blockers...)
	out = append(out, w.Reliability.Reasons...)
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func gradeColor(g string) Color {
	switch g {
	case "A":
		return ColPos
	case "B":
		return hex("#6a7f2a")
	case "C":
		return ColWarn
	}
	return ColNeg
}

func (r Report) chartSeries() ([]ChartSeries, time.Time) {
	start, _ := tseries.ParseDate(r.Analysis.Window.Start)
	var out []ChartSeries
	ci := 0
	for _, s := range r.Dataset.Series {
		if len(s.Views) == 0 {
			continue
		}
		wa := r.wikiAnalysis(s.Wiki)
		vals := s.Views.Floats()
		if r.Analysis.Options.Metric != "absolute" {
			vals = tseries.SharePerMillion(vals, s.ProjectViews.Floats())
		}
		cs := ChartSeries{
			Label:  s.Wiki,
			Values: vals,
			Color:  Palette[ci%len(Palette)],
		}
		ci++
		if wa != nil {
			t := wa.Share
			if r.Analysis.Options.Metric == "absolute" {
				t = wa.Absolute
			}
			if t != nil && !math.IsNaN(t.GrowthPctPerYear) {
				// Draw the fitted model as an exponential through the series mean,
				// so the dashed line is comparable with the data even on a log axis.
				n := float64(len(vals))
				gm := geoMean(vals)
				rate := math.Pow(1+t.GrowthPctPerYear/100, 1/365.25)
				half := n / 2
				cs.TrendFrom = gm * math.Pow(rate, -half)
				cs.TrendTo = gm * math.Pow(rate, half)
				cs.HasTrend = cs.TrendFrom > 0 && !math.IsInf(cs.TrendFrom, 0) && !math.IsInf(cs.TrendTo, 0)
				if !math.IsNaN(t.CILow) && !math.IsNaN(t.CIHigh) && cs.HasTrend {
					rl := math.Pow(1+t.CILow/100, 1/365.25)
					rh := math.Pow(1+t.CIHigh/100, 1/365.25)
					cs.CILowFrom, cs.CILowTo = gm*math.Pow(rl, -half), gm*math.Pow(rl, half)
					cs.CIHighFrom, cs.CIHighTo = gm*math.Pow(rh, -half), gm*math.Pow(rh, half)
					cs.HasCI = true
				}
			}
			if wa.Events != nil {
				for _, sp := range wa.Events.TopSpikes {
					if d, err := tseries.ParseDate(sp.Date); err == nil {
						cs.Spikes = append(cs.Spikes, int(d.Sub(start).Hours()/24))
					}
				}
			}
		}
		out = append(out, cs)
	}
	return out, start
}

// geoMean anchors the drawn trend line at the series' typical level on the log
// scale the model was fitted on, rather than at the arithmetic mean, which spikes
// would pull upward.
func geoMean(v []float64) float64 {
	s, n := 0.0, 0
	for _, x := range v {
		if math.IsNaN(x) || x <= 0 {
			continue
		}
		s += math.Log(x)
		n++
	}
	if n == 0 {
		return math.NaN()
	}
	return math.Exp(s / float64(n))
}

func (r Report) wikiAnalysis(wiki string) *model.WikiAnalysis {
	for i := range r.Analysis.Wikis {
		if r.Analysis.Wikis[i].Wiki == wiki {
			return &r.Analysis.Wikis[i]
		}
	}
	return nil
}

func (r Report) growthRows() []BarRow {
	var rows []BarRow
	for _, w := range r.Analysis.Wikis {
		t := w.Share
		if r.Analysis.Options.Metric == "absolute" {
			t = w.Absolute
		}
		row := BarRow{Label: w.Wiki}
		if t == nil || len(w.Reliability.Blockers) > 0 {
			row.Value = math.NaN()
			if len(w.Reliability.Blockers) > 0 {
				row.Note = w.Reliability.Blockers[0]
			} else {
				row.Note = "no data"
			}
			rows = append(rows, row)
			continue
		}
		g := t.GrowthPctPerYear
		if t.SpikeDependent {
			g = t.GrowthExSpikesPctPerYear
		}
		row.Value = g
		row.Note = fmt.Sprintf("CI %+.0f%%..%+.0f%% · adj p %s · grade %s",
			t.CILow, t.CIHigh, fmtP(t.PValueAdj), w.Reliability.Grade)
		if t.Direction == "flat" || t.Direction == "unclear" {
			row.Color = ColMuted
		}
		rows = append(rows, row)
	}
	return rows
}

// FmtP formats a p-value the way a reader should see it: "<0.001" rather than
// "0.000", which invites being read as exactly zero.
func FmtP(p float64) string { return fmtP(p) }

func fmtP(p float64) string {
	if math.IsNaN(p) {
		return "n/a"
	}
	if p < 0.001 {
		return "<0.001"
	}
	return fmt.Sprintf("%.3f", p)
}

func (r Report) drawTable(c Canvas, y float64) float64 {
	cols := []struct {
		hdr   string
		w     float64
		align Align
	}{
		{"edition", 52, AlignStart},
		{"views/day", 54, AlignEnd},
		{"per million", 58, AlignEnd},
		{"%/yr", 46, AlignEnd},
		{"95% CI", 82, AlignEnd},
		{"YoY", 46, AlignEnd},
		{"adj p", 44, AlignEnd},
		{"wiki %/yr", 50, AlignEnd},
		{"spikes", 40, AlignEnd},
		{"grade", 34, AlignEnd},
	}
	hs := TextStyle{Size: 7, Bold: true, Color: ColMuted}
	x := margin
	for _, cl := range cols {
		ax := x
		if cl.align == AlignEnd {
			ax = x + cl.w - 2
		}
		c.Text(ax, y+7, cl.hdr, TextStyle{Size: hs.Size, Bold: true, Color: hs.Color, Align: cl.align})
		x += cl.w
	}
	y += 10
	c.Line(margin, y, pageW-margin, y, ColGrid, 0.6, false)
	y += 3

	for _, w := range r.Analysis.Wikis {
		t := w.Share
		if r.Analysis.Options.Metric == "absolute" {
			t = w.Absolute
		}
		vals := []string{w.Wiki, "—", "—", "—", "—", "—", "—", "—", "—", w.Reliability.Grade}
		if w.Level != nil {
			vals[1] = fmtNum(w.Level.Last90Mean)
			vals[2] = fmt.Sprintf("%.2f", w.Level.Last90PerMillion)
		}
		if t != nil && !math.IsNaN(t.GrowthPctPerYear) {
			vals[3] = fmt.Sprintf("%+.0f%%", t.GrowthPctPerYear)
			vals[4] = fmt.Sprintf("%+.0f..%+.0f", t.CILow, t.CIHigh)
			if t.YoYComparable {
				vals[5] = fmt.Sprintf("%+.0f%%", t.YoYPct)
			}
			vals[6] = fmtP(t.PValueAdj)
		}
		if w.Project_ != nil && !math.IsNaN(w.Project_.GrowthPctPerYear) {
			vals[7] = fmt.Sprintf("%+.0f%%", w.Project_.GrowthPctPerYear)
		}
		if w.Events != nil {
			vals[8] = fmt.Sprintf("%d", w.Events.SpikeDays)
		}
		x = margin
		for i, cl := range cols {
			ax := x
			if cl.align == AlignEnd {
				ax = x + cl.w - 2
			}
			st := TextStyle{Size: 7.4, Color: ColInk, Align: cl.align}
			if i == 0 || i == 9 {
				st.Bold = true
			}
			if i == 9 {
				st.Color = gradeColor(w.Reliability.Grade)
			}
			c.Text(ax, y+7, vals[i], st)
			x += cl.w
		}
		y += 11
	}
	c.Text(margin, y+7, "views/day and per-million are 90-day means. \"wiki %/yr\" is the whole edition's traffic trend, for context.",
		TextStyle{Size: 6.6, Color: ColMuted})
	return y + 12
}
