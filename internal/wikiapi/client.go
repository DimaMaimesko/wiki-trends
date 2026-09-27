// Package wikiapi is the only part of the skill that talks to Wikimedia.
//
// It exists to make the rest of the code deterministic and offline-friendly: a
// disk cache keyed by request URL, chunking that maximises cache reuse, polite
// rate limiting, and a clear separation between "the API said there is no data"
// and "the request failed".
package wikiapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultUserAgent follows Wikimedia's User-Agent policy, which asks automated
// clients to identify themselves and provide a contact route. Requests without a
// descriptive agent are liable to be throttled or blocked, so this is not
// cosmetic. Override with WIKITRENDS_UA.
const DefaultUserAgent = "wikitrends/1.0 (wiki-trends skill; +https://github.com/dmytromaimesko/wiki-trends)"

type Client struct {
	HTTP      *http.Client
	UserAgent string
	Cache     *Cache
	limiter   chan struct{}
	ticker    *time.Ticker

	// Offline refuses any request that is not already cached, and serves cached
	// entries even after their TTL has expired. It makes report regeneration and
	// test runs fully hermetic.
	Offline bool

	Stats struct {
		Hits, Misses, Requests, Retries int
	}
}

type Option func(*Client)

func WithOffline(v bool) Option { return func(c *Client) { c.Offline = v } }

func New(cache *Cache, opts ...Option) *Client {
	ua := os.Getenv("WIKITRENDS_UA")
	if ua == "" {
		ua = DefaultUserAgent
	}
	c := &Client{
		HTTP:      &http.Client{Timeout: 45 * time.Second},
		UserAgent: ua,
		Cache:     cache,
		limiter:   make(chan struct{}, 4),
		// ~8 requests/second sustained. Wikimedia tolerates far more, but a
		// research tool has no reason to push, and the cache means a rerun costs
		// nothing anyway.
		ticker: time.NewTicker(125 * time.Millisecond),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) Close() {
	if c.ticker != nil {
		c.ticker.Stop()
	}
}

// ErrNoData means the endpoint answered correctly that it holds nothing for this
// request: an article that does not exist, or a date range outside coverage.
// Callers turn it into a finding, never into a failure.
var ErrNoData = fmt.Errorf("wikiapi: no data for request")

// getJSON fetches and decodes a URL, consulting the cache first. ttl == 0 means
// the response is immutable and cached forever.
func (c *Client) getJSON(ctx context.Context, u string, ttl time.Duration, out any) error {
	if c.Cache != nil {
		// Offline means "use whatever is cached". An expired entry is still the
		// best answer available, and refusing it made the recorded test fixtures
		// fail one actionTTL after they were recorded.
		cacheTTL := ttl
		if c.Offline {
			cacheTTL = 0
		}
		if b, ok := c.Cache.Get(u, cacheTTL); ok {
			c.Stats.Hits++
			if len(b) == 0 {
				return ErrNoData
			}
			return json.Unmarshal(b, out)
		}
	}
	c.Stats.Misses++
	if c.Offline {
		return fmt.Errorf("offline mode: %s not in cache", u)
	}

	c.limiter <- struct{}{}
	defer func() { <-c.limiter }()

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			c.Stats.Retries++
			// exponential backoff with a floor; 429 and 5xx are the only retried
			// conditions, and both benefit from waiting rather than hammering.
			d := time.Duration(1<<attempt) * 400 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.ticker.C:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", c.UserAgent)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Api-User-Agent", c.UserAgent) // required by the Action API
		c.Stats.Requests++

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}

		switch {
		case resp.StatusCode == http.StatusNotFound:
			// A 404 from the pageviews API is a statement about the data, not a
			// transport failure. Cache it as an empty body so repeated scans over
			// many wikis do not re-ask for articles that do not exist.
			if c.Cache != nil {
				c.Cache.Put(u, nil)
			}
			return ErrNoData
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("%s: %s", u, resp.Status)
			continue
		case resp.StatusCode != http.StatusOK:
			return fmt.Errorf("%s: %s: %s", u, resp.Status, truncate(string(body), 300))
		}
		if c.Cache != nil {
			c.Cache.Put(u, body)
		}
		return json.Unmarshal(body, out)
	}
	return fmt.Errorf("request failed after retries: %w", lastErr)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// encodeTitle prepares an article title for a pageviews path segment.
//
// Two traps live here. Wikipedia titles use underscores for spaces, and the
// pageviews API keys on the exact stored title, so "Intermittent fasting" and
// "Intermittent_fasting" are not interchangeable. And url.PathEscape leaves "/"
// alone, which silently splits titles like "Faith/Belief" across path segments
// and yields a confusing 404.
func encodeTitle(t string) string {
	t = strings.ReplaceAll(strings.TrimSpace(t), " ", "_")
	return strings.ReplaceAll(url.PathEscape(t), "/", "%2F")
}
