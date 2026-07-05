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

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMethodsUseExpectedEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		call      func(context.Context, *Client) error
		wantPath  string
		wantQuery url.Values
		response  string
	}{
		{
			name: "search parts",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchParts(ctx, SearchQuery{Search: "resistor", Limit: 7, Offset: 3})
				return err
			},
			wantPath:  "/api/part/",
			wantQuery: url.Values{"search": []string{"resistor"}, "limit": []string{"7"}, "offset": []string{"3"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":10,"name":"resistor"}]}`,
		},
		{
			name: "get part",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetPart(ctx, 10)
				return err
			},
			wantPath: "/api/part/10/",
			response: `{"pk":10,"name":"resistor"}`,
		},
		{
			name: "search categories",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchPartCategories(ctx, SearchQuery{Search: "passives"})
				return err
			},
			wantPath:  "/api/part/category/",
			wantQuery: url.Values{"search": []string{"passives"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":20,"name":"passives"}]}`,
		},
		{
			name: "get category",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetPartCategory(ctx, 20)
				return err
			},
			wantPath: "/api/part/category/20/",
			response: `{"pk":20,"name":"passives"}`,
		},
		{
			name: "search companies",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchCompanies(ctx, SearchQuery{Search: "acme"})
				return err
			},
			wantPath:  "/api/company/",
			wantQuery: url.Values{"search": []string{"acme"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":30,"name":"acme"}]}`,
		},
		{
			name: "search suppliers",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchSuppliers(ctx, SearchQuery{Search: "acme"})
				return err
			},
			wantPath:  "/api/company/",
			wantQuery: url.Values{"search": []string{"acme"}, "is_supplier": []string{"true"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":30,"name":"acme","is_supplier":true}]}`,
		},
		{
			name: "search manufacturers",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchManufacturers(ctx, SearchQuery{})
				return err
			},
			wantPath:  "/api/company/",
			wantQuery: url.Values{"is_manufacturer": []string{"true"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":31,"name":"maker","is_manufacturer":true}]}`,
		},
		{
			name: "search stock locations",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchStockLocations(ctx, SearchQuery{Search: "bin"})
				return err
			},
			wantPath:  "/api/stock/location/",
			wantQuery: url.Values{"search": []string{"bin"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":40,"name":"bin"}]}`,
		},
		{
			name: "get stock location",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetStockLocation(ctx, 40)
				return err
			},
			wantPath: "/api/stock/location/40/",
			response: `{"pk":40,"name":"bin"}`,
		},
		{
			name: "search stock items",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchStockItems(ctx, StockItemQuery{PartID: 10, LocationID: 40, Limit: 8, Offset: 4})
				return err
			},
			wantPath:  "/api/stock/",
			wantQuery: url.Values{"part": []string{"10"}, "location": []string{"40"}, "limit": []string{"8"}, "offset": []string{"4"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":50,"part":10,"quantity":2}]}`,
		},
		{
			name: "search part parameters",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchPartParameters(ctx, PartParameterQuery{PartID: 10, Limit: 9, Offset: 5})
				return err
			},
			wantPath:  "/api/parameter/",
			wantQuery: url.Values{"model_id": []string{"10"}, "model_type": []string{"part.part"}, "limit": []string{"9"}, "offset": []string{"5"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":60,"model_type":"part.part","model_id":10,"template":70,"data":"10k"}]}`,
		},
		{
			name: "search parameter templates",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchParameterTemplates(ctx, SearchQuery{Search: "Resistance"})
				return err
			},
			wantPath:  "/api/parameter/template/",
			wantQuery: url.Values{"search": []string{"Resistance"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":70,"name":"Resistance","units":"ohm","choices":"10k,22k"}]}`,
		},
		{
			name: "get parameter template",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetParameterTemplate(ctx, 70)
				return err
			},
			wantPath: "/api/parameter/template/70/",
			response: `{"pk":70,"name":"Resistance","units":"ohm","choices":"10k,22k","enabled":true}`,
		},
		{
			name: "search category parameter templates",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchCategoryParameterTemplates(ctx, CategoryParameterTemplateQuery{CategoryID: 20})
				return err
			},
			wantPath:  "/api/part/category/parameters/",
			wantQuery: nil,
			response:  `{"count":2,"next":null,"previous":null,"results":[{"pk":80,"category":20,"template":70},{"pk":81,"category":21,"template":70}]}`,
		},
		{
			name: "list attachments",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.ListAttachments(ctx, AttachmentQuery{ModelType: "part", ModelID: 10, Search: "datasheet", Limit: 10, Offset: 6})
				return err
			},
			wantPath:  "/api/attachment/",
			wantQuery: url.Values{"model_type": []string{"part"}, "model_id": []string{"10"}, "search": []string{"datasheet"}, "limit": []string{"10"}, "offset": []string{"6"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":90,"model_type":"part","model_id":10,"filename":"datasheet.pdf"}]}`,
		},
		{
			name: "get attachment metadata",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetAttachmentMetadata(ctx, 90)
				return err
			},
			wantPath: "/api/attachment/90/",
			response: `{"pk":90,"model_type":"part","model_id":10,"filename":"datasheet.pdf"}`,
		},
		{
			name: "search supplier parts",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchSupplierParts(ctx, SupplierPartQuery{Part: 10, Supplier: 30, SKU: "abc"})
				return err
			},
			wantPath:  "/api/company/part/",
			wantQuery: url.Values{"part": []string{"10"}, "supplier": []string{"30"}, "SKU": []string{"abc"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":100,"part":10,"supplier":30,"SKU":"abc"}]}`,
		},
		{
			name: "search manufacturer parts",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchManufacturerParts(ctx, ManufacturerPartQuery{Part: 10, Manufacturer: 31, MPN: "mfg-1"})
				return err
			},
			wantPath:  "/api/company/part/manufacturer/",
			wantQuery: url.Values{"part": []string{"10"}, "manufacturer": []string{"31"}, "MPN": []string{"mfg-1"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":110,"part":10,"manufacturer":31,"MPN":"mfg-1"}]}`,
		},
		{
			name: "search purchase orders",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchPurchaseOrders(ctx, PurchaseOrderQuery{Supplier: 30})
				return err
			},
			wantPath:  "/api/order/po/",
			wantQuery: url.Values{"supplier": []string{"30"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":120,"reference":"PO-1","supplier":30}]}`,
		},
		{
			name: "get purchase order",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetPurchaseOrder(ctx, 120)
				return err
			},
			wantPath: "/api/order/po/120/",
			response: `{"pk":120,"reference":"PO-1","supplier":30}`,
		},
		{
			name: "search purchase order lines",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SearchPurchaseOrderLines(ctx, PurchaseOrderLineQuery{Order: 120})
				return err
			},
			wantPath:  "/api/order/po-line/",
			wantQuery: url.Values{"order": []string{"120"}},
			response:  `{"count":1,"next":null,"previous":null,"results":[{"pk":130,"order":120,"part":10,"quantity":1}]}`,
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
					a.Equal(http.MethodGet, req.Method)
					a.Equal(tt.wantPath, req.URL.Path)
					a.Equal(tt.wantQuery.Encode(), req.URL.Query().Encode())
					a.Equal("Token secret", req.Header.Get("Authorization"))
					return jsonResponse(req, http.StatusOK, tt.response), nil
				})},
			})
			r.NoError(err)

			r.NoError(tt.call(ctx, client))
		})
	}
}

