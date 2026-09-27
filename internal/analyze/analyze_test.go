package analyze

import (
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
)

const day = 24 * time.Hour

// build makes a dataset with one series per spec, so each behaviour the skill
// promises can be asserted against data with a known shape.
type spec struct {
	wiki        string
	status      model.SeriesStatus
	level       float64
	growth      float64 // compound %/yr in ABSOLUTE views
	projGrowth  float64 // compound %/yr in the edition's own traffic
	projLevel   float64
	spikeAt     int
	spikeMult   float64
	seasonalAmp float64
	noise       float64
	days        int
}

func build(t *testing.T, specs ...spec) *model.Dataset {
	t.Helper()
	days := 1100
	for _, s := range specs {
		if s.days > 0 {
			days = s.days
		}
	}
	start := time.Now().UTC().AddDate(0, 0, -(days + 2)).Truncate(day)
	ds := &model.Dataset{
		Schema: model.DatasetSchema,
		Topic:  model.Topic{Query: "test topic", QID: "Q1", LabelEn: "test topic"},
		Window: model.Window{
			Start:       start.Format("2006-01-02"),
			End:         start.AddDate(0, 0, days-1).Format("2006-01-02"),
			Granularity: "daily",
		},
		Params: model.FetchParams{Access: "all-access", Agent: "user"},
	}
	for si, sp := range specs {
		s := model.Series{Wiki: sp.wiki, Project: sp.wiki + ".wikipedia", Status: sp.status,
			Start: ds.Window.Start}
		if sp.status != model.StatusOK {
			s.Note = "synthetic"
			ds.Series = append(ds.Series, s)
			continue
		}
		r := rand.New(rand.NewSource(int64(si) + 7))
		pl := sp.projLevel
		if pl == 0 {
			pl = 1e6
		}
		views := make([]float64, days)
		proj := make([]float64, days)
		gd := math.Pow(1+sp.growth/100, 1.0/365.25)
		pd := math.Pow(1+sp.projGrowth/100, 1.0/365.25)
		for i := 0; i < days; i++ {
			d := start.AddDate(0, 0, i)
			seas := 1.0
			if sp.seasonalAmp != 0 {
				seas = 1 + sp.seasonalAmp*math.Cos(2*math.Pi*float64(d.YearDay())/365.25)
			}
			n := 1.0
			if sp.noise > 0 {
				n = math.Exp(sp.noise * r.NormFloat64())
			}
			views[i] = math.Round(sp.level * math.Pow(gd, float64(i)) * seas * n)
			proj[i] = pl * math.Pow(pd, float64(i))
		}
		if sp.spikeMult > 0 {
			for i := sp.spikeAt; i < sp.spikeAt+7 && i < days; i++ {
				views[i] *= sp.spikeMult
			}
		}
		s.Views, s.ProjectViews = model.Nums(views), model.Nums(proj)
		s.Coverage = model.Coverage{Days: days, Observed: days, Completeness: 1}
		s.Articles = []model.Article{{Title: "Article", QID: "Q1", CreatedAt: "2005-01-01"}}
		ds.Series = append(ds.Series, s)
	}
	return ds
}

func find(a *model.Analysis, wiki string) *model.WikiAnalysis {
	for i := range a.Wikis {
		if a.Wikis[i].Wiki == wiki {
			return &a.Wikis[i]
		}
	}
	return nil
}

func TestHealthyGrowthIsReportedConfidently(t *testing.T) {
	ds := build(t, spec{wiki: "pl", status: model.StatusOK, level: 800, growth: 35, noise: 0.06})
	a, err := Run(ds, Options{})
	if err != nil {
		t.Fatal(err)
	}
	w := find(a, "pl")
	if w.Share.Direction != "rising" {
		t.Fatalf("direction %q, want rising (growth %.1f)", w.Share.Direction, w.Share.GrowthPctPerYear)
	}
	if math.Abs(w.Share.GrowthPctPerYear-35) > 8 {
		t.Fatalf("growth %.1f%%/yr, want ~35", w.Share.GrowthPctPerYear)
	}
	if w.Reliability.Grade != "A" && w.Reliability.Grade != "B" {
		t.Fatalf("grade %s with reasons %v", w.Reliability.Grade, w.Reliability.Reasons)
	}
	if len(w.Reliability.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %v", w.Reliability.Blockers)
	}
	if !strings.Contains(w.Verdict, "rising") {
		t.Fatalf("verdict does not state the direction: %q", w.Verdict)
	}
	// the verdict must always carry window-independent context
	for _, want := range []string{"CI", "Reliability"} {
		if !strings.Contains(w.Verdict, want) {
			t.Fatalf("verdict missing %q: %q", want, w.Verdict)
		}
	}
}

