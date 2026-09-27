// Package analyze turns a fetched dataset into measured claims plus an explicit
// statement of how much those claims can bear.
//
// The organising idea: every headline number is computed more than once, in ways
// that fail differently, and disagreement between them is reported rather than
// resolved. A log-linear fit and a rank test disagree when a few days are doing
// the work. Absolute and share-normalised trends disagree when the wiki itself
// moved. A trend with and without spikes disagrees when a news event is being
// mistaken for a market. Each disagreement lowers the reliability grade and
// appears, in words, in the output.
package analyze

import (
	"fmt"
	"math"
	"time"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
	"github.com/dmytromaimesko/wiki-trends/internal/stats"
	"github.com/dmytromaimesko/wiki-trends/internal/tseries"
)

type Options struct {
	Metric        string  // "share" (default) or "absolute"
	SpikePolicy   string  // keep | winsorize | both (default)
	MinDailyViews float64 // below this, no trend claim is made at all
	SpikeZ        float64
	Alpha         float64
	Weights       map[string]float64 // opportunity-score weights
}

func (o *Options) defaults() {
	if o.Metric == "" {
		o.Metric = "share"
	}
	if o.SpikePolicy == "" {
		o.SpikePolicy = "both"
	}
	if o.MinDailyViews == 0 {
		o.MinDailyViews = 10
	}
	if o.SpikeZ == 0 {
		o.SpikeZ = 4
	}
	if o.Alpha == 0 {
		o.Alpha = 0.05
	}
	if o.Weights == nil {
		o.Weights = map[string]float64{"level": 0.45, "growth": 0.35, "reliability": 0.20}
	}
}

const spikeWindow = 29

func Run(ds *model.Dataset, o Options) (*model.Analysis, error) {
	o.defaults()
	start, err := tseries.ParseDate(ds.Window.Start)
	if err != nil {
		return nil, err
	}

	an := &model.Analysis{
		Schema:     model.AnalysisSchema,
		AnalyzedAt: time.Now().UTC(),
		Topic:      ds.Topic,
		Window:     ds.Window,
		Params:     ds.Params,
		Options: model.AnalysisOptions{
			Metric: o.Metric, SpikePolicy: o.SpikePolicy,
			MinDailyViews: o.MinDailyViews, SpikeZ: o.SpikeZ, Alpha: o.Alpha,
			Weights: o.Weights,
		},
	}

	for _, s := range ds.Series {
		an.Wikis = append(an.Wikis, analyzeSeries(start, s, o))
	}

	// Multiple-comparison control across the wikis tested in this run. Scanning
	// twenty language editions at alpha=0.05 produces a "significant" trend by
	// chance alone; without this, a portfolio scan reliably recommends noise.
	applyFDR(an.Wikis)

	an.Comparison = compare(an.Wikis, o)
	an.Limitations = limitations(ds, an)
	return an, nil
}

