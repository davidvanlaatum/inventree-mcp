// Package telemetry owns the process-wide OpenTelemetry setup used by the
// server, MCP middleware, and outbound HTTP clients.
package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	clientprom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	metricapi "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	apiTrace "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	ExporterGRPC = "otlpgrpc"
	ExporterHTTP = "otlphttp"

	defaultServiceName   = "inventree-mcp"
	defaultBatchTimeout  = 5 * time.Second
	defaultExportTimeout = 30 * time.Second
	defaultMetricsPath   = "/metrics"
)

var processEnabled atomic.Bool

var metricsMu sync.RWMutex
var activeMetrics *metrics
var activeMetricsHandler http.Handler
var toolAllowlistMu sync.RWMutex
var toolAllowlist map[string]struct{}
var runtimeMu sync.Mutex
var activeRuntime *Runtime

// Config controls opt-in trace export. Headers may contain credentials and
// must never be logged or included in diagnostic output.
type Config struct {
	Enabled        bool
	ServiceName    string
	Exporter       string
	Endpoint       string
	Insecure       bool
	Headers        map[string]string
	SampleRatio    float64
	BatchTimeout   time.Duration
	ExportTimeout  time.Duration
	MetricsEnabled bool
	MetricsPath    string
}

func DefaultConfig() Config {
	return Config{
		ServiceName:   defaultServiceName,
		Exporter:      ExporterGRPC,
		SampleRatio:   1,
		BatchTimeout:  defaultBatchTimeout,
		ExportTimeout: defaultExportTimeout,
		MetricsPath:   defaultMetricsPath,
	}
}

func (c Config) Validate() error {
	if c.MetricsEnabled {
		if c.MetricsPath == "" || c.MetricsPath == "/" || !strings.HasPrefix(c.MetricsPath, "/") || strings.Contains(c.MetricsPath, "..") || strings.HasSuffix(c.MetricsPath, "/") {
			return errors.New("OpenTelemetry metrics path must be a canonical absolute path")
		}
	}
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.ServiceName) == "" {
		return errors.New("OpenTelemetry service name is required when telemetry is enabled")
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return errors.New("OpenTelemetry endpoint is required when telemetry is enabled")
	}
	switch strings.ToLower(strings.TrimSpace(c.Exporter)) {
	case ExporterGRPC, ExporterHTTP:
	default:
		return fmt.Errorf("OpenTelemetry exporter must be %q or %q", ExporterGRPC, ExporterHTTP)
	}
	if math.IsNaN(c.SampleRatio) || math.IsInf(c.SampleRatio, 0) || c.SampleRatio < 0 || c.SampleRatio > 1 {
		return errors.New("OpenTelemetry sample ratio must be between 0 and 1")
	}
	if c.BatchTimeout <= 0 {
		return errors.New("OpenTelemetry batch timeout must be greater than zero")
	}
	if c.ExportTimeout <= 0 {
		return errors.New("OpenTelemetry export timeout must be greater than zero")
	}
	for key := range c.Headers {
		if strings.TrimSpace(key) == "" {
			return errors.New("OpenTelemetry header names must not be empty")
		}
	}
	return nil
}

// Runtime represents the configured process-wide telemetry providers.
type Runtime struct {
	enabled        bool
	provider       *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	metricsHandler http.Handler
	mu             sync.Mutex
}

// New installs the configured global tracer provider and propagator. Disabled
// telemetry leaves the SDK on its no-op provider and does not create an
// exporter or network dependency.
func New(ctx context.Context, cfg Config) (*Runtime, error) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if activeRuntime != nil {
		return nil, errors.New("OpenTelemetry runtime is already active")
	}
	if !cfg.Enabled && !cfg.MetricsEnabled {
		resetGlobals()
		return &Runtime{}, nil
	}

	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(cfg.ServiceName)))
	if err != nil {
		return nil, fmt.Errorf("create OpenTelemetry resource: %w", err)
	}
	runtime := &Runtime{enabled: true}
	if cfg.Enabled {
		exporter, err := newTraceExporter(ctx, cfg)
		if err != nil {
			return nil, err
		}
		runtime.provider = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(cfg.BatchTimeout), sdktrace.WithExportTimeout(cfg.ExportTimeout)),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(runtime.provider)
		processEnabled.Store(true)
	} else {
		otel.SetTracerProvider(noop.NewTracerProvider())
		processEnabled.Store(false)
	}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	if cfg.MetricsEnabled {
		registry := clientprom.NewRegistry()
		exporter, err := otelprom.New(otelprom.WithRegisterer(registry), otelprom.WithNamespace("inventree_mcp"))
		if err != nil {
			if runtime.provider != nil {
				_ = runtime.provider.Shutdown(ctx)
			}
			resetGlobals()
			return nil, fmt.Errorf("create OpenTelemetry Prometheus exporter: %w", err)
		}
		runtime.meterProvider = sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter), sdkmetric.WithResource(res))
		otel.SetMeterProvider(runtime.meterProvider)
		runtime.metricsHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
		setMetrics(newMetrics(runtime.meterProvider.Meter("inventree-mcp")))
		metricsMu.Lock()
		activeMetricsHandler = runtime.metricsHandler
		metricsMu.Unlock()
	} else {
		otel.SetMeterProvider(sdkmetric.NewMeterProvider())
		setMetrics(nil)
	}
	activeRuntime = runtime
	return runtime, nil
}

func newTraceExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	options := []otlptracegrpc.Option{otlptracegrpc.WithHeaders(cfg.Headers), otlptracegrpc.WithTimeout(cfg.ExportTimeout)}
	if strings.Contains(cfg.Endpoint, "://") {
		options = append(options, otlptracegrpc.WithEndpointURL(cfg.Endpoint))
	} else {
		options = append(options, otlptracegrpc.WithEndpoint(cfg.Endpoint))
	}
	if cfg.Insecure {
		options = append(options, otlptracegrpc.WithInsecure())
	}
	if strings.EqualFold(cfg.Exporter, ExporterGRPC) {
		exporter, err := otlptracegrpc.New(ctx, options...)
		if err != nil {
			return nil, fmt.Errorf("create OpenTelemetry exporter: %w", err)
		}
		return exporter, nil
	}
	httpOptions := []otlptracehttp.Option{otlptracehttp.WithHeaders(cfg.Headers), otlptracehttp.WithTimeout(cfg.ExportTimeout)}
	if strings.Contains(cfg.Endpoint, "://") {
		httpOptions = append(httpOptions, otlptracehttp.WithEndpointURL(cfg.Endpoint))
	} else {
		httpOptions = append(httpOptions, otlptracehttp.WithEndpoint(cfg.Endpoint))
	}
	if cfg.Insecure {
		httpOptions = append(httpOptions, otlptracehttp.WithInsecure())
	}
	exporter, err := otlptracehttp.New(ctx, httpOptions...)
	if err != nil {
		return nil, fmt.Errorf("create OpenTelemetry exporter: %w", err)
	}
	return exporter, nil
}

