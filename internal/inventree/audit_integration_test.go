//go:build !no_integration_tests

package inventree_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testGenericAuditHistory(t *testing.T, shared *testenv.SharedInvenTree) {
	models := []string{"part.part", "company.company", "company.supplierpart", "company.manufacturerpart", "stock.stockitem", "order.purchaseorder"}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			a := assert.New(t)
			fixture := newClientMethodFixture(t, shared)
			value, err := fixture.run.Name("audit-updated")
			r.NoError(err)
			var id int
			field := "description"
			switch model {
			case "part.part":
				id = fixture.ensure(t, testenv.FixturePart).ID
				_, err = fixture.client.UpdatePart(ctx, id, inventree.PatchFields{field: inventree.Set(value)})
			case "company.company":
				id = fixture.ensure(t, testenv.FixtureSupplier).ID
				_, err = fixture.client.UpdateCompany(ctx, id, inventree.PatchFields{field: inventree.Set(value)})
			case "company.supplierpart":
				id = fixture.ensure(t, testenv.FixtureSupplierPart).ID
				field = "SKU"
				_, err = fixture.client.UpdateSupplierPart(ctx, id, inventree.PatchFields{field: inventree.Set(value)})
			case "company.manufacturerpart":
				part := fixture.ensure(t, testenv.FixturePart)
				manufacturer := fixture.ensure(t, testenv.FixtureManufacturer)
				record, createErr := fixture.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{Part: part.ID, Manufacturer: manufacturer.ID, MPN: &value})
				r.NoError(createErr)
				id = record.PK
				field = "MPN"
				_, err = fixture.client.UpdateManufacturerPart(ctx, id, inventree.PatchFields{field: inventree.Set(value + "-changed")})
			case "stock.stockitem":
				part := fixture.ensure(t, testenv.FixturePart)
				location := fixture.ensure(t, testenv.FixtureLocation)
				record, createErr := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 2})
				r.NoError(createErr)
				id = record.PK
				field = "packaging"
				_, err = fixture.client.UpdateStockItem(ctx, id, inventree.PatchFields{field: inventree.Set(value)})
			case "order.purchaseorder":
				id = newAuditPurchaseOrder(t, ctx, fixture)
				_, err = fixture.client.UpdatePurchaseOrder(ctx, id, inventree.PatchFields{field: inventree.Set(value)})
			}
			r.NoError(err)
			scope := inventree.AuditScope{ModelType: model, ModelID: id}
			page, err := fixture.client.SearchAuditEntriesPage(ctx, inventree.AuditQuery{AuditScope: scope, Limit: 100})
			r.NoError(err)
			r.NotEmpty(page.Results)
			var changed inventree.AuditEntry
			for _, entry := range page.Results {
				a.Equal(model, entry.ModelType)
				a.Equal(id, entry.ModelID)
				if _, ok := entry.Changes[field]; ok && entry.Action == 1 {
					changed = entry
				}
			}
			r.Positive(changed.PK, "an actual field update must produce a readable entry for %s", model)
			exact, err := fixture.client.GetAuditEntry(ctx, inventree.AuditEntryQuery{AuditScope: scope, ID: changed.PK})
			r.NoError(err)
			a.Equal(changed, exact)
			encoded, err := json.Marshal(exact)
			r.NoError(err)
			for _, key := range []string{"actor_detail", "actor_username", "remote_addr", "remote_port", "changes_detail", "object_repr", "additional_data", "@example.test"} {
				a.NotContains(string(encoded), key)
			}
			_, err = fixture.client.GetAuditEntry(ctx, inventree.AuditEntryQuery{AuditScope: inventree.AuditScope{ModelType: model, ModelID: id + 1000000}, ID: changed.PK})
			var apiErr *inventree.APIError
			r.ErrorAs(err, &apiErr)
			a.Equal(http.StatusNotFound, apiErr.StatusCode)
			restricted := newNonStaffClient(t, ctx, fixture, "audit-no-permissions", "Audit-Noperms-Integration-1!")
			hidden, err := restricted.SearchAuditEntriesPage(ctx, inventree.AuditQuery{AuditScope: scope, Limit: 100})
			r.NoError(err)
			a.Empty(hidden.Results)
			_, err = restricted.GetAuditEntry(ctx, inventree.AuditEntryQuery{AuditScope: scope, ID: changed.PK})
			r.ErrorAs(err, &apiErr)
			a.Equal(http.StatusNotFound, apiErr.StatusCode)
		})
	}
	t.Run("child_scope_stays_allowlisted", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		r := require.New(t)
		a := assert.New(t)
		fixture := newClientMethodFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		location := fixture.ensure(t, testenv.FixtureLocation)
		stock, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 1})
		r.NoError(err)
		own := inventree.AuditScope{ModelType: "part.part", ModelID: part.ID}
		page, err := fixture.client.SearchAuditEntriesPage(ctx, inventree.AuditQuery{AuditScope: own, Limit: 100})
		r.NoError(err)
		r.NotEmpty(page.Results)
		for _, entry := range page.Results {
			a.Equal("part.part", entry.ModelType)
			a.Equal(part.ID, entry.ModelID)
		}
		own.IncludeChildren = true
		children, err := fixture.client.SearchAuditEntriesPage(ctx, inventree.AuditQuery{AuditScope: own, Limit: 100})
		r.NoError(err)
		found := false
		for _, entry := range children.Results {
			if entry.ModelType == "stock.stockitem" && entry.ModelID == stock.PK {
				found = true
				_, err = fixture.client.GetAuditEntry(ctx, inventree.AuditEntryQuery{AuditScope: own, ID: entry.PK})
				r.NoError(err)
			}
		}
		a.True(found, "explicit child scope must include the audited stock child")
		orderID := newAuditPurchaseOrder(t, ctx, fixture)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		_, err = fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: orderID, SupplierPart: supplierPart.ID, Quantity: 1})
		r.NoError(err)
		poHistory, err := fixture.client.SearchAuditEntriesPage(ctx, inventree.AuditQuery{AuditScope: inventree.AuditScope{ModelType: "order.purchaseorder", ModelID: orderID, IncludeChildren: true}, Limit: 100})
		r.NoError(err)
		r.NotEmpty(poHistory.Results)
		for _, entry := range poHistory.Results {
			a.Equal("order.purchaseorder", entry.ModelType, "purchase-order lines remain outside the six-type allowlist")
		}
	})
}

func newAuditPurchaseOrder(t *testing.T, ctx context.Context, fixture clientMethodFixture) int {
	t.Helper()
	r := require.New(t)
	supplier := fixture.ensure(t, testenv.FixtureSupplier)
	reference, err := fixture.run.Name("audit-po")
	r.NoError(err)
	order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, SupplierReference: &reference})
	r.NoError(err)
	r.Equal(reference, order.SupplierReference)
	r.NoError(fixture.run.RequireOwnedName(order.SupplierReference))
	return order.PK
}
