package inventree

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRequestAppliesTokenAndBearerAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		credential Credential
		wantHeader string
	}{
		{
			name:       "token",
			credential: Credential{Scheme: AuthSchemeToken, Token: "abc123"},
			wantHeader: "Token abc123",
		},
		{
			name:       "bearer",
			credential: Credential{Scheme: AuthSchemeBearer, Token: "abc123"},
			wantHeader: "Bearer abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			client, err := NewClient(Config{
				BaseURL:    "https://inventory.example.test",
				Credential: tt.credential,
				HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return jsonResponse(req, http.StatusOK, `{"ok":true}`), nil
				})},
			})
			r.NoError(err)

			req, err := client.NewRequest(context.Background(), http.MethodGet, "/api/part/", url.Values{"search": []string{"resistor"}}, nil)
			r.NoError(err)

			r.Equal("https://inventory.example.test/api/part/?search=resistor", req.URL.String())
			r.Equal(tt.wantHeader, req.Header.Get("Authorization"))
			r.Equal("application/json", req.Header.Get("Accept"))
		})
	}
}

func TestDoJSONMapsAPIErrorFields(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, http.StatusBadRequest, `{"detail":"invalid request","name":["This field is required."],"active":["Must be a valid boolean."]}`), nil
		})},
	})
	r.NoError(err)
	req, err := client.NewRequest(context.Background(), http.MethodPost, "/api/part/", nil, map[string]string{"name": ""})
	r.NoError(err)

	var out map[string]any
	err = client.DoJSON(req, &out)
	r.Error(err)

	var apiErr *APIError
	r.True(errors.As(err, &apiErr))
	a.Equal(http.StatusBadRequest, apiErr.StatusCode)
	a.Equal(ErrorKindValidation, apiErr.Kind)
	a.Equal(http.MethodPost, apiErr.Method)
	a.Equal("/api/part/", apiErr.Path)
	a.Equal("invalid request", apiErr.Detail)
	a.Equal([]string{"This field is required."}, apiErr.FieldErrors["name"])
	a.Equal([]string{"Must be a valid boolean."}, apiErr.FieldErrors["active"])
}

func TestDoJSONClassifiesCommonAPIErrorStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantKind   ErrorKind
		wantDetail string
	}{
		{
			name:       "conflict",
			statusCode: http.StatusConflict,
			body:       `{"detail":"duplicate"}`,
			wantKind:   ErrorKindConflict,
			wantDetail: "duplicate",
		},
		{
			name:       "validation unprocessable",
			statusCode: http.StatusUnprocessableEntity,
			body:       `{"detail":["invalid value"]}`,
			wantKind:   ErrorKindValidation,
			wantDetail: "invalid value",
		},
		{
			name:       "authentication",
			statusCode: http.StatusUnauthorized,
			body:       `{"detail":"bad token"}`,
			wantKind:   ErrorKindAuthentication,
			wantDetail: "bad token",
		},
		{
			name:       "permission",
			statusCode: http.StatusForbidden,
			body:       `{"detail":"denied"}`,
			wantKind:   ErrorKindPermission,
			wantDetail: "denied",
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			body:       `{"detail":"missing"}`,
			wantKind:   ErrorKindNotFound,
			wantDetail: "missing",
		},
		{
			name:       "rate limit",
			statusCode: http.StatusTooManyRequests,
			body:       `{"detail":"slow down"}`,
			wantKind:   ErrorKindRateLimit,
			wantDetail: "slow down",
		},
		{
			name:       "server",
			statusCode: http.StatusInternalServerError,
			body:       `{"detail":"broken"}`,
			wantKind:   ErrorKindServer,
			wantDetail: "broken",
		},
		{
			name:       "unexpected non json",
			statusCode: http.StatusTeapot,
			body:       `plain text failure`,
			wantKind:   ErrorKindUnexpected,
			wantDetail: "plain text failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)

			client, err := NewClient(Config{
				BaseURL:    "https://inventory.example.test",
				Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
				HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return jsonResponse(req, tt.statusCode, tt.body), nil
				})},
			})
			r.NoError(err)
			req, err := client.NewRequest(context.Background(), http.MethodGet, "/api/part/10/", nil, nil)
			r.NoError(err)

			err = client.DoJSON(req, nil)
			r.Error(err)

			var apiErr *APIError
			r.True(errors.As(err, &apiErr))
			a.Equal(tt.statusCode, apiErr.StatusCode)
			a.Equal(tt.wantKind, apiErr.Kind)
			a.Equal(tt.wantDetail, apiErr.Detail)
		})
	}
}

func TestAPIErrorStringIncludesDetailWhenAvailable(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	withDetail := &APIError{
		StatusCode: http.StatusNotFound,
		Method:     http.MethodGet,
		Path:       "/api/part/10/",
		Detail:     "missing",
	}
	withoutDetail := &APIError{
		StatusCode: http.StatusInternalServerError,
		Method:     http.MethodPost,
		Path:       "/api/part/",
	}

	a.Equal("InvenTree GET /api/part/10/ failed with 404: missing", withDetail.Error())
	a.Equal("InvenTree POST /api/part/ failed with 500", withoutDetail.Error())
}

func TestNewClientRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	_, err := NewClient(Config{
		BaseURL: "ftp://inventory.example.test",
		Credential: Credential{
			Scheme: AuthScheme("Basic"),
		},
	})
	r.Error(err)
	a.Contains(err.Error(), "base URL scheme must be http or https")
	a.Contains(err.Error(), "auth scheme must be")

	_, err = NewClient(Config{
		BaseURL: "https://inventory.example.test",
		Credential: Credential{
			Scheme: AuthSchemeToken,
		},
	})
	r.Error(err)
	a.Contains(err.Error(), "token is required")
}

func TestNewRequestRejectsInvalidPathAndBody(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
	})
	r.NoError(err)

	_, err = client.NewRequest(context.Background(), http.MethodGet, "api/part/", nil, nil)
	r.Error(err)
	a.Contains(err.Error(), "path must start with /")

	_, err = client.NewRequest(context.Background(), http.MethodGet, "//evil.example.test/api/part/", nil, nil)
	r.Error(err)
	a.Contains(err.Error(), "must not be protocol-relative")

	_, err = client.NewRequest(context.Background(), http.MethodPost, "/api/part/", nil, func() {})
	r.Error(err)
	a.Contains(err.Error(), "encode request body")
}

func TestDoJSONReportsDecodeAndDiscardErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		body      io.ReadCloser
		out       any
		wantError string
	}{
		{
			name:      "decode",
			status:    http.StatusOK,
			body:      io.NopCloser(strings.NewReader(`not-json`)),
			out:       &struct{}{},
			wantError: "decode InvenTree response",
		},
		{
			name:      "discard",
			status:    http.StatusNoContent,
			body:      errReadCloser{},
			out:       nil,
			wantError: "discard InvenTree response body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			client, err := NewClient(Config{
				BaseURL:    "https://inventory.example.test",
				Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
				HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: tt.status,
						Body:       tt.body,
						Request:    req,
					}, nil
				})},
			})
			r.NoError(err)
			req, err := client.NewRequest(context.Background(), http.MethodGet, "/api/part/", nil, nil)
			r.NoError(err)

			err = client.DoJSON(req, tt.out)
			r.Error(err)
			r.Contains(err.Error(), tt.wantError)
		})
	}
}

func TestListAllFollowsPagination(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var requested []string
	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeBearer, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requested = append(requested, req.URL.String())
			switch req.URL.Query().Get("offset") {
			case "":
				return jsonResponse(req, http.StatusOK, `{"count":3,"next":"https://inventory.example.test/api/part/?limit=2&offset=2","previous":null,"results":[{"pk":1},{"pk":2}]}`), nil
			case "2":
				return jsonResponse(req, http.StatusOK, `{"count":3,"next":null,"previous":"https://inventory.example.test/api/part/?limit=2","results":[{"pk":3}]}`), nil
			default:
				return jsonResponse(req, http.StatusInternalServerError, `{"detail":"unexpected page"}`), nil
			}
		})},
	})
	r.NoError(err)

	type part struct {
		PK int `json:"pk"`
	}
	parts, err := ListAll[part](context.Background(), client, "/api/part/", url.Values{"limit": []string{"2"}})
	r.NoError(err)

	r.Equal([]part{{PK: 1}, {PK: 2}, {PK: 3}}, parts)
	a.Equal([]string{
		"https://inventory.example.test/api/part/?limit=2",
		"https://inventory.example.test/api/part/?limit=2&offset=2",
	}, requested)
}

func TestListAllRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	_, err := ListAll[struct{}](context.Background(), nil, "/api/part/", nil)
	r.Error(err)
	a.Contains(err.Error(), "client is required")

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, http.StatusOK, `{"count":1,"next":"://bad-url","previous":null,"results":[]}`), nil
		})},
	})
	r.NoError(err)

	_, err = ListAll[struct{}](context.Background(), client, "/api/part/", nil)
	r.Error(err)
}

func TestPatchFieldsPreserveOmittedAndExplicitZeroValues(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var patchBody map[string]any
	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			r.Equal(http.MethodPatch, req.Method)
			r.Equal("application/json", req.Header.Get("Content-Type"))
			r.NoError(json.NewDecoder(req.Body).Decode(&patchBody))
			return jsonResponse(req, http.StatusOK, `{"pk":10}`), nil
		})},
	})
	r.NoError(err)

	var out struct {
		PK int `json:"pk"`
	}
	err = client.Patch(context.Background(), "/api/part/10/", PatchFields{
		"active":      Set(false),
		"description": Set(""),
		"keywords":    Set([]string{}),
		"minimum":     Set(0),
		"category":    Null(),
	}, &out)
	r.NoError(err)

	a.Equal(10, out.PK)
	a.Contains(patchBody, "active")
	a.Contains(patchBody, "description")
	a.Contains(patchBody, "keywords")
	a.Contains(patchBody, "minimum")
	a.Contains(patchBody, "category")
	a.NotContains(patchBody, "omitted")
	a.Equal(false, patchBody["active"])
	a.Equal("", patchBody["description"])
	a.Equal([]any{}, patchBody["keywords"])
	a.Equal(float64(0), patchBody["minimum"])
	a.Nil(patchBody["category"])
}

func TestPatchRejectsNoOpUpdateBeforeRequest(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("empty PATCH should not send a request")
			return nil, nil
		})},
	})
	r.NoError(err)

	err = client.Patch(context.Background(), "/api/part/10/", PatchFields{}, nil)
	r.Error(err)
	r.Contains(err.Error(), "PATCH requires at least one field")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (errReadCloser) Close() error {
	return nil
}
