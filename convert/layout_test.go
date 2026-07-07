package convert

import (
	"io"
	"strings"
	"testing"

	"github.com/h0rn3t/goffice/document"
)

// Default page frame the tests assert against: the A4/one-inch geometry a
// document resolves to when it declares no w:sectPr. Derived from the single
// source (document.DefaultPageGeometry) so it can't drift.
var (
	testGeometry   = document.DefaultPageGeometry()
	testPage       = pageFrom(testGeometry)
	marginPt       = testGeometry.MarginTopPt
	contentWidthPt = testGeometry.WidthPt - testGeometry.MarginLeftPt - testGeometry.MarginRightPt
)

// fakeRenderer is a deterministic renderer: every glyph is half the point size
// wide, so wrapping and pagination are exactly predictable in tests.
type fakeRenderer struct {
	page    int
	bold    bool
	size    float64
	draws   []drawCall
	fills   []fillCall
	strokes []strokeCall
}

type drawCall struct {
	page int
	x, y float64
	text string
	size float64
	bold bool
}

type fillCall struct {
	page       int
	x, y, w, h float64
	color      string
}

type strokeCall struct {
	page                    int
	x1, y1, x2, y2, widthPt float64
	color                   string
}

func (f *fakeRenderer) SetFont(_ string, bold, _, _ bool, sizePt float64) {
	f.bold, f.size = bold, sizePt
}
func (f *fakeRenderer) TextWidth(s string) float64 {
	return float64(len([]rune(s))) * f.size * 0.5
}
func (f *fakeRenderer) AddPage() { f.page++ }
func (f *fakeRenderer) DrawText(x, y float64, s string) {
	f.draws = append(f.draws, drawCall{page: f.page, x: x, y: y, text: s, size: f.size, bold: f.bold})
}
func (f *fakeRenderer) FillRect(x, y, w, h float64, colorHex string) {
	f.fills = append(f.fills, fillCall{page: f.page, x: x, y: y, w: w, h: h, color: colorHex})
}
func (f *fakeRenderer) StrokeLine(x1, y1, x2, y2, widthPt float64, colorHex string) {
	f.strokes = append(f.strokes, strokeCall{page: f.page, x1: x1, y1: y1, x2: x2, y2: y2, widthPt: widthPt, color: colorHex})
}
func (f *fakeRenderer) Output(io.Writer) error { return nil }

func run(text string, size float64) document.Run {
	return document.Run{Text: text, Props: document.RunProperties{FontFamily: "Helvetica", SizePt: size}}
}

func para(align document.Alignment, pageBreak bool, runs ...document.Run) document.Paragraph {
	return document.Paragraph{Runs: runs, Props: document.ParagraphProperties{Alignment: align, PageBreak: pageBreak}}
}

func paraWithIndent(align document.Alignment, indent document.Indent, runs ...document.Run) document.Paragraph {
	return document.Paragraph{Runs: runs, Props: document.ParagraphProperties{Alignment: align, Indent: indent}}
}

func paraWithSpacing(align document.Alignment, spacing document.Spacing, runs ...document.Run) document.Paragraph {
	return document.Paragraph{Runs: runs, Props: document.ParagraphProperties{Alignment: align, Spacing: spacing}}
}

// bodyOf wraps paragraphs into a Document.Body, one BodyElement each.
func bodyOf(paras ...document.Paragraph) []document.BodyElement {
	body := make([]document.BodyElement, len(paras))
	for i := range paras {
		body[i] = document.BodyElement{Paragraph: &paras[i]}
	}
	return body
}

func TestLayout_LongParagraphWraps(t *testing.T) {
	text := strings.TrimSpace(strings.Repeat("word ", 200))
	lines := layoutParagraph(&fakeRenderer{}, para(document.AlignLeft, false, run(text, 12)), contentWidthPt)

	if len(lines) < 2 {
		t.Fatalf("expected the long paragraph to wrap into multiple lines, got %d", len(lines))
	}
	for i, ln := range lines {
		if ln.natural > contentWidthPt+0.01 {
			t.Fatalf("line %d natural width %.2f exceeds content width %.2f", i, ln.natural, contentWidthPt)
		}
	}
}

