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
	// SectPr is the body-level section geometry (w:sectPr), or nil when the
	// document declares none.
	SectPr *xmlSectPr
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
		case "sectPr":
			var s xmlSectPr
			if err := d.DecodeElement(&s, &start); err != nil {
				return err
			}
			b.SectPr = &s
		default:
			if err := d.Skip(); err != nil {
				return err
			}
		}
	}
}

// xmlSectPr is the body-level <w:sectPr> section geometry. Only page size
// (w:pgSz) and margins (w:pgMar) are modeled; columns, headers/footers,
// gutter, and section-type breaks are out of scope - see design.md.
type xmlSectPr struct {
	PgSz  *xmlPgSz  `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pgSz"`
	PgMar *xmlPgMar `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pgMar"`
}

// xmlPgSz is <w:pgSz>'s w:w/w:h page dimensions, in dxa (twentieths of a point).
type xmlPgSz struct {
	W string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main w,attr"`
	H string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main h,attr"`
}

// xmlPgMar is <w:pgMar>'s four page margins, in dxa. w:header/w:footer/w:gutter
// are out of scope.
type xmlPgMar struct {
	Top    string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main top,attr"`
	Right  string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main right,attr"`
	Bottom string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main bottom,attr"`
	Left   string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main left,attr"`
}

type xmlParagraph struct {
	Props *xmlPPr  `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pPr"`
	Runs  []xmlRun `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main r"`
}

type xmlPPr struct {
	PStyle          *xmlVal      `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pStyle"`
	Jc              *xmlVal      `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main jc"`
	PageBreakBefore *xmlOnOff    `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pageBreakBefore"`
	Ind             *xmlInd      `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main ind"`
	Spacing         *xmlPSpacing `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main spacing"`
}

// xmlInd is <w:ind>: w:left/w:right/w:firstLine/w:hanging, all in dxa
// (twentieths of a point). w:start/w:end (the RTL-aware aliases) are out of
// scope - see design.md.
type xmlInd struct {
	Left      string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main left,attr"`
	Right     string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main right,attr"`
	FirstLine string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main firstLine,attr"`
	Hanging   string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main hanging,attr"`
}

// xmlPSpacing is <w:spacing>'s w:before/w:after (space around the paragraph)
// and w:line/w:lineRule (spacing between lines within it), all in dxa. The
// AutoSpacing flags are out of scope - see design.md.
type xmlPSpacing struct {
	Before   string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main before,attr"`
	After    string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main after,attr"`
	Line     string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main line,attr"`
	LineRule string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main lineRule,attr"`
}

// xmlRun captures its rPr plus an ordered sequence of text and break children
// via a custom UnmarshalXML (encoding/xml's separate slices would lose the
// document order of <w:t> and <w:br>, which matters for in-paragraph line
// breaks). Unsupported run children (e.g. <w:tab>) are skipped without error.
type xmlRun struct {
	Props   *xmlRPr
	Content []xmlRunContent
}

// xmlRunContent is one ordered item of a run: exactly one of Text or Break is
// non-nil.
type xmlRunContent struct {
	Text  *string
	Break *xmlBr
}

func (r *xmlRun) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) error {
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
			if _, end := tok.(xml.EndElement); end {
				return nil // </w:r>: children were consumed by DecodeElement/Skip
			}
			continue
		}
		if start.Name.Space != wordNS {
			if err := d.Skip(); err != nil {
				return err
			}
			continue
		}
		switch start.Name.Local {
		case "rPr":
			var p xmlRPr
			if err := d.DecodeElement(&p, &start); err != nil {
				return err
			}
			r.Props = &p
		case "t":
			var txt xmlText
			if err := d.DecodeElement(&txt, &start); err != nil {
				return err
			}
			s := txt.Value
			r.Content = append(r.Content, xmlRunContent{Text: &s})
		case "br":
			var b xmlBr
			if err := d.DecodeElement(&b, &start); err != nil {
				return err
			}
			r.Content = append(r.Content, xmlRunContent{Break: &b})
		default:
			if err := d.Skip(); err != nil {
				return err
			}
		}
	}
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
	Ind     *xmlTblInd  `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tblInd"`
}

// xmlTblInd is <w:tblInd>: the table's own left indent from its containing
// frame, in dxa. w:type is not distinguished (the schema only really uses
// "dxa" in practice) - see design.md.
type xmlTblInd struct {
	W string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main w,attr"`
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
	DocDefaults *xmlDocDefaults `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main docDefaults"`
	Styles      []xmlStyle      `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main style"`
}

