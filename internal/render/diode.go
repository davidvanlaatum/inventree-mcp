package render

import (
	"errors"
	"fmt"
	"image/color"
	"math"

	"github.com/fogleman/gg"
)

// DiodeParams describes an axial through-hole diode: a body with a single
// contrasting cathode band near one end.
type DiodeParams struct {
	// BodyColorHex defaults to black when empty.
	BodyColorHex string
	// CathodeBandColorHex defaults to white when empty. It must differ
	// from the resolved body color, or the band would be invisible.
	CathodeBandColorHex string
	// CathodeSide is "left" (default) or "right".
	CathodeSide string
	// Markings is optional body text, for example a part number.
	Markings                     string
	Size                         string
	BodyLengthMM, BodyDiameterMM float64
}

var (
	diodeDefaultBodyColor = color.RGBA{R: 0x18, G: 0x18, B: 0x18, A: 0xff}
	diodeDefaultBandColor = color.RGBA{R: 0xf0, G: 0xf0, B: 0xf0, A: 0xff}
	diodeLeadColor        = color.RGBA{R: 0xb8, G: 0xb8, B: 0xb8, A: 0xff}
)

// RenderDiode renders an axial diode with a single cathode band.
func RenderDiode(canvas CanvasOptions, params DiodeParams) (Result, error) {
	bodyColor, err := validateHexOrDefault(params.BodyColorHex, diodeDefaultBodyColor)
	if err != nil {
		return Result{}, err
	}
	bandColorRGBA, err := validateHexOrDefault(params.CathodeBandColorHex, diodeDefaultBandColor)
	if err != nil {
		return Result{}, err
	}
	if bodyColor == bandColorRGBA {
		return Result{}, errors.New("diode cathode_band_color_hex must differ from body_color_hex")
	}
	side := params.CathodeSide
	if side == "" {
		side = "left"
	}
	if side != "left" && side != "right" {
		return Result{}, fmt.Errorf("diode cathode_side must be one of %v", Sides())
	}
	if err := validateMarkings(params.Markings); err != nil {
		return Result{}, err
	}
	size, err := resolveBodySize(params.Size)
	if err != nil {
		return Result{}, err
	}
	aspect, err := resolveAspect(params.BodyLengthMM, params.BodyDiameterMM, 2.4)
	if err != nil {
		return Result{}, err
	}

	return renderCanvas(canvas, func(dc *gg.Context, w, h int) error {
		drawAxialDiodeGlyph(dc, w, h, size, aspect, bodyColor, bandColorRGBA, side, params.Markings)
		return nil
	})
}

func drawAxialDiodeGlyph(dc *gg.Context, w, h int, sizeFrac, aspect float64, bodyColor, bandColor color.RGBA, cathodeSide, markings string) {
	fw, fh := float64(w), float64(h)
	bodyHeight := fh * sizeFrac
	bodyWidth := math.Min(bodyHeight*aspect, fw*0.85)
	bodyWidth = math.Max(bodyWidth, fw*0.2)

	cx, cy := fw/2, fh/2
	bx := cx - bodyWidth/2
	by := cy - bodyHeight/2
	radius := bodyHeight * 0.25

	leadWidth := math.Max(2, fh/40)

	dc.SetColor(diodeLeadColor)
	dc.SetLineWidth(leadWidth)
	dc.DrawLine(0, cy, bx, cy)
	dc.DrawLine(bx+bodyWidth, cy, fw, cy)
	dc.Stroke()

	dc.SetColor(bodyColor)
	dc.DrawRoundedRectangle(bx, by, bodyWidth, bodyHeight, radius)
	dc.Fill()

	bandWidth := bodyWidth * 0.12
	var bandX, freeZoneStart, freeZoneEnd float64
	margin := bodyWidth * 0.04
	if cathodeSide == "left" {
		bandX = bx + bodyWidth*0.12
		freeZoneStart = bandX + bandWidth + margin
		freeZoneEnd = bx + bodyWidth - margin
	} else {
		bandX = bx + bodyWidth*0.88 - bandWidth
		freeZoneStart = bx + margin
		freeZoneEnd = bandX - margin
	}
	dc.SetColor(bandColor)
	dc.DrawRectangle(bandX, by, bandWidth, bodyHeight)
	dc.Fill()

	if markings != "" {
		textColor := contrastingTextColor(bodyColor)
		textCX := (freeZoneStart + freeZoneEnd) / 2
		points := fitFontSize(markings, (freeZoneEnd-freeZoneStart)*0.92, bodyHeight*0.6, false)
		drawCenteredText(dc, markings, textCX, cy, points, false, textColor)
	}
}

// contrastingTextColor returns black or white, whichever reads better
// against bg, using relative luminance.
func contrastingTextColor(bg color.RGBA) color.RGBA {
	luminance := 0.299*float64(bg.R) + 0.587*float64(bg.G) + 0.114*float64(bg.B)
	if luminance > 140 {
		return color.RGBA{A: 0xff}
	}
	return color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
}
