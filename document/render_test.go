package document_test

import (
	"bytes"
	"compress/zlib"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/h0rn3t/docx2pdf/document"
	"github.com/h0rn3t/docx2pdf/internal/docxtest"
)

type fixture struct {
	name    string
	body    string
	present []string
	absent  []string
}

var fixtures = []fixture{
	{
		name: "plain multi-paragraph",
		body: `<w:p><w:r><w:t>Alpha paragraph one.</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>Bravo paragraph two.</w:t></w:r></w:p>`,
		present: []string{"Alpha", "Bravo"},
	},
	{
		name: "mixed formatting and sizes",
		body: `<w:p>` +
			`<w:r><w:rPr><w:b/></w:rPr><w:t>Bold </w:t></w:r>` +
			`<w:r><w:rPr><w:i/></w:rPr><w:t>italic </w:t></w:r>` +
			`<w:r><w:rPr><w:u w:val="single"/><w:sz w:val="40"/></w:rPr><w:t>underline</w:t></w:r>` +
			`</w:p>`,
		present: []string{"Bold", "italic", "underline"},
	},
	{
		name:    "centered paragraph",
		body:    `<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>Centered heading</w:t></w:r></w:p>`,
		present: []string{"Centered"},
	},
	{
		name: "explicit page break",
		body: `<w:p><w:r><w:t>Before break</w:t></w:r></w:p>` +
			`<w:p><w:r><w:br w:type="page"/></w:r><w:r><w:t>After break</w:t></w:r></w:p>`,
		present: []string{"Before", "After"},
	},
	{
		name: "table renders",
		body: `<w:p><w:r><w:t>Kept paragraph</w:t></w:r></w:p>` +
			`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>InTable</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`,
		present: []string{"Kept", "InTable"},
	},
	{
		name:    "unicode text renders legibly",
		body:    `<w:p><w:r><w:t>Кирилиця é ü ПРИВІТ</w:t></w:r></w:p>`,
		present: []string{"Кирилиця", "é", "ü", "ПРИВІТ"},
	},
	{
		name: "unicode paragraph and a merged/shaded/bordered table together",
		body: `<w:p><w:r><w:t>Заголовок документа</w:t></w:r></w:p>` +
			`<w:tbl>` +
			`<w:tblPr><w:tblBorders><w:top w:val="single" w:sz="8" w:color="000000"/></w:tblBorders></w:tblPr>` +
			`<w:tr>` +
			`<w:tc><w:tcPr><w:gridSpan w:val="2"/><w:shd w:fill="D9D9D9"/></w:tcPr><w:p><w:r><w:t>Шапка таблиці</w:t></w:r></w:p></w:tc>` +
			`</w:tr>` +
			`<w:tr>` +
			`<w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr><w:p><w:r><w:t>Комірка A</w:t></w:r></w:p></w:tc>` +
			`<w:tc><w:p><w:r><w:t>Комірка B</w:t></w:r></w:p></w:tc>` +
			`</w:tr>` +
			`<w:tr>` +
			`<w:tc><w:tcPr><w:vMerge/></w:tcPr><w:p/></w:tc>` +
			`<w:tc><w:p><w:r><w:t>Комірка C</w:t></w:r></w:p></w:tc>` +
			`</w:tr>` +
			`</w:tbl>`,
		present: []string{"Заголовок", "документа", "Шапка", "таблиці", "Комірка", "A", "B", "C"},
	},
}

func TestConvertFixturesEndToEnd(t *testing.T) {
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			doc := openDoc(t, fx.body)

			var buf bytes.Buffer
			if err := document.ConvertToPdf(doc).Write(&buf); err != nil {
				t.Fatalf("Write: %v", err)
			}
			raw := buf.Bytes()
			if !bytes.HasPrefix(raw, []byte("%PDF-")) {
				t.Fatalf("output does not start with %%PDF- header")
			}
			if len(raw) < 100 {
				t.Fatalf("output is implausibly small (%d bytes)", len(raw))
			}

			text := decodePDFText(raw)
			for _, tok := range fx.present {
				if !strings.Contains(text, tok) {
					t.Errorf("PDF text is missing expected token %q", tok)
				}
			}
			for _, tok := range fx.absent {
				if strings.Contains(text, tok) {
					t.Errorf("PDF text unexpectedly contains skipped token %q", tok)
				}
			}
		})
	}
}

func TestWriteToFile_OK(t *testing.T) {
	doc := openDoc(t, `<w:p><w:r><w:t>File output</w:t></w:r></w:p>`)
	path := filepath.Join(t.TempDir(), "out.pdf")

	if err := document.ConvertToPdf(doc).WriteToFile(path); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatal("written file is not a PDF")
	}
}

func TestWriteToFile_UnwritableDestination(t *testing.T) {
	doc := openDoc(t, `<w:p><w:r><w:t>x</w:t></w:r></w:p>`)
	// A file inside a non-existent directory cannot be created.
	path := filepath.Join(t.TempDir(), "missing-dir", "out.pdf")

	if err := document.ConvertToPdf(doc).WriteToFile(path); err == nil {
		t.Fatal("expected an error writing to an uncreatable destination")
	}
}

func openDoc(t *testing.T, bodyXML string) *document.Document {
	t.Helper()
	doc, err := document.Open(docxtest.Build(t, bodyXML))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = doc.Close() })
	return doc
}

