package inventree

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
)

type PartCreate struct {
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Category        *int     `json:"category,omitempty"`
	IPN             string   `json:"IPN,omitempty"`
	Units           *string  `json:"units,omitempty"`
	Active          *bool    `json:"active,omitempty"`
	Assembly        *bool    `json:"assembly,omitempty"`
	Component       *bool    `json:"component,omitempty"`
	Purchaseable    *bool    `json:"purchaseable,omitempty"`
	Trackable       *bool    `json:"trackable,omitempty"`
	Virtual         *bool    `json:"virtual,omitempty"`
	DefaultLocation *int     `json:"default_location,omitempty"`
	Consumable      *bool    `json:"consumable,omitempty"`
	DefaultExpiry   *int     `json:"default_expiry,omitempty"`
	IsTemplate      *bool    `json:"is_template,omitempty"`
	Keywords        *string  `json:"keywords,omitempty"`
	Link            *string  `json:"link,omitempty"`
	Locked          *bool    `json:"locked,omitempty"`
	MinimumStock    *float64 `json:"minimum_stock,omitempty"`
	MaximumStock    *float64 `json:"maximum_stock,omitempty"`
	Revision        *string  `json:"revision,omitempty"`
	Salable         *bool    `json:"salable,omitempty"`
	Testable        *bool    `json:"testable,omitempty"`
	Notes           *string  `json:"notes,omitempty"`
}

type CompanyCreate struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Currency       string `json:"currency"`
	Website        string `json:"website,omitempty"`
	Active         *bool  `json:"active,omitempty"`
	IsSupplier     bool   `json:"is_supplier,omitempty"`
	IsManufacturer bool   `json:"is_manufacturer,omitempty"`
}

type CategoryCreate struct {
	Name            string  `json:"name"`
	Description     *string `json:"description,omitempty"`
	DefaultLocation *int    `json:"default_location,omitempty"`
	DefaultKeywords *string `json:"default_keywords,omitempty"`
	Parent          *int    `json:"parent,omitempty"`
	Structural      *bool   `json:"structural,omitempty"`
	Icon            *string `json:"icon,omitempty"`
}

type SupplierPartCreate struct {
	Part             int      `json:"part"`
	Supplier         int      `json:"supplier"`
	SKU              string   `json:"SKU"`
	Description      *string  `json:"description,omitempty"`
	Link             *string  `json:"link,omitempty"`
	Active           *bool    `json:"active,omitempty"`
	Primary          *bool    `json:"primary,omitempty"`
	ManufacturerPart *int     `json:"manufacturer_part,omitempty"`
	Packaging        *string  `json:"packaging,omitempty"`
	Note             *string  `json:"note,omitempty"`
	Notes            *string  `json:"notes,omitempty"`
	Available        *float64 `json:"available,omitempty"`
}

type ManufacturerPartCreate struct {
	Part         int     `json:"part"`
	Manufacturer int     `json:"manufacturer"`
	MPN          *string `json:"MPN,omitempty"`
	Description  *string `json:"description,omitempty"`
	Link         *string `json:"link,omitempty"`
	Notes        *string `json:"notes,omitempty"`
}

type ParameterCreate struct {
	Template  int    `json:"template"`
	ModelType string `json:"model_type"`
	ModelID   int    `json:"model_id"`
	Data      string `json:"data"`
}

type ParameterTemplateCreate struct {
	Name          string               `json:"name"`
	Units         string               `json:"units"`
	Description   string               `json:"description"`
	ModelType     string               `json:"model_type"`
	Checkbox      bool                 `json:"checkbox"`
	Choices       string               `json:"choices"`
	SelectionList *int                 `json:"selectionlist,omitempty"`
	Enabled       bool                 `json:"enabled"`
	Unique        *ParameterUniqueness `json:"unique,omitempty"`
}

type CategoryParameterTemplateCreate struct {
	Category     int    `json:"category"`
	Template     int    `json:"template"`
	DefaultValue string `json:"default_value"`
}

type StockItemCreate struct {
	Part            int     `json:"part"`
	Location        int     `json:"location"`
	Quantity        float64 `json:"quantity"`
	Status          *int    `json:"status,omitempty"`
	Batch           *string `json:"batch,omitempty"`
	Serial          *string `json:"serial,omitempty"`
	Packaging       *string `json:"packaging,omitempty"`
	Notes           *string `json:"notes,omitempty"`
	DeleteOnDeplete *bool   `json:"delete_on_deplete,omitempty"`
}

