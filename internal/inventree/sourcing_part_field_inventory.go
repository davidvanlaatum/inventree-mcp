package inventree

// SourcingPartFieldClass records the approved handling for every field in the
// pinned API 530 SupplierPart and ManufacturerPart serializers. These
// inventories are exhaustive so schema drift cannot silently widen exact
// sourcing-link output or ordinary mutation inputs.
type SourcingPartFieldClass string

const (
	SourcingPartFieldExposed        SourcingPartFieldClass = "exposed"
	SourcingPartFieldSeparateLookup SourcingPartFieldClass = "separate_lookup"
	SourcingPartFieldDeferred       SourcingPartFieldClass = "deferred"
	SourcingPartFieldWriteOnly      SourcingPartFieldClass = "write_only"
	SourcingPartFieldExcluded       SourcingPartFieldClass = "excluded"
)

var SupplierPartFieldInventory = map[string]SourcingPartFieldClass{
	"available":                SourcingPartFieldExposed,
	"availability_updated":     SourcingPartFieldExposed,
	"description":              SourcingPartFieldExposed,
	"duplicate":                SourcingPartFieldWriteOnly,
	"in_stock":                 SourcingPartFieldExposed,
	"on_order":                 SourcingPartFieldExposed,
	"link":                     SourcingPartFieldExposed,
	"active":                   SourcingPartFieldExposed,
	"primary":                  SourcingPartFieldExposed,
	"manufacturer_detail":      SourcingPartFieldSeparateLookup,
	"manufacturer_part":        SourcingPartFieldExposed,
	"manufacturer_part_detail": SourcingPartFieldSeparateLookup,
	"MPN":                      SourcingPartFieldExposed,
	"note":                     SourcingPartFieldExposed,
	"pk":                       SourcingPartFieldExposed,
	"barcode_hash":             SourcingPartFieldExcluded,
	"packaging":                SourcingPartFieldExposed,
	"pack_quantity":            SourcingPartFieldExposed,
	"pack_quantity_native":     SourcingPartFieldExposed,
	"part":                     SourcingPartFieldExposed,
	"pretty_name":              SourcingPartFieldDeferred,
	"SKU":                      SourcingPartFieldExposed,
	"supplier":                 SourcingPartFieldExposed,
	"supplier_detail":          SourcingPartFieldSeparateLookup,
	"updated":                  SourcingPartFieldExposed,
	"notes":                    SourcingPartFieldExposed,
	"part_detail":              SourcingPartFieldSeparateLookup,
	"tags":                     SourcingPartFieldDeferred,
	"price_breaks":             SourcingPartFieldDeferred,
	"parameters":               SourcingPartFieldSeparateLookup,
}

var ManufacturerPartFieldInventory = map[string]SourcingPartFieldClass{
	"pk":                  SourcingPartFieldExposed,
	"part":                SourcingPartFieldExposed,
	"part_detail":         SourcingPartFieldSeparateLookup,
	"pretty_name":         SourcingPartFieldDeferred,
	"manufacturer":        SourcingPartFieldExposed,
	"manufacturer_detail": SourcingPartFieldSeparateLookup,
	"description":         SourcingPartFieldExposed,
	"duplicate":           SourcingPartFieldWriteOnly,
	"MPN":                 SourcingPartFieldExposed,
	"link":                SourcingPartFieldExposed,
	"barcode_hash":        SourcingPartFieldExcluded,
	"notes":               SourcingPartFieldExposed,
	"tags":                SourcingPartFieldDeferred,
	"parameters":          SourcingPartFieldSeparateLookup,
}
