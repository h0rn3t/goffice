package document

import (
	"math"
	"unicode"
)

// word is a single whitespace-delimited token carrying its run's formatting.
// gap is the width of the single space that precedes it on a line (0 for the
// first word of a line or when no whitespace separated it from the previous
// token — e.g. a formatting change mid-word).
type word struct {
	text  string
	props RunProperties
	width float64
	gap   float64
	// lineBreak marks an explicit in-paragraph line break (a zero-width marker,
	// never drawn); layoutParagraph ends the current line when it reaches one.
	lineBreak bool
	// img is the inline picture this word draws instead of text; its width is
	// the picture's, and it sits on the baseline like a glyph.
	img *Image
}

// height is what the word contributes to its line's height: a picture's full
// height, or (for text) zero - text height comes from the line's font size.
func (w word) height() float64 {
	if w.img == nil {
		return 0
	}
	return w.img.HeightPt
}

// line is a packed row of words ready to draw.
type line struct {
	words   []word
	natural float64 // words plus their internal gaps
	maxSize float64 // tallest run size on the line
	maxImg  float64 // tallest inline image on the line (0 when text-only)
	height  float64 // vertical advance
}

// lineHeightFor computes a line's vertical advance from the paragraph's
// resolved line spacing. The natural single-spaced height of a line is its
// tallest run size times the default multiplier (lineSpacing), floored at the
// tallest inline image so a picture is never clipped by the line it sits on;
// OOXML's "auto" rule counts that natural height in lines (w:line ÷ 240), so a
// multiple scales the natural height rather than the bare font size. Exact fixes
// the height; at-least floors it at the natural height; single uses the natural
// height.
func lineHeightFor(maxSize, maxImg float64, sp Spacing) float64 {
	natural := math.Max(maxSize*lineSpacing, maxImg)
	switch sp.LineRule {
	case LineSpacingMultiple:
		return natural * sp.LineValue
	case LineSpacingExact:
		return math.Max(sp.LineValue, maxImg)
	case LineSpacingAtLeast:
		return math.Max(natural, sp.LineValue)
	default: // LineSpacingSingle
		return natural
	}
}

// layoutParagraph packs a paragraph's runs into lines that fit width. A word
// wider than width is placed alone on its line and allowed to overflow rather
// than looping forever. Returns nil for an empty paragraph.
func layoutParagraph(r renderer, p Paragraph, width float64) []line {
	words := measureWords(r, p)
	if len(words) == 0 {
		return nil
	}

	var lines []line
	var cur []word
	var curW, curMax, curImg float64
	flush := func() {
		lines = append(lines, line{
			words:   cur,
			natural: curW,
			maxSize: curMax,
			maxImg:  curImg,
			height:  lineHeightFor(curMax, curImg, p.Props.Spacing),
		})
		cur, curW, curMax, curImg = nil, 0, 0, 0
	}

	// hardStart marks a line whose first word may keep its leading whitespace as
	// an indent: the paragraph's first line and any line after an explicit
	// <w:br/>. A soft-wrap continuation is not a hard start, so its leading
	// whitespace is dropped.
	hardStart := true
	for _, w := range words {
		if w.lineBreak {
			if len(cur) > 0 {
				flush()
			} else { // a break on an empty line is a blank line
				lines = append(lines, line{height: lineHeightFor(defaultRenderSizePt, 0, p.Props.Spacing)})
			}
			hardStart = true
			continue
		}
		gap := w.gap
		if len(cur) == 0 && !hardStart {
			gap = 0 // drop leading whitespace at a soft-wrap continuation line
		}
		if len(cur) > 0 && curW+gap+w.width > width {
			flush()
			gap = 0 // this word now opens a soft-wrap line
		}
		w.gap = gap
		cur = append(cur, w)
		curW += gap + w.width
		curMax = math.Max(curMax, w.props.SizePt)
		curImg = math.Max(curImg, w.height())
		hardStart = false
	}
	if len(cur) > 0 {
		flush()
	}
	return lines
}

