package render

import (
	"fmt"
	"image/color"
	"math"

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
}

type ledLensColor struct {
	name string
	fill color.RGBA
}

// ledLensColors is fixed and ordered; do not derive from a map range.
var ledLensColors = []ledLensColor{
	{"red", color.RGBA{R: 0xd9, G: 0x1f, B: 0x1f, A: 0xff}},
	{"green", color.RGBA{R: 0x2a, G: 0x8a, B: 0x2a, A: 0xff}},
	{"blue", color.RGBA{R: 0x1f, G: 0x4a, B: 0xd9, A: 0xff}},
	{"yellow", color.RGBA{R: 0xf0, G: 0xd8, B: 0x10, A: 0xff}},
	{"orange", color.RGBA{R: 0xe8, G: 0x7a, B: 0x11, A: 0xff}},
	{"white", color.RGBA{R: 0xf5, G: 0xf5, B: 0xf5, A: 0xff}},
	{"clear", color.RGBA{R: 0xd8, G: 0xe8, B: 0xf0, A: 0xff}},
}

// LEDLensColors returns the supported LED lens color names in a fixed,
// stable order.
func LEDLensColors() []string {
	names := make([]string, len(ledLensColors))
	for i, c := range ledLensColors {
		names[i] = c.name
	}
	return names
}

func ledLensColorByName(name string) (color.RGBA, bool) {
	for _, c := range ledLensColors {
		if c.name == name {
			return c.fill, true
		}
	}
	return color.RGBA{}, false
}

var (
	ledFlangeColor   = color.RGBA{R: 0x2a, G: 0x2a, B: 0x2a, A: 0xff}
	ledCathodeMark   = color.RGBA{R: 0x0, G: 0x0, B: 0x0, A: 0xff}
	ledLeadColor     = color.RGBA{R: 0xb8, G: 0xb8, B: 0xb8, A: 0xff}
	ledHighlightFill = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x90}
)

var ledSizeFractions = map[string]float64{
	"3mm":  0.3,
	"5mm":  0.42,
	"10mm": 0.6,
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
		return Result{}, fmt.Errorf("led cathode_side must be \"left\" or \"right\"")
	}
	size := params.Size
	if size == "" {
		size = "5mm"
	}
	domeFrac, ok := ledSizeFractions[size]
	if !ok {
		return Result{}, fmt.Errorf("led size must be \"3mm\", \"5mm\", or \"10mm\"")
	}

	return renderCanvas(canvas, func(dc *gg.Context, w, h int) error {
		drawLEDGlyph(dc, w, h, domeFrac, lens, params.Diffused, side)
		return nil
	})
}

func drawLEDGlyph(dc *gg.Context, w, h int, domeFrac float64, lens color.RGBA, diffused bool, cathodeSide string) {
	fw, fh := float64(w), float64(h)
	domeDiameter := math.Min(fw, fh) * domeFrac
	domeRadius := domeDiameter / 2
	cx := fw / 2
	flangeHeight := domeDiameter * 0.35
	domeCenterY := fh*0.32 + domeRadius*0.1
	flangeTop := domeCenterY + domeRadius*0.55
	flangeBottom := flangeTop + flangeHeight

	dc.SetColor(ledFlangeColor)
	dc.DrawRoundedRectangle(cx-domeRadius, flangeTop, domeDiameter, flangeHeight, flangeHeight*0.25)
	dc.Fill()

	dc.SetColor(lens)
	dc.DrawCircle(cx, domeCenterY, domeRadius)
	dc.Fill()

	if !diffused {
		dc.SetColor(ledHighlightFill)
		dc.DrawEllipse(cx-domeRadius*0.35, domeCenterY-domeRadius*0.35, domeRadius*0.28, domeRadius*0.18)
		dc.Fill()
	}

	notchWidth := domeDiameter * 0.14
	var notchX float64
	if cathodeSide == "left" {
		notchX = cx - domeRadius
	} else {
		notchX = cx + domeRadius - notchWidth
	}
	dc.SetColor(ledCathodeMark)
	dc.SetLineWidth(math.Max(1, fh/100))
	dc.DrawLine(notchX, flangeTop+2, notchX+notchWidth, flangeTop+2)
	dc.Stroke()

	leadGap := domeDiameter * 0.36
	anodeX := cx + leadGap/2
	cathodeX := cx - leadGap/2
	if cathodeSide == "right" {
		anodeX, cathodeX = cathodeX, anodeX
	}
	leadWidth := math.Max(2, fh/60)
	dc.SetColor(ledLeadColor)
	dc.SetLineWidth(leadWidth)
	dc.DrawLine(anodeX, flangeBottom, anodeX, fh)
	dc.DrawLine(cathodeX, flangeBottom, cathodeX, fh*0.82)
	dc.Stroke()
}
