//go:build !no_integration_tests

package inventree_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/stretchr/testify/require"
)

// F-S107 characterization only: no runtime client method or MCP tool is added.
type discoveredNoteImage struct {
	PK    int    `json:"pk"`
	Image string `json:"image"`
	Note  int    `json:"note"`
	User  int    `json:"user"`
}

func runNoteImageDiscovery(t *testing.T, shared *testenv.SharedInvenTree) {
	t.Helper()
	for _, model := range []string{"part.part", "company.company", "company.supplierpart", "company.manufacturerpart", "stock.stockitem", "order.purchaseorder"} {
		t.Run(model, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			f := newClientMethodFixture(t, shared)
			var parent int
			switch model {
			case "part.part":
				parent = f.ensure(t, testenv.FixturePart).ID
			case "company.company":
				parent = f.ensure(t, testenv.FixtureSupplier).ID
			case "company.supplierpart":
				parent = f.ensure(t, testenv.FixtureSupplierPart).ID
			case "company.manufacturerpart":
				part := f.ensure(t, testenv.FixturePart)
				company := f.ensure(t, testenv.FixtureManufacturer)
				name, err := f.run.Name("image-mpn")
				r.NoError(err)
				p, err := f.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{Part: part.ID, Manufacturer: company.ID, MPN: &name})
				r.NoError(err)
				parent = p.PK
			case "stock.stockitem":
				part := f.ensure(t, testenv.FixturePart)
				location := f.ensure(t, testenv.FixtureLocation)
				p, err := f.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 1})
				r.NoError(err)
				parent = p.PK
			case "order.purchaseorder":
				supplier := f.ensure(t, testenv.FixtureSupplier)
				reference, err := f.run.Name("image-order")
				r.NoError(err)
				order, err := f.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, SupplierReference: &reference})
				r.NoError(err)
				parent = order.PK
			}
			name, err := f.run.Name("image-note")
			r.NoError(err)
			note, err := f.client.CreateNote(ctx, inventree.NoteCreate{ModelType: model, ModelID: parent, Title: name, Content: "original"})
			r.NoError(err)
			image := uploadDiscoveredNoteImage(t, ctx, f.client, note.PK, name+".png", tinyPNG(), http.StatusCreated)
			r.Equal(note.PK, image.Note)
			r.Positive(image.User)
			before, err := f.client.GetNote(ctx, note.PK)
			r.NoError(err)
			r.Equal(note, before, "upload does not update note content or primary state")
			images := listDiscoveredNoteImages(t, ctx, f.client, url.Values{"note": {strconv.Itoa(note.PK)}})
			r.Len(images, 1)
			r.Equal(image, images[0])
			images = listDiscoveredNoteImages(t, ctx, f.client, url.Values{"model_type": {model}, "model_id": {strconv.Itoa(parent)}})
			r.Len(images, 1)
			_, short, ok := strings.Cut(model, ".")
			r.True(ok)
			images = listDiscoveredNoteImages(t, ctx, f.client, url.Values{"model_type": {short}, "model_id": {strconv.Itoa(parent)}})
			r.Len(images, 1)
			content := `<p>diagram</p><img src="` + image.Image + `" alt="diagram">`
			updated, err := f.client.UpdateNote(ctx, note.PK, inventree.PatchFields{"content": inventree.Set(content)})
			r.NoError(err)
			r.Contains(updated.Content, `<img src="`+image.Image+`"`)
			r.True(updated.Primary)
			r.Len(listDiscoveredNoteImages(t, ctx, f.client, url.Values{"note": {strconv.Itoa(note.PK)}}), 1)
			status, data := discoveredMedia(t, ctx, f.client, image.Image)
			r.Equal(http.StatusOK, status)
			r.Equal(tinyPNG(), data)
			r.NoError(f.client.DeleteNote(ctx, note.PK))
			// The note FK filter validates existence: a deleted note yields
			// 400, so verify absence through the surviving parent instead.
			req, err := f.client.NewRequest(ctx, http.MethodGet, "/api/note/image/", url.Values{"limit": {"100"}, "note": {strconv.Itoa(note.PK)}}, nil)
			r.NoError(err)
			var apiErr *inventree.APIError
			r.ErrorAs(f.client.DoJSON(req, nil), &apiErr)
			r.Equal(http.StatusBadRequest, apiErr.StatusCode)
			r.Contains(apiErr.FieldErrors, "note")
			r.Empty(listDiscoveredNoteImages(t, ctx, f.client, url.Values{"model_type": {model}, "model_id": {strconv.Itoa(parent)}}))
			status, _ = discoveredMedia(t, ctx, f.client, image.Image)
			r.Equal(http.StatusNotFound, status, "note cascade removes stored file")
			uploadDiscoveredNoteImage(t, ctx, f.client, note.PK, name+".png", tinyPNG(), http.StatusBadRequest)
		})
	}
	t.Run("lifecycle_and_permissions", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		f := newClientMethodFixture(t, shared)
		part := f.ensure(t, testenv.FixturePart)
		name, err := f.run.Name("lifecycle")
		r.NoError(err)
		note, err := f.client.CreateNote(ctx, inventree.NoteCreate{ModelType: "part.part", ModelID: part.ID, Title: name, Content: "original"})
		r.NoError(err)
		image := uploadDiscoveredNoteImage(t, ctx, f.client, note.PK, name+".png", tinyPNG(), http.StatusCreated)
		duplicate := uploadDiscoveredNoteImage(t, ctx, f.client, note.PK, name+".png", tinyPNG(), http.StatusCreated)
		r.NotEqual(image.PK, duplicate.PK)
		r.NotEqual(image.Image, duplicate.Image)
		uploadDiscoveredNoteImage(t, ctx, f.client, 0, name+".png", tinyPNG(), http.StatusBadRequest)
		uploadDiscoveredNoteImage(t, ctx, f.client, note.PK, name+".png", []byte("invalid image"), http.StatusBadRequest)
		uploadDiscoveredNoteImage(t, ctx, f.client, note.PK, name+".svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), http.StatusBadRequest)
		// No schema-exposed image detail or direct replacement/deletion route.
		for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
			req, err := f.client.NewRequest(ctx, method, fmt.Sprintf("/api/note/image/%d/", image.PK), nil, nil)
			r.NoError(err)
			err = f.client.DoJSON(req, nil)
			var apiErr *inventree.APIError
			r.ErrorAs(err, &apiErr)
			r.Equal(http.StatusNotFound, apiErr.StatusCode)
		}
		// Metadata follows parent model permissions, not image creator identity.
		restricted := newNonStaffClient(t, ctx, f, "image-reader", "integration-image-password-9284")
		r.Empty(listDiscoveredNoteImages(t, ctx, restricted, url.Values{"note": {strconv.Itoa(note.PK)}}))
		uploadDiscoveredNoteImage(t, ctx, restricted, note.PK, name+".png", tinyPNG(), http.StatusForbidden)
		status, data := discoveredMedia(t, ctx, restricted, image.Image)
		r.Equal(http.StatusOK, status, "authenticated media access is broader than note metadata permissions")
		r.Equal(tinyPNG(), data)
		parsed, err := url.Parse(image.Image)
		r.NoError(err)
		target := shared.Environment().BaseURL + parsed.Path
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		r.NoError(err)
		resp, err := http.DefaultClient.Do(req)
		r.NoError(err)
		r.NoError(resp.Body.Close())
		r.Equal(http.StatusUnauthorized, resp.StatusCode)
		var me struct {
			PK int `json:"pk"`
		}
		req, err = restricted.NewRequest(ctx, http.MethodGet, "/api/user/me/", nil, nil)
		r.NoError(err)
		r.NoError(restricted.DoJSON(req, &me))
		var group struct {
			PK int `json:"pk"`
		}
		r.NoError(f.client.Post(ctx, "/api/user/group/", map[string]any{"name": name}, &group))
		var rules []struct {
			PK    int    `json:"pk"`
			Group int    `json:"group"`
			Name  string `json:"name"`
		}
		req, err = f.client.NewRequest(ctx, http.MethodGet, "/api/user/ruleset/", nil, nil)
		r.NoError(err)
		r.NoError(f.client.DoJSON(req, &rules))
		ruleID := 0
		for _, rule := range rules {
			if rule.Group == group.PK && rule.Name == "part" {
				ruleID = rule.PK
			}
		}
		r.Positive(ruleID)
		r.NoError(f.client.Patch(ctx, fmt.Sprintf("/api/user/ruleset/%d/", ruleID), inventree.PatchFields{"can_view": inventree.Set(true), "can_change": inventree.Set(false)}, nil))
		r.NoError(f.client.Patch(ctx, fmt.Sprintf("/api/user/%d/", me.PK), inventree.PatchFields{"group_ids": inventree.Set([]int{group.PK})}, nil))
		r.Len(listDiscoveredNoteImages(t, ctx, restricted, url.Values{"note": {strconv.Itoa(note.PK)}}), 2)
		uploadDiscoveredNoteImage(t, ctx, restricted, note.PK, name+".png", tinyPNG(), http.StatusForbidden)
		r.NoError(f.client.Patch(ctx, fmt.Sprintf("/api/user/ruleset/%d/", ruleID), inventree.PatchFields{"can_change": inventree.Set(true)}, nil))
		other := uploadDiscoveredNoteImage(t, ctx, restricted, note.PK, name+".png", tinyPNG(), http.StatusCreated)
		r.Equal(me.PK, other.User)
		// Bare Markdown is retained as text; the browser editor consumes HTML.
		markdown := "![diagram](" + image.Image + ")"
		updated, err := f.client.UpdateNote(ctx, note.PK, inventree.PatchFields{"content": inventree.Set(markdown)})
		r.NoError(err)
		r.Equal(markdown, updated.Content)
		images := listDiscoveredNoteImages(t, ctx, f.client, url.Values{"note": {strconv.Itoa(note.PK)}})
		r.Len(images, 1)
		r.Equal(image.PK, images[0].PK)
		status, _ = discoveredMedia(t, ctx, f.client, duplicate.Image)
		r.Equal(http.StatusNotFound, status)
		// A metadata-only save still invokes cleanup on all unreferenced images.
		orphan := uploadDiscoveredNoteImage(t, ctx, f.client, note.PK, name+".png", tinyPNG(), http.StatusCreated)
		_, err = f.client.UpdateNote(ctx, note.PK, inventree.PatchFields{"description": inventree.Set("metadata save")})
		r.NoError(err)
		status, _ = discoveredMedia(t, ctx, f.client, orphan.Image)
		r.Equal(http.StatusNotFound, status)
		_, err = f.client.UpdateNote(ctx, note.PK, inventree.PatchFields{"content": inventree.Set("removed")})
		r.NoError(err)
		r.Empty(listDiscoveredNoteImages(t, ctx, f.client, url.Values{"note": {strconv.Itoa(note.PK)}}))
		status, _ = discoveredMedia(t, ctx, f.client, image.Image)
		r.Equal(http.StatusNotFound, status)
	})
}

