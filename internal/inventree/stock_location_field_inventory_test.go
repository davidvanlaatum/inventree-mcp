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

func TestStockLocationFieldInventoriesClassifyPinnedSerializersExactly(t *testing.T) {
	t.Parallel()
	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	require.NoError(t, err)

	tests := []struct {
		name      string
		schema    string
		inventory map[string]inventree.StockLocationFieldClass
		model     any
	}{
		{name: "location", schema: "Location", inventory: inventree.StockLocationFieldInventory, model: inventree.StockLocation{}},
		{name: "location_type", schema: "StockLocationType", inventory: inventree.StockLocationTypeFieldInventory, model: inventree.StockLocationType{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			serializer, ok := openapi.Components.Schemas[tc.schema]
			r.True(ok)
			a.Len(tc.inventory, len(serializer.Properties), "inventory must not contain stale or extra fields")
			a.Equal(serializer.Properties, selectStockLocationSchemaProperties(serializer.Properties, tc.inventory), "inventory must classify every and only pinned serializer field")

			allowed := map[inventree.StockLocationFieldClass]bool{
				inventree.StockLocationFieldExposed: true, inventree.StockLocationFieldSeparateLookup: true,
				inventree.StockLocationFieldDeferred: true, inventree.StockLocationFieldWriteOnly: true,
				inventree.StockLocationFieldExcluded: true,
			}
			exposed := map[string]bool{}
			for field, class := range tc.inventory {
				a.True(allowed[class], "field %s has unknown classification %q", field, class)
				if class == inventree.StockLocationFieldExposed {
					exposed[field] = true
				}
			}
			a.Equal(exposed, stockLocationModelFields(tc.model), "exact detail model must expose every and only fields classified as exposed")
		})
	}
}

func TestStockLocationPreservesEffectiveIconAndOmitsExcludedFields(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var location inventree.StockLocation
	r.NoError(json.Unmarshal([]byte(`{"pk":1,"barcode_hash":"secret","name":"Bin 1","level":0,"description":"","parent":null,"pathstring":"Bin 1","path":[{"pk":1,"name":"Bin 1","icon":""}],"items":0,"sublocations":0,"owner":null,"icon":"ti:box:outline","custom_icon":null,"structural":false,"external":false,"location_type":2,"location_type_detail":{"pk":2,"name":"Bin","description":"","icon":"ti:box:outline","location_count":1},"tags":["reference"],"parameters":[]}`), &location))
	a.Equal("ti:box:outline", location.Icon)
	a.Nil(location.CustomIcon)
	r.NotNil(location.LocationTypeDetail)
	a.Equal("ti:box:outline", location.LocationTypeDetail.Icon)
	a.Equal([]string{"reference"}, location.Tags)
	assertStockLocationJSONOmits(t, location, "barcode_hash", "parameters")

	encoded, err := json.Marshal(location)
	r.NoError(err)
	var keys map[string]any
	r.NoError(json.Unmarshal(encoded, &keys))
	a.Equal("ti:box:outline", keys["icon"], "icon must project the effective InvenTree-computed icon")
	a.Contains(keys, "tags")
}

func stockLocationModelFields(model any) map[string]bool {
	fields := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(model)) {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && name != "web_url" && name != "parent_web_url" {
			fields[name] = true
		}
	}
	return fields
}

func selectStockLocationSchemaProperties(fields map[string]schema.SchemaRef, inventory map[string]inventree.StockLocationFieldClass) map[string]schema.SchemaRef {
	selected := make(map[string]schema.SchemaRef, len(inventory))
	for field := range inventory {
		if property, ok := fields[field]; ok {
			selected[field] = property
		}
	}
	return selected
}

func assertStockLocationJSONOmits(t *testing.T, value any, fields ...string) {
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
