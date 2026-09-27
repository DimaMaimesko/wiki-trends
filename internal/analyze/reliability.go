package analyze

import (
	"fmt"
	"math"
	"time"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
)

// grade scores how much weight a trend claim can carry, and — more importantly —
// says why in plain language.
//
// The design rule is that a grade is never allowed to be a bare letter. A letter
// invites an agent to restate it as confidence ("high-reliability growth") and
// a reader to accept it. A list of named checks, each of which the user can
// disagree with, keeps the argument inspectable: "B, because the window is
// under two years and 31% of views sit inside two news spikes" is something a
// founder can act on or push back on. "B" is not.
//
// Blockers are separate from penalties. A blocker means the question cannot be
// answered from this data at all, and the correct output is "we don't know",
// not a number with a caveat.
func grade(w model.WikiAnalysis, primary *model.TrendStats, o Options) model.Reliability {
	r := model.Reliability{Score: 1}
	if primary == nil || w.Level == nil {
		return model.Reliability{Grade: "D", Blockers: []string{"no trend could be estimated"}}
	}

	add := func(pen float64, format string, args ...any) {
		r.Score -= pen
		r.Reasons = append(r.Reasons, fmt.Sprintf(format, args...))
	}
	block := func(format string, args ...any) {
		r.Blockers = append(r.Blockers, fmt.Sprintf(format, args...))
	}

	// ---- blockers: the data cannot answer the question
	if w.Level.MeanDaily < o.MinDailyViews {
		block("only %.1f views/day on average (floor is %.0f): day-to-day counts at this level are dominated by noise, and a %% change is not measurable",
			w.Level.MeanDaily, o.MinDailyViews)
	}
	if w.Coverage.Completeness < 0.80 && w.Coverage.Days > 0 {
		block("only %.0f%% of eligible days have data", w.Coverage.Completeness*100)
	}
	if primary.N < 120 {
		block("only %d usable days: too short for any trend claim", primary.N)
	}
	if w.Status == model.StatusTooNew {
		block("the article did not exist for most of the window, so early days measure its absence, not low interest")
	}

	// ---- penalties: the data answers, with qualifications
	switch {
	case w.Level.MeanDaily < 50:
		add(0.20, "low volume (%.0f views/day): percentage changes are unstable at this scale", w.Level.MeanDaily)
	case w.Level.MeanDaily < 200:
		add(0.08, "moderate volume (%.0f views/day)", w.Level.MeanDaily)
	}

	// A topic can clear the volume floor on its window average while having
	// decayed to nothing recently. The trend is then measurable but the
	// conclusion ("where is this now") is not, so it is called out separately.
	if w.Level.MeanDaily >= o.MinDailyViews && w.Level.Last90Mean < o.MinDailyViews {
		add(0.15, "the window average clears the volume floor but the last 90 days average only %.0f views/day: "+
			"the trend is measurable historically, the current level is not", w.Level.Last90Mean)
	}

	if w.Seasonal != nil && w.Seasonal.YearsCovered < 2 {
		add(0.15, "window covers %.1f years: annual seasonality cannot be separated from trend, so part of the measured growth may be the calendar",
			w.Seasonal.YearsCovered)
	}

	if !primary.YoYComparable {
		add(0.10, "no aligned year-over-year comparison is possible in this window")
	}

	sig := !math.IsNaN(primary.PValueAdj) && primary.PValueAdj <= o.Alpha
	if !sig {
		if math.IsNaN(primary.PValueAdj) {
			add(0.15, "not enough monthly observations for a rank-based significance test")
		} else {
			add(0.25, "the monotonic trend is not significant after adjusting for the number of language editions tested (adjusted p = %.3f)",
				primary.PValueAdj)
		}
	}

	if !math.IsNaN(primary.CILow) && !math.IsNaN(primary.CIHigh) && primary.CILow < 0 && primary.CIHigh > 0 {
		add(0.20, "the 95%% confidence interval spans zero (%.0f%% to %.0f%% per year): the direction itself is uncertain",
			primary.CILow, primary.CIHigh)
	}

	if primary.SpikeDependent {
		add(0.25, "the trend depends on event spikes: %.0f%%/yr with them, %.0f%%/yr without",
			primary.GrowthPctPerYear, primary.GrowthExSpikesPctPerYear)
	}
	if w.Events != nil && w.Events.SpikeViewShare > 0.25 {
		add(0.10, "%.0f%% of all views in the window fall inside spike days: much of this audience is one-off attention",
			w.Events.SpikeViewShare*100)
	}

	// OLS and Theil-Sen answer the same question with different failure modes.
	// When they disagree, a handful of days is steering the parametric fit.
	if !math.IsNaN(primary.TheilSenPctPerYear) && !math.IsNaN(primary.GrowthPctPerYear) {
		a, b := primary.GrowthPctPerYear, primary.TheilSenPctPerYear
		if a*b < 0 && (math.Abs(a) > 10 || math.Abs(b) > 10) {
			add(0.20, "the log-linear fit (%.0f%%/yr) and the robust Theil-Sen slope (%.0f%%/yr) disagree on direction", a, b)
		} else if math.Abs(a-b) > math.Max(20, 0.75*math.Abs(a)) {
			add(0.10, "the log-linear fit (%.0f%%/yr) and the robust Theil-Sen slope (%.0f%%/yr) differ substantially", a, b)
		}
	}

	// Full-window trend versus the most recent year. These measure different
	// things — a slope over the whole window, and the last twelve months against
	// the twelve before — so disagreement is not an error: it is an inflection.
	// A topic that fell for two years and stopped falling last year is a very
	// different investment case from one still falling, and a single %/yr figure
	// hides that completely.
	if primary.YoYComparable && !math.IsNaN(primary.YoYPct) && !math.IsNaN(primary.GrowthPctPerYear) {
		g, y := primary.GrowthPctPerYear, primary.YoYPct
		if g*y < 0 && (math.Abs(g) > 8 || math.Abs(y) > 8) {
			add(0.15, "the full-window trend (%+.0f%%/yr) and the most recent year (%+.0f%% year over year) point in opposite directions: "+
				"the series is not monotonic, so quote the recent year for \"where is it now\" and the trend only for \"how did it get here\"", g, y)
		}
	}

	// Absolute vs share: the single most valuable cross-check the skill makes.
	if w.Absolute != nil && w.Share != nil && w.Project_ != nil {
		ga, gs := w.Absolute.GrowthPctPerYear, w.Share.GrowthPctPerYear
		gp := w.Project_.GrowthPctPerYear
		if !math.IsNaN(ga) && !math.IsNaN(gs) && ga*gs < 0 {
			add(0.20, "absolute views move one way (%.0f%%/yr) and share of the wiki's traffic the other (%.0f%%/yr): "+
				"the wiki's own traffic changed by %.0f%%/yr, so pick the metric that matches your question", ga, gs, gp)
		}
		if !math.IsNaN(gp) && math.Abs(gp) > 15 {
			add(0.05, "%s.wikipedia's overall traffic itself moved %.0f%%/yr in this window", w.Wiki, gp)
		}
	}

	if w.Events != nil && len(w.Events.LevelShifts) > 0 {
		s := w.Events.LevelShifts[0]
		add(0.20, "a persistent %.0f%% level shift around %s remains after trend and seasonality: check the article history for a page move, "+
			"merge or redirect change before reading this as demand", s.RatioPct, s.Date)
	}

	if w.Coverage.BeforeCreate > 0 {
		add(0.10, "the article was created inside the window (%d days precede it and are excluded)", w.Coverage.BeforeCreate)
	}
	if w.Coverage.Gaps > w.Coverage.Days/50 && w.Coverage.Days > 0 {
		add(0.10, "%d days have no data at all", w.Coverage.Gaps)
	}

	if r.Score < 0 {
		r.Score = 0
	}
	if len(r.Blockers) > 0 {
		r.Grade, r.Score = "D", 0
		return r
	}
	switch {
	case r.Score >= 0.80:
		r.Grade = "A"
	case r.Score >= 0.60:
		r.Grade = "B"
	case r.Score >= 0.40:
		r.Grade = "C"
	default:
		r.Grade = "D"
	}
	if len(r.Reasons) == 0 {
		r.Reasons = append(r.Reasons, "all checks passed: adequate volume, at least two years of data, significant and spike-independent trend, consistent across absolute and share metrics")
	}
	return r
}

