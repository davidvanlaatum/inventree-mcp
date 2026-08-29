package render

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

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
	// Type is "carbon_film" (default) or "metal_film". It only sets the
	// default body color (beige or blue, matching near-universal
	// manufacturer convention); BodyColorHex overrides it when supplied.
	// Wirewound and similar constructions are out of scope: they are not
	// normally marked with painted IEC 60062 color bands at all, so they
	// do not fit this band-based template.
	Type string
	// BodyColorHex overrides Type's default body color when supplied.
	BodyColorHex string
	// PowerRatingWatts is optional and must be > 0 when supplied. It is
	// only used for the ShowLabel caption and, when Size is not also
	// explicitly supplied, to pick a larger body size preset for higher
	// power ratings (a real, well-established convention, not a claim of
	// exact physical scale).
	PowerRatingWatts float64
	// ShowLabel draws a bold resistance-and-tolerance line above the body
	// (for example "4.7 kΩ ±5%") and a caption below it with the resistor
	// type, power rating when supplied, band count, and band color names
	// in order. Every value is already present in the request or derived
	// from it; nothing about material or package is invented.
	ShowLabel bool
}

var resistorLeadColor = color.RGBA{R: 0xb8, G: 0xb8, B: 0xb8, A: 0xff}
var resistorOutlineColor = color.RGBA{R: 0x40, G: 0x38, B: 0x28, A: 0xff}
var resistorLightOutlineColor = color.RGBA{R: 0xc8, G: 0xc8, B: 0xc8, A: 0xff}

// ResistorType is the accepted render_component_image resistor body-type
// values.
type ResistorType string

const (
	ResistorTypeCarbonFilm ResistorType = "carbon_film"
	ResistorTypeMetalFilm  ResistorType = "metal_film"
)

// resistorTypes maps the supported Type values to a human-readable label
// (used only in the derived ShowLabel caption) and a default body color
// matching near-universal manufacturer convention. Fixed and ordered; do
// not derive from a map range.
var resistorTypes = []struct {
	value     ResistorType
	label     string
	bodyColor color.RGBA
}{
	{ResistorTypeCarbonFilm, "Carbon film", color.RGBA{R: 0xe6, G: 0xd9, B: 0xb3, A: 0xff}},
	{ResistorTypeMetalFilm, "Metal film", color.RGBA{R: 0x4a, G: 0x8f, B: 0xe0, A: 0xff}},
}

func resistorTypeInfo(value string) (label string, bodyColor color.RGBA, ok bool) {
	if value == "" {
		value = string(ResistorTypeCarbonFilm)
	}
	for _, t := range resistorTypes {
		if string(t.value) == value {
			return t.label, t.bodyColor, true
		}
	}
	return "", color.RGBA{}, false
}

// ResistorTypes returns the supported resistor type values in a fixed,
// stable order.
func ResistorTypes() []string {
	values := make([]string, len(resistorTypes))
	for i, t := range resistorTypes {
		values[i] = string(t.value)
	}
	return values
}

// resistorOutlineColorFor picks a dark or light outline so the body edge
// stays visible against both light ceramic tones and dark body colors.
func resistorOutlineColorFor(bodyColor color.RGBA) color.RGBA {
	luminance := 0.299*float64(bodyColor.R) + 0.587*float64(bodyColor.G) + 0.114*float64(bodyColor.B)
	if luminance < 100 {
		return resistorLightOutlineColor
	}
	return resistorOutlineColor
}

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
	if params.PowerRatingWatts < 0 || math.IsInf(params.PowerRatingWatts, 0) || math.IsNaN(params.PowerRatingWatts) {
		return Result{}, errors.New("resistor power_rating_watts must not be negative")
	}
	requestedSize := params.Size
	if requestedSize == "" && params.PowerRatingWatts > 0 {
		requestedSize = sizeForPowerRating(params.PowerRatingWatts)
	}
	size, err := resolveBodySize(requestedSize)
	if err != nil {
		return Result{}, err
	}
	aspect, err := resolveAspect(params.BodyLengthMM, params.BodyDiameterMM, 2.6)
	if err != nil {
		return Result{}, err
	}
	typeLabel, typeBodyColor, ok := resistorTypeInfo(params.Type)
	if !ok {
		return Result{}, fmt.Errorf("resistor type must be one of %v", ResistorTypes())
	}
	bodyColor, err := validateHexOrDefault(params.BodyColorHex, typeBodyColor)
	if err != nil {
		return Result{}, err
	}

	bands := make([]bandColor, 0, bandCount)
	for _, d := range digits {
		bands = append(bands, digitColors[d])
	}
	bands = append(bands, multiplierColor(multiplierExp))
	bands = append(bands, toleranceColor)

	var valueLine string
	var captionLines []string
	if params.ShowLabel {
		valueLine = formatResistanceValue(params.ResistanceOhms) + " ±" + params.ToleranceLabel

		bandNames := make([]string, len(bands))
		for i, b := range bands {
			bandNames[i] = b.name
		}
		typeCaption := fmt.Sprintf("%s, %d-band", typeLabel, bandCount)
		if params.PowerRatingWatts > 0 {
			typeCaption = fmt.Sprintf("%s, %s, %d-band", typeLabel, formatWattage(params.PowerRatingWatts), bandCount)
		}
		captionLines = []string{
			typeCaption,
			"Bands: " + strings.Join(bandNames, " / "),
		}
	}

	return renderCanvas(canvas, func(dc *gg.Context, w, h int) error {
		drawAxialResistorGlyph(dc, w, h, size, aspect, bodyColor, bands, valueLine, captionLines)
		return nil
	})
}

