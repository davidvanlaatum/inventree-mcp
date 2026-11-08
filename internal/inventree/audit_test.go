package inventree

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditProjectionUsesPerModelFieldRules(t *testing.T) {
	cases := []struct{ model, field string }{
		{"part.part", "description"}, {"company.company", "email"}, {"company.supplierpart", "SKU"},
		{"company.manufacturerpart", "MPN"}, {"stock.stockitem", "quantity"}, {"order.purchaseorder", "description"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			wire := stockAuditWire{PK: 8, ModelType: tc.model, ObjectPK: "7", Timestamp: "2026-11-08T01:02:03Z", Action: 1}
			wire.Changes = json.RawMessage(`{"` + tc.field + `":["old","new"],"barcode_hash":["secret","other"],"metadata":{"token":"secret"},"future_secret":["hidden","hidden"],"notes":["free text","free text"]}`)
			wire.Deltas = json.RawMessage(`{"actor_username":"private","remote_addr":"192.0.2.1","cid":"private-cid","notes":"free text","part":{"model_type":"part.part","pk":7,"str":"private display"},"who":{"pk":4,"email":"person@example.test"},"quantity":5}`)
			entry, err := wire.auditEntry()
			r.NoError(err)
			a.Equal(map[string]any{tc.field: []any{"old", "new"}}, entry.Changes)
			a.Equal(map[string]any{"part": float64(7), "quantity": float64(5)}, entry.Deltas)
			encoded, err := json.Marshal(entry)
			r.NoError(err)
			for _, secret := range []string{"private", "secret", "person@example", "free text", "barcode_hash", "metadata"} {
				a.NotContains(string(encoded), secret)
			}
		})
	}
}

func TestAuditChangesRejectsMalformedAndOversizedApprovedData(t *testing.T) {
	for _, raw := range []string{`[]`, `{"description":"` + strings.Repeat("x", stockAuditDeltasMaxBytes) + `"}`} {
		t.Run(strconv.Itoa(len(raw)), func(t *testing.T) {
			r := require.New(t)
			_, err := sanitizeAuditChanges("part.part", json.RawMessage(raw))
			r.Error(err)
		})
	}
}

