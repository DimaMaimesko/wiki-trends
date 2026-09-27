package wikiapi

import (
	"context"
	"fmt"
	"time"
)

const restBase = "https://wikimedia.org/api/rest_v1/metrics/pageviews"

// PageviewsStart is the first day the per-article pageviews API covers. Requests
// before it are not errors but they return nothing, so the skill clamps windows
// and says so instead of silently producing a shorter series than asked for.
var PageviewsStart = time.Date(2015, 7, 1, 0, 0, 0, 0, time.UTC)

type pvResponse struct {
	Items []struct {
		Timestamp string  `json:"timestamp"`
		Views     float64 `json:"views"`
	} `json:"items"`
}

// freshness decides how long a cached response stays valid.
//
// Pageview counts for a day are finalised within a couple of days and then never
// change, so any chunk that ends well in the past is immutable and cached
// forever. Only chunks touching the recent tail need re-checking, and Wikimedia
// itself lags a day or two, which is why the tail is treated as 45 days wide
// rather than a handful.
func freshness(end time.Time) time.Duration {
	if time.Since(end) > 45*24*time.Hour {
		return 0 // immutable
	}
	return 6 * time.Hour
}

// DailySeries is a date-keyed map of observed values. Absence is meaningful and
// is resolved by the caller (see Reconcile), never by filling in zeros here.
type DailySeries map[string]float64

// ArticleViews returns daily pageviews for one article.
//
// Requests are split by calendar year. That costs a few extra HTTP calls on a
// first run and repays it immediately: when the user follows up with "make that
// three years" or "compare against last year too", every already-fetched year is
// a cache hit, so only the new slice is downloaded. Windows are also the thing
// users change most often, and a single-request-per-window scheme would
// invalidate the whole cache on every change.
func (c *Client) ArticleViews(ctx context.Context, project, title, access, agent string, start, end time.Time) (DailySeries, error) {
	out := DailySeries{}
	found := false
	var firstErr error
	for _, ch := range yearChunks(start, end) {
		u := fmt.Sprintf("%s/per-article/%s/%s/%s/%s/daily/%s/%s",
			restBase, project, access, agent, encodeTitle(title),
			ch.from.Format("20060102"), ch.to.Format("20060102"))
		var r pvResponse
		err := c.getJSON(ctx, u, freshness(ch.to), &r)
		if err == ErrNoData {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		found = true
		for _, it := range r.Items {
			if len(it.Timestamp) < 8 {
				continue
			}
			ts := it.Timestamp[:4] + "-" + it.Timestamp[4:6] + "-" + it.Timestamp[6:8]
			out[ts] += it.Views
		}
	}
	if !found {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, ErrNoData
	}
	return out, firstErr
}

// ProjectViews returns total daily pageviews for a whole wiki under the same
// access/agent filters.
//
// This is the denominator that makes every other number trustworthy. Absolute
// pageviews for one article move for reasons that have nothing to do with the
// topic: the wiki's overall traffic grows or shrinks, Wikimedia reclassifies
// traffic as automated, search engines and AI assistants change how many people
// click through. Dividing by the wiki's own total absorbs all of it, and turns
// "views" into "share of this audience's attention", which is both comparable
// across languages and resistant to platform-wide shocks.
func (c *Client) ProjectViews(ctx context.Context, project, access, agent string, start, end time.Time) (DailySeries, error) {
	out := DailySeries{}
	found := false
	for _, ch := range yearChunks(start, end) {
		u := fmt.Sprintf("%s/aggregate/%s/%s/%s/daily/%s/%s",
			restBase, project, access, agent,
			ch.from.Format("2006010200"), ch.to.Format("2006010200"))
		var r pvResponse
		err := c.getJSON(ctx, u, freshness(ch.to), &r)
		if err == ErrNoData {
			continue
		}
		if err != nil {
			return nil, err
		}
		found = true
		for _, it := range r.Items {
			if len(it.Timestamp) < 8 {
				continue
			}
			ts := it.Timestamp[:4] + "-" + it.Timestamp[4:6] + "-" + it.Timestamp[6:8]
			out[ts] += it.Views
		}
	}
	if !found {
		return nil, ErrNoData
	}
	return out, nil
}

type chunk struct{ from, to time.Time }

func yearChunks(start, end time.Time) []chunk {
	var out []chunk
	for y := start.Year(); y <= end.Year(); y++ {
		from := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(y, 12, 31, 0, 0, 0, 0, time.UTC)
		if from.Before(start) {
			from = start
		}
		if to.After(end) {
			to = end
		}
		if to.Before(from) {
			continue
		}
		out = append(out, chunk{from, to})
	}
	return out
}
