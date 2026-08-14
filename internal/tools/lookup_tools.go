package tools

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ScopeInventreeRead        = "inventree.read"
	ScopeInventreeWrite       = "inventree.write"
	ScopeInventreeUpload      = "inventree.upload"
	ScopeInventreeOperational = "inventree.operational"
	ScopeInventreeDestructive = "inventree.destructive"

	SearchPartsToolName                     = "search_parts"
	GetPartToolName                         = "get_part"
	SearchPartCategoriesToolName            = "search_part_categories"
	GetPartCategoryToolName                 = "get_part_category"
	SearchParameterTemplatesToolName        = "search_parameter_templates"
	GetPartParametersToolName               = "get_part_parameters"
	SearchPartParametersToolName            = "search_part_parameters"
	DeletePartParameterToolName             = "delete_part_parameter"
	CreateParameterTemplateToolName         = "create_parameter_template"
	UpdateParameterTemplateToolName         = "update_parameter_template"
	DeleteParameterTemplateToolName         = "delete_parameter_template"
	MergeParameterTemplatesToolName         = "merge_parameter_templates"
	SearchCategoryParameterDefaultsToolName = "search_category_parameter_defaults"
	CreateCategoryParameterDefaultToolName  = "create_category_parameter_default"
	UpdateCategoryParameterDefaultToolName  = "update_category_parameter_default"
	DeleteCategoryParameterDefaultToolName  = "delete_category_parameter_default"
	AuditParameterConsistencyToolName       = "audit_parameter_consistency"
	BulkPropagatePartParametersToolName     = "bulk_propagate_part_parameters"
	SearchCompaniesToolName                 = "search_companies"
	SearchSuppliersToolName                 = "search_suppliers"
	SearchManufacturersToolName             = "search_manufacturers"
	GetCompanyToolName                      = "get_company"
	SearchSupplierPartsToolName             = "search_supplier_parts"
	GetSupplierPartToolName                 = "get_supplier_part"
	SearchManufacturerPartsToolName         = "search_manufacturer_parts"
	GetManufacturerPartToolName             = "get_manufacturer_part"
	SearchStockLocationsToolName            = "search_stock_locations"
	GetStockLocationToolName                = "get_stock_location"
	SearchStockLocationTypesToolName        = "search_stock_location_types"
	GetStockLocationTypeToolName            = "get_stock_location_type"
	SearchStockItemsToolName                = "search_stock_items"
	GetStockItemToolName                    = "get_stock_item"
	ListAttachmentsToolName                 = "list_attachments"
	GetAttachmentMetadataToolName           = "get_attachment_metadata"
	DownloadAttachmentToolName              = "download_attachment"
	DownloadPartImageToolName               = "download_part_image"
	PreviewPurchaseOrderToolName            = "preview_purchase_order_with_lines"
	SearchPurchaseOrdersToolName            = "search_purchase_orders"
	GetPurchaseOrderToolName                = "get_purchase_order"
	SearchPurchaseOrderLinesToolName        = "search_purchase_order_lines"
	GetPurchaseOrderLineToolName            = "get_purchase_order_line"
	SearchPurchaseOrderExtraLinesToolName   = "search_purchase_order_extra_lines"
	GetPurchaseOrderExtraLineToolName       = "get_purchase_order_extra_line"
	CreatePartToolName                      = "create_part"
	UpdatePartToolName                      = "update_part"
	DeletePartToolName                      = "delete_part"
	CreatePartCategoryToolName              = "create_part_category"
	UpdatePartCategoryToolName              = "update_part_category"
	SetPartParametersToolName               = "set_part_parameters"
	CreateCompanyToolName                   = "create_company"
	CreateSupplierPartToolName              = "create_supplier_part"
	CreateManufacturerPartToolName          = "create_manufacturer_part"
	UpdateCompanyToolName                   = "update_company"
	UpdateSupplierPartToolName              = "update_supplier_part"
	UpdateManufacturerPartToolName          = "update_manufacturer_part"
	UpsertPartWorkflowToolName              = "upsert_part_with_supplier_and_manufacturer"
	CreateStockItemToolName                 = "create_stock_item"
	CreateStockLocationToolName             = "create_stock_location"
	UpdateStockLocationToolName             = "update_stock_location"
	RestructureStockLocationToolName        = "restructure_stock_location"
	UpdateStockItemMetadataToolName         = "update_stock_item_metadata"
	InitialStockWorkflowToolName            = "create_initial_stock_entry"
	AdjustStockQuantityToolName             = "adjust_stock_quantity"
	SetStockStatusToolName                  = "set_stock_status"
	StocktakeAdjustmentToolName             = "stocktake_adjustment"
	DepleteStockItemToolName                = "deplete_stock_item"
	TransferStockItemToolName               = "transfer_stock_item"
	CreatePurchaseOrderToolName             = "create_purchase_order"
	AddPurchaseOrderLineToolName            = "add_purchase_order_line"
	UpdatePurchaseOrderLineToolName         = "update_purchase_order_line"
	DeletePurchaseOrderLineToolName         = "delete_purchase_order_line"
	CreatePurchaseOrderExtraLineToolName    = "create_purchase_order_extra_line"
	UpdatePurchaseOrderExtraLineToolName    = "update_purchase_order_extra_line"
	DeletePurchaseOrderExtraLineToolName    = "delete_purchase_order_extra_line"
	CreatePurchaseOrderWorkflowToolName     = "create_purchase_order_with_lines"
	IssuePurchaseOrderToolName              = "issue_purchase_order"
	ReceivePurchaseOrderToolName            = "receive_purchase_order_items"
	UploadAttachmentToolName                = "upload_attachment"
	UploadAttachmentFromURLToolName         = "upload_attachment_from_url"
	CreateLinkAttachmentToolName            = "create_link_attachment"
	UpdateAttachmentMetadataToolName        = "update_attachment_metadata"
	DeleteAttachmentToolName                = "delete_attachment"
	SetPrimaryImageToolName                 = "set_primary_image"
	SetCompanyImageToolName                 = "set_company_image"
	SetCompanyImageFromURLToolName          = "set_company_image_from_url"
	ClearCompanyImageToolName               = "clear_company_image"

	defaultDownloadMaxBytes int64 = 5 * 1024 * 1024
	maxDownloadMaxBytes     int64 = 25 * 1024 * 1024
)

var inScopeAttachmentModelTypes = map[string]bool{
	"part":             true,
	"stockitem":        true,
	"company":          true,
	"manufacturerpart": true,
	"supplierpart":     true,
	"purchaseorder":    true,
}

type ToolAuthorization struct {
	Name            string
	MilestoneStatus string
	MutationClass   string
	Scopes          []string
	Annotations     AnnotationClass
}

