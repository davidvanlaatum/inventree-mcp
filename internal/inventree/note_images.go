package inventree

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/raster"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
)

// NoteImage is upstream metadata. Image is private transport data: tools must
// project metadata rather than serialize this record to an MCP caller.
type NoteImage struct {
	PK    int    `json:"pk"`
	Note  int    `json:"note"`
	Image string `json:"image"`
	User  *int   `json:"user"`
	Date  string `json:"date"`
}

type NoteImageQuery struct{ NoteID, Limit, Offset int }
type NoteImageUpload struct {
	Filename, ContentType string
	Content               []byte
}
type DownloadedNoteImage struct {
	Image       NoteImage
	Content     []byte
	ContentType string
}

var ErrNoteImageOutOfScope = errors.New("note image is not on the selected readable note or configured media path")

func (c *Client) noteImageURL(raw string) (*url.URL, error) {
	u, err := c.resolveInvenTreeContentURL(raw)
	if err != nil {
		return nil, ErrNoteImageOutOfScope
	}
	prefix := path.Join(c.baseURL.Path, "media", "notes") + "/"
	if len(raw) > 2048 || !strings.HasPrefix(u.Path, prefix) || u.Path == prefix || path.Clean(u.Path) != u.Path || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Path, "\\") {
		return nil, ErrNoteImageOutOfScope
	}
	return u, nil
}

func (c *Client) ListNoteImagesPage(ctx context.Context, q NoteImageQuery) (Page[NoteImage], error) {
	if q.NoteID <= 0 || q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 5000 {
		return Page[NoteImage]{}, errors.New("note images require a positive note_id, limit 1-100 and offset 0-5000")
	}
	if _, err := c.GetNote(ctx, q.NoteID); err != nil {
		return Page[NoteImage]{}, err
	}
	p, err := listPage[NoteImage](ctx, c, "/api/note/image/", url.Values{"note": {strconv.Itoa(q.NoteID)}, "limit": {strconv.Itoa(q.Limit)}, "offset": {strconv.Itoa(q.Offset)}, "ordering": {"pk"}})
	if err != nil {
		return Page[NoteImage]{}, err
	}
	if p.Count < 0 || len(p.Results) > q.Limit || p.Count < len(p.Results) {
		return Page[NoteImage]{}, ErrNoteImageOutOfScope
	}
	seen := map[int]bool{}
	for _, im := range p.Results {
		if im.PK <= 0 || im.Note != q.NoteID || seen[im.PK] {
			return Page[NoteImage]{}, ErrNoteImageOutOfScope
		}
		seen[im.PK] = true
		if _, err := c.noteImageURL(im.Image); err != nil {
			return Page[NoteImage]{}, err
		}
	}
	return p, nil
}

func (c *Client) UploadNoteImage(ctx context.Context, noteID int, input NoteImageUpload) (NoteImage, error) {
	if _, err := c.GetNote(ctx, noteID); err != nil {
		return NoteImage{}, err
	}
	if _, err := raster.Validate(raster.Source{Filename: input.Filename, ContentType: input.ContentType, Content: input.Content}); err != nil {
		return NoteImage{}, err
	}
	var out NoteImage
	err := c.postMultipart(ctx, "/api/note/image/", map[string]string{"note": strconv.Itoa(noteID)}, multipartFile{fieldName: "image", filename: input.Filename, contentType: input.ContentType, content: input.Content}, &out)
	if err != nil {
		return NoteImage{}, err
	}
	if out.PK <= 0 || out.Note != noteID {
		return NoteImage{}, ErrNoteImageOutOfScope
	}
	if _, err = c.noteImageURL(out.Image); err != nil {
		return NoteImage{}, err
	}
	return out, nil
}

func (c *Client) DownloadNoteImage(ctx context.Context, noteID, imageID int, maxBytes int64) (DownloadedNoteImage, error) {
	if imageID <= 0 || maxBytes <= 0 || maxBytes > raster.MaxBytes {
		return DownloadedNoteImage{}, errors.New("image_id and bounded max_bytes are required")
	}
	// Never take a URL from the caller or treat a successful media fetch as
	// authorization. Only the scoped, permission-checked metadata is usable.
	var selected *NoteImage
	for offset := 0; offset <= 5000; offset += 100 {
		p, err := c.ListNoteImagesPage(ctx, NoteImageQuery{NoteID: noteID, Limit: 100, Offset: offset})
		if err != nil {
			return DownloadedNoteImage{}, err
		}
		for _, im := range p.Results {
			if im.PK == imageID {
				selected = &im
				break
			}
		}
		if selected != nil || offset+len(p.Results) >= p.Count || len(p.Results) < 100 {
			break
		}
	}
	if selected == nil {
		return DownloadedNoteImage{}, ErrNoteImageOutOfScope
	}
	u, err := c.noteImageURL(selected.Image)
	if err != nil {
		return DownloadedNoteImage{}, err
	}
	downloadCtx, cancel := boundedDownloadContext(ctx, c.httpClient)
	defer cancel()
	downloadCtx = requestctx.WithExplicitRoute(downloadCtx, clientMethodRoutes["DownloadNoteImage"].ManifestID, string(RequestFamilyImageDownload))
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return DownloadedNoteImage{}, ErrNoteImageOutOfScope
	}
	req.Header.Set("Accept", "image/png,image/jpeg,image/webp")
	c.applyRequestIdentity(req)
	resp, err := noRedirectClient(c.httpClient).Do(req)
	if err != nil {
		return DownloadedNoteImage{}, errors.New("note image download failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return DownloadedNoteImage{}, errors.New("note image download was denied, redirected or unavailable")
	}
	data, err := readBounded(resp.Body, maxBytes)
	if err != nil {
		return DownloadedNoteImage{}, errors.New("note image download exceeded bounds or failed")
	}
	mediaType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if _, err = raster.Validate(raster.Source{Filename: path.Base(u.Path), ContentType: mediaType, Content: data}); err != nil {
		return DownloadedNoteImage{}, errors.New("note image download is not a supported bounded raster")
	}
	return DownloadedNoteImage{Image: *selected, Content: data, ContentType: mediaType}, nil
}
