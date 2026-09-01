package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
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

func TestMCPServerToolAndInvenTreeClientSpansShareTrace(t *testing.T) {
	exporter := withRecordingProvider(t)
	invenTreeClient, err := inventree.NewClient(inventree.Config{
		BaseURL:    "https://inventory.example.test",
		Credential: inventree.Credential{Scheme: inventree.AuthSchemeToken, Token: "test-token"},
		HTTPClient: WrapHTTPClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, "/api/part/42/", req.URL.Path)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"pk":42,"name":"Trace test"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})}),
	})
	require.NoError(t, err)
	server := mcp.NewServer(&mcp.Implementation{Name: "trace-test", Version: "1"}, nil)
	server.AddReceivingMiddleware(MCPMiddleware)
	server.AddTool(&mcp.Tool{Name: "get_part", InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_, err := invenTreeClient.GetPart(ctx, 42)
		return &mcp.CallToolResult{}, err
	})

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	_, err = server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	client := mcp.NewClient(&mcp.Implementation{Name: "trace-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, session.Close()) }()
	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_part"})
	require.NoError(t, err)

	var toolTrace, clientTrace apiTrace.TraceID
	for _, span := range exporter.spans {
		attrs := make(map[attribute.Key]attribute.Value, len(span.Attributes()))
		for _, attr := range span.Attributes() {
			attrs[attr.Key] = attr.Value
		}
		if attrs["mcp.tool.name"].AsString() == "get_part" {
			toolTrace = span.SpanContext().TraceID()
		}
		if attrs["http.request.method"].AsString() == http.MethodGet && attrs["url.path"].AsString() == "/api/part/42/" {
			clientTrace = span.SpanContext().TraceID()
		}
	}
	require.NotEqual(t, apiTrace.TraceID{}, toolTrace)
	require.NotEqual(t, apiTrace.TraceID{}, clientTrace)
	assert.Equal(t, toolTrace, clientTrace)
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
	assert.IsType(t, instrumentedRoundTripper{}, client.Transport)
}

func TestInstrumentedRoundTripperUnwrapExposesTheWrappedTransport(t *testing.T) {
	withRecordingProvider(t)

	inner := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(http.NoBody), Header: make(http.Header), Request: req}, nil
	})
	wrapped := WrapRoundTripper(inner)

	unwrapper, ok := wrapped.(interface{ Unwrap() http.RoundTripper })
	require.True(t, ok, "instrumentedRoundTripper must implement Unwrap so layered wrapping can reach the real transport")
	assert.Equal(t, fmt.Sprintf("%p", inner), fmt.Sprintf("%p", unwrapper.Unwrap()))
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

func TestHTTPHandlerNamesRootSpanWithMCPTool(t *testing.T) {
	exporter := withRecordingProvider(t)
	SetToolAllowlist([]string{"search_stock_locations"})
	mcpHandler := MCPMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, nil
	})
	handler := HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, err := mcpHandler(req.Context(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
			Name: "search_stock_locations",
		}})
		require.NoError(t, err)
		w.WriteHeader(http.StatusNoContent)
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://mcp.example.test/mcp", nil))
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Len(t, exporter.spans, 2)

	var names []string
	for _, span := range exporter.spans {
		names = append(names, span.Name())
	}
	assert.Equal(t, []string{
		"mcp.tools/call/search_stock_locations",
		"mcp.tools/call/search_stock_locations",
	}, names)
}

func TestHTTPHandlerUsesBoundedNamesForUnknownRoutesAndTools(t *testing.T) {
	exporter := withRecordingProvider(t)
	SetToolAllowlist([]string{"known_tool"})
	mcpHandler := MCPMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, nil
	})
	handler := HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, err := mcpHandler(req.Context(), "future/method", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
			Name: "unknown_tool_123",
		}})
		require.NoError(t, err)
		w.WriteHeader(http.StatusNoContent)
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://mcp.example.test/record/987654", nil))
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Len(t, exporter.spans, 2)
	for _, span := range exporter.spans {
		assert.Equal(t, "mcp.other/other", span.Name())
	}
}

func TestMCPMiddlewareNamesStdioToolSpanWithoutHTTPRoot(t *testing.T) {
	exporter := withRecordingProvider(t)
	SetToolAllowlist([]string{"get_part"})
	next := MCPMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, nil
	})

	_, err := next(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name: "get_part",
	}})
	require.NoError(t, err)
	require.Len(t, exporter.spans, 1)
	assert.Equal(t, "mcp.tools/call/get_part", exporter.spans[0].Name())
}

func TestMCPMiddlewarePreservesKnownProtocolMethodNames(t *testing.T) {
	exporter := withRecordingProvider(t)
	next := MCPMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, nil
	})

	_, err := next(context.Background(), "notifications/progress", nil)
	require.NoError(t, err)
	require.Len(t, exporter.spans, 1)
	assert.Equal(t, "mcp.notifications/progress", exporter.spans[0].Name())
}