func TestDownloadAttachmentFetchesOnlyMetadataURLWithBounds(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	var requests []string
	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests = append(requests, req.Method+" "+req.URL.String())
			a.Equal("Token secret", req.Header.Get("Authorization"))
			switch req.URL.Path {
			case "/api/attachment/90/":
				body := `{"pk":90,"model_type":"part","model_id":10,"attachment":"/media/attachments/datasheet.pdf?signature=secret","filename":"datasheet.pdf"}`
				return jsonResponse(req, http.StatusOK, body), nil
			case "/media/attachments/datasheet.pdf":
				_, hasDeadline := req.Context().Deadline()
				a.True(hasDeadline)
				a.Equal("signature=secret", req.URL.RawQuery)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/pdf"}},
					Body:       io.NopCloser(strings.NewReader("pdf-bytes")),
					Request:    req,
				}, nil
			default:
				return jsonResponse(req, http.StatusNotFound, `{"detail":"unexpected path"}`), nil
			}
		})},
	})
	r.NoError(err)

	download, err := client.DownloadAttachment(ctx, 90, AttachmentContentOriginal, 32)
	r.NoError(err)

	a.Equal("pdf-bytes", string(download.Content))
	a.Equal("application/pdf", download.ContentType)
	a.Equal("https://inventory.example.test/media/attachments/datasheet.pdf", download.SourceURL)
	a.Equal([]string{
		"GET https://inventory.example.test/api/attachment/90/",
		"GET https://inventory.example.test/media/attachments/datasheet.pdf?signature=secret",
	}, requests)
}

