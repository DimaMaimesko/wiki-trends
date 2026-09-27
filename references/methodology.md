# Methodology

Every estimator used by `wikitrends`, its assumptions, and why the obvious
alternative was rejected. The unifying theme: Wikipedia pageview series are
**multiplicative, autocorrelated, spiky and seasonal**, and the textbook default
for each of those properties gives a confidently wrong answer here.

## Data source and filters

| choice | value | why |
|---|---|---|
| endpoint | Wikimedia REST pageviews API | the only official per-article source |
| earliest data | 2015-07-01 | per-article coverage starts there; earlier windows are clamped and the change is reported |
| latest data | today − 2 days | Wikimedia publishes with a lag; including a partial day reads as a collapse |
| `agent` | `user` | excludes declared crawlers. `all-agents` would make a trend in scraping look like a trend in demand |
| `access` | `all-access` | desktop/mobile split shifted over the years; splitting invites artefacts |
| granularity | daily | monthly hides spikes and weekly structure; monthly aggregates are derived where needed |

Requests are chunked by **calendar year**. Completed years are immutable and
cached permanently; only chunks touching the last 45 days carry a TTL. This makes
window changes cheap, which is what makes iterative research possible.

## Concept resolution

Titles are never matched across languages by string similarity. A free-text topic
is resolved to a **Wikidata item**, and each edition's article is taken from that
item's sitelinks. Consequences:

- A comparison is guaranteed to be about one concept.
- A missing sitelink is a positive finding: that edition does not cover the topic.
- Redirects are followed before measuring. Pageviews are counted per *title*, so
  measuring a redirect measures one alias's share of arrivals, not the topic.
- Article creation dates are fetched, so pre-creation days can be excluded.

## What API silence means

The API omits days rather than returning zero. Three cases are distinguished by
cross-referencing the edition's own total pageviews for the same day:

| article row | project row | meaning | stored as |
|---|---|---|---|
| absent | absent | nothing was measured | `null` (unknown) |
| absent | present, > 0 | pipeline ran, article got no views | `0` (true zero) |
| absent | any, before article creation | article did not exist | `null` (not applicable) |
| present | any | observed | the value |

Getting this wrong is the single easiest way to fabricate a trend: filling
pre-creation days with zeros makes every young article look like explosive
growth, and dropping true zeros biases the mean upward and hides exactly the
"too small to matter" signal a product decision needs.

## Normalisation: share of attention

```
share_t = views_t / project_views_t × 10^6
```

The denominator is that edition's total pageviews under the same `agent`/`access`
filters. This is the default metric because it answers the question actually
being asked ("how much of this audience cares") and because it absorbs:

- differences in edition size (German serves orders of magnitude more traffic than Czech);
- platform-wide traffic decline;
- step changes in Wikimedia's bot classification, which hit the numerator and
  denominator together.

Its weakness is symmetric and is reported: share falls if the edition grows for
unrelated reasons. Both metrics are always computed, and the edition's own trend
is always reported alongside.

## Trend estimation

### Log scale

Fits are on `log(x + q)`, where `q` is the metric's smallest meaningful increment
— one pageview for counts, its share-equivalent (`10^6 / median(project views)`)
for the normalised series. Reported as **compound % per year**:

```
growth = (exp(slope_per_day × 365.25) − 1) × 100
```

Log because attention is multiplicative: "+40 %/yr" transfers between a large and
a small edition, "+31 views/day" does not. The `q` offset keeps genuine zero days
representable; a fixed `log1p` would distort a rate whose typical value is 2.5.

### Newey–West HAC standard errors

Consecutive days of Wikipedia traffic are strongly correlated (weekly rhythm plus
persistent news cycles), so 730 daily observations carry far less independent
information than 730. Ordinary least-squares standard errors assume independence
and, on this data, understate uncertainty by a factor of roughly 2–5 — which turns
noise into "statistically significant growth".

Heteroskedasticity- and autocorrelation-consistent errors are used instead, with
the standard bandwidth `L = floor(4 (n/100)^(2/9))`. Confidence intervals are
built on the log slope and then exponentiated, so they are asymmetric in percent
space — correct for a multiplicative process, and informative in itself: a CI of
`[−5 %, +90 %]` is not "+40 % growth".

`internal/stats` has a test asserting HAC errors exceed naive ones by ≥1.5× at
φ=0.9. If that test fails, every p-value the skill reports is too small.

### Robust cross-checks

- **Theil–Sen** — median of all pairwise slopes, 29 % breakdown point, so it
  ignores spikes entirely. Disagreement with the log-linear slope means a few days
  are steering the parametric fit.
- **Mann–Kendall** — rank-based, distribution-free, insensitive to outlier
  magnitude. This is the headline "is it real?" number. It assumes serially
  independent observations, so it is run on **monthly means**, where that
  assumption is tenable. Running it on daily data would inflate significance for
  exactly the reason HAC errors exist. Tie-corrected variance.
- **Aligned year-over-year** — mean of the last 365 days against the 365 before.
  Needs no model and cannot be faked by seasonality, because both windows span
  every season once. This is the number to quote to a non-technical stakeholder.
  Its cost is that it uses two numbers and says nothing about shape.

