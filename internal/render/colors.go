package render

import (
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
)

// straightRGBA converts straight (non-premultiplied) 0-255 red, green,
// blue, and alpha into a correctly premultiplied color.RGBA. Go's
// color.RGBA is defined as already alpha-premultiplied, so every component
// must be <= alpha; constructing it directly from straight RGB values with
// alpha < 255 (for example color.RGBA{R: 0xf6, ..., A: 0xf0}, where R
// exceeds A) is an invalid premultiplied color and can blend into badly
// wrong pixels. Any non-opaque color in this package must be built through
// this helper rather than a direct struct literal.
func straightRGBA(r, g, b, a uint8) color.RGBA {
	return color.RGBA{
		R: uint8(uint32(r) * uint32(a) / 0xff),
		G: uint8(uint32(g) * uint32(a) / 0xff),
		B: uint8(uint32(b) * uint32(a) / 0xff),
		A: a,
	}
}

// bandColor is one entry in the standard IEC 60062 resistor color code,
// shared with other families that reuse the same physical marking colors
// (for example a fuse's metal cap or a capacitor's polarity stripe).
type bandColor struct {
	name string
	rgb  color.RGBA
}

// digitColors is the fixed, ordered IEC 60062 digit/multiplier color
// sequence: index N is the color for digit N and for multiplier 10^N.
// Order is significant and must never be produced by ranging over a map.
var digitColors = []bandColor{
	{"black", color.RGBA{R: 0x1a, G: 0x1a, B: 0x1a, A: 0xff}},
	{"brown", color.RGBA{R: 0x6b, G: 0x3a, B: 0x1e, A: 0xff}},
	{"red", color.RGBA{R: 0xd9, G: 0x1f, B: 0x1f, A: 0xff}},
	{"orange", color.RGBA{R: 0xe8, G: 0x7a, B: 0x11, A: 0xff}},
	{"yellow", color.RGBA{R: 0xf0, G: 0xd8, B: 0x10, A: 0xff}},
	{"green", color.RGBA{R: 0x2a, G: 0x8a, B: 0x2a, A: 0xff}},
	{"blue", color.RGBA{R: 0x1f, G: 0x4a, B: 0xd9, A: 0xff}},
	{"violet", color.RGBA{R: 0x7a, G: 0x2a, B: 0xd9, A: 0xff}},
	{"grey", color.RGBA{R: 0x8a, G: 0x8a, B: 0x8a, A: 0xff}},
	{"white", color.RGBA{R: 0xf5, G: 0xf5, B: 0xf5, A: 0xff}},
}

var (
	colorGold   = bandColor{"gold", color.RGBA{R: 0xc8, G: 0x9b, B: 0x3c, A: 0xff}}
	colorSilver = bandColor{"silver", color.RGBA{R: 0xc0, G: 0xc0, B: 0xc0, A: 0xff}}
)

// ToleranceLabel is the accepted render_component_image resistor
// tolerance_label values.
type ToleranceLabel string

const (
	ToleranceLabel005Percent ToleranceLabel = "0.05%"
	ToleranceLabel01Percent  ToleranceLabel = "0.1%"
	ToleranceLabel025Percent ToleranceLabel = "0.25%"
	ToleranceLabel05Percent  ToleranceLabel = "0.5%"
	ToleranceLabel1Percent   ToleranceLabel = "1%"
	ToleranceLabel2Percent   ToleranceLabel = "2%"
	ToleranceLabel5Percent   ToleranceLabel = "5%"
	ToleranceLabel10Percent  ToleranceLabel = "10%"
)

// toleranceColors is the fixed, ordered IEC 60062 tolerance band mapping.
// The 20% "no band" case is intentionally not represented: this package
// always draws one discrete color per band position, so a tolerance with
// no drawn color cannot be expressed in a fixed band_count contract.
var toleranceColors = []struct {
	label ToleranceLabel
	color bandColor
}{
	{ToleranceLabel005Percent, bandColor{"grey", digitColors[8].rgb}},
	{ToleranceLabel01Percent, bandColor{"violet", digitColors[7].rgb}},
	{ToleranceLabel025Percent, bandColor{"blue", digitColors[6].rgb}},
	{ToleranceLabel05Percent, bandColor{"green", digitColors[5].rgb}},
	{ToleranceLabel1Percent, bandColor{"brown", digitColors[1].rgb}},
	{ToleranceLabel2Percent, bandColor{"red", digitColors[2].rgb}},
	{ToleranceLabel5Percent, colorGold},
	{ToleranceLabel10Percent, colorSilver},
}

func toleranceColorByLabel(label string) (bandColor, bool) {
	for _, entry := range toleranceColors {
		if string(entry.label) == label {
			return entry.color, true
		}
	}
	return bandColor{}, false
}

// ToleranceLabels returns the supported resistor tolerance labels in a
// fixed, stable order for use in error messages and documentation.
func ToleranceLabels() []string {
	labels := make([]string, len(toleranceColors))
	for i, entry := range toleranceColors {
		labels[i] = string(entry.label)
	}
	return labels
}

func parseHexColor(s string) (color.RGBA, error) {
	if len(s) != 6 && len(s) != 7 {
		return color.RGBA{}, errors.New("must be a 6-digit hex color, optionally prefixed with #")
	}
	if len(s) == 7 {
		if s[0] != '#' {
			return color.RGBA{}, errors.New("must be a 6-digit hex color, optionally prefixed with #")
		}
		s = s[1:]
	}
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != 3 {
		return color.RGBA{}, fmt.Errorf("must be a 6-digit hex color: %w", err)
	}
	return color.RGBA{R: raw[0], G: raw[1], B: raw[2], A: 0xff}, nil
}
