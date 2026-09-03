package inventree

// StockLocationFieldClass records the approved handling for every field in
// the pinned API 530 Location and StockLocationType serializers. These
// inventories are exhaustive so schema drift cannot silently widen exact
// stock-location output or ordinary mutation inputs.
type StockLocationFieldClass string

const (
	StockLocationFieldExposed        StockLocationFieldClass = "exposed"
	StockLocationFieldSeparateLookup StockLocationFieldClass = "separate_lookup"
	StockLocationFieldDeferred       StockLocationFieldClass = "deferred"
	StockLocationFieldWriteOnly      StockLocationFieldClass = "write_only"
	StockLocationFieldExcluded       StockLocationFieldClass = "excluded"
)

// StockLocationFieldInventory classifies every pinned Location serializer
// field. barcode_hash stays excluded by design: F-S99 derives a computed
// has_barcode bool from it instead (see GetStockLocation), rather than
// reclassifying barcode_hash itself. parameters stays deferred to F-S64.
// location_type_detail is exposed rather than a separate
// lookup because InvenTree always embeds it in this serializer and the
// existing get_stock_location/search_stock_locations projection already
// carries it. tags is exposed per F-S91: the underlying GET only requests
// the ?tags=true query flag from GetStockLocation, so it stays empty on
// concise search_stock_locations results despite sharing this struct.
var StockLocationFieldInventory = map[string]StockLocationFieldClass{
	"pk":                   StockLocationFieldExposed,
	"barcode_hash":         StockLocationFieldExcluded,
	"name":                 StockLocationFieldExposed,
	"level":                StockLocationFieldExposed,
	"description":          StockLocationFieldExposed,
	"parent":               StockLocationFieldExposed,
	"pathstring":           StockLocationFieldExposed,
	"path":                 StockLocationFieldExposed,
	"items":                StockLocationFieldExposed,
	"sublocations":         StockLocationFieldExposed,
	"owner":                StockLocationFieldExposed,
	"icon":                 StockLocationFieldExposed,
	"custom_icon":          StockLocationFieldExposed,
	"structural":           StockLocationFieldExposed,
	"external":             StockLocationFieldExposed,
	"location_type":        StockLocationFieldExposed,
	"location_type_detail": StockLocationFieldExposed,
	"tags":                 StockLocationFieldExposed,
	"parameters":           StockLocationFieldDeferred,
}

// StockLocationTypeFieldInventory classifies every pinned StockLocationType
// serializer field. Every field is exposed; the type is a small reference
// record with no deferred or nested content.
var StockLocationTypeFieldInventory = map[string]StockLocationFieldClass{
	"pk":             StockLocationFieldExposed,
	"name":           StockLocationFieldExposed,
	"description":    StockLocationFieldExposed,
	"icon":           StockLocationFieldExposed,
	"location_count": StockLocationFieldExposed,
}
