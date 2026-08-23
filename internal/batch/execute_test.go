package batch_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	return ctx
}

type itemPlan struct {
	id           int
	preflightErr error
	skip         bool
	skipReason   string
	mutateErr    error
	verifyErr    error
	delay        time.Duration
	// mutateStarted, if non-nil, is closed the instant Mutate begins, before
	// any delay or mutateProceed wait. mutateProceed, if non-nil, blocks
	// Mutate until it is closed. Together these let a test deterministically
	// interleave an action (like cancelling the batch context) strictly
	// between one item's Mutate starting and finishing, without depending on
	// wall-clock timing.
	mutateStarted chan struct{}
	mutateProceed chan struct{}
}

type recordingAdapter struct {
	mu             sync.Mutex
	preflightCalls []int
	mutateCalls    []int
	verifyCalls    []int
	inFlight       atomic.Int64
	maxInFlight    atomic.Int64
}

func (a *recordingAdapter) Preflight(_ context.Context, item itemPlan) (bool, string, error) {
	a.mu.Lock()
	a.preflightCalls = append(a.preflightCalls, item.id)
	a.mu.Unlock()
	return item.skip, item.skipReason, item.preflightErr
}

func (a *recordingAdapter) Mutate(_ context.Context, item itemPlan) error {
	a.trackInFlight()
	defer a.inFlight.Add(-1)

	a.mu.Lock()
	a.mutateCalls = append(a.mutateCalls, item.id)
	a.mu.Unlock()

	if item.mutateStarted != nil {
		close(item.mutateStarted)
	}
	if item.mutateProceed != nil {
		<-item.mutateProceed
	}
	if item.delay > 0 {
		time.Sleep(item.delay)
	}
	return item.mutateErr
}

func (a *recordingAdapter) trackInFlight() {
	current := a.inFlight.Add(1)
	for {
		previous := a.maxInFlight.Load()
		if current <= previous || a.maxInFlight.CompareAndSwap(previous, current) {
			return
		}
	}
}

func (a *recordingAdapter) Verify(_ context.Context, item itemPlan) error {
	a.mu.Lock()
	a.verifyCalls = append(a.verifyCalls, item.id)
	a.mu.Unlock()
	return item.verifyErr
}

func TestExecuteAllSuccessPreservesInputOrder(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	items := []itemPlan{
		{id: 0, delay: 30 * time.Millisecond},
		{id: 1},
		{id: 2},
	}
	adapter := &recordingAdapter{}

	results := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 3})

	r.Len(results, 3)
	for i, result := range results {
		r.Equal(i, result.Item.id)
		r.Equal(batch.OutcomeApplied, result.Outcome)
		r.True(result.Attempted)
	}
}

func TestExecuteSkipsWithoutMutateOrVerify(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	items := []itemPlan{{id: 0, skip: true, skipReason: "already at target"}}
	adapter := &recordingAdapter{}

	results := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 1})

	r.Len(results, 1)
	r.Equal(batch.OutcomeSkipped, results[0].Outcome)
	r.Equal("already at target", results[0].Message)
	r.Empty(adapter.mutateCalls)
	r.Empty(adapter.verifyCalls)
}

func TestExecutePreflightErrorFailsWithoutMutateOrVerify(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	items := []itemPlan{{id: 0, preflightErr: errors.New("unsafe drift")}}
	adapter := &recordingAdapter{}

	results := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 1})

	r.Len(results, 1)
	r.Equal(batch.OutcomeFailed, results[0].Outcome)
	r.True(results[0].Attempted)
	r.NotEmpty(results[0].RecoveryPlan)
	r.Empty(adapter.mutateCalls)
	r.Empty(adapter.verifyCalls)
}

func TestExecuteMutateErrorIsAmbiguousAndSkipsVerify(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	items := []itemPlan{{id: 0, mutateErr: errors.New("upstream write failed")}}
	adapter := &recordingAdapter{}

	results := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 1})

	r.Len(results, 1)
	r.Equal(batch.OutcomeAmbiguous, results[0].Outcome)
	r.True(results[0].Attempted)
	r.NotEmpty(results[0].RecoveryPlan)
	r.NotContains(results[0].Message, "upstream write failed", "raw adapter error text must never reach Result.Message")
	r.Empty(adapter.verifyCalls)
}

func TestExecuteVerifyErrorIsUnverifiedButAttempted(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	items := []itemPlan{{id: 0, verifyErr: errors.New("read-back mismatch")}}
	adapter := &recordingAdapter{}

	results := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 1})

	r.Len(results, 1)
	r.Equal(batch.OutcomeUnverified, results[0].Outcome)
	r.True(results[0].Attempted)
	r.NotContains(results[0].Message, "read-back mismatch")
}

func TestExecuteBoundsConcurrency(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	const itemCount = 10
	const concurrency = 3
	items := make([]itemPlan, itemCount)
	for i := range items {
		items[i] = itemPlan{id: i, delay: 20 * time.Millisecond}
	}
	adapter := &recordingAdapter{}

	batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: concurrency})

	r.LessOrEqual(adapter.maxInFlight.Load(), int64(concurrency))
	r.Equal(int64(concurrency), adapter.maxInFlight.Load(), "with more items than the concurrency limit, the limit should actually be reached")
}

