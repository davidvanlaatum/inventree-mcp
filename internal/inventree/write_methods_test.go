package inventree

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteMethodsUseExpectedEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		call     func(context.Context, *Client) error
		method   string
		path     string
		response string
		assert   func(*assert.Assertions, map[string]any)
	}{
		{
			name: "create part",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreatePart(ctx, PartCreate{
					Name:         "10k resistor",
					Category:     dvgoutils.Ptr(20),
					Purchaseable: dvgoutils.Ptr(false),
				})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/part/",
			response: `{"pk":10,"name":"10k resistor","category":20,"purchaseable":false}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal("10k resistor", body["name"])
				a.Equal(float64(20), body["category"])
				a.Equal(false, body["purchaseable"])
				_, hasCustomerRole := body["is_customer"]
				a.False(hasCustomerRole)
			},
		},
		{
			name: "update part preserves explicit false and empty",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdatePart(ctx, 10, PatchFields{
					"active":      Set(false),
					"description": Set(""),
					"category":    Set(20),
				})
				return err
			},
			method:   http.MethodPatch,
			path:     "/api/part/10/",
			response: `{"pk":10,"name":"10k resistor","active":false}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal(false, body["active"])
				a.Equal("", body["description"])
				a.Equal(float64(20), body["category"])
			},
		},
		{
			name: "create part category preserves explicit fields",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreatePartCategory(ctx, CategoryCreate{Name: "Passives", Parent: dvgoutils.Ptr(20), DefaultLocation: dvgoutils.Ptr(40), DefaultKeywords: dvgoutils.Ptr(""), Structural: dvgoutils.Ptr(false), Icon: dvgoutils.Ptr("")})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/part/category/",
			response: `{"pk":21,"name":"Passives","parent":20,"default_location":40,"structural":false}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal(float64(20), body["parent"])
				a.Equal(float64(40), body["default_location"])
				a.Equal("", body["default_keywords"])
				a.Equal(false, body["structural"])
				a.Equal("", body["icon"])
			},
		},
		{
			name: "update part category preserves explicit null false and empty",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdatePartCategory(ctx, 21, PatchFields{"parent": Null(), "default_location": Null(), "default_keywords": Set(""), "structural": Set(false), "icon": Null()})
				return err
			},
			method:   http.MethodPatch,
			path:     "/api/part/category/21/",
			response: `{"pk":21,"name":"Passives","parent":null,"default_location":null,"structural":false}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Nil(body["parent"])
				a.Nil(body["default_location"])
				a.Equal("", body["default_keywords"])
				a.Equal(false, body["structural"])
				a.Nil(body["icon"])
			},
		},
		{
			name: "create company omits customer role",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreateCompany(ctx, CompanyCreate{
					Name:           "Acme",
					Currency:       "USD",
					IsSupplier:     true,
					IsManufacturer: true,
				})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/company/",
			response: `{"pk":30,"name":"Acme","currency":"USD","is_supplier":true,"is_manufacturer":true}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal(true, body["is_supplier"])
				a.Equal(true, body["is_manufacturer"])
				_, hasCustomerRole := body["is_customer"]
				a.False(hasCustomerRole)
			},
		},
		{
			name: "update company preserves null notes and false",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdateCompany(ctx, 30, PatchFields{"notes": Null(), "active": Set(false)})
				return err
			},
			method: http.MethodPatch, path: "/api/company/30/", response: `{"pk":30,"notes":null,"active":false}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Nil(body["notes"])
				a.Equal(false, body["active"])
			},
		},
		{
			name: "create supplier part",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreateSupplierPart(ctx, SupplierPartCreate{Part: 10, Supplier: 30, SKU: "SKU-1", Active: dvgoutils.Ptr(false)})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/company/part/",
			response: `{"pk":40,"part":10,"supplier":30,"SKU":"SKU-1","active":false}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal(float64(10), body["part"])
				a.Equal(float64(30), body["supplier"])
				a.Equal("SKU-1", body["SKU"])
				a.Equal(false, body["active"])
			},
		},
		{
			name: "update supplier part preserves null and false",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdateSupplierPart(ctx, 40, PatchFields{"link": Null(), "primary": Set(false), "pack_quantity": Set("0")})
				return err
			},
			method: http.MethodPatch, path: "/api/company/part/40/", response: `{"pk":40,"part":10,"supplier":30,"SKU":"SKU-1"}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Nil(body["link"])
				a.Equal(false, body["primary"])
				a.Equal("0", body["pack_quantity"])
			},
		},
		{
			name: "create manufacturer part",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreateManufacturerPart(ctx, ManufacturerPartCreate{Part: 10, Manufacturer: 31, MPN: dvgoutils.Ptr("MPN-1")})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/company/part/manufacturer/",
			response: `{"pk":50,"part":10,"manufacturer":31,"MPN":"MPN-1"}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal(float64(10), body["part"])
				a.Equal(float64(31), body["manufacturer"])
				a.Equal("MPN-1", body["MPN"])
			},
		},
		{
			name: "create manufacturer part without MPN omits field",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreateManufacturerPart(ctx, ManufacturerPartCreate{Part: 10, Manufacturer: 31})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/company/part/manufacturer/",
			response: `{"pk":51,"part":10,"manufacturer":31,"MPN":null}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal(float64(10), body["part"])
				a.Equal(float64(31), body["manufacturer"])
				a.NotContains(body, "MPN")
			},
		},
		{
			name: "update manufacturer part preserves null",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdateManufacturerPart(ctx, 50, PatchFields{"MPN": Null(), "link": Null()})
				return err
			},
			method: http.MethodPatch, path: "/api/company/part/manufacturer/50/", response: `{"pk":50,"part":10,"manufacturer":31,"MPN":null}`,
			assert: func(a *assert.Assertions, body map[string]any) { a.Nil(body["MPN"]); a.Nil(body["link"]) },
		},
		{
			name: "create part parameter preserves explicit empty",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreatePartParameter(ctx, NewPartParameter(10, 70, ""))
				return err
			},
			method:   http.MethodPost,
			path:     "/api/parameter/",
			response: `{"pk":60,"template":70,"model_type":"part.part","model_id":10,"data":""}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal(float64(70), body["template"])
				a.Equal("part.part", body["model_type"])
				a.Equal(float64(10), body["model_id"])
				a.Equal("", body["data"])
			},
		},
		{
			name: "update part parameter preserves explicit zero",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdatePartParameter(ctx, 60, PatchFields{"data": Set("0")})
				return err
			},
			method:   http.MethodPatch,
			path:     "/api/parameter/60/",
			response: `{"pk":60,"template":70,"model_type":"part.part","model_id":10,"data":"0"}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal("0", body["data"])
			},
		},
		{
			name: "create stock item decodes array response",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreateStockItem(ctx, StockItemCreate{
					Part:      10,
					Location:  40,
					Quantity:  7,
					Status:    dvgoutils.Ptr(10),
					Batch:     dvgoutils.Ptr("B-1"),
					Serial:    dvgoutils.Ptr("S-1"),
					Packaging: dvgoutils.Ptr("reel"),
					Notes:     dvgoutils.Ptr("initial stock"),
				})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/stock/",
			response: `[{"pk":50,"part":10,"location":40,"quantity":7,"status":10,"batch":"B-1","serial":"S-1","notes":"initial stock"}]`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal(float64(10), body["part"])
				a.Equal(float64(40), body["location"])
				a.Equal(float64(7), body["quantity"])
				a.Equal(float64(10), body["status"])
				a.Equal("B-1", body["batch"])
				a.Equal("S-1", body["serial"])
				a.Equal("reel", body["packaging"])
				a.Equal("initial stock", body["notes"])
				_, hasCustomer := body["customer"]
				a.False(hasCustomer)
				_, hasSalesOrder := body["sales_order"]
				a.False(hasSalesOrder)
			},
		},
		{
			name: "create stock location preserves explicit references and false",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreateStockLocation(ctx, StockLocationCreate{Name: "Bin", Description: dvgoutils.Ptr(""), Parent: dvgoutils.Ptr(40), Owner: dvgoutils.Ptr(20), CustomIcon: dvgoutils.Ptr(""), Structural: dvgoutils.Ptr(false), External: dvgoutils.Ptr(false), LocationType: dvgoutils.Ptr(3)})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/stock/location/",
			response: `{"pk":41,"name":"Bin","parent":40,"owner":20,"structural":false,"external":false,"location_type":3}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal("", body["description"])
				a.Equal(float64(40), body["parent"])
				a.Equal(float64(20), body["owner"])
				a.Equal("", body["custom_icon"])
				a.Equal(false, body["structural"])
				a.Equal(false, body["external"])
				a.Equal(float64(3), body["location_type"])
			},
		},
		{
			name: "update stock location preserves null and false",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdateStockLocation(ctx, 41, PatchFields{"parent": Null(), "owner": Null(), "structural": Set(false), "external": Set(false)})
				return err
			},
			method:   http.MethodPatch,
			path:     "/api/stock/location/41/",
			response: `{"pk":41,"name":"Bin","parent":null,"owner":null,"structural":false,"external":false}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Nil(body["parent"])
				a.Nil(body["owner"])
				a.Equal(false, body["structural"])
				a.Equal(false, body["external"])
			},
		},
		{
			name: "update stock item uses constrained patch endpoint",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdateStockItem(ctx, 50, PatchFields{"batch": Set("B-2"), "expiry_date": Null(), "packaging": Set("tray"), "notes": Set("checked"), "link": Set("https://example.test/item")})
				return err
			},
			method:   http.MethodPatch,
			path:     "/api/stock/50/",
			response: `{"pk":50,"part":10,"quantity":7,"batch":"B-2","packaging":"tray","notes":"checked","link":"https://example.test/item"}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal("B-2", body["batch"])
				a.Nil(body["expiry_date"])
				a.Equal("tray", body["packaging"])
				a.Equal("checked", body["notes"])
				a.Equal("https://example.test/item", body["link"])
			},
		},
		{
			name: "add stock uses native adjustment endpoint",
			call: func(ctx context.Context, client *Client) error {
				return client.AddStock(ctx, StockAdjustment{Items: []StockAdjustmentItem{{PK: 50, Quantity: "2.5"}}, Notes: "cycle count correction"})
			},
			method:   http.MethodPost,
			path:     "/api/stock/add/",
			response: `{}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				items := body["items"].([]any)
				item := items[0].(map[string]any)
				a.Equal(float64(50), item["pk"])
				a.Equal("2.5", item["quantity"])
				a.Equal("cycle count correction", body["notes"])
			},
		},
		{
			name: "remove stock uses native adjustment endpoint",
			call: func(ctx context.Context, client *Client) error {
				return client.RemoveStock(ctx, StockAdjustment{Items: []StockAdjustmentItem{{PK: 50, Quantity: "1"}}, Notes: "damaged unit"})
			},
			method:   http.MethodPost,
			path:     "/api/stock/remove/",
			response: `{}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				items := body["items"].([]any)
				item := items[0].(map[string]any)
				a.Equal(float64(50), item["pk"])
				a.Equal("1", item["quantity"])
				a.Equal("damaged unit", body["notes"])
			},
		},
		{
			name: "count stock uses absolute quantity without hidden metadata",
			call: func(ctx context.Context, client *Client) error {
				return client.CountStock(ctx, StockAdjustment{Items: []StockAdjustmentItem{{PK: 50, Quantity: "7"}}, Notes: "shelf count"})
			},
			method:   http.MethodPost,
			path:     "/api/stock/count/",
			response: `{}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				items := body["items"].([]any)
				item := items[0].(map[string]any)
				a.Equal(float64(50), item["pk"])
				a.Equal("7", item["quantity"])
				a.Len(item, 2)
				a.NotContains(body, "location")
				a.Equal("shelf count", body["notes"])
			},
		},
		{
			name: "change stock status uses status-only endpoint",
			call: func(ctx context.Context, client *Client) error {
				return client.ChangeStockStatus(ctx, StockStatusChange{Items: []int{50}, Status: 60, Note: "destroyed after inspection"})
			},
			method:   http.MethodPost,
			path:     "/api/stock/change_status/",
			response: `{}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal([]any{float64(50)}, body["items"])
				a.Equal(float64(60), body["status"])
				a.Equal("destroyed after inspection", body["note"])
				a.Len(body, 3)
			},
		},
		{
			name: "create purchase order",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreatePurchaseOrder(ctx, PurchaseOrderCreate{Supplier: 30, SupplierReference: dvgoutils.Ptr("EBAY-42"), Description: dvgoutils.Ptr("order page import")})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/order/po/",
			response: `{"pk":120,"reference":"PO-0001","supplier":30,"supplier_reference":"EBAY-42"}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				_, hasReference := body["reference"]
				a.False(hasReference)
				a.Equal(float64(30), body["supplier"])
				a.Equal("EBAY-42", body["supplier_reference"])
				a.Equal("order page import", body["description"])
			},
		},
		{
			name: "update purchase order preserves explicit empty",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdatePurchaseOrder(ctx, 120, PatchFields{"description": Set("")})
				return err
			},
			method:   http.MethodPatch,
			path:     "/api/order/po/120/",
			response: `{"pk":120,"reference":"checkout-42","supplier":30,"description":""}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal("", body["description"])
			},
		},
		{
			name: "create purchase order line",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreatePurchaseOrderLine(ctx, PurchaseOrderLineCreate{Order: 120, SupplierPart: 40, Reference: dvgoutils.Ptr("checkout-42-1"), Quantity: 2, PurchasePrice: dvgoutils.Ptr("1.25"), PurchasePriceCurrency: dvgoutils.Ptr("AUD")})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/order/po-line/",
			response: `{"pk":130,"order":120,"part":40,"reference":"checkout-42-1","quantity":2}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal(float64(120), body["order"])
				a.Equal(float64(40), body["part"])
				a.Equal(false, body["auto_pricing"])
				a.Equal(false, body["merge_items"])
				a.Equal("checkout-42-1", body["reference"])
				a.Equal("1.25", body["purchase_price"])
				a.Equal("AUD", body["purchase_price_currency"])
			},
		},
		{
			name: "update purchase order line uses patch",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdatePurchaseOrderLine(ctx, 130, PatchFields{"quantity": Set(3.0), "notes": Set("")})
				return err
			},
			method:   http.MethodPatch,
			path:     "/api/order/po-line/130/",
			response: `{"pk":130,"order":120,"part":40,"quantity":3,"notes":""}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Equal(float64(3), body["quantity"])
				a.Equal("", body["notes"])
			},
		},
		{
			name: "receive purchase order creates stock through receive endpoint",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.ReceivePurchaseOrder(ctx, 120, PurchaseOrderReceive{Items: []PurchaseOrderReceiveItem{{LineItem: 130, Location: dvgoutils.Ptr(40), Quantity: "1.5", Status: dvgoutils.Ptr(10), BatchCode: dvgoutils.Ptr("B-1")}}})
				return err
			},
			method:   http.MethodPost,
			path:     "/api/order/po/120/receive/",
			response: `[{"pk":50,"part":10,"location":40,"quantity":1.5,"status":10,"batch":"B-1"}]`,
			assert: func(a *assert.Assertions, body map[string]any) {
				items := body["items"].([]any)
				item := items[0].(map[string]any)
				a.Equal(float64(130), item["line_item"])
				a.Equal(float64(40), item["location"])
				a.Equal("1.5", item["quantity"])
				a.Equal(float64(10), item["status"])
				a.Equal("B-1", item["batch_code"])
				_, hasGlobalLocation := body["location"]
				a.False(hasGlobalLocation)
			},
		},
		{
			name: "issue purchase order uses explicit status transition endpoint",
			call: func(ctx context.Context, client *Client) error {
				return client.IssuePurchaseOrder(ctx, 120)
			},
			method:   http.MethodPost,
			path:     "/api/order/po/120/issue/",
			response: `{}`,
			assert: func(a *assert.Assertions, body map[string]any) {
				a.Empty(body)
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
					a.Equal(tt.method, req.Method)
					a.Equal(tt.path, req.URL.Path)
					a.Equal("application/json", req.Header.Get("Content-Type"))
					a.Equal("Token secret", req.Header.Get("Authorization"))

					var body map[string]any
					r.NoError(json.NewDecoder(req.Body).Decode(&body))
					tt.assert(a, body)
					return jsonResponse(req, http.StatusOK, tt.response), nil
				})},
			})
			r.NoError(err)

			r.NoError(tt.call(ctx, client))
		})
	}
}

func TestDeletePartParameterUsesStableDetailEndpoint(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			a.Equal(http.MethodDelete, req.Method)
			a.Equal("/api/parameter/60/", req.URL.Path)
			a.Equal("Token secret", req.Header.Get("Authorization"))
			return jsonResponse(req, http.StatusNoContent, ``), nil
		})},
	})
	r.NoError(err)
	r.NoError(client.DeletePartParameter(ctx, 60))
}

func TestDeletePartParameterReportsRequestAndResponseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ctx       func(t *testing.T) context.Context
		response  func(*http.Request) (*http.Response, error)
		wantError string
	}{
		{
			name: "request construction",
			ctx: func(t *testing.T) context.Context {
				return nil
			},
			response: func(req *http.Request) (*http.Response, error) {
				return jsonResponse(req, http.StatusNoContent, ``), nil
			},
			wantError: "nil Context",
		},
		{
			name: "API response",
			response: func(req *http.Request) (*http.Response, error) {
				return jsonResponse(req, http.StatusConflict, `{"detail":"parameter is in use"}`), nil
			},
			wantError: "parameter is in use",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			if tt.ctx != nil {
				ctx = tt.ctx(t)
			}
			client, err := NewClient(Config{
				BaseURL:    "https://inventory.example.test",
				Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
				HTTPClient: &http.Client{Transport: roundTripFunc(tt.response)},
			})
			r.NoError(err)

			r.ErrorContains(client.DeletePartParameter(ctx, 60), tt.wantError)
		})
	}
}

func TestAttachmentWriteMethodsUseExpectedEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("upload attachment multipart", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		comment := ""

		client, err := NewClient(Config{
			BaseURL:    "https://inventory.example.test",
			Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
			HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				a.Equal(http.MethodPost, req.Method)
				a.Equal("/api/attachment/", req.URL.Path)
				a.Equal("Token secret", req.Header.Get("Authorization"))
				fields, files := readMultipartRequest(t, req)
				a.Equal("part", fields["model_type"])
				a.Equal("10", fields["model_id"])
				a.Equal("", fields["comment"])
				a.Equal("datasheet.pdf", files["attachment"].filename)
				a.Equal("application/pdf", files["attachment"].contentType)
				a.Equal("pdf bytes", string(files["attachment"].content))
				return jsonResponse(req, http.StatusOK, `{"pk":90,"model_type":"part","model_id":10,"filename":"datasheet.pdf"}`), nil
			})},
		})
		r.NoError(err)

		record, err := client.UploadAttachment(ctx, AttachmentCreate{
			ModelType:   "part",
			ModelID:     10,
			Filename:    "datasheet.pdf",
			ContentType: "application/pdf",
			Content:     []byte("pdf bytes"),
			Comment:     &comment,
		})
		r.NoError(err)
		a.Equal(90, record.PK)
	})

	t.Run("link update and delete", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		var calls []string

		client, err := NewClient(Config{
			BaseURL:    "https://inventory.example.test",
			Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
			HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls = append(calls, req.Method+" "+req.URL.Path)
				switch req.Method + " " + req.URL.Path {
				case "POST /api/attachment/":
					fields, _ := readMultipartRequest(t, req)
					a.Equal("https://example.test/datasheet.pdf", fields["link"])
					a.NotContains(fields, "filename")
					return jsonResponse(req, http.StatusOK, `{"pk":91,"model_type":"part","model_id":10,"filename":"datasheet","link":"https://example.test/datasheet.pdf"}`), nil
				case "PATCH /api/attachment/91/":
					var body map[string]any
					r.NoError(json.NewDecoder(req.Body).Decode(&body))
					a.Equal("", body["comment"])
					return jsonResponse(req, http.StatusOK, `{"pk":91,"model_type":"part","model_id":10,"filename":"datasheet","comment":""}`), nil
				case "DELETE /api/attachment/91/":
					return jsonResponse(req, http.StatusNoContent, ``), nil
				default:
					return jsonResponse(req, http.StatusNotFound, `{}`), nil
				}
			})},
		})
		r.NoError(err)

		_, err = client.CreateLinkAttachment(ctx, AttachmentCreate{ModelType: "part", ModelID: 10, Filename: "datasheet", Link: "https://example.test/datasheet.pdf"})
		r.NoError(err)
		_, err = client.UpdateAttachmentMetadata(ctx, 91, PatchFields{"comment": Set("")})
		r.NoError(err)
		r.NoError(client.DeleteAttachment(ctx, 91))
		a.Equal([]string{"POST /api/attachment/", "PATCH /api/attachment/91/", "DELETE /api/attachment/91/"}, calls)
	})

	t.Run("set part primary image patches part image multipart", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)

		client, err := NewClient(Config{
			BaseURL:    "https://inventory.example.test",
			Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
			HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				a.Equal(http.MethodPatch, req.Method)
				a.Equal("/api/part/10/", req.URL.Path)
				a.Equal("Token secret", req.Header.Get("Authorization"))
				_, files := readMultipartRequest(t, req)
				a.Equal("resistor.png", files["image"].filename)
				a.Equal("image/png", files["image"].contentType)
				a.Equal([]byte("png bytes"), files["image"].content)
				return jsonResponse(req, http.StatusOK, `{"image":"/media/part_images/resistor.png"}`), nil
			})},
		})
		r.NoError(err)

		part, err := client.SetPartPrimaryImage(ctx, 10, PartPrimaryImageCreate{
			Filename:    "resistor.png",
			ContentType: "image/png",
			Content:     []byte("png bytes"),
		})
		r.NoError(err)
		a.Equal("/media/part_images/resistor.png", *part.Image)
	})
}

func TestUploadAttachmentRejectsUnsafeMultipartHeaders(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, http.StatusOK, `{}`), nil
		})},
	})
	r.NoError(err)

	_, err = client.UploadAttachment(ctx, AttachmentCreate{
		ModelType:   "part",
		ModelID:     10,
		Filename:    "data\nsheet.pdf",
		ContentType: "application/pdf",
		Content:     []byte("pdf bytes"),
	})
	r.ErrorContains(err, "filename contains control characters")

	_, err = client.UploadAttachment(ctx, AttachmentCreate{
		ModelType:   "part",
		ModelID:     10,
		Filename:    "datasheet.pdf",
		ContentType: "application/pdf\r\nx-bad: yes",
		Content:     []byte("pdf bytes"),
	})
	r.ErrorContains(err, "content type contains control characters")

	_, err = client.UploadAttachment(ctx, AttachmentCreate{
		ModelType:   "part",
		ModelID:     10,
		Filename:    "datasheet.pdf",
		ContentType: "not a media type",
		Content:     []byte("pdf bytes"),
	})
	r.ErrorContains(err, "content type is invalid")
}

type multipartFileData struct {
	filename    string
	contentType string
	content     []byte
}

func readMultipartRequest(t *testing.T, req *http.Request) (map[string]string, map[string]multipartFileData) {
	t.Helper()
	r := require.New(t)
	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	r.NoError(err)
	r.Equal("multipart/form-data", mediaType)
	reader := multipart.NewReader(req.Body, params["boundary"])
	fields := map[string]string{}
	files := map[string]multipartFileData{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		r.NoError(err)
		content, err := io.ReadAll(part)
		r.NoError(err)
		if part.FileName() == "" {
			fields[part.FormName()] = string(content)
			continue
		}
		files[part.FormName()] = multipartFileData{filename: part.FileName(), contentType: part.Header.Get("Content-Type"), content: content}
	}
	return fields, files
}
