package document_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/h0rn3t/docx2pdf/document"
	"github.com/h0rn3t/docx2pdf/internal/docxtest"
)

// --- Hyperlinks ---

func TestHyperlink_TextIsSurfacedInOrder(t *testing.T) {
	body := `<w:p>` +
		`<w:r><w:t>See </w:t></w:r>` +
		`<w:hyperlink r:id="rId5"><w:r><w:t>the link</w:t></w:r></w:hyperlink>` +
		`<w:r><w:t> for details</w:t></w:r>` +
		`</w:p>`
	doc := mustOpen(t, body)
	got := concatText(paragraphs(t, doc))
	if want := "See the link for details"; got != want {
		t.Fatalf("paragraph text = %q, want %q (hyperlink text must render in place)", got, want)
	}
}

// --- Page-number fields ---

func TestField_ComplexPageBecomesAComputedRun(t *testing.T) {
	body := `<w:p>` +
		`<w:r><w:t>p</w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:instrText> PAGE \* MERGEFORMAT </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:t>7</w:t></w:r>` + // stale cached value: must NOT be surfaced
		`<w:r><w:fldChar w:fldCharType="end"/></w:r>` +
		`</w:p>`
	runs := paragraphs(t, doc(t, body))[0].Runs
	var fields, texts int
	for _, r := range runs {
		if r.Field == document.FieldPage {
			fields++
		}
		if strings.Contains(r.Text, "7") {
			texts++
		}
	}
	if fields != 1 {
		t.Fatalf("PAGE field runs = %d, want 1", fields)
	}
	if texts != 0 {
		t.Fatalf("the stale cached result %q leaked as text; it must be replaced by the field", "7")
	}
}

func TestField_UnknownComplexFieldKeepsItsCachedText(t *testing.T) {
	body := `<w:p>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:instrText> DATE </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:t>2026-07-15</w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="end"/></w:r>` +
		`</w:p>`
	got := concatText(paragraphs(t, doc(t, body)))
	if want := "2026-07-15"; got != want {
		t.Fatalf("unknown field text = %q, want %q (its cached result must still render)", got, want)
	}
}

func TestField_SimplePageAndUnknown(t *testing.T) {
	body := `<w:p>` +
		`<w:fldSimple w:instr=" PAGE "><w:r><w:t>3</w:t></w:r></w:fldSimple>` +
		`<w:fldSimple w:instr=" AUTHOR "><w:r><w:t>Jane</w:t></w:r></w:fldSimple>` +
		`</w:p>`
	runs := paragraphs(t, doc(t, body))[0].Runs
	var page bool
	var text string
	for _, r := range runs {
		if r.Field == document.FieldPage {
			page = true
		}
		text += r.Text
	}
	if !page {
		t.Error("a fldSimple PAGE must become a computed PAGE run")
	}
	if !strings.Contains(text, "Jane") {
		t.Errorf("unknown fldSimple text missing; got %q, want it to contain %q", text, "Jane")
	}
	if strings.Contains(text, "3") {
		t.Errorf("the PAGE fldSimple's cached %q must be replaced by the field, not surfaced", "3")
	}
}

// End-to-end: a PAGE/NUMPAGES footer shows each page's own number and the real
// total, not the stale cached value baked into the .docx.
func TestField_PageNumberRendersPerPage(t *testing.T) {
	footer := docxtest.Footer(`<w:p>` +
		`<w:r><w:t>Page </w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:instrText> PAGE </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:t>1</w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="end"/></w:r>` +
		`<w:r><w:t> of </w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:instrText> NUMPAGES </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:t>1</w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="end"/></w:r>` +
		`</w:p>`)
	// Two pages, forced by an explicit page break; body text carries no digits so
	// every digit in the output comes from the footers.
	body := `<w:p><w:r><w:t>AlphaBody</w:t></w:r></w:p>` +
		`<w:p><w:r><w:br w:type="page"/><w:t>BravoBody</w:t></w:r></w:p>` +
		sectPr(`<w:footerReference w:type="default" r:id="rId11"/>`)
	doc := mustOpenWith(t, body, map[string]string{
		"word/_rels/document.xml.rels": docxtest.Rels(docxtest.Rel("rId11", "footer", "footer1.xml")),
		"word/footer1.xml":             footer,
	})

	var buf bytes.Buffer
	if err := document.ConvertToPdf(doc).Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	text := decodePDFText(buf.Bytes())

	// PAGE resolves to 1 on the first page and 2 on the second; NUMPAGES to 2.
	for _, want := range []string{"Page", "1", "of", "2"} {
		if !strings.Contains(text, want) {
			t.Errorf("PDF text missing %q; got %q", want, text)
		}
	}
	// The "2" proves the field is computed: the .docx cached only "1".
	if strings.Count(text, "2") == 0 {
		t.Error("expected a page number 2 (PAGE on page two and NUMPAGES total); the field was not computed")
	}
}

// doc is a local alias for mustOpen kept short for the table-free field tests.
func doc(t *testing.T, body string) *document.Document {
	t.Helper()
	return mustOpen(t, body)
}