var lookupToolNames = []string{
	SearchPartsToolName,
	GetPartToolName,
	SearchPartCategoriesToolName,
	GetPartCategoryToolName,
	SearchParameterTemplatesToolName,
	GetPartParametersToolName,
	SearchPartParametersToolName,
	SearchCategoryParameterDefaultsToolName,
	AuditParameterConsistencyToolName,
	SearchCompaniesToolName,
	SearchSuppliersToolName,
	SearchManufacturersToolName,
	GetCompanyToolName,
	SearchSupplierPartsToolName,
	GetSupplierPartToolName,
	SearchManufacturerPartsToolName,
	GetManufacturerPartToolName,
	SearchStockLocationsToolName,
	GetStockLocationToolName,
	SearchStockLocationTypesToolName,
	GetStockLocationTypeToolName,
	SearchStockItemsToolName,
	GetStockItemToolName,
	ListAttachmentsToolName,
	GetAttachmentMetadataToolName,
	DownloadAttachmentToolName,
	DownloadPartImageToolName,
	PreviewPurchaseOrderToolName,
	SearchPurchaseOrdersToolName,
	GetPurchaseOrderToolName,
	SearchPurchaseOrderLinesToolName,
	GetPurchaseOrderLineToolName,
	SearchPurchaseOrderExtraLinesToolName,
	GetPurchaseOrderExtraLineToolName,
}

var writeToolNames = []string{
	CreatePartToolName,
	UpdatePartToolName,
	DeletePartToolName,
	CreatePartCategoryToolName,
	UpdatePartCategoryToolName,
	SetPartParametersToolName,
	DeletePartParameterToolName,
	CreateParameterTemplateToolName,
	UpdateParameterTemplateToolName,
	DeleteParameterTemplateToolName,
	MergeParameterTemplatesToolName,
	CreateCategoryParameterDefaultToolName,
	UpdateCategoryParameterDefaultToolName,
	DeleteCategoryParameterDefaultToolName,
	BulkPropagatePartParametersToolName,
	CreateCompanyToolName,
	CreateSupplierPartToolName,
	CreateManufacturerPartToolName,
	UpdateCompanyToolName,
	UpdateSupplierPartToolName,
	UpdateManufacturerPartToolName,
	UpsertPartWorkflowToolName,
	CreateStockItemToolName,
	CreateStockLocationToolName,
	UpdateStockLocationToolName,
	RestructureStockLocationToolName,
	UpdateStockItemMetadataToolName,
	InitialStockWorkflowToolName,
	AdjustStockQuantityToolName,
	SetStockStatusToolName,
	StocktakeAdjustmentToolName,
	DepleteStockItemToolName,
	TransferStockItemToolName,
	CreatePurchaseOrderToolName,
	AddPurchaseOrderLineToolName,
	UpdatePurchaseOrderLineToolName,
	DeletePurchaseOrderLineToolName,
	CreatePurchaseOrderExtraLineToolName,
	UpdatePurchaseOrderExtraLineToolName,
	DeletePurchaseOrderExtraLineToolName,
	CreatePurchaseOrderWorkflowToolName,
	IssuePurchaseOrderToolName,
	ReceivePurchaseOrderToolName,
	UploadAttachmentToolName,
	UploadAttachmentFromURLToolName,
	CreateLinkAttachmentToolName,
	UpdateAttachmentMetadataToolName,
	DeleteAttachmentToolName,
	SetPrimaryImageToolName,
	SetCompanyImageToolName,
	SetCompanyImageFromURLToolName,
	ClearCompanyImageToolName,
}

var ToolAuthorizations = map[string]ToolAuthorization{
	HealthVersionToolName: {
		Name:            HealthVersionToolName,
		MilestoneStatus: ToolMilestone1,
		MutationClass:   "read_only",
		Scopes:          nil,
		Annotations:     ReadOnlyAnnotations,
	},
	GetLocalUploadPolicyToolName: {
		Name:            GetLocalUploadPolicyToolName,
		MilestoneStatus: ToolMilestone1,
		MutationClass:   "read_only",
		Scopes:          nil,
		Annotations:     ReadOnlyAnnotations,
	},
}

func init() {
	for _, name := range lookupToolNames {
		ToolAuthorizations[name] = ToolAuthorization{
			Name:            name,
			MilestoneStatus: ToolMilestone1,
			MutationClass:   "read_only",
			Scopes:          []string{ScopeInventreeRead},
			Annotations:     ReadOnlyAnnotations,
		}
	}
	for _, name := range writeToolNames {
		scopes := []string{ScopeInventreeWrite}
		mutationClass := "write"
		switch name {
		case CreateParameterTemplateToolName, UpdateParameterTemplateToolName, CreateCategoryParameterDefaultToolName, UpdateCategoryParameterDefaultToolName, CreatePartCategoryToolName, UpdatePartCategoryToolName, UpdateCompanyToolName, UpdateSupplierPartToolName, UpdateManufacturerPartToolName, CreateStockLocationToolName, UpdateStockLocationToolName, CreatePurchaseOrderExtraLineToolName, UpdatePurchaseOrderExtraLineToolName, CreatePurchaseOrderWorkflowToolName, IssuePurchaseOrderToolName:
			scopes = []string{ScopeInventreeRead, ScopeInventreeWrite}
		case BulkPropagatePartParametersToolName:
			scopes = []string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeDestructive}
			mutationClass = "destructive"
		case CreateStockItemToolName, InitialStockWorkflowToolName:
			scopes = []string{ScopeInventreeWrite, ScopeInventreeOperational}
			mutationClass = "operational"
		case AdjustStockQuantityToolName, SetStockStatusToolName, StocktakeAdjustmentToolName, TransferStockItemToolName, RestructureStockLocationToolName, UpdateStockItemMetadataToolName:
			scopes = []string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational}
			mutationClass = "operational"
		case DepleteStockItemToolName:
			scopes = []string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational, ScopeInventreeDestructive}
			mutationClass = "destructive"
		case ReceivePurchaseOrderToolName:
			scopes = []string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational}
			mutationClass = "operational"
		case UploadAttachmentToolName, UploadAttachmentFromURLToolName, CreateLinkAttachmentToolName, UpdateAttachmentMetadataToolName, SetPrimaryImageToolName:
			scopes = []string{ScopeInventreeWrite, ScopeInventreeUpload}
		case SetCompanyImageToolName, SetCompanyImageFromURLToolName:
			scopes = []string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeUpload}
		case DeletePartParameterToolName, DeleteParameterTemplateToolName, MergeParameterTemplatesToolName, DeleteCategoryParameterDefaultToolName, DeletePurchaseOrderExtraLineToolName, DeletePurchaseOrderLineToolName, DeletePartToolName:
			scopes = []string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeDestructive}
			mutationClass = "destructive"
		case DeleteAttachmentToolName:
			scopes = []string{ScopeInventreeWrite, ScopeInventreeUpload, ScopeInventreeDestructive}
			mutationClass = "destructive"
		case ClearCompanyImageToolName:
			scopes = []string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeUpload, ScopeInventreeDestructive}
			mutationClass = "destructive"
		}
		annotations := WriteAnnotations
		if name == UploadAttachmentFromURLToolName || name == SetCompanyImageFromURLToolName {
			annotations.OpenWorld = true
		}
		if name == DeleteAttachmentToolName || name == ClearCompanyImageToolName || name == DeletePartParameterToolName || name == DeleteParameterTemplateToolName || name == MergeParameterTemplatesToolName || name == DeleteCategoryParameterDefaultToolName || name == DeletePurchaseOrderExtraLineToolName || name == DeletePurchaseOrderLineToolName || name == BulkPropagatePartParametersToolName || name == DepleteStockItemToolName || name == DeletePartToolName {
			annotations.Destructive = true
		}
		ToolAuthorizations[name] = ToolAuthorization{
			Name:            name,
			MilestoneStatus: ToolMilestone1,
			MutationClass:   mutationClass,
			Scopes:          scopes,
			Annotations:     annotations,
		}
	}
	categoryUpdate := ToolAuthorizations[UpdatePartCategoryToolName]
	categoryUpdate.Annotations.Idempotent = true
	ToolAuthorizations[UpdatePartCategoryToolName] = categoryUpdate
	for _, name := range []string{UpdateCompanyToolName, UpdateSupplierPartToolName, UpdateManufacturerPartToolName} {
		auth := ToolAuthorizations[name]
		auth.Annotations.Idempotent = true
		ToolAuthorizations[name] = auth
	}
	for _, name := range []string{UpdateStockLocationToolName, RestructureStockLocationToolName, UpdateStockItemMetadataToolName} {
		auth := ToolAuthorizations[name]
		auth.Annotations.Idempotent = true
		ToolAuthorizations[name] = auth
	}
	extraLineUpdate := ToolAuthorizations[UpdatePurchaseOrderExtraLineToolName]
	extraLineUpdate.Annotations.Idempotent = true
	ToolAuthorizations[UpdatePurchaseOrderExtraLineToolName] = extraLineUpdate
	for _, name := range []string{SearchPurchaseOrdersToolName, GetPurchaseOrderToolName, SearchPurchaseOrderLinesToolName, GetPurchaseOrderLineToolName, SearchPurchaseOrderExtraLinesToolName, GetPurchaseOrderExtraLineToolName, CreatePurchaseOrderToolName, AddPurchaseOrderLineToolName, UpdatePurchaseOrderLineToolName, DeletePurchaseOrderLineToolName, CreatePurchaseOrderExtraLineToolName, UpdatePurchaseOrderExtraLineToolName, DeletePurchaseOrderExtraLineToolName, CreatePurchaseOrderWorkflowToolName, IssuePurchaseOrderToolName, ReceivePurchaseOrderToolName} {
		auth := ToolAuthorizations[name]
		auth.MilestoneStatus = ToolMilestone1
		ToolAuthorizations[name] = auth
	}
}

