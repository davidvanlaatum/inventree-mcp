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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
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
)

var processEnabled atomic.Bool

// Config controls opt-in trace export. Headers may contain credentials and
// must never be logged or included in diagnostic output.
type Config struct {
	Enabled       bool
	ServiceName   string
	Exporter      string
	Endpoint      string
	Insecure      bool
	Headers       map[string]string
	SampleRatio   float64
	BatchTimeout  time.Duration
	ExportTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		ServiceName:   defaultServiceName,
		Exporter:      ExporterGRPC,
		SampleRatio:   1,
		BatchTimeout:  defaultBatchTimeout,
		ExportTimeout: defaultExportTimeout,
	}
}

func (c Config) Validate() error {
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
	enabled  bool
	provider *sdktrace.TracerProvider
	mu       sync.Mutex
}

// New installs the configured global tracer provider and propagator. Disabled
// telemetry leaves the SDK on its no-op provider and does not create an
// exporter or network dependency.
func New(ctx context.Context, cfg Config) (*Runtime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		processEnabled.Store(false)
		otel.SetTracerProvider(noop.NewTracerProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
		return &Runtime{}, nil
	}

	var exporter sdktrace.SpanExporter
	var err error
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
		exporter, err = otlptracegrpc.New(ctx, options...)
	} else {
		httpOptions := []otlptracehttp.Option{otlptracehttp.WithHeaders(cfg.Headers), otlptracehttp.WithTimeout(cfg.ExportTimeout)}
		if strings.Contains(cfg.Endpoint, "://") {
			httpOptions = append(httpOptions, otlptracehttp.WithEndpointURL(cfg.Endpoint))
		} else {
			httpOptions = append(httpOptions, otlptracehttp.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			httpOptions = append(httpOptions, otlptracehttp.WithInsecure())
		}
		exporter, err = otlptracehttp.New(ctx, httpOptions...)
	}
	if err != nil {
		return nil, fmt.Errorf("create OpenTelemetry exporter: %w", err)
	}

	serviceName := cfg.ServiceName
	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		_ = exporter.Shutdown(ctx)
		return nil, fmt.Errorf("create OpenTelemetry resource: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(cfg.BatchTimeout), sdktrace.WithExportTimeout(cfg.ExportTimeout)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	processEnabled.Store(true)
	return &Runtime{enabled: true, provider: provider}, nil
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil || !r.enabled || r.provider == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.provider.Shutdown(ctx); err != nil {
		return err
	}
	otel.SetTracerProvider(noop.NewTracerProvider())
	processEnabled.Store(false)
	return nil
}

// WrapHTTPClient instruments outbound HTTP requests and propagates the active
// W3C trace context. It preserves the caller's client settings and returns the
// original client when tracing is disabled.
func WrapHTTPClient(client *http.Client) *http.Client {
	if client == nil || !processEnabled.Load() {
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
	if transport == nil || !processEnabled.Load() {
		return transport
	}
	return instrumentedRoundTripper(func(req *http.Request) (*http.Response, error) {
		ctx, span := otel.Tracer("inventree-mcp").Start(req.Context(), "HTTP "+req.Method, apiTrace.WithSpanKind(apiTrace.SpanKindClient))
		setHTTPAttributes(span, req)
		outbound := req.Clone(ctx)
		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(outbound.Header))
		response, err := transport.RoundTrip(outbound)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "HTTP request failed")
		} else {
			span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
		}
		span.End()
		return response, err
	})
}

type instrumentedRoundTripper func(*http.Request) (*http.Response, error)

func (f instrumentedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

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
	if !processEnabled.Load() {
		return next
	}
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		ctx, span := tracer.Start(ctx, "mcp."+method)
		defer span.End()
		span.SetAttributes(attribute.String("mcp.method", method))
		if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil {
			span.SetAttributes(attribute.String("mcp.tool.name", call.Params.Name))
			setNumericIdentifiers(span, call.Params.Arguments)
		}
		result, err := next(ctx, method, req)
		if err != nil {
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
