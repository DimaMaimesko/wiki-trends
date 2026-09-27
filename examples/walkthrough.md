# Worked examples

The three request shapes the skill is built for, with the actual commands and
what the data actually said. Outputs are real, from runs made while building this.

`examples/fasting/` contains the full artifact set from example 1, so you can see
the deliverable without running anything.

---

## 1. "Compare the growth of interest in intermittent fasting in the Polish and Czech Wikipedia over the last two years."

### Resolve first — and the resolve step is the answer

```bash
go run ./cmd/wikitrends resolve --topic "intermittent fasting" --wikis pl,cs,uk
```

```
Concept candidates for "intermittent fasting" (best first):
  * Q1666254     Intermittent fasting — ... meal timing schedules that cycle between fasting and non-fasting
    Q44602       Fasting — the act of refraining from eating
    Q6831466     Michael Mosley — ...
  * = used below. If it is the wrong concept, re-run with --qid <other>.

Article per language edition:
  pl     NO ARTICLE for this concept
           candidate: Głodówka lecznicza
           ...
  cs     Přerušovaný půst  (created 2020-10-28)
  uk     Інтервальне голодування  (created 2019-11-10)
```

**Polish Wikipedia has no article for intermittent fasting.** The comparison the
user asked for cannot be made directly, and finding that out costs nine cached API
calls rather than a misleading chart.

At this point you go back to the user. Two honest options: report the gap, or
substitute the nearest Polish article and say what changed. Doing the second:

```bash
go run ./cmd/wikitrends run --qid Q1666254 --topic "intermittent fasting" \
  --wikis cs,pl --titles "pl=Głodówka lecznicza" --months 24 \
  --title "Intermittent fasting: Czech vs Polish Wikipedia" \
  --question "Compare the growth of interest ... over the last two years." \
  --out examples/fasting
```

### What came back

```
### cs.wikipedia — reliability D
Not measurable: only 9.2 views/day on average (floor is 10) ... Treat this
edition as unmeasured, not as flat — the point estimate below is reported for
context only and must not be quoted as a growth rate.
- level shift: +75% around 2025-12-23 — check article history before reading as demand

### pl.wikipedia — reliability D
Not measurable: only 8.0 views/day ...
- level shift: +44% around 2024-12-29 — check article history before reading as demand
```

**Both articles are too small to support a growth claim.** The honest answer to the
user's question is that Wikipedia cannot answer it for these two editions: at 8–9
views a day, a percentage change is noise. The trend figures are printed
(−42 %/yr and −30 %/yr in share) but flagged unquotable, and each edition also
shows a level shift that would need checking against the article history before
anyone read it as demand.

The window is 2024-09-21 to 2026-09-21. `--months 24` counts back from the last
finalised day, so a re-run on a later date covers a different window and prints
slightly different figures.

What to tell the user: intermittent fasting has essentially no Wikipedia footprint
in Czech or Polish. If the product idea is real, the signal has to come from
somewhere else — keyword volume, app-store search — and a Wikipedia-based
comparison is the wrong instrument. That is a more useful answer than a chart of
two noisy lines.

---

## 2. "We are thinking about adding an astronomy course. Is interest growing in the Ukrainian Wikipedia, and how reliable is this growth?"

A subject area, not one article — so use a basket:

```bash
go run ./cmd/wikitrends run --topic "astronomy" --qid Q333 \
  --basket-topics "solar system,galaxy,black hole" \
  --wikis uk --months 30 --out runs/astro
```

```
Topic basket: Solar System (Q544), galaxy (Q318), black hole (Q589)

### uk.wikipedia — reliability B
Interest is declining: -27%/yr in share of the wiki's pageviews (95% CI -30% to
-24%). Aligned year-over-year: -29%. Baseline 155 views/day (91.0 per million
pageviews). Reliability B. Strongly seasonal: February runs about 3.1x the July
level (pattern repeats across years, consistency 0.93) and matches an academic
calendar.
```

Three things the basket bought: 155 views/day instead of 48 for the bare
*Astronomy* article, which moved the grade from C to B; a seasonality estimate
that is actually usable; and a measurement of the subject rather than of one
encyclopaedia entry.

The single-article version shows why the edition's own trend matters:

