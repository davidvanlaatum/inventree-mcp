package render

import (
	"errors"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/fogleman/gg"
)

// CapacitorParams describes a radial electrolytic capacitor: an upright
// cylindrical body with two leads exiting the bottom and a negative
// polarity stripe on one side.
type CapacitorParams struct {
	// BodyColorHex defaults to black when empty.
	BodyColorHex string
	// NegativeStripeColorHex defaults to light grey when empty.
	NegativeStripeColorHex string
	// CapacitanceMicrofarads is optional; when supplied it must be > 0
	// and is formatted into the primary marking line.
	CapacitanceMicrofarads float64
	// VoltageRatingV is optional and requires CapacitanceMicrofarads.
	VoltageRatingV float64
	// MarkingsText is an optional extra line, for example a series code.
	MarkingsText string
	// NegativeSide is "left" (default) or "right".
	NegativeSide string
	Size         string
	// DiameterMM and HeightMM are optional and must be supplied together.
	DiameterMM, HeightMM float64
}

var (
	capacitorDefaultBodyColor   = color.RGBA{R: 0x18, G: 0x18, B: 0x18, A: 0xff}
	capacitorDefaultStripeColor = color.RGBA{R: 0xd8, G: 0xd8, B: 0xd8, A: 0xff}
	capacitorLeadColor          = color.RGBA{R: 0xb8, G: 0xb8, B: 0xb8, A: 0xff}
)

// RenderCapacitor renders a radial electrolytic capacitor.
func RenderCapacitor(canvas CanvasOptions, params CapacitorParams) (Result, error) {
	bodyColor, err := validateHexOrDefault(params.BodyColorHex, capacitorDefaultBodyColor)
	if err != nil {
		return Result{}, err
	}
	stripeColor, err := validateHexOrDefault(params.NegativeStripeColorHex, capacitorDefaultStripeColor)
	if err != nil {
		return Result{}, err
	}
	if bodyColor == stripeColor {
		return Result{}, errors.New("capacitor negative_stripe_color_hex must differ from body_color_hex")
	}
	if params.VoltageRatingV > 0 && !(params.CapacitanceMicrofarads > 0) {
		return Result{}, errors.New("capacitor voltage_rating_v requires capacitance_microfarads")
	}
	if params.CapacitanceMicrofarads < 0 || params.VoltageRatingV < 0 {
		return Result{}, errors.New("capacitor capacitance_microfarads and voltage_rating_v must not be negative")
	}
	if err := validateMarkings(params.MarkingsText); err != nil {
		return Result{}, err
	}
	side := params.NegativeSide
	if side == "" {
		side = "left"
	}
	if side != "left" && side != "right" {
		return Result{}, errors.New("capacitor negative_side must be \"left\" or \"right\"")
	}
	size, err := resolveBodySize(params.Size)
	if err != nil {
		return Result{}, err
	}
	aspect, err := resolveAspect(params.HeightMM, params.DiameterMM, 2.0)
	if err != nil {
		return Result{}, err
	}

	primaryMarking := formatCapacitorMarking(params.CapacitanceMicrofarads, params.VoltageRatingV)

	return renderCanvas(canvas, func(dc *gg.Context, w, h int) error {
		drawCapacitorGlyph(dc, w, h, size, aspect, bodyColor, stripeColor, side, primaryMarking, params.MarkingsText)
		return nil
	})
}

func formatCapacitorMarking(microfarads, voltage float64) string {
	if microfarads <= 0 {
		return ""
	}
	value := strconv.FormatFloat(microfarads, 'f', -1, 64)
	marking := value + "uF"
	if voltage > 0 {
		marking += " " + strconv.FormatFloat(voltage, 'f', -1, 64) + "V"
	}
	return marking
}

