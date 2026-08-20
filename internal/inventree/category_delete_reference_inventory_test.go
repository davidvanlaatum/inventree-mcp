package inventree_test

import (
	"strings"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/docs"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCategorySchemaForeignKeysMatchPinnedInventory scans every OpenAPI
// component schema for an integer "category" property -- InvenTree's
// consistent naming for a PartCategory foreign key -- and asserts the
// resulting set of schemas exactly matches inventree.CategorySchemaForeignKeys.
// A future InvenTree schema bump that adds a new "category" foreign key on an
// unrelated model fails this test until delete_part_category's reference
// inventory explicitly classifies it.
func TestCategorySchemaForeignKeysMatchPinnedInventory(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	r.NoError(err)

	found := map[string]bool{}
	for name, s := range openapi.Components.Schemas {
		if strings.HasPrefix(name, "Patched") {
			continue
		}
		prop, ok := s.Properties["category"]
		if ok && prop.Type == "integer" {
			found[name] = true
		}
	}

	expected := map[string]bool{}
	for _, name := range inventree.CategorySchemaForeignKeys {
		expected[name] = true
	}
	a.Equal(expected, found, "a new or removed \"category\" foreign key must be reflected in inventree.CategorySchemaForeignKeys")
}

// TestCategoryDeleteReferenceInventoryIsWellFormed pins the shape of every
// reference surface delete_part_category's preflight relies on so a
// half-classified entry (missing endpoint, filter, or blocker field name)
// cannot silently ship.
func TestCategoryDeleteReferenceInventoryIsWellFormed(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	names := map[string]bool{}
	for _, surface := range inventree.CategoryDeleteReferenceInventory {
		a.NotEmpty(surface.Name)
		a.NotEmpty(surface.Endpoint)
		a.NotEmpty(surface.Filter)
		a.NotEmpty(surface.Bound)
		a.NotEmpty(surface.Permission)
		a.NotEmpty(surface.Blocker)
		a.False(names[surface.Name], "duplicate reference surface name %q", surface.Name)
		names[surface.Name] = true
	}
	a.Equal(map[string]bool{
		"direct_parts": true, "direct_child_categories": true,
		"category_parameter_template_links": true, "generic_parameter_values": true,
	}, names)
}
