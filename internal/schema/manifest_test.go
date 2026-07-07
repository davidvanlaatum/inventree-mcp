package schema_test

import (
	"strings"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/docs"
	"github.com/davidvanlaatum/inventree-mcp/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndpointManifestMatchesOpenAPISchema(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	data := docs.APISchemaYAML()
	openapi, err := schema.ParseOpenAPI(data)
	r.NoError(err)
	manifest, err := schema.ParseManifest(docs.EndpointManifestYAML())
	r.NoError(err)

	r.NoError(manifest.Validate(openapi, schema.SHA256Hex(data)))
}

func TestSchemaProvenanceDocumentsCurrentDigest(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	data := docs.APISchemaYAML()
	openapi, err := schema.ParseOpenAPI(data)
	r.NoError(err)
	schemaDocs := docs.APISchemaMarkdown()

	a.Contains(schemaDocs, "SHA256: `"+schema.SHA256Hex(data)+"`")
	a.Contains(schemaDocs, "- OpenAPI: `"+openapi.OpenAPI+"`")
	a.Contains(schemaDocs, "- API version: `"+openapi.Info.Version+"`")
}

func TestManifestBlocksDeferredFileSurfaces(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	openapi, err := schema.ParseOpenAPI(docs.APISchemaYAML())
	r.NoError(err)
	manifest, err := schema.ParseManifest(docs.EndpointManifestYAML())
	r.NoError(err)

	for _, path := range manifest.ForbiddenPaths {
		_, inSchema := openapi.Paths[path]
		a.True(inSchema, "forbidden path %s should be schema-known so the guard proves the manifest excludes it intentionally", path)
	}

	rendered := renderManifestEndpointPaths(manifest)
	for _, path := range manifest.ForbiddenPaths {
		a.NotContains(rendered, path)
	}
}

func TestManifestRequiresStrictSchemaContracts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	data := docs.APISchemaYAML()
	openapi, err := schema.ParseOpenAPI(data)
	r.NoError(err)
	manifest, err := schema.ParseManifest(docs.EndpointManifestYAML())
	r.NoError(err)

	requestCase := *manifest
	requestCase.Endpoints = append([]schema.Endpoint(nil), manifest.Endpoints...)
	for i := range requestCase.Endpoints {
		if requestCase.Endpoints[i].ID == "create_part" {
			requestCase.Endpoints[i].RequestSchema = ""
			break
		}
	}
	a.ErrorContains(requestCase.Validate(openapi, schema.SHA256Hex(data)), "request_schema is required")

	responseCase := *manifest
	responseCase.Endpoints = append([]schema.Endpoint(nil), manifest.Endpoints...)
	for i := range responseCase.Endpoints {
		if responseCase.Endpoints[i].ID == "search_parts" {
			responseCase.Endpoints[i].ResponseSchema = ""
			break
		}
	}
	a.ErrorContains(responseCase.Validate(openapi, schema.SHA256Hex(data)), "response_schema is required")
}

func TestManifestRequiresKnownQueryParameters(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	data := docs.APISchemaYAML()
	openapi, err := schema.ParseOpenAPI(data)
	r.NoError(err)
	manifest, err := schema.ParseManifest(docs.EndpointManifestYAML())
	r.NoError(err)

	badQuery := *manifest
	badQuery.Endpoints = append([]schema.Endpoint(nil), manifest.Endpoints...)
	for i := range badQuery.Endpoints {
		if badQuery.Endpoints[i].ID == "search_suppliers" {
			badQuery.Endpoints[i].RequiredQuery = []string{"not_a_schema_filter"}
			break
		}
	}

	a.ErrorContains(badQuery.Validate(openapi, schema.SHA256Hex(data)), "required query parameter")
}

func TestManifestRejectsUnknownYAMLFields(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	data := append(docs.EndpointManifestYAML(), []byte("\nunknown_field: true\n")...)
	_, err := schema.ParseManifest(data)
	r.ErrorContains(err, "field unknown_field not found")
}

func renderManifestEndpointPaths(manifest *schema.Manifest) string {
	paths := make([]string, 0, len(manifest.Endpoints))
	for _, endpoint := range manifest.Endpoints {
		paths = append(paths, endpoint.Path)
	}
	return strings.Join(paths, "\n")
}
