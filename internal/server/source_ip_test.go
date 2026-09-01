package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedLogRecord struct {
	message string
	attrs   []slog.Attr
}

type capturingHandler struct {
	root    *capturingHandler
	mu      sync.Mutex
	attrs   []slog.Attr
	records []capturedLogRecord
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, record slog.Record) error {
	root := h
	if root.root != nil {
		root = root.root
	}
	attrs := append([]slog.Attr{}, h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	root.mu.Lock()
	root.records = append(root.records, capturedLogRecord{message: record.Message, attrs: attrs})
	root.mu.Unlock()
	return nil
}

func (h *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	root := h
	if root.root != nil {
		root = root.root
	}
	return &capturingHandler{root: root, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *capturingHandler) WithGroup(string) slog.Handler { return h }

func (h *capturingHandler) snapshot() []capturedLogRecord {
	root := h
	if root.root != nil {
		root = root.root
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	return append([]capturedLogRecord{}, root.records...)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSourceIPResolverTrustBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		remoteAddress string
		forwardedFor  []string
		wantAddress   string
		wantForwarded bool
	}{
		{name: "untrusted peer ignores forwarded chain", remoteAddress: "198.51.100.10:1234", forwardedFor: []string{"203.0.113.20"}, wantAddress: "198.51.100.10"},
		{name: "trusted peer accepts client", remoteAddress: "10.0.0.10:1234", forwardedFor: []string{"203.0.113.20"}, wantAddress: "203.0.113.20", wantForwarded: true},
		{name: "walks trusted hops right to left", remoteAddress: "10.0.0.10:1234", forwardedFor: []string{"203.0.113.20, 192.0.2.10"}, wantAddress: "203.0.113.20", wantForwarded: true},
		{name: "stops at injected untrusted hop", remoteAddress: "10.0.0.10:1234", forwardedFor: []string{"198.51.100.30, 203.0.113.20, 192.0.2.10"}, wantAddress: "203.0.113.20", wantForwarded: true},
		{name: "supports multiple header lines", remoteAddress: "10.0.0.10:1234", forwardedFor: []string{"203.0.113.20", "192.0.2.10"}, wantAddress: "203.0.113.20", wantForwarded: true},
		{name: "multi-line walk ignores injected untrusted value left of client", remoteAddress: "10.0.0.10:1234", forwardedFor: []string{"198.51.100.30", "203.0.113.20", "192.0.2.10"}, wantAddress: "203.0.113.20", wantForwarded: true},
		{name: "multi-line walk ignores malformed attacker value left of client", remoteAddress: "10.0.0.10:1234", forwardedFor: []string{"malformed", "203.0.113.20", "192.0.2.10"}, wantAddress: "203.0.113.20", wantForwarded: true},
		{name: "multi-line malformed trusted segment fails closed to peer", remoteAddress: "10.0.0.10:1234", forwardedFor: []string{"203.0.113.20", "malformed"}, wantAddress: "10.0.0.10"},
		{name: "malformed attacker value left of client is ignored", remoteAddress: "10.0.0.10:1234", forwardedFor: []string{"malformed, 203.0.113.20, 192.0.2.10"}, wantAddress: "203.0.113.20", wantForwarded: true},
		{name: "malformed trusted segment fails closed to peer", remoteAddress: "10.0.0.10:1234", forwardedFor: []string{"203.0.113.20, unknown"}, wantAddress: "10.0.0.10"},
		{name: "missing peer is unresolved", remoteAddress: "not-an-address", forwardedFor: []string{"203.0.113.20"}},
	}

	resolver, err := newSourceIPResolver([]string{"10.0.0.0/8", "192.0.2.0/24"})
	require.NoError(t, err)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := assert.New(t)
			req := httptest.NewRequest(http.MethodGet, "/authorize", nil)
			req.RemoteAddr = tt.remoteAddress
			for _, value := range tt.forwardedFor {
				req.Header.Add("X-Forwarded-For", value)
			}

			resolved := resolver.resolve(req)
			if tt.wantAddress == "" {
				a.False(resolved.address.IsValid())
			} else {
				a.Equal(tt.wantAddress, resolved.address.String())
			}
			a.Equal(tt.wantForwarded, resolved.forwarded)
		})
	}
}

