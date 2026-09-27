package stats

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

var epoch = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

// synth builds a daily series with a known compound trend, optional weekly and
// annual seasonality, and optional AR(1) noise, so every estimator can be checked
// against a ground truth.
func synth(n int, level, slopePerDay, weeklyAmp, annualAmp, noise, phi float64, seed int64) []float64 {
	r := rand.New(rand.NewSource(seed))
	out := make([]float64, n)
	e := 0.0
	for i := 0; i < n; i++ {
		d := epoch.AddDate(0, 0, i)
		f := 1.0
		if weeklyAmp != 0 {
			f *= 1 + weeklyAmp*math.Cos(2*math.Pi*float64(int(d.Weekday()))/7)
		}
		if annualAmp != 0 {
			f *= 1 + annualAmp*math.Cos(2*math.Pi*float64(d.YearDay())/365.25)
		}
		e = phi*e + noise*r.NormFloat64()
		out[i] = level * math.Exp(slopePerDay*float64(i)) * f * math.Exp(e)
	}
	return out
}

func TestFitLogLinearRecoversKnownGrowth(t *testing.T) {
	// +0.001/day on the log scale == exp(0.001*365.25)-1 == +44.1%/yr
	want := GrowthPctPerYear(0.001)
	x := synth(730, 200, 0.001, 0, 0, 0.05, 0, 7)
	f := FitLogLinear(Log1p(x))
	got := GrowthPctPerYear(f.SlopePerDay)
	if math.Abs(got-want) > 2 {
		t.Fatalf("growth: got %.2f%%/yr want ~%.2f%%/yr", got, want)
	}
	if f.N != 730 {
		t.Fatalf("N: got %d", f.N)
	}
	if f.R2 < 0.9 {
		t.Fatalf("R2 too low for a clean trend: %.3f", f.R2)
	}
	lo, hi := f.CIPctPerYear(1.96)
	if !(lo < want && want < hi) {
		t.Fatalf("CI [%.2f, %.2f] excludes truth %.2f", lo, hi, want)
	}
}

// The central methodological claim of this package: on autocorrelated data,
// Newey-West standard errors must be materially larger than naive OLS ones.
// If this test fails, every p-value the skill reports is too small.
func TestHACWiderThanNaiveUnderAutocorrelation(t *testing.T) {
	x := synth(730, 500, 0, 0, 0, 0.15, 0.9, 11) // no trend, strong AR(1)
	y := Log1p(x)
	f := FitLogLinear(y)

	// naive homoskedastic OLS standard error for the slope
	var sx, sy float64
	for i, v := range y {
		sx += float64(i)
		sy += v
	}
	n := float64(len(y))
	mx, my := sx/n, sy/n
	var sxx, ssr float64
	for i, v := range y {
		dx := float64(i) - mx
		sxx += dx * dx
		_ = my
		r := v - (f.Intercept + f.SlopePerDay*float64(i))
		ssr += r * r
	}
	naive := math.Sqrt(ssr/(n-2)) / math.Sqrt(sxx)

	if f.StdErr <= naive {
		t.Fatalf("HAC se %.3g should exceed naive se %.3g under AR(1)", f.StdErr, naive)
	}
	if f.StdErr/naive < 1.5 {
		t.Fatalf("HAC inflation only %.2fx; expected >=1.5x at phi=0.9", f.StdErr/naive)
	}
	if f.NWLag < 3 {
		t.Fatalf("Newey-West lag %d unexpectedly small for n=730", f.NWLag)
	}
	// and the flat series must not be declared significant
	if f.PValue < 0.05 {
		t.Fatalf("no-trend series reported significant, p=%.4f", f.PValue)
	}
}

func TestTheilSenAgreesWithOLSOnCleanTrend(t *testing.T) {
	x := synth(400, 100, 0.0015, 0, 0, 0.05, 0, 3)
	y := Log1p(x)
	ols := FitLogLinear(y).SlopePerDay
	ts := TheilSen(y)
	if math.Abs(ols-ts)/math.Abs(ols) > 0.15 {
		t.Fatalf("Theil-Sen %.5f vs OLS %.5f differ by >15%%", ts, ols)
	}
}

