package render

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStraightRGBA(t *testing.T) {
	a := assert.New(t)

	// Fully opaque: straight and premultiplied values are identical.
	c := straightRGBA(0xf6, 0xf6, 0xf2, 0xff)
	a.Equal(color.RGBA{R: 0xf6, G: 0xf6, B: 0xf2, A: 0xff}, c)

	// This exact combination caused the fuse marking backing panel to
	// render near-black before straightRGBA existed: a direct
	// color.RGBA{R: 0xf6, G: 0xf6, B: 0xf2, A: 0xf0} literal is an invalid
	// premultiplied color because R/G/B (0xf6) exceed A (0xf0). The
	// correctly premultiplied result must have every component <= alpha.
	c = straightRGBA(0xf6, 0xf6, 0xf2, 0xf0)
	assertValidPremultiplied(t, c)
	a.Equal(uint8(0xf0), c.A)

	// Half alpha: components roughly halve.
	c = straightRGBA(0xff, 0x80, 0x00, 0x80)
	assertValidPremultiplied(t, c)
	a.InDelta(0x80, int(c.R), 1)
	a.InDelta(0x40, int(c.G), 1)
	a.Equal(uint8(0), c.B)

	// Zero alpha: every component must be zero, regardless of input RGB.
	c = straightRGBA(0xff, 0xff, 0xff, 0)
	a.Equal(color.RGBA{}, c)
}

// assertValidPremultiplied checks Go's color.RGBA invariant: every
// premultiplied component must be <= alpha. straightRGBA is the only
// permitted way to construct a non-opaque color.RGBA in this package
// specifically to guarantee this; a direct struct literal with alpha < 255
// can silently violate it (as fuseGlassColor and fuseMarkingBackColor once
// did), producing badly wrong blended pixels rather than a compile or
// runtime error.
func assertValidPremultiplied(t *testing.T, c color.RGBA) {
	t.Helper()
	r := require.New(t)
	r.LessOrEqualf(c.R, c.A, "R %d exceeds A %d: invalid premultiplied color", c.R, c.A)
	r.LessOrEqualf(c.G, c.A, "G %d exceeds A %d: invalid premultiplied color", c.G, c.A)
	r.LessOrEqualf(c.B, c.A, "B %d exceeds A %d: invalid premultiplied color", c.B, c.A)
}

func TestNoInvalidPremultipliedColorLiterals(t *testing.T) {
	// Every non-opaque color used by the package's templates must have
	// been built through straightRGBA rather than a direct struct
	// literal, so it can never violate the premultiplied invariant. This
	// pins the exact colors known to be exercised today; a new non-opaque
	// color.RGBA{...} literal added elsewhere in the package without
	// going through straightRGBA would not be caught by this test alone,
	// but every such color reachable from a render call is still
	// guarded end-to-end by TestRenderSamplesMatchCheckedInGallery.
	assertValidPremultiplied(t, fuseGlassColor)
	assertValidPremultiplied(t, fuseMarkingBackColor)
}