Disagreement between the full-window trend and the aligned year-over-year figure
is reported as an **inflection**, not an error: a topic that fell for two years and
stopped falling last year is a different investment case from one still falling.

### Multiple comparisons

Mann–Kendall p-values are adjusted across the editions in a run using
Benjamini–Hochberg. At ten editions and α = 0.05 you would otherwise expect a
false "significant trend" in roughly every second scan.

## Seasonality

Multiplicative indices by ratio-to-level, with the two cycles handled differently:

- **Weekly** — ratio to a 7-day centred moving average, median of ratios by
  weekday. Cheap and robust; a month of data suffices.
- **Annual** — needs two full years. The series is de-weeklied, then detrended on
  the log scale against a 365-day centred mean whose undefined half-year at each
  edge is filled by extrapolating the mean's own local slope. Monthly index is the
  median of the residuals for that calendar month.

Two alternatives were tried and rejected on real data:

1. A plain 365-day centred mean leaves 182 days undefined at each end, so a
   two-year window yields one observation per calendar month and the "index"
   absorbs whatever else happened that month.
2. A single global log-linear detrend uses every day but describes a steeply
   falling series badly, and its residuals map onto months by accident — on
   Ukrainian astronomy it produced a 5.3× peak-to-trough ratio with February a
   peak and *March* a trough.

### The consistency gate

An annual pattern is only claimed if it **repeats**. The monthly shape is
re-estimated inside each 365-day block and the blocks are correlated pairwise;
mean correlation below 0.35 drops the claim. An earlier version only checked
whether the overall peak and trough months fell on the right side of each block's
median, which a single 60-day bump passes about half the time by chance.

Confidence (`high`/`medium`/`low`/`none`) combines years of data, mean daily
volume and that consistency. At `low`, peak months are suppressed rather than
named — a monthly median over single-digit counts is arithmetic, not evidence.

Validated against Ukrainian astronomy, where September is the peak and July the
trough in **every** year of the window, at roughly 5× amplitude.

## Events and structural breaks

### Spikes

Baseline is a 29-day centred rolling **median**; residuals are taken on the log
scale; scale is the **MAD** of those residuals; days above `z = 4` are flagged and
consecutive flagged days are grouped into one event (a news cycle lasts days).

Each choice defends against a specific failure: a rolling mean would let a spike
lift its own baseline; linear residuals would make the threshold mean "k views
above" instead of "k times the usual level", so the same test could not serve an
article with 30 views/day and one with 300 000; a standard deviation would be
inflated by one huge event enough to hide every other one.

Every trend is then computed twice — as-is, and with spike days replaced by the
local baseline. A sign flip, or more than half the growth disappearing, sets
`spike_dependent`. Rankings use the spike-free estimate.

### Level shifts

Fitted jointly for every candidate break date `t`:

```
log(v_i) = a + b·i + c·1{i ≥ t} + u_i
```

Break date chosen by minimum residual sum of squares (coarse 7-day grid, then
local refinement), then refitted with Newey–West errors. Reported only if the step
exceeds 25 % and |z| ≥ 3.

Estimating slope and step **together** is essential, and was the result of getting
it wrong first: fitting a trend and then hunting for a step in the residuals lets
least squares tilt the line to straddle the step, so an injected 40 % drop is
reported as 13 %.

After trend and seasonality are accounted for, a surviving discontinuity is
usually *measurement* — a page move, a merge, a redirect retarget, a bot
reclassification, a mobile app release — so it is flagged for interpretation
rather than modelled away.

## Reliability grading

A weighted deduction from 1.0, split into **blockers** (the question cannot be
answered) and **penalties** (it can, with qualifications).

Blockers: mean daily views below the floor (default 10); completeness below 80 %;
fewer than 120 usable days; article younger than half the window.

Penalties, each contributing a sentence to the output: low volume; window under
two years; no aligned year-over-year available; trend not significant after FDR;
CI spanning zero; spike-dependence; high spike share of views; OLS/Theil–Sen
disagreement; trend/year-over-year disagreement; absolute/share disagreement; a
detected level shift; article created inside the window; data gaps.

Grade thresholds: A ≥ 0.80, B ≥ 0.60, C ≥ 0.40, else D. Any blocker forces D.

The grade is never emitted without its reasons. A bare letter invites an agent to
restate it as confidence and a reader to accept it; a list of named checks keeps
the argument inspectable and lets a user disagree with one check rather than the
whole verdict.

## Rendering

Charts are described once against a `Canvas` interface and emitted by two
backends: SVG (full Unicode, for screen and HTML) and a from-scratch PDF writer
(base-14 Helvetica, for the shareable one-pager). Because base-14 fonts cannot
render Cyrillic, Greek or CJK, the PDF prefers the concept's **English Wikidata
label** and transliterates anything still non-Latin; native titles are preserved
in the HTML, SVG, CSV and JSON.

Long series are decimated by min/max bucketing, not stride sampling: dropping
every other day of a spiky series can drop the spike, which is the feature the
reader is looking for. Min/max keeps the envelope exactly at a fraction of the
vertices.