func drawCapacitorGlyph(dc *gg.Context, w, h int, sizeFrac, aspect float64, bodyColor, stripeColor color.RGBA, negativeSide string, primaryMarking, extraMarking string) {
	fw, fh := float64(w), float64(h)
	bodyHeight := fh * sizeFrac
	bodyWidth := math.Min(bodyHeight/aspect, fw*0.7)
	bodyWidth = math.Max(bodyWidth, fw*0.15)

	cx := fw / 2
	bodyTop := fh*0.5 - bodyHeight/2
	bodyBottom := bodyTop + bodyHeight
	bx := cx - bodyWidth/2
	radius := bodyWidth * 0.18

	leadGap := bodyWidth * 0.4
	leadWidth := math.Max(2, fh/60)
	dc.SetColor(capacitorLeadColor)
	dc.SetLineWidth(leadWidth)
	dc.DrawLine(cx-leadGap/2, bodyBottom, cx-leadGap/2, fh)
	dc.DrawLine(cx+leadGap/2, bodyBottom, cx+leadGap/2, fh)
	dc.Stroke()

	// Cylindrical shading: a left-to-right gradient with a highlight band
	// suggests a rounded can rather than a flat rectangle.
	shadow := mixColor(bodyColor, shadeBlack, 0.35)
	highlight := mixColor(bodyColor, shadeWhite, 0.3)
	bodyGradient := gg.NewLinearGradient(bx, 0, bx+bodyWidth, 0)
	bodyGradient.AddColorStop(0, shadow)
	bodyGradient.AddColorStop(0.38, highlight)
	bodyGradient.AddColorStop(0.6, bodyColor)
	bodyGradient.AddColorStop(1, shadow)
	dc.SetFillStyle(bodyGradient)
	dc.DrawRoundedRectangle(bx, bodyTop, bodyWidth, bodyHeight, radius)
	dc.Fill()

	// Clip the stripe to the can's own rounded silhouette so its corners
	// follow the same rounding instead of a flat rectangle edge poking
	// past the curved top/bottom.
	dc.DrawRoundedRectangle(bx, bodyTop, bodyWidth, bodyHeight, radius)
	dc.Clip()

	// The negative stripe is a printed marking on the can, not a separate
	// panel: it gets the same left-to-right shading as the body (in its
	// own tone) so it curves with the cylinder instead of reading as a
	// flat piece stuck on top.
	stripeWidth := bodyWidth * 0.16
	var stripeX float64
	if negativeSide == "left" {
		stripeX = bx
	} else {
		stripeX = bx + bodyWidth - stripeWidth
	}
	stripeShadow := mixColor(stripeColor, shadeBlack, 0.3)
	stripeGradient := gg.NewLinearGradient(bx, 0, bx+bodyWidth, 0)
	stripeGradient.AddColorStop(0, stripeShadow)
	stripeGradient.AddColorStop(0.38, mixColor(stripeColor, shadeWhite, 0.25))
	stripeGradient.AddColorStop(0.6, stripeColor)
	stripeGradient.AddColorStop(1, stripeShadow)
	dc.SetFillStyle(stripeGradient)
	dc.DrawRectangle(stripeX, bodyTop, stripeWidth, bodyHeight)
	dc.Fill()

	drawMinusMarks(dc, stripeX+stripeWidth/2, bodyTop+bodyHeight*0.08, bodyTop+bodyHeight*0.92, stripeWidth*0.55, contrastingTextColor(stripeColor))

	// The top cap ellipse is drawn after (and still clipped to the can),
	// matching a real capacitor where the crimped top disk is a single
	// continuous surface that the printed stripe stops short of, rather
	// than the stripe running all the way into the rounded top.
	dc.SetColor(mixColor(bodyColor, shadeWhite, 0.15))
	dc.DrawEllipse(cx, bodyTop+radius*0.15, bodyWidth/2*0.94, radius*0.5)
	dc.Fill()

	dc.ResetClip()

	// Printed markings run vertically along the can, matching real
	// electrolytic capacitors: the tall, narrow body has far more usable
	// length top-to-bottom than side-to-side.
	textColor := contrastingTextColor(bodyColor)
	var textCX, freeZoneWidth float64
	if negativeSide == "left" {
		textCX = bx + stripeWidth + (bodyWidth-stripeWidth)/2
	} else {
		textCX = bx + (bodyWidth-stripeWidth)/2
	}
	freeZoneWidth = bodyWidth - stripeWidth

	marking := strings.TrimSpace(primaryMarking + " " + extraMarking)
	if marking != "" {
		points := fitFontSize(marking, bodyHeight*0.85, freeZoneWidth*0.8, false)
		dc.Push()
		dc.RotateAbout(-math.Pi/2, textCX, bodyTop+bodyHeight/2)
		drawCenteredText(dc, marking, textCX, bodyTop+bodyHeight/2, points, false, textColor)
		dc.Pop()
	}
}

func drawMinusMarks(dc *gg.Context, cx, top, bottom, width float64, markColor color.RGBA) {
	count := 6
	dc.SetColor(markColor)
	dc.SetLineWidth(math.Max(1, width/6))
	for i := 0; i < count; i++ {
		y := top + (bottom-top)*float64(i)/float64(count-1)
		dc.DrawLine(cx-width/2, y, cx+width/2, y)
		dc.Stroke()
	}
}