// The most important behaviour in the whole skill: a series too small to measure
// must never be reported as flat, and must never yield a quotable growth rate.
func TestLowVolumeIsBlockedNotCalledFlat(t *testing.T) {
	ds := build(t, spec{wiki: "cs", status: model.StatusOK, level: 6, growth: 0, noise: 0.3})
	a, err := Run(ds, Options{})
	if err != nil {
		t.Fatal(err)
	}
	w := find(a, "cs")
	if w.Reliability.Grade != "D" {
		t.Fatalf("grade %s, want D", w.Reliability.Grade)
	}
	if len(w.Reliability.Blockers) == 0 {
		t.Fatal("expected a volume blocker")
	}
	if !strings.Contains(w.Verdict, "Not measurable") {
		t.Fatalf("verdict must say not measurable, got %q", w.Verdict)
	}
	// It must warn against reading it as flat, and must not itself assert
	// flatness. Checking for the bare word is not enough: "not as flat" contains
	// it. Strip the warning, then look for any remaining claim of flatness.
	if !strings.Contains(w.Verdict, "not as flat") && !strings.Contains(w.Verdict, "unmeasured") {
		t.Fatalf("verdict should warn against reading it as flat: %q", w.Verdict)
	}
	stripped := strings.ReplaceAll(strings.ToLower(w.Verdict), "not as flat", "")
	if strings.Contains(stripped, "flat") {
		t.Fatalf("a blocked series must not be described as flat: %q", w.Verdict)
	}
	// no quotable growth figure may appear in the verdict of a blocked series
	if strings.Contains(w.Verdict, "%/yr") {
		t.Fatalf("a blocked verdict must not carry a growth rate: %q", w.Verdict)
	}
	// and it must not be rankable
	for _, r := range a.Comparison.Ranking {
		if r.Wiki == "cs" && !math.IsNaN(r.OpportunityScore) {
			t.Fatalf("blocked series was given an opportunity score %.2f", r.OpportunityScore)
		}
	}
}

func TestMissingArticleIsAFindingNotZero(t *testing.T) {
	ds := build(t,
		spec{wiki: "cs", status: model.StatusOK, level: 400, growth: 10, noise: 0.05},
		spec{wiki: "pl", status: model.StatusNoArticle},
	)
	a, err := Run(ds, Options{})
	if err != nil {
		t.Fatal(err)
	}
	w := find(a, "pl")
	if w.Level != nil || w.Share != nil {
		t.Fatal("no metrics should be computed for a missing article")
	}
	v := strings.ToLower(w.Verdict)
	if !strings.Contains(v, "no article") {
		t.Fatalf("verdict must name the gap: %q", w.Verdict)
	}
	if !strings.Contains(v, "not evidence of low interest") && !strings.Contains(v, "content gap") {
		t.Fatalf("verdict must not imply zero interest: %q", w.Verdict)
	}
	found := false
	for _, l := range a.Limitations {
		if strings.Contains(l, "pl.wikipedia has no article") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the coverage gap must appear in the limitations: %v", a.Limitations)
	}
}

func TestEventDrivenGrowthIsFlagged(t *testing.T) {
	// flat series plus one huge late event
	// A late event on a two-year window: the realistic shape of "this looks like
	// growth but is one news cycle". Over a much longer window the same spike has
	// little leverage on the fit, which is correct and is why the detector is
	// relative rather than absolute.
	ds := build(t, spec{wiki: "uk", status: model.StatusOK, level: 300, growth: 0,
		noise: 0.05, spikeAt: 650, spikeMult: 60, days: 730})
	a, err := Run(ds, Options{})
	if err != nil {
		t.Fatal(err)
	}
	w := find(a, "uk")
	if w.Events == nil || w.Events.SpikeDays == 0 {
		t.Fatalf("spike not detected: %+v", w.Events)
	}
	if !w.Share.SpikeDependent {
		t.Fatalf("trend should be flagged spike-dependent: with %.1f, without %.1f",
			w.Share.GrowthPctPerYear, w.Share.GrowthExSpikesPctPerYear)
	}
	if math.Abs(w.Share.GrowthExSpikesPctPerYear) > 12 {
		t.Fatalf("spike-free estimate %.1f%%/yr should be near flat", w.Share.GrowthExSpikesPctPerYear)
	}
	hit := false
	for _, r := range w.Reliability.Reasons {
		if strings.Contains(r, "depends on event spikes") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("reliability reasons must name spike dependence: %v", w.Reliability.Reasons)
	}
	// and the ranking must use the spike-free number
	for _, r := range a.Comparison.Ranking {
		if r.Wiki == "uk" && math.Abs(r.GrowthPctPerYear-w.Share.GrowthExSpikesPctPerYear) > 0.01 {
			t.Fatalf("ranking used the spike-inflated growth: %.1f", r.GrowthPctPerYear)
		}
	}
}

