package render

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
)

// fixture builds an analysis exercising the cases the report must handle at
// once: a healthy series, a blocked one, a missing article, non-Latin titles, and
// statistics that could not be computed.
func fixture() (*model.Dataset, *model.Analysis) {
	n := 800
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	views := make([]float64, n)
	proj := make([]float64, n)
	for i := range views {
		views[i] = 300 * math.Pow(1.0008, float64(i)) * (1 + 0.2*math.Sin(float64(i)/7))
		proj[i] = 2e6
		if i == 400 {
			views[i] *= 20
		}
		if i > 600 && i < 610 {
			views[i] = math.NaN() // a data gap must break the line, not interpolate
		}
	}
	small := make([]float64, n)
	for i := range small {
		small[i] = 4
	}
	ds := &model.Dataset{
		Schema: model.DatasetSchema,
		Topic:  model.Topic{Query: "intermittent fasting", QID: "Q1666254", LabelEn: "intermittent fasting", DescriptionEn: "a diet"},
		Window: model.Window{Start: start.Format("2006-01-02"), End: start.AddDate(0, 0, n-1).Format("2006-01-02"), Granularity: "daily"},
		Params: model.FetchParams{Access: "all-access", Agent: "user"},
		Notes:  []string{"a fetch note"},
		Series: []model.Series{
			{Wiki: "uk", Project: "uk.wikipedia", Status: model.StatusOK, Start: start.Format("2006-01-02"),
				Views: model.Nums(views), ProjectViews: model.Nums(proj),
				Coverage: model.Coverage{Days: n, Observed: n - 10, Gaps: 10, Completeness: 0.99},
				Articles: []model.Article{{Title: "Інтервальне голодування", QID: "Q1666254",
					URL: "https://uk.wikipedia.org/wiki/x", CreatedAt: "2019-11-10"}}},
			{Wiki: "cs", Project: "cs.wikipedia", Status: model.StatusOK, Start: start.Format("2006-01-02"),
				Views: model.Nums(small), ProjectViews: model.Nums(proj),
				Coverage: model.Coverage{Days: n, Observed: n, Completeness: 1},
				Articles: []model.Article{{Title: "Přerušovaný půst", CreatedAt: "2020-10-28"}}},
			{Wiki: "pl", Project: "pl.wikipedia", Status: model.StatusNoArticle,
				Note:       "no sitelink",
				Candidates: []model.Candidate{{Title: "Głodówka lecznicza"}}},
		},
	}
	an := &model.Analysis{
		Schema: model.AnalysisSchema, Topic: ds.Topic, Window: ds.Window, Params: ds.Params,
		Options: model.AnalysisOptions{Metric: "share", MinDailyViews: 10, SpikeZ: 4, Alpha: 0.05},
		Wikis: []model.WikiAnalysis{
			{Wiki: "uk", Status: model.StatusOK, Articles: ds.Series[0].Articles,
				Coverage: ds.Series[0].Coverage,
				Level:    &model.LevelStats{MeanDaily: 380, Last90Mean: 520, Last90PerMillion: 260, MedianDaily: 360},
				Share: &model.TrendStats{GrowthPctPerYear: 34, CILow: 12, CIHigh: 60, PValueAdj: 0.002,
					YoYPct: 28, YoYComparable: true, Direction: "rising", N: 790, NWLag: 6,
					TheilSenPctPerYear: 31, MannKendallP: 0.001, GrowthExSpikesPctPerYear: 33},
				Absolute: &model.TrendStats{GrowthPctPerYear: 34, CILow: 12, CIHigh: 60, Direction: "rising"},
				Project_: &model.TrendStats{GrowthPctPerYear: -12},
				Seasonal: &model.SeasonStats{HasAnnual: true, YearsCovered: 2.2, AnnualAmp: 1.8,
					AnnualConsistency: 0.7, Confidence: "medium", PeakMonth: 9, TroughMonth: 7,
					MonthlyIndex: []float64{1, 1, 1, 1, 1, 0.8, 0.6, 0.7, 1.8, 1.2, 1.1, 1}},
				Events:      &model.EventStats{SpikeDays: 3, SpikeViewShare: 0.08, TopSpikes: []model.Spike{{Date: "2025-02-04", Views: 6000, Base: 300, Z: 9}}},
				Reliability: model.Reliability{Grade: "B", Score: 0.7, Reasons: []string{"window covers 2.2 years"}},
				Verdict:     "Interest is rising: +34%/yr in share of the wiki's pageviews."},
			{Wiki: "cs", Status: model.StatusOK, Articles: ds.Series[1].Articles, Coverage: ds.Series[1].Coverage,
				Level: &model.LevelStats{MeanDaily: 4, Last90Mean: 4, Last90PerMillion: 2},
				// every statistic un-computable: the report must not print zeros
				Share: &model.TrendStats{GrowthPctPerYear: math.NaN(), CILow: math.NaN(), CIHigh: math.NaN(),
					PValueAdj: math.NaN(), YoYPct: math.NaN(), TheilSenPctPerYear: math.NaN(), Direction: "unclear"},
				Reliability: model.Reliability{Grade: "D", Blockers: []string{"only 4 views/day"}},
				Verdict:     "Not measurable: only 4 views/day."},
			{Wiki: "pl", Status: model.StatusNoArticle, Candidates: ds.Series[2].Candidates,
				Reliability: model.Reliability{Grade: "D", Blockers: []string{"no article for this concept"}},
				Verdict:     "pl.wikipedia has no article for this concept."},
		},
		Comparison: &model.Comparison{Metric: "share",
			Weights: map[string]float64{"level": 0.45, "growth": 0.35, "reliability": 0.2},
			Ranking: []model.RankRow{
				{Wiki: "uk", LevelPerMillion: 260, GrowthPctPerYear: 34, YoYPct: 28, PValueAdj: 0.002, Grade: "B", OpportunityScore: 0.81, Rationale: "rising"},
				{Wiki: "cs", OpportunityScore: math.NaN(), GrowthPctPerYear: math.NaN(), YoYPct: math.NaN(), PValueAdj: math.NaN(), Grade: "D", Rationale: "not rankable"},
			},
			Notes: []string{"a note"}},
		Limitations: []string{"Wikipedia pageviews measure curiosity, not purchase intent."},
	}
	return ds, an
}

