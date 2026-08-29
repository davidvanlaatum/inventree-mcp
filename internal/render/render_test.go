package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validResistorCanvas() CanvasOptions {
	return CanvasOptions{Width: 300, Height: 120, Orientation: OrientationHorizontal, Background: BackgroundTransparent}
}

func TestValidateCanvasOptions(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	r.NoError(ValidateCanvasOptions(validResistorCanvas()))

	a.Error(ValidateCanvasOptions(CanvasOptions{Width: MinDimensionPx - 1, Height: 100, Orientation: OrientationHorizontal, Background: BackgroundTransparent}), "width below minimum")
	a.Error(ValidateCanvasOptions(CanvasOptions{Width: MaxDimensionPx + 1, Height: 100, Orientation: OrientationHorizontal, Background: BackgroundTransparent}), "width above maximum")
	a.Error(ValidateCanvasOptions(CanvasOptions{Width: 100, Height: MinDimensionPx - 1, Orientation: OrientationHorizontal, Background: BackgroundTransparent}), "height below minimum")
	a.Error(ValidateCanvasOptions(CanvasOptions{Width: 100, Height: MaxDimensionPx + 1, Orientation: OrientationHorizontal, Background: BackgroundTransparent}), "height above maximum")
	a.NoError(ValidateCanvasOptions(CanvasOptions{Width: MinDimensionPx, Height: MinDimensionPx, Orientation: OrientationHorizontal, Background: BackgroundTransparent}), "width/height exactly at minimum is valid")
	a.NoError(ValidateCanvasOptions(CanvasOptions{Width: MaxDimensionPx, Height: MaxDimensionPx, Orientation: OrientationHorizontal, Background: BackgroundTransparent}), "width/height exactly at maximum is valid")
	a.Error(ValidateCanvasOptions(CanvasOptions{Width: 100, Height: 100, Orientation: "diagonal", Background: BackgroundTransparent}), "invalid orientation")
	a.Error(ValidateCanvasOptions(CanvasOptions{Width: 100, Height: 100, Orientation: OrientationHorizontal, Background: "rainbow"}), "invalid background")
	a.Error(ValidateCanvasOptions(CanvasOptions{Width: 100, Height: 100, Orientation: OrientationHorizontal, Background: BackgroundColor}), "color background requires hex")
	a.Error(ValidateCanvasOptions(CanvasOptions{Width: 100, Height: 100, Orientation: OrientationHorizontal, Background: BackgroundColor, BackgroundColorHex: "zzzzzz"}), "invalid hex")
	a.Error(ValidateCanvasOptions(CanvasOptions{Width: 100, Height: 100, Orientation: OrientationHorizontal, Background: BackgroundWhite, BackgroundColorHex: "ffffff"}), "hex only valid with color background")
	a.NoError(ValidateCanvasOptions(CanvasOptions{Width: 100, Height: 100, Orientation: OrientationHorizontal, Background: BackgroundColor, BackgroundColorHex: "#ff00ff"}))
}

func TestOrientationsAndBackgrounds(t *testing.T) {
	a := assert.New(t)
	a.Equal([]string{"horizontal", "vertical"}, Orientations())
	a.Equal([]string{"transparent", "white", "color"}, Backgrounds())
}

func TestFamilies(t *testing.T) {
	a := assert.New(t)
	a.Equal([]Family{FamilyResistor, FamilyDiode, FamilyLED, FamilyCapacitor, FamilyFuse}, Families())

	// The returned slice must be a copy: mutating it must not affect the
	// next call's result.
	families := Families()
	families[0] = "mutated"
	a.Equal([]Family{FamilyResistor, FamilyDiode, FamilyLED, FamilyCapacitor, FamilyFuse}, Families())
}

func TestRenderDeterministic(t *testing.T) {
	r := require.New(t)
	canvas := validResistorCanvas()
	params := ResistorParams{ResistanceOhms: 4700, BandCount: 4, ToleranceLabel: "5%"}

	first, err := RenderResistor(canvas, params)
	r.NoError(err)
	second, err := RenderResistor(canvas, params)
	r.NoError(err)

	r.Equal(first.SHA256, second.SHA256)
	r.True(bytes.Equal(first.PNG, second.PNG))
}

func TestRenderDifferentInputsProduceDifferentOutput(t *testing.T) {
	r := require.New(t)
	canvas := validResistorCanvas()

	a, err := RenderResistor(canvas, ResistorParams{ResistanceOhms: 100, ToleranceLabel: "5%"})
	r.NoError(err)
	b, err := RenderResistor(canvas, ResistorParams{ResistanceOhms: 220, ToleranceLabel: "5%"})
	r.NoError(err)
	r.NotEqual(a.SHA256, b.SHA256, "different resistance values must render differently")

	c, err := RenderResistor(canvas, ResistorParams{ResistanceOhms: 100, ToleranceLabel: "1%"})
	r.NoError(err)
	r.NotEqual(a.SHA256, c.SHA256, "different tolerance must render differently")

	four, err := RenderResistor(canvas, ResistorParams{ResistanceOhms: 470, BandCount: 4, ToleranceLabel: "5%"})
	r.NoError(err)
	five, err := RenderResistor(canvas, ResistorParams{ResistanceOhms: 470, BandCount: 5, ToleranceLabel: "5%"})
	r.NoError(err)
	r.NotEqual(four.SHA256, five.SHA256, "band_count 4 vs 5 must render differently")
}

