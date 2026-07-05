package inventree

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type AttachmentContentMode string

const (
	AttachmentContentOriginal  AttachmentContentMode = "original"
	AttachmentContentThumbnail AttachmentContentMode = "thumbnail"
)

const defaultAttachmentDownloadTimeout = 30 * time.Second

const parameterModelTypePart = "part.part"

var downloadAttachmentModelTypes = map[string]bool{
	"part":             true,
	"stockitem":        true,
	"company":          true,
	"manufacturerpart": true,
	"supplierpart":     true,
	"purchaseorder":    true,
}

type DownloadedAttachment struct {
	Attachment  Attachment
	Content     []byte
	ContentType string
	SourceURL   string
}

func (c *Client) SearchParts(ctx context.Context, query url.Values) ([]Part, error) {
	return listAll[Part](ctx, c, "/api/part/", query)
}

func (c *Client) GetPart(ctx context.Context, id int) (Part, error) {
	var out Part
	err := c.get(ctx, fmt.Sprintf("/api/part/%d/", id), &out)
	return out, err
}

func (c *Client) SearchPartCategories(ctx context.Context, query url.Values) ([]Category, error) {
	return listAll[Category](ctx, c, "/api/part/category/", query)
}

func (c *Client) GetPartCategory(ctx context.Context, id int) (Category, error) {
	var out Category
	err := c.get(ctx, fmt.Sprintf("/api/part/category/%d/", id), &out)
	return out, err
}

func (c *Client) SearchCompanies(ctx context.Context, query url.Values) ([]Company, error) {
	return listAll[Company](ctx, c, "/api/company/", query)
}

func (c *Client) SearchSuppliers(ctx context.Context, query url.Values) ([]Company, error) {
	return c.searchCompaniesWithRole(ctx, query, "is_supplier")
}

func (c *Client) SearchManufacturers(ctx context.Context, query url.Values) ([]Company, error) {
	return c.searchCompaniesWithRole(ctx, query, "is_manufacturer")
}

func (c *Client) SearchStockLocations(ctx context.Context, query url.Values) ([]StockLocation, error) {
	return listAll[StockLocation](ctx, c, "/api/stock/location/", query)
}

func (c *Client) GetStockLocation(ctx context.Context, id int) (StockLocation, error) {
	var out StockLocation
	err := c.get(ctx, fmt.Sprintf("/api/stock/location/%d/", id), &out)
	return out, err
}

func (c *Client) SearchStockItems(ctx context.Context, query url.Values) ([]StockItem, error) {
	return listAll[StockItem](ctx, c, "/api/stock/", query)
}

func (c *Client) SearchPartParameters(ctx context.Context, query url.Values) ([]Parameter, error) {
	nextQuery := cloneValues(query)
	if nextQuery == nil {
		nextQuery = url.Values{}
	}
	if partID := nextQuery.Get("part"); partID != "" && nextQuery.Get("model_id") == "" {
		nextQuery.Set("model_id", partID)
	}
	nextQuery.Del("part")
	nextQuery.Set("model_type", parameterModelTypePart)
	return listAll[Parameter](ctx, c, "/api/parameter/", nextQuery)
}

func (c *Client) SearchParameterTemplates(ctx context.Context, query url.Values) ([]ParameterTemplate, error) {
	return listAll[ParameterTemplate](ctx, c, "/api/parameter/template/", query)
}

func (c *Client) SearchCategoryParameterTemplates(ctx context.Context, query url.Values) ([]CategoryParameterTemplate, error) {
	nextQuery := cloneValues(query)
	var categoryFilter string
	if nextQuery != nil {
		categoryFilter = nextQuery.Get("category")
		nextQuery.Del("category")
	}
	records, err := listAll[CategoryParameterTemplate](ctx, c, "/api/part/category/parameters/", nextQuery)
	if err != nil || categoryFilter == "" {
		return records, err
	}
	filtered := records[:0]
	for _, record := range records {
		if fmt.Sprint(record.Category) == categoryFilter {
			filtered = append(filtered, record)
		}
	}
	return filtered, nil
}

func (c *Client) ListAttachments(ctx context.Context, query url.Values) ([]Attachment, error) {
	return listAll[Attachment](ctx, c, "/api/attachment/", query)
}

func (c *Client) GetAttachmentMetadata(ctx context.Context, id int) (Attachment, error) {
	var out Attachment
	err := c.get(ctx, fmt.Sprintf("/api/attachment/%d/", id), &out)
	return out, err
}

