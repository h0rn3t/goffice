package document

// paraBox is one cell paragraph's wrapped lines plus its alignment and
// indent (drawLine needs all three, but a cell's draw pass happens after its
// row's cells are measured, so they're carried from layout time to then).
type paraBox struct {
	lines   []line
	align   Alignment
	indent  Indent
	spacing Spacing
}

// cellContent is a cell's laid-out paragraphs and (if any) its nested table,
// plus the total content height they need (excluding cell padding).
type cellContent struct {
	paragraphs []paraBox
	nested     *tableLayout
	height     float64
}

// cellLayout pairs a cell with its laid-out content and its starting column
// index (needed to look up vertical-merge continuations in later rows).
type cellLayout struct {
	cell    Cell
	col     int
	content cellContent
}

// rowLayout is one table row: its resolved height and its cells' layouts.
type rowLayout struct {
	height float64
	cells  []cellLayout
}

// tableLayout is a fully measured table, ready to draw at any x/y origin:
// column offsets and row heights are relative, not absolute page coordinates.
type tableLayout struct {
	colWidths   []float64
	colOffsets  []float64 // colOffsets[i] = sum(colWidths[:i])
	rows        []rowLayout
	totalHeight float64
}

// layoutTable measures every row and cell of t, resolving column x-offsets
// from t.IndentPt and t.ColumnWidths (so every column - and therefore every
// cell drawn via x0 + colOffsets[i] - picks up the table's own indent,
// relative to whatever frame x0 itself is measured from: the page margin
// for a top-level table, a parent cell's content edge for a nested one) and
// each row's height from its tallest non-continuation cell (floored at
// minRowHeightPt).
//
// simplified: a vMerge-restart cell's own content can't force its spanned
// rows taller than their independently-computed heights - only the common
// case (merged cell content fits within the rows it spans) is handled.
func layoutTable(r renderer, t Table) tableLayout {
	offsets := make([]float64, len(t.ColumnWidths))
	acc := t.IndentPt
	for i, w := range t.ColumnWidths {
		offsets[i] = acc
		acc += w
	}
	tl := tableLayout{colWidths: t.ColumnWidths, colOffsets: offsets}

	for _, row := range t.Rows {
		col := 0
		rl := rowLayout{height: minRowHeightPt}
		for _, cell := range row.Cells {
			span := colSpan(cell)
			width := spanWidth(t.ColumnWidths, col, span)

			var content cellContent
			if cell.VMerge != VMergeContinue {
				content = layoutCellContent(r, cell, width-2*cellPaddingPt)
				if h := content.height + 2*cellPaddingPt; h > rl.height {
					rl.height = h
				}
			}
			rl.cells = append(rl.cells, cellLayout{cell: cell, col: col, content: content})
			col += span
		}
		tl.rows = append(tl.rows, rl)
		tl.totalHeight += rl.height
	}
	return tl
}

// layoutCellContent lays out cell's paragraphs (stacked) and, if present, its
// nested table, against the given inner width (already padding-adjusted).
func layoutCellContent(r renderer, cell Cell, width float64) cellContent {
	var cc cellContent
	for _, p := range cell.Paragraphs {
		innerWidth := width - p.Props.Indent.LeftPt - p.Props.Indent.RightPt
		lines := layoutParagraph(r, p, innerWidth)
		cc.paragraphs = append(cc.paragraphs, paraBox{lines: lines, align: p.Props.Alignment, indent: p.Props.Indent, spacing: p.Props.Spacing})
		if len(lines) == 0 {
			cc.height += lineHeightFor(defaultRenderSizePt, p.Props.Spacing)
			continue
		}
		for _, ln := range lines {
			cc.height += ln.height
		}
	}
	if cell.Nested != nil {
		nt := layoutTable(r, *cell.Nested)
		cc.nested = &nt
		cc.height += nt.totalHeight
	}
	return cc
}

// cellAt returns row's cell starting at column col, if any.
func (rl rowLayout) cellAt(col int) (cellLayout, bool) {
	for _, cl := range rl.cells {
		if cl.col == col {
			return cl, true
		}
	}
	return cellLayout{}, false
}

