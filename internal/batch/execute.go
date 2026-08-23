package batch

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"golang.org/x/sync/errgroup"
)

// Outcome classifies the final disposition of one batch item after Execute
// runs it through an Adapter.
type Outcome string

const (
	// OutcomeApplied means Mutate and Verify both succeeded: the mutation
	// happened and read-back confirms it matches the reviewed plan.
	OutcomeApplied Outcome = "applied"
	// OutcomeSkipped means Preflight found nothing to do: the item was
	// already in its target state.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeFailed means Preflight found a definite, verified reason the
	// mutation cannot safely proceed. No write was attempted.
	OutcomeFailed Outcome = "failed"
	// OutcomeAmbiguous means Mutate was attempted but returned an error, so
	// whether the upstream write partially applied is unknown. Callers must
	// not retry blindly; a fresh dry run is required to see current state.
	OutcomeAmbiguous Outcome = "ambiguous"
	// OutcomeUnverified means either Mutate succeeded but Verify could not
	// confirm the result (Attempted is true), or the item was never
	// attempted because the batch's context was already cancelled or past
	// its deadline before the item's turn (Attempted is false).
	OutcomeUnverified Outcome = "unverified"
)

// Result records one item's outcome from Execute. Results are returned in
// the same order as the input items, regardless of completion order.
type Result[T any] struct {
	Item T
	// Outcome classifies what happened to this item.
	Outcome Outcome
	// Attempted is false only when Outcome is OutcomeUnverified because the
	// batch was cancelled or its deadline expired before this item's turn;
	// it is true for every other outcome, including a Verify failure.
	Attempted bool
	// Message is a fixed, caller-safe description of the outcome. It never
	// contains raw adapter error text; real errors are logged, not
	// returned, except for Preflight's own reason string, which the
	// adapter is responsible for keeping safe to surface.
	Message string
	// RecoveryPlan is a fixed, non-blind-retry recovery instruction, present
	// for every outcome except OutcomeApplied and OutcomeSkipped.
	RecoveryPlan string
}

// Adapter performs the three steps Execute needs for one batch item of type
// T: Preflight re-validates that the item's already-planned target values
// are still safe to apply (returning skip=true for a genuine no-op, or a
// definite err for an unsafe drift); Mutate performs the write using values
// already fixed in the item at plan-build time; Verify reads the item back
// and confirms it matches the plan. No data flows from Preflight into
// Mutate — Mutate must not depend on Preflight's read, or a
// preflight-passed/mutate-still-stale race reappears.
type Adapter[T any] interface {
	Preflight(ctx context.Context, item T) (skip bool, reason string, err error)
	Mutate(ctx context.Context, item T) error
	Verify(ctx context.Context, item T) error
}

// ExecuteOptions bounds Execute's concurrency.
type ExecuteOptions struct {
	// Concurrency is the maximum number of items processed at once. Values
	// less than 1 are clamped to 1.
	Concurrency int
}

const (
	notAttemptedMessage      = "not attempted: batch context was cancelled or its deadline expired before this item's turn"
	notAttemptedRecovery     = "Re-run a fresh dry run for the remaining items; this item was never touched."
	failedPreflightRecovery  = "No write was attempted. Prepare a fresh dry run for this item once the reported condition is resolved."
	ambiguousMessage         = "mutation attempted; result unknown"
	ambiguousRecovery        = "Do not retry blindly. Prepare a fresh dry run and inspect current state before deciding what, if anything, still needs to change."
	unverifiedAttemptMessage = "mutation may have completed but could not be confirmed"
	unverifiedRecovery       = "Do not retry blindly. Prepare a fresh dry run and inspect current state before deciding what, if anything, still needs to change."
)

// Execute runs items through adapter with bounded concurrency, giving every
// item an independent outcome: one item's failure never stops, skips, or
// cancels its siblings, because batch items are expected to be safely
// independent — that is the premise of offering a bulk tool at all. Execute
// itself claims no cross-item atomicity; see the package doc.
//
// If ctx is already cancelled or past its deadline before an item's turn,
// that item is not attempted at all and is reported as OutcomeUnverified
// with Attempted:false. In-flight items still receive ctx, so their own
// upstream calls fail through their normal cancellation handling.
func Execute[T any](ctx context.Context, items []T, adapter Adapter[T], opts ExecuteOptions) []Result[T] {
	results := make([]Result[T], len(items))
	concurrency := opts.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	var group errgroup.Group
	group.SetLimit(concurrency)

	for index, item := range items {
		group.Go(func() error {
			// Re-check cancellation here, not before dispatch: SetLimit can
			// block this closure from starting until a worker slot frees,
			// and cancellation may happen during that wait. Checking only
			// before Go() would miss it and still launch the adapter.
			if ctx.Err() != nil {
				results[index] = Result[T]{
					Item:         item,
					Outcome:      OutcomeUnverified,
					Attempted:    false,
					Message:      notAttemptedMessage,
					RecoveryPlan: notAttemptedRecovery,
				}
				return nil
			}
			results[index] = runOne(ctx, adapter, item)
			return nil
		})
	}
	_ = group.Wait()

	return results
}

func runOne[T any](ctx context.Context, adapter Adapter[T], item T) Result[T] {
	skip, reason, err := adapter.Preflight(ctx, item)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "batch item preflight failed", logging.Err(err))
		return Result[T]{Item: item, Outcome: OutcomeFailed, Attempted: true, Message: reason, RecoveryPlan: failedPreflightRecovery}
	}
	if skip {
		return Result[T]{Item: item, Outcome: OutcomeSkipped, Attempted: true, Message: reason}
	}

	if err := adapter.Mutate(ctx, item); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "batch item mutation failed", logging.Err(err), slog.String("error_type", fmt.Sprintf("%T", err)))
		return Result[T]{Item: item, Outcome: OutcomeAmbiguous, Attempted: true, Message: ambiguousMessage, RecoveryPlan: ambiguousRecovery}
	}

	if err := adapter.Verify(ctx, item); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "batch item verification failed", logging.Err(err), slog.String("error_type", fmt.Sprintf("%T", err)))
		return Result[T]{Item: item, Outcome: OutcomeUnverified, Attempted: true, Message: unverifiedAttemptMessage, RecoveryPlan: unverifiedRecovery}
	}

	return Result[T]{Item: item, Outcome: OutcomeApplied, Attempted: true}
}