func TestDownloadPartImageFetchesOnlyPartImageURLWithBounds(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	var requests []string
	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests = append(requests, req.Method+" "+req.URL.String())
			a.Equal("Token secret", req.Header.Get("Authorization"))
			switch req.URL.Path {
			case "/api/part/10/":
				body := `{"pk":10,"name":"resistor","image":"/media/part_images/resistor.png?signature=secret"}`
				return jsonResponse(req, http.StatusOK, body), nil
			case "/media/part_images/resistor.png":
				_, hasDeadline := req.Context().Deadline()
				a.True(hasDeadline)
				a.Equal("signature=secret", req.URL.RawQuery)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"image/png"}},
					Body:       io.NopCloser(strings.NewReader("png-bytes")),
					Request:    req,
				}, nil
			default:
				return jsonResponse(req, http.StatusNotFound, `{"detail":"unexpected path"}`), nil
			}
		})},
	})
	r.NoError(err)

	download, err := client.DownloadPartImage(ctx, 10, AttachmentContentOriginal, 32)
	r.NoError(err)

	a.Equal("png-bytes", string(download.Content))
	a.Equal("image/png", download.ContentType)
	a.Equal("https://inventory.example.test/media/part_images/resistor.png", download.SourceURL)
	a.Equal([]string{
		"GET https://inventory.example.test/api/part/10/",
		"GET https://inventory.example.test/media/part_images/resistor.png?signature=secret",
	}, requests)
}

func TestDownloadPartImageThumbnailUsesPartThumbEndpoint(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	var requests []string
	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests = append(requests, req.Method+" "+req.URL.String())
			a.Equal("Token secret", req.Header.Get("Authorization"))
			switch req.URL.Path {
			case "/api/part/10/":
				return jsonResponse(req, http.StatusOK, `{"pk":10,"name":"resistor","image":"/media/part_images/resistor.png"}`), nil
			case "/api/part/thumbs/10/":
				return jsonResponse(req, http.StatusOK, `{"image":"/media/part_images/resistor.thumb.png?signature=secret"}`), nil
			case "/media/part_images/resistor.thumb.png":
				a.Equal("signature=secret", req.URL.RawQuery)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"image/png"}},
					Body:       io.NopCloser(strings.NewReader("thumb-bytes")),
					Request:    req,
				}, nil
			default:
				return jsonResponse(req, http.StatusNotFound, `{"detail":"unexpected path"}`), nil
			}
		})},
	})
	r.NoError(err)

	download, err := client.DownloadPartImage(ctx, 10, AttachmentContentThumbnail, 32)
	r.NoError(err)

	a.Equal("thumb-bytes", string(download.Content))
	a.Equal("https://inventory.example.test/media/part_images/resistor.thumb.png", download.SourceURL)
	a.Equal([]string{
		"GET https://inventory.example.test/api/part/10/",
		"GET https://inventory.example.test/api/part/thumbs/10/",
		"GET https://inventory.example.test/media/part_images/resistor.thumb.png?signature=secret",
	}, requests)
}

