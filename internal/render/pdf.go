package render

import (
	"bytes"
	"fmt"
	"math"
	"strings"
)

// PDF is a minimal PDF 1.4 writer: pages, vector paths, filled shapes with
// constant alpha, and base-14 Helvetica text.
//
// Writing it by hand is what keeps the skill dependency-free. The format needs
// exactly three things done carefully — a cross-reference table whose byte
// offsets are correct to the character, a content stream whose /Length matches,
// and text escaped for the literal-string syntax. Everything else a chart needs
// is a handful of operators.
//
// Coordinates arriving here use the Canvas convention (top-left origin, y down);
// PDF's origin is bottom-left, so every y is flipped on the way out.
type PDF struct {
	W, H   float64
	pages  []*bytes.Buffer
	cur    *bytes.Buffer
	alphas []float64
}

func NewPDF(w, h float64) *PDF {
	p := &PDF{W: w, H: h, alphas: []float64{1, 0.08, 0.15, 0.25, 0.40, 0.60}}
	p.NewPage()
	return p
}

// A4Portrait in points. The report is designed to print without scaling.
const (
	A4W = 595.28
	A4H = 841.89
)

func (p *PDF) NewPage() {
	b := &bytes.Buffer{}
	p.pages = append(p.pages, b)
	p.cur = b
}

func (p *PDF) Size() (float64, float64) { return p.W, p.H }

func (p *PDF) y(v float64) float64 { return p.H - v }

func (p *PDF) setFill(c Color)   { fmt.Fprintf(p.cur, "%.3f %.3f %.3f rg\n", c.R, c.G, c.B) }
func (p *PDF) setStroke(c Color) { fmt.Fprintf(p.cur, "%.3f %.3f %.3f RG\n", c.R, c.G, c.B) }

func (p *PDF) setDash(d bool) {
	if d {
		p.cur.WriteString("[4 3] 0 d\n")
	} else {
		p.cur.WriteString("[] 0 d\n")
	}
}

func (p *PDF) Rect(x, yy, w, h float64, fill *Color, stroke *Color, sw float64) {
	if fill == nil && stroke == nil {
		return
	}
	p.cur.WriteString("q\n")
	if fill != nil {
		p.setFill(*fill)
	}
	if stroke != nil {
		p.setStroke(*stroke)
		fmt.Fprintf(p.cur, "%.2f w\n", sw)
	}
	fmt.Fprintf(p.cur, "%.2f %.2f %.2f %.2f re\n", x, p.y(yy+h), w, h)
	switch {
	case fill != nil && stroke != nil:
		p.cur.WriteString("B\n")
	case fill != nil:
		p.cur.WriteString("f\n")
	default:
		p.cur.WriteString("S\n")
	}
	p.cur.WriteString("Q\n")
}

func (p *PDF) Line(x1, y1, x2, y2 float64, col Color, w float64, dashed bool) {
	p.cur.WriteString("q\n")
	p.setStroke(col)
	p.setDash(dashed)
	fmt.Fprintf(p.cur, "%.2f w\n%.2f %.2f m %.2f %.2f l S\nQ\n", w, x1, p.y(y1), x2, p.y(y2))
}

func (p *PDF) Polyline(pts []Pt, col Color, w float64, dashed bool) {
	if len(pts) < 2 {
		return
	}
	p.cur.WriteString("q\n")
	p.setStroke(col)
	p.setDash(dashed)
	fmt.Fprintf(p.cur, "%.2f w 1 J 1 j\n", w)
	// One decimal is a third of a printer dot at 300dpi: more precision only
	// costs bytes, and a multi-year chart has thousands of vertices.
	fmt.Fprintf(p.cur, "%.1f %.1f m\n", pts[0].X, p.y(pts[0].Y))
	for _, q := range pts[1:] {
		fmt.Fprintf(p.cur, "%.1f %.1f l\n", q.X, p.y(q.Y))
	}
	p.cur.WriteString("S\nQ\n")
}

func (p *PDF) Polygon(pts []Pt, fill Color, alpha float64) {
	if len(pts) < 3 {
		return
	}
	p.cur.WriteString("q\n")
	fmt.Fprintf(p.cur, "/GS%d gs\n", p.alphaIndex(alpha))
	p.setFill(fill)
	fmt.Fprintf(p.cur, "%.2f %.2f m\n", pts[0].X, p.y(pts[0].Y))
	for _, q := range pts[1:] {
		fmt.Fprintf(p.cur, "%.2f %.2f l\n", q.X, p.y(q.Y))
	}
	p.cur.WriteString("h f\nQ\n")
}

