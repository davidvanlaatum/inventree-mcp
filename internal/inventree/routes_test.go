package inventree

import (
	"reflect"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/docs"
	"github.com/davidvanlaatum/inventree-mcp/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// excludedClientMethods are exported *Client methods that intentionally have
// no clientMethodRoutes entry: generic per-request helpers (not one per
// endpoint), credential-bearing bootstrap/OAuth calls issued against a
// throwaway client outside normal tool dispatch, and a pure delegator that
// issues no independent request of its own.
var excludedClientMethods = map[string]bool{
	"Patch":                    true,
	"Post":                     true,
	"NewRequest":               true,
	"DoJSON":                   true,
	"GetCurrentUser":           true,
	"CreateCurrentUserToken":   true,
	"ClearCompanyPrimaryImage": true,
}

// downloadFamilies are the request families whose ManifestID does not
// resolve to a real docs/endpoint-manifest.yaml entry, because they hit
// InvenTree's opaque signed /media/... content URLs rather than an
// OpenAPI-documented endpoint. See the comment on clientMethodRoutes.
var downloadFamilies = map[RequestFamily]bool{
	RequestFamilyAttachmentDownload: true,
	RequestFamilyImageDownload:      true,
	RequestFamilyDataOutputDownload: true,
}

// TestClientMethodRoutesCoverEveryIncludedExportedMethod is the drift test
// that keeps the closed route registry honest as Client grows: it fails the
// moment a new exported method is added without a registry entry, or an
// entry is left behind for a method that no longer exists.
func TestClientMethodRoutesCoverEveryIncludedExportedMethod(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	clientType := reflect.TypeOf(&Client{})
	actual := make(map[string]bool, clientType.NumMethod())
	for i := range clientType.NumMethod() {
		method := clientType.Method(i)
		if !method.IsExported() {
			continue
		}
		actual[method.Name] = true
	}

	for name := range actual {
		if excludedClientMethods[name] {
			a.NotContains(clientMethodRoutes, name, "%s is excluded and must not also have a registry entry", name)
			continue
		}
		a.Contains(clientMethodRoutes, name, "%s is missing a clientMethodRoutes entry", name)
	}

	for name := range clientMethodRoutes {
		a.True(actual[name], "clientMethodRoutes has a stale entry for %s, which is no longer an exported Client method", name)
		a.False(excludedClientMethods[name], "clientMethodRoutes has an entry for %s, which is on the excluded list", name)
	}

	for name := range excludedClientMethods {
		a.True(actual[name], "excludedClientMethods lists %s, which is not an exported Client method", name)
	}
}

// TestClientMethodRoutesHaveAClosedFamily fails if any entry carries a
// Family value outside the fixed five-member closed vocabulary.
func TestClientMethodRoutesHaveAClosedFamily(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	for name, route := range clientMethodRoutes {
		switch route.Family {
		case RequestFamilyJSONAPI, RequestFamilyMultipartAPI, RequestFamilyAttachmentDownload,
			RequestFamilyImageDownload, RequestFamilyDataOutputDownload:
		default:
			a.Fail("unclosed family", "%s has an unrecognized family %q", name, route.Family)
		}
		a.NotEmpty(route.Method, name)
		a.NotEmpty(route.Path, name)
		a.NotEmpty(route.ManifestID, name)
	}
}

// TestClientMethodRoutesMatchEndpointManifest cross-validates every registry
// entry outside the download families against the real, schema-validated
// docs/endpoint-manifest.yaml: the ManifestID must exist, and its recorded
// method/path must match exactly. Download-family entries are exempt (see
// downloadFamilies) because they reference opaque signed media URLs, not
// documented API operations.
func TestClientMethodRoutesMatchEndpointManifest(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	manifest, err := schema.ParseManifest(docs.EndpointManifestYAML())
	r.NoError(err)
	byID := make(map[string]schema.Endpoint, len(manifest.Endpoints))
	for _, endpoint := range manifest.Endpoints {
		byID[endpoint.ID] = endpoint
	}

	for name, route := range clientMethodRoutes {
		if downloadFamilies[route.Family] {
			continue
		}
		endpoint, ok := byID[route.ManifestID]
		if !a.True(ok, "%s references manifest id %q, which does not exist in docs/endpoint-manifest.yaml", name, route.ManifestID) {
			continue
		}
		a.Equal(endpoint.Method, route.Method, "%s: registry method disagrees with manifest entry %q", name, route.ManifestID)
		a.Equal(endpoint.Path, route.Path, "%s: registry path disagrees with manifest entry %q", name, route.ManifestID)
	}
}

// TestClientMethodRoutesDocumentsQueryDisambiguatedGroups reports (without
// failing) every (Method, Path, Family) group whose members disagree on
// ManifestID. This is expected for GET searches that share one list
// endpoint's path and differ only by query filter — e.g. SearchCompanies
// (all companies) vs SearchSuppliers/SearchManufacturers
// (is_supplier/is_manufacturer=true), or SearchPartParameters vs
// SearchTemplateParametersPage/SearchObjectParametersPage. resolveRoute
// cannot see query parameters (only method, path, and Content-Type), so it
// resolves these to an arbitrary member of the group rather than a
// fabricated or wrong family — a documented, accepted precision limit, not
// a correctness bug: the logged operation always names one of the real
// candidate endpoints for that exact URL, never an invented one. This test
// exists so a NEW ambiguous group introduced later is visible in test
// output rather than silently accepted.
func TestClientMethodRoutesDocumentsQueryDisambiguatedGroups(t *testing.T) {
	t.Parallel()

	type key struct {
		method, path string
		family       RequestFamily
	}
	ids := make(map[key]map[string]bool)
	for _, route := range clientMethodRoutes {
		if downloadFamilies[route.Family] {
			continue // resolved via explicit marker; path-sharing is expected, see clientMethodRoutes.
		}
		k := key{route.Method, route.Path, route.Family}
		if ids[k] == nil {
			ids[k] = map[string]bool{}
		}
		ids[k][route.ManifestID] = true
	}
	for k, distinctIDs := range ids {
		if len(distinctIDs) > 1 {
			names := make([]string, 0, len(distinctIDs))
			for id := range distinctIDs {
				names = append(names, id)
			}
			t.Logf("query-disambiguated group at %s %s (%s): resolveRoute picks arbitrarily among %v", k.method, k.path, k.family, names)
		}
	}
}