func TestHTTPHandlerUsesBoundedRouteNameBeforeMCPDispatch(t *testing.T) {
	exporter := withRecordingProvider(t)
	handler := HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("CUSTOM", "http://mcp.example.test/mcp/random-id", nil))
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Len(t, exporter.spans, 1)
	assert.Equal(t, "other /other", exporter.spans[0].Name())
}

func TestHTTPHandlerWithRouteUsesConfiguredMCPPath(t *testing.T) {
	exporter := withRecordingProvider(t)
	handler := HTTPHandlerWithRoute(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "/custom-mcp")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://mcp.example.test/custom-mcp/", nil))
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Len(t, exporter.spans, 1)
	assert.Equal(t, "POST /custom-mcp", exporter.spans[0].Name())
}

func TestDisabledTelemetryDoesNotWrapHTTPClient(t *testing.T) {
	runtime, err := New(context.Background(), DefaultConfig())
	require.NoError(t, err)
	require.NoError(t, runtime.Shutdown(context.Background()))

	client := &http.Client{}
	assert.Same(t, client, WrapHTTPClient(client))
	assert.Same(t, http.DefaultTransport, WrapRoundTripper(http.DefaultTransport))
}

func TestMetricsOnlyRuntimeExposesPrometheusAndRecordsMCPRequests(t *testing.T) {
	SetToolAllowlist([]string{"get_part"})
	runtime, err := New(context.Background(), Config{
		MetricsEnabled: true,
		MetricsPath:    "/metrics",
		ServiceName:    defaultServiceName,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })

	next := MCPMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, nil
	})
	_, err = next(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "get_part"}})
	require.NoError(t, err)
	_, err = next(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "future_tool_123"}})
	require.NoError(t, err)
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header), Request: req}, nil
	})}
	request, err := http.NewRequestWithContext(WithInvenTreeAPI(context.Background()), http.MethodGet, "https://inventory.example.test/api/part/1/", nil)
	require.NoError(t, err)
	clientResponse, err := WrapHTTPClient(client).Do(request)
	require.NoError(t, err)
	require.NoError(t, clientResponse.Body.Close())
	unknownRequest, err := http.NewRequestWithContext(WithInvenTreeAPI(context.Background()), http.MethodGet, "https://inventory.example.test/api/future-resource/opaque-key/", nil)
	require.NoError(t, err)
	unknownResponse, err := WrapHTTPClient(client).Do(unknownRequest)
	require.NoError(t, err)
	require.NoError(t, unknownResponse.Body.Close())
	RecordBulkOperation(context.Background(), "bulk_update_parts", "started")

	scrape := httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, scrape.Code)
	body := scrape.Body.String()
	assert.Contains(t, body, "inventree_mcp_mcp_requests_total")
	assert.Contains(t, body, `method="tools/call"`)
	assert.Contains(t, body, "inventree_mcp_http_client_requests_total")
	assert.Contains(t, body, `method="GET"`)
	assert.Contains(t, body, `outcome="success"`)
	assert.Contains(t, body, `status_class="2xx"`)
	assert.Contains(t, body, "inventree_mcp_http_client_request_duration_seconds")
	assert.Contains(t, body, "inventree_mcp_bulk_operations_started_total")
	assert.Contains(t, body, `operation="bulk"`)
	assert.Contains(t, body, `outcome="started"`)
	assert.Contains(t, body, "inventree_mcp_tool_calls_total")
	assert.Contains(t, body, `tool="get_part"`)
	assert.Contains(t, body, `tool="other"`)
	assert.NotContains(t, body, `tool="future_tool_123"`)
	assert.Contains(t, body, "inventree_mcp_tool_call_duration_seconds")
	assert.Contains(t, body, "inventree_mcp_tool_calls_in_flight")
	assert.Contains(t, body, "inventree_mcp_inventree_api_requests_total")
	assert.Contains(t, body, `operation="part"`)
	assert.Contains(t, body, "inventree_mcp_inventree_api_request_duration_seconds")
	assert.Contains(t, body, "inventree_mcp_inventree_api_requests_in_flight")
	assert.Contains(t, body, `operation="other"`)
	assert.NotContains(t, body, `operation="future-resource"`)
}

func TestMetricsConfigRejectsNonCanonicalPath(t *testing.T) {
	err := (Config{MetricsEnabled: true, MetricsPath: "metrics", ServiceName: defaultServiceName}).Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metrics path")
}

