// Package model defines the on-disk JSON contracts between pipeline stages.
//
// Design notes
//
//   - Every artifact carries a "schema" string. The agent (and future versions of
//     this skill) can detect shape changes instead of guessing.
//   - Daily series are stored as parallel arrays anchored at a single Start date
//     rather than as [{date,value}] records. A two-year, ten-wiki fetch is ~7300
//     points per wiki; records would triple the file size for no gain.
//   - Missing data is `null`, never 0. The Wikimedia API omits days entirely when
//     an article had no views, and it also omits days it has no data for at all.
//     Those two cases mean completely different things for a trend estimate, so
//     they are resolved at fetch time (see tseries.Reconcile) and recorded
//     separately in Coverage.
package model

import "time"

const (
	DatasetSchema  = "wikitrends/dataset/1"
	AnalysisSchema = "wikitrends/analysis/1"
)

// ---------------------------------------------------------------- dataset

type Dataset struct {
	Schema    string    `json:"schema"`
	Tool      string    `json:"tool"`
	FetchedAt time.Time `json:"fetched_at"`

	Topic  Topic       `json:"topic"`
	Window Window      `json:"window"`
	Params FetchParams `json:"params"`
	Series []Series    `json:"series"`

	// Notes are fetch-time observations the agent must surface to the user:
	// missing articles, truncated windows, suspicious coverage.
	Notes []string `json:"notes,omitempty"`
}

// Topic identifies *the concept*, not a title. Comparing "Astronomie" (de) with
// "Астрономія" (uk) is only meaningful if both are the same Wikidata item;
// resolving through Wikidata is what makes cross-language numbers comparable.
type Topic struct {
	Query         string `json:"query"`
	QID           string `json:"qid,omitempty"`
	LabelEn       string `json:"label_en,omitempty"`
	DescriptionEn string `json:"description_en,omitempty"`
	// Basket holds extra QIDs when the topic is broader than one article
	// (e.g. "astronomy" as a course subject). Views are summed per wiki.
	Basket []BasketItem `json:"basket,omitempty"`
}

type BasketItem struct {
	QID     string `json:"qid"`
	LabelEn string `json:"label_en,omitempty"`
}

type Window struct {
	Start       string `json:"start"` // YYYY-MM-DD inclusive
	End         string `json:"end"`   // YYYY-MM-DD inclusive
	Granularity string `json:"granularity"`
}

type FetchParams struct {
	Access           string `json:"access"` // all-access | desktop | mobile-web | mobile-app
	Agent            string `json:"agent"`  // user | all-agents | spider | automated
	IncludeRedirects bool   `json:"include_redirects"`
}

// SeriesStatus explains why a wiki may have no numbers. These are findings, not
// errors: "Polish Wikipedia has no article on this topic" is a real answer to a
// product question and must never be silently dropped or shown as zero interest.
type SeriesStatus string

const (
	StatusOK        SeriesStatus = "ok"
	StatusNoArticle SeriesStatus = "no_article" // concept not covered in this wiki
	StatusTooNew    SeriesStatus = "too_new"    // article younger than the window
	StatusNoData    SeriesStatus = "no_data"    // API returned nothing usable
	StatusAmbiguous SeriesStatus = "ambiguous"  // needs human/agent disambiguation
)

type Series struct {
	Wiki    string       `json:"wiki"`    // language code, e.g. "cs"
	Project string       `json:"project"` // e.g. "cs.wikipedia"
	Status  SeriesStatus `json:"status"`
	Note    string       `json:"note,omitempty"`

	Articles []Article `json:"articles,omitempty"`

	Start string `json:"start,omitempty"` // first date of the arrays
	Views Nums   `json:"views,omitempty"` // NaN encodes missing -> marshals as null
	// ProjectViews is the wiki's total pageviews for the same day and the same
	// access/agent filters. It is the denominator that makes cross-wiki and
	// cross-time comparison honest: it absorbs wiki growth, bot-reclassification
	// steps and platform-wide traffic shocks.
	ProjectViews Nums `json:"project_views,omitempty"`

	Coverage Coverage `json:"coverage"`

	// Candidates are offered when resolution failed or was ambiguous so the agent
	// can ask the user a precise question instead of guessing.
	Candidates []Candidate `json:"candidates,omitempty"`
}

type Article struct {
	Title     string   `json:"title"`
	TitleEn   string   `json:"title_en,omitempty"`
	URL       string   `json:"url,omitempty"`
	QID       string   `json:"qid,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	Redirects []string `json:"redirects,omitempty"`
}

type Candidate struct {
	Title   string `json:"title"`
	QID     string `json:"qid,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Size    int    `json:"size,omitempty"`
}

