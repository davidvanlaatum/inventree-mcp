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

func TestSourcingPartFieldInventoriesClassifyPinnedSerializersExactly(t *testing.T) {
	t.Parallel()
	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	require.NoError(t, err)

	tests := []struct {
		name      string
		schema    string
		inventory map[string]inventree.SourcingPartFieldClass
		model     any
	}{
		{name: "supplier", schema: "SupplierPart", inventory: inventree.SupplierPartFieldInventory, model: inventree.SupplierPartDetail{}},
		{name: "manufacturer", schema: "ManufacturerPart", inventory: inventree.ManufacturerPartFieldInventory, model: inventree.ManufacturerPartDetail{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			serializer, ok := openapi.Components.Schemas[tc.schema]
			r.True(ok)
			a.Len(tc.inventory, len(serializer.Properties), "inventory must not contain stale or extra fields")
			a.Equal(serializer.Properties, selectSourcingSchemaProperties(serializer.Properties, tc.inventory), "inventory must classify every and only pinned serializer field")

			allowed := map[inventree.SourcingPartFieldClass]bool{
				inventree.SourcingPartFieldExposed: true, inventree.SourcingPartFieldSeparateLookup: true,
				inventree.SourcingPartFieldDeferred: true, inventree.SourcingPartFieldWriteOnly: true,
				inventree.SourcingPartFieldExcluded: true,
			}
			exposed := map[string]bool{}
			for field, class := range tc.inventory {
				a.True(allowed[class], "field %s has unknown classification %q", field, class)
				if class == inventree.SourcingPartFieldExposed {
					exposed[field] = true
				}
			}
			a.Equal(exposed, sourcingModelFields(tc.model), "exact detail model must expose every and only fields classified as exposed")
		})
	}
}

func TestSourcingPartDetailsPreserveNullableFieldsAndOmitUnapprovedData(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var supplier inventree.SupplierPartDetail
	r.NoError(json.Unmarshal([]byte(`{"pk":40,"part":10,"supplier":30,"SKU":"SKU-1","pretty_name":null,"available":0,"availability_updated":null,"in_stock":null,"on_order":2.5,"MPN":null,"note":"short","notes":null,"updated":null,"barcode_hash":"secret","supplier_detail":{"pk":30},"tags":["deferred"],"price_breaks":[],"parameters":[]}`), &supplier))
	a.Zero(supplier.Available)
	a.Nil(supplier.InStock)
	r.NotNil(supplier.OnOrder)
	a.Equal(2.5, *supplier.OnOrder)
	a.Nil(supplier.MPN)
	a.Equal("short", *supplier.Note)
	a.Nil(supplier.Notes)
	assertSourcingJSONOmits(t, supplier, "barcode_hash", "supplier_detail", "tags", "price_breaks", "parameters")

	var manufacturer inventree.ManufacturerPartDetail
	r.NoError(json.Unmarshal([]byte(`{"pk":50,"part":10,"manufacturer":31,"pretty_name":null,"MPN":null,"description":null,"link":null,"notes":"markdown","barcode_hash":"secret","manufacturer_detail":{"pk":31},"parameters":[]}`), &manufacturer))
	a.Nil(manufacturer.MPN)
	a.Equal("markdown", *manufacturer.Notes)
	assertSourcingJSONOmits(t, manufacturer, "barcode_hash", "manufacturer_detail", "parameters")
}

func sourcingModelFields(model any) map[string]bool {
	fields := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(model)) {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && name != "web_url" && name != "parent_web_url" {
			fields[name] = true
		}
	}
	return fields
}

func selectSourcingSchemaProperties(fields map[string]schema.SchemaRef, inventory map[string]inventree.SourcingPartFieldClass) map[string]schema.SchemaRef {
	selected := make(map[string]schema.SchemaRef, len(inventory))
	for field := range inventory {
		if property, ok := fields[field]; ok {
			selected[field] = property
		}
	}
	return selected
}

func assertSourcingJSONOmits(t *testing.T, value any, fields ...string) {
	t.Helper()
	r := require.New(t)
	a := assert.New(t)
	encoded, err := json.Marshal(value)
	r.NoError(err)
	var keys map[string]any
	r.NoError(json.Unmarshal(encoded, &keys))
	for _, field := range fields {
		a.NotContains(keys, field)
	}
}
