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
	// Color is the run's resolved text color as "#RRGGBB", or "" when
	// undeclared, "auto", or an unresolved theme reference - the renderer draws
	// "" as the backend default (black).
	Color string
	// Shading is the run's background color as "#RRGGBB", or "" when it has
	// none: the run's w:highlight, or - when it carries none - its w:shd, which
	// the highlight would paint over anyway.
	Shading string
}

// Image is a picture embedded in the document, ready to be drawn: the media
// part's bytes together with the size the document draws it at.
type Image struct {
	// Name is the media part's name in the package (e.g. word/media/image1.png),
	// unique per picture, which the PDF backend uses as its cache key so an
	// image reused across the document is embedded once.
	Name string
	// Type is the encoded format: "PNG", "JPG" or "GIF".
	Type string
	Data []byte
	// WidthPt/HeightPt are the drawn size, from the picture's declared extent or
	// - when it declares none - its intrinsic pixel size at 96 dpi.
	WidthPt, HeightPt float64
}

// Run is a span of text sharing one set of character-formatting properties, an
// inline image, or an explicit in-paragraph line break. When LineBreak is true
// the run carries no text and Props is unset; it marks a `<w:br/>` at its
// position among the paragraph's runs.
type Run struct {
	Text      string
	Props     RunProperties
	LineBreak bool
	// Image is the picture this run draws instead of Text (an inline w:drawing
	// or a picture bullet); nil for a text run.
	Image *Image
	// MinWidthPt floors the run's advance width, so what follows it starts at
	// least that far along: a list marker's tab suffix reserves the level's
	// hanging indent this way, putting the paragraph's text at the tab stop
	// Word aligns it to. Zero for an ordinary run.
	MinWidthPt float64
	// Field, when set, marks this run as a computed field the renderer fills in
	// at draw time: FieldPage → the page's own number, FieldNumPages → the total
	// page count. Text holds the value cached in the .docx, used as-is until the
	// renderer overwrites it. "" for an ordinary run.
	Field string
}

// Field kinds a Run may carry (see Run.Field). Only page-number fields are
// computed; every other Word field renders its cached result text verbatim.
const (
	FieldPage     = "PAGE"
	FieldNumPages = "NUMPAGES"
)

// ParagraphProperties is the paragraph-level formatting needed for layout.
type ParagraphProperties struct {
	Alignment Alignment
	// PageBreak reports whether an explicit page break precedes this paragraph
	// (a <w:br w:type="page"/> in one of its runs or <w:pageBreakBefore/>).
	PageBreak bool
	// ColumnBreak reports an explicit column break (<w:br w:type="column"/>)
	// preceding this paragraph: in a multi-column section it moves to the next
	// column, and on the last column to the next page.
	ColumnBreak bool
	// Indent is the paragraph's left/right/first-line indentation (from
	// w:ind), in points. The zero value is no indent.
	Indent Indent
	// Spacing is the paragraph's space-before/space-after (from w:spacing),
	// in points. The zero value is no added spacing.
	Spacing Spacing
	// Shading is the paragraph's background color as "#RRGGBB" (from w:shd), or
	// "" when it has none. It is painted across the paragraph's indented text
	// area, behind its lines.
	Shading string
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
	Rows []Row
	// ColumnWidths is each column's width in points. A zero width means the
	// column declares none and is sized from its content when the table is laid
	// out (Word's auto-fit), possibly via ColumnPercents.
	ColumnWidths []float64
	// ColumnPercents is each column's width as a percentage of the available
	// width (from a w:tcW of type "pct"), or zero when it declares none. Only
	// consulted for a column whose ColumnWidths entry is zero.
	ColumnPercents []float64
	// IndentPt is the table's resolved left indent in points (from
	// w:tblInd, inline winning over the table's named style). Zero when
	// neither declares one.
	IndentPt float64
	// HeaderRows is how many leading rows are marked as header rows
	// (w:tblHeader) and are therefore repeated at the top of every page the
	// table continues onto.
	HeaderRows int
	// FixedLayout reports w:tblLayout w:type="fixed": the declared widths are the
	// widths, and the content never gets a say. A column left undeclared then
	// takes an equal share of what the others leave, instead of being sized from
	// what is in it.
	FixedLayout bool
	// WidthPt and WidthPct are the table's own declared width (w:tblW), in points
	// or as a percentage of the width available to it; both are zero for an "auto"
	// width. The columns are scaled to whichever is declared.
	WidthPt  float64
	WidthPct float64
	// Float is a floating table's placement (w:tblpPr), or nil for one that flows
	// with the text.
	Float *TableFloat
}

