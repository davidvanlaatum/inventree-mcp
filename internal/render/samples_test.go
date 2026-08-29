package render

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleGalleryDir is relative to this package, so it resolves correctly
// regardless of the working directory `go test` is invoked from.
const sampleGalleryDir = "../../docs/images/render-samples"

// Rendering is deterministic within a single build (same Go toolchain,
// same GOARCH): identical input always produces byte-identical PNG bytes,
// verified by TestRenderDeterministic. Across different CPU architectures
// (observed: arm64 vs. amd64 builds of the exact same source), gg's
// floating-point anti-aliasing rasterizer can round a handful of edge
// pixels by a couple of least-significant bits differently — confirmed by
// reproducing the checked-in gallery under an emulated linux/amd64
// container: one sample differed in exactly one pixel out of 54,400, by 2
// out of 255 in a single color channel, invisible to the eye. That is
// architecture-level floating-point noise inherent to the underlying
// rasterizer, not a rendering defect, so this test compares decoded pixel
// content within a small bounded tolerance instead of raw PNG bytes.
const (
	maxDiffPixelFraction  = 0.001 // 0.1% of a sample's pixels may differ
	minDiffPixelAllowance = 4     // always allow a few pixels regardless of image size
	maxPerChannelDelta    = 8     // and only by this many of 255 per channel
)

// TestRenderSamplesMatchCheckedInGallery is the project's visual
// regression check: none of the other render package tests inspect pixel
// content beyond a few structural spot checks (background fill, output
// dimensions, the rotation helper's own pixel mapping), so a rendering
// change that silently alters a family's actual artwork would otherwise
// pass every test. This re-renders every internal/render.Samples entry and
// compares its decoded pixels, within the small tolerance above, against
// the checked-in gallery PNG that cmd/generate-render-samples produced. A
// failure means either the checked-in gallery is stale (regenerate it and
// review the diff) or the rendering change was unintentional; the two real
// bugs this test exists to catch (a third-party gradient-alpha defect and
// an invalid premultiplied color) each differed by large regions and a
// channel delta over 100, far outside this tolerance.
func TestRenderSamplesMatchCheckedInGallery(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	r.NotEmpty(Samples, "sample list must not be empty")

	seenNames := make(map[string]bool, len(Samples))
	for _, sample := range Samples {
		r.Falsef(seenNames[sample.Name], "duplicate sample name %q", sample.Name)
		seenNames[sample.Name] = true
	}

	for _, sample := range Samples {
		sample := sample
		t.Run(sample.Name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			result, err := sample.Render()
			r.NoError(err)

			path := sampleGalleryDir + "/" + sample.Name + ".png"
			want, err := os.ReadFile(path)
			r.NoErrorf(err, "missing checked-in gallery image %s; run `go generate ./internal/render/...` from internal/render and add the new file", path)

			assertImagesVisuallyEqual(t, want, result.PNG, sample.Name, path)
		})
	}
}

// assertImagesVisuallyEqual decodes both PNGs and compares pixels within
// the package-level tolerance constants, so it accepts the architecture-
// level floating-point rounding noise documented above while still
// catching real rendering regressions by a wide margin.
func assertImagesVisuallyEqual(t *testing.T, want, got []byte, name, path string) {
	t.Helper()
	r := require.New(t)
	a := assert.New(t)

	wantImg, err := png.Decode(bytes.NewReader(want))
	r.NoErrorf(err, "%s: checked-in gallery PNG failed to decode", name)
	gotImg, err := png.Decode(bytes.NewReader(got))
	r.NoErrorf(err, "%s: rendered PNG failed to decode", name)

	wantBounds, gotBounds := wantImg.Bounds(), gotImg.Bounds()
	r.Equalf(wantBounds, gotBounds, "%s: image dimensions differ", name)

	total := wantBounds.Dx() * wantBounds.Dy()
	maxAllowedDiffPixels := int(float64(total) * maxDiffPixelFraction)
	if maxAllowedDiffPixels < minDiffPixelAllowance {
		maxAllowedDiffPixels = minDiffPixelAllowance
	}

	diffPixels := 0
	maxDelta := 0
	var worst string
	for y := wantBounds.Min.Y; y < wantBounds.Max.Y; y++ {
		for x := wantBounds.Min.X; x < wantBounds.Max.X; x++ {
			delta := channelDelta(wantImg, gotImg, x, y)
			if delta == 0 {
				continue
			}
			diffPixels++
			if delta > maxDelta {
				maxDelta = delta
				worst = fmt.Sprintf("(%d,%d) delta=%d", x, y, delta)
			}
		}
	}

	a.LessOrEqualf(diffPixels, maxAllowedDiffPixels,
		"%s no longer matches the checked-in gallery image at %s: %d/%d pixels differ (allowed %d), worst %s; if this rendering change is intentional, run `go generate ./internal/render/...`, review the visual diff, and commit the updated PNG and docs/render-samples.md together",
		name, path, diffPixels, total, maxAllowedDiffPixels, worst)
	a.LessOrEqualf(maxDelta, maxPerChannelDelta,
		"%s no longer matches the checked-in gallery image at %s: a pixel differs by %d/255 in a channel (allowed %d), worst %s; this is larger than architecture-level anti-aliasing noise and likely a real rendering change — if intentional, run `go generate ./internal/render/...`, review the visual diff, and commit the updated PNG and docs/render-samples.md together",
		name, path, maxDelta, maxPerChannelDelta, worst)
}

// channelDelta returns the largest per-channel 8-bit difference between
// the two images at (x, y), across R, G, B, and A.
func channelDelta(a, b image.Image, x, y int) int {
	ar, ag, ab, aa := a.At(x, y).RGBA()
	br, bg, bb, ba := b.At(x, y).RGBA()
	max := 0
	for _, d := range []int{
		abs8(ar, br), abs8(ag, bg), abs8(ab, bb), abs8(aa, ba),
	} {
		if d > max {
			max = d
		}
	}
	return max
}

func abs8(a, b uint32) int {
	d := int(a>>8) - int(b>>8)
	if d < 0 {
		return -d
	}
	return d
}