type PartLookupClient interface {
	SearchParts(context.Context, inventree.SearchQuery) ([]inventree.Part, error)
	GetPart(context.Context, int) (inventree.Part, error)
}

type CategoryLookupClient interface {
	SearchPartCategories(context.Context, inventree.SearchQuery) ([]inventree.Category, error)
	GetPartCategory(context.Context, int) (inventree.Category, error)
}

type ParameterLookupClient interface {
	SearchPartParameters(context.Context, inventree.PartParameterQuery) ([]inventree.Parameter, error)
	SearchParameterTemplates(context.Context, inventree.SearchQuery) ([]inventree.ParameterTemplate, error)
}

type CompanyLookupClient interface {
	SearchCompanies(context.Context, inventree.SearchQuery) ([]inventree.Company, error)
	SearchSuppliers(context.Context, inventree.SearchQuery) ([]inventree.Company, error)
	SearchManufacturers(context.Context, inventree.SearchQuery) ([]inventree.Company, error)
}

type StockLookupClient interface {
	SearchStockLocations(context.Context, inventree.SearchQuery) ([]inventree.StockLocation, error)
	SearchStockItems(context.Context, inventree.StockItemQuery) ([]inventree.StockItem, error)
}

type StockLocationExactLookupClient interface {
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
}

type StockLocationTypeLookupClient interface {
	SearchStockLocationTypes(context.Context, inventree.SearchQuery) ([]inventree.StockLocationType, error)
	GetStockLocationType(context.Context, int) (inventree.StockLocationType, error)
}

type StockItemExactLookupClient interface {
	GetStockItem(context.Context, int) (inventree.StockItem, error)
}

type AttachmentLookupClient interface {
	ListAttachments(context.Context, inventree.AttachmentQuery) ([]inventree.Attachment, error)
	GetAttachmentMetadata(context.Context, int) (inventree.Attachment, error)
	DownloadAttachment(context.Context, int, inventree.AttachmentContentMode, int64) (inventree.DownloadedAttachment, error)
	DownloadPartImage(context.Context, int, inventree.AttachmentContentMode, int64) (inventree.DownloadedPartImage, error)
}

type PurchasePreviewClient interface {
	GetSupplierPart(context.Context, int) (inventree.SupplierPart, error)
	SearchSupplierParts(context.Context, inventree.SupplierPartQuery) ([]inventree.SupplierPart, error)
}

