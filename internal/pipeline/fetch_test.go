package pipeline

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
	"github.com/dmytromaimesko/wiki-trends/internal/tseries"
	"github.com/dmytromaimesko/wiki-trends/internal/wikiapi"
)

// The fixture cache under testdata/ holds real recorded API responses for a
// window that ended long ago, so it can never change. Running the client in
// offline mode against it exercises the whole fetch path — resolution, redirect
// following, creation dates, the project denominator, reconciliation — with no
// network and no flakiness.
//
// Re-record with:
//
//	WIKITRENDS_CACHE=$PWD/testdata/cache go run ./cmd/wikitrends run \
//	  --qid Q1666254 --topic "intermittent fasting" --wikis cs,pl \
//	  --start 2024-01-01 --end 2024-06-30 --out /tmp/x
func offlineClient(t *testing.T) *wikiapi.Client {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "cache"))
	if err != nil {
		t.Fatal(err)
	}
	cache, err := wikiapi.OpenCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	return wikiapi.New(cache, wikiapi.WithOffline(true))
}

func TestFetchFromRecordedResponses(t *testing.T) {
	c := offlineClient(t)
	defer c.Close()

	ds, err := Fetch(context.Background(), c, FetchOptions{
		QID:   "Q1666254",
		Query: "intermittent fasting",
		Wikis: []string{"cs", "pl"},
		Start: tseries.MustParse("2024-01-01"),
		End:   tseries.MustParse("2024-06-30"),
	})
	if err != nil {
		t.Fatalf("offline fetch failed (fixtures missing or stale?): %v", err)
	}
	if c.Stats.Requests != 0 {
		t.Fatalf("offline mode made %d network requests", c.Stats.Requests)
	}
	if ds.Topic.LabelEn != "intermittent fasting" {
		t.Fatalf("label: %q", ds.Topic.LabelEn)
	}
	if ds.Window.Start != "2024-01-01" || ds.Window.End != "2024-06-30" {
		t.Fatalf("window: %+v", ds.Window)
	}
	if len(ds.Series) != 2 {
		t.Fatalf("expected two series, got %d", len(ds.Series))
	}

	var cs, pl *model.Series
	for i := range ds.Series {
		switch ds.Series[i].Wiki {
		case "cs":
			cs = &ds.Series[i]
		case "pl":
			pl = &ds.Series[i]
		}
	}

	// Czech has the article.
	if cs.Status != model.StatusOK {
		t.Fatalf("cs status %q (%s)", cs.Status, cs.Note)
	}
	if len(cs.Articles) != 1 || cs.Articles[0].Title != "Přerušovaný půst" {
		t.Fatalf("cs article: %+v", cs.Articles)
	}
	if cs.Articles[0].CreatedAt != "2020-10-28" {
		t.Fatalf("creation date not captured: %q", cs.Articles[0].CreatedAt)
	}
	want := tseries.Days(tseries.MustParse("2024-01-01"), tseries.MustParse("2024-06-30"))
	if len(cs.Views) != want || len(cs.ProjectViews) != want {
		t.Fatalf("series length %d/%d, want %d", len(cs.Views), len(cs.ProjectViews), want)
	}
	if cs.Coverage.Days != want {
		t.Fatalf("coverage days %d, want %d", cs.Coverage.Days, want)
	}
	// the project denominator must be populated and plausible
	for i, v := range cs.ProjectViews {
		if math.IsNaN(v) || v < 1e5 {
			t.Fatalf("project views[%d] = %v, expected a full-wiki daily total", i, v)
		}
	}

	// Polish genuinely has no article for this concept. This is the case the
	// skill must never turn into "zero interest", and it is the reason resolve
	// exists as a separate step.
	if pl.Status != model.StatusNoArticle {
		t.Fatalf("pl status %q, want no_article", pl.Status)
	}
	if len(pl.Views) != 0 {
		t.Fatal("a missing article must produce no series at all, not zeros")
	}
	if len(pl.Candidates) == 0 {
		t.Fatal("search candidates should be offered so the agent can propose a substitute")
	}
	foundNote := false
	for _, n := range ds.Notes {
		if n == "pl.wikipedia has no article for this concept - a coverage gap, not zero interest" {
			foundNote = true
		}
	}
	if !foundNote {
		t.Fatalf("the coverage gap must be recorded in the dataset notes: %v", ds.Notes)
	}
}

func TestFetchRejectsEmptyWikiList(t *testing.T) {
	c := offlineClient(t)
	defer c.Close()
	if _, err := Fetch(context.Background(), c, FetchOptions{QID: "Q1", Start: tseries.MustParse("2024-01-01"), End: tseries.MustParse("2024-02-01")}); err == nil {
		t.Fatal("expected an error with no editions requested")
	}
}

func TestMergeCoverageKeepsCompletenessAFraction(t *testing.T) {
	a := model.Coverage{Days: 100, Observed: 90, ZeroFilled: 5, Gaps: 5, BeforeCreate: 0}
	b := model.Coverage{Days: 100, Observed: 80, ZeroFilled: 10, Gaps: 10, BeforeCreate: 20}
	got := mergeCoverage(a, b)
	if got.Completeness > 1 {
		t.Fatalf("completeness %v exceeds 1 — counts were summed across basket members", got.Completeness)
	}
	if got.BeforeCreate != 0 {
		t.Fatalf("a basket exists once any member does; before_create = %d", got.BeforeCreate)
	}
	// merging into an empty coverage must return the other side untouched
	if got := mergeCoverage(model.Coverage{}, b); got != b {
		t.Fatalf("merge with zero value altered the input: %+v", got)
	}
}
