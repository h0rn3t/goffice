package document

import (
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

// wordNS is the WordprocessingML namespace URI. Element struct tags match on
// this URI plus local name, so any prefix bound to it in the source decodes
// correctly and elements in other namespaces are ignored by encoding/xml —
// which is exactly the "skip unsupported content" behaviour we want.
const wordNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

type xmlDocument struct {
	Body xmlBody `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main body"`
}

// xmlBody captures its p/tbl children in document order via a custom
// UnmarshalXML (encoding/xml has no ordered-union tag); other body children
// (e.g. sectPr) are skipped without error.
type xmlBody struct {
	Children []xmlBodyChild
}

// xmlBodyChild is one body-level child: exactly one of Paragraph or Table is
// non-nil.
type xmlBodyChild struct {
	Paragraph *xmlParagraph
	Table     *xmlTable
}

func (b *xmlBody) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue // EndElement of <w:body> itself, or CharData/Comment
		}
		if start.Name.Space != wordNS {
			if err := d.Skip(); err != nil {
				return err
			}
			continue
		}
		switch start.Name.Local {
		case "p":
			var p xmlParagraph
			if err := d.DecodeElement(&p, &start); err != nil {
				return err
			}
			b.Children = append(b.Children, xmlBodyChild{Paragraph: &p})
		case "tbl":
			var t xmlTable
			if err := d.DecodeElement(&t, &start); err != nil {
				return err
			}
			b.Children = append(b.Children, xmlBodyChild{Table: &t})
		default:
			if err := d.Skip(); err != nil {
				return err
			}
		}
	}
}

type xmlParagraph struct {
	Props *xmlPPr  `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pPr"`
	Runs  []xmlRun `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main r"`
}

type xmlPPr struct {
	Jc              *xmlVal   `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main jc"`
	PageBreakBefore *xmlOnOff `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pageBreakBefore"`
}

type xmlRun struct {
	Props  *xmlRPr   `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main rPr"`
	Texts  []xmlText `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main t"`
	Breaks []xmlBr   `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main br"`
}

type xmlRPr struct {
	Bold      *xmlOnOff  `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main b"`
	Italic    *xmlOnOff  `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main i"`
	Underline *xmlVal    `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main u"`
	Fonts     *xmlRFonts `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main rFonts"`
	Size      *xmlVal    `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main sz"`
}

type xmlRFonts struct {
	ASCII string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main ascii,attr"`
	HAnsi string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main hAnsi,attr"`
}

type xmlText struct {
	Value string `xml:",chardata"`
}

type xmlBr struct {
	Type string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main type,attr"`
}

// xmlVal holds a w:val attribute (jc, sz, u, …).
type xmlVal struct {
	Val string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main val,attr"`
}

// xmlOnOff is a WordprocessingML toggle property (w:b, w:i, w:pageBreakBefore).
// Presence means "on" unless w:val explicitly negates it.
type xmlOnOff struct {
	Val string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main val,attr"`
}

func (o *xmlOnOff) on() bool {
	if o == nil {
		return false
	}
	switch strings.ToLower(o.Val) {
	case "false", "0", "off":
		return false
	}
	return true
}

// --- Tables (w:tbl) ---

type xmlTable struct {
	Grid  *xmlTblGrid `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tblGrid"`
	Props *xmlTblPr   `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tblPr"`
	Rows  []xmlRow    `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tr"`
}

type xmlTblPr struct {
	StyleID *xmlVal     `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tblStyle"`
	Borders *xmlBorders `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tblBorders"`
}

type xmlTblGrid struct {
	Cols []xmlGridCol `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main gridCol"`
}

type xmlGridCol struct {
	W string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main w,attr"`
}

type xmlRow struct {
	Cells []xmlCell `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tc"`
}

type xmlCell struct {
	Props        *xmlTcPr       `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tcPr"`
	Paragraphs   []xmlParagraph `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main p"`
	NestedTables []xmlTable     `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tbl"`
}

type xmlTcPr struct {
	GridSpan *xmlVal     `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main gridSpan"`
	VMerge   *xmlVMerge  `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main vMerge"`
	Borders  *xmlBorders `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tcBorders"`
	Shd      *xmlShd     `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main shd"`
	TcW      *xmlTcW     `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tcW"`
}

// xmlVMerge is <w:vMerge/> (continue - w:val absent or "continue") or
// <w:vMerge w:val="restart"/>.
type xmlVMerge struct {
	Val string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main val,attr"`
}

type xmlTcW struct {
	W    string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main w,attr"`
	Type string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main type,attr"`
}

type xmlShd struct {
	Fill string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main fill,attr"`
}

// xmlBorders is shared by w:tblBorders and w:tcBorders (top/bottom/left/right
// sides only; diagonal and inside-grid-line borders are out of scope).
type xmlBorders struct {
	Top    *xmlBorder `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main top"`
	Bottom *xmlBorder `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main bottom"`
	Left   *xmlBorder `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main left"`
	Right  *xmlBorder `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main right"`
}

