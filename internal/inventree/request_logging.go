package inventree

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
)

const (
	logEventRequestStarted       = "inventree_request_started"
	logEventRequestCompleted     = "inventree_request_completed"
	logEventCallSequenceOverflow = "inventree_request_call_sequence_overflow"

	// requestAttempt is always 1: no retry policy exists anywhere in this
	// codebase yet (internal/batch's Execute makes exactly one attempt per
	// item). The field is emitted now, capped and ready, so a future retry
	// policy only needs to pass its own attempt number through, not add a
	// new log contract.
	requestAttempt = 1
)

// WrapRequestLogging instruments outbound InvenTree HTTP requests with
// bounded, safe inventree_request_started/inventree_request_completed INFO
// log records: HTTP method, a closed-registry operation value, request
// family, call sequence, attempt, duration, and status class. It never logs
// request/response bodies, credentials, raw query values, or full URLs.
//
// Only requests whose method+path resolve against the closed
// clientMethodRoutes registry are logged; everything else passes through
// unlogged. This is also how OAuth, bootstrap, MCP well-known metadata, and
// arbitrary URL-fetch traffic stay excluded: callers achieve that by only
// wrapping the transport used by the server's long-lived tool-facing
// InvenTree client (see cmd/inventree-mcp's inventreeHTTPClient), never the
// throwaway clients oauth.InvenTreeCredentialBroker or internal/upload's
// URL fetcher construct for themselves.
func WrapRequestLogging(transport http.RoundTripper) http.RoundTripper {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return requestLoggingRoundTripper{next: transport}
}

type requestLoggingRoundTripper struct {
	next http.RoundTripper
}

// Unwrap exposes the wrapped transport, following the standard library's
// unwrap convention so callers (and tests) can reach the underlying
// transport through layered wrapping such as telemetry.WrapHTTPClient.
func (rt requestLoggingRoundTripper) Unwrap() http.RoundTripper { return rt.next }

func (rt requestLoggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	route, ok := resolveRoute(req)
	if !ok {
		return rt.next.RoundTrip(req)
	}
	ctx := req.Context()
	logger := logging.FromContext(ctx)
	if systemOperation, ok := requestctx.SystemOperation(ctx); ok {
		logger = logger.With(slog.String("caller", "system."+systemOperation))
	}

	fields := []any{
		slog.String("method", req.Method),
		slog.String("operation", route.ManifestID),
		slog.String("family", string(route.Family)),
	}
	if requestctx.HasCorrelation(ctx) {
		sequence, withinCap := requestctx.NextCallSequence(ctx)
		if !withinCap {
			if requestctx.CallSequenceOverflowed(ctx) {
				logger.WarnContext(ctx, logEventCallSequenceOverflow)
			}
			return rt.next.RoundTrip(req)
		}
		fields = append(fields, slog.Int("call_sequence", sequence), slog.Int("attempt", requestAttempt))
	}
	logger.InfoContext(ctx, logEventRequestStarted, fields...)

	started := time.Now()
	resp, err := rt.next.RoundTrip(req)
	duration := time.Since(started)

	completedFields := append(append([]any{}, fields...), slog.Duration("duration", duration))
	switch {
	case err != nil:
		completedFields = append(completedFields, slog.String("status_class", "transport_error"))
	case resp.StatusCode >= http.StatusBadRequest:
		completedFields = append(completedFields,
			slog.String("status_class", statusClassOf(resp.StatusCode)),
			slog.String("error_kind", string(classifyStatus(resp.StatusCode))),
		)
	default:
		completedFields = append(completedFields, slog.String("status_class", statusClassOf(resp.StatusCode)))
	}
	logger.InfoContext(ctx, logEventRequestCompleted, completedFields...)
	return resp, err
}

// statusClassOf buckets a response status into a fixed, bounded class. On a
// failure response (>=400), error_kind (see classifyStatus) is logged
// alongside it as the additional closed safe error class.
func statusClassOf(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return "2xx"
	case statusCode >= 300 && statusCode < 400:
		return "3xx"
	case statusCode >= 400 && statusCode < 500:
		return "4xx"
	case statusCode >= 500:
		return "5xx"
	default:
		return "other"
	}
}

// resolveRoute identifies the registry entry req belongs to. Download
// methods (DownloadAttachment, DownloadPartImage, DownloadCompanyImage,
// DownloadDataOutput) set an explicit requestctx route marker before
// issuing their request, because InvenTree's opaque signed /media/... URLs
// carry no path information distinguishing which of the four issued them;
// that marker takes priority when present.
//
// Everything else is identified by matching req's method and path against
// the closed clientMethodRoutes registry. Multiple registry entries commonly
// share one (method, path template) pair. Most often that's List/Page/Query
// variants of the identical operation (same ManifestID, interchangeable). A
// PATCH to a part/company's own URL is genuinely ambiguous between an
// ordinary JSON field update and a multipart primary-image replacement
// (SetPartPrimaryImage/SetCompanyPrimaryImage reuse UpdatePart/UpdateCompany's
// exact path) — that case is resolved below by the outgoing request's
// Content-Type. A few GET list endpoints share one path across genuinely
// different ManifestIDs distinguished only by query filter (for example
// SearchCompanies vs SearchSuppliers/SearchManufacturers on
// /api/company/, is_supplier/is_manufacturer notwithstanding); resolveRoute
// cannot see query parameters, so it picks an arbitrary member of that group
// (see TestClientMethodRoutesDocumentsQueryDisambiguatedGroups) — a bounded
// precision limit (the logged operation always names a real candidate
// endpoint for the exact URL hit), not a correctness bug.
func resolveRoute(req *http.Request) (clientRoute, bool) {
	if req == nil || req.URL == nil {
		return clientRoute{}, false
	}
	if operation, family, ok := requestctx.ExplicitRoute(req.Context()); ok {
		return clientRoute{Method: req.Method, Path: req.URL.Path, Family: RequestFamily(family), ManifestID: operation}, true
	}
	isMultipart := strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/")
	fallback, haveFallback := clientRoute{}, false
	for _, route := range clientMethodRoutes {
		if route.Method != req.Method || !pathMatchesTemplate(req.URL.Path, route.Path) {
			continue
		}
		if (route.Family == RequestFamilyMultipartAPI) == isMultipart {
			return route, true
		}
		if !haveFallback {
			fallback, haveFallback = route, true
		}
	}
	return fallback, haveFallback
}

// pathMatchesTemplate reports whether actual matches template, where
// template segments wrapped in "{...}" match any single non-empty actual
// path segment.
func pathMatchesTemplate(actual, template string) bool {
	actualParts := strings.Split(strings.Trim(actual, "/"), "/")
	templateParts := strings.Split(strings.Trim(template, "/"), "/")
	if len(actualParts) != len(templateParts) {
		return false
	}
	for i, part := range templateParts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			if actualParts[i] == "" {
				return false
			}
			continue
		}
		if part != actualParts[i] {
			return false
		}
	}
	return true
}
