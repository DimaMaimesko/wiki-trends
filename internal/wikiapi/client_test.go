package wikiapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// An offline client must serve an entry whose TTL has expired. Before this was
// the case, the recorded fixtures in testdata/cache stopped working one
// actionTTL after they were recorded, and `go test ./...` failed on any fresh
// checkout.
func TestOfflineServesExpiredEntries(t *testing.T) {
	cache, err := OpenCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	u := "https://example.org/w/api.php?action=query&format=json"
	cache.Put(u, []byte(`{"ok":true}`))

	// Age the entry well past actionTTL.
	_, meta := cache.paths(u)
	b, err := json.Marshal(entry{URL: u, FetchedAt: time.Now().UTC().Add(-48 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(meta, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Get(u, actionTTL); ok {
		t.Fatal("setup: entry should be expired for an online lookup")
	}

	c := New(cache, WithOffline(true))
	defer c.Close()
	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.getJSON(context.Background(), u, actionTTL, &out); err != nil {
		t.Fatalf("offline client refused an expired cache entry: %v", err)
	}
	if !out.OK {
		t.Fatal("cached body was not decoded")
	}
	if c.Stats.Requests != 0 {
		t.Fatalf("offline client made %d network requests", c.Stats.Requests)
	}
}