type StockLocationCreate struct {
	Name         string  `json:"name"`
	Description  *string `json:"description,omitempty"`
	Parent       *int    `json:"parent,omitempty"`
	Owner        *int    `json:"owner,omitempty"`
	CustomIcon   *string `json:"custom_icon,omitempty"`
	Structural   *bool   `json:"structural,omitempty"`
	External     *bool   `json:"external,omitempty"`
	LocationType *int    `json:"location_type,omitempty"`
}

type StockLocationTypeCreate struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

type StockAdjustmentItem struct {
	PK       int    `json:"pk"`
	Quantity string `json:"quantity"`
}

type StockAdjustment struct {
	Items []StockAdjustmentItem `json:"items"`
	Notes string                `json:"notes"`
}

type StockTransfer struct {
	Items    []StockAdjustmentItem `json:"items"`
	Notes    string                `json:"notes"`
	Location int                   `json:"location"`
}

type StockStatusChange struct {
	Items  []int  `json:"items"`
	Status int    `json:"status"`
	Note   string `json:"note"`
}

// InstallStockItem mirrors docs/api-schema.yaml's InstallStockItem serializer
// for POST /api/stock/{id}/install/, where {id} is the parent stock item
// receiving the install and StockItem is the child being installed into it.
type InstallStockItem struct {
	StockItem int    `json:"stock_item"`
	Quantity  int    `json:"quantity,omitempty"`
	Note      string `json:"note,omitempty"`
}

// UninstallStockItem mirrors docs/api-schema.yaml's UninstallStockItem
// serializer for POST /api/stock/{id}/uninstall/, where {id} is the
// currently-installed child stock item being removed to Location.
type UninstallStockItem struct {
	Location int    `json:"location"`
	Note     string `json:"note,omitempty"`
}

type PurchaseOrderCreate struct {
	Reference         string  `json:"reference,omitempty"`
	Supplier          int     `json:"supplier"`
	SupplierReference *string `json:"supplier_reference,omitempty"`
	Description       *string `json:"description,omitempty"`
	CreationDate      *string `json:"creation_date,omitempty"`
	StartDate         *string `json:"start_date,omitempty"`
	TargetDate        *string `json:"target_date,omitempty"`
	OrderCurrency     *string `json:"order_currency,omitempty"`
	Destination       *int    `json:"destination,omitempty"`
}

type PurchaseOrderLineCreate struct {
	Order                 int     `json:"order"`
	SupplierPart          int     `json:"part"`
	AutoPricing           bool    `json:"auto_pricing"`
	MergeItems            bool    `json:"merge_items"`
	Line                  *string `json:"line,omitempty"`
	Reference             *string `json:"reference,omitempty"`
	Notes                 *string `json:"notes,omitempty"`
	Quantity              float64 `json:"quantity"`
	TargetDate            *string `json:"target_date,omitempty"`
	PurchasePrice         *string `json:"purchase_price,omitempty"`
	PurchasePriceCurrency *string `json:"purchase_price_currency,omitempty"`
	Destination           *int    `json:"destination,omitempty"`
}

type PurchaseOrderExtraLineCreate struct {
	Order         int     `json:"order"`
	Line          *string `json:"line,omitempty"`
	Reference     string  `json:"reference"`
	Description   *string `json:"description,omitempty"`
	Link          *string `json:"link,omitempty"`
	Notes         *string `json:"notes,omitempty"`
	Quantity      float64 `json:"quantity"`
	Price         *string `json:"price,omitempty"`
	PriceCurrency *string `json:"price_currency,omitempty"`
	TargetDate    *string `json:"target_date,omitempty"`
}

type PurchaseOrderReceive struct {
	Items    []PurchaseOrderReceiveItem `json:"items"`
	Location *int                       `json:"location,omitempty"`
}

type PurchaseOrderComplete struct {
	AcceptIncomplete bool `json:"accept_incomplete"`
}

type PurchaseOrderReceiveItem struct {
	LineItem      int     `json:"line_item"`
	Location      *int    `json:"location,omitempty"`
	Quantity      string  `json:"quantity"`
	BatchCode     *string `json:"batch_code,omitempty"`
	ExpiryDate    *string `json:"expiry_date,omitempty"`
	SerialNumbers *string `json:"serial_numbers,omitempty"`
	Status        *int    `json:"status,omitempty"`
	Packaging     *string `json:"packaging,omitempty"`
	Note          *string `json:"note,omitempty"`
	Barcode       *string `json:"barcode,omitempty"`
}

