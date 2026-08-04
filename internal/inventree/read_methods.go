package inventree

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type AttachmentContentMode string

const (
	AttachmentContentOriginal  AttachmentContentMode = "original"
	AttachmentContentThumbnail AttachmentContentMode = "thumbnail"
)

const defaultAttachmentDownloadTimeout = 30 * time.Second

const parameterModelTypePart = "part.part"

var ErrPartImageMissing = errors.New("part has no primary image")

var downloadAttachmentModelTypes = map[string]bool{
	"part":             true,
	"stockitem":        true,
	"company":          true,
	"manufacturerpart": true,
	"supplierpart":     true,
	"purchaseorder":    true,
}

func (c *Client) GetCurrentUser(ctx context.Context) (CurrentUser, error) {
	var out CurrentUser
	err := c.get(ctx, "/api/user/me/", &out)
	return out, err
}

func (c *Client) CreateCurrentUserToken(ctx context.Context, name string) (UserToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return UserToken{}, errors.New("current-user token name is required")
	}
	var out UserToken
	req, err := c.NewRequest(ctx, http.MethodGet, "/api/user/me/token/", url.Values{"name": []string{name}}, nil)
	if err == nil {
		err = c.DoJSON(req, &out)
	}
	if err == nil && out.Token == "" {
		return UserToken{}, errors.New("InvenTree did not return the newly created token secret")
	}
	return out, err
}

type DownloadedAttachment struct {
	Attachment  Attachment
	Content     []byte
	ContentType string
	SourceURL   string
}

type DownloadedPartImage struct {
	Part        Part
	Content     []byte
	Filename    string
	ContentType string
	SourceURL   string
}

func (c *Client) SearchParts(ctx context.Context, query SearchQuery) ([]Part, error) {
	return listAll[Part](ctx, c, "/api/part/", query.values())
}

func (c *Client) SearchPartsPage(ctx context.Context, query PartQuery) (PartPage, error) {
	page, err := listPage[Part](ctx, c, "/api/part/", query.values())
	return PartPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil && *page.Next != ""}, err
}

func (c *Client) GetPart(ctx context.Context, id int) (Part, error) {
	var out Part
	err := c.get(ctx, fmt.Sprintf("/api/part/%d/", id), &out)
	return out, err
}

func (c *Client) SearchPartCategories(ctx context.Context, query SearchQuery) ([]Category, error) {
	return listAll[Category](ctx, c, "/api/part/category/", query.values())
}

func (c *Client) SearchPartCategoriesPage(ctx context.Context, query CategoryQuery) (CategoryPage, error) {
	page, err := listPage[Category](ctx, c, "/api/part/category/", query.values())
	return CategoryPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil && *page.Next != ""}, err
}

func (c *Client) GetPartCategory(ctx context.Context, id int) (Category, error) {
	var out Category
	req, err := c.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/part/category/%d/", id), url.Values{"path_detail": []string{"true"}}, nil)
	if err == nil {
		err = c.DoJSON(req, &out)
	}
	return out, err
}

func (c *Client) SearchCompanies(ctx context.Context, query SearchQuery) ([]Company, error) {
	return listAll[Company](ctx, c, "/api/company/", query.values())
}

func (c *Client) SearchCompaniesPage(ctx context.Context, query SearchQuery) (CompanyPage, error) {
	page, err := listPage[CompanyDetail](ctx, c, "/api/company/", query.values())
	return CompanyPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil && *page.Next != ""}, err
}

func (c *Client) GetCompanyDetail(ctx context.Context, id int) (CompanyDetail, error) {
	var out CompanyDetail
	err := c.get(ctx, fmt.Sprintf("/api/company/%d/", id), &out)
	return out, err
}

func (c *Client) GetCompany(ctx context.Context, id int) (Company, error) {
	var out Company
	err := c.get(ctx, fmt.Sprintf("/api/company/%d/", id), &out)
	return out, err
}