func resetGlobals() {
	processEnabled.Store(false)
	otel.SetTracerProvider(noop.NewTracerProvider())
	otel.SetMeterProvider(sdkmetric.NewMeterProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	setMetrics(nil)
	metricsMu.Lock()
	activeMetricsHandler = nil
	metricsMu.Unlock()
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil || !r.enabled {
		return nil
	}
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if activeRuntime != r {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var shutdownErr error
	if r.provider != nil {
		shutdownErr = errors.Join(shutdownErr, r.provider.Shutdown(ctx))
	}
	if r.meterProvider != nil {
		shutdownErr = errors.Join(shutdownErr, r.meterProvider.Shutdown(ctx))
	}
	resetGlobals()
	activeRuntime = nil
	return shutdownErr
}

// MetricsHandler returns the configured Prometheus scrape handler, or nil when
// metrics are disabled. The handler is safe to mount only at Config.MetricsPath.
func (r *Runtime) MetricsHandler() http.Handler {
	if r == nil {
		return nil
	}
	return r.metricsHandler
}

// MetricsHandler returns the process metrics scrape handler, or nil when
// metrics are disabled. It is intended for the configured HTTP server only.
func MetricsHandler() http.Handler {
	metricsMu.RLock()
	defer metricsMu.RUnlock()
	return activeMetricsHandler
}

// WrapHTTPClient instruments outbound HTTP requests and propagates the active
// W3C trace context. It preserves the caller's client settings and returns the
// original client when tracing is disabled.
func WrapHTTPClient(client *http.Client) *http.Client {
	if client == nil || (!processEnabled.Load() && getMetrics() == nil) {
		return client
	}
	copy := *client
	transport := client.Transport
	if transport == nil {
		// Do not instrument the process-wide default transport in place or
		// share it with unrelated clients such as Testcontainers' Docker
		// client. A private clone keeps instrumentation scoped to this client.
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			transport = defaultTransport.Clone()
		} else {
			transport = http.DefaultTransport
		}
	}
	copy.Transport = WrapRoundTripper(transport)
	return &copy
}

// WrapRoundTripper instruments an HTTP transport while preserving its request
// policy. It is useful for clients, such as the SSRF-constrained URL fetcher,
// that construct their own http.Client internally.
func WrapRoundTripper(transport http.RoundTripper) http.RoundTripper {
	if transport == nil || (!processEnabled.Load() && getMetrics() == nil) {
		return transport
	}
	return instrumentedRoundTripper(func(req *http.Request) (*http.Response, error) {
		started := time.Now()
		apiOperation := normalizeInvenTreeOperation(req)
		current := getMetrics()
		recordAPIInFlight(current, req.Context(), apiOperation, 1)
		defer func() { recordAPIInFlight(current, req.Context(), apiOperation, -1) }()
		ctx, span := otel.Tracer("inventree-mcp").Start(req.Context(), "HTTP "+req.Method, apiTrace.WithSpanKind(apiTrace.SpanKindClient))
		setHTTPAttributes(span, req)
		outbound := req.Clone(ctx)
		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(outbound.Header))
		response, err := transport.RoundTrip(outbound)
		if err != nil {
			recordHTTP(ctx, req.Method, "error", "error", time.Since(started))
			recordAPI(current, ctx, apiOperation, "error", "error", time.Since(started))
			span.RecordError(err)
			span.SetStatus(codes.Error, "HTTP request failed")
		} else {
			recordHTTP(ctx, req.Method, "success", fmt.Sprintf("%dxx", response.StatusCode/100), time.Since(started))
			recordAPI(current, ctx, apiOperation, "success", fmt.Sprintf("%dxx", response.StatusCode/100), time.Since(started))
			span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
		}
		span.End()
		return response, err
	})
}

type instrumentedRoundTripper func(*http.Request) (*http.Response, error)

func (f instrumentedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type metrics struct {
	mcpRequests    metricapi.Int64Counter
	httpRequests   metricapi.Int64Counter
	httpDuration   metricapi.Float64Histogram
	bulkOperations metricapi.Int64Counter
	toolCalls      metricapi.Int64Counter
	toolDuration   metricapi.Float64Histogram
	toolInFlight   metricapi.Int64UpDownCounter
	apiRequests    metricapi.Int64Counter
	apiDuration    metricapi.Float64Histogram
	apiInFlight    metricapi.Int64UpDownCounter
}

func newMetrics(meter metricapi.Meter) *metrics {
	mcpRequests, _ := meter.Int64Counter("mcp_requests", metricapi.WithDescription("MCP methods received by inventree-mcp"), metricapi.WithUnit("{request}"))
	httpRequests, _ := meter.Int64Counter("http_client_requests", metricapi.WithDescription("Outbound HTTP requests made by inventree-mcp"), metricapi.WithUnit("{request}"))
	httpDuration, _ := meter.Float64Histogram("http_client_request_duration", metricapi.WithDescription("Outbound HTTP request duration"), metricapi.WithUnit("s"))
	bulkOperations, _ := meter.Int64Counter("bulk_operations_started", metricapi.WithDescription("Bulk operations started by inventree-mcp"), metricapi.WithUnit("{operation}"))
	toolCalls, _ := meter.Int64Counter("tool_calls", metricapi.WithDescription("MCP tool calls made to inventree-mcp"), metricapi.WithUnit("{call}"))
	toolDuration, _ := meter.Float64Histogram("tool_call_duration", metricapi.WithDescription("MCP tool call duration"), metricapi.WithUnit("s"))
	toolInFlight, _ := meter.Int64UpDownCounter("tool_calls_in_flight", metricapi.WithDescription("MCP tool calls currently executing"), metricapi.WithUnit("{call}"))
	apiRequests, _ := meter.Int64Counter("inventree_api_requests", metricapi.WithDescription("InvenTree API requests made by inventree-mcp"), metricapi.WithUnit("{request}"))
	apiDuration, _ := meter.Float64Histogram("inventree_api_request_duration", metricapi.WithDescription("InvenTree API request duration"), metricapi.WithUnit("s"))
	apiInFlight, _ := meter.Int64UpDownCounter("inventree_api_requests_in_flight", metricapi.WithDescription("InvenTree API requests currently executing"), metricapi.WithUnit("{request}"))
	return &metrics{mcpRequests: mcpRequests, httpRequests: httpRequests, httpDuration: httpDuration, bulkOperations: bulkOperations, toolCalls: toolCalls, toolDuration: toolDuration, toolInFlight: toolInFlight, apiRequests: apiRequests, apiDuration: apiDuration, apiInFlight: apiInFlight}
}

func setMetrics(value *metrics) {
	metricsMu.Lock()
	activeMetrics = value
	metricsMu.Unlock()
}

func getMetrics() *metrics {
	metricsMu.RLock()
	defer metricsMu.RUnlock()
	return activeMetrics
}

func recordMCP(ctx context.Context, method string, err error) {
	current := getMetrics()
	if current == nil {
		return
	}
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	current.mcpRequests.Add(ctx, 1, metricapi.WithAttributes(attribute.String("method", normalizeMCPMethod(method)), attribute.String("outcome", outcome)))
}

func normalizeMCPMethod(method string) string {
	switch method {
	case "initialize", "notifications/initialized", "tools/list", "tools/call", "ping", "notifications/cancelled":
		return method
	default:
		return "other"
	}
}

func recordHTTP(ctx context.Context, method, outcome, statusClass string, duration time.Duration) {
	current := getMetrics()
	if current == nil {
		return
	}
	attributes := metricapi.WithAttributes(attribute.String("method", normalizeHTTPMethod(method)), attribute.String("outcome", outcome), attribute.String("status_class", statusClass))
	current.httpRequests.Add(ctx, 1, attributes)
	current.httpDuration.Record(ctx, duration.Seconds(), attributes)
}

func normalizeHTTPMethod(method string) string {
	switch method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace:
		return method
	default:
		return "other"
	}
}

