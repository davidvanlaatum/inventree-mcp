package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