// xmlDocDefaults is <w:docDefaults>; only its paragraph-property default
// (w:pPrDefault/w:pPr, reusing xmlPPr) is modeled - the document-wide spacing
// fallback beneath any paragraph style. w:rPrDefault is out of scope.
type xmlDocDefaults struct {
	PPrDefault *xmlPPrDefault `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pPrDefault"`
}

type xmlPPrDefault struct {
	PPr *xmlPPr `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pPr"`
}

type xmlStyle struct {
	Type    string    `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main type,attr"`
	StyleID string    `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main styleId,attr"`
	BasedOn *xmlVal   `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main basedOn"`
	TblPr   *xmlTblPr `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tblPr"`
	TcPr    *xmlTcPr  `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tcPr"`
	PPr     *xmlPPr   `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pPr"`
}

// resolvedTableStyle is a table style's effective border/shading/indent
// base, after following its w:basedOn chain.
type resolvedTableStyle struct {
	Borders *xmlBorders
	Shd     *xmlShd
	Ind     *xmlTblInd
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
		own.Ind = def.TblPr.Ind
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
		Ind:     coalesceTblInd(own.Ind, parent.Ind),
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

func coalesceTblInd(a, b *xmlTblInd) *xmlTblInd {
	if a != nil {
		return a
	}
	return b
}

// --- Paragraph styles (word/styles.xml) ---
//
// Mirrors the table-style machinery above: only a paragraph style's own
// w:pPr/w:spacing is modeled (the one property this change resolves through
// styles), following its w:basedOn chain. Alignment/indent/run properties
// carried by paragraph styles are out of scope - see design.md.

// resolvedParaStyle is a paragraph style's effective spacing base, after
// following its w:basedOn chain.
type resolvedParaStyle struct {
	Spacing *xmlPSpacing
}

// buildParaStyles resolves every paragraph style declared in xs (nil-safe:
// an absent styles.xml yields an empty map, so callers fall back to inline/
// docDefaults resolution).
func buildParaStyles(xs *xmlStyles) map[string]resolvedParaStyle {
	resolved := make(map[string]resolvedParaStyle)
	if xs == nil {
		return resolved
	}
	defs := make(map[string]xmlStyle)
	for _, s := range xs.Styles {
		if strings.EqualFold(s.Type, "paragraph") && s.StyleID != "" {
			defs[s.StyleID] = s
		}
	}
	for id := range defs {
		resolved[id] = resolveParaStyle(id, defs, resolved, make(map[string]bool))
	}
	return resolved
}

// resolveParaStyle resolves id's own spacing merged over its w:basedOn ancestor
// chain (nearest style wins per field). visiting guards against a cyclic
// basedOn chain; an unresolvable id resolves to the zero value.
func resolveParaStyle(id string, defs map[string]xmlStyle, cache map[string]resolvedParaStyle, visiting map[string]bool) resolvedParaStyle {
	if rs, ok := cache[id]; ok {
		return rs
	}
	def, ok := defs[id]
	if !ok || visiting[id] {
		return resolvedParaStyle{}
	}
	visiting[id] = true
	defer delete(visiting, id)

	var own resolvedParaStyle
	if def.PPr != nil {
		own.Spacing = def.PPr.Spacing
	}
	var parent resolvedParaStyle
	if def.BasedOn != nil && def.BasedOn.Val != "" {
		parent = resolveParaStyle(def.BasedOn.Val, defs, cache, visiting)
	}
	merged := resolvedParaStyle{Spacing: mergeSpacing(own.Spacing, parent.Spacing)}
	cache[id] = merged
	return merged
}

// mergeSpacing combines two xmlPSpacing per field (before/after), own winning
// where it declares a value; a nil result means neither declared anything.
// Structured like mergeBorders.
func mergeSpacing(own, fallback *xmlPSpacing) *xmlPSpacing {
	if own == nil && fallback == nil {
		return nil
	}
	var o, f xmlPSpacing
	if own != nil {
		o = *own
	}
	if fallback != nil {
		f = *fallback
	}
	merged := xmlPSpacing{
		Before: coalesceStr(o.Before, f.Before),
		After:  coalesceStr(o.After, f.After),
	}
	// Line and LineRule travel as a pair: a rule must not be split from its
	// value, so whichever source declares w:line supplies both.
	if o.Line != "" {
		merged.Line, merged.LineRule = o.Line, o.LineRule
	} else {
		merged.Line, merged.LineRule = f.Line, f.LineRule
	}
	return &merged
}

func coalesceStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// --- Builders: xml* -> the public document model ---

// styleContext bundles the resolved style data threaded through the builders:
// table styles (borders/shading/indent), paragraph styles (spacing), and the
// document-wide default spacing from w:docDefaults.
type styleContext struct {
	tables         map[string]resolvedTableStyle
	paragraphs     map[string]resolvedParaStyle
	defaultSpacing *xmlPSpacing
}

func buildBody(body xmlBody, sc styleContext) []BodyElement {
	elems := make([]BodyElement, 0, len(body.Children))
	for _, c := range body.Children {
		switch {
		case c.Paragraph != nil:
			p := buildParagraph(*c.Paragraph, sc)
			elems = append(elems, BodyElement{Paragraph: &p})
		case c.Table != nil:
			t := buildTable(*c.Table, sc)
			elems = append(elems, BodyElement{Table: &t})
		}
	}
	return elems
}

func buildParagraphSlice(xps []xmlParagraph, sc styleContext) []Paragraph {
	paras := make([]Paragraph, 0, len(xps))
	for _, xp := range xps {
		paras = append(paras, buildParagraph(xp, sc))
	}
	return paras
}

func buildParagraph(xp xmlParagraph, sc styleContext) Paragraph {
	var p Paragraph
	var inlineSpacing *xmlPSpacing
	var pStyleID string
	if xp.Props != nil {
		p.Props.Alignment = alignmentFrom(xp.Props.Jc)
		p.Props.PageBreak = xp.Props.PageBreakBefore.on()
		p.Props.Indent = indentFrom(xp.Props.Ind)
		inlineSpacing = xp.Props.Spacing
		if xp.Props.PStyle != nil {
			pStyleID = xp.Props.PStyle.Val
		}
	}
	p.Props.Spacing = resolveSpacing(inlineSpacing, pStyleID, sc)
	for _, xr := range xp.Runs {
		props := runProps(xr.Props)
		for _, c := range xr.Content {
			switch {
			case c.Break != nil:
				if strings.EqualFold(strings.TrimSpace(c.Break.Type), "page") {
					p.Props.PageBreak = true
				} else {
					p.Runs = append(p.Runs, Run{LineBreak: true})
				}
			case c.Text != nil && *c.Text != "":
				p.Runs = append(p.Runs, Run{Text: *c.Text, Props: props})
			}
		}
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

// indentFrom resolves w:ind into an Indent, folding w:firstLine/w:hanging
// into one signed FirstLineOffsetPt (positive/negative respectively; zero
// when neither is declared, or when ind is nil).
func indentFrom(ind *xmlInd) Indent {
	if ind == nil {
		return Indent{}
	}
	var firstLine float64
	switch {
	case ind.FirstLine != "":
		firstLine = dxaToPt(ind.FirstLine)
	case ind.Hanging != "":
		firstLine = -dxaToPt(ind.Hanging)
	}
	return Indent{
		LeftPt:            dxaToPt(ind.Left),
		RightPt:           dxaToPt(ind.Right),
		FirstLineOffsetPt: firstLine,
	}
}

// resolveSpacing resolves a paragraph's effective spacing per field: inline
// w:spacing wins, else the referenced paragraph style's resolved spacing
// (looked up by w:pStyle), else the document defaults, else zero.
func resolveSpacing(inline *xmlPSpacing, pStyleID string, sc styleContext) Spacing {
	styleThenDefault := sc.defaultSpacing
	if ps, ok := sc.paragraphs[pStyleID]; ok {
		styleThenDefault = mergeSpacing(ps.Spacing, styleThenDefault)
	}
	return spacingFrom(mergeSpacing(inline, styleThenDefault))
}

// spacingFrom resolves w:spacing into a Spacing (zero value when sp is nil).
func spacingFrom(sp *xmlPSpacing) Spacing {
	if sp == nil {
		return Spacing{}
	}
	rule, val := lineSpacingFrom(sp.Line, sp.LineRule)
	return Spacing{
		BeforePt:  dxaToPt(sp.Before),
		AfterPt:   dxaToPt(sp.After),
		LineRule:  rule,
		LineValue: val,
	}
}

// lineSpacingFrom resolves w:line/w:lineRule into a rule and its value: a
// multiple for "auto" (w:line in 240ths of a line, e.g. 276 → 1.15), or a
// point height for "exact"/"atLeast" (w:line in dxa). An absent w:lineRule
// with a w:line defaults to "auto" (per OOXML); an absent or unparseable
// w:line yields the single (default) rule.
func lineSpacingFrom(line, rule string) (LineSpacingRule, float64) {
	v, err := strconv.ParseFloat(strings.TrimSpace(line), 64)
	if err != nil {
		return LineSpacingSingle, 0
	}
	switch strings.ToLower(strings.TrimSpace(rule)) {
	case "exact":
		return LineSpacingExact, v / 20
	case "atleast":
		return LineSpacingAtLeast, v / 20
	default: // "auto" or absent
		return LineSpacingMultiple, v / 240
	}
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
func buildTable(xt xmlTable, sc styleContext) Table {
	var tblBorders *xmlBorders
	var styleShd *xmlShd
	var tblInd *xmlTblInd
	if xt.Props != nil {
		tblBorders = xt.Props.Borders
		tblInd = xt.Props.Ind
		if xt.Props.StyleID != nil {
			if rs, ok := sc.tables[xt.Props.StyleID.Val]; ok {
				tblBorders = mergeBorders(tblBorders, rs.Borders)
				styleShd = rs.Shd
				if tblInd == nil {
					tblInd = rs.Ind
				}
			}
		}
	}
	rows := make([]Row, 0, len(xt.Rows))
	for _, xr := range xt.Rows {
		rows = append(rows, buildRow(xr, tblBorders, styleShd, sc))
	}
	return Table{Rows: rows, ColumnWidths: resolveColumnWidths(xt), IndentPt: tblIndPt(tblInd)}
}

func tblIndPt(ind *xmlTblInd) float64 {
	if ind == nil {
		return 0
	}
	return dxaToPt(ind.W)
}

func buildRow(xr xmlRow, tblBorders *xmlBorders, styleShd *xmlShd, sc styleContext) Row {
	cells := make([]Cell, 0, len(xr.Cells))
	for _, xc := range xr.Cells {
		cells = append(cells, buildCell(xc, tblBorders, styleShd, sc))
	}
	return Row{Cells: cells}
}

func buildCell(xc xmlCell, tblBorders *xmlBorders, styleShd *xmlShd, sc styleContext) Cell {
	cell := Cell{
		Paragraphs: buildParagraphSlice(xc.Paragraphs, sc),
		ColSpan:    gridSpan(xc.Props),
		VMerge:     vMergeState(xc.Props),
		Borders:    resolveCellBorders(tblBorders, tcBordersOf(xc.Props)),
		Shading:    cellShading(shdOf(xc.Props), styleShd),
	}
	if len(xc.NestedTables) > 0 {
		nested := buildTable(xc.NestedTables[0], sc)
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

// dxaToPtOr converts a dxa attribute to points, returning def when the
// attribute is absent (empty) or unparseable. Unlike dxaToPt, a declared "0"
// is honored as 0 pt (a valid margin), so only truly missing values default.
func dxaToPtOr(s string, def float64) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v / 20
}

// Default page geometry (A4, one-inch margins) used when a document declares
// no w:sectPr, no w:pgSz/w:pgMar, or an unparseable value.
const (
	defaultPageWidthPt  = 595.28 // A4 width
	defaultPageHeightPt = 841.89 // A4 height
	defaultMarginPt     = 72.0   // 1 inch
)

// buildPageGeometry resolves a body-level w:sectPr into the public
// PageGeometry, converting dxa → pt and falling back to the A4/one-inch
// defaults per missing element or attribute (nil-safe). A non-positive page
// dimension is treated as absent.
func buildPageGeometry(sp *xmlSectPr) PageGeometry {
	g := PageGeometry{
		WidthPt:        defaultPageWidthPt,
		HeightPt:       defaultPageHeightPt,
		MarginTopPt:    defaultMarginPt,
		MarginRightPt:  defaultMarginPt,
		MarginBottomPt: defaultMarginPt,
		MarginLeftPt:   defaultMarginPt,
	}
	if sp == nil {
		return g
	}
	if sp.PgSz != nil {
		if w := dxaToPtOr(sp.PgSz.W, 0); w > 0 {
			g.WidthPt = w
		}
		if h := dxaToPtOr(sp.PgSz.H, 0); h > 0 {
			g.HeightPt = h
		}
	}
	if sp.PgMar != nil {
		g.MarginTopPt = dxaToPtOr(sp.PgMar.Top, defaultMarginPt)
		g.MarginRightPt = dxaToPtOr(sp.PgMar.Right, defaultMarginPt)
		g.MarginBottomPt = dxaToPtOr(sp.PgMar.Bottom, defaultMarginPt)
		g.MarginLeftPt = dxaToPtOr(sp.PgMar.Left, defaultMarginPt)
	}
	return g
}

// eighthsToPt converts eighths of a point (the unit border w:sz uses) to points.
func eighthsToPt(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v / 8
}