func TestLayout_MixedSizeLineHeight(t *testing.T) {
	p := para(document.AlignLeft, false, run("small", 10), run(" big", 20))
	lines := layoutParagraph(&fakeRenderer{}, p, contentWidthPt)

	if len(lines) != 1 {
		t.Fatalf("expected a single line, got %d", len(lines))
	}
	if lines[0].maxSize != 20 {
		t.Fatalf("line maxSize = %.1f, want 20 (tallest run)", lines[0].maxSize)
	}
	if lines[0].height != 20*lineSpacing {
		t.Fatalf("line height = %.2f, want %.2f", lines[0].height, 20*lineSpacing)
	}
}

func TestLayout_OversizedWord(t *testing.T) {
	// A single word far wider than the content width must land on its own line
	// and be allowed to overflow rather than looping forever.
	huge := strings.Repeat("x", 400)
	lines := layoutParagraph(&fakeRenderer{}, para(document.AlignLeft, false, run(huge, 12)), contentWidthPt)

	if len(lines) != 1 {
		t.Fatalf("expected the oversized word on one line, got %d lines", len(lines))
	}
	if lines[0].natural <= contentWidthPt {
		t.Fatalf("expected natural width to exceed content width, got %.2f", lines[0].natural)
	}
}

func TestRender_Paginates(t *testing.T) {
	var paras []document.Paragraph
	for range 200 {
		paras = append(paras, para(document.AlignLeft, false, run("line of text", 12)))
	}
	f := &fakeRenderer{}
	(&Converter{doc: &document.Document{Body: bodyOf(paras...)}}).render(f)

	if f.page < 2 {
		t.Fatalf("expected multiple pages, got %d", f.page)
	}
}

func TestRender_CenteredLineIsCentered(t *testing.T) {
	f := &fakeRenderer{}
	(&Converter{doc: &document.Document{Body: bodyOf(
		para(document.AlignCenter, false, run("hi", 12)),
	)}}).render(f)

	if len(f.draws) != 1 {
		t.Fatalf("expected one draw, got %d", len(f.draws))
	}
	// "hi" is 12pt → width 12; centered x0 = margin + (content-12)/2.
	wantX := marginPt + (contentWidthPt-12)/2
	if diff := f.draws[0].x - wantX; diff < -0.5 || diff > 0.5 {
		t.Fatalf("centered x = %.2f, want ~%.2f", f.draws[0].x, wantX)
	}
}

func TestRender_ExplicitPageBreakStartsNewPage(t *testing.T) {
	f := &fakeRenderer{}
	(&Converter{doc: &document.Document{Body: bodyOf(
		para(document.AlignLeft, false, run("first", 12)),
		para(document.AlignLeft, true, run("second", 12)),
	)}}).render(f)

	if len(f.draws) != 2 {
		t.Fatalf("expected two draws, got %d", len(f.draws))
	}
	if f.draws[1].page != f.draws[0].page+1 {
		t.Fatalf("expected the page-break paragraph on the next page, got pages %d and %d", f.draws[0].page, f.draws[1].page)
	}
	// The second paragraph begins at the top of the fresh page — same vertical
	// position as the first paragraph did on its own page.
	if f.draws[1].y != f.draws[0].y {
		t.Fatalf("expected second paragraph at the page top (y=%.2f), got y=%.2f", f.draws[0].y, f.draws[1].y)
	}
}

func TestRender_LeftAndRightIndentShiftsAndNarrowsTheLine(t *testing.T) {
	f := &fakeRenderer{}
	p := paraWithIndent(document.AlignLeft, document.Indent{LeftPt: 36, RightPt: 18}, run("hi", 12))
	(&Converter{doc: &document.Document{Body: bodyOf(p)}}).render(f)

	if len(f.draws) != 1 {
		t.Fatalf("expected one draw, got %d", len(f.draws))
	}
	if want := marginPt + 36.0; f.draws[0].x != want {
		t.Fatalf("indented x = %.2f, want %.2f", f.draws[0].x, want)
	}
}

func TestDrawLine_FirstLineOffsetAppliesOnlyToFirstLine(t *testing.T) {
	f := &fakeRenderer{}
	ln := layoutParagraph(&fakeRenderer{}, para(document.AlignLeft, false, run("hi", 12)), contentWidthPt)[0]

	drawLine(f, ln, document.AlignLeft, marginPt, contentWidthPt, 100, true, 36, true)
	drawLine(f, ln, document.AlignLeft, marginPt, contentWidthPt, 120, true, 36, false)

	if len(f.draws) != 2 {
		t.Fatalf("expected 2 draws, got %d", len(f.draws))
	}
	if want := marginPt + 36.0; f.draws[0].x != want {
		t.Fatalf("first-line draw x = %.2f, want %.2f (shifted by first-line offset)", f.draws[0].x, want)
	}
	if f.draws[1].x != marginPt {
		t.Fatalf("non-first-line draw x = %.2f, want %.2f (unaffected by first-line offset)", f.draws[1].x, marginPt)
	}
}

