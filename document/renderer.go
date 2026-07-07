package document

import (
	"io"
)

const (
	// lineSpacing multiplies a line's tallest font size to get its box height.
	lineSpacing = 1.2

	// cellPaddingPt is the fixed inner padding applied to every table cell on
	// every side; per-cell padding customization (w:tcMar) is out of scope.
	cellPaddingPt = 4.0
	// minRowHeightPt floors a table row's height so an all-empty or
	// all-vMerge-continuation row still occupies visible space.
	minRowHeightPt = defaultRenderSizePt*lineSpacing + 2*cellPaddingPt
)

// page is the per-document layout frame in points, derived from the
// document's PageGeometry: where content starts (left/top margin), how wide it
// may be, and the y beyond which content must paginate (page height minus the
// bottom margin).
type page struct {
	originX, originY float64
	contentWidth     float64
	bottomLimit      float64
}

func pageFrom(g PageGeometry) page {
	return page{
		originX:      g.MarginLeftPt,
		originY:      g.MarginTopPt,
		contentWidth: g.WidthPt - g.MarginLeftPt - g.MarginRightPt,
		bottomLimit:  g.HeightPt - g.MarginBottomPt,
	}
}

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
