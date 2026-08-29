package render

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderLEDValidation(t *testing.T) {
	canvas := CanvasOptions{Width: 150, Height: 200, Orientation: OrientationHorizontal, Background: BackgroundTransparent}

	_, err := RenderLED(canvas, LEDParams{LensColor: "ultraviolet"})
	require.Error(t, err, "unsupported lens color")

	_, err = RenderLED(canvas, LEDParams{LensColor: "red", CathodeSide: "front"})
	require.Error(t, err, "invalid cathode_side")

	_, err = RenderLED(canvas, LEDParams{LensColor: "red", Size: "50mm"})
	require.Error(t, err, "unsupported size")

	for _, name := range LEDLensColors() {
		res, err := RenderLED(canvas, LEDParams{LensColor: name})
		require.NoErrorf(t, err, "lens color %s", name)
		require.NotEmpty(t, res.PNG)
	}

	res, err := RenderLED(canvas, LEDParams{LensColor: "clear", Diffused: false, CathodeSide: "right", Size: "3mm"})
	require.NoError(t, err)
	require.NotEmpty(t, res.PNG)
}
