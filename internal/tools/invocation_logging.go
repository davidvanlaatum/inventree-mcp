package tools

import (
	"context"
	"log/slog"
	"reflect"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/trace"
)

const (
	logEventToolInvocation = "tool_invocation"
	logEventToolCompletion = "tool_completion"

	internalErrorMessage = "internal error: unable to process request"
)

// InvocationLoggingMiddleware establishes the per-tool-call request-scoped
// logger and emits INFO-level invocation/completion log records,
// independently of whether OTEL tracing or metrics are enabled. It must be
// registered together with telemetry.MCPMiddleware in a single
// AddReceivingMiddleware call, with telemetry.MCPMiddleware listed first, so
// any MCP-level span already exists by the time this middleware reads trace
// correlation fields.
//
// generateRequestID is injectable for deterministic tests; a nil value falls
// back to platform.RequestIDGenerator{}.NewRequestID. If ID generation fails,
// the invocation is failed closed with a safe internal error before dispatch,
// and only a separate request-ID-less error record is emitted.
func InvocationLoggingMiddleware(generateRequestID func(context.Context) (string, error)) mcp.Middleware {
	if generateRequestID == nil {
		generateRequestID = platform.RequestIDGenerator{}.NewRequestID
	}
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if !ok || call.Params == nil {
				return next(ctx, method, req)
			}
			toolName := call.Params.Name

			requestID, err := generateRequestID(ctx)
			if err != nil {
				logging.FromContext(ctx).ErrorContext(ctx, "failed to generate request id; refusing tool invocation",
					slog.String("tool", toolName), logging.Err(err))
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: internalErrorMessage}},
				}, nil
			}

			logger := logging.FromContext(ctx).With(
				slog.String("request_id", requestID),
				slog.String("tool", toolName),
			)
			if sourceIP, ok := requestctx.SourceIP(ctx); ok {
				logger = logger.With(slog.String("source_ip", sourceIP.String()))
			}
			if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
				logger = logger.With(
					slog.String("trace_id", spanContext.TraceID().String()),
					slog.String("span_id", spanContext.SpanID().String()),
				)
			}
			invocationFields := safeInvocationFields(toolName, call.Params.Arguments)
			logger = loggerWithAttrs(logger, invocationFields)

			ctx = logging.WithLogger(ctx, logger)
			ctx = WithOutcomeRecorder(ctx)
			logger.InfoContext(ctx, logEventToolInvocation)

			started := time.Now()
			result, err := next(ctx, method, req)
			duration := time.Since(started)

			completionLogger := logging.FromContext(ctx)
			completionAttrs := []any{
				slog.String("outcome", string(completionOutcome(ctx, result, err))),
				slog.Duration("duration", duration),
			}
			if count, ok := boundedResultCount(result); ok {
				completionAttrs = append(completionAttrs, slog.Int("result_count", count))
			}
			completionLogger.InfoContext(ctx, logEventToolCompletion, completionAttrs...)
			return result, err
		}
	}
}

func loggerWithAttrs(logger *slog.Logger, attrs []slog.Attr) *slog.Logger {
	if len(attrs) == 0 {
		return logger
	}
	args := make([]any, len(attrs))
	for i, attr := range attrs {
		args[i] = attr
	}
	return logger.With(args...)
}

// completionOutcome classifies how the invocation completed using the closed
// Outcome vocabulary. Context cancellation takes priority over any explicitly
// recorded outcome. An explicit RecordOutcome call (from safeToolError or
// GuardTool) takes priority over the generic IsError-derived default, which
// exists only as a safe fallback for the small number of tool handlers that
// do not route through those chokepoints.
func completionOutcome(ctx context.Context, result mcp.Result, err error) Outcome {
	if ctx.Err() != nil {
		return OutcomeCancellation
	}
	if outcome, ok := OutcomeFromContext(ctx); ok {
		return outcome
	}
	if err != nil {
		return OutcomeInternalFailure
	}
	if call, ok := result.(*mcp.CallToolResult); ok && call != nil && call.IsError {
		return OutcomeInternalFailure
	}
	return OutcomeSuccess
}

// boundedResultCount opportunistically reads an exported integer "Count"
// field off the tool's typed structured output, reusing the existing
// LookupOutput/BulkUpdateOutput convention so completion logs get a bounded
// item count without per-tool wiring. It never inspects the underlying
// records.
func boundedResultCount(result mcp.Result) (int, bool) {
	call, ok := result.(*mcp.CallToolResult)
	if !ok || call == nil || call.StructuredContent == nil {
		return 0, false
	}
	value := reflect.ValueOf(call.StructuredContent)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0, false
	}
	field := value.FieldByName("Count")
	if !field.IsValid() || field.Kind() != reflect.Int {
		return 0, false
	}
	count := int(field.Int())
	if count < 0 {
		return 0, false
	}
	return count, true
}
