package inventree_test

import (
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/docs"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStockLocationDeleteReferenceInventoryMatchesPinnedLocationForeignKeys(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	r.NoError(err)
	expected := map[string][]string{
		"Build": {"take_from", "destination"}, "PatchedBuild": {"take_from", "destination"},
		"TransferOrder": {"take_from", "destination"}, "PatchedTransferOrder": {"take_from", "destination"},
		"PurchaseOrder": {"destination"}, "PatchedPurchaseOrder": {"destination"},
		"PurchaseOrderLineItem": {"destination"}, "PatchedPurchaseOrderLineItem": {"destination"}, "Part": {"default_location"},
		"PatchedPart": {"default_location"}, "Category": {"default_location"},
		"PatchedCategory": {"default_location"}, "StockItem": {"location"},
		"Location": {"parent"}, "PatchedLocation": {"parent"},
	}
	for component, fields := range expected {
		serializer, ok := openapi.Components.Schemas[component]
		r.True(ok, "schema component %s must remain present", component)
		for _, field := range fields {
			property, ok := serializer.Properties[field]
			r.True(ok, "%s.%s must remain a pinned stock-location reference", component, field)
			r.Equal("integer", property.Type, "%s.%s must remain an integer location FK", component, field)
		}
	}
}

func TestStockLocationDeleteReferenceInventoryIsWellFormed(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	for _, surface := range inventree.StockLocationDeleteReferenceInventory {
		assert.NotEmpty(t, surface.Name)
		assert.NotEmpty(t, surface.Endpoint)
		assert.NotEmpty(t, surface.Filter)
		assert.NotEmpty(t, surface.Bound)
		assert.NotEmpty(t, surface.Permission)
		assert.NotEmpty(t, surface.Blocker)
		assert.False(t, names[surface.Name], "duplicate reference surface name %q", surface.Name)
		names[surface.Name] = true
	}
	assert.Equal(t, map[string]bool{
		"direct_stock_items": true, "direct_child_locations": true,
		"part_default_locations": true, "category_default_locations": true,
		"purchase_order_destinations": true, "purchase_order_line_destinations": true,
		"generic_location_parameters": true, "build_locations": true,
		"transfer_order_locations": true,
	}, names)
}