func TestDrawLine_HangingIndentShiftsFirstLineLeftWithoutClamping(t *testing.T) {
	f := &fakeRenderer{}
	ln := layoutParagraph(&fakeRenderer{}, para(document.AlignLeft, false, run("hi", 12)), contentWidthPt)[0]

	drawLine(f, ln, document.AlignLeft, marginPt, contentWidthPt, 100, true, -36, true)

	if len(f.draws) != 1 {
		t.Fatalf("expected 1 draw, got %d", len(f.draws))
	}
	if want := marginPt - 36.0; f.draws[0].x != want {
		t.Fatalf("hanging first-line draw x = %.2f, want %.2f (the overflow clamp must not erase a legitimate hanging shift)", f.draws[0].x, want)
	}
}

func TestRender_SpaceBeforeShiftsParagraphDown(t *testing.T) {
	f := &fakeRenderer{}
	p := paraWithSpacing(document.AlignLeft, document.Spacing{BeforePt: 20}, run("hi", 12))
	(&Converter{doc: &document.Document{Body: bodyOf(p)}}).render(f)

	if len(f.draws) != 1 {
		t.Fatalf("expected one draw, got %d", len(f.draws))
	}
	wantBaseline := marginPt + 20 + 12
	if f.draws[0].y != wantBaseline {
		t.Fatalf("draw y = %.2f, want %.2f (shifted by space-before)", f.draws[0].y, wantBaseline)
	}
}

func TestRender_SpaceAfterShiftsNextParagraphDown(t *testing.T) {
	f := &fakeRenderer{}
	first := paraWithSpacing(document.AlignLeft, document.Spacing{AfterPt: 20}, run("first", 12))
	second := para(document.AlignLeft, false, run("second", 12))
	(&Converter{doc: &document.Document{Body: bodyOf(first, second)}}).render(f)

	if len(f.draws) != 2 {
		t.Fatalf("expected 2 draws, got %d", len(f.draws))
	}
	wantSecondBaseline := marginPt + 12*lineSpacing + 20 + 12
	if f.draws[1].y != wantSecondBaseline {
		t.Fatalf("second paragraph y = %.2f, want %.2f (shifted by the first paragraph's space-after)", f.draws[1].y, wantSecondBaseline)
	}
}

func TestRender_SpaceAfterShiftsFollowingTableDown(t *testing.T) {
	f := &fakeRenderer{}
	p := paraWithSpacing(document.AlignLeft, document.Spacing{AfterPt: 20}, run("hi", 12))
	tbl := document.Table{
		ColumnWidths: []float64{100},
		Rows: []document.Row{mkRow(document.Cell{
			ColSpan:    1,
			Paragraphs: []document.Paragraph{para(document.AlignLeft, false, run("cell", 12))},
		})},
	}
	body := []document.BodyElement{{Paragraph: &p}, {Table: &tbl}}
	(&Converter{doc: &document.Document{Body: body}}).render(f)

	if len(f.draws) != 2 {
		t.Fatalf("expected 2 draws (paragraph + cell text), got %d", len(f.draws))
	}
	wantCellBaseline := marginPt + 12*lineSpacing + 20 + cellPaddingPt + 12
	if f.draws[1].y != wantCellBaseline {
		t.Fatalf("table cell text y = %.2f, want %.2f (shifted by the preceding paragraph's space-after)", f.draws[1].y, wantCellBaseline)
	}
}

// lineCount returns how many distinct baselines the renderer drew on the first
// page, i.e. the number of wrapped lines (words on one line share a baseline).
func lineCount(f *fakeRenderer) int {
	seen := map[float64]bool{}
	for _, d := range f.draws {
		seen[d.y] = true
	}
	return len(seen)
}

func paraWithLineSpacing(rule document.LineSpacingRule, value float64, runs ...document.Run) document.Paragraph {
	return document.Paragraph{Runs: runs, Props: document.ParagraphProperties{
		Spacing: document.Spacing{LineRule: rule, LineValue: value},
	}}
}

