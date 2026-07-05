package inventree

import (
	"context"
	"encoding/json"
	"fmt"
)

type PartCreate struct {
	Name            string  `json:"name"`
	Description     string  `json:"description,omitempty"`
	Category        *int    `json:"category,omitempty"`
	IPN             string  `json:"IPN,omitempty"`
	Units           *string `json:"units,omitempty"`
	Active          *bool   `json:"active,omitempty"`
	Assembly        *bool   `json:"assembly,omitempty"`
	Component       *bool   `json:"component,omitempty"`
	Purchaseable    *bool   `json:"purchaseable,omitempty"`
	Trackable       *bool   `json:"trackable,omitempty"`
	Virtual         *bool   `json:"virtual,omitempty"`
	DefaultLocation *int    `json:"default_location,omitempty"`
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

type SupplierPartCreate struct {
	Part             int     `json:"part"`
	Supplier         int     `json:"supplier"`
	SKU              string  `json:"SKU"`
	Description      *string `json:"description,omitempty"`
	Link             *string `json:"link,omitempty"`
	Active           *bool   `json:"active,omitempty"`
	Primary          *bool   `json:"primary,omitempty"`
	ManufacturerPart *int    `json:"manufacturer_part,omitempty"`
	Packaging        *string `json:"packaging,omitempty"`
	Note             *string `json:"note,omitempty"`
}

type ManufacturerPartCreate struct {
	Part         int     `json:"part"`
	Manufacturer int     `json:"manufacturer"`
	MPN          *string `json:"MPN,omitempty"`
	Description  *string `json:"description,omitempty"`
	Link         *string `json:"link,omitempty"`
}

type ParameterCreate struct {
	Template  int    `json:"template"`
	ModelType string `json:"model_type"`
	ModelID   int    `json:"model_id"`
	Data      string `json:"data"`
}

type StockItemCreate struct {
	Part     int     `json:"part"`
	Location int     `json:"location"`
	Quantity float64 `json:"quantity"`
	Status   *int    `json:"status,omitempty"`
	Batch    *string `json:"batch,omitempty"`
	Serial   *string `json:"serial,omitempty"`
	Notes    *string `json:"notes,omitempty"`
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

func (c *Client) CreateCompany(ctx context.Context, input CompanyCreate) (Company, error) {
	var out Company
	err := c.Post(ctx, "/api/company/", input, &out)
	return out, err
}

func (c *Client) CreateSupplierPart(ctx context.Context, input SupplierPartCreate) (SupplierPart, error) {
	var out SupplierPart
	err := c.Post(ctx, "/api/company/part/", input, &out)
	return out, err
}

func (c *Client) CreateManufacturerPart(ctx context.Context, input ManufacturerPartCreate) (ManufacturerPart, error) {
	var out ManufacturerPart
	err := c.Post(ctx, "/api/company/part/manufacturer/", input, &out)
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

func NewPartParameter(partID int, templateID int, data string) ParameterCreate {
	return ParameterCreate{
		Template:  templateID,
		ModelType: parameterModelTypePart,
		ModelID:   partID,
		Data:      data,
	}
}