```
- share of wiki traffic: -39%/yr      <- the topic, normalised
- absolute views:        -51%/yr      <- what a naive chart would show
- context: uk.wikipedia overall traffic -21%/yr
```

Roughly half the apparent collapse is Ukrainian Wikipedia losing traffic, not
astronomy losing readers. Quoting −51 %/yr without that context would be correct
and misleading.

**Answer to the user:** interest is not growing, it is declining, and the decline
is real but roughly half of it is platform-wide. The reliable finding is the
seasonality: demand repeats a 3× February-versus-July swing across every year in
the window, which is a launch-timing fact worth more than the trend.

Verified by hand against the raw data — September and February peak and July
troughs in *every* year:

```
per-year monthly medians (Астрономія):
  2023:     .     .     .     .     .     .     .     .   210   116   122   120
  2024:   109    97    65    76    60    32    18    24   138    55    56    44
  2025:    42    44    32    32    25    12    10    11    47    19    19    20
  2026:    14    14    13    14    20     9     8    12    30     .     .     .
```

---

## 3. "We are creating a language learning app. Compare interest in learning English across our selected editions; which audiences should we research next and why?"

```bash
go run ./cmd/wikitrends scan --topic "English language" \
  --wikis pl,cs,uk,de,es,tr,id,vi,ro,hu --months 40 --out runs/english
```

```
| # | edition | score | per million | share %/yr | abs %/yr | wiki %/yr | YoY | adj p | grade |
|---|---------|-------|-------------|------------|----------|-----------|------|--------|-------|
| 1 | vi      | 0.95  | 194.74      | -9%        | -22%     | -15%      | +1%  | <0.001 | A     |
| 2 | tr      | 0.64  | 59.64       | -12%       | -23%     | -13%      | +2%  | <0.001 | A     |
| 3 | uk      | 0.63  | 107.20      | -23%       | -38%     | -20%      | -15% | <0.001 | B     |
| 4 | pl      | 0.60  | 43.33       | -12%       | -18%     | -7%       | -10% | <0.001 | A     |
...
| 7 | id      | 0.55  | 154.91      | -38%       | -52%     | -24%      | -19% | <0.001 | B     |
```

Read it in this order:

1. **Nothing is growing.** The tool says so explicitly, and that note matters more
   than the ranking: the order reflects existing audience, not momentum.
2. **Vietnamese and Indonesian are three to four times more interested than the
   European editions** — 195 and 155 per million versus 31–62. For a language
   learning product that is the headline, not the trend.
3. **Vietnamese is at an inflection.** The full-window trend is −9 %/yr but the
   most recent year is **+1 %**. The verdict says so directly: *"the most recent
   year moves the other way, so treat this as an inflection rather than a steady
   trend."* Vietnamese is the only edition combining the top attention share with a
   recent-year figure that has stopped falling.
4. **Turkish shows the same shape** at a third of the volume: −12 %/yr trend,
   +2 % year over year, grade A.
5. Every edition's decline is smaller in share than in absolute terms, because
   every edition is losing traffic. That is the platform, not the topic.

**Answer to the user:** research Vietnamese first and Turkish second. Both combine
high existing attention with a recent year that has stopped declining, both at
grade A. Indonesian has the second-highest attention share but the steepest decline
(−38 %/yr in share, −19 % year over year) and should be treated as a market that
is moving away from this behaviour, not toward it.

Then the caveats that change what you do with that ranking:

- Wikipedia measures curiosity. The next step is keyword volume and cost-per-click
  for English-learning terms in Vietnamese and Turkish — the cheapest test of
  whether this curiosity has commercial intent.
- These are language editions, not markets. Vietnamese maps closely to one country;
  Turkish largely does; the European editions understate their audiences because
  many of those readers use English Wikipedia instead.
- Willingness to pay across these markets differs by an order of magnitude and
  Wikipedia knows nothing about it.

### Following up without refetching

```bash
# "we care about audience size more than growth"
go run ./cmd/wikitrends analyze --in runs/english --weights level=0.7,growth=0.15,reliability=0.15

# "show me raw pageviews instead"
go run ./cmd/wikitrends analyze --in runs/english --metric absolute

# "add German and Portuguese" — the ten cached editions are not refetched
go run ./cmd/wikitrends run --topic "English language" \
  --wikis pl,cs,uk,de,es,tr,id,vi,ro,hu,pt --months 40 --out runs/english
```