func TestRotate90CWExactMapping(t *testing.T) {
	// 3-wide by 2-tall source with a distinct value per pixel, so the
	// rotation direction and axis mapping can be checked exactly rather
	// than just the output dimensions.
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.SetRGBA(0, 0, color.RGBA{R: 1, A: 0xff})
	src.SetRGBA(1, 0, color.RGBA{R: 2, A: 0xff})
	src.SetRGBA(2, 0, color.RGBA{R: 3, A: 0xff})
	src.SetRGBA(0, 1, color.RGBA{R: 4, A: 0xff})
	src.SetRGBA(1, 1, color.RGBA{R: 5, A: 0xff})
	src.SetRGBA(2, 1, color.RGBA{R: 6, A: 0xff})

	dst := rotate90CW(src)
	r := require.New(t)
	r.Equal(2, dst.Bounds().Dx())
	r.Equal(3, dst.Bounds().Dy())

	// Clockwise rotation of a 3x2 image: the source's top-left pixel ends
	// up at the rotated image's top-right, and the leftmost source column
	// becomes the rotated image's top row.
	r.Equal(uint8(4), dst.RGBAAt(0, 0).R)
	r.Equal(uint8(5), dst.RGBAAt(0, 1).R)
	r.Equal(uint8(6), dst.RGBAAt(0, 2).R)
	r.Equal(uint8(1), dst.RGBAAt(1, 0).R)
	r.Equal(uint8(2), dst.RGBAAt(1, 1).R)
	r.Equal(uint8(3), dst.RGBAAt(1, 2).R)
}

func TestEncodePNGEnforcesMaxOutputBytes(t *testing.T) {
	// High-entropy content that PNG compression cannot meaningfully shrink,
	// well beyond MaxOutputBytes, exercised directly against encodePNG so
	// a regression in the cap (or in the compression level that keeps
	// real templates under it) is caught even though no in-scope template
	// approaches this size in practice.
	img := image.NewRGBA(image.Rect(0, 0, 3000, 3000))
	for y := 0; y < 3000; y++ {
		for x := 0; x < 3000; x++ {
			v := uint8((x*2654435761 + y*40503 + x*y) >> 3)
			img.SetRGBA(x, y, color.RGBA{R: v, G: v ^ 0xaa, B: v + uint8(y), A: 0xff})
		}
	}
	_, err := encodePNG(img)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum")
}

func TestRenderOutputDimensionsAndOrientation(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	horizontal := CanvasOptions{Width: 300, Height: 120, Orientation: OrientationHorizontal, Background: BackgroundTransparent}
	res, err := RenderResistor(horizontal, ResistorParams{ResistanceOhms: 100, ToleranceLabel: "5%"})
	r.NoError(err)
	a.Equal(300, res.Width)
	a.Equal(120, res.Height)
	decodeAndCheckSize(t, res.PNG, 300, 120)

	vertical := CanvasOptions{Width: 120, Height: 300, Orientation: OrientationVertical, Background: BackgroundTransparent}
	res2, err := RenderResistor(vertical, ResistorParams{ResistanceOhms: 100, ToleranceLabel: "5%"})
	r.NoError(err)
	a.Equal(120, res2.Width)
	a.Equal(300, res2.Height)
	decodeAndCheckSize(t, res2.PNG, 120, 300)
}

func TestRenderBackgroundVariants(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)
	base := CanvasOptions{Width: 200, Height: 200, Orientation: OrientationHorizontal}
	params := ResistorParams{ResistanceOhms: 100, ToleranceLabel: "5%"}

	transparent := base
	transparent.Background = BackgroundTransparent
	resT, err := RenderResistor(transparent, params)
	r.NoError(err)
	imgT := decodePNG(t, resT.PNG)
	_, _, _, alpha := imgT.At(0, 0).RGBA()
	a.Equal(uint32(0), alpha, "corner pixel should be fully transparent")

	white := base
	white.Background = BackgroundWhite
	resW, err := RenderResistor(white, params)
	r.NoError(err)
	imgW := decodePNG(t, resW.PNG)
	cr, cg, cb, ca := imgW.At(0, 0).RGBA()
	a.Equal(uint32(0xffff), cr)
	a.Equal(uint32(0xffff), cg)
	a.Equal(uint32(0xffff), cb)
	a.Equal(uint32(0xffff), ca)

	explicit := base
	explicit.Background = BackgroundColor
	explicit.BackgroundColorHex = "#336699"
	resC, err := RenderResistor(explicit, params)
	r.NoError(err)
	imgC := decodePNG(t, resC.PNG)
	er, eg, eb, _ := imgC.At(0, 0).RGBA()
	a.Equal(uint32(0x33)<<8|0x33, er)
	a.Equal(uint32(0x66)<<8|0x66, eg)
	a.Equal(uint32(0x99)<<8|0x99, eb)
}

func decodePNG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	return img
}

func decodeAndCheckSize(t *testing.T, data []byte, w, h int) {
	t.Helper()
	img := decodePNG(t, data)
	b := img.Bounds()
	require.Equal(t, w, b.Dx())
	require.Equal(t, h, b.Dy())
}
