package document_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/h0rn3t/docx2pdf/document"
	"github.com/h0rn3t/docx2pdf/internal/docxtest"
)

// redTheme is a color scheme whose accent1 is pure red, so a tint/shade of it
// has an exactly predictable result.
const redTheme = `<a:accent1><a:srgbClr val="FF0000"/></a:accent1>`

// --- Theme color tint and shade (w:themeTint / w:themeShade) ---

func TestThemeColor_TintLightensTowardWhite(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:color w:themeColor="accent1" w:themeTint="99"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, map[string]string{"word/theme/theme1.xml": docxtest.Theme(redTheme)})

	// 0x99/255 = 0.6 of the color's luminance plus 0.4 toward white: Word's
	// "Lighter 40%" of red.
	if got := paragraphs(t, doc)[0].Runs[0].Props.Color; got != "#FF6666" {
		t.Fatalf("tinted theme color = %q, want #FF6666", got)
	}
}

func TestThemeColor_ShadeDarkensTowardBlack(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:color w:themeColor="accent1" w:themeShade="BF"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, map[string]string{"word/theme/theme1.xml": docxtest.Theme(redTheme)})

	// 0xBF/255 = 0.749 of the luminance: Word's "Darker 25%" of red.
	if got := paragraphs(t, doc)[0].Runs[0].Props.Color; got != "#BF0000" {
		t.Fatalf("shaded theme color = %q, want #BF0000", got)
	}
}

func TestThemeColor_ExplicitHexIgnoresTint(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:color w:val="112233" w:themeTint="99"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, map[string]string{"word/theme/theme1.xml": docxtest.Theme(redTheme)})

	if got := paragraphs(t, doc)[0].Runs[0].Props.Color; got != "#112233" {
		t.Fatalf("color = %q, want #112233 (an explicit value is not a theme color to tint)", got)
	}
}

// --- Theme fonts (w:rFonts/@w:asciiTheme) ---

const fontTheme = `<a:dk1><a:srgbClr val="000000"/></a:dk1>`

// themeWithFonts is a theme part carrying both a color scheme and a font scheme.
func themeWithFonts(t *testing.T) string {
	t.Helper()
	theme := docxtest.Theme(fontTheme)
	fonts := `<a:fontScheme name="Test">
		<a:majorFont><a:latin typeface="Georgia"/></a:majorFont>
		<a:minorFont><a:latin typeface="Verdana"/></a:minorFont>
	</a:fontScheme>`
	// Insert the font scheme as a sibling of the color scheme.
	return strings.Replace(theme, "</a:themeElements>", fonts+"</a:themeElements>", 1)
}

func TestThemeFont_MinorAndMajorResolveFromFontScheme(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:rFonts w:asciiTheme="minorHAnsi"/></w:rPr><w:t>body</w:t></w:r>` +
		`<w:r><w:rPr><w:rFonts w:asciiTheme="majorHAnsi"/></w:rPr><w:t>heading</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, map[string]string{"word/theme/theme1.xml": themeWithFonts(t)})

	runs := paragraphs(t, doc)[0].Runs
	if got := runs[0].Props.FontFamily; got != "Verdana" {
		t.Errorf("minorHAnsi font = %q, want Verdana", got)
	}
	if got := runs[1].Props.FontFamily; got != "Georgia" {
		t.Errorf("majorHAnsi font = %q, want Georgia", got)
	}
}

func TestThemeFont_ExplicitTypefaceWins(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:rFonts w:ascii="Arial" w:asciiTheme="minorHAnsi"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, map[string]string{"word/theme/theme1.xml": themeWithFonts(t)})

	if got := paragraphs(t, doc)[0].Runs[0].Props.FontFamily; got != "Arial" {
		t.Fatalf("font = %q, want Arial (an explicit typeface wins over the theme reference)", got)
	}
}

