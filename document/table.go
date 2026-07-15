package document

import "math"

// paraBox is one cell paragraph's wrapped lines plus its alignment, indent,
// spacing and shading (drawLine needs them all, but a cell's draw pass happens
// after its row's cells are measured, so they're carried from layout time to
// then).
type paraBox struct {
	lines   []line
	align   Alignment
	indent  Indent
	spacing Spacing
	shading string
}

// cellContent is a cell's laid-out paragraphs and (if any) its nested table,
// plus the space they need: height along the cell's stacking direction, and
// length (the widest unwrapped line) - which is what a vertical cell needs, its
// text running along the row's height.
type cellContent struct {
	paragraphs []paraBox
	nested     *tableLayout
	height     float64
	length     float64
}

// cellLayout pairs a cell with its laid-out content, its starting column index
// (needed to look up vertical-merge continuations in later rows) and its margins
// resolved to points (a percentage margin needs the table's width, which is only
// known once the columns are).
type cellLayout struct {
	cell    Cell
	col     int
	margins CellMargins
	content cellContent
}

// rowLayout is one table row: its resolved height, whether that height is fixed
// (w:hRule="exact", so the row may neither grow nor let its content spill out)
// and its cells' layouts.
type rowLayout struct {
	height float64
	fixed  bool
	cells  []cellLayout
}

// tableLayout is a fully measured table, ready to draw at any x/y origin:
// column offsets and row heights are relative, not absolute page coordinates.
type tableLayout struct {
	colWidths   []float64
	colOffsets  []float64 // colOffsets[i] = sum(colWidths[:i])
	rows        []rowLayout
	headerRows  int
	totalHeight float64
}

// width is how wide the table draws from its frame's origin: its own indent plus
// every column.
func (tl tableLayout) width() float64 {
	var w float64
	if len(tl.colOffsets) > 0 {
		w = tl.colOffsets[0] // the table's indent
	}
	for _, c := range tl.colWidths {
		w += c
	}
	return w
}

// layoutTable measures every row and cell of t against the width available to
// it (the page column for a top-level table, the parent cell's content box for a
// nested one). Column widths that the document leaves open are resolved from the
// content first (Word's auto-fit); column x-offsets then follow from t.IndentPt,
// so every cell - drawn via x0 + colOffsets[i] - picks up the table's own
// indent, relative to whatever frame x0 is measured from. Each row's height
// comes from its tallest non-continuation cell, floored at minRowHeightPt, and a
// vertically merged cell then stretches the rows it spans if it still needs more
// room.
func layoutTable(r renderer, t Table, availWidth, availHeight float64) tableLayout {
	widths := resolveTableWidths(r, t, availWidth)
	offsets := make([]float64, len(widths))
	acc := t.IndentPt
	for i, w := range widths {
		offsets[i] = acc
		acc += w
	}
	tl := tableLayout{colWidths: widths, colOffsets: offsets, headerRows: t.HeaderRows}
	// A percentage cell margin is a percentage of the table's width, which is
	// only settled now that the columns are.
	tableWidth := acc - t.IndentPt

	for _, row := range t.Rows {
		col := 0
		rl := rowLayout{height: minRowHeightPt, fixed: row.HeightRule == RowHeightExact}
		// Vertical text runs along the row's height, so that is what it wraps at:
		// a declared height when the row has one, else whatever room the frame
		// still offers.
		wrapLen := availHeight
		if row.HeightRule != RowHeightAuto && row.HeightPt > 0 {
			wrapLen = row.HeightPt
		}
		for _, cell := range row.Cells {
			span := colSpan(cell)
			width := spanWidth(widths, col, span)
			margins := cell.Margins.resolve(tableWidth)

			var content cellContent
			if cell.VMerge != VMergeContinue {
				content = layoutCellContent(r, cell, margins, width, wrapLen, availHeight)
				// A restart cell's content spans every row it merges, so its height is
				// applied by growMergedRows across the whole span. Piling it onto the
				// starting row instead would leave the first row tall and the merged
				// rows below it short (uneven).
				if cell.VMerge != VMergeRestart {
					if h := cellHeightNeeded(cell, margins, content); h > rl.height {
						rl.height = h
					}
				}
			}
			rl.cells = append(rl.cells, cellLayout{cell: cell, col: col, margins: margins, content: content})
			col += span
		}
		switch row.HeightRule {
		case RowHeightExact: // fixed, even when the content needs more
			rl.height = row.HeightPt
		case RowHeightAtLeast:
			rl.height = math.Max(rl.height, row.HeightPt)
		}
		tl.rows = append(tl.rows, rl)
	}
	growMergedRows(&tl)
	return tl
}

