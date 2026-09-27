// Package render draws charts and reports with no external dependencies.
//
// Charts are described once against the Canvas interface and emitted by two
// backends: SVG for screen and HTML (full Unicode, vector, theme-aware) and PDF
// for the shareable one-pager. Writing both by hand rather than pulling in a
// plotting library is what lets the whole skill run with `go run` and nothing
// else installed — no module downloads, no vendor tree, no build step.
//
// Coordinates are top-left origin with y increasing downwards, in points
// (1/72 inch), matching SVG. The PDF backend flips the axis on output.
package render

import (
	"fmt"
	"math"
	"strings"
)

type Color struct{ R, G, B float64 }

func hex(s string) Color {
	var r, g, b int
	fmt.Sscanf(strings.TrimPrefix(s, "#"), "%02x%02x%02x", &r, &g, &b)
	return Color{float64(r) / 255, float64(g) / 255, float64(b) / 255}
}

func (c Color) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", clamp255(c.R), clamp255(c.G), clamp255(c.B))
}

func clamp255(v float64) int {
	i := int(math.Round(v * 255))
	if i < 0 {
		return 0
	}
	if i > 255 {
		return 255
	}
	return i
}

// Palette is a small categorical set chosen to stay distinguishable in
// grayscale print and for the most common colour-vision deficiencies: hue and
// lightness both vary between neighbours, so a reader who cannot separate the
// red from the green can still separate dark from light.
var Palette = []Color{
	hex("#1f5fa9"), // blue
	hex("#c2571a"), // orange
	hex("#2f7d52"), // green
	hex("#8c3a6b"), // magenta
	hex("#7a6a1f"), // olive
	hex("#3d6f8e"), // steel
	hex("#a6342c"), // brick
	hex("#5a4b8c"), // violet
}

var (
	ColInk   = hex("#1a1a1a")
	ColMuted = hex("#6b7280")
	ColGrid  = hex("#e3e6ea")
	ColAxis  = hex("#9aa2ab")
	ColPaper = hex("#ffffff")
	ColPanel = hex("#f7f8fa")
	ColWarn  = hex("#c2571a")
	ColPos   = hex("#2f7d52")
	ColNeg   = hex("#a6342c")
)

type Align int

const (
	AlignStart Align = iota
	AlignMiddle
	AlignEnd
)

type TextStyle struct {
	Size  float64
	Bold  bool
	Color Color
	Align Align
}

type Pt struct{ X, Y float64 }

// Canvas is the drawing surface both backends implement. It is deliberately
// tiny: everything a statistical chart needs and nothing else.
type Canvas interface {
	Size() (w, h float64)
	Rect(x, y, w, h float64, fill *Color, stroke *Color, strokeWidth float64)
	Line(x1, y1, x2, y2 float64, col Color, width float64, dashed bool)
	Polyline(pts []Pt, col Color, width float64, dashed bool)
	Polygon(pts []Pt, fill Color, alpha float64)
	Circle(x, y, r float64, fill Color)
	Text(x, y float64, s string, st TextStyle)
	TextWidth(s string, st TextStyle) float64
}

// --- shared text metrics (Helvetica / Helvetica-Bold, from the base-14 AFMs)

var helvW = [95]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
}

var helvBoldW = [95]int{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
	975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
	333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
	611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584,
}

// textWidth measures a string in points. Characters outside the base-14 range
// fall back to the average advance, which keeps layout stable even when a
// backend renders glyphs this table does not describe.
func textWidth(s string, st TextStyle) float64 {
	tbl := &helvW
	if st.Bold {
		tbl = &helvBoldW
	}
	total := 0.0
	for _, r := range s {
		switch {
		case r >= 32 && r < 127:
			total += float64(tbl[r-32])
		default:
			total += 556
		}
	}
	return total / 1000 * st.Size
}

// alignedX converts a logical anchor into a left edge for backends that can only
// draw text from the left (PDF).
func alignedX(x float64, s string, st TextStyle) float64 {
	switch st.Align {
	case AlignMiddle:
		return x - textWidth(s, st)/2
	case AlignEnd:
		return x - textWidth(s, st)
	}
	return x
}

// Ellipsis shortens a label to fit a width, so a long article title degrades
// gracefully instead of colliding with the next column.
func Ellipsis(s string, st TextStyle, max float64) string {
	if textWidth(s, st) <= max {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		if textWidth(string(r)+"…", st) <= max {
			return string(r) + "…"
		}
	}
	return ""
}

// wrapText greedily breaks a string into lines that fit a width. Used for
// verdicts and limitation notes, which are prose and must not be truncated.
func wrapText(s string, st TextStyle, width float64) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		if textWidth(cur+" "+w, st) <= width {
			cur += " " + w
			continue
		}
		lines = append(lines, cur)
		cur = w
	}
	return append(lines, cur)
}
