package tools

import (
	"context"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GlobalSearchClient interface {
	GlobalSearch(context.Context, inventree.GlobalSearchQuery) (inventree.GlobalSearchResult, error)
}

type GlobalSearchInput struct {
	Search      string   `json:"search" jsonschema:"Search text."`
	ObjectTypes []string `json:"object_types,omitempty" jsonschema:"Object types to search. One or more of: part, partcategory, stockitem, stocklocation, company, supplierpart, manufacturerpart, purchaseorder. Defaults to all supported types when omitted. Does not support salesorder, returnorder, or build: InvenTree's search endpoint recognizes those, but inventree-mcp has no exact-read tool to route their results to yet."`
	SearchRegex bool     `json:"search_regex,omitempty" jsonschema:"Treat search as a regular expression instead of plain text."`
	SearchWhole bool     `json:"search_whole,omitempty" jsonschema:"Match whole words only."`
	SearchNotes bool     `json:"search_notes,omitempty" jsonschema:"Also match free-text notes/description fields, not only names and identifiers."`
	Limit       int      `json:"limit,omitempty" jsonschema:"Maximum number of records to return per object type. Defaults to 20 and is capped at 100."`
}

// GlobalSearchTypeResult carries one requested object type's bounded matches
// plus DetailTool, the exact get_* tool name to call with a result's PK for
// a complete read. Only object types actually requested (and returned by
// InvenTree) appear in GlobalSearchOutput.
type GlobalSearchTypeResult[T any] struct {
	Count      int    `json:"count"`
	DetailTool string `json:"detail_tool"`
	Results    []T    `json:"results,omitempty"`
}

type GlobalSearchOutput struct {
	Status            string                                              `json:"status"`
	Parts             *GlobalSearchTypeResult[inventree.Part]             `json:"parts,omitempty"`
	PartCategories    *GlobalSearchTypeResult[inventree.Category]         `json:"part_categories,omitempty"`
	StockItems        *GlobalSearchTypeResult[inventree.StockItem]        `json:"stock_items,omitempty"`
	StockLocations    *GlobalSearchTypeResult[inventree.StockLocation]    `json:"stock_locations,omitempty"`
	Companies         *GlobalSearchTypeResult[inventree.Company]          `json:"companies,omitempty"`
	SupplierParts     *GlobalSearchTypeResult[inventree.SupplierPart]     `json:"supplier_parts,omitempty"`
	ManufacturerParts *GlobalSearchTypeResult[inventree.ManufacturerPart] `json:"manufacturer_parts,omitempty"`
	PurchaseOrders    *GlobalSearchTypeResult[inventree.PurchaseOrder]    `json:"purchase_orders,omitempty"`
}

func registerGlobalSearchTool(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, GlobalSearchToolName, "Global search",
		"Searches across multiple InvenTree object types (parts, categories, stock items, stock locations, companies, supplier parts, manufacturer parts, purchase orders) in one bounded call. Each match includes the exact get_* tool to call for a complete read; prefer the dedicated search_* tools when you already know the object type.",
		globalSearch(deps))
}

func globalSearch(deps Dependencies) mcp.ToolHandlerFor[GlobalSearchInput, GlobalSearchOutput] {
	return LookupHandler[GlobalSearchClient, GlobalSearchInput, GlobalSearchOutput](deps, GlobalSearchToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client GlobalSearchClient, input GlobalSearchInput) (*mcp.CallToolResult, GlobalSearchOutput, error) {
			objectTypes := make([]inventree.GlobalSearchObjectType, 0, len(input.ObjectTypes))
			for _, raw := range input.ObjectTypes {
				objectTypes = append(objectTypes, inventree.GlobalSearchObjectType(raw))
			}
			if len(objectTypes) == 0 {
				objectTypes = inventree.SupportedGlobalSearchObjectTypes
			}

			result, err := client.GlobalSearch(ctx, inventree.GlobalSearchQuery{
				Search:      input.Search,
				SearchRegex: input.SearchRegex,
				SearchWhole: input.SearchWhole,
				SearchNotes: input.SearchNotes,
				ObjectTypes: objectTypes,
				Limit:       NormalizeLookupLimit(input.Limit),
			})
			if err != nil {
				return nil, GlobalSearchOutput{}, err
			}
			output := globalSearchOutput(result)
			return TextResult(output.Status), output, nil
		})
}

func globalSearchOutput(result inventree.GlobalSearchResult) GlobalSearchOutput {
	output := GlobalSearchOutput{Status: StatusNotFound}
	if result.Parts != nil {
		output.Parts = &GlobalSearchTypeResult[inventree.Part]{Count: result.Parts.Count, DetailTool: GetPartToolName, Results: result.Parts.Results}
	}
	if result.PartCategories != nil {
		output.PartCategories = &GlobalSearchTypeResult[inventree.Category]{Count: result.PartCategories.Count, DetailTool: GetPartCategoryToolName, Results: result.PartCategories.Results}
	}
	if result.StockItems != nil {
		output.StockItems = &GlobalSearchTypeResult[inventree.StockItem]{Count: result.StockItems.Count, DetailTool: GetStockItemToolName, Results: result.StockItems.Results}
	}
	if result.StockLocations != nil {
		output.StockLocations = &GlobalSearchTypeResult[inventree.StockLocation]{Count: result.StockLocations.Count, DetailTool: GetStockLocationToolName, Results: result.StockLocations.Results}
	}
	if result.Companies != nil {
		output.Companies = &GlobalSearchTypeResult[inventree.Company]{Count: result.Companies.Count, DetailTool: GetCompanyToolName, Results: result.Companies.Results}
	}
	if result.SupplierParts != nil {
		output.SupplierParts = &GlobalSearchTypeResult[inventree.SupplierPart]{Count: result.SupplierParts.Count, DetailTool: GetSupplierPartToolName, Results: result.SupplierParts.Results}
	}
	if result.ManufacturerParts != nil {
		output.ManufacturerParts = &GlobalSearchTypeResult[inventree.ManufacturerPart]{Count: result.ManufacturerParts.Count, DetailTool: GetManufacturerPartToolName, Results: result.ManufacturerParts.Results}
	}
	if result.PurchaseOrders != nil {
		output.PurchaseOrders = &GlobalSearchTypeResult[inventree.PurchaseOrder]{Count: result.PurchaseOrders.Count, DetailTool: GetPurchaseOrderToolName, Results: result.PurchaseOrders.Results}
	}
	if globalSearchTotalCount(output) > 0 {
		output.Status = StatusOK
	}
	return output
}

func globalSearchTotalCount(output GlobalSearchOutput) int {
	total := 0
	if output.Parts != nil {
		total += output.Parts.Count
	}
	if output.PartCategories != nil {
		total += output.PartCategories.Count
	}
	if output.StockItems != nil {
		total += output.StockItems.Count
	}
	if output.StockLocations != nil {
		total += output.StockLocations.Count
	}
	if output.Companies != nil {
		total += output.Companies.Count
	}
	if output.SupplierParts != nil {
		total += output.SupplierParts.Count
	}
	if output.ManufacturerParts != nil {
		total += output.ManufacturerParts.Count
	}
	if output.PurchaseOrders != nil {
		total += output.PurchaseOrders.Count
	}
	return total
}
