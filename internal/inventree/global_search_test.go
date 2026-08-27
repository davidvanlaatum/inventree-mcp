package inventree

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobalSearchSendsExpectedRequest(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			a.Equal(http.MethodPost, req.Method)
			a.Equal("/api/search/", req.URL.Path)

			var body map[string]any
			r.NoError(json.NewDecoder(req.Body).Decode(&body))
			a.Equal("resistor", body["search"])
			a.Equal(true, body["search_regex"])
			a.Equal(true, body["search_whole"])
			a.Equal(true, body["search_notes"])
			a.Equal(float64(7), body["limit"])
			a.Equal(map[string]any{}, body["part"])
			a.Equal(map[string]any{}, body["company"])
			a.NotContains(body, "stockitem")

			return jsonResponse(req, http.StatusOK, `{
				"part": {"count": 1, "next": null, "previous": null, "results": [{"pk": 1, "name": "resistor"}]},
				"company": {"count": 0, "next": null, "previous": null, "results": []}
			}`), nil
		})},
	})
	r.NoError(err)

	result, err := client.GlobalSearch(ctx, GlobalSearchQuery{
		Search:      "resistor",
		SearchRegex: true,
		SearchWhole: true,
		SearchNotes: true,
		ObjectTypes: []GlobalSearchObjectType{GlobalSearchPart, GlobalSearchCompany},
		Limit:       7,
	})
	r.NoError(err)
	r.NotNil(result.Parts)
	a.Equal(1, result.Parts.Count)
	a.Len(result.Parts.Results, 1)
	a.Equal(1, result.Parts.Results[0].PK)
	r.NotNil(result.Companies)
	a.Zero(result.Companies.Count)
	a.Empty(result.Companies.Results)
	a.Nil(result.StockItems)
}

func TestGlobalSearchDefaultsLimitWhenUnset(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var body map[string]any
			r.NoError(json.NewDecoder(req.Body).Decode(&body))
			a.Equal(float64(20), body["limit"])
			return jsonResponse(req, http.StatusOK, `{"part": {"count": 0, "next": null, "previous": null, "results": []}}`), nil
		})},
	})
	r.NoError(err)

	_, err = client.GlobalSearch(ctx, GlobalSearchQuery{Search: "x", ObjectTypes: []GlobalSearchObjectType{GlobalSearchPart}})
	r.NoError(err)
}

func TestGlobalSearchValidatesInputBeforeSendingAnyRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query GlobalSearchQuery
		want  string
	}{
		{
			name:  "empty search",
			query: GlobalSearchQuery{ObjectTypes: []GlobalSearchObjectType{GlobalSearchPart}},
			want:  "non-empty search term",
		},
		{
			name:  "no object types",
			query: GlobalSearchQuery{Search: "x"},
			want:  "at least one object type",
		},
		{
			name:  "unsupported object type",
			query: GlobalSearchQuery{Search: "x", ObjectTypes: []GlobalSearchObjectType{"salesorder"}},
			want:  "does not support object type",
		},
		{
			name:  "duplicate object type",
			query: GlobalSearchQuery{Search: "x", ObjectTypes: []GlobalSearchObjectType{GlobalSearchPart, GlobalSearchPart}},
			want:  "more than once",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)

			client, err := NewClient(Config{
				BaseURL:    "https://inventory.example.test",
				Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
				HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					t.Fatal("GlobalSearch must validate input before issuing any HTTP request")
					return nil, nil
				})},
			})
			r.NoError(err)

			_, err = client.GlobalSearch(ctx, tt.query)
			r.Error(err)
			a.Contains(err.Error(), tt.want)
		})
	}
}

func TestGlobalSearchFailsClosedOnMissingRequestedBucket(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			// InvenTree silently omits any key it does not recognize rather
			// than erroring, so a requested-but-missing bucket in an
			// otherwise-successful response simulates upstream schema drift.
			return jsonResponse(req, http.StatusOK, `{}`), nil
		})},
	})
	r.NoError(err)

	_, err = client.GlobalSearch(ctx, GlobalSearchQuery{Search: "x", ObjectTypes: []GlobalSearchObjectType{GlobalSearchPart}})
	r.ErrorIs(err, ErrGlobalSearchSchemaDrift)
}

func TestGlobalSearchWrapsBucketDecodeErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, http.StatusOK, `{"part": "not an object"}`), nil
		})},
	})
	r.NoError(err)

	_, err = client.GlobalSearch(ctx, GlobalSearchQuery{Search: "x", ObjectTypes: []GlobalSearchObjectType{GlobalSearchPart}})
	r.Error(err)
	r.Contains(err.Error(), `decode global search "part" results`)
}

func TestValidGlobalSearchObjectType(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	for _, objectType := range SupportedGlobalSearchObjectTypes {
		a.True(ValidGlobalSearchObjectType(objectType))
	}
	a.False(ValidGlobalSearchObjectType("salesorder"))
	a.False(ValidGlobalSearchObjectType("build"))
	a.False(ValidGlobalSearchObjectType(""))
}
