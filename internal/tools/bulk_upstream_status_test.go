package tools

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// bulkStatusFakeTransport is a real *inventree.Client backed by a
// deterministic fake http.RoundTripper (no Docker, no httptest.Server),
// following category_admin_tools_test.go's categoryRoundTripFunc pattern.
// It lets tests inject a specific upstream HTTP status on a chosen GET call
// (identified by 1-based call order) and/or on every PATCH call, to exercise
// real *inventree.APIError classification through bulk_update_parts.
type bulkStatusFakeTransport struct {
	mu sync.Mutex

	getCalls int
	// getFailOnCall, if > 0, makes that 1-based GET call return getFailStatus
	// instead of a successful part-detail body. Every other GET call succeeds.
	getFailOnCall int
	getFailStatus int

	patchCalls int
	// patchFailStatus, if > 0, makes every PATCH call return this status
	// instead of succeeding.
	patchFailStatus int
}

func (f *bulkStatusFakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path != "/api/part/1/" {
		return bulkStatusJSONResponse(req, http.StatusNotFound, `{}`), nil
	}
	switch req.Method {
	case http.MethodGet:
		f.mu.Lock()
		f.getCalls++
		call := f.getCalls
		f.mu.Unlock()
		if f.getFailOnCall > 0 && call == f.getFailOnCall {
			return bulkStatusJSONResponse(req, f.getFailStatus, `{"detail":"injected failure"}`), nil
		}
		return bulkStatusJSONResponse(req, http.StatusOK, `{"pk":1,"name":"Widget","active":true}`), nil
	case http.MethodPatch:
		f.mu.Lock()
		f.patchCalls++
		f.mu.Unlock()
		if f.patchFailStatus > 0 {
			return bulkStatusJSONResponse(req, f.patchFailStatus, `{"detail":"injected failure"}`), nil
		}
		return bulkStatusJSONResponse(req, http.StatusOK, `{"pk":1,"name":"Widget","active":false}`), nil
	default:
		return bulkStatusJSONResponse(req, http.StatusMethodNotAllowed, `{}`), nil
	}
}

func bulkStatusJSONResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func newBulkStatusTransportClient(t *testing.T, transport http.RoundTripper) *inventree.Client {
	t.Helper()
	client, err := inventree.NewClient(inventree.Config{
		BaseURL:    "https://inventree.example",
		Credential: inventree.Credential{Scheme: inventree.AuthSchemeToken, Token: "test-token"},
		HTTPClient: &http.Client{Transport: transport},
	})
	require.NoError(t, err)
	return client
}

func bulkUpdatePartsDryRunThenConfirm(t *testing.T, ctx context.Context, deps Dependencies) (BulkUpdateOutput[PartDetailView], BulkUpdateOutput[PartDetailView], error) {
	t.Helper()
	r := require.New(t)
	handler := bulkUpdateParts(deps)
	items := []BulkUpdatePartItem{{ID: 1, Active: dvgoutils.Ptr(false)}}

	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, DryRun: true})
	r.NoError(err)
	r.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	return dryOut, confirmOut, err
}

func TestBulkUpdatePartsUpstream429DuringMutateIsAmbiguousNotRetried(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	transport := &bulkStatusFakeTransport{patchFailStatus: http.StatusTooManyRequests}
	client := newBulkStatusTransportClient(t, transport)
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }, BulkMaxItems: 25, BulkConcurrency: 4}
	deps.partBulkPlanStore = mustBulkStore(batch.Options[partBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p partBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})

	_, confirmOut, err := bulkUpdatePartsDryRunThenConfirm(t, ctx, deps)

	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	r.Equal(string(batch.OutcomeAmbiguous), confirmOut.Items[0].Outcome)
	r.Equal(1, transport.patchCalls, "a 429 mutate failure must not be automatically retried")
}

func TestBulkUpdatePartsUpstream408DuringPreflightIsFailedNotAmbiguous(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	// GET call 1 is the dry-run's plan-build read, GET call 2 is confirm's own
	// plan-rebuild read (needed to match the dry-run plan digest), and GET
	// call 3 is the adapter's Preflight re-read, which this test fails with 408.
	transport := &bulkStatusFakeTransport{getFailOnCall: 3, getFailStatus: http.StatusRequestTimeout}
	client := newBulkStatusTransportClient(t, transport)
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }, BulkMaxItems: 25, BulkConcurrency: 4}
	deps.partBulkPlanStore = mustBulkStore(batch.Options[partBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p partBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})

	_, confirmOut, err := bulkUpdatePartsDryRunThenConfirm(t, ctx, deps)

	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	r.Equal(string(batch.OutcomeFailed), confirmOut.Items[0].Outcome, "a Preflight read failure must fail closed without attempting a write")
	r.Equal(0, transport.patchCalls, "no write should be attempted when Preflight's re-read fails")
}

func TestBulkUpdatePartsUpstream425DuringVerifyIsUnverified(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	// GET calls 1 (dry-run plan build), 2 (confirm plan rebuild), and 3
	// (Preflight re-read) succeed; the PATCH succeeds; GET call 4 (Verify's
	// read-back) fails with 425.
	transport := &bulkStatusFakeTransport{getFailOnCall: 4, getFailStatus: http.StatusTooEarly}
	client := newBulkStatusTransportClient(t, transport)
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }, BulkMaxItems: 25, BulkConcurrency: 4}
	deps.partBulkPlanStore = mustBulkStore(batch.Options[partBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p partBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})

	_, confirmOut, err := bulkUpdatePartsDryRunThenConfirm(t, ctx, deps)

	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	r.Equal(string(batch.OutcomeUnverified), confirmOut.Items[0].Outcome)
	r.Equal(1, transport.patchCalls, "the mutation itself must have been attempted exactly once")
}

func TestBulkUpdatePartsMixedLatencyFakeTransportReportsTiming(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	const itemDelay = 30 * time.Millisecond
	transport := &delayedPartTransport{delay: itemDelay}
	client := newBulkStatusTransportClient(t, transport)
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }, BulkMaxItems: 25, BulkConcurrency: 4}
	deps.partBulkPlanStore = mustBulkStore(batch.Options[partBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p partBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})

	handler := bulkUpdateParts(deps)
	items := []BulkUpdatePartItem{{ID: 1, Active: dvgoutils.Ptr(false)}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.NotNil(confirmOut.Timing)
	r.GreaterOrEqual(confirmOut.Timing.UpstreamMS, itemDelay.Milliseconds())
	r.Equal(1, confirmOut.Timing.ItemCount)
	r.Equal(1, confirmOut.Timing.Concurrency, "Concurrency reports workers actually engaged (min of the configured limit and item count), not the raw configured limit")
}

// delayedPartTransport always succeeds but sleeps delay before every
// response, to give Timing.UpstreamMS/OrchestrationMS a real, non-zero,
// deterministic-lower-bound value without Docker or network variance.
type delayedPartTransport struct {
	delay time.Duration
}

func (f *delayedPartTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	time.Sleep(f.delay)
	switch req.Method {
	case http.MethodGet:
		return bulkStatusJSONResponse(req, http.StatusOK, `{"pk":1,"name":"Widget","active":true}`), nil
	case http.MethodPatch:
		return bulkStatusJSONResponse(req, http.StatusOK, `{"pk":1,"name":"Widget","active":false}`), nil
	default:
		return bulkStatusJSONResponse(req, http.StatusMethodNotAllowed, `{}`), nil
	}
}