// drawLine renders one packed line at baseline y, positioned within [x0, x0+width]
// per the paragraph's alignment. isLast suppresses justification of a
// paragraph's final line. firstLineOffsetPt shifts only the first line
// (isFirst): positive for a first-line indent, negative for a hanging
// indent; it applies to AlignLeft/AlignJustify only - Word's first-line/
// hanging indent combined with center/right alignment is out of scope.
func drawLine(r renderer, ln line, align Alignment, x0, width, y float64, isLast bool, firstLineOffsetPt float64, isFirst bool) {
	// lead is a line's leading-whitespace indent (a hard-start line's first-word
	// gap; layout already zeroed it for soft-wrap continuations). It shifts
	// left/justify text right; centered/right-aligned text is positioned on its
	// content alone, so lead is subtracted from the alignment width and not drawn.
	var lead float64
	if len(ln.words) > 0 {
		lead = ln.words[0].gap
	}
	x := x0
	var extraPerGap float64
	switch align {
	case AlignRight:
		x = x0 + (width - (ln.natural - lead))
	case AlignCenter:
		x = x0 + (width-(ln.natural-lead))/2
	case AlignJustify:
		if !isLast {
			if gaps := gapCount(ln); gaps > 0 {
				if slack := width - ln.natural; slack > 0 {
					extraPerGap = slack / float64(gaps)
				}
			}
		}
		if isFirst {
			x += firstLineOffsetPt
		}
		x += lead
	default: // AlignLeft
		if isFirst {
			x += firstLineOffsetPt
		}
		x += lead
	}
	minX := x0
	if isFirst && firstLineOffsetPt < 0 { // a hanging indent legitimately starts left of x0
		minX = x0 + firstLineOffsetPt
	}
	if x < minX { // clamp an overflowing (oversized) line to its left edge
		x = minX
	}

	// An inline image sits on the baseline like a glyph, so a line taller than
	// its text pushes the baseline down to the image's foot rather than letting
	// the picture overflow the line box upward.
	baseline := y + math.Max(ln.maxSize, ln.maxImg)
	placed := make([]float64, len(ln.words)) // each word's x, so shading can be drawn under it
	for i, w := range ln.words {
		if i > 0 {
			x += w.gap
			if extraPerGap > 0 && w.gap > 0 {
				x += extraPerGap
			}
		}
		placed[i] = x
		x += w.width
	}

	// Backgrounds first, so text and pictures draw on top of their shading. A
	// run's shading covers the spaces inside it too, so a word whose predecessor
	// carries the same background extends its rectangle back over the gap
	// between them rather than leaving it unpainted.
	for i, w := range ln.words {
		if w.props.Shading == "" || w.img != nil {
			continue
		}
		left := placed[i]
		if i > 0 && ln.words[i-1].props.Shading == w.props.Shading && ln.words[i-1].img == nil {
			left = placed[i-1] + ln.words[i-1].width
		}
		top := baseline - w.props.SizePt
		r.FillRect(left, top, placed[i]+w.width-left, w.props.SizePt*lineSpacing, w.props.Shading)
	}
	for i, w := range ln.words {
		if w.img != nil {
			r.DrawImage(placed[i], baseline-w.img.HeightPt, w.img.WidthPt, w.img.HeightPt, w.img)
			continue
		}
		r.SetFont(w.props.FontFamily, w.props.Bold, w.props.Italic, w.props.Underline, w.props.SizePt)
		r.SetTextColor(w.props.Color)
		r.DrawText(placed[i], baseline, w.text)
	}
}

func gapCount(ln line) int {
	n := 0
	for i, w := range ln.words {
		if i > 0 && w.gap > 0 {
			n++
		}
	}
	return n
}

// measureWords tokenizes a paragraph's runs into words, each carrying the
// measured width of the whitespace preceding it (its gap). A whitespace run is
// measured at its full width in its own font - preserved `xml:space="preserve"`
// spaces keep their width rather than collapsing to one - accumulating across
// consecutive whitespace tokens (e.g. spaces split by a formatting change).
func measureWords(r renderer, p Paragraph) []word {
	var words []word
	var pendingGap float64

	for _, run := range p.Runs {
		switch {
		case run.LineBreak:
			words = append(words, word{lineBreak: true})
			pendingGap = 0
			continue
		case run.Image != nil:
			words = append(words, word{
				img:   run.Image,
				props: run.Props,
				width: math.Max(run.Image.WidthPt, run.MinWidthPt),
				gap:   pendingGap,
			})
			pendingGap = 0
			continue
		}
		for i, tok := range tokenize(run.Text) {
			r.SetFont(run.Props.FontFamily, run.Props.Bold, run.Props.Italic, run.Props.Underline, run.Props.SizePt)
			if tok.space {
				pendingGap += r.TextWidth(tok.text)
				continue
			}
			w := word{text: tok.text, props: run.Props, width: r.TextWidth(tok.text), gap: pendingGap}
			if i == 0 { // the run's advance floor applies to its first token (a list marker is one)
				w.width = math.Max(w.width, run.MinWidthPt)
			}
			words = append(words, w)
			pendingGap = 0
		}
	}
	return words
}

type token struct {
	text  string
	space bool
}

// tokenize splits s into alternating runs of whitespace and non-whitespace.
// Whitespace runs collapse to a single space token (width is measured as one
// space); non-whitespace runs become word tokens.
func tokenize(s string) []token {
	var out []token
	start := 0
	inSpace := false
	initialized := false
	for i, r := range s {
		sp := unicode.IsSpace(r)
		if !initialized {
			inSpace = sp
			initialized = true
			continue
		}
		if sp != inSpace {
			out = append(out, token{text: s[start:i], space: inSpace})
			start = i
			inSpace = sp
		}
	}
	if initialized {
		out = append(out, token{text: s[start:], space: inSpace})
	}
	return out
}
