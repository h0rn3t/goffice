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
	r := newFPDFRenderer()
	c.render(r)
	return r.Output(w)
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
	r.AddPage()
	cursorY := marginPt
	atPageTop := true

	if c.doc == nil {
		return
	}

	for _, el := range c.doc.Body {
		switch {
		case el.Paragraph != nil:
			cursorY, atPageTop = renderParagraph(r, *el.Paragraph, cursorY, atPageTop)
		case el.Table != nil:
			cursorY, atPageTop = renderTable(r, *el.Table, marginPt, cursorY, atPageTop)
		}
	}
}

// renderParagraph lays out and draws one top-level paragraph across the full
// content width, paginating as needed, and returns the cursor position after it.
func renderParagraph(r renderer, p document.Paragraph, cursorY float64, atPageTop bool) (float64, bool) {
	if p.Props.PageBreak && !atPageTop {
		r.AddPage()
		cursorY, atPageTop = marginPt, true
	}

	lines := layoutParagraph(r, p, contentWidthPt)
	if len(lines) == 0 { // empty paragraph advances one default line
		return advance(r, cursorY, atPageTop, defaultRenderSizePt*lineSpacing)
	}

	for i, ln := range lines {
		if cursorY+ln.height > marginPt+contentHeightPt && !atPageTop {
			r.AddPage()
			cursorY = marginPt
		}
		drawLine(r, ln, p.Props.Alignment, marginPt, contentWidthPt, cursorY, i == len(lines)-1)
		cursorY += ln.height
		atPageTop = false
	}
	return cursorY, atPageTop
}

// advance moves the cursor down by h, paginating first if the step would cross
// the bottom margin. Used for blank (run-less) paragraphs.
func advance(r renderer, cursorY float64, atPageTop bool, h float64) (float64, bool) {
	if cursorY+h > marginPt+contentHeightPt && !atPageTop {
		r.AddPage()
		return marginPt + h, false
	}
	return cursorY + h, false
}