func TestMetricsNormalizeUnknownMCPMethods(t *testing.T) {
	runtime, err := New(context.Background(), Config{MetricsEnabled: true, MetricsPath: "/metrics", ServiceName: defaultServiceName})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })

	next := MCPMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) { return nil, nil })
	for i := 0; i < 100; i++ {
		_, err := next(context.Background(), fmt.Sprintf("unknown/%d", i), nil)
		require.NoError(t, err)
	}
	response := httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	assert.Contains(t, body, `method="other"`)
	assert.NotContains(t, body, "unknown/99")
}

func TestMetricsRuntimeOwnsGlobalLifecycle(t *testing.T) {
	runtime, err := New(context.Background(), Config{MetricsEnabled: true, MetricsPath: "/metrics", ServiceName: defaultServiceName})
	require.NoError(t, err)
	_, err = New(context.Background(), Config{MetricsEnabled: true, MetricsPath: "/metrics", ServiceName: defaultServiceName})
	require.ErrorContains(t, err, "already active")
	require.NoError(t, runtime.Shutdown(context.Background()))
	require.NoError(t, runtime.Shutdown(context.Background()))

	next, err := New(context.Background(), Config{MetricsEnabled: true, MetricsPath: "/metrics", ServiceName: defaultServiceName})
	require.NoError(t, err)
	require.NoError(t, next.Shutdown(context.Background()))
}

func TestMetricsNormalizeUnknownHTTPMethods(t *testing.T) {
	runtime, err := New(context.Background(), Config{MetricsEnabled: true, MetricsPath: "/metrics", ServiceName: defaultServiceName})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })

	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header), Request: req}, nil
	})}
	request, err := http.NewRequestWithContext(context.Background(), "CUSTOM", "https://inventory.example.test/api/part/1/", nil)
	require.NoError(t, err)
	response, err := WrapHTTPClient(client).Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	scrape := httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(t, scrape.Body.String(), `method="other"`)
	assert.NotContains(t, scrape.Body.String(), `method="CUSTOM"`)
}

func TestMetricsRecordToolAndAPIErrorOutcomes(t *testing.T) {
	SetToolAllowlist([]string{"failing_tool"})
	runtime, err := New(context.Background(), Config{MetricsEnabled: true, MetricsPath: "/metrics", ServiceName: defaultServiceName})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })

	toolHandler := MCPMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, errors.New("tool failed")
	})
	_, err = toolHandler(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "failing_tool"}})
	require.Error(t, err)

	apiClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("busy")), Header: make(http.Header), Request: req}, nil
	})}
	request, err := http.NewRequestWithContext(WithInvenTreeAPI(context.Background()), http.MethodGet, "https://inventory.example.test/api/part/1/", nil)
	require.NoError(t, err)
	response, err := WrapHTTPClient(apiClient).Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	scrape := httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := scrape.Body.String()
	assert.Contains(t, body, `tool="failing_tool"`)
	assert.Contains(t, body, `outcome="error"`)
	assert.Contains(t, body, `operation="part"`)
	assert.Contains(t, body, `status_class="5xx"`)
	assertMetricValue(t, body, "inventree_mcp_tool_calls_total", `tool="failing_tool"`, "1")
	assertMetricValue(t, body, "inventree_mcp_tool_call_duration_seconds_count", `tool="failing_tool"`, "1")
	assertMetricValue(t, body, "inventree_mcp_inventree_api_requests_total", `operation="part"`, "1")
	assertMetricValue(t, body, "inventree_mcp_inventree_api_request_duration_seconds_count", `operation="part"`, "1")

	transportErrorClient := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network failed")
	})}
	transportRequest, err := http.NewRequestWithContext(WithInvenTreeAPI(context.Background()), http.MethodGet, "https://inventory.example.test/api/part/2/", nil)
	require.NoError(t, err)
	_, err = WrapHTTPClient(transportErrorClient).Do(transportRequest)
	require.Error(t, err)

	scrape = httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body = scrape.Body.String()
	assertMetricValueWithLabels(t, body, "inventree_mcp_inventree_api_requests_total", []string{`operation="part"`, `outcome="error"`, `status_class="error"`}, "1")
	assertMetricValueWithLabels(t, body, "inventree_mcp_inventree_api_request_duration_seconds_count", []string{`operation="part"`, `outcome="error"`, `status_class="error"`}, "1")
	assertMetricValueWithLabels(t, body, "inventree_mcp_inventree_api_requests_total", []string{`operation="part"`, `outcome="success"`, `status_class="5xx"`}, "1")
	assertMetricValueWithLabels(t, body, "inventree_mcp_inventree_api_request_duration_seconds_count", []string{`operation="part"`, `outcome="success"`, `status_class="5xx"`}, "1")
	assertMetricValue(t, body, "inventree_mcp_inventree_api_requests_in_flight", `operation="part"`, "0")
	assert.Contains(t, body, `status_class="error"`)
}

