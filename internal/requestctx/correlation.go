package requestctx

import (
	"context"
	"sync"
)

// maxCallSequence bounds how many outbound InvenTree API calls one tool
// invocation logs in detail. It must stay small and fixed so a runaway or
// adversarial multi-call tool cannot produce unbounded log volume.
const maxCallSequence = 64

type correlationKey struct{}

// correlation is a mutable box referenced through an immutable context value,
// scoped to one tool invocation. It assigns a bounded, monotonically
// increasing sequence number to each outbound InvenTree API call the
// invocation makes, so a multi-call tool's log records can be correlated and
// ordered without every call site threading a counter through its own
// parameters.
type correlation struct {
	mu             sync.Mutex
	sequence       int
	overflowLogged bool
}

// WithCorrelation attaches a fresh call-sequence counter to ctx. It must be
// called once per tool invocation, before any outbound InvenTree API call
// runs, so NextCallSequence observations share one counter for the
// invocation's lifetime.
func WithCorrelation(ctx context.Context) context.Context {
	return context.WithValue(ctx, correlationKey{}, &correlation{})
}

// HasCorrelation reports whether ctx carries a call-sequence counter,
// distinguishing "no invocation is tracking call sequence" (log without a
// sequence number) from "the cap has been reached" (NextCallSequence
// returns ok=false either way, so callers that need to tell these apart
// must check this first).
func HasCorrelation(ctx context.Context) bool {
	_, found := ctx.Value(correlationKey{}).(*correlation)
	return found
}

// NextCallSequence atomically assigns the next call-sequence number for the
// current invocation, starting at 1. Once maxCallSequence is reached, it
// returns ok=false and the caller must stop emitting detailed per-call
// fields for the remainder of the invocation; request execution itself must
// not change. A missing correlation (no invocation in progress, such as a
// unit test that does not call WithCorrelation) also returns ok=false.
func NextCallSequence(ctx context.Context) (sequence int, ok bool) {
	c, found := ctx.Value(correlationKey{}).(*correlation)
	if !found {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sequence >= maxCallSequence {
		return 0, false
	}
	c.sequence++
	return c.sequence, true
}

// CallSequenceOverflowed reports whether this is the first observation of
// the call-sequence cap being exceeded for the current invocation. Callers
// must emit exactly one bounded overflow marker log when this returns true,
// and none on later calls within the same invocation.
func CallSequenceOverflowed(ctx context.Context) bool {
	c, found := ctx.Value(correlationKey{}).(*correlation)
	if !found {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.overflowLogged {
		return false
	}
	c.overflowLogged = true
	return true
}
