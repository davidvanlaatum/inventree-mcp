package inventree

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/require"
)

func noteTestPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	require.New(t).NoError(png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	return b.Bytes()
}

func TestNoteImageClientSuccessfulContracts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	data := noteTestPNG(t)
	im := NoteImage{PK: 10, Note: 2, Image: "/media/notes/x.png"}
	var posted, downloaded bool
	c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "private-token"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		r.Equal("Token private-token", req.Header.Get("Authorization"))
		switch req.URL.Path {
		case "/api/note/2/":
			return jsonResponse(req, 200, `{"pk":2,"model_type":"part.part","model_id":1,"title":"N"}`), nil
		case "/api/part/1/":
			return jsonResponse(req, 200, `{"pk":1}`), nil
		case "/api/note/image/":
			if req.Method == http.MethodPost {
				posted = true
				r.NoError(req.ParseMultipartForm(6 << 20))
				r.Equal("2", req.FormValue("note"))
				file, header, e := req.FormFile("image")
				r.NoError(e)
				defer func() { r.NoError(file.Close()) }()
				b, e := io.ReadAll(file)
				r.NoError(e)
				r.Equal(data, b)
				r.Equal("x.png", header.Filename)
				r.Equal("image/png", header.Header.Get("Content-Type"))
				b, e = json.Marshal(im)
				r.NoError(e)
				return jsonResponse(req, 201, string(b)), nil
			}
			r.Equal("2", req.URL.Query().Get("note"))
			r.Equal("pk", req.URL.Query().Get("ordering"))
			r.NotContains(req.URL.Query(), "model_type")
			b, e := json.Marshal(Page[NoteImage]{Count: 1, Results: []NoteImage{im}})
			r.NoError(e)
			return jsonResponse(req, 200, string(b)), nil
		case "/media/notes/x.png":
			downloaded = true
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(data)), Request: req}, nil
		default:
			t.Errorf("unexpected request %s", req.URL.Path)
			return jsonResponse(req, 404, `{}`), nil
		}
	})}})
	r.NoError(err)
	p, err := c.ListNoteImagesPage(ctx, NoteImageQuery{NoteID: 2, Limit: 20})
	r.NoError(err)
	r.Equal([]NoteImage{im}, p.Results)
	got, err := c.UploadNoteImage(ctx, 2, NoteImageUpload{Filename: "x.png", ContentType: "image/png", Content: data})
	r.NoError(err)
	r.Equal(im, got)
	r.True(posted)
	d, err := c.DownloadNoteImage(ctx, 2, 10, 1024)
	r.NoError(err)
	r.Equal(data, d.Content)
	r.Equal(im, d.Image)
	r.Equal("image/png", d.ContentType)
	r.True(downloaded)
}

func TestNoteImageClientRejectsUnsafeScopesAndMedia(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, url               string
		note, pk, count, status int
	}{
		{"other note", "/media/notes/x.png", 3, 10, 1, 200},
		{"zero identity", "/media/notes/x.png", 2, 0, 1, 200},
		{"negative count", "/media/notes/x.png", 2, 10, -1, 200},
		{"other origin", "https://evil.example.test/media/notes/x.png?token=private", 2, 10, 1, 200},
		{"userinfo", "https://user:private@inventory.example.test/media/notes/x.png", 2, 10, 1, 200},
		{"wrong media path", "/media/part_images/x.png", 2, 10, 1, 200},
		{"dot path", "/media/notes/../x.png", 2, 10, 1, 200},
		{"encoded slash", "/media/notes/a%2fb.png", 2, 10, 1, 200},
		{"query", "/media/notes/x.png?private=yes", 2, 10, 1, 200},
		{"fragment", "/media/notes/x.png#private", 2, 10, 1, 200},
		{"invalid escape", "/media/notes/%private.png", 2, 10, 1, 200},
		{"backslash", `/media/notes\private.png`, 2, 10, 1, 200},
		{"empty filename", "/media/notes/", 2, 10, 1, 200},
		{"oversized URL", "/media/notes/" + strings.Repeat("x", 2050), 2, 10, 1, 200},
		{"denied list", "/media/notes/x.png", 2, 10, 1, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			mediaCalls := 0
			c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/note/2/" {
					return jsonResponse(req, 200, `{"pk":2,"model_type":"part.part","model_id":1}`), nil
				}
				if req.URL.Path == "/api/part/1/" {
					return jsonResponse(req, 200, `{"pk":1}`), nil
				}
				if req.URL.Path == "/api/note/image/" {
					b, e := json.Marshal(Page[NoteImage]{Count: tc.count, Results: []NoteImage{{PK: tc.pk, Note: tc.note, Image: tc.url}}})
					r.NoError(e)
					return jsonResponse(req, tc.status, string(b)), nil
				}
				mediaCalls++
				return jsonResponse(req, 200, `{}`), nil
			})}})
			r.NoError(err)
			_, err = c.ListNoteImagesPage(ctx, NoteImageQuery{NoteID: 2, Limit: 20})
			r.Error(err)
			_, err = c.DownloadNoteImage(ctx, 2, 10, 1024)
			r.Error(err)
			r.NotContains(err.Error(), "private")
			r.Zero(mediaCalls)
		})
	}
}

