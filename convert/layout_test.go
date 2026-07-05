package convert

import (
	"io"
	"strings"
	"testing"

	"github.com/h0rn3t/goffice/document"
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