type AttachmentCreate struct {
	ModelType   string
	ModelID     int
	Filename    string
	ContentType string
	Content     []byte
	Link        string
	Comment     *string
	Tags        []string
}

type PartPrimaryImageCreate struct {
	Filename    string
	ContentType string
	Content     []byte
}

type CompanyPrimaryImageCreate struct {
	Filename    string
	ContentType string
	Content     []byte
}

func (c *Client) CreatePart(ctx context.Context, input PartCreate) (Part, error) {
	var out Part
	err := c.Post(ctx, "/api/part/", input, &out)
	return out, err
}

func (c *Client) UpdatePart(ctx context.Context, id int, fields PatchFields) (Part, error) {
	var out Part
	err := c.Patch(ctx, fmt.Sprintf("/api/part/%d/", id), fields, &out)
	return out, err
}

func (c *Client) DeletePart(ctx context.Context, id int) error {
	req, err := c.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/part/%d/", id), nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, nil)
}

func (c *Client) CreatePartRelation(ctx context.Context, input PartRelationCreate) (PartRelation, error) {
	var out PartRelation
	err := c.Post(ctx, "/api/part/related/", input, &out)
	return out, err
}

func (c *Client) UpdatePartRelation(ctx context.Context, id int, fields PatchFields) (PartRelation, error) {
	var out PartRelation
	err := c.Patch(ctx, fmt.Sprintf("/api/part/related/%d/", id), fields, &out)
	return out, err
}

func (c *Client) DeletePartRelation(ctx context.Context, id int) error {
	req, err := c.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/part/related/%d/", id), nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, nil)
}

func (c *Client) CreatePartCategory(ctx context.Context, input CategoryCreate) (Category, error) {
	var out Category
	err := c.Post(ctx, "/api/part/category/", input, &out)
	return out, err
}

func (c *Client) UpdatePartCategory(ctx context.Context, id int, fields PatchFields) (Category, error) {
	var out Category
	err := c.Patch(ctx, fmt.Sprintf("/api/part/category/%d/", id), fields, &out)
	return out, err
}

func (c *Client) CreateCompany(ctx context.Context, input CompanyCreate) (Company, error) {
	var out Company
	err := c.Post(ctx, "/api/company/", input, &out)
	return out, err
}

func (c *Client) UpdateCompany(ctx context.Context, id int, fields PatchFields) (CompanyDetail, error) {
	var out CompanyDetail
	err := c.Patch(ctx, fmt.Sprintf("/api/company/%d/", id), fields, &out)
	return out, err
}

func (c *Client) CreateSupplierPart(ctx context.Context, input SupplierPartCreate) (SupplierPart, error) {
	var out SupplierPart
	err := c.Post(ctx, "/api/company/part/", input, &out)
	return out, err
}

func (c *Client) UpdateSupplierPart(ctx context.Context, id int, fields PatchFields) (SupplierPartDetail, error) {
	var out SupplierPartDetail
	err := c.Patch(ctx, fmt.Sprintf("/api/company/part/%d/", id), fields, &out)
	return out, err
}

func (c *Client) CreateManufacturerPart(ctx context.Context, input ManufacturerPartCreate) (ManufacturerPart, error) {
	var out ManufacturerPart
	err := c.Post(ctx, "/api/company/part/manufacturer/", input, &out)
	return out, err
}

func (c *Client) UpdateManufacturerPart(ctx context.Context, id int, fields PatchFields) (ManufacturerPartDetail, error) {
	var out ManufacturerPartDetail
	err := c.Patch(ctx, fmt.Sprintf("/api/company/part/manufacturer/%d/", id), fields, &out)
	return out, err
}

func (c *Client) CreatePartParameter(ctx context.Context, input ParameterCreate) (Parameter, error) {
	var out Parameter
	err := c.Post(ctx, "/api/parameter/", input, &out)
	return out, err
}

func (c *Client) UpdatePartParameter(ctx context.Context, id int, fields PatchFields) (Parameter, error) {
	var out Parameter
	err := c.Patch(ctx, fmt.Sprintf("/api/parameter/%d/", id), fields, &out)
	return out, err
}

func (c *Client) DeletePartParameter(ctx context.Context, id int) error {
	req, err := c.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/parameter/%d/", id), nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, nil)
}

func (c *Client) CreateParameterTemplate(ctx context.Context, input ParameterTemplateCreate) (ParameterTemplate, error) {
	var out ParameterTemplate
	err := c.Post(ctx, "/api/parameter/template/", input, &out)
	return out, err
}

