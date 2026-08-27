package inventree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// GlobalSearchObjectType identifies one InvenTree model recognized by
// POST /api/search/, using the same unqualified key InvenTree itself uses
// as both the request opt-in key and the response bucket key.
type GlobalSearchObjectType string

const (
	GlobalSearchPart             GlobalSearchObjectType = "part"
	GlobalSearchPartCategory     GlobalSearchObjectType = "partcategory"
	GlobalSearchStockItem        GlobalSearchObjectType = "stockitem"
	GlobalSearchStockLocation    GlobalSearchObjectType = "stocklocation"
	GlobalSearchCompany          GlobalSearchObjectType = "company"
	GlobalSearchSupplierPart     GlobalSearchObjectType = "supplierpart"
	GlobalSearchManufacturerPart GlobalSearchObjectType = "manufacturerpart"
	GlobalSearchPurchaseOrder    GlobalSearchObjectType = "purchaseorder"
)

// SupportedGlobalSearchObjectTypes is the MCP-approved subset of InvenTree's
// POST /api/search/ model-key registry. The F-S88 spike verified InvenTree
// 1.5.2/API 530 additionally recognizes salesorder, returnorder, and build,
// but inventree-mcp has no get_* tool for any of those object families, so
// global search cannot satisfy its routing-to-an-exact-read contract for
// them; exposing them is deferred until those workflows are separately
// implemented and approved.
var SupportedGlobalSearchObjectTypes = []GlobalSearchObjectType{
	GlobalSearchPart,
	GlobalSearchPartCategory,
	GlobalSearchStockItem,
	GlobalSearchStockLocation,
	GlobalSearchCompany,
	GlobalSearchSupplierPart,
	GlobalSearchManufacturerPart,
	GlobalSearchPurchaseOrder,
}

// ErrGlobalSearchSchemaDrift reports that InvenTree's response omitted a
// bucket for an object type this client explicitly requested. InvenTree
// silently drops any request key it does not recognize rather than
// erroring, so a missing bucket after a successful response means the
// upstream model registry no longer matches SupportedGlobalSearchObjectTypes.
var ErrGlobalSearchSchemaDrift = errors.New("InvenTree global search response is missing a requested object type")

func ValidGlobalSearchObjectType(objectType GlobalSearchObjectType) bool {
	for _, supported := range SupportedGlobalSearchObjectTypes {
		if supported == objectType {
			return true
		}
	}
	return false
}

type GlobalSearchQuery struct {
	Search      string
	SearchRegex bool
	SearchWhole bool
	SearchNotes bool
	ObjectTypes []GlobalSearchObjectType
	// Limit bounds the number of results returned per requested object
	// type. InvenTree's own default is 1, so callers must set this
	// explicitly to get a useful result set.
	Limit int
}

func (q GlobalSearchQuery) validate() error {
	if strings.TrimSpace(q.Search) == "" {
		return errors.New("global search requires a non-empty search term")
	}
	if len(q.ObjectTypes) == 0 {
		return errors.New("global search requires at least one object type")
	}
	seen := make(map[GlobalSearchObjectType]bool, len(q.ObjectTypes))
	for _, objectType := range q.ObjectTypes {
		if !ValidGlobalSearchObjectType(objectType) {
			return fmt.Errorf("global search does not support object type %q", objectType)
		}
		if seen[objectType] {
			return fmt.Errorf("global search object type %q was requested more than once", objectType)
		}
		seen[objectType] = true
	}
	return nil
}

// GlobalSearchBucket mirrors one object type's bucket in InvenTree's
// POST /api/search/ response: a count plus the matching records, using the
// same list-endpoint projection already approved for that type's dedicated
// search_* tool (for example Part, Company). InvenTree's own bucket also
// carries next/previous pagination cursors; global search does not expose
// them and instead relies on the caller narrowing Search or ObjectTypes,
// consistent with the bounded, non-paginated contract other MCP search
// tools already use.
type GlobalSearchBucket[T any] struct {
	Count   int `json:"count"`
	Results []T `json:"results"`
}

