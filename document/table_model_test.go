package document_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/h0rn3t/docx2pdf/document"
	"github.com/h0rn3t/docx2pdf/internal/docxtest"
)

// tbl wraps rows into a <w:tbl> with the given w:tblPr contents.
func tbl(tblPr, rows string) string {
	if tblPr != "" {
		tblPr = `<w:tblPr>` + tblPr + `</w:tblPr>`
	}
	return `<w:tbl>` + tblPr + rows + `</w:tbl>`
}

// --- Cell margins (w:tcMar / w:tblCellMar) ---

func TestCellMargins_WordDefaultsWhenNothingIsDeclared(t *testing.T) {
	doc := mustOpenWith(t, tbl("", `<w:tr><w:tc><w:p><w:r><w:t>x</w:t></w:r></w:p></w:tc></w:tr>`), nil)

	m := singleTable(t, doc).Rows[0].Cells[0].Margins
	want := document.CellMargins{LeftPt: 5.4, RightPt: 5.4}
	if m != want {
		t.Fatalf("margins = %+v, want %+v (Word's 108 dxa left/right, none top/bottom)", m, want)
	}
}

func TestCellMargins_TableLevelThenCellLevel(t *testing.T) {
	tblCellMar := `<w:tblCellMar>` +
		`<w:top w:w="120" w:type="dxa"/><w:left w:w="240" w:type="dxa"/>` +
		`<w:bottom w:w="120" w:type="dxa"/><w:right w:w="240" w:type="dxa"/></w:tblCellMar>`
	rows := `<w:tr>` +
		`<w:tc><w:p><w:r><w:t>a</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:tcPr><w:tcMar><w:left w:w="600" w:type="dxa"/></w:tcMar></w:tcPr>` +
		`<w:p><w:r><w:t>b</w:t></w:r></w:p></w:tc>` +
		`</w:tr>`
	doc := mustOpenWith(t, tbl(tblCellMar, rows), nil)

	cells := singleTable(t, doc).Rows[0].Cells
	fromTable := document.CellMargins{TopPt: 6, BottomPt: 6, LeftPt: 12, RightPt: 12}
	if cells[0].Margins != fromTable {
		t.Errorf("first cell margins = %+v, want %+v (the table's w:tblCellMar)", cells[0].Margins, fromTable)
	}
	// The cell's own w:tcMar wins on the side it declares; the rest still come
	// from the table.
	own := document.CellMargins{TopPt: 6, BottomPt: 6, LeftPt: 30, RightPt: 12}
	if cells[1].Margins != own {
		t.Errorf("second cell margins = %+v, want %+v (its own left, the table's other sides)", cells[1].Margins, own)
	}
}

func TestCellMargins_FromTableStyle(t *testing.T) {
	styles := docxtest.Styles(`<w:style w:type="table" w:styleId="S"><w:tblPr>` +
		`<w:tblCellMar><w:left w:w="360" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>`)
	body := tbl(`<w:tblStyle w:val="S"/>`, `<w:tr><w:tc><w:p><w:r><w:t>x</w:t></w:r></w:p></w:tc></w:tr>`)
	doc := mustOpenWith(t, body, map[string]string{"word/styles.xml": styles})

	if got := singleTable(t, doc).Rows[0].Cells[0].Margins.LeftPt; got != 18 {
		t.Fatalf("left margin = %.1f, want 18 (360 dxa from the table style)", got)
	}
}

// --- Column widths ---

func TestColumnWidths_AutoWhenNeitherGridNorCellDeclaresOne(t *testing.T) {
	rows := `<w:tr><w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/></w:tcPr>` +
		`<w:p><w:r><w:t>x</w:t></w:r></w:p></w:tc></w:tr>`
	doc := mustOpenWith(t, tbl("", rows), nil)

	w := singleTable(t, doc).ColumnWidths
	if len(w) != 1 || w[0] != 0 {
		t.Fatalf("column widths = %v, want [0] (an auto column, sized from content at layout)", w)
	}
}