func TestDownloadPartImageRejectsUnsafeSourcesAndOversizedContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		partBody  string
		thumbBody string
		imageBody string
		status    int
		mode      AttachmentContentMode
		wantError string
	}{
		{
			name:      "missing image",
			partBody:  `{"pk":10,"name":"resistor"}`,
			wantError: "no primary image URL",
		},
		{
			name:      "external image URL",
			partBody:  `{"pk":10,"name":"resistor","image":"https://evil.example.test/image.png"}`,
			wantError: "outside configured InvenTree instance",
		},
		{
			name:      "image URL with userinfo",
			partBody:  `{"pk":10,"name":"resistor","image":"https://user:pass@inventory.example.test/image.png"}`,
			wantError: "must not include userinfo",
		},
		{
			name:      "redirect",
			partBody:  `{"pk":10,"name":"resistor","image":"/media/image.png"}`,
			status:    http.StatusFound,
			wantError: "redirected with status 302",
		},
		{
			name:      "oversized",
			partBody:  `{"pk":10,"name":"resistor","image":"/media/image.png"}`,
			imageBody: "too-large",
			status:    http.StatusOK,
			wantError: "exceeds maxBytes 4",
		},
		{
			name:      "thumbnail URL outside configured instance",
			partBody:  `{"pk":10,"name":"resistor","image":"/media/image.png"}`,
			thumbBody: `{"image":"https://evil.example.test/thumb.png"}`,
			mode:      AttachmentContentThumbnail,
			wantError: "outside configured InvenTree instance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)

			client, err := NewClient(Config{
				BaseURL:    "https://inventory.example.test",
				Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
				HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					switch req.URL.Path {
					case "/api/part/10/":
						return jsonResponse(req, http.StatusOK, tt.partBody), nil
					case "/api/part/thumbs/10/":
						return jsonResponse(req, http.StatusOK, tt.thumbBody), nil
					case "/media/image.png":
						return &http.Response{
							StatusCode: tt.status,
							Header:     http.Header{"Content-Type": []string{"image/png"}},
							Body:       io.NopCloser(strings.NewReader(tt.imageBody)),
							Request:    req,
						}, nil
					default:
						return jsonResponse(req, http.StatusNotFound, `{"detail":"unexpected path"}`), nil
					}
				})},
			})
			r.NoError(err)

			_, err = client.DownloadPartImage(ctx, 10, tt.mode, 4)
			r.ErrorContains(err, tt.wantError)
		})
	}
}

func TestDownloadPartImageDoesNotSurfaceSensitiveURLOnTransportError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/api/part/10/":
				return jsonResponse(req, http.StatusOK, `{"pk":10,"name":"resistor","image":"/media/image.png?signature=secret"}`), nil
			case "/media/image.png":
				return nil, errors.New("dial tcp inventory.example.test:443 failed")
			default:
				return jsonResponse(req, http.StatusNotFound, `{"detail":"unexpected path"}`), nil
			}
		})},
	})
	r.NoError(err)

	_, err = client.DownloadPartImage(ctx, 10, AttachmentContentOriginal, 1024)
	r.Error(err)
	a.Equal("download InvenTree part image failed", err.Error())
}