// growMergedRows lets a vertically merged cell whose content is taller than the
// rows it spans stretch them: the shortfall is shared among the rows that may
// grow (an exact-height row may not), in proportion to their heights. It also
// (re)computes the table's total height.
func growMergedRows(tl *tableLayout) {
	for ri := range tl.rows {
		for _, cl := range tl.rows[ri].cells {
			if cl.cell.VMerge != VMergeRestart {
				continue
			}
			last := ri
			for rj := ri + 1; rj < len(tl.rows); rj++ {
				c, ok := tl.rows[rj].cellAt(cl.col)
				if !ok || c.cell.VMerge != VMergeContinue {
					break
				}
				last = rj
			}
			var have, flexible float64
			var grow []int
			for rj := ri; rj <= last; rj++ {
				have += tl.rows[rj].height
				if !tl.rows[rj].fixed {
					grow = append(grow, rj)
					flexible += tl.rows[rj].height
				}
			}
			deficit := cellHeightNeeded(cl.cell, cl.margins, cl.content) - have
			if deficit <= 0 || len(grow) == 0 {
				continue
			}
			for _, rj := range grow {
				if flexible <= 0 { // nothing to be proportional to: share it evenly
					tl.rows[rj].height += deficit / float64(len(grow))
					continue
				}
				tl.rows[rj].height += deficit * tl.rows[rj].height / flexible
			}
		}
	}
	tl.totalHeight = 0
	for _, rl := range tl.rows {
		tl.totalHeight += rl.height
	}
}

// cellHeightNeeded is how tall a row must be to hold the cell. A vertical cell's
// text runs along the row's height, so what it needs is the length of its
// longest line, padded by the margins at the ends of that text.
func cellHeightNeeded(cell Cell, m CellMargins, content cellContent) float64 {
	if cell.TextDirection != TextDirectionHorizontal {
		return content.length + m.LeftPt + m.RightPt
	}
	return content.height + m.TopPt + m.BottomPt
}

// layoutCellContent lays out cell's paragraphs (stacked) and, if present, its
// nested table, inside the cell's width minus its margins. A vertical cell's
// lines run along the row's height instead, so they are wrapped at wrapLen (the
// row's declared height, or the room the frame has left) rather than at the
// cell's width.
func layoutCellContent(r renderer, cell Cell, m CellMargins, width, wrapLen, availHeight float64) cellContent {
	inner := width - m.LeftPt - m.RightPt
	if cell.TextDirection != TextDirectionHorizontal {
		inner = wrapLen - m.LeftPt - m.RightPt
	}

	var cc cellContent
	for _, p := range cell.Paragraphs {
		innerWidth := inner - p.Props.Indent.LeftPt - p.Props.Indent.RightPt
		lines := layoutParagraph(r, p, innerWidth)
		cc.paragraphs = append(cc.paragraphs, paraBox{
			lines:   lines,
			align:   p.Props.Alignment,
			indent:  p.Props.Indent,
			spacing: p.Props.Spacing,
			shading: p.Props.Shading,
		})
		if len(lines) == 0 {
			cc.height += lineHeightFor(defaultRenderSizePt, 0, p.Props.Spacing)
			continue
		}
		for _, ln := range lines {
			cc.height += ln.height
			cc.length = math.Max(cc.length, ln.natural+p.Props.Indent.LeftPt+p.Props.Indent.RightPt)
		}
	}
	if cell.Nested != nil {
		nt := layoutTable(r, *cell.Nested, inner, availHeight)
		cc.nested = &nt
		cc.height += nt.totalHeight
	}
	return cc
}

// --- Column widths ---

