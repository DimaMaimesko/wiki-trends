package cli

import (
	"strings"
	"testing"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
	"github.com/dmytromaimesko/wiki-trends/internal/tseries"
)

func TestParseKVHandlesTitlesWithSpacesAndDiacritics(t *testing.T) {
	got := parseKV("pl=Głodówka lecznicza,cs=Přerušovaný půst")
	if got["pl"] != "Głodówka lecznicza" || got["cs"] != "Přerušovaný půst" {
		t.Fatalf("got %#v", got)
	}
	if len(parseKV("")) != 0 {
		t.Fatal("empty input should yield no pairs")
	}
	// a value with no '=' is skipped rather than producing an empty key
	if len(parseKV("junk,pl=X")) != 1 {
		t.Fatalf("got %#v", parseKV("junk,pl=X"))
	}
}

func TestParseWeightsRequiresAllThree(t *testing.T) {
	w, err := parseWeights("level=0.5,growth=0.3,reliability=0.2")
	if err != nil || w["level"] != 0.5 || w["growth"] != 0.3 || w["reliability"] != 0.2 {
		t.Fatalf("w=%v err=%v", w, err)
	}
	if _, err := parseWeights("level=0.5,growth=0.5"); err == nil {
		t.Fatal("a partial weight set must be rejected, not silently defaulted")
	} else if !strings.Contains(err.Error(), "reliability") {
		t.Fatalf("error should name the missing key: %v", err)
	}
	if _, err := parseWeights("level=x,growth=1,reliability=1"); err == nil {
		t.Fatal("a non-numeric weight must be rejected")
	}
	if w, err := parseWeights(""); err != nil || w != nil {
		t.Fatal("empty means 'use defaults'")
	}
}

func TestWindowDefaultsAndValidation(t *testing.T) {
	c := common{months: 24}
	start, end, err := c.window()
	if err != nil {
		t.Fatal(err)
	}
	// the end must stop short of today: the last days are not final
	if d := tseries.Days(end, tseries.MustParse("2099-01-01")); d <= 0 {
		t.Fatal("end is in the future")
	}
	if got := tseries.Days(start, end); got < 720 || got > 740 {
		t.Fatalf("24 months spans %d days", got)
	}

	c2 := common{months: 24, start: "2025-01-01", end: "2024-01-01"}
	if _, _, err := c2.window(); err == nil {
		t.Fatal("an inverted window must be rejected")
	}
	c3 := common{months: 24, start: "not-a-date"}
	if _, _, err := c3.window(); err == nil {
		t.Fatal("a malformed date must be rejected")
	}
}

func TestSlugIsStableAndFilesystemSafe(t *testing.T) {
	ds := &model.Dataset{
		Topic:  model.Topic{LabelEn: "Intermittent fasting / IF!"},
		Series: []model.Series{{Wiki: "cs"}, {Wiki: "pl"}},
	}
	got := slug(ds)
	if strings.ContainsAny(got, ` /!.`) {
		t.Fatalf("slug is not path-safe: %q", got)
	}
	if got != slug(ds) {
		t.Fatal("slug must be deterministic so follow-up runs land in the same directory")
	}
	if !strings.Contains(got, "intermittent-fasting") || !strings.Contains(got, "cspl") {
		t.Fatalf("slug should identify topic and editions: %q", got)
	}
	// many editions are truncated rather than producing an unusable path
	var many []model.Series
	for _, w := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		many = append(many, model.Series{Wiki: w})
	}
	long := slug(&model.Dataset{Topic: model.Topic{Query: "x"}, Series: many})
	if !strings.Contains(long, "and3") {
		t.Fatalf("expected truncation marker: %q", long)
	}
	// a topic with no usable name still yields a directory
	if s := slug(&model.Dataset{}); s == "" || strings.HasPrefix(s, "-") {
		t.Fatalf("degenerate slug: %q", s)
	}
}

func TestSplitList(t *testing.T) {
	if got := splitList(" pl , cs ,, uk "); len(got) != 3 || got[0] != "pl" || got[2] != "uk" {
		t.Fatalf("got %#v", got)
	}
	if got := splitList(""); got != nil {
		t.Fatalf("empty input should yield nil, got %#v", got)
	}
}

func TestMainRejectsUnknownCommand(t *testing.T) {
	if code := Main([]string{"nonsense"}); code != 2 {
		t.Fatalf("exit code %d, want 2", code)
	}
	if code := Main(nil); code != 2 {
		t.Fatalf("no args should print usage and exit 2, got %d", code)
	}
	if code := Main([]string{"version"}); code != 0 {
		t.Fatalf("version exit code %d", code)
	}
}
