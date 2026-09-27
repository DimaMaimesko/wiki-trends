package render

import (
	"fmt"
	"math"
	"strings"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
)

// Markdown is the compact summary written to stdout and to summary.md.
//
// This is the artifact the agent actually reads back, so it is optimised for
// exactly that: every number a follow-up question is likely to need, in a few
// hundred tokens, with no prose padding. The full analysis.json stays on disk for
// drill-down, and the agent is told in SKILL.md to read this file rather than the
// JSON unless it needs a specific field.
//
// It deliberately leads with the verdict and the reliability grade rather than
// with the growth rate. An agent that reads a number first tends to report a
// number first.
func (r Report) Markdown() string {
	var b strings.Builder
	a := r.Analysis
	f := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	f("# %s\n\n", r.topicName())
	if a.Topic.DescriptionEn != "" {
		f("_%s_ (%s)\n\n", a.Topic.DescriptionEn, a.Topic.QID)
	}
	f("Window %s → %s · metric `%s` · agent=%s access=%s\n\n",
		a.Window.Start, a.Window.End, a.Options.Metric, a.Params.Agent, a.Params.Access)
	if len(a.Topic.Basket) > 0 {
		f("Topic basket: ")
		for i, it := range a.Topic.Basket {
			if i > 0 {
				f(", ")
			}
			f("%s (%s)", it.LabelEn, it.QID)
		}
		f("\n\n")
	}

	f("## Answer per language edition\n\n")
	for _, w := range a.Wikis {
		f("### %s.wikipedia — reliability %s\n\n", w.Wiki, w.Reliability.Grade)
		f("%s\n\n", w.Verdict)
		for _, art := range w.Articles {
			created := art.CreatedAt
			if created == "" {
				created = "unknown"
			}
			f("- article: %s (created %s)", art.Title, created)
			if len(art.Redirects) > 0 {
				f(", %d redirect titles summed", len(art.Redirects))
			}
			f("\n")
		}
		if len(w.Candidates) > 0 {
			f("- search candidates in this edition: ")
			for i, c := range w.Candidates {
				if i > 0 {
					f("; ")
				}
				f("%s", c.Title)
			}
			f("\n")
		}
		if w.Level != nil {
			f("- level: %.0f views/day mean, %.0f last 90d, %.2f per million pageviews last 90d\n",
				w.Level.MeanDaily, w.Level.Last90Mean, w.Level.Last90PerMillion)
		}
		writeTrend(&b, "share of wiki traffic", w.Share)
		writeTrend(&b, "absolute views", w.Absolute)
		if w.Project_ != nil && !math.IsNaN(w.Project_.GrowthPctPerYear) {
			f("- context: %s.wikipedia overall traffic %+.0f%%/yr\n", w.Wiki, w.Project_.GrowthPctPerYear)
		}
		if s := w.Seasonal; s != nil && s.HasAnnual {
			f("- seasonality (confidence %s): annual peak/trough ratio %.2f, year-to-year shape consistency %.2f", s.Confidence, s.AnnualAmp, s.AnnualConsistency)
			if len(s.PeakMonths) > 0 {
				f(", peaks in months %v", s.PeakMonths)
			}
			if len(s.TroughMonths) > 0 {
				f(", troughs in %v", s.TroughMonths)
			}
			if s.SchoolPattern {
				f(" — matches an academic-calendar pattern")
			}
			f("\n")
		} else if s != nil {
			f("- seasonality: annual pattern not estimable (%.1f years of data)\n", s.YearsCovered)
		}
		if e := w.Events; e != nil {
			if e.SpikeDays > 0 {
				f("- events: %d spike days, %.0f%% of window views inside spikes", e.SpikeDays, e.SpikeViewShare*100)
				if len(e.TopSpikes) > 0 {
					f("; largest: ")
					for i, s := range e.TopSpikes {
						if i > 0 {
							f(", ")
						}
						f("%s (%.0f views vs %.0f baseline)", s.Date, s.Views, s.Base)
					}
				}
				f("\n")
			}
			for _, ls := range e.LevelShifts {
				f("- level shift: %+.0f%% around %s — check article history before reading as demand\n", ls.RatioPct, ls.Date)
			}
		}
		f("- coverage: %d days, %.0f%% complete, %d days before article creation, %d gaps\n",
			w.Coverage.Days, w.Coverage.Completeness*100, w.Coverage.BeforeCreate, w.Coverage.Gaps)
		if len(w.Reliability.Blockers) > 0 {
			f("- **blockers**:\n")
			for _, x := range w.Reliability.Blockers {
				f("  - %s\n", x)
			}
		}
		if len(w.Reliability.Reasons) > 0 {
			f("- reliability notes (score %.2f):\n", w.Reliability.Score)
			for _, x := range w.Reliability.Reasons {
				f("  - %s\n", x)
			}
		}
		f("\n")
	}

	if c := a.Comparison; c != nil && len(c.Ranking) > 1 {
		f("## Shortlist\n\n")
		f("Weights: level %.2f, growth %.2f, reliability %.2f (change with `--weights`)\n\n",
			c.Weights["level"], c.Weights["growth"], c.Weights["reliability"])
		f("| # | edition | score | per million | share %%/yr | abs %%/yr | wiki %%/yr | YoY | adj p | grade | why |\n")
		f("|---|---|---|---|---|---|---|---|---|---|---|\n")
		for i, row := range c.Ranking {
			score := "—"
			if !math.IsNaN(row.OpportunityScore) {
				score = fmt.Sprintf("%.2f", row.OpportunityScore)
			}
			yoy := "—"
			if !math.IsNaN(row.YoYPct) {
				yoy = fmt.Sprintf("%+.0f%%", row.YoYPct)
			}
			g := "—"
			if !math.IsNaN(row.GrowthPctPerYear) {
				g = fmt.Sprintf("%+.0f%%", row.GrowthPctPerYear)
			}
			f("| %d | %s | %s | %.2f | %s | %s | %s | %s | %s | %s | %s |\n",
				i+1, row.Wiki, score, row.LevelPerMillion, g,
				pct(row.AbsGrowthPctPerYear), pct(row.WikiGrowthPctPerYear),
				yoy, fmtP(row.PValueAdj), row.Grade, row.Rationale)
		}
		f("\n")
		for _, n := range c.Notes {
			f("- %s\n", n)
		}
		f("\n")
	}

	if len(r.Dataset.Notes) > 0 {
		f("## Fetch notes\n\n")
		for _, n := range r.Dataset.Notes {
			f("- %s\n", n)
		}
		f("\n")
	}

	f("## Assumptions and limitations\n\n")
	for _, l := range a.Limitations {
		f("- %s\n", l)
	}
	return b.String()
}