// Absolute views falling while the edition falls faster means the topic is
// GAINING share. Reporting only the absolute number would invert the conclusion.
func TestEditionDeclineIsSeparatedFromTopicDecline(t *testing.T) {
	ds := build(t, spec{wiki: "vi", status: model.StatusOK, level: 900,
		growth: -10, projGrowth: -30, noise: 0.05})
	a, err := Run(ds, Options{})
	if err != nil {
		t.Fatal(err)
	}
	w := find(a, "vi")
	if w.Absolute.GrowthPctPerYear >= 0 {
		t.Fatalf("absolute should fall, got %.1f", w.Absolute.GrowthPctPerYear)
	}
	if w.Share.GrowthPctPerYear <= 0 {
		t.Fatalf("share should RISE when the edition falls faster; got %.1f", w.Share.GrowthPctPerYear)
	}
	if w.Project_ == nil || math.Abs(w.Project_.GrowthPctPerYear-(-30)) > 5 {
		t.Fatalf("edition trend should be ~-30%%/yr, got %v", w.Project_)
	}
	hit := false
	for _, r := range w.Reliability.Reasons {
		if strings.Contains(r, "absolute views move one way") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("the metric disagreement must be reported: %v", w.Reliability.Reasons)
	}
}

func TestFDRAdjustmentAcrossEditions(t *testing.T) {
	var specs []spec
	for i, w := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		g := 0.0
		if i == 0 {
			g = 60 // one genuinely growing edition
		}
		specs = append(specs, spec{wiki: w, status: model.StatusOK, level: 500, growth: g, noise: 0.12})
	}
	a, err := Run(build(t, specs...), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range a.Wikis {
		if w.Share == nil || math.IsNaN(w.Share.MannKendallP) {
			continue
		}
		if w.Share.PValueAdj < w.Share.MannKendallP-1e-12 {
			t.Fatalf("%s: adjusted p %.4f below raw %.4f", w.Wiki, w.Share.PValueAdj, w.Share.MannKendallP)
		}
	}
	if find(a, "a").Share.PValueAdj > 0.05 {
		t.Fatalf("the genuinely growing edition should survive FDR, adj p=%.4f", find(a, "a").Share.PValueAdj)
	}
	// limitations must disclose that an adjustment happened
	hit := false
	for _, l := range a.Limitations {
		if strings.Contains(l, "Benjamini-Hochberg") {
			hit = true
		}
	}
	if !hit {
		t.Fatal("multiple-comparison adjustment not disclosed in limitations")
	}
}

func TestAllDecliningShortlistSaysSo(t *testing.T) {
	ds := build(t,
		spec{wiki: "pl", status: model.StatusOK, level: 700, growth: -20, noise: 0.05},
		spec{wiki: "cs", status: model.StatusOK, level: 500, growth: -30, noise: 0.05},
	)
	a, err := Run(ds, Options{})
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	for _, n := range a.Comparison.Notes {
		if strings.Contains(n, "No edition shows growing attention share") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("a shortlist with nothing rising must say so: %v", a.Comparison.Notes)
	}
}

func TestWeightsChangeTheRanking(t *testing.T) {
	// "pl" has the bigger audience, "cs" the faster growth.
	ds := build(t,
		spec{wiki: "pl", status: model.StatusOK, level: 5000, growth: 5, noise: 0.05},
		spec{wiki: "cs", status: model.StatusOK, level: 300, growth: 70, noise: 0.05},
	)
	byLevel, err := Run(ds, Options{Weights: map[string]float64{"level": 0.9, "growth": 0.05, "reliability": 0.05}})
	if err != nil {
		t.Fatal(err)
	}
	byGrowth, err := Run(ds, Options{Weights: map[string]float64{"level": 0.05, "growth": 0.9, "reliability": 0.05}})
	if err != nil {
		t.Fatal(err)
	}
	if byLevel.Comparison.Ranking[0].Wiki != "pl" {
		t.Fatalf("weighting level should rank pl first, got %s", byLevel.Comparison.Ranking[0].Wiki)
	}
	if byGrowth.Comparison.Ranking[0].Wiki != "cs" {
		t.Fatalf("weighting growth should rank cs first, got %s", byGrowth.Comparison.Ranking[0].Wiki)
	}
	// the weights used must travel in the output so the user can change them
	if byGrowth.Comparison.Weights["growth"] != 0.9 {
		t.Fatalf("weights not echoed: %v", byGrowth.Comparison.Weights)
	}
}