func TestColumnWidths_PercentCellWidth(t *testing.T) {
	rows := `<w:tr>` +
		`<w:tc><w:tcPr><w:tcW w:w="2500" w:type="pct"/></w:tcPr><w:p/></w:tc>` +
		`<w:tc><w:tcPr><w:tcW w:w="75%" w:type="pct"/></w:tcPr><w:p/></w:tc>` +
		`</w:tr>`
	doc := mustOpenWith(t, tbl("", rows), nil)

	table := singleTable(t, doc)
	if got := table.ColumnPercents; len(got) != 2 || got[0] != 50 || got[1] != 75 {
		t.Fatalf("column percents = %v, want [50 75] (2500 fiftieths of a percent, and a literal 75%%)", got)
	}
	for i, w := range table.ColumnWidths {
		if w != 0 {
			t.Errorf("column %d width = %.1f, want 0 (it is sized by percentage)", i, w)
		}
	}
}

func TestCellMargins_PercentSideKeepsItsPercentage(t *testing.T) {
	rows := `<w:tr><w:tc><w:tcPr><w:tcMar><w:left w:w="500" w:type="pct"/></w:tcMar></w:tcPr>` +
		`<w:p/></w:tc></w:tr>`
	doc := mustOpenWith(t, tbl("", rows), nil)

	m := singleTable(t, doc).Rows[0].Cells[0].Margins
	if m.LeftPct != 10 || m.LeftPt != 0 { // 500 fiftieths of a percent = 10%
		t.Fatalf("left margin = %.1f pt / %.1f%%, want 0 pt / 10%% (resolved against the table's width at layout)",
			m.LeftPt, m.LeftPct)
	}
}

// --- The table's own width (w:tblW) and floating placement (w:tblpPr) ---

func TestTableWidth_DxaAndPct(t *testing.T) {
	rows := `<w:tr><w:tc><w:p/></w:tc></w:tr>`
	fixed := mustOpenWith(t, tbl(`<w:tblW w:w="5760" w:type="dxa"/>`, rows), nil)
	pct := mustOpenWith(t, tbl(`<w:tblW w:w="2500" w:type="pct"/>`, rows), nil)
	auto := mustOpenWith(t, tbl(`<w:tblW w:w="0" w:type="auto"/>`, rows), nil)

	if got := singleTable(t, fixed).WidthPt; got != 288 {
		t.Errorf("table width = %.1f pt, want 288 (5760 dxa)", got)
	}
	if got := singleTable(t, pct).WidthPct; got != 50 {
		t.Errorf("table width = %.1f%%, want 50 (2500 fiftieths of a percent)", got)
	}
	if table := singleTable(t, auto); table.WidthPt != 0 || table.WidthPct != 0 {
		t.Errorf("auto table width = %.1f pt / %.1f%%, want neither", table.WidthPt, table.WidthPct)
	}
}

func TestTableFloat_OffsetsAndAnchors(t *testing.T) {
	props := `<w:tblpPr w:tblpX="720" w:tblpY="1440" w:horzAnchor="margin" w:vertAnchor="page"/>`
	doc := mustOpenWith(t, tbl(props, `<w:tr><w:tc><w:p/></w:tc></w:tr>`), nil)

	fl := singleTable(t, doc).Float
	if fl == nil {
		t.Fatal("expected a floating table")
	}
	if fl.XPt != 36 || fl.YPt != 72 {
		t.Errorf("offset = (%.1f, %.1f) pt, want (36, 72)", fl.XPt, fl.YPt)
	}
	if fl.HorzAnchor != document.AnchorMargin || fl.VertAnchor != document.AnchorPage {
		t.Errorf("anchors = %v/%v, want margin/page", fl.HorzAnchor, fl.VertAnchor)
	}
}

func TestTableFloat_AlignmentSpecAndDefaultAnchor(t *testing.T) {
	doc := mustOpenWith(t, tbl(`<w:tblpPr w:tblpXSpec="center"/>`, `<w:tr><w:tc><w:p/></w:tc></w:tr>`), nil)

	fl := singleTable(t, doc).Float
	if fl.XSpec != "center" {
		t.Errorf("XSpec = %q, want center", fl.XSpec)
	}
	if fl.HorzAnchor != document.AnchorText {
		t.Errorf("horzAnchor = %v, want AnchorText (the default)", fl.HorzAnchor)
	}
}