func TestThemeFont_DocDefaultsReachEveryRun(t *testing.T) {
	styles := docxtest.Styles(`<w:docDefaults><w:rPrDefault><w:rPr>` +
		`<w:rFonts w:asciiTheme="minorHAnsi"/></w:rPr></w:rPrDefault></w:docDefaults>`)
	doc := mustOpenWith(t, `<w:p><w:r><w:t>x</w:t></w:r></w:p>`, map[string]string{
		"word/styles.xml":       styles,
		"word/theme/theme1.xml": themeWithFonts(t),
	})

	if got := paragraphs(t, doc)[0].Runs[0].Props.FontFamily; got != "Verdana" {
		t.Fatalf("font = %q, want Verdana (docDefaults theme font)", got)
	}
}

// --- Run and paragraph shading (w:shd) and highlighting (w:highlight) ---

func TestRunShading_FillIsTheBackground(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:shd w:val="clear" w:color="auto" w:fill="FFFF00"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, nil)

	if got := paragraphs(t, doc)[0].Runs[0].Props.Shading; got != "#FFFF00" {
		t.Fatalf("run shading = %q, want #FFFF00", got)
	}
}

func TestRunShading_PercentPatternBlendsForegroundOverFill(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:shd w:val="pct50" w:color="000000" w:fill="FFFFFF"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, nil)

	if got := paragraphs(t, doc)[0].Runs[0].Props.Shading; got != "#808080" {
		t.Fatalf("pct50 black-on-white shading = %q, want #808080", got)
	}
}

func TestRunShading_ThemeFillWithTint(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:shd w:val="clear" w:themeFill="accent1" w:themeFillTint="99"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, map[string]string{"word/theme/theme1.xml": docxtest.Theme(redTheme)})

	if got := paragraphs(t, doc)[0].Runs[0].Props.Shading; got != "#FF6666" {
		t.Fatalf("theme fill with tint = %q, want #FF6666", got)
	}
}

func TestRunShading_NilPatternPaintsNothing(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:shd w:val="nil" w:fill="FFFF00"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, nil)

	if got := paragraphs(t, doc)[0].Runs[0].Props.Shading; got != "" {
		t.Fatalf("shading = %q, want empty for w:val=nil", got)
	}
}

func TestHighlight_NamedColorWinsOverShading(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:shd w:val="clear" w:fill="00FF00"/><w:highlight w:val="yellow"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, nil)

	if got := paragraphs(t, doc)[0].Runs[0].Props.Shading; got != "#FFFF00" {
		t.Fatalf("shading = %q, want #FFFF00 (the highlight paints over the shading)", got)
	}
}

func TestHighlight_NoneFallsBackToShading(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:shd w:val="clear" w:fill="00FF00"/><w:highlight w:val="none"/></w:rPr><w:t>x</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, nil)

	if got := paragraphs(t, doc)[0].Runs[0].Props.Shading; got != "#00FF00" {
		t.Fatalf("shading = %q, want #00FF00 (highlight none leaves the shading visible)", got)
	}
}

func TestParagraphShading_FromStyle(t *testing.T) {
	styles := docxtest.Styles(`<w:style w:type="paragraph" w:styleId="S"><w:pPr>` +
		`<w:shd w:val="clear" w:fill="EEEEEE"/></w:pPr></w:style>`)
	doc := mustOpenWith(t, `<w:p><w:pPr><w:pStyle w:val="S"/></w:pPr><w:r><w:t>x</w:t></w:r></w:p>`,
		map[string]string{"word/styles.xml": styles})

	if got := paragraphs(t, doc)[0].Props.Shading; got != "#EEEEEE" {
		t.Fatalf("paragraph shading = %q, want #EEEEEE (from the paragraph style)", got)
	}
}

// --- Numbering: legal numbering (w:isLgl) and the suffix (w:suff) ---

// romanThenLegal is a list whose first level is upper-roman and whose second
// level is a legal one: its %1 placeholder must render as a decimal.
const romanThenLegal = `
	<w:abstractNum w:abstractNumId="0">
		<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="upperRoman"/><w:lvlText w:val="%1."/></w:lvl>
		<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1.%2"/><w:isLgl/></w:lvl>
	</w:abstractNum>
	<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`