type xmlBorder struct {
	Val   string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main val,attr"`
	Sz    string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main sz,attr"`
	Color string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main color,attr"`
}

// --- Table styles (word/styles.xml) ---
//
// Only what a table style contributes to border/shading resolution is
// modeled: its own (non-conditional) w:tblPr/w:tblBorders and w:tcPr/w:shd,
// plus its w:basedOn parent. Region-specific w:tblStylePr overrides (banded
// rows/columns, first/last row or column) are out of scope - see design.md.
type xmlStyles struct {
	Styles []xmlStyle `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main style"`
}

type xmlStyle struct {
	Type    string    `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main type,attr"`
	StyleID string    `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main styleId,attr"`
	BasedOn *xmlVal   `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main basedOn"`
	TblPr   *xmlTblPr `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tblPr"`
	TcPr    *xmlTcPr  `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tcPr"`
}

// resolvedTableStyle is a table style's effective border/shading base, after
// following its w:basedOn chain.
type resolvedTableStyle struct {
	Borders *xmlBorders
	Shd     *xmlShd
}

// buildTableStyles resolves every table style declared in xs (nil-safe: an
// absent or empty word/styles.xml yields an empty map, so callers fall back
// to inline-only resolution exactly as before this existed).
func buildTableStyles(xs *xmlStyles) map[string]resolvedTableStyle {
	resolved := make(map[string]resolvedTableStyle)
	if xs == nil {
		return resolved
	}
	defs := make(map[string]xmlStyle)
	for _, s := range xs.Styles {
		if strings.EqualFold(s.Type, "table") && s.StyleID != "" {
			defs[s.StyleID] = s
		}
	}
	for id := range defs {
		resolved[id] = resolveTableStyle(id, defs, resolved, make(map[string]bool))
	}
	return resolved
}

// resolveTableStyle resolves id's own declarations merged over its w:basedOn
// ancestor chain (nearest style wins per side/field). visiting guards against
// a cyclic basedOn chain; an unresolvable id (missing from defs) resolves to
// the zero value, i.e. no style-level border/shading contribution.
func resolveTableStyle(id string, defs map[string]xmlStyle, cache map[string]resolvedTableStyle, visiting map[string]bool) resolvedTableStyle {
	if rs, ok := cache[id]; ok {
		return rs
	}
	def, ok := defs[id]
	if !ok || visiting[id] {
		return resolvedTableStyle{}
	}
	visiting[id] = true
	defer delete(visiting, id)

	var own resolvedTableStyle
	if def.TblPr != nil {
		own.Borders = def.TblPr.Borders
	}
	if def.TcPr != nil {
		own.Shd = def.TcPr.Shd
	}
	var parent resolvedTableStyle
	if def.BasedOn != nil && def.BasedOn.Val != "" {
		parent = resolveTableStyle(def.BasedOn.Val, defs, cache, visiting)
	}
	merged := resolvedTableStyle{
		Borders: mergeBorders(own.Borders, parent.Borders),
		Shd:     coalesceShd(own.Shd, parent.Shd),
	}
	cache[id] = merged
	return merged
}

// mergeBorders combines two xmlBorders per side, own winning; a nil result
// means neither declared anything.
func mergeBorders(own, fallback *xmlBorders) *xmlBorders {
	if own == nil && fallback == nil {
		return nil
	}
	var o, f xmlBorders
	if own != nil {
		o = *own
	}
	if fallback != nil {
		f = *fallback
	}
	return &xmlBorders{
		Top:    coalesceBorder(o.Top, f.Top),
		Bottom: coalesceBorder(o.Bottom, f.Bottom),
		Left:   coalesceBorder(o.Left, f.Left),
		Right:  coalesceBorder(o.Right, f.Right),
	}
}

func coalesceBorder(a, b *xmlBorder) *xmlBorder {
	if a != nil {
		return a
	}
	return b
}

func coalesceShd(a, b *xmlShd) *xmlShd {
	if a != nil {
		return a
	}
	return b
}

// --- Builders: xml* -> the public document model ---

func buildBody(body xmlBody, tableStyles map[string]resolvedTableStyle) []BodyElement {
	elems := make([]BodyElement, 0, len(body.Children))
	for _, c := range body.Children {
		switch {
		case c.Paragraph != nil:
			p := buildParagraph(*c.Paragraph)
			elems = append(elems, BodyElement{Paragraph: &p})
		case c.Table != nil:
			t := buildTable(*c.Table, tableStyles)
			elems = append(elems, BodyElement{Table: &t})
		}
	}
	return elems
}

