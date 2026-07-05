package convert

import "io"

// Page geometry in points (A4, 1-inch margins). Fixed for the MVP because
// section geometry (w:sectPr) is out of scope; a single set of constants keeps
// it trivial to make configurable later.
const (
	pageWidthPt  = 595.28 // A4 width
	pageHeightPt = 841.89 // A4 height
	marginPt     = 72.0   // 1 inch

	contentWidthPt  = pageWidthPt - 2*marginPt
	contentHeightPt = pageHeightPt - 2*marginPt

	// lineSpacing multiplies a line's tallest font size to get its box height.
	lineSpacing = 1.2

	// cellPaddingPt is the fixed inner padding applied to every table cell on
	// every side; per-cell padding customization (w:tcMar) is out of scope.
	cellPaddingPt = 4.0
	// minRowHeightPt floors a table row's height so an all-empty or
	// all-vMerge-continuation row still occupies visible space.
	minRowHeightPt = defaultRenderSizePt*lineSpacing + 2*cellPaddingPt
)

// renderer is the thin seam over the PDF backend. Coordinates and sizes are in
// points; the origin is the top-left of the page and DrawText positions text by
// its baseline. Keeping this interface minimal lets the fpdf backend be swapped
// (or gain TTF embedding) without touching the layout or public API.
type renderer interface {
	// SetFont selects the active font for subsequent TextWidth/DrawText calls.
	SetFont(family string, bold, italic, underline bool, sizePt float64)
	// TextWidth measures s in the active font, in points.
	TextWidth(s string) float64
	// AddPage starts a new page and makes it current.
	AddPage()
	// DrawText draws s with its baseline at (x, y) in the active font.
	DrawText(x, y float64, s string)
	// FillRect fills the rectangle with top-left corner (x, y), width w and
	// height h, in the given "#RRGGBB" color. A no-op for an empty colorHex.
	FillRect(x, y, w, h float64, colorHex string)
	// StrokeLine draws a line from (x1, y1) to (x2, y2) with the given width
	// (points) and "#RRGGBB" color. A no-op for an empty colorHex.
	StrokeLine(x1, y1, x2, y2, widthPt float64, colorHex string)
	// Output writes the finished PDF, returning any accumulated backend error.
	Output(w io.Writer) error
}