// resolveTableWidths turns the table's declared column widths into final ones:
// a declared width stands, a percentage width is taken of the table's width, and
// a column with neither is sized from the content of the cells in it (Word's
// auto-fit). Auto columns share whatever the declared ones leave: each is
// guaranteed at least its longest unbreakable word, and the rest of the room is
// handed out in proportion to how much wider each still wants to be. A fixed
// layout (w:tblLayout) never measures content at all - undeclared columns simply
// split what is left evenly. When the table declares a width of its own
// (w:tblW), the columns end up scaled to exactly that; otherwise the table is as
// wide as its columns turn out to be, up to the room it has.
func resolveTableWidths(r renderer, t Table, availWidth float64) []float64 {
	widths := make([]float64, len(t.ColumnWidths))
	copy(widths, t.ColumnWidths)

	// room is what the columns have to share: the table's own declared width when
	// it has one, else whatever is left of the frame beside its indent.
	room := math.Max(availWidth-t.IndentPt, 0)
	target := tableTargetWidth(t, availWidth)
	if target > 0 {
		room = target
	}

	var auto []int
	var declared float64
	for i := range widths {
		if widths[i] <= 0 && i < len(t.ColumnPercents) && t.ColumnPercents[i] > 0 {
			widths[i] = room * t.ColumnPercents[i] / 100
		}
		if widths[i] > 0 {
			declared += widths[i]
		} else {
			auto = append(auto, i)
		}
	}
	remaining := math.Max(room-declared, 0)
	shareEvenly := func() {
		for _, i := range auto {
			widths[i] = remaining / float64(len(auto))
		}
	}

	switch {
	case len(auto) == 0: // nothing to size
	case t.FixedLayout:
		shareEvenly()
	default:
		natural, minimal := columnDemands(r, t, len(widths))
		var want, need float64
		for _, i := range auto {
			want += natural[i]
			need += minimal[i]
		}
		switch {
		case want <= 0: // nothing to measure (empty cells)
			shareEvenly()
		case want <= remaining: // everything fits at its natural width
			for _, i := range auto {
				widths[i] = natural[i]
			}
		case need >= remaining: // not even the longest words fit: scale them together
			for _, i := range auto {
				widths[i] = minimal[i] * remaining / need
			}
		default: // every column gets its longest word, then shares what is left over
			extra, slack := remaining-need, want-need
			for _, i := range auto {
				widths[i] = minimal[i] + (natural[i]-minimal[i])*extra/slack
			}
		}
	}
	if target > 0 {
		scaleToWidth(widths, target)
	}
	return widths
}

// tableTargetWidth is the width the table declares for itself (w:tblW): a
// percentage of the room it has, or a fixed size. Zero when it declares none
// ("auto"), in which case the columns decide how wide it is.
func tableTargetWidth(t Table, availWidth float64) float64 {
	switch {
	case t.WidthPct > 0:
		return math.Max(availWidth-t.IndentPt, 0) * t.WidthPct / 100
	case t.WidthPt > 0:
		return t.WidthPt
	default:
		return 0
	}
}

// scaleToWidth stretches or shrinks the columns so that together they are
// exactly target wide.
func scaleToWidth(widths []float64, target float64) {
	var sum float64
	for _, w := range widths {
		sum += w
	}
	if sum <= 0 {
		return
	}
	for i := range widths {
		widths[i] *= target / sum
	}
}

// columnDemands is what each column's content asks for: naturally (its widest
// unwrapped line) and at minimum (its longest unbreakable word). A cell spanning
// several columns can't be charged to one of them, so it only widens the columns
// it covers when they are together narrower than it needs - and then in
// proportion to what they already ask for.
func columnDemands(r renderer, t Table, n int) (natural, minimal []float64) {
	natural, minimal = make([]float64, n), make([]float64, n)
	type spanning struct {
		col, span        int
		natural, minimal float64
	}
	var spans []spanning

	for _, row := range t.Rows {
		col := 0
		for _, cell := range row.Cells {
			span := colSpan(cell)
			nat, min := cellDemands(r, cell)
			switch {
			case span == 1 && col < n:
				natural[col] = math.Max(natural[col], nat)
				minimal[col] = math.Max(minimal[col], min)
			case span > 1 && col+span <= n:
				spans = append(spans, spanning{col: col, span: span, natural: nat, minimal: min})
			}
			col += span
		}
	}
	for _, s := range spans {
		spreadDemand(natural, s.col, s.span, s.natural)
		spreadDemand(minimal, s.col, s.span, s.minimal)
	}
	return natural, minimal
}

