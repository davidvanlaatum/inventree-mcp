package inventree

import "encoding/json"

const (
	PurchaseOrderStatusPending   = 10
	PurchaseOrderStatusPlaced    = 20
	PurchaseOrderStatusOnHold    = 25
	PurchaseOrderStatusComplete  = 30
	PurchaseOrderStatusCancelled = 40
)

// DecimalString preserves schema decimal values while accepting InvenTree
// responses that encode the same field as either a JSON string or number.
type DecimalString string

// WebLinkFields are MCP projection fields populated from trusted process
// configuration. InvenTree API responses do not supply these values.
type WebLinkFields struct {
	WebURL       string `json:"web_url,omitempty"`
	ParentWebURL string `json:"parent_web_url,omitempty"`
}

func (value *DecimalString) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*value = DecimalString(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*value = DecimalString(number.String())
	return nil
}

type Part struct {
	WebLinkFields
	PK              int     `json:"pk"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Category        *int    `json:"category"`
	DefaultLocation *int    `json:"default_location"`
	Active          bool    `json:"active"`
	Assembly        bool    `json:"assembly"`
	Component       bool    `json:"component"`
	Purchaseable    bool    `json:"purchaseable"`
	Salable         bool    `json:"salable"`
	Trackable       bool    `json:"trackable"`
	Virtual         bool    `json:"virtual"`
	Image           *string `json:"image"`
	VariantOf       *int    `json:"variant_of"`
}

// PartDetail is the approved complete scalar projection returned by the exact
// part endpoint. Embedded/nested records and deferred workflow fields are
// intentionally omitted; see PartFieldInventory for the pinned classification.
type PartDetail struct {
	WebLinkFields
	PK                      int            `json:"pk"`
	Name                    string         `json:"name"`
	FullName                string         `json:"full_name"`
	Description             string         `json:"description"`
	IPN                     string         `json:"IPN"`
	Category                *int           `json:"category"`
	CategoryName            string         `json:"category_name"`
	CategoryDefaultLocation *int           `json:"category_default_location"`
	DefaultLocation         *int           `json:"default_location"`
	DefaultExpiry           int            `json:"default_expiry"`
	Active                  bool           `json:"active"`
	Assembly                bool           `json:"assembly"`
	Component               bool           `json:"component"`
	Purchaseable            bool           `json:"purchaseable"`
	Salable                 bool           `json:"salable"`
	Trackable               bool           `json:"trackable"`
	Virtual                 bool           `json:"virtual"`
	Consumable              bool           `json:"consumable"`
	IsTemplate              bool           `json:"is_template"`
	Locked                  bool           `json:"locked"`
	Testable                bool           `json:"testable"`
	Starred                 bool           `json:"starred"`
	Image                   *string        `json:"image"`
	Thumbnail               string         `json:"thumbnail"`
	Keywords                *string        `json:"keywords"`
	Link                    *string        `json:"link"`
	MinimumStock            float64        `json:"minimum_stock"`
	MaximumStock            float64        `json:"maximum_stock"`
	Notes                   *string        `json:"notes"`
	Revision                *string        `json:"revision"`
	RevisionOf              *int           `json:"revision_of"`
	RevisionCount           *int           `json:"revision_count"`
	Units                   *string        `json:"units"`
	VariantOf               *int           `json:"variant_of"`
	CreationDate            *string        `json:"creation_date"`
	CreationUser            *int           `json:"creation_user"`
	PricingMin              *DecimalString `json:"pricing_min"`
	PricingMax              *DecimalString `json:"pricing_max"`
	PricingUpdated          *string        `json:"pricing_updated"`
	AllocatedToBuildOrders  *float64       `json:"allocated_to_build_orders"`
	AllocatedToSalesOrders  *float64       `json:"allocated_to_sales_orders"`
	Building                *float64       `json:"building"`
	ScheduledToBuild        *float64       `json:"scheduled_to_build"`
	InStock                 *float64       `json:"in_stock"`
	Ordering                *float64       `json:"ordering"`
	RequiredForBuildOrders  *int           `json:"required_for_build_orders"`
	RequiredForSalesOrders  *int           `json:"required_for_sales_orders"`
	StockItemCount          *int           `json:"stock_item_count"`
	TotalInStock            *float64       `json:"total_in_stock"`
	ExternalStock           *float64       `json:"external_stock"`
	UnallocatedStock        *float64       `json:"unallocated_stock"`
	VariantStock            *float64       `json:"variant_stock"`
	Responsible             *int           `json:"responsible"`
	Tags                    []string       `json:"tags,omitempty"`
	// HasBarcode is computed by GetPartDetail from the upstream barcode_hash
	// value (non-empty means assigned) and has no OpenAPI counterpart of its
	// own -- see the field-inventory drift tests' web_url-style exclusion.
	HasBarcode bool `json:"has_barcode"`
}

type PartPage struct {
	Count   int
	Results []Part
	HasMore bool
}

type PartThumb struct {
	Image string `json:"image"`
}

type Category struct {
	WebLinkFields
	PK                    int        `json:"pk"`
	Name                  string     `json:"name"`
	Description           string     `json:"description"`
	DefaultLocation       *int       `json:"default_location"`
	DefaultKeywords       *string    `json:"default_keywords"`
	Level                 int        `json:"level"`
	Parent                *int       `json:"parent"`
	PartCount             *int       `json:"part_count"`
	Subcategories         *int       `json:"subcategories"`
	PathString            string     `json:"pathstring"`
	Starred               bool       `json:"starred"`
	Structural            bool       `json:"structural"`
	Icon                  *string    `json:"icon"`
	ParentDefaultLocation *int       `json:"parent_default_location"`
	Path                  []TreePath `json:"path,omitempty"`
}

type TreePath struct {
	PK   int    `json:"pk"`
	Name string `json:"name"`
}

type CategoryPage struct {
	Count   int
	Results []Category
	HasMore bool
}

type Company struct {
	WebLinkFields
	PK                int     `json:"pk"`
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	Currency          string  `json:"currency"`
	Image             *string `json:"image"`
	Active            bool    `json:"active"`
	IsSupplier        bool    `json:"is_supplier"`
	IsManufacturer    bool    `json:"is_manufacturer"`
	IsCustomer        bool    `json:"is_customer"`
	PartsSupplied     int     `json:"parts_supplied"`
	PartsManufactured int     `json:"parts_manufactured"`
}

type StockLocation struct {
	WebLinkFields
	PK                 int                `json:"pk"`
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	Parent             *int               `json:"parent"`
	PathString         string             `json:"pathstring"`
	Level              int                `json:"level"`
	Items              int                `json:"items"`
	Sublocations       int                `json:"sublocations"`
	Owner              *int               `json:"owner"`
	Icon               string             `json:"icon"`
	CustomIcon         *string            `json:"custom_icon"`
	Structural         bool               `json:"structural"`
	External           bool               `json:"external"`
	LocationType       *int               `json:"location_type"`
	LocationTypeDetail *StockLocationType `json:"location_type_detail,omitempty"`
	Path               []TreePath         `json:"path,omitempty"`
	Tags               []string           `json:"tags,omitempty"`
	// HasBarcode is computed by GetStockLocation from the upstream
	// barcode_hash value and has no OpenAPI counterpart -- see the
	// field-inventory drift tests' web_url-style exclusion.
	HasBarcode bool `json:"has_barcode"`
}

type StockLocationPage struct {
	Count   int
	Results []StockLocation
	HasMore bool
}

type StockLocationType struct {
	PK            int    `json:"pk"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Icon          string `json:"icon"`
	LocationCount *int   `json:"location_count"`
}

