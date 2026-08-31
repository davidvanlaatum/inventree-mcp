package platform

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/require"
)

var hexRequestID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestRequestIDGeneratorProducesFixedLowercaseHex(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	id, err := RequestIDGenerator{Random: fixedRandomSource{fill: 0xab}}.NewRequestID(ctx)

	r.NoError(err)
	r.Regexp(hexRequestID, id)
	r.Equal(strings.Repeat("ab", 16), id)
}

func TestRequestIDGeneratorIsDeterministicForFixedRandomness(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	first, err := RequestIDGenerator{Random: fixedRandomSource{fill: 0x11}}.NewRequestID(ctx)
	r.NoError(err)
	second, err := RequestIDGenerator{Random: fixedRandomSource{fill: 0x11}}.NewRequestID(ctx)
	r.NoError(err)

	r.Equal(first, second)
}

func TestRequestIDGeneratorProducesUniqueIDsFromDistinctRandomness(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	first, err := RequestIDGenerator{}.NewRequestID(ctx)
	r.NoError(err)
	second, err := RequestIDGenerator{}.NewRequestID(ctx)
	r.NoError(err)

	r.Regexp(hexRequestID, first)
	r.Regexp(hexRequestID, second)
	r.NotEqual(first, second)
}

func TestRequestIDGeneratorReportsRandomFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	_, err := RequestIDGenerator{Random: failingRandomSource{err: errors.New("boom")}}.NewRequestID(ctx)

	r.Error(err)
	r.Contains(err.Error(), "boom")
}
