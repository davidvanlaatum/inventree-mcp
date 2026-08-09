package inventree

import "encoding/json"

const (
	PurchaseOrderStatusPending  = 10
	PurchaseOrderStatusPlaced   = 20
	PurchaseOrderStatusComplete = 30
)

// DecimalString preserves schema decimal values while accepting InvenTree
// responses that encode the same field as either a JSON string or number.
type DecimalString string

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

type PartPage struct {
	Count   int
	Results []Part
	HasMore bool
}

type PartThumb struct {
	Image string `json:"image"`
}

type Category struct {
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
	PK                 int                `json:"pk"`
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	Parent             *int               `json:"parent"`
	PathString         string             `json:"pathstring"`
	Level              int                `json:"level"`
	Items              int                `json:"items"`
	Sublocations       int                `json:"sublocations"`
	Owner              *int               `json:"owner"`
	CustomIcon         *string            `json:"custom_icon"`
	Structural         bool               `json:"structural"`
	External           bool               `json:"external"`
	LocationType       *int               `json:"location_type"`
	LocationTypeDetail *StockLocationType `json:"location_type_detail,omitempty"`
	Path               []TreePath         `json:"path,omitempty"`
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

type StockItem struct {
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
	LocationPath           []TreePath     `json:"location_path,omitempty"`
}

type Parameter struct {
	PK        int    `json:"pk"`
	Template  int    `json:"template"`
	ModelType string `json:"model_type"`
	ModelID   int    `json:"model_id"`
	Data      string `json:"data"`
}

type PartParameterPage struct {
	Count   int
	Results []Parameter
	HasMore bool
}

type ParameterTemplate struct {
	PK            int     `json:"pk"`
	Name          string  `json:"name"`
	Units         *string `json:"units"`
	Description   string  `json:"description"`
	ModelType     *string `json:"model_type"`
	Choices       string  `json:"choices"`
	Checkbox      bool    `json:"checkbox"`
	SelectionList *int    `json:"selectionlist"`
	Enabled       bool    `json:"enabled"`
}

type CategoryParameterTemplate struct {
	PK             int                `json:"pk"`
	Category       int                `json:"category"`
	CategoryDetail *Category          `json:"category_detail"`
	Template       int                `json:"template"`
	TemplateDetail *ParameterTemplate `json:"template_detail"`
	DefaultValue   string             `json:"default_value"`
}

type Attachment struct {
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
	PK           int    `json:"pk"`
	Part         int    `json:"part"`
	Manufacturer int    `json:"manufacturer"`
	MPN          string `json:"MPN"`
	Description  string `json:"description"`
}

type CompanyDetail struct {
	Company
	Website        string  `json:"website"`
	Phone          string  `json:"phone"`
	Email          *string `json:"email"`
	Contact        string  `json:"contact"`
	Link           string  `json:"link"`
	Notes          *string `json:"notes"`
	TaxID          string  `json:"tax_id"`
	PrimaryAddress any     `json:"primary_address"`
	Parameters     any     `json:"parameters"`
	Tags           any     `json:"tags"`
}

type SupplierPartDetail struct {
	PK                 int     `json:"pk"`
	Part               int     `json:"part"`
	Supplier           int     `json:"supplier"`
	SKU                string  `json:"SKU"`
	Description        *string `json:"description"`
	Active             bool    `json:"active"`
	Primary            bool    `json:"primary"`
	Packaging          *string `json:"packaging"`
	PackQuantityNative float64 `json:"pack_quantity_native"`
	Link               *string `json:"link"`
	ManufacturerPart   *int    `json:"manufacturer_part"`
	PackQuantity       string  `json:"pack_quantity"`
	Note               *string `json:"note"`
}

type ManufacturerPartDetail struct {
	PK           int     `json:"pk"`
	Part         int     `json:"part"`
	Manufacturer int     `json:"manufacturer"`
	MPN          *string `json:"MPN"`
	Description  *string `json:"description"`
	Link         *string `json:"link"`
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

type PurchaseOrderLineItem struct {
	PK                    int            `json:"pk"`
	Order                 int            `json:"order"`
	Part                  int            `json:"part"`
	SupplierPart          *int           `json:"supplier_part,omitempty"`
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
}

type PurchaseOrderExtraLine struct {
	PK            int            `json:"pk"`
	Order         int            `json:"order"`
	Line          string         `json:"line"`
	Reference     string         `json:"reference"`
	Description   string         `json:"description"`
	Link          string         `json:"link"`
	Notes         string         `json:"notes"`
	Quantity      float64        `json:"quantity"`
	Price         *DecimalString `json:"price"`
	PriceCurrency string         `json:"price_currency"`
	TargetDate    *string        `json:"target_date"`
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
	PK        int    `json:"pk"`
	Part      int    `json:"part"`
	Reference string `json:"reference"`
	Status    int    `json:"status"`
}

// PartRelation is read-only in this client: it exists solely to let
// delete_part detect and block on related-part links, since InvenTree
// itself does not protect a part from deletion while one exists.
type PartRelation struct {
	PK    int    `json:"pk"`
	Part1 int    `json:"part_1"`
	Part2 int    `json:"part_2"`
	Note  string `json:"note"`
}
