# Confounders

Things that manufacture a fake trend in Wikipedia pageview data, roughly in order
of how often they matter. For each: how it shows up, how `wikitrends` detects it,
and what to do.

## 1. Platform-wide traffic change

**The dominant confounder right now.** Human pageviews have been falling across
Wikipedia editions, driven by search engines and AI assistants answering questions
without a click-through, and by improved classification of automated traffic. The
effect is large enough that **almost any topic looks like it is declining in
absolute terms.**

- *Shows up as*: every article in every edition declining at a similar rate.
- *Detected by*: the `share` metric (divides it out) and the reported `wiki %/yr`
  (names it).
- *Do*: report share, and state the edition's own trend whenever you quote
  absolute numbers.

## 2. Bot and crawler reclassification

Wikimedia's `user` vs `automated` split is best-effort, and its accuracy has
changed over time. A change in classification moves measured "human" traffic
without any change in human behaviour.

- *Shows up as*: a step change on a specific date, often across many articles at
  once.
- *Detected by*: level-shift detection; and by the same shift appearing in the
  project denominator.
- *Do*: check whether the shift date is shared across editions or articles. If it
  is, it is measurement. Never use `--agent all-agents` for trend work.

## 3. Seasonality, especially the academic calendar

Education-adjacent topics swing several-fold between term time and July/August. A
window that starts and ends in different seasons reads the calendar as a trend.

- *Shows up as*: large "growth" in any window that is not a whole number of years.
- *Detected by*: seasonal adjustment before fitting; aligned year-over-year; the
  annual index with its cross-year consistency score.
- *Do*: prefer whole-year windows. Quote aligned year-over-year to lay audiences.
  Treat a detected academic pattern as a launch-timing finding.

## 4. News and event spikes

One event can carry a year of apparent growth, particularly for health, politics
and celebrity-adjacent topics.

- *Shows up as*: high growth with most views concentrated in a few days.
- *Detected by*: spike detection; `spike_view_share`; the trend recomputed with
  spikes masked; Theil–Sen disagreeing with the log-linear fit.
- *Do*: if `spike_dependent`, report it as event-driven. Identify the event
  (article history, news for that date) or say the cause is unidentified — do not
  invent one.

## 5. Title-level accounting

Views are counted per title, not per concept. Page moves, merges and redirect
retargets move traffic between titles with no change in interest.

- *Shows up as*: a step change, sometimes a near-total collapse or doubling.
- *Detected by*: level-shift detection; the article's revision history.
- *Do*: check the history at the shift date. Consider `--include-redirects`, and
  say so when you use it — it captures alias traffic but can also fold in a merged
  former article.

## 6. Article creation and stub growth

A new article starts at zero because it did not exist. A stub that becomes a good
article attracts more search traffic without more underlying interest.

- *Shows up as*: explosive early growth.
- *Detected by*: creation dates are fetched; pre-creation days are `null`, not 0;
  an article younger than half the window is a blocker.
- *Do*: shorten the window to the article's lifetime, or pick an older article.

## 7. Low volume

Below roughly 10 views/day, day-to-day counts are dominated by noise and a
percentage change is not measurable. Below ~50, it is unstable.

- *Shows up as*: wide confidence intervals; OLS and Theil–Sen disagreeing;
  spectacular percentages.
- *Detected by*: the volume blocker and penalty.
- *Do*: use a basket of related concepts, widen the window, or report "not
  measurable". Never report it as flat.

## 8. One article is not one topic

The article *Astronomy* is an encyclopaedia entry about the discipline. Interest in
astronomy as a subject lives across dozens of articles.

- *Shows up as*: a low-volume, noisy series that under-represents real interest.
- *Detected by*: nothing automatic — this is your editorial judgement.
- *Do*: use `--basket-topics`. List the members in what you report.

## 9. Language edition ≠ country ≠ language community

Speakers of smaller languages frequently read the English, Russian or German
edition instead of their own, so a home edition understates its audience. Large
editions serve many countries (Spanish, Portuguese, Arabic, French).

- *Shows up as*: implausibly low per-million figures for a language whose speakers
  you know care about the topic.
- *Detected by*: nothing automatic.
- *Do*: say so when recommending a locale. Consider comparing the same topic in
  the likely substitute edition.

## 10. Disambiguation and homonyms

A title that means two things measures both. A disambiguation page measures
neither.

- *Shows up as*: a level that makes no sense for the topic.
- *Detected by*: resolution through Wikidata, and the candidate list from
  `resolve`.
- *Do*: read the Wikidata description before accepting a candidate. Ask the user
  when ambiguous.

## 11. Front-page and campaign exposure

"Did you know", "On this day", a featured article slot, or a Wikipedia editing
campaign can move an article's traffic substantially for days.

- *Shows up as*: a spike with no matching external news.
- *Detected by*: spike detection only; the cause needs the article's talk page or
  the edition's front-page archive.
- *Do*: treat as an internal Wikipedia event, not demand.

## 12. Mobile/desktop and app mix

The device mix shifted over the decade the API covers, and mobile app traffic is
counted differently from mobile web.

- *Shows up as*: slow drift over multi-year windows.
- *Detected by*: comparing `--access desktop` with `--access mobile-web`.
- *Do*: keep `all-access` for trend work. Split only to investigate a suspected
  artefact.

## 13. Multiple comparisons

Scanning many editions guarantees some will look significant by chance.

- *Detected by*: Benjamini–Hochberg adjustment across the editions in a run.
- *Do*: quote the **adjusted** p-value. Note that the adjustment depends on which
  editions were in the run.

## The universal check

Run a **control topic** over the same window and editions — something stable and
unrelated (a country, a common household object). If the control moves the same
way, you are measuring the platform, not the topic.
