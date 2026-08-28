package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatFuseMarking(t *testing.T) {
	a := assert.New(t)
	a.Equal("F 1A", formatFuseMarking("fast", 1, 0))
	a.Equal("T 500mA", formatFuseMarking("slow", 0.5, 0))
	a.Equal("F 2.5A 250V", formatFuseMarking("fast", 2.5, 250))
}

func TestRenderFuseValidation(t *testing.T) {
	canvas := validResistorCanvas()

	_, err := RenderFuse(canvas, FuseParams{RatingAmps: 0})
	require.Error(t, err, "zero rating")

	_, err = RenderFuse(canvas, FuseParams{RatingAmps: 1, RatingVoltage: -5})
	require.Error(t, err, "negative voltage")

	_, err = RenderFuse(canvas, FuseParams{RatingAmps: 1, Speed: "medium"})
	require.Error(t, err, "invalid speed")

	_, err = RenderFuse(canvas, FuseParams{RatingAmps: 1, Size: "9x40mm"})
	require.Error(t, err, "invalid size")

	res, err := RenderFuse(canvas, FuseParams{RatingAmps: 1.5, RatingVoltage: 250, Speed: "slow", Size: "6x30mm"})
	require.NoError(t, err)
	require.NotEmpty(t, res.PNG)
}
