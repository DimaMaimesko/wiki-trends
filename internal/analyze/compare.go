package analyze

import (
	"fmt"
	"math"
	"sort"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
)

// compare ranks language editions into a shortlist.
//
// The scoring is intentionally simple, transparent and adjustable rather than
// clever. A founder's real objective function includes willingness to pay,
// competition, distribution cost and the team's own languages, none of which
// Wikipedia knows. So the score combines only what the data supports:
//
//	level        how much of that audience's attention the topic already holds
//	growth       how fast that share is changing
//	reliability  how much the growth number can be trusted
//
// The weights travel in the output, so "I care about audience size, not growth"
// becomes a re-run of `analyze` with different weights — no new fetch, no new
// argument about methodology.
//
// Level is normalised on a log scale: the difference between 2 and 20 per
// million is a different kind of difference from 200 versus 218, and on a linear
// scale one large wiki flattens every other row to zero.
func compare(ws []model.WikiAnalysis, o Options) *model.Comparison {
	c := &model.Comparison{Metric: o.Metric, Weights: o.Weights}

	type row struct {
		w                  *model.WikiAnalysis
		t                  *model.TrendStats
		level, growth, rel float64
		ok                 bool
	}
	rows := make([]row, 0, len(ws))
	for i := range ws {
		w := &ws[i]
		t := w.Share
		if o.Metric == "absolute" {
			t = w.Absolute
		}
		if t == nil || w.Level == nil {
			rows = append(rows, row{w: w})
			continue
		}
		g := t.GrowthPctPerYear
		if t.SpikeDependent && !math.IsNaN(t.GrowthExSpikesPctPerYear) {
			// Rank on the spike-free estimate: a shortlist built on one news
			// cycle is the classic way this kind of analysis misleads.
			g = t.GrowthExSpikesPctPerYear
		}
		rows = append(rows, row{
			w: w, t: t,
			level:  math.Log10(math.Max(w.Level.Last90PerMillion, 0.01)),
			growth: g,
			rel:    w.Reliability.Score,
			// A D grade reached through stacked penalties is as unquotable as one
			// forced by a blocker. Ranking it anyway put a D-grade "+10%/yr"
			// edition at the top of a real shortlist, and an agent reported it as
			// the growing market.
			ok: len(w.Reliability.Blockers) == 0 && w.Reliability.Grade != "D",
		})
	}

	levels, growths := []float64{}, []float64{}
	for _, r := range rows {
		if r.ok {
			levels = append(levels, r.level)
			growths = append(growths, squash(r.growth))
		}
	}
	normLevel := normalizer(levels)
	normGrowth := normalizer(growths)

	for _, r := range rows {
		out := model.RankRow{Wiki: r.w.Wiki, Grade: r.w.Reliability.Grade}
		if r.w.Level != nil {
			out.LevelPerMillion = r.w.Level.Last90PerMillion
		}
		if r.t != nil {
			out.GrowthPctPerYear = r.growth
			out.YoYPct = r.t.YoYPct
			out.PValueAdj = r.t.PValueAdj
		}
		out.AbsGrowthPctPerYear, out.WikiGrowthPctPerYear = math.NaN(), math.NaN()
		if r.w.Absolute != nil {
			out.AbsGrowthPctPerYear = r.w.Absolute.GrowthPctPerYear
		}
		if r.w.Project_ != nil {
			out.WikiGrowthPctPerYear = r.w.Project_.GrowthPctPerYear
		}
		if !r.ok {
			out.OpportunityScore = math.NaN()
			out.Rationale = notRankable(r.w)
		} else {
			out.OpportunityScore = o.Weights["level"]*normLevel(r.level) +
				o.Weights["growth"]*normGrowth(squash(r.growth)) +
				o.Weights["reliability"]*r.rel
			out.Rationale = rationale(r.w, r.t)
		}
		c.Ranking = append(c.Ranking, out)
	}

	sort.SliceStable(c.Ranking, func(i, j int) bool {
		a, b := c.Ranking[i].OpportunityScore, c.Ranking[j].OpportunityScore
		if math.IsNaN(a) != math.IsNaN(b) {
			return math.IsNaN(b) // unrankable rows sink to the bottom
		}
		return a > b
	})

	// When nothing is growing, saying so explicitly matters more than the ranking.
	// A shortlist implies momentum; if every candidate is flat or falling, the
	// order is a statement about existing audience only, and a reader who is not
	// told that will read the top row as "the growing one".
	rankable, rising := 0, 0
	for _, row := range c.Ranking {
		if math.IsNaN(row.OpportunityScore) {
			continue
		}
		rankable++
		if row.GrowthPctPerYear > 5 {
			rising++
		}
	}
	if rankable > 1 && rising == 0 {
		c.Notes = append(c.Notes,
			"No edition shows growing attention share for this topic in this window. The ranking therefore reflects current audience size, not momentum: read it as \"where the existing interest is\", not \"where interest is emerging\".")
	}

	c.Notes = append(c.Notes,
		"The opportunity score mixes current attention share, growth and reliability using the weights shown. It is a shortlisting aid, not a forecast.",
		"Scores are relative within this run only: adding or removing a language edition changes every score.",
		"Wikipedia attention is not willingness to pay. Use this to choose what to validate next, not what to build.")
	return c
}

