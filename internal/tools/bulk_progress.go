package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/telemetry"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bulkProgressMinInterval throttles interior progress notifications so an
// operator-configured batch does not emit one MCP notification per item; the
// final (done == total) notification always bypasses the throttle so callers
// reliably see completion.
const bulkProgressMinInterval = 250 * time.Millisecond

// defaultBulkMaxItems and defaultBulkConcurrency preserve this codebase's
// pre-F-S81 fixed behavior for any Dependencies value that leaves the new
// operator-configurable BulkMaxItems/BulkConcurrency fields unset (zero).
// Real deployments always set both from validated Config (which rejects
// non-positive values outright), so these fallbacks only matter for
// Dependencies literals built directly — chiefly tests — that predate the
// two fields and would otherwise silently treat every batch as oversized.
const (
	defaultBulkMaxItems    = 25
	defaultBulkConcurrency = 4
)

// effectiveBulkMaxItems returns deps.BulkMaxItems, or defaultBulkMaxItems
// when it has not been explicitly configured.
func effectiveBulkMaxItems(deps Dependencies) int {
	if deps.BulkMaxItems > 0 {
		return deps.BulkMaxItems
	}
	return defaultBulkMaxItems
}

// effectiveBulkConcurrency returns deps.BulkConcurrency, or
// defaultBulkConcurrency when it has not been explicitly configured.
func effectiveBulkConcurrency(deps Dependencies) int {
	if deps.BulkConcurrency > 0 {
		return deps.BulkConcurrency
	}
	return defaultBulkConcurrency
}

// bulkProgressReporter delivers batch.ExecuteOptions.OnProgress callbacks as
// MCP progress notifications on the session that made the call, only when
// that call attached a progress token. A nil *bulkProgressReporter is always
// safe to call report on, so callers never need a separate nil check.
type bulkProgressReporter struct {
	mu       sync.Mutex
	session  *mcp.ServerSession
	token    any
	toolName string
	lastSent time.Time
}

// newBulkProgressReporter returns nil when req carries no session or no
// progress token, since there is then nothing to notify.
func newBulkProgressReporter(req *mcp.CallToolRequest, toolName string) *bulkProgressReporter {
	telemetry.RecordBulkOperation(context.Background(), toolName, "started")
	if req == nil || req.Session == nil || req.Params == nil {
		return nil
	}
	token := req.Params.GetProgressToken()
	if token == nil {
		return nil
	}
	return &bulkProgressReporter{session: req.Session, token: token, toolName: toolName}
}

// report is called concurrently from batch.Execute's worker goroutines via
// OnProgress. The lock is held across the NotifyProgress call itself (not
// just the throttle check) so concurrent reports serialize in the order they
// pass the throttle, instead of racing on delivery order over the wire.
func (r *bulkProgressReporter) report(ctx context.Context, done, total int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	final := done >= total
	if !final && now.Sub(r.lastSent) < bulkProgressMinInterval {
		return
	}
	r.lastSent = now

	err := r.session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
		ProgressToken: r.token,
		Message:       fmt.Sprintf("%s: %d/%d items complete", r.toolName, done, total),
		Progress:      float64(done),
		Total:         float64(total),
	})
	if err != nil {
		// Best-effort: a progress notification failure must never affect a
		// batch item's outcome or fail the tool call.
		logging.FromContext(ctx).WarnContext(ctx, "bulk progress notification failed", logging.Err(err))
	}
}

// BulkTimingEvidence reports aggregate client/orchestration time separately
// from aggregate upstream request time for one bulk call, without any
// per-item detail, so it stays bounded regardless of batch size. Orchestration
// is the batch's total wall-clock time; Upstream sums every item's own
// Preflight/Mutate/Verify duration independently, so Upstream normally
// exceeds Orchestration under concurrency — that gap is the throughput
// bounded concurrency buys. Concurrency is the number of workers actually
// engaged (min of the configured limit and ItemCount), not the raw
// configured limit, so it always matches what this specific call could have
// used.
type BulkTimingEvidence struct {
	OrchestrationMS int64 `json:"orchestration_ms"`
	UpstreamMS      int64 `json:"upstream_ms"`
	ItemCount       int   `json:"item_count"`
	Concurrency     int   `json:"concurrency"`
}

func bulkTimingEvidence(timing batch.Timing, itemCount, concurrencyLimit int) *BulkTimingEvidence {
	engaged := concurrencyLimit
	if itemCount < engaged {
		engaged = itemCount
	}
	return &BulkTimingEvidence{
		OrchestrationMS: timing.Orchestration.Milliseconds(),
		UpstreamMS:      timing.Upstream.Milliseconds(),
		ItemCount:       itemCount,
		Concurrency:     engaged,
	}
}