func (c *Client) SearchSuppliers(ctx context.Context, query SearchQuery) ([]Company, error) {
	return c.searchCompaniesWithRole(ctx, query, "is_supplier")
}

func (c *Client) SearchManufacturers(ctx context.Context, query SearchQuery) ([]Company, error) {
	return c.searchCompaniesWithRole(ctx, query, "is_manufacturer")
}

func (c *Client) SearchStockLocations(ctx context.Context, query SearchQuery) ([]StockLocation, error) {
	return listAll[StockLocation](ctx, c, "/api/stock/location/", query.values())
}

func (c *Client) SearchStockLocationsPage(ctx context.Context, query StockLocationQuery) (StockLocationPage, error) {
	page, err := listPage[StockLocation](ctx, c, "/api/stock/location/", query.values())
	return StockLocationPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil && *page.Next != ""}, err
}

func (c *Client) GetStockLocation(ctx context.Context, id int) (StockLocation, error) {
	var out StockLocation
	req, err := c.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/stock/location/%d/", id), url.Values{"path_detail": []string{"true"}}, nil)
	if err == nil {
		err = c.DoJSON(req, &out)
	}
	return out, err
}

func (c *Client) SearchStockLocationTypes(ctx context.Context, query SearchQuery) ([]StockLocationType, error) {
	return listAll[StockLocationType](ctx, c, "/api/stock/location-type/", query.values())
}

func (c *Client) GetStockLocationType(ctx context.Context, id int) (StockLocationType, error) {
	var out StockLocationType
	err := c.get(ctx, fmt.Sprintf("/api/stock/location-type/%d/", id), &out)
	return out, err
}

func (c *Client) SearchStockItems(ctx context.Context, query StockItemQuery) ([]StockItem, error) {
	return listAll[StockItem](ctx, c, "/api/stock/", query.values())
}

func (c *Client) GetStockItem(ctx context.Context, id int) (StockItem, error) {
	var out StockItem
	err := c.get(ctx, fmt.Sprintf("/api/stock/%d/", id), &out)
	return out, err
}

func (c *Client) SearchPartParameters(ctx context.Context, query PartParameterQuery) ([]Parameter, error) {
	return listAll[Parameter](ctx, c, "/api/parameter/", query.values())
}

func (c *Client) SearchPartParametersPage(ctx context.Context, query PartParameterQuery) (PartParameterPage, error) {
	page, err := listPage[Parameter](ctx, c, "/api/parameter/", query.values())
	return PartParameterPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil && *page.Next != ""}, err
}

func (c *Client) SearchTemplateParametersPage(ctx context.Context, query TemplateParameterQuery) (PartParameterPage, error) {
	page, err := listPage[Parameter](ctx, c, "/api/parameter/", query.values())
	return PartParameterPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil && *page.Next != ""}, err
}

func (c *Client) GetPartParameter(ctx context.Context, id int) (Parameter, error) {
	var out Parameter
	err := c.get(ctx, fmt.Sprintf("/api/parameter/%d/", id), &out)
	return out, err
}

func (c *Client) SearchParameterTemplates(ctx context.Context, query SearchQuery) ([]ParameterTemplate, error) {
	return listAll[ParameterTemplate](ctx, c, "/api/parameter/template/", query.values())
}

func (c *Client) SearchParameterTemplatesPage(ctx context.Context, query SearchQuery) (Page[ParameterTemplate], error) {
	return listPage[ParameterTemplate](ctx, c, "/api/parameter/template/", query.values())
}

func (c *Client) GetParameterTemplate(ctx context.Context, id int) (ParameterTemplate, error) {
	var out ParameterTemplate
	err := c.get(ctx, fmt.Sprintf("/api/parameter/template/%d/", id), &out)
	return out, err
}

