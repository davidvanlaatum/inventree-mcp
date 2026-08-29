package render

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleGalleryDir is relative to this package, so it resolves correctly
// regardless of the working directory `go test` is invoked from.
const sampleGalleryDir = "../../docs/images/render-samples"

// TestRenderSamplesMatchCheckedInGallery is the project's visual
// regression check: none of the other render package tests inspect pixel
// content beyond a few structural spot checks (background fill, output
// dimensions, the rotation helper's own pixel mapping), so a rendering
// change that silently alters a family's actual artwork would otherwise
// pass every test. This re-renders every internal/render.Samples entry and
// compares it byte-for-byte against the checked-in gallery PNG that
// cmd/generate-render-samples produced. A mismatch means either the
// checked-in gallery is stale (regenerate it and review the diff) or the
// rendering change was unintentional.
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
			a := assert.New(t)

			result, err := sample.Render()
			r.NoError(err)

			path := sampleGalleryDir + "/" + sample.Name + ".png"
			want, err := os.ReadFile(path)
			r.NoErrorf(err, "missing checked-in gallery image %s; run `go generate ./internal/render/...` from internal/render and add the new file", path)

			a.Equalf(want, result.PNG, "%s no longer matches the checked-in gallery image at %s; if this rendering change is intentional, run `go generate ./internal/render/...`, review the visual diff, and commit the updated PNG and docs/render-samples.md together", sample.Name, path)
		})
	}
}
