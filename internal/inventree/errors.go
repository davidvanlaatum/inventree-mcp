package inventree

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxErrorBodyBytes = 1 << 20

type APIError struct {
	StatusCode  int
	Kind        ErrorKind
	Method      string
	Path        string
	Detail      string
	FieldErrors map[string][]string
}

type ErrorKind string

const (
	ErrorKindValidation     ErrorKind = "validation"
	ErrorKindAuthentication ErrorKind = "authentication"
	ErrorKindPermission     ErrorKind = "permission"
	ErrorKindNotFound       ErrorKind = "not_found"
	ErrorKindConflict       ErrorKind = "conflict"
	ErrorKindRateLimit      ErrorKind = "rate_limit"
	ErrorKindServer         ErrorKind = "server"
	ErrorKindUnexpected     ErrorKind = "unexpected"
)

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("InvenTree %s %s failed with %d: %s", e.Method, e.Path, e.StatusCode, e.Detail)
	}
	return fmt.Sprintf("InvenTree %s %s failed with %d", e.Method, e.Path, e.StatusCode)
}

func parseAPIError(resp *http.Response) error {
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	apiErr := &APIError{
		StatusCode:  resp.StatusCode,
		Kind:        classifyStatus(resp.StatusCode),
		Method:      resp.Request.Method,
		Path:        resp.Request.URL.Path,
		FieldErrors: map[string][]string{},
	}
	if readErr != nil {
		apiErr.Detail = "failed to read error response"
		return apiErr
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		apiErr.Detail = strings.TrimSpace(string(body))
		return apiErr
	}
	apiErr.Detail = firstString(payload["detail"])
	for key, value := range payload {
		if key == "detail" {
			continue
		}
		messages := stringList(value)
		if len(messages) > 0 {
			apiErr.FieldErrors[key] = messages
		}
	}
	if apiErr.Detail == "" {
		apiErr.Detail = http.StatusText(resp.StatusCode)
	}
	return apiErr
}

func classifyStatus(status int) ErrorKind {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return ErrorKindValidation
	case http.StatusUnauthorized:
		return ErrorKindAuthentication
	case http.StatusForbidden:
		return ErrorKindPermission
	case http.StatusNotFound:
		return ErrorKindNotFound
	case http.StatusConflict:
		return ErrorKindConflict
	// 408 (Request Timeout) shares 429's "the caller's request pacing/timing
	// exceeded what the server tolerated, back off rather than retry
	// blindly" semantics, so it is folded into the same classification
	// (F-S81) rather than left an implicit default-case unexpected error.
	case http.StatusTooManyRequests, http.StatusRequestTimeout:
		return ErrorKindRateLimit
	// 425 (Too Early) is a TLS early-data replay-protection status with no
	// InvenTree-specific meaning ever observed; this explicit case documents
	// that the Unexpected classification is deliberate, not an oversight
	// (F-S81), even though it resolves the same as the default branch below.
	case http.StatusTooEarly:
		return ErrorKindUnexpected
	default:
		if status >= http.StatusInternalServerError {
			return ErrorKindServer
		}
		return ErrorKindUnexpected
	}
}

func firstString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		if len(typed) > 0 {
			return firstString(typed[0])
		}
	}
	return ""
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		messages := make([]string, 0, len(typed))
		for _, item := range typed {
			if message, ok := item.(string); ok {
				messages = append(messages, message)
			}
		}
		return messages
	case map[string]any:
		encoded, err := json.Marshal(typed)
		if err == nil {
			return []string{string(encoded)}
		}
	}
	return nil
}
