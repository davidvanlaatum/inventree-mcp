package render

import (
	"errors"
	"fmt"
	"image/color"
	"math"

	"github.com/fogleman/gg"
	"golang.org/x/image/font/basicfont"
)

// bodySizeFractions maps a package-variant size preset to the fraction of
// the canvas height the component body occupies. These are illustrative
// layout presets only, not a claim of physical scale; BodyLengthMM/
// BodyDiameterMM (or family-specific equivalents) are the only inputs that
// influence drawn proportions to an explicitly supplied dimension.
var bodySizeFractions = map[string]float64{
	"small":  0.35,
	"medium": 0.5,
	"large":  0.68,
}

func resolveBodySize(size string) (float64, error) {
	if size == "" {
		size = "medium"
	}
	frac, ok := bodySizeFractions[size]
	if !ok {
		return 0, errors.New("size must be \"small\", \"medium\", or \"large\"")
	}
	return frac, nil
}

// resolveAspect returns the drawn length:diameter aspect ratio for an axial
// body. When both dimensions are supplied it uses their ratio; otherwise it
// returns defaultAspect. Supplying exactly one of the two is a validation
// error rather than a guessed default for the other.
func resolveAspect(lengthMM, diameterMM, defaultAspect float64) (float64, error) {
	hasLength := lengthMM != 0
	hasDiameter := diameterMM != 0
	switch {
	case !hasLength && !hasDiameter:
		return defaultAspect, nil
	case hasLength != hasDiameter:
		return 0, errors.New("body length and diameter dimensions must be supplied together")
	}
	if lengthMM <= 0 || diameterMM <= 0 || math.IsInf(lengthMM, 0) || math.IsInf(diameterMM, 0) || math.IsNaN(lengthMM) || math.IsNaN(diameterMM) {
		return 0, errors.New("body length and diameter dimensions must be positive finite values")
	}
	return lengthMM / diameterMM, nil
}

// validateHexOrDefault validates an optional caller-supplied hex color,
// returning fallback when hex is empty.
func validateHexOrDefault(hex string, fallback color.RGBA) (color.RGBA, error) {
	if hex == "" {
		return fallback, nil
	}
	c, err := parseHexColor(hex)
	if err != nil {
		return color.RGBA{}, err
	}
	return c, nil
}

const maxMarkingsLen = 16

// validateMarkings bounds optional caller-supplied marking text to a small,
// printable ASCII set so basicfont's fixed glyph coverage and the drawn
// text bounds stay predictable.
func validateMarkings(text string) error {
	if len(text) > maxMarkingsLen {
		return fmt.Errorf("markings text must be %d characters or fewer", maxMarkingsLen)
	}
	for _, r := range text {
		if r < 0x20 || r > 0x7e {
			return errors.New("markings text must contain only printable ASCII characters")
		}
	}
	return nil
}

// drawCenteredMarkings draws text using a fixed bitmap font (no external
// font asset) centered at (cx, cy), scaled up to remain legible at small
// canvas sizes.
func drawCenteredMarkings(dc *gg.Context, text string, cx, cy float64, textColor color.RGBA, scale int) {
	if text == "" {
		return
	}
	if scale < 1 {
		scale = 1
	}
	face := basicfont.Face7x13
	glyphW := 7 * scale
	width := glyphW * len(text)
	height := 13 * scale

	layer := gg.NewContext(width, height)
	layer.SetFontFace(face)
	layer.SetColor(textColor)
	if scale == 1 {
		layer.DrawString(text, 0, float64(height)-3)
	} else {
		layer.Scale(float64(scale), float64(scale))
		layer.DrawString(text, 0, 13-3)
	}

	dc.DrawImageAnchored(layer.Image(), int(math.Round(cx)), int(math.Round(cy)), 0.5, 0.5)
}
