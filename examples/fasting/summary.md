# intermittent fasting

_a diet that cycles between a period of fasting and non-fasting_ (Q1666254)

Window 2024-09-21 → 2026-09-21 · metric `share` · agent=user access=all-access

## Answer per language edition

### cs.wikipedia — reliability D

Not measurable: only 9.2 views/day on average (floor is 10): day-to-day counts at this level are dominated by noise, and a % change is not measurable. Treat this edition as unmeasured, not as flat — the point estimate below is reported for context only and must not be quoted as a growth rate.

- article: Přerušovaný půst (created 2020-10-28)
- level: 9 views/day mean, 3 last 90d, 1.76 per million pageviews last 90d
- share of wiki traffic: -42%/yr (95% CI -49% to -35%, HAC lag 6, n=731), direction falling; Theil-Sen -43%/yr; Mann-Kendall p=<0.001 (adj <0.001); YoY -50%
- absolute views: -50%/yr (95% CI -55% to -44%, HAC lag 6, n=731), direction falling; Theil-Sen -48%/yr; Mann-Kendall p=<0.001 (adj <0.001); YoY -55%
- context: cs.wikipedia overall traffic -12%/yr
- seasonality (confidence low): annual peak/trough ratio 1.91, year-to-year shape consistency 0.38
- events: 6 spike days, 15% of window views inside spikes; largest: 2025-04-14 (508 views vs 14 baseline), 2025-08-15 (164 views vs 6 baseline), 2025-09-24 (77 views vs 5 baseline), 2024-10-17 (110 views vs 14 baseline)
- level shift: +75% around 2025-12-23 — check article history before reading as demand
- coverage: 731 days, 100% complete, 0 days before article creation, 0 gaps
- **blockers**:
  - only 9.2 views/day on average (floor is 10): day-to-day counts at this level are dominated by noise, and a % change is not measurable
- reliability notes (score 0.00):
  - low volume (9 views/day): percentage changes are unstable at this scale
  - a persistent 75% level shift around 2025-12-23 remains after trend and seasonality: check the article history for a page move, merge or redirect change before reading this as demand

### pl.wikipedia — reliability D

Not measurable: only 8.0 views/day on average (floor is 10): day-to-day counts at this level are dominated by noise, and a % change is not measurable. Treat this edition as unmeasured, not as flat — the point estimate below is reported for context only and must not be quoted as a growth rate.

- article: Głodówka lecznicza (created 2004-08-06)
- level: 8 views/day mean, 5 last 90d, 0.95 per million pageviews last 90d
- share of wiki traffic: -30%/yr (95% CI -36% to -23%, HAC lag 6, n=731), direction falling; Theil-Sen -30%/yr; Mann-Kendall p=<0.001 (adj <0.001); YoY -31%
- absolute views: -37%/yr (95% CI -43% to -30%, HAC lag 6, n=731), direction falling; Theil-Sen -36%/yr; Mann-Kendall p=<0.001 (adj <0.001); YoY -38%
- context: pl.wikipedia overall traffic -9%/yr
- seasonality: annual pattern not estimable (2.0 years of data)
- level shift: +44% around 2024-12-29 — check article history before reading as demand
- coverage: 731 days, 100% complete, 0 days before article creation, 0 gaps
- **blockers**:
  - only 8.0 views/day on average (floor is 10): day-to-day counts at this level are dominated by noise, and a % change is not measurable
- reliability notes (score 0.00):
  - low volume (8 views/day): percentage changes are unstable at this scale
  - a persistent 44% level shift around 2024-12-29 remains after trend and seasonality: check the article history for a page move, merge or redirect change before reading this as demand

## Shortlist

Weights: level 0.45, growth 0.35, reliability 0.20 (change with `--weights`)

| # | edition | score | per million | share %/yr | abs %/yr | wiki %/yr | YoY | adj p | grade | why |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | cs | — | 1.76 | -42% | -50% | -12% | -50% | <0.001 | D | not rankable: only 9.2 views/day on average (floor is 10): day-to-day counts at this level are dominated by noise, and a % change is not measurable |
| 2 | pl | — | 0.95 | -30% | -37% | -9% | -31% | <0.001 | D | not rankable: only 8.0 views/day on average (floor is 10): day-to-day counts at this level are dominated by noise, and a % change is not measurable |

- The opportunity score mixes current attention share, growth and reliability using the weights shown. It is a shortlisting aid, not a forecast.
- Scores are relative within this run only: adding or removing a language edition changes every score.
- Wikipedia attention is not willingness to pay. Use this to choose what to validate next, not what to build.

## Assumptions and limitations

- Wikipedia pageviews measure curiosity, not purchase intent. A rising article is a reason to run a landing-page or ad test, not evidence of a market.
- Counts use agent=user and access=all-access. Wikimedia's bot filtering is best-effort and its accuracy has changed over time, which can create step changes unrelated to demand.
- Views are counted per article title. Page moves, merges and redirect changes can shift traffic between titles without any change in interest.
- Each language edition is read by a different and not strictly national audience: many speakers of smaller languages read the English, Russian or German editions instead, which understates their home edition.
- Share of a wiki's total pageviews is the default metric. It controls for wiki-wide traffic changes, but it falls when the wiki grows for unrelated reasons.
- 2 language editions were tested, so p-values are Benjamini-Hochberg adjusted for multiple comparisons.
- Only the canonical article title was counted. Traffic arriving through redirects is excluded; re-run with --include-redirects to include it.
