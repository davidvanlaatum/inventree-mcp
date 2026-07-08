package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PartWriteClient interface {
	SearchParts(context.Context, inventree.SearchQuery) ([]inventree.Part, error)
	CreatePart(context.Context, inventree.PartCreate) (inventree.Part, error)
	UpdatePart(context.Context, int, inventree.PatchFields) (inventree.Part, error)
}

type CompanyWriteClient interface {
	SearchCompanies(context.Context, inventree.SearchQuery) ([]inventree.Company, error)
	CreateCompany(context.Context, inventree.CompanyCreate) (inventree.Company, error)
}

type SupplierPartWriteClient interface {
	SearchSupplierParts(context.Context, inventree.SupplierPartQuery) ([]inventree.SupplierPart, error)
	CreateSupplierPart(context.Context, inventree.SupplierPartCreate) (inventree.SupplierPart, error)
}

type ManufacturerPartWriteClient interface {
	SearchManufacturerParts(context.Context, inventree.ManufacturerPartQuery) ([]inventree.ManufacturerPart, error)
	CreateManufacturerPart(context.Context, inventree.ManufacturerPartCreate) (inventree.ManufacturerPart, error)
}

type StockItemWriteClient interface {
	SearchStockItems(context.Context, inventree.StockItemQuery) ([]inventree.StockItem, error)
	CreateStockItem(context.Context, inventree.StockItemCreate) (inventree.StockItem, error)
}