func TestLayout_MultipleLineSpacingScalesHeight(t *testing.T) {
	p := paraWithLineSpacing(document.LineSpacingMultiple, 1.15, run("hi", 20))
	lines := layoutParagraph(&fakeRenderer{}, p, contentWidthPt)
	// A "multiple" counts natural single-spaced lines, so 1.15 scales the
	// natural height (20 × lineSpacing), landing above single spacing.
	if got, want := lines[0].height, 20*lineSpacing*1.15; got-want < -0.001 || got-want > 0.001 {
		t.Fatalf("line height = %.4f, want %.4f (1.15 × natural)", got, want)
	}
	if lines[0].height <= 20*lineSpacing {
		t.Fatalf("1.15 line height %.2f should exceed the single-spaced %.2f", lines[0].height, 20*lineSpacing)
	}
}

func TestLayout_ExactLineSpacingIsFontIndependent(t *testing.T) {
	small := layoutParagraph(&fakeRenderer{}, paraWithLineSpacing(document.LineSpacingExact, 18, run("s", 8)), contentWidthPt)
	big := layoutParagraph(&fakeRenderer{}, paraWithLineSpacing(document.LineSpacingExact, 18, run("b", 40)), contentWidthPt)
	if small[0].height != 18 || big[0].height != 18 {
		t.Fatalf("exact line heights = %.2f / %.2f, want 18 / 18 (independent of font size)", small[0].height, big[0].height)
	}
}

func TestRender_WrapWidthFollowsDocumentGeometry(t *testing.T) {
	text := strings.TrimSpace(strings.Repeat("word ", 60))
	p := para(document.AlignLeft, false, run(text, 12))
	// Tall pages so pagination never interferes; only the width differs.
	narrow := document.PageGeometry{WidthPt: 300, HeightPt: 4000, MarginTopPt: 72, MarginRightPt: 72, MarginBottomPt: 72, MarginLeftPt: 72}
	wide := document.PageGeometry{WidthPt: 900, HeightPt: 4000, MarginTopPt: 72, MarginRightPt: 72, MarginBottomPt: 72, MarginLeftPt: 72}

	fN, fW := &fakeRenderer{}, &fakeRenderer{}
	(&Converter{doc: &document.Document{Body: bodyOf(p), Geometry: narrow}}).render(fN)
	(&Converter{doc: &document.Document{Body: bodyOf(p), Geometry: wide}}).render(fW)

	if lineCount(fN) <= lineCount(fW) {
		t.Fatalf("narrow geometry should wrap into more lines: narrow=%d wide=%d", lineCount(fN), lineCount(fW))
	}
}

func TestConverter_DefaultsToA4WhenNoGeometry(t *testing.T) {
	// A document built without geometry (zero-value) must fall back to A4/1-inch.
	g := (&Converter{doc: &document.Document{}}).geometry()
	if g.WidthPt != 595.28 || g.HeightPt != 841.89 {
		t.Fatalf("default geometry = %v × %v, want 595.28 × 841.89 (A4)", g.WidthPt, g.HeightPt)
	}
	if g.MarginLeftPt != 72 {
		t.Fatalf("default left margin = %v, want 72", g.MarginLeftPt)
	}
}

func TestRender_MultipleSpacesKeepWidth(t *testing.T) {
	f := &fakeRenderer{}
	// fakeRenderer: each glyph is size×0.5 wide → "a"=5, four spaces=20, "b"=5.
	(&Converter{doc: &document.Document{Body: bodyOf(para(document.AlignLeft, false, run("a    b", 10)))}}).render(f)

	if len(f.draws) != 2 {
		t.Fatalf("expected 2 word draws, got %d", len(f.draws))
	}
	if gap := f.draws[1].x - (f.draws[0].x + 5); gap != 20 {
		t.Fatalf("inter-word gap = %.1f, want 20 (four spaces, not one)", gap)
	}
}

func TestRender_LeadingSpacesIndentLeftFirstLine(t *testing.T) {
	f := &fakeRenderer{}
	// Two leading spaces at size 10 → 10pt first-line indent.
	(&Converter{doc: &document.Document{Body: bodyOf(para(document.AlignLeft, false, run("  hi", 10)))}}).render(f)

	if len(f.draws) != 1 {
		t.Fatalf("expected 1 draw, got %d", len(f.draws))
	}
	if got := f.draws[0].x - marginPt; got != 10 {
		t.Fatalf("first word x offset = %.1f, want 10 (two leading spaces)", got)
	}
}

