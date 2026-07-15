package fonts

import (
	"testing"

	"golang.org/x/image/font/sfnt"
)

// TestEmbeddedFontsCoverCyrillic guards the package's core promise: every
// embedded family, in every style, maps Cyrillic (incl. Ukrainian-specific)
// letters to real glyphs. A font that lacks them - as the metric-compatible
// Caladea did - renders Cyrillic as blank .notdef boxes, so it must not be
// embedded. This is exactly the check that would have caught that regression.
func TestEmbeddedFontsCoverCyrillic(t *testing.T) {
	// A spread of Russian/Ukrainian letters, including the four Ukrainian ones
	// (ґ і ї є) that live outside the core Russian block.
	sample := []rune("АаБбЯяІіЇїЄєҐґ")
	styles := []struct {
		name         string
		bold, italic bool
	}{
		{"regular", false, false},
		{"bold", true, false},
		{"italic", false, true},
		{"bold-italic", true, true},
	}
	for _, family := range Families {
		for _, st := range styles {
			f, err := sfnt.Parse(Bytes(family, st.bold, st.italic))
			if err != nil {
				t.Fatalf("%s %s: parse: %v", family, st.name, err)
			}
			var buf sfnt.Buffer
			for _, r := range sample {
				idx, err := f.GlyphIndex(&buf, r)
				if err != nil {
					t.Fatalf("%s %s: GlyphIndex(%q): %v", family, st.name, r, err)
				}
				if idx == 0 {
					t.Errorf("%s %s: no glyph for %q (U+%04X) - font does not cover Cyrillic", family, st.name, r, r)
				}
			}
		}
	}
}
