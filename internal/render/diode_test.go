package render

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderDiodeValidation(t *testing.T) {
	canvas := validResistorCanvas()

	res, err := RenderDiode(canvas, DiodeParams{})
	require.NoError(t, err, "defaults (black body, white band, left cathode) should render")
	require.NotEmpty(t, res.PNG)

	_, err = RenderDiode(canvas, DiodeParams{BodyColorHex: "#ffffff", CathodeBandColorHex: "#ffffff"})
	require.Error(t, err, "identical body and band color must be rejected")

	_, err = RenderDiode(canvas, DiodeParams{BodyColorHex: "#FFFFFF", CathodeBandColorHex: "#ffffff"})
	require.Error(t, err, "identical colors differing only by hex case must still be rejected")

	_, err = RenderDiode(canvas, DiodeParams{CathodeSide: "up"})
	require.Error(t, err, "invalid cathode_side")

	_, err = RenderDiode(canvas, DiodeParams{BodyColorHex: "not-a-color"})
	require.Error(t, err, "invalid body color hex")

	_, err = RenderDiode(canvas, DiodeParams{Markings: strings.Repeat("x", maxMarkingsLen+1)})
	require.Error(t, err, "markings too long")

	resAtLimit, err := RenderDiode(canvas, DiodeParams{Markings: strings.Repeat("x", maxMarkingsLen)})
	require.NoError(t, err, "markings exactly at the maximum length must be accepted")
	require.NotEmpty(t, resAtLimit.PNG)

	_, err = RenderDiode(canvas, DiodeParams{Markings: "café"})
	require.Error(t, err, "markings must be printable ASCII")

	res2, err := RenderDiode(canvas, DiodeParams{CathodeSide: "right", Markings: "1N4148"})
	require.NoError(t, err)
	require.NotEmpty(t, res2.PNG)
}
