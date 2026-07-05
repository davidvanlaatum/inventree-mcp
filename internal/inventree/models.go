package inventree

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
	PK       int     `json:"pk"`
	Part     int     `json:"part"`
	Location *int    `json:"location"`
	Quantity float64 `json:"quantity"`
	Serial   *string `json:"serial"`
	Batch    *string `json:"batch"`
	Notes    *string `json:"notes"`
	Status   int     `json:"status"`
}

type Parameter struct {
	PK        int    `json:"pk"`
	Template  int    `json:"template"`
	ModelType string `json:"model_type"`
	ModelID   int    `json:"model_id"`
	Data      string `json:"data"`
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
	PK          int    `json:"pk"`
	Part        int    `json:"part"`
	Supplier    int    `json:"supplier"`
	SKU         string `json:"SKU"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
	Primary     bool   `json:"primary"`
}

type ManufacturerPart struct {
	PK           int    `json:"pk"`
	Part         int    `json:"part"`
	Manufacturer int    `json:"manufacturer"`
	MPN          string `json:"MPN"`
	Description  string `json:"description"`
}

type PurchaseOrder struct {
	PK        int    `json:"pk"`
	Reference string `json:"reference"`
	Supplier  int    `json:"supplier"`
	Status    int    `json:"status"`
}

type PurchaseOrderLineItem struct {
	PK           int     `json:"pk"`
	Order        int     `json:"order"`
	Part         int     `json:"part"`
	SupplierPart *int    `json:"supplier_part"`
	Quantity     float64 `json:"quantity"`
}