func TestMetricOptionSwitchesTheVerdictBasis(t *testing.T) {
	ds := build(t, spec{wiki: "de", status: model.StatusOK, level: 900, growth: -10, projGrowth: -30, noise: 0.05})
	share, _ := Run(ds, Options{Metric: "share"})
	abs, _ := Run(ds, Options{Metric: "absolute"})
	if !strings.Contains(share.Wikis[0].Verdict, "share of the wiki's pageviews") {
		t.Fatalf("share verdict: %q", share.Wikis[0].Verdict)
	}
	if !strings.Contains(abs.Wikis[0].Verdict, "absolute pageviews") {
		t.Fatalf("absolute verdict: %q", abs.Wikis[0].Verdict)
	}
	if !strings.Contains(share.Wikis[0].Verdict, "rising") || !strings.Contains(abs.Wikis[0].Verdict, "declining") {
		t.Fatalf("the two metrics should tell opposite stories here:\nshare: %s\nabs:   %s",
			share.Wikis[0].Verdict, abs.Wikis[0].Verdict)
	}
}

func TestDGradeWithoutBlockerIsNotRankedOrCalledRising(t *testing.T) {
	// Mirrors a real 11-edition scan run on Claude Haiku 4.5: ro.wikipedia had the
	// fastest share growth (+10%/yr) but reliability D from stacked penalties
	// (trend vs year-over-year, absolute vs share, a level shift) and no blocker.
	// It topped the shortlist, its verdict opened with "Interest is rising", and
	// the agent recommended it as the one growing market.
	mk := func(wiki, grade string, score, perMillion, growth float64) model.WikiAnalysis {
		dir := "falling"
		if growth > 0 {
			dir = "rising"
		}
		tr := &model.TrendStats{
			GrowthPctPerYear: growth, CILow: growth - 5, CIHigh: growth + 5, Direction: dir,
			TheilSenPctPerYear: growth, GrowthExSpikesPctPerYear: growth, PValueAdj: 0.01,
			YoYPct: math.NaN(),
		}
		return model.WikiAnalysis{
			Wiki: wiki, Status: model.StatusOK,
			Level:       &model.LevelStats{MeanDaily: 134, Last90Mean: 68, Last90PerMillion: perMillion},
			Share:       tr,
			Absolute:    tr,
			Reliability: model.Reliability{Grade: grade, Score: score, Reasons: []string{"r1", "r2", "r3"}},
		}
	}
	ws := []model.WikiAnalysis{
		mk("ro", "D", 0.37, 89, 10),
		mk("uk", "A", 0.95, 78, -6),
		mk("hu", "A", 0.90, 81, -13),
	}
	o := Options{}
	o.defaults()

	c := compare(ws, o)
	if c.Ranking[0].Wiki == "ro" {
		t.Fatalf("a D-grade edition must not top the shortlist: %+v", c.Ranking)
	}
	last := c.Ranking[len(c.Ranking)-1]
	if last.Wiki != "ro" || !math.IsNaN(last.OpportunityScore) || !strings.Contains(last.Rationale, "reliability D") {
		t.Fatalf("the D-grade edition should sink unranked with the reason stated, got %+v", last)
	}
	hit := false
	for _, n := range c.Notes {
		if strings.Contains(n, "No edition shows growing attention share") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("with the D-grade edition excluded nothing rankable is rising, and the notes must say so: %v", c.Notes)
	}

	v := verdict(ws[0], ws[0].Share, o)
	if !strings.HasPrefix(v, "Not reliable enough to quote") {
		t.Fatalf("a D-grade verdict must lead with the hedge, got %q", v)
	}
	if strings.Contains(v, "rising") {
		t.Fatalf("a D-grade verdict must not call the series rising: %q", v)
	}
}

func TestVerdictStatesTopicAgainstItsEdition(t *testing.T) {
	// On Claude Haiku 4.5 the agent compared "share -15%/yr" with "edition
	// -23%/yr" and reported the topic as holding up better than its edition.
	// Share already divides the edition out, so the verdict must say in words
	// which way the topic moved relative to it.
	ds := build(t,
		spec{wiki: "uk", status: model.StatusOK, level: 900, growth: -34, projGrowth: -23, noise: 0.05},
		spec{wiki: "vi", status: model.StatusOK, level: 900, growth: -10, projGrowth: -30, noise: 0.05},
	)
	a, err := Run(ds, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if v := find(a, "uk").Verdict; !strings.Contains(v, "losing ground within the edition") {
		t.Fatalf("topic falling faster than its edition must be called losing ground: %q", v)
	}
	if v := find(a, "vi").Verdict; !strings.Contains(v, "gaining ground within the edition") {
		t.Fatalf("topic falling slower than its edition must be called gaining ground: %q", v)
	}
}
