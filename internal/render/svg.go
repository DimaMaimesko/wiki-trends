package render

import (
	"bytes"
	"fmt"
	"strings"
)

// SVG is the screen backend: vector, full Unicode (so Cyrillic, Greek and CJK
// article titles render natively), and directly embeddable in the HTML report.
type SVG struct {
	W, H float64
	buf  bytes.Buffer
	// Dark declares a second set of colours emitted as a CSS media query, so a
	// chart pasted into a dark-themed page does not become a white slab.
	css strings.Builder
}

func NewSVG(w, h float64) *SVG {
	s := &SVG{W: w, H: h}
	return s
}

func (s *SVG) Size() (float64, float64) { return s.W, s.H }

func (s *SVG) Rect(x, y, w, h float64, fill *Color, stroke *Color, sw float64) {
	f, st := "none", "none"
	if fill != nil {
		f = fill.Hex()
	}
	if stroke != nil {
		st = stroke.Hex()
	}
	fmt.Fprintf(&s.buf, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s" stroke="%s" stroke-width="%.2f"/>`+"\n",
		x, y, w, h, f, st, sw)
}

func (s *SVG) Line(x1, y1, x2, y2 float64, col Color, w float64, dashed bool) {
	d := ""
	if dashed {
		d = ` stroke-dasharray="4 3"`
	}
	fmt.Fprintf(&s.buf, `<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="%s" stroke-width="%.2f"%s/>`+"\n",
		x1, y1, x2, y2, col.Hex(), w, d)
}

func (s *SVG) Polyline(pts []Pt, col Color, w float64, dashed bool) {
	if len(pts) < 2 {
		return
	}
	d := ""
	if dashed {
		d = ` stroke-dasharray="4 3"`
	}
	fmt.Fprintf(&s.buf, `<polyline fill="none" stroke="%s" stroke-width="%.2f" stroke-linejoin="round" stroke-linecap="round"%s points="%s"/>`+"\n",
		col.Hex(), w, d, points(pts))
}

func (s *SVG) Polygon(pts []Pt, fill Color, alpha float64) {
	if len(pts) < 3 {
		return
	}
	fmt.Fprintf(&s.buf, `<polygon fill="%s" fill-opacity="%.3f" stroke="none" points="%s"/>`+"\n",
		fill.Hex(), alpha, points(pts))
}

func (s *SVG) Circle(x, y, r float64, fill Color) {
	fmt.Fprintf(&s.buf, `<circle cx="%.2f" cy="%.2f" r="%.2f" fill="%s"/>`+"\n", x, y, r, fill.Hex())
}

func (s *SVG) Text(x, y float64, str string, st TextStyle) {
	anchor := "start"
	switch st.Align {
	case AlignMiddle:
		anchor = "middle"
	case AlignEnd:
		anchor = "end"
	}
	weight := "400"
	if st.Bold {
		weight = "700"
	}
	fmt.Fprintf(&s.buf, `<text x="%.2f" y="%.2f" font-family="Helvetica, Arial, sans-serif" font-size="%.2f" font-weight="%s" fill="%s" text-anchor="%s">%s</text>`+"\n",
		x, y, st.Size, weight, st.Color.Hex(), anchor, escapeXML(str))
}

func (s *SVG) TextWidth(str string, st TextStyle) float64 { return textWidth(str, st) }

// Bytes renders the standalone SVG document.
func (s *SVG) Bytes() []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" role="img">`+"\n", s.W, s.H, s.W, s.H)
	out.WriteString(s.css.String())
	out.Write(s.buf.Bytes())
	out.WriteString("</svg>\n")
	return out.Bytes()
}

// Inner returns the drawing commands without the <svg> wrapper, for inlining
// several charts into one HTML page.
func (s *SVG) Inner() string { return s.buf.String() }

func points(pts []Pt) string {
	var b strings.Builder
	for i, p := range pts {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", p.X, p.Y)
	}
	return b.String()
}

func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