func TestDownloadAttachmentRejectsUnsafeSourcesAndOversizedContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		metadataBody string
		contentBody  string
		status       int
		wantError    string
	}{
		{
			name:         "missing URL",
			metadataBody: `{"pk":90,"model_type":"part","model_id":10}`,
			wantError:    "no file attachment URL",
		},
		{
			name:         "external URL",
			metadataBody: `{"pk":90,"model_type":"part","model_id":10,"attachment":"https://evil.example.test/file.pdf"}`,
			wantError:    "outside configured InvenTree instance",
		},
		{
			name:         "out of scope model type",
			metadataBody: `{"pk":90,"model_type":"salesorder","model_id":10,"attachment":"/media/file.pdf"}`,
			wantError:    `model type "salesorder" is out of scope`,
		},
		{
			name:         "userinfo",
			metadataBody: `{"pk":90,"model_type":"part","model_id":10,"attachment":"https://user:pass@inventory.example.test/media/file.pdf"}`,
			wantError:    "must not include userinfo",
		},
		{
			name:         "redirect",
			metadataBody: `{"pk":90,"model_type":"part","model_id":10,"attachment":"/media/file.pdf"}`,
			status:       http.StatusFound,
			wantError:    "redirected",
		},
		{
			name:         "oversized",
			metadataBody: `{"pk":90,"model_type":"part","model_id":10,"attachment":"/media/file.pdf"}`,
			status:       http.StatusOK,
			contentBody:  "too-large",
			wantError:    "exceeds maxBytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)

			client, err := NewClient(Config{
				BaseURL:    "https://inventory.example.test",
				Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
				HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					switch req.URL.Path {
					case "/api/attachment/90/":
						return jsonResponse(req, http.StatusOK, tt.metadataBody), nil
					case "/media/file.pdf":
						status := tt.status
						if status == 0 {
							status = http.StatusOK
						}
						return &http.Response{
							StatusCode: status,
							Header:     http.Header{"Content-Type": []string{"application/octet-stream"}},
							Body:       io.NopCloser(strings.NewReader(tt.contentBody)),
							Request:    req,
						}, nil
					default:
						return jsonResponse(req, http.StatusNotFound, `{"detail":"unexpected path"}`), nil
					}
				})},
			})
			r.NoError(err)

			_, err = client.DownloadAttachment(ctx, 90, AttachmentContentOriginal, 4)
			r.Error(err)
			r.Contains(err.Error(), tt.wantError)
		})
	}
}

func TestDownloadAttachmentDoesNotSurfaceSensitiveURLOnTransportError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/api/attachment/90/":
				return jsonResponse(req, http.StatusOK, `{"pk":90,"model_type":"part","model_id":10,"attachment":"/media/file.pdf?signature=secret"}`), nil
			case "/media/file.pdf":
				return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: io.ErrUnexpectedEOF}
			default:
				return jsonResponse(req, http.StatusNotFound, `{"detail":"unexpected path"}`), nil
			}
		})},
	})
	r.NoError(err)

	_, err = client.DownloadAttachment(ctx, 90, AttachmentContentOriginal, 1024)
	r.Error(err)
	r.Contains(err.Error(), "download InvenTree attachment content failed")
	r.NotContains(err.Error(), "signature=secret")
	r.NotContains(err.Error(), "/media/file.pdf")
}

func TestDownloadAttachmentRejectsInvalidOptionsBeforeFetch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid maxBytes should not send a request")
			return nil, nil
		})},
	})
	r.NoError(err)

	_, err = client.DownloadAttachment(ctx, 90, AttachmentContentOriginal, 0)
	r.Error(err)
	r.Contains(err.Error(), "maxBytes must be positive")
}

func TestReadModelsDecodeRepresentativeJSON(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var part Part
	r.NoError(json.Unmarshal([]byte(`{"pk":10,"name":"R1","category":20,"default_location":40,"image":"/media/part.png"}`), &part))
	r.Equal(10, part.PK)
	r.Equal("R1", part.Name)
	r.NotNil(part.Category)
	r.NotNil(part.DefaultLocation)
	r.NotNil(part.Image)

	var template ParameterTemplate
	r.NoError(json.Unmarshal([]byte(`{"pk":70,"name":"Resistance","units":"ohm","choices":"10k,22k","checkbox":false}`), &template))
	r.Equal("10k,22k", template.Choices)
	r.NotNil(template.Units)

	var parameter Parameter
	r.NoError(json.Unmarshal([]byte(`{"pk":60,"template":70,"model_type":"part.part","model_id":10,"data":"10k"}`), &parameter))
	r.Equal("part.part", parameter.ModelType)
	r.Equal(10, parameter.ModelID)

	var categoryTemplate CategoryParameterTemplate
	r.NoError(json.Unmarshal([]byte(`{"pk":80,"category":20,"template":70}`), &categoryTemplate))
	r.Equal(70, categoryTemplate.Template)
}
