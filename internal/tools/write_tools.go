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

type ParameterWriteClient interface {
	GetPart(context.Context, int) (inventree.Part, error)
	SearchPartParameters(context.Context, inventree.PartParameterQuery) ([]inventree.Parameter, error)
	SearchParameterTemplates(context.Context, inventree.SearchQuery) ([]inventree.ParameterTemplate, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
	SearchCategoryParameterTemplates(context.Context, inventree.CategoryParameterTemplateQuery) ([]inventree.CategoryParameterTemplate, error)
	CreatePartParameter(context.Context, inventree.ParameterCreate) (inventree.Parameter, error)
	UpdatePartParameter(context.Context, int, inventree.PatchFields) (inventree.Parameter, error)
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

type parameterWritePlan struct {
	data     string
	template int
	existing *inventree.Parameter
}

func registerWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, CreatePartToolName, "Create part", "Creates an InvenTree part in an existing category.", createPart(deps))
	addWriteTool(server, UpdatePartToolName, "Update part", "Partially updates an InvenTree part.", updatePart(deps))
	addWriteTool(server, SetPartParametersToolName, "Set part parameters", "Creates or updates part parameter values using existing linked templates.", setPartParameters(deps))
	addWriteTool(server, CreateCompanyToolName, "Create company", "Creates a supplier and/or manufacturer company.", createCompany(deps))
	addWriteTool(server, CreateSupplierPartToolName, "Create supplier part", "Creates a supplier-part link for existing records.", createSupplierPart(deps))
	addWriteTool(server, CreateManufacturerPartToolName, "Create manufacturer part", "Creates a manufacturer-part link for existing records.", createManufacturerPart(deps))
	addWriteTool(server, CreateStockItemToolName, "Create stock item", "Creates initial stock after checking for duplicate stock at the same part and location.", createStockItem(deps))
}

func addWriteTool[In, Out any](server *mcp.Server, name string, title string, description string, handler mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name,
		Title:       title,
		Description: description,
		Annotations: ToolAnnotations(WriteAnnotations),
	}, handler)
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
