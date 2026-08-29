package render

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"github.com/fogleman/gg"
)

// LEDParams describes a through-hole LED front view: a dome-shaped lens on
// a small flange, with a shorter cathode lead and a longer anode lead.
type LEDParams struct {
	// LensColor must be one of LEDLensColors().
	LensColor string
	// Diffused draws a matte lens (flat fill) when true, or a clear/
	// water-clear lens (glossy specular highlight) when false.
	Diffused bool
	// CathodeSide is "left" (default) or "right".
	CathodeSide string
	// Size is "3mm", "5mm" (default), or "10mm".
	Size string
	// ShowLabel draws a caption below the LED with its size and lens
	// color, and whether the lens is diffused or clear. Every value in
	// the caption is already present in the request; nothing about
	// brightness, forward voltage, or viewing angle is invented.
	ShowLabel bool
}

// LEDLensColor is the accepted render_component_image LED lens_color
// values.
type LEDLensColor string

const (
	LEDLensColorRed    LEDLensColor = "red"
	LEDLensColorGreen  LEDLensColor = "green"
	LEDLensColorBlue   LEDLensColor = "blue"
	LEDLensColorYellow LEDLensColor = "yellow"
	LEDLensColorOrange LEDLensColor = "orange"
	LEDLensColorWhite  LEDLensColor = "white"
	LEDLensColorClear  LEDLensColor = "clear"
)

type ledLensColor struct {
	name LEDLensColor
	fill color.RGBA
}

// ledLensColors is fixed and ordered; do not derive from a map range.
var ledLensColors = []ledLensColor{
	{LEDLensColorRed, color.RGBA{R: 0xd9, G: 0x1f, B: 0x1f, A: 0xff}},
	{LEDLensColorGreen, color.RGBA{R: 0x2a, G: 0x8a, B: 0x2a, A: 0xff}},
	{LEDLensColorBlue, color.RGBA{R: 0x1f, G: 0x4a, B: 0xd9, A: 0xff}},
	{LEDLensColorYellow, color.RGBA{R: 0xf0, G: 0xd8, B: 0x10, A: 0xff}},
	{LEDLensColorOrange, color.RGBA{R: 0xe8, G: 0x7a, B: 0x11, A: 0xff}},
	{LEDLensColorWhite, color.RGBA{R: 0xf5, G: 0xf5, B: 0xf5, A: 0xff}},
	{LEDLensColorClear, color.RGBA{R: 0xd8, G: 0xe8, B: 0xf0, A: 0xff}},
}

// LEDLensColors returns the supported LED lens color names in a fixed,
// stable order.
func LEDLensColors() []string {
	names := make([]string, len(ledLensColors))
	for i, c := range ledLensColors {
		names[i] = string(c.name)
	}
	return names
}

func ledLensColorByName(name string) (color.RGBA, bool) {
	for _, c := range ledLensColors {
		if string(c.name) == name {
			return c.fill, true
		}
	}
	return color.RGBA{}, false
}

var (
	ledLeadColor    = color.RGBA{R: 0xb8, G: 0xb8, B: 0xb8, A: 0xff}
	ledPolarityMark = color.RGBA{A: 0xff}
)

// LEDSize is the accepted render_component_image LED size values.
type LEDSize string

const (
	LEDSize3mm  LEDSize = "3mm"
	LEDSize5mm  LEDSize = "5mm"
	LEDSize10mm LEDSize = "10mm"
)

var ledSizeFractions = map[string]float64{
	string(LEDSize3mm):  0.3,
	string(LEDSize5mm):  0.42,
	string(LEDSize10mm): 0.6,
}

// ledSizeOrder is the fixed, stable order of ledSizeFractions' supported
// values, for use in error messages, documentation, and JSON Schema enums.
var ledSizeOrder = []LEDSize{LEDSize3mm, LEDSize5mm, LEDSize10mm}

// LEDSizes returns the supported LED size preset values in a fixed, stable
// order.
func LEDSizes() []string {
	values := make([]string, len(ledSizeOrder))
	for i, s := range ledSizeOrder {
		values[i] = string(s)
	}
	return values
}

// RenderLED renders a through-hole LED front view.
func RenderLED(canvas CanvasOptions, params LEDParams) (Result, error) {
	lens, ok := ledLensColorByName(params.LensColor)
	if !ok {
		return Result{}, fmt.Errorf("led lens_color must be one of %v", LEDLensColors())
	}
	side := params.CathodeSide
	if side == "" {
		side = "left"
	}
	if side != "left" && side != "right" {
		return Result{}, fmt.Errorf("led cathode_side must be one of %v", Sides())
	}
	size := params.Size
	if size == "" {
		size = "5mm"
	}
	domeFrac, ok := ledSizeFractions[size]
	if !ok {
		return Result{}, fmt.Errorf("led size must be one of %v", LEDSizes())
	}

	var captionLines []string
	if params.ShowLabel {
		lensState := "Clear"
		if params.Diffused {
			lensState = "Diffused"
		}
		captionLines = []string{
			strings.ToUpper(size[:1]) + size[1:] + " " + strings.ToUpper(params.LensColor[:1]) + params.LensColor[1:] + " LED",
			lensState + " lens",
		}
	}

	return renderCanvas(canvas, func(dc *gg.Context, w, h int) error {
		drawLEDGlyph(dc, w, h, domeFrac, lens, params.Diffused, side, captionLines)
		return nil
	})
}

