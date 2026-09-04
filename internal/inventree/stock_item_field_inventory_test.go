package inventree_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/docs"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStockItemFieldInventoryClassifiesPinnedSerializerExactly(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	r.NoError(err)
	stockItemSchema, ok := openapi.Components.Schemas["StockItem"]
	r.True(ok)

	a.Len(inventree.StockItemFieldInventory, len(stockItemSchema.Properties), "inventory must not contain stale or extra fields")
	a.Equal(stockItemSchema.Properties, selectStockItemSchemaProperties(stockItemSchema.Properties, inventree.StockItemFieldInventory), "inventory must classify every and only pinned StockItem field")
	allowedClasses := map[inventree.StockItemFieldClass]bool{
		inventree.StockItemFieldExposed:        true,
		inventree.StockItemFieldSeparateLookup: true,
		inventree.StockItemFieldDeferred:       true,
		inventree.StockItemFieldWriteOnly:      true,
		inventree.StockItemFieldExcluded:       true,
	}
	for field, class := range inventree.StockItemFieldInventory {
		a.True(allowedClasses[class], "field %s has unknown classification %q", field, class)
	}

	exposed := map[string]bool{}
	for field, class := range inventree.StockItemFieldInventory {
		if class == inventree.StockItemFieldExposed {
			exposed[field] = true
		}
	}
	modeled := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(inventree.StockItemDetail{})) {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && name != "web_url" && name != "parent_web_url" && name != "has_barcode" {
			modeled[name] = true
		}
	}
	a.Equal(exposed, modeled, "exact detail model must expose every and only inventory field classified as exposed")
}

func TestStockItemDetailPreservesNullableScalarsAndOmitsUnapprovedFields(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	raw := []byte(`{"pk":50,"part":5,"quantity":2,"SKU":null,"MPN":"MPN-50","expired":true,"stale":false,"sales_order":null,"sales_order_reference":null,"location_path":[{"pk":40,"name":"Bin"}],"barcode_hash":"secret","location_detail":{"pk":40},"part_detail":{"pk":5},"supplier_part_detail":{"pk":9},"tags":["reference"],"tests":[{"pk":1}],"use_pack_size":true,"serial_numbers":"1-5"}`)
	var detail inventree.StockItemDetail
	r.NoError(json.Unmarshal(raw, &detail))
	for field, expected := range map[string]inventree.StockItemFieldClass{
		"pk":                   inventree.StockItemFieldExposed,
		"expired":              inventree.StockItemFieldExposed,
		"stale":                inventree.StockItemFieldExposed,
		"location_path":        inventree.StockItemFieldExposed,
		"barcode_hash":         inventree.StockItemFieldExcluded,
		"location_detail":      inventree.StockItemFieldSeparateLookup,
		"part_detail":          inventree.StockItemFieldSeparateLookup,
		"supplier_part_detail": inventree.StockItemFieldSeparateLookup,
		"tags":                 inventree.StockItemFieldExposed,
		"tests":                inventree.StockItemFieldDeferred,
		"use_pack_size":        inventree.StockItemFieldWriteOnly,
		"serial_numbers":       inventree.StockItemFieldWriteOnly,
	} {
		a.Equal(expected, inventree.StockItemFieldInventory[field], "representative raw field %s", field)
	}
	a.Equal(50, detail.PK)
	a.Equal(5, detail.Part)
	a.Nil(detail.SKU)
	r.NotNil(detail.MPN)
	a.Equal("MPN-50", *detail.MPN)
	r.NotNil(detail.Expired)
	a.True(*detail.Expired)
	r.NotNil(detail.Stale)
	a.False(*detail.Stale)
	a.Nil(detail.SalesOrder)
	a.Nil(detail.SalesOrderReference)
	r.Len(detail.LocationPath, 1)
	a.Equal("Bin", detail.LocationPath[0].Name)
	a.Equal([]string{"reference"}, detail.Tags)

	encoded, err := json.Marshal(detail)
	r.NoError(err)
	var keys map[string]any
	r.NoError(json.Unmarshal(encoded, &keys))
	a.NotContains(keys, "barcode_hash")
	a.NotContains(keys, "location_detail")
	a.NotContains(keys, "part_detail")
	a.NotContains(keys, "supplier_part_detail")
	a.Contains(keys, "tags")
	a.NotContains(keys, "tests")
	a.NotContains(keys, "use_pack_size")
	a.NotContains(keys, "serial_numbers")
}

func TestStockItemDetailLocationPathHandlesNullOmittedEmptyAndPopulated(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	cases := []struct {
		name string
		raw  string
	}{
		{name: "omitted", raw: `{"pk":50,"part":5,"quantity":2}`},
		{name: "null", raw: `{"pk":50,"part":5,"quantity":2,"location_path":null}`},
		{name: "empty", raw: `{"pk":50,"part":5,"quantity":2,"location_path":[]}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)

			var detail inventree.StockItemDetail
			r.NoError(json.Unmarshal([]byte(tt.raw), &detail))
			a.Empty(detail.LocationPath)

			encoded, err := json.Marshal(detail)
			r.NoError(err)
			var keys map[string]any
			r.NoError(json.Unmarshal(encoded, &keys))
			a.NotContains(keys, "location_path")
		})
	}

	var populated inventree.StockItemDetail
	r.NoError(json.Unmarshal([]byte(`{"pk":50,"part":5,"quantity":2,"location_path":[{"pk":10,"name":"Warehouse"},{"pk":40,"name":"Bin"}]}`), &populated))
	r.Len(populated.LocationPath, 2)
	a.Equal("Warehouse", populated.LocationPath[0].Name)
	a.Equal("Bin", populated.LocationPath[1].Name)
	encoded, err := json.Marshal(populated)
	r.NoError(err)
	var keys map[string]any
	r.NoError(json.Unmarshal(encoded, &keys))
	a.Contains(keys, "location_path")
}

func selectStockItemSchemaProperties(schemaFields map[string]schema.SchemaRef, inventory map[string]inventree.StockItemFieldClass) map[string]schema.SchemaRef {
	selected := make(map[string]schema.SchemaRef, len(inventory))
	for field := range inventory {
		if property, ok := schemaFields[field]; ok {
			selected[field] = property
		}
	}
	return selected
}