func TestSourceIPMiddlewareAttachesNormalizedLogContext(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, handler, _ := testhandler.SetupTestHandler(t)
	resolver, err := newSourceIPResolver([]string{"10.0.0.0/8"})
	r.NoError(err)
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sourceIP, ok := requestctx.SourceIP(req.Context())
		r.True(ok)
		a.Equal("203.0.113.20", sourceIP.String())
		logging.FromContext(req.Context()).InfoContext(req.Context(), "proxied request")
		w.WriteHeader(http.StatusNoContent)
	})
	middleware := sourceIPMiddleware(logging.FromContext(ctx), resolver, next)
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.RemoteAddr = "10.0.0.10:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.30, 203.0.113.20")
	recorder := httptest.NewRecorder()

	middleware.ServeHTTP(recorder, req)

	a.Equal(http.StatusNoContent, recorder.Code)
	record := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == "proxied request"
	})
	r.NotNil(record)
	a.Equal("203.0.113.20", record["source_ip"])
	a.NotContains(record, "source_ip_forwarded")
	a.NotContains(record, "x_forwarded_for")
	a.NotContains(record, "X-Forwarded-For")
}

func TestSourceIPFlowsThroughToolAndOutboundLogsOnlyOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		stdio         bool
		remoteAddress string
		forwardedFor  string
		wantSourceIP  string
	}{
		{name: "trusted proxy", remoteAddress: "10.0.0.10:1234", forwardedFor: "198.51.100.30, 203.0.113.20", wantSourceIP: "203.0.113.20"},
		{name: "direct client", remoteAddress: "203.0.113.20:1234", wantSourceIP: "203.0.113.20"},
		{name: "untrusted proxy", remoteAddress: "198.51.100.30:1234", forwardedFor: "203.0.113.20", wantSourceIP: "198.51.100.30"},
		{name: "stdio", stdio: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			capture := &capturingHandler{}
			ctx := logging.WithLogger(context.Background(), slog.New(capture))
			client, err := inventree.NewClient(inventree.Config{
				BaseURL:    "https://inventory.example.test",
				Credential: inventree.Credential{Scheme: inventree.AuthSchemeToken, Token: "test-token"},
				HTTPClient: &http.Client{Transport: inventree.WrapRequestLogging(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
				}))},
			})
			r.NoError(err)

			invoke := tools.InvocationLoggingMiddleware(func(context.Context) (string, error) {
				return strings.Repeat("a", 32), nil
			})(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
				req, err := client.NewRequest(ctx, http.MethodGet, "/api/part/42/", nil, nil)
				if err != nil {
					return nil, err
				}
				return &mcp.CallToolResult{}, client.DoJSON(req, nil)
			})
			run := func(runCtx context.Context) {
				_, err := invoke(runCtx, "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "get_part", Arguments: []byte(`{"id":42}`)}})
				r.NoError(err)
			}

			if tt.stdio {
				run(ctx)
			} else {
				resolver, err := newSourceIPResolver([]string{"10.0.0.0/8"})
				r.NoError(err)
				req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
				req.RemoteAddr = tt.remoteAddress
				if tt.forwardedFor != "" {
					req.Header.Set("X-Forwarded-For", tt.forwardedFor)
				}
				sourceIPMiddleware(logging.FromContext(ctx), resolver, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
					run(request.Context())
				})).ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))
			}

			records := capture.snapshot()
			r.GreaterOrEqual(len(records), 4)
			for _, record := range records {
				count := 0
				value := ""
				for _, attr := range record.attrs {
					if attr.Key == "source_ip" {
						count++
						value = attr.Value.String()
					}
				}
				a.LessOrEqual(count, 1, "message %q contains duplicate source_ip attributes", record.message)
				if tt.wantSourceIP == "" {
					a.Equal(0, count, "message %q must not contain source_ip", record.message)
				} else {
					a.Equal(1, count, "message %q must contain source_ip once", record.message)
					a.Equal(tt.wantSourceIP, value)
				}
			}
		})
	}
}

func TestSourceIPResolverDoesNotAllocatePerAttackerControlledHop(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	resolver, err := newSourceIPResolver([]string{"10.0.0.0/8"})
	r.NoError(err)
	req := httptest.NewRequest(http.MethodGet, "/authorize", nil)
	req.RemoteAddr = "10.0.0.10:1234"
	req.Header.Set("X-Forwarded-For", strings.Repeat("malformed,", 100_000)+"203.0.113.20")

	resolved := resolver.resolve(req)
	a.Equal("203.0.113.20", resolved.address.String())
	a.True(resolved.forwarded)
	allocations := testing.AllocsPerRun(5, func() {
		resolver.resolve(req)
	})
	a.Less(allocations, float64(50))
}

func TestNewSourceIPResolverRejectsInvalidCIDR(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := newSourceIPResolver([]string{"not-a-cidr"})
	r.ErrorContains(err, "parse trusted proxy CIDR")
	_, err = newSourceIPResolver([]string{"0.0.0.0/0"})
	r.ErrorContains(err, "must not trust every address")
	_, err = newSourceIPResolver([]string{"::/0"})
	r.ErrorContains(err, "must not trust every address")
}
