package document

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	_ "image/gif"  // registers GIF for DecodeConfig (intrinsic image size)
	_ "image/jpeg" // registers JPEG for DecodeConfig
	_ "image/png"  // registers PNG for DecodeConfig
	"io"
	"path"
	"strconv"
	"strings"
)

// --- Package parts, relationships (word/_rels/*.rels) and media ---

type xmlRelationships struct {
	Rels []xmlRelationship `xml:"http://schemas.openxmlformats.org/package/2006/relationships Relationship"`
}

// xmlRelationship is one <Relationship> (its attributes are unqualified).
type xmlRelationship struct {
	ID         string `xml:"Id,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr"`
}

// pkg is the opened .docx: every part by name, plus a cache of already-read
// media blobs so an image used twice is read (and later embedded) once.
type pkg struct {
	parts map[string]*zip.File
	media map[string][]byte
}

func newPkg(files []*zip.File) *pkg {
	parts := make(map[string]*zip.File, len(files))
	for _, f := range files {
		parts[f.Name] = f
	}
	return &pkg{parts: parts, media: make(map[string][]byte)}
}

// part returns the named part's decoded bytes, or ok=false when it is absent or
// unreadable. Optional parts degrade rather than failing Open.
func (p *pkg) part(name string) ([]byte, bool) {
	f, ok := p.parts[name]
	if !ok {
		return nil, false
	}
	rc, err := f.Open()
	if err != nil {
		return nil, false
	}
	defer func() { _ = rc.Close() }()

	b, err := io.ReadAll(rc)
	if err != nil {
		return nil, false
	}
	return b, true
}

// rels reads the relationship part belonging to partName (e.g.
// word/_rels/document.xml.rels for word/document.xml) and returns rId → part
// name, with each Target resolved against the owning part's directory.
// External-mode relationships (TargetMode="External") are skipped: they point
// outside the package, and linked-not-embedded media is out of scope. A missing
// or malformed .rels degrades to an empty map.
func (p *pkg) rels(partName string) map[string]string {
	dir := path.Dir(partName)
	data, ok := p.part(path.Join(dir, "_rels", path.Base(partName)+".rels"))
	if !ok {
		return nil
	}
	var xr xmlRelationships
	if xml.Unmarshal(data, &xr) != nil {
		return nil
	}
	out := make(map[string]string, len(xr.Rels))
	for _, rel := range xr.Rels {
		if rel.ID == "" || rel.Target == "" || strings.EqualFold(rel.TargetMode, "External") {
			continue
		}
		out[rel.ID] = path.Clean(path.Join(dir, rel.Target))
	}
	return out
}

// imageResolver turns a relationship id (r:embed on a DrawingML blip, r:id on a
// VML imagedata) used inside one part into an Image carrying the media bytes.
// Each part gets its own resolver because ids are part-scoped: rId1 in
// word/numbering.xml and rId1 in word/document.xml are unrelated.
type imageResolver struct {
	pkg  *pkg
	rels map[string]string
}

func (p *pkg) resolverFor(partName string) *imageResolver {
	return &imageResolver{pkg: p, rels: p.rels(partName)}
}

// resolve loads the image referenced by rID at the requested size. A zero or
// missing size falls back to the picture's intrinsic pixel size at 96 dpi.
// It returns nil - and the caller drops the picture, keeping the conversion
// successful - when the id is unknown, the media part is missing, or its format
// is one the PDF backend cannot embed (EMF/WMF/SVG/TIFF).
func (ir *imageResolver) resolve(rID string, widthPt, heightPt float64) *Image {
	if ir == nil || rID == "" {
		return nil
	}
	name := ir.rels[rID]
	if name == "" {
		return nil
	}
	typ := imageType(name)
	if typ == "" {
		return nil
	}
	data, cached := ir.pkg.media[name]
	if !cached {
		var ok bool
		if data, ok = ir.pkg.part(name); !ok {
			return nil
		}
		ir.pkg.media[name] = data
	}
	if widthPt <= 0 || heightPt <= 0 {
		if widthPt, heightPt = intrinsicSizePt(data); widthPt <= 0 || heightPt <= 0 {
			return nil
		}
	}
	return &Image{Name: name, Type: typ, Data: data, WidthPt: widthPt, HeightPt: heightPt}
}

// imageType maps a media part's extension onto the fpdf image type, or "" for a
// format the backend cannot embed.
func imageType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "PNG"
	case ".jpg", ".jpeg":
		return "JPG"
	case ".gif":
		return "GIF"
	default: // .emf/.wmf/.svg/.tif: no fpdf support
		return ""
	}
}

// intrinsicSizePt reads an image's pixel dimensions from its header and
// converts them to points at 96 dpi (Word's own screen-pixel assumption),
// used when a picture declares no explicit extent.
func intrinsicSizePt(data []byte) (float64, float64) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return float64(cfg.Width) * 72 / 96, float64(cfg.Height) * 72 / 96
}

