package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomIDGeneratorUsesInjectedRandomSource(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	id, err := RandomIDGenerator{
		Random: fixedRandomSource{fill: 'a'},
		Bytes:  3,
	}.NewID(context.Background())

	r.NoError(err)
	r.Equal("YWFh", id)
}

func TestRandomIDGeneratorReportsRandomFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := RandomIDGenerator{
		Random: failingRandomSource{err: errors.New("boom")},
		Bytes:  3,
	}.NewID(context.Background())

	r.Error(err)
	r.Contains(err.Error(), "boom")
}

func TestRandomIDGeneratorRejectsNegativeByteCount(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := RandomIDGenerator{Bytes: -1}.NewID(context.Background())

	r.Error(err)
	r.Contains(err.Error(), "ID byte count")
}

func TestRedactionHelpersAvoidSecretValues(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	a.Equal("", RedactSecret(""))
	a.Equal("[REDACTED]", RedactSecret("secret-token"))
	a.Equal("[REDACTED]", RedactedAttr("token").Value.String())
}

type fixedRandomSource struct {
	fill byte
}

func (s fixedRandomSource) ReadRandom(_ context.Context, out []byte) error {
	for i := range out {
		out[i] = s.fill
	}
	return nil
}

type failingRandomSource struct {
	err error
}

func (s failingRandomSource) ReadRandom(context.Context, []byte) error {
	return s.err
}
