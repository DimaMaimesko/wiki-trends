package render

import (
	"bytes"
	"fmt"
	"math"
	"strings"

	"github.com/dmytromaimesko/wiki-trends/internal/tseries"
)

// HTML renders a self-contained single-file report.
//
// It exists alongside the PDF rather than instead of it because each format wins
// somewhere the other loses. The PDF is what gets attached to an email and read
// in a meeting. The HTML keeps native article titles in every script, links out
// to the articles so a claim can be checked in two clicks, works in dark mode,
// and prints to the same one-page A4 layout from the browser. Both are generated
// from the same numbers, so they cannot disagree.
func (r Report) HTML() []byte {
	var b bytes.Buffer
	a := r.Analysis

	series, start := r.chartSeries()
	metricLabel := "Share of the edition's pageviews (per million)"
	if a.Options.Metric == "absolute" {
		metricLabel = "Pageviews per day"
	}

	chart := NewSVG(920, 300)
	TimeChart{
		Title:    metricLabel,
		Subtitle: "thin line: daily values · thick line: 7-day mean · dashed: fitted trend with 95% band · orange ticks: event spikes",
		Start:    start,
		Series:   series,
		LogY:     AutoLog(series),
	}.Draw(chart, 8, 4, 904, 292)

	var bars *SVG
	if rows := r.growthRows(); len(rows) > 0 {
		h := float64(len(rows))*26 + 34
		bars = NewSVG(920, h)
		BarChart{
			Title: "Growth in attention share, % per year",
			Rows:  rows,
			Unit:  "%",
		}.Draw(bars, 8, 2, 904, h-6)
	}

	var season *SVG
	if s := r.seasonRows(); len(s) > 0 {
		h := float64(len(s))*26 + 34
		season = NewSVG(920, h)
		BarChart{
			Title:    "Seasonality: month with the highest and lowest demand",
			Rows:     s,
			Unit:     "%",
			Footnote: "deviation of the month's index from the annual average; requires at least two years of data",
		}.Draw(season, 8, 2, 904, h-6)
	}

	title := r.Title
	if title == "" {
		title = r.topicName()
	}

	b.WriteString("<title>" + escapeXML(title) + " — Wikipedia interest</title>\n")
	b.WriteString(htmlCSS)
	fmt.Fprintf(&b, `<main>
<header>
  <h1>%s</h1>
  <p class="meta">%s</p>
`, escapeXML(title), escapeXML(r.headerLine()))
	if r.Question != "" {
		fmt.Fprintf(&b, `  <p class="q">%s</p>`+"\n", escapeXML(r.Question))
	}
	b.WriteString("</header>\n")

	// verdicts
	for _, w := range a.Wikis {
		fmt.Fprintf(&b, `<section class="verdict g%s">
  <div class="vhead"><strong>%s.wikipedia</strong><span class="grade">reliability %s</span></div>
  <p>%s</p>
`, w.Reliability.Grade, w.Wiki, w.Reliability.Grade, escapeXML(w.Verdict))
		if len(w.Articles) > 0 {
			b.WriteString(`  <p class="arts">`)
			for i, art := range w.Articles {
				if i > 0 {
					b.WriteString(" · ")
				}
				if art.URL != "" {
					fmt.Fprintf(&b, `<a href="%s">%s</a>`, escapeXML(art.URL), escapeXML(art.Title))
				} else {
					b.WriteString(escapeXML(art.Title))
				}
				if art.CreatedAt != "" {
					fmt.Fprintf(&b, ` <span class="dim">created %s</span>`, art.CreatedAt)
				}
			}
			b.WriteString("</p>\n")
		}
		if len(w.Candidates) > 0 {
			b.WriteString(`  <p class="arts dim">candidates found by search: `)
			for i, c := range w.Candidates {
				if i > 0 {
					b.WriteString(" · ")
				}
				b.WriteString(escapeXML(c.Title))
			}
			b.WriteString("</p>\n")
		}
		if len(w.Reliability.Blockers) > 0 {
			b.WriteString(`  <ul class="blockers">`)
			for _, x := range w.Reliability.Blockers {
				fmt.Fprintf(&b, "<li>%s</li>", escapeXML(x))
			}
			b.WriteString("</ul>\n")
		}
		if len(w.Reliability.Reasons) > 0 {
			b.WriteString(`  <details><summary>Reliability checks (score `)
			fmt.Fprintf(&b, "%.2f)</summary><ul>", w.Reliability.Score)
			for _, x := range w.Reliability.Reasons {
				fmt.Fprintf(&b, "<li>%s</li>", escapeXML(x))
			}
			b.WriteString("</ul></details>\n")
		}
		b.WriteString("</section>\n")
	}

	fmt.Fprintf(&b, `<figure><svg viewBox="0 0 920 300" class="chart" role="img" aria-label="%s over time">%s</svg></figure>`+"\n",
		escapeXML(metricLabel), chart.Inner())
	if bars != nil {
		w, h := bars.Size()
		fmt.Fprintf(&b, `<figure><svg viewBox="0 0 %.0f %.0f" class="chart" role="img" aria-label="growth per year by edition">%s</svg></figure>`+"\n", w, h, bars.Inner())
	}
	if season != nil {
		w, h := season.Size()
		fmt.Fprintf(&b, `<figure><svg viewBox="0 0 %.0f %.0f" class="chart" role="img" aria-label="seasonality by edition">%s</svg></figure>`+"\n", w, h, season.Inner())
	}

	// table
	b.WriteString(`<table><thead><tr>
<th>edition</th><th>views/day</th><th>per million</th><th>%/yr</th><th>95% CI</th><th>YoY</th><th>adj p</th><th>wiki %/yr</th><th>spike days</th><th>grade</th>
</tr></thead><tbody>
`)
	for _, w := range a.Wikis {
		t := w.Share
		if a.Options.Metric == "absolute" {
			t = w.Absolute
		}
		cells := []string{w.Wiki, "—", "—", "—", "—", "—", "—", "—", "—", w.Reliability.Grade}
		if w.Level != nil {
			cells[1] = fmtNum(w.Level.Last90Mean)
			cells[2] = fmt.Sprintf("%.2f", w.Level.Last90PerMillion)
		}
		if t != nil && !math.IsNaN(t.GrowthPctPerYear) {
			cells[3] = fmt.Sprintf("%+.0f%%", t.GrowthPctPerYear)
			cells[4] = fmt.Sprintf("%+.0f%% … %+.0f%%", t.CILow, t.CIHigh)
			if t.YoYComparable {
				cells[5] = fmt.Sprintf("%+.0f%%", t.YoYPct)
			}
			cells[6] = fmtP(t.PValueAdj)
		}
		if w.Project_ != nil && !math.IsNaN(w.Project_.GrowthPctPerYear) {
			cells[7] = fmt.Sprintf("%+.0f%%", w.Project_.GrowthPctPerYear)
		}
		if w.Events != nil {
			cells[8] = fmt.Sprint(w.Events.SpikeDays)
		}
		b.WriteString("<tr>")
		for i, c := range cells {
			cls := ""
			if i == 9 {
				cls = ` class="g` + w.Reliability.Grade + `-t"`
			}
			fmt.Fprintf(&b, "<td%s>%s</td>", cls, escapeXML(c))
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody></table>\n")

	if c := a.Comparison; c != nil && len(c.Ranking) > 1 {
		b.WriteString(`<section class="shortlist"><h2>Shortlist</h2><ol>`)
		for _, row := range c.Ranking {
			score := "not rankable"
			if !math.IsNaN(row.OpportunityScore) {
				score = fmt.Sprintf("score %.2f", row.OpportunityScore)
			}
			fmt.Fprintf(&b, "<li><strong>%s</strong> — %s. %s</li>", row.Wiki, score, escapeXML(row.Rationale))
		}
		b.WriteString("</ol><p class=\"dim\">")
		for i, n := range c.Notes {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(escapeXML(n))
		}
		b.WriteString("</p></section>\n")
	}

	b.WriteString(`<footer><h2>Assumptions and limitations</h2><ul>`)
	for _, l := range a.Limitations {
		fmt.Fprintf(&b, "<li>%s</li>", escapeXML(l))
	}
	for _, n := range r.Dataset.Notes {
		fmt.Fprintf(&b, "<li>%s</li>", escapeXML(n))
	}
	fmt.Fprintf(&b, `</ul><p class="dim">Source: Wikimedia Pageviews API (agent=%s, access=%s) and Wikidata. Generated by wikitrends.</p></footer>`,
		a.Params.Agent, a.Params.Access)
	b.WriteString("\n</main>\n")
	return b.Bytes()
}

func (r Report) seasonRows() []BarRow {
	var rows []BarRow
	for _, w := range r.Analysis.Wikis {
		s := w.Seasonal
		if s == nil || !s.HasAnnual || len(s.MonthlyIndex) != 12 {
			continue
		}
		hi, lo := 0, 0
		for m := 1; m < 12; m++ {
			if s.MonthlyIndex[m] > s.MonthlyIndex[hi] {
				hi = m
			}
			if s.MonthlyIndex[m] < s.MonthlyIndex[lo] {
				lo = m
			}
		}
		note := fmt.Sprintf("peak %s (+%.0f%%), trough %s (%.0f%%)",
			monthName(hi), (s.MonthlyIndex[hi]-1)*100, monthName(lo), (s.MonthlyIndex[lo]-1)*100)
		if s.SchoolPattern {
			note += " · academic-calendar pattern"
		}
		rows = append(rows, BarRow{
			Label: w.Wiki,
			Value: (s.AnnualAmp - 1) * 100,
			Note:  note,
		})
	}
	return rows
}

func monthName(i int) string {
	return [...]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}[i]
}

// SVGCharts returns the standalone chart files, for users who want to drop a
// single chart into a deck rather than share the whole report.
func (r Report) SVGCharts() map[string][]byte {
	out := map[string][]byte{}
	series, start := r.chartSeries()
	metricLabel := "Share of the edition's pageviews (per million)"
	if r.Analysis.Options.Metric == "absolute" {
		metricLabel = "Pageviews per day"
	}
	c := NewSVG(920, 320)
	c.Rect(0, 0, 920, 320, &ColPaper, nil, 0)
	TimeChart{
		Title:    metricLabel,
		Subtitle: r.headerLine(),
		Start:    start,
		Series:   series,
		LogY:     AutoLog(series),
	}.Draw(c, 10, 6, 900, 308)
	out["trend.svg"] = c.Bytes()

	if rows := r.growthRows(); len(rows) > 0 {
		h := float64(len(rows))*26 + 40
		g := NewSVG(920, h)
		g.Rect(0, 0, 920, h, &ColPaper, nil, 0)
		BarChart{Title: "Growth in attention share, % per year", Rows: rows, Unit: "%"}.Draw(g, 10, 4, 900, h-10)
		out["growth.svg"] = g.Bytes()
	}
	return out
}

// Dates is exposed for callers that want to emit the series as CSV alongside the
// report, so a reader can check any number against the raw data.
func (r Report) CSV() []byte {
	var b bytes.Buffer
	b.WriteString("date,wiki,article,views,project_views,per_million\n")
	start, _ := tseries.ParseDate(r.Analysis.Window.Start)
	for _, s := range r.Dataset.Series {
		if len(s.Views) == 0 {
			continue
		}
		title := ""
		if len(s.Articles) > 0 {
			title = s.Articles[0].Title
			if len(s.Articles) > 1 {
				title += fmt.Sprintf(" +%d more", len(s.Articles)-1)
			}
		}
		share := tseries.SharePerMillion(s.Views.Floats(), s.ProjectViews.Floats())
		dates := tseries.Dates(start, len(s.Views))
		for i := range s.Views {
			fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s\n",
				dates[i], s.Wiki, csvQuote(title),
				num(s.Views[i]), num(s.ProjectViews[i]), num(share[i]))
		}
	}
	return b.Bytes()
}

