// Package document opens a .docx (OOXML) package and exposes an in-memory
// model of its body: paragraphs, text runs, tables, and the formatting needed
// to lay them out (bold, italic, underline, font family, size, alignment,
// explicit page breaks, table column widths, cell merging/borders/shading).
//
// The model is read eagerly on Open, so it is self-contained: the underlying
// ZIP is closed before Open returns and the returned Document does not depend
// on any open file handle.
package document

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"sync"
)

// Default character formatting reported for runs whose properties are absent
// in word/document.xml. Values are non-zero so callers always have something
// concrete to lay out. styles.xml/theme inheritance is out of scope (MVP).
const (
	defaultFontFamily = "Calibri"
	defaultFontSizePt = 11.0
)

// Alignment is a paragraph's horizontal alignment.
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignCenter
	AlignRight
	AlignJustify
)

// RunProperties is the character formatting a Run needs for layout.
type RunProperties struct {
	Bold       bool
	Italic     bool
	Underline  bool
	FontFamily string
	SizePt     float64
}

// Run is a span of text sharing one set of character-formatting properties.
type Run struct {
	Text  string
	Props RunProperties
}

// ParagraphProperties is the paragraph-level formatting needed for layout.
type ParagraphProperties struct {
	Alignment Alignment
	// PageBreak reports whether an explicit page break precedes this paragraph
	// (a <w:br w:type="page"/> in one of its runs or <w:pageBreakBefore/>).
	PageBreak bool
}

// Paragraph is an ordered sequence of runs plus its paragraph properties.
type Paragraph struct {
	Runs  []Run
	Props ParagraphProperties
}

// BodyElement is one paragraph- or table-level child of a Document's body, in
// document order. Exactly one of Paragraph or Table is non-nil.
type BodyElement struct {
	Paragraph *Paragraph
	Table     *Table
}

// Table is a parsed <w:tbl>: an ordered sequence of rows plus each column's
// resolved width in points (from w:tblGrid, or - when absent - the widest
// w:tcW seen for that column).
type Table struct {
	Rows         []Row
	ColumnWidths []float64
}

// Row is an ordered sequence of cells.
type Row struct {
	Cells []Cell
}

// VMergeState is a cell's role in a vertical merge (w:vMerge).
type VMergeState int

const (
	// VMergeNone means the cell does not participate in a vertical merge.
	VMergeNone VMergeState = iota
	// VMergeRestart means the cell starts a new vertical merge, absorbing the
	// VMergeContinue cells below it in the same column.
	VMergeRestart
	// VMergeContinue means the cell continues the vertical merge started by
	// the VMergeRestart cell above it; its own content is not drawn.
	VMergeContinue
)

// BorderSide is one border's style, width, and color. A zero value means no
// border is drawn on that side.
type BorderSide struct {
	Style   string // raw w:val (e.g. "single", "double"); "" means no border
	WidthPt float64
	Color   string // "#RRGGBB"; "" when undeclared or "auto"
}

// CellBorders holds a cell's resolved per-side border (w:tcBorders overriding
// the table's w:tblBorders).
type CellBorders struct {
	Top, Bottom, Left, Right BorderSide
}

// Cell is one table cell: its paragraphs, horizontal/vertical merge state,
// resolved borders and shading, and an optional nested table.
type Cell struct {
	Paragraphs []Paragraph
	// ColSpan is the number of grid columns this cell spans (from
	// w:gridSpan); 1 when absent.
	ColSpan int
	VMerge  VMergeState
	Borders CellBorders
	// Shading is the cell's background color ("#RRGGBB"), or "" when
	// undeclared or "auto"/"nil".
	Shading string
	// Nested is the first table found directly inside this cell, or nil.
	Nested *Table
}

// Document is the in-memory model of a .docx body.
type Document struct {
	Body []BodyElement

	mu     sync.Mutex
	closed bool
}

// Open opens a .docx file, reads its word/document.xml main part, and returns
// an in-memory Document. It returns a nil Document and a non-nil error when the
// file does not exist, is not a valid ZIP archive, or lacks word/document.xml.
func Open(path string) (*Document, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("goffice: open docx %q: %w", path, err)
	}
	defer func() { _ = r.Close() }()

	var main, styles *zip.File
	for _, f := range r.File {
		switch f.Name {
		case "word/document.xml":
			main = f
		case "word/styles.xml":
			styles = f
		}
	}
	if main == nil {
		return nil, fmt.Errorf("goffice: %q is not a valid docx: missing word/document.xml", path)
	}

	rc, err := main.Open()
	if err != nil {
		return nil, fmt.Errorf("goffice: read word/document.xml in %q: %w", path, err)
	}
	defer func() { _ = rc.Close() }()

	var xdoc xmlDocument
	if err := xml.NewDecoder(rc).Decode(&xdoc); err != nil {
		return nil, fmt.Errorf("goffice: parse word/document.xml in %q: %w", path, err)
	}

	return &Document{Body: buildBody(xdoc.Body, buildTableStyles(readTableStyles(styles)))}, nil
}

// readTableStyles decodes word/styles.xml when present. It is an optional
// part: absent, unreadable, or malformed styles.xml degrades to nil (no
// style-level border/shading tier) rather than failing Open - only
// word/document.xml is a required part.
func readTableStyles(f *zip.File) *xmlStyles {
	if f == nil {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }()

	var xs xmlStyles
	if xml.NewDecoder(rc).Decode(&xs) != nil {
		return nil
	}
	return &xs
}

// Close releases resources held since Open. Because Open reads eagerly and
// closes the underlying package itself, Close only guards against double
// invocation; it is safe to call more than once and never panics.
func (d *Document) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}