func TestNumbering_IsLglRendersEveryLevelAsDecimal(t *testing.T) {
	item := func(ilvl string) string {
		return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + ilvl + `"/><w:numId w:val="1"/></w:numPr></w:pPr>` +
			`<w:r><w:t>x</w:t></w:r></w:p>`
	}
	doc := mustOpenWith(t, item("0")+item("1"), map[string]string{
		"word/numbering.xml": docxtest.Numbering(romanThenLegal),
	})

	ps := paragraphs(t, doc)
	if got := ps[0].Runs[0].Text; got != "I." {
		t.Fatalf("level 0 marker = %q, want I. (upper roman)", got)
	}
	if got := ps[1].Runs[0].Text; got != "1.1" {
		t.Fatalf("legal level marker = %q, want 1.1 (isLgl renders level 0 as a decimal)", got)
	}
}

// suffixNumbering is a decimal list whose suffix mode is set per test.
func suffixNumbering(suff string) string {
	return `<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0">` +
		`<w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/>` + suff +
		`<w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum>` +
		`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`
}

func TestNumbering_SuffixModes(t *testing.T) {
	tests := []struct {
		name       string
		suff       string
		wantText   string
		wantMinPt  float64
		wantReason string
	}{
		{
			name: "tab (default)", suff: "", wantText: "1.", wantMinPt: 18,
			wantReason: "the default tab suffix reserves the level's hanging indent (360 dxa = 18 pt)",
		},
		{
			name: "explicit tab", suff: `<w:suff w:val="tab"/>`, wantText: "1.", wantMinPt: 18,
			wantReason: "an explicit tab behaves like the default",
		},
		{
			name: "space", suff: `<w:suff w:val="space"/>`, wantText: "1. ", wantMinPt: 0,
			wantReason: "a space suffix is one plain space, with no reserved advance",
		},
		{
			name: "nothing", suff: `<w:suff w:val="nothing"/>`, wantText: "1.", wantMinPt: 0,
			wantReason: "a nothing suffix puts the text straight after the marker",
		},
	}
	body := `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>x</w:t></w:r></w:p>`
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustOpenWith(t, body, map[string]string{
				"word/numbering.xml": docxtest.Numbering(suffixNumbering(tt.suff)),
			})
			marker := paragraphs(t, doc)[0].Runs[0]
			if marker.Text != tt.wantText {
				t.Errorf("marker text = %q, want %q", marker.Text, tt.wantText)
			}
			if marker.MinWidthPt != tt.wantMinPt {
				t.Errorf("marker MinWidthPt = %.2f, want %.2f (%s)", marker.MinWidthPt, tt.wantMinPt, tt.wantReason)
			}
		})
	}
}

// --- Pictures: inline images and picture bullets ---

// imageRels is the document part's relationship to the media blob.
func imageRels(id string) string {
	return docxtest.Rels(docxtest.Rel(id, "image", "media/image1.png"))
}

// inlineDrawing is a run containing a wp:inline picture of cx × cy EMU.
func inlineDrawing(rID string, cx, cy int) string {
	return `<w:p><w:r><w:drawing>
		<wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">
			<wp:extent cx="` + itoaN(cx) + `" cy="` + itoaN(cy) + `"/>
			<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
				<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">
					<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">
						<pic:blipFill>
							<a:blip xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" r:embed="` + rID + `"/>
						</pic:blipFill>
					</pic:pic>
				</a:graphicData>
			</a:graphic>
		</wp:inline>
	</w:drawing></w:r></w:p>`
}

func TestImage_InlineDrawingBecomesAnImageRun(t *testing.T) {
	doc := mustOpenWith(t, inlineDrawing("rId5", 914400, 457200), map[string]string{
		"word/_rels/document.xml.rels": imageRels("rId5"),
		"word/media/image1.png":        docxtest.PNG(t, 8, 4),
	})

	img := paragraphs(t, doc)[0].Runs[0].Image
	if img == nil {
		t.Fatal("expected an image run for the inline w:drawing")
	}
	// 914400 EMU is one inch: 72 pt wide, 36 pt tall.
	if img.WidthPt != 72 || img.HeightPt != 36 {
		t.Errorf("image size = %.1f×%.1f pt, want 72×36 (from wp:extent)", img.WidthPt, img.HeightPt)
	}
	if img.Type != "PNG" || len(img.Data) == 0 {
		t.Errorf("image = {Type:%q, %d bytes}, want a non-empty PNG", img.Type, len(img.Data))
	}
}

