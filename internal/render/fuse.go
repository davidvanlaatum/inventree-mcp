package render

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"strconv"

	"github.com/fogleman/gg"
)

// FuseParams describes an axial glass cartridge fuse: two metal end caps
// joined by a glass tube with a visible fusible element.
type FuseParams struct {
	// RatingAmps is required and must be > 0.
	RatingAmps float64
	// RatingVoltage is optional and must be > 0 when supplied.
	RatingVoltage float64
	// Speed is "fast" (default) or "slow" (time-delay). It controls both
	// the printed speed letter and whether the fusible element is drawn
	// straight (fast) or coiled (slow, a common time-delay construction).
	Speed string
	// CapColorHex defaults to a silver metal tone when empty.
	CapColorHex string
	// Size is "5x20mm" (default) or "6x30mm". This selects an aspect
	// ratio preset only; it is not a physical scale claim.
	Size string
}

var (
	fuseDefaultCapColor = color.RGBA{R: 0xc0, G: 0xc0, B: 0xc0, A: 0xff}
	fuseGlassColor      = color.RGBA{R: 0xd8, G: 0xe8, B: 0xf0, A: 0xa0}
	fuseWireColor       = color.RGBA{R: 0x30, G: 0x30, B: 0x30, A: 0xff}
	fuseLeadColor       = color.RGBA{R: 0xb0, G: 0xb0, B: 0xb0, A: 0xff}
)

var fuseSizeAspect = map[string]float64{
	"5x20mm": 4.0,
	"6x30mm": 5.0,
}

// RenderFuse renders an axial glass cartridge fuse.
func RenderFuse(canvas CanvasOptions, params FuseParams) (Result, error) {
	if !(params.RatingAmps > 0) || math.IsInf(params.RatingAmps, 0) || math.IsNaN(params.RatingAmps) {
		return Result{}, errors.New("fuse rating_amps must be a positive finite value")
	}
	if params.RatingVoltage < 0 || math.IsInf(params.RatingVoltage, 0) || math.IsNaN(params.RatingVoltage) {
		return Result{}, errors.New("fuse rating_voltage must not be negative")
	}
	speed := params.Speed
	if speed == "" {
		speed = "fast"
	}
	if speed != "fast" && speed != "slow" {
		return Result{}, errors.New("fuse speed must be \"fast\" or \"slow\"")
	}
	capColor, err := validateHexOrDefault(params.CapColorHex, fuseDefaultCapColor)
	if err != nil {
		return Result{}, err
	}
	size := params.Size
	if size == "" {
		size = "5x20mm"
	}
	aspect, ok := fuseSizeAspect[size]
	if !ok {
		return Result{}, fmt.Errorf("fuse size must be \"5x20mm\" or \"6x30mm\"")
	}

	marking := formatFuseMarking(speed, params.RatingAmps, params.RatingVoltage)

	return renderCanvas(canvas, func(dc *gg.Context, w, h int) error {
		drawFuseGlyph(dc, w, h, aspect, capColor, speed, marking)
		return nil
	})
}

func formatFuseMarking(speed string, amps, voltage float64) string {
	letter := "F"
	if speed == "slow" {
		letter = "T"
	}
	var ampText string
	if amps < 1 {
		ampText = strconv.FormatFloat(amps*1000, 'f', -1, 64) + "mA"
	} else {
		ampText = strconv.FormatFloat(amps, 'f', -1, 64) + "A"
	}
	marking := letter + " " + ampText
	if voltage > 0 {
		marking += " " + strconv.FormatFloat(voltage, 'f', -1, 64) + "V"
	}
	return marking
}

func drawFuseGlyph(dc *gg.Context, w, h int, aspect float64, capColor color.RGBA, speed, marking string) {
	fw, fh := float64(w), float64(h)
	bodyHeight := fh * 0.4
	bodyWidth := math.Min(bodyHeight*aspect, fw*0.85)
	bodyWidth = math.Max(bodyWidth, fw*0.3)

	cx, cy := fw/2, fh/2
	bx := cx - bodyWidth/2
	by := cy - bodyHeight/2
	capWidth := bodyWidth * 0.12

	leadWidth := math.Max(2, fh/50)
	dc.SetColor(fuseLeadColor)
	dc.SetLineWidth(leadWidth)
	dc.DrawLine(0, cy, bx, cy)
	dc.DrawLine(bx+bodyWidth, cy, fw, cy)
	dc.Stroke()

	dc.SetColor(fuseGlassColor)
	dc.DrawRoundedRectangle(bx, by, bodyWidth, bodyHeight, bodyHeight*0.15)
	dc.Fill()

	dc.SetColor(fuseWireColor)
	dc.SetLineWidth(math.Max(1, fh/150))
	wireY := cy
	wireStartX := bx + capWidth
	wireEndX := bx + bodyWidth - capWidth
	if speed == "fast" {
		dc.DrawLine(wireStartX, wireY, wireEndX, wireY)
		dc.Stroke()
	} else {
		drawCoiledWire(dc, wireStartX, wireEndX, wireY, bodyHeight*0.2)
	}

	dc.SetColor(capColor)
	dc.DrawRectangle(bx, by, capWidth, bodyHeight)
	dc.Fill()
	dc.DrawRectangle(bx+bodyWidth-capWidth, by, capWidth, bodyHeight)
	dc.Fill()

	if marking != "" {
		scale := 1
		if fh >= 240 {
			scale = 2
		}
		drawCenteredMarkings(dc, marking, cx, cy, color.RGBA{A: 0xff}, scale)
	}
}

func drawCoiledWire(dc *gg.Context, startX, endX, y, amplitude float64) {
	segments := 8
	segmentWidth := (endX - startX) / float64(segments)
	amplitude = math.Min(amplitude, segmentWidth*1.2)
	dc.MoveTo(startX, y)
	for i := 1; i <= segments; i++ {
		x := startX + (endX-startX)*float64(i)/float64(segments)
		offset := amplitude
		if i%2 == 0 {
			offset = -amplitude
		}
		dc.LineTo(x, y+offset)
	}
	dc.LineTo(endX, y)
	dc.Stroke()
}
