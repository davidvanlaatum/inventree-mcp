//go:build !no_integration_tests

package inventree_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The suite owns only the environment. Every model subtest owns its account,
// run prefix, parent, and note records.
func runNoteClientCases(t *testing.T, shared *testenv.SharedInvenTree) {
	t.Helper()
	for _, modelType := range []string{"part.part", "company.company", "company.supplierpart", "company.manufacturerpart", "stock.stockitem", "order.purchaseorder"} {
		t.Run(modelType, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			f := newClientMethodFixture(t, shared)
			id := 0
			switch modelType {
			case "part.part":
				id = f.ensure(t, testenv.FixturePart).ID
			case "company.company":
				id = f.ensure(t, testenv.FixtureSupplier).ID
			case "company.supplierpart":
				id = f.ensure(t, testenv.FixtureSupplierPart).ID
			case "company.manufacturerpart":
				part := f.ensure(t, testenv.FixturePart)
				manufacturer := f.ensure(t, testenv.FixtureManufacturer)
				mpn, err := f.run.Name("notes-mpn")
				r.NoError(err)
				created, err := f.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{Part: part.ID, Manufacturer: manufacturer.ID, MPN: &mpn})
				r.NoError(err)
				id = created.PK
			case "stock.stockitem":
				part := f.ensure(t, testenv.FixturePart)
				location := f.ensure(t, testenv.FixtureLocation)
				stock, err := f.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 1})
				r.NoError(err)
				id = stock.PK
			case "order.purchaseorder":
				supplier := f.ensure(t, testenv.FixtureSupplier)
				reference, err := f.run.Name("notes-order")
				r.NoError(err)
				order, err := f.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, SupplierReference: &reference})
				r.NoError(err)
				id = order.PK
			}
			title, err := f.run.Name("first-note")
			r.NoError(err)
			first, err := f.client.CreateNote(ctx, inventree.NoteCreate{ModelType: modelType, ModelID: id, Title: title, Content: "**Markdown**"})
			r.NoError(err)
			r.Positive(first.PK)
			a.True(first.Primary)
			exact, err := f.client.GetNote(ctx, first.PK)
			r.NoError(err)
			a.Equal(first.PK, exact.PK)
			a.Equal("**Markdown**", exact.Content)
			secondTitle, err := f.run.Name("second-note")
			r.NoError(err)
			second, err := f.client.CreateNote(ctx, inventree.NoteCreate{ModelType: modelType, ModelID: id, Title: secondTitle, Content: "second", Primary: dvgoutils.Ptr(false)})
			r.NoError(err)
			a.False(second.Primary)
			promoted, err := f.client.UpdateNote(ctx, second.PK, inventree.PatchFields{"primary": inventree.Set(true), "content": inventree.Set(""), "description": inventree.Set("")})
			r.NoError(err)
			a.True(promoted.Primary)
			a.Empty(promoted.Content)
			exact, err = f.client.GetNote(ctx, first.PK)
			r.NoError(err)
			a.False(exact.Primary)
			a.Equal("**Markdown**", exact.Content)
			page, err := f.client.SearchNotesPage(ctx, inventree.NoteQuery{ModelType: modelType, ModelID: id, Limit: 100})
			r.NoError(err)
			a.Equal(2, page.Count)
			r.Len(page.Results, 2)
			filtered, err := f.client.SearchNotesPage(ctx, inventree.NoteQuery{ModelType: modelType, ModelID: id, Search: secondTitle, Limit: 10})
			r.NoError(err)
			r.Len(filtered.Results, 1)
			a.Equal(second.PK, filtered.Results[0].PK)
			r.NoError(f.client.DeleteNote(ctx, second.PK))
			_, err = f.client.GetNote(ctx, second.PK)
			r.Error(err)
			r.NoError(f.client.DeleteNote(ctx, first.PK))
		})
	}
	t.Run("permission_and_template_boundaries", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		f := newClientMethodFixture(t, shared)
		part := f.ensure(t, testenv.FixturePart)
		title, err := f.run.Name("restricted-note")
		r.NoError(err)
		note, err := f.client.CreateNote(ctx, inventree.NoteCreate{ModelType: "part.part", ModelID: part.ID, Title: title, Content: "private"})
		r.NoError(err)
		restricted := newNonStaffClient(t, ctx, f, "notes-unprivileged", "integration-notes-password-9284")
		_, err = restricted.GetNote(ctx, note.PK)
		r.Error(err)
		_, err = restricted.CreateNote(ctx, inventree.NoteCreate{ModelType: "part.part", ModelID: part.ID, Title: title})
		r.Error(err)
		_, err = restricted.UpdateNote(ctx, note.PK, inventree.PatchFields{"content": inventree.Set("blocked")})
		r.Error(err)
		r.Error(restricted.DeleteNote(ctx, note.PK))

		// Grant parent view only through the native group/ruleset API, then
		// distinguish upstream write denial from the client's readability gate.
		var me struct {
			PK int `json:"pk"`
		}
		req, err := restricted.NewRequest(ctx, http.MethodGet, "/api/user/me/", nil, nil)
		r.NoError(err)
		r.NoError(restricted.DoJSON(req, &me))
		groupName, err := f.run.Name("notes-readonly")
		r.NoError(err)
		var group struct {
			PK int `json:"pk"`
		}
		r.NoError(f.client.Post(ctx, "/api/user/group/", map[string]any{"name": groupName}, &group))
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
		r.NoError(f.client.Patch(ctx, fmt.Sprintf("/api/user/ruleset/%d/", ruleID), inventree.PatchFields{"can_view": inventree.Set(true), "can_add": inventree.Set(false), "can_change": inventree.Set(false), "can_delete": inventree.Set(false)}, nil))
		r.NoError(f.client.Patch(ctx, fmt.Sprintf("/api/user/%d/", me.PK), inventree.PatchFields{"group_ids": inventree.Set([]int{group.PK})}, nil))
		visible, err := restricted.GetNote(ctx, note.PK)
		r.NoError(err)
		r.Equal(note, visible)
		_, err = restricted.CreateNote(ctx, inventree.NoteCreate{ModelType: "part.part", ModelID: part.ID, Title: title + " denied"})
		r.Error(err)
		_, err = restricted.UpdateNote(ctx, note.PK, inventree.PatchFields{"content": inventree.Set("blocked")})
		r.Error(err)
		r.Error(restricted.DeleteNote(ctx, note.PK))
		unchanged, err := f.client.GetNote(ctx, note.PK)
		r.NoError(err)
		r.Equal(note, unchanged)
		page, err := f.client.SearchNotesPage(ctx, inventree.NoteQuery{ModelType: "part.part", ModelID: part.ID, Limit: 100})
		r.NoError(err)
		r.Equal(1, page.Count)
		var template inventree.Note
		r.NoError(f.client.Post(ctx, "/api/note/", map[string]any{"template": true, "title": title}, &template))
		_, err = f.client.GetNote(ctx, template.PK)
		r.ErrorIs(err, inventree.ErrNoteOutOfScope)
	})
}