// drawLEDGlyph draws a single-silhouette side view: a domed top and a
// straight-sided epoxy body as one continuous outline (not a round dome
// sitting on a separately shaped base, which reads as two different
// viewing angles mashed together). The cathode side gets a small chamfered
// corner at the base, the common simplified-icon convention for an LED's
// flat polarity indicator.
func drawLEDGlyph(dc *gg.Context, w, h int, domeFrac float64, lens color.RGBA, diffused bool, cathodeSide string, captionLines []string) {
	fw, fh := float64(w), float64(h)
	top, bottom := verticalLayoutBand(fh, false, len(captionLines))
	componentAreaHeight := bottom - top
	domeDiameter := math.Min(fw, componentAreaHeight) * domeFrac
	domeRadius := domeDiameter / 2
	cx := fw / 2
	cylHeight := domeRadius * 1.1
	yCylTop := top + componentAreaHeight*0.30 + domeRadius*0.1
	yBottom := yCylTop + cylHeight
	x0, x1 := cx-domeRadius, cx+domeRadius
	chamfer := domeRadius * 0.4

	dc.NewSubPath()
	if cathodeSide == "left" {
		dc.MoveTo(x0+chamfer, yBottom)
		dc.LineTo(x1, yBottom)
		dc.LineTo(x1, yCylTop)
		dc.DrawArc(cx, yCylTop, domeRadius, 0, -math.Pi)
		dc.LineTo(x0, yBottom-chamfer)
		dc.LineTo(x0+chamfer, yBottom)
	} else {
		dc.MoveTo(x1-chamfer, yBottom)
		dc.LineTo(x0, yBottom)
		dc.LineTo(x0, yCylTop)
		dc.DrawArc(cx, yCylTop, domeRadius, -math.Pi, 0)
		dc.LineTo(x1, yBottom-chamfer)
		dc.LineTo(x1-chamfer, yBottom)
	}
	dc.ClosePath()

	// Self-shading radial gradient (light source upper-left) gives the
	// body a rounded look instead of a single flat fill color, for both
	// diffused and clear lenses.
	domeCenterY := yCylTop
	lightX, lightY := cx-domeRadius*0.4, domeCenterY-domeRadius*0.4
	rimShade := mixColor(lens, shadeBlack, 0.35)
	bodyGradient := gg.NewRadialGradient(lightX, lightY, 0, cx, domeCenterY, domeRadius*1.6)
	bodyGradient.AddColorStop(0, mixColor(lens, shadeWhite, 0.3))
	bodyGradient.AddColorStop(0.45, lens)
	bodyGradient.AddColorStop(1, rimShade)
	dc.SetFillStyle(bodyGradient)
	dc.Fill()

	if !diffused {
		// A soft specular highlight reads as glossy plastic rather than a
		// flat fill. Every stop stays fully opaque and fades into the
		// body gradient's own light tone at the edge (rather than to a
		// transparent stop): gg's gradient color interpolation produces
		// visible artifacts when a stop's alpha differs from its
		// neighbors, so the fade is achieved through color alone.
		hx, hy := cx-domeRadius*0.32, domeCenterY-domeRadius*0.38
		highlightGradient := gg.NewRadialGradient(hx, hy, 0, hx, hy, domeRadius*0.34)
		highlightGradient.AddColorStop(0, shadeWhite)
		highlightGradient.AddColorStop(1, mixColor(lens, shadeWhite, 0.3))
		dc.SetFillStyle(highlightGradient)
		dc.DrawCircle(hx, hy, domeRadius*0.34)
		dc.Fill()
	}

	leadGap := domeDiameter * 0.36
	anodeX := cx + leadGap/2
	cathodeX := cx - leadGap/2
	if cathodeSide == "right" {
		anodeX, cathodeX = cathodeX, anodeX
	}
	leadWidth := math.Max(2, componentAreaHeight/60)
	dc.SetColor(ledLeadColor)
	dc.SetLineWidth(leadWidth)
	dc.DrawLine(anodeX, yBottom, anodeX, bottom)
	dc.DrawLine(cathodeX, yBottom, cathodeX, yBottom+(bottom-yBottom)*0.82)
	dc.Stroke()

	// "+" beside the anode lead is a second, unambiguous polarity cue
	// alongside the chamfered cathode corner.
	plusX := anodeX + leadGap*0.55
	if cathodeSide == "right" {
		plusX = anodeX - leadGap*0.55
	}
	markPoints := fitFontSize("+", leadGap*0.7, (bottom-yBottom)*0.5, true)
	drawCenteredText(dc, "+", plusX, yBottom+(bottom-yBottom)*0.25, markPoints, true, ledPolarityMark)

	drawCaptionLines(dc, captionLines, cx, bottom, fh, fw)
}
