package inventree

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func fakeResponse(req *http.Request, statusCode int) *http.Response {
	return &http.Response{StatusCode: statusCode, Body: http.NoBody, Request: req}
}

func newTestRequest(t *testing.T, ctx context.Context, method, path string, contentType string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, "https://inventory.example.test"+path, nil).WithContext(ctx)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func TestWrapRequestLoggingPassesThroughUnmatchedRequestsUnlogged(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = requestctx.WithCorrelation(ctx)

	called := false
	next := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return fakeResponse(req, http.StatusOK), nil
	})

	resp, err := WrapRequestLogging(next).RoundTrip(newTestRequest(t, ctx, http.MethodGet, "/api/user/me/", ""))

	r.NoError(err)
	r.NotNil(resp)
	a.True(called, "an unmatched request must still reach the wrapped transport")
	a.Nil(handler.FirstMatchingLogForAssert(func(testhandler.LogRecord) bool { return true }), "no log record should be emitted for a route the registry doesn't cover")
}

func TestWrapRequestLoggingEmitsStartedAndCompletedForAMatchedRequest(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("tool", "get_part"))
	ctx = requestctx.WithCorrelation(ctx)

	next := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return fakeResponse(req, http.StatusOK), nil
	})

	_, err := WrapRequestLogging(next).RoundTrip(newTestRequest(t, ctx, http.MethodGet, "/api/part/42/", ""))
	r.NoError(err)

	started := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestStarted
	})
	r.NotNil(started)
	a.Equal("GET", started["method"])
	a.Equal("get_part", started["operation"])
	a.Equal("json_api", started["family"])
	a.Equal("get_part", started["tool"])
	a.Equal(int64(1), started["call_sequence"])
	a.Equal(int64(1), started["attempt"])

	completed := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestCompleted
	})
	r.NotNil(completed)
	a.Equal("2xx", completed["status_class"])
	a.Contains(completed, "duration")
	a.NotContains(completed, "error_kind")
}

func TestWrapRequestLoggingLogsErrorKindOnFailureStatus(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = requestctx.WithCorrelation(ctx)

	next := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return fakeResponse(req, http.StatusNotFound), nil
	})

	_, err := WrapRequestLogging(next).RoundTrip(newTestRequest(t, ctx, http.MethodGet, "/api/part/42/", ""))
	r.NoError(err)

	completed := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestCompleted
	})
	r.NotNil(completed)
	a.Equal("4xx", completed["status_class"])
	a.Equal("not_found", completed["error_kind"])
}

func TestWrapRequestLoggingLogsTransportErrorWithoutLeakingItsText(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = requestctx.WithCorrelation(ctx)

	next := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp 10.0.0.5:443: connection refused, credential=secret")
	})

	_, err := WrapRequestLogging(next).RoundTrip(newTestRequest(t, ctx, http.MethodGet, "/api/part/42/", ""))
	r.Error(err)

	completed := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestCompleted
	})
	r.NotNil(completed)
	a.Equal("transport_error", completed["status_class"])
	for _, value := range completed {
		if text, ok := value.(string); ok {
			a.NotContains(text, "secret")
			a.NotContains(text, "10.0.0.5")
		}
	}
}

func TestWrapRequestLoggingOmitsCallSequenceWithoutCorrelation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)

	next := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return fakeResponse(req, http.StatusOK), nil
	})

	_, err := WrapRequestLogging(next).RoundTrip(newTestRequest(t, ctx, http.MethodGet, "/api/part/42/", ""))
	r.NoError(err)

	started := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestStarted
	})
	r.NotNil(started, "a call outside any tracked invocation must still be logged, just without a sequence")
	a.NotContains(started, "call_sequence")
	a.NotContains(started, "attempt")
}

func TestWrapRequestLoggingSuppressesDetailAfterCallSequenceCap(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = requestctx.WithCorrelation(ctx)
	for i := 0; i < 64; i++ {
		_, ok := requestctx.NextCallSequence(ctx)
		r.True(ok)
	}

	called := false
	next := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return fakeResponse(req, http.StatusOK), nil
	})

	_, err := WrapRequestLogging(next).RoundTrip(newTestRequest(t, ctx, http.MethodGet, "/api/part/42/", ""))
	r.NoError(err)

	a.True(called, "the underlying HTTP call must still execute past the cap; only logging detail is suppressed")
	started := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestStarted
	})
	a.Nil(started, "no detailed started log once the cap is exceeded")
	overflow := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventCallSequenceOverflow
	})
	r.NotNil(overflow, "exactly one overflow marker must be emitted")
}

func TestWrapRequestLoggingIncludesSystemOperationCallerWhenSet(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = requestctx.WithSystemOperation(ctx, "startup_check")
	ctx = requestctx.WithCorrelation(ctx)

	next := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return fakeResponse(req, http.StatusOK), nil
	})

	_, err := WrapRequestLogging(next).RoundTrip(newTestRequest(t, ctx, http.MethodGet, "/api/version/", ""))
	r.NoError(err)

	started := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestStarted
	})
	r.NotNil(started)
	a.Equal("system.startup_check", started["caller"])
}

