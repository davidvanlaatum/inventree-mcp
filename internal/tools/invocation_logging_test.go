package tools

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiTrace "go.opentelemetry.io/otel/trace"
)

func callToolRequest(toolName string, arguments string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: toolName, Arguments: []byte(arguments)}}
}

func fixedRequestIDGenerator(id string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return id, nil }
}

// fixedHexID returns a 32-character lowercase hex string built from a single
// repeated character, for tests that only need a stable, format-valid ID.
func fixedHexID(char byte) string {
	return strings.Repeat(string(char), 32)
}

func TestInvocationLoggingMiddlewareEmitsInfoInvocationAndCompletionLogsWithoutTelemetry(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('a')))
	next := middleware(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	result, err := next(ctx, "tools/call", callToolRequest(GetPartToolName, `{"id":42}`))

	r.NoError(err)
	r.NotNil(result)
	invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolInvocation
	})
	r.NotNil(invocation)
	a.Equal("INFO", invocation["level"])
	a.Equal(fixedHexID('a'), invocation["request_id"])
	a.Equal(GetPartToolName, invocation["tool"])
	a.Equal(int64(42), invocation["id"])

	completion := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolCompletion
	})
	r.NotNil(completion)
	a.Equal("INFO", completion["level"])
	a.Equal(string(OutcomeSuccess), completion["outcome"])
	a.Contains(completion, "duration")
}

func TestInvocationLoggingMiddlewareFailsClosedWhenRequestIDGenerationFails(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	nextCalled := false
	middleware := InvocationLoggingMiddleware(func(context.Context) (string, error) { return "", errors.New("random source exhausted") })
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		nextCalled = true
		return &mcp.CallToolResult{}, nil
	})

	result, err := next(ctx, "tools/call", callToolRequest(GetPartToolName, `{"id":42}`))

	r.NoError(err)
	a.False(nextCalled, "the wrapped handler must not run when request ID generation fails")
	callResult, ok := result.(*mcp.CallToolResult)
	r.True(ok)
	a.True(callResult.IsError)
	a.NotContains(callResult.Content[0].(*mcp.TextContent).Text, "random source exhausted")

	invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolInvocation
	})
	a.Nil(invocation, "no invocation record should claim a request ID that was never generated")
	errorRecord := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Level == slog.LevelError
	})
	r.NotNil(errorRecord)
	a.NotContains(errorRecord, "request_id")
}

func TestInvocationLoggingMiddlewareIgnoresNonToolCallMethods(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('b')))
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, nil
	})

	_, err := next(ctx, "tools/list", &mcp.ListToolsRequest{})

	r.NoError(err)
	invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolInvocation
	})
	a.Nil(invocation)
}

func TestInvocationLoggingMiddlewareIncludesSourceIPWhenPresent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = requestctx.WithSourceIP(ctx, netip.MustParseAddr("203.0.113.5"))

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('c')))
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	_, err := next(ctx, "tools/call", callToolRequest(HealthVersionToolName, `{}`))

	r.NoError(err)
	invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolInvocation
	})
	r.NotNil(invocation)
	a.Equal("203.0.113.5", invocation["source_ip"])
}

func TestInvocationLoggingMiddlewareOmitsSourceIPWhenAbsent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('d')))
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	_, err := next(ctx, "tools/call", callToolRequest(HealthVersionToolName, `{}`))

	r.NoError(err)
	invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolInvocation
	})
	r.NotNil(invocation)
	a.NotContains(invocation, "source_ip")
}

func TestInvocationLoggingMiddlewareIncludesTraceCorrelationOnlyForValidSpan(t *testing.T) {
	t.Parallel()

	// middleware/next are pure setup built once in the parent goroutine before
	// any subtest starts, so sharing them is safe. Each subtest below must
	// build its own require/assert objects bound to its own *testing.T: a
	// require/assert object bound to the parent t and invoked from a
	// t.Parallel() subtest goroutine would call FailNow on the wrong test.
	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('e')))
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	t.Run("valid span context is included", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		a := assert.New(t)
		ctx, handler, _ := testhandler.SetupTestHandler(t)

		traceID, err := apiTrace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
		r.NoError(err)
		spanID, err := apiTrace.SpanIDFromHex("00f067aa0ba902b7")
		r.NoError(err)
		validSpanContext := apiTrace.NewSpanContext(apiTrace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     spanID,
			TraceFlags: apiTrace.FlagsSampled,
		})
		ctx = apiTrace.ContextWithSpanContext(ctx, validSpanContext)

		_, err = next(ctx, "tools/call", callToolRequest(HealthVersionToolName, `{}`))
		r.NoError(err)

		invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
			return record.Msg == logEventToolInvocation
		})
		r.NotNil(invocation)
		a.Equal("4bf92f3577b34da6a3ce929d0e0e4736", invocation["trace_id"])
		a.Equal("00f067aa0ba902b7", invocation["span_id"])
	})

	t.Run("absent span context is omitted", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		a := assert.New(t)
		ctx, handler, _ := testhandler.SetupTestHandler(t)

		_, err := next(ctx, "tools/call", callToolRequest(HealthVersionToolName, `{}`))
		r.NoError(err)

		invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
			return record.Msg == logEventToolInvocation
		})
		r.NotNil(invocation)
		a.NotContains(invocation, "trace_id")
		a.NotContains(invocation, "span_id")
	})

	t.Run("zero span context is omitted", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		a := assert.New(t)
		ctx, handler, _ := testhandler.SetupTestHandler(t)
		ctx = apiTrace.ContextWithSpanContext(ctx, apiTrace.SpanContext{})

		_, err := next(ctx, "tools/call", callToolRequest(HealthVersionToolName, `{}`))
		r.NoError(err)

		invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
			return record.Msg == logEventToolInvocation
		})
		r.NotNil(invocation)
		a.NotContains(invocation, "trace_id")
		a.NotContains(invocation, "span_id")
	})
}

