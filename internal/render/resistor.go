package render

import (
	"errors"
	"fmt"
	"image/color"
	"math"

	"github.com/fogleman/gg"
)

// ResistorParams describes an axial through-hole resistor. Band colors are
// never supplied directly; they are deterministically derived from
// ResistanceOhms and BandCount using the IEC 60062 color code, so the same
// resistance and tolerance always produce the same bands.
type ResistorParams struct {
	// ResistanceOhms must be exactly representable as BandCount-1
	// significant digits times a power of ten (the same constraint real
	// E-series color-coded resistors satisfy).
	ResistanceOhms float64
	// BandCount is 4 or 5. Zero defaults to 4.
	BandCount int
	// ToleranceLabel must be one of ToleranceLabels(); required.
	ToleranceLabel string
	// Size is "small", "medium" (default), or "large".
	Size string
	// BodyLengthMM and BodyDiameterMM are optional; both must be supplied
	// together and are used only to set the drawn body aspect ratio, not
	// to claim physical scale.
	BodyLengthMM   float64
	BodyDiameterMM float64
}

var resistorLeadColor = color.RGBA{R: 0xb8, G: 0xb8, B: 0xb8, A: 0xff}
var resistorBodyColor = color.RGBA{R: 0xe6, G: 0xd9, B: 0xb3, A: 0xff}
var resistorOutlineColor = color.RGBA{R: 0x40, G: 0x38, B: 0x28, A: 0xff}

// RenderResistor renders an axial resistor with IEC 60062 color bands
// derived from ResistanceOhms, BandCount, and ToleranceLabel.
func RenderResistor(canvas CanvasOptions, params ResistorParams) (Result, error) {
	bandCount := params.BandCount
	if bandCount == 0 {
		bandCount = 4
	}
	if bandCount != 4 && bandCount != 5 {
		return Result{}, errors.New("resistor band_count must be 4 or 5")
	}
	if !(params.ResistanceOhms > 0) || math.IsInf(params.ResistanceOhms, 0) || math.IsNaN(params.ResistanceOhms) {
		return Result{}, errors.New("resistor resistance_ohms must be a positive finite value")
	}
	toleranceColor, ok := toleranceColorByLabel(params.ToleranceLabel)
	if !ok {
		return Result{}, fmt.Errorf("resistor tolerance_label must be one of %v", ToleranceLabels())
	}
	digits, multiplierExp, err := encodeResistorBands(params.ResistanceOhms, bandCount)
	if err != nil {
		return Result{}, err
	}
	size, err := resolveBodySize(params.Size)
	if err != nil {
		return Result{}, err
	}
	aspect, err := resolveAspect(params.BodyLengthMM, params.BodyDiameterMM, 2.6)
	if err != nil {
		return Result{}, err
	}

	bands := make([]bandColor, 0, bandCount)
	for _, d := range digits {
		bands = append(bands, digitColors[d])
	}
	bands = append(bands, multiplierColor(multiplierExp))
	bands = append(bands, toleranceColor)

	return renderCanvas(canvas, func(dc *gg.Context, w, h int) error {
		drawAxialResistorGlyph(dc, w, h, size, aspect, bands)
		return nil
	})
}

func drawAxialResistorGlyph(dc *gg.Context, w, h int, sizeFrac float64, aspect float64, bands []bandColor) {
	fw, fh := float64(w), float64(h)
	bodyHeight := fh * sizeFrac
	bodyWidth := math.Min(bodyHeight*aspect, fw*0.85)
	bodyWidth = math.Max(bodyWidth, fw*0.2)

	cx, cy := fw/2, fh/2
	bx := cx - bodyWidth/2
	by := cy - bodyHeight/2
	radius := bodyHeight * 0.4

	leadWidth := math.Max(2, fh/40)

	dc.SetColor(resistorLeadColor)
	dc.SetLineWidth(leadWidth)
	dc.DrawLine(0, cy, bx, cy)
	dc.DrawLine(bx+bodyWidth, cy, fw, cy)
	dc.Stroke()

	dc.SetColor(resistorBodyColor)
	dc.DrawRoundedRectangle(bx, by, bodyWidth, bodyHeight, radius)
	dc.Fill()

	drawResistorBands(dc, bx, by, bodyWidth, bodyHeight, radius, bands)

	dc.SetColor(resistorOutlineColor)
	dc.SetLineWidth(math.Max(1, fh/120))
	dc.DrawRoundedRectangle(bx, by, bodyWidth, bodyHeight, radius)
	dc.Stroke()
}

// drawResistorBands lays out the value/multiplier bands evenly across the
// left 65% of the body interior and the tolerance band separately in the
// right 15%, matching the visual gap real color-coded resistors use to
// separate the tolerance band from the value.
func drawResistorBands(dc *gg.Context, bx, by, bw, bh, radius float64, bands []bandColor) {
	margin := radius * 0.6
	valueBands := bands[:len(bands)-1]
	toleranceBand := bands[len(bands)-1]

	valueZoneStart := bx + margin
	valueZoneWidth := bw*0.62 - margin
	bandStride := valueZoneWidth / float64(len(valueBands))
	bandWidth := bandStride * 0.55

	for i, band := range valueBands {
		x := valueZoneStart + bandStride*float64(i) + (bandStride-bandWidth)/2
		dc.SetColor(band.rgb)
		dc.DrawRectangle(x, by, bandWidth, bh)
		dc.Fill()
	}

	toleranceX := bx + bw - margin - bandWidth
	dc.SetColor(toleranceBand.rgb)
	dc.DrawRectangle(toleranceX, by, bandWidth, bh)
	dc.Fill()
}

func multiplierColor(exp int) bandColor {
	switch {
	case exp == -1:
		return colorGold
	case exp == -2:
		return colorSilver
	case exp >= 0 && exp <= 9:
		return digitColors[exp]
	default:
		return digitColors[0]
	}
}

// encodeResistorBands finds the unique (digits, exponent) decomposition of
// ohms into sigFigs = bandCount-2 significant decimal digits times 10^exp,
// with exp in the representable multiplier range [-2, 9]. It fails closed
// when ohms is not exactly representable that way, rather than rounding to
// an invented value.
func encodeResistorBands(ohms float64, bandCount int) ([]int, int, error) {
	sigFigs := bandCount - 2
	const epsilon = 1e-9

	for exp := -2; exp <= 9; exp++ {
		mantissa := ohms / math.Pow(10, float64(exp))
		rounded := math.Round(mantissa)
		if math.Abs(mantissa-rounded) > epsilon*math.Max(1, math.Abs(mantissa)) {
			continue
		}
		lower := math.Pow(10, float64(sigFigs-1))
		upper := math.Pow(10, float64(sigFigs)) - 1
		if rounded < lower || rounded > upper {
			continue
		}
		reconstructed := rounded * math.Pow(10, float64(exp))
		if math.Abs(reconstructed-ohms) > epsilon*math.Max(1, math.Abs(ohms)) {
			continue
		}
		digits := make([]int, sigFigs)
		remaining := int64(rounded)
		for i := sigFigs - 1; i >= 0; i-- {
			digits[i] = int(remaining % 10)
			remaining /= 10
		}
		return digits, exp, nil
	}
	return nil, 0, fmt.Errorf("resistance_ohms %g is not exactly representable with %d significant digits on the standard decade grid", ohms, sigFigs)
}
