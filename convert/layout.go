package convert

import (
	"unicode"

	"github.com/h0rn3t/goffice/document"
)

// word is a single whitespace-delimited token carrying its run's formatting.
// gap is the width of the single space that precedes it on a line (0 for the
// first word of a line or when no whitespace separated it from the previous
// token — e.g. a formatting change mid-word).
type word struct {
	text  string
	props document.RunProperties
	width float64
	gap   float64
}

// line is a packed row of words ready to draw.
type line struct {
	words   []word
	natural float64 // words plus their internal gaps
	maxSize float64 // tallest run size on the line
	height  float64 // vertical advance (maxSize * lineSpacing)
}

// layoutParagraph packs a paragraph's runs into lines that fit width. A word
// wider than width is placed alone on its line and allowed to overflow rather
// than looping forever. Returns nil for an empty paragraph.
func layoutParagraph(r renderer, p document.Paragraph, width float64) []line {
	words := measureWords(r, p)
	if len(words) == 0 {
		return nil
	}

	var lines []line
	var cur []word
	var curW, curMax float64
	flush := func() {
		lines = append(lines, line{
			words:   cur,
			natural: curW,
			maxSize: curMax,
			height:  curMax * lineSpacing,
		})
		cur, curW, curMax = nil, 0, 0
	}

	for _, w := range words {
		gap := w.gap
		if len(cur) == 0 {
			gap = 0 // no leading space at the start of a line
		}
		if len(cur) > 0 && curW+gap+w.width > width {
			flush()
			gap = 0
		}
		w.gap = gap
		cur = append(cur, w)
		curW += gap + w.width
		if w.props.SizePt > curMax {
			curMax = w.props.SizePt
		}
	}
	if len(cur) > 0 {
		flush()
	}
	return lines
}

// drawLine renders one packed line at baseline y, positioned within [x0, x0+width]
// per the paragraph's alignment. isLast suppresses justification of a
// paragraph's final line.
func drawLine(r renderer, ln line, align document.Alignment, x0, width, y float64, isLast bool) {
	x := x0
	var extraPerGap float64
	switch align {
	case document.AlignRight:
		x = x0 + (width - ln.natural)
	case document.AlignCenter:
		x = x0 + (width-ln.natural)/2
	case document.AlignJustify:
		if !isLast {
			if gaps := gapCount(ln); gaps > 0 {
				if slack := width - ln.natural; slack > 0 {
					extraPerGap = slack / float64(gaps)
				}
			}
		}
	}
	if x < x0 { // clamp an overflowing (oversized) line to the left edge
		x = x0
	}

	baseline := y + ln.maxSize
	for i, w := range ln.words {
		if i > 0 {
			x += w.gap
			if extraPerGap > 0 && w.gap > 0 {
				x += extraPerGap
			}
		}
		r.SetFont(w.props.FontFamily, w.props.Bold, w.props.Italic, w.props.Underline, w.props.SizePt)
		r.DrawText(x, baseline, w.text)
		x += w.width
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

// measureWords tokenizes a paragraph's runs into words with widths and leading
// gaps measured in each token's own font.
func measureWords(r renderer, p document.Paragraph) []word {
	var words []word
	pendingGap := false
	var gapProps document.RunProperties

	for _, run := range p.Runs {
		for _, tok := range tokenize(run.Text) {
			if tok.space {
				pendingGap = true
				gapProps = run.Props
				continue
			}
			r.SetFont(run.Props.FontFamily, run.Props.Bold, run.Props.Italic, run.Props.Underline, run.Props.SizePt)
			w := word{text: tok.text, props: run.Props, width: r.TextWidth(tok.text)}
			if pendingGap {
				r.SetFont(gapProps.FontFamily, gapProps.Bold, gapProps.Italic, gapProps.Underline, gapProps.SizePt)
				w.gap = r.TextWidth(" ")
			}
			words = append(words, w)
			pendingGap = false
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
