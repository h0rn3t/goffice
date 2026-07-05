# goffice

Open, community-driven alternative to UniOffice. A pure Go library for creating
and processing Office Word (.docx), Excel (.xlsx), and PowerPoint (.pptx)
documents.

No cgo, no external runtime (no LibreOffice or headless browser) — just Go.

## docx → PDF

```go
package main

import (
	"log"

	"github.com/h0rn3t/goffice/convert"
	"github.com/h0rn3t/goffice/document"
)

func main() {
	doc, err := document.Open("in.docx")
	if err != nil {
		log.Fatal(err)
	}
	defer doc.Close()

	c := convert.ConvertToPdf(doc)
	if err := c.WriteToFile("out.pdf"); err != nil {
		log.Fatal(err)
	}
}
```

A runnable version lives in [`examples/docx2pdf`](examples/docx2pdf):

```sh
go run ./examples/docx2pdf in.docx out.pdf
```

## MVP fidelity limitations

The current conversion renders **legible, flowed body text — not a
pixel-perfect reproduction of Word**. Supported today:

- paragraphs and text runs, in document order alongside tables;
- per-run **bold**, *italic*, underline, font family and size;
- paragraph alignment (left, center, right, justify) and explicit page breaks;
- automatic word-wrap and pagination;
- Unicode text, including Cyrillic — rendered through embedded Liberation
  Sans/Serif/Mono fonts (SIL OFL 1.1) instead of the Latin-only PDF core fonts,
  so non-Latin scripts render as correct glyphs, not mojibake;
- tables: rows/cells, column widths (`w:tblGrid`), horizontal (`gridSpan`) and
  vertical (`vMerge`) cell merging, per-side borders and cell shading, nested
  tables, and pagination (a row is never split across pages). Borders/shading
  resolve from inline `w:tblPr`/`w:tcPr` first, falling back to the table's
  named style (`w:tblStyle`, via `styles.xml`, following `w:basedOn`) — so a
  table styled the ordinary way in Word (e.g. the built-in "Table Grid")
  renders with a visible grid, not just correctly positioned text.

Not yet handled (unsupported content is **skipped**, the conversion still
succeeds):

- images/drawings, headers/footers, footnotes, fields, hyperlinks;
- `styles.xml` paragraph/run/numbering style inheritance and theme colors;
  for tables, only a style's *base* border/shading is resolved — banded rows/
  columns and first/last-row/column formatting (`w:tblStylePr`) are not;
- section geometry (`sectPr`) — a fixed A4 page with 1-inch margins is used;
- fonts beyond the bundled Liberation family (Word fonts are mapped to serif →
  Liberation Serif, monospace → Liberation Mono, else Liberation Sans, so line
  breaks and page counts can differ slightly from Word);
- table auto-fit/content-based column sizing, cell padding customization
  (a fixed default is used), repeating header rows across a page break, and
  vertical text direction or diagonal cell borders.