// verdict writes the one-sentence answer, worded to match what the evidence
// actually supports. Hedging language is not decoration here: it is the only
// thing preventing a D-grade series from being quoted as a growth rate.
func verdict(w model.WikiAnalysis, primary *model.TrendStats, o Options) string {
	if primary == nil || w.Level == nil {
		return "No usable data."
	}
	metric := "share of the wiki's pageviews"
	if o.Metric == "absolute" {
		metric = "absolute pageviews"
	}
	if len(w.Reliability.Blockers) > 0 {
		// Wording matters here. "No growth" and "not measurable" lead to opposite
		// decisions, and the second is what this data supports.
		return fmt.Sprintf("Not measurable: %s. Treat this edition as unmeasured, not as flat — "+
			"the point estimate below is reported for context only and must not be quoted as a growth rate.",
			w.Reliability.Blockers[0])
	}
	g := primary.GrowthPctPerYear
	if primary.SpikeDependent {
		g = primary.GrowthExSpikesPctPerYear
	}
	var head string
	switch {
	case w.Reliability.Grade == "D":
		// No single blocker, but enough penalties stack up that the trend is not
		// quotable. Lead with that and keep direction words out of the sentence:
		// "Interest is rising" is what gets repeated, whatever follows it.
		head = fmt.Sprintf("Not reliable enough to quote (reliability D, score %.2f): %d reliability checks fail, listed below. "+
			"Point estimate for context only: %+.0f%%/yr in %s (95%% CI %+.0f%% to %+.0f%%)",
			w.Reliability.Score, len(w.Reliability.Reasons), g, metric, primary.CILow, primary.CIHigh)
	case primary.Direction == "rising":
		head = fmt.Sprintf("Interest is rising: %+.0f%%/yr in %s (95%% CI %+.0f%% to %+.0f%%)", g, metric, primary.CILow, primary.CIHigh)
	case primary.Direction == "falling":
		head = fmt.Sprintf("Interest is declining: %+.0f%%/yr in %s (95%% CI %+.0f%% to %+.0f%%)", g, metric, primary.CILow, primary.CIHigh)
	case primary.Direction == "flat":
		head = fmt.Sprintf("Interest is essentially flat (%+.0f%%/yr in %s, confidence interval spans zero)", g, metric)
	default:
		head = fmt.Sprintf("Direction unclear: point estimate %+.0f%%/yr in %s, but the 95%% CI spans %+.0f%% to %+.0f%%", g, metric, primary.CILow, primary.CIHigh)
	}
	if primary.YoYComparable {
		head += fmt.Sprintf(". Aligned year-over-year: %+.0f%%", primary.YoYPct)
		// Name the inflection in the verdict, not only in the reliability notes:
		// it changes the recommendation.
		if g*primary.YoYPct < 0 && (math.Abs(g) > 8 || math.Abs(primary.YoYPct) > 8) {
			head += " — the most recent year moves the other way, so treat this as an inflection rather than a steady trend"
		}
	}
	head += fmt.Sprintf(". Baseline %.0f views/day (%.1f per million pageviews). Reliability %s.",
		w.Level.Last90Mean, w.Level.Last90PerMillion, w.Reliability.Grade)
	if w.Reliability.Grade != "D" {
		head += editionContext(w)
	}
	// Seasonality is stated as a launch-timing fact rather than as a statistic,
	// because that is the decision it actually informs.
	if s := w.Seasonal; s != nil && s.HasAnnual && s.PeakMonth > 0 && s.AnnualAmp >= 1.5 &&
		(s.Confidence == "high" || s.Confidence == "medium") {
		head += fmt.Sprintf(" Strongly seasonal: %s runs about %.1fx the %s level (pattern repeats across years, consistency %.2f)",
			monthName(s.PeakMonth), s.AnnualAmp, monthName(s.TroughMonth), s.AnnualConsistency)
		if s.SchoolPattern {
			head += " and matches an academic calendar"
		}
		head += "."
	}
	return head
}

