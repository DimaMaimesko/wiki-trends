package wikiapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Cache is a content-addressed disk cache for API responses.
//
// It is what makes iterative research affordable. A founder's follow-up ("now
// also show me German", "make it three years", "exclude the spike") re-runs the
// pipeline; without a cache each iteration would re-download megabytes and take
// minutes, which in practice means the agent stops iterating. With it, only the
// genuinely new slice is fetched.
//
// A cached zero-length body is meaningful: it records "the API says there is no
// data here", so scanning 40 wikis for a topic covered by 6 costs 6 requests on
// the second run, not 40.
type Cache struct {
	Dir string
}

type entry struct {
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
	Empty     bool      `json:"empty,omitempty"`
}

func DefaultDir() string {
	if d := os.Getenv("WIKITRENDS_CACHE"); d != "" {
		return d
	}
	if h, err := os.UserCacheDir(); err == nil {
		return filepath.Join(h, "wikitrends")
	}
	return filepath.Join(os.TempDir(), "wikitrends-cache")
}

func OpenCache(dir string) (*Cache, error) {
	if dir == "" {
		dir = DefaultDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Cache{Dir: dir}, nil
}

func (c *Cache) paths(u string) (body, meta string) {
	sum := sha256.Sum256([]byte(u))
	h := hex.EncodeToString(sum[:])
	// two-level fan-out keeps directory listings usable after tens of thousands
	// of entries
	dir := filepath.Join(c.Dir, h[:2])
	return filepath.Join(dir, h[2:]+".json"), filepath.Join(dir, h[2:]+".meta")
}

// Get returns the cached body if present and fresh. ttl == 0 means "never
// expires", used for date ranges that can no longer change.
func (c *Cache) Get(u string, ttl time.Duration) ([]byte, bool) {
	body, meta := c.paths(u)
	mb, err := os.ReadFile(meta)
	if err != nil {
		return nil, false
	}
	var e entry
	if json.Unmarshal(mb, &e) != nil {
		return nil, false
	}
	if ttl > 0 && time.Since(e.FetchedAt) > ttl {
		return nil, false
	}
	if e.Empty {
		return nil, true
	}
	b, err := os.ReadFile(body)
	if err != nil {
		return nil, false
	}
	return b, true
}

func (c *Cache) Put(u string, body []byte) {
	bp, mp := c.paths(u)
	if os.MkdirAll(filepath.Dir(bp), 0o755) != nil {
		return
	}
	e := entry{URL: u, FetchedAt: time.Now().UTC(), Empty: len(body) == 0}
	if len(body) > 0 {
		if writeAtomic(bp, body) != nil {
			return
		}
	}
	mb, _ := json.Marshal(e)
	_ = writeAtomic(mp, mb)
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Stat reports cache size for the `cache` subcommand.
func (c *Cache) Stat() (files int, bytes int64) {
	_ = filepath.Walk(c.Dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".meta" {
			files++
		}
		bytes += info.Size()
		return nil
	})
	return
}

func (c *Cache) Clear() error { return os.RemoveAll(c.Dir) }
