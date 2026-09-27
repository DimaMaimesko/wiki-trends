# Choosing language editions

Wikipedia language editions are the unit of measurement here, and they are a
*proxy* for an audience — an imperfect one in ways that change the conclusion.

## The three things that are not the same

- **A language edition** is a website. Its readers are whoever reads that
  language's Wikipedia.
- **A language community** is everyone who speaks the language, many of whom read a
  different edition.
- **A market** is a country or region with a currency, payment rails, app stores
  and a price level.

A shortlist built from editions and presented as a market ranking is wrong in a way
that is invisible from the data. Always name which one you are talking about.

## Substitution: the biggest distortion

Speakers of smaller languages routinely read larger editions instead of their own,
which systematically understates the home edition. The direction of substitution is
usually predictable:

| home edition | common substitutes | consequence |
|---|---|---|
| Ukrainian, Belarusian, Kazakh | Russian, English | home edition understated |
| Dutch, Nordic languages, Greek | English | home edition strongly understated |
| Czech, Slovak, Slovenian, Croatian | English, German | moderately understated |
| Catalan, Galician, Basque | Spanish | home edition understated |
| Hindi and other Indian languages | English | severely understated |
| Arabic, Persian | English, French | varies by country |

For technical, scientific and business topics, substitution towards English is
strongest — exactly the topics an educational product often covers. For local
culture, law, geography and language learning *of* English, the home edition is
much more representative.

**Practical check**: run the same topic in the home edition and the likely
substitute. If the substitute's per-million share is much higher, substitution is
in play and the home number is a floor, not an estimate.

## Multi-country editions

Spanish, Portuguese, Arabic, French, English and German each serve many countries
with very different price levels. A high share for `es` says nothing about whether
the readers are in Spain, Mexico or Argentina. The pageviews API does not break
down by country, so if that distinction matters to the decision, say it is
unanswerable here and recommend a source that can answer it.

## Picking a comparison set

Good sets share something that makes the comparison meaningful:

- **Similar size and region** — `cs, sk, hu, ro, pl` for Central Europe.
- **A localisation decision already scoped** — whatever the user's candidate
  locales are; do not silently add others.
- **A deliberate contrast** — one large edition as a reference point makes the
  small ones readable on a log axis.

Bad sets: everything you can think of. Every added edition tightens the
Benjamini-Hochberg correction and makes real effects harder to detect, and it
dilutes the opportunity score, which is normalised within the run.

Six to twelve editions is a good scan. Beyond that, scan in themed batches.

## Reading per-million shares across editions

Per-million share is comparable across editions but not perfectly: editions differ
in how much of their traffic is reference lookups versus reading, and in how
complete their coverage of a subject is. A topic with a good, well-linked article in
one edition and a stub in another will show a share difference that is partly
editorial, not audience interest.

**Check the article, not only the number**, when a difference is surprising. The
HTML report links every article for exactly this.

## Codes

Standard language codes as used in the project name (`pl` → `pl.wikipedia`).
Multi-part codes work too (`zh-yue`, `be-tarask`, `pt-br` where an edition exists).
A code with no edition simply returns no data, which is reported as such.