func (c *Client) UpdateParameterTemplate(ctx context.Context, id int, fields PatchFields) (ParameterTemplate, error) {
	var out ParameterTemplate
	err := c.Patch(ctx, fmt.Sprintf("/api/parameter/template/%d/", id), fields, &out)
	return out, err
}

func (c *Client) DeleteParameterTemplate(ctx context.Context, id int) error {
	req, err := c.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/parameter/template/%d/", id), nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, nil)
}

func (c *Client) CreateCategoryParameterTemplate(ctx context.Context, input CategoryParameterTemplateCreate) (CategoryParameterTemplate, error) {
	var out CategoryParameterTemplate
	err := c.Post(ctx, "/api/part/category/parameters/", input, &out)
	return out, err
}

func (c *Client) UpdateCategoryParameterTemplate(ctx context.Context, id int, fields PatchFields) (CategoryParameterTemplate, error) {
	var out CategoryParameterTemplate
	err := c.Patch(ctx, fmt.Sprintf("/api/part/category/parameters/%d/", id), fields, &out)
	return out, err
}

func (c *Client) DeleteCategoryParameterTemplate(ctx context.Context, id int) error {
	req, err := c.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/part/category/parameters/%d/", id), nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, nil)
}

func (c *Client) CreateStockItem(ctx context.Context, input StockItemCreate) (StockItem, error) {
	var raw json.RawMessage
	if err := c.Post(ctx, "/api/stock/", input, &raw); err != nil {
		return StockItem{}, err
	}

	var out StockItem
	if err := json.Unmarshal(raw, &out); err == nil {
		return out, nil
	}

	var batch []StockItem
	if err := json.Unmarshal(raw, &batch); err != nil {
		return StockItem{}, err
	}
	if len(batch) == 0 {
		return StockItem{}, fmt.Errorf("InvenTree stock create returned no stock items")
	}
	return batch[0], nil
}

func (c *Client) UpdateStockItem(ctx context.Context, id int, fields PatchFields) (StockItem, error) {
	var out StockItem
	err := c.Patch(ctx, fmt.Sprintf("/api/stock/%d/", id), fields, &out)
	return out, err
}

func (c *Client) CreateStockLocation(ctx context.Context, input StockLocationCreate) (StockLocation, error) {
	var out StockLocation
	err := c.Post(ctx, "/api/stock/location/", input, &out)
	return out, err
}

func (c *Client) UpdateStockLocation(ctx context.Context, id int, fields PatchFields) (StockLocation, error) {
	var out StockLocation
	err := c.Patch(ctx, fmt.Sprintf("/api/stock/location/%d/", id), fields, &out)
	return out, err
}

func (c *Client) CreateStockLocationType(ctx context.Context, input StockLocationTypeCreate) (StockLocationType, error) {
	var out StockLocationType
	err := c.Post(ctx, "/api/stock/location-type/", input, &out)
	return out, err
}

func (c *Client) UpdateStockLocationType(ctx context.Context, id int, fields PatchFields) (StockLocationType, error) {
	var out StockLocationType
	err := c.Patch(ctx, fmt.Sprintf("/api/stock/location-type/%d/", id), fields, &out)
	return out, err
}

func (c *Client) DeleteStockLocationType(ctx context.Context, id int) error {
	req, err := c.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/stock/location-type/%d/", id), nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, nil)
}

func (c *Client) AddStock(ctx context.Context, input StockAdjustment) error {
	return c.Post(ctx, "/api/stock/add/", input, nil)
}

func (c *Client) RemoveStock(ctx context.Context, input StockAdjustment) error {
	return c.Post(ctx, "/api/stock/remove/", input, nil)
}

func (c *Client) CountStock(ctx context.Context, input StockAdjustment) error {
	return c.Post(ctx, "/api/stock/count/", input, nil)
}

func (c *Client) TransferStock(ctx context.Context, input StockTransfer) error {
	return c.Post(ctx, "/api/stock/transfer/", input, nil)
}

func (c *Client) ChangeStockStatus(ctx context.Context, input StockStatusChange) error {
	return c.Post(ctx, "/api/stock/change_status/", input, nil)
}

// InstallStockItem's 201 response echoes the InstallStockItem request body
// (per docs/api-schema.yaml), not the resulting stock-item state, so callers
// must re-fetch the affected stock items to observe the applied change.
func (c *Client) InstallStockItem(ctx context.Context, parentID int, input InstallStockItem) error {
	return c.Post(ctx, fmt.Sprintf("/api/stock/%d/install/", parentID), input, nil)
}

