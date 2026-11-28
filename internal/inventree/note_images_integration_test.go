//go:build !no_integration_tests

package inventree_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/stretchr/testify/require"
)

func noteImageParent(t *testing.T, ctx context.Context, f clientMethodFixture, model string) int {
	t.Helper()
	r := require.New(t)
	switch model {
	case "part.part":
		return f.ensure(t, testenv.FixturePart).ID
	case "company.company":
		return f.ensure(t, testenv.FixtureSupplier).ID
	case "company.supplierpart":
		return f.ensure(t, testenv.FixtureSupplierPart).ID
	case "company.manufacturerpart":
		part := f.ensure(t, testenv.FixturePart)
		company := f.ensure(t, testenv.FixtureManufacturer)
		name, err := f.run.Name("image-mpn")
		r.NoError(err)
		p, err := f.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{Part: part.ID, Manufacturer: company.ID, MPN: &name})
		r.NoError(err)
		return p.PK
	case "stock.stockitem":
		part := f.ensure(t, testenv.FixturePart)
		location := f.ensure(t, testenv.FixtureLocation)
		s, err := f.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 1})
		r.NoError(err)
		return s.PK
	case "order.purchaseorder":
		supplier := f.ensure(t, testenv.FixtureSupplier)
		name, err := f.run.Name("image-order")
		r.NoError(err)
		p, err := f.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, SupplierReference: &name})
		r.NoError(err)
		return p.PK
	default:
		t.Fatalf("unsupported fixture model %s", model)
		return 0
	}
}

// Every model owns its run, credentials, client and records. This matrix also
// isolates no-role, view-only and change permissions for each domain rather
// than relying on the previous investigation's part-only role evidence.
func runNoteImageClientCases(t *testing.T, shared *testenv.SharedInvenTree) {
	t.Helper()
	for _, model := range []string{"part.part", "company.company", "company.supplierpart", "company.manufacturerpart", "stock.stockitem", "order.purchaseorder"} {
		t.Run(model, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			f := newClientMethodFixture(t, shared)
			parent := noteImageParent(t, ctx, f, model)
			name, err := f.run.Name("client-image")
			r.NoError(err)
			note, err := f.client.CreateNote(ctx, inventree.NoteCreate{ModelType: model, ModelID: parent, Title: name, Content: "<p>original</p>"})
			r.NoError(err)
			im, err := f.client.UploadNoteImage(ctx, note.PK, inventree.NoteImageUpload{Filename: name + ".png", ContentType: "image/png", Content: tinyPNG()})
			r.NoError(err)
			r.Equal(note.PK, im.Note)
			r.NotNil(im.User)
			page, err := f.client.ListNoteImagesPage(ctx, inventree.NoteImageQuery{NoteID: note.PK, Limit: 100})
			r.NoError(err)
			r.Equal([]inventree.NoteImage{im}, page.Results)
			data, err := f.client.DownloadNoteImage(ctx, note.PK, im.PK, 1024)
			r.NoError(err)
			r.Equal(tinyPNG(), data.Content)
			_, err = f.client.UpdateNote(ctx, note.PK, inventree.PatchFields{"content": inventree.Set(`<p>original</p><img src="` + im.Image + `">`)})
			r.NoError(err)
			restricted := newNonStaffClient(t, ctx, f, "image-role-user", "integration-image-role-password-9284")
			_, err = restricted.ListNoteImagesPage(ctx, inventree.NoteImageQuery{NoteID: note.PK, Limit: 100})
			r.Error(err)
			_, err = restricted.DownloadNoteImage(ctx, note.PK, im.PK, 1024)
			r.Error(err)
			_, err = restricted.UploadNoteImage(ctx, note.PK, inventree.NoteImageUpload{Filename: name + ".png", ContentType: "image/png", Content: tinyPNG()})
			r.Error(err)
			status, raw := discoveredMedia(t, ctx, restricted, im.Image)
			r.Equal(http.StatusOK, status)
			r.Equal(tinyPNG(), raw, "known media access does not imply scoped method authorization")
			role := "purchase_order"
			if model == "part.part" {
				role = "part"
			}
			if model == "stock.stockitem" {
				role = "stock"
			}
			ruleID := grantNoteImageViewRole(t, ctx, f, restricted, role)
			page, err = restricted.ListNoteImagesPage(ctx, inventree.NoteImageQuery{NoteID: note.PK, Limit: 100})
			r.NoError(err)
			r.Len(page.Results, 1)
			data, err = restricted.DownloadNoteImage(ctx, note.PK, im.PK, 1024)
			r.NoError(err)
			r.Equal(tinyPNG(), data.Content)
			_, err = restricted.UploadNoteImage(ctx, note.PK, inventree.NoteImageUpload{Filename: name + ".png", ContentType: "image/png", Content: tinyPNG()})
			var apiErr *inventree.APIError
			r.ErrorAs(err, &apiErr)
			r.Equal(http.StatusForbidden, apiErr.StatusCode)
			r.NoError(f.client.Patch(ctx, fmt.Sprintf("/api/user/ruleset/%d/", ruleID), inventree.PatchFields{"can_change": inventree.Set(true)}, nil))
			second, err := restricted.UploadNoteImage(ctx, note.PK, inventree.NoteImageUpload{Filename: name + ".png", ContentType: "image/png", Content: alternateTinyPNG()})
			r.NoError(err)
			r.NotEqual(im.PK, second.PK)
			r.NotEqual(im.Image, second.Image)
			_, err = restricted.UpdateNote(ctx, note.PK, inventree.PatchFields{"content": inventree.Set(`<img src="` + im.Image + `"><img src="` + second.Image + `">`)})
			r.NoError(err)
			page, err = f.client.ListNoteImagesPage(ctx, inventree.NoteImageQuery{NoteID: note.PK, Limit: 1, Offset: 1})
			r.NoError(err)
			r.Equal(2, page.Count)
			r.Len(page.Results, 1)
			r.Equal(second.PK, page.Results[0].PK)
			r.NoError(f.client.DeleteNote(ctx, note.PK))
			_, err = f.client.DownloadNoteImage(ctx, note.PK, im.PK, 1024)
			r.Error(err)
			status, _ = discoveredMedia(t, ctx, f.client, im.Image)
			r.Equal(http.StatusNotFound, status)
		})
	}
}

func grantNoteImageViewRole(t *testing.T, ctx context.Context, f clientMethodFixture, c *inventree.Client, role string) int {
	t.Helper()
	r := require.New(t)
	var me struct {
		PK int `json:"pk"`
	}
	req, err := c.NewRequest(ctx, http.MethodGet, "/api/user/me/", nil, nil)
	r.NoError(err)
	r.NoError(c.DoJSON(req, &me))
	name, err := f.run.Name("image-role")
	r.NoError(err)
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
	id := 0
	for _, rule := range rules {
		if rule.Group == group.PK && rule.Name == role {
			id = rule.PK
		}
	}
	r.Positive(id)
	r.NoError(f.client.Patch(ctx, fmt.Sprintf("/api/user/ruleset/%d/", id), inventree.PatchFields{"can_view": inventree.Set(true), "can_add": inventree.Set(false), "can_change": inventree.Set(false), "can_delete": inventree.Set(false)}, nil))
	r.NoError(f.client.Patch(ctx, fmt.Sprintf("/api/user/%d/", me.PK), inventree.PatchFields{"group_ids": inventree.Set([]int{group.PK})}, nil))
	return id
}