func TestRender_SoftWrapDropsLeadingSpace(t *testing.T) {
	f := &fakeRenderer{}
	text := strings.TrimSpace(strings.Repeat("word ", 60))
	narrow := document.PageGeometry{WidthPt: 200, HeightPt: 4000, MarginTopPt: 72, MarginRightPt: 72, MarginBottomPt: 72, MarginLeftPt: 72}
	(&Converter{doc: &document.Document{Body: bodyOf(para(document.AlignLeft, false, run(text, 10))), Geometry: narrow}}).render(f)

	// Group draws by baseline; the second line's first word must start at the
	// content edge (no leading-space offset carried across the wrap).
	firstLineY := f.draws[0].y
	for _, d := range f.draws {
		if d.y != firstLineY {
			if d.x != marginPt {
				t.Fatalf("continuation line first word x = %.1f, want %.1f (no leading offset)", d.x, marginPt)
			}
			return
		}
	}
	t.Fatal("expected the paragraph to wrap onto a second line")
}

func TestRender_CenteredIgnoresLeadingSpaces(t *testing.T) {
	f := &fakeRenderer{}
	(&Converter{doc: &document.Document{Body: bodyOf(para(document.AlignCenter, false, run("  hi", 10)))}}).render(f)

	if len(f.draws) != 1 {
		t.Fatalf("expected 1 draw, got %d", len(f.draws))
	}
	// "hi" is 10 wide; centered on content as if the two leading spaces were absent.
	want := marginPt + (contentWidthPt-10)/2
	if diff := f.draws[0].x - want; diff < -0.01 || diff > 0.01 {
		t.Fatalf("centered x = %.2f, want %.2f (leading spaces excluded)", f.draws[0].x, want)
	}
}

func brk() document.Run { return document.Run{LineBreak: true} }

func TestRender_LineBreakStartsNewLine(t *testing.T) {
	f := &fakeRenderer{}
	p := document.Paragraph{Runs: []document.Run{run("A", 12), brk(), run("B", 12)}}
	(&Converter{doc: &document.Document{Body: bodyOf(p)}}).render(f)

	if len(f.draws) != 2 {
		t.Fatalf("expected 2 draws, got %d", len(f.draws))
	}
	if f.draws[1].y <= f.draws[0].y {
		t.Fatalf("expected B (y=%.1f) on a line below A (y=%.1f)", f.draws[1].y, f.draws[0].y)
	}
}

func TestRender_LeadingLineBreakEmitsBlankLine(t *testing.T) {
	f := &fakeRenderer{}
	withBreak := document.Paragraph{Runs: []document.Run{brk(), run("hi", 12)}}
	(&Converter{doc: &document.Document{Body: bodyOf(withBreak)}}).render(f)
	withoutBreak := &fakeRenderer{}
	(&Converter{doc: &document.Document{Body: bodyOf(para(document.AlignLeft, false, run("hi", 12)))}}).render(withoutBreak)

	if len(f.draws) != 1 {
		t.Fatalf("expected 1 draw, got %d", len(f.draws))
	}
	if f.draws[0].y <= withoutBreak.draws[0].y {
		t.Fatalf("leading break should push text down: with=%.1f without=%.1f", f.draws[0].y, withoutBreak.draws[0].y)
	}
}

func TestRender_LineBreakRespectsCenterAlignment(t *testing.T) {
	f := &fakeRenderer{}
	p := document.Paragraph{
		Runs:  []document.Run{run("hi", 12), brk(), run("yo", 12)},
		Props: document.ParagraphProperties{Alignment: document.AlignCenter},
	}
	(&Converter{doc: &document.Document{Body: bodyOf(p)}}).render(f)

	if len(f.draws) != 2 {
		t.Fatalf("expected 2 draws, got %d", len(f.draws))
	}
	want := marginPt + (contentWidthPt-12)/2 // "hi"/"yo" are each 12 wide
	for i, d := range f.draws {
		if diff := d.x - want; diff < -0.01 || diff > 0.01 {
			t.Fatalf("draw %d x = %.2f, want %.2f (each line centered)", i, d.x, want)
		}
	}
}

func TestRender_NilDocumentProducesOnePage(t *testing.T) {
	f := &fakeRenderer{}
	ConvertToPdf(nil).render(f)
	if f.page != 1 {
		t.Fatalf("expected exactly one page for a nil document, got %d", f.page)
	}
	if len(f.draws) != 0 {
		t.Fatalf("expected no draws for a nil document, got %d", len(f.draws))
	}
}