func TestImage_MissingExtentFallsBackToIntrinsicSize(t *testing.T) {
	doc := mustOpenWith(t, inlineDrawing("rId5", 0, 0), map[string]string{
		"word/_rels/document.xml.rels": imageRels("rId5"),
		"word/media/image1.png":        docxtest.PNG(t, 96, 48),
	})

	img := paragraphs(t, doc)[0].Runs[0].Image
	if img == nil {
		t.Fatal("expected an image run")
	}
	// 96×48 px at 96 dpi is one inch by half an inch.
	if img.WidthPt != 72 || img.HeightPt != 36 {
		t.Fatalf("image size = %.1f×%.1f pt, want 72×36 (intrinsic pixels at 96 dpi)", img.WidthPt, img.HeightPt)
	}
}

func TestImage_UnsupportedFormatIsSkipped(t *testing.T) {
	doc := mustOpenWith(t, inlineDrawing("rId5", 914400, 914400), map[string]string{
		"word/_rels/document.xml.rels": docxtest.Rels(docxtest.Rel("rId5", "image", "media/drawing.emf")),
		"word/media/drawing.emf":       "not an embeddable raster",
	})

	if runs := paragraphs(t, doc)[0].Runs; len(runs) != 0 {
		t.Fatalf("expected the EMF picture to be dropped, got %d run(s)", len(runs))
	}
}

const picBulletNumbering = `
	<w:numPicBullet w:numPicBulletId="0">
		<w:pict>
			<v:shape xmlns:v="urn:schemas-microsoft-com:vml" style="width:9pt;height:9pt">
				<v:imagedata xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" r:id="rId1"/>
			</v:shape>
		</w:pict>
	</w:numPicBullet>
	<w:abstractNum w:abstractNumId="0">
		<w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val=""/><w:lvlPicBulletId w:val="0"/>
			<w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl>
	</w:abstractNum>
	<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`

func TestNumbering_PictureBulletMarkerIsAnImage(t *testing.T) {
	body := `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>item</w:t></w:r></w:p>`
	doc := mustOpenWith(t, body, map[string]string{
		"word/numbering.xml":            docxtest.Numbering(picBulletNumbering),
		"word/_rels/numbering.xml.rels": imageRels("rId1"),
		"word/media/image1.png":         docxtest.PNG(t, 12, 12),
	})

	runs := paragraphs(t, doc)[0].Runs
	marker := runs[0]
	if marker.Image == nil {
		t.Fatal("expected the picture bullet to be an image marker run")
	}
	if marker.Image.WidthPt != 9 || marker.Image.HeightPt != 9 {
		t.Errorf("bullet size = %.1f×%.1f pt, want 9×9 (from the VML shape style)", marker.Image.WidthPt, marker.Image.HeightPt)
	}
	if marker.Text != "" {
		t.Errorf("picture-bullet marker text = %q, want empty", marker.Text)
	}
	if runs[1].Text != "item" {
		t.Errorf("second run = %q, want the paragraph's own text", runs[1].Text)
	}
}

// --- Sections, columns, headers and footers ---

// sectPr builds a body-level section with the given children.
func sectPr(inner string) string {
	return `<w:sectPr xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` + inner + `</w:sectPr>`
}

func TestSections_ParagraphSectPrEndsASection(t *testing.T) {
	body := `<w:p><w:pPr>` + sectPr(`<w:pgSz w:w="11906" w:h="16838"/>`) + `</w:pPr><w:r><w:t>first</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>second</w:t></w:r></w:p>` +
		sectPr(`<w:pgSz w:w="16838" w:h="11906"/>`) // landscape A4 for the last section
	doc := mustOpenWith(t, body, nil)

	if len(doc.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(doc.Sections))
	}
	if doc.Sections[0].End != 1 {
		t.Errorf("first section End = %d, want 1 (it ends after its own paragraph)", doc.Sections[0].End)
	}
	if w := doc.Sections[0].Geometry.WidthPt; w > doc.Sections[0].Geometry.HeightPt {
		t.Errorf("first section is landscape (%.0f wide), want portrait", w)
	}
	if w, h := doc.Sections[1].Geometry.WidthPt, doc.Sections[1].Geometry.HeightPt; w < h {
		t.Errorf("second section = %.0f×%.0f, want landscape (wider than tall)", w, h)
	}
	if doc.Geometry != doc.Sections[0].Geometry {
		t.Error("Document.Geometry must stay the first section's geometry")
	}
}

