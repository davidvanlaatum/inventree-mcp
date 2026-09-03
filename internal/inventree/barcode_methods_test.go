package inventree

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBarcodeMethodsUseExpectedEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		call       func(context.Context, *Client) error
		wantMethod string
		wantPath   string
		wantQuery  url.Values
		wantBody   map[string]any
		response   string
	}{
		{
			name: "generate barcode",
			call: func(ctx context.Context, client *Client) error {
				text, err := client.GenerateBarcode(ctx, "stockitem", 12)
				if err == nil && text != "generated-text" {
					err = assert.AnError
				}
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/api/barcode/generate/",
			wantBody:   map[string]any{"model": "stockitem", "pk": float64(12)},
			response:   `{"barcode":"generated-text"}`,
		},
		{
			name: "link barcode",
			call: func(ctx context.Context, client *Client) error {
				return client.LinkBarcode(ctx, "abc123", "stockitem", 12)
			},
			wantMethod: http.MethodPost,
			wantPath:   "/api/barcode/link/",
			wantBody:   map[string]any{"barcode": "abc123", "stockitem": float64(12)},
			response:   `{"success":"Assigned barcode to stockitem instance"}`,
		},
		{
			name: "unlink barcode",
			call: func(ctx context.Context, client *Client) error {
				return client.UnlinkBarcode(ctx, "purchaseorder", 7)
			},
			wantMethod: http.MethodPost,
			wantPath:   "/api/barcode/unlink/",
			wantBody:   map[string]any{"purchaseorder": float64(7)},
			response:   `{"success":"Barcode unassigned from purchaseorder instance"}`,
		},
		{
			name: "search barcode scan history forwards real filters only",
			call: func(ctx context.Context, client *Client) error {
				resultFilter := true
				userID := 9
				page, err := client.SearchBarcodeScanHistoryPage(ctx, BarcodeScanHistoryQuery{Result: &resultFilter, UserID: &userID, Search: "abc", Ordering: "-timestamp", Limit: 50, Offset: 10})
				if err == nil && (!page.HasMore || page.Count != 3 || len(page.Results) != 1) {
					err = assert.AnError
				}
				return err
			},
			wantMethod: http.MethodGet,
			wantPath:   "/api/barcode/history/",
			wantQuery:  url.Values{"result": []string{"true"}, "user": []string{"9"}, "search": []string{"abc"}, "ordering": []string{"-timestamp"}, "limit": []string{"50"}, "offset": []string{"10"}},
			response:   `{"count":3,"next":"https://inventory.example.test/api/barcode/history/?offset=60","previous":null,"results":[{"pk":1,"data":"abc123","timestamp":"2026-01-01T00:00:00Z","endpoint":"stock-detail","result":true,"user":9}]}`,
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
					a.Equal(tt.wantMethod, req.Method)
					a.Equal(tt.wantPath, req.URL.Path)
					if tt.wantQuery != nil {
						a.Equal(tt.wantQuery.Encode(), req.URL.Query().Encode())
					}
					a.Equal("Token secret", req.Header.Get("Authorization"))
					if tt.wantBody != nil {
						var body map[string]any
						r.NoError(json.NewDecoder(req.Body).Decode(&body))
						a.Equal(tt.wantBody, body)
					}
					return jsonResponse(req, http.StatusOK, tt.response), nil
				})},
			})
			r.NoError(err)

			r.NoError(tt.call(ctx, client))
		})
	}
}

func TestSearchBarcodeScanHistoryPageDoesNotForwardClientSideOnlyFilters(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			a.NotContains(req.URL.Query(), "endpoint")
			a.NotContains(req.URL.Query(), "from")
			a.NotContains(req.URL.Query(), "to")
			return jsonResponse(req, http.StatusOK, `{"count":0,"next":null,"previous":null,"results":[]}`), nil
		})},
	})
	r.NoError(err)

	_, err = client.SearchBarcodeScanHistoryPage(ctx, BarcodeScanHistoryQuery{Limit: 20})
	r.NoError(err)
}

