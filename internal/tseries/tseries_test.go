package tseries

import (
	"math"
	"testing"
	"time"
)

func TestReconcileDistinguishesThreeKindsOfAbsence(t *testing.T) {
	start := MustParse("2024-01-01")
	end := MustParse("2024-01-10")

	// The wiki reported traffic on every day except Jan 8 (a pipeline gap).
	project := map[string]float64{}
	for i := 0; i < 10; i++ {
		d := start.AddDate(0, 0, i).Format(DateFmt)
		if d == "2024-01-08" {
			continue
		}
		project[d] = 1_000_000
	}
	// The article was created on Jan 4 and has views only on 4, 5 and 9.
	article := map[string]float64{"2024-01-04": 10, "2024-01-05": 12, "2024-01-09": 7}

	views, proj, cov := Reconcile(start, end, article, project, "2024-01-04")

	if cov.Days != 10 {
		t.Fatalf("days: %d", cov.Days)
	}
	if cov.BeforeCreate != 3 { // Jan 1-3
		t.Fatalf("before_create: %d", cov.BeforeCreate)
	}
	for i := 0; i < 3; i++ {
		if !math.IsNaN(views[i]) {
			t.Fatalf("pre-creation day %d must be NaN, got %v", i, views[i])
		}
	}
	if cov.Observed != 3 {
		t.Fatalf("observed: %d", cov.Observed)
	}
	// Jan 6, 7, 10 -> true zeros (project reported traffic, article did not)
	if cov.ZeroFilled != 3 {
		t.Fatalf("zero_filled: %d", cov.ZeroFilled)
	}
	if views[5] != 0 || views[6] != 0 || views[9] != 0 {
		t.Fatalf("expected true zeros at idx 5,6,9: %v", views)
	}
	// Jan 8 -> unknown, because the project is silent too
	if cov.Gaps != 1 || !math.IsNaN(views[7]) {
		t.Fatalf("gap not detected: gaps=%d views[7]=%v", cov.Gaps, views[7])
	}
	if !math.IsNaN(proj[7]) {
		t.Fatalf("project series must be NaN on the gap day")
	}
	want := float64(3+3) / float64(10-3)
	if math.Abs(cov.Completeness-want) > 1e-9 {
		t.Fatalf("completeness %.4f want %.4f", cov.Completeness, want)
	}
}

func TestReconcileWithoutCreationDate(t *testing.T) {
	start, end := MustParse("2024-01-01"), MustParse("2024-01-03")
	project := map[string]float64{"2024-01-01": 100, "2024-01-02": 100, "2024-01-03": 100}
	views, _, cov := Reconcile(start, end, map[string]float64{"2024-01-02": 5}, project, "")
	if cov.BeforeCreate != 0 || cov.ZeroFilled != 2 || cov.Observed != 1 {
		t.Fatalf("cov = %+v", cov)
	}
	if views[0] != 0 || views[1] != 5 || views[2] != 0 {
		t.Fatalf("views = %v", views)
	}
}

func TestSharePerMillion(t *testing.T) {
	v := []float64{100, math.NaN(), 50, 10}
	p := []float64{1e6, 1e6, math.NaN(), 0}
	s := SharePerMillion(v, p)
	if s[0] != 100 {
		t.Fatalf("share[0] = %v want 100", s[0])
	}
	for _, i := range []int{1, 2, 3} {
		if !math.IsNaN(s[i]) {
			t.Fatalf("share[%d] should be NaN, got %v", i, s[i])
		}
	}
}

func TestAddBasket(t *testing.T) {
	a := []float64{1, math.NaN(), 3, math.NaN()}
	b := []float64{10, 20, math.NaN(), math.NaN()}
	got := Add(append([]float64(nil), a...), b)
	if got[0] != 11 || got[1] != 20 || got[2] != 3 {
		t.Fatalf("got %v", got)
	}
	if !math.IsNaN(got[3]) {
		t.Fatalf("both-missing must stay missing, got %v", got[3])
	}
}

func TestClampWindow(t *testing.T) {
	api := MustParse("2015-07-01")
	today := MustParse("2026-09-23")
	s, e, notes := ClampWindow(MustParse("2010-01-01"), MustParse("2026-09-23"), api, today)
	if !s.Equal(api) {
		t.Fatalf("start not clamped: %v", s)
	}
	if !e.Equal(MustParse("2026-09-21")) {
		t.Fatalf("end not clamped: %v", e)
	}
	if len(notes) != 2 {
		t.Fatalf("expected two notes, got %v", notes)
	}
	// a window fully inside the valid range is left alone and stays quiet
	s2, e2, n2 := ClampWindow(MustParse("2023-01-01"), MustParse("2024-01-01"), api, today)
	if len(n2) != 0 || !s2.Equal(MustParse("2023-01-01")) || !e2.Equal(MustParse("2024-01-01")) {
		t.Fatalf("valid window altered: %v %v %v", s2, e2, n2)
	}
}

func TestDays(t *testing.T) {
	if got := Days(MustParse("2024-01-01"), MustParse("2024-01-01")); got != 1 {
		t.Fatalf("same day should be 1, got %d", got)
	}
	if got := Days(MustParse("2024-01-01"), MustParse("2024-12-31")); got != 366 {
		t.Fatalf("leap year: %d", got)
	}
	// DST does not exist in UTC, but guard the arithmetic anyway
	if got := Days(MustParse("2024-03-01"), MustParse("2024-04-01")); got != 32 {
		t.Fatalf("across March: %d", got)
	}
	_ = time.UTC
}
