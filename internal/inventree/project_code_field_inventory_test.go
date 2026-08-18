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

func TestProjectCodeFieldInventoryClassifiesPinnedSerializerExactly(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	r.NoError(err)

	serializer, ok := openapi.Components.Schemas["ProjectCode"]
	r.True(ok)
	a.Len(inventree.ProjectCodeFieldInventory, len(serializer.Properties), "inventory must not contain stale or extra fields")
	a.Equal(serializer.Properties, selectProjectCodeSchemaProperties(serializer.Properties, inventree.ProjectCodeFieldInventory), "inventory must classify every and only pinned serializer field")

	allowed := map[inventree.ProjectCodeFieldClass]bool{inventree.ProjectCodeFieldExposed: true, inventree.ProjectCodeFieldDeferred: true}
	exposed := map[string]bool{}
	for field, class := range inventree.ProjectCodeFieldInventory {
		a.True(allowed[class], "field %s has unknown classification %q", field, class)
		if class == inventree.ProjectCodeFieldExposed {
			exposed[field] = true
		}
	}
	a.Equal(exposed, projectCodeModelFields(inventree.ProjectCode{}), "exact project-code model must expose every and only fields classified as exposed")
}

func TestProjectCodePreservesIdentityAndOmitsDeferredData(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var record inventree.ProjectCode
	r.NoError(json.Unmarshal([]byte(`{"pk":7,"code":"PRJ-001","description":"Widget rollout","active":true,"responsible":3,"responsible_detail":{"pk":3}}`), &record))
	a.Equal(7, record.PK)
	a.Equal("PRJ-001", record.Code)
	a.Equal("Widget rollout", record.Description)
	a.True(record.Active)
	assertProjectCodeJSONOmits(t, record, "responsible", "responsible_detail")
}

func projectCodeModelFields(model any) map[string]bool {
	fields := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(model)) {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
	return fields
}

func selectProjectCodeSchemaProperties(fields map[string]schema.SchemaRef, inventory map[string]inventree.ProjectCodeFieldClass) map[string]schema.SchemaRef {
	selected := make(map[string]schema.SchemaRef, len(inventory))
	for field := range inventory {
		if property, ok := fields[field]; ok {
			selected[field] = property
		}
	}
	return selected
}

func assertProjectCodeJSONOmits(t *testing.T, value any, fields ...string) {
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