// UninstallStockItem's 201 response echoes the UninstallStockItem request
// body (per docs/api-schema.yaml), not the resulting stock-item state, so
// callers must re-fetch the affected stock item to observe the applied
// change.
func (c *Client) UninstallStockItem(ctx context.Context, childID int, input UninstallStockItem) error {
	return c.Post(ctx, fmt.Sprintf("/api/stock/%d/uninstall/", childID), input, nil)
}

func (c *Client) CreatePurchaseOrder(ctx context.Context, input PurchaseOrderCreate) (PurchaseOrder, error) {
	var out PurchaseOrder
	err := c.Post(ctx, "/api/order/po/", input, &out)
	return out, err
}

func (c *Client) UpdatePurchaseOrder(ctx context.Context, id int, fields PatchFields) (PurchaseOrder, error) {
	var out PurchaseOrder
	err := c.Patch(ctx, fmt.Sprintf("/api/order/po/%d/", id), fields, &out)
	return out, err
}

func (c *Client) UpdatePurchaseOrderDetail(ctx context.Context, id int, fields PatchFields) (PurchaseOrderDetail, error) {
	var out PurchaseOrderDetail
	err := c.Patch(ctx, fmt.Sprintf("/api/order/po/%d/", id), fields, &out)
	return out, err
}

func (c *Client) CreatePurchaseOrderLine(ctx context.Context, input PurchaseOrderLineCreate) (PurchaseOrderLineItem, error) {
	var out PurchaseOrderLineItem
	err := c.Post(ctx, "/api/order/po-line/", input, &out)
	return out, err
}

func (c *Client) UpdatePurchaseOrderLine(ctx context.Context, id int, fields PatchFields) (PurchaseOrderLineItem, error) {
	var out PurchaseOrderLineItem
	err := c.Patch(ctx, fmt.Sprintf("/api/order/po-line/%d/", id), fields, &out)
	return out, err
}

func (c *Client) DeletePurchaseOrderLine(ctx context.Context, id int) error {
	req, err := c.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/order/po-line/%d/", id), nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, nil)
}

func (c *Client) CreatePurchaseOrderExtraLine(ctx context.Context, input PurchaseOrderExtraLineCreate) (PurchaseOrderExtraLine, error) {
	var out PurchaseOrderExtraLine
	err := c.Post(ctx, "/api/order/po-extra-line/", input, &out)
	return out, err
}

func (c *Client) UpdatePurchaseOrderExtraLine(ctx context.Context, id int, fields PatchFields) (PurchaseOrderExtraLine, error) {
	var out PurchaseOrderExtraLine
	err := c.Patch(ctx, fmt.Sprintf("/api/order/po-extra-line/%d/", id), fields, &out)
	return out, err
}

func (c *Client) DeletePurchaseOrderExtraLine(ctx context.Context, id int) error {
	req, err := c.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/order/po-extra-line/%d/", id), nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, nil)
}

func (c *Client) ReceivePurchaseOrder(ctx context.Context, id int, input PurchaseOrderReceive) ([]StockItem, error) {
	var out []StockItem
	err := c.Post(ctx, fmt.Sprintf("/api/order/po/%d/receive/", id), input, &out)
	return out, err
}

func (c *Client) CompletePurchaseOrder(ctx context.Context, id int) error {
	return c.Post(ctx, fmt.Sprintf("/api/order/po/%d/complete/", id), PurchaseOrderComplete{AcceptIncomplete: false}, nil)
}

func (c *Client) IssuePurchaseOrder(ctx context.Context, id int) error {
	return c.Post(ctx, fmt.Sprintf("/api/order/po/%d/issue/", id), struct{}{}, nil)
}

func (c *Client) UploadAttachment(ctx context.Context, input AttachmentCreate) (Attachment, error) {
	fields := map[string]string{
		"model_type": input.ModelType,
		"model_id":   strconv.Itoa(input.ModelID),
	}
	if input.Comment != nil {
		fields["comment"] = *input.Comment
	}
	for i, tag := range input.Tags {
		fields[fmt.Sprintf("tags[%d]", i)] = tag
	}
	var out Attachment
	err := c.postMultipart(ctx, "/api/attachment/", fields, multipartFile{
		fieldName:   "attachment",
		filename:    input.Filename,
		contentType: input.ContentType,
		content:     input.Content,
	}, &out)
	return out, err
}