func TestTable_NotFloatingWithoutTblpPr(t *testing.T) {
	doc := mustOpenWith(t, tbl("", `<w:tr><w:tc><w:p/></w:tc></w:tr>`), nil)

	if got := singleTable(t, doc).Float; got != nil {
		t.Fatalf("Float = %+v, want nil for a table that flows with the text", got)
	}
}

// --- Header rows (w:tblHeader) ---

func TestHeaderRows_LeadingMarkedRowsOnly(t *testing.T) {
	header := `<w:tr><w:trPr><w:tblHeader/></w:trPr><w:tc><w:p><w:r><w:t>h</w:t></w:r></w:p></w:tc></w:tr>`
	body := `<w:tr><w:tc><w:p><w:r><w:t>b</w:t></w:r></w:p></w:tc></w:tr>`
	// A w:tblHeader further down does not make that row repeat, and Word ignores
	// it too - only the leading block of header rows repeats.
	doc := mustOpenWith(t, tbl("", header+header+body+header), nil)

	table := singleTable(t, doc)
	if table.HeaderRows != 2 {
		t.Fatalf("HeaderRows = %d, want 2 (only the leading marked rows)", table.HeaderRows)
	}
	if !table.Rows[0].Header || !table.Rows[1].Header || table.Rows[2].Header {
		t.Fatal("the first two rows must be header rows and the third must not")
	}
}

// --- Text direction and diagonal borders ---

func TestCell_TextDirection(t *testing.T) {
	rows := `<w:tr>` +
		`<w:tc><w:tcPr><w:textDirection w:val="btLr"/></w:tcPr><w:p/></w:tc>` +
		`<w:tc><w:tcPr><w:textDirection w:val="tbRl"/></w:tcPr><w:p/></w:tc>` +
		`<w:tc><w:p/></w:tc>` +
		`</w:tr>`
	doc := mustOpenWith(t, tbl("", rows), nil)

	cells := singleTable(t, doc).Rows[0].Cells
	want := []document.TextDirection{
		document.TextDirectionBTLR, document.TextDirectionTBRL, document.TextDirectionHorizontal,
	}
	for i, w := range want {
		if got := cells[i].TextDirection; got != w {
			t.Errorf("cell %d text direction = %v, want %v", i, got, w)
		}
	}
}

func TestCell_DiagonalBorders(t *testing.T) {
	rows := `<w:tr><w:tc><w:tcPr><w:tcBorders>` +
		`<w:tl2br w:val="single" w:sz="8" w:color="FF0000"/>` +
		`<w:tr2bl w:val="dashed" w:sz="4" w:color="00FF00"/>` +
		`</w:tcBorders></w:tcPr><w:p/></w:tc></w:tr>`
	doc := mustOpenWith(t, tbl("", rows), nil)

	b := singleTable(t, doc).Rows[0].Cells[0].Borders
	if b.DiagDown.Style != "single" || b.DiagDown.WidthPt != 1 || b.DiagDown.Color != "#FF0000" {
		t.Errorf("tl2br = %+v, want a 1 pt single red diagonal", b.DiagDown)
	}
	if b.DiagUp.Style != "dashed" || b.DiagUp.Color != "#00FF00" {
		t.Errorf("tr2bl = %+v, want a dashed green diagonal", b.DiagUp)
	}
}

// --- Vertical alignment, row height, table layout, inside grid lines ---

func TestCell_VAlign(t *testing.T) {
	rows := `<w:tr>` +
		`<w:tc><w:tcPr><w:vAlign w:val="center"/></w:tcPr><w:p/></w:tc>` +
		`<w:tc><w:tcPr><w:vAlign w:val="bottom"/></w:tcPr><w:p/></w:tc>` +
		`<w:tc><w:p/></w:tc></w:tr>`
	doc := mustOpenWith(t, tbl("", rows), nil)

	cells := singleTable(t, doc).Rows[0].Cells
	want := []document.VerticalAlign{document.VAlignCenter, document.VAlignBottom, document.VAlignTop}
	for i, w := range want {
		if got := cells[i].VAlign; got != w {
			t.Errorf("cell %d vAlign = %v, want %v", i, got, w)
		}
	}
}

