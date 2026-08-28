package render

import (
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
)

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

// toleranceColors is the fixed, ordered IEC 60062 tolerance band mapping.
// The 20% "no band" case is intentionally not represented: this package
// always draws one discrete color per band position, so a tolerance with
// no drawn color cannot be expressed in a fixed band_count contract.
var toleranceColors = []struct {
	label string
	color bandColor
}{
	{"0.05%", bandColor{"grey", digitColors[8].rgb}},
	{"0.1%", bandColor{"violet", digitColors[7].rgb}},
	{"0.25%", bandColor{"blue", digitColors[6].rgb}},
	{"0.5%", bandColor{"green", digitColors[5].rgb}},
	{"1%", bandColor{"brown", digitColors[1].rgb}},
	{"2%", bandColor{"red", digitColors[2].rgb}},
	{"5%", colorGold},
	{"10%", colorSilver},
}

func toleranceColorByLabel(label string) (bandColor, bool) {
	for _, entry := range toleranceColors {
		if entry.label == label {
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
		labels[i] = entry.label
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
