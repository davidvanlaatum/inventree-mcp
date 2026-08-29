package render

import (
	"errors"
	"fmt"
	"image/color"
	"math"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
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
// printable ASCII set so the drawn text bounds stay predictable. This
// applies only to caller-supplied free text (for example a diode part
// number); server-derived caption text such as ShowLabel captions is not
// caller input and is not run through this check.
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

// Text rendering uses Go's own "Go Sans" TTF font (golang.org/x/image's
// gofont package, already available through the existing golang.org/x/image
// dependency; no new go.mod entry or separate font license) instead of a
// fixed bitmap font, so derived captions can use real Unicode glyphs such
// as Ω and ± and scale to any point size with proper anti-aliasing.
var (
	goRegularFont = mustParseFont(goregular.TTF)
	goBoldFont    = mustParseFont(gobold.TTF)
)

// mustParseFont parses compiled-in font bytes from the golang.org/x/image
// module. It can only fail if that embedded data is malformed, which would
// be a build-time invariant violation, not a runtime/input error.
func mustParseFont(data []byte) *truetype.Font {
	f, err := truetype.Parse(data)
	if err != nil {
		panic(fmt.Sprintf("render: parse embedded font: %v", err))
	}
	return f
}

const (
	minTextPoints = 6.0
	maxTextPoints = 120.0
)

func textFace(points float64, bold bool) font.Face {
	f := goRegularFont
	if bold {
		f = goBoldFont
	}
	return truetype.NewFace(f, &truetype.Options{Size: points})
}

// measureText returns the rendered width and height of text at the given
// point size.
func measureText(text string, points float64, bold bool) (w, h float64) {
	dc := gg.NewContext(1, 1)
	dc.SetFontFace(textFace(points, bold))
	return dc.MeasureString(text)
}

// drawCenteredText draws a single line of text centered at (cx, cy) using
// Go Sans at the given point size.
func drawCenteredText(dc *gg.Context, text string, cx, cy, points float64, bold bool, textColor color.RGBA) {
	if text == "" {
		return
	}
	dc.SetFontFace(textFace(points, bold))
	dc.SetColor(textColor)
	dc.DrawStringAnchored(text, cx, cy, 0.5, 0.5)
}

// fitFontSize returns the largest point size in [minTextPoints,
// maxTextPoints] such that text fits within maxWidth x maxHeight, so
// markings and captions use the available body space instead of a fixed
// small size regardless of canvas size. The search is a fixed number of
// deterministic bisection steps, not a data-dependent loop.
func fitFontSize(text string, maxWidth, maxHeight float64, bold bool) float64 {
	if text == "" {
		return minTextPoints
	}
	lo, hi := minTextPoints, maxTextPoints
	best := lo
	for i := 0; i < 24; i++ {
		mid := (lo + hi) / 2
		w, h := measureText(text, mid, bold)
		if w <= maxWidth && h <= maxHeight {
			best = mid
			lo = mid
		} else {
			hi = mid
		}
	}
	return best
}

// verticalLayoutBand returns the [top, bottom] vertical band a template
// should draw its own component in, reserving space above for an optional
// bold value line and below for captionLineCount caption lines. Callers
// must add top as an explicit offset to every Y coordinate they compute
// (component height = bottom - top): gg's gradient patterns sample in
// device pixel space regardless of dc.Translate, so a canvas-level
// translation would desync a gradient's own coordinates from the shape it
// fills. Explicit offsets avoid that trap entirely.
func verticalLayoutBand(fh float64, hasValueLine bool, captionLineCount int) (top, bottom float64) {
	top = 0
	if hasValueLine {
		top = fh * 0.2
	}
	bottom = fh
	if captionLineCount > 0 {
		captionFraction := 0.08 + 0.12*float64(captionLineCount)
		if captionFraction > 0.6 {
			captionFraction = 0.6
		}
		bottom = fh * (1 - captionFraction)
	}
	return top, bottom
}

// mixColor linearly interpolates from c toward target by t in [0, 1],
// used to derive lighter/darker shading tones from a caller-supplied base
// color for gradient fills (t=0 returns c, t=1 returns target).
func mixColor(c, target color.RGBA, t float64) color.RGBA {
	lerp := func(a, b uint8) uint8 {
		return uint8(float64(a) + (float64(b)-float64(a))*t)
	}
	return color.RGBA{R: lerp(c.R, target.R), G: lerp(c.G, target.G), B: lerp(c.B, target.B), A: c.A}
}

var (
	shadeWhite = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	shadeBlack = color.RGBA{A: 0xff}
)

// drawCenteredTextOnBacking draws an opaque rounded-rect panel sized to the
// text bounds before the text itself, so markings stay legible over a busy
// background (for example a fuse's fusible element) regardless of what
// would otherwise be directly behind them.
func drawCenteredTextOnBacking(dc *gg.Context, text string, cx, cy, points float64, bold bool, textColor, backingColor color.RGBA) {
	if text == "" {
		return
	}
	width, height := measureText(text, points, bold)
	padX := points * 0.35
	padY := points * 0.22

	dc.SetColor(backingColor)
	dc.DrawRoundedRectangle(cx-width/2-padX, cy-height/2-padY, width+padX*2, height+padY*2, padY)
	dc.Fill()

	drawCenteredText(dc, text, cx, cy, points, bold, textColor)
}
