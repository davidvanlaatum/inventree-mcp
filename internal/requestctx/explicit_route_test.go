package requestctx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExplicitRouteRoundTrips(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	ctx := WithExplicitRoute(context.Background(), "download_part_image_content", "image_download")

	operation, family, ok := ExplicitRoute(ctx)
	a.True(ok)
	a.Equal("download_part_image_content", operation)
	a.Equal("image_download", family)
}

func TestExplicitRouteReturnsFalseWhenUnset(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	operation, family, ok := ExplicitRoute(context.Background())
	a.False(ok)
	a.Empty(operation)
	a.Empty(family)
}
