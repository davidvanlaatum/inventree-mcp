package tools

import (
	"context"
	"sync"
)

// Outcome is the closed vocabulary used to classify how a tool invocation
// completed in structured completion logs. It intentionally excludes
// upstream error detail so completion records stay safe to log without
// per-tool redaction review.
type Outcome string

const (
	OutcomeSuccess              Outcome = "success"
	OutcomeValidationFailure    Outcome = "validation_failure"
	OutcomeAuthorizationFailure Outcome = "authorization_failure"
	OutcomeUpstreamFailure      Outcome = "upstream_failure"
	OutcomeCancellation         Outcome = "cancellation"
	OutcomeInternalFailure      Outcome = "internal_failure"
)

type outcomeRecorderKey struct{}

// outcomeRecorder is a mutable box referenced through an immutable context
// value. It lets deeply nested handlers (safeToolError, GuardTool's scope
// check) classify the eventual completion outcome without threading a
// classification return value through every intervening call site.
type outcomeRecorder struct {
	mu      sync.Mutex
	outcome Outcome
	set     bool
}

// WithOutcomeRecorder attaches a fresh outcome recorder to ctx. It must be
// called once per tool invocation, before the invocation's handler chain
// runs, so RecordOutcome and OutcomeFromContext observe the same recorder.
func WithOutcomeRecorder(ctx context.Context) context.Context {
	return context.WithValue(ctx, outcomeRecorderKey{}, &outcomeRecorder{})
}

// RecordOutcome sets the closed-vocabulary outcome classification for the
// current tool invocation. The most specific classifier on the call path
// should call this; if none does, the caller falls back to a safe
// IsError-derived default. A missing recorder (no invocation in progress,
// such as in a unit test that does not call WithOutcomeRecorder) is a no-op.
func RecordOutcome(ctx context.Context, outcome Outcome) {
	recorder, ok := ctx.Value(outcomeRecorderKey{}).(*outcomeRecorder)
	if !ok {
		return
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.outcome = outcome
	recorder.set = true
}

// OutcomeFromContext returns the explicitly recorded outcome for the current
// tool invocation, if any classifier has called RecordOutcome.
func OutcomeFromContext(ctx context.Context) (Outcome, bool) {
	recorder, ok := ctx.Value(outcomeRecorderKey{}).(*outcomeRecorder)
	if !ok {
		return "", false
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.outcome, recorder.set
}
