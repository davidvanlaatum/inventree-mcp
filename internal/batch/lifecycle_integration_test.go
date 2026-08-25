//go:build !no_integration_tests

package batch_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/stretchr/testify/require"
)

// inventoryItem and inventoryPlan model a realistic resource-specific batch
// plan the way a future F-S77+ tool would shape one: an ordered set of
// items, each carrying the state captured when the plan was built plus its
// target value.
type inventoryItem struct {
	ID          int
	BeforeState string
	TargetState string
}

type inventoryPlan struct {
	Operation string
	Items     []inventoryItem
}

func (p inventoryPlan) Digest() (string, error) {
	return batch.JSONDigest(p)
}

// fakeInventory is a minimal in-memory stand-in for an upstream API: the
// adapter reads and writes through it exactly like a real InvenTree client
// call would.
type fakeInventory struct {
	mu       sync.Mutex
	state    map[int]string
	failOnID map[int]bool
}

func newFakeInventory(initial map[int]string) *fakeInventory {
	return &fakeInventory{state: initial, failOnID: map[int]bool{}}
}

func (f *fakeInventory) get(id int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state[id]
}

func (f *fakeInventory) set(id int, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOnID[id] {
		return errors.New("simulated upstream write failure")
	}
	f.state[id] = value
	return nil
}

type inventoryAdapter struct {
	inventory *fakeInventory
}

func (a inventoryAdapter) Preflight(_ context.Context, item inventoryItem) (bool, string, error) {
	current := a.inventory.get(item.ID)
	if current == item.TargetState {
		return true, "already at target state", nil
	}
	if current != item.BeforeState {
		return false, "", errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a inventoryAdapter) Mutate(_ context.Context, item inventoryItem) error {
	return a.inventory.set(item.ID, item.TargetState)
}

func (a inventoryAdapter) Verify(_ context.Context, item inventoryItem) error {
	if a.inventory.get(item.ID) != item.TargetState {
		return errors.New("read-back does not match the reviewed plan")
	}
	return nil
}

func newLifecycleStore(t *testing.T) *batch.Store[inventoryPlan] {
	t.Helper()
	r := require.New(t)
	store, err := batch.NewStore(batch.Options[inventoryPlan]{
		IDGenerator: platform.RandomIDGenerator{},
		Principal:   func(context.Context) string { return "oauth:test-operator" },
		SupersedeKey: func(p inventoryPlan) string {
			return p.Operation
		},
	})
	r.NoError(err)
	return store
}

func TestLifecycleIssueConsumeExecuteSuccessfulBatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := testContext(t)

	inventory := newFakeInventory(map[int]string{1: "OK", 2: "OK", 3: "OK"})
	plan := inventoryPlan{Operation: "set-status", Items: []inventoryItem{
		{ID: 1, BeforeState: "OK", TargetState: "Attention"},
		{ID: 2, BeforeState: "OK", TargetState: "Attention"},
		{ID: 3, BeforeState: "OK", TargetState: "Attention"},
	}}
	store := newLifecycleStore(t)

	token, err := store.Issue(ctx, plan)
	r.NoError(err)
	r.True(store.Consume(ctx, token, plan))

	results, _ := batch.Execute(ctx, plan.Items, inventoryAdapter{inventory: inventory}, batch.ExecuteOptions{Concurrency: 3})

	r.Len(results, 3)
	for _, result := range results {
		r.Equal(batch.OutcomeApplied, result.Outcome)
	}
	r.Equal("Attention", inventory.get(1))
	r.Equal("Attention", inventory.get(2))
	r.Equal("Attention", inventory.get(3))
}

func TestLifecycleRejectsStalePlanAfterUpstreamChange(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := testContext(t)

	plan := inventoryPlan{Operation: "set-status", Items: []inventoryItem{
		{ID: 1, BeforeState: "OK", TargetState: "Attention"},
	}}
	store := newLifecycleStore(t)

	token, err := store.Issue(ctx, plan)
	r.NoError(err)

	// The operator reviewed a two-item batch, but the tool is now asked to
	// confirm a one-item batch under the same token: this must be rejected
	// exactly like a token minted for a different set of items.
	changedPlan := inventoryPlan{Operation: "set-status", Items: []inventoryItem{
		{ID: 1, BeforeState: "Attention", TargetState: "OK"},
	}}
	r.False(store.Consume(ctx, token, changedPlan), "a plan that drifted since the dry run must not be confirmable with the old token")

	// The original, unmodified plan is still confirmable.
	r.True(store.Consume(ctx, token, plan))
}