func buildParagraphSlice(xps []xmlParagraph) []Paragraph {
	paras := make([]Paragraph, 0, len(xps))
	for _, xp := range xps {
		paras = append(paras, buildParagraph(xp))
	}
	return paras
}

func buildParagraph(xp xmlParagraph) Paragraph {
	var p Paragraph
	if xp.Props != nil {
		p.Props.Alignment = alignmentFrom(xp.Props.Jc)
		p.Props.PageBreak = xp.Props.PageBreakBefore.on()
	}
	for _, xr := range xp.Runs {
		if hasPageBreak(xr) {
			p.Props.PageBreak = true
		}
		text := runText(xr)
		if text == "" {
			continue
		}
		p.Runs = append(p.Runs, Run{Text: text, Props: runProps(xr.Props)})
	}
	return p
}

func alignmentFrom(jc *xmlVal) Alignment {
	if jc == nil {
		return AlignLeft
	}
	switch strings.ToLower(jc.Val) {
	case "center":
		return AlignCenter
	case "right", "end":
		return AlignRight
	case "both", "distribute", "justify":
		return AlignJustify
	default:
		return AlignLeft
	}
}

func hasPageBreak(xr xmlRun) bool {
	for _, br := range xr.Breaks {
		if strings.EqualFold(br.Type, "page") {
			return true
		}
	}
	return false
}

func runText(xr xmlRun) string {
	var b strings.Builder
	for _, t := range xr.Texts {
		b.WriteString(t.Value)
	}
	return b.String()
}

func runProps(rpr *xmlRPr) RunProperties {
	props := RunProperties{
		FontFamily: defaultFontFamily,
		SizePt:     defaultFontSizePt,
	}
	if rpr == nil {
		return props
	}
	props.Bold = rpr.Bold.on()
	props.Italic = rpr.Italic.on()
	props.Underline = underlineOn(rpr.Underline)
	if rpr.Fonts != nil {
		if name := firstNonEmpty(rpr.Fonts.ASCII, rpr.Fonts.HAnsi); name != "" {
			props.FontFamily = name
		}
	}
	if rpr.Size != nil {
		if hp, err := strconv.ParseFloat(strings.TrimSpace(rpr.Size.Val), 64); err == nil && hp > 0 {
			props.SizePt = hp / 2 // half-points → points
		}
	}
	return props
}

