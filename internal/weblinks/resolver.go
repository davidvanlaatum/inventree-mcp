// Package weblinks constructs trusted, user-facing InvenTree frontend URLs.
package weblinks

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// Kind identifies a stable InvenTree frontend route.
type Kind string

const (
	// DefaultMount is the stock InvenTree frontend basename.
	DefaultMount = "web"

	Part             Kind = "part"
	PartCategory     Kind = "part_category"
	Company          Kind = "company"
	Supplier         Kind = "supplier"
	Manufacturer     Kind = "manufacturer"
	SupplierPart     Kind = "supplier_part"
	ManufacturerPart Kind = "manufacturer_part"
	StockLocation    Kind = "stock_location"
	StockItem        Kind = "stock_item"
	PurchaseOrder    Kind = "purchase_order"
	User             Kind = "user"
)

var routePatterns = map[Kind]string{
	Part:             "part/%d/",
	PartCategory:     "part/category/%d/",
	Company:          "company/%d/",
	Supplier:         "purchasing/supplier/%d/",
	Manufacturer:     "purchasing/manufacturer/%d/",
	SupplierPart:     "purchasing/supplier-part/%d/",
	ManufacturerPart: "purchasing/manufacturer-part/%d/",
	StockLocation:    "stock/location/%d/",
	StockItem:        "stock/item/%d/",
	PurchaseOrder:    "purchasing/purchase-order/%d/",
	// User follows the same "core/" section pattern the pinned InvenTree
	// 1.5.0 frontend router registers for every other object
	// (<Route path='core/'><Route path='user/:id/*' .../></Route> in
	// router.tsx, mirroring company/:id/* -> "company/%d/" below). The
	// F-S104 operator decision's suggested .../detail suffix reflects a
	// live browser observation of UserDetail's own default internal tab
	// redirect, not the canonical registered route -- every other Kind
	// here already omits its object's default-tab suffix the same way and
	// still resolves correctly, so User does too.
	User: "core/user/%d/",
}

// NewAtDefaultMount validates a credential-free InvenTree site base and
// resolves links beneath the stock frontend mount.
func NewAtDefaultMount(raw string, key string, requireHTTPS bool) (*Resolver, error) {
	resolver, err := New(raw, key, requireHTTPS)
	if err != nil {
		return nil, err
	}
	resolver.base.Path += DefaultMount + "/"
	return resolver, nil
}

// Resolver joins typed frontend routes to one validated process-scoped base.
type Resolver struct {
	base url.URL
}

// New validates and normalizes a credential-free HTTP(S) frontend base.
// Errors intentionally identify only the configuration key and rejection
// reason; the rejected value is never included.
func New(raw string, key string, requireHTTPS bool) (*Resolver, error) {
	if key == "" {
		key = "web base"
	}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%s is required", key)
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return nil, fmt.Errorf("%s must be an absolute URL with a valid authority", key)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s scheme must be http or https", key)
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s must use https in production", key)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("%s must not include userinfo", key)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return nil, fmt.Errorf("%s must not include a query", key)
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("%s must not include a fragment", key)
	}
	if parsed.RawPath != "" || strings.Contains(parsed.EscapedPath(), "%") {
		return nil, fmt.Errorf("%s path must not use escaped separators or segments", key)
	}
	cleaned := path.Clean("/" + strings.TrimSpace(parsed.Path))
	if cleaned == "/." {
		cleaned = "/"
	}
	if parsed.Path != "" && parsed.Path != "/" && path.Clean(parsed.Path) != strings.TrimSuffix(parsed.Path, "/") {
		return nil, fmt.Errorf("%s path must be canonical", key)
	}
	parsed.Path = strings.TrimSuffix(cleaned, "/") + "/"
	if parsed.Path == "//" {
		parsed.Path = "/"
	}
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return &Resolver{base: *parsed}, nil
}

// URL returns an absolute frontend URL for a positive stable ID.
func (r *Resolver) URL(kind Kind, id int) string {
	if r == nil || id <= 0 {
		return ""
	}
	pattern, ok := routePatterns[kind]
	if !ok {
		return ""
	}
	resolved := r.base
	resolved.Path = r.base.Path + fmt.Sprintf(pattern, id)
	return resolved.String()
}

// KindForAPIPath maps the clarification-only REST paths that identify objects
// with stable dedicated frontend routes.
func KindForAPIPath(apiPath string) (Kind, int, bool) {
	parsed, err := url.Parse(apiPath)
	if err != nil || parsed.IsAbs() || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", 0, false
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 3 || segments[0] != "api" {
		return "", 0, false
	}
	lastID := func() (int, bool) {
		id, parseErr := strconv.Atoi(segments[len(segments)-1])
		return id, parseErr == nil && id > 0
	}
	id, ok := lastID()
	if !ok {
		return "", 0, false
	}
	switch strings.Join(segments[1:len(segments)-1], "/") {
	case "part":
		return Part, id, true
	case "part/category":
		return PartCategory, id, true
	case "company":
		return Company, id, true
	case "company/part":
		return SupplierPart, id, true
	case "company/part/manufacturer":
		return ManufacturerPart, id, true
	case "stock/location":
		return StockLocation, id, true
	case "stock":
		return StockItem, id, true
	case "order/po":
		return PurchaseOrder, id, true
	default:
		return "", 0, false
	}
}

// ValidateAPIPath returns a sanitized relative REST path or an error.
func ValidateAPIPath(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/api/") {
		return "", errors.New("API URL must be a relative /api/ path")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", errors.New("API URL must not include authority, userinfo, query, fragment, or escaped path data")
	}
	if path.Clean(parsed.Path) != strings.TrimSuffix(parsed.Path, "/") {
		return "", errors.New("API URL path must be canonical")
	}
	return parsed.Path, nil
}
