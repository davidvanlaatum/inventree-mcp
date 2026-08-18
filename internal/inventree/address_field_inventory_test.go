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

func TestAddressFieldInventoryClassifiesPinnedSerializerExactly(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	r.NoError(err)

	serializer, ok := openapi.Components.Schemas["Address"]
	r.True(ok)
	a.Len(inventree.AddressFieldInventory, len(serializer.Properties), "inventory must not contain stale or extra fields")
	a.Equal(serializer.Properties, selectAddressSchemaProperties(serializer.Properties, inventree.AddressFieldInventory), "inventory must classify every and only pinned serializer field")

	allowed := map[inventree.AddressFieldClass]bool{inventree.AddressFieldExposed: true, inventree.AddressFieldExcluded: true}
	exposed := map[string]bool{}
	for field, class := range inventree.AddressFieldInventory {
		a.True(allowed[class], "field %s has unknown classification %q", field, class)
		if class == inventree.AddressFieldExposed {
			exposed[field] = true
		}
	}
	a.Equal(exposed, addressModelFields(inventree.Address{}), "exact address model must expose every and only fields classified as exposed")
}

func TestAddressPreservesIdentityAndOmitsUnapprovedData(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var address inventree.Address
	r.NoError(json.Unmarshal([]byte(`{"pk":5,"company":9,"title":"Head Office","primary":true,"line1":"1 Test Street","line2":"","postal_code":"5000","postal_city":"Adelaide","province":"SA","country":"AU","shipping_notes":"Loading dock B","internal_shipping_notes":"call ahead","link":""}`), &address))
	a.Equal(5, address.PK)
	a.Equal(9, address.Company)
	a.Equal("Head Office", address.Title)
	a.True(address.Primary)
	a.Equal("Adelaide", address.PostalCity)
	a.Equal("SA", address.Province)
	a.Equal("AU", address.Country)
	a.Equal("Loading dock B", address.ShippingNotes)
	a.Equal("call ahead", address.InternalShippingNotes)
	assertAddressJSONOmits(t, address, "line1", "line2", "postal_code")
}

func addressModelFields(model any) map[string]bool {
	fields := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(model)) {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
	return fields
}

func selectAddressSchemaProperties(fields map[string]schema.SchemaRef, inventory map[string]inventree.AddressFieldClass) map[string]schema.SchemaRef {
	selected := make(map[string]schema.SchemaRef, len(inventory))
	for field := range inventory {
		if property, ok := fields[field]; ok {
			selected[field] = property
		}
	}
	return selected
}

func assertAddressJSONOmits(t *testing.T, value any, fields ...string) {
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