type PartParametersInput struct {
	PartID int `json:"part_id" jsonschema:"Stable InvenTree part primary key."`
	Limit  int `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset int `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type StockItemsInput struct {
	Search          string `json:"search,omitempty" jsonschema:"Optional search text passed to the InvenTree endpoint."`
	PartID          int    `json:"part_id,omitempty" jsonschema:"Optional part primary key filter."`
	LocationID      int    `json:"location_id,omitempty" jsonschema:"Optional stock location primary key filter."`
	PurchaseOrderID int    `json:"purchase_order_id,omitempty" jsonschema:"Optional source purchase-order primary key filter for receipt recovery."`
	Limit           int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset          int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type DownloadInput struct {
	ID       int    `json:"id" jsonschema:"Stable InvenTree primary key."`
	Mode     string `json:"mode,omitempty" jsonschema:"Download mode. Use original by default or thumbnail when supported."`
	MaxBytes int64  `json:"max_bytes,omitempty" jsonschema:"Maximum content bytes to return. Defaults to 5 MiB and is capped at 25 MiB."`
}

type LookupOutput[T any] struct {
	Status        string                 `json:"status"`
	Count         int                    `json:"count,omitempty"`
	Results       []T                    `json:"results,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
}

type RecordOutput[T any] struct {
	Status string `json:"status"`
	Record T      `json:"record,omitempty"`
}

type DownloadOutput struct {
	Status      string `json:"status"`
	ID          int    `json:"id"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Size        int    `json:"size"`
	SHA256      string `json:"sha256"`
	Mode        string `json:"mode"`
	SourceURL   string `json:"source_url,omitempty"`
	Text        string `json:"text,omitempty"`
	Base64      string `json:"base64,omitempty"`
}

type PurchasePreviewInput struct {
	SupplierID int                        `json:"supplier_id,omitempty" jsonschema:"Supplier company primary key used to validate line supplier parts."`
	Lines      []PurchasePreviewLineInput `json:"lines" jsonschema:"Purchase-order lines to preview without writing."`
}

type PurchasePreviewLineInput struct {
	PartID         int      `json:"part_id,omitempty" jsonschema:"Existing part primary key when supplier_part_id is not supplied."`
	SupplierPartID int      `json:"supplier_part_id,omitempty" jsonschema:"Existing supplier-part primary key."`
	SupplierSKU    string   `json:"supplier_sku,omitempty" jsonschema:"Supplier SKU used with part_id and supplier_id to find a supplier-part link."`
	Quantity       float64  `json:"quantity" jsonschema:"Requested order quantity. Must be greater than zero."`
	UnitPrice      *float64 `json:"unit_price,omitempty" jsonschema:"Optional unit price for preview totals."`
	Currency       string   `json:"currency,omitempty" jsonschema:"Currency required when unit_price is supplied."`
	Notes          string   `json:"notes,omitempty" jsonschema:"Optional operator-facing line note."`
}

type PurchasePreviewOutput struct {
	Status        string                      `json:"status"`
	SupplierID    int                         `json:"supplier_id,omitempty"`
	Lines         []PurchasePreviewLineOutput `json:"lines,omitempty"`
	Warnings      []string                    `json:"warnings,omitempty"`
	Clarification *ClarificationResponse      `json:"clarification,omitempty"`
}

type PurchasePreviewLineOutput struct {
	Index          int      `json:"index"`
	PartID         int      `json:"part_id"`
	SupplierID     int      `json:"supplier_id"`
	SupplierPartID int      `json:"supplier_part_id"`
	SupplierSKU    string   `json:"supplier_sku,omitempty"`
	Quantity       float64  `json:"quantity"`
	UnitPrice      *float64 `json:"unit_price,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	LineTotal      *float64 `json:"line_total,omitempty"`
	Notes          string   `json:"notes,omitempty"`
}

type AttachmentMetadata struct {
	inventree.WebLinkFields
	PK            int      `json:"pk"`
	ModelType     string   `json:"model_type"`
	ModelID       int      `json:"model_id"`
	Filename      string   `json:"filename"`
	Comment       string   `json:"comment,omitempty"`
	IsImage       bool     `json:"is_image"`
	IsLink        bool     `json:"is_link"`
	FileSize      *int64   `json:"file_size,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	UploadDate    string   `json:"upload_date,omitempty"`
	UploadUser    *int     `json:"upload_user,omitempty"`
	HasFile       bool     `json:"has_file"`
	HasThumbnail  bool     `json:"has_thumbnail"`
	AttachmentURL string   `json:"attachment_url,omitempty"`
	ThumbnailURL  string   `json:"thumbnail_url,omitempty"`
	LinkURL       string   `json:"link_url,omitempty"`
}

func registerLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchPartsToolName, "Search parts", "Searches InvenTree parts.", searchParts(deps))
	addReadOnlyTool(server, deps, GetPartToolName, "Get part", "Retrieves one InvenTree part by ID.", getPart(deps))
	addReadOnlyTool(server, deps, SearchPartCategoriesToolName, "Search part categories", "Searches InvenTree part categories.", searchPartCategories(deps))
	addReadOnlyTool(server, deps, GetPartCategoryToolName, "Get part category", "Retrieves one InvenTree part category with hierarchy and default metadata by stable ID.", getPartCategory(deps))
	addReadOnlyTool(server, deps, SearchParameterTemplatesToolName, "Search parameter templates", "Searches InvenTree parameter templates.", searchParameterTemplates(deps))
	addReadOnlyTool(server, deps, GetPartParametersToolName, "Get part parameters", "Lists parameter values for one part.", getPartParameters(deps))
	addReadOnlyTool(server, deps, SearchPartParametersToolName, "Search part parameters", "Searches parameter values across parts using stable filters.", searchPartParameterValues(deps))
	registerCategoryParameterLookupTools(server, deps)
	registerParameterBulkLookupTools(server, deps)
	addReadOnlyTool(server, deps, SearchCompaniesToolName, "Search companies", "Searches InvenTree companies.", searchCompanies(deps))
	addReadOnlyTool(server, deps, SearchSuppliersToolName, "Search suppliers", "Searches companies with the supplier role.", searchSuppliers(deps))
	addReadOnlyTool(server, deps, SearchManufacturersToolName, "Search manufacturers", "Searches companies with the manufacturer role.", searchManufacturers(deps))
	registerCompanyAdminLookupTools(server, deps)
	addReadOnlyTool(server, deps, SearchStockLocationsToolName, "Search stock locations", "Searches InvenTree stock locations.", searchStockLocations(deps))
	addReadOnlyTool(server, deps, GetStockLocationToolName, "Get stock location", "Retrieves one stock location with hierarchy metadata by stable ID.", getStockLocation(deps))
	addReadOnlyTool(server, deps, SearchStockLocationTypesToolName, "Search stock location types", "Searches existing stock location types for reference selection.", searchStockLocationTypes(deps))
	addReadOnlyTool(server, deps, GetStockLocationTypeToolName, "Get stock location type", "Retrieves one stock location type by stable ID.", getStockLocationType(deps))
	addReadOnlyTool(server, deps, SearchStockItemsToolName, "Search stock items", "Searches InvenTree stock items.", searchStockItems(deps))
	addReadOnlyTool(server, deps, GetStockItemToolName, "Get stock item", "Retrieves one stock item with traceability and source context by stable ID.", getStockItem(deps))
	addReadOnlyTool(server, deps, ListAttachmentsToolName, "List attachments", "Lists attachment metadata for an in-scope InvenTree object.", listAttachments(deps))
	addReadOnlyTool(server, deps, GetAttachmentMetadataToolName, "Get attachment metadata", "Retrieves one attachment metadata record by ID.", getAttachmentMetadata(deps))
	addReadOnlyTool(server, deps, DownloadAttachmentToolName, "Download attachment", "Downloads bounded content for one file attachment.", downloadAttachment(deps))
	addReadOnlyTool(server, deps, DownloadPartImageToolName, "Download part image", "Downloads bounded content for a part primary image.", downloadPartImage(deps))
	addReadOnlyTool(server, deps, PreviewPurchaseOrderToolName, "Preview purchase order with lines", "Validates supplier-part lines and returns a no-write purchase-order preview.", previewPurchaseOrder(deps))
	registerPurchasingLookupTools(server, deps)
}

func addReadOnlyTool[In, Out any](server *mcp.Server, deps Dependencies, name string, title string, description string, handler mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(server, ToolDescriptor(name, title, description), GuardTool(deps, name, handler))
}

func searchParts(deps Dependencies) mcp.ToolHandlerFor[SearchInput, LookupOutput[inventree.Part]] {
	return LookupHandler[PartLookupClient, SearchInput, LookupOutput[inventree.Part]](deps, SearchPartsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartLookupClient, input SearchInput) (*mcp.CallToolResult, LookupOutput[inventree.Part], error) {
			records, err := client.SearchParts(ctx, searchQuery(input))
			return searchOutput(records, input.Search, "part", "part_id", "Which part should be used?", err)
		})
}

func getPart(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.Part]] {
	return LookupHandler[PartLookupClient, IDInput, RecordOutput[inventree.Part]](deps, GetPartToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.Part], error) {
			record, err := client.GetPart(ctx, input.ID)
			return recordOutput(record, err)
		})
}

func searchPartCategories(deps Dependencies) mcp.ToolHandlerFor[SearchInput, LookupOutput[inventree.Category]] {
	return LookupHandler[CategoryLookupClient, SearchInput, LookupOutput[inventree.Category]](deps, SearchPartCategoriesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CategoryLookupClient, input SearchInput) (*mcp.CallToolResult, LookupOutput[inventree.Category], error) {
			records, err := client.SearchPartCategories(ctx, searchQuery(input))
			return searchOutput(records, input.Search, "category", "category_id", "Which category should be used?", err)
		})
}

