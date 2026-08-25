package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"regexp"
	"sync"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// connCountingTransport counts how many underlying TCP connections its
// requests use (via httptrace's GotConn.Reused), independent of request
// count, to prove batch.Execute's concurrent workers share the underlying
// *inventree.Client's connection pool rather than opening one connection per
// request or per item.
type connCountingTransport struct {
	base http.RoundTripper

	mu      sync.Mutex
	total   int
	reused  int
	newConn int
}

func (t *connCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.total++
			if info.Reused {
				t.reused++
			} else {
				t.newConn++
			}
		},
	}
	return t.base.RoundTrip(req.WithContext(httptrace.WithClientTrace(req.Context(), trace)))
}

var bulkConnReusePartPathPattern = regexp.MustCompile(`^/api/part/(\d+)/$`)

// newBulkConnReusePartServer serves /api/part/{id}/ for GET and PATCH,
// tracking each id's "active" field statefully so a full plan-build →
// Preflight → Mutate → Verify round trip per item behaves like a real
// upstream: active starts true, and a PATCH setting it false is reflected in
// every subsequent GET, letting Verify's read-back genuinely match.
func newBulkConnReusePartServer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	active := map[int]bool{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		match := bulkConnReusePartPathPattern.FindStringSubmatch(r.URL.Path)
		if match == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var pk int
		_, _ = fmt.Sscan(match[1], &pk)

		mu.Lock()
		if r.Method == http.MethodPatch {
			var body struct {
				Active *bool `json:"active"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Active != nil {
				active[pk] = *body.Active
			}
		}
		isActive, ok := active[pk]
		if !ok {
			isActive = true
			active[pk] = true
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"pk": pk, "name": "Widget", "active": isActive})
	}))
}

func TestBulkUpdatePartsSharesConnectionsAcrossConcurrentWorkers(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	const itemCount = 8
	const concurrency = 4

	server := newBulkConnReusePartServer(t)
	defer server.Close()

	// http.DefaultTransport's MaxIdleConnsPerHost defaults to 2, which can
	// force new connections under concurrent load regardless of actual reuse
	// opportunity; raise it to the batch's own concurrency so the assertion
	// below measures batch.Execute's bound, not an unrelated transport default.
	baseTransport := &http.Transport{MaxIdleConnsPerHost: concurrency}
	counting := &connCountingTransport{base: baseTransport}
	client, err := inventree.NewClient(inventree.Config{
		BaseURL:    server.URL,
		Credential: inventree.Credential{Scheme: inventree.AuthSchemeToken, Token: "test-token"},
		HTTPClient: &http.Client{Transport: counting},
	})
	r.NoError(err)

	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }, BulkMaxItems: 25, BulkConcurrency: concurrency}
	deps.partBulkPlanStore = mustBulkStore(batch.Options[partBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p partBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})

	handler := bulkUpdateParts(deps)
	items := make([]BulkUpdatePartItem, itemCount)
	for i := range items {
		items[i] = BulkUpdatePartItem{ID: i + 1, Active: dvgoutils.Ptr(false)}
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, itemCount)
	for _, item := range confirmOut.Items {
		r.Equal(string(batch.OutcomeApplied), item.Outcome)
	}

	counting.mu.Lock()
	defer counting.mu.Unlock()
	r.Greater(counting.total, itemCount, "sanity: more than one HTTP request per item was made")
	// A small tolerance above concurrency absorbs a transient extra dial: Go's
	// transport dials immediately if no connection is idle *at that instant*
	// rather than waiting for one to free up, so a connection released back
	// to the pool a moment too late under scheduler contention can still
	// cause one more dial than the steady-state worker count. The bound
	// still proves pooling, not one-connection-per-request (this batch made
	// far more than concurrency+2 requests).
	const connectionCountTolerance = 2
	r.LessOrEqual(counting.newConn, concurrency+connectionCountTolerance,
		fmt.Sprintf("connections opened (%d) must stay close to bounded by concurrency (%d), proving the shared *inventree.Client pools connections instead of opening one per request", counting.newConn, concurrency))
	r.Greater(counting.reused, 0, "keep-alive connection reuse must actually occur across the batch's many sequential-per-worker requests")
}