func TestTheilSenResistsSpikesWhileOLSDoesNot(t *testing.T) {
	x := synth(400, 100, 0, 0, 0, 0.02, 0, 5) // flat
	for i := 380; i < 400; i++ {
		x[i] *= 60 // late mega-event
	}
	y := Log1p(x)
	ols := GrowthPctPerYear(FitLogLinear(y).SlopePerDay)
	ts := GrowthPctPerYear(TheilSen(y))
	if ols < 50 {
		t.Fatalf("expected OLS to be fooled by the event, got %.1f%%/yr", ols)
	}
	if math.Abs(ts) > 15 {
		t.Fatalf("Theil-Sen should stay near flat, got %.1f%%/yr", ts)
	}
}

func TestMannKendall(t *testing.T) {
	inc := make([]float64, 36)
	for i := range inc {
		inc[i] = float64(i) + 0.3*math.Sin(float64(i))
	}
	tau, p := MannKendall(inc)
	if tau < 0.9 || p > 0.001 {
		t.Fatalf("monotonic: tau=%.3f p=%.4f", tau, p)
	}

	r := rand.New(rand.NewSource(42))
	noise := make([]float64, 36)
	for i := range noise {
		noise[i] = r.NormFloat64()
	}
	_, p2 := MannKendall(noise)
	if p2 < 0.1 {
		t.Fatalf("white noise declared trending, p=%.4f", p2)
	}
	if _, p3 := MannKendall([]float64{1, 2, 3}); !math.IsNaN(p3) {
		t.Fatalf("too-short input must return NaN, got %v", p3)
	}
}

func TestSeasonalityRecoveryAndAdjustment(t *testing.T) {
	x := synth(800, 300, 0.0005, 0.35, 0.30, 0.02, 0, 13)
	s := EstimateSeasonality(epoch, x)
	if !s.HasWeekly || !s.HasAnnual {
		t.Fatalf("expected both cycles detected: weekly=%v annual=%v", s.HasWeekly, s.HasAnnual)
	}
	// injected weekly factor spans 1-0.35 .. 1+0.35 -> amplitude ~ 1.35/0.65 = 2.08
	if s.WeeklyAmp < 1.7 || s.WeeklyAmp > 2.5 {
		t.Fatalf("weekly amplitude %.2f outside expected range", s.WeeklyAmp)
	}
	if s.AnnualAmp < 1.5 || s.AnnualAmp > 2.5 {
		t.Fatalf("annual amplitude %.2f outside expected range", s.AnnualAmp)
	}
	// adjusting must shrink the weekly rhythm to nearly nothing
	adj := s.Adjust(epoch, x)
	after := EstimateSeasonality(epoch, adj)
	if after.WeeklyAmp > 1.15 {
		t.Fatalf("weekly amplitude still %.2f after adjustment", after.WeeklyAmp)
	}
	// and it must not destroy the trend
	if g := GrowthPctPerYear(FitLogLinear(Log1p(adj)).SlopePerDay); math.Abs(g-GrowthPctPerYear(0.0005)) > 4 {
		t.Fatalf("trend distorted by adjustment: %.2f%%/yr", g)
	}
}

func TestSeasonalityRefusesAnnualUnderTwoYears(t *testing.T) {
	x := synth(500, 300, 0, 0.2, 0.3, 0.02, 0, 17)
	if s := EstimateSeasonality(epoch, x); s.HasAnnual {
		t.Fatal("annual seasonality must not be claimed with <730 days")
	}
}

func TestYoYIsSeasonalityProof(t *testing.T) {
	// two years, identical strong seasonality, second year uniformly +25%
	x := synth(730, 400, 0, 0.3, 0.4, 0.01, 0, 19)
	for i := 365; i < 730; i++ {
		x[i] *= 1.25
	}
	pct, _, _, ok := YoY(x)
	if !ok {
		t.Fatal("YoY should be comparable with 730 days")
	}
	if math.Abs(pct-25) > 4 {
		t.Fatalf("YoY got %.2f%% want ~25%%", pct)
	}
	if _, _, _, ok := YoY(x[:400]); ok {
		t.Fatal("YoY must refuse windows shorter than two years")
	}
}