func getPartCategory(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.Category]] {
	return LookupHandler[CategoryLookupClient, IDInput, RecordOutput[inventree.Category]](deps, GetPartCategoryToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CategoryLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.Category], error) {
			record, err := client.GetPartCategory(ctx, input.ID)
			if err != nil {
				var apiErr *inventree.APIError
				if !errors.As(err, &apiErr) || apiErr.Kind != inventree.ErrorKindNotFound {
					return nil, RecordOutput[inventree.Category]{}, errors.New("part-category lookup failed; inspect InvenTree availability and permissions before retrying")
				}
			}
			if err == nil && record.PK != input.ID {
				return nil, RecordOutput[inventree.Category]{}, errors.New("InvenTree returned a mismatched part-category identity")
			}
			return recordOutput(record, err)
		})
}

func searchParameterTemplates(deps Dependencies) mcp.ToolHandlerFor[SearchInput, LookupOutput[inventree.ParameterTemplate]] {
	return LookupHandler[ParameterLookupClient, SearchInput, LookupOutput[inventree.ParameterTemplate]](deps, SearchParameterTemplatesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ParameterLookupClient, input SearchInput) (*mcp.CallToolResult, LookupOutput[inventree.ParameterTemplate], error) {
			records, err := client.SearchParameterTemplates(ctx, searchQuery(input))
			return searchOutput(records, input.Search, "template", "template_id", "Which parameter template should be used?", err)
		})
}

func getPartParameters(deps Dependencies) mcp.ToolHandlerFor[PartParametersInput, LookupOutput[inventree.Parameter]] {
	return LookupHandler[ParameterLookupClient, PartParametersInput, LookupOutput[inventree.Parameter]](deps, GetPartParametersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ParameterLookupClient, input PartParametersInput) (*mcp.CallToolResult, LookupOutput[inventree.Parameter], error) {
			records, err := client.SearchPartParameters(ctx, inventree.PartParameterQuery{
				PartID: input.PartID,
				Limit:  NormalizeLookupLimit(input.Limit),
				Offset: input.Offset,
			})
			return listOutput(records, err)
		})
}

func searchCompanies(deps Dependencies) mcp.ToolHandlerFor[SearchInput, LookupOutput[inventree.Company]] {
	return LookupHandler[CompanyLookupClient, SearchInput, LookupOutput[inventree.Company]](deps, SearchCompaniesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyLookupClient, input SearchInput) (*mcp.CallToolResult, LookupOutput[inventree.Company], error) {
			records, err := client.SearchCompanies(ctx, searchQuery(input))
			return searchOutput(records, input.Search, "company", "company_id", "Which company should be used?", err)
		})
}

func searchSuppliers(deps Dependencies) mcp.ToolHandlerFor[SearchInput, LookupOutput[inventree.Company]] {
	return LookupHandler[CompanyLookupClient, SearchInput, LookupOutput[inventree.Company]](deps, SearchSuppliersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyLookupClient, input SearchInput) (*mcp.CallToolResult, LookupOutput[inventree.Company], error) {
			records, err := client.SearchSuppliers(ctx, searchQuery(input))
			return searchOutput(records, input.Search, "supplier", "supplier_id", "Which supplier should be used?", err)
		})
}

func searchManufacturers(deps Dependencies) mcp.ToolHandlerFor[SearchInput, LookupOutput[inventree.Company]] {
	return LookupHandler[CompanyLookupClient, SearchInput, LookupOutput[inventree.Company]](deps, SearchManufacturersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyLookupClient, input SearchInput) (*mcp.CallToolResult, LookupOutput[inventree.Company], error) {
			records, err := client.SearchManufacturers(ctx, searchQuery(input))
			return searchOutput(records, input.Search, "manufacturer", "manufacturer_id", "Which manufacturer should be used?", err)
		})
}

func searchStockLocations(deps Dependencies) mcp.ToolHandlerFor[SearchInput, LookupOutput[inventree.StockLocation]] {
	return LookupHandler[StockLookupClient, SearchInput, LookupOutput[inventree.StockLocation]](deps, SearchStockLocationsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockLookupClient, input SearchInput) (*mcp.CallToolResult, LookupOutput[inventree.StockLocation], error) {
			records, err := client.SearchStockLocations(ctx, searchQuery(input))
			return searchOutput(records, input.Search, "location", "location_id", "Which stock location should be used?", err)
		})
}

func getStockLocation(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.StockLocation]] {
	return LookupHandler[StockLocationExactLookupClient, IDInput, RecordOutput[inventree.StockLocation]](deps, GetStockLocationToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockLocationExactLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.StockLocation], error) {
			record, err := client.GetStockLocation(ctx, input.ID)
			if err == nil && record.PK != input.ID {
				return nil, RecordOutput[inventree.StockLocation]{}, errors.New("InvenTree returned a mismatched stock-location identity")
			}
			return recordOutput(record, err)
		})
}

func searchStockLocationTypes(deps Dependencies) mcp.ToolHandlerFor[SearchInput, LookupOutput[inventree.StockLocationType]] {
	return LookupHandler[StockLocationTypeLookupClient, SearchInput, LookupOutput[inventree.StockLocationType]](deps, SearchStockLocationTypesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockLocationTypeLookupClient, input SearchInput) (*mcp.CallToolResult, LookupOutput[inventree.StockLocationType], error) {
			records, err := client.SearchStockLocationTypes(ctx, searchQuery(input))
			return searchOutput(records, input.Search, "location type", "location_type_id", "Which existing stock location type should be used?", err)
		})
}

func getStockLocationType(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.StockLocationType]] {
	return LookupHandler[StockLocationTypeLookupClient, IDInput, RecordOutput[inventree.StockLocationType]](deps, GetStockLocationTypeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockLocationTypeLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.StockLocationType], error) {
			record, err := client.GetStockLocationType(ctx, input.ID)
			if err == nil && record.PK != input.ID {
				return nil, RecordOutput[inventree.StockLocationType]{}, errors.New("InvenTree returned a mismatched stock-location-type identity")
			}
			return recordOutput(record, err)
		})
}

func searchStockItems(deps Dependencies) mcp.ToolHandlerFor[StockItemsInput, LookupOutput[inventree.StockItem]] {
	return LookupHandler[StockLookupClient, StockItemsInput, LookupOutput[inventree.StockItem]](deps, SearchStockItemsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockLookupClient, input StockItemsInput) (*mcp.CallToolResult, LookupOutput[inventree.StockItem], error) {
			records, err := client.SearchStockItems(ctx, inventree.StockItemQuery{
				Search:          input.Search,
				PartID:          input.PartID,
				LocationID:      input.LocationID,
				PurchaseOrderID: input.PurchaseOrderID,
				Limit:           NormalizeLookupLimit(input.Limit),
				Offset:          input.Offset,
			})
			for i := range records {
				records[i] = sanitizedStockItem(records[i])
			}
			return listOutput(records, err)
		})
}

