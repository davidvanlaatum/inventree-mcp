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

	req, err := c.NewRequest(ctx, http.MethodPost, path, nil, nil)
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
