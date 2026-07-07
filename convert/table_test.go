package convert

import (
	"testing"

	"github.com/h0rn3t/goffice/document"
)

func mkCell(colSpan int, vmerge document.VMergeState, paras ...document.Paragraph) document.Cell {
	return document.Cell{Paragraphs: paras, ColSpan: colSpan, VMerge: vmerge}
}

func mkRow(cells ...document.Cell) document.Row {
	return document.Row{Cells: cells}
}

func TestLayoutTable_ColumnOffsets(t *testing.T) {
	tbl := document.Table{ColumnWidths: []float64{50, 30, 20}}
	tl := layoutTable(&fakeRenderer{}, tbl)

	want := []float64{0, 50, 80}
	for i, w := range want {
		if tl.colOffsets[i] != w {
			t.Errorf("colOffsets[%d] = %.2f, want %.2f", i, tl.colOffsets[i], w)
		}
	}
}

func TestLayoutTable_ColumnOffsetsIncludeTableIndent(t *testing.T) {
	tbl := document.Table{ColumnWidths: []float64{50, 30, 20}, IndentPt: 100}
	tl := layoutTable(&fakeRenderer{}, tbl)

	want := []float64{100, 150, 180}
	for i, w := range want {
		if tl.colOffsets[i] != w {
			t.Errorf("colOffsets[%d] = %.2f, want %.2f", i, tl.colOffsets[i], w)
		}
	}
}

func TestRenderTable_CellsAtColumnPositions(t *testing.T) {
	f := &fakeRenderer{}
	tbl := document.Table{
		ColumnWidths: []float64{100, 200},
		Rows: []document.Row{
			mkRow(
				mkCell(1, document.VMergeNone, para(document.AlignLeft, false, run("A1", 12))),
				mkCell(1, document.VMergeNone, para(document.AlignLeft, false, run("B1", 12))),
			),
		},
	}
	renderTable(f, tbl, testPage, marginPt, true)

	if len(f.draws) != 2 {
		t.Fatalf("expected 2 draws, got %d", len(f.draws))
	}
	if want := marginPt + cellPaddingPt; f.draws[0].x != want {
		t.Errorf("cell A1 x = %.2f, want %.2f", f.draws[0].x, want)
	}
	if want := marginPt + 100 + cellPaddingPt; f.draws[1].x != want {
		t.Errorf("cell B1 x = %.2f, want %.2f", f.draws[1].x, want)
	}
}

func TestLayoutTable_RowHeightFitsTallestCell(t *testing.T) {
	// Narrow column forces this cell to wrap into several lines; the other
	// cell is a single short word.
	tall := para(document.AlignLeft, false, run("aa bb cc dd ee", 12))
	short := para(document.AlignLeft, false, run("x", 12))
	tbl := document.Table{
		ColumnWidths: []float64{20, 100},
		Rows: []document.Row{
			mkRow(mkCell(1, document.VMergeNone, tall), mkCell(1, document.VMergeNone, short)),
		},
	}
	tl := layoutTable(&fakeRenderer{}, tbl)

	if got := len(tl.rows[0].cells[0].content.paragraphs[0].lines); got < 2 {
		t.Fatalf("expected the narrow cell to wrap into multiple lines, got %d", got)
	}
	shortBoxHeight := tl.rows[0].cells[1].content.height + 2*cellPaddingPt
	if tl.rows[0].height <= shortBoxHeight {
		t.Fatalf("row height %.2f should exceed the short cell's own box height %.2f", tl.rows[0].height, shortBoxHeight)
	}
}

func TestRenderTable_HorizontalMergeSpansCombinedWidthNoInteriorBorder(t *testing.T) {
	f := &fakeRenderer{}
	borders := document.CellBorders{Top: document.BorderSide{Style: "single", WidthPt: 1, Color: "#000000"}}
	tbl := document.Table{
		ColumnWidths: []float64{50, 50},
		Rows: []document.Row{
			mkRow(document.Cell{
				ColSpan:    2,
				Borders:    borders,
				Paragraphs: []document.Paragraph{para(document.AlignLeft, false, run("wide", 12))},
			}),
		},
	}
	renderTable(f, tbl, testPage, marginPt, true)

	if len(f.strokes) != 1 {
		t.Fatalf("expected exactly one border stroke (no interior border at the merge boundary), got %d", len(f.strokes))
	}
	if want := marginPt + 100; f.strokes[0].x2 != want {
		t.Fatalf("merged cell right edge = %.2f, want %.2f (spanning both columns)", f.strokes[0].x2, want)
	}
}

