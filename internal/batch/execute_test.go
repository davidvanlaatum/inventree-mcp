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

	results, _ := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 3})

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

	results, _ := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 1})

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

	results, _ := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 1})

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

	results, _ := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 1})

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

	results, _ := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 1})

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

	results, _ := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 2})

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

	results, _ := batch.Execute(ctx, items, adapter, batch.ExecuteOptions{Concurrency: 2})

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
		results, _ := batch.Execute(ctx, items, adapter, batch.ExecuteOptions{Concurrency: 1})
		resultsCh <- results
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
		results, _ := batch.Execute(ctx, items, adapter, batch.ExecuteOptions{Concurrency: 1})
		resultsCh <- results
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
		results, _ := batch.Execute(ctx, items, adapter, batch.ExecuteOptions{Concurrency: 0})
		done <- results
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
	results, _ := batch.Execute(testContext(t), []itemPlan{}, adapter, batch.ExecuteOptions{Concurrency: 4})

	r.Empty(results)
	r.Empty(adapter.preflightCalls)
}

func TestExecuteOnProgressFiresOncePerItemEndingAtTotal(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	items := []itemPlan{{id: 0}, {id: 1}, {id: 2, mutateErr: errors.New("boom")}}
	adapter := &recordingAdapter{}

	var mu sync.Mutex
	var calls [][2]int
	onProgress := func(done, total int) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, [2]int{done, total})
	}

	results, _ := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 2, OnProgress: onProgress})

	r.Len(results, 3)
	r.Len(calls, 3, "OnProgress must fire exactly once per item, regardless of outcome")
	seenDone := make(map[int]bool)
	for _, call := range calls {
		r.Equal(3, call[1], "total must be the full item count on every call")
		seenDone[call[0]] = true
	}
	r.Equal(map[int]bool{1: true, 2: true, 3: true}, seenDone, "done must reach every value from 1 to total exactly once")
}

func TestExecuteOnProgressNilIsSafe(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	items := []itemPlan{{id: 0}, {id: 1}}
	adapter := &recordingAdapter{}

	r.NotPanics(func() {
		batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 2})
	})
}

func TestExecuteOnProgressCountsNotAttemptedCancelledItems(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, cancel := context.WithCancel(testContext(t))
	cancel()

	items := []itemPlan{{id: 0}, {id: 1}}
	adapter := &recordingAdapter{}

	var progressCalls atomic.Int64
	var lastDone, lastTotal atomic.Int64
	onProgress := func(done, total int) {
		progressCalls.Add(1)
		lastDone.Store(int64(done))
		lastTotal.Store(int64(total))
	}

	results, _ := batch.Execute(ctx, items, adapter, batch.ExecuteOptions{Concurrency: 2, OnProgress: onProgress})

	r.Len(results, 2)
	r.Equal(int64(2), progressCalls.Load(), "not-attempted items under an already-cancelled context must still be counted")
	r.Equal(int64(2), lastDone.Load())
	r.Equal(int64(2), lastTotal.Load())
}

func TestExecuteTimingSeparatesOrchestrationFromUpstream(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	const perItemDelay = 40 * time.Millisecond
	items := []itemPlan{
		{id: 0, delay: perItemDelay},
		{id: 1, delay: perItemDelay},
		{id: 2, delay: perItemDelay},
	}
	adapter := &recordingAdapter{}

	_, timing := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 3})

	r.GreaterOrEqual(timing.Upstream, 3*perItemDelay, "Upstream sums each item's own duration independently")
	r.Less(timing.Orchestration, 2*perItemDelay, "Orchestration should reflect concurrent wall time, not the summed upstream time")
	r.Greater(timing.Upstream, timing.Orchestration, "concurrent execution must make aggregate upstream time exceed wall-clock orchestration time")
}

func TestExecuteBoundedConcurrencyIndependentOfCallPacing(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	const itemCount = 6
	const perItemDelay = 30 * time.Millisecond
	newItems := func() []itemPlan {
		items := make([]itemPlan, itemCount)
		for i := range items {
			items[i] = itemPlan{id: i, delay: perItemDelay}
		}
		return items
	}

	_, sequentialTiming := batch.Execute(testContext(t), newItems(), &recordingAdapter{}, batch.ExecuteOptions{Concurrency: 1})
	_, concurrentTiming := batch.Execute(testContext(t), newItems(), &recordingAdapter{}, batch.ExecuteOptions{Concurrency: itemCount})

	r.Greater(sequentialTiming.Orchestration, concurrentTiming.Orchestration*2,
		"raising Concurrency alone must materially reduce wall time; throughput is governed by the concurrency setting, not by caller pacing")
}

func TestExecuteFastSlowMixedLatencyPreservesIndependentOutcomes(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	items := []itemPlan{
		{id: 0, delay: 0},
		{id: 1, delay: 50 * time.Millisecond},
		{id: 2, delay: 0, mutateErr: errors.New("boom")},
		{id: 3, delay: 50 * time.Millisecond},
	}
	adapter := &recordingAdapter{}

	start := time.Now()
	results, _ := batch.Execute(testContext(t), items, adapter, batch.ExecuteOptions{Concurrency: 4})
	elapsed := time.Since(start)

	r.Len(results, 4)
	r.Equal(batch.OutcomeApplied, results[0].Outcome)
	r.Equal(batch.OutcomeApplied, results[1].Outcome)
	r.Equal(batch.OutcomeAmbiguous, results[2].Outcome)
	r.Equal(batch.OutcomeApplied, results[3].Outcome)
	r.Less(elapsed, 200*time.Millisecond, "a fast item must not be held up by a slower sibling under sufficient concurrency")
}