type Coverage struct {
	Days         int     `json:"days"`
	Observed     int     `json:"observed"`      // days with a real API value
	ZeroFilled   int     `json:"zero_filled"`   // API silent but wiki reported traffic -> true 0
	Gaps         int     `json:"gaps"`          // no article data AND no project data -> unknown
	BeforeCreate int     `json:"before_create"` // days the article did not exist
	Completeness float64 `json:"completeness"`  // (observed+zero)/eligible days
}

// ---------------------------------------------------------------- analysis

type Analysis struct {
	Schema     string          `json:"schema"`
	AnalyzedAt time.Time       `json:"analyzed_at"`
	Topic      Topic           `json:"topic"`
	Window     Window          `json:"window"`
	Params     FetchParams     `json:"params"`
	Options    AnalysisOptions `json:"options"`

	Wikis      []WikiAnalysis `json:"wikis"`
	Comparison *Comparison    `json:"comparison,omitempty"`

	Limitations []string `json:"limitations"`
}

// AnalysisOptions is echoed into analysis.json so that a later `report` run can
// reproduce the exact same analysis from dataset.json without being told the
// flags again. Every field here is a finite scalar or a string, so it round-trips
// losslessly - unlike the computed metrics, which use NaN for "not computed".
type AnalysisOptions struct {
	// Metric the headline verdict is based on. "share" = views per million
	// project pageviews (default, comparable); "absolute" = raw views.
	Metric string `json:"metric"`
	// SpikePolicy controls whether event spikes feed the trend estimate.
	SpikePolicy   string             `json:"spike_policy"` // keep | winsorize | both
	MinDailyViews float64            `json:"min_daily_views"`
	SpikeZ        float64            `json:"spike_z"`
	Alpha         float64            `json:"alpha"`
	Weights       map[string]float64 `json:"weights,omitempty"`
}

type WikiAnalysis struct {
	Wiki     string       `json:"wiki"`
	Project  string       `json:"project"`
	Status   SeriesStatus `json:"status"`
	Note     string       `json:"note,omitempty"`
	Articles []Article    `json:"articles,omitempty"`
	Coverage Coverage     `json:"coverage"`
	// Candidates carry forward the search hits offered when a wiki has no
	// article for the concept, so the agent can propose a native-language title
	// without re-querying.
	Candidates []Candidate `json:"candidates,omitempty"`

	Level    *LevelStats  `json:"level,omitempty"`
	Absolute *TrendStats  `json:"absolute,omitempty"`
	Share    *TrendStats  `json:"share,omitempty"`
	Project_ *TrendStats  `json:"project_trend,omitempty"`
	Seasonal *SeasonStats `json:"seasonality,omitempty"`
	Events   *EventStats  `json:"events,omitempty"`

	Reliability Reliability `json:"reliability"`
	Verdict     string      `json:"verdict"`
}

type LevelStats struct {
	MeanDaily        float64 `json:"mean_daily"`
	MedianDaily      float64 `json:"median_daily"`
	TotalViews       float64 `json:"total_views"`
	Last90Mean       float64 `json:"last90_mean_daily"`
	MeanPerMillion   float64 `json:"mean_per_million"`
	Last90PerMillion float64 `json:"last90_per_million"`
	// ShareOfWiki is the topic's average share of all pageviews on that wiki,
	// expressed per million. It is the closest available proxy for "how much of
	// this audience's attention does the topic already hold".
}

type TrendStats struct {
	// Growth is expressed as compound % per year, derived from a log-linear fit.
	// Log scale because attention series are multiplicative: "+40%/yr" travels
	// across wikis of different size, "+31 views/day" does not.
	GrowthPctPerYear float64 `json:"growth_pct_per_year"`
	CILow            float64 `json:"ci_low_pct"`
	CIHigh           float64 `json:"ci_high_pct"`
	// HAC standard errors (Newey-West). Daily pageviews are strongly
	// autocorrelated; naive OLS errors would understate uncertainty several-fold
	// and manufacture significance.
	StdErrKind string  `json:"stderr_kind"`
	NWLag      int     `json:"nw_lag"`
	PValue     float64 `json:"p_value"`
	PValueAdj  float64 `json:"p_value_adjusted,omitempty"` // Benjamini-Hochberg across wikis
	R2         float64 `json:"r2"`
	N          int     `json:"n"`

	// Robust, distribution-free cross-check on monthly aggregates.
	TheilSenPctPerYear float64 `json:"theil_sen_pct_per_year"`
	MannKendallP       float64 `json:"mann_kendall_p"`
	MannKendallTau     float64 `json:"mann_kendall_tau"`

	// Seasonality-immune comparison of aligned calendar windows.
	YoYPct        float64 `json:"yoy_pct"`
	YoYComparable bool    `json:"yoy_comparable"`
	YoYRecentMean float64 `json:"yoy_recent_mean"`
	YoYPriorMean  float64 `json:"yoy_prior_mean"`

	// Trend recomputed with event spikes winsorized.
	GrowthExSpikesPctPerYear float64 `json:"growth_ex_spikes_pct_per_year"`
	SpikeDependent           bool    `json:"spike_dependent"`

	Direction string `json:"direction"` // rising | falling | flat | unclear
}

