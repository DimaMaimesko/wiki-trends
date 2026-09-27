# Interpretation

Turning the numbers into a recommendation a founder can act on, without
overstating what Wikipedia can support.

## What this data can and cannot answer

| question | answerable? | with what |
|---|---|---|
| Is public curiosity about X changing? | yes | share trend + aligned YoY |
| Which of these audiences is most curious about X? | yes | share per million, compared |
| When in the year is curiosity highest? | yes, with ≥2–3 years | annual index + consistency |
| Is X bigger than Y for this audience? | yes, if both are resolvable concepts | share levels, same window |
| Will people pay for X? | **no** | nothing here |
| How large is the market for X? | **no** | pageviews are not users |
| Is there competition in X? | **no** | out of scope |
| Did our campaign work? | weakly | a spike coincident with the campaign is suggestive, not proof |

The honest framing of every positive result is: *this audience shows measurable
and growing curiosity, which makes it a good candidate for the next validation
step.* Never: *there is demand here.*

## Decision rules

These are defaults to reason from, not a formula to apply blindly.

### Should we develop this topic for this audience?

| condition | read as |
|---|---|
| grade A/B, share rising, ≥ 2 years, not spike-dependent | strongest signal this data offers — validate next |
| grade A/B, share flat, high absolute level | established interest, no momentum — fine for a proven format, weak for a bet |
| share rising but absolute falling | topic is gaining share of a shrinking audience — check whether the audience is going somewhere you can follow |
| spike-dependent growth | an event, not a market — check whether the event repeats (annual, news cycle, seasonal) |
| grade C | directional only; widen the window or use a basket before deciding |
| grade D / blockers | unmeasured. Recommend a different measurement, not a different decision |
| no article in that edition | coverage gap; may indicate the audience reads another edition |

### Which language to launch in?

Rank on `share per million` (current attention) first, growth second, and read the
reliability column before believing either. Then apply the things Wikipedia does
not know, explicitly:

1. **Language ≠ market.** Check whether that audience reads a larger edition
   instead (see `editions.md`).
2. **Localisation cost** differs by script, right-to-left layout, and voice/TTS
   availability.
3. **Willingness to pay** varies by an order of magnitude across the editions a
   scan will hand you.
4. **Your team's languages** determine what you can actually support.

Say which of these you are ignoring. A shortlist that silently assumes equal ARPU
is a shortlist that recommends the poorest market.

### How much growth is "a lot"?

On share of edition pageviews, for a topic with adequate volume:

| share growth | reading |
|---|---|
| > +30 %/yr | strong, verify it is not spike-driven or article-quality-driven |
| +10 to +30 %/yr | meaningful |
| −10 to +10 %/yr | flat; do not describe as growth even if the sign is positive |
| < −10 %/yr | declining |

Always check that the confidence interval agrees with the label. A point estimate
of +25 %/yr with a CI of −5 % to +60 % is not "+25 % growth"; it is "unclear,
possibly growing".

## Wording templates

Use these shapes. They encode the window, the metric and the uncertainty, which is
what makes them safe to forward.

**Strong result**

> Interest in {topic} on {edition} is rising: **+{g} %/yr** in share of that
> edition's pageviews over {window} (95 % CI +{lo} % to +{hi} %, adjusted
> p {p}). Aligned year-over-year: +{yoy} %. Current baseline {n} views/day
> ({pm} per million). Reliability **{grade}**. Recommended next step:
> {validation step}.

**Event-driven**

> The apparent **+{g} %/yr** for {topic} on {edition} is event-driven: {share} % of
> all views in the window fall inside spikes, the largest around {date}. Excluding
> them the trend is **{g2} %/yr**. Treat this as attention, not a market, unless
> the event recurs.

**Edition decline vs topic decline**

> {topic} lost {abs} %/yr in absolute views on {edition}, but {edition} itself lost
> {wiki} %/yr over the same window. In share of that edition's traffic the topic
> moved {share} %/yr — so this is mostly the platform, not the topic.

**Not measurable**

> Not measurable on {edition}: the article averages {n} views/day, below the level
> at which a percentage change can be distinguished from noise. This is **not**
> evidence of low interest — it means this source cannot answer the question for
> this audience. Options: a basket of related articles, a longer window, or a
> different source.

**Coverage gap**

> {edition} has no article for {topic}. That is a content gap, not zero interest:
> readers there may use {candidate}, or read about it in another language. The
> nearest available article is {candidate}, which covers {difference} — measuring
> it answers a slightly different question.

**Inflection**

> Over the full {window} window, share fell {g} %/yr, but the most recent year is
> {yoy} % against the year before. The series is not monotonic: it declined and
> then stabilised. Quote the recent year for where it stands now, and the
> full-window trend only for how it got there.

**Seasonality as timing advice**

> Demand is strongly seasonal and repeats: {peak} runs about {amp}× the {trough}
> level, consistently across {years} years. For a launch, that argues for
> {peak_minus_1} rather than {trough}.

## Recommending next steps

Wikipedia narrows the field; it does not close it. Good next steps to recommend,
roughly in increasing cost:

1. Keyword volume and cost-per-click for the topic in that language — the cheapest
   proxy for commercial intent, and it directly tests the gap between curiosity and
   willingness to pay.
2. A landing page with a waitlist, in that language, in the seasonal peak.
3. A small paid-acquisition test against the same audience.
4. App-store search volume for the category in that locale.
5. Talking to ten people in that audience.

Tie the recommendation to what the data actually showed: if the finding was
seasonal, recommend timing; if it was a coverage gap, recommend checking substitute
editions; if it was low volume, recommend a different source.

## Follow-up conversations

Users refine assumptions. Treat every refinement as a re-analysis, not a new
project, and keep the same run directory so the numbers stay comparable.

When a user pushes back on a conclusion, the reliability reasons are the right
place to look: they name the specific check that produced the caveat. If they
disagree with the check — "I don't care about the spike, that event happens every
January" — that is useful information. Re-run with different assumptions and say
what changed.

When a user asks for a number you cannot support, say which specific check blocks
it and what would unblock it (a longer window, a basket, a different source). That
is more useful than either refusing or complying.