func getStockItem(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.StockItem]] {
	return LookupHandler[StockItemExactLookupClient, IDInput, RecordOutput[inventree.StockItem]](deps, GetStockItemToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockItemExactLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.StockItem], error) {
			record, err := client.GetStockItem(ctx, input.ID)
			if err == nil && record.PK != input.ID {
				return nil, RecordOutput[inventree.StockItem]{}, errors.New("InvenTree returned a mismatched stock-item identity")
			}
			record = sanitizedStockItem(record)
			return recordOutput(record, err)
		})
}

func listAttachments(deps Dependencies) mcp.ToolHandlerFor[ObjectLookupInput, LookupOutput[AttachmentMetadata]] {
	return LookupHandler[AttachmentLookupClient, ObjectLookupInput, LookupOutput[AttachmentMetadata]](deps, ListAttachmentsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentLookupClient, input ObjectLookupInput) (*mcp.CallToolResult, LookupOutput[AttachmentMetadata], error) {
			if err := validateAttachmentModelType(input.ModelType); err != nil {
				return nil, LookupOutput[AttachmentMetadata]{}, err
			}
			records, err := client.ListAttachments(ctx, inventree.AttachmentQuery{
				ModelType: input.ModelType,
				ModelID:   input.ModelID,
				Search:    input.Search,
				Limit:     NormalizeLookupLimit(input.Limit),
				Offset:    input.Offset,
			})
			return listOutput(sanitizeAttachments(records), err)
		})
}

func getAttachmentMetadata(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[AttachmentMetadata]] {
	return LookupHandler[AttachmentLookupClient, IDInput, RecordOutput[AttachmentMetadata]](deps, GetAttachmentMetadataToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[AttachmentMetadata], error) {
			record, err := client.GetAttachmentMetadata(ctx, input.ID)
			if err != nil {
				return recordOutput(AttachmentMetadata{}, err)
			}
			if err := validateAttachmentModelType(record.ModelType); err != nil {
				return nil, RecordOutput[AttachmentMetadata]{}, err
			}
			return recordOutput(sanitizeAttachment(record), nil)
		})
}

func downloadAttachment(deps Dependencies) mcp.ToolHandlerFor[DownloadInput, DownloadOutput] {
	return LookupHandler[AttachmentLookupClient, DownloadInput, DownloadOutput](deps, DownloadAttachmentToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentLookupClient, input DownloadInput) (*mcp.CallToolResult, DownloadOutput, error) {
			mode := attachmentMode(input.Mode)
			download, err := client.DownloadAttachment(ctx, input.ID, mode, normalizeDownloadMaxBytes(input.MaxBytes))
			if err != nil {
				if isNotFound(err) {
					return TextResult(StatusNotFound), DownloadOutput{Status: StatusNotFound, ID: input.ID}, nil
				}
				return nil, DownloadOutput{}, err
			}
			return downloadOutput(input.ID, download.Attachment.Filename, string(mode), download.ContentType, download.SourceURL, download.Content)
		})
}

func downloadPartImage(deps Dependencies) mcp.ToolHandlerFor[DownloadInput, DownloadOutput] {
	return LookupHandler[AttachmentLookupClient, DownloadInput, DownloadOutput](deps, DownloadPartImageToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentLookupClient, input DownloadInput) (*mcp.CallToolResult, DownloadOutput, error) {
			mode := attachmentMode(input.Mode)
			download, err := client.DownloadPartImage(ctx, input.ID, mode, normalizeDownloadMaxBytes(input.MaxBytes))
			if err != nil {
				if isNotFound(err) {
					return TextResult(StatusNotFound), DownloadOutput{Status: StatusNotFound, ID: input.ID}, nil
				}
				if errors.Is(err, inventree.ErrPartImageMissing) {
					return TextResult(StatusNoImage), DownloadOutput{Status: StatusNoImage, ID: input.ID, Mode: string(mode)}, nil
				}
				return nil, DownloadOutput{}, err
			}
			return downloadOutput(input.ID, download.Filename, string(mode), download.ContentType, download.SourceURL, download.Content)
		})
}

func previewPurchaseOrder(deps Dependencies) mcp.ToolHandlerFor[PurchasePreviewInput, PurchasePreviewOutput] {
	return LookupHandler[PurchasePreviewClient, PurchasePreviewInput, PurchasePreviewOutput](deps, PreviewPurchaseOrderToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchasePreviewClient, input PurchasePreviewInput) (*mcp.CallToolResult, PurchasePreviewOutput, error) {
			if input.SupplierID < 0 {
				clarification := NewClarification("Which supplier should be used for this preview?", "supplier", "supplier_id must be positive when provided", "supplier_id", true, nil, map[string]any{"supplier_id": input.SupplierID})
				return TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			if len(input.Lines) == 0 {
				clarification := NewClarification("Which purchase-order lines should be previewed?", "lines", "preview_purchase_order_with_lines requires at least one line", "lines", true, nil, nil)
				return TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}

			output := PurchasePreviewOutput{Status: StatusOK, SupplierID: input.SupplierID}
			for index, line := range input.Lines {
				if line.Quantity <= 0 {
					clarification := NewClarification("What quantity should be ordered for this line?", "quantity", "quantity must be greater than zero", "quantity", true, nil, map[string]any{"line_index": index, "part_id": line.PartID, "supplier_part_id": line.SupplierPartID})
					return TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: output.SupplierID, Clarification: &clarification}, nil
				}
				if line.UnitPrice != nil && strings.TrimSpace(line.Currency) == "" {
					clarification := NewClarification("Which currency applies to this preview price?", "currency", "currency is required when unit_price is supplied", "currency", true, nil, map[string]any{"line_index": index, "unit_price": *line.UnitPrice})
					return TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: output.SupplierID, Clarification: &clarification}, nil
				}

				supplierPart, result, clarificationOutput, ok, err := resolvePreviewSupplierPart(ctx, client, input.SupplierID, index, line)
				if err != nil || !ok {
					return result, clarificationOutput, err
				}
				if input.SupplierID == 0 && output.SupplierID == 0 {
					output.SupplierID = supplierPart.Supplier
				}
				if output.SupplierID != 0 && supplierPart.Supplier != output.SupplierID {
					clarification := NewClarification("Which supplier should be used for this preview?", "supplier", "supplier_part does not belong to the requested supplier", "supplier_id", true, candidatesFor([]inventree.SupplierPart{supplierPart}), map[string]any{"supplier_id": input.SupplierID, "line_index": index, "supplier_part_id": supplierPart.PK})
					return TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: output.SupplierID, Clarification: &clarification}, nil
				}

				lineOutput := PurchasePreviewLineOutput{
					Index:          index,
					PartID:         supplierPart.Part,
					SupplierID:     supplierPart.Supplier,
					SupplierPartID: supplierPart.PK,
					SupplierSKU:    supplierPart.SKU,
					Quantity:       line.Quantity,
					UnitPrice:      line.UnitPrice,
					Currency:       line.Currency,
					Notes:          line.Notes,
				}
				if line.UnitPrice != nil {
					total := *line.UnitPrice * line.Quantity
					lineOutput.LineTotal = &total
				} else {
					output.Warnings = append(output.Warnings, fmt.Sprintf("line %d has no unit_price; total omitted", index))
				}
				output.Lines = append(output.Lines, lineOutput)
			}
			return TextResult(StatusOK), output, nil
		})
}

