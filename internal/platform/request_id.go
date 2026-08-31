package platform

import (
	"context"
	"encoding/hex"
	"fmt"
)

// requestIDBytes yields a fixed 32-character lowercase hex request ID, the
// format F-S95 requires so multi-call tool sequences can be correlated in
// logs without the ID doubling as identity or authentication material.
const requestIDBytes = 16

// RequestIDGenerator produces the server-generated opaque request-correlation
// ID embedded once per tool invocation. It reuses the same injectable
// RandomSource seam as RandomIDGenerator so tests can supply deterministic or
// failing randomness instead of crypto/rand.
type RequestIDGenerator struct {
	Random RandomSource
}

// NewRequestID returns a fresh 32-character lowercase hex request ID, or an
// error if the underlying randomness source failed. Callers must fail the
// invocation closed on error rather than proceeding without a request ID.
func (g RequestIDGenerator) NewRequestID(ctx context.Context) (string, error) {
	random := g.Random
	if random == nil {
		random = CryptoRandomSource{}
	}
	buf := make([]byte, requestIDBytes)
	if err := random.ReadRandom(ctx, buf); err != nil {
		return "", fmt.Errorf("generate request id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