func TestLifecycleCancellationMidBatchLeavesLaterItemsUnattempted(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := testContext(t)

	inventory := newFakeInventory(map[int]string{1: "OK", 2: "OK"})
	plan := inventoryPlan{Operation: "set-status", Items: []inventoryItem{
		{ID: 1, BeforeState: "OK", TargetState: "Attention"},
		{ID: 2, BeforeState: "OK", TargetState: "Attention"},
	}}
	store := newLifecycleStore(t)

	token, err := store.Issue(ctx, plan)
	r.NoError(err)
	r.True(store.Consume(ctx, token, plan))

	// started/proceed pin cancellation strictly between item 1's Mutate
	// starting and finishing, deterministically — no wall-clock race.
	started := make(chan struct{})
	proceed := make(chan struct{})
	gatedAdapter := gatedInventoryAdapter{
		inventoryAdapter: inventoryAdapter{inventory: inventory},
		gateItemID:       1,
		started:          started,
		proceed:          proceed,
	}
	execCtx, cancel := context.WithCancel(ctx)

	resultsCh := make(chan []batch.Result[inventoryItem], 1)
	go func() {
		results, _ := batch.Execute(execCtx, plan.Items, gatedAdapter, batch.ExecuteOptions{Concurrency: 1})
		resultsCh <- results
	}()

	<-started
	cancel()
	close(proceed)
	results := <-resultsCh

	r.Len(results, 2)
	r.True(results[0].Attempted)
	r.False(results[1].Attempted, "item 2 should never have been reached once the batch context was cancelled")
	r.Equal(batch.OutcomeUnverified, results[1].Outcome)
}

// gatedInventoryAdapter blocks Mutate on gateItemID until proceed is closed,
// signalling via started the instant Mutate begins, so a test can
// deterministically interleave an action between one item's Mutate starting
// and finishing without depending on wall-clock timing.
type gatedInventoryAdapter struct {
	inventoryAdapter
	gateItemID int
	started    chan struct{}
	proceed    chan struct{}
}

func (a gatedInventoryAdapter) Mutate(ctx context.Context, item inventoryItem) error {
	if item.ID == a.gateItemID {
		close(a.started)
		<-a.proceed
	}
	return a.inventoryAdapter.Mutate(ctx, item)
}

func TestLifecycleMixedPartialFailureBatchReportsIndependentOutcomes(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := testContext(t)

	inventory := newFakeInventory(map[int]string{
		1: "OK",        // will apply cleanly
		2: "Attention", // already at target: skip
		3: "OK",        // upstream write fails: ambiguous
		4: "Damaged",   // drifted since the dry run: failed at preflight
	})
	inventory.failOnID[3] = true

	plan := inventoryPlan{Operation: "set-status", Items: []inventoryItem{
		{ID: 1, BeforeState: "OK", TargetState: "Attention"},
		{ID: 2, BeforeState: "OK", TargetState: "Attention"},
		{ID: 3, BeforeState: "OK", TargetState: "Attention"},
		{ID: 4, BeforeState: "OK", TargetState: "Attention"},
	}}
	store := newLifecycleStore(t)

	token, err := store.Issue(ctx, plan)
	r.NoError(err)
	r.True(store.Consume(ctx, token, plan))

	results, _ := batch.Execute(ctx, plan.Items, inventoryAdapter{inventory: inventory}, batch.ExecuteOptions{Concurrency: 2})

	r.Len(results, 4)
	r.Equal(batch.OutcomeApplied, results[0].Outcome)
	r.Equal(batch.OutcomeSkipped, results[1].Outcome)
	r.Equal(batch.OutcomeAmbiguous, results[2].Outcome)
	r.Equal(batch.OutcomeFailed, results[3].Outcome)

	// Item 1 applying and item 3 failing are independent: neither prevented
	// the other, and item 4's unsafe drift didn't stop item 1 or item 2.
	r.Equal("Attention", inventory.get(1))
	r.Equal("Attention", inventory.get(2))
	r.Equal("OK", inventory.get(3), "the failed write must not have silently applied")
	r.Equal("Damaged", inventory.get(4), "a preflight-failed item must never be mutated")
}