func resolvePreviewSupplierPart(ctx context.Context, client PurchasePreviewClient, supplierID int, index int, line PurchasePreviewLineInput) (inventree.SupplierPart, *mcp.CallToolResult, PurchasePreviewOutput, bool, error) {
	if line.SupplierPartID < 0 {
		clarification := NewClarification("Which supplier part should be previewed?", "supplier_part", "supplier_part_id must be positive when provided", "supplier_part_id", true, nil, map[string]any{"line_index": index, "supplier_part_id": line.SupplierPartID})
		return inventree.SupplierPart{}, TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: supplierID, Clarification: &clarification}, false, nil
	}
	if line.SupplierPartID > 0 {
		record, err := client.GetSupplierPart(ctx, line.SupplierPartID)
		if err != nil {
			return inventree.SupplierPart{}, nil, PurchasePreviewOutput{}, false, err
		}
		if line.PartID < 0 {
			clarification := NewClarification("Which part should be ordered on this preview line?", "part", "part_id must be positive when provided", "part_id", true, candidatesFor([]inventree.SupplierPart{record}), map[string]any{"line_index": index, "part_id": line.PartID, "supplier_part_id": line.SupplierPartID})
			return inventree.SupplierPart{}, TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: supplierID, Clarification: &clarification}, false, nil
		}
		if line.PartID > 0 && record.Part != line.PartID {
			clarification := NewClarification("Which part should be ordered on this preview line?", "part", "supplier_part does not belong to the requested part", "part_id", true, candidatesFor([]inventree.SupplierPart{record}), map[string]any{"line_index": index, "part_id": line.PartID, "supplier_part_id": line.SupplierPartID})
			return inventree.SupplierPart{}, TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: supplierID, Clarification: &clarification}, false, nil
		}
		if strings.TrimSpace(line.SupplierSKU) != "" && line.SupplierSKU != record.SKU {
			clarification := NewClarification("Which supplier SKU should be used for this preview line?", "supplier_sku", "supplier_sku does not match the requested supplier_part_id", "supplier_sku", true, candidatesFor([]inventree.SupplierPart{record}), map[string]any{"line_index": index, "supplier_sku": line.SupplierSKU, "supplier_part_id": line.SupplierPartID})
			return inventree.SupplierPart{}, TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: supplierID, Clarification: &clarification}, false, nil
		}
		return record, nil, PurchasePreviewOutput{}, true, nil
	}
	if line.PartID <= 0 {
		clarification := NewClarification("Which part should be ordered on this preview line?", "part", "part_id is required when supplier_part_id is omitted", "part_id", true, nil, map[string]any{"line_index": index, "part_id": line.PartID})
		return inventree.SupplierPart{}, TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: supplierID, Clarification: &clarification}, false, nil
	}
	if supplierID <= 0 {
		clarification := NewClarification("Which supplier should be used for this preview line?", "supplier", "supplier_id is required when supplier_part_id is omitted", "supplier_id", true, nil, map[string]any{"line_index": index, "part_id": line.PartID})
		return inventree.SupplierPart{}, TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: supplierID, Clarification: &clarification}, false, nil
	}
	records, err := client.SearchSupplierParts(ctx, inventree.SupplierPartQuery{Part: line.PartID, Supplier: supplierID, SKU: line.SupplierSKU})
	if err != nil {
		return inventree.SupplierPart{}, nil, PurchasePreviewOutput{}, false, err
	}
	switch len(records) {
	case 0:
		clarification := NewClarification("Which supplier part should be used for this purchase preview line?", "supplier_part", "no supplier-part link matches the requested part and supplier", "supplier_part_id", true, nil, map[string]any{"line_index": index, "part_id": line.PartID, "supplier_id": supplierID, "supplier_sku": line.SupplierSKU})
		return inventree.SupplierPart{}, TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: supplierID, Clarification: &clarification}, false, nil
	case 1:
		return records[0], nil, PurchasePreviewOutput{}, true, nil
	default:
		clarification := NewClarification("Which supplier part should be used for this purchase preview line?", "supplier_part", "multiple supplier-part links match the requested part and supplier", "supplier_part_id", false, candidatesFor(records), map[string]any{"line_index": index, "part_id": line.PartID, "supplier_id": supplierID, "supplier_sku": line.SupplierSKU})
		return inventree.SupplierPart{}, TextResult(StatusClarificationRequired), PurchasePreviewOutput{Status: StatusClarificationRequired, SupplierID: supplierID, Clarification: &clarification}, false, nil
	}
}

func searchOutput[T any](records []T, search string, field string, retry string, question string, err error) (*mcp.CallToolResult, LookupOutput[T], error) {
	if err != nil {
		return nil, LookupOutput[T]{}, err
	}
	switch len(records) {
	case 0:
		return TextResult(StatusNotFound), LookupOutput[T]{Status: StatusNotFound}, nil
	case 1:
		return TextResult(StatusOK), LookupOutput[T]{Status: StatusOK, Count: 1, Results: records}, nil
	default:
		clarification := NewClarification(
			question,
			field,
			fmt.Sprintf("search matched multiple %s records", field),
			retry,
			false,
			candidatesFor(records),
			retryValues(search),
		)
		return TextResult(StatusClarificationRequired), LookupOutput[T]{
			Status:        StatusClarificationRequired,
			Count:         len(records),
			Results:       records,
			Clarification: &clarification,
		}, nil
	}
}

func listOutput[T any](records []T, err error) (*mcp.CallToolResult, LookupOutput[T], error) {
	if err != nil {
		return nil, LookupOutput[T]{}, err
	}
	if len(records) == 0 {
		return TextResult(StatusNotFound), LookupOutput[T]{Status: StatusNotFound}, nil
	}
	return TextResult(StatusOK), LookupOutput[T]{Status: StatusOK, Count: len(records), Results: records}, nil
}

func recordOutput[T any](record T, err error) (*mcp.CallToolResult, RecordOutput[T], error) {
	if err != nil {
		if isNotFound(err) {
			return TextResult(StatusNotFound), RecordOutput[T]{Status: StatusNotFound}, nil
		}
		return nil, RecordOutput[T]{}, err
	}
	return TextResult(StatusOK), RecordOutput[T]{Status: StatusOK, Record: record}, nil
}

func searchQuery(input SearchInput) inventree.SearchQuery {
	return inventree.SearchQuery{
		Search: input.Search,
		Limit:  NormalizeLookupLimit(input.Limit),
		Offset: input.Offset,
	}
}

func attachmentMode(raw string) inventree.AttachmentContentMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(inventree.AttachmentContentOriginal):
		return inventree.AttachmentContentOriginal
	case string(inventree.AttachmentContentThumbnail):
		return inventree.AttachmentContentThumbnail
	default:
		return inventree.AttachmentContentMode(raw)
	}
}

func normalizeDownloadMaxBytes(maxBytes int64) int64 {
	if maxBytes <= 0 {
		return defaultDownloadMaxBytes
	}
	if maxBytes > maxDownloadMaxBytes {
		return maxDownloadMaxBytes
	}
	return maxBytes
}

