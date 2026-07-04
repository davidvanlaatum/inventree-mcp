package platform

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

const defaultIDBytes = 16

type RandomSource interface {
	ReadRandom(context.Context, []byte) error
}

type CryptoRandomSource struct {
	Reader io.Reader
}

func (s CryptoRandomSource) ReadRandom(_ context.Context, out []byte) error {
	reader := s.Reader
	if reader == nil {
		reader = rand.Reader
	}
	_, err := io.ReadFull(reader, out)
	return err
}

type IDGenerator interface {
	NewID(context.Context) (string, error)
}

type RandomIDGenerator struct {
	Random RandomSource
	Bytes  int
}

func (g RandomIDGenerator) NewID(ctx context.Context) (string, error) {
	byteCount := g.Bytes
	if byteCount == 0 {
		byteCount = defaultIDBytes
	}
	if byteCount < 0 {
		return "", fmt.Errorf("ID byte count must be non-negative")
	}
	random := g.Random
	if random == nil {
		random = CryptoRandomSource{}
	}
	buf := make([]byte, byteCount)
	if err := random.ReadRandom(ctx, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
