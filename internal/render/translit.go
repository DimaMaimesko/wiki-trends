package render

import "strings"

// asciiFold converts text to something a base-14 PDF font can actually draw.
//
// The PDF one-pager uses Helvetica with WinAnsiEncoding, which covers Western
// European letters and nothing else. A report comparing Ukrainian, Czech and
// Greek Wikipedia would otherwise print boxes exactly where the article titles
// go. Two mitigations are used together: the report prefers the concept's
// English label from Wikidata wherever one exists, and anything still
// non-Latin passes through this table.
//
// The native titles are never lost — the HTML report, the SVG charts and every
// JSON artifact carry the original strings. This is a display fallback for one
// output format, applied as late as possible.
func asciiFold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 128 {
			b.WriteRune(r)
			continue
		}
		if rep, ok := foldMap[r]; ok {
			b.WriteString(rep)
			continue
		}
		if r >= 0xA0 && r <= 0xFF {
			b.WriteRune(r) // representable in WinAnsi
			continue
		}
		b.WriteByte('?')
	}
	return b.String()
}

var foldMap = map[rune]string{
	// Central/Eastern European Latin
	'ą': "a", 'Ą': "A", 'ć': "c", 'Ć': "C", 'ę': "e", 'Ę': "E", 'ł': "l", 'Ł': "L",
	'ń': "n", 'Ń': "N", 'ó': "o", 'Ó': "O", 'ś': "s", 'Ś': "S", 'ź': "z", 'Ź': "Z",
	'ż': "z", 'Ż': "Z", 'č': "c", 'Č': "C", 'ď': "d", 'Ď': "D", 'ě': "e", 'Ě': "E",
	'ň': "n", 'Ň': "N", 'ř': "r", 'Ř': "R", 'š': "s", 'Š': "S", 'ť': "t", 'Ť': "T",
	'ů': "u", 'Ů': "U", 'ž': "z", 'Ž': "Z", 'ı': "i", 'İ': "I", 'ğ': "g", 'Ğ': "G",
	'ő': "o", 'Ő': "O", 'ű': "u", 'Ű': "U", 'ā': "a", 'ē': "e", 'ī': "i", 'ū': "u",
	'ș': "s", 'Ș': "S", 'ț': "t", 'Ț': "T", 'ĺ': "l", 'ľ': "l", 'ŕ': "r", 'ĝ': "g",

	// Cyrillic (Russian, Ukrainian, Belarusian, Bulgarian, Serbian)
	'а': "a", 'б': "b", 'в': "v", 'г': "h", 'ґ': "g", 'д': "d", 'е': "e", 'є': "ie",
	'ж': "zh", 'з': "z", 'и': "y", 'і': "i", 'ї': "i", 'й': "i", 'к': "k", 'л': "l",
	'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch", 'ь': "", 'ъ': "",
	'ы': "y", 'э': "e", 'ю': "iu", 'я': "ia", 'ё': "e", 'ў': "u", 'ђ': "dj", 'ј': "j",
	'љ': "lj", 'њ': "nj", 'ћ': "c", 'џ': "dz",
	'А': "A", 'Б': "B", 'В': "V", 'Г': "H", 'Ґ': "G", 'Д': "D", 'Е': "E", 'Є': "Ye",
	'Ж': "Zh", 'З': "Z", 'И': "Y", 'І': "I", 'Ї': "Yi", 'Й': "I", 'К': "K", 'Л': "L",
	'М': "M", 'Н': "N", 'О': "O", 'П': "P", 'Р': "R", 'С': "S", 'Т': "T", 'У': "U",
	'Ф': "F", 'Х': "Kh", 'Ц': "Ts", 'Ч': "Ch", 'Ш': "Sh", 'Щ': "Shch", 'Ы': "Y",
	'Э': "E", 'Ю': "Yu", 'Я': "Ya", 'Ё': "E", 'Ў': "U",

	// Greek
	'α': "a", 'β': "b", 'γ': "g", 'δ': "d", 'ε': "e", 'ζ': "z", 'η': "i", 'θ': "th",
	'ι': "i", 'κ': "k", 'λ': "l", 'μ': "m", 'ν': "n", 'ξ': "x", 'ο': "o", 'π': "p",
	'ρ': "r", 'σ': "s", 'ς': "s", 'τ': "t", 'υ': "y", 'φ': "f", 'χ': "ch", 'ψ': "ps", 'ω': "o",

	// punctuation that WinAnsi does have but which is safer spelled out
	'—': "-", '–': "-", '’': "'", '‘': "'", '“': `"`, '”': `"`, '…': "...", ' ': " ",
}