func analyzeSeries(start time.Time, s model.Series, o Options) model.WikiAnalysis {
	w := model.WikiAnalysis{
		Wiki: s.Wiki, Project: s.Project, Status: s.Status, Note: s.Note,
		Articles: s.Articles, Coverage: s.Coverage, Candidates: s.Candidates,
	}
	if s.Status == model.StatusNoArticle || s.Status == model.StatusNoData || len(s.Views) == 0 {
		w.Reliability = model.Reliability{Grade: "D", Score: 0,
			Blockers: []string{statusBlocker(s)},
			Reasons:  []string{"no usable pageview series for this language edition"}}
		w.Verdict = statusVerdict(s)
		return w
	}

	views := s.Views.Floats()
	proj := s.ProjectViews.Floats()
	share := tseries.SharePerMillion(views, proj)

	// ---- level
	medProj := stats.Median(proj)
	w.Level = &model.LevelStats{
		MeanDaily:        stats.Mean(views),
		MedianDaily:      stats.Median(views),
		TotalViews:       stats.Sum(views),
		Last90Mean:       stats.Mean(tail(views, 90)),
		MeanPerMillion:   stats.Mean(share),
		Last90PerMillion: stats.Mean(tail(share, 90)),
	}

	// ---- seasonality, measured on absolute views
	//
	// Deliberately not on the share series: a wiki's own traffic also falls in
	// summer, so share normalisation partly cancels the academic-calendar effect
	// and would hide the very pattern a course-launch decision depends on.
	seas := stats.EstimateSeasonality(start, views)
	peaks, troughs := seas.PeakTrough()
	w.Seasonal = &model.SeasonStats{
		HasAnnual: seas.HasAnnual, YearsCovered: seas.YearsCovered,
		WeeklyAmp: seas.WeeklyAmp, AnnualAmp: seas.AnnualAmp,
		AnnualConsistency: seas.AnnualConsistency,
		MonthlyIndex:      seas.Monthly[:], PeakMonths: peaks, TroughMonths: troughs,
		SchoolPattern: seas.LooksLikeSchoolYear(),
		Confidence:    seasonConfidence(seas, w.Level.MeanDaily),
	}
	if seas.HasAnnual {
		w.Seasonal.PeakMonth, w.Seasonal.TroughMonth = seas.PeakMonth(), seas.TroughMonth()
	}
	if w.Seasonal.Confidence == "low" || w.Seasonal.Confidence == "none" {
		// Do not name peak months the data cannot support.
		w.Seasonal.PeakMonths, w.Seasonal.TroughMonths = nil, nil
		w.Seasonal.SchoolPattern = false
		w.Seasonal.PeakMonth, w.Seasonal.TroughMonth = 0, 0
	}

	// ---- events, detected on absolute views
	events := stats.DetectSpikes(start, views, o.SpikeZ, spikeWindow)
	w.Events = &model.EventStats{
		SpikeDays:      countSpikeDays(events),
		SpikeViewShare: stats.SpikeViewShare(views, events),
	}
	for _, e := range topSpikes(events, 5) {
		w.Events.TopSpikes = append(w.Events.TopSpikes, model.Spike{
			Date: e.Date, Views: e.Views, Base: e.Baseline, Z: e.Z,
		})
	}
	maskedViews := stats.MaskSpikes(views, events, spikeWindow)
	maskedShare := tseries.SharePerMillion(maskedViews, proj)

	// ---- trends
	// The "quantum" is the smallest meaningful increment of each metric: one
	// pageview for absolute counts, and its share-equivalent for the normalised
	// series. Using it inside log(x + q) keeps zero days representable without
	// distorting small values the way a fixed log1p would for a rate.
	w.Absolute = trend(start, views, maskedViews, seas, 1)
	shareQuantum := 1.0
	if medProj > 0 {
		shareQuantum = 1e6 / medProj
	}
	w.Share = trend(start, share, maskedShare, stats.EstimateSeasonality(start, share), shareQuantum)

	// The wiki's own trajectory, so "is the topic growing?" can be answered
	// relative to "is this Wikipedia growing?".
	w.Project_ = trend(start, proj, proj, stats.EstimateSeasonality(start, proj), 1)

	// ---- structural breaks, on the seasonally adjusted primary metric
	primary := w.Share
	primarySeries := share
	if o.Metric == "absolute" {
		primary, primarySeries = w.Absolute, views
	}
	adj := stats.EstimateSeasonality(start, primarySeries).Adjust(start, primarySeries)
	logAdj := logWithQuantum(adj, quantumFor(o.Metric, shareQuantum))
	for _, sh := range stats.DetectLevelShifts(start, logAdj, 90, 25) {
		w.Events.LevelShifts = append(w.Events.LevelShifts, model.Shift{
			Date: sh.Date, RatioPct: sh.RatioPct,
		})
	}

	w.Reliability = grade(w, primary, o)
	w.Verdict = verdict(w, primary, o)
	return w
}

// seasonConfidence rates how much the annual index can be trusted. Years of data
// bound it from above (two years gives two observations per calendar month, which
// is the bare minimum), and daily volume bounds it from below (a monthly median
// over single-digit counts is noise).
func seasonConfidence(s stats.Seasonality, meanDaily float64) string {
	if !s.HasAnnual {
		return "none"
	}
	switch {
	case s.YearsCovered >= 3 && meanDaily >= 100 && s.AnnualConsistency >= 0.6:
		return "high"
	case s.YearsCovered >= 2.5 && meanDaily >= 30 && s.AnnualConsistency >= 0.45:
		return "medium"
	case meanDaily < 20 || s.AnnualConsistency < 0.45:
		return "low"
	default:
		return "medium"
	}
}

func quantumFor(metric string, shareQ float64) float64 {
	if metric == "absolute" {
		return 1
	}
	return shareQ
}