type GlobalSearchResult struct {
	Parts             *GlobalSearchBucket[Part]
	PartCategories    *GlobalSearchBucket[Category]
	StockItems        *GlobalSearchBucket[StockItem]
	StockLocations    *GlobalSearchBucket[StockLocation]
	Companies         *GlobalSearchBucket[Company]
	SupplierParts     *GlobalSearchBucket[SupplierPart]
	ManufacturerParts *GlobalSearchBucket[ManufacturerPart]
	PurchaseOrders    *GlobalSearchBucket[PurchaseOrder]
}

// GlobalSearch calls POST /api/search/, requesting exactly the caller's
// selected object types. InvenTree includes a model's bucket in the
// response only when that model's key is present in the request (even as
// an empty object) and silently omits any key it does not recognize, so a
// requested-but-missing bucket after a successful response is treated as
// upstream schema drift (ErrGlobalSearchSchemaDrift) and fails closed
// rather than silently returning a short result.
func (c *Client) GlobalSearch(ctx context.Context, query GlobalSearchQuery) (GlobalSearchResult, error) {
	if err := query.validate(); err != nil {
		return GlobalSearchResult{}, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	// limit/offset are top-level InvenTree request fields applied uniformly
	// to every requested bucket -- verified live (F-S88 spike follow-up):
	// nesting "limit" inside a per-model key is silently ignored, so every
	// bucket is truncated to InvenTree's own top-level default of 1 unless
	// the top-level field is set here.
	body := map[string]any{"search": query.Search, "limit": limit}
	if query.SearchRegex {
		body["search_regex"] = true
	}
	if query.SearchWhole {
		body["search_whole"] = true
	}
	if query.SearchNotes {
		body["search_notes"] = true
	}
	for _, objectType := range query.ObjectTypes {
		body[string(objectType)] = map[string]any{}
	}

	var raw map[string]json.RawMessage
	if err := c.Post(ctx, "/api/search/", body, &raw); err != nil {
		return GlobalSearchResult{}, err
	}

	var result GlobalSearchResult
	for _, objectType := range query.ObjectTypes {
		payload, ok := raw[string(objectType)]
		if !ok {
			return GlobalSearchResult{}, fmt.Errorf("%w: %q", ErrGlobalSearchSchemaDrift, objectType)
		}
		var err error
		switch objectType {
		case GlobalSearchPart:
			result.Parts, err = decodeGlobalSearchBucket[Part](payload)
		case GlobalSearchPartCategory:
			result.PartCategories, err = decodeGlobalSearchBucket[Category](payload)
		case GlobalSearchStockItem:
			result.StockItems, err = decodeGlobalSearchBucket[StockItem](payload)
		case GlobalSearchStockLocation:
			result.StockLocations, err = decodeGlobalSearchBucket[StockLocation](payload)
		case GlobalSearchCompany:
			result.Companies, err = decodeGlobalSearchBucket[Company](payload)
		case GlobalSearchSupplierPart:
			result.SupplierParts, err = decodeGlobalSearchBucket[SupplierPart](payload)
		case GlobalSearchManufacturerPart:
			result.ManufacturerParts, err = decodeGlobalSearchBucket[ManufacturerPart](payload)
		case GlobalSearchPurchaseOrder:
			result.PurchaseOrders, err = decodeGlobalSearchBucket[PurchaseOrder](payload)
		default:
			err = fmt.Errorf("global search does not support object type %q", objectType)
		}
		if err != nil {
			return GlobalSearchResult{}, fmt.Errorf("decode global search %q results: %w", objectType, err)
		}
	}
	return result, nil
}

func decodeGlobalSearchBucket[T any](raw json.RawMessage) (*GlobalSearchBucket[T], error) {
	bucket := new(GlobalSearchBucket[T])
	if err := json.Unmarshal(raw, bucket); err != nil {
		return nil, err
	}
	return bucket, nil
}