func pct(v float64) string {
	if math.IsNaN(v) {
		return "—"
	}
	return fmt.Sprintf("%+.0f%%", v)
}

func writeTrend(b *strings.Builder, name string, t *model.TrendStats) {
	if t == nil || math.IsNaN(t.GrowthPctPerYear) {
		return
	}
	fmt.Fprintf(b, "- %s: %+.0f%%/yr (95%% CI %+.0f%% to %+.0f%%, HAC lag %d, n=%d), direction %s",
		name, t.GrowthPctPerYear, t.CILow, t.CIHigh, t.NWLag, t.N, t.Direction)
	if !math.IsNaN(t.TheilSenPctPerYear) {
		fmt.Fprintf(b, "; Theil-Sen %+.0f%%/yr", t.TheilSenPctPerYear)
	}
	if !math.IsNaN(t.MannKendallP) {
		fmt.Fprintf(b, "; Mann-Kendall p=%s (adj %s)", fmtP(t.MannKendallP), fmtP(t.PValueAdj))
	}
	if t.YoYComparable {
		fmt.Fprintf(b, "; YoY %+.0f%%", t.YoYPct)
	}
	if t.SpikeDependent {
		fmt.Fprintf(b, "; **spike-dependent** (%+.0f%%/yr without spikes)", t.GrowthExSpikesPctPerYear)
	}
	b.WriteString("\n")
}
