// Package fonts embeds the Liberation Fonts family (Sans, Serif, Mono; each
// with Regular/Bold/Italic/BoldItalic variants) so convert can render Unicode
// text - Cyrillic included - without relying on the PDF core fonts, which are
// Latin-only. Liberation Fonts are SIL OFL 1.1 licensed (see
// LICENSE-liberation-fonts.txt) and metric-compatible with Arial/Times New
// Roman/Courier New, the fonts Word documents most often declare.
package fonts

import _ "embed"

//go:embed LiberationSans-Regular.ttf
var sansRegular []byte

//go:embed LiberationSans-Bold.ttf
var sansBold []byte

//go:embed LiberationSans-Italic.ttf
var sansItalic []byte

//go:embed LiberationSans-BoldItalic.ttf
var sansBoldItalic []byte

//go:embed LiberationSerif-Regular.ttf
var serifRegular []byte

//go:embed LiberationSerif-Bold.ttf
var serifBold []byte

//go:embed LiberationSerif-Italic.ttf
var serifItalic []byte

//go:embed LiberationSerif-BoldItalic.ttf
var serifBoldItalic []byte

//go:embed LiberationMono-Regular.ttf
var monoRegular []byte

//go:embed LiberationMono-Bold.ttf
var monoBold []byte

//go:embed LiberationMono-Italic.ttf
var monoItalic []byte

//go:embed LiberationMono-BoldItalic.ttf
var monoBoldItalic []byte

// Sans, Serif, and Mono are the embedded font family names, matching the
// names AddUTF8FontFromBytes is registered under.
const (
	Sans  = "Liberation Sans"
	Serif = "Liberation Serif"
	Mono  = "Liberation Mono"
)

// Families lists every embedded family name.
var Families = []string{Sans, Serif, Mono}

// Bytes returns the embedded TTF bytes for family's bold/italic variant.
// It panics if family is not one of Sans, Serif, or Mono.
func Bytes(family string, bold, italic bool) []byte {
	switch family {
	case Sans:
		return pick(sansRegular, sansBold, sansItalic, sansBoldItalic, bold, italic)
	case Serif:
		return pick(serifRegular, serifBold, serifItalic, serifBoldItalic, bold, italic)
	case Mono:
		return pick(monoRegular, monoBold, monoItalic, monoBoldItalic, bold, italic)
	default:
		panic("docx2pdf: unknown embedded font family " + family)
	}
}

func pick(regular, b, i, bi []byte, bold, italic bool) []byte {
	switch {
	case bold && italic:
		return bi
	case bold:
		return b
	case italic:
		return i
	default:
		return regular
	}
}