func TestReportRendersEveryFormat(t *testing.T) {
	ds, an := fixture()
	r := Report{Dataset: ds, Analysis: an, Title: "Fasting interest", Question: "Where is interest growing?"}

	pdf := r.PDF()
	if len(pdf) < 2000 || !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) || !bytes.HasSuffix(pdf, []byte("%%EOF\n")) {
		t.Fatalf("PDF looks malformed (%d bytes)", len(pdf))
	}
	for _, bad := range []string{"NaN", "+Inf", "-Inf"} {
		if bytes.Contains(pdf, []byte(bad)) {
			t.Fatalf("%q leaked into the PDF", bad)
		}
	}

	html := string(r.HTML())
	for _, want := range []string{
		"Fasting interest",
		"Where is interest growing?",
		"Інтервальне голодування", // native titles must survive in HTML
		"Přerušovaný půst",
		"Głodówka lecznicza",
		"not purchase intent",  // limitations must travel with the report
		"reliability D",        // grades shown
		"<svg",                 // charts inlined
		"prefers-color-scheme", // theme-aware
		"@page",                // prints to A4
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	for _, bad := range []string{"NaN", "+Inf"} {
		if strings.Contains(html, bad) {
			t.Errorf("%q leaked into the HTML", bad)
		}
	}

	md := r.Markdown()
	for _, want := range []string{"# intermittent fasting", "reliability B", "**blockers**", "Assumptions and limitations"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
	if strings.Contains(md, "NaN") {
		t.Error("NaN leaked into the markdown summary")
	}

	csv := string(r.CSV())
	if !strings.HasPrefix(csv, "date,wiki,article,views,project_views,per_million\n") {
		t.Fatal("CSV header wrong")
	}
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 1+800+800 { // two series with data; the missing article has none
		t.Fatalf("CSV has %d lines", len(lines))
	}
	// a gap must be an empty field, never a zero
	if !strings.Contains(csv, ",,") {
		t.Error("CSV should leave missing days empty rather than writing 0")
	}
}

// A missing statistic must render as an em dash, never as 0, in every format.
func TestUncomputedStatisticsRenderAsDashes(t *testing.T) {
	ds, an := fixture()
	r := Report{Dataset: ds, Analysis: an}
	html := string(r.HTML())
	row := html[strings.Index(html, "<tr><td>cs</td>"):]
	row = row[:strings.Index(row, "</tr>")]
	if strings.Contains(row, ">+0%<") || strings.Contains(row, ">0.000<") {
		t.Fatalf("un-computed values rendered as zero: %s", row)
	}
	if strings.Count(row, "—") < 4 {
		t.Fatalf("expected dashes for un-computed values: %s", row)
	}
}

func TestChartHandlesGapsAndSingleSeries(t *testing.T) {
	// a series that is entirely missing must not panic or draw anything absurd
	c := NewSVG(400, 200)
	TimeChart{Start: time.Now(), Series: []ChartSeries{{Label: "x", Values: []float64{math.NaN(), math.NaN()}}}}.
		Draw(c, 0, 0, 400, 200)
	if !strings.Contains(string(c.Bytes()), "no data") {
		t.Fatal("an all-missing series should render a 'no data' message")
	}

	// a gap must split the polyline rather than bridge it
	vals := make([]float64, 100)
	for i := range vals {
		vals[i] = 10
	}
	for i := 40; i < 50; i++ {
		vals[i] = math.NaN()
	}
	c2 := NewSVG(400, 200)
	TimeChart{Start: time.Now(), Series: []ChartSeries{{Label: "x", Values: vals, Color: Palette[0]}}}.
		Draw(c2, 0, 0, 400, 200)
	if n := strings.Count(string(c2.Bytes()), "<polyline"); n < 2 {
		t.Fatalf("expected the gap to split the line into segments, got %d polylines", n)
	}
}

func TestAutoLogPicksLogForWideRanges(t *testing.T) {
	wide := []ChartSeries{{Values: []float64{1, 2, 3}}, {Values: []float64{500, 600, 700}}}
	if !AutoLog(wide) {
		t.Error("a 500x range should use a log axis")
	}
	narrow := []ChartSeries{{Values: []float64{10, 12}}, {Values: []float64{14, 16}}}
	if AutoLog(narrow) {
		t.Error("a narrow range should stay linear")
	}
	if AutoLog([]ChartSeries{{Values: []float64{0, 0, math.NaN()}}}) {
		t.Error("an all-zero series must not select a log axis")
	}
}
