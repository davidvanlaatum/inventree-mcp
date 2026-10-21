package inventree

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoteMethodsUseScopedEndpointsAndExplicitValues(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	note := Note{PK: 2, ModelType: "part.part", ModelID: 1, Title: "Test", Content: "text", Primary: true}
	var calls []string
	c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.Method+" "+req.URL.Path)
		switch req.URL.Path {
		case "/api/part/1/":
			return jsonResponse(req, 200, `{"pk":1}`), nil
		case "/api/note/":
			if req.Method == http.MethodGet {
				a.Equal("part.part", req.URL.Query().Get("model_type"))
				a.Equal("false", req.URL.Query().Get("template"))
				a.Equal("false", req.URL.Query().Get("updated_by_detail"))
				a.Equal("1", req.URL.Query().Get("model_id"))
				a.Equal("needle", req.URL.Query().Get("search"))
				a.Equal("10", req.URL.Query().Get("offset"))
				b, _ := json.Marshal(Page[Note]{Count: 1, Results: []Note{note}})
				return jsonResponse(req, 200, string(b)), nil
			}
			var body map[string]any
			r.NoError(json.NewDecoder(req.Body).Decode(&body))
			a.Equal(false, body["primary"])
			a.Equal("", body["content"])
			a.NotContains(body, "template")
			b, _ := json.Marshal(note)
			return jsonResponse(req, 201, string(b)), nil
		case "/api/note/2/":
			if req.Method == http.MethodDelete {
				return jsonResponse(req, 204, ""), nil
			}
			if req.Method == http.MethodPatch {
				var body map[string]any
				r.NoError(json.NewDecoder(req.Body).Decode(&body))
				a.Equal(map[string]any{"content": "", "primary": false}, body)
			}
			b, _ := json.Marshal(note)
			return jsonResponse(req, 200, string(b)), nil
		default:
			t.Errorf("unexpected path %s", req.URL.Path)
			return jsonResponse(req, 404, `{}`), nil
		}
	})}})
	r.NoError(err)
	page, err := c.SearchNotesPage(ctx, NoteQuery{ModelType: "part.part", ModelID: 1, Search: "needle", Limit: 20, Offset: 10})
	r.NoError(err)
	a.Len(page.Results, 1)
	_, err = c.GetNote(ctx, 2)
	r.NoError(err)
	_, err = c.CreateNote(ctx, NoteCreate{ModelType: "part.part", ModelID: 1, Title: "Test", Content: "", Primary: dvgoutils.Ptr(false)})
	r.NoError(err)
	_, err = c.UpdateNote(ctx, 2, PatchFields{"content": Set(""), "primary": Set(false)})
	r.NoError(err)
	r.NoError(c.DeleteNote(ctx, 2))
	a.Contains(calls, "DELETE /api/note/2/")
}

func TestNotesRejectOutOfScopeAndUnsafeInputs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		note       Note
		parentCode int
	}{
		{"template", Note{PK: 2, Template: true}, 200},
		{"sales object", Note{PK: 2, ModelType: "order.salesorder", ModelID: 1}, 200},
		{"mismatched identity", Note{PK: 3, ModelType: "part.part", ModelID: 1}, 200},
		{"unreadable parent", Note{PK: 2, ModelType: "part.part", ModelID: 1}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			writes := 0
			c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" {
					writes++
				}
				if strings.HasPrefix(req.URL.Path, "/api/part/") {
					return jsonResponse(req, tc.parentCode, `{}`), nil
				}
				b, _ := json.Marshal(tc.note)
				return jsonResponse(req, 200, string(b)), nil
			})}})
			r.NoError(err)
			_, err = c.GetNote(ctx, 2)
			r.Error(err)
			_, err = c.UpdateNote(ctx, 2, PatchFields{"content": Set("changed")})
			r.Error(err)
			r.Error(c.DeleteNote(ctx, 2))
			r.Zero(writes)
		})
	}
	t.Run("invalid fields and bounds", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.HasPrefix(req.URL.Path, "/api/part/") {
				return jsonResponse(req, 200, `{"pk":1}`), nil
			}
			return jsonResponse(req, 200, `{"pk":2,"model_type":"part.part","model_id":1,"title":"Test"}`), nil
		})}})
		r.NoError(err)
		for _, fields := range []PatchFields{{}, {"model_type": Set("company.company")}, {"model_id": Set(3)}, {"template": Set(true)}, {"primary": Null()}, {"title": Set("")}, {"content": Set(strings.Repeat("a", 50001))}, {"title": Set(strings.Repeat("é", 101))}, {"description": Null()}} {
			_, err = c.UpdateNote(ctx, 2, fields)
			r.Error(err)
		}
		for _, q := range []NoteQuery{{}, {ModelType: "part.part", Limit: 101}, {ModelType: "part.part", Limit: 1, Offset: 5001}, {ModelType: "part.part", Limit: 1, ModelID: -1}} {
			_, err = c.SearchNotesPage(ctx, q)
			r.Error(err)
		}
		_, err = c.CreateNote(ctx, NoteCreate{ModelType: "part.part", ModelID: 1, Title: ""})
		r.Error(err)
	})
}

