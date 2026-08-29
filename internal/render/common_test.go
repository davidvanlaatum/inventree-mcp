package render

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVerticalLayoutBand(t *testing.T) {
	a := assert.New(t)

	top, bottom := verticalLayoutBand(200, false, 0)
	a.Equal(0.0, top)
	a.Equal(200.0, bottom)

	top, bottom = verticalLayoutBand(200, true, 0)
	a.Equal(40.0, top, "value line reserves 20% of height")
	a.Equal(200.0, bottom)

	top, bottom = verticalLayoutBand(200, false, 2)
	a.Equal(0.0, top)
	a.InDelta(200*(1-0.32), bottom, 0.001, "2 caption lines reserve 8%+2*12%=32%")

	// The caption fraction is clamped so a large line count never
	// collapses the component area to nothing.
	_, bottomManyLines := verticalLayoutBand(200, false, 20)
	_, bottomFewerLines := verticalLayoutBand(200, false, 5)
	a.Greater(bottomManyLines, 0.0)
	a.LessOrEqual(bottomManyLines, bottomFewerLines)
}

func TestFitFontSize(t *testing.T) {
	a := assert.New(t)

	a.Equal(minTextPoints, fitFontSize("", 1000, 1000, false), "empty text returns the minimum")

	small := fitFontSize("hello", 40, 40, false)
	large := fitFontSize("hello", 400, 400, false)
	a.GreaterOrEqual(small, minTextPoints)
	a.LessOrEqual(large, maxTextPoints)
	a.Greaterf(large, small, "more available space must not shrink the chosen point size")

	w, h := measureText("hello", large, false)
	a.LessOrEqualf(w, 400.0, "chosen size must fit the requested width")
	a.LessOrEqualf(h, 400.0, "chosen size must fit the requested height")
}

func TestMixColor(t *testing.T) {
	a := assert.New(t)
	red := color.RGBA{R: 0xff, A: 0xff}
	blue := color.RGBA{B: 0xff, A: 0xff}

	a.Equal(red, mixColor(red, blue, 0), "t=0 returns the original color")
	a.Equal(blue, mixColor(red, blue, 1), "t=1 returns the target color")

	mid := mixColor(red, blue, 0.5)
	a.InDelta(0x7f, int(mid.R), 1)
	a.InDelta(0x7f, int(mid.B), 1)
	a.Equal(uint8(0xff), mid.A, "mixColor preserves the source alpha")
}