func TestRow_TrHeight(t *testing.T) {
	rows := `<w:tr><w:trPr><w:trHeight w:val="1440" w:hRule="exact"/></w:trPr><w:tc><w:p/></w:tc></w:tr>` +
		`<w:tr><w:trPr><w:trHeight w:val="720"/></w:trPr><w:tc><w:p/></w:tc></w:tr>` +
		`<w:tr><w:trPr><w:trHeight w:val="720" w:hRule="auto"/></w:trPr><w:tc><w:p/></w:tc></w:tr>`
	doc := mustOpenWith(t, tbl("", rows), nil)

	got := singleTable(t, doc).Rows
	if got[0].HeightRule != document.RowHeightExact || got[0].HeightPt != 72 {
		t.Errorf("row 0 = %v/%.1f pt, want exact/72 (1440 dxa)", got[0].HeightRule, got[0].HeightPt)
	}
	if got[1].HeightRule != document.RowHeightAtLeast || got[1].HeightPt != 36 {
		t.Errorf("row 1 = %v/%.1f pt, want atLeast/36 (an absent w:hRule is a minimum)", got[1].HeightRule, got[1].HeightPt)
	}
	if got[2].HeightRule != document.RowHeightAuto {
		t.Errorf("row 2 = %v, want auto (w:hRule=auto leaves the row content-sized)", got[2].HeightRule)
	}
}

func TestTable_FixedLayout(t *testing.T) {
	rows := `<w:tr><w:tc><w:p/></w:tc></w:tr>`
	fixed := mustOpenWith(t, tbl(`<w:tblLayout w:type="fixed"/>`, rows), nil)
	auto := mustOpenWith(t, tbl(`<w:tblLayout w:type="autofit"/>`, rows), nil)

	if !singleTable(t, fixed).FixedLayout {
		t.Error("w:tblLayout type=fixed must set FixedLayout")
	}
	if singleTable(t, auto).FixedLayout {
		t.Error("w:tblLayout type=autofit must leave the table auto-fitting")
	}
}

func TestBorders_InsideLinesApplyBetweenCellsOnly(t *testing.T) {
	borders := `<w:tblBorders>` +
		`<w:top w:val="single" w:sz="16" w:color="000000"/>` +
		`<w:bottom w:val="single" w:sz="16" w:color="000000"/>` +
		`<w:left w:val="single" w:sz="16" w:color="000000"/>` +
		`<w:right w:val="single" w:sz="16" w:color="000000"/>` +
		`<w:insideH w:val="single" w:sz="4" w:color="AAAAAA"/>` +
		`<w:insideV w:val="single" w:sz="4" w:color="BBBBBB"/>` +
		`</w:tblBorders>`
	rows := `<w:tr><w:tc><w:p/></w:tc><w:tc><w:p/></w:tc></w:tr>` +
		`<w:tr><w:tc><w:p/></w:tc><w:tc><w:p/></w:tc></w:tr>`
	doc := mustOpenWith(t, tbl(borders, rows), nil)

	table := singleTable(t, doc)
	topLeft := table.Rows[0].Cells[0].Borders
	if topLeft.Top.WidthPt != 2 || topLeft.Left.WidthPt != 2 {
		t.Errorf("top-left outer sides = %+v/%+v, want the 2 pt outline", topLeft.Top, topLeft.Left)
	}
	if topLeft.Bottom.WidthPt != 0.5 || topLeft.Bottom.Color != "#AAAAAA" {
		t.Errorf("top-left bottom = %+v, want the 0.5 pt insideH line", topLeft.Bottom)
	}
	if topLeft.Right.WidthPt != 0.5 || topLeft.Right.Color != "#BBBBBB" {
		t.Errorf("top-left right = %+v, want the 0.5 pt insideV line", topLeft.Right)
	}
	bottomRight := table.Rows[1].Cells[1].Borders
	if bottomRight.Bottom.WidthPt != 2 || bottomRight.Right.WidthPt != 2 {
		t.Errorf("bottom-right outer sides = %+v/%+v, want the 2 pt outline", bottomRight.Bottom, bottomRight.Right)
	}
	if bottomRight.Top.WidthPt != 0.5 || bottomRight.Left.WidthPt != 0.5 {
		t.Errorf("bottom-right inner sides = %+v/%+v, want the inside lines", bottomRight.Top, bottomRight.Left)
	}
}

