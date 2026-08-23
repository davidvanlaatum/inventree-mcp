package batch_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/stretchr/testify/require"
)

type testPlan struct {
	Operation string
	Items     []string
}

func (p testPlan) Digest() (string, error) {
	return batch.JSONDigest(p)
}

// undigestiblePlan implements Digestible with a Digest that always fails,
// exercising the store's error-propagation path when a plan cannot be
// hashed (for example if a real plan type's JSONDigest hit an unmarshalable
// field).
type undigestiblePlan struct{}

func (undigestiblePlan) Digest() (string, error) {
	return "", errors.New("cannot digest this plan")
}

type fixedClock struct {
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	return c.now
}

func (c *fixedClock) Since(t time.Time) time.Duration {
	return c.now.Sub(t)
}

type sequentialIDGenerator struct {
	mu   sync.Mutex
	next int
}

func (g *sequentialIDGenerator) NewID(context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next++
	return "token-" + strconv.Itoa(g.next), nil
}

type failingIDGenerator struct {
	err error
}

func (g failingIDGenerator) NewID(context.Context) (string, error) {
	return "", g.err
}

func principalFor(name string) func(context.Context) string {
	return func(context.Context) string { return name }
}

func supersedeByOperation(plan testPlan) string {
	return plan.Operation
}

func newTestStore(t *testing.T, clock *fixedClock, principal func(context.Context) string) *batch.Store[testPlan] {
	t.Helper()
	r := require.New(t)
	store, err := batch.NewStore(batch.Options[testPlan]{
		Clock:        clock,
		IDGenerator:  &sequentialIDGenerator{},
		Principal:    principal,
		SupersedeKey: supersedeByOperation,
	})
	r.NoError(err)
	return store
}

func TestStoreRequiresIDGeneratorPrincipalAndSupersedeKey(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := batch.NewStore(batch.Options[testPlan]{Principal: principalFor("a"), SupersedeKey: supersedeByOperation})
	r.Error(err)

	_, err = batch.NewStore(batch.Options[testPlan]{IDGenerator: &sequentialIDGenerator{}, SupersedeKey: supersedeByOperation})
	r.Error(err)

	_, err = batch.NewStore(batch.Options[testPlan]{IDGenerator: &sequentialIDGenerator{}, Principal: principalFor("a")})
	r.Error(err)

	_, err = batch.NewStore(batch.Options[testPlan]{IDGenerator: &sequentialIDGenerator{}, Principal: principalFor("a"), SupersedeKey: supersedeByOperation})
	r.NoError(err)
}

func TestStoreIssueConsumeIsSingleUse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store := newTestStore(t, clock, principalFor("alice"))

	plan := testPlan{Operation: "op", Items: []string{"a", "b"}}
	token, err := store.Issue(ctx, plan)
	r.NoError(err)
	r.NotEmpty(token)

	r.True(store.Consume(ctx, token, plan))
	r.False(store.Consume(ctx, token, plan))
}

func TestStoreConsumeRejectsStalePlan(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store := newTestStore(t, clock, principalFor("alice"))

	plan := testPlan{Operation: "op", Items: []string{"a", "b"}}
	token, err := store.Issue(ctx, plan)
	r.NoError(err)

	changed := testPlan{Operation: "op", Items: []string{"a", "c"}}
	r.False(store.Consume(ctx, token, changed))
	r.True(store.Consume(ctx, token, plan), "an unmodified plan should still be consumable after a failed attempt")
}

func TestStoreConsumeRejectsReorderedItems(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store := newTestStore(t, clock, principalFor("alice"))

	plan := testPlan{Operation: "op", Items: []string{"a", "b"}}
	token, err := store.Issue(ctx, plan)
	r.NoError(err)

	reordered := testPlan{Operation: "op", Items: []string{"b", "a"}}
	r.False(store.Consume(ctx, token, reordered))
}

func TestStoreConsumeRejectsWrongPrincipal(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	plan := testPlan{Operation: "op", Items: []string{"a"}}

	// Principal is read from context per call, so one store instance can
	// serve multiple principals; issue as alice and attempt to consume as
	// bob to prove the token is principal-bound, not just token-bound.
	multiPrincipalStore, err := batch.NewStore(batch.Options[testPlan]{
		Clock:       clock,
		IDGenerator: &sequentialIDGenerator{},
		Principal: func(ctx context.Context) string {
			if v, ok := ctx.Value(principalContextKey{}).(string); ok {
				return v
			}
			return "unknown"
		},
		SupersedeKey: supersedeByOperation,
	})
	r.NoError(err)

	issueCtx := context.WithValue(ctx, principalContextKey{}, "alice")
	token, err := multiPrincipalStore.Issue(issueCtx, plan)
	r.NoError(err)

	consumeCtx := context.WithValue(ctx, principalContextKey{}, "bob")
	r.False(multiPrincipalStore.Consume(consumeCtx, token, plan))
	r.True(multiPrincipalStore.Consume(issueCtx, token, plan))
}

type principalContextKey struct{}

func TestStoreConsumeRejectsExpiredToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store, err := batch.NewStore(batch.Options[testPlan]{
		Clock:        clock,
		IDGenerator:  &sequentialIDGenerator{},
		Principal:    principalFor("alice"),
		SupersedeKey: supersedeByOperation,
		Lifetime:     time.Minute,
	})
	r.NoError(err)

	plan := testPlan{Operation: "op", Items: []string{"a"}}
	token, err := store.Issue(ctx, plan)
	r.NoError(err)

	clock.now = clock.now.Add(time.Minute)
	r.False(store.Consume(ctx, token, plan))
}

