package inventree

// PurchaseOrderFieldClass records the approved handling for every field in the
// pinned API 530 PurchaseOrder, PurchaseOrderLineItem, and PurchaseOrderExtraLine
// serializers. These inventories are exhaustive so schema drift cannot silently
// widen exact purchase-order output or ordinary mutation inputs.
type PurchaseOrderFieldClass string

const (
	PurchaseOrderFieldExposed        PurchaseOrderFieldClass = "exposed"
	PurchaseOrderFieldSeparateLookup PurchaseOrderFieldClass = "separate_lookup"
	PurchaseOrderFieldDeferred       PurchaseOrderFieldClass = "deferred"
	PurchaseOrderFieldWriteOnly      PurchaseOrderFieldClass = "write_only"
	PurchaseOrderFieldExcluded       PurchaseOrderFieldClass = "excluded"
)

var PurchaseOrderFieldInventory = map[string]PurchaseOrderFieldClass{
	"pk":                  PurchaseOrderFieldExposed,
	"created_by":          PurchaseOrderFieldExposed,
	"creation_date":       PurchaseOrderFieldExposed,
	"issue_date":          PurchaseOrderFieldExposed,
	"start_date":          PurchaseOrderFieldExposed,
	"target_date":         PurchaseOrderFieldExposed,
	"description":         PurchaseOrderFieldExposed,
	"line_items":          PurchaseOrderFieldExposed,
	"completed_lines":     PurchaseOrderFieldExposed,
	"link":                PurchaseOrderFieldExposed,
	"project_code":        PurchaseOrderFieldExposed,
	"reference":           PurchaseOrderFieldExposed,
	"responsible":         PurchaseOrderFieldExposed,
	"contact":             PurchaseOrderFieldExposed,
	"address":             PurchaseOrderFieldExposed,
	"status":              PurchaseOrderFieldExposed,
	"status_text":         PurchaseOrderFieldExposed,
	"status_custom_key":   PurchaseOrderFieldExposed,
	"notes":               PurchaseOrderFieldExposed,
	"barcode_hash":        PurchaseOrderFieldExcluded,
	"overdue":             PurchaseOrderFieldExposed,
	"duplicate":           PurchaseOrderFieldWriteOnly,
	"address_detail":      PurchaseOrderFieldSeparateLookup,
	"contact_detail":      PurchaseOrderFieldSeparateLookup,
	"project_code_detail": PurchaseOrderFieldSeparateLookup,
	"project_code_label":  PurchaseOrderFieldExposed,
	"responsible_detail":  PurchaseOrderFieldSeparateLookup,
	"parameters":          PurchaseOrderFieldDeferred,
	"tags":                PurchaseOrderFieldExposed,
	"complete_date":       PurchaseOrderFieldExposed,
	"supplier":            PurchaseOrderFieldExposed,
	"supplier_detail":     PurchaseOrderFieldSeparateLookup,
	"supplier_reference":  PurchaseOrderFieldExposed,
	"supplier_name":       PurchaseOrderFieldExposed,
	"total_price":         PurchaseOrderFieldExposed,
	"order_currency":      PurchaseOrderFieldExposed,
	"destination":         PurchaseOrderFieldExposed,
	"updated_at":          PurchaseOrderFieldExposed,
}

var PurchaseOrderLineFieldInventory = map[string]PurchaseOrderFieldClass{
	"pk":                      PurchaseOrderFieldExposed,
	"line":                    PurchaseOrderFieldExposed,
	"link":                    PurchaseOrderFieldExposed,
	"notes":                   PurchaseOrderFieldExposed,
	"order":                   PurchaseOrderFieldExposed,
	"project_code":            PurchaseOrderFieldExposed,
	"quantity":                PurchaseOrderFieldExposed,
	"reference":               PurchaseOrderFieldExposed,
	"target_date":             PurchaseOrderFieldExposed,
	"order_detail":            PurchaseOrderFieldSeparateLookup,
	"project_code_label":      PurchaseOrderFieldExposed,
	"project_code_detail":     PurchaseOrderFieldSeparateLookup,
	"part":                    PurchaseOrderFieldExposed,
	"build_order":             PurchaseOrderFieldExposed,
	"discount":                PurchaseOrderFieldExposed,
	"overdue":                 PurchaseOrderFieldExposed,
	"received":                PurchaseOrderFieldExposed,
	"purchase_price":          PurchaseOrderFieldExposed,
	"purchase_price_currency": PurchaseOrderFieldExposed,
	"auto_pricing":            PurchaseOrderFieldExposed,
	"destination":             PurchaseOrderFieldExposed,
	"total_price":             PurchaseOrderFieldExposed,
	"merge_items":             PurchaseOrderFieldWriteOnly,
	"sku":                     PurchaseOrderFieldExposed,
	"mpn":                     PurchaseOrderFieldExposed,
	"ipn":                     PurchaseOrderFieldExposed,
	"internal_part":           PurchaseOrderFieldExposed,
	"internal_part_name":      PurchaseOrderFieldExposed,
	"build_order_detail":      PurchaseOrderFieldDeferred,
	"destination_detail":      PurchaseOrderFieldSeparateLookup,
	"part_detail":             PurchaseOrderFieldSeparateLookup,
	"supplier_part_detail":    PurchaseOrderFieldSeparateLookup,
}

var PurchaseOrderExtraLineFieldInventory = map[string]PurchaseOrderFieldClass{
	"pk":                  PurchaseOrderFieldExposed,
	"line":                PurchaseOrderFieldExposed,
	"description":         PurchaseOrderFieldExposed,
	"discount":            PurchaseOrderFieldExposed,
	"link":                PurchaseOrderFieldExposed,
	"notes":               PurchaseOrderFieldExposed,
	"order":               PurchaseOrderFieldExposed,
	"price":               PurchaseOrderFieldExposed,
	"price_currency":      PurchaseOrderFieldExposed,
	"project_code":        PurchaseOrderFieldExposed,
	"quantity":            PurchaseOrderFieldExposed,
	"reference":           PurchaseOrderFieldExposed,
	"target_date":         PurchaseOrderFieldExposed,
	"total_price":         PurchaseOrderFieldExposed,
	"order_detail":        PurchaseOrderFieldSeparateLookup,
	"project_code_label":  PurchaseOrderFieldExposed,
	"project_code_detail": PurchaseOrderFieldSeparateLookup,
}