// squash maps a percent growth rate onto a bounded scale so outliers compress
// instead of dominating: +50%/yr -> 0.39, +400%/yr -> 0.997.
func squash(g float64) float64 {
	if math.IsNaN(g) {
		return 0
	}
	return math.Tanh(g / 120)
}

// normalizer min-max scales a component into 0..1. With a single candidate, or
// with no spread, everything maps to the midpoint rather than to 1 — a lone wiki
// should not score full marks on "relative attention" when there is nothing to
// be relative to.
func normalizer(vals []float64) func(float64) float64 {
	if len(vals) == 0 {
		return func(float64) float64 { return 0.5 }
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range vals {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	if hi-lo < 1e-9 {
		return func(float64) float64 { return 0.5 }
	}
	return func(v float64) float64 {
		s := (v - lo) / (hi - lo)
		return math.Max(0, math.Min(1, s))
	}
}

func notRankable(w *model.WikiAnalysis) string {
	if len(w.Reliability.Blockers) > 0 {
		return "not rankable: " + w.Reliability.Blockers[0]
	}
	if w.Reliability.Grade == "D" {
		return fmt.Sprintf("not ranked: reliability D (score %.2f) - too many reliability checks fail for its trend to be quoted; see this edition's reliability notes",
			w.Reliability.Score)
	}
	return "not rankable: no usable series"
}

func rationale(w *model.WikiAnalysis, t *model.TrendStats) string {
	parts := []string{fmt.Sprintf("%.1f views per million pageviews in the last 90 days", w.Level.Last90PerMillion)}
	switch t.Direction {
	case "rising":
		parts = append(parts, fmt.Sprintf("rising %+.0f%%/yr", t.GrowthPctPerYear))
	case "falling":
		parts = append(parts, fmt.Sprintf("falling %+.0f%%/yr", t.GrowthPctPerYear))
	default:
		parts = append(parts, "no clear direction")
	}
	if t.YoYComparable {
		parts = append(parts, fmt.Sprintf("%+.0f%% year over year", t.YoYPct))
	}
	if w.Seasonal != nil && w.Seasonal.SchoolPattern {
		parts = append(parts, "demand follows the academic calendar")
	}
	if t.SpikeDependent {
		parts = append(parts, "growth is event-driven; the spike-free estimate is used here")
	}
	parts = append(parts, "reliability "+w.Reliability.Grade)
	out := parts[0]
	for _, p := range parts[1:] {
		out += "; " + p
	}
	return out
}

// limitations are the caveats that apply to the whole run regardless of result.
// They are emitted into every report so that a chart shared out of context still
// carries the conditions under which it is true.
func limitations(ds *model.Dataset, an *model.Analysis) []string {
	out := []string{
		"Wikipedia pageviews measure curiosity, not purchase intent. A rising article is a reason to run a landing-page or ad test, not evidence of a market.",
		fmt.Sprintf("Counts use agent=%s and access=%s. Wikimedia's bot filtering is best-effort and its accuracy has changed over time, which can create step changes unrelated to demand.", ds.Params.Agent, ds.Params.Access),
		"Views are counted per article title. Page moves, merges and redirect changes can shift traffic between titles without any change in interest.",
		"Each language edition is read by a different and not strictly national audience: many speakers of smaller languages read the English, Russian or German editions instead, which understates their home edition.",
		"Share of a wiki's total pageviews is the default metric. It controls for wiki-wide traffic changes, but it falls when the wiki grows for unrelated reasons.",
	}
	if len(an.Wikis) > 1 {
		out = append(out, fmt.Sprintf("%d language editions were tested, so p-values are Benjamini-Hochberg adjusted for multiple comparisons.", len(an.Wikis)))
	}
	if ds.Params.IncludeRedirects {
		out = append(out, "Redirect titles were summed into each article, which captures alias traffic but can also fold in a merged former article.")
	} else {
		out = append(out, "Only the canonical article title was counted. Traffic arriving through redirects is excluded; re-run with --include-redirects to include it.")
	}
	for _, w := range an.Wikis {
		if w.Status == model.StatusNoArticle {
			out = append(out, fmt.Sprintf("%s.wikipedia has no article for this concept, so it appears as a gap rather than as zero interest.", w.Wiki))
		}
	}
	return out
}
