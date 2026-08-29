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

func TestCompanyFieldInventoryClassifiesPinnedSerializerExactly(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	r.NoError(err)

	serializer, ok := openapi.Components.Schemas["Company"]
	r.True(ok)
	a.Len(inventree.CompanyFieldInventory, len(serializer.Properties), "inventory must not contain stale or extra fields")
	a.Equal(serializer.Properties, selectCompanySchemaProperties(serializer.Properties, inventree.CompanyFieldInventory), "inventory must classify every and only pinned serializer field")

	allowed := map[inventree.CompanyFieldClass]bool{
		inventree.CompanyFieldExposed: true, inventree.CompanyFieldSeparateLookup: true,
		inventree.CompanyFieldDeferred: true, inventree.CompanyFieldWriteOnly: true,
		inventree.CompanyFieldExcluded: true,
	}
	exposed := map[string]bool{}
	for field, class := range inventree.CompanyFieldInventory {
		a.True(allowed[class], "field %s has unknown classification %q", field, class)
		if class == inventree.CompanyFieldExposed {
			exposed[field] = true
		}
	}
	a.Equal(exposed, companyModelFields(inventree.CompanyDetail{}), "exact detail model must expose every and only fields classified as exposed")
}

func TestCompanyDetailPreservesNullableFieldsAndOmitsUnapprovedData(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var company inventree.CompanyDetail
	r.NoError(json.Unmarshal([]byte(`{"pk":10,"name":"Acme","description":"","website":"","phone":"","email":null,"currency":"AUD","contact":"","link":"","image":null,"active":true,"is_customer":false,"is_manufacturer":false,"is_supplier":true,"notes":null,"parts_supplied":0,"parts_manufactured":0,"primary_address":null,"tax_id":"","parameters":[],"tags":[]}`), &company))
	a.Equal(10, company.PK)
	a.Nil(company.Email)
	a.Nil(company.Notes)
	a.Empty(company.Tags)
	assertCompanyJSONOmits(t, company, "primary_address", "parameters")

	var populated inventree.CompanyDetail
	r.NoError(json.Unmarshal([]byte(`{"pk":11,"name":"Acme","description":"","website":"","phone":"555-0100","email":"ap@example.com","currency":"AUD","contact":"Jane Doe","link":"","image":null,"active":true,"is_customer":true,"is_manufacturer":false,"is_supplier":false,"notes":"secret notes","parts_supplied":0,"parts_manufactured":0,"primary_address":{"pk":1},"tax_id":"ABN123","parameters":[{"pk":1}],"tags":["a"]}`), &populated))
	a.Equal("555-0100", populated.Phone)
	r.NotNil(populated.Email)
	a.Equal("ap@example.com", *populated.Email)
	a.Equal("Jane Doe", populated.Contact)
	a.Equal("ABN123", populated.TaxID)
	r.NotNil(populated.Notes)
	a.Equal("secret notes", *populated.Notes)
	a.Equal([]string{"a"}, populated.Tags)
	assertCompanyJSONOmits(t, populated, "primary_address", "parameters")
}

func companyModelFields(model any) map[string]bool {
	fields := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(model)) {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && name != "web_url" && name != "parent_web_url" {
			fields[name] = true
		}
	}
	return fields
}

func selectCompanySchemaProperties(fields map[string]schema.SchemaRef, inventory map[string]inventree.CompanyFieldClass) map[string]schema.SchemaRef {
	selected := make(map[string]schema.SchemaRef, len(inventory))
	for field := range inventory {
		if property, ok := fields[field]; ok {
			selected[field] = property
		}
	}
	return selected
}

func assertCompanyJSONOmits(t *testing.T, value any, fields ...string) {
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
