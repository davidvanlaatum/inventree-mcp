// Package render implements deterministic, template-driven PNG rendering
// for a small, fixed set of highly repetitive electronic component
// families. Every template is a fixed Go drawing routine parameterized by a
// small, validated input struct; there is no AI generation, no free-form
// vector/SVG output contract, and no attempt to infer package geometry,
// markings, ratings, pinouts, or physical scale beyond what a caller
// explicitly supplies. Identical input always produces byte-identical PNG
// output.
package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	imgdraw "image/draw"
	"image/png"

	"github.com/fogleman/gg"
)

// Family identifies one of the fixed set of supported component templates.
type Family string

const (
	FamilyResistor  Family = "resistor"
	FamilyDiode     Family = "diode"
	FamilyLED       Family = "led"
	FamilyCapacitor Family = "capacitor"
	FamilyFuse      Family = "fuse"
)

// Orientation controls whole-glyph placement within the output canvas.
// Horizontal is each template's native drawing orientation. Vertical
// rotates the fully rendered glyph 90 degrees clockwise as a final,
// pixel-exact step; it does not change how a template lays out its own
// geometry.
type Orientation string

const (
	OrientationHorizontal Orientation = "horizontal"
	OrientationVertical   Orientation = "vertical"
)

// Background selects how the canvas is filled before the glyph is drawn.
type Background string

const (
	BackgroundTransparent Background = "transparent"
	BackgroundWhite       Background = "white"
	BackgroundColor       Background = "color"
)

const (
	// MinDimensionPx and MaxDimensionPx bound the requested output width
	// and height in pixels. The upper bound keeps worst-case memory and
	// encode time small and predictable for a local, synchronous render.
	MinDimensionPx = 64
	MaxDimensionPx = 1024

	// MaxOutputBytes bounds the encoded PNG size. The flat-shaded,
	// low-detail templates in this package never approach this in
	// practice; it exists as an explicit contract limit rather than an
	// expected trigger.
	MaxOutputBytes = 8 * 1024 * 1024
)

// CanvasOptions configures the shared output canvas independent of any
// family-specific template parameters.
type CanvasOptions struct {
	Width              int
	Height             int
	Orientation        Orientation
	Background         Background
	BackgroundColorHex string
}

// Result is the bounded, deterministic output of a render.
type Result struct {
	PNG    []byte
	Width  int
	Height int
	SHA256 string
}

// ValidateCanvasOptions checks the shared canvas fields independent of any
// family-specific template.
func ValidateCanvasOptions(opts CanvasOptions) error {
	if opts.Width < MinDimensionPx || opts.Width > MaxDimensionPx {
		return fmt.Errorf("width must be between %d and %d pixels", MinDimensionPx, MaxDimensionPx)
	}
	if opts.Height < MinDimensionPx || opts.Height > MaxDimensionPx {
		return fmt.Errorf("height must be between %d and %d pixels", MinDimensionPx, MaxDimensionPx)
	}
	switch opts.Orientation {
	case OrientationHorizontal, OrientationVertical:
	default:
		return fmt.Errorf("orientation must be %q or %q", OrientationHorizontal, OrientationVertical)
	}
	switch opts.Background {
	case BackgroundTransparent, BackgroundWhite:
		if opts.BackgroundColorHex != "" {
			return errors.New("background_color_hex is only valid when background is \"color\"")
		}
	case BackgroundColor:
		if _, err := parseHexColor(opts.BackgroundColorHex); err != nil {
			return fmt.Errorf("background_color_hex: %w", err)
		}
	default:
		return fmt.Errorf("background must be %q, %q, or %q", BackgroundTransparent, BackgroundWhite, BackgroundColor)
	}
	return nil
}

// Template draws one family's glyph into a landscape (width >= height in
// its own native frame) logical canvas. Templates never see Orientation;
// renderCanvas applies rotation uniformly after the template runs.
type Template func(dc *gg.Context, logicalWidth, logicalHeight int) error

// renderCanvas builds the shared canvas, background fill, orientation
// rotation, and bounded PNG encoding around one family Template.
func renderCanvas(opts CanvasOptions, draw Template) (Result, error) {
	if err := ValidateCanvasOptions(opts); err != nil {
		return Result{}, err
	}

	logicalW, logicalH := opts.Width, opts.Height
	if opts.Orientation == OrientationVertical {
		logicalW, logicalH = opts.Height, opts.Width
	}

	logical := gg.NewContext(logicalW, logicalH)
	if err := draw(logical, logicalW, logicalH); err != nil {
		return Result{}, err
	}

	glyph := logical.Image()
	if opts.Orientation == OrientationVertical {
		glyph = rotate90CW(glyph)
	}

	final := image.NewRGBA(image.Rect(0, 0, opts.Width, opts.Height))
	if err := fillBackground(final, opts); err != nil {
		return Result{}, err
	}
	imgdraw.Draw(final, final.Bounds(), glyph, image.Point{}, imgdraw.Over)

	return encodePNG(final)
}

func fillBackground(dst *image.RGBA, opts CanvasOptions) error {
	switch opts.Background {
	case BackgroundTransparent:
		// image.NewRGBA is already zero-valued, fully transparent.
		return nil
	case BackgroundWhite:
		imgdraw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, imgdraw.Src)
		return nil
	case BackgroundColor:
		c, err := parseHexColor(opts.BackgroundColorHex)
		if err != nil {
			return err
		}
		imgdraw.Draw(dst, dst.Bounds(), &image.Uniform{C: c}, image.Point{}, imgdraw.Src)
		return nil
	default:
		return fmt.Errorf("background must be %q, %q, or %q", BackgroundTransparent, BackgroundWhite, BackgroundColor)
	}
}

// rotate90CW rotates src 90 degrees clockwise using exact pixel copies, so
// no interpolation blur is introduced by orientation alone.
func rotate90CW(src image.Image) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func encodePNG(img *image.RGBA) (Result, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return Result{}, fmt.Errorf("encode PNG: %w", err)
	}
	if buf.Len() > MaxOutputBytes {
		return Result{}, fmt.Errorf("encoded PNG size %d exceeds maximum %d bytes", buf.Len(), MaxOutputBytes)
	}
	sum := sha256.Sum256(buf.Bytes())
	b := img.Bounds()
	return Result{
		PNG:    buf.Bytes(),
		Width:  b.Dx(),
		Height: b.Dy(),
		SHA256: hex.EncodeToString(sum[:]),
	}, nil
}