func TestExecuteOneItemFailureDoesNotAffectSiblings(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	items := []itemPlan{
		{id: 0, mutateErr: errors.New("boom")},
		{id: 1},
		{id: 2, preflightErr: errors.New("unsafe")},
		{id: 3},
	}
	adapter := &recordingAdapter{}

	results := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 2})

	a.Len(results, 4)
	a.Equal(batch.OutcomeAmbiguous, results[0].Outcome)
	a.Equal(batch.OutcomeApplied, results[1].Outcome)
	a.Equal(batch.OutcomeFailed, results[2].Outcome)
	a.Equal(batch.OutcomeApplied, results[3].Outcome)
	// Every item was at least preflighted, proving one item's failure never
	// short-circuited a sibling's attempt (no accidental stop-on-first-failure
	// or errgroup.WithContext-style cross-cancellation).
	a.ElementsMatch([]int{0, 1, 2, 3}, adapter.preflightCalls)
}

func TestExecuteAlreadyCancelledContextAttemptsNothing(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, cancel := context.WithCancel(testContext(t))
	cancel()

	items := []itemPlan{{id: 0}, {id: 1}}
	adapter := &recordingAdapter{}

	results := batch.Execute(ctx, items, adapter, batch.ExecuteOptions{Concurrency: 2})

	r.Len(results, 2)
	for _, result := range results {
		r.Equal(batch.OutcomeUnverified, result.Outcome)
		r.False(result.Attempted)
	}
	r.Empty(adapter.preflightCalls)
}

func TestExecuteCancellationPartwayThroughStopsLaterItems(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// started/proceed pin cancellation strictly between item 0's Mutate
	// starting and finishing, deterministically — no wall-clock race against
	// item 1's dispatch, which (at Concurrency:1) cannot even be attempted
	// until item 0's goroutine releases its worker slot.
	started := make(chan struct{})
	proceed := make(chan struct{})
	ctx, cancel := context.WithCancel(testContext(t))
	items := []itemPlan{
		{id: 0, mutateStarted: started, mutateProceed: proceed},
		{id: 1},
	}
	adapter := &recordingAdapter{}

	resultsCh := make(chan []batch.Result[itemPlan], 1)
	go func() {
		resultsCh <- batch.Execute(ctx, items, adapter, batch.ExecuteOptions{Concurrency: 1})
	}()

	<-started
	cancel()
	close(proceed)
	results := <-resultsCh

	r.Len(results, 2)
	r.True(results[0].Attempted, "item 0 had already started before cancellation and should still finish with a real outcome")
	r.False(results[1].Attempted, "item 1 had not started before cancellation and must not be launched")
	r.Equal(batch.OutcomeUnverified, results[1].Outcome)
}

func TestExecuteDeadlineExceededMidBatchAttemptsNoFutureItems(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	started := make(chan struct{})
	proceed := make(chan struct{})
	ctx, cancel := context.WithTimeout(testContext(t), 20*time.Millisecond)
	defer cancel()
	items := []itemPlan{
		{id: 0, mutateStarted: started, mutateProceed: proceed},
		{id: 1},
	}
	adapter := &recordingAdapter{}

	resultsCh := make(chan []batch.Result[itemPlan], 1)
	go func() {
		resultsCh <- batch.Execute(ctx, items, adapter, batch.ExecuteOptions{Concurrency: 1})
	}()

	<-started
	// Block until the real deadline has actually elapsed, then let item 0
	// finish, proving the deadline-expiry path (not just manual Cancel)
	// stops item 1 from being attempted. ctx.Err() treats DeadlineExceeded
	// and Canceled identically, but this exercises the real timer path.
	<-ctx.Done()
	close(proceed)
	results := <-resultsCh

	r.Len(results, 2)
	r.True(results[0].Attempted)
	r.False(results[1].Attempted, "item 1 must not be attempted once the batch deadline has expired")
	r.Equal(batch.OutcomeUnverified, results[1].Outcome)
	r.ErrorIs(ctx.Err(), context.DeadlineExceeded)
}

func TestExecuteZeroConcurrencyDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, cancel := context.WithTimeout(testContext(t), 2*time.Second)
	defer cancel()

	items := []itemPlan{{id: 0}, {id: 1}}
	adapter := &recordingAdapter{}

	done := make(chan []batch.Result[itemPlan], 1)
	go func() {
		done <- batch.Execute(ctx, items, adapter, batch.ExecuteOptions{Concurrency: 0})
	}()

	select {
	case results := <-done:
		r.Len(results, 2)
	case <-ctx.Done():
		t.Fatal("Execute with Concurrency:0 deadlocked instead of clamping to 1")
	}
}

func TestExecuteEmptyItemsInvokesNothing(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	adapter := &recordingAdapter{}
	results := batch.Execute(testContext(t), []itemPlan{}, adapter, batch.ExecuteOptions{Concurrency: 4})

	r.Empty(results)
	r.Empty(adapter.preflightCalls)
}
