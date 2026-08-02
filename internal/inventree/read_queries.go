package inventree

import (
	"net/url"
	"strconv"
)

type SearchQuery struct {
	Search string
	Limit  int
	Offset int
}

type CategoryQuery struct {
	Parent     *int
	TopLevel   *bool
	PathDetail *bool
	Limit      int
	Offset     int
}

type PartQuery struct {
	CategoryID int
	Cascade    *bool
	Limit      int
	Offset     int
}

type PartParameterQuery struct {
	Search     string
	PartID     int
	TemplateID int
	Limit      int
	Offset     int
}

type TemplateParameterQuery struct {
	TemplateID int
	Limit      int
	Offset     int
}

type CategoryParameterTemplateQuery struct {
	CategoryID  int
	FetchParent *bool
	Limit       int
	Offset      int
}

type StockItemQuery struct {
	Search          string
	PartID          int
	LocationID      int
	PurchaseOrderID int
	Limit           int
	Offset          int
}

type AttachmentQuery struct {
	ModelType string
	ModelID   int
	Search    string
	Limit     int
	Offset    int
}

type SupplierPartQuery struct {
	Part     int
	Supplier int
	SKU      string
}

type ManufacturerPartQuery struct {
	Part         int
	Manufacturer int
	MPN          string
}

type PurchaseOrderQuery struct {
	Search           string
	Supplier         int
	Reference        string
	Status           *int
	StartDateAfter   string
	StartDateBefore  string
	TargetDateAfter  string
	TargetDateBefore string
	Limit            int
	Offset           int
}

type PurchaseOrderLineQuery struct {
	Search       string
	Order        int
	SupplierPart int
	Pending      *bool
	Received     *bool
	Limit        int
	Offset       int
}

func (q SearchQuery) values() url.Values {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q CategoryQuery) values() url.Values {
	values := url.Values{}
	if q.Parent != nil {
		values.Set("parent", strconv.Itoa(*q.Parent))
	}
	if q.TopLevel != nil {
		values.Set("top_level", strconv.FormatBool(*q.TopLevel))
	}
	if q.PathDetail != nil {
		values.Set("path_detail", strconv.FormatBool(*q.PathDetail))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q PartQuery) values() url.Values {
	values := url.Values{}
	if q.CategoryID != 0 {
		values.Set("category", strconv.Itoa(q.CategoryID))
	}
	if q.Cascade != nil {
		values.Set("cascade", strconv.FormatBool(*q.Cascade))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q PartParameterQuery) values() url.Values {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	if q.PartID != 0 {
		values.Set("model_id", strconv.Itoa(q.PartID))
	}
	if q.TemplateID != 0 {
		values.Set("template", strconv.Itoa(q.TemplateID))
	}
	values.Set("model_type", parameterModelTypePart)
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q TemplateParameterQuery) values() url.Values {
	values := url.Values{}
	if q.TemplateID != 0 {
		values.Set("template", strconv.Itoa(q.TemplateID))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q CategoryParameterTemplateQuery) values() url.Values {
	values := url.Values{}
	if q.CategoryID != 0 {
		values.Set("category", strconv.Itoa(q.CategoryID))
	}
	if q.FetchParent != nil {
		values.Set("fetch_parent", strconv.FormatBool(*q.FetchParent))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q StockItemQuery) values() url.Values {
	values := SearchQuery{Search: q.Search, Limit: q.Limit, Offset: q.Offset}.values()
	if q.PartID != 0 {
		values.Set("part", strconv.Itoa(q.PartID))
	}
	if q.LocationID != 0 {
		values.Set("location", strconv.Itoa(q.LocationID))
	}
	if q.PurchaseOrderID != 0 {
		values.Set("purchase_order", strconv.Itoa(q.PurchaseOrderID))
	}
	return values
}

func (q AttachmentQuery) values() url.Values {
	values := SearchQuery{Search: q.Search, Limit: q.Limit, Offset: q.Offset}.values()
	values.Set("model_type", q.ModelType)
	if q.ModelID != 0 {
		values.Set("model_id", strconv.Itoa(q.ModelID))
	}
	return values
}

func (q SupplierPartQuery) values() url.Values {
	values := url.Values{}
	if q.Part != 0 {
		values.Set("part", strconv.Itoa(q.Part))
	}
	if q.Supplier != 0 {
		values.Set("supplier", strconv.Itoa(q.Supplier))
	}
	if q.SKU != "" {
		values.Set("SKU", q.SKU)
	}
	return values
}

func (q ManufacturerPartQuery) values() url.Values {
	values := url.Values{}
	if q.Part != 0 {
		values.Set("part", strconv.Itoa(q.Part))
	}
	if q.Manufacturer != 0 {
		values.Set("manufacturer", strconv.Itoa(q.Manufacturer))
	}
	if q.MPN != "" {
		values.Set("MPN", q.MPN)
	}
	return values
}

func (q PurchaseOrderQuery) values() url.Values {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	if q.Supplier != 0 {
		values.Set("supplier", strconv.Itoa(q.Supplier))
	}
	if q.Reference != "" {
		values.Set("reference", q.Reference)
	}
	if q.Status != nil {
		values.Set("status", strconv.Itoa(*q.Status))
	}
	if q.StartDateAfter != "" {
		values.Set("start_date_after", q.StartDateAfter)
	}
	if q.StartDateBefore != "" {
		values.Set("start_date_before", q.StartDateBefore)
	}
	if q.TargetDateAfter != "" {
		values.Set("target_date_after", q.TargetDateAfter)
	}
	if q.TargetDateBefore != "" {
		values.Set("target_date_before", q.TargetDateBefore)
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q PurchaseOrderLineQuery) values() url.Values {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	if q.Order != 0 {
		values.Set("order", strconv.Itoa(q.Order))
	}
	if q.SupplierPart != 0 {
		values.Set("part", strconv.Itoa(q.SupplierPart))
	}
	if q.Pending != nil {
		values.Set("pending", strconv.FormatBool(*q.Pending))
	}
	if q.Received != nil {
		values.Set("received", strconv.FormatBool(*q.Received))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func setPagination(values url.Values, limit int, offset int) {
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		values.Set("offset", strconv.Itoa(offset))
	}
}