func TestSections_EqualWidthColumns(t *testing.T) {
	body := `<w:p><w:r><w:t>x</w:t></w:r></w:p>` +
		sectPr(`<w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/>`+
			`<w:cols w:num="2" w:space="720"/>`)
	doc := mustOpenWith(t, body, nil)

	cols := doc.Sections[0].Columns
	if len(cols) != 2 {
		t.Fatalf("columns = %d, want 2", len(cols))
	}
	// Content width is 6.5" = 468 pt; two columns split it around a 36 pt gap.
	if want := (468.0 - 36) / 2; cols[0].WidthPt != want {
		t.Errorf("column width = %.2f, want %.2f", cols[0].WidthPt, want)
	}
	if cols[0].SpaceAfterPt != 36 {
		t.Errorf("column gap = %.2f, want 36 (720 dxa)", cols[0].SpaceAfterPt)
	}
	if cols[1].SpaceAfterPt != 0 {
		t.Errorf("last column gap = %.2f, want 0", cols[1].SpaceAfterPt)
	}
}

func TestSections_ColumnSeparatorFlag(t *testing.T) {
	with := `<w:cols w:num="2" w:space="720" w:sep="1"/>`
	without := `<w:cols w:num="2" w:space="720"/>`
	if s := mustOpenWith(t, `<w:p/>`+sectPr(with), nil).Sections[0]; !s.Separator {
		t.Error("w:sep=\"1\" must set Section.Separator")
	}
	if s := mustOpenWith(t, `<w:p/>`+sectPr(without), nil).Sections[0]; s.Separator {
		t.Error("no w:sep must leave Section.Separator false")
	}
}

func TestSections_SingleFullWidthColumnByDefault(t *testing.T) {
	doc := mustOpenWith(t, `<w:p><w:r><w:t>x</w:t></w:r></w:p>`, nil)

	cols := doc.Sections[0].Columns
	g := doc.Sections[0].Geometry
	if len(cols) != 1 {
		t.Fatalf("columns = %d, want 1 for a section with no w:cols", len(cols))
	}
	if want := g.WidthPt - g.MarginLeftPt - g.MarginRightPt; cols[0].WidthPt != want {
		t.Fatalf("column width = %.2f, want the full content width %.2f", cols[0].WidthPt, want)
	}
}

func TestHeaderFooter_ContentIsLoadedFromItsPart(t *testing.T) {
	body := `<w:p><w:r><w:t>body</w:t></w:r></w:p>` +
		sectPr(`<w:headerReference w:type="default" r:id="rId10"/>`+
			`<w:footerReference w:type="default" r:id="rId11"/>`+
			`<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="576"/>`)
	doc := mustOpenWith(t, body, map[string]string{
		"word/_rels/document.xml.rels": docxtest.Rels(
			docxtest.Rel("rId10", "header", "header1.xml") + docxtest.Rel("rId11", "footer", "footer1.xml")),
		"word/header1.xml": docxtest.Header(`<w:p><w:r><w:t>Page header</w:t></w:r></w:p>`),
		"word/footer1.xml": docxtest.Footer(`<w:p><w:r><w:t>Page footer</w:t></w:r></w:p>`),
	})

	s := doc.Sections[0]
	if len(s.Header) != 1 || s.Header[0].Paragraph.Runs[0].Text != "Page header" {
		t.Fatalf("header content = %+v, want one paragraph reading %q", s.Header, "Page header")
	}
	if len(s.Footer) != 1 || s.Footer[0].Paragraph.Runs[0].Text != "Page footer" {
		t.Fatalf("footer content = %+v, want one paragraph reading %q", s.Footer, "Page footer")
	}
	if s.HeaderOffsetPt != 36 || s.FooterOffsetPt != 28.8 {
		t.Errorf("header/footer offsets = %.1f/%.1f pt, want 36/28.8 (720/576 dxa)", s.HeaderOffsetPt, s.FooterOffsetPt)
	}
}