func underlineOn(u *xmlVal) bool {
	if u == nil {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(u.Val), "none")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// buildTable converts a parsed <w:tbl> into the public Table model, resolving
// column widths and (per cell) merge/border/shading state. tableStyles is
// consulted when the table references a w:tblStyle, its resolved border
// falling under the table's own inline w:tblBorders (inline wins per side).
func buildTable(xt xmlTable, tableStyles map[string]resolvedTableStyle) Table {
	var tblBorders *xmlBorders
	var styleShd *xmlShd
	if xt.Props != nil {
		tblBorders = xt.Props.Borders
		if xt.Props.StyleID != nil {
			if rs, ok := tableStyles[xt.Props.StyleID.Val]; ok {
				tblBorders = mergeBorders(tblBorders, rs.Borders)
				styleShd = rs.Shd
			}
		}
	}
	rows := make([]Row, 0, len(xt.Rows))
	for _, xr := range xt.Rows {
		rows = append(rows, buildRow(xr, tblBorders, styleShd, tableStyles))
	}
	return Table{Rows: rows, ColumnWidths: resolveColumnWidths(xt)}
}

func buildRow(xr xmlRow, tblBorders *xmlBorders, styleShd *xmlShd, tableStyles map[string]resolvedTableStyle) Row {
	cells := make([]Cell, 0, len(xr.Cells))
	for _, xc := range xr.Cells {
		cells = append(cells, buildCell(xc, tblBorders, styleShd, tableStyles))
	}
	return Row{Cells: cells}
}

func buildCell(xc xmlCell, tblBorders *xmlBorders, styleShd *xmlShd, tableStyles map[string]resolvedTableStyle) Cell {
	cell := Cell{
		Paragraphs: buildParagraphSlice(xc.Paragraphs),
		ColSpan:    gridSpan(xc.Props),
		VMerge:     vMergeState(xc.Props),
		Borders:    resolveCellBorders(tblBorders, tcBordersOf(xc.Props)),
		Shading:    cellShading(shdOf(xc.Props), styleShd),
	}
	if len(xc.NestedTables) > 0 {
		nested := buildTable(xc.NestedTables[0], tableStyles)
		cell.Nested = &nested
	}
	return cell
}

func gridSpan(tcPr *xmlTcPr) int {
	if tcPr == nil || tcPr.GridSpan == nil {
		return 1
	}
	n, err := strconv.Atoi(strings.TrimSpace(tcPr.GridSpan.Val))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

func vMergeState(tcPr *xmlTcPr) VMergeState {
	if tcPr == nil || tcPr.VMerge == nil {
		return VMergeNone
	}
	if strings.EqualFold(strings.TrimSpace(tcPr.VMerge.Val), "restart") {
		return VMergeRestart
	}
	return VMergeContinue // absent w:val (self-closing) or explicit "continue"
}

func tcBordersOf(tcPr *xmlTcPr) *xmlBorders {
	if tcPr == nil {
		return nil
	}
	return tcPr.Borders
}

func shdOf(tcPr *xmlTcPr) *xmlShd {
	if tcPr == nil {
		return nil
	}
	return tcPr.Shd
}

func resolveCellBorders(tblBorders, tcBorders *xmlBorders) CellBorders {
	var tt, tb, tl, tr *xmlBorder
	if tblBorders != nil {
		tt, tb, tl, tr = tblBorders.Top, tblBorders.Bottom, tblBorders.Left, tblBorders.Right
	}
	var ct, cb, cl, cr *xmlBorder
	if tcBorders != nil {
		ct, cb, cl, cr = tcBorders.Top, tcBorders.Bottom, tcBorders.Left, tcBorders.Right
	}
	return CellBorders{
		Top:    resolveBorderSide(tt, ct),
		Bottom: resolveBorderSide(tb, cb),
		Left:   resolveBorderSide(tl, cl),
		Right:  resolveBorderSide(tr, cr),
	}
}

// defaultBorderWidthPt is used when a declared border omits w:sz (or it's
// unparseable), matching Word's own thin default rule width.
const defaultBorderWidthPt = 0.75

// resolveBorderSide prefers the cell-level border, falling back to the
// table-level one; a nil result (neither declared) is the zero BorderSide,
// meaning no border. Unlike shading, a border's color defaults to black
// ("auto"/absent) rather than "no color" - the border still renders.
func resolveBorderSide(tblSide, cellSide *xmlBorder) BorderSide {
	b := cellSide
	if b == nil {
		b = tblSide
	}
	if b == nil || strings.EqualFold(strings.TrimSpace(b.Val), "none") {
		return BorderSide{}
	}
	width := eighthsToPt(b.Sz) // border w:sz is in eighths of a point
	if width <= 0 {
		width = defaultBorderWidthPt
	}
	return BorderSide{
		Style:   b.Val,
		WidthPt: width,
		Color:   normalizeBorderColor(b.Color),
	}
}

// cellShading resolves a cell's shading, falling back to the table style's
// base shading (styleShd) when the cell declares none.
func cellShading(shd, styleShd *xmlShd) string {
	if shd == nil {
		shd = styleShd
	}
	if shd == nil {
		return ""
	}
	return normalizeShadingColor(shd.Fill)
}

// normalizeBorderColor turns a raw OOXML border color into "#RRGGBB",
// defaulting "auto" or an absent value to black - a border with a declared
// style still needs a visible color.
func normalizeBorderColor(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "auto") {
		return "#000000"
	}
	return "#" + strings.ToUpper(v)
}

// normalizeShadingColor turns a raw OOXML fill color into "#RRGGBB", or ""
// for "auto"/"nil"/absent - which mean no shading at all, not black.
func normalizeShadingColor(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "auto") || strings.EqualFold(v, "nil") {
		return ""
	}
	return "#" + strings.ToUpper(v)
}

// resolveColumnWidths reads column widths from w:tblGrid/w:gridCol; when
// absent, each column's width falls back to the widest single-span w:tcW
// seen for that column (cells contributing via gridSpan aren't attributable
// to one column, so they don't affect the fallback).
func resolveColumnWidths(xt xmlTable) []float64 {
	if xt.Grid != nil && len(xt.Grid.Cols) > 0 {
		widths := make([]float64, len(xt.Grid.Cols))
		for i, c := range xt.Grid.Cols {
			widths[i] = dxaToPt(c.W)
		}
		return widths
	}

	var widths []float64
	for _, row := range xt.Rows {
		col := 0
		for _, cell := range row.Cells {
			span := gridSpan(cell.Props)
			for len(widths) < col+span {
				widths = append(widths, 0)
			}
			if span == 1 {
				if w := cellDxaWidth(cell.Props); w > widths[col] {
					widths[col] = w
				}
			}
			col += span
		}
	}
	return widths
}

func cellDxaWidth(tcPr *xmlTcPr) float64 {
	if tcPr == nil || tcPr.TcW == nil {
		return 0
	}
	if t := strings.ToLower(strings.TrimSpace(tcPr.TcW.Type)); t != "" && t != "dxa" {
		return 0
	}
	return dxaToPt(tcPr.TcW.W)
}

// dxaToPt converts twentieths of a point (the unit w:w/w:tblGrid use) to points.
func dxaToPt(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v / 20
}

// eighthsToPt converts eighths of a point (the unit border w:sz uses) to points.
func eighthsToPt(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v / 8
}