func (c *Client) CreateLinkAttachment(ctx context.Context, input AttachmentCreate) (Attachment, error) {
	fields := map[string]string{
		"model_type": input.ModelType,
		"model_id":   strconv.Itoa(input.ModelID),
		"link":       input.Link,
	}
	if input.Comment != nil {
		fields["comment"] = *input.Comment
	}
	for i, tag := range input.Tags {
		fields[fmt.Sprintf("tags[%d]", i)] = tag
	}
	var out Attachment
	err := c.postMultipart(ctx, "/api/attachment/", fields, multipartFile{}, &out)
	return out, err
}

func (c *Client) UpdateAttachmentMetadata(ctx context.Context, id int, fields PatchFields) (Attachment, error) {
	var out Attachment
	err := c.Patch(ctx, fmt.Sprintf("/api/attachment/%d/", id), fields, &out)
	return out, err
}

func (c *Client) DeleteAttachment(ctx context.Context, id int) error {
	req, err := c.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/attachment/%d/", id), nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, nil)
}

func (c *Client) SetPartPrimaryImage(ctx context.Context, partID int, input PartPrimaryImageCreate) (Part, error) {
	var out Part
	err := c.patchMultipart(ctx, fmt.Sprintf("/api/part/%d/", partID), nil, multipartFile{
		fieldName:   "image",
		filename:    input.Filename,
		contentType: input.ContentType,
		content:     input.Content,
	}, &out)
	return out, err
}

func (c *Client) SetCompanyPrimaryImage(ctx context.Context, companyID int, input CompanyPrimaryImageCreate) (CompanyDetail, error) {
	var out CompanyDetail
	err := c.patchMultipart(ctx, fmt.Sprintf("/api/company/%d/", companyID), nil, multipartFile{
		fieldName:   "image",
		filename:    input.Filename,
		contentType: input.ContentType,
		content:     input.Content,
	}, &out)
	return out, err
}

func (c *Client) ClearCompanyPrimaryImage(ctx context.Context, companyID int) (CompanyDetail, error) {
	return c.UpdateCompany(ctx, companyID, PatchFields{"image": Null()})
}

func NewPartParameter(partID int, templateID int, data string) ParameterCreate {
	return ParameterCreate{
		Template:  templateID,
		ModelType: parameterModelTypePart,
		ModelID:   partID,
		Data:      data,
	}
}

type multipartFile struct {
	fieldName   string
	filename    string
	contentType string
	content     []byte
}

func (c *Client) postMultipart(ctx context.Context, path string, fields map[string]string, file multipartFile, out any) error {
	return c.doMultipart(ctx, http.MethodPost, path, fields, file, out)
}

func (c *Client) patchMultipart(ctx context.Context, path string, fields map[string]string, file multipartFile, out any) error {
	return c.doMultipart(ctx, http.MethodPatch, path, fields, file, out)
}

func (c *Client) doMultipart(ctx context.Context, method string, path string, fields map[string]string, file multipartFile, out any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return fmt.Errorf("encode multipart field %q: %w", key, err)
		}
	}
	if file.fieldName != "" {
		part, err := createMultipartFilePart(writer, file)
		if err != nil {
			return err
		}
		if _, err := io.Copy(part, bytes.NewReader(file.content)); err != nil {
			return fmt.Errorf("encode multipart file %q: %w", file.fieldName, err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := c.NewRequest(ctx, method, path, nil, nil)
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(&body)
	req.ContentLength = int64(body.Len())
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return c.DoJSON(req, out)
}

func createMultipartFilePart(writer *multipart.Writer, file multipartFile) (io.Writer, error) {
	if hasControlCharacter(file.filename) {
		return nil, fmt.Errorf("multipart filename contains control characters")
	}
	if hasControlCharacter(file.contentType) {
		return nil, fmt.Errorf("multipart content type contains control characters")
	}
	if file.contentType == "" {
		return writer.CreateFormFile(file.fieldName, file.filename)
	}
	if _, _, err := mime.ParseMediaType(file.contentType); err != nil {
		return nil, fmt.Errorf("multipart content type is invalid: %w", err)
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes(file.fieldName), escapeQuotes(file.filename)))
	header.Set("Content-Type", file.contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, fmt.Errorf("create multipart file %q: %w", file.fieldName, err)
	}
	return part, nil
}

func escapeQuotes(value string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace(value)
}

func hasControlCharacter(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) >= 0
}