// Anchor is what a floating table's position is measured from (w:horzAnchor /
// w:vertAnchor).
type Anchor int

const (
	// AnchorText is the default: the position is measured from the text column
	// the table sits in.
	AnchorText Anchor = iota
	// AnchorMargin measures from the page's margin.
	AnchorMargin
	// AnchorPage measures from the edge of the page.
	AnchorPage
)

// TableFloat is a floating table's placement (w:tblpPr): an offset from its
// anchor, or - when the document names one instead - an alignment against it.
type TableFloat struct {
	XPt, YPt               float64
	HorzAnchor, VertAnchor Anchor
	// XSpec/YSpec are w:tblpXSpec/w:tblpYSpec ("left"/"center"/"right",
	// "top"/"center"/"bottom"); "" means the offset is used instead.
	XSpec, YSpec string
}

// RowHeightRule is how a row's declared height (w:trHeight) constrains it.
type RowHeightRule int

const (
	// RowHeightAuto means the row declares no height: it is as tall as its
	// content needs.
	RowHeightAuto RowHeightRule = iota
	// RowHeightAtLeast (w:hRule="atLeast") floors the row's height.
	RowHeightAtLeast
	// RowHeightExact (w:hRule="exact") fixes it, even below what the content
	// needs.
	RowHeightExact
)

// Row is an ordered sequence of cells.
type Row struct {
	Cells []Cell
	// Header reports a header row (w:tblHeader): one repeated at the top of each
	// page the table spills onto.
	Header bool
	// HeightPt and HeightRule are the row's declared height (w:trHeight); the
	// rule is RowHeightAuto when it declares none.
	HeightPt   float64
	HeightRule RowHeightRule
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
// the table's w:tblBorders) plus its two diagonals, which only a cell can
// declare (w:tl2br, w:tr2bl).
type CellBorders struct {
	Top, Bottom, Left, Right BorderSide
	// DiagDown runs from the cell's top-left to its bottom-right (w:tl2br);
	// DiagUp runs from the bottom-left to the top-right (w:tr2bl).
	DiagDown, DiagUp BorderSide
}

// CellMargins is a cell's inner padding, from w:tcMar (or the table's
// w:tblCellMar). The Word default is 108 dxa (5.4 pt) left and right, none top
// and bottom.
type CellMargins struct {
	TopPt, BottomPt, LeftPt, RightPt float64
	// The *Pct fields are sides declared as a percentage of the table's width
	// (w:type="pct") rather than in points; they are resolved against the table's
	// resolved width when it is laid out, and are zero for an ordinary side.
	TopPct, BottomPct, LeftPct, RightPct float64
}

// resolve turns percentage sides into points against the table's width, so the
// layout works with plain point margins.
func (m CellMargins) resolve(tableWidth float64) CellMargins {
	return CellMargins{
		TopPt:    m.TopPt + tableWidth*m.TopPct/100,
		BottomPt: m.BottomPt + tableWidth*m.BottomPct/100,
		LeftPt:   m.LeftPt + tableWidth*m.LeftPct/100,
		RightPt:  m.RightPt + tableWidth*m.RightPct/100,
	}
}

// TextDirection is how a cell's text runs (w:textDirection).
type TextDirection int

const (
	// TextDirectionHorizontal is ordinary left-to-right text (the default, and
	// w:val="lrTb").
	TextDirectionHorizontal TextDirection = iota
	// TextDirectionBTLR rotates the text 90° counter-clockwise, so it reads
	// bottom-to-top (w:val="btLr").
	TextDirectionBTLR
	// TextDirectionTBRL rotates the text 90° clockwise, so it reads top-to-bottom
	// down the right (w:val="tbRl"/"tbRlV").
	TextDirectionTBRL
)

// VerticalAlign is where a cell's content sits within its height (w:vAlign).
type VerticalAlign int

const (
	// VAlignTop is the default: content starts at the cell's top margin.
	VAlignTop VerticalAlign = iota
	VAlignCenter
	VAlignBottom
)

// Cell is one table cell: its paragraphs, horizontal/vertical merge state,
// resolved borders, shading, margins, text direction and vertical alignment,
// and an optional nested table.
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
	// Margins is the cell's inner padding, resolved from w:tcMar, the table's
	// w:tblCellMar (inline then from its style), or Word's defaults.
	Margins CellMargins
	// TextDirection is which way the cell's text runs (w:textDirection).
	TextDirection TextDirection
	// VAlign is where the content sits within the cell's height (w:vAlign).
	VAlign VerticalAlign
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
	// Geometry is the page size and margins of the document's first section,
	// from w:sectPr.
	Geometry PageGeometry
	// Sections are the document's sections in order, each covering the range of
	// Body that ends at its End index. There is always at least one.
	Sections []Section

	mu     sync.Mutex
	closed bool
}