type SeasonStats struct {
	HasAnnual    bool    `json:"has_annual"`
	YearsCovered float64 `json:"years_covered"`
	WeeklyAmp    float64 `json:"weekly_amplitude"` // max/min of day-of-week index
	AnnualAmp    float64 `json:"annual_amplitude"` // max/min of monthly index
	// AnnualConsistency is the mean correlation between the monthly shapes of
	// each year in the window: how much the pattern actually repeats. A one-off
	// event scores near zero; a real academic calendar scores high.
	AnnualConsistency float64   `json:"annual_consistency"`
	MonthlyIndex      []float64 `json:"monthly_index,omitempty"`
	PeakMonths        []int     `json:"peak_months,omitempty"`
	TroughMonths      []int     `json:"trough_months,omitempty"`
	SchoolPattern     bool      `json:"school_pattern"` // Sep-May high, Jul-Aug low
	// Confidence guards against over-claiming. A monthly index estimated from
	// two years of a 9-views-per-day article is arithmetic, not evidence, and the
	// report must say so rather than name a peak month.
	Confidence string `json:"confidence"` // high | medium | low | none
	// PeakMonth/TroughMonth and PeakTroughRatio express the pattern as the one
	// fact a launch decision needs: which month is busiest, and by how much.
	PeakMonth   int `json:"peak_month,omitempty"`
	TroughMonth int `json:"trough_month,omitempty"`
}

type EventStats struct {
	SpikeDays      int     `json:"spike_days"`
	SpikeViewShare float64 `json:"spike_view_share"` // fraction of window views inside spikes
	TopSpikes      []Spike `json:"top_spikes,omitempty"`
	LevelShifts    []Shift `json:"level_shifts,omitempty"`
}

type Spike struct {
	Date  string  `json:"date"`
	Views float64 `json:"views"`
	Base  float64 `json:"baseline"`
	Z     float64 `json:"z"`
}

type Shift struct {
	Date     string  `json:"date"`
	Before   float64 `json:"before"`
	After    float64 `json:"after"`
	RatioPct float64 `json:"ratio_pct"`
}

// Reliability is deliberately a grade *plus its reasons*. A bare letter invites
// the agent to assert confidence it cannot justify; the reasons let it explain,
// and let the user disagree with a specific check rather than the whole verdict.
type Reliability struct {
	Grade    string   `json:"grade"` // A | B | C | D
	Score    float64  `json:"score"` // 0..1
	Reasons  []string `json:"reasons"`
	Blockers []string `json:"blockers,omitempty"`
}

type Comparison struct {
	Metric  string    `json:"metric"`
	Ranking []RankRow `json:"ranking"`
	// Weights used for the opportunity score, echoed so the user can change them.
	Weights map[string]float64 `json:"weights"`
	Notes   []string           `json:"notes,omitempty"`
}

type RankRow struct {
	Wiki             string  `json:"wiki"`
	LevelPerMillion  float64 `json:"level_per_million"`
	GrowthPctPerYear float64 `json:"growth_pct_per_year"`
	// AbsGrowthPctPerYear is the same topic measured without normalising by the
	// edition's own traffic. Reporting both is what separates "fewer people read
	// about this" from "fewer people read this Wikipedia".
	AbsGrowthPctPerYear  float64 `json:"abs_growth_pct_per_year"`
	WikiGrowthPctPerYear float64 `json:"wiki_growth_pct_per_year"`
	YoYPct               float64 `json:"yoy_pct"`
	PValueAdj            float64 `json:"p_value_adjusted"`
	Grade                string  `json:"grade"`
	OpportunityScore     float64 `json:"opportunity_score"`
	Rationale            string  `json:"rationale"`
}