func openDocWithStyles(t *testing.T, bodyXML, stylesXML string) *document.Document {
	t.Helper()
	doc, err := document.Open(docxtest.BuildWithStyles(t, bodyXML, stylesXML))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = doc.Close() })
	return doc
}

// TestConvertTableWithOnlyNamedStyleRendersBorders is the end-to-end
// regression for the docx-table-style-borders change: a table with no
// inline w:tblBorders/w:tcBorders, only a w:tblStyle reference, must still
// produce visible cell borders in the PDF - not just correctly positioned
// text with an invisible grid.
func TestConvertTableWithOnlyNamedStyleRendersBorders(t *testing.T) {
	body := `<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/></w:tblPr>` +
		`<w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	styles := `<w:style w:type="table" w:styleId="TableGrid">` +
		`<w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:color="auto"/></w:tblBorders></w:tblPr>` +
		`</w:style>`
	doc := openDocWithStyles(t, body, styles)

	var buf bytes.Buffer
	if err := document.ConvertToPdf(doc).Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	content := decodePDFStreams(buf.Bytes())
	if !bytes.Contains(content, []byte(" l S")) {
		t.Fatalf("expected a stroked border line ('l S' operator) in the rendered PDF content, got:\n%s", content)
	}
}

// TestConvertBandedTableStyleRendersFill is a smoke check that band1Horz
// shading from w:tblStylePr reaches the PDF as a filled path.
func TestConvertBandedTableStyleRendersFill(t *testing.T) {
	body := `<w:tbl><w:tblPr><w:tblStyle w:val="Banded"/>` +
		`<w:tblLook w:firstRow="0" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="1"/>` +
		`</w:tblPr>` +
		`<w:tr><w:tc><w:p><w:r><w:t>a</w:t></w:r></w:p></w:tc></w:tr>` +
		`<w:tr><w:tc><w:p><w:r><w:t>b</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	styles := `<w:style w:type="table" w:styleId="Banded">` +
		`<w:tblStylePr w:type="band1Horz"><w:tcPr><w:shd w:fill="D6DCE4"/></w:tcPr></w:tblStylePr>` +
		`</w:style>`
	doc := openDocWithStyles(t, body, styles)

	var buf bytes.Buffer
	if err := document.ConvertToPdf(doc).Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	content := decodePDFStreams(buf.Bytes())
	// Cell fill is drawn with the 'f' operator after an RGB set ('rg').
	if !bytes.Contains(content, []byte(" rg")) || !bytes.Contains(content, []byte(" f")) {
		t.Fatalf("expected a filled cell shading path in the rendered PDF content, got:\n%s", content)
	}
}

// decodePDFStreams concatenates every decompressed page content stream in a
// produced PDF, giving tests raw access to the drawing operators fpdf wrote
// (text, fills, strokes, ...).
func decodePDFStreams(raw []byte) []byte {
	var out bytes.Buffer
	rest := raw
	for {
		_, after, found := bytes.Cut(rest, []byte("stream"))
		if !found {
			break
		}
		after = bytes.TrimPrefix(after, []byte("\r"))
		after = bytes.TrimPrefix(after, []byte("\n"))
		chunk, tail, found := bytes.Cut(after, []byte("endstream"))
		if !found {
			break
		}
		if zr, err := zlib.NewReader(bytes.NewReader(chunk)); err == nil {
			if data, err := io.ReadAll(zr); err == nil {
				out.Write(data)
			}
			_ = zr.Close()
		}
		rest = tail
	}
	return out.Bytes()
}

// decodePDFText extracts the text actually drawn in a produced PDF, so tests
// can assert on what was rendered. convert always draws through an embedded
// UTF-8 (Identity-H) font (see document/fonts), and fpdf's Text() encodes
// drawn text as UTF-16BE inside "(...) Tj" literal-string operators, so
// recovering it just requires un-escaping the PDF string syntax and reading
// the bytes back as UTF-16BE.
func decodePDFText(raw []byte) string {
	return extractShownText(decodePDFStreams(raw))
}

// extractShownText scans a decompressed PDF content stream for "(...) Tj"
// literal-string operators and decodes each one as UTF-16BE.
func extractShownText(content []byte) string {
	var out strings.Builder
	for i := 0; i < len(content); i++ {
		if content[i] != '(' {
			continue
		}
		lit, end, ok := scanPDFLiteral(content, i)
		if !ok {
			break
		}
		out.WriteString(decodeUTF16BE(lit))
		i = end
	}
	return out.String()
}

// scanPDFLiteral reads a PDF literal string starting at b[start] == '(',
// unescaping fpdf's own escape set (\\, \(, \), \r). It returns the
// unescaped bytes and the index of the closing ')'.
func scanPDFLiteral(b []byte, start int) (lit []byte, end int, ok bool) {
	i := start + 1
	for i < len(b) {
		switch b[i] {
		case '\\':
			if i+1 >= len(b) {
				return nil, 0, false
			}
			c := b[i+1]
			if c == 'r' {
				c = '\r'
			}
			lit = append(lit, c)
			i += 2
		case ')':
			return lit, i, true
		default:
			lit = append(lit, b[i])
			i++
		}
	}
	return nil, 0, false
}

// decodeUTF16BE decodes b as big-endian UTF-16 code units, returning "" for
// odd-length input (never produced by convert's own renderer).
func decodeUTF16BE(b []byte) string {
	if len(b)%2 != 0 {
		return ""
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return string(utf16.Decode(units))
}
