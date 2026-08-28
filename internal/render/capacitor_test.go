package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatCapacitorMarking(t *testing.T) {
	a := assert.New(t)
	a.Equal("", formatCapacitorMarking(0, 0))
	a.Equal("100uF", formatCapacitorMarking(100, 0))
	a.Equal("100uF 16V", formatCapacitorMarking(100, 16))
	a.Equal("2.2uF 50V", formatCapacitorMarking(2.2, 50))
}

func TestRenderCapacitorValidation(t *testing.T) {
	canvas := CanvasOptions{Width: 150, Height: 300, Orientation: OrientationHorizontal, Background: BackgroundTransparent}

	res, err := RenderCapacitor(canvas, CapacitorParams{})
	require.NoError(t, err, "defaults should render with no markings")
	require.NotEmpty(t, res.PNG)

	_, err = RenderCapacitor(canvas, CapacitorParams{VoltageRatingV: 16})
	require.Error(t, err, "voltage without capacitance")

	_, err = RenderCapacitor(canvas, CapacitorParams{CapacitanceMicrofarads: -1})
	require.Error(t, err, "negative capacitance")

	_, err = RenderCapacitor(canvas, CapacitorParams{BodyColorHex: "#000000", NegativeStripeColorHex: "#000000"})
	require.Error(t, err, "identical body and stripe color")

	_, err = RenderCapacitor(canvas, CapacitorParams{BodyColorHex: "#abcdef", NegativeStripeColorHex: "#ABCDEF"})
	require.Error(t, err, "identical colors differing only by hex case must still be rejected")

	_, err = RenderCapacitor(canvas, CapacitorParams{NegativeSide: "top"})
	require.Error(t, err, "invalid negative_side")

	_, err = RenderCapacitor(canvas, CapacitorParams{DiameterMM: 5})
	require.Error(t, err, "dimensions must be supplied together")

	res2, err := RenderCapacitor(canvas, CapacitorParams{
		CapacitanceMicrofarads: 220, VoltageRatingV: 25, MarkingsText: "X7R",
		NegativeSide: "right", DiameterMM: 8, HeightMM: 11.5,
	})
	require.NoError(t, err)
	require.NotEmpty(t, res2.PNG)
}
