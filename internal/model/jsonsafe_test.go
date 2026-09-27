package model

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestMarshalSafeTurnsNonFiniteIntoNull(t *testing.T) {
	a := Analysis{
		Schema:     AnalysisSchema,
		AnalyzedAt: time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
		Wikis: []WikiAnalysis{{
			Wiki:   "uk",
			Status: StatusOK,
			Share: &TrendStats{
				GrowthPctPerYear:   42,
				CILow:              math.NaN(),
				CIHigh:             math.Inf(1),
				TheilSenPctPerYear: math.NaN(),
				MannKendallP:       0.01,
			},
			Level:       &LevelStats{MeanDaily: math.NaN()},
			Reliability: Reliability{Grade: "B", Score: 0.7},
		}},
		Comparison: &Comparison{
			Weights: map[string]float64{"level": 0.45, "growth": math.NaN()},
			Ranking: []RankRow{{Wiki: "uk", OpportunityScore: math.NaN()}},
		},
	}
	b, err := MarshalSafe(&a, " ")
	if err != nil {
		t.Fatalf("MarshalSafe: %v", err)
	}
	s := string(b)
	if strings.Contains(s, "NaN") || strings.Contains(s, "Inf") {
		t.Fatalf("non-finite value leaked into JSON:\n%s", s)
	}

	// and it must still be valid JSON that round-trips into the model
	var back Analysis
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("round trip: %v\n%s", err, s)
	}
	if len(back.Wikis) != 1 || back.Wikis[0].Wiki != "uk" {
		t.Fatalf("structure lost: %+v", back)
	}
	if back.Wikis[0].Share.GrowthPctPerYear != 42 {
		t.Fatalf("finite value altered: %v", back.Wikis[0].Share.GrowthPctPerYear)
	}
	// A JSON null decodes into a plain float64 as 0, not NaN. That asymmetry is
	// why analysis.json is treated as a write-only artifact: `wikitrends report`
	// recomputes the analysis from dataset.json rather than re-reading it, so
	// "not computed" can never come back as a confident zero.
	if back.Wikis[0].Share.CILow != 0 {
		t.Fatalf("null decodes to the zero value; got %v", back.Wikis[0].Share.CILow)
	}
	if back.Wikis[0].Reliability.Grade != "B" {
		t.Fatal("nested struct lost")
	}
	if back.Comparison.Weights["level"] != 0.45 {
		t.Fatalf("map lost: %+v", back.Comparison.Weights)
	}
}

func TestMarshalSafeMatchesEncodingJSONOnFiniteData(t *testing.T) {
	// When there is nothing to sanitise, output must be byte-identical to
	// encoding/json, so switching writers cannot change the contract.
	ds := Dataset{
		Schema: DatasetSchema,
		Tool:   "wikitrends",
		Topic:  Topic{Query: "astronomy", QID: "Q333", LabelEn: "astronomy"},
		Window: Window{Start: "2024-01-01", End: "2025-01-01", Granularity: "daily"},
		Params: FetchParams{Access: "all-access", Agent: "user"},
		Series: []Series{{
			Wiki: "uk", Project: "uk.wikipedia", Status: StatusOK,
			Start:    "2024-01-01",
			Views:    Nums{1, 2, math.NaN()},
			Coverage: Coverage{Days: 3, Observed: 2, Completeness: 0.66},
			Articles: []Article{{Title: "Астрономія", CreatedAt: "2004-05-01"}},
		}},
		Notes: []string{"note"},
	}
	want, err := json.Marshal(&ds)
	if err != nil {
		t.Fatal(err)
	}
	got, err := MarshalSafe(&ds, "")
	if err != nil {
		t.Fatal(err)
	}
	// Key order differs (maps are unordered), so compare semantically.
	var a, b any
	if err := json.Unmarshal(want, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatalf("%v\n%s", err, got)
	}
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		t.Fatalf("output differs from encoding/json\nencoding/json: %s\nMarshalSafe:   %s", aj, bj)
	}
	// Nums must keep its own null encoding rather than being re-walked.
	if !strings.Contains(string(got), `"views":[1,2,null]`) {
		t.Fatalf("Nums encoding lost: %s", got)
	}
	// non-ASCII titles must survive
	if !strings.Contains(string(got), `А`) && !strings.Contains(string(got), "Астрономія") {
		t.Fatalf("unicode title lost: %s", got)
	}
}

func TestMarshalSafeOmitEmpty(t *testing.T) {
	b, err := MarshalSafe(&TrendStats{GrowthPctPerYear: 1, PValueAdj: math.NaN()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "p_value_adjusted") {
		t.Fatalf("omitempty NaN should be omitted, not null: %s", b)
	}
}