// editionContext says in words how the topic moved relative to its own edition.
//
// Left to the reader, the share trend gets compared with the edition's trend as
// if they were the same kind of number. A cheap model read "share -15%/yr,
// edition -23%/yr" as "holding up better than the edition", which is backwards:
// share already has the edition divided out, so any fall in share is ground lost
// inside the edition.
func editionContext(w model.WikiAnalysis) string {
	if w.Absolute == nil || w.Share == nil || w.Project_ == nil {
		return ""
	}
	ga, gs, gp := w.Absolute.GrowthPctPerYear, w.Share.GrowthPctPerYear, w.Project_.GrowthPctPerYear
	if math.IsNaN(ga) || math.IsNaN(gs) || math.IsNaN(gp) {
		return ""
	}
	rel := "the topic moves roughly with the edition"
	switch w.Share.Direction {
	case "rising":
		rel = "the topic is gaining ground within the edition"
	case "falling":
		rel = "the topic is losing ground within the edition, beyond the edition's own trend"
	}
	return fmt.Sprintf(" Against its own edition: absolute views %+.0f%%/yr while all of %s.wikipedia moved %+.0f%%/yr, so %s (share %+.0f%%/yr).",
		ga, w.Wiki, gp, rel, gs)
}

func monthName(m int) string {
	if m < 1 || m > 12 {
		return "?"
	}
	return time.Month(m).String()
}
