package tools

import (
	"context"

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

type WriteRecordOutput[T any] struct {
	Status        string                 `json:"status"`
	Record        T                      `json:"record,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
}

func registerWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, CreatePartToolName, "Create part", "Creates an InvenTree part in an existing category.", createPart(deps))
	addWriteTool(server, UpdatePartToolName, "Update part", "Partially updates an InvenTree part.", updatePart(deps))
	addWriteTool(server, CreateCompanyToolName, "Create company", "Creates a supplier and/or manufacturer company.", createCompany(deps))
	addWriteTool(server, CreateSupplierPartToolName, "Create supplier part", "Creates a supplier-part link for existing records.", createSupplierPart(deps))
	addWriteTool(server, CreateManufacturerPartToolName, "Create manufacturer part", "Creates a manufacturer-part link for existing records.", createManufacturerPart(deps))
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