var metricNameSegment = regexp.MustCompile(`^[a-z0-9_-]+$`)

var inventreeResources = map[string]struct{}{
	"attachment": {}, "bom": {}, "build": {}, "company": {}, "data-output": {}, "order": {},
	"parameter": {}, "part": {}, "project-code": {}, "settings": {}, "stock": {}, "user": {}, "version": {},
}

// WithInvenTreeAPI marks a request as an InvenTree API call for metrics.
func WithInvenTreeAPI(ctx context.Context) context.Context {
	return requestctx.WithInvenTreeAPI(ctx)
}

func normalizeInvenTreeOperation(req *http.Request) string {
	if req == nil || req.URL == nil || !requestctx.IsInvenTreeAPI(req.Context()) {
		return ""
	}
	if !strings.HasPrefix(req.URL.Path, "/api/") {
		return "other"
	}
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "api" {
		return "other"
	}
	if _, ok := inventreeResources[parts[1]]; !ok {
		return "other"
	}
	return parts[1]
}

func recordAPIInFlight(current *metrics, ctx context.Context, operation string, delta int64) {
	if current == nil || operation == "" {
		return
	}
	current.apiInFlight.Add(ctx, delta, metricapi.WithAttributes(attribute.String("operation", operation)))
}

func recordAPI(current *metrics, ctx context.Context, operation, outcome, statusClass string, duration time.Duration) {
	if current == nil || operation == "" {
		return
	}
	attributes := metricapi.WithAttributes(attribute.String("operation", operation), attribute.String("outcome", outcome), attribute.String("status_class", statusClass))
	current.apiRequests.Add(ctx, 1, attributes)
	current.apiDuration.Record(ctx, duration.Seconds(), attributes)
}

func normalizeToolName(name string) string {
	toolAllowlistMu.RLock()
	_, known := toolAllowlist[name]
	toolAllowlistMu.RUnlock()
	if !known {
		return "other"
	}
	return name
}

// SetToolAllowlist configures the fixed set of registered MCP tool names used
// by per-tool metrics. Unknown names are exported as "other".
func SetToolAllowlist(names []string) {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		if len(name) > 0 && len(name) <= 96 && metricNameSegment.MatchString(name) {
			allowed[name] = struct{}{}
		}
	}
	toolAllowlistMu.Lock()
	toolAllowlist = allowed
	toolAllowlistMu.Unlock()
}