func TestInvocationLoggingMiddlewareNeverLogsRawSearchText(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('f')))
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	_, err := next(ctx, "tools/call", callToolRequest(SearchPartsToolName, `{"search":"do not log this secret","limit":5,"offset":10}`))

	r.NoError(err)
	invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolInvocation
	})
	r.NotNil(invocation)
	a.Equal(int64(5), invocation["limit"])
	a.Equal(int64(10), invocation["offset"])
	a.NotContains(invocation, "search")
	for _, value := range invocation {
		if text, ok := value.(string); ok {
			a.NotContains(text, "do not log this secret")
		}
	}
}

func TestInvocationLoggingMiddlewareToleratesMalformedArgumentsForAMappedTool(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('7')))
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	result, err := next(ctx, "tools/call", callToolRequest(GetPartToolName, `{invalid`))

	r.NoError(err)
	r.NotNil(result)
	invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolInvocation
	})
	r.NotNil(invocation)
	a.Equal(GetPartToolName, invocation["tool"])
	a.NotContains(invocation, "id")
	completion := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolCompletion
	})
	r.NotNil(completion)
	a.Equal(string(OutcomeSuccess), completion["outcome"])
}

func TestInvocationLoggingMiddlewareDefaultsUnmappedToolsToBaseFieldsOnly(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('1')))
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	_, err := next(ctx, "tools/call", callToolRequest("some_unmapped_tool", `{"anything":"should not appear","id":7}`))

	r.NoError(err)
	invocation := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolInvocation
	})
	r.NotNil(invocation)
	a.NotContains(invocation, "anything")
	a.NotContains(invocation, "id")
}

func TestInvocationLoggingMiddlewareClassifiesCancellationOverExplicitOutcome(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('2')))
	next := middleware(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		RecordOutcome(ctx, OutcomeValidationFailure)
		return &mcp.CallToolResult{}, nil
	})

	_, err := next(ctx, "tools/call", callToolRequest(HealthVersionToolName, `{}`))

	r.NoError(err)
	completion := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolCompletion
	})
	r.NotNil(completion)
	a.Equal(string(OutcomeCancellation), completion["outcome"])
}

func TestInvocationLoggingMiddlewareUsesExplicitOutcomeOverIsErrorDefault(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('3')))
	next := middleware(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		RecordOutcome(ctx, OutcomeValidationFailure)
		return &mcp.CallToolResult{IsError: true}, nil
	})

	_, err := next(ctx, "tools/call", callToolRequest(HealthVersionToolName, `{}`))

	r.NoError(err)
	completion := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolCompletion
	})
	r.NotNil(completion)
	a.Equal(string(OutcomeValidationFailure), completion["outcome"])
}

func TestInvocationLoggingMiddlewareFallsBackToInternalFailureWithoutExplicitOutcome(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('4')))
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{IsError: true}, nil
	})

	_, err := next(ctx, "tools/call", callToolRequest("some_unmapped_tool", `{}`))

	r.NoError(err)
	completion := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolCompletion
	})
	r.NotNil(completion)
	a.Equal(string(OutcomeInternalFailure), completion["outcome"])
}

func TestInvocationLoggingMiddlewareLogsBoundedResultCountWhenAvailable(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('5')))
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{StructuredContent: LookupOutput[int]{Status: StatusOK, Count: 3, Results: []int{1, 2, 3}}}, nil
	})

	_, err := next(ctx, "tools/call", callToolRequest(SearchPartsToolName, `{}`))

	r.NoError(err)
	completion := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolCompletion
	})
	r.NotNil(completion)
	a.Equal(int64(3), completion["result_count"])
}

func TestInvocationLoggingMiddlewareOmitsResultCountWhenUnavailable(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	middleware := InvocationLoggingMiddleware(fixedRequestIDGenerator(fixedHexID('6')))
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	_, err := next(ctx, "tools/call", callToolRequest(HealthVersionToolName, `{}`))

	r.NoError(err)
	completion := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventToolCompletion
	})
	r.NotNil(completion)
	a.NotContains(completion, "result_count")
}