func TestMetricsInFlightGaugesReturnToZero(t *testing.T) {
	SetToolAllowlist([]string{"blocking_tool"})
	runtime, err := New(context.Background(), Config{MetricsEnabled: true, MetricsPath: "/metrics", ServiceName: defaultServiceName})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })

	started := make(chan struct{})
	release := make(chan struct{})
	toolHandler := MCPMiddleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		close(started)
		<-release
		return nil, nil
	})
	done := make(chan struct{})
	go func() {
		_, _ = toolHandler(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "blocking_tool"}})
		close(done)
	}()
	<-started
	scrape := httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assertMetricValue(t, scrape.Body.String(), "inventree_mcp_tool_calls_in_flight", `tool="blocking_tool"`, "1")
	close(release)
	<-done
	scrape = httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assertMetricValue(t, scrape.Body.String(), "inventree_mcp_tool_calls_in_flight", `tool="blocking_tool"`, "0")
	assertMetricValue(t, scrape.Body.String(), "inventree_mcp_tool_calls_total", `tool="blocking_tool"`, "1")
	assertMetricValue(t, scrape.Body.String(), "inventree_mcp_tool_call_duration_seconds_count", `tool="blocking_tool"`, "1")
	// Wall-clock durations are intentionally nondeterministic; the count and
	// positive sum prove observation without coupling this test to scheduling.
	assertMetricPositive(t, scrape.Body.String(), "inventree_mcp_tool_call_duration_seconds_sum", `tool="blocking_tool"`)

	apiStarted := make(chan struct{})
	apiRelease := make(chan struct{})
	apiClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		close(apiStarted)
		<-apiRelease
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header), Request: req}, nil
	})}
	apiRequest, err := http.NewRequestWithContext(WithInvenTreeAPI(context.Background()), http.MethodGet, "https://inventory.example.test/api/part/1/", nil)
	require.NoError(t, err)
	apiDone := make(chan struct{})
	go func() {
		response, requestErr := WrapHTTPClient(apiClient).Do(apiRequest)
		if response != nil {
			_ = response.Body.Close()
		}
		require.NoError(t, requestErr)
		close(apiDone)
	}()
	<-apiStarted
	scrape = httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assertMetricValue(t, scrape.Body.String(), "inventree_mcp_inventree_api_requests_in_flight", `operation="part"`, "1")
	close(apiRelease)
	<-apiDone
	scrape = httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assertMetricValue(t, scrape.Body.String(), "inventree_mcp_inventree_api_requests_in_flight", `operation="part"`, "0")
	assertMetricValue(t, scrape.Body.String(), "inventree_mcp_inventree_api_requests_total", `operation="part"`, "1")
	assertMetricValue(t, scrape.Body.String(), "inventree_mcp_inventree_api_request_duration_seconds_count", `operation="part"`, "1")
	assertMetricPositive(t, scrape.Body.String(), "inventree_mcp_inventree_api_request_duration_seconds_sum", `operation="part"`)
}

func assertMetricValue(t *testing.T, exposition, metricName, label, value string) {
	assertMetricValueWithLabels(t, exposition, metricName, []string{label}, value)
}

func assertMetricValueWithLabels(t *testing.T, exposition, metricName string, labels []string, value string) {
	t.Helper()
	for _, line := range strings.Split(exposition, "\n") {
		allLabelsPresent := true
		for _, label := range labels {
			if !strings.Contains(line, label) {
				allLabelsPresent = false
				break
			}
		}
		if strings.HasPrefix(line, metricName+"{") && allLabelsPresent && strings.HasSuffix(line, "} "+value) {
			return
		}
	}
	t.Errorf("metric %s with labels %v=%s not found", metricName, labels, value)
}

func assertMetricPositive(t *testing.T, exposition, metricName, label string) {
	t.Helper()
	for _, line := range strings.Split(exposition, "\n") {
		if !strings.HasPrefix(line, metricName+"{") || !strings.Contains(line, label) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] != "0" {
			return
		}
	}
	t.Errorf("positive metric %s with %s not found", metricName, label)
}

func TestNewOTLPHTTPFlushesSpansAndSendsHeadersOnShutdown(t *testing.T) {
	var requests atomic.Int32
	var gotHeader atomic.Value
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		gotHeader.Store(req.Header.Get("x-test-header"))
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exporter = ExporterHTTP
	cfg.Endpoint = collector.URL
	cfg.Insecure = true
	cfg.Headers = map[string]string{"x-test-header": "present"}
	runtime, err := New(context.Background(), cfg)
	require.NoError(t, err)
	_, span := otel.Tracer("flush-test").Start(context.Background(), "span")
	span.End()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, runtime.Shutdown(shutdownCtx))
	assert.Greater(t, requests.Load(), int32(0))
	assert.Equal(t, "present", gotHeader.Load())
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
