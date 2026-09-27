---
name: wiki-trends
description: Research how public interest in a topic is changing, per language, using Wikipedia pageview data, then produce a shareable one-page report. Use it for every such question, in any language, instead of web search or reading result files left from earlier runs: whether interest in a subject is growing or declining; which countries, languages or markets are most interested in a topic; which language to launch or localize into next; how interest compares across countries or Wikipedia language editions; whether a course, content or feature idea has an audience. Also use for follow-ups that change assumptions (window, metric, weights, spikes, more languages). Triggers on "is interest in X growing", "which language should we launch in", "compare interest in X in A vs B", "which countries are most interested in X", "should we add a course on X", "Wikipedia pageviews", "порівняй інтерес до X", "в яких країнах цікавляться X", "чи росте інтерес до X".
license: MIT
---

# wiki-trends

Turn Wikipedia pageview data into a defensible answer to "where should we invest
next?" — with the reliability of that answer stated as plainly as the answer
itself.

The code lives in this directory. It handles concept resolution, fetching,
caching, the statistics, and rendering. **Your job is to frame the question,
choose the comparison, and communicate the result honestly.** Do not re-implement
any of the analysis yourself, and do not compute trends from raw pageview numbers
in your head or in an ad-hoc script — the estimators here exist specifically
because the naive versions of them give confidently wrong answers on this data.

**Answer every new question from a run you make for it.** Do not answer from
directories already sitting in `runs/`, `examples/` or anywhere else: they were
computed for another question, window, set of editions or metric, and a report
built on them quotes numbers the user never asked for.

## Setup

Requires Go 1.21 or newer and nothing else. There are no dependencies to install,
no build step, and no compiled binaries in this repository.

```bash
cd <this skill directory>
go run ./cmd/wikitrends doctor      # checks network, cache, API reachability
```

Run every command as `go run ./cmd/wikitrends <subcommand>` from this directory.
`go run ./cmd/wikitrends help` lists all flags.

## The workflow — follow it in order

### 1. Resolve the concept before measuring anything

```bash
go run ./cmd/wikitrends resolve --topic "intermittent fasting" --wikis pl,cs,uk
```

This is not optional for a new topic. It answers three questions that silently
ruin the analysis if skipped:

- **Which concept?** "Astronomy" could be the science (Q333), the journal, or a
  disambiguation page. The command prints candidates with Wikidata ids.
- **Which article in each edition?** Titles are not translations. Polish for
  intermittent fasting is not "Przerywany post"; matching is done through
  Wikidata, not string similarity.
- **Does the article exist at all?** Polish Wikipedia has *no* article for
  intermittent fasting. That is a finding, not an error — see "No article" below.

**If the top candidate is not obviously right, ask the user which concept they
mean before running anything.** Show them the candidate labels and descriptions.
Guessing here produces a chart that looks fine and answers the wrong question.

For a broad subject ("astronomy as a course topic"), one article is usually too
narrow. Resolve several and pass them as a basket — see "Baskets" below.

### 2. Run the analysis

```bash
go run ./cmd/wikitrends run \
  --qid Q1666254 --wikis cs,uk --months 24 \
  --question "Is interest in intermittent fasting growing in Czech and Ukrainian?" \
  --out runs/fasting
```

Always pass `--qid` once you have resolved it, and always pass `--question` so
the report explains itself to whoever it gets forwarded to.

Prints a compact Markdown summary to stdout — **read that, not the JSON** — and
writes to `--out`:

| file | what it is |
|---|---|
| `summary.md` | what was printed; the thing to re-read later |
| `report.pdf` | the one-page shareable report |
| `report.html` | same content, native scripts, clickable articles, prints to A4 |
| `analysis.json` | every computed statistic, for drilling into a specific number |
| `dataset.json` | the daily series; input for re-analysis |
| `series.csv` | raw daily values, so any claim can be checked by hand |
| `trend.svg`, `growth.svg` | individual charts for decks |

### 3. Handle follow-ups without refetching

Nearly every follow-up is a change of *assumption*, not of data. Re-run
`analyze` against the existing directory — it never touches the network:

```bash
go run ./cmd/wikitrends analyze --in runs/fasting --metric absolute
go run ./cmd/wikitrends analyze --in runs/fasting --weights level=0.7,growth=0.2,reliability=0.1
go run ./cmd/wikitrends analyze --in runs/fasting --min-daily 30
```

Only re-run `run` when the *data* must change: a different window, a new language
edition, a different article, or `--include-redirects`. Even then the cache means
only the genuinely new slice is downloaded, so a 24→36 month change is cheap.

### 4. Shortlist many editions

```bash
go run ./cmd/wikitrends scan --topic "English language" \
  --wikis pl,cs,uk,de,es,tr,id,vi,ro,hu --months 40 --out runs/english
```