func TestNoteImageClientMediaBoundsAndFailures(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"denied parent", "denied note", "template", "missing image", "too large", "bad raster", "bad type", "redirect", "transport failure", "truncated", "next page", "duplicate rows", "oversized page"} {
		t.Run(mode, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			data := noteTestPNG(t)
			mediaCalls := 0
			c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/api/note/2/":
					if mode == "denied note" {
						return jsonResponse(req, 403, `{}`), nil
					}
					if mode == "template" {
						return jsonResponse(req, 200, `{"pk":2,"template":true}`), nil
					}
					return jsonResponse(req, 200, `{"pk":2,"model_type":"part.part","model_id":1}`), nil
				case "/api/part/1/":
					status := 200
					if mode == "denied parent" {
						status = 403
					}
					return jsonResponse(req, status, `{"pk":1}`), nil
				case "/api/note/image/":
					p := Page[NoteImage]{Count: 1, Results: []NoteImage{{PK: 10, Note: 2, Image: "/media/notes/x.png"}}}
					if mode == "missing image" {
						p.Results = nil
						p.Count = 0
					}
					if mode == "duplicate rows" {
						p.Results = append(p.Results, p.Results[0])
						p.Count = 2
					}
					if mode == "oversized page" {
						for i := 0; i < 101; i++ {
							p.Results = append(p.Results, NoteImage{PK: i + 20, Note: 2, Image: "/media/notes/x.png"})
						}
						p.Count = len(p.Results)
					}
					if mode == "next page" && req.URL.Query().Get("offset") == "0" {
						p.Count = 101
						p.Results = nil
						for i := 0; i < 100; i++ {
							p.Results = append(p.Results, NoteImage{PK: i + 20, Note: 2, Image: "/media/notes/x.png"})
						}
					}
					b, e := json.Marshal(p)
					r.NoError(e)
					return jsonResponse(req, 200, string(b)), nil
				default:
					mediaCalls++
					if mode == "transport failure" {
						return nil, errors.New("private URL transport error")
					}
					resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(data)), Request: req}
					if mode == "redirect" {
						resp.StatusCode = 302
						resp.Header.Set("Location", "https://evil.example.test/private")
					}
					if mode == "bad type" {
						resp.Header.Set("Content-Type", "text/html")
					}
					if mode == "bad raster" {
						resp.Body = io.NopCloser(strings.NewReader("private-not-an-image"))
					}
					if mode == "truncated" {
						resp.Body = io.NopCloser(bytes.NewReader(data[:20]))
					}
					return resp, nil
				}
			})}})
			r.NoError(err)
			maxBytes := int64(1024)
			if mode == "too large" {
				maxBytes = 4
			}
			_, err = c.DownloadNoteImage(ctx, 2, 10, maxBytes)
			if mode == "next page" {
				r.NoError(err)
				r.Equal(1, mediaCalls)
			} else {
				r.Error(err)
				r.NotContains(err.Error(), "private")
				if mode == "denied parent" || mode == "denied note" || mode == "template" || mode == "missing image" || mode == "duplicate rows" || mode == "oversized page" {
					r.Zero(mediaCalls)
				}
			}
		})
	}
}

func TestNoteImageClientInputAndUploadFailures(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	data := noteTestPNG(t)
	for _, mode := range []string{"invalid raster", "post denied", "response wrong note", "response bad URL", "invalid page request", "invalid download request"} {
		t.Run(mode, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			writes := 0
			c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/note/2/" {
					return jsonResponse(req, 200, `{"pk":2,"model_type":"part.part","model_id":1}`), nil
				}
				if req.URL.Path == "/api/part/1/" {
					return jsonResponse(req, 200, `{"pk":1}`), nil
				}
				writes++
				if mode == "post denied" {
					return jsonResponse(req, 403, `{}`), nil
				}
				im := NoteImage{PK: 10, Note: 2, Image: "/media/notes/x.png"}
				if mode == "response wrong note" {
					im.Note = 3
				}
				if mode == "response bad URL" {
					im.Image = "https://evil.example.test/private"
				}
				b, e := json.Marshal(im)
				r.NoError(e)
				return jsonResponse(req, 201, string(b)), nil
			})}})
			r.NoError(err)
			switch mode {
			case "invalid page request":
				for _, q := range []NoteImageQuery{{NoteID: 0, Limit: 20}, {NoteID: 2, Limit: 101}, {NoteID: 2, Limit: 20, Offset: 5001}, {NoteID: 2, Limit: 20, Offset: -1}} {
					_, err = c.ListNoteImagesPage(ctx, q)
					r.Error(err)
				}
				r.Zero(writes)
			case "invalid download request":
				_, err = c.DownloadNoteImage(ctx, 2, 0, 1024)
				r.Error(err)
				_, err = c.DownloadNoteImage(ctx, 2, 10, 0)
				r.Error(err)
				_, err = c.DownloadNoteImage(ctx, 2, 10, 6<<20)
				r.Error(err)
				r.Zero(writes)
			default:
				b := data
				if mode == "invalid raster" {
					b = []byte("not png")
				}
				_, err = c.UploadNoteImage(ctx, 2, NoteImageUpload{Filename: "x.png", ContentType: "image/png", Content: b})
				r.Error(err)
				if mode == "invalid raster" {
					r.Zero(writes)
				} else {
					r.Equal(1, writes)
				}
			}
		})
	}
	// GetNote denies invalid identities before any upload work.
	c, err := NewClient(Config{BaseURL: "https://inventory.example.test", Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"}})
	r.NoError(err)
	_, err = c.UploadNoteImage(ctx, 0, NoteImageUpload{})
	r.Error(err)
}
