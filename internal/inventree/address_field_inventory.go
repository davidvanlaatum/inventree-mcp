package inventree

// AddressFieldClass records the approved handling for every field in the
// pinned API 530 Address serializer. This inventory is exhaustive so schema
// drift cannot silently widen exact address output.
type AddressFieldClass string

const (
	AddressFieldExposed  AddressFieldClass = "exposed"
	AddressFieldExcluded AddressFieldClass = "excluded"
)

// AddressFieldInventory classifies every pinned Address serializer field.
// Per the F-S49 operator decision, the street-address lines and postal code
// are excluded from every MCP projection so precise physical-address PII
// never reaches agent context; city/region, country, and shipping notes
// remain to disambiguate between a company's addresses.
var AddressFieldInventory = map[string]AddressFieldClass{
	"pk":                      AddressFieldExposed,
	"company":                 AddressFieldExposed,
	"title":                   AddressFieldExposed,
	"primary":                 AddressFieldExposed,
	"line1":                   AddressFieldExcluded,
	"line2":                   AddressFieldExcluded,
	"postal_code":             AddressFieldExcluded,
	"postal_city":             AddressFieldExposed,
	"province":                AddressFieldExposed,
	"country":                 AddressFieldExposed,
	"shipping_notes":          AddressFieldExposed,
	"internal_shipping_notes": AddressFieldExposed,
	"link":                    AddressFieldExposed,
}
