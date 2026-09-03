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
	Parent            *int
	DefaultLocationID int
	TopLevel          *bool
	PathDetail        *bool
	Limit             int
	Offset            int
}

type PartQuery struct {
	CategoryID        int
	DefaultLocationID int
	Cascade           *bool
	RevisionOf        int
	VariantOf         int
	Limit             int
	Offset            int
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

// ObjectParameterQuery lists /api/parameter/ rows scoped by an explicit,
// caller-supplied model_type (one of InvenTree's qualified app.model
// values), unlike PartParameterQuery which always forces model_type to
// "part.part". ModelID is optional: omit it to scan every object of
// ModelType sharing TemplateID, used for model-type-scoped uniqueness scans.
type ObjectParameterQuery struct {
	ModelType  string
	ModelID    int
	TemplateID int
	Search     string
	Limit      int
	Offset     int
}

// TagQuery lists /api/tag/ rows, InvenTree's shared cross-object tag
// taxonomy. ModelType optionally scopes results to tags currently referenced
// by that qualified app.model value; /api/tag/ requires an explicit limit or
// it returns a bare JSON array instead of the normal paginated shape, so
// values() always sets one.
type TagQuery struct {
	ModelType string
	Search    string
	Limit     int
	Offset    int
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
	SupplierPartID  int
	Customer        int
	Serial          string
	SerialGTE       *int
	SerialLTE       *int
	Serialized      *bool
	Limit           int
	Offset          int
}

type SalesOrderQuery struct {
	Customer int
	Limit    int
	Offset   int
}

type StockLocationQuery struct {
	Search       string
	Parent       *int
	TopLevel     *bool
	PathDetail   *bool
	LocationType *int
	Limit        int
	Offset       int
}

type AttachmentQuery struct {
	ModelType string
	ModelID   int
	Search    string
	Limit     int
	Offset    int
}

type SupplierPartQuery struct {
	Search           string
	Part             int
	Company          int
	Supplier         int
	SKU              string
	ManufacturerPart int
	Ordering         string
	Limit            int
	Offset           int
}

type ManufacturerPartQuery struct {
	Search       string
	Part         int
	Manufacturer int
	MPN          string
	Ordering     string
	Limit        int
	Offset       int
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
	BasePart     int
	Pending      *bool
	Received     *bool
	Limit        int
	Offset       int
}

type PurchaseOrderExtraLineQuery struct {
	Search string
	Order  int
	Limit  int
	Offset int
}

type BomItemQuery struct {
	Part   int
	Uses   int
	Limit  int
	Offset int
}

type SalesOrderLineQuery struct {
	Part   int
	Limit  int
	Offset int
}

type BuildQuery struct {
	Part   int
	Limit  int
	Offset int
}

type TransferOrderQuery struct {
	Limit  int
	Offset int
}

type PartRelationQuery struct {
	Part   int
	Part1  int
	Part2  int
	Limit  int
	Offset int
}

type OwnerQuery struct {
	Search   string
	IsActive *bool
	Limit    int
	Offset   int
}

type ContactQuery struct {
	CompanyID int
	Search    string
	Limit     int
	Offset    int
}

type AddressQuery struct {
	CompanyID int
	Search    string
	Limit     int
	Offset    int
}

type ProjectCodeQuery struct {
	Search   string
	IsActive *bool
	Limit    int
	Offset   int
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
	if q.DefaultLocationID != 0 {
		values.Set("default_location", strconv.Itoa(q.DefaultLocationID))
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
	if q.DefaultLocationID != 0 {
		values.Set("default_location", strconv.Itoa(q.DefaultLocationID))
	}
	if q.VariantOf != 0 {
		values.Set("variant_of", strconv.Itoa(q.VariantOf))
	}
	if q.RevisionOf != 0 {
		values.Set("revision_of", strconv.Itoa(q.RevisionOf))
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

func (q ObjectParameterQuery) values() url.Values {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	if q.ModelID != 0 {
		values.Set("model_id", strconv.Itoa(q.ModelID))
	}
	if q.TemplateID != 0 {
		values.Set("template", strconv.Itoa(q.TemplateID))
	}
	values.Set("model_type", q.ModelType)
	setPagination(values, q.Limit, q.Offset)
	return values
}

// defaultTagLimit backs TagQuery.values() when the caller passes a
// non-positive Limit, since /api/tag/ requires an explicit limit query
// parameter or it returns a bare JSON array instead of the normal
// {count, results} paginated shape.
const defaultTagLimit = 20

func (q TagQuery) values() url.Values {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	if q.ModelType != "" {
		values.Set("model_type", q.ModelType)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultTagLimit
	}
	setPagination(values, limit, q.Offset)
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
	if q.SupplierPartID != 0 {
		values.Set("supplier_part", strconv.Itoa(q.SupplierPartID))
	}
	if q.Customer != 0 {
		values.Set("customer", strconv.Itoa(q.Customer))
	}
	if q.Serial != "" {
		values.Set("serial", q.Serial)
	}
	if q.SerialGTE != nil {
		values.Set("serial_gte", strconv.Itoa(*q.SerialGTE))
	}
	if q.SerialLTE != nil {
		values.Set("serial_lte", strconv.Itoa(*q.SerialLTE))
	}
	if q.Serialized != nil {
		values.Set("serialized", strconv.FormatBool(*q.Serialized))
	}
	return values
}

func (q SalesOrderQuery) values() url.Values {
	values := SearchQuery{Limit: q.Limit, Offset: q.Offset}.values()
	if q.Customer != 0 {
		values.Set("customer", strconv.Itoa(q.Customer))
	}
	return values
}

func (q StockLocationQuery) values() url.Values {
	values := SearchQuery{Search: q.Search, Limit: q.Limit, Offset: q.Offset}.values()
	if q.Parent != nil {
		values.Set("parent", strconv.Itoa(*q.Parent))
	}
	if q.TopLevel != nil {
		values.Set("top_level", strconv.FormatBool(*q.TopLevel))
	}
	if q.PathDetail != nil {
		values.Set("path_detail", strconv.FormatBool(*q.PathDetail))
	}
	if q.LocationType != nil {
		values.Set("location_type", strconv.Itoa(*q.LocationType))
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
	values := SearchQuery{Search: q.Search, Limit: q.Limit, Offset: q.Offset}.values()
	if q.Part != 0 {
		values.Set("part", strconv.Itoa(q.Part))
	}
	if q.Company != 0 {
		values.Set("company", strconv.Itoa(q.Company))
	}
	if q.Supplier != 0 {
		values.Set("supplier", strconv.Itoa(q.Supplier))
	}
	if q.SKU != "" {
		values.Set("SKU", q.SKU)
	}
	if q.ManufacturerPart != 0 {
		values.Set("manufacturer_part", strconv.Itoa(q.ManufacturerPart))
	}
	if q.Ordering != "" {
		values.Set("ordering", q.Ordering)
	}
	return values
}

func (q ManufacturerPartQuery) values() url.Values {
	values := SearchQuery{Search: q.Search, Limit: q.Limit, Offset: q.Offset}.values()
	if q.Part != 0 {
		values.Set("part", strconv.Itoa(q.Part))
	}
	if q.Manufacturer != 0 {
		values.Set("manufacturer", strconv.Itoa(q.Manufacturer))
	}
	if q.MPN != "" {
		values.Set("MPN", q.MPN)
	}
	if q.Ordering != "" {
		values.Set("ordering", q.Ordering)
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
	if q.BasePart != 0 {
		values.Set("base_part", strconv.Itoa(q.BasePart))
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

func (q PurchaseOrderExtraLineQuery) values() url.Values {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	if q.Order != 0 {
		values.Set("order", strconv.Itoa(q.Order))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q BomItemQuery) values() url.Values {
	values := url.Values{}
	if q.Part != 0 {
		values.Set("part", strconv.Itoa(q.Part))
	}
	if q.Uses != 0 {
		values.Set("uses", strconv.Itoa(q.Uses))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q SalesOrderLineQuery) values() url.Values {
	values := url.Values{}
	if q.Part != 0 {
		values.Set("part", strconv.Itoa(q.Part))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q BuildQuery) values() url.Values {
	values := url.Values{}
	if q.Part != 0 {
		values.Set("part", strconv.Itoa(q.Part))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q TransferOrderQuery) values() url.Values {
	values := url.Values{}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q PartRelationQuery) values() url.Values {
	values := url.Values{}
	if q.Part != 0 {
		values.Set("part", strconv.Itoa(q.Part))
	}
	if q.Part1 != 0 {
		values.Set("part_1", strconv.Itoa(q.Part1))
	}
	if q.Part2 != 0 {
		values.Set("part_2", strconv.Itoa(q.Part2))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

type PartInternalPriceBreakQuery struct {
	Part   int
	Limit  int
	Offset int
}

func (q PartInternalPriceBreakQuery) values() url.Values {
	values := url.Values{}
	if q.Part != 0 {
		values.Set("part", strconv.Itoa(q.Part))
	}
	values.Set("ordering", "quantity")
	setPagination(values, q.Limit, q.Offset)
	return values
}

type PartSalePriceBreakQuery struct {
	Part   int
	Limit  int
	Offset int
}

func (q PartSalePriceBreakQuery) values() url.Values {
	values := url.Values{}
	if q.Part != 0 {
		values.Set("part", strconv.Itoa(q.Part))
	}
	values.Set("ordering", "quantity")
	setPagination(values, q.Limit, q.Offset)
	return values
}

// SupplierPriceBreakQuery.SupplierPart filters by the SupplierPart primary
// key -- the upstream "part" query parameter, despite its name.
type SupplierPriceBreakQuery struct {
	SupplierPart int
	Supplier     int
	Limit        int
	Offset       int
}

func (q SupplierPriceBreakQuery) values() url.Values {
	values := url.Values{}
	if q.SupplierPart != 0 {
		values.Set("part", strconv.Itoa(q.SupplierPart))
	}
	if q.Supplier != 0 {
		values.Set("supplier", strconv.Itoa(q.Supplier))
	}
	values.Set("ordering", "quantity")
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q OwnerQuery) values() url.Values {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	if q.IsActive != nil {
		values.Set("is_active", strconv.FormatBool(*q.IsActive))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q ContactQuery) values() url.Values {
	values := url.Values{}
	values.Set("company", strconv.Itoa(q.CompanyID))
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q AddressQuery) values() url.Values {
	values := url.Values{}
	values.Set("company", strconv.Itoa(q.CompanyID))
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

func (q ProjectCodeQuery) values() url.Values {
	values := url.Values{}
	if q.Search != "" {
		values.Set("search", q.Search)
	}
	if q.IsActive != nil {
		values.Set("active", strconv.FormatBool(*q.IsActive))
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
