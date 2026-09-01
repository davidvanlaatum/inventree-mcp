package requestctx

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextCallSequenceStartsAtOneAndIncrementsPerCall(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := WithCorrelation(context.Background())

	first, ok := NextCallSequence(ctx)
	r.True(ok)
	r.Equal(1, first)

	second, ok := NextCallSequence(ctx)
	r.True(ok)
	r.Equal(2, second)
}

func TestNextCallSequenceResetsPerInvocation(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	first := WithCorrelation(context.Background())
	sequence, ok := NextCallSequence(first)
	r.True(ok)
	r.Equal(1, sequence)
	_, ok = NextCallSequence(first)
	r.True(ok)

	second := WithCorrelation(context.Background())
	sequence, ok = NextCallSequence(second)
	r.True(ok)
	r.Equal(1, sequence, "a fresh WithCorrelation context must not share state with an earlier invocation")
}

func TestNextCallSequenceReturnsFalseWithoutCorrelation(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	sequence, ok := NextCallSequence(context.Background())

	a.False(ok)
	a.Equal(0, sequence)
}

func TestNextCallSequenceCapsAtMaxCallSequence(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := WithCorrelation(context.Background())

	for i := 1; i <= maxCallSequence; i++ {
		sequence, ok := NextCallSequence(ctx)
		r.True(ok, "call %d should still be within the cap", i)
		r.Equal(i, sequence)
	}

	sequence, ok := NextCallSequence(ctx)
	r.False(ok, "the call past the cap must be refused")
	r.Equal(0, sequence)

	sequence, ok = NextCallSequence(ctx)
	r.False(ok, "the cap must stay refused on subsequent calls, not reopen")
	r.Equal(0, sequence)
}

func TestNextCallSequenceIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := WithCorrelation(context.Background())

	const goroutines = 16
	seen := make(chan int, goroutines*4)
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 4 {
				if sequence, ok := NextCallSequence(ctx); ok {
					seen <- sequence
				}
			}
		}()
	}
	wg.Wait()
	close(seen)

	unique := make(map[int]struct{})
	for sequence := range seen {
		_, duplicate := unique[sequence]
		r.False(duplicate, "sequence %d was assigned more than once", sequence)
		unique[sequence] = struct{}{}
	}
	r.Len(unique, goroutines*4)
}

func TestCallSequenceOverflowedFiresOnceAfterExhaustingTheCap(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := WithCorrelation(context.Background())
	for i := 0; i < maxCallSequence; i++ {
		_, ok := NextCallSequence(ctx)
		r.True(ok)
	}
	_, ok := NextCallSequence(ctx)
	r.False(ok)

	r.True(CallSequenceOverflowed(ctx), "the first observation past the cap must report the overflow")
	r.False(CallSequenceOverflowed(ctx), "a later observation must not report the overflow again")
	r.False(CallSequenceOverflowed(ctx), "the overflow marker must stay latched, not toggle")
}

func TestCallSequenceOverflowedReturnsFalseWithoutCorrelation(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	a.False(CallSequenceOverflowed(context.Background()))
}

func TestHasCorrelationReportsPresence(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	a.False(HasCorrelation(context.Background()))
	a.True(HasCorrelation(WithCorrelation(context.Background())))
}
