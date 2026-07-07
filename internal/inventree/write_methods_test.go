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
					Part:     10,
					Location: 40,
					Quantity: 7,
					Status:   dvgoutils.Ptr(10),
					Batch:    dvgoutils.Ptr("B-1"),
					Serial:   dvgoutils.Ptr("S-1"),
					Notes:    dvgoutils.Ptr("initial stock"),
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
				a.Equal("initial stock", body["notes"])
				_, hasCustomer := body["customer"]
				a.False(hasCustomer)
				_, hasSalesOrder := body["sales_order"]
				a.False(hasSalesOrder)
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