func (c *Client) SearchCategoryParameterTemplates(ctx context.Context, query CategoryParameterTemplateQuery) ([]CategoryParameterTemplate, error) {
	nextQuery := query.values()
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

func (c *Client) SearchCategoryParameterTemplatesPage(ctx context.Context, query CategoryParameterTemplateQuery) (Page[CategoryParameterTemplate], error) {
	return listPage[CategoryParameterTemplate](ctx, c, "/api/part/category/parameters/", query.values())
}

func (c *Client) GetCategoryParameterTemplate(ctx context.Context, id int) (CategoryParameterTemplate, error) {
	var out CategoryParameterTemplate
	err := c.get(ctx, fmt.Sprintf("/api/part/category/parameters/%d/", id), &out)
	return out, err
}

func (c *Client) ListAttachments(ctx context.Context, query AttachmentQuery) ([]Attachment, error) {
	return listAll[Attachment](ctx, c, "/api/attachment/", query.values())
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
		return DownloadedAttachment{}, fmt.Errorf("download InvenTree attachment content failed with status %d", resp.StatusCode)
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

func (c *Client) DownloadPartImage(ctx context.Context, id int, mode AttachmentContentMode, maxBytes int64) (DownloadedPartImage, error) {
	if maxBytes <= 0 {
		return DownloadedPartImage{}, errors.New("part image download maxBytes must be positive")
	}
	part, err := c.GetPart(ctx, id)
	if err != nil {
		return DownloadedPartImage{}, err
	}
	rawURL, err := c.partImageURL(ctx, part, mode)
	if err != nil {
		return DownloadedPartImage{}, err
	}
	sourceURL, err := c.resolveInvenTreeContentURL(rawURL)
	if err != nil {
		return DownloadedPartImage{}, err
	}

	downloadCtx, cancel := boundedDownloadContext(ctx, c.httpClient)
	defer cancel()
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, sourceURL.String(), nil)
	if err != nil {
		return DownloadedPartImage{}, err
	}
	req.Header.Set("Accept", "image/*,*/*")
	c.credential.Apply(req)

	httpClient := noRedirectClient(c.httpClient)
	resp, err := httpClient.Do(req)
	if err != nil {
		return DownloadedPartImage{}, errors.New("download InvenTree part image failed")
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
			return DownloadedPartImage{}, fmt.Errorf("InvenTree part image redirected with status %d", resp.StatusCode)
		}
		return DownloadedPartImage{}, fmt.Errorf("download InvenTree part image failed with status %d", resp.StatusCode)
	}
	content, err := readBounded(resp.Body, maxBytes)
	if err != nil {
		return DownloadedPartImage{}, err
	}
	return DownloadedPartImage{
		Part:        part,
		Content:     content,
		Filename:    filenameFromContentURL(sourceURL),
		ContentType: resp.Header.Get("Content-Type"),
		SourceURL:   redactedURLString(sourceURL),
	}, nil
}

func (c *Client) partImageURL(ctx context.Context, part Part, mode AttachmentContentMode) (string, error) {
	switch mode {
	case "", AttachmentContentOriginal:
		if part.Image == nil || *part.Image == "" {
			return "", ErrPartImageMissing
		}
		return *part.Image, nil
	case AttachmentContentThumbnail:
		var thumb PartThumb
		if err := c.get(ctx, fmt.Sprintf("/api/part/thumbs/%d/", part.PK), &thumb); err != nil {
			return "", err
		}
		if thumb.Image == "" {
			return "", ErrPartImageMissing
		}
		return thumb.Image, nil
	default:
		return "", fmt.Errorf("unsupported part image content mode %q", mode)
	}
}

func filenameFromContentURL(sourceURL *url.URL) string {
	if sourceURL == nil {
		return ""
	}
	filename := path.Base(sourceURL.EscapedPath())
	if filename == "." || filename == "/" {
		return ""
	}
	unescaped, err := url.PathUnescape(filename)
	if err != nil {
		return filename
	}
	return unescaped
}

func (c *Client) SearchSupplierParts(ctx context.Context, query SupplierPartQuery) ([]SupplierPart, error) {
	return listAll[SupplierPart](ctx, c, "/api/company/part/", query.values())
}

