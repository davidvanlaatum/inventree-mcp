package weblinks

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolverRouteMatrixPreservesBasePrefix(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	resolver, err := New("https://inventory.example.test/inventree/", "INVENTREE_WEB_URL", true)
	r.NoError(err)

	want := map[Kind]string{
		Part:             "https://inventory.example.test/inventree/part/11/",
		PartCategory:     "https://inventory.example.test/inventree/part/category/12/",
		Company:          "https://inventory.example.test/inventree/company/13/",
		Supplier:         "https://inventory.example.test/inventree/purchasing/supplier/14/",
		Manufacturer:     "https://inventory.example.test/inventree/purchasing/manufacturer/15/",
		SupplierPart:     "https://inventory.example.test/inventree/purchasing/supplier-part/16/",
		ManufacturerPart: "https://inventory.example.test/inventree/purchasing/manufacturer-part/17/",
		StockLocation:    "https://inventory.example.test/inventree/stock/location/18/",
		StockItem:        "https://inventory.example.test/inventree/stock/item/19/",
		PurchaseOrder:    "https://inventory.example.test/inventree/purchasing/purchase-order/20/",
	}
	ids := map[Kind]int{Part: 11, PartCategory: 12, Company: 13, Supplier: 14, Manufacturer: 15, SupplierPart: 16, ManufacturerPart: 17, StockLocation: 18, StockItem: 19, PurchaseOrder: 20}
	for kind, expected := range want {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, expected, resolver.URL(kind, ids[kind]))
		})
	}
}

func TestResolverAtDefaultMountPreservesDeploymentPrefix(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	resolver, err := NewAtDefaultMount("https://inventory.example.test/inventree/", "INVENTREE_URL", true)
	r.NoError(err)
	r.Equal("https://inventory.example.test/inventree/web/purchasing/purchase-order/65/", resolver.URL(PurchaseOrder, 65))

	_, err = NewAtDefaultMount("http://inventory.example.test", "INVENTREE_URL", true)
	r.ErrorContains(err, "INVENTREE_URL must use https in production")
}

func TestResolverRejectsUnsafeBasesWithoutEchoingValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		value  string
		reason string
	}{
		{name: "userinfo", value: "https://secret-user:secret-pass@inventory.example.test", reason: "must not include userinfo"},
		{name: "query", value: "https://inventory.example.test?token=secret-query", reason: "must not include a query"},
		{name: "empty query", value: "https://inventory.example.test/secret-empty-query?", reason: "must not include a query"},
		{name: "fragment", value: "https://inventory.example.test/#secret-fragment", reason: "must not include a fragment"},
		{name: "scheme", value: "ftp://inventory.example.test/secret-scheme", reason: "scheme must be http or https"},
		{name: "authority", value: "https:///secret-host", reason: "valid authority"},
		{name: "production http", value: "http://inventory.example.test/secret-path", reason: "must use https in production"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := assert.New(t)
			_, err := New(tc.value, "INVENTREE_WEB_URL", true)
			require.Error(t, err)
			a.Contains(err.Error(), "INVENTREE_WEB_URL")
			a.Contains(err.Error(), tc.reason)
			a.NotContains(err.Error(), tc.value)
			a.NotContains(err.Error(), "secret-")
		})
	}
}