func TestMonthlyMeansDropsPartialMonths(t *testing.T) {
	// start mid-January so the first month is incomplete
	start := time.Date(2023, 1, 20, 0, 0, 0, 0, time.UTC)
	x := make([]float64, 400)
	for i := range x {
		x[i] = 10
	}
	vals, labels := MonthlyMeans(start, x)
	if len(vals) != len(labels) || len(labels) == 0 {
		t.Fatal("bad shape")
	}
	if labels[0] == "2023-01" {
		t.Fatalf("partial first month kept: %v", labels[:3])
	}
	for _, v := range vals {
		if math.Abs(v-10) > 1e-9 {
			t.Fatalf("mean per day should be 10, got %v", v)
		}
	}
}

func TestDetectSpikesAndMaskRestoresTrend(t *testing.T) {
	x := synth(500, 200, 0.0008, 0.15, 0, 0.03, 0, 23)
	want := GrowthPctPerYear(0.0008)
	for i := 300; i < 305; i++ {
		x[i] *= 25
	}
	ev := DetectSpikes(epoch, x, 4, 29)
	if len(ev) == 0 {
		t.Fatal("no spike detected")
	}
	hit := false
	for _, e := range ev {
		if e.PeakIdx >= 299 && e.PeakIdx <= 306 {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("spike not located near day 300: %+v", ev)
	}
	if sh := SpikeViewShare(x, ev); sh < 0.1 {
		t.Fatalf("spike view share suspiciously low: %.3f", sh)
	}
	inflated := GrowthPctPerYear(FitLogLinear(Log1p(x)).SlopePerDay)
	cleaned := GrowthPctPerYear(FitLogLinear(Log1p(MaskSpikes(x, ev, 29))).SlopePerDay)
	if math.Abs(cleaned-want) > 3 {
		t.Fatalf("masked trend %.2f%%/yr should return to ~%.2f%%/yr", cleaned, want)
	}
	if math.Abs(inflated-want) <= math.Abs(cleaned-want) {
		t.Fatalf("masking did not improve the estimate (raw %.2f, masked %.2f, truth %.2f)", inflated, cleaned, want)
	}
}

func TestDetectSpikesScaleInvariant(t *testing.T) {
	small := synth(400, 30, 0, 0, 0, 0.05, 0, 29)
	big := make([]float64, len(small))
	for i := range small {
		big[i] = small[i] * 10000
	}
	for i := 200; i < 203; i++ {
		small[i] *= 12
		big[i] *= 12
	}
	a := DetectSpikes(epoch, small, 4, 29)
	b := DetectSpikes(epoch, big, 4, 29)
	if len(a) != len(b) {
		t.Fatalf("detector is not scale invariant: %d vs %d events", len(a), len(b))
	}
}

func TestDetectLevelShiftsJointFit(t *testing.T) {
	// A pure trend must NOT be reported as a level shift.
	pure := Log1p(synth(700, 300, 0.001, 0, 0, 0.02, 0, 31))
	if s := DetectLevelShifts(epoch, pure, 90, 20); len(s) != 0 {
		t.Fatalf("pure trend misreported as level shift: %+v", s)
	}
	// An injected 40% step on top of a trend must be found, at close to the right
	// date and close to the right magnitude. Recovering the magnitude is what the
	// joint fit buys: residual-based detection reports roughly a third of it.
	x := synth(700, 300, 0.001, 0, 0, 0.02, 0, 31)
	for i := 400; i < len(x); i++ {
		x[i] *= 0.6
	}
	sh := DetectLevelShifts(epoch, Log1p(x), 90, 20)
	if len(sh) != 1 {
		t.Fatalf("expected one shift, got %+v", sh)
	}
	if sh[0].Idx < 390 || sh[0].Idx > 410 {
		t.Fatalf("shift located at %d (%s), expected ~400", sh[0].Idx, sh[0].Date)
	}
	if math.Abs(sh[0].RatioPct-(-40)) > 6 {
		t.Fatalf("step magnitude %.1f%%, expected ~-40%%", sh[0].RatioPct)
	}
	// With the step modelled, the underlying slope must come back out clean.
	if math.Abs(sh[0].Z) < 3 {
		t.Fatalf("a 40%% step should be highly significant, z=%.2f", sh[0].Z)
	}
	// Noise alone must not produce a shift.
	flat := Log1p(synth(700, 300, 0, 0, 0, 0.05, 0.5, 41))
	if s := DetectLevelShifts(epoch, flat, 90, 20); len(s) != 0 {
		t.Fatalf("noise misreported as level shift: %+v", s)
	}
}

func TestFitOLSMatchesSimpleRegression(t *testing.T) {
	y := Log1p(synth(500, 200, 0.0012, 0, 0, 0.03, 0, 43))
	X := make([][]float64, len(y))
	for i := range y {
		X[i] = []float64{1, float64(i)}
	}
	m := FitOLS(X, y, NWLag(len(y)))
	f := FitLogLinear(y)
	if m == nil {
		t.Fatal("nil fit")
	}
	if math.Abs(m.Beta[1]-f.SlopePerDay) > 1e-9 {
		t.Fatalf("slopes differ: %v vs %v", m.Beta[1], f.SlopePerDay)
	}
	if math.Abs(m.SE[1]-f.StdErr)/f.StdErr > 0.02 {
		t.Fatalf("HAC standard errors differ: %v vs %v", m.SE[1], f.StdErr)
	}
}

func TestBenjaminiHochberg(t *testing.T) {
	// textbook example
	p := []float64{0.001, 0.008, 0.039, 0.041, 0.042, 0.06, 0.074, 0.205}
	adj := BenjaminiHochberg(p)
	for i := 1; i < len(adj); i++ {
		if adj[i] < adj[i-1]-1e-12 {
			t.Fatalf("adjusted p-values must be monotone: %v", adj)
		}
	}
	if adj[0] < p[0] {
		t.Fatal("adjustment must not lower a p-value")
	}
	if math.Abs(adj[0]-0.008) > 1e-9 {
		t.Fatalf("smallest adjusted p: got %.6f want 0.008", adj[0])
	}
	// a lone marginal result among many tests must stop being significant
	many := make([]float64, 20)
	for i := range many {
		many[i] = 0.5
	}
	many[0] = 0.04
	if a := BenjaminiHochberg(many); a[0] <= 0.05 {
		t.Fatalf("p=0.04 out of 20 tests should not survive FDR, got %.3f", a[0])
	}
	// NaNs pass through
	if a := BenjaminiHochberg([]float64{math.NaN(), 0.01}); !math.IsNaN(a[0]) {
		t.Fatal("NaN input must stay NaN")
	}
}

func TestNaNAwareness(t *testing.T) {
	x := []float64{1, math.NaN(), 3, math.NaN(), 5}
	if m := Mean(x); m != 3 {
		t.Fatalf("Mean ignoring NaN: got %v", m)
	}
	if s := Sum(x); s != 9 {
		t.Fatalf("Sum ignoring NaN: got %v", s)
	}
	if md := Median(x); md != 3 {
		t.Fatalf("Median ignoring NaN: got %v", md)
	}
	// a fit must skip missing days rather than treat them as zero
	y := Log1p(synth(400, 100, 0.001, 0, 0, 0.02, 0, 37))
	for i := 100; i < 150; i++ {
		y[i] = math.NaN()
	}
	f := FitLogLinear(y)
	if f.N != 350 {
		t.Fatalf("N should exclude NaN days: %d", f.N)
	}
	if g := GrowthPctPerYear(f.SlopePerDay); math.Abs(g-GrowthPctPerYear(0.001)) > 3 {
		t.Fatalf("gaps distorted the trend: %.2f%%/yr", g)
	}
}

func TestWinsorizePreservesShapeAndNaN(t *testing.T) {
	x := []float64{1, 2, 3, 4, 100, math.NaN()}
	w := Winsorize(x, 0.05, 0.80)
	if len(w) != len(x) || !math.IsNaN(w[5]) {
		t.Fatal("shape or NaN not preserved")
	}
	if w[4] >= 100 {
		t.Fatalf("upper tail not clipped: %v", w)
	}
}

func TestSeasonalityRejectsNonRepeatingPattern(t *testing.T) {
	// A flat series with one 60-day bump in a single year must NOT be reported as
	// annual seasonality: the pattern does not repeat.
	x := synth(1100, 500, 0, 0.1, 0, 0.03, 0, 101)
	for i := 400; i < 460; i++ {
		x[i] *= 3
	}
	if s := EstimateSeasonality(epoch, x); s.HasAnnual {
		t.Fatalf("one-off bump reported as annual seasonality, amp=%.2f monthly=%v", s.AnnualAmp, s.Monthly)
	}
}

func TestSeasonalityAcceptsRepeatingPattern(t *testing.T) {
	// Three years of a genuine annual cycle on top of a steep decline: the cycle
	// must still be recovered, and its amplitude must stay plausible. A global
	// log-linear detrend produced a 5x amplitude on data shaped like this.
	x := synth(1100, 2000, -0.0014, 0.15, 0.30, 0.03, 0, 103)
	s := EstimateSeasonality(epoch, x)
	if !s.HasAnnual {
		t.Fatal("repeating annual cycle not detected")
	}
	if s.AnnualAmp < 1.5 || s.AnnualAmp > 2.6 {
		t.Fatalf("annual amplitude %.2f implausible for an injected 1.30/0.70 cycle", s.AnnualAmp)
	}
	// the injected cycle peaks at day-of-year 1 (cos at 0), so January must lead
	if p := argMax(s.Monthly[:]); p != 0 && p != 11 {
		t.Fatalf("peak month index %d, expected January (0) or December (11); monthly=%v", p, s.Monthly)
	}
}

func TestExtendEdgesUsesLocalSlope(t *testing.T) {
	// a linear ramp: a 365-day centred mean is defined only in the middle, and
	// extrapolation must reconstruct the ends
	n := 800
	x := make([]float64, n)
	for i := range x {
		x[i] = float64(i)
	}
	ma := CentredMA(x, 365)
	if !math.IsNaN(ma[0]) {
		t.Fatal("expected undefined edge before extension")
	}
	ext := extendEdges(ma)
	for _, i := range []int{0, 50, n - 1} {
		if math.IsNaN(ext[i]) {
			t.Fatalf("index %d still undefined", i)
		}
		if math.Abs(ext[i]-x[i]) > 2 {
			t.Fatalf("extrapolation at %d = %.2f, want ~%.2f", i, ext[i], x[i])
		}
	}
}

// Regression test for a real failure: Ukrainian astronomy has a textbook
// academic-calendar shape (September 2.2x, July 0.41x) but the first version of
// LooksLikeSchoolYear rejected it, because March and April sat just below average
// and dragged a hardcoded four-month "term" mean to exactly 1.00.
func TestSchoolYearPatternRealShape(t *testing.T) {
	ukAstronomy := [12]float64{1.116, 1.127, 0.868, 0.929, 0.904, 0.492, 0.407, 0.574, 2.205, 1.087, 1.119, 1.171}
	s := Seasonality{HasAnnual: true, Monthly: ukAstronomy}
	if !s.LooksLikeSchoolYear() {
		t.Fatal("real academic-calendar shape not recognised")
	}
	if got := s.PeakMonth(); got != 9 {
		t.Fatalf("peak month %d, want 9 (September)", got)
	}
	if got := s.TroughMonth(); got != 7 {
		t.Fatalf("trough month %d, want 7 (July)", got)
	}

	// An inverted shape — busy summer, quiet term — must be rejected.
	holiday := [12]float64{0.7, 0.7, 0.8, 0.9, 1.1, 1.4, 1.6, 1.5, 1.0, 0.8, 0.7, 0.8}
	if (Seasonality{HasAnnual: true, Monthly: holiday}).LooksLikeSchoolYear() {
		t.Fatal("summer-peaking shape must not be called an academic calendar")
	}
	// A flat shape must be rejected.
	var flat Seasonality
	flat.HasAnnual = true
	for i := range flat.Monthly {
		flat.Monthly[i] = 1
	}
	if flat.LooksLikeSchoolYear() {
		t.Fatal("flat seasonality must not look like a school year")
	}
}