func (c *Client) DownloadAttachment(ctx context.Context, id int, mode AttachmentContentMode, maxBytes int64) (DownloadedAttachment, error) {
	if maxBytes <= 0 {
		return DownloadedAttachment{}, errors.New("attachment download maxBytes must be positive")
	}
	metadata, err := c.GetAttachmentMetadata(ctx, id)
	if err != nil {
		return DownloadedAttachment{}, err
	}
	if !downloadAttachmentModelTypes[metadata.ModelType] {
		return DownloadedAttachment{}, fmt.Errorf("attachment model type %q is out of scope", metadata.ModelType)
	}
	rawURL, err := attachmentContentURL(metadata, mode)
	if err != nil {
		return DownloadedAttachment{}, err
	}
	sourceURL, err := c.resolveInvenTreeContentURL(rawURL)
	if err != nil {
		return DownloadedAttachment{}, err
	}

	downloadCtx, cancel := boundedDownloadContext(ctx, c.httpClient)
	defer cancel()
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, sourceURL.String(), nil)
	if err != nil {
		return DownloadedAttachment{}, err
	}
	req.Header.Set("Accept", "*/*")
	c.credential.Apply(req)

	httpClient := noRedirectClient(c.httpClient)
	resp, err := httpClient.Do(req)
	if err != nil {
		return DownloadedAttachment{}, errors.New("download InvenTree attachment content failed")
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
			return DownloadedAttachment{}, fmt.Errorf("InvenTree attachment content redirected with status %d", resp.StatusCode)
		}
		return DownloadedAttachment{}, parseAPIError(resp)
	}
	content, err := readBounded(resp.Body, maxBytes)
	if err != nil {
		return DownloadedAttachment{}, err
	}
	return DownloadedAttachment{
		Attachment:  metadata,
		Content:     content,
		ContentType: resp.Header.Get("Content-Type"),
		SourceURL:   redactedURLString(sourceURL),
	}, nil
}

func (c *Client) SearchSupplierParts(ctx context.Context, query url.Values) ([]SupplierPart, error) {
	return listAll[SupplierPart](ctx, c, "/api/company/part/", query)
}

func (c *Client) SearchManufacturerParts(ctx context.Context, query url.Values) ([]ManufacturerPart, error) {
	return listAll[ManufacturerPart](ctx, c, "/api/company/part/manufacturer/", query)
}

func (c *Client) SearchPurchaseOrders(ctx context.Context, query url.Values) ([]PurchaseOrder, error) {
	return listAll[PurchaseOrder](ctx, c, "/api/order/po/", query)
}

func (c *Client) GetPurchaseOrder(ctx context.Context, id int) (PurchaseOrder, error) {
	var out PurchaseOrder
	err := c.get(ctx, fmt.Sprintf("/api/order/po/%d/", id), &out)
	return out, err
}

func (c *Client) SearchPurchaseOrderLines(ctx context.Context, query url.Values) ([]PurchaseOrderLineItem, error) {
	return listAll[PurchaseOrderLineItem](ctx, c, "/api/order/po-line/", query)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, out)
}

func (c *Client) searchCompaniesWithRole(ctx context.Context, query url.Values, roleFilter string) ([]Company, error) {
	nextQuery := cloneValues(query)
	if nextQuery == nil {
		nextQuery = url.Values{}
	}
	nextQuery.Set(roleFilter, "true")
	return c.SearchCompanies(ctx, nextQuery)
}

func listAll[T any](ctx context.Context, client *Client, path string, query url.Values) ([]T, error) {
	return ListAll[T](ctx, client, path, query)
}

func attachmentContentURL(metadata Attachment, mode AttachmentContentMode) (string, error) {
	switch mode {
	case "", AttachmentContentOriginal:
		if metadata.Attachment == nil || *metadata.Attachment == "" {
			return "", errors.New("attachment metadata has no file attachment URL")
		}
		return *metadata.Attachment, nil
	case AttachmentContentThumbnail:
		if metadata.Thumbnail == nil || *metadata.Thumbnail == "" {
			return "", errors.New("attachment metadata has no thumbnail URL")
		}
		return *metadata.Thumbnail, nil
	default:
		return "", fmt.Errorf("unsupported attachment content mode %q", mode)
	}
}

func (c *Client) resolveInvenTreeContentURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse attachment content URL: %w", err)
	}
	resolved := c.baseURL.ResolveReference(parsed)
	if resolved.Scheme != c.baseURL.Scheme || resolved.Host != c.baseURL.Host {
		return nil, errors.New("attachment content URL is outside configured InvenTree instance")
	}
	if resolved.User != nil {
		return nil, errors.New("attachment content URL must not include userinfo")
	}
	return resolved, nil
}

func boundedDownloadContext(ctx context.Context, client *http.Client) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	if client != nil && client.Timeout > 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultAttachmentDownloadTimeout)
}

func noRedirectClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &copy
}

func readBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read InvenTree attachment content: %w", err)
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("InvenTree attachment content exceeds maxBytes %d", maxBytes)
	}
	return content, nil
}

func redactedURLString(value *url.URL) string {
	copy := *value
	copy.User = nil
	copy.RawQuery = ""
	copy.Fragment = ""
	return copy.String()
}
