package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	apiTrace "go.opentelemetry.io/otel/trace"
)

type recordingExporter struct {
	mu    sync.Mutex
	spans []trace.ReadOnlySpan
}

func (e *recordingExporter) ExportSpans(_ context.Context, spans []trace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *recordingExporter) Shutdown(context.Context) error { return nil }

func withRecordingProvider(t *testing.T) *recordingExporter {
	t.Helper()
	exporter := &recordingExporter{}
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(trace.NewSimpleSpanProcessor(exporter)))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	processEnabled.Store(true)
	t.Cleanup(func() {
		processEnabled.Store(false)
		_ = provider.Shutdown(context.Background())
	})
	return exporter
}

func TestMCPMiddlewareRecordsToolAndNumericIdentifiersWithoutArguments(t *testing.T) {
	exporter := withRecordingProvider(t)
	called := false
	next := MCPMiddleware(func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		called = true
		assert.Equal(t, "tools/call", method)
		assert.True(t, apiTrace.SpanFromContext(ctx).SpanContext().IsValid())
		return nil, nil
	})

	_, err := next(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name:      "get_part",
		Arguments: []byte(`{"part_id":42,"token":"secret"}`),
	}})
	require.NoError(t, err)
	require.True(t, called)
	require.Len(t, exporter.spans, 1)
	attributes := exporter.spans[0].Attributes()
	values := make(map[attribute.Key]attribute.Value, len(attributes))
	for _, attr := range attributes {
		values[attr.Key] = attr.Value
	}
	assert.Equal(t, "tools/call", values["mcp.method"].AsString())
	assert.Equal(t, "get_part", values["mcp.tool.name"].AsString())
	assert.Equal(t, int64(42), values["mcp.tool.identifier.part_id"].AsInt64())
	for _, attr := range attributes {
		assert.NotContains(t, string(attr.Key), "token")
	}
}

func TestWrapHTTPClientInjectsTraceContext(t *testing.T) {
	exporter := withRecordingProvider(t)
	var gotTraceparent string
	client := WrapHTTPClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		gotTraceparent = req.Header.Get("traceparent")
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(http.NoBody), Header: make(http.Header), Request: req}, nil
	})})
	parentCtx, span := otel.Tracer("test").Start(context.Background(), "parent")
	req, err := http.NewRequestWithContext(parentCtx, http.MethodGet, "https://inventory.example.test/api/part/42/?token=do-not-export", nil)
	require.NoError(t, err)
	response, err := client.Do(req)
	span.End()
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.NotEmpty(t, gotTraceparent)
	assert.NotEmpty(t, exporter.spans)
	for _, span := range exporter.spans {
		for _, attr := range span.Attributes() {
			assert.NotContains(t, attr.Value.AsString(), "do-not-export")
		}
	}
}

func TestWrapHTTPClientClonesDefaultTransport(t *testing.T) {
	withRecordingProvider(t)

	client := WrapHTTPClient(&http.Client{})
	assert.IsType(t, instrumentedRoundTripper(nil), client.Transport)
}

func TestHTTPHandlerCorrelatesInboundAndOutboundSpans(t *testing.T) {
	exporter := withRecordingProvider(t)
	client := WrapHTTPClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		assert.True(t, apiTrace.SpanFromContext(req.Context()).SpanContext().IsValid())
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(http.NoBody), Header: make(http.Header), Request: req}, nil
	})})
	handler := HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		outbound, err := http.NewRequestWithContext(req.Context(), http.MethodGet, "https://inventory.example.test/api/version/", nil)
		require.NoError(t, err)
		response, err := client.Do(outbound)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		w.WriteHeader(http.StatusNoContent)
	}))

	parentCtx, parent := otel.Tracer("test").Start(context.Background(), "parent")
	req := httptest.NewRequest(http.MethodGet, "http://mcp.example.test/mcp", nil).WithContext(parentCtx)
	propagation.TraceContext{}.Inject(parentCtx, propagation.HeaderCarrier(req.Header))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	parent.End()

	require.Equal(t, http.StatusNoContent, response.Code)
	require.GreaterOrEqual(t, len(exporter.spans), 3)
	traceIDs := make(map[apiTrace.TraceID]struct{})
	for _, span := range exporter.spans {
		traceIDs[span.SpanContext().TraceID()] = struct{}{}
	}
	assert.Len(t, traceIDs, 1)
}

func TestDisabledTelemetryDoesNotWrapHTTPClient(t *testing.T) {
	runtime, err := New(context.Background(), DefaultConfig())
	require.NoError(t, err)
	require.NoError(t, runtime.Shutdown(context.Background()))

	client := &http.Client{}
	assert.Same(t, client, WrapHTTPClient(client))
	assert.Same(t, http.DefaultTransport, WrapRoundTripper(http.DefaultTransport))
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
