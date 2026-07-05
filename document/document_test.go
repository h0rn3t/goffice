package document_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/h0rn3t/goffice/document"
	"github.com/h0rn3t/goffice/internal/docxtest"
)

func TestOpen_ValidDocument(t *testing.T) {
	path := docxtest.Build(t, `<w:p><w:r><w:t>hello</w:t></w:r></w:p>`)

	doc, err := document.Open(path)
	if err != nil {
		t.Fatalf("Open: unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("Open: expected non-nil document")
	}
	t.Cleanup(func() { _ = doc.Close() })
}

func TestOpen_MissingFile(t *testing.T) {
	doc, err := document.Open(filepath.Join(t.TempDir(), "nope.docx"))
	if err == nil {
		t.Fatal("Open: expected error for missing file")
	}
	if doc != nil {
		t.Fatal("Open: expected nil document on error")
	}
}

func TestOpen_NotAValidZip(t *testing.T) {
	doc, err := document.Open(docxtest.Corrupt(t))
	if err == nil {
		t.Fatal("Open: expected error for non-zip file")
	}
	if doc != nil {
		t.Fatal("Open: expected nil document on error")
	}
}

func TestOpen_MissingDocumentPart(t *testing.T) {
	path := docxtest.BuildParts(t, map[string]string{
		"[Content_Types].xml": "<Types/>",
	})
	doc, err := document.Open(path)
	if err == nil {
		t.Fatal("Open: expected error when word/document.xml is missing")
	}
	if doc != nil {
		t.Fatal("Open: expected nil document on error")
	}
}

func TestClose_AfterOpen(t *testing.T) {
	doc := mustOpen(t, `<w:p><w:r><w:t>x</w:t></w:r></w:p>`)
	if err := doc.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	doc := mustOpen(t, `<w:p><w:r><w:t>x</w:t></w:r></w:p>`)
	if err := doc.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := doc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestParagraphAndRunOrdering(t *testing.T) {
	body := `<w:p><w:r><w:t>Hello </w:t></w:r><w:r><w:t>world</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Second</w:t></w:r><w:r><w:t> line</w:t></w:r></w:p>`
	doc := mustOpen(t, body)

	paras := paragraphs(t, doc)
	if got, want := len(paras), 2; got != want {
		t.Fatalf("paragraph count = %d, want %d", got, want)
	}
	if got, want := concatText(paras), "Hello worldSecond line"; got != want {
		t.Fatalf("concatenated text = %q, want %q", got, want)
	}
	if got := len(paras[0].Runs); got != 2 {
		t.Fatalf("paragraph 0 run count = %d, want 2", got)
	}
}

func TestEmptyParagraphPreserved(t *testing.T) {
	body := `<w:p><w:r><w:t>first</w:t></w:r></w:p><w:p/><w:p><w:r><w:t>third</w:t></w:r></w:p>`
	doc := mustOpen(t, body)

	paras := paragraphs(t, doc)
	if got, want := len(paras), 3; got != want {
		t.Fatalf("paragraph count = %d, want %d", got, want)
	}
	if got := len(paras[1].Runs); got != 0 {
		t.Fatalf("empty paragraph run count = %d, want 0", got)
	}
}

func TestBoldItalicRun(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:b/><w:i/></w:rPr><w:t>strong</w:t></w:r></w:p>`
	doc := mustOpen(t, body)

	p := paragraphs(t, doc)[0].Runs[0].Props
	if !p.Bold || !p.Italic {
		t.Fatalf("bold=%v italic=%v, want both true", p.Bold, p.Italic)
	}
}

func TestExplicitFontSizeHalfPoints(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:sz w:val="28"/></w:rPr><w:t>big</w:t></w:r></w:p>`
	doc := mustOpen(t, body)

	if got, want := paragraphs(t, doc)[0].Runs[0].Props.SizePt, 14.0; got != want {
		t.Fatalf("size = %v pt, want %v pt", got, want)
	}
}

func TestMissingFormattingUsesDefaults(t *testing.T) {
	doc := mustOpen(t, `<w:p><w:r><w:t>plain</w:t></w:r></w:p>`)

	p := paragraphs(t, doc)[0].Runs[0].Props
	if p.Bold || p.Italic || p.Underline {
		t.Fatalf("expected no formatting, got %+v", p)
	}
	if p.FontFamily == "" {
		t.Fatal("expected a non-empty default font family")
	}
	if p.SizePt <= 0 {
		t.Fatalf("expected a positive default size, got %v", p.SizePt)
	}
}

func TestUnderlineRun(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:u w:val="single"/></w:rPr><w:t>u</w:t></w:r></w:p>`
	doc := mustOpen(t, body)
	if !paragraphs(t, doc)[0].Runs[0].Props.Underline {
		t.Fatal("expected underline true")
	}
}

func TestFontFamilyFromRFonts(t *testing.T) {
	body := `<w:p><w:r><w:rPr><w:rFonts w:ascii="Times New Roman"/></w:rPr><w:t>t</w:t></w:r></w:p>`
	doc := mustOpen(t, body)
	if got := paragraphs(t, doc)[0].Runs[0].Props.FontFamily; got != "Times New Roman" {
		t.Fatalf("font family = %q, want %q", got, "Times New Roman")
	}
}

func TestCenteredParagraph(t *testing.T) {
	body := `<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>c</w:t></w:r></w:p>`
	doc := mustOpen(t, body)
	if got := paragraphs(t, doc)[0].Props.Alignment; got != document.AlignCenter {
		t.Fatalf("alignment = %v, want AlignCenter", got)
	}
}

func TestExplicitPageBreak(t *testing.T) {
	body := `<w:p><w:r><w:t>page1</w:t></w:r></w:p>` +
		`<w:p><w:r><w:br w:type="page"/></w:r><w:r><w:t>page2</w:t></w:r></w:p>`
	doc := mustOpen(t, body)

	paras := paragraphs(t, doc)
	if paras[0].Props.PageBreak {
		t.Fatal("paragraph 0 should not have a page break")
	}
	if !paras[1].Props.PageBreak {
		t.Fatal("paragraph 1 should be marked with a page break")
	}
}

func TestParagraphsAndTablesInterleaveInBodyOrder(t *testing.T) {
	body := `<w:p><w:r><w:t>before</w:t></w:r></w:p>` +
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
		`<w:p><w:r><w:t>after</w:t></w:r></w:p>`
	doc := mustOpen(t, body)

	if got, want := len(doc.Body), 3; got != want {
		t.Fatalf("body element count = %d, want %d", got, want)
	}
	if doc.Body[0].Paragraph == nil || doc.Body[0].Table != nil {
		t.Fatalf("body[0] = %+v, want a paragraph", doc.Body[0])
	}
	if doc.Body[1].Table == nil || doc.Body[1].Paragraph != nil {
		t.Fatalf("body[1] = %+v, want a table", doc.Body[1])
	}
	if doc.Body[2].Paragraph == nil || doc.Body[2].Table != nil {
		t.Fatalf("body[2] = %+v, want a paragraph", doc.Body[2])
	}
	if got, want := doc.Body[0].Paragraph.Runs[0].Text, "before"; got != want {
		t.Fatalf("body[0] text = %q, want %q", got, want)
	}
	if got, want := doc.Body[2].Paragraph.Runs[0].Text, "after"; got != want {
		t.Fatalf("body[2] text = %q, want %q", got, want)
	}
	if got, want := doc.Body[1].Table.Rows[0].Cells[0].Paragraphs[0].Runs[0].Text, "cell"; got != want {
		t.Fatalf("table cell text = %q, want %q", got, want)
	}
}

func TestTable_SimpleParse(t *testing.T) {
	body := `<w:tbl>` +
		`<w:tr><w:tc><w:p><w:r><w:t>A1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>B1</w:t></w:r></w:p></w:tc></w:tr>` +
		`<w:tr><w:tc><w:p><w:r><w:t>A2</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>B2</w:t></w:r></w:p></w:tc></w:tr>` +
		`</w:tbl>`
	doc := mustOpen(t, body)

	tbl := singleTable(t, doc)
	if got, want := len(tbl.Rows), 2; got != want {
		t.Fatalf("row count = %d, want %d", got, want)
	}
	if got, want := len(tbl.Rows[0].Cells), 2; got != want {
		t.Fatalf("cell count = %d, want %d", got, want)
	}
	if got, want := tbl.Rows[0].Cells[0].Paragraphs[0].Runs[0].Text, "A1"; got != want {
		t.Fatalf("cell[0][0] text = %q, want %q", got, want)
	}
	if got, want := tbl.Rows[1].Cells[1].Paragraphs[0].Runs[0].Text, "B2"; got != want {
		t.Fatalf("cell[1][1] text = %q, want %q", got, want)
	}
}

func TestTable_ColumnWidthsFromTblGrid(t *testing.T) {
	body := `<w:tbl><w:tblGrid><w:gridCol w:w="1440"/><w:gridCol w:w="2880"/></w:tblGrid>` +
		`<w:tr><w:tc><w:p/></w:tc><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	doc := mustOpen(t, body)

	tbl := singleTable(t, doc)
	want := []float64{72, 144} // 1440/20, 2880/20 twentieths-of-a-point -> points
	if len(tbl.ColumnWidths) != len(want) {
		t.Fatalf("column widths = %v, want %v", tbl.ColumnWidths, want)
	}
	for i, w := range want {
		if tbl.ColumnWidths[i] != w {
			t.Errorf("column %d width = %v, want %v", i, tbl.ColumnWidths[i], w)
		}
	}
}

func TestTable_GridSpan(t *testing.T) {
	body := `<w:tbl><w:tr><w:tc><w:tcPr><w:gridSpan w:val="3"/></w:tcPr><w:p><w:r><w:t>wide</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	doc := mustOpen(t, body)

	cell := singleTable(t, doc).Rows[0].Cells[0]
	if got, want := cell.ColSpan, 3; got != want {
		t.Fatalf("ColSpan = %d, want %d", got, want)
	}
}

func TestTable_VMergeRestartAndContinue(t *testing.T) {
	body := `<w:tbl>` +
		`<w:tr><w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr><w:p><w:r><w:t>top</w:t></w:r></w:p></w:tc></w:tr>` +
		`<w:tr><w:tc><w:tcPr><w:vMerge/></w:tcPr><w:p/></w:tc></w:tr>` +
		`</w:tbl>`
	doc := mustOpen(t, body)

	tbl := singleTable(t, doc)
	if got := tbl.Rows[0].Cells[0].VMerge; got != document.VMergeRestart {
		t.Fatalf("row 0 VMerge = %v, want VMergeRestart", got)
	}
	if got := tbl.Rows[1].Cells[0].VMerge; got != document.VMergeContinue {
		t.Fatalf("row 1 VMerge = %v, want VMergeContinue", got)
	}
}

func TestTable_TableLevelBordersApplyToAllCells(t *testing.T) {
	body := `<w:tbl><w:tblPr><w:tblBorders><w:top w:val="single" w:sz="8" w:color="000000"/></w:tblBorders></w:tblPr>` +
		`<w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	doc := mustOpen(t, body)

	top := singleTable(t, doc).Rows[0].Cells[0].Borders.Top
	if top.Style != "single" || top.WidthPt != 1 || top.Color != "#000000" {
		t.Fatalf("top border = %+v, want {single 1 #000000}", top)
	}
}

func TestTable_CellBorderOverridesTableBorder(t *testing.T) {
	body := `<w:tbl><w:tblPr><w:tblBorders><w:top w:val="single" w:sz="8" w:color="000000"/></w:tblBorders></w:tblPr>` +
		`<w:tr><w:tc><w:tcPr><w:tcBorders><w:top w:val="double" w:sz="16" w:color="FF0000"/></w:tcBorders></w:tcPr><w:p/></w:tc></w:tr></w:tbl>`
	doc := mustOpen(t, body)

	top := singleTable(t, doc).Rows[0].Cells[0].Borders.Top
	if top.Style != "double" || top.WidthPt != 2 || top.Color != "#FF0000" {
		t.Fatalf("top border = %+v, want {double 2 #FF0000}", top)
	}
}

func TestTable_CellShading(t *testing.T) {
	body := `<w:tbl><w:tr><w:tc><w:tcPr><w:shd w:fill="D9D9D9"/></w:tcPr><w:p/></w:tc></w:tr></w:tbl>`
	doc := mustOpen(t, body)

	if got, want := singleTable(t, doc).Rows[0].Cells[0].Shading, "#D9D9D9"; got != want {
		t.Fatalf("shading = %q, want %q", got, want)
	}
}

func TestTable_NestedTable(t *testing.T) {
	body := `<w:tbl><w:tr><w:tc>` +
		`<w:p><w:r><w:t>outer</w:t></w:r></w:p>` +
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>inner</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
		`</w:tc></w:tr></w:tbl>`
	doc := mustOpen(t, body)

	cell := singleTable(t, doc).Rows[0].Cells[0]
	if got, want := cell.Paragraphs[0].Runs[0].Text, "outer"; got != want {
		t.Fatalf("outer cell text = %q, want %q", got, want)
	}
	if cell.Nested == nil {
		t.Fatal("expected a nested table")
	}
	if got, want := cell.Nested.Rows[0].Cells[0].Paragraphs[0].Runs[0].Text, "inner"; got != want {
		t.Fatalf("nested cell text = %q, want %q", got, want)
	}
}

func TestTable_StyleBordersApplyWhenNoInlineBorders(t *testing.T) {
	body := `<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/></w:tblPr>` +
		`<w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	styles := `<w:style w:type="table" w:styleId="TableGrid">` +
		`<w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:color="auto"/></w:tblBorders></w:tblPr>` +
		`</w:style>`
	doc := mustOpenWithStyles(t, body, styles)

	top := singleTable(t, doc).Rows[0].Cells[0].Borders.Top
	if top.Style != "single" || top.WidthPt != 0.5 || top.Color != "#000000" {
		t.Fatalf("top border = %+v, want {single 0.5 #000000}", top)
	}
}

func TestTable_InlineTableBordersOverrideStyle(t *testing.T) {
	body := `<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/>` +
		`<w:tblBorders><w:top w:val="double" w:sz="16" w:color="FF0000"/></w:tblBorders></w:tblPr>` +
		`<w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	styles := `<w:style w:type="table" w:styleId="TableGrid">` +
		`<w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:color="auto"/></w:tblBorders></w:tblPr>` +
		`</w:style>`
	doc := mustOpenWithStyles(t, body, styles)

	top := singleTable(t, doc).Rows[0].Cells[0].Borders.Top
	if top.Style != "double" || top.WidthPt != 2 || top.Color != "#FF0000" {
		t.Fatalf("top border = %+v, want {double 2 #FF0000}", top)
	}
}

func TestTable_CellBorderOverridesStyle(t *testing.T) {
	body := `<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/></w:tblPr>` +
		`<w:tr><w:tc><w:tcPr><w:tcBorders><w:top w:val="double" w:sz="16" w:color="FF0000"/></w:tcBorders></w:tcPr><w:p/></w:tc></w:tr></w:tbl>`
	styles := `<w:style w:type="table" w:styleId="TableGrid">` +
		`<w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:color="auto"/></w:tblBorders></w:tblPr>` +
		`</w:style>`
	doc := mustOpenWithStyles(t, body, styles)

	top := singleTable(t, doc).Rows[0].Cells[0].Borders.Top
	if top.Style != "double" || top.WidthPt != 2 || top.Color != "#FF0000" {
		t.Fatalf("top border = %+v, want {double 2 #FF0000}", top)
	}
}

func TestTable_StyleBordersResolveThroughBasedOnChain(t *testing.T) {
	body := `<w:tbl><w:tblPr><w:tblStyle w:val="Child"/></w:tblPr>` +
		`<w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	styles := `<w:style w:type="table" w:styleId="Child"><w:basedOn w:val="Parent"/></w:style>` +
		`<w:style w:type="table" w:styleId="Parent">` +
		`<w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:color="auto"/></w:tblBorders></w:tblPr>` +
		`</w:style>`
	doc := mustOpenWithStyles(t, body, styles)

	top := singleTable(t, doc).Rows[0].Cells[0].Borders.Top
	if top.Style != "single" {
		t.Fatalf("top border style = %q, want %q (resolved from basedOn ancestor)", top.Style, "single")
	}
}

func TestTable_MissingStyleFallsBackToInlineOnly(t *testing.T) {
	body := `<w:tbl><w:tblPr><w:tblStyle w:val="DoesNotExist"/></w:tblPr>` +
		`<w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	doc := mustOpen(t, body) // no word/styles.xml part at all

	top := singleTable(t, doc).Rows[0].Cells[0].Borders.Top
	if top.Style != "" {
		t.Fatalf("top border = %+v, want no border (unresolvable style, no styles.xml)", top)
	}
}

func TestTable_StyleSuppliesCellShading(t *testing.T) {
	body := `<w:tbl><w:tblPr><w:tblStyle w:val="Shaded"/></w:tblPr>` +
		`<w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	styles := `<w:style w:type="table" w:styleId="Shaded"><w:tcPr><w:shd w:fill="D9D9D9"/></w:tcPr></w:style>`
	doc := mustOpenWithStyles(t, body, styles)

	if got, want := singleTable(t, doc).Rows[0].Cells[0].Shading, "#D9D9D9"; got != want {
		t.Fatalf("shading = %q, want %q", got, want)
	}
}

func mustOpenWithStyles(t *testing.T, bodyXML, stylesXML string) *document.Document {
	t.Helper()
	doc, err := document.Open(docxtest.BuildWithStyles(t, bodyXML, stylesXML))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = doc.Close() })
	return doc
}

func mustOpen(t *testing.T, bodyXML string) *document.Document {
	t.Helper()
	doc, err := document.Open(docxtest.Build(t, bodyXML))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = doc.Close() })
	return doc
}

// paragraphs extracts every paragraph-typed body element of doc, in order,
// failing the test if the body contains anything else.
func paragraphs(t *testing.T, doc *document.Document) []document.Paragraph {
	t.Helper()
	paras := make([]document.Paragraph, 0, len(doc.Body))
	for _, el := range doc.Body {
		if el.Paragraph == nil {
			t.Fatalf("unexpected non-paragraph body element: %+v", el)
		}
		paras = append(paras, *el.Paragraph)
	}
	return paras
}

// singleTable returns the sole table-typed body element of doc, failing the
// test if there isn't exactly one.
func singleTable(t *testing.T, doc *document.Document) document.Table {
	t.Helper()
	var tables []document.Table
	for _, el := range doc.Body {
		if el.Table != nil {
			tables = append(tables, *el.Table)
		}
	}
	if len(tables) != 1 {
		t.Fatalf("table count = %d, want 1", len(tables))
	}
	return tables[0]
}

func concatText(paras []document.Paragraph) string {
	var b strings.Builder
	for _, p := range paras {
		for _, r := range p.Runs {
			b.WriteString(r.Text)
		}
	}
	return b.String()
}