func downloadOutput(id int, filename string, mode string, contentType string, sourceURL string, content []byte) (*mcp.CallToolResult, DownloadOutput, error) {
	sum := sha256.Sum256(content)
	out := DownloadOutput{
		Status:      StatusOK,
		ID:          id,
		Filename:    filename,
		ContentType: contentType,
		Size:        len(content),
		SHA256:      hex.EncodeToString(sum[:]),
		Mode:        mode,
		SourceURL:   sourceURL,
	}
	if isTextContent(contentType, content) {
		out.Text = string(content)
	} else {
		out.Base64 = base64.StdEncoding.EncodeToString(content)
	}
	return TextResult(StatusOK), out, nil
}

func isTextContent(contentType string, content []byte) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && (strings.HasPrefix(mediaType, "text/") || mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")) {
		return utf8.Valid(content)
	}
	return contentType == "" && utf8.Valid(content)
}

func retryValues(search string) map[string]any {
	if search == "" {
		return nil
	}
	return map[string]any{"search": search}
}

func candidatesFor[T any](records []T) []ClarificationCandidate {
	candidates := make([]ClarificationCandidate, 0, len(records))
	for _, record := range records {
		candidates = append(candidates, candidateFor(record))
	}
	return candidates
}

func candidateFor(record any) ClarificationCandidate {
	switch v := record.(type) {
	case inventree.Part:
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.Name, Summary: v.Description, APIURL: fmt.Sprintf("/api/part/%d/", v.PK)}
	case inventree.Category:
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.Name, Summary: v.Description, APIURL: fmt.Sprintf("/api/part/category/%d/", v.PK), Fields: map[string]any{"structural": v.Structural}}
	case inventree.ParameterTemplate:
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.Name, APIURL: fmt.Sprintf("/api/parameter/template/%d/", v.PK), Fields: map[string]any{"units": v.Units, "choices": v.Choices, "checkbox": v.Checkbox, "enabled": v.Enabled}}
	case inventree.Parameter:
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: strconv.Itoa(v.Template), APIURL: fmt.Sprintf("/api/parameter/%d/", v.PK), Fields: map[string]any{"template": v.Template, "model_type": v.ModelType, "model_id": v.ModelID, "data": v.Data}}
	case inventree.Company:
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.Name, Summary: v.Description, APIURL: fmt.Sprintf("/api/company/%d/", v.PK), Fields: map[string]any{"supplier": v.IsSupplier, "manufacturer": v.IsManufacturer, "active": v.Active}}
	case inventree.SupplierPart:
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.SKU, Summary: v.Description, APIURL: fmt.Sprintf("/api/company/part/%d/", v.PK), Fields: map[string]any{"part": v.Part, "supplier": v.Supplier, "active": v.Active, "primary": v.Primary}}
	case inventree.ManufacturerPart:
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.MPN, Summary: v.Description, APIURL: fmt.Sprintf("/api/company/part/manufacturer/%d/", v.PK), Fields: map[string]any{"part": v.Part, "manufacturer": v.Manufacturer}}
	case inventree.StockLocation:
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.Name, Summary: v.Description, APIURL: fmt.Sprintf("/api/stock/location/%d/", v.PK), Fields: map[string]any{"structural": v.Structural, "external": v.External}}
	case inventree.StockLocationType:
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.Name, Summary: v.Description, APIURL: fmt.Sprintf("/api/stock/location-type/%d/", v.PK)}
	case inventree.StockItem:
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: fmt.Sprintf("stock item %d", v.PK), APIURL: fmt.Sprintf("/api/stock/%d/", v.PK), Fields: map[string]any{"part": v.Part, "location": v.Location, "quantity": v.Quantity, "status": v.Status, "serial": v.Serial, "batch": v.Batch}}
	case inventree.PurchaseOrderLineItem:
		fields := map[string]any{"order": v.Order, "supplier_part_id": v.Part, "quantity": v.Quantity, "received": v.Received, "destination": v.Destination, "purchase_price_currency": v.PurchasePriceCurrency}
		if v.PurchasePrice != nil {
			fields["purchase_price"] = *v.PurchasePrice
		}
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.Reference, Summary: v.Notes, APIURL: fmt.Sprintf("/api/order/po-line/%d/", v.PK), Fields: fields}
	case inventree.PurchaseOrderExtraLine:
		fields := map[string]any{"order": v.Order, "quantity": v.Quantity, "price_currency": v.PriceCurrency}
		if v.Price != nil {
			fields["price"] = *v.Price
		}
		if v.Link != "" {
			fields["link"] = redactedMetadataURL(&v.Link)
		}
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.Reference, Summary: v.Description, APIURL: fmt.Sprintf("/api/order/po-extra-line/%d/", v.PK), Fields: fields}
	case inventree.Attachment:
		fields := map[string]any{
			"model_type": v.ModelType,
			"model_id":   v.ModelID,
			"is_file":    v.Attachment != nil && *v.Attachment != "",
			"is_link":    v.IsLink,
		}
		if v.FileSize != nil {
			fields["file_size"] = *v.FileSize
		}
		return ClarificationCandidate{ID: strconv.Itoa(v.PK), Label: v.Filename, Summary: v.Comment, APIURL: fmt.Sprintf("/api/attachment/%d/", v.PK), Fields: fields}
	default:
		return ClarificationCandidate{ID: fmt.Sprint(record), Label: fmt.Sprint(record)}
	}
}

func validateAttachmentModelType(modelType string) error {
	if !inScopeAttachmentModelTypes[modelType] {
		return fmt.Errorf("attachment model type %q is out of scope", modelType)
	}
	return nil
}

func sanitizeAttachments(records []inventree.Attachment) []AttachmentMetadata {
	sanitized := make([]AttachmentMetadata, 0, len(records))
	for _, record := range records {
		sanitized = append(sanitized, sanitizeAttachment(record))
	}
	return sanitized
}

func sanitizeAttachment(record inventree.Attachment) AttachmentMetadata {
	return AttachmentMetadata{
		PK:            record.PK,
		ModelType:     record.ModelType,
		ModelID:       record.ModelID,
		Filename:      record.Filename,
		Comment:       record.Comment,
		IsImage:       record.IsImage,
		IsLink:        record.IsLink,
		FileSize:      record.FileSize,
		Tags:          record.Tags,
		UploadDate:    record.UploadDate,
		UploadUser:    record.UploadUser,
		HasFile:       record.Attachment != nil && *record.Attachment != "",
		HasThumbnail:  record.Thumbnail != nil && *record.Thumbnail != "",
		AttachmentURL: redactedMetadataURL(record.Attachment),
		ThumbnailURL:  redactedMetadataURL(record.Thumbnail),
		LinkURL:       redactedMetadataURL(record.Link),
	}
}

func redactedMetadataURL(raw *string) string {
	if raw == nil || *raw == "" {
		return ""
	}
	parsed, err := url.Parse(*raw)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func isNotFound(err error) bool {
	var apiErr *inventree.APIError
	return errors.As(err, &apiErr) && apiErr.Kind == inventree.ErrorKindNotFound
}