func TestRenderTable_VerticalMergeSpansCombinedHeightAndSkipsContinueDraw(t *testing.T) {
	restart := document.Cell{VMerge: document.VMergeRestart, ColSpan: 1,
		Paragraphs: []document.Paragraph{para(document.AlignLeft, false, run("top", 12))}}
	continued := document.Cell{VMerge: document.VMergeContinue, ColSpan: 1}
	tbl := document.Table{
		ColumnWidths: []float64{100},
		Rows:         []document.Row{mkRow(restart), mkRow(continued)},
	}

	f := &fakeRenderer{}
	renderTable(f, tbl, testPage, marginPt, true)
	if len(f.draws) != 1 {
		t.Fatalf("expected exactly one draw (the vMerge-continue cell must not redraw), got %d", len(f.draws))
	}

	tl := layoutTable(&fakeRenderer{}, tbl)
	got := tl.mergedHeight(0, 0)
	want := tl.rows[0].height + tl.rows[1].height
	if got != want {
		t.Fatalf("merged height = %.2f, want %.2f (sum of both rows)", got, want)
	}
}

func TestRenderTable_CellShadingFills(t *testing.T) {
	f := &fakeRenderer{}
	tbl := document.Table{
		ColumnWidths: []float64{100},
		Rows:         []document.Row{mkRow(document.Cell{ColSpan: 1, Shading: "#D9D9D9"})},
	}
	renderTable(f, tbl, testPage, marginPt, true)

	if len(f.fills) != 1 {
		t.Fatalf("expected one fill call, got %d", len(f.fills))
	}
	if f.fills[0].color != "#D9D9D9" {
		t.Fatalf("fill color = %q, want %q", f.fills[0].color, "#D9D9D9")
	}
	if f.fills[0].w != 100 {
		t.Fatalf("fill width = %.2f, want 100", f.fills[0].w)
	}
}

func TestRenderTable_BordersPerSide(t *testing.T) {
	f := &fakeRenderer{}
	borders := document.CellBorders{
		Top:    document.BorderSide{Style: "single", WidthPt: 1, Color: "#111111"},
		Bottom: document.BorderSide{Style: "double", WidthPt: 2, Color: "#222222"},
	}
	tbl := document.Table{
		ColumnWidths: []float64{100},
		Rows:         []document.Row{mkRow(document.Cell{ColSpan: 1, Borders: borders})},
	}
	renderTable(f, tbl, testPage, marginPt, true)

	if len(f.strokes) != 2 {
		t.Fatalf("expected 2 border strokes (top+bottom only; left/right undeclared), got %d", len(f.strokes))
	}
	if f.strokes[0].widthPt != 1 || f.strokes[0].color != "#111111" {
		t.Errorf("top border = %+v, want {width:1 color:#111111}", f.strokes[0])
	}
	if f.strokes[1].widthPt != 2 || f.strokes[1].color != "#222222" {
		t.Errorf("bottom border = %+v, want {width:2 color:#222222}", f.strokes[1])
	}
}

func TestRenderTable_PaginatesWithoutSplittingRows(t *testing.T) {
	f := &fakeRenderer{}
	var rows []document.Row
	for range 200 {
		rows = append(rows, mkRow(document.Cell{
			ColSpan:    1,
			Paragraphs: []document.Paragraph{para(document.AlignLeft, false, run("row", 12))},
		}))
	}
	tbl := document.Table{ColumnWidths: []float64{100}, Rows: rows}
	renderTable(f, tbl, testPage, marginPt, true)

	if f.page < 2 {
		t.Fatalf("expected the table to paginate across multiple pages, got %d", f.page)
	}
	if len(f.draws) != 200 {
		t.Fatalf("expected 200 draws (one per row, none split), got %d", len(f.draws))
	}
}

