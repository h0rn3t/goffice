package document

import "testing"

// colorRun is a text run carrying a resolved text color.
func colorRun(text, color string) Run {
	return Run{Text: text, Props: RunProperties{FontFamily: "Helvetica", SizePt: 12, Color: color}}
}

// TestRender_TextColorRecordedPerDraw verifies the renderer sets each run's
// resolved color before drawing it: a non-empty color is passed through, and an
// empty color reaches the backend as "" (which fpdf's SetTextColor draws black).
func TestRender_TextColorRecordedPerDraw(t *testing.T) {
	p := Paragraph{Runs: []Run{colorRun("red", "#FF0000"), colorRun(" plain", "")}}
	f := &fakeRenderer{}
	(&Converter{doc: &Document{Body: bodyOf(p)}}).render(f)

	byText := map[string]string{}
	for _, d := range f.draws {
		byText[d.text] = d.color
	}
	if got := byText["red"]; got != "#FF0000" {
		t.Fatalf("draw %q color = %q, want #FF0000", "red", got)
	}
	if got := byText["plain"]; got != "" {
		t.Fatalf("draw %q color = %q, want empty (black path)", "plain", got)
	}
}