type InitialStockWorkflowClient interface {
	SearchParts(context.Context, inventree.SearchQuery) ([]inventree.Part, error)
	GetPart(context.Context, int) (inventree.Part, error)
	SearchStockLocations(context.Context, inventree.SearchQuery) ([]inventree.StockLocation, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	SearchStockItems(context.Context, inventree.StockItemQuery) ([]inventree.StockItem, error)
	CreateStockItem(context.Context, inventree.StockItemCreate) (inventree.StockItem, error)
}

type ParameterWriteClient interface {
	GetPart(context.Context, int) (inventree.Part, error)
	SearchPartParameters(context.Context, inventree.PartParameterQuery) ([]inventree.Parameter, error)
	SearchParameterTemplates(context.Context, inventree.SearchQuery) ([]inventree.ParameterTemplate, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
	SearchCategoryParameterTemplates(context.Context, inventree.CategoryParameterTemplateQuery) ([]inventree.CategoryParameterTemplate, error)
	CreatePartParameter(context.Context, inventree.ParameterCreate) (inventree.Parameter, error)
	UpdatePartParameter(context.Context, int, inventree.PatchFields) (inventree.Parameter, error)
}

type PartUpsertWorkflowClient interface {
	SearchParts(context.Context, inventree.SearchQuery) ([]inventree.Part, error)
	GetPart(context.Context, int) (inventree.Part, error)
	CreatePart(context.Context, inventree.PartCreate) (inventree.Part, error)
	UpdatePart(context.Context, int, inventree.PatchFields) (inventree.Part, error)
	SearchSuppliers(context.Context, inventree.SearchQuery) ([]inventree.Company, error)
	SearchManufacturers(context.Context, inventree.SearchQuery) ([]inventree.Company, error)
	CreateCompany(context.Context, inventree.CompanyCreate) (inventree.Company, error)
	SearchSupplierParts(context.Context, inventree.SupplierPartQuery) ([]inventree.SupplierPart, error)
	CreateSupplierPart(context.Context, inventree.SupplierPartCreate) (inventree.SupplierPart, error)
	SearchManufacturerParts(context.Context, inventree.ManufacturerPartQuery) ([]inventree.ManufacturerPart, error)
	CreateManufacturerPart(context.Context, inventree.ManufacturerPartCreate) (inventree.ManufacturerPart, error)
}

type CreatePartInput struct {
	Name            string  `json:"name" jsonschema:"Part name."`
	Description     string  `json:"description,omitempty" jsonschema:"Optional part description."`
	CategoryID      int     `json:"category_id" jsonschema:"Existing InvenTree part category primary key."`
	IPN             string  `json:"ipn,omitempty" jsonschema:"Optional internal part number."`
	Units           *string `json:"units,omitempty" jsonschema:"Optional unit of measure."`
	Active          *bool   `json:"active,omitempty" jsonschema:"Optional explicit active flag."`
	Assembly        *bool   `json:"assembly,omitempty" jsonschema:"Optional explicit assembly flag."`
	Component       *bool   `json:"component,omitempty" jsonschema:"Optional explicit component flag."`
	Purchaseable    *bool   `json:"purchaseable,omitempty" jsonschema:"Optional explicit purchasable flag."`
	Trackable       *bool   `json:"trackable,omitempty" jsonschema:"Optional explicit trackable flag."`
	Virtual         *bool   `json:"virtual,omitempty" jsonschema:"Optional explicit virtual flag."`
	DefaultLocation *int    `json:"default_location_id,omitempty" jsonschema:"Optional existing stock location primary key."`
}

type UpdatePartInput struct {
	ID              int     `json:"id" jsonschema:"Stable InvenTree part primary key."`
	Name            *string `json:"name,omitempty" jsonschema:"Optional replacement part name."`
	Description     *string `json:"description,omitempty" jsonschema:"Optional replacement description."`
	CategoryID      *int    `json:"category_id,omitempty" jsonschema:"Optional existing category primary key."`
	IPN             *string `json:"ipn,omitempty" jsonschema:"Optional replacement internal part number."`
	Units           *string `json:"units,omitempty" jsonschema:"Optional replacement units."`
	Active          *bool   `json:"active,omitempty" jsonschema:"Optional explicit active flag."`
	Assembly        *bool   `json:"assembly,omitempty" jsonschema:"Optional explicit assembly flag."`
	Component       *bool   `json:"component,omitempty" jsonschema:"Optional explicit component flag."`
	Purchaseable    *bool   `json:"purchaseable,omitempty" jsonschema:"Optional explicit purchasable flag."`
	Trackable       *bool   `json:"trackable,omitempty" jsonschema:"Optional explicit trackable flag."`
	Virtual         *bool   `json:"virtual,omitempty" jsonschema:"Optional explicit virtual flag."`
	DefaultLocation *int    `json:"default_location_id,omitempty" jsonschema:"Optional existing stock location primary key."`
}

type CreateCompanyInput struct {
	Name           string `json:"name" jsonschema:"Company name."`
	Description    string `json:"description,omitempty" jsonschema:"Optional company description."`
	Currency       string `json:"currency,omitempty" jsonschema:"Default supplier currency."`
	Website        string `json:"website,omitempty" jsonschema:"Optional company website URL."`
	IsSupplier     bool   `json:"is_supplier,omitempty" jsonschema:"Create the company with supplier role."`
	IsManufacturer bool   `json:"is_manufacturer,omitempty" jsonschema:"Create the company with manufacturer role."`
}

type CreateSupplierPartInput struct {
	PartID             int     `json:"part_id" jsonschema:"Existing purchasable part primary key."`
	SupplierID         int     `json:"supplier_id" jsonschema:"Existing supplier company primary key."`
	SKU                string  `json:"sku" jsonschema:"Supplier SKU."`
	Description        *string `json:"description,omitempty" jsonschema:"Optional supplier part description."`
	Link               *string `json:"link,omitempty" jsonschema:"Optional external supplier part URL."`
	Active             *bool   `json:"active,omitempty" jsonschema:"Optional explicit active flag."`
	Primary            *bool   `json:"primary,omitempty" jsonschema:"Optional explicit primary flag."`
	ManufacturerPartID *int    `json:"manufacturer_part_id,omitempty" jsonschema:"Optional existing manufacturer-part primary key."`
	Packaging          *string `json:"packaging,omitempty" jsonschema:"Optional packaging text."`
	Note               *string `json:"note,omitempty" jsonschema:"Optional short note."`
}

type CreateManufacturerPartInput struct {
	PartID         int     `json:"part_id" jsonschema:"Existing part primary key."`
	ManufacturerID int     `json:"manufacturer_id" jsonschema:"Existing manufacturer company primary key."`
	MPN            *string `json:"mpn,omitempty" jsonschema:"Optional manufacturer part number."`
	Description    *string `json:"description,omitempty" jsonschema:"Optional manufacturer part description."`
	Link           *string `json:"link,omitempty" jsonschema:"Optional external manufacturer part URL."`
}

type CreateStockItemInput struct {
	PartID     int     `json:"part_id" jsonschema:"Existing part primary key."`
	LocationID int     `json:"location_id" jsonschema:"Existing stock location primary key."`
	Quantity   float64 `json:"quantity" jsonschema:"Initial stock quantity. Must be greater than zero."`
	Status     *int    `json:"status,omitempty" jsonschema:"Optional InvenTree stock status code."`
	Batch      *string `json:"batch,omitempty" jsonschema:"Optional batch code."`
	Serial     *string `json:"serial,omitempty" jsonschema:"Optional serial number."`
	Notes      *string `json:"notes,omitempty" jsonschema:"Optional markdown notes."`
}

type SetPartParametersInput struct {
	PartID     int                 `json:"part_id" jsonschema:"Existing part primary key."`
	Parameters []ParameterSetInput `json:"parameters" jsonschema:"Parameter values to create or update."`
}

type UpsertPartWorkflowInput struct {
	DryRun               bool    `json:"dry_run,omitempty" jsonschema:"When true, return a write plan without creating or updating records."`
	PartID               int     `json:"part_id,omitempty" jsonschema:"Existing part primary key to update or link."`
	Name                 string  `json:"name,omitempty" jsonschema:"Part name to search or create when part_id is omitted."`
	Description          *string `json:"description,omitempty" jsonschema:"Optional replacement or creation part description."`
	CategoryID           int     `json:"category_id,omitempty" jsonschema:"Existing category primary key required when creating a part."`
	IPN                  *string `json:"ipn,omitempty" jsonschema:"Optional internal part number."`
	Units                *string `json:"units,omitempty" jsonschema:"Optional unit of measure."`
	Purchaseable         *bool   `json:"purchaseable,omitempty" jsonschema:"Optional explicit purchasable flag."`
	DefaultLocation      *int    `json:"default_location_id,omitempty" jsonschema:"Optional existing stock location primary key."`
	SupplierID           int     `json:"supplier_id,omitempty" jsonschema:"Existing supplier company primary key."`
	SupplierName         string  `json:"supplier_name,omitempty" jsonschema:"Supplier company name to search or create when supplier_id is omitted."`
	SupplierCurrency     string  `json:"supplier_currency,omitempty" jsonschema:"Currency required when creating a supplier company."`
	SupplierSKU          string  `json:"supplier_sku,omitempty" jsonschema:"Supplier SKU required when creating a supplier-part link."`
	ManufacturerID       int     `json:"manufacturer_id,omitempty" jsonschema:"Existing manufacturer company primary key."`
	ManufacturerName     string  `json:"manufacturer_name,omitempty" jsonschema:"Manufacturer company name to search or create when manufacturer_id is omitted."`
	ManufacturerCurrency string  `json:"manufacturer_currency,omitempty" jsonschema:"Currency required when creating a manufacturer company."`
	MPN                  *string `json:"mpn,omitempty" jsonschema:"Optional manufacturer part number."`
	Link                 *string `json:"link,omitempty" jsonschema:"Optional supplier/manufacturer part URL."`
}

type InitialStockWorkflowInput struct {
	DryRun         bool    `json:"dry_run,omitempty" jsonschema:"When true, return a stock-entry plan without creating stock."`
	PartID         int     `json:"part_id,omitempty" jsonschema:"Existing part primary key."`
	PartSearch     string  `json:"part_search,omitempty" jsonschema:"Part search text when part_id is omitted."`
	LocationID     int     `json:"location_id,omitempty" jsonschema:"Existing stock location primary key."`
	LocationSearch string  `json:"location_search,omitempty" jsonschema:"Stock location search text when location_id is omitted."`
	Quantity       float64 `json:"quantity" jsonschema:"Initial stock quantity. Must be greater than zero."`
	Status         *int    `json:"status,omitempty" jsonschema:"Optional InvenTree stock status code."`
	Batch          *string `json:"batch,omitempty" jsonschema:"Optional batch code."`
	Serial         *string `json:"serial,omitempty" jsonschema:"Optional serial number."`
	Notes          *string `json:"notes,omitempty" jsonschema:"Optional markdown notes."`
}

type ParameterSetInput struct {
	Name        string   `json:"name,omitempty" jsonschema:"Existing parameter template name when template_id is not supplied."`
	TemplateID  *int     `json:"template_id,omitempty" jsonschema:"Existing parameter template primary key."`
	Value       *string  `json:"value,omitempty" jsonschema:"Explicit string parameter value. Empty string is preserved when supplied."`
	BoolValue   *bool    `json:"bool_value,omitempty" jsonschema:"Explicit boolean parameter value. False is preserved when supplied."`
	NumberValue *float64 `json:"number_value,omitempty" jsonschema:"Explicit numeric parameter value. Zero is preserved when supplied."`
}

type WriteRecordOutput[T any] struct {
	Status        string                 `json:"status"`
	Record        T                      `json:"record,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
}

type PartUpsertWorkflowOutput struct {
	Status                   string                      `json:"status"`
	DryRun                   bool                        `json:"dry_run"`
	Actions                  []PartUpsertWorkflowAction  `json:"actions"`
	Part                     *inventree.Part             `json:"part,omitempty"`
	Supplier                 *inventree.Company          `json:"supplier,omitempty"`
	Manufacturer             *inventree.Company          `json:"manufacturer,omitempty"`
	SupplierPart             *inventree.SupplierPart     `json:"supplier_part,omitempty"`
	ManufacturerPart         *inventree.ManufacturerPart `json:"manufacturer_part,omitempty"`
	OmittedRecommendedFields []string                    `json:"omitted_recommended_fields,omitempty"`
	Clarification            *ClarificationResponse      `json:"clarification,omitempty"`
}

type InitialStockWorkflowOutput struct {
	Status        string                       `json:"status"`
	DryRun        bool                         `json:"dry_run"`
	Actions       []InitialStockWorkflowAction `json:"actions,omitempty"`
	Part          *inventree.Part              `json:"part,omitempty"`
	Location      *inventree.StockLocation     `json:"location,omitempty"`
	StockItem     *inventree.StockItem         `json:"stock_item,omitempty"`
	Clarification *ClarificationResponse       `json:"clarification,omitempty"`
}

type InitialStockWorkflowAction struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	RecordType string `json:"record_type,omitempty"`
	ID         int    `json:"id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type PartUpsertWorkflowAction struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	RecordType string `json:"record_type,omitempty"`
	ID         int    `json:"id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type parameterWritePlan struct {
	data     string
	template int
	existing *inventree.Parameter
}

func registerWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, CreatePartToolName, "Create part", "Creates an InvenTree part in an existing category.", createPart(deps))
	addWriteTool(server, deps, UpdatePartToolName, "Update part", "Partially updates an InvenTree part.", updatePart(deps))
	addWriteTool(server, deps, SetPartParametersToolName, "Set part parameters", "Creates or updates part parameter values using existing linked templates.", setPartParameters(deps))
	addWriteTool(server, deps, CreateCompanyToolName, "Create company", "Creates a supplier and/or manufacturer company.", createCompany(deps))
	addWriteTool(server, deps, CreateSupplierPartToolName, "Create supplier part", "Creates a supplier-part link for existing records.", createSupplierPart(deps))
	addWriteTool(server, deps, CreateManufacturerPartToolName, "Create manufacturer part", "Creates a manufacturer-part link for existing records.", createManufacturerPart(deps))
	addWriteTool(server, deps, UpsertPartWorkflowToolName, "Upsert part with supplier and manufacturer", "Plans or performs a safe part upsert with supplier and manufacturer links.", upsertPartWorkflow(deps))
	addWriteTool(server, deps, CreateStockItemToolName, "Create stock item", "Creates initial stock after checking for duplicate stock at the same part and location.", createStockItem(deps))
	addWriteTool(server, deps, InitialStockWorkflowToolName, "Create initial stock entry", "Plans or creates initial stock after resolving the part, location, and duplicate guard.", initialStockWorkflow(deps))
	registerAttachmentWriteTools(server, deps)
}

func addWriteTool[In, Out any](server *mcp.Server, deps Dependencies, name string, title string, description string, handler mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(server, ToolDescriptor(name, title, description), GuardTool(deps, name, handler))
}

func createPart(deps Dependencies) mcp.ToolHandlerFor[CreatePartInput, WriteRecordOutput[inventree.Part]] {
	return LookupHandler[PartWriteClient, CreatePartInput, WriteRecordOutput[inventree.Part]](deps, CreatePartToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartWriteClient, input CreatePartInput) (*mcp.CallToolResult, WriteRecordOutput[inventree.Part], error) {
			if input.CategoryID <= 0 {
				return hardClarification[inventree.Part]("Which existing category should contain the new part?", "category_id", "create_part requires an existing category_id", "category_id", map[string]any{"name": input.Name})
			}
			if input.DefaultLocation != nil && *input.DefaultLocation <= 0 {
				return hardClarification[inventree.Part]("Which default stock location should be used?", "default_location_id", "default_location_id must be positive when provided", "default_location_id", map[string]any{"default_location_id": *input.DefaultLocation})
			}
			if input.Name != "" {
				records, err := client.SearchParts(ctx, inventree.SearchQuery{Search: input.Name, Limit: DefaultLookupLimit})
				if err != nil {
					return nil, WriteRecordOutput[inventree.Part]{}, err
				}
				if len(records) > 0 {
					clarification := NewClarification("Should an existing part be used instead of creating a new one?", "part", "matching part records already exist", "part_id", false, candidatesFor(records), map[string]any{"name": input.Name})
					return TextResult(StatusClarificationRequired), WriteRecordOutput[inventree.Part]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
				}
			}
			record, err := client.CreatePart(ctx, inventree.PartCreate{
				Name:            input.Name,
				Description:     input.Description,
				Category:        &input.CategoryID,
				IPN:             input.IPN,
				Units:           input.Units,
				Active:          input.Active,
				Assembly:        input.Assembly,
				Component:       input.Component,
				Purchaseable:    input.Purchaseable,
				Trackable:       input.Trackable,
				Virtual:         input.Virtual,
				DefaultLocation: input.DefaultLocation,
			})
			return writeRecordOutput(record, err)
		})
}

func updatePart(deps Dependencies) mcp.ToolHandlerFor[UpdatePartInput, WriteRecordOutput[inventree.Part]] {
	return LookupHandler[PartWriteClient, UpdatePartInput, WriteRecordOutput[inventree.Part]](deps, UpdatePartToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartWriteClient, input UpdatePartInput) (*mcp.CallToolResult, WriteRecordOutput[inventree.Part], error) {
			if input.ID <= 0 {
				return hardClarification[inventree.Part]("Which part should be updated?", "part", "update_part requires a positive part id", "id", map[string]any{"id": input.ID})
			}
			if input.CategoryID != nil && *input.CategoryID <= 0 {
				return hardClarification[inventree.Part]("Which category should contain this part?", "category_id", "category_id must be positive when provided", "category_id", map[string]any{"category_id": *input.CategoryID})
			}
			if input.DefaultLocation != nil && *input.DefaultLocation <= 0 {
				return hardClarification[inventree.Part]("Which default stock location should be used?", "default_location_id", "default_location_id must be positive when provided", "default_location_id", map[string]any{"default_location_id": *input.DefaultLocation})
			}
			fields := partPatchFields(input)
			if len(fields) == 0 {
				return hardClarification[inventree.Part]("Which part fields should be updated?", "part", "update_part requires at least one PATCH field", "id", map[string]any{"id": input.ID})
			}
			record, err := client.UpdatePart(ctx, input.ID, fields)
			return writeRecordOutput(record, err)
		})
}

func setPartParameters(deps Dependencies) mcp.ToolHandlerFor[SetPartParametersInput, WriteRecordOutput[[]inventree.Parameter]] {
	return LookupHandler[ParameterWriteClient, SetPartParametersInput, WriteRecordOutput[[]inventree.Parameter]](deps, SetPartParametersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ParameterWriteClient, input SetPartParametersInput) (*mcp.CallToolResult, WriteRecordOutput[[]inventree.Parameter], error) {
			if input.PartID <= 0 {
				return hardClarification[[]inventree.Parameter]("Which part should receive these parameters?", "part", "set_part_parameters requires a positive part_id", "part_id", map[string]any{"part_id": input.PartID})
			}
			if len(input.Parameters) == 0 {
				return hardClarification[[]inventree.Parameter]("Which parameter values should be set?", "parameters", "set_part_parameters requires at least one parameter", "parameters", map[string]any{"part_id": input.PartID})
			}
			part, err := client.GetPart(ctx, input.PartID)
			if err != nil {
				return writeRecordOutput([]inventree.Parameter{}, err)
			}
			if part.Category == nil || *part.Category <= 0 {
				return hardClarification[[]inventree.Parameter]("Which category parameter link should authorize these parameters?", "category_id", "part has no category for category parameter link validation", "category_id", map[string]any{"part_id": input.PartID})
			}
			links, err := client.SearchCategoryParameterTemplates(ctx, inventree.CategoryParameterTemplateQuery{CategoryID: *part.Category})
			if err != nil {
				return nil, WriteRecordOutput[[]inventree.Parameter]{}, err
			}
			existing, err := client.SearchPartParameters(ctx, inventree.PartParameterQuery{PartID: input.PartID})
			if err != nil {
				return nil, WriteRecordOutput[[]inventree.Parameter]{}, err
			}
			plans := make([]parameterWritePlan, 0, len(input.Parameters))
			seenTemplates := map[int]ParameterSetInput{}
			for _, parameter := range input.Parameters {
				data, result, output, ok := parameterData(parameter, input.PartID)
				if !ok {
					return result, output, nil
				}
				templateID, result, output, ok, err := resolveParameterTemplate(ctx, client, *part.Category, links, existing, parameter)
				if err != nil {
					return nil, WriteRecordOutput[[]inventree.Parameter]{}, err
				}
				if !ok {
					return result, output, nil
				}
				if prior, seen := seenTemplates[templateID]; seen {
					retryValues := map[string]any{
						"part_id":     input.PartID,
						"template_id": templateID,
						"first":       retryParameterValues(0, prior),
						"duplicate":   retryParameterValues(0, parameter),
					}
					clarification := NewClarification("Which single value should be used for this parameter template?", "template_id", "multiple requested parameter values resolve to the same template", "template_id", true, nil, retryValues)
					return TextResult(StatusClarificationRequired), WriteRecordOutput[[]inventree.Parameter]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
				}
				seenTemplates[templateID] = parameter
				matches := parametersByTemplate(existing, templateID)
				if len(matches) > 1 {
					clarification := NewClarification("Which existing part parameter should be updated?", "parameter_id", "multiple existing part parameters use the same template", "parameter_id", false, candidatesFor(matches), retryParameterValues(input.PartID, parameter))
					return TextResult(StatusClarificationRequired), WriteRecordOutput[[]inventree.Parameter]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
				}
				plan := parameterWritePlan{data: data, template: templateID}
				if len(matches) == 1 {
					plan.existing = &matches[0]
				}
				plans = append(plans, plan)
			}
			records := make([]inventree.Parameter, 0, len(plans))
			for _, plan := range plans {
				var record inventree.Parameter
				if plan.existing != nil {
					record, err = client.UpdatePartParameter(ctx, plan.existing.PK, inventree.PatchFields{"data": inventree.Set(plan.data)})
				} else {
					record, err = client.CreatePartParameter(ctx, inventree.NewPartParameter(input.PartID, plan.template, plan.data))
				}
				if err != nil {
					return nil, WriteRecordOutput[[]inventree.Parameter]{}, err
				}
				records = append(records, record)
			}
			return TextResult(StatusOK), WriteRecordOutput[[]inventree.Parameter]{Status: StatusOK, Record: records}, nil
		})
}

func createCompany(deps Dependencies) mcp.ToolHandlerFor[CreateCompanyInput, WriteRecordOutput[inventree.Company]] {
	return LookupHandler[CompanyWriteClient, CreateCompanyInput, WriteRecordOutput[inventree.Company]](deps, CreateCompanyToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyWriteClient, input CreateCompanyInput) (*mcp.CallToolResult, WriteRecordOutput[inventree.Company], error) {
			if !input.IsSupplier && !input.IsManufacturer {
				return hardClarification[inventree.Company]("Should this company be a supplier or manufacturer?", "company", "create_company requires supplier or manufacturer role in milestone 1", "is_supplier", map[string]any{"name": input.Name})
			}
			if input.Currency == "" {
				return hardClarification[inventree.Company]("Which currency should be used for this company?", "currency", "create_company requires explicit currency", "currency", map[string]any{"name": input.Name})
			}
			records, err := client.SearchCompanies(ctx, inventree.SearchQuery{Search: input.Name, Limit: DefaultLookupLimit})
			if err != nil {
				return nil, WriteRecordOutput[inventree.Company]{}, err
			}
			if len(records) > 0 {
				clarification := NewClarification("Should an existing company be used instead of creating a new one?", "company", "matching company records already exist", "company_id", false, candidatesFor(records), map[string]any{"name": input.Name})
				return TextResult(StatusClarificationRequired), WriteRecordOutput[inventree.Company]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			record, err := client.CreateCompany(ctx, inventree.CompanyCreate{
				Name:           input.Name,
				Description:    input.Description,
				Currency:       input.Currency,
				Website:        input.Website,
				IsSupplier:     input.IsSupplier,
				IsManufacturer: input.IsManufacturer,
			})
			return writeRecordOutput(record, err)
		})
}

func createSupplierPart(deps Dependencies) mcp.ToolHandlerFor[CreateSupplierPartInput, WriteRecordOutput[inventree.SupplierPart]] {
	return LookupHandler[SupplierPartWriteClient, CreateSupplierPartInput, WriteRecordOutput[inventree.SupplierPart]](deps, CreateSupplierPartToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client SupplierPartWriteClient, input CreateSupplierPartInput) (*mcp.CallToolResult, WriteRecordOutput[inventree.SupplierPart], error) {
			if input.PartID <= 0 {
				return hardClarification[inventree.SupplierPart]("Which part should be linked to the supplier?", "part", "create_supplier_part requires a positive part_id", "part_id", map[string]any{"part_id": input.PartID})
			}
			if input.SupplierID <= 0 {
				return hardClarification[inventree.SupplierPart]("Which supplier should be linked to the part?", "supplier", "create_supplier_part requires a positive supplier_id", "supplier_id", map[string]any{"supplier_id": input.SupplierID})
			}
			if input.ManufacturerPartID != nil && *input.ManufacturerPartID <= 0 {
				return hardClarification[inventree.SupplierPart]("Which manufacturer part should be linked to this supplier part?", "manufacturer_part_id", "manufacturer_part_id must be positive when provided", "manufacturer_part_id", map[string]any{"manufacturer_part_id": *input.ManufacturerPartID})
			}
			query := inventree.SupplierPartQuery{Part: input.PartID, Supplier: input.SupplierID, SKU: input.SKU}
			records, err := client.SearchSupplierParts(ctx, query)
			if err != nil {
				return nil, WriteRecordOutput[inventree.SupplierPart]{}, err
			}
			if len(records) > 0 {
				clarification := NewClarification("Should an existing supplier part be used instead of creating a new one?", "supplier_part", "matching supplier-part records already exist", "supplier_part_id", false, candidatesFor(records), nil)
				return TextResult(StatusClarificationRequired), WriteRecordOutput[inventree.SupplierPart]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			record, err := client.CreateSupplierPart(ctx, inventree.SupplierPartCreate{
				Part:             input.PartID,
				Supplier:         input.SupplierID,
				SKU:              input.SKU,
				Description:      input.Description,
				Link:             input.Link,
				Active:           input.Active,
				Primary:          input.Primary,
				ManufacturerPart: input.ManufacturerPartID,
				Packaging:        input.Packaging,
				Note:             input.Note,
			})
			return writeRecordOutput(record, err)
		})
}

func createManufacturerPart(deps Dependencies) mcp.ToolHandlerFor[CreateManufacturerPartInput, WriteRecordOutput[inventree.ManufacturerPart]] {
	return LookupHandler[ManufacturerPartWriteClient, CreateManufacturerPartInput, WriteRecordOutput[inventree.ManufacturerPart]](deps, CreateManufacturerPartToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ManufacturerPartWriteClient, input CreateManufacturerPartInput) (*mcp.CallToolResult, WriteRecordOutput[inventree.ManufacturerPart], error) {
			if input.PartID <= 0 {
				return hardClarification[inventree.ManufacturerPart]("Which part should be linked to the manufacturer?", "part", "create_manufacturer_part requires a positive part_id", "part_id", map[string]any{"part_id": input.PartID})
			}
			if input.ManufacturerID <= 0 {
				return hardClarification[inventree.ManufacturerPart]("Which manufacturer should be linked to the part?", "manufacturer", "create_manufacturer_part requires a positive manufacturer_id", "manufacturer_id", map[string]any{"manufacturer_id": input.ManufacturerID})
			}
			query := inventree.ManufacturerPartQuery{Part: input.PartID, Manufacturer: input.ManufacturerID}
			if input.MPN != nil {
				query.MPN = *input.MPN
			}
			records, err := client.SearchManufacturerParts(ctx, query)
			if err != nil {
				return nil, WriteRecordOutput[inventree.ManufacturerPart]{}, err
			}
			if len(records) > 0 {
				clarification := NewClarification("Should an existing manufacturer part be used instead of creating a new one?", "manufacturer_part", "matching manufacturer-part records already exist", "manufacturer_part_id", false, candidatesFor(records), nil)
				return TextResult(StatusClarificationRequired), WriteRecordOutput[inventree.ManufacturerPart]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			record, err := client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{
				Part:         input.PartID,
				Manufacturer: input.ManufacturerID,
				MPN:          input.MPN,
				Description:  input.Description,
				Link:         input.Link,
			})
			return writeRecordOutput(record, err)
		})
}

func upsertPartWorkflow(deps Dependencies) mcp.ToolHandlerFor[UpsertPartWorkflowInput, PartUpsertWorkflowOutput] {
	return LookupHandler[PartUpsertWorkflowClient, UpsertPartWorkflowInput, PartUpsertWorkflowOutput](deps, UpsertPartWorkflowToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartUpsertWorkflowClient, input UpsertPartWorkflowInput) (*mcp.CallToolResult, PartUpsertWorkflowOutput, error) {
			if !input.DryRun {
				preflightInput := input
				preflightInput.DryRun = true
				result, preflightOutput, err := runPartUpsertWorkflow(ctx, client, preflightInput)
				if err != nil || preflightOutput.Status != StatusOK {
					preflightOutput.DryRun = false
					return result, preflightOutput, err
				}
			}
			return runPartUpsertWorkflow(ctx, client, input)
		})
}

func runPartUpsertWorkflow(ctx context.Context, client PartUpsertWorkflowClient, input UpsertPartWorkflowInput) (*mcp.CallToolResult, PartUpsertWorkflowOutput, error) {
	output := PartUpsertWorkflowOutput{Status: StatusOK, DryRun: input.DryRun, OmittedRecommendedFields: omittedPartUpsertFields(input)}
	part, ok, result, clarificationOutput, err := resolveWorkflowPart(ctx, client, input, &output)
	if err != nil || !ok {
		return result, clarificationOutput, err
	}
	output.Part = &part

	manufacturer, manufacturerOK, result, clarificationOutput, err := resolveWorkflowCompany(ctx, client, "manufacturer", input.ManufacturerID, input.ManufacturerName, input.ManufacturerCurrency, input.DryRun, &output)
	if err != nil || !manufacturerOK {
		return result, clarificationOutput, err
	}
	if manufacturer != nil {
		output.Manufacturer = manufacturer
		manufacturerPart, ok, result, clarificationOutput, err := resolveWorkflowManufacturerPart(ctx, client, part.PK, *manufacturer, input, &output)
		if err != nil || !ok {
			return result, clarificationOutput, err
		}
		output.ManufacturerPart = manufacturerPart
	}

	supplier, supplierOK, result, clarificationOutput, err := resolveWorkflowCompany(ctx, client, "supplier", input.SupplierID, input.SupplierName, input.SupplierCurrency, input.DryRun, &output)
	if err != nil || !supplierOK {
		return result, clarificationOutput, err
	}
	if supplier != nil {
		output.Supplier = supplier
		supplierPart, ok, result, clarificationOutput, err := resolveWorkflowSupplierPart(ctx, client, part.PK, *supplier, output.ManufacturerPart, input, &output)
		if err != nil || !ok {
			return result, clarificationOutput, err
		}
		output.SupplierPart = supplierPart
	}

	return TextResult(StatusOK), output, nil
}

func createStockItem(deps Dependencies) mcp.ToolHandlerFor[CreateStockItemInput, WriteRecordOutput[inventree.StockItem]] {
	return LookupHandler[StockItemWriteClient, CreateStockItemInput, WriteRecordOutput[inventree.StockItem]](deps, CreateStockItemToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockItemWriteClient, input CreateStockItemInput) (*mcp.CallToolResult, WriteRecordOutput[inventree.StockItem], error) {
			if input.PartID <= 0 {
				return hardClarification[inventree.StockItem]("Which part should receive initial stock?", "part", "create_stock_item requires a positive part_id", "part_id", map[string]any{"part_id": input.PartID})
			}
			if input.LocationID <= 0 {
				return hardClarification[inventree.StockItem]("Which stock location should receive initial stock?", "location", "create_stock_item requires a positive location_id", "location_id", map[string]any{"location_id": input.LocationID})
			}
			if input.Quantity <= 0 {
				return hardClarification[inventree.StockItem]("What initial stock quantity should be created?", "quantity", "create_stock_item requires quantity greater than zero", "quantity", map[string]any{"part_id": input.PartID, "location_id": input.LocationID, "quantity": input.Quantity})
			}
			if input.Status != nil && *input.Status < 0 {
				return hardClarification[inventree.StockItem]("Which stock status should be used?", "status", "status must be a non-negative InvenTree stock status code when provided", "status", map[string]any{"status": *input.Status})
			}

			query := inventree.StockItemQuery{PartID: input.PartID, LocationID: input.LocationID, Limit: DefaultLookupLimit}
			records, err := client.SearchStockItems(ctx, query)
			if err != nil {
				return nil, WriteRecordOutput[inventree.StockItem]{}, err
			}
			if len(records) > 0 {
				clarification := NewClarification("Should an existing stock item be used instead of creating duplicate initial stock?", "stock_item", "existing stock items already match the requested part and location", "stock_item_id", false, candidatesFor(records), map[string]any{"part_id": input.PartID, "location_id": input.LocationID, "quantity": input.Quantity})
				return TextResult(StatusClarificationRequired), WriteRecordOutput[inventree.StockItem]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}

			record, err := client.CreateStockItem(ctx, inventree.StockItemCreate{
				Part:     input.PartID,
				Location: input.LocationID,
				Quantity: input.Quantity,
				Status:   input.Status,
				Batch:    input.Batch,
				Serial:   input.Serial,
				Notes:    input.Notes,
			})
			return writeRecordOutput(record, err)
		})
}

func initialStockWorkflow(deps Dependencies) mcp.ToolHandlerFor[InitialStockWorkflowInput, InitialStockWorkflowOutput] {
	return LookupHandler[InitialStockWorkflowClient, InitialStockWorkflowInput, InitialStockWorkflowOutput](deps, InitialStockWorkflowToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client InitialStockWorkflowClient, input InitialStockWorkflowInput) (*mcp.CallToolResult, InitialStockWorkflowOutput, error) {
			output := InitialStockWorkflowOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.Quantity <= 0 {
				return initialStockClarification(input.DryRun, "What initial stock quantity should be created?", "quantity", "quantity must be greater than zero", "quantity", map[string]any{"quantity": input.Quantity, "part_id": input.PartID, "location_id": input.LocationID})
			}
			if input.Status != nil && *input.Status < 0 {
				return initialStockClarification(input.DryRun, "Which stock status should be used?", "status", "status must be a non-negative InvenTree stock status code when provided", "status", map[string]any{"status": *input.Status})
			}

			part, ok, result, clarificationOutput, err := resolveInitialStockPart(ctx, client, input, &output)
			if err != nil || !ok {
				return result, clarificationOutput, err
			}
			output.Part = &part

			location, ok, result, clarificationOutput, err := resolveInitialStockLocation(ctx, client, input, &output)
			if err != nil || !ok {
				return result, clarificationOutput, err
			}
			output.Location = &location

			records, err := client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: part.PK, LocationID: location.PK, Limit: DefaultLookupLimit})
			if err != nil {
				return nil, InitialStockWorkflowOutput{}, err
			}
			if len(records) > 0 {
				clarification := NewClarification("Should an existing stock item be used instead of creating duplicate initial stock?", "stock_item", "existing stock items already match the requested part and location", "stock_item_id", false, candidatesFor(records), map[string]any{"part_id": part.PK, "location_id": location.PK, "quantity": input.Quantity})
				return TextResult(StatusClarificationRequired), InitialStockWorkflowOutput{Status: StatusClarificationRequired, DryRun: input.DryRun, Part: &part, Location: &location, Clarification: &clarification}, nil
			}

			if input.DryRun {
				output.Actions = append(output.Actions, initialStockAction("create_stock_item", "planned", "stockitem", 0, "no matching stock item found"))
				return TextResult(StatusOK), output, nil
			}
			record, err := client.CreateStockItem(ctx, inventree.StockItemCreate{
				Part:     part.PK,
				Location: location.PK,
				Quantity: input.Quantity,
				Status:   input.Status,
				Batch:    input.Batch,
				Serial:   input.Serial,
				Notes:    input.Notes,
			})
			if err != nil {
				return nil, InitialStockWorkflowOutput{}, err
			}
			output.StockItem = &record
			output.Actions = append(output.Actions, initialStockAction("create_stock_item", "created", "stockitem", record.PK, "no matching stock item found"))
			return TextResult(StatusOK), output, nil
		})
}

func resolveInitialStockPart(ctx context.Context, client InitialStockWorkflowClient, input InitialStockWorkflowInput, output *InitialStockWorkflowOutput) (inventree.Part, bool, *mcp.CallToolResult, InitialStockWorkflowOutput, error) {
	if input.PartID < 0 {
		result, clarificationOutput, err := initialStockClarification(input.DryRun, "Which part should receive initial stock?", "part", "part_id must be positive when provided", "part_id", map[string]any{"part_id": input.PartID})
		return inventree.Part{}, false, result, clarificationOutput, err
	}
	if input.PartID > 0 {
		part, err := client.GetPart(ctx, input.PartID)
		if err != nil {
			return inventree.Part{}, false, nil, InitialStockWorkflowOutput{}, err
		}
		output.Actions = append(output.Actions, initialStockAction("reuse_part", "reused", "part", input.PartID, "part_id supplied"))
		return part, true, nil, InitialStockWorkflowOutput{}, nil
	}
	if strings.TrimSpace(input.PartSearch) == "" {
		result, clarificationOutput, err := initialStockClarification(input.DryRun, "Which part should receive initial stock?", "part", "provide part_id or part_search", "part_id", map[string]any{"part_search": input.PartSearch})
		return inventree.Part{}, false, result, clarificationOutput, err
	}
	records, err := client.SearchParts(ctx, inventree.SearchQuery{Search: input.PartSearch, Limit: DefaultLookupLimit})
	if err != nil {
		return inventree.Part{}, false, nil, InitialStockWorkflowOutput{}, err
	}
	if len(records) == 1 {
		output.Actions = append(output.Actions, initialStockAction("reuse_part", "reused", "part", records[0].PK, "single matching part found"))
		return records[0], true, nil, InitialStockWorkflowOutput{}, nil
	}
	if len(records) > 1 {
		clarification := NewClarification("Which part should receive initial stock?", "part", "multiple matching parts found", "part_id", false, candidatesFor(records), map[string]any{"part_search": input.PartSearch})
		return inventree.Part{}, false, TextResult(StatusClarificationRequired), InitialStockWorkflowOutput{Status: StatusClarificationRequired, DryRun: input.DryRun, Clarification: &clarification}, nil
	}
	result, clarificationOutput, err := initialStockClarification(input.DryRun, "Which existing part should receive initial stock?", "part", "no matching part found; create or select a part before adding stock", "part_id", map[string]any{"part_search": input.PartSearch})
	return inventree.Part{}, false, result, clarificationOutput, err
}

func resolveInitialStockLocation(ctx context.Context, client InitialStockWorkflowClient, input InitialStockWorkflowInput, output *InitialStockWorkflowOutput) (inventree.StockLocation, bool, *mcp.CallToolResult, InitialStockWorkflowOutput, error) {
	if input.LocationID < 0 {
		result, clarificationOutput, err := initialStockClarification(input.DryRun, "Which stock location should receive initial stock?", "location", "location_id must be positive when provided", "location_id", map[string]any{"location_id": input.LocationID})
		return inventree.StockLocation{}, false, result, clarificationOutput, err
	}
	if input.LocationID > 0 {
		location, err := client.GetStockLocation(ctx, input.LocationID)
		if err != nil {
			return inventree.StockLocation{}, false, nil, InitialStockWorkflowOutput{}, err
		}
		output.Actions = append(output.Actions, initialStockAction("reuse_location", "reused", "stocklocation", input.LocationID, "location_id supplied"))
		return location, true, nil, InitialStockWorkflowOutput{}, nil
	}
	if strings.TrimSpace(input.LocationSearch) == "" {
		result, clarificationOutput, err := initialStockClarification(input.DryRun, "Which stock location should receive initial stock?", "location", "provide location_id or location_search", "location_id", map[string]any{"location_search": input.LocationSearch})
		return inventree.StockLocation{}, false, result, clarificationOutput, err
	}
	records, err := client.SearchStockLocations(ctx, inventree.SearchQuery{Search: input.LocationSearch, Limit: DefaultLookupLimit})
	if err != nil {
		return inventree.StockLocation{}, false, nil, InitialStockWorkflowOutput{}, err
	}
	if len(records) == 1 {
		output.Actions = append(output.Actions, initialStockAction("reuse_location", "reused", "stocklocation", records[0].PK, "single matching stock location found"))
		return records[0], true, nil, InitialStockWorkflowOutput{}, nil
	}
	if len(records) > 1 {
		clarification := NewClarification("Which stock location should receive initial stock?", "location", "multiple matching stock locations found", "location_id", false, candidatesFor(records), map[string]any{"location_search": input.LocationSearch})
		return inventree.StockLocation{}, false, TextResult(StatusClarificationRequired), InitialStockWorkflowOutput{Status: StatusClarificationRequired, DryRun: input.DryRun, Clarification: &clarification}, nil
	}
	result, clarificationOutput, err := initialStockClarification(input.DryRun, "Which existing stock location should receive initial stock?", "location", "no matching stock location found; create or select a location before adding stock", "location_id", map[string]any{"location_search": input.LocationSearch})
	return inventree.StockLocation{}, false, result, clarificationOutput, err
}

func initialStockClarification(dryRun bool, question string, field string, reason string, retry string, retryValues map[string]any) (*mcp.CallToolResult, InitialStockWorkflowOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, retryValues)
	return TextResult(StatusClarificationRequired), InitialStockWorkflowOutput{Status: StatusClarificationRequired, DryRun: dryRun, Clarification: &clarification}, nil
}

func initialStockAction(name string, status string, recordType string, id int, reason string) InitialStockWorkflowAction {
	return InitialStockWorkflowAction{Name: name, Status: status, RecordType: recordType, ID: id, Reason: reason}
}

func resolveWorkflowPart(ctx context.Context, client PartUpsertWorkflowClient, input UpsertPartWorkflowInput, output *PartUpsertWorkflowOutput) (inventree.Part, bool, *mcp.CallToolResult, PartUpsertWorkflowOutput, error) {
	if input.PartID < 0 {
		return workflowClarification[inventree.Part](input.DryRun, "Which part should be created or updated?", "part", "part_id must be positive when provided", "part_id", map[string]any{"part_id": input.PartID})
	}
	if input.PartID > 0 {
		if input.CategoryID < 0 {
			return workflowClarification[inventree.Part](input.DryRun, "Which category should contain this part?", "category_id", "category_id must be positive when provided", "category_id", map[string]any{"category_id": input.CategoryID})
		}
		if input.DefaultLocation != nil && *input.DefaultLocation <= 0 {
			return workflowClarification[inventree.Part](input.DryRun, "Which default stock location should be used?", "default_location_id", "default_location_id must be positive when provided", "default_location_id", map[string]any{"default_location_id": *input.DefaultLocation})
		}
		part, err := client.GetPart(ctx, input.PartID)
		if err != nil {
			return inventree.Part{}, false, nil, PartUpsertWorkflowOutput{}, err
		}
		output.Actions = append(output.Actions, workflowAction("reuse_part", "reused", "part", part.PK, "part_id supplied"))
		fields := partWorkflowPatchFields(input)
		if len(fields) == 0 {
			return part, true, nil, PartUpsertWorkflowOutput{}, nil
		}
		if input.DryRun {
			output.Actions = append(output.Actions, workflowAction("update_part", "planned", "part", part.PK, "supplied fields would be patched"))
			return part, true, nil, PartUpsertWorkflowOutput{}, nil
		}
		updated, err := client.UpdatePart(ctx, input.PartID, fields)
		if err != nil {
			return inventree.Part{}, false, nil, PartUpsertWorkflowOutput{}, err
		}
		output.Actions = append(output.Actions, workflowAction("update_part", "updated", "part", updated.PK, "supplied fields patched"))
		return updated, true, nil, PartUpsertWorkflowOutput{}, nil
	}
	if input.CategoryID < 0 {
		return workflowClarification[inventree.Part](input.DryRun, "Which category should contain this part?", "category_id", "category_id must be positive when provided", "category_id", map[string]any{"category_id": input.CategoryID})
	}
	if input.DefaultLocation != nil && *input.DefaultLocation <= 0 {
		return workflowClarification[inventree.Part](input.DryRun, "Which default stock location should be used?", "default_location_id", "default_location_id must be positive when provided", "default_location_id", map[string]any{"default_location_id": *input.DefaultLocation})
	}
	if strings.TrimSpace(input.Name) == "" {
		return workflowClarification[inventree.Part](input.DryRun, "Which part should be created or updated?", "part", "provide part_id or name", "part_id", map[string]any{"name": input.Name})
	}
	parts, err := client.SearchParts(ctx, inventree.SearchQuery{Search: input.Name, Limit: DefaultLookupLimit})
	if err != nil {
		return inventree.Part{}, false, nil, PartUpsertWorkflowOutput{}, err
	}
	if len(parts) == 1 {
		output.Actions = append(output.Actions, workflowAction("reuse_part", "reused", "part", parts[0].PK, "single matching part found"))
		fields := partWorkflowPatchFields(input)
		if len(fields) == 0 {
			return parts[0], true, nil, PartUpsertWorkflowOutput{}, nil
		}
		if input.DryRun {
			output.Actions = append(output.Actions, workflowAction("update_part", "planned", "part", parts[0].PK, "supplied fields would be patched"))
			return parts[0], true, nil, PartUpsertWorkflowOutput{}, nil
		}
		updated, err := client.UpdatePart(ctx, parts[0].PK, fields)
		if err != nil {
			return inventree.Part{}, false, nil, PartUpsertWorkflowOutput{}, err
		}
		output.Actions = append(output.Actions, workflowAction("update_part", "updated", "part", updated.PK, "supplied fields patched"))
		return updated, true, nil, PartUpsertWorkflowOutput{}, nil
	}
	if len(parts) > 1 {
		clarification := NewClarification("Which existing part should be used?", "part", "multiple matching parts found", "part_id", false, candidatesFor(parts), map[string]any{"name": input.Name})
		return inventree.Part{}, false, TextResult(StatusClarificationRequired), workflowClarificationOutput(clarification, input.DryRun), nil
	}
	if input.CategoryID <= 0 {
		return workflowClarification[inventree.Part](input.DryRun, "Which existing category should contain the new part?", "category_id", "category_id is required when creating a part", "category_id", map[string]any{"name": input.Name})
	}
	if input.DryRun {
		output.Actions = append(output.Actions, workflowAction("create_part", "planned", "part", 0, "no matching part found"))
		return inventree.Part{Name: input.Name, Category: &input.CategoryID}, true, nil, PartUpsertWorkflowOutput{}, nil
	}
	part, err := client.CreatePart(ctx, inventree.PartCreate{
		Name:            input.Name,
		Description:     derefString(input.Description),
		Category:        &input.CategoryID,
		IPN:             derefString(input.IPN),
		Units:           input.Units,
		Purchaseable:    input.Purchaseable,
		DefaultLocation: input.DefaultLocation,
	})
	if err != nil {
		return inventree.Part{}, false, nil, PartUpsertWorkflowOutput{}, err
	}
	output.Actions = append(output.Actions, workflowAction("create_part", "created", "part", part.PK, "no matching part found"))
	return part, true, nil, PartUpsertWorkflowOutput{}, nil
}

func resolveWorkflowCompany(ctx context.Context, client PartUpsertWorkflowClient, role string, id int, name string, currency string, dryRun bool, output *PartUpsertWorkflowOutput) (*inventree.Company, bool, *mcp.CallToolResult, PartUpsertWorkflowOutput, error) {
	if id < 0 {
		clarification := NewClarification("Which "+role+" company should be used?", role, role+"_id must be positive when provided", role+"_id", true, nil, map[string]any{role + "_id": id})
		return nil, false, TextResult(StatusClarificationRequired), workflowClarificationOutput(clarification, dryRun), nil
	}
	if id > 0 {
		company := inventree.Company{PK: id}
		output.Actions = append(output.Actions, workflowAction("reuse_"+role, "reused", "company", id, role+"_id supplied"))
		return &company, true, nil, PartUpsertWorkflowOutput{}, nil
	}
	if strings.TrimSpace(name) == "" {
		return nil, true, nil, PartUpsertWorkflowOutput{}, nil
	}
	var records []inventree.Company
	var err error
	if role == "supplier" {
		records, err = client.SearchSuppliers(ctx, inventree.SearchQuery{Search: name, Limit: DefaultLookupLimit})
	} else {
		records, err = client.SearchManufacturers(ctx, inventree.SearchQuery{Search: name, Limit: DefaultLookupLimit})
	}
	if err != nil {
		return nil, false, nil, PartUpsertWorkflowOutput{}, err
	}
	if len(records) == 1 {
		output.Actions = append(output.Actions, workflowAction("reuse_"+role, "reused", "company", records[0].PK, "single matching "+role+" found"))
		return &records[0], true, nil, PartUpsertWorkflowOutput{}, nil
	}
	if len(records) > 1 {
		clarification := NewClarification("Which "+role+" company should be used?", role, "multiple matching "+role+" companies found", role+"_id", false, candidatesFor(records), map[string]any{role + "_name": name})
		return nil, false, TextResult(StatusClarificationRequired), workflowClarificationOutput(clarification, dryRun), nil
	}
	if strings.TrimSpace(currency) == "" {
		clarification := NewClarification("Which currency should be used for the new "+role+" company?", role+"_currency", role+"_currency is required when creating a company", role+"_currency", true, nil, map[string]any{role + "_name": name})
		return nil, false, TextResult(StatusClarificationRequired), workflowClarificationOutput(clarification, dryRun), nil
	}
	if dryRun {
		output.Actions = append(output.Actions, workflowAction("create_"+role, "planned", "company", 0, "no matching "+role+" found"))
		return &inventree.Company{Name: name, Currency: currency}, true, nil, PartUpsertWorkflowOutput{}, nil
	}
	input := inventree.CompanyCreate{Name: name, Currency: currency}
	if role == "supplier" {
		input.IsSupplier = true
	} else {
		input.IsManufacturer = true
	}
	company, err := client.CreateCompany(ctx, input)
	if err != nil {
		return nil, false, nil, PartUpsertWorkflowOutput{}, err
	}
	output.Actions = append(output.Actions, workflowAction("create_"+role, "created", "company", company.PK, "no matching "+role+" found"))
	return &company, true, nil, PartUpsertWorkflowOutput{}, nil
}

func resolveWorkflowManufacturerPart(ctx context.Context, client PartUpsertWorkflowClient, partID int, manufacturer inventree.Company, input UpsertPartWorkflowInput, output *PartUpsertWorkflowOutput) (*inventree.ManufacturerPart, bool, *mcp.CallToolResult, PartUpsertWorkflowOutput, error) {
	if input.DryRun && (partID <= 0 || manufacturer.PK <= 0) {
		output.Actions = append(output.Actions, workflowAction("create_manufacturer_part", "planned", "manufacturerpart", 0, "new part or manufacturer would be created first"))
		return nil, true, nil, PartUpsertWorkflowOutput{}, nil
	}
	query := inventree.ManufacturerPartQuery{Part: partID, Manufacturer: manufacturer.PK}
	if input.MPN != nil {
		query.MPN = *input.MPN
	}
	records, err := client.SearchManufacturerParts(ctx, query)
	if err != nil {
		return nil, false, nil, PartUpsertWorkflowOutput{}, err
	}
	if len(records) == 1 {
		output.Actions = append(output.Actions, workflowAction("reuse_manufacturer_part", "reused", "manufacturerpart", records[0].PK, "single matching manufacturer-part found"))
		return &records[0], true, nil, PartUpsertWorkflowOutput{}, nil
	}
	if len(records) > 1 {
		clarification := NewClarification("Which manufacturer part should be used?", "manufacturer_part", "multiple matching manufacturer-part records found", "manufacturer_part_id", false, candidatesFor(records), nil)
		return nil, false, TextResult(StatusClarificationRequired), workflowClarificationOutput(clarification, input.DryRun), nil
	}
	if input.DryRun {
		output.Actions = append(output.Actions, workflowAction("create_manufacturer_part", "planned", "manufacturerpart", 0, "no matching manufacturer-part found"))
		return nil, true, nil, PartUpsertWorkflowOutput{}, nil
	}
	record, err := client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{Part: partID, Manufacturer: manufacturer.PK, MPN: input.MPN, Link: input.Link})
	if err != nil {
		return nil, false, nil, PartUpsertWorkflowOutput{}, err
	}
	output.Actions = append(output.Actions, workflowAction("create_manufacturer_part", "created", "manufacturerpart", record.PK, "no matching manufacturer-part found"))
	return &record, true, nil, PartUpsertWorkflowOutput{}, nil
}

func resolveWorkflowSupplierPart(ctx context.Context, client PartUpsertWorkflowClient, partID int, supplier inventree.Company, manufacturerPart *inventree.ManufacturerPart, input UpsertPartWorkflowInput, output *PartUpsertWorkflowOutput) (*inventree.SupplierPart, bool, *mcp.CallToolResult, PartUpsertWorkflowOutput, error) {
	if strings.TrimSpace(input.SupplierSKU) == "" {
		clarification := NewClarification("Which supplier SKU should be linked to this part?", "supplier_sku", "supplier_sku is required when creating or matching a supplier-part link", "supplier_sku", true, nil, map[string]any{"part_id": partID, "supplier_id": supplier.PK})
		return nil, false, TextResult(StatusClarificationRequired), workflowClarificationOutput(clarification, input.DryRun), nil
	}
	if input.DryRun && (partID <= 0 || supplier.PK <= 0) {
		output.Actions = append(output.Actions, workflowAction("create_supplier_part", "planned", "supplierpart", 0, "new part or supplier would be created first"))
		return nil, true, nil, PartUpsertWorkflowOutput{}, nil
	}
	records, err := client.SearchSupplierParts(ctx, inventree.SupplierPartQuery{Part: partID, Supplier: supplier.PK, SKU: input.SupplierSKU})
	if err != nil {
		return nil, false, nil, PartUpsertWorkflowOutput{}, err
	}
	if len(records) == 1 {
		output.Actions = append(output.Actions, workflowAction("reuse_supplier_part", "reused", "supplierpart", records[0].PK, "single matching supplier-part found"))
		return &records[0], true, nil, PartUpsertWorkflowOutput{}, nil
	}
	if len(records) > 1 {
		clarification := NewClarification("Which supplier part should be used?", "supplier_part", "multiple matching supplier-part records found", "supplier_part_id", false, candidatesFor(records), nil)
		return nil, false, TextResult(StatusClarificationRequired), workflowClarificationOutput(clarification, input.DryRun), nil
	}
	if input.DryRun {
		output.Actions = append(output.Actions, workflowAction("create_supplier_part", "planned", "supplierpart", 0, "no matching supplier-part found"))
		return nil, true, nil, PartUpsertWorkflowOutput{}, nil
	}
	var manufacturerPartID *int
	if manufacturerPart != nil {
		manufacturerPartID = &manufacturerPart.PK
	}
	record, err := client.CreateSupplierPart(ctx, inventree.SupplierPartCreate{Part: partID, Supplier: supplier.PK, SKU: input.SupplierSKU, ManufacturerPart: manufacturerPartID, Link: input.Link})
	if err != nil {
		return nil, false, nil, PartUpsertWorkflowOutput{}, err
	}
	output.Actions = append(output.Actions, workflowAction("create_supplier_part", "created", "supplierpart", record.PK, "no matching supplier-part found"))
	return &record, true, nil, PartUpsertWorkflowOutput{}, nil
}

func workflowClarification[T any](dryRun bool, question string, field string, reason string, retry string, retryValues map[string]any) (T, bool, *mcp.CallToolResult, PartUpsertWorkflowOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, retryValues)
	return *new(T), false, TextResult(StatusClarificationRequired), workflowClarificationOutput(clarification, dryRun), nil
}

func workflowClarificationOutput(clarification ClarificationResponse, dryRun bool) PartUpsertWorkflowOutput {
	return PartUpsertWorkflowOutput{Status: StatusClarificationRequired, DryRun: dryRun, Clarification: &clarification}
}

func workflowAction(name string, status string, recordType string, id int, reason string) PartUpsertWorkflowAction {
	return PartUpsertWorkflowAction{Name: name, Status: status, RecordType: recordType, ID: id, Reason: reason}
}

func partWorkflowPatchFields(input UpsertPartWorkflowInput) inventree.PatchFields {
	fields := inventree.PatchFields{}
	if input.Description != nil {
		fields["description"] = inventree.Set(*input.Description)
	}
	if input.CategoryID > 0 {
		fields["category"] = inventree.Set(input.CategoryID)
	}
	if input.IPN != nil {
		fields["IPN"] = inventree.Set(*input.IPN)
	}
	if input.Units != nil {
		fields["units"] = inventree.Set(*input.Units)
	}
	if input.Purchaseable != nil {
		fields["purchaseable"] = inventree.Set(*input.Purchaseable)
	}
	if input.DefaultLocation != nil {
		fields["default_location"] = inventree.Set(*input.DefaultLocation)
	}
	return fields
}

func omittedPartUpsertFields(input UpsertPartWorkflowInput) []string {
	fields := []string{}
	if input.IPN == nil || strings.TrimSpace(*input.IPN) == "" {
		fields = append(fields, "ipn")
	}
	if input.Units == nil || strings.TrimSpace(*input.Units) == "" {
		fields = append(fields, "units")
	}
	if input.Purchaseable == nil {
		fields = append(fields, "purchaseable")
	}
	if input.DefaultLocation == nil {
		fields = append(fields, "default_location_id")
	}
	if input.SupplierName != "" || input.SupplierID > 0 {
		if strings.TrimSpace(input.SupplierSKU) == "" {
			fields = append(fields, "supplier_sku")
		}
	}
	if input.ManufacturerName != "" || input.ManufacturerID > 0 {
		if input.MPN == nil || strings.TrimSpace(*input.MPN) == "" {
			fields = append(fields, "mpn")
		}
	}
	return fields
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func parameterData(input ParameterSetInput, partID int) (string, *mcp.CallToolResult, WriteRecordOutput[[]inventree.Parameter], bool) {
	setCount := 0
	var data string
	if input.Value != nil {
		setCount++
		data = *input.Value
	}
	if input.BoolValue != nil {
		setCount++
		data = strconv.FormatBool(*input.BoolValue)
	}
	if input.NumberValue != nil {
		setCount++
		data = strconv.FormatFloat(*input.NumberValue, 'f', -1, 64)
	}
	if setCount == 1 {
		return data, nil, WriteRecordOutput[[]inventree.Parameter]{}, true
	}
	retry := retryParameterValues(partID, input)
	clarification := NewClarification("Which single value should be used for this parameter?", "value", "provide exactly one of value, bool_value, or number_value", "value", true, nil, retry)
	return "", TextResult(StatusClarificationRequired), WriteRecordOutput[[]inventree.Parameter]{Status: StatusClarificationRequired, Clarification: &clarification}, false
}

func resolveParameterTemplate(ctx context.Context, client ParameterWriteClient, categoryID int, links []inventree.CategoryParameterTemplate, existing []inventree.Parameter, input ParameterSetInput) (int, *mcp.CallToolResult, WriteRecordOutput[[]inventree.Parameter], bool, error) {
	if input.TemplateID != nil {
		if *input.TemplateID <= 0 {
			result, output, err := hardClarification[[]inventree.Parameter]("Which parameter template should be used?", "template_id", "template_id must be positive when provided", "template_id", retryParameterValues(0, input))
			return 0, result, output, false, err
		}
		template, err := client.GetParameterTemplate(ctx, *input.TemplateID)
		if err != nil {
			return 0, nil, WriteRecordOutput[[]inventree.Parameter]{}, false, err
		}
		if !template.Enabled {
			clarification := NewClarification("Which enabled parameter template should be used?", "template_id", "selected parameter template is disabled", "template_id", true, templateCandidates([]inventree.ParameterTemplate{template}, links, existing), retryParameterValues(0, input))
			return 0, TextResult(StatusClarificationRequired), WriteRecordOutput[[]inventree.Parameter]{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
		}
		if strings.TrimSpace(input.Name) != "" && !templateNameMatches(template.Name, input.Name) {
			clarification := NewClarification("Which parameter template should be used?", "template_id", "template_id does not match the supplied template name", "template_id", true, templateCandidates([]inventree.ParameterTemplate{template}, links, existing), retryParameterValues(0, input))
			return 0, TextResult(StatusClarificationRequired), WriteRecordOutput[[]inventree.Parameter]{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
		}
		if !categoryLinksTemplate(links, *input.TemplateID) {
			clarification := NewClarification("Which existing category-linked template should be used?", "template_id", "template is not linked to the part category and new category links are out of scope", "template_id", true, nil, retryParameterValues(0, input))
			return 0, TextResult(StatusClarificationRequired), WriteRecordOutput[[]inventree.Parameter]{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
		}
		return *input.TemplateID, nil, WriteRecordOutput[[]inventree.Parameter]{}, true, nil
	}
	if strings.TrimSpace(input.Name) == "" {
		result, output, err := hardClarification[[]inventree.Parameter]("Which existing parameter template should be used?", "template", "provide template_id or template name", "template_id", retryParameterValues(0, input))
		return 0, result, output, false, err
	}
	templates, err := client.SearchParameterTemplates(ctx, inventree.SearchQuery{Search: input.Name, Limit: MaxLookupLimit})
	if err != nil {
		return 0, nil, WriteRecordOutput[[]inventree.Parameter]{}, false, err
	}
	candidates := matchingLinkedTemplates(input.Name, templates, links)
	if len(candidates) == 1 {
		return candidates[0].PK, nil, WriteRecordOutput[[]inventree.Parameter]{}, true, nil
	}
	if len(candidates) > 1 {
		clarification := NewClarification("Which existing parameter template should be used?", "template", "multiple enabled category-linked templates match this name", "template_id", false, templateCandidates(candidates, links, existing), retryParameterValues(0, input))
		return 0, TextResult(StatusClarificationRequired), WriteRecordOutput[[]inventree.Parameter]{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
	}
	disabled := matchingDisabledTemplates(input.Name, templates, links)
	if len(disabled) > 0 {
		clarification := NewClarification("Which enabled parameter template should be used?", "template", "matching category-linked templates are disabled", "template_id", true, templateCandidates(disabled, links, existing), retryParameterValues(0, input))
		return 0, TextResult(StatusClarificationRequired), WriteRecordOutput[[]inventree.Parameter]{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
	}
	clarification := NewClarification("Which existing category-linked template should be used?", "template", fmt.Sprintf("no enabled template named %q is linked to category %d; creating templates or category links is out of scope", input.Name, categoryID), "template_id", true, templateCandidates(templates, links, existing), retryParameterValues(0, input))
	return 0, TextResult(StatusClarificationRequired), WriteRecordOutput[[]inventree.Parameter]{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
}

func templateCandidates(templates []inventree.ParameterTemplate, links []inventree.CategoryParameterTemplate, existing []inventree.Parameter) []ClarificationCandidate {
	candidates := make([]ClarificationCandidate, 0, len(templates))
	for _, template := range templates {
		candidate := candidateFor(template)
		if candidate.Fields == nil {
			candidate.Fields = map[string]any{}
		}
		if link, ok := categoryLinkForTemplate(links, template.PK); ok {
			candidate.Fields["category_linked"] = true
			candidate.Fields["category_link_id"] = link.PK
			candidate.Fields["category_id"] = link.Category
			if link.DefaultValue != "" {
				candidate.Fields["default_value"] = link.DefaultValue
			}
		} else {
			candidate.Fields["category_linked"] = false
		}
		if matches := parametersByTemplate(existing, template.PK); len(matches) == 1 {
			candidate.Fields["existing_parameter_id"] = matches[0].PK
			candidate.Fields["existing_value"] = matches[0].Data
		} else if len(matches) > 1 {
			candidate.Fields["existing_parameter_count"] = len(matches)
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func matchingLinkedTemplates(name string, templates []inventree.ParameterTemplate, links []inventree.CategoryParameterTemplate) []inventree.ParameterTemplate {
	matches := []inventree.ParameterTemplate{}
	for _, template := range templates {
		if template.Enabled && templateNameMatches(template.Name, name) && categoryLinksTemplate(links, template.PK) {
			matches = append(matches, template)
		}
	}
	return matches
}

func matchingDisabledTemplates(name string, templates []inventree.ParameterTemplate, links []inventree.CategoryParameterTemplate) []inventree.ParameterTemplate {
	matches := []inventree.ParameterTemplate{}
	for _, template := range templates {
		if !template.Enabled && templateNameMatches(template.Name, name) && categoryLinksTemplate(links, template.PK) {
			matches = append(matches, template)
		}
	}
	return matches
}

func templateNameMatches(got string, want string) bool {
	return strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
}

func categoryLinksTemplate(links []inventree.CategoryParameterTemplate, templateID int) bool {
	_, ok := categoryLinkForTemplate(links, templateID)
	return ok
}

func categoryLinkForTemplate(links []inventree.CategoryParameterTemplate, templateID int) (inventree.CategoryParameterTemplate, bool) {
	for _, link := range links {
		if link.Template == templateID {
			return link, true
		}
	}
	return inventree.CategoryParameterTemplate{}, false
}

func parametersByTemplate(parameters []inventree.Parameter, templateID int) []inventree.Parameter {
	matches := []inventree.Parameter{}
	for _, parameter := range parameters {
		if parameter.Template == templateID {
			matches = append(matches, parameter)
		}
	}
	return matches
}

func retryParameterValues(partID int, input ParameterSetInput) map[string]any {
	values := map[string]any{}
	if partID > 0 {
		values["part_id"] = partID
	}
	if input.Name != "" {
		values["name"] = input.Name
	}
	if input.TemplateID != nil {
		values["template_id"] = *input.TemplateID
	}
	if input.Value != nil {
		values["value"] = *input.Value
	}
	if input.BoolValue != nil {
		values["bool_value"] = *input.BoolValue
	}
	if input.NumberValue != nil {
		values["number_value"] = *input.NumberValue
	}
	return values
}

func writeRecordOutput[T any](record T, err error) (*mcp.CallToolResult, WriteRecordOutput[T], error) {
	result, out, err := recordOutput(record, err)
	return result, WriteRecordOutput[T]{Status: out.Status, Record: out.Record}, err
}

func hardClarification[T any](question string, field string, reason string, retry string, retryValues map[string]any) (*mcp.CallToolResult, WriteRecordOutput[T], error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, retryValues)
	return TextResult(StatusClarificationRequired), WriteRecordOutput[T]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func partPatchFields(input UpdatePartInput) inventree.PatchFields {
	fields := inventree.PatchFields{}
	setPatchString(fields, "name", input.Name)
	setPatchString(fields, "description", input.Description)
	setPatchInt(fields, "category", input.CategoryID)
	setPatchString(fields, "IPN", input.IPN)
	setPatchString(fields, "units", input.Units)
	setPatchBool(fields, "active", input.Active)
	setPatchBool(fields, "assembly", input.Assembly)
	setPatchBool(fields, "component", input.Component)
	setPatchBool(fields, "purchaseable", input.Purchaseable)
	setPatchBool(fields, "trackable", input.Trackable)
	setPatchBool(fields, "virtual", input.Virtual)
	setPatchInt(fields, "default_location", input.DefaultLocation)
	return fields
}

func setPatchString(fields inventree.PatchFields, name string, value *string) {
	if value != nil {
		fields[name] = inventree.Set(*value)
	}
}

func setPatchBool(fields inventree.PatchFields, name string, value *bool) {
	if value != nil {
		fields[name] = inventree.Set(*value)
	}
}

func setPatchInt(fields inventree.PatchFields, name string, value *int) {
	if value != nil {
		fields[name] = inventree.Set(*value)
	}
}