// mergedHeight returns the total height a vMerge-restart cell at row ri,
// column col draws across: its own row plus every immediately-following row
// that continues the merge at that column.
func (tl tableLayout) mergedHeight(ri, col int) float64 {
	h := tl.rows[ri].height
	for rj := ri + 1; rj < len(tl.rows); rj++ {
		cl, ok := tl.rows[rj].cellAt(col)
		if !ok || cl.cell.VMerge != VMergeContinue {
			break
		}
		h += tl.rows[rj].height
	}
	return h
}

func colSpan(cell Cell) int {
	if cell.ColSpan < 1 {
		return 1
	}
	return cell.ColSpan
}

func spanWidth(colWidths []float64, col, span int) float64 {
	var w float64
	for i := col; i < col+span && i < len(colWidths); i++ {
		w += colWidths[i]
	}
	return w
}

// renderTable lays out t and draws it starting at (pg.originX, cursorY),
// starting a new page before any row that would cross the bottom margin (a row
// is never split across pages). It returns the cursor position after the table.
func renderTable(r renderer, t Table, pg page, cursorY float64, atPageTop bool) (float64, bool) {
	tl := layoutTable(r, t)
	for ri := range tl.rows {
		rh := tl.rows[ri].height
		if cursorY+rh > pg.bottomLimit && !atPageTop {
			r.AddPage()
			cursorY = pg.originY
		}
		drawRow(r, tl, ri, pg.originX, cursorY)
		cursorY += rh
		atPageTop = false
	}
	return cursorY, atPageTop
}

// drawRow draws every non-continuation cell of row ri; vMerge-continue cells
// are skipped since their area was already drawn by the restart cell above them.
func drawRow(r renderer, tl tableLayout, ri int, x0, y float64) {
	row := tl.rows[ri]
	for _, cl := range row.cells {
		if cl.cell.VMerge == VMergeContinue {
			continue
		}
		width := spanWidth(tl.colWidths, cl.col, colSpan(cl.cell))
		height := tl.mergedHeight(ri, cl.col)
		x := x0 + tl.colOffsets[cl.col]
		drawCellBox(r, cl, x, y, width, height)
	}
}

// drawCellBox draws one cell's shading, borders, paragraph text, and nested
// table (if any) within the rectangle [x, x+width] x [y, y+height].
func drawCellBox(r renderer, cl cellLayout, x, y, width, height float64) {
	if cl.cell.Shading != "" {
		r.FillRect(x, y, width, height, cl.cell.Shading)
	}
	drawCellBorders(r, cl.cell.Borders, x, y, width, height)

	cy := y + cellPaddingPt
	for _, pb := range cl.content.paragraphs {
		if len(pb.lines) == 0 {
			cy += lineHeightFor(defaultRenderSizePt, pb.spacing)
			continue
		}
		innerX := x + cellPaddingPt + pb.indent.LeftPt
		innerWidth := width - 2*cellPaddingPt - pb.indent.LeftPt - pb.indent.RightPt
		for i, ln := range pb.lines {
			drawLine(r, ln, pb.align, innerX, innerWidth, cy, i == len(pb.lines)-1, pb.indent.FirstLineOffsetPt, i == 0)
			cy += ln.height
		}
	}
	if cl.content.nested != nil {
		drawTableRows(r, *cl.content.nested, x+cellPaddingPt, cy)
	}
}

// drawTableRows draws every row of tl starting at (x, y) with no pagination
// of its own; a nested table's total height is already folded into its parent
// cell's row height, so it fits unless taller than a full page (out of scope).
func drawTableRows(r renderer, tl tableLayout, x, y float64) {
	cy := y
	for ri := range tl.rows {
		drawRow(r, tl, ri, x, cy)
		cy += tl.rows[ri].height
	}
}

func drawCellBorders(r renderer, b CellBorders, x, y, w, h float64) {
	drawBorderSide(r, b.Top, x, y, x+w, y)
	drawBorderSide(r, b.Bottom, x, y+h, x+w, y+h)
	drawBorderSide(r, b.Left, x, y, x, y+h)
	drawBorderSide(r, b.Right, x+w, y, x+w, y+h)
}

func drawBorderSide(r renderer, side BorderSide, x1, y1, x2, y2 float64) {
	if side.Style == "" {
		return
	}
	r.StrokeLine(x1, y1, x2, y2, side.WidthPt, side.Color)
}