// emuToPt converts English Metric Units (the unit wp:extent uses) to points:
// 914400 EMU per inch, 72 pt per inch.
func emuToPt(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v / 12700
}

// --- VML (w:pict): legacy pictures and picture bullets ---
//
// A picture bullet (w:numPicBullet) is always VML, even in files Word writes
// today, so the shape's size comes from its CSS-ish style attribute rather than
// a wp:extent.

type xmlPict struct {
	Shape *xmlVShape `xml:"urn:schemas-microsoft-com:vml shape"`
}

type xmlVShape struct {
	Style     string        `xml:"style,attr"`
	ImageData *xmlImageData `xml:"urn:schemas-microsoft-com:vml imagedata"`
}

type xmlImageData struct {
	ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
}

// image resolves a VML shape into an Image sized from its style attribute
// (e.g. style="width:11.25pt;height:11.25pt"); an absent or unparseable size
// falls back to the picture's intrinsic size.
func (p *xmlPict) image(ir *imageResolver) *Image {
	if p == nil || p.Shape == nil || p.Shape.ImageData == nil {
		return nil
	}
	style := parseVMLStyle(p.Shape.Style)
	return ir.resolve(p.Shape.ImageData.ID, cssLengthPt(style["width"]), cssLengthPt(style["height"]))
}

// parseVMLStyle splits a VML style attribute ("width:9pt;height:9pt") into its
// lower-cased property→value pairs.
func parseVMLStyle(style string) map[string]string {
	out := make(map[string]string)
	for _, decl := range strings.Split(style, ";") {
		k, v, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = strings.ToLower(strings.TrimSpace(v))
	}
	return out
}

// cssLengthPt converts a VML/CSS length ("11.25pt", "15px", "0.25in") to
// points; a unitless or unknown-unit value yields 0, so the caller falls back to
// the image's intrinsic size.
func cssLengthPt(v string) float64 {
	for unit, perUnit := range map[string]float64{
		"pt": 1,
		"px": 72.0 / 96,
		"in": 72,
		"pc": 12,
		"mm": 72 / 25.4,
		"cm": 72 / 2.54,
	} {
		if num, ok := strings.CutSuffix(v, unit); ok {
			f, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
			if err != nil || f <= 0 {
				return 0
			}
			return f * perUnit
		}
	}
	return 0
}

// --- DrawingML (w:drawing): inline and floating pictures ---

// xmlDrawing is a run's <w:drawing>: an inline picture (wp:inline) or a
// floating one (wp:anchor). Floating pictures are laid out inline too - text
// wrapping and absolute positioning are out of scope - so both shapes decode
// into the same struct.
type xmlDrawing struct {
	Inline []xmlAnchor `xml:"http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing inline"`
	Anchor []xmlAnchor `xml:"http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing anchor"`
}

type xmlAnchor struct {
	Extent  xmlExtent  `xml:"http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing extent"`
	Graphic xmlGraphic `xml:"http://schemas.openxmlformats.org/drawingml/2006/main graphic"`
}

// xmlExtent is <wp:extent>'s picture size, in EMU (unqualified attributes).
type xmlExtent struct {
	Cx string `xml:"cx,attr"`
	Cy string `xml:"cy,attr"`
}

type xmlGraphic struct {
	Data xmlGraphicData `xml:"http://schemas.openxmlformats.org/drawingml/2006/main graphicData"`
}

// xmlGraphicData holds the graphic's payload; only a picture (pic:pic) is
// modeled, so charts, diagrams and shapes decode to a nil Pic and are skipped.
type xmlGraphicData struct {
	Pic *xmlPic `xml:"http://schemas.openxmlformats.org/drawingml/2006/picture pic"`
}

type xmlPic struct {
	BlipFill xmlBlipFill `xml:"http://schemas.openxmlformats.org/drawingml/2006/picture blipFill"`
}

type xmlBlipFill struct {
	Blip *xmlBlip `xml:"http://schemas.openxmlformats.org/drawingml/2006/main blip"`
}

// xmlBlip is <a:blip>, whose r:embed names the media relationship.
type xmlBlip struct {
	Embed string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships embed,attr"`
}

// image resolves a w:drawing into an Image at the size its wp:extent declares,
// or nil when it carries no embedded picture (a chart, a shape, a linked image).
func (d *xmlDrawing) image(ir *imageResolver) *Image {
	if d == nil {
		return nil
	}
	for _, a := range append(append([]xmlAnchor{}, d.Inline...), d.Anchor...) {
		pic := a.Graphic.Data.Pic
		if pic == nil || pic.BlipFill.Blip == nil {
			continue
		}
		if img := ir.resolve(pic.BlipFill.Blip.Embed, emuToPt(a.Extent.Cx), emuToPt(a.Extent.Cy)); img != nil {
			return img
		}
	}
	return nil
}
