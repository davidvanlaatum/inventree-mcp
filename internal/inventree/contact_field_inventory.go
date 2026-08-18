package inventree

// ContactFieldClass records the approved handling for every field in the
// pinned API 530 Contact serializer. This inventory is exhaustive so schema
// drift cannot silently widen exact contact output.
type ContactFieldClass string

const (
	ContactFieldExposed  ContactFieldClass = "exposed"
	ContactFieldExcluded ContactFieldClass = "excluded"
)

// ContactFieldInventory classifies every pinned Contact serializer field.
// Per the F-S49 operator decision, phone and email are excluded from every
// MCP projection so contact PII never reaches agent context.
var ContactFieldInventory = map[string]ContactFieldClass{
	"pk":           ContactFieldExposed,
	"company":      ContactFieldExposed,
	"company_name": ContactFieldExposed,
	"name":         ContactFieldExposed,
	"phone":        ContactFieldExcluded,
	"email":        ContactFieldExcluded,
	"role":         ContactFieldExposed,
}
