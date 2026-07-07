// Package convert renders a document.Document into a paginated PDF: text
// measurement, word-wrap, per-run formatting, alignment and page breaks. The
// public entry point mirrors UniOffice: ConvertToPdf(doc) → WriteToFile/Write.
package convert

import (
	"fmt"
	"io"
	"os"

	"github.com/h0rn3t/goffice/document"
)

// Converter renders a document to PDF. Create one with ConvertToPdf.
type Converter struct {
	doc *document.Document
}

// ConvertToPdf returns a Converter that renders doc's body as flowed PDF text.
func ConvertToPdf(doc *document.Document) *Converter {
	return &Converter{doc: doc}
}

// Write renders the document and writes the complete PDF byte stream to w.
func (c *Converter) Write(w io.Writer) error {
	g := c.geometry()
	r := newFPDFRenderer(g.WidthPt, g.HeightPt)
	c.render(r)
	return r.Output(w)
}

// geometry returns the page geometry to render on: the document's own
// (from w:sectPr) when present, else the A4/one-inch default - also used for a
// nil document or one built without geometry (a non-positive width).
func (c *Converter) geometry() document.PageGeometry {
	if c.doc == nil || c.doc.Geometry.WidthPt <= 0 {
		return document.DefaultPageGeometry()
	}
	return c.doc.Geometry
}

// WriteToFile renders the document and writes the PDF to path. It returns a
// non-nil error if the destination cannot be created or written; it never
// panics.
func (c *Converter) WriteToFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("goffice: create pdf %q: %w", path, err)
	}
	if err := c.Write(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("goffice: write pdf %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("goffice: close pdf %q: %w", path, err)
	}
	return nil
}

// render lays every paragraph out across pages. It always emits at least one
// page so the output is a valid PDF even for an empty or nil document.
func (c *Converter) render(r renderer) {
	pg := pageFrom(c.geometry())
	r.AddPage()
	cursorY := pg.originY
	atPageTop := true

	if c.doc == nil {
		return
	}

	for _, el := range c.doc.Body {
		switch {
		case el.Paragraph != nil:
			cursorY, atPageTop = renderParagraph(r, *el.Paragraph, pg, cursorY, atPageTop)
		case el.Table != nil:
			cursorY, atPageTop = renderTable(r, *el.Table, pg, cursorY, atPageTop)
		}
	}
}

// renderParagraph lays out and draws one top-level paragraph across the
// content width (narrowed by the paragraph's own indent), paginating as
// needed, and returns the cursor position after it. The paragraph's own
// space-before/space-after (p.Props.Spacing) is added as plain cursor
// advances with no dedicated overflow check of its own - a table's
// surrounding vertical gap is produced entirely by its neighboring
// paragraphs' spacing this way, with no changes needed in convert/table.go.
func renderParagraph(r renderer, p document.Paragraph, pg page, cursorY float64, atPageTop bool) (float64, bool) {
	if p.Props.PageBreak && !atPageTop {
		r.AddPage()
		cursorY, atPageTop = pg.originY, true
	}
	cursorY += p.Props.Spacing.BeforePt

	innerX := pg.originX + p.Props.Indent.LeftPt
	innerWidth := pg.contentWidth - p.Props.Indent.LeftPt - p.Props.Indent.RightPt

	lines := layoutParagraph(r, p, innerWidth)
	if len(lines) == 0 { // empty paragraph advances one line at its line spacing
		cursorY, atPageTop = advance(r, pg, cursorY, atPageTop, lineHeightFor(defaultRenderSizePt, p.Props.Spacing))
		return cursorY + p.Props.Spacing.AfterPt, atPageTop
	}

	for i, ln := range lines {
		if cursorY+ln.height > pg.bottomLimit && !atPageTop {
			r.AddPage()
			cursorY = pg.originY
		}
		drawLine(r, ln, p.Props.Alignment, innerX, innerWidth, cursorY, i == len(lines)-1, p.Props.Indent.FirstLineOffsetPt, i == 0)
		cursorY += ln.height
		atPageTop = false
	}
	return cursorY + p.Props.Spacing.AfterPt, atPageTop
}

// advance moves the cursor down by h, paginating first if the step would cross
// the bottom margin. Used for blank (run-less) paragraphs.
func advance(r renderer, pg page, cursorY float64, atPageTop bool, h float64) (float64, bool) {
	if cursorY+h > pg.bottomLimit && !atPageTop {
		r.AddPage()
		return pg.originY + h, false
	}
	return cursorY + h, false
}
