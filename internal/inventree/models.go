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
}

type PartThumb struct {
	Image string `json:"image"`
}

type Category struct {
	PK          int    `json:"pk"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Structural  bool   `json:"structural"`
}

type Company struct {
	PK             int    `json:"pk"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Currency       string `json:"currency"`
	Active         bool   `json:"active"`
	IsSupplier     bool   `json:"is_supplier"`
	IsManufacturer bool   `json:"is_manufacturer"`
	IsCustomer     bool   `json:"is_customer"`
}

type StockLocation struct {
	PK          int    `json:"pk"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Structural  bool   `json:"structural"`
	External    bool   `json:"external"`
}

type StockItem struct {
	PK                     int     `json:"pk"`
	Part                   int     `json:"part"`
	Location               *int    `json:"location"`
	Quantity               float64 `json:"quantity"`
	Serial                 *string `json:"serial"`
	Batch                  *string `json:"batch"`
	Packaging              *string `json:"packaging"`
	Notes                  *string `json:"notes"`
	Status                 int     `json:"status"`
	DeleteOnDeplete        bool    `json:"delete_on_deplete"`
	PurchaseOrder          *int    `json:"purchase_order"`
	PurchaseOrderReference *string `json:"purchase_order_reference"`
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
	PK       int     `json:"pk"`
	Name     string  `json:"name"`
	Units    *string `json:"units"`
	Choices  string  `json:"choices"`
	Checkbox bool    `json:"checkbox"`
	Enabled  bool    `json:"enabled"`
}

type CategoryParameterTemplate struct {
	PK           int    `json:"pk"`
	Category     int    `json:"category"`
	Template     int    `json:"template"`
	DefaultValue string `json:"default_value"`
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
	PK                int     `json:"pk"`
	Reference         string  `json:"reference"`
	Supplier          int     `json:"supplier"`
	SupplierReference string  `json:"supplier_reference"`
	Description       string  `json:"description"`
	CreationDate      *string `json:"creation_date"`
	StartDate         *string `json:"start_date"`
	TargetDate        *string `json:"target_date"`
	OrderCurrency     *string `json:"order_currency"`
	Destination       *int    `json:"destination"`
	Status            int     `json:"status"`
}

type PurchaseOrderLineItem struct {
	PK                    int            `json:"pk"`
	Order                 int            `json:"order"`
	Part                  int            `json:"part"`
	SupplierPart          *int           `json:"supplier_part,omitempty"`
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
