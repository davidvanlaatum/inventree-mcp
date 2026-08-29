package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeResistorBands4Band(t *testing.T) {
	cases := []struct {
		ohms   float64
		digits []int
		exp    int
	}{
		{100, []int{1, 0}, 1},
		{220, []int{2, 2}, 1},
		{4700, []int{4, 7}, 2},
		{10000, []int{1, 0}, 3},
		{1000000, []int{1, 0}, 5},
		{2.2, []int{2, 2}, -1},
		{0.47, []int{4, 7}, -2},
	}
	for _, c := range cases {
		digits, exp, err := encodeResistorBands(c.ohms, 4)
		require.NoErrorf(t, err, "ohms=%v", c.ohms)
		assert.Equalf(t, c.digits, digits, "ohms=%v digits", c.ohms)
		assert.Equalf(t, c.exp, exp, "ohms=%v exponent", c.ohms)
	}
}

func TestEncodeResistorBands5Band(t *testing.T) {
	digits, exp, err := encodeResistorBands(4700, 5)
	require.NoError(t, err)
	assert.Equal(t, []int{4, 7, 0}, digits)
	assert.Equal(t, 1, exp)

	digits, exp, err = encodeResistorBands(499, 5)
	require.NoError(t, err)
	assert.Equal(t, []int{4, 9, 9}, digits)
	assert.Equal(t, 0, exp)
}

func TestEncodeResistorBandsRejectsNonDecadeValues(t *testing.T) {
	_, _, err := encodeResistorBands(4712, 4)
	require.Error(t, err, "4712 needs 3 significant digits, not representable with 4-band's 2")

	_, _, err = encodeResistorBands(123456789, 5)
	require.Error(t, err, "out of representable multiplier range")
}

func TestRenderResistorValidation(t *testing.T) {
	canvas := validResistorCanvas()

	_, err := RenderResistor(canvas, ResistorParams{ResistanceOhms: 0, ToleranceLabel: "5%"})
	require.Error(t, err, "zero resistance")

	_, err = RenderResistor(canvas, ResistorParams{ResistanceOhms: -100, ToleranceLabel: "5%"})
	require.Error(t, err, "negative resistance")

	_, err = RenderResistor(canvas, ResistorParams{ResistanceOhms: 100, ToleranceLabel: "3%"})
	require.Error(t, err, "unsupported tolerance label")

	_, err = RenderResistor(canvas, ResistorParams{ResistanceOhms: 100, ToleranceLabel: "5%", BandCount: 6})
	require.Error(t, err, "unsupported band count")

	_, err = RenderResistor(canvas, ResistorParams{ResistanceOhms: 100, ToleranceLabel: "5%", BodyLengthMM: 6})
	require.Error(t, err, "dimensions must be supplied together")

	_, err = RenderResistor(canvas, ResistorParams{ResistanceOhms: 4712, ToleranceLabel: "5%", BandCount: 5})
	require.Error(t, err, "4712 is not exactly representable with 5-band's 3 significant digits either")

	res, err := RenderResistor(canvas, ResistorParams{ResistanceOhms: 4700, ToleranceLabel: "5%", BandCount: 4, BodyLengthMM: 6.6, BodyDiameterMM: 2.5})
	require.NoError(t, err)
	require.NotEmpty(t, res.PNG)
}

func TestMultiplierColor(t *testing.T) {
	assert.Equal(t, colorGold, multiplierColor(-1))
	assert.Equal(t, colorSilver, multiplierColor(-2))
	assert.Equal(t, digitColors[0], multiplierColor(0))
	assert.Equal(t, digitColors[9], multiplierColor(9))
}

func TestSizeForPowerRating(t *testing.T) {
	cases := []struct {
		watts float64
		want  string
	}{
		{0.125, "small"},
		{0.2, "small"},
		{0.21, "medium"},
		{0.25, "medium"},
		{0.6, "medium"},
		{0.61, "large"},
		{1, "large"},
		{5, "large"},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, sizeForPowerRating(c.watts), "watts=%v", c.watts)
	}
}

func TestResistorExplicitSizeOverridesPowerRating(t *testing.T) {
	r := require.New(t)
	canvas := validResistorCanvas()
	params := ResistorParams{ResistanceOhms: 100, ToleranceLabel: "5%"}

	// A large wattage alone picks the "large" preset.
	withWattageOnly, err := RenderResistor(canvas, mergeResistorParams(params, ResistorParams{PowerRatingWatts: 5}))
	r.NoError(err)
	large, err := RenderResistor(canvas, mergeResistorParams(params, ResistorParams{Size: "large"}))
	r.NoError(err)
	r.Equal(large.SHA256, withWattageOnly.SHA256, "power_rating_watts alone must resolve to the same body as size: large")

	// An explicit small size must win over a large wattage: the drawn
	// body must exactly match a plain small-size render (dimension-level
	// check, not just "the hash differs from something else").
	small, err := RenderResistor(canvas, mergeResistorParams(params, ResistorParams{Size: "small"}))
	r.NoError(err)
	explicitSmallWithLargeWattage, err := RenderResistor(canvas, mergeResistorParams(params, ResistorParams{Size: "small", PowerRatingWatts: 5}))
	r.NoError(err)
	r.Equal(small.SHA256, explicitSmallWithLargeWattage.SHA256, "explicit size must override a power_rating_watts-derived preset")
}

// mergeResistorParams overlays extra's non-zero fields onto base's shared
// resistance_ohms/tolerance_label, so each ResistorParams case above states
// only what it's actually testing.
func mergeResistorParams(base, extra ResistorParams) ResistorParams {
	out := base
	out.Size = extra.Size
	out.PowerRatingWatts = extra.PowerRatingWatts
	return out
}