Same pipeline, compact ranked output. p-values are Benjamini-Hochberg adjusted
for the number of editions tested, because at ten editions and α=0.05 you would
otherwise expect a false "significant trend" roughly every other run.

## How to read the output

### Two metrics, and why `share` is the default

- **`share`** — views per million of that edition's *total* pageviews. This is
  the default and usually the right one. It answers "how much of this audience's
  attention does the topic hold", it is comparable between a large and a small
  edition, and it absorbs platform-wide shocks.
- **`absolute`** — raw daily views. Right for market *sizing* ("how many people
  might we reach"), wrong for trend comparison.

The distinction is not academic right now: every Wikipedia edition has been
losing human pageviews, so **almost any topic looks like it is declining in
absolute terms**. The summary always prints the edition's own traffic trend
(`wiki %/yr`) next to the topic's. Use it:

| absolute | share | means |
|---|---|---|
| falling | flat | the topic is holding its ground; the edition is shrinking |
| falling | falling | genuine loss of interest |
| flat | rising | the topic is gaining share of a shrinking audience |
| rising | falling | the topic grew slower than the edition did |

**When you report an absolute change you must state the edition's own change in
the same sentence.** Otherwise the number is misleading even though it is correct.

**Never compare the share trend with `wiki %/yr`.** Share already has the edition
divided out: share −15%/yr next to an edition at −23%/yr does *not* mean the topic
"holds up better than the edition" — it means the topic lost ground *inside* the
edition. Compare `absolute` with `wiki %/yr`, or simply quote the verdict's
"Against its own edition" sentence, which does this for you.

### Reliability grades

Every edition gets A/B/C/D plus the *reasons*. Quote the reasons, never the bare
letter.

- **A** — adequate volume, ≥2 years, significant after FDR adjustment,
  spike-independent, consistent across metrics and estimators.
- **B** — usable with stated caveats.
- **C** — directional only. Say "suggests" and name the weakness.
- **D / blockers present** — **do not report a growth rate.** The correct output
  is "not measurable from this data", not "flat" and not the point estimate.
  This holds for every D, with or without a listed blocker: a D without one
  means several checks failed at once. D editions are left out of the ranking
  and their verdict opens with "Not reliable enough to quote".

A blocker means the question cannot be answered from this data. The most common
one is volume: below ~10 views/day, a percentage change is noise. The summary
still prints a point estimate for context; **it is not quotable.**

### The cross-checks, and what disagreement means

The tool computes each trend several ways on purpose. When they disagree, say so
— that *is* the finding:

| disagreement | what it means | what to say |
|---|---|---|
| trend vs trend-without-spikes | a news event is carrying the growth | "event-driven, not a trend" |
| log-linear fit vs Theil–Sen | a few days steer the parametric fit | "fragile estimate" |
| absolute vs share | the edition moved, not the topic | report both, name the cause |
| full-window trend vs year-over-year | the series is not monotonic | "an inflection: it fell, then stopped" |
| a level shift survives detrending | likely a *measurement* change | check article history before claiming demand |

A **level shift** is a persistent step the trend and seasonality do not explain.
Its usual causes are a page move, a merge, a redirect retarget, or a change in
Wikimedia's bot classification — not demand. When one is reported, check the
article's revision history before interpreting it, or say it is unexplained.

### Seasonality

The annual index is only claimed when the pattern *repeats*: the monthly shape is
estimated per year and the years are correlated with each other
(`annual_consistency`). A one-off March event is therefore not reported as "demand
peaks in spring". Confidence is `high`/`medium`/`low`/`none` from years of data,
daily volume and that consistency; at `low` the peak months are suppressed
entirely rather than named.

Name a peak month only if the output lists it (`peaks in months [...]` or a
"Strongly seasonal" verdict), and say "academic calendar" only if the output says
so. Otherwise say the edition shows no seasonal pattern the data can support.

When an academic-calendar pattern is detected, it is directly actionable — say so:
it changes launch timing, content calendars and ad spend. For an education
product this is often more valuable than the trend itself.

### "No article" is a finding

When an edition has no article for the concept, that is reported as a coverage
gap with search candidates, never as zero interest. It can mean the topic is
genuinely not discussed in that language, or that readers there use a different
article, or that they read about it in English/German/Russian instead. Options,
in order of preference:

1. Report the gap, and say explicitly that it is not evidence of low interest.
2. Pick a near-equivalent article from the candidates and be explicit about the
   substitution: `--titles pl="Głodówka lecznicza"` measures therapeutic fasting,
   which is *related to but not the same as* intermittent fasting. Say that in the
   report.
3. Suggest the user check whether that audience reads a larger edition instead.

### Baskets: one article is rarely one topic

"Is there interest in astronomy?" is not answered by the article *Astronomy*.
Resolve several related concepts and sum them:

```bash
go run ./cmd/wikitrends run --topic "astronomy" --qid Q333 \
  --basket-topics "solar system,galaxy,black hole,planet,star" \
  --wikis uk --months 36 --out runs/astro
```

**Use `--basket-topics` (names), not `--basket` (Wikidata ids), unless you have
just resolved the ids in this session.** Ids are not memorable and a wrong one
fails silently: `Q11405` looks like it could be an astronomy article and is in
fact *flute*. Names are resolved through the same search path as `--topic`, and
every resolution is recorded in the report's fetch notes so the substitution is
visible.

Use a basket when the topic is a subject area, a course, or a product category.
Keep it to one concept when the user asks about a specific named thing. Always
list the basket members in what you report — a basket is an editorial choice and
the reader must be able to disagree with it.

## Rules for what you may claim

These exist because the failure mode of this kind of analysis is not a wrong
number, it is a plausible number acted on by someone who was not told what it
rests on.

1. **Never state a growth rate without its window, its metric and its reliability
   grade.** "+40%/yr" alone is not a finding.
2. **Never convert attention into demand.** Wikipedia measures curiosity. The
   correct recommendation from a rising article is *what to validate next* — a
   landing page, an ad test, a keyword check — not what to build.
3. **Never describe a D-grade series as rising, falling, stable or flat, and never
   recommend it first.** "Not measurable" and "no growth" lead to opposite
   decisions. At most say "not measurable; the point estimate is unreliable
   because …" and quote its reliability reasons.
4. **Never explain a spike you have not checked.** Either look it up (article
   history, news for that date) or say the cause is unidentified. Listing
   possible causes ("political news? an education reform?") is explaining it too.
5. **Say when a shortlist has no rising candidate.** The ranking then reflects
   existing audience only, and the top row must not be read as momentum.
6. **Carry the limitations into anything you paste elsewhere.** They are in every
   report for that reason.
7. **A language edition is not a country.** Speakers of smaller languages often
   read the English, Russian or German edition, which understates their own.
   Say so when recommending a locale. Name editions as editions ("Ukrainian
   Wikipedia", `uk`), not as countries and not with flags.
8. **Use the tool's direction words.** A series marked `falling` is not "stable"
   or "holding up" because other editions fall faster; say "falling more slowly
   than the others".
9. **Keep article and edition numbers apart.** `level:` lines are the article's
   own views; the whole edition appears only as `wiki %/yr` and the `context:`
   line. A high share per million in a small edition means a large slice of a
   small audience, not a large audience.

## Follow-up playbook

| the user says | do this |
|---|---|
| "use absolute numbers" | `analyze --metric absolute` |
| "ignore the spike" | already reported; quote `growth_ex_spikes_pct_per_year`, or `analyze --spike-z 3` to catch more |
| "I care about audience size, not growth" | `analyze --weights level=0.7,growth=0.15,reliability=0.15` |
| "make it three years" / "since 2019" | `run --months 36` or `--start`; cached years are reused |
| "add German and Spanish" | `run` again with the extra codes; existing editions are cache hits |
| "is that significant?" | quote the FDR-adjusted p-value, the CI, and the Mann-Kendall result together |
| "why is it falling?" | separate topic decline from edition decline using `share` vs `absolute` vs `wiki %/yr`; check level shifts; do not speculate beyond that |
| "include redirects" | `run --include-redirects` (re-fetch; it changes the data) |
| "what about mobile only" | `run --access mobile-web` |
| "give me a one-pager" | `report.pdf` is already written; re-render with `report --in DIR --title "..."` |
| "compare against a control topic" | run a second topic over the same window and editions, then compare share levels |

## Traps checklist

Before reporting, confirm each of these:

- [ ] Every number comes from a run made for this question, not from an older directory.
- [ ] The article is the concept the user meant, not a synonym or disambiguation page.
- [ ] The metric is named in the sentence that contains the number.
- [ ] The edition's own traffic trend is stated if absolute numbers are quoted.
- [ ] No growth rate or direction is quoted for a D-grade series, and none is recommended first.
- [ ] Editions are named as language editions, not as countries.
- [ ] Reliability reasons accompany every grade.
- [ ] Spike-dependence and level shifts are mentioned if flagged.
- [ ] `--agent user` (the default) was used; `all-agents` measures crawlers too.
- [ ] Seasonality claims are gated on the reported confidence.
- [ ] The recommendation is a validation step, not a build decision.

## Deeper reference

Load these only when you need them:

- `references/methodology.md` — every estimator, its assumptions, and why the
  obvious alternative was rejected.
- `references/interpretation.md` — turning numbers into recommendations, with
  worked decision rules and wording templates.
- `references/confounders.md` — the catalogue of things that manufacture a fake
  trend in this data, and how each is detected here.
- `references/editions.md` — choosing which language editions to compare.
