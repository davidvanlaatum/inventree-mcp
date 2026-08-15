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

func TestPartFieldInventoryClassifiesPinnedSerializerExactly(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	r.NoError(err)
	partSchema, ok := openapi.Components.Schemas["Part"]
	r.True(ok)

	a.Len(inventree.PartFieldInventory, len(partSchema.Properties), "inventory must not contain stale or extra fields")
	a.Equal(partSchema.Properties, selectSchemaProperties(partSchema.Properties, inventree.PartFieldInventory), "inventory must classify every and only pinned Part field")
	allowedClasses := map[inventree.PartFieldClass]bool{
		inventree.PartFieldExposed:        true,
		inventree.PartFieldSeparateLookup: true,
		inventree.PartFieldDeferred:       true,
		inventree.PartFieldWriteOnly:      true,
		inventree.PartFieldExcluded:       true,
	}
	for field, class := range inventree.PartFieldInventory {
		a.True(allowedClasses[class], "field %s has unknown classification %q", field, class)
	}

	exposed := map[string]bool{}
	for field, class := range inventree.PartFieldInventory {
		if class == inventree.PartFieldExposed {
			exposed[field] = true
		}
	}
	modeled := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(inventree.PartDetail{})) {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && name != "web_url" && name != "parent_web_url" {
			modeled[name] = true
		}
	}
	a.Equal(exposed, modeled, "exact detail model must expose every and only inventory field classified as exposed")
}

func TestPartDetailPreservesNullableScalarsAndOmitsUnapprovedFields(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	raw := []byte(`{"pk":10,"name":"resistor","IPN":"R-10K","notes":null,"pricing_min":null,"pricing_max":"2.500000","in_stock":null,"allocated_to_build_orders":3.5,"creation_user":7,"barcode_hash":"secret","category_detail":{"pk":20},"tags":["deferred"]}`)
	var detail inventree.PartDetail
	r.NoError(json.Unmarshal(raw, &detail))
	for field, expected := range map[string]inventree.PartFieldClass{
		"pk":              inventree.PartFieldExposed,
		"notes":           inventree.PartFieldExposed,
		"pricing_min":     inventree.PartFieldExposed,
		"barcode_hash":    inventree.PartFieldExcluded,
		"category_detail": inventree.PartFieldSeparateLookup,
		"tags":            inventree.PartFieldDeferred,
	} {
		a.Equal(expected, inventree.PartFieldInventory[field], "representative raw field %s", field)
	}
	a.Equal(10, detail.PK)
	a.Equal("R-10K", detail.IPN)
	a.Nil(detail.Notes)
	a.Nil(detail.PricingMin)
	r.NotNil(detail.PricingMax)
	a.Equal(inventree.DecimalString("2.500000"), *detail.PricingMax)
	a.Nil(detail.InStock)
	r.NotNil(detail.AllocatedToBuildOrders)
	a.Equal(3.5, *detail.AllocatedToBuildOrders)

	encoded, err := json.Marshal(detail)
	r.NoError(err)
	var keys map[string]any
	r.NoError(json.Unmarshal(encoded, &keys))
	a.NotContains(keys, "barcode_hash")
	a.NotContains(keys, "category_detail")
	a.NotContains(keys, "tags")
}

func selectSchemaProperties(schemaFields map[string]schema.SchemaRef, inventory map[string]inventree.PartFieldClass) map[string]schema.SchemaRef {
	selected := make(map[string]schema.SchemaRef, len(inventory))
	for field := range inventory {
		if property, ok := schemaFields[field]; ok {
			selected[field] = property
		}
	}
	return selected
}