func TestResolveBarcodeHandlesMatchNoMatchAndOutOfScopeType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		response     string
		status       int
		fieldErrors  map[string][]string
		wantMatched  bool
		wantErr      bool
		wantObject   string
		wantObjectID int
		wantWebURL   string
	}{
		{
			name:         "matched in-scope stock item",
			response:     `{"success":"Match found","plugin":"InvenTreeBarcodePlugin","barcode_data":"abc123","barcode_hash":"deadbeef","stockitem":{"api_url":"/api/stock/12/","pk":12,"web_url":"https://inventory.example.test/stock/item/12/","instance":{"pk":12,"part_detail":{"pk":5}}}}`,
			wantMatched:  true,
			wantObject:   "stockitem",
			wantObjectID: 12,
			wantWebURL:   "https://inventory.example.test/stock/item/12/",
		},
		{
			name:        "matched out-of-scope build",
			response:    `{"success":"Match found","plugin":"InvenTreeBarcodePlugin","barcode_data":"abc123","barcode_hash":"deadbeef","build":{"api_url":"/api/build/1/","pk":1,"web_url":"https://inventory.example.test/build/1/"}}`,
			wantMatched: true,
		},
		{
			name:        "no match",
			status:      http.StatusBadRequest,
			fieldErrors: map[string][]string{"error": {barcodeNoMatchMessage}},
			wantMatched: false,
		},
		{
			// Live evidence only ever showed one recognized object-type key
			// per response, but ResolveBarcode must not silently pick one at
			// random (via unordered map iteration) if this were ever
			// violated -- it must report the anomaly instead.
			name:        "ambiguous match with two recognized object-type keys",
			response:    `{"success":"Match found","stockitem":{"api_url":"/api/stock/12/","pk":12,"web_url":"https://inventory.example.test/stock/item/12/"},"part":{"api_url":"/api/part/5/","pk":5,"web_url":"https://inventory.example.test/part/5/"}}`,
			wantMatched: false,
			wantErr:     true,
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
					a.Equal(http.MethodPost, req.Method)
					a.Equal("/api/barcode/", req.URL.Path)
					if tt.status != 0 {
						payload := map[string]any{}
						for key, messages := range tt.fieldErrors {
							anyMessages := make([]any, len(messages))
							for i, m := range messages {
								anyMessages[i] = m
							}
							payload[key] = anyMessages
						}
						encoded, marshalErr := json.Marshal(payload)
						r.NoError(marshalErr)
						return jsonResponse(req, tt.status, string(encoded)), nil
					}
					return jsonResponse(req, http.StatusOK, tt.response), nil
				})},
			})
			r.NoError(err)

			match, matched, err := client.ResolveBarcode(ctx, "abc123")
			if tt.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			a.Equal(tt.wantMatched, matched)
			a.Equal(tt.wantObject, match.ObjectType)
			a.Equal(tt.wantObjectID, match.ObjectID)
			a.Equal(tt.wantWebURL, match.WebURL)
		})
	}
}

func TestLinkBarcodePropagatesRawDuplicateConflictError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	embedded := `{"api_url":"/api/stock/12/","instance":{"pk":12,"part_detail":{"pk":5}},"pk":12,"web_url":"https://inventory.example.test/stock/item/12/"}`
	body := map[string]any{
		"error":     []string{"Barcode matches existing item"},
		"stockitem": []string{embedded},
	}
	encoded, err := json.Marshal(body)
	r.NoError(err)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, http.StatusBadRequest, string(encoded)), nil
		})},
	})
	r.NoError(err)

	linkErr := client.LinkBarcode(ctx, "abc123", "stockitem", 99)
	r.Error(linkErr)
	var apiErr *APIError
	r.ErrorAs(linkErr, &apiErr)
	a.Equal(http.StatusBadRequest, apiErr.StatusCode)
	a.Equal([]string{"Barcode matches existing item"}, apiErr.FieldErrors["error"])
	r.Len(apiErr.FieldErrors["stockitem"], 1)
	a.Equal(embedded, apiErr.FieldErrors["stockitem"][0], "the client layer must not redact or reshape the raw upstream duplicate-conflict field error -- that is the tools layer's job")
}

func TestGetDetailMethodsDeriveHasBarcodeAndDiscardRawHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		response     string
		wantHasFalse bool
		call         func(context.Context, *Client) (any, error)
	}{
		{
			name:     "part with barcode",
			path:     "/api/part/10/",
			response: `{"pk":10,"name":"resistor","barcode_hash":"deadbeef"}`,
			call: func(ctx context.Context, client *Client) (any, error) {
				return client.GetPartDetail(ctx, 10)
			},
		},
		{
			name:         "part without barcode",
			path:         "/api/part/10/",
			response:     `{"pk":10,"name":"resistor","barcode_hash":""}`,
			wantHasFalse: true,
			call: func(ctx context.Context, client *Client) (any, error) {
				return client.GetPartDetail(ctx, 10)
			},
		},
		{
			name:     "stock item with barcode",
			path:     "/api/stock/50/",
			response: `{"pk":50,"part":5,"quantity":2,"barcode_hash":"deadbeef"}`,
			call: func(ctx context.Context, client *Client) (any, error) {
				return client.GetStockItemDetail(ctx, 50)
			},
		},
		{
			name:     "stock location with barcode",
			path:     "/api/stock/location/20/",
			response: `{"pk":20,"name":"Bin 1","barcode_hash":"deadbeef"}`,
			call: func(ctx context.Context, client *Client) (any, error) {
				return client.GetStockLocation(ctx, 20)
			},
		},
		{
			name:     "purchase order with barcode",
			path:     "/api/order/po/30/",
			response: `{"pk":30,"reference":"PO-0001","barcode_hash":"deadbeef"}`,
			call: func(ctx context.Context, client *Client) (any, error) {
				return client.GetPurchaseOrderDetail(ctx, 30)
			},
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
					a.Equal(tt.path, req.URL.Path)
					return jsonResponse(req, http.StatusOK, tt.response), nil
				})},
			})
			r.NoError(err)

			record, callErr := tt.call(ctx, client)
			r.NoError(callErr)

			encoded, marshalErr := json.Marshal(record)
			r.NoError(marshalErr)
			var keys map[string]any
			r.NoError(json.Unmarshal(encoded, &keys))
			a.NotContains(keys, "barcode_hash", "the raw barcode_hash must never be marshaled back out")
			hasBarcode, ok := keys["has_barcode"].(bool)
			r.True(ok, "has_barcode must be present and boolean")
			if tt.wantHasFalse {
				a.False(hasBarcode)
			} else {
				a.True(hasBarcode)
			}
		})
	}
}
