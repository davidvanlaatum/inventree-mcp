// Package batch provides shared primitives for future resource- or
// operation-specific bulk MCP tools: a generic single-use confirmation-token
// store (Store) and a bounded-concurrency executor with independent
// per-item outcomes (Execute).
//
// Bulk execution here means concurrent orchestration of independent
// requests, not upstream atomicity. Execute makes no cross-item transaction
// guarantee: a batch can partially apply, and callers must not assume that
// one item's failure prevented or will prevent any other item's mutation.
// This package intentionally does not expose a generic arbitrary-record
// PATCH tool; every resource-specific bulk tool built on it (see F-S77
// through F-S81 in docs/TASKS.md) keeps its own typed schema, field
// allowlist, and safety checks and implements Adapter to plug into Execute.
//
// # Building a batch tool on this package
//
// A resource-specific plan type embeds an ordered slice of items, each
// carrying the values captured when the plan was built (mirroring the
// existing single-item StockAdjustmentPlan.Before/.After shape, just as a
// slice) and implements Digestible via JSONDigest so Store can bind a
// confirmation token to the plan's complete current state. The typical
// tool-level flow is:
//
//  1. dry_run: build the ordered plan, call Store.Issue to get a token, and
//     return it without writing anything.
//  2. confirm + token: call Store.Consume; a false result means the plan is
//     stale, changed, reordered, or belongs to a different principal, and
//     the caller must return the existing structured-clarification "prepare
//     a fresh dry run" response rather than proceeding.
//  3. On a successful Consume, call Execute with the plan's items and a
//     resource-specific Adapter, then translate each Result into the tool's
//     own output shape.
//
// This package deliberately has no dependency on MCP, HTTP, or InvenTree
// client types: Principal is injected via Options so callers supply their
// own auth-context extraction (see internal/tools' existing
// stockPlanPrincipal for the convention this generalizes), and Adapter
// deals only in caller-defined item types.
//
// # The Adapter contract
//
// Preflight re-fetches current state and compares it to the target values
// already fixed in the item at plan-build time. It returns skip:true for a
// genuine no-op (nothing needs to change), or a non-nil err for a definite,
// unsafe drift (Execute reports this as OutcomeFailed; no write is
// attempted). No data flows from Preflight into Mutate: Mutate must use
// only values already baked into the item when the plan was built, never a
// value read during Preflight, or a preflight-passed/mutate-still-stale
// TOCTOU race reappears between the two calls. Verify performs an exact
// read-back and confirms it matches the plan; a Verify failure after a
// successful Mutate is reported as OutcomeUnverified, not OutcomeFailed,
// because the mutation may have applied even though it could not be
// confirmed.
package batch