func TestAuditClientRequiresScopeAndProjectsOnlySupportedRows(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests++
		a.Equal("false", req.URL.Query().Get("actor_detail"))
		a.Equal("false", req.URL.Query().Get("changes_detail"))
		switch req.URL.Path {
		case "/api/audit/":
			a.Equal("part.part", req.URL.Query().Get("model_type"))
			a.Equal("7", req.URL.Query().Get("model_id"))
			a.Equal("true", req.URL.Query().Get("include_children"))
			_, _ = w.Write([]byte(`{"count":3,"next":"/api/audit/?offset=100","results":[{"pk":1,"timestamp":"today","action":0,"model_type":"part.part","object_pk":"7","actor_detail":{"email":"private@example.test"},"actor_username":"private","object_repr":"private","changes":{"description":[null,"part"]}},{"pk":2,"timestamp":"today","action":1,"model_type":"stock.stockitem","object_pk":"9","parent_model_type":"part.part","parent_object_pk":"7","changes":{"quantity":["1","2"]}},{"pk":3,"timestamp":"today","action":1,"model_type":"common.attachment","object_pk":"20","parent_model_type":"part.part","parent_object_pk":"7","additional_data":{"secret":"private"}}]}`))
		case "/api/audit/2/":
			_, _ = w.Write([]byte(`{"pk":2,"timestamp":"today","action":1,"model_type":"stock.stockitem","object_pk":"9","parent_model_type":"part.part","parent_object_pk":"7","notes":"audit reason","remote_addr":"private","changes":{}}`))
		case "/api/audit/3/":
			_, _ = w.Write([]byte(`{"pk":3,"timestamp":"today","action":1,"model_type":"auth.user","object_pk":"1","changes":{"password":"private"}}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Credential: Credential{Scheme: AuthSchemeToken, Token: "test-token"}, HTTPClient: server.Client()})
	r.NoError(err)
	_, err = client.SearchAuditEntriesPage(ctx, AuditQuery{})
	r.Error(err)
	a.Zero(requests)
	_, err = client.GetAuditEntry(ctx, AuditEntryQuery{ID: 2})
	r.Error(err)
	a.Zero(requests)
	scope := AuditScope{ModelType: "part.part", ModelID: 7, IncludeChildren: true}
	page, err := client.SearchAuditEntriesPage(ctx, AuditQuery{AuditScope: scope, Limit: 100})
	r.NoError(err)
	r.Len(page.Results, 2)
	a.Equal(3, page.Count)
	r.NotNil(page.Next)
	payload, err := json.Marshal(page.Results)
	r.NoError(err)
	a.NotContains(string(payload), "private")
	entry, err := client.GetAuditEntry(ctx, AuditEntryQuery{AuditScope: scope, ID: 2})
	r.NoError(err)
	a.Equal("audit reason", entry.Notes)
	_, err = client.GetAuditEntry(ctx, AuditEntryQuery{AuditScope: AuditScope{ModelType: "part.part", ModelID: 7}, ID: 2})
	var apiErr *APIError
	r.ErrorAs(err, &apiErr)
	a.Equal(http.StatusNotFound, apiErr.StatusCode)
	_, err = client.GetAuditEntry(ctx, AuditEntryQuery{AuditScope: scope, ID: 3})
	r.ErrorAs(err, &apiErr)
	a.Equal(http.StatusNotFound, apiErr.StatusCode)
}

func TestAuditClientRejectsScopeDriftAndMalformedRecords(t *testing.T) {
	cases := []struct{ name, payload string }{
		{"wrong object", `{"pk":4,"timestamp":"today","action":1,"model_type":"part.part","object_pk":"8"}`},
		{"bad object", `{"pk":4,"timestamp":"today","action":1,"model_type":"part.part","object_pk":"not-an-id"}`},
		{"bad parent", `{"pk":4,"timestamp":"today","action":1,"model_type":"part.part","object_pk":"7","parent_model_type":"part.part","parent_object_pk":"invalid"}`},
		{"bad changes", `{"pk":4,"timestamp":"today","action":1,"model_type":"part.part","object_pk":"7","changes":[]}`},
		{"bad action", `{"pk":4,"timestamp":"today","action":99,"model_type":"part.part","object_pk":"7"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"count":1,"results":[` + tc.payload + `]}`))
			}))
			defer server.Close()
			client, err := NewClient(Config{BaseURL: server.URL, Credential: Credential{Scheme: AuthSchemeToken, Token: "test-token"}, HTTPClient: server.Client()})
			r.NoError(err)
			page, err := client.SearchAuditEntriesPage(ctx, AuditQuery{AuditScope: AuditScope{ModelType: "part.part", ModelID: 7}, Limit: 100})
			r.Error(err)
			r.Empty(page.Results)
		})
	}
}

func TestAuditDetailRejectsMalformedIdentityAndUpstreamFailures(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
	}{
		{"mismatched ID", `{"pk":99,"timestamp":"today","action":1,"model_type":"part.part","object_pk":"7"}`, http.StatusOK},
		{"bad object", `{"pk":4,"timestamp":"today","action":1,"model_type":"part.part","object_pk":"bad"}`, http.StatusOK},
		{"upstream failure", `{"detail":"unavailable"}`, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := NewClient(Config{BaseURL: server.URL, Credential: Credential{Scheme: AuthSchemeToken, Token: "test-token"}, HTTPClient: server.Client()})
			r.NoError(err)
			out, err := client.GetAuditEntry(ctx, AuditEntryQuery{AuditScope: AuditScope{ModelType: "part.part", ModelID: 7}, ID: 4})
			r.Error(err)
			r.Zero(out.PK)
		})
	}
}

func TestStockAuditConvenienceReadersShareGenericScopeAndProjection(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/audit/" {
			if req.URL.Query().Get("model_type") == "part.part" {
				_, _ = w.Write([]byte(`{"count":2,"results":[{"pk":3,"timestamp":"today","action":0,"model_type":"part.part","object_pk":"7"},{"pk":4,"timestamp":"today","action":1,"model_type":"stock.stockitem","object_pk":"9","parent_model_type":"part.part","parent_object_pk":"7","changes":{"quantity":[1,2]}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"count":1,"results":[{"pk":4,"timestamp":"today","action":1,"model_type":"stock.stockitem","object_pk":"9","parent_model_type":"part.part","parent_object_pk":"7","changes":{"quantity":[1,2]}}]}`))
			return
		}
		switch req.URL.Path {
		case "/api/audit/4/":
			_, _ = w.Write([]byte(`{"pk":4,"timestamp":"today","action":1,"model_type":"stock.stockitem","object_pk":"9","parent_model_type":"part.part","parent_object_pk":"7","changes":{"quantity":[1,2]}}`))
		case "/api/audit/5/":
			_, _ = w.Write([]byte(`{"pk":5,"timestamp":"today","action":1,"model_type":"part.part","object_pk":"7"}`))
		case "/api/audit/6/":
			_, _ = w.Write([]byte(`{"pk":6,"timestamp":"today","action":1,"model_type":"stock.stockitem","object_pk":"bad"}`))
		case "/api/audit/7/":
			_, _ = w.Write([]byte(`{"pk":8,"timestamp":"today","action":1,"model_type":"stock.stockitem","object_pk":"9"}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Credential: Credential{Scheme: AuthSchemeToken, Token: "test-token"}, HTTPClient: server.Client()})
	r.NoError(err)
	byPart, err := client.SearchStockAuditPage(ctx, StockAuditQuery{PartID: 7, Limit: 100})
	r.NoError(err)
	r.Len(byPart.Results, 1)
	a.Equal(9, *byPart.Results[0].Item)
	byItem, err := client.SearchStockAuditPage(ctx, StockAuditQuery{ItemID: 9, PartID: 7, Limit: 100})
	r.NoError(err)
	a.Equal(byPart.Results, byItem.Results)
	filtered, err := client.SearchStockAuditPage(ctx, StockAuditQuery{ItemID: 9, PartID: 8, Limit: 100})
	r.NoError(err)
	a.Empty(filtered.Results)
	for _, q := range []StockAuditQuery{{}, {ItemID: -1}, {PartID: -1}, {ItemID: 9, Limit: 1001}} {
		_, err = client.SearchStockAuditPage(ctx, q)
		r.Error(err)
	}
	exact, err := client.GetStockAuditEntry(ctx, 4)
	r.NoError(err)
	a.Equal(byItem.Results[0], exact)
	for _, id := range []int{0, 5, 6, 7, 999} {
		_, err = client.GetStockAuditEntry(ctx, id)
		r.Error(err)
	}
}