func TestNotesReadEverySupportedParentAndRejectBadListResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ modelType, parentPath string }{
		{"part.part", "/api/part/1/"}, {"company.company", "/api/company/1/"},
		{"company.supplierpart", "/api/company/part/1/"}, {"company.manufacturerpart", "/api/company/part/manufacturer/1/"},
		{"stock.stockitem", "/api/stock/1/"}, {"order.purchaseorder", "/api/order/po/1/"},
	} {
		t.Run(tc.modelType, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			parentRead := false
			c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == tc.parentPath {
					parentRead = true
					return jsonResponse(req, 200, `{"pk":1}`), nil
				}
				b, _ := json.Marshal(Note{PK: 2, ModelType: tc.modelType, ModelID: 1, Title: "T"})
				return jsonResponse(req, 200, string(b)), nil
			})}})
			r.NoError(err)
			_, err = c.GetNote(ctx, 2)
			r.NoError(err)
			r.True(parentRead)
		})
	}
	for _, payload := range []string{
		`{"count":1,"results":[{"pk":2,"model_type":"order.salesorder","model_id":1}]}`,
		`{"count":1,"results":[{"pk":2,"model_type":"part.part","model_id":2}]}`,
		`{"count":1,"results":[{"pk":2,"template":true,"model_type":"part.part","model_id":1}]}`,
		`{"count":2,"results":[{"pk":2,"model_type":"part.part","model_id":1},{"pk":3,"model_type":"part.part","model_id":1}]}`,
	} {
		t.Run(payload, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/part/1/" {
					return jsonResponse(req, 200, `{"pk":1}`), nil
				}
				return jsonResponse(req, 200, payload), nil
			})}})
			r.NoError(err)
			_, err = c.SearchNotesPage(ctx, NoteQuery{ModelType: "part.part", ModelID: 1, Limit: 1})
			r.Error(err)
		})
	}
}

func TestNoteWritesRejectMalformedSuccessfulResponses(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"create", "update"} {
		for _, tc := range []struct {
			name string
			note Note
		}{
			{"missing identity", Note{ModelType: "part.part", ModelID: 1}},
			{"template", Note{PK: 2, Template: true, ModelType: "part.part", ModelID: 1}},
			{"different model", Note{PK: 2, ModelType: "company.company", ModelID: 1}},
			{"different parent", Note{PK: 2, ModelType: "part.part", ModelID: 3}},
		} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				r := require.New(t)
				ctx, _, _ := testhandler.SetupTestHandler(t)
				writes := 0
				c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Path == "/api/part/1/" {
						return jsonResponse(req, 200, `{"pk":1}`), nil
					}
					if req.Method == http.MethodGet {
						r.Equal("/api/note/2/", req.URL.Path)
						return jsonResponse(req, 200, `{"pk":2,"model_type":"part.part","model_id":1,"title":"Before"}`), nil
					}
					writes++
					if action == "create" {
						r.Equal(http.MethodPost, req.Method)
						r.Equal("/api/note/", req.URL.Path)
					} else {
						r.Equal(http.MethodPatch, req.Method)
						r.Equal("/api/note/2/", req.URL.Path)
					}
					body, err := json.Marshal(tc.note)
					r.NoError(err)
					return jsonResponse(req, 200, string(body)), nil
				})}})
				r.NoError(err)
				var result Note
				if action == "create" {
					result, err = c.CreateNote(ctx, NoteCreate{ModelType: "part.part", ModelID: 1, Title: "Requested"})
				} else {
					result, err = c.UpdateNote(ctx, 2, PatchFields{"title": Set("Requested")})
				}
				r.ErrorIs(err, ErrNoteOutOfScope)
				r.Equal(Note{}, result, "a malformed successful response must not become an approved note record")
				r.Equal(1, writes, "an acknowledged write with invalid identity must never be retried blindly")
			})
		}
	}
}

func TestNoteSearchRejectsUnreadableParents(t *testing.T) {
	t.Parallel()
	for _, scoped := range []bool{true, false} {
		name := "result parent"
		if scoped {
			name = "requested parent"
		}
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			listCalls := 0
			c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/part/1/" {
					return jsonResponse(req, http.StatusForbidden, `{"detail":"not permitted"}`), nil
				}
				r.Equal("/api/note/", req.URL.Path)
				listCalls++
				return jsonResponse(req, 200, `{"count":1,"results":[{"pk":2,"model_type":"part.part","model_id":1,"title":"Private"}]}`), nil
			})}})
			r.NoError(err)
			query := NoteQuery{ModelType: "part.part", Limit: 20}
			if scoped {
				query.ModelID = 1
			}
			page, err := c.SearchNotesPage(ctx, query)
			var apiErr *APIError
			r.ErrorAs(err, &apiErr)
			r.Equal(http.StatusForbidden, apiErr.StatusCode)
			r.Equal(Page[Note]{}, page, "no partial note content may escape after a parent permission failure")
			if scoped {
				r.Zero(listCalls, "reject an unreadable requested parent before listing its notes")
			} else {
				r.Equal(1, listCalls)
			}
		})
	}
}