func uploadDiscoveredNoteImage(t *testing.T, ctx context.Context, c *inventree.Client, note int, filename string, data []byte, wantStatus int) discoveredNoteImage {
	t.Helper()
	r := require.New(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if note != 0 {
		r.NoError(writer.WriteField("note", strconv.Itoa(note)))
	}
	file, err := writer.CreateFormFile("image", filename)
	r.NoError(err)
	_, err = file.Write(data)
	r.NoError(err)
	r.NoError(writer.Close())
	req, err := c.NewRequest(ctx, http.MethodPost, "/api/note/image/", nil, nil)
	r.NoError(err)
	req.Body = io.NopCloser(&body)
	req.ContentLength = int64(body.Len())
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	r.NoError(err)
	defer func() { r.NoError(resp.Body.Close()) }()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	r.NoError(err)
	r.Equal(wantStatus, resp.StatusCode, "notes-image upload status")
	var image discoveredNoteImage
	if wantStatus == http.StatusCreated {
		r.NoError(json.Unmarshal(payload, &image))
		r.Positive(image.PK)
		r.NotEmpty(image.Image)
	}
	return image
}

func listDiscoveredNoteImages(t *testing.T, ctx context.Context, c *inventree.Client, q url.Values) []discoveredNoteImage {
	t.Helper()
	r := require.New(t)
	q.Set("limit", "100")
	req, err := c.NewRequest(ctx, http.MethodGet, "/api/note/image/", q, nil)
	r.NoError(err)
	var page struct {
		Results []discoveredNoteImage `json:"results"`
	}
	r.NoError(c.DoJSON(req, &page))
	return page.Results
}

func discoveredMedia(t *testing.T, ctx context.Context, c *inventree.Client, raw string) (int, []byte) {
	t.Helper()
	r := require.New(t)
	u, err := url.Parse(raw)
	r.NoError(err)
	r.True(strings.HasPrefix(u.Path, "/media/notes/"))
	req, err := c.NewRequest(ctx, http.MethodGet, u.Path, nil, nil)
	r.NoError(err)
	resp, err := http.DefaultClient.Do(req)
	r.NoError(err)
	defer func() { r.NoError(resp.Body.Close()) }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	r.NoError(err)
	return resp.StatusCode, data
}
