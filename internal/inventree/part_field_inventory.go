package inventree

// PartFieldClass records the approved handling for every field in the pinned
// API 530 Part serializer. The inventory is intentionally exhaustive so schema
// drift cannot silently widen exact part output or mutation inputs.
type PartFieldClass string

const (
	PartFieldExposed        PartFieldClass = "exposed"
	PartFieldSeparateLookup PartFieldClass = "separate_lookup"
	PartFieldDeferred       PartFieldClass = "deferred"
	PartFieldWriteOnly      PartFieldClass = "write_only"
	PartFieldExcluded       PartFieldClass = "excluded"
)

var PartFieldInventory = map[string]PartFieldClass{
	"active":                    PartFieldExposed,
	"allocated_to_build_orders": PartFieldExposed,
	"allocated_to_sales_orders": PartFieldExposed,
	"assembly":                  PartFieldExposed,
	"barcode_hash":              PartFieldExcluded,
	"building":                  PartFieldExposed,
	"category":                  PartFieldExposed,
	"category_default_location": PartFieldExposed,
	"category_detail":           PartFieldSeparateLookup,
	"category_name":             PartFieldExposed,
	"category_path":             PartFieldSeparateLookup,
	"component":                 PartFieldExposed,
	"consumable":                PartFieldExposed,
	"copy_category_parameters":  PartFieldWriteOnly,
	"creation_date":             PartFieldExposed,
	"creation_user":             PartFieldExposed,
	"default_expiry":            PartFieldExposed,
	"default_location":          PartFieldExposed,
	"default_location_detail":   PartFieldSeparateLookup,
	"description":               PartFieldExposed,
	"duplicate":                 PartFieldWriteOnly,
	"existing_image":            PartFieldWriteOnly,
	"external_stock":            PartFieldExposed,
	"full_name":                 PartFieldExposed,
	"image":                     PartFieldExposed,
	"initial_stock":             PartFieldWriteOnly,
	"initial_supplier":          PartFieldWriteOnly,
	"in_stock":                  PartFieldExposed,
	"IPN":                       PartFieldExposed,
	"is_template":               PartFieldExposed,
	"keywords":                  PartFieldExposed,
	"link":                      PartFieldExposed,
	"locked":                    PartFieldExposed,
	"maximum_stock":             PartFieldExposed,
	"minimum_stock":             PartFieldExposed,
	"name":                      PartFieldExposed,
	"notes":                     PartFieldExposed,
	"ordering":                  PartFieldExposed,
	"parameters":                PartFieldSeparateLookup,
	"pk":                        PartFieldExposed,
	"price_breaks":              PartFieldDeferred,
	"pricing_max":               PartFieldExposed,
	"pricing_min":               PartFieldExposed,
	"pricing_updated":           PartFieldExposed,
	"purchaseable":              PartFieldExposed,
	"required_for_build_orders": PartFieldExposed,
	"required_for_sales_orders": PartFieldExposed,
	"responsible":               PartFieldDeferred,
	"revision":                  PartFieldExposed,
	"revision_count":            PartFieldDeferred,
	"revision_of":               PartFieldDeferred,
	"salable":                   PartFieldExposed,
	"scheduled_to_build":        PartFieldExposed,
	"starred":                   PartFieldExposed,
	"stock_item_count":          PartFieldExposed,
	"tags":                      PartFieldDeferred,
	"testable":                  PartFieldExposed,
	"thumbnail":                 PartFieldExposed,
	"total_in_stock":            PartFieldExposed,
	"trackable":                 PartFieldExposed,
	"unallocated_stock":         PartFieldExposed,
	"units":                     PartFieldExposed,
	"variant_of":                PartFieldDeferred,
	"variant_stock":             PartFieldExposed,
	"virtual":                   PartFieldExposed,
}
