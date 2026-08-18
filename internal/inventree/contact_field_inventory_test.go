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

func TestContactFieldInventoryClassifiesPinnedSerializerExactly(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	r.NoError(err)

	serializer, ok := openapi.Components.Schemas["Contact"]
	r.True(ok)
	a.Len(inventree.ContactFieldInventory, len(serializer.Properties), "inventory must not contain stale or extra fields")
	a.Equal(serializer.Properties, selectContactSchemaProperties(serializer.Properties, inventree.ContactFieldInventory), "inventory must classify every and only pinned serializer field")

	allowed := map[inventree.ContactFieldClass]bool{inventree.ContactFieldExposed: true, inventree.ContactFieldExcluded: true}
	exposed := map[string]bool{}
	for field, class := range inventree.ContactFieldInventory {
		a.True(allowed[class], "field %s has unknown classification %q", field, class)
		if class == inventree.ContactFieldExposed {
			exposed[field] = true
		}
	}
	a.Equal(exposed, contactModelFields(inventree.Contact{}), "exact contact model must expose every and only fields classified as exposed")
}

func TestContactPreservesIdentityAndOmitsUnapprovedData(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var contact inventree.Contact
	r.NoError(json.Unmarshal([]byte(`{"pk":5,"company":9,"company_name":"Acme","name":"Jane Doe","phone":"555-0100","email":"jane@example.com","role":"Purchasing"}`), &contact))
	a.Equal(5, contact.PK)
	a.Equal(9, contact.Company)
	a.Equal("Acme", contact.CompanyName)
	a.Equal("Jane Doe", contact.Name)
	a.Equal("Purchasing", contact.Role)
	assertContactJSONOmits(t, contact, "phone", "email")
}

func contactModelFields(model any) map[string]bool {
	fields := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(model)) {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
	return fields
}

func selectContactSchemaProperties(fields map[string]schema.SchemaRef, inventory map[string]inventree.ContactFieldClass) map[string]schema.SchemaRef {
	selected := make(map[string]schema.SchemaRef, len(inventory))
	for field := range inventory {
		if property, ok := fields[field]; ok {
			selected[field] = property
		}
	}
	return selected
}

func assertContactJSONOmits(t *testing.T, value any, fields ...string) {
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