// --- Conditional paragraph/run formatting in w:tblStylePr ---

func TestTableStyle_ConditionalRunAndParagraphFormatting(t *testing.T) {
	styles := docxtest.Styles(
		`<w:style w:type="table" w:styleId="S">` +
			`<w:tblStylePr w:type="firstRow">` +
			`<w:pPr><w:jc w:val="center"/></w:pPr>` +
			`<w:rPr><w:b/><w:color w:val="FFFFFF"/></w:rPr>` +
			`</w:tblStylePr>` +
			`</w:style>`)
	rows := `<w:tr><w:tc><w:p><w:r><w:t>head</w:t></w:r></w:p></w:tc></w:tr>` +
		`<w:tr><w:tc><w:p><w:r><w:t>body</w:t></w:r></w:p></w:tc></w:tr>`
	doc := mustOpenWith(t, tbl(`<w:tblStyle w:val="S"/>`, rows), map[string]string{"word/styles.xml": styles})

	table := singleTable(t, doc)
	head := table.Rows[0].Cells[0].Paragraphs[0]
	if !head.Runs[0].Props.Bold || head.Runs[0].Props.Color != "#FFFFFF" {
		t.Errorf("header run = %+v, want bold and white (the firstRow region's w:rPr)", head.Runs[0].Props)
	}
	if head.Props.Alignment != document.AlignCenter {
		t.Errorf("header alignment = %v, want AlignCenter (the firstRow region's w:pPr)", head.Props.Alignment)
	}

	body := table.Rows[1].Cells[0].Paragraphs[0]
	if body.Runs[0].Props.Bold || body.Props.Alignment != document.AlignLeft {
		t.Errorf("body cell = %+v / %v, want unformatted (the region covers the first row only)",
			body.Runs[0].Props, body.Props.Alignment)
	}
}

func TestTableStyle_ParagraphStyleWinsOverTheTableStyle(t *testing.T) {
	styles := docxtest.Styles(
		`<w:style w:type="table" w:styleId="S">` +
			`<w:tblStylePr w:type="firstRow"><w:rPr><w:b/></w:rPr></w:tblStylePr></w:style>` +
			`<w:style w:type="paragraph" w:styleId="P"><w:rPr><w:b w:val="false"/></w:rPr></w:style>`)
	rows := `<w:tr><w:tc><w:p><w:pPr><w:pStyle w:val="P"/></w:pPr><w:r><w:t>head</w:t></w:r></w:p></w:tc></w:tr>`
	doc := mustOpenWith(t, tbl(`<w:tblStyle w:val="S"/>`, rows), map[string]string{"word/styles.xml": styles})

	if got := singleTable(t, doc).Rows[0].Cells[0].Paragraphs[0].Runs[0].Props.Bold; got {
		t.Fatal("the paragraph style's explicit non-bold must win over the table style's bold region")
	}
}

// TestConvertStyledTableEndToEnd renders a table that uses every new table
// feature at once - a repeated header row whose style makes it bold, a vertical
// cell, a diagonal border, and auto-sized columns - and checks the PDF carries
// the text and the rotation transform.
func TestConvertStyledTableEndToEnd(t *testing.T) {
	styles := docxtest.Styles(`<w:style w:type="table" w:styleId="S">` +
		`<w:tblStylePr w:type="firstRow"><w:rPr><w:b/></w:rPr>` +
		`<w:tcPr><w:shd w:fill="DDDDDD"/></w:tcPr></w:tblStylePr></w:style>`)
	rows := `<w:tr><w:trPr><w:tblHeader/></w:trPr>` +
		`<w:tc><w:p><w:r><w:t>Заголовок</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:tcPr><w:textDirection w:val="btLr"/></w:tcPr>` +
		`<w:p><w:r><w:t>Вертикально</w:t></w:r></w:p></w:tc></w:tr>` +
		`<w:tr><w:tc><w:tcPr><w:tcBorders><w:tl2br w:val="single" w:sz="8" w:color="000000"/></w:tcBorders></w:tcPr>` +
		`<w:p><w:r><w:t>Комірка</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:p><w:r><w:t>Друга</w:t></w:r></w:p></w:tc></w:tr>`
	doc := mustOpenWith(t, tbl(`<w:tblStyle w:val="S"/>`, rows), map[string]string{"word/styles.xml": styles})

	var buf bytes.Buffer
	if err := document.ConvertToPdf(doc).Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	text := decodePDFText(buf.Bytes())
	for _, want := range []string{"Заголовок", "Вертикально", "Комірка", "Друга"} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered text is missing %q", want)
		}
	}
	// A rotated cell is drawn inside a saved/restored transform block.
	if content := decodePDFStreams(buf.Bytes()); !bytes.Contains(content, []byte(" cm")) {
		t.Error("expected a transform matrix (the vertical cell's rotation) in the page content")
	}
}