func (c *Client) SearchSupplierPartsPage(ctx context.Context, query SupplierPartQuery) (SupplierPartPage, error) {
	page, err := listPage[SupplierPartDetail](ctx, c, "/api/company/part/", query.values())
	return SupplierPartPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil && *page.Next != ""}, err
}

func (c *Client) GetSupplierPartDetail(ctx context.Context, id int) (SupplierPartDetail, error) {
	var out SupplierPartDetail
	err := c.get(ctx, fmt.Sprintf("/api/company/part/%d/", id), &out)
	return out, err
}

func (c *Client) GetSupplierPart(ctx context.Context, id int) (SupplierPart, error) {
	var out SupplierPart
	err := c.get(ctx, fmt.Sprintf("/api/company/part/%d/", id), &out)
	return out, err
}

func (c *Client) SearchManufacturerParts(ctx context.Context, query ManufacturerPartQuery) ([]ManufacturerPart, error) {
	return listAll[ManufacturerPart](ctx, c, "/api/company/part/manufacturer/", query.values())
}

func (c *Client) SearchManufacturerPartsPage(ctx context.Context, query ManufacturerPartQuery) (ManufacturerPartPage, error) {
	page, err := listPage[ManufacturerPartDetail](ctx, c, "/api/company/part/manufacturer/", query.values())
	return ManufacturerPartPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil && *page.Next != ""}, err
}

func (c *Client) GetManufacturerPartDetail(ctx context.Context, id int) (ManufacturerPartDetail, error) {
	var out ManufacturerPartDetail
	err := c.get(ctx, fmt.Sprintf("/api/company/part/manufacturer/%d/", id), &out)
	return out, err
}

func (c *Client) SearchPurchaseOrders(ctx context.Context, query PurchaseOrderQuery) ([]PurchaseOrder, error) {
	return listAll[PurchaseOrder](ctx, c, "/api/order/po/", query.values())
}

func (c *Client) GetPurchaseOrder(ctx context.Context, id int) (PurchaseOrder, error) {
	var out PurchaseOrder
	err := c.get(ctx, fmt.Sprintf("/api/order/po/%d/", id), &out)
	return out, err
}

func (c *Client) SearchPurchaseOrderLines(ctx context.Context, query PurchaseOrderLineQuery) ([]PurchaseOrderLineItem, error) {
	return listAll[PurchaseOrderLineItem](ctx, c, "/api/order/po-line/", query.values())
}

func (c *Client) GetPurchaseOrderLine(ctx context.Context, id int) (PurchaseOrderLineItem, error) {
	var out PurchaseOrderLineItem
	err := c.get(ctx, fmt.Sprintf("/api/order/po-line/%d/", id), &out)
	return out, err
}

func (c *Client) SearchPurchaseOrderExtraLines(ctx context.Context, query PurchaseOrderExtraLineQuery) ([]PurchaseOrderExtraLine, error) {
	return listAll[PurchaseOrderExtraLine](ctx, c, "/api/order/po-extra-line/", query.values())
}

func (c *Client) SearchPurchaseOrderExtraLinesPage(ctx context.Context, query PurchaseOrderExtraLineQuery) (Page[PurchaseOrderExtraLine], error) {
	return listPage[PurchaseOrderExtraLine](ctx, c, "/api/order/po-extra-line/", query.values())
}

func (c *Client) GetPurchaseOrderExtraLine(ctx context.Context, id int) (PurchaseOrderExtraLine, error) {
	var out PurchaseOrderExtraLine
	err := c.get(ctx, fmt.Sprintf("/api/order/po-extra-line/%d/", id), &out)
	return out, err
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, out)
}

func (c *Client) searchCompaniesWithRole(ctx context.Context, query SearchQuery, roleFilter string) ([]Company, error) {
	nextQuery := query.values()
	nextQuery.Set(roleFilter, "true")
	return listAll[Company](ctx, c, "/api/company/", nextQuery)
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