// trend computes every trend statistic for one metric series.
func trend(start time.Time, x, masked []float64, seas stats.Seasonality, quantum float64) *model.TrendStats {
	t := &model.TrendStats{}

	// Seasonal adjustment before fitting: a window that does not start and end in
	// the same season otherwise reads the calendar as growth.
	adj := seas.Adjust(start, x)
	lg := logWithQuantum(adj, quantum)
	f := stats.FitLogLinear(lg)
	t.GrowthPctPerYear = stats.GrowthPctPerYear(f.SlopePerDay)
	t.CILow, t.CIHigh = f.CIPctPerYear(1.96)
	t.StdErrKind = "newey-west-hac"
	t.NWLag = f.NWLag
	t.PValue = f.PValue
	t.R2 = f.R2
	t.N = f.N

	// Rank-based cross-check on monthly means, where serial correlation is weak
	// enough for Mann-Kendall's independence assumption to be tenable.
	mv, _ := stats.MonthlyMeans(start, adj)
	if len(mv) >= 8 {
		lm := logWithQuantum(mv, quantum)
		t.TheilSenPctPerYear = (math.Exp(stats.TheilSen(lm)*12) - 1) * 100
		t.MannKendallTau, t.MannKendallP = stats.MannKendall(mv)
	} else {
		t.TheilSenPctPerYear, t.MannKendallP, t.MannKendallTau = math.NaN(), math.NaN(), math.NaN()
	}

	// Aligned year-over-year on the *unadjusted* series: it needs no seasonal
	// model because both halves span every season exactly once.
	t.YoYPct, t.YoYRecentMean, t.YoYPriorMean, t.YoYComparable = stats.YoY(x)

	// The same fit without event spikes.
	fm := stats.FitLogLinear(logWithQuantum(seas.Adjust(start, masked), quantum))
	t.GrowthExSpikesPctPerYear = stats.GrowthPctPerYear(fm.SlopePerDay)
	t.SpikeDependent = spikeDependent(t.GrowthPctPerYear, t.GrowthExSpikesPctPerYear)

	t.Direction = direction(t)
	return t
}

// spikeDependent is true when removing event days changes the story rather than
// the decimal: a sign flip, or more than half the growth disappearing.
func spikeDependent(with, without float64) bool {
	if math.IsNaN(with) || math.IsNaN(without) {
		return false
	}
	if math.Abs(with) < 5 && math.Abs(without) < 5 {
		return false // both effectively flat
	}
	if with*without < 0 {
		return true
	}
	return math.Abs(with-without) > 0.5*math.Abs(with)
}

func direction(t *model.TrendStats) string {
	if math.IsNaN(t.GrowthPctPerYear) {
		return "unclear"
	}
	ciExcludesZero := !math.IsNaN(t.CILow) && !math.IsNaN(t.CIHigh) && ((t.CILow > 0 && t.CIHigh > 0) || (t.CILow < 0 && t.CIHigh < 0))
	if !ciExcludesZero {
		if math.Abs(t.GrowthPctPerYear) < 10 {
			return "flat"
		}
		return "unclear"
	}
	if t.GrowthPctPerYear > 0 {
		return "rising"
	}
	return "falling"
}

// logWithQuantum maps a non-negative metric onto the log scale, offsetting by the
// metric's smallest meaningful increment so that genuine zero days survive.
func logWithQuantum(x []float64, q float64) []float64 {
	if q <= 0 {
		q = 1
	}
	out := make([]float64, len(x))
	for i, v := range x {
		if math.IsNaN(v) {
			out[i] = math.NaN()
			continue
		}
		out[i] = math.Log(math.Max(v, 0) + q)
	}
	return out
}

func tail(x []float64, n int) []float64 {
	if len(x) <= n {
		return x
	}
	return x[len(x)-n:]
}

func countSpikeDays(ev []stats.SpikeEvent) int {
	n := 0
	for _, e := range ev {
		n += e.EndIdx - e.StartIdx + 1
	}
	return n
}

func topSpikes(ev []stats.SpikeEvent, n int) []stats.SpikeEvent {
	c := append([]stats.SpikeEvent(nil), ev...)
	for i := 0; i < len(c); i++ {
		for j := i + 1; j < len(c); j++ {
			if c[j].Z > c[i].Z {
				c[i], c[j] = c[j], c[i]
			}
		}
	}
	if len(c) > n {
		c = c[:n]
	}
	return c
}

func statusBlocker(s model.Series) string {
	switch s.Status {
	case model.StatusNoArticle:
		return "no article for this concept in this language edition"
	case model.StatusNoData:
		return "pageviews API returned no data"
	}
	return string(s.Status)
}

func statusVerdict(s model.Series) string {
	switch s.Status {
	case model.StatusNoArticle:
		return fmt.Sprintf("%s.wikipedia has no article for this concept. That is a content gap, not evidence of low interest: "+
			"readers there may use a different article, or read about the topic in another language.", s.Wiki)
	case model.StatusNoData:
		return fmt.Sprintf("No pageview data available for %s.wikipedia in this window.", s.Wiki)
	}
	return "No usable data."
}

func applyFDR(ws []model.WikiAnalysis) {
	collect := func(get func(*model.WikiAnalysis) *model.TrendStats) {
		ps := make([]float64, len(ws))
		for i := range ws {
			t := get(&ws[i])
			if t == nil {
				ps[i] = math.NaN()
				continue
			}
			ps[i] = t.MannKendallP
		}
		adj := stats.BenjaminiHochberg(ps)
		for i := range ws {
			if t := get(&ws[i]); t != nil {
				t.PValueAdj = adj[i]
			}
		}
	}
	collect(func(w *model.WikiAnalysis) *model.TrendStats { return w.Share })
	collect(func(w *model.WikiAnalysis) *model.TrendStats { return w.Absolute })
}