// TestConvertFloatingAndClippedTableEndToEnd renders a floating half-width table
// whose row has a fixed height, and checks the PDF carries the text and the
// clipping path.
func TestConvertFloatingAndClippedTableEndToEnd(t *testing.T) {
	props := `<w:tblW w:w="2500" w:type="pct"/>` +
		`<w:tblpPr w:tblpX="720" w:tblpY="720" w:horzAnchor="margin" w:vertAnchor="margin"/>`
	rows := `<w:tr><w:trPr><w:trHeight w:val="240" w:hRule="exact"/></w:trPr>` +
		`<w:tc><w:p><w:r><w:t>Плаваюча</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Обрізана</w:t></w:r></w:p></w:tc></w:tr>`
	doc := mustOpenWith(t, tbl(props, rows)+`<w:p><w:r><w:t>Текст поруч</w:t></w:r></w:p>`, nil)

	var buf bytes.Buffer
	if err := document.ConvertToPdf(doc).Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	text := decodePDFText(buf.Bytes())
	for _, want := range []string{"Плаваюча", "Текст"} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered text is missing %q", want)
		}
	}
	// fpdf's ClipRect ends its path with the "W n" clipping operator.
	if content := decodePDFStreams(buf.Bytes()); !bytes.Contains(content, []byte("W n")) {
		t.Error("expected a clipping path (the exact-height row) in the page content")
	}
}

func TestTableStyle_CornerRegionWinsOverRowAndColumn(t *testing.T) {
	styles := docxtest.Styles(
		`<w:style w:type="table" w:styleId="S">` +
			`<w:tblStylePr w:type="firstRow"><w:tcPr><w:shd w:fill="111111"/></w:tcPr></w:tblStylePr>` +
			`<w:tblStylePr w:type="firstCol"><w:tcPr><w:shd w:fill="222222"/></w:tcPr></w:tblStylePr>` +
			`<w:tblStylePr w:type="nwCell"><w:tcPr><w:shd w:fill="333333"/></w:tcPr></w:tblStylePr>` +
			`</w:style>`)
	rows := `<w:tr>` +
		`<w:tc><w:p><w:r><w:t>corner</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:p><w:r><w:t>top</w:t></w:r></w:p></w:tc></w:tr>` +
		`<w:tr><w:tc><w:p><w:r><w:t>left</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:p><w:r><w:t>plain</w:t></w:r></w:p></w:tc></w:tr>`
	props := `<w:tblStyle w:val="S"/>` +
		`<w:tblLook w:firstRow="1" w:lastRow="0" w:firstColumn="1" w:lastColumn="0" w:noHBand="1" w:noVBand="1"/>`
	doc := mustOpenWith(t, tbl(props, rows), map[string]string{"word/styles.xml": styles})

	table := singleTable(t, doc)
	if got := table.Rows[0].Cells[0].Shading; got != "#333333" {
		t.Errorf("corner cell shading = %q, want #333333 (nwCell beats both firstRow and firstCol)", got)
	}
	if got := table.Rows[0].Cells[1].Shading; got != "#111111" {
		t.Errorf("first-row cell shading = %q, want #111111", got)
	}
	if got := table.Rows[1].Cells[0].Shading; got != "#222222" {
		t.Errorf("first-column cell shading = %q, want #222222", got)
	}
	if got := table.Rows[1].Cells[1].Shading; got != "" {
		t.Errorf("plain cell shading = %q, want none", got)
	}
}