func recordTool(current *metrics, ctx context.Context, tool, outcome string, duration time.Duration) {
	if current == nil {
		return
	}
	attributes := metricapi.WithAttributes(attribute.String("tool", tool), attribute.String("outcome", outcome))
	current.toolCalls.Add(ctx, 1, attributes)
	current.toolDuration.Record(ctx, duration.Seconds(), attributes)
}

func recordToolInFlight(current *metrics, ctx context.Context, tool string, delta int64) {
	if current == nil {
		return
	}
	current.toolInFlight.Add(ctx, delta, metricapi.WithAttributes(attribute.String("tool", tool)))
}

// RecordBulkOperation records a bounded bulk-operation event. It intentionally
// accepts no record identifiers or error text.
func RecordBulkOperation(ctx context.Context, operation, outcome string) {
	current := getMetrics()
	if current == nil {
		return
	}
	// The public helper intentionally buckets both values. Accepting caller
	// values as labels would make cardinality depend on future or untrusted
	// callers.
	_ = operation
	_ = outcome
	current.bulkOperations.Add(ctx, 1, metricapi.WithAttributes(attribute.String("operation", "bulk"), attribute.String("outcome", "started")))
}

// HTTPHandler instruments inbound HTTP requests, extracting W3C trace context.
func HTTPHandler(next http.Handler) http.Handler {
	if next == nil || !processEnabled.Load() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))
		ctx, span := otel.Tracer("inventree-mcp").Start(ctx, "HTTP "+req.Method, apiTrace.WithSpanKind(apiTrace.SpanKindServer))
		setHTTPAttributes(span, req)
		writer := &statusWriter{ResponseWriter: w}
		defer func() {
			if writer.status != 0 {
				span.SetAttributes(attribute.Int("http.response.status_code", writer.status))
			}
			span.End()
		}()
		next.ServeHTTP(writer, req.WithContext(ctx))
	})
}

func setHTTPAttributes(span apiTrace.Span, req *http.Request) {
	span.SetAttributes(
		attribute.String("http.request.method", req.Method),
		attribute.String("url.scheme", req.URL.Scheme),
		attribute.String("server.address", req.URL.Hostname()),
		attribute.String("url.path", req.URL.Path),
	)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		if w.status == 0 {
			w.WriteHeader(http.StatusOK)
		}
		flusher.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *statusWriter) Push(target string, options *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, options)
}

func (w *statusWriter) ReadFrom(src io.Reader) (int64, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(src)
	}
	return io.Copy(w.ResponseWriter, src)
}

// MCPMiddleware creates a span for every inbound MCP method and records the
// stable method/tool name. Request arguments and results are intentionally not
// recorded.
func MCPMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	tracer := otel.Tracer("inventree-mcp")
	if !processEnabled.Load() && getMetrics() == nil {
		return next
	}
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		current := getMetrics()
		tool := ""
		toolStarted := time.Time{}
		if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil {
			tool = normalizeToolName(call.Params.Name)
			toolStarted = time.Now()
			recordToolInFlight(current, ctx, tool, 1)
			defer func() {
				recordToolInFlight(current, ctx, tool, -1)
			}()
		}
		span := apiTrace.Span(nil)
		if processEnabled.Load() {
			ctx, span = tracer.Start(ctx, "mcp."+method)
			defer span.End()
			span.SetAttributes(attribute.String("mcp.method", method))
		}
		if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil {
			if span != nil {
				span.SetAttributes(attribute.String("mcp.tool.name", call.Params.Name))
				setNumericIdentifiers(span, call.Params.Arguments)
			}
		}
		result, err := next(ctx, method, req)
		recordMCP(ctx, method, err)
		if tool != "" {
			outcome := "success"
			if err != nil {
				outcome = "error"
			}
			recordTool(current, ctx, tool, outcome, time.Since(toolStarted))
		}
		if err != nil && span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "MCP method failed")
		}
		return result, err
	}
}

func setNumericIdentifiers(span apiTrace.Span, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var arguments map[string]json.RawMessage
	if json.Unmarshal(raw, &arguments) != nil {
		return
	}
	attributes := make([]attribute.KeyValue, 0, 8)
	for key, value := range arguments {
		if key != "id" && !strings.HasSuffix(key, "_id") {
			continue
		}
		var id int64
		if json.Unmarshal(value, &id) != nil || id <= 0 {
			continue
		}
		attributes = append(attributes, attribute.Int64("mcp.tool.identifier."+key, id))
		if len(attributes) == 8 {
			break
		}
	}
	span.SetAttributes(attributes...)
}
