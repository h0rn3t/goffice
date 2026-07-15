package document

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// --- Theme color tint/shade (w:themeTint / w:themeShade) ---

// applyTintShade lightens (w:themeTint) or darkens (w:themeShade) a resolved
// theme color. Both attributes are a hex byte ("00".."FF") read as a fraction
// of 255 and applied to the color's HSL luminance - the same mapping Word uses
// for its "Lighter 40%" / "Darker 25%" theme variants (which are DrawingML's
// lumMod/lumOff): tint t → L' = L·t + (1-t) (toward white), shade s → L' = L·s
// (toward black). An absent or unparseable attribute leaves hex unchanged; tint
// wins when a color carries both.
func applyTintShade(hex, tint, shade string) string {
	if f, ok := hexFraction(tint); ok {
		return withLuminance(hex, func(l float64) float64 { return l*f + (1 - f) })
	}
	if f, ok := hexFraction(shade); ok {
		return withLuminance(hex, func(l float64) float64 { return l * f })
	}
	return hex
}

// hexFraction parses a ST_UcharHexNumber ("00".."FF") as a 0..1 fraction.
func hexFraction(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(v, 16, 16)
	if err != nil || n > 255 {
		return 0, false
	}
	return float64(n) / 255, true
}

// withLuminance maps hex through f in HSL space, keeping hue and saturation.
// A malformed hex is returned unchanged.
func withLuminance(hex string, f func(float64) float64) string {
	r, g, b, ok := parseHexColor(hex)
	if !ok {
		return hex
	}
	h, s, l := rgbToHSL(r, g, b)
	return formatHexColor(hslToRGB(h, s, clamp01(f(l))))
}

func rgbToHSL(r, g, b int) (h, s, l float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	l = (max + min) / 2
	if max == min {
		return 0, 0, l // achromatic
	}
	d := max - min
	if l > 0.5 {
		s = d / (2 - max - min)
	} else {
		s = d / (max + min)
	}
	switch max {
	case rf:
		h = (gf - bf) / d
		if gf < bf {
			h += 6
		}
	case gf:
		h = (bf-rf)/d + 2
	default:
		h = (rf-gf)/d + 4
	}
	return h / 6, s, l
}

func hslToRGB(h, s, l float64) (r, g, b int) {
	if s == 0 {
		v := int(math.Round(l * 255))
		return v, v, v
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return channel(p, q, h+1.0/3), channel(p, q, h), channel(p, q, h-1.0/3)
}

func channel(p, q, t float64) int {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	var v float64
	switch {
	case t < 1.0/6:
		v = p + (q-p)*6*t
	case t < 1.0/2:
		v = q
	case t < 2.0/3:
		v = p + (q-p)*(2.0/3-t)*6
	default:
		v = p
	}
	return int(math.Round(clamp01(v) * 255))
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

func formatHexColor(r, g, b int) string {
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

// --- Color and shading resolution ---

// resolveThemeHex resolves one color slot of an OOXML element: an explicit sRGB
// hex (val) wins, else the theme reference (themeVal) looked up in the theme map
// and modulated by its tint/shade. "auto"/"nil", an absent or malformed value,
// an unknown theme slot, or an absent theme all resolve to "" (no color
// declared), so a bad value never reaches the renderer as a color.
func resolveThemeHex(val, themeVal, tint, shade string, theme map[string]string) string {
	if hex, ok := sRGBHex(val); ok {
		return hex
	}
	if slot := themeSlot(themeVal); slot != "" {
		if hex, ok := theme[slot]; ok {
			return applyTintShade(hex, tint, shade)
		}
	}
	return ""
}

// sRGBHex validates an OOXML color attribute as six hex digits and returns it as
// "#RRGGBB". "auto", "nil", an absent value, and anything malformed are not
// colors.
func sRGBHex(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if len(v) != 6 {
		return "", false
	}
	if _, err := strconv.ParseUint(v, 16, 32); err != nil {
		return "", false
	}
	return "#" + strings.ToUpper(v), true
}

// resolveShading turns a w:shd (on a run, paragraph, or table cell) into a
// background color "#RRGGBB", or "" for no shading. w:val is the pattern:
// "clear" (and an absent value) paints the plain w:fill; "nil"/"none" paint
// nothing; "solid" paints the foreground w:color; a "pctN" pattern blends the
// foreground over the fill at N%. Named patterns (stripes, grids) are
// approximated by their fill.
func resolveShading(shd *xmlShd, theme map[string]string) string {
	if shd == nil {
		return ""
	}
	fill := resolveThemeHex(shd.Fill, shd.ThemeFill, shd.ThemeFillTint, shd.ThemeFillShade, theme)
	fore := resolveThemeHex(shd.Color, shd.ThemeColor, shd.ThemeTint, shd.ThemeShade, theme)

	switch val := strings.ToLower(strings.TrimSpace(shd.Val)); {
	case val == "nil" || val == "none":
		return ""
	case val == "solid":
		return firstNonEmpty(fore, fill)
	case strings.HasPrefix(val, "pct"):
		if pct, err := strconv.ParseFloat(strings.TrimPrefix(val, "pct"), 64); err == nil && pct > 0 {
			return blendHex(fore, fill, pct/100)
		}
		return fill
	default: // "clear", absent, or a named pattern approximated by its fill
		return fill
	}
}

// blendHex mixes ratio (0..1) of the foreground color over the background one -
// how a w:shd percentage pattern (pct25, pct50, …) reads at a distance. An
// undeclared foreground is black and an undeclared background is white, matching
// OOXML's "auto" defaults for a shading pattern.
func blendHex(fore, back string, ratio float64) string {
	fr, fg, fb, ok := parseHexColor(fore)
	if !ok {
		fr, fg, fb = 0, 0, 0
	}
	br, bg, bb, ok := parseHexColor(back)
	if !ok {
		br, bg, bb = 255, 255, 255
	}
	mix := func(f, b int) int {
		return int(math.Round(float64(f)*ratio + float64(b)*(1-ratio)))
	}
	return formatHexColor(mix(fr, br), mix(fg, bg), mix(fb, bb))
}

// highlightPalette is Word's fixed w:highlight enumeration (ST_HighlightColor);
// "none" and any unknown value resolve to no highlight.
var highlightPalette = map[string]string{
	"black":       "#000000",
	"blue":        "#0000FF",
	"cyan":        "#00FFFF",
	"darkblue":    "#000080",
	"darkcyan":    "#008080",
	"darkgray":    "#808080",
	"darkgreen":   "#008000",
	"darkmagenta": "#800080",
	"darkred":     "#800000",
	"darkyellow":  "#808000",
	"green":       "#00FF00",
	"lightgray":   "#C0C0C0",
	"magenta":     "#FF00FF",
	"red":         "#FF0000",
	"white":       "#FFFFFF",
	"yellow":      "#FFFF00",
}

// highlightHex resolves a w:highlight into its palette color, or "" for "none"
// and unknown values.
func highlightHex(v *xmlVal) string {
	if v == nil {
		return ""
	}
	return highlightPalette[strings.ToLower(strings.TrimSpace(v.Val))]
}
