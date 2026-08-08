package inventree

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetAndClearCompanyPrimaryImageUseNarrowPatchContracts(t *testing.T) {
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
			a.Equal("Token secret", req.Header.Get("Authorization"))
			switch len(calls) {
			case 1:
				_, files := readMultipartRequest(t, req)
				a.Len(files, 1)
				a.Equal("logo.png", files["image"].filename)
				a.Equal("image/png", files["image"].contentType)
				a.Equal([]byte("png bytes"), files["image"].content)
				return jsonResponse(req, http.StatusOK, `{"pk":30,"name":"Supplier","currency":"AUD","image":"/media/company_images/company_30_img.png"}`), nil
			case 2:
				var payload map[string]any
				r.NoError(json.NewDecoder(req.Body).Decode(&payload))
				r.Contains(payload, "image")
				a.Nil(payload["image"])
				a.Len(payload, 1)
				return jsonResponse(req, http.StatusOK, `{"pk":30,"name":"Supplier","currency":"AUD","image":null}`), nil
			default:
				return jsonResponse(req, http.StatusInternalServerError, `{}`), nil
			}
		})},
	})
	r.NoError(err)

	updated, err := client.SetCompanyPrimaryImage(ctx, 30, CompanyPrimaryImageCreate{Filename: "logo.png", ContentType: "image/png", Content: []byte("png bytes")})
	r.NoError(err)
	a.Equal(30, updated.PK)
	a.NotNil(updated.Image)
	cleared, err := client.ClearCompanyPrimaryImage(ctx, 30)
	r.NoError(err)
	a.Equal(30, cleared.PK)
	a.Nil(cleared.Image)
	a.Equal([]string{"PATCH /api/company/30/", "PATCH /api/company/30/"}, calls)
}

func TestDownloadCompanyImageUsesOnlyExactSchemaExposedSameInstanceURL(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	var calls []string

	client, err := NewClient(Config{
		BaseURL:    "https://inventory.example.test",
		Credential: Credential{Scheme: AuthSchemeBearer, Token: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls = append(calls, req.Method+" "+req.URL.String())
			a.Equal("Bearer secret", req.Header.Get("Authorization"))
			switch req.URL.Path {
			case "/api/company/30/":
				return jsonResponse(req, http.StatusOK, `{"pk":30,"name":"Supplier","currency":"AUD","image":"/media/company_images/logo.png?signature=secret"}`), nil
			case "/media/company_images/logo.png":
				resp := jsonResponse(req, http.StatusOK, "png bytes")
				resp.Header = http.Header{"Content-Type": []string{"image/png"}}
				return resp, nil
			default:
				return jsonResponse(req, http.StatusNotFound, `{}`), nil
			}
		})},
	})
	r.NoError(err)

	download, err := client.DownloadCompanyImage(ctx, 30, 32)
	r.NoError(err)
	a.Equal(30, download.Company.PK)
	a.Equal([]byte("png bytes"), download.Content)
	a.Equal("logo.png", download.Filename)
	a.Equal("image/png", download.ContentType)
	a.Equal("https://inventory.example.test/media/company_images/logo.png", download.SourceURL)
	r.Len(calls, 2)
}

func TestDownloadCompanyImageRejectsMissingOutOfScopeAndOversizedContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, image, mediaBody, want string
		mediaStatus                  int
		mediaError                   bool
	}{
		{name: "missing", image: "null", want: ErrCompanyImageMissing.Error()},
		{name: "cross instance", image: `"https://other.example.test/logo.png"`, want: "configured InvenTree instance"},
		{name: "oversized", image: `"/media/logo.png"`, mediaBody: "12345", want: "exceeds maxBytes 4"},
		{name: "redirect", image: `"/media/logo.png"`, mediaStatus: http.StatusFound, want: "redirected with status 302"},
		{name: "not found", image: `"/media/logo.png"`, mediaStatus: http.StatusNotFound, want: "failed with status 404"},
		{name: "transport failure", image: `"/media/logo.png"`, mediaError: true, want: "download InvenTree company image failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			client, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/company/30/" {
					return jsonResponse(req, http.StatusOK, `{"pk":30,"name":"Supplier","currency":"AUD","image":`+tt.image+`}`), nil
				}
				if tt.mediaError {
					return nil, errors.New("secret transport detail")
				}
				status := tt.mediaStatus
				if status == 0 {
					status = http.StatusOK
				}
				resp := jsonResponse(req, status, tt.mediaBody)
				resp.Header = http.Header{"Content-Type": []string{"image/png"}}
				return resp, nil
			})}})
			r.NoError(err)
			_, err = client.DownloadCompanyImage(ctx, 30, 4)
			r.ErrorContains(err, tt.want)
		})
	}
}

func TestDownloadCompanyImageRejectsInvalidLimitAndCompanyLookupFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, http.StatusInternalServerError, `{}`), nil
	})}})
	r.NoError(err)
	_, err = client.DownloadCompanyImage(ctx, 30, 0)
	r.ErrorContains(err, "maxBytes must be positive")
	_, err = client.DownloadCompanyImage(ctx, 30, 4)
	r.ErrorContains(err, "failed with 500")
}

func TestDownloadCompanyImageRejectsMismatchedCompanyBeforeMediaFetch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	var calls []string
	client, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.Path)
		return jsonResponse(req, http.StatusOK, `{"pk":31,"name":"Wrong","currency":"AUD","image":"/media/wrong.png"}`), nil
	})}})
	r.NoError(err)
	_, err = client.DownloadCompanyImage(ctx, 30, 1024)
	r.ErrorContains(err, "mismatched identity")
	a.Equal([]string{"/api/company/30/"}, calls)
}
