package inventree

// CompanyFieldClass records the approved handling for every field in the
// pinned API 530 Company serializer. This inventory is exhaustive so schema
// drift cannot silently widen exact company output or ordinary mutation
// inputs.
type CompanyFieldClass string

const (
	CompanyFieldExposed        CompanyFieldClass = "exposed"
	CompanyFieldSeparateLookup CompanyFieldClass = "separate_lookup"
	CompanyFieldDeferred       CompanyFieldClass = "deferred"
	CompanyFieldWriteOnly      CompanyFieldClass = "write_only"
	CompanyFieldExcluded       CompanyFieldClass = "excluded"
)

var CompanyFieldInventory = map[string]CompanyFieldClass{
	"pk":                 CompanyFieldExposed,
	"name":               CompanyFieldExposed,
	"description":        CompanyFieldExposed,
	"website":            CompanyFieldExposed,
	"phone":              CompanyFieldExposed,
	"email":              CompanyFieldExposed,
	"currency":           CompanyFieldExposed,
	"contact":            CompanyFieldExposed,
	"duplicate":          CompanyFieldWriteOnly,
	"link":               CompanyFieldExposed,
	"image":              CompanyFieldExposed,
	"active":             CompanyFieldExposed,
	"is_customer":        CompanyFieldExposed,
	"is_manufacturer":    CompanyFieldExposed,
	"is_supplier":        CompanyFieldExposed,
	"notes":              CompanyFieldExposed,
	"parts_supplied":     CompanyFieldExposed,
	"parts_manufactured": CompanyFieldExposed,
	"primary_address":    CompanyFieldSeparateLookup,
	"tax_id":             CompanyFieldExposed,
	"parameters":         CompanyFieldDeferred,
	"tags":               CompanyFieldDeferred,
}