func TestHeaderFooter_MissingPartLeavesTheSectionBare(t *testing.T) {
	body := `<w:p><w:r><w:t>body</w:t></w:r></w:p>` + sectPr(`<w:headerReference w:type="default" r:id="rId99"/>`)
	doc := mustOpenWith(t, body, nil) // no rels, no header part

	if got := doc.Sections[0].Header; got != nil {
		t.Fatalf("header = %+v, want none when the referenced part is missing", got)
	}
}

func TestHeaderFooter_NumberedListDoesNotAdvanceTheBodyCounter(t *testing.T) {
	item := `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>x</w:t></w:r></w:p>`
	body := item + item + sectPr(`<w:headerReference w:type="default" r:id="rId10"/>`)
	doc := mustOpenWith(t, body, map[string]string{
		"word/numbering.xml":           docxtest.Numbering(simpleNumbering),
		"word/_rels/document.xml.rels": docxtest.Rels(docxtest.Rel("rId10", "header", "header1.xml")),
		"word/header1.xml":             docxtest.Header(item),
	})

	ps := paragraphs(t, doc)
	if got := ps[0].Runs[0].Text; got != "1." {
		t.Errorf("first body marker = %q, want 1.", got)
	}
	if got := ps[1].Runs[0].Text; got != "2." {
		t.Errorf("second body marker = %q, want 2. (the header's list must not consume a number)", got)
	}
	hdr := doc.Sections[0].Header
	if got := hdr[0].Paragraph.Runs[0].Text; got != "1." {
		t.Errorf("header marker = %q, want 1. (the header's list counts on its own)", got)
	}
}

// --- End to end ---

// TestConvertEverythingEndToEnd renders one document exercising each new
// feature at once and checks the PDF carries them: the text, an embedded image,
// and filled shading rectangles.
func TestConvertEverythingEndToEnd(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:highlight w:val="yellow"/></w:rPr><w:t>Виділений текст</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:shd w:val="clear" w:fill="EEEEEE"/></w:pPr><w:r><w:t>Абзац із заливкою</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Пункт списку</w:t></w:r></w:p>` +
		inlineDrawing("rId5", 914400, 457200) +
		`<w:p><w:r><w:rPr><w:color w:themeColor="accent1" w:themeShade="BF"/></w:rPr><w:t>Темний акцент</w:t></w:r></w:p>` +
		sectPr(`<w:headerReference w:type="default" r:id="rId10"/><w:cols w:num="2" w:space="720"/>`)

	doc := mustOpenWith(t, body, map[string]string{
		"word/theme/theme1.xml":        docxtest.Theme(redTheme),
		"word/numbering.xml":           docxtest.Numbering(simpleNumbering),
		"word/_rels/document.xml.rels": docxtest.Rels(docxtest.Rel("rId5", "image", "media/image1.png") + docxtest.Rel("rId10", "header", "header1.xml")),
		"word/media/image1.png":        docxtest.PNG(t, 8, 4),
		"word/header1.xml":             docxtest.Header(`<w:p><w:r><w:t>Колонтитул</w:t></w:r></w:p>`),
	})

	var buf bytes.Buffer
	if err := document.ConvertToPdf(doc).Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw := buf.Bytes()
	if !bytes.HasPrefix(raw, []byte("%PDF-")) {
		t.Fatal("output is not a PDF")
	}
	text := decodePDFText(raw)
	for _, want := range []string{"Виділений", "заливкою", "Пункт", "акцент", "Колонтитул"} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered text is missing %q", want)
		}
	}
	if !bytes.Contains(raw, []byte("/Image")) {
		t.Error("expected the embedded picture as an image XObject in the PDF")
	}
	if content := decodePDFStreams(raw); !bytes.Contains(content, []byte(" rg")) {
		t.Error("expected a filled shading rectangle in the page content")
	}
}

// itoaN renders n for the EMU attributes in the picture fixtures.
func itoaN(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