type StockLocationTypePage struct {
	Count   int
	Results []StockLocationType
	HasMore bool
}

// Owner projects InvenTree's read-only "Owner" model, which represents
// either a user or a group. The serializer intentionally exposes no
// email, phone, address, or tax identifiers, so this projection is
// privacy-safe as-is; owner_model distinguishes a user from a group.
type Owner struct {
	PK         int    `json:"pk"`
	OwnerID    int    `json:"owner_id"`
	OwnerModel string `json:"owner_model"`
	Name       string `json:"name"`
	Label      string `json:"label"`
}

// User projects InvenTree's `/api/user/` account model. Per the F-S104
// operator decision, this is an intentionally narrow safe-identity
// projection: email, groups, permissions, and profile detail are never
// decoded here even though the upstream response carries them. is_staff and
// is_superuser are query-side filters only (see UserQuery) and are
// deliberately absent from this struct so they can never leak into a tool
// projection.
type User struct {
	PK        int    `json:"pk"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	IsActive  bool   `json:"is_active"`
}

// UserPage wraps a bounded page of User search results with a
// caller-facing HasMore flag, matching the PartPage/CategoryPage precedent.
type UserPage struct {
	Count   int
	Results []User
	HasMore bool
}

// Contact projects InvenTree's structured company Contact model. Per the
// F-S49 operator decision, phone and email are intentionally absent from
// every MCP projection (search results and exact reads alike) so contact PII
// never reaches agent context; only enough identity remains (name, role, and
// the owning company) to select the correct record for reference assignment.
type Contact struct {
	PK          int    `json:"pk"`
	Company     int    `json:"company"`
	CompanyName string `json:"company_name"`
	Name        string `json:"name"`
	Role        string `json:"role"`
}

// Address projects InvenTree's structured company Address model. Per the
// F-S49 operator decision, the street-address lines (line1/line2) and postal
// code are intentionally absent from every MCP projection so precise
// physical-address PII never reaches agent context; city/region, country,
// and shipping notes remain to disambiguate between a company's addresses.
type Address struct {
	PK                    int    `json:"pk"`
	Company               int    `json:"company"`
	Title                 string `json:"title"`
	Primary               bool   `json:"primary"`
	PostalCity            string `json:"postal_city"`
	Province              string `json:"province"`
	Country               string `json:"country"`
	ShippingNotes         string `json:"shipping_notes"`
	InternalShippingNotes string `json:"internal_shipping_notes"`
	Link                  string `json:"link"`
}

// ProjectCode projects InvenTree's ProjectCode model. Per the F-S48 operator
// decision, ProjectCode was excluded from the owner/responsible object
// matrix, so its own `responsible`/`responsible_detail` fields stay out of
// scope here too; see ProjectCodeFieldInventory for the pinned
// classification.
type ProjectCode struct {
	PK          int    `json:"pk"`
	Code        string `json:"code"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

type StockItem struct {
	WebLinkFields
	PK                     int            `json:"pk"`
	Part                   int            `json:"part"`
	Location               *int           `json:"location"`
	Quantity               float64        `json:"quantity"`
	Serial                 *string        `json:"serial"`
	Batch                  *string        `json:"batch"`
	ExpiryDate             *string        `json:"expiry_date"`
	Packaging              *string        `json:"packaging"`
	Notes                  *string        `json:"notes"`
	Link                   string         `json:"link"`
	Status                 int            `json:"status"`
	StatusText             *string        `json:"status_text"`
	StatusCustomKey        *int           `json:"status_custom_key"`
	DeleteOnDeplete        bool           `json:"delete_on_deplete"`
	InStock                bool           `json:"in_stock"`
	IsBuilding             bool           `json:"is_building"`
	Owner                  *int           `json:"owner"`
	SupplierPart           *int           `json:"supplier_part"`
	Build                  *int           `json:"build"`
	ConsumedBy             *int           `json:"consumed_by"`
	Customer               *int           `json:"customer"`
	SalesOrder             *int           `json:"sales_order"`
	BelongsTo              *int           `json:"belongs_to"`
	Parent                 *int           `json:"parent"`
	Allocated              *float64       `json:"allocated"`
	InstalledItems         *int           `json:"installed_items"`
	ChildItems             *int           `json:"child_items"`
	TrackingItems          *int           `json:"tracking_items"`
	PurchasePrice          *DecimalString `json:"purchase_price"`
	PurchasePriceCurrency  string         `json:"purchase_price_currency"`
	CreationDate           *string        `json:"creation_date"`
	StocktakeDate          *string        `json:"stocktake_date"`
	Updated                *string        `json:"updated"`
	PurchaseOrder          *int           `json:"purchase_order"`
	PurchaseOrderReference *string        `json:"purchase_order_reference"`
}

// StockItemDetail is the approved complete scalar projection returned by the
// exact stock-item endpoint. Nested location, part, and supplier-part detail
// remain separate lookups, and tests remain deferred; see
// StockItemFieldInventory for the pinned classification.
type StockItemDetail struct {
	WebLinkFields
	PK                     int            `json:"pk"`
	Part                   int            `json:"part"`
	Location               *int           `json:"location"`
	Quantity               float64        `json:"quantity"`
	Serial                 *string        `json:"serial"`
	Batch                  *string        `json:"batch"`
	ExpiryDate             *string        `json:"expiry_date"`
	Packaging              *string        `json:"packaging"`
	Notes                  *string        `json:"notes"`
	Link                   string         `json:"link"`
	Status                 int            `json:"status"`
	StatusText             *string        `json:"status_text"`
	StatusCustomKey        *int           `json:"status_custom_key"`
	DeleteOnDeplete        bool           `json:"delete_on_deplete"`
	InStock                bool           `json:"in_stock"`
	IsBuilding             bool           `json:"is_building"`
	Owner                  *int           `json:"owner"`
	SupplierPart           *int           `json:"supplier_part"`
	SKU                    *string        `json:"SKU"`
	MPN                    *string        `json:"MPN"`
	Build                  *int           `json:"build"`
	ConsumedBy             *int           `json:"consumed_by"`
	Customer               *int           `json:"customer"`
	SalesOrder             *int           `json:"sales_order"`
	SalesOrderReference    *string        `json:"sales_order_reference"`
	BelongsTo              *int           `json:"belongs_to"`
	Parent                 *int           `json:"parent"`
	Allocated              *float64       `json:"allocated"`
	Expired                *bool          `json:"expired"`
	Stale                  *bool          `json:"stale"`
	InstalledItems         *int           `json:"installed_items"`
	ChildItems             *int           `json:"child_items"`
	TrackingItems          *int           `json:"tracking_items"`
	PurchasePrice          *DecimalString `json:"purchase_price"`
	PurchasePriceCurrency  string         `json:"purchase_price_currency"`
	CreationDate           *string        `json:"creation_date"`
	StocktakeDate          *string        `json:"stocktake_date"`
	Updated                *string        `json:"updated"`
	PurchaseOrder          *int           `json:"purchase_order"`
	PurchaseOrderReference *string        `json:"purchase_order_reference"`
	LocationPath           []TreePath     `json:"location_path,omitempty"`
	Tags                   []string       `json:"tags,omitempty"`
	// HasBarcode is computed by GetStockItemDetail from the upstream
	// barcode_hash value and has no OpenAPI counterpart -- see the
	// field-inventory drift tests' web_url-style exclusion.
	HasBarcode bool `json:"has_barcode"`
}

// StockItemPage is a bounded single-request page over /api/stock/, used by
// dependency audits that only need an exact upstream count, not a full scan.
type StockItemPage struct {
	Count   int
	Results []StockItem
	HasMore bool
}

// SalesOrderSummary is a minimal read-only projection used solely to let
// company customer-role removal prove whether sales orders still reference
// the company as customer. It is not a general sales-order client surface.
type SalesOrderSummary struct {
	PK        int    `json:"pk"`
	Reference string `json:"reference"`
}

type SalesOrderPage struct {
	Count   int
	Results []SalesOrderSummary
	HasMore bool
}

type Parameter struct {
	WebLinkFields
	PK        int    `json:"pk"`
	Template  int    `json:"template"`
	ModelType string `json:"model_type"`
	ModelID   int    `json:"model_id"`
	Data      string `json:"data"`
}

// Tag is one row of InvenTree's shared cross-object tag taxonomy
// (/api/tag/). The same Name/PK is reused across every object type that
// references it; Slug is server-derived and read-only. Direct /api/tag/
// mutation (rename/delete) is staff-only and out of MCP scope.
type Tag struct {
	PK   int    `json:"pk"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type TagPage struct {
	Count   int
	Results []Tag
	HasMore bool
}

type PartParameterPage struct {
	Count   int
	Results []Parameter
	HasMore bool
}

// ParameterUniqueness mirrors docs/api-schema.yaml's UniqueEnum for
// ParameterTemplate.Unique: 0 means no uniqueness is enforced, 1 means values
// must be unique per model type, and 2 means values must be globally unique
// across every model type sharing the template.
type ParameterUniqueness int

const (
	ParameterUniquenessNone      ParameterUniqueness = 0
	ParameterUniquenessModelType ParameterUniqueness = 1
	ParameterUniquenessGlobal    ParameterUniqueness = 2
)

type ParameterTemplate struct {
	PK            int                 `json:"pk"`
	Name          string              `json:"name"`
	Units         *string             `json:"units"`
	Description   string              `json:"description"`
	ModelType     *string             `json:"model_type"`
	Choices       string              `json:"choices"`
	Checkbox      bool                `json:"checkbox"`
	SelectionList *int                `json:"selectionlist"`
	Enabled       bool                `json:"enabled"`
	Unique        ParameterUniqueness `json:"unique"`
}

type CategoryParameterTemplate struct {
	WebLinkFields
	PK             int                `json:"pk"`
	Category       int                `json:"category"`
	CategoryDetail *Category          `json:"category_detail"`
	Template       int                `json:"template"`
	TemplateDetail *ParameterTemplate `json:"template_detail"`
	DefaultValue   string             `json:"default_value"`
}

type Attachment struct {
	WebLinkFields
	PK           int      `json:"pk"`
	ModelType    string   `json:"model_type"`
	ModelID      int      `json:"model_id"`
	Attachment   *string  `json:"attachment"`
	Thumbnail    *string  `json:"thumbnail"`
	Filename     string   `json:"filename"`
	Link         *string  `json:"link"`
	Comment      string   `json:"comment"`
	IsImage      bool     `json:"is_image"`
	IsLink       bool     `json:"is_link"`
	FileSize     *int64   `json:"file_size"`
	Tags         []string `json:"tags"`
	UploadDate   string   `json:"upload_date"`
	UploadUser   *int     `json:"upload_user"`
	HasThumbnail bool     `json:"has_thumbnail"`
}

type SupplierPart struct {
	WebLinkFields
	PK                 int     `json:"pk"`
	Part               int     `json:"part"`
	Supplier           int     `json:"supplier"`
	SKU                string  `json:"SKU"`
	Description        string  `json:"description"`
	Active             bool    `json:"active"`
	Primary            bool    `json:"primary"`
	Packaging          *string `json:"packaging"`
	PackQuantityNative float64 `json:"pack_quantity_native"`
}

type ManufacturerPart struct {
	WebLinkFields
	PK           int    `json:"pk"`
	Part         int    `json:"part"`
	Manufacturer int    `json:"manufacturer"`
	MPN          string `json:"MPN"`
	Description  string `json:"description"`
}

// CompanyDetail is the approved complete scalar projection returned by the
// exact company endpoint. API 530 `primary_address` remains a separate
// structured-address lookup, and `parameters` remains deferred to its own
// dedicated story; see CompanyFieldInventory for the pinned classification.
// Deferred/separate-lookup/write-only fields have no corresponding Go field
// so json.Unmarshal silently drops them and they never appear on re-marshal.
type CompanyDetail struct {
	Company
	Website string   `json:"website"`
	Phone   string   `json:"phone"`
	Email   *string  `json:"email"`
	Contact string   `json:"contact"`
	Link    string   `json:"link"`
	Notes   *string  `json:"notes"`
	TaxID   string   `json:"tax_id"`
	Tags    []string `json:"tags,omitempty"`
}

type SupplierPartDetail struct {
	WebLinkFields
	PK                  int      `json:"pk"`
	Part                int      `json:"part"`
	Supplier            int      `json:"supplier"`
	SKU                 string   `json:"SKU"`
	Description         *string  `json:"description"`
	Link                *string  `json:"link"`
	Active              bool     `json:"active"`
	Primary             bool     `json:"primary"`
	ManufacturerPart    *int     `json:"manufacturer_part"`
	MPN                 *string  `json:"MPN"`
	Packaging           *string  `json:"packaging"`
	PackQuantity        string   `json:"pack_quantity"`
	PackQuantityNative  float64  `json:"pack_quantity_native"`
	Note                *string  `json:"note"`
	Notes               *string  `json:"notes"`
	Available           float64  `json:"available"`
	AvailabilityUpdated *string  `json:"availability_updated"`
	InStock             *float64 `json:"in_stock"`
	OnOrder             *float64 `json:"on_order"`
	Updated             *string  `json:"updated"`
	Tags                []string `json:"tags,omitempty"`
}

type ManufacturerPartDetail struct {
	WebLinkFields
	PK           int      `json:"pk"`
	Part         int      `json:"part"`
	Manufacturer int      `json:"manufacturer"`
	MPN          *string  `json:"MPN"`
	Description  *string  `json:"description"`
	Link         *string  `json:"link"`
	Notes        *string  `json:"notes"`
	Tags         []string `json:"tags,omitempty"`
}

type CompanyPage struct {
	Count   int
	Results []CompanyDetail
	HasMore bool
}

type SupplierPartPage struct {
	Count   int
	Results []SupplierPartDetail
	HasMore bool
}

type ManufacturerPartPage struct {
	Count   int
	Results []ManufacturerPartDetail
	HasMore bool
}

// CurrentUser is the stable identity subset returned by /api/user/me/ and
// needed to bind connector OAuth tokens to an InvenTree account.
type CurrentUser struct {
	PK       int    `json:"pk"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// UserToken is the one-time secret response returned when InvenTree creates
// or rotates a named current-user API token.
type UserToken struct {
	Token  string `json:"token"`
	Name   string `json:"name"`
	Expiry string `json:"expiry"`
}

type PurchaseOrder struct {
	WebLinkFields
	PK                int            `json:"pk"`
	Reference         string         `json:"reference"`
	Supplier          int            `json:"supplier"`
	SupplierReference string         `json:"supplier_reference"`
	Description       string         `json:"description"`
	CreationDate      *string        `json:"creation_date"`
	StartDate         *string        `json:"start_date"`
	TargetDate        *string        `json:"target_date"`
	OrderCurrency     *string        `json:"order_currency"`
	Destination       *int           `json:"destination"`
	Status            int            `json:"status"`
	TotalPrice        *DecimalString `json:"total_price"`
}

// PurchaseOrderPage is a bounded single-request page over /api/order/po/.
// It is used by dependency audits that must prove complete coverage without
// loading an unbounded collection.
type PurchaseOrderPage struct {
	Count   int
	Results []PurchaseOrder
	HasMore bool
}

// PurchaseOrderCreatedBy projects the nested API 530 order.created_by User
// object to its stable creator user ID at the MCP boundary. The wire format
// decodes a full User object, but only the ID is exposed; unmarshalling into
// this narrow type means the caller's username/email are never retained.
type PurchaseOrderCreatedBy struct {
	PK int
}

func (value *PurchaseOrderCreatedBy) UnmarshalJSON(data []byte) error {
	var wire struct {
		PK int `json:"pk"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	value.PK = wire.PK
	return nil
}

func (value PurchaseOrderCreatedBy) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.PK)
}

// PurchaseOrderDetail is the approved complete scalar projection returned by
// the exact purchase-order endpoint. Embedded/nested records and deferred
// fields are intentionally omitted; see PurchaseOrderFieldInventory for the
// pinned classification.
type PurchaseOrderDetail struct {
	PurchaseOrder
	CreatedBy        PurchaseOrderCreatedBy `json:"created_by"`
	IssueDate        *string                `json:"issue_date"`
	LineItems        *int                   `json:"line_items"`
	CompletedLines   *int                   `json:"completed_lines"`
	Link             string                 `json:"link"`
	StatusText       *string                `json:"status_text"`
	StatusCustomKey  *int                   `json:"status_custom_key"`
	Notes            *string                `json:"notes"`
	Overdue          *bool                  `json:"overdue"`
	CompleteDate     *string                `json:"complete_date"`
	SupplierName     string                 `json:"supplier_name"`
	UpdatedAt        *string                `json:"updated_at"`
	Responsible      *int                   `json:"responsible"`
	Contact          *int                   `json:"contact"`
	Address          *int                   `json:"address"`
	ProjectCode      *int                   `json:"project_code"`
	ProjectCodeLabel string                 `json:"project_code_label"`
	Tags             []string               `json:"tags,omitempty"`
	// HasBarcode is computed by GetPurchaseOrderDetail from the upstream
	// barcode_hash value and has no OpenAPI counterpart -- see the
	// field-inventory drift tests' web_url-style exclusion.
	HasBarcode bool `json:"has_barcode"`
}

type PurchaseOrderLineItem struct {
	WebLinkFields
	PK                    int            `json:"pk"`
	Order                 int            `json:"order"`
	Part                  int            `json:"part"`
	InternalPart          *int           `json:"internal_part"`
	Destination           *int           `json:"destination"`
	Line                  string         `json:"line"`
	Reference             string         `json:"reference"`
	Notes                 string         `json:"notes"`
	Quantity              float64        `json:"quantity"`
	Received              float64        `json:"received"`
	TargetDate            *string        `json:"target_date"`
	PurchasePrice         *DecimalString `json:"purchase_price"`
	PurchasePriceCurrency string         `json:"purchase_price_currency"`
	Link                  string         `json:"link"`
	Discount              float64        `json:"discount"`
	ProjectCode           *int           `json:"project_code"`
	ProjectCodeLabel      string         `json:"project_code_label"`
}

// PurchaseOrderLinePage is a bounded single-request page over /api/order/po-line/.
type PurchaseOrderLinePage struct {
	Count   int
	Results []PurchaseOrderLineItem
	HasMore bool
}

// PurchaseOrderLineItemDetail is the approved complete scalar projection
// returned by the exact purchase-order-line endpoint. Nested order, part, and
// supplier-part detail remain separate exact lookups; see
// PurchaseOrderLineFieldInventory for the pinned classification.
type PurchaseOrderLineItemDetail struct {
	PurchaseOrderLineItem
	BuildOrder       *int           `json:"build_order"`
	Overdue          *bool          `json:"overdue"`
	AutoPricing      bool           `json:"auto_pricing"`
	SKU              *string        `json:"sku"`
	MPN              *string        `json:"mpn"`
	IPN              *string        `json:"ipn"`
	InternalPartName string         `json:"internal_part_name"`
	TotalPrice       *DecimalString `json:"total_price"`
}

type PurchaseOrderExtraLine struct {
	WebLinkFields
	PK               int            `json:"pk"`
	Order            int            `json:"order"`
	Line             string         `json:"line"`
	Reference        string         `json:"reference"`
	Description      string         `json:"description"`
	Link             string         `json:"link"`
	Notes            string         `json:"notes"`
	Quantity         float64        `json:"quantity"`
	Price            *DecimalString `json:"price"`
	PriceCurrency    string         `json:"price_currency"`
	TargetDate       *string        `json:"target_date"`
	Discount         float64        `json:"discount"`
	TotalPrice       *DecimalString `json:"total_price"`
	ProjectCode      *int           `json:"project_code"`
	ProjectCodeLabel string         `json:"project_code_label"`
}

// BomItem is read-only in this client: it exists solely to let delete_part
// detect whether a part is used in a bill of materials, either as the
// assembly (Part) or as a component of another assembly (SubPart).
type BomItem struct {
	PK       int     `json:"pk"`
	Part     int     `json:"part"`
	SubPart  int     `json:"sub_part"`
	Quantity float64 `json:"quantity"`
}

// SalesOrderLineItem is read-only in this client: it exists solely to let
// delete_part detect whether a part is referenced by a sales order.
type SalesOrderLineItem struct {
	PK       int     `json:"pk"`
	Order    int     `json:"order"`
	Part     *int    `json:"part"`
	Quantity float64 `json:"quantity"`
}

// Build is read-only in this client: it exists solely to let delete_part
// detect whether a part is the top-level assembly of a build order.
type Build struct {
	PK          int    `json:"pk"`
	Part        int    `json:"part"`
	Reference   string `json:"reference"`
	Status      int    `json:"status"`
	TakeFrom    *int   `json:"take_from"`
	Destination *int   `json:"destination"`
}

type BuildPage struct {
	Count   int
	Results []Build
	HasMore bool
}

type TransferOrder struct {
	PK          int    `json:"pk"`
	Reference   string `json:"reference"`
	TakeFrom    *int   `json:"take_from"`
	Destination *int   `json:"destination"`
}

type TransferOrderPage struct {
	Count   int
	Results []TransferOrder
	HasMore bool
}

// PartRelation is an undirected link between two stable part IDs. InvenTree
// preserves the submitted endpoint order but treats the pair as bidirectional.
type PartRelation struct {
	PK    int    `json:"pk"`
	Part1 int    `json:"part_1"`
	Part2 int    `json:"part_2"`
	Note  string `json:"note"`
}

type PartRelationCreate struct {
	Part1 int    `json:"part_1"`
	Part2 int    `json:"part_2"`
	Note  string `json:"note,omitempty"`
}

// PartInternalPriceBreak is one quantity/price row for a Part's internal
// price. InvenTree enforces a unique (part, quantity) pair server-side.
type PartInternalPriceBreak struct {
	PK            int           `json:"pk"`
	Part          int           `json:"part"`
	Quantity      float64       `json:"quantity"`
	Price         DecimalString `json:"price"`
	PriceCurrency string        `json:"price_currency"`
}

type PartInternalPriceBreakCreate struct {
	Part          int     `json:"part"`
	Quantity      float64 `json:"quantity"`
	Price         string  `json:"price"`
	PriceCurrency string  `json:"price_currency"`
}

// PartSalePriceBreak is one quantity/price row for a Part's sale price.
// InvenTree only accepts a "part" id that has salable=true and enforces a
// unique (part, quantity) pair server-side.
type PartSalePriceBreak struct {
	PK            int           `json:"pk"`
	Part          int           `json:"part"`
	Quantity      float64       `json:"quantity"`
	Price         DecimalString `json:"price"`
	PriceCurrency string        `json:"price_currency"`
}

type PartSalePriceBreakCreate struct {
	Part          int     `json:"part"`
	Quantity      float64 `json:"quantity"`
	Price         string  `json:"price"`
	PriceCurrency string  `json:"price_currency"`
}

// SupplierPriceBreak is one quantity/price row for a SupplierPart. The
// upstream field named "part" is a SupplierPart primary key, not a Part
// primary key. Supplier and Updated are read-only, derived from the
// SupplierPart's own supplier.
type SupplierPriceBreak struct {
	PK            int           `json:"pk"`
	SupplierPart  int           `json:"part"`
	Quantity      float64       `json:"quantity"`
	Price         DecimalString `json:"price"`
	PriceCurrency string        `json:"price_currency"`
	Supplier      int           `json:"supplier"`
	Updated       *string       `json:"updated"`
}

type SupplierPriceBreakCreate struct {
	SupplierPart  int     `json:"part"`
	Quantity      float64 `json:"quantity"`
	Price         string  `json:"price"`
	PriceCurrency string  `json:"price_currency"`
}

// PartPricing is a Part's computed pricing snapshot. Every *_min/*_max field
// is read-only except OverrideMin/OverrideMax and their currencies. Update
// is a write-only trigger that is never populated on a read and is applied
// through RefreshPartPricing rather than through this struct.
type PartPricing struct {
	Currency            string         `json:"currency"`
	Updated             *string        `json:"updated"`
	ScheduledForUpdate  bool           `json:"scheduled_for_update"`
	BOMCostMin          *DecimalString `json:"bom_cost_min"`
	BOMCostMax          *DecimalString `json:"bom_cost_max"`
	PurchaseCostMin     *DecimalString `json:"purchase_cost_min"`
	PurchaseCostMax     *DecimalString `json:"purchase_cost_max"`
	InternalCostMin     *DecimalString `json:"internal_cost_min"`
	InternalCostMax     *DecimalString `json:"internal_cost_max"`
	SupplierPriceMin    *DecimalString `json:"supplier_price_min"`
	SupplierPriceMax    *DecimalString `json:"supplier_price_max"`
	VariantCostMin      *DecimalString `json:"variant_cost_min"`
	VariantCostMax      *DecimalString `json:"variant_cost_max"`
	OverrideMin         *DecimalString `json:"override_min"`
	OverrideMinCurrency string         `json:"override_min_currency"`
	OverrideMax         *DecimalString `json:"override_max"`
	OverrideMaxCurrency string         `json:"override_max_currency"`
	OverallMin          *DecimalString `json:"overall_min"`
	OverallMax          *DecimalString `json:"overall_max"`
	SalePriceMin        *DecimalString `json:"sale_price_min"`
	SalePriceMax        *DecimalString `json:"sale_price_max"`
	SaleHistoryMin      *DecimalString `json:"sale_history_min"`
	SaleHistoryMax      *DecimalString `json:"sale_history_max"`
}

// BarcodeMatch is ResolveBarcode's success projection: only the matched
// object's type/ID/web URL, never the nested "instance" record InvenTree
// embeds in a match response. ObjectType is one of the four in-scope bare
// object-type keys ("part", "stockitem", "stocklocation", "purchaseorder")
// on a supported match, or empty when the barcode matched an out-of-scope
// InvenTree object type (e.g. "build", "manufacturerpart") this server has
// no tool support for.
type BarcodeMatch struct {
	ObjectType string
	ObjectID   int
	WebURL     string
}

// BarcodeScanHistoryEntry is one /api/barcode/history/ row, allowlisted to
// the fields approved for MCP exposure. Context, Response, and UserDetail
// are deliberately absent: F-S99 excludes them from every tool output, and
// User stays a raw nullable ID rather than an expanded/embedded user object.
type BarcodeScanHistoryEntry struct {
	PK        int    `json:"pk"`
	Data      string `json:"data"`
	Timestamp string `json:"timestamp"`
	Endpoint  string `json:"endpoint"`
	Result    bool   `json:"result"`
	UserID    *int   `json:"user"`
}

// BarcodeScanHistoryPage is a bounded page over /api/barcode/history/,
// shaped like SearchTagsPage: HasMore is computed from the upstream Next
// cursor rather than surfacing the raw Next/Previous URLs.
type BarcodeScanHistoryPage struct {
	Count   int
	Results []BarcodeScanHistoryEntry
	HasMore bool
}