func TestWrapRequestLoggingDisambiguatesMultipartUpdateFromJSONUpdateByContentType(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = requestctx.WithCorrelation(ctx)

	next := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return fakeResponse(req, http.StatusOK), nil
	})

	_, err := WrapRequestLogging(next).RoundTrip(newTestRequest(t, ctx, http.MethodPatch, "/api/part/42/", "multipart/form-data; boundary=x"))
	r.NoError(err)

	started := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestStarted
	})
	r.NotNil(started)
	a.Equal("multipart_api", started["family"])
	a.Equal("update_part", started["operation"])
}

func TestWrapRequestLoggingResolvesDownloadFamiliesByExplicitMarkerNotPath(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = requestctx.WithCorrelation(ctx)
	ctx = requestctx.WithExplicitRoute(ctx, "download_part_image_content", string(RequestFamilyImageDownload))

	next := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return fakeResponse(req, http.StatusOK), nil
	})

	_, err := WrapRequestLogging(next).RoundTrip(newTestRequest(t, ctx, http.MethodGet, "/media/part_images/42/thumb.png", ""))
	r.NoError(err)

	started := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestStarted
	})
	r.NotNil(started)
	a.Equal("download_part_image_content", started["operation"])
	a.Equal("image_download", started["family"])
}

func TestResolveRouteReturnsFalseForNilRequestOrURL(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	_, ok := resolveRoute(nil)
	a.False(ok)

	_, ok = resolveRoute(&http.Request{})
	a.False(ok)
}

func TestPathMatchesTemplate(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	a.True(pathMatchesTemplate("/api/part/42/", "/api/part/{id}/"))
	a.True(pathMatchesTemplate("api/part/42", "/api/part/{id}/"))
	a.False(pathMatchesTemplate("/api/part/42/extra/", "/api/part/{id}/"))
	a.False(pathMatchesTemplate("/api/part/", "/api/part/{id}/"))
	a.False(pathMatchesTemplate("/api/company/42/", "/api/part/{id}/"))
}

func TestStatusClassOf(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	a.Equal("2xx", statusClassOf(200))
	a.Equal("3xx", statusClassOf(301))
	a.Equal("4xx", statusClassOf(404))
	a.Equal("5xx", statusClassOf(500))
	a.Equal("other", statusClassOf(0))
}

// TestWrapRequestLoggingThroughARealClientCallSequencesTwoCallsAndResolvesTheDownloadMarker
// exercises the full stack end to end: a real Client, wrapped with
// WrapRequestLogging, making a real DownloadAttachment call (which itself
// issues two outbound requests: a json_api metadata GET, then the raw
// attachment_download GET identified by the explicit route marker
// DownloadAttachment sets on itself). It proves call_sequence genuinely
// increments across two real calls in one invocation (not just in
// requestctx's own unit tests), and that the download-family explicit-route
// wiring in read_methods.go is actually read correctly by a live
// RoundTripper, not only by a synthetic *http.Request. It also scans every
// logged field on both records for the attachment's signed-URL query
// string, proving redaction holds across the whole field set, not just the
// two fields spot-checked elsewhere.
func TestWrapRequestLoggingThroughARealClientCallSequencesTwoCallsAndResolvesTheDownloadMarker(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = requestctx.WithCorrelation(ctx)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: WrapRequestLogging(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/api/attachment/90/":
				body := `{"pk":90,"model_type":"part","model_id":10,"attachment":"/media/attachments/datasheet.pdf?signature=do-not-log-me","filename":"datasheet.pdf"}`
				return jsonResponse(req, http.StatusOK, body), nil
			case "/media/attachments/datasheet.pdf":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/pdf"}},
					Body:       http.NoBody,
					Request:    req,
				}, nil
			default:
				return jsonResponse(req, http.StatusNotFound, `{"detail":"unexpected path"}`), nil
			}
		}))},
	})
	r.NoError(err)

	_, err = client.DownloadAttachment(ctx, 90, AttachmentContentOriginal, 1024)
	r.NoError(err)

	started := handler.FindAllMatchingLogsForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestStarted
	})
	r.Len(started, 2, "one metadata call plus one content download call")
	a.Equal("get_attachment_metadata", started[0]["operation"])
	a.Equal("json_api", started[0]["family"])
	a.Equal(int64(1), started[0]["call_sequence"])
	a.Equal("download_attachment_content", started[1]["operation"])
	a.Equal("attachment_download", started[1]["family"])
	a.Equal(int64(2), started[1]["call_sequence"], "the second real outbound call in the same invocation must sequence to 2, not restart at 1")

	completed := handler.FindAllMatchingLogsForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == logEventRequestCompleted
	})
	r.Len(completed, 2)

	for _, record := range append(started, completed...) {
		for key, value := range record {
			text, ok := value.(string)
			if !ok {
				continue
			}
			a.NotContains(text, "do-not-log-me", "field %q must not leak the signed download URL's query string", key)
			a.NotContains(text, "signature=", "field %q must not leak any part of a query string", key)
			a.NotContains(text, "/media/", "field %q must not leak the raw request path", key)
			a.NotContains(text, "secret", "field %q must not leak the credential", key)
		}
	}
}