// spreadDemand widens the columns [col, col+span) until they together hold need.
func spreadDemand(demand []float64, col, span int, need float64) {
	var have float64
	for i := col; i < col+span; i++ {
		have += demand[i]
	}
	if have >= need {
		return
	}
	deficit := need - have
	for i := col; i < col+span; i++ {
		if have <= 0 { // nothing to be proportional to: split evenly
			demand[i] += deficit / float64(span)
			continue
		}
		demand[i] += deficit * demand[i] / have
	}
}

// cellDemands is how wide a cell wants to be, naturally and at minimum. A
// vertical cell's text runs down the row rather than across it, so what it asks
// of its column is the thickness of its stacked lines, which cannot be reduced.
func cellDemands(r renderer, cell Cell) (natural, minimal float64) {
	if cell.TextDirection != TextDirectionHorizontal {
		var thickness float64
		for _, p := range cell.Paragraphs {
			for _, ln := range layoutParagraph(r, p, math.Inf(1)) {
				thickness += ln.height
			}
		}
		w := thickness + cell.Margins.TopPt + cell.Margins.BottomPt
		return w, w
	}
	for _, p := range cell.Paragraphs {
		ind := p.Props.Indent.LeftPt + p.Props.Indent.RightPt
		for _, ln := range layoutParagraph(r, p, math.Inf(1)) {
			natural = math.Max(natural, ln.natural+ind)
		}
		// Laying the paragraph out at zero width puts every word on a line of its
		// own, so the widest of those lines is its longest unbreakable word.
		for _, ln := range layoutParagraph(r, p, 0) {
			minimal = math.Max(minimal, ln.natural+ind)
		}
	}
	m := cell.Margins.LeftPt + cell.Margins.RightPt
	return natural + m, minimal + m
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

// renderTable lays out t and draws it down the flow's current column, moving to
// the next column or page before any row that would cross the bottom margin (a
// row is never split). The table's leading header rows (w:tblHeader) are redrawn
// at the top of every frame the table continues into. A floating table
// (w:tblpPr) is drawn at its own position instead and leaves the cursor where it
// was, since it is not part of the text flow.
func renderTable(f *flow, t Table) {
	fr := f.frame()
	tl := layoutTable(f.r, t, fr.contentWidth, fr.bottomLimit-fr.originY)
	if t.Float != nil {
		x, y := floatOrigin(f, *t.Float, tl)
		drawTableRows(f.r, tl, x, y)
		return
	}
	for ri := range tl.rows {
		rh := tl.rows[ri].height
		if f.y+rh > f.frame().bottomLimit && !f.atTop {
			f.breakFrame()
			if ri >= tl.headerRows { // the header rows themselves are not repeated above themselves
				drawHeaderRows(f, tl)
			}
		}
		drawRow(f.r, tl, ri, f.frame().originX, f.y)
		f.y += rh
		f.atTop = false
	}
}

// floatOrigin places a floating table: each axis is measured from its anchor -
// the text column, the page's margin, or the page's edge - either by the offset
// the table declares (w:tblpX/w:tblpY) or, when it names an alignment instead
// (w:tblpXSpec/w:tblpYSpec), by aligning it against that anchor.
//
// ponytail: the surrounding text does not wrap around a floating table, so body
// text can run underneath it; wrapping needs the layout to know about exclusion
// zones.
func floatOrigin(f *flow, fl TableFloat, tl tableLayout) (float64, float64) {
	g := f.sec.sec.Geometry
	fr := f.frame()

	x, availW := fr.originX, fr.contentWidth
	switch fl.HorzAnchor {
	case AnchorPage:
		x, availW = 0, g.WidthPt
	case AnchorMargin:
		x, availW = g.MarginLeftPt, g.WidthPt-g.MarginLeftPt-g.MarginRightPt
	}
	switch fl.XSpec {
	case "center":
		x += (availW - tl.width()) / 2
	case "right", "outside":
		x += availW - tl.width()
	case "left", "inside":
	default:
		x += fl.XPt
	}

	y, availH := f.y, fr.bottomLimit-f.y
	switch fl.VertAnchor {
	case AnchorPage:
		y, availH = 0, g.HeightPt
	case AnchorMargin:
		y, availH = g.MarginTopPt, g.HeightPt-g.MarginTopPt-g.MarginBottomPt
	}
	switch fl.YSpec {
	case "center":
		y += (availH - tl.totalHeight) / 2
	case "bottom", "outside":
		y += availH - tl.totalHeight
	case "top", "inside":
	default:
		y += fl.YPt
	}
	return x, y
}

// drawHeaderRows repeats the table's header rows at the cursor, advancing it
// past them.
func drawHeaderRows(f *flow, tl tableLayout) {
	for ri := 0; ri < tl.headerRows && ri < len(tl.rows); ri++ {
		drawRow(f.r, tl, ri, f.frame().originX, f.y)
		f.y += tl.rows[ri].height
		f.atTop = false
	}
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
		drawCellBox(r, cl, x, y, width, height, row.fixed)
	}
}