func num(v float64) string {
	if math.IsNaN(v) {
		return ""
	}
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.4f", v)
}

func csvQuote(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

const htmlCSS = `<style>
:root{
  --bg:#ffffff; --panel:#f7f8fa; --ink:#1a1a1a; --muted:#5d6570; --line:#e3e6ea;
  --pos:#2f7d52; --neg:#a6342c; --warn:#c2571a; --ok:#6a7f2a;
}
:root:not([data-theme="light"]){}
@media (prefers-color-scheme: dark){
  :root:not([data-theme="light"]){
    --bg:#15181c; --panel:#1e2329; --ink:#e8eaed; --muted:#9aa2ab; --line:#2c333b;
    --pos:#6fbf8e; --neg:#e58a80; --warn:#e3a05e; --ok:#b6c96a;
  }
}
:root[data-theme="dark"]{
  --bg:#15181c; --panel:#1e2329; --ink:#e8eaed; --muted:#9aa2ab; --line:#2c333b;
  --pos:#6fbf8e; --neg:#e58a80; --warn:#e3a05e; --ok:#b6c96a;
}
body{background:var(--bg);color:var(--ink);font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;}
main{max-width:980px;margin:0 auto;padding:28px 20px 48px;}
h1{font-size:24px;margin:0 0 6px;letter-spacing:-0.01em;}
h2{font-size:15px;margin:22px 0 8px;}
.meta{color:var(--muted);font-size:12px;margin:0 0 4px;font-variant-numeric:tabular-nums;}
.q{font-size:13px;margin:8px 0 0;padding:8px 10px;background:var(--panel);border-radius:6px;}
.dim{color:var(--muted);}
section.verdict{background:var(--panel);border-left:3px solid var(--line);border-radius:4px;padding:10px 14px;margin:12px 0;}
section.verdict.gA{border-left-color:var(--pos);} section.verdict.gB{border-left-color:var(--ok);}
section.verdict.gC{border-left-color:var(--warn);} section.verdict.gD{border-left-color:var(--neg);}
.vhead{display:flex;justify-content:space-between;align-items:baseline;gap:12px;}
.grade{font-size:12px;color:var(--muted);}
section.verdict p{margin:6px 0;}
.arts{font-size:12px;}
.arts a{color:inherit;}
ul.blockers{margin:6px 0;padding-left:18px;color:var(--neg);font-size:12.5px;}
details{font-size:12.5px;margin-top:6px;} details summary{cursor:pointer;color:var(--muted);}
details ul{margin:6px 0;padding-left:18px;}
figure{margin:18px 0;overflow-x:auto;}
svg.chart{width:100%;height:auto;display:block;background:var(--bg);}
table{width:100%;border-collapse:collapse;font-size:12px;font-variant-numeric:tabular-nums;margin:14px 0;}
th,td{text-align:right;padding:5px 6px;border-bottom:1px solid var(--line);}
th:first-child,td:first-child{text-align:left;font-weight:600;}
thead th{color:var(--muted);font-weight:600;font-size:11px;text-transform:uppercase;letter-spacing:.04em;}
.gA-t{color:var(--pos);font-weight:700;} .gB-t{color:var(--ok);font-weight:700;}
.gC-t{color:var(--warn);font-weight:700;} .gD-t{color:var(--neg);font-weight:700;}
.shortlist ol{padding-left:20px;font-size:13px;}
footer{margin-top:26px;border-top:1px solid var(--line);padding-top:12px;}
footer ul{font-size:11.5px;color:var(--muted);padding-left:18px;}
@media print{
  @page{size:A4;margin:12mm;}
  body{background:#fff;color:#000;font-size:9.5pt;}
  main{max-width:none;padding:0;}
  details{display:none;} figure{page-break-inside:avoid;}
  section.verdict{background:#f4f4f4;}
}
</style>
`
