package convert

import (
	"bytes"
	"testing"

	"github.com/h0rn3t/goffice/convert/fonts"
)

func TestMapFontFamily(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Times New Roman", fonts.Serif},
		{"Georgia", fonts.Serif},
		{"Garamond", fonts.Serif},
		{"Calibri", fonts.Sans},
		{"Arial", fonts.Sans},
		{"", fonts.Sans},
		{"Courier New", fonts.Mono},
		{"Consolas", fonts.Mono},
		{"Menlo", fonts.Mono},
	}
	for _, tt := range tests {
		if got := mapFontFamily(tt.name); got != tt.want {
			t.Errorf("mapFontFamily(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestTrivialRenderEmitsPDF(t *testing.T) {
	r := newFPDFRenderer()
	r.AddPage()
	r.SetFont(fonts.Sans, true, false, true, 12)
	r.DrawText(marginPt, 120, "hello")

	var buf bytes.Buffer
	if err := r.Output(&buf); err != nil {
		t.Fatalf("Output: %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("output does not start with %%PDF-: %q", buf.Bytes()[:min(8, buf.Len())])
	}
}