// drawCellBox draws one cell's shading, borders and content within the rectangle
// [x, x+width] x [y, y+height]. A cell with vertical text (w:textDirection) has
// its content drawn into that rectangle turned 90°: the box is rotated about one
// of its corners, so the same horizontal layout produces text reading bottom-to-
// top (btLr) or top-to-bottom (tbRl). In a row of exact height (w:hRule="exact")
// the content is clipped to the cell, as Word clips it, instead of spilling over
// the row below.
func drawCellBox(r renderer, cl cellLayout, x, y, width, height float64, clip bool) {
	if cl.cell.Shading != "" {
		r.FillRect(x, y, width, height, cl.cell.Shading)
	}
	drawCellBorders(r, cl.cell.Borders, x, y, width, height)

	if clip {
		r.Clip(x, y, width, height)
		defer r.ClipEnd()
	}
	switch cl.cell.TextDirection {
	case TextDirectionBTLR:
		// Rotating counter-clockwise about the bottom-left corner turns a box that
		// runs right-and-down from there into the cell's own rectangle.
		r.Rotate(90, x, y+height)
		drawCellContent(r, cl, x, y+height, height, width)
		r.RotateEnd()
	case TextDirectionTBRL:
		r.Rotate(-90, x+width, y)
		drawCellContent(r, cl, x+width, y, height, width)
		r.RotateEnd()
	default:
		drawCellContent(r, cl, x, y, width, height)
	}
}

// drawCellContent draws a cell's paragraphs and nested table inside the box at
// (x, y) of the given size, inset by the cell's margins and pushed down by the
// cell's vertical alignment (w:vAlign) when the content is shorter than the box.
func drawCellContent(r renderer, cl cellLayout, x, y, width, height float64) {
	m := cl.margins
	cy := y + m.TopPt
	if free := height - m.TopPt - m.BottomPt - cl.content.height; free > 0 {
		switch cl.cell.VAlign {
		case VAlignCenter:
			cy += free / 2
		case VAlignBottom:
			cy += free
		}
	}
	for _, pb := range cl.content.paragraphs {
		if len(pb.lines) == 0 {
			cy += lineHeightFor(defaultRenderSizePt, 0, pb.spacing)
			continue
		}
		innerX := x + m.LeftPt + pb.indent.LeftPt
		innerWidth := width - m.LeftPt - m.RightPt - pb.indent.LeftPt - pb.indent.RightPt
		for i, ln := range pb.lines {
			if pb.shading != "" { // paragraph shading sits over the cell's, under the text
				r.FillRect(innerX, cy, innerWidth, ln.height, pb.shading)
			}
			drawLine(r, ln, pb.align, innerX, innerWidth, cy, i == len(pb.lines)-1, pb.indent.FirstLineOffsetPt, i == 0)
			cy += ln.height
		}
	}
	if cl.content.nested != nil {
		drawTableRows(r, *cl.content.nested, x+m.LeftPt, cy)
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
	drawBorderSide(r, b.DiagDown, x, y, x+w, y+h)
	drawBorderSide(r, b.DiagUp, x, y+h, x+w, y)
}

func drawBorderSide(r renderer, side BorderSide, x1, y1, x2, y2 float64) {
	if side.Style == "" {
		return
	}
	r.StrokeLine(x1, y1, x2, y2, side.WidthPt, side.Color)
}
