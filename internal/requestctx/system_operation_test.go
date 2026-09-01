package requestctx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemOperationRoundTrips(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	ctx := WithSystemOperation(context.Background(), "startup_check")

	name, ok := SystemOperation(ctx)
	a.True(ok)
	a.Equal("startup_check", name)
}

func TestSystemOperationReturnsFalseWhenUnset(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	name, ok := SystemOperation(context.Background())
	a.False(ok)
	a.Empty(name)
}

func TestWithSystemOperationIgnoresEmptyName(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	ctx := WithSystemOperation(context.Background(), "")

	_, ok := SystemOperation(ctx)
	a.False(ok)
}