func TestStoreIssueSupersedesEarlierTokenForSameKey(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store := newTestStore(t, clock, principalFor("alice"))

	plan := testPlan{Operation: "op", Items: []string{"a"}}
	firstToken, err := store.Issue(ctx, plan)
	r.NoError(err)

	secondToken, err := store.Issue(ctx, plan)
	r.NoError(err)
	r.NotEqual(firstToken, secondToken)

	r.False(store.Consume(ctx, firstToken, plan), "issuing a fresh token for the same principal+key must invalidate the earlier one")
	r.True(store.Consume(ctx, secondToken, plan))
}

func TestStoreIssueEnforcesGlobalCapacity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store, err := batch.NewStore(batch.Options[testPlan]{
		Clock:                  clock,
		IDGenerator:            &sequentialIDGenerator{},
		Principal:              principalFor("alice"),
		SupersedeKey:           func(p testPlan) string { return p.Operation },
		MaxEntries:             2,
		MaxEntriesPerPrincipal: 64,
	})
	r.NoError(err)

	_, err = store.Issue(ctx, testPlan{Operation: "op-1"})
	r.NoError(err)
	_, err = store.Issue(ctx, testPlan{Operation: "op-2"})
	r.NoError(err)
	_, err = store.Issue(ctx, testPlan{Operation: "op-3"})
	r.ErrorIs(err, batch.ErrCapacity)
}

func TestStoreIssueEnforcesPerPrincipalCapacity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store, err := batch.NewStore(batch.Options[testPlan]{
		Clock:                  clock,
		IDGenerator:            &sequentialIDGenerator{},
		Principal:              principalFor("alice"),
		SupersedeKey:           func(p testPlan) string { return p.Operation },
		MaxEntries:             64,
		MaxEntriesPerPrincipal: 1,
	})
	r.NoError(err)

	_, err = store.Issue(ctx, testPlan{Operation: "op-1"})
	r.NoError(err)
	_, err = store.Issue(ctx, testPlan{Operation: "op-2"})
	r.ErrorIs(err, batch.ErrCapacity)
}

func TestStoreIssueSurfacesIDGeneratorFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	boom := errors.New("boom")
	store, err := batch.NewStore(batch.Options[testPlan]{
		Clock:        clock,
		IDGenerator:  failingIDGenerator{err: boom},
		Principal:    principalFor("alice"),
		SupersedeKey: supersedeByOperation,
	})
	r.NoError(err)

	_, err = store.Issue(ctx, testPlan{Operation: "op"})
	r.ErrorIs(err, boom)
}

func TestStoreIssueIgnoresOtherPrincipalsEntriesForSupersessionAndCapacity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	multiPrincipalStore, err := batch.NewStore(batch.Options[testPlan]{
		Clock:       clock,
		IDGenerator: &sequentialIDGenerator{},
		Principal: func(ctx context.Context) string {
			if v, ok := ctx.Value(principalContextKey{}).(string); ok {
				return v
			}
			return "unknown"
		},
		SupersedeKey:           supersedeByOperation,
		MaxEntriesPerPrincipal: 1,
	})
	r.NoError(err)

	plan := testPlan{Operation: "op", Items: []string{"a"}}
	aliceCtx := context.WithValue(ctx, principalContextKey{}, "alice")
	aliceToken, err := multiPrincipalStore.Issue(aliceCtx, plan)
	r.NoError(err)

	// Bob issuing for the same operation must neither be blocked by alice's
	// per-principal capacity nor supersede alice's still-valid token.
	bobCtx := context.WithValue(ctx, principalContextKey{}, "bob")
	bobToken, err := multiPrincipalStore.Issue(bobCtx, plan)
	r.NoError(err)
	r.NotEqual(aliceToken, bobToken)

	r.True(multiPrincipalStore.Consume(aliceCtx, aliceToken, plan))
	r.True(multiPrincipalStore.Consume(bobCtx, bobToken, plan))
}

func TestJSONDigestReportsMarshalFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := batch.JSONDigest(make(chan int))
	r.Error(err)
}

func TestStoreIssueAndConsumeSurfaceDigestFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store, err := batch.NewStore(batch.Options[undigestiblePlan]{
		Clock:        clock,
		IDGenerator:  &sequentialIDGenerator{},
		Principal:    principalFor("alice"),
		SupersedeKey: func(undigestiblePlan) string { return "op" },
	})
	r.NoError(err)

	_, err = store.Issue(ctx, undigestiblePlan{})
	r.Error(err)

	r.False(store.Consume(ctx, "any-token", undigestiblePlan{}))
}

func TestStoreConcurrentIssueAndConsume(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store, err := batch.NewStore(batch.Options[testPlan]{
		Clock:                  clock,
		IDGenerator:            &sequentialIDGenerator{},
		Principal:              principalFor("alice"),
		SupersedeKey:           func(p testPlan) string { return p.Operation },
		MaxEntries:             1000,
		MaxEntriesPerPrincipal: 1000,
	})
	r.NoError(err)

	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			plan := testPlan{Operation: "op", Items: []string{string(rune('a' + i))}}
			token, issueErr := store.Issue(ctx, plan)
			if issueErr != nil {
				return
			}
			store.Consume(ctx, token, plan)
		}()
	}
	wg.Wait()
}