func TestRenderTable_CellParagraphIndentShiftsText(t *testing.T) {
	f := &fakeRenderer{}
	indented := document.Cell{
		ColSpan:    1,
		Paragraphs: []document.Paragraph{paraWithIndent(document.AlignLeft, document.Indent{LeftPt: 20}, run("hi", 12))},
	}
	tbl := document.Table{
		ColumnWidths: []float64{100},
		Rows:         []document.Row{mkRow(indented)},
	}
	renderTable(f, tbl, testPage, marginPt, true)

	if len(f.draws) != 1 {
		t.Fatalf("expected 1 draw, got %d", len(f.draws))
	}
	if want := marginPt + cellPaddingPt + 20.0; f.draws[0].x != want {
		t.Fatalf("indented cell text x = %.2f, want %.2f", f.draws[0].x, want)
	}
}

func TestRenderTable_NestedTableIndentIsIndependentOfParent(t *testing.T) {
	f := &fakeRenderer{}
	inner := document.Table{
		IndentPt:     20,
		ColumnWidths: []float64{50},
		Rows: []document.Row{mkRow(document.Cell{
			ColSpan:    1,
			Paragraphs: []document.Paragraph{para(document.AlignLeft, false, run("inner", 12))},
		})},
	}
	outer := document.Table{
		IndentPt:     40,
		ColumnWidths: []float64{100},
		Rows: []document.Row{mkRow(document.Cell{
			ColSpan:    1,
			Paragraphs: []document.Paragraph{para(document.AlignLeft, false, run("outer", 12))},
			Nested:     &inner,
		})},
	}
	renderTable(f, outer, testPage, marginPt, true)

	if len(f.draws) != 2 {
		t.Fatalf("expected 2 draws (outer cell + nested table), got %d", len(f.draws))
	}
	wantOuterX := marginPt + 40 + cellPaddingPt
	if f.draws[0].x != wantOuterX {
		t.Fatalf("outer cell text x = %.2f, want %.2f (shifted by the outer table's own indent)", f.draws[0].x, wantOuterX)
	}
	wantInnerX := f.draws[0].x + cellPaddingPt + 20
	if f.draws[1].x != wantInnerX {
		t.Fatalf("nested cell text x = %.2f, want %.2f (own indent, additional to and independent of the parent's)", f.draws[1].x, wantInnerX)
	}
}

func TestRenderTable_NestedTableRendersWithinParentCell(t *testing.T) {
	f := &fakeRenderer{}
	inner := document.Table{
		ColumnWidths: []float64{50},
		Rows: []document.Row{mkRow(document.Cell{
			ColSpan:    1,
			Paragraphs: []document.Paragraph{para(document.AlignLeft, false, run("inner", 12))},
		})},
	}
	outer := document.Table{
		ColumnWidths: []float64{100},
		Rows: []document.Row{mkRow(document.Cell{
			ColSpan:    1,
			Paragraphs: []document.Paragraph{para(document.AlignLeft, false, run("outer", 12))},
			Nested:     &inner,
		})},
	}
	renderTable(f, outer, testPage, marginPt, true)

	if len(f.draws) != 2 {
		t.Fatalf("expected 2 draws (outer cell + nested table), got %d", len(f.draws))
	}
	if f.draws[0].text != "outer" || f.draws[1].text != "inner" {
		t.Fatalf("draw text = %q, %q, want \"outer\", \"inner\"", f.draws[0].text, f.draws[1].text)
	}
	// The nested table sits inside the outer cell's padded content area, and
	// its own cell adds one more padding level, so its text lands one
	// cellPaddingPt to the right of the outer cell's own text.
	if want := f.draws[0].x + cellPaddingPt; f.draws[1].x != want {
		t.Fatalf("nested cell x = %.2f, want %.2f (outer text x + one cell padding)", f.draws[1].x, want)
	}
	if f.draws[1].y <= f.draws[0].y {
		t.Fatalf("expected the nested table drawn below the outer cell's own text (y=%.2f)", f.draws[0].y)
	}
}
