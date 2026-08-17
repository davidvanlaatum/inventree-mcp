package inventree

// StockItemFieldClass records the approved handling for every field in the
// pinned API 530 StockItem serializer. The inventory is intentionally
// exhaustive so schema drift cannot silently widen exact stock-item output.
type StockItemFieldClass string

const (
	StockItemFieldExposed        StockItemFieldClass = "exposed"
	StockItemFieldSeparateLookup StockItemFieldClass = "separate_lookup"
	StockItemFieldDeferred       StockItemFieldClass = "deferred"
	StockItemFieldWriteOnly      StockItemFieldClass = "write_only"
	StockItemFieldExcluded       StockItemFieldClass = "excluded"
)

var StockItemFieldInventory = map[string]StockItemFieldClass{
	"pk":                       StockItemFieldExposed,
	"part":                     StockItemFieldExposed,
	"quantity":                 StockItemFieldExposed,
	"serial":                   StockItemFieldExposed,
	"batch":                    StockItemFieldExposed,
	"location":                 StockItemFieldExposed,
	"belongs_to":               StockItemFieldExposed,
	"build":                    StockItemFieldExposed,
	"consumed_by":              StockItemFieldExposed,
	"customer":                 StockItemFieldExposed,
	"delete_on_deplete":        StockItemFieldExposed,
	"expiry_date":              StockItemFieldExposed,
	"in_stock":                 StockItemFieldExposed,
	"is_building":              StockItemFieldExposed,
	"link":                     StockItemFieldExposed,
	"notes":                    StockItemFieldExposed,
	"owner":                    StockItemFieldExposed,
	"packaging":                StockItemFieldExposed,
	"parent":                   StockItemFieldExposed,
	"purchase_order":           StockItemFieldExposed,
	"purchase_order_reference": StockItemFieldExposed,
	"sales_order":              StockItemFieldExposed,
	"sales_order_reference":    StockItemFieldExposed,
	"status":                   StockItemFieldExposed,
	"status_text":              StockItemFieldExposed,
	"status_custom_key":        StockItemFieldExposed,
	"supplier_part":            StockItemFieldExposed,
	"SKU":                      StockItemFieldExposed,
	"MPN":                      StockItemFieldExposed,
	"barcode_hash":             StockItemFieldExcluded,
	"creation_date":            StockItemFieldExposed,
	"stocktake_date":           StockItemFieldExposed,
	"updated":                  StockItemFieldExposed,
	"purchase_price":           StockItemFieldExposed,
	"purchase_price_currency":  StockItemFieldExposed,
	"use_pack_size":            StockItemFieldWriteOnly,
	"serial_numbers":           StockItemFieldWriteOnly,
	"allocated":                StockItemFieldExposed,
	"expired":                  StockItemFieldExposed,
	"installed_items":          StockItemFieldExposed,
	"child_items":              StockItemFieldExposed,
	"stale":                    StockItemFieldExposed,
	"location_detail":          StockItemFieldSeparateLookup,
	"location_path":            StockItemFieldExposed,
	"part_detail":              StockItemFieldSeparateLookup,
	"supplier_part_detail":     StockItemFieldSeparateLookup,
	"tags":                     StockItemFieldDeferred,
	"tests":                    StockItemFieldDeferred,
	"tracking_items":           StockItemFieldExposed,
}
