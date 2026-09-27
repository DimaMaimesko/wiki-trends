package render

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
)

// The cross-reference table is the one part of a hand-written PDF that fails
// silently: a wrong offset yields a file that some readers open and others
// reject. This test parses the file back and checks every offset really points
// at the object it claims.
func TestPDFCrossReferenceTableIsExact(t *testing.T) {
	p := NewPDF(A4W, A4H)
	p.Rect(20, 20, 200, 100, &ColPanel, &ColAxis, 0.6)
	p.Line(20, 200, 400, 260, ColInk, 1.2, true)
	p.Polyline([]Pt{{20, 300}, {120, 340}, {220, 310}}, Palette[0], 1.5, false)
	p.Polygon([]Pt{{20, 400}, {200, 380}, {200, 460}, {20, 470}}, Palette[1], 0.15)
	p.Circle(300, 400, 4, Palette[2])
	p.Text(40, 500, "Trend: +42%/yr (CI +11% to +83%)", TextStyle{Size: 11, Color: ColInk})
	// parentheses and a backslash must survive the literal-string escape
	p.Text(40, 520, `edge (case) \ test`, TextStyle{Size: 9, Color: ColMuted, Align: AlignMiddle})
	p.NewPage()
	p.Text(40, 40, "page two", TextStyle{Size: 12, Bold: true, Color: ColInk})
	b := p.Bytes()

	if !bytes.HasPrefix(b, []byte("%PDF-1.4")) {
		t.Fatal("missing header")
	}
	if !bytes.HasSuffix(b, []byte("%%EOF\n")) {
		t.Fatal("missing trailer marker")
	}

	sx := regexp.MustCompile(`startxref\n(\d+)\n%%EOF`).FindSubmatch(b)
	if sx == nil {
		t.Fatal("no startxref")
	}
	off, _ := strconv.Atoi(string(sx[1]))
	if off <= 0 || off >= len(b) {
		t.Fatalf("startxref %d out of range (len %d)", off, len(b))
	}
	if !bytes.HasPrefix(b[off:], []byte("xref\n")) {
		t.Fatalf("startxref does not point at an xref table, found %q", b[off:off+20])
	}

	entries := regexp.MustCompile(`(?m)^(\d{10}) 00000 n $`).FindAllSubmatch(b[off:], -1)
	if len(entries) == 0 {
		t.Fatal("no xref entries")
	}
	for i, e := range entries {
		o, _ := strconv.Atoi(string(e[1]))
		want := []byte(fmt.Sprintf("%d 0 obj", i+1))
		if o < 0 || o+len(want) > len(b) || !bytes.HasPrefix(b[o:], want) {
			got := ""
			if o >= 0 && o < len(b) {
				end := o + 24
				if end > len(b) {
					end = len(b)
				}
				got = string(b[o:end])
			}
			t.Fatalf("xref entry %d points at offset %d, expected %q, found %q", i+1, o, want, got)
		}
	}
	// declared /Size must match the number of entries plus the free object
	size := regexp.MustCompile(`/Size (\d+)`).FindSubmatch(b)
	if size == nil {
		t.Fatal("no /Size")
	}
	if n, _ := strconv.Atoi(string(size[1])); n != len(entries)+1 {
		t.Fatalf("/Size %d but %d objects", n, len(entries)+1)
	}
}

// Every stream's declared /Length must match the bytes actually written, or
// readers desynchronise and render a blank page.
func TestPDFStreamLengthsMatch(t *testing.T) {
	p := NewPDF(300, 200)
	p.Text(10, 20, "hello", TextStyle{Size: 10, Color: ColInk})
	b := p.Bytes()
	re := regexp.MustCompile(`(?s)/Length (\d+)>>\nstream\n`)
	locs := re.FindAllSubmatchIndex(b, -1)
	if len(locs) == 0 {
		t.Fatal("no streams found")
	}
	for _, l := range locs {
		n, _ := strconv.Atoi(string(b[l[2]:l[3]]))
		start := l[1]
		if start+n+len("\nendstream") > len(b) {
			t.Fatal("stream runs past end of file")
		}
		if !bytes.HasPrefix(b[start+n:], []byte("\nendstream")) {
			t.Fatalf("declared /Length %d does not reach endstream", n)
		}
	}
}

func TestPDFEscapesLiteralStrings(t *testing.T) {
	if got := escapePDFString(`a(b)c\d`); got != `a\(b\)c\\d` {
		t.Fatalf("escape: %q", got)
	}
	// high bytes must become octal escapes, not raw UTF-8
	if got := escapePDFString("caf\u00e9"); got != `caf\351` {
		t.Fatalf("high byte escape: %q", got)
	}
	if got := escapePDFString("a\u4e2db"); got != "a?b" {
		t.Fatalf("unrepresentable rune: %q", got)
	}
}

// Non-Latin titles must degrade to readable Latin rather than to boxes.
func TestAsciiFold(t *testing.T) {
	cases := map[string]string{
		"Інтервальне голодування": "Intervalne holoduvannia",
		"Přerušovaný půst":        "Prerusovaný pust", // y-acute exists in WinAnsi; r-caron and u-ring do not
		"Astronomía":              "Astronomía",       // WinAnsi covers this one directly
		"plain":                   "plain",
	}
	for in, want := range cases {
		if got := asciiFold(in); got != want {
			t.Errorf("asciiFold(%q) = %q, want %q", in, got, want)
		}
	}
}

// If a real PDF checker happens to be installed, use it. Skipped otherwise so
// the suite stays hermetic.
func TestPDFAgainstExternalCheckerIfAvailable(t *testing.T) {
	bin, err := exec.LookPath("qpdf")
	if err != nil {
		t.Skip("qpdf not installed")
	}
	p := NewPDF(A4W, A4H)
	p.Text(40, 40, "check me", TextStyle{Size: 12, Color: ColInk})
	f := t.TempDir() + "/t.pdf"
	if err := writeFile(f, p.Bytes()); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "--check", f).CombinedOutput()
	if err != nil {
		t.Fatalf("qpdf --check failed: %v\n%s", err, out)
	}
}

func TestDecimatePreservesSpikesAndEndpoints(t *testing.T) {
	// A flat line with one tall spike: naive stride sampling can drop the spike,
	// which is the one feature a reader is looking for.
	pts := make([]Pt, 4000)
	for i := range pts {
		pts[i] = Pt{X: float64(i), Y: 100}
	}
	pts[1777].Y = 5 // a visual peak (y grows downward on the canvas)
	out := decimate(pts, 800)
	if len(out) > 810 {
		t.Fatalf("decimate returned %d points, expected <= ~800", len(out))
	}
	if len(out) >= len(pts) {
		t.Fatal("no reduction happened")
	}
	found := false
	minY := 1e9
	for _, p := range out {
		if p.Y < minY {
			minY = p.Y
		}
		if p.X == 1777 && p.Y == 5 {
			found = true
		}
	}
	if !found || minY != 5 {
		t.Fatalf("spike lost by decimation (minY=%v, exact point kept=%v)", minY, found)
	}
	if out[0] != pts[0] || out[len(out)-1] != pts[len(pts)-1] {
		t.Fatal("endpoints not preserved")
	}
	// x must be non-decreasing, or the line zig-zags backwards
	for i := 1; i < len(out); i++ {
		if out[i].X < out[i-1].X {
			t.Fatalf("x decreased at %d: %v -> %v", i, out[i-1], out[i])
		}
	}
	// short inputs pass through untouched
	short := pts[:100]
	if got := decimate(short, 800); len(got) != 100 {
		t.Fatalf("short input altered: %d", len(got))
	}
}