func (p *PDF) Circle(x, yy, r float64, fill Color) {
	// four Bezier arcs; k is the standard circle-to-cubic magic constant
	const k = 0.5523
	cy := p.y(yy)
	p.cur.WriteString("q\n")
	p.setFill(fill)
	fmt.Fprintf(p.cur, "%.2f %.2f m\n", x+r, cy)
	fmt.Fprintf(p.cur, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", x+r, cy+r*k, x+r*k, cy+r, x, cy+r)
	fmt.Fprintf(p.cur, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", x-r*k, cy+r, x-r, cy+r*k, x-r, cy)
	fmt.Fprintf(p.cur, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", x-r, cy-r*k, x-r*k, cy-r, x, cy-r)
	fmt.Fprintf(p.cur, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", x+r*k, cy-r, x+r, cy-r*k, x+r, cy)
	p.cur.WriteString("f\nQ\n")
}

func (p *PDF) Text(x, yy float64, s string, st TextStyle) {
	s = asciiFold(s)
	if s == "" {
		return
	}
	font := "F1"
	if st.Bold {
		font = "F2"
	}
	lx := alignedX(x, s, st)
	p.cur.WriteString("q\n")
	p.setFill(st.Color)
	fmt.Fprintf(p.cur, "BT /%s %.2f Tf 1 0 0 1 %.2f %.2f Tm (%s) Tj ET\nQ\n",
		font, st.Size, lx, p.y(yy), escapePDFString(s))
}

func (p *PDF) TextWidth(s string, st TextStyle) float64 { return textWidth(asciiFold(s), st) }

func (p *PDF) alphaIndex(a float64) int {
	best, bd := 0, math.Inf(1)
	for i, v := range p.alphas {
		if d := math.Abs(v - a); d < bd {
			best, bd = i, d
		}
	}
	return best
}

// escapePDFString encodes a string for PDF's literal-string syntax.
//
// Two things must happen here. The characters that terminate or nest a literal
// string are escaped — missing one produces a file that opens in some readers
// and silently truncates in others, the worst possible failure mode. And every
// byte above 127 is written as an octal escape: Go strings are UTF-8, but the
// font is declared WinAnsiEncoding, which is single-byte. Passing "ý" through
// as its two UTF-8 bytes would render as two pieces of mojibake, so it is
// emitted as \375 instead.
func escapePDFString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '(':
			b.WriteString(`\(`)
		case r == ')':
			b.WriteString(`\)`)
		case r == '\r' || r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r < 32:
			// control characters have no glyph; drop them rather than emit noise
		case r < 127:
			b.WriteRune(r)
		case r <= 255:
			// WinAnsi and Latin-1 agree on this range apart from 0x80-0x9F,
			// which asciiFold has already removed.
			fmt.Fprintf(&b, "\\%03o", r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// Bytes assembles the document. Object offsets are recorded as the buffer grows
// so the xref table is exact.
func (p *PDF) Bytes() []byte {
	var out bytes.Buffer
	var offsets []int

	obj := func(body string) int {
		offsets = append(offsets, out.Len())
		n := len(offsets)
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, body)
		return n
	}
	objStream := func(dict string, data []byte) int {
		offsets = append(offsets, out.Len())
		n := len(offsets)
		fmt.Fprintf(&out, "%d 0 obj\n<<%s /Length %d>>\nstream\n", n, dict, len(data))
		out.Write(data)
		out.WriteString("\nendstream\nendobj\n")
		return n
	}

	out.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")

	// object numbers are assigned in creation order; reserve 1 and 2 for the
	// catalog and page tree by writing them last and patching? No - instead lay
	// out fonts and gstates first, then pages, then the tree, then the catalog,
	// and reference by the numbers actually returned.
	f1 := obj("<</Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding>>")
	f2 := obj("<</Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding>>")
	gs := make([]int, len(p.alphas))
	for i, a := range p.alphas {
		gs[i] = obj(fmt.Sprintf("<</Type /ExtGState /ca %.3f /CA %.3f>>", a, a))
	}
	var gsDict strings.Builder
	for i, n := range gs {
		fmt.Fprintf(&gsDict, "/GS%d %d 0 R ", i, n)
	}
	res := fmt.Sprintf("/Resources <</Font <</F1 %d 0 R /F2 %d 0 R>> /ExtGState <<%s>>>>", f1, f2, gsDict.String())

	contents := make([]int, len(p.pages))
	for i, b := range p.pages {
		contents[i] = objStream("", b.Bytes())
	}

	// The page tree object number must be known by each page; it is allocated
	// after the pages, so pages are written with a forward reference computed
	// from the count.
	pagesObjNum := len(offsets) + len(p.pages) + 1
	pageNums := make([]int, len(p.pages))
	for i := range p.pages {
		pageNums[i] = obj(fmt.Sprintf(
			"<</Type /Page /Parent %d 0 R /MediaBox [0 0 %.2f %.2f] %s /Contents %d 0 R>>",
			pagesObjNum, p.W, p.H, res, contents[i]))
	}
	var kids strings.Builder
	for _, n := range pageNums {
		fmt.Fprintf(&kids, "%d 0 R ", n)
	}
	pagesNum := obj(fmt.Sprintf("<</Type /Pages /Kids [%s] /Count %d>>", strings.TrimSpace(kids.String()), len(pageNums)))
	if pagesNum != pagesObjNum {
		panic(fmt.Sprintf("pdf: page tree object number mismatch (%d vs predicted %d)", pagesNum, pagesObjNum))
	}
	catalog := obj(fmt.Sprintf("<</Type /Catalog /Pages %d 0 R>>", pagesNum))

	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<</Size %d /Root %d 0 R>>\nstartxref\n%d\n%%%%EOF\n",
		len(offsets)+1, catalog, xref)
	return out.Bytes()
}
