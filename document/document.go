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

// Run is a span of text sharing one set of character-formatting properties, or
// an explicit in-paragraph line break. When LineBreak is true the run carries
// no text and Props is unset; it marks a `<w:br/>` at its position among the
// paragraph's runs.
type Run struct {
	Text      string
	Props     RunProperties
	LineBreak bool
}

// ParagraphProperties is the paragraph-level formatting needed for layout.
type ParagraphProperties struct {
	Alignment Alignment
	// PageBreak reports whether an explicit page break precedes this paragraph
	// (a <w:br w:type="page"/> in one of its runs or <w:pageBreakBefore/>).
	PageBreak bool
	// Indent is the paragraph's left/right/first-line indentation (from
	// w:ind), in points. The zero value is no indent.
	Indent Indent
	// Spacing is the paragraph's space-before/space-after (from w:spacing),
	// in points. The zero value is no added spacing.
	Spacing Spacing
}

// Indent is a paragraph's indentation in points, from w:ind.
type Indent struct {
	LeftPt, RightPt float64
	// FirstLineOffsetPt shifts only the paragraph's first line: positive for
	// w:firstLine (indented further right), negative for w:hanging (indented
	// further left of the rest of the paragraph). Zero when neither is
	// declared.
	FirstLineOffsetPt float64
}

// LineSpacingRule is how a paragraph's line height is determined, from
// w:spacing's w:lineRule.
type LineSpacingRule int

const (
	// LineSpacingSingle means no w:line was declared; the renderer applies its
	// own default line-height multiplier.
	LineSpacingSingle LineSpacingRule = iota
	// LineSpacingMultiple (w:lineRule="auto") multiplies the line's font size;
	// LineValue is the multiple (w:line ÷ 240).
	LineSpacingMultiple
	// LineSpacingExact (w:lineRule="exact") fixes the line height; LineValue is
	// that height in points (w:line ÷ 20).
	LineSpacingExact
	// LineSpacingAtLeast (w:lineRule="atLeast") is a minimum line height;
	// LineValue is that minimum in points (w:line ÷ 20).
	LineSpacingAtLeast
)

// Spacing is a paragraph's space-before/space-after and line spacing, from
// w:spacing. The zero value is no added spacing and single (default) line
// spacing.
type Spacing struct {
	BeforePt, AfterPt float64
	// LineRule and LineValue describe line spacing; LineRule is
	// LineSpacingSingle (and LineValue 0) when w:line is absent.
	LineRule  LineSpacingRule
	LineValue float64
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
	// IndentPt is the table's resolved left indent in points (from
	// w:tblInd, inline winning over the table's named style). Zero when
	// neither declares one.
	IndentPt float64
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

// PageGeometry is the document's page size and margins in points, resolved
// from the body-level w:sectPr (w:pgSz/w:pgMar). When the document declares no
// section geometry, it defaults to A4 (595.28 × 841.89 pt) with one-inch
// (72 pt) margins on all sides.
type PageGeometry struct {
	WidthPt, HeightPt                                        float64
	MarginTopPt, MarginRightPt, MarginBottomPt, MarginLeftPt float64
}

// DefaultPageGeometry returns the fallback geometry used when a document
// declares no section geometry: A4 with one-inch margins.
func DefaultPageGeometry() PageGeometry { return buildPageGeometry(nil) }

// Document is the in-memory model of a .docx body.
type Document struct {
	Body []BodyElement
	// Geometry is the page size and margins for layout, from w:sectPr.
	Geometry PageGeometry

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

	xs := readStyles(styles)
	sc := styleContext{
		tables:     buildTableStyles(xs),
		paragraphs: buildParaStyles(xs),
	}
	if xs != nil && xs.DocDefaults != nil && xs.DocDefaults.PPrDefault != nil && xs.DocDefaults.PPrDefault.PPr != nil {
		sc.defaultSpacing = xs.DocDefaults.PPrDefault.PPr.Spacing
	}

	return &Document{
		Body:     buildBody(xdoc.Body, sc),
		Geometry: buildPageGeometry(xdoc.Body.SectPr),
	}, nil
}

// readStyles decodes word/styles.xml when present. It is an optional part:
// absent, unreadable, or malformed styles.xml degrades to nil (no style-level
// tier for table borders/shading or paragraph spacing, no docDefaults) rather
// than failing Open - only word/document.xml is a required part.
func readStyles(f *zip.File) *xmlStyles {
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