// Open opens a .docx file, reads its word/document.xml main part, and returns
// an in-memory Document. It returns a nil Document and a non-nil error when the
// file does not exist, is not a valid ZIP archive, or lacks word/document.xml.
func Open(path string) (*Document, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("docx2pdf: open docx %q: %w", path, err)
	}
	defer func() { _ = r.Close() }()

	p := newPkg(r.File)
	main, ok := p.part("word/document.xml")
	if !ok {
		return nil, fmt.Errorf("docx2pdf: %q is not a valid docx: missing word/document.xml", path)
	}
	var xdoc xmlDocument
	if err := xml.Unmarshal(main, &xdoc); err != nil {
		return nil, fmt.Errorf("docx2pdf: parse word/document.xml in %q: %w", path, err)
	}

	xs := optionalPart[xmlStyles](p, "word/styles.xml")
	theme := optionalPart[xmlTheme](p, "word/theme/theme1.xml")
	numbering := optionalPart[xmlNumbering](p, "word/numbering.xml")

	sc := styleContext{
		tables:     buildTableStyles(xs),
		paragraphs: buildParaStyles(xs),
		chars:      buildCharStyles(xs),
		theme:      buildThemeMap(theme),
		themeFonts: buildThemeFontMap(theme),
		images:     p.resolverFor("word/document.xml"),
		// A picture bullet's media is referenced from the numbering part, so it
		// resolves through that part's own relationships.
		numbering: newNumberingState(buildNumbering(numbering, p.resolverFor("word/numbering.xml"))),
	}
	if xs != nil && xs.DocDefaults != nil {
		if xs.DocDefaults.PPrDefault != nil {
			sc.defaultPPr = xs.DocDefaults.PPrDefault.PPr
		}
		if xs.DocDefaults.RPrDefault != nil {
			sc.defaultRPr = xs.DocDefaults.RPrDefault.RPr
		}
	}

	body, breaks := buildBody(xdoc.Body, sc)
	sections := buildSections(breaks, xdoc.Body.SectPr, len(body), p, sc)
	return &Document{
		Body:     body,
		Geometry: sections[0].Geometry,
		Sections: sections,
	}, nil
}

// optionalPart decodes a supporting part (styles, theme, numbering) into T.
// Every such part is optional: absent, unreadable, or malformed, it degrades to
// nil - the corresponding formatting tier is then simply skipped - rather than
// failing Open, since only word/document.xml is a required part.
func optionalPart[T any](p *pkg, name string) *T {
	data, ok := p.part(name)
	if !ok {
		return nil
	}
	var v T
	if xml.Unmarshal(data, &v) != nil {
		return nil
	}
	return &v
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
