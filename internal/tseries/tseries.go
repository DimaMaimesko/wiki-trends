// Package tseries turns sparse API responses into dense, calendar-aligned
// series and holds the date arithmetic the rest of the skill relies on.
package tseries

import (
	"fmt"
	"math"
	"time"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
)

const DateFmt = "2006-01-02"

func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(DateFmt, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad date %q (want YYYY-MM-DD): %w", s, err)
	}
	return t.UTC(), nil
}

func MustParse(s string) time.Time {
	t, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return t
}

// Days returns the inclusive number of days between two dates.
func Days(start, end time.Time) int { return int(end.Sub(start).Hours()/24) + 1 }

// Reconcile converts the sparse maps returned by the pageviews API into dense
// daily arrays, deciding for every absent day what that absence means.
//
// This is the single most consequential piece of data handling in the skill, and
// the easiest to get silently wrong. The Wikimedia API simply omits days, and
// omission carries three completely different meanings:
//
//	before the article existed   -> not applicable. Counting it as 0 makes every
//	                                young article look like explosive growth,
//	                                because the fit sees demand rising out of
//	                                nothing.
//	the article got no views     -> a true zero. The API omits these for
//	                                low-traffic articles. Dropping them instead
//	                                would bias the mean upward and hide exactly
//	                                the "too small to matter" signal a founder
//	                                needs.
//	a pipeline gap               -> unknown. Treating it as 0 invents a crash.
//
// The three are told apart by cross-checking the wiki's own total pageviews for
// the same day: if the project reported traffic, the measurement pipeline was
// running, so an absent article row is a real zero. If the project is silent
// too, nothing was measured and the day stays unknown (NaN).
func Reconcile(start, end time.Time, article, project map[string]float64, createdAt string) (views, proj []float64, cov model.Coverage) {
	n := Days(start, end)
	views = make([]float64, n)
	proj = make([]float64, n)
	var created time.Time
	hasCreated := false
	if createdAt != "" {
		if t, err := ParseDate(createdAt); err == nil {
			created, hasCreated = t, true
		}
	}
	cov.Days = n
	for i := 0; i < n; i++ {
		d := start.AddDate(0, 0, i)
		key := d.Format(DateFmt)

		if pv, ok := project[key]; ok {
			proj[i] = pv
		} else {
			proj[i] = math.NaN()
		}

		switch {
		case hasCreated && d.Before(created):
			views[i] = math.NaN()
			cov.BeforeCreate++
		default:
			if v, ok := article[key]; ok {
				views[i] = v
				cov.Observed++
			} else if p, ok := project[key]; ok && p > 0 {
				views[i] = 0
				cov.ZeroFilled++
			} else {
				views[i] = math.NaN()
				cov.Gaps++
			}
		}
	}
	if eligible := cov.Days - cov.BeforeCreate; eligible > 0 {
		cov.Completeness = float64(cov.Observed+cov.ZeroFilled) / float64(eligible)
	}
	return
}

// SharePerMillion converts an absolute series into the topic's share of its
// wiki's total attention, expressed per million pageviews.
//
// This is the skill's default metric. Absolute pageviews answer "how many
// people", which matters for market sizing, but they are not comparable across
// language editions (German Wikipedia serves roughly two orders of magnitude
// more traffic than Czech) and they drift with platform-wide changes that have
// nothing to do with the topic. Share answers "how much of this audience cares",
// which is the question behind "should we launch in this language".
func SharePerMillion(views, proj []float64) []float64 {
	out := make([]float64, len(views))
	for i := range views {
		if math.IsNaN(views[i]) || math.IsNaN(proj[i]) || proj[i] <= 0 {
			out[i] = math.NaN()
			continue
		}
		out[i] = views[i] / proj[i] * 1e6
	}
	return out
}

// Add accumulates b into a elementwise, treating NaN as "nothing to add" unless
// both are NaN. Used to sum a basket of articles into one topic series: a course
// subject is rarely one Wikipedia article.
func Add(a, b []float64) []float64 {
	if a == nil {
		return append([]float64(nil), b...)
	}
	for i := range a {
		switch {
		case math.IsNaN(a[i]) && math.IsNaN(b[i]):
		case math.IsNaN(a[i]):
			a[i] = b[i]
		case math.IsNaN(b[i]):
		default:
			a[i] += b[i]
		}
	}
	return a
}

// Dates materialises the calendar labels for a dense series.
func Dates(start time.Time, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = start.AddDate(0, 0, i).Format(DateFmt)
	}
	return out
}

// ClampWindow limits a requested window to what the pageviews API can answer and
// reports what it changed, so the caller can tell the user instead of quietly
// returning a shorter series than they asked for.
func ClampWindow(start, end, apiStart, today time.Time) (time.Time, time.Time, []string) {
	var notes []string
	if start.Before(apiStart) {
		notes = append(notes, fmt.Sprintf("window start moved from %s to %s: the pageviews API has no per-article data before then",
			start.Format(DateFmt), apiStart.Format(DateFmt)))
		start = apiStart
	}
	// Wikimedia publishes with a lag; the last day or two is usually incomplete
	// and would read as a sudden collapse.
	latest := today.AddDate(0, 0, -2)
	if end.After(latest) {
		notes = append(notes, fmt.Sprintf("window end moved from %s to %s: the most recent days are not yet final",
			end.Format(DateFmt), latest.Format(DateFmt)))
		end = latest
	}
	return start, end, notes
}