// sizeForPowerRating maps a power rating to a body size preset, matching
// the well-established convention that higher-wattage resistors have
// physically larger bodies. It is only used as a default when the caller
// does not also supply Size explicitly.
func sizeForPowerRating(watts float64) string {
	switch {
	case watts <= 0.2:
		return "small"
	case watts <= 0.6:
		return "medium"
	default:
		return "large"
	}
}

// formatResistanceValue formats ohms as a decimal value with its SI-prefixed
// ohm unit, for example 4700 -> "4.7 kΩ", 100 -> "100 Ω".
func formatResistanceValue(ohms float64) string {
	unit, div := "Ω", 1.0
	switch {
	case ohms >= 1e9:
		unit, div = "GΩ", 1e9
	case ohms >= 1e6:
		unit, div = "MΩ", 1e6
	case ohms >= 1e3:
		unit, div = "kΩ", 1e3
	}
	return strconv.FormatFloat(ohms/div, 'f', -1, 64) + " " + unit
}

// formatWattage formats a power rating using the common fraction notation
// (1/8W, 1/4W, 1/2W) for the values it applies to, or a plain decimal
// suffix otherwise (for example 2 -> "2W", 0.33 -> "0.33W").
func formatWattage(watts float64) string {
	switch watts {
	case 0.125:
		return "1/8W"
	case 0.25:
		return "1/4W"
	case 0.5:
		return "1/2W"
	}
	return strconv.FormatFloat(watts, 'f', -1, 64) + "W"
}

func drawAxialResistorGlyph(dc *gg.Context, w, h int, sizeFrac float64, aspect float64, bodyColor color.RGBA, bands []bandColor, valueLine string, captionLines []string) {
	fw, fh := float64(w), float64(h)
	top, bottom := verticalLayoutBand(fh, valueLine != "", len(captionLines))
	componentAreaHeight := bottom - top

	bodyHeight := componentAreaHeight * sizeFrac
	bodyWidth := math.Min(bodyHeight*aspect, fw*0.85)
	bodyWidth = math.Max(bodyWidth, fw*0.2)

	cx, cy := fw/2, top+componentAreaHeight/2
	bx := cx - bodyWidth/2
	by := cy - bodyHeight/2
	radius := bodyHeight * 0.4

	leadWidth := math.Max(2, componentAreaHeight/40)

	dc.SetColor(resistorLeadColor)
	dc.SetLineWidth(leadWidth)
	dc.DrawLine(0, cy, bx, cy)
	dc.DrawLine(bx+bodyWidth, cy, fw, cy)
	dc.Stroke()

	dc.SetColor(bodyColor)
	dc.DrawRoundedRectangle(bx, by, bodyWidth, bodyHeight, radius)
	dc.Fill()

	drawResistorBands(dc, bx, by, bodyWidth, bodyHeight, radius, bands)

	dc.SetColor(resistorOutlineColorFor(bodyColor))
	dc.SetLineWidth(math.Max(1, componentAreaHeight/120))
	dc.DrawRoundedRectangle(bx, by, bodyWidth, bodyHeight, radius)
	dc.Stroke()

	if valueLine != "" {
		points := fitFontSize(valueLine, fw*0.92, top*0.7, true)
		drawCenteredText(dc, valueLine, fw/2, top/2, points, true, shadeBlack)
	}
	drawCaptionLines(dc, captionLines, fw/2, bottom, fh, fw)
}

// drawCaptionLines lays out one or more derived-value caption lines
// (never caller-supplied free text) centered below componentBottom,
// scaled to fill the available caption band.
func drawCaptionLines(dc *gg.Context, lines []string, cx, componentBottom, fh, fw float64) {
	if len(lines) == 0 {
		return
	}
	captionHeight := fh - componentBottom
	longest := lines[0]
	for _, l := range lines {
		if len(l) > len(longest) {
			longest = l
		}
	}
	points := fitFontSize(longest, fw*0.92, captionHeight/float64(len(lines))*0.7, false)
	lineGap := points * 1.5
	startY := componentBottom + captionHeight/2 - lineGap*float64(len(lines)-1)/2
	for i, line := range lines {
		drawCenteredText(dc, line, cx, startY+lineGap*float64(i), points, false, shadeBlack)
	}
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