func TestPinnedRouterEvidenceCoversEveryRoutePattern(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	data, err := os.ReadFile("testdata/inventree-1.4.3-router-routes.txt")
	r.NoError(err)
	evidence := string(data)
	r.Contains(evidence, "source_commit=6b237de54e4cbfd7f51daff8403c17869898d965")
	r.Contains(evidence, "source_blob=ddeb3a21365761e999568c84d6417915817a9024")
	r.Contains(evidence, "source_desktop_blob=ba921811446397503ef4d45f5476e504888882ef")
	r.Contains(evidence, "source_navigation_blob=939dd0211522e4ebcb06cbf0d3b587ae3dbf2721")
	r.Contains(evidence, "<BrowserRouter basename={getBaseUrl()}>")
	r.Contains(evidence, "(window as any).INVENTREE_SETTINGS?.base_url || 'web'")
	r.Contains(evidence, "canonical_default_mount="+DefaultMount)

	wantDeclarations := []string{
		"<Route path='part/'>", "category/:id?/*", ":id/*",
		"<Route path='stock/'>", "location/:id?/*", "item/:id/*",
		"<Route path='purchasing/'>", "purchase-order/:id/*", "supplier/:id/*",
		"supplier-part/:id/*", "manufacturer/:id/*", "manufacturer-part/:id/*",
		"company/:id/*",
	}
	for _, declaration := range wantDeclarations {
		r.True(strings.Contains(evidence, declaration), declaration)
	}
	wantComposed := map[Kind]string{
		Part:             "part/:id/*",
		PartCategory:     "part/category/:id?/*",
		Company:          "company/:id/*",
		Supplier:         "purchasing/supplier/:id/*",
		Manufacturer:     "purchasing/manufacturer/:id/*",
		SupplierPart:     "purchasing/supplier-part/:id/*",
		ManufacturerPart: "purchasing/manufacturer-part/:id/*",
		StockLocation:    "stock/location/:id?/*",
		StockItem:        "stock/item/:id/*",
		PurchaseOrder:    "purchasing/purchase-order/:id/*",
	}
	r.Len(routePatterns, len(wantComposed))
	for kind, route := range wantComposed {
		r.Contains(evidence, "canonical_route "+string(kind)+"="+route)
		idRoute := strings.ReplaceAll(strings.ReplaceAll(route, ":id?", strconv.Itoa(47)), ":id", strconv.Itoa(47))
		idRoute = strings.TrimSuffix(idRoute, "*")
		r.Equal(idRoute, fmt.Sprintf(routePatterns[kind], 47))
	}
}

func TestClarificationAPIPathClassification(t *testing.T) {
	t.Parallel()
	kind, id, ok := KindForAPIPath("/api/order/po/42/")
	require.True(t, ok)
	assert.Equal(t, PurchaseOrder, kind)
	assert.Equal(t, 42, id)

	_, _, ok = KindForAPIPath("https://attacker.example/api/part/1/")
	assert.False(t, ok)
	_, err := ValidateAPIPath("/api/part/1/?token=secret")
	assert.Error(t, err)
}

func TestClarificationAPIPathClassificationCoversAllDirectKinds(t *testing.T) {
	t.Parallel()
	tests := map[string]Kind{
		"/api/part/1/":                      Part,
		"/api/part/category/1/":             PartCategory,
		"/api/company/1/":                   Company,
		"/api/company/part/1/":              SupplierPart,
		"/api/company/part/manufacturer/1/": ManufacturerPart,
		"/api/stock/location/1/":            StockLocation,
		"/api/stock/1/":                     StockItem,
		"/api/order/po/1/":                  PurchaseOrder,
	}
	for apiPath, want := range tests {
		apiPath, want := apiPath, want
		t.Run(string(want), func(t *testing.T) {
			t.Parallel()
			kind, id, ok := KindForAPIPath(apiPath)
			require.True(t, ok)
			assert.Equal(t, want, kind)
			assert.Equal(t, 1, id)
		})
	}

	for _, invalid := range []string{"", "/part/1/", "/api/part/not-an-id/", "/api/unknown/1/", "/api/part/0/", "/api/part/1/?query=x", "/api/part/1/#fragment"} {
		_, _, ok := KindForAPIPath(invalid)
		assert.False(t, ok, invalid)
	}
}

func TestResolverRejectsCanonicalAndEscapedPathViolations(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "https://inventory.example.test/a/../b", "https://inventory.example.test/a//b", "https://inventory.example.test/a%2Fb"} {
		_, err := New(value, "INVENTREE_WEB_URL", false)
		require.Error(t, err, value)
	}
	resolver, err := New("https://inventory.example.test", "INVENTREE_WEB_URL", false)
	require.NoError(t, err)
	assert.Empty(t, resolver.URL(Part, 0))
	assert.Empty(t, resolver.URL(Kind("unknown"), 1))
	assert.Empty(t, (*Resolver)(nil).URL(Part, 1))

	for _, invalid := range []string{"https://attacker.example/api/part/1/", "/not-api/1/", "/api/part/1/?q=x", "/api/part/1/#x", "/api/part/a/../1/"} {
		_, err := ValidateAPIPath(invalid)
		require.Error(t, err, invalid)
	}
}
