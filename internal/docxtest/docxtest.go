// Package docxtest builds minimal .docx fixtures on disk for tests.
//
// Fixtures are generated (not committed as binary blobs) so every byte a test
// depends on is visible in the test source, and the WordprocessingML namespace
// declarations exercise the same prefix→URI resolution real Word files use.
package docxtest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

// Document wraps bodyXML (the inner content of <w:body>) into a full
// word/document.xml with the WordprocessingML namespace declared.
func Document(bodyXML string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>` + bodyXML + `</w:body>
</w:document>`
}

// Build writes a valid .docx package to a temp file whose word/document.xml
// contains bodyXML (the inner content of <w:body>) and returns its path.
func Build(t *testing.T, bodyXML string) string {
	t.Helper()
	return writeZip(t, "fixture.docx", map[string]string{
		"[Content_Types].xml": contentTypes,
		"_rels/.rels":         rootRels,
		"word/document.xml":   Document(bodyXML),
	})
}

// BuildParts writes a .docx-like ZIP with exactly the given parts (used to
// build packages that deliberately omit word/document.xml).
func BuildParts(t *testing.T, parts map[string]string) string {
	t.Helper()
	return writeZip(t, "fixture.docx", parts)
}

// BuildWithStyles is Build plus a word/styles.xml part wrapping
// stylesInnerXML (the inner content of <w:styles>) with the WordprocessingML
// namespace declared.
func BuildWithStyles(t *testing.T, bodyXML, stylesInnerXML string) string {
	t.Helper()
	styles := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		stylesInnerXML + `</w:styles>`
	return writeZip(t, "fixture.docx", map[string]string{
		"[Content_Types].xml": contentTypes,
		"_rels/.rels":         rootRels,
		"word/document.xml":   Document(bodyXML),
		"word/styles.xml":     styles,
	})
}

// BuildWith writes a valid .docx (the base parts plus word/document.xml from
// bodyXML) together with any extra parts given - e.g. word/styles.xml,
// word/theme/theme1.xml, word/numbering.xml. Extras with an empty value are
// skipped, so a test can inject exactly the optional parts it needs.
func BuildWith(t *testing.T, bodyXML string, extra map[string]string) string {
	t.Helper()
	parts := map[string]string{
		"[Content_Types].xml": contentTypes,
		"_rels/.rels":         rootRels,
		"word/document.xml":   Document(bodyXML),
	}
	for name, content := range extra {
		if content != "" {
			parts[name] = content
		}
	}
	return writeZip(t, "fixture.docx", parts)
}

// Styles wraps stylesInnerXML (the inner content of <w:styles>) into a full
// word/styles.xml part with the WordprocessingML namespace declared.
func Styles(stylesInnerXML string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		stylesInnerXML + `</w:styles>`
}

// Numbering wraps numberingInnerXML (the inner content of <w:numbering>, i.e.
// its w:abstractNum and w:num children) into a full word/numbering.xml part.
func Numbering(numberingInnerXML string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		numberingInnerXML + `</w:numbering>`
}

// Theme wraps clrSchemeInnerXML (the inner content of <a:clrScheme>, i.e. its
// dk1/lt1/…/folHlink slots) into a full word/theme/theme1.xml DrawingML part.
func Theme(clrSchemeInnerXML string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Test">
  <a:themeElements>
    <a:clrScheme name="Test">` + clrSchemeInnerXML + `</a:clrScheme>
  </a:themeElements>
</a:theme>`
}

// Rels wraps relationship entries (the <Relationship> children) into a full
// .rels part, e.g. for word/_rels/document.xml.rels.
func Rels(relationshipsXML string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		relationshipsXML + `</Relationships>`
}

// Rel builds one <Relationship> of the given officeDocument type ("image",
// "header", "footer", …) pointing at target (relative to the owning part).
func Rel(id, typ, target string) string {
	return `<Relationship Id="` + id +
		`" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/` + typ +
		`" Target="` + target + `"/>`
}

// Header wraps p/tbl children into a full word/headerN.xml part; Footer does the
// same for word/footerN.xml.
func Header(innerXML string) string { return hdrFtr("hdr", innerXML) }

// Footer wraps p/tbl children into a full word/footerN.xml part.
func Footer(innerXML string) string { return hdrFtr("ftr", innerXML) }

func hdrFtr(root, innerXML string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:` + root + ` xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		innerXML + `</w:` + root + `>`
}

// PNG returns the bytes of a valid w×h PNG, as a string so it can be dropped
// straight into a part map alongside the XML parts.
func PNG(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}
	return buf.String()
}

// Corrupt writes a file that is not a ZIP archive and returns its path.
func Corrupt(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corrupt.docx")
	if err := os.WriteFile(path, []byte("this is not a zip archive"), 0o600); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}
	return path
}

func writeZip(t *testing.T, name string, parts map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	for partName, content := range parts {
		w, err := zw.Create(partName)
		if err != nil {
			t.Fatalf("add part %q: %v", partName, err)
		}
		if _, err := fmt.Fprint(w, content); err != nil {
			t.Fatalf("write part %q: %v", partName, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	return path
}
