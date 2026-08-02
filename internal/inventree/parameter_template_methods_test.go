package inventree

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParameterTemplateWriteMethodsUseSchemaEndpoints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		method     string
		path       string
		status     int
		response   string
		call       func(context.Context, *Client) error
		assertBody func(*assert.Assertions, map[string]any)
	}{
		{name: "create", method: http.MethodPost, path: "/api/parameter/template/", status: http.StatusCreated, response: `{"pk":70,"name":"Resistance"}`, call: func(ctx context.Context, client *Client) error {
			_, err := client.CreateParameterTemplate(ctx, ParameterTemplateCreate{Name: "Resistance", Units: "ohm", Description: "resistance", ModelType: "part.part", Checkbox: false, Choices: "10k,22k", Enabled: true})
			return err
		}, assertBody: func(a *assert.Assertions, body map[string]any) {
			a.Equal("Resistance", body["name"])
			a.Equal(false, body["checkbox"])
			a.Equal(true, body["enabled"])
		}},
		{name: "update", method: http.MethodPatch, path: "/api/parameter/template/70/", status: http.StatusOK, response: `{"pk":70,"name":"Resistance","units":""}`, call: func(ctx context.Context, client *Client) error {
			_, err := client.UpdateParameterTemplate(ctx, 70, PatchFields{"units": Set(""), "enabled": Set(false), "selectionlist": Null()})
			return err
		}, assertBody: func(a *assert.Assertions, body map[string]any) {
			a.Equal("", body["units"])
			a.Equal(false, body["enabled"])
			a.Contains(body, "selectionlist")
			a.Nil(body["selectionlist"])
		}},
		{name: "delete", method: http.MethodDelete, path: "/api/parameter/template/70/", status: http.StatusNoContent, call: func(ctx context.Context, client *Client) error { return client.DeleteParameterTemplate(ctx, 70) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			client, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				a.Equal(tt.method, req.Method)
				a.Equal(tt.path, req.URL.Path)
				if tt.assertBody != nil {
					var body map[string]any
					r.NoError(json.NewDecoder(req.Body).Decode(&body))
					tt.assertBody(a, body)
				}
				return jsonResponse(req, tt.status, tt.response), nil
			})}})
			r.NoError(err)
			r.NoError(tt.call(ctx, client))
		})
	}
}

func TestSearchTemplateParametersPageDoesNotNarrowModelType(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		a.Equal("70", req.URL.Query().Get("template"))
		a.Empty(req.URL.Query().Get("model_type"))
		return jsonResponse(req, http.StatusOK, `{"count":1,"next":null,"previous":null,"results":[{"pk":80,"template":70,"model_type":"part.part","model_id":10,"data":"10k"}]}`), nil
	})}})
	r.NoError(err)
	page, err := client.SearchTemplateParametersPage(ctx, TemplateParameterQuery{TemplateID: 70, Limit: 100})
	r.NoError(err)
	r.Len(page.Results, 1)
	a.Equal(80, page.Results[0].PK)
}

func TestSearchCategoryParameterTemplatesPageUsesBoundedPagination(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		a.Equal("100", req.URL.Query().Get("limit"))
		a.Equal("200", req.URL.Query().Get("offset"))
		return jsonResponse(req, http.StatusOK, `{"count":301,"next":"https://inventory.example.test/api/part/category/parameters/?limit=100&offset=300","previous":null,"results":[{"pk":80,"category":20,"template":70}]}`), nil
	})}})
	r.NoError(err)
	page, err := client.SearchCategoryParameterTemplatesPage(ctx, CategoryParameterTemplateQuery{Limit: 100, Offset: 200})
	r.NoError(err)
	r.Len(page.Results, 1)
	a.Equal(80, page.Results[0].PK)
	r.NotNil(page.Next)
}
