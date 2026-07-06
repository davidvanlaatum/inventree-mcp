package tools

import (
	"reflect"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteToolAuthorizationsUseWriteScope(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	for _, name := range writeToolNames {
		auth, ok := ToolAuthorizations[name]
		r.True(ok, "missing authorization for %s", name)
		if name == CreateStockItemToolName {
			a.Equal("operational", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite, ScopeInventreeOperational}, auth.Scopes)
		} else {
			a.Equal("write", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite}, auth.Scopes)
		}
		a.Equal(WriteAnnotations, auth.Annotations)
	}
}

func TestWriteToolInputsExcludeSalesAndCustomerWorkflowFields(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	for _, schemaType := range []reflect.Type{
		reflect.TypeOf(CreatePartInput{}),
		reflect.TypeOf(UpdatePartInput{}),
		reflect.TypeOf(CreateCompanyInput{}),
		reflect.TypeOf(CreateSupplierPartInput{}),
		reflect.TypeOf(CreateManufacturerPartInput{}),
		reflect.TypeOf(CreateStockItemInput{}),
		reflect.TypeOf(SetPartParametersInput{}),
		reflect.TypeOf(ParameterSetInput{}),
		reflect.TypeOf(inventree.PartCreate{}),
		reflect.TypeOf(inventree.CompanyCreate{}),
		reflect.TypeOf(inventree.SupplierPartCreate{}),
		reflect.TypeOf(inventree.ManufacturerPartCreate{}),
		reflect.TypeOf(inventree.StockItemCreate{}),
		reflect.TypeOf(inventree.ParameterCreate{}),
	} {
		for _, field := range reflect.VisibleFields(schemaType) {
			jsonName := jsonFieldName(field.Tag.Get("json"))
			a.NotContains(strings.ToLower(field.Name), "customer")
			a.NotContains(strings.ToLower(jsonName), "customer")
			a.NotContains(strings.ToLower(field.Name), "salable")
			a.NotContains(strings.ToLower(jsonName), "salable")
			a.NotContains(strings.ToLower(field.Name), "sales")
			a.NotContains(strings.ToLower(jsonName), "sales")
		}
	}
}

func TestCreateStockItemAsksBeforeDuplicateCreate(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakeMilestoneLookupClient{
		stockItems: []inventree.StockItem{{PK: 50, Part: 10, Location: &locationID, Quantity: 2}},
	}

	_, output, err := createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{PartID: 10, LocationID: locationID, Quantity: 7})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("stock_item", output.Clarification.Field)
	a.Equal("stock_item_id", output.Clarification.Retry)
	a.Equal("50", output.Clarification.Candidates[0].ID)
	a.Equal(inventree.StockItemQuery{PartID: 10, LocationID: locationID, Limit: DefaultLookupLimit}, fake.lastSearchStockItemsQuery)
	a.False(fake.createdStockItem)
}

func TestCreateStockItemValidatesInputsBeforeWrite(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{LocationID: 40, Quantity: 1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)

	_, output, err = createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{PartID: 10, Quantity: 1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("location", output.Clarification.Field)

	_, output, err = createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{PartID: 10, LocationID: 40})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("quantity", output.Clarification.Field)

	_, output, err = createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{PartID: 10, LocationID: 40, Quantity: 1, Status: dvgoutils.Ptr(-1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("status", output.Clarification.Field)

	a.False(fake.createdStockItem)
	a.Equal(inventree.StockItemQuery{}, fake.lastSearchStockItemsQuery)
}

func TestCreateStockItemWritesAfterDuplicatePreflight(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{
		PartID:     10,
		LocationID: 40,
		Quantity:   7,
		Status:     dvgoutils.Ptr(10),
		Batch:      dvgoutils.Ptr("B-1"),
		Notes:      dvgoutils.Ptr("initial stock"),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(fake.createdStockItem)
	a.Equal(inventree.StockItemQuery{PartID: 10, LocationID: 40, Limit: DefaultLookupLimit}, fake.lastSearchStockItemsQuery)
	a.Equal(inventree.StockItemCreate{Part: 10, Location: 40, Quantity: 7, Status: dvgoutils.Ptr(10), Batch: dvgoutils.Ptr("B-1"), Notes: dvgoutils.Ptr("initial stock")}, fake.lastCreateStockItem)
}

func TestCreatePartAsksBeforeDuplicateCreate(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts: []inventree.Part{{PK: 10, Name: "10k resistor"}},
	}

	result, output, err := createPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreatePartInput{Name: "10k resistor", CategoryID: 20})

	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("part_id", output.Clarification.Retry)
	a.Equal("10", output.Clarification.Candidates[0].ID)
	a.False(fake.createdPart)
}

func TestCreatePartAsksWhenCategoryMissing(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	result, output, err := createPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreatePartInput{Name: "10k resistor"})

	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("category_id", output.Clarification.Field)
	a.Equal("category_id", output.Clarification.Retry)
	a.True(output.Clarification.HardError)
	a.False(fake.createdPart)

	_, output, err = createPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreatePartInput{Name: "10k resistor", CategoryID: -1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("category_id", output.Clarification.Field)
	a.False(fake.createdPart)

	_, output, err = createPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreatePartInput{Name: "10k resistor", CategoryID: 20, DefaultLocation: dvgoutils.Ptr(-1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("default_location_id", output.Clarification.Field)
	a.False(fake.createdPart)
}

func TestCreatePartPassesExplicitFalseValues(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := createPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreatePartInput{
		Name:         "10k resistor",
		CategoryID:   20,
		Purchaseable: dvgoutils.Ptr(false),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(fake.createdPart)
	a.Equal(inventree.PartCreate{Name: "10k resistor", Category: dvgoutils.Ptr(20), Purchaseable: dvgoutils.Ptr(false)}, fake.lastCreatePart)
}

func TestUpdatePartPatchPreservesExplicitEmptyAndFalse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}
	empty := ""
	active := false

	_, output, err := updatePart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartInput{ID: 10, Description: &empty, Active: &active})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(inventree.PatchFields{"description": inventree.Set(""), "active": inventree.Set(false)}, fake.lastUpdatePartFields)
}

func TestUpdatePartAsksWhenNoPatchFieldsProvided(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	result, output, err := updatePart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartInput{ID: 10})

	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("part", output.Clarification.Field)
	a.Equal("id", output.Clarification.Retry)
	a.Nil(fake.lastUpdatePartFields)
}

func TestUpdatePartAsksForPositiveIDFields(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}
	name := "resistor"

	_, output, err := updatePart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartInput{ID: -1, Name: &name})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)
	a.Nil(fake.lastUpdatePartFields)

	_, output, err = updatePart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartInput{ID: 10, CategoryID: dvgoutils.Ptr(-1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("category_id", output.Clarification.Field)
	a.Nil(fake.lastUpdatePartFields)

	_, output, err = updatePart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartInput{ID: 10, DefaultLocation: dvgoutils.Ptr(-1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("default_location_id", output.Clarification.Field)
	a.Nil(fake.lastUpdatePartFields)
}

func TestCreateCompanyAsksBeforeDuplicateAndOmitsCustomerRole(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		companies: []inventree.Company{{PK: 30, Name: "Acme", IsSupplier: true}},
	}

	_, output, err := createCompany(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateCompanyInput{Name: "Acme", Currency: "AUD", IsSupplier: true})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("company_id", output.Clarification.Retry)
	a.False(fake.createdCompany)

	fake = &fakeMilestoneLookupClient{}
	_, output, err = createCompany(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateCompanyInput{Name: "NewCo", Currency: "AUD", IsSupplier: true, IsManufacturer: true})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(inventree.CompanyCreate{Name: "NewCo", Currency: "AUD", IsSupplier: true, IsManufacturer: true}, fake.lastCreateCompany)
}

func TestCreateCompanyAsksForSupportedRoleAndCurrency(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := createCompany(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateCompanyInput{Name: "NeutralCo", Currency: "AUD"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("company", output.Clarification.Field)
	a.True(output.Clarification.HardError)
	a.False(fake.createdCompany)

	_, output, err = createCompany(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateCompanyInput{Name: "SupplierCo", IsSupplier: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("currency", output.Clarification.Field)
	a.True(output.Clarification.HardError)
	a.False(fake.createdCompany)
}

func TestCreateSupplierAndManufacturerPartsAskBeforeDuplicate(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fakeSupplier := &fakeMilestoneLookupClient{
		supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"}},
	}
	_, supplierOutput, err := createSupplierPart(depsForFake(fakeSupplier))(ctx, &mcp.CallToolRequest{}, CreateSupplierPartInput{PartID: 10, SupplierID: 30, SKU: "SKU-1"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, supplierOutput.Status)
	a.Equal("supplier_part_id", supplierOutput.Clarification.Retry)
	a.Equal(inventree.SupplierPartQuery{Part: 10, Supplier: 30, SKU: "SKU-1"}, fakeSupplier.lastSearchSupplierPartsQuery)
	a.False(fakeSupplier.createdSupplierPart)

	fakeManufacturer := &fakeMilestoneLookupClient{
		manufacturerParts: []inventree.ManufacturerPart{{PK: 50, Part: 10, Manufacturer: 31, MPN: "MPN-1"}},
	}
	_, manufacturerOutput, err := createManufacturerPart(depsForFake(fakeManufacturer))(ctx, &mcp.CallToolRequest{}, CreateManufacturerPartInput{PartID: 10, ManufacturerID: 31, MPN: dvgoutils.Ptr("MPN-1")})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, manufacturerOutput.Status)
	a.Equal("manufacturer_part_id", manufacturerOutput.Clarification.Retry)
	a.Equal(inventree.ManufacturerPartQuery{Part: 10, Manufacturer: 31, MPN: "MPN-1"}, fakeManufacturer.lastSearchManufacturerPartsQuery)
	a.False(fakeManufacturer.createdManufacturerPart)
}

func TestCreateSupplierAndManufacturerPartsAskForPositiveIDs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, supplierOutput, err := createSupplierPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateSupplierPartInput{PartID: 0, SupplierID: 30, SKU: "SKU-1"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, supplierOutput.Status)
	a.Equal("part", supplierOutput.Clarification.Field)
	a.True(supplierOutput.Clarification.HardError)
	a.False(fake.createdSupplierPart)

	_, supplierOutput, err = createSupplierPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateSupplierPartInput{PartID: 10, SupplierID: 0, SKU: "SKU-1"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, supplierOutput.Status)
	a.Equal("supplier", supplierOutput.Clarification.Field)
	a.True(supplierOutput.Clarification.HardError)
	a.False(fake.createdSupplierPart)

	_, supplierOutput, err = createSupplierPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateSupplierPartInput{PartID: 10, SupplierID: 30, SKU: "SKU-1", ManufacturerPartID: dvgoutils.Ptr(-1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, supplierOutput.Status)
	a.Equal("manufacturer_part_id", supplierOutput.Clarification.Field)
	a.True(supplierOutput.Clarification.HardError)
	a.False(fake.createdSupplierPart)

	_, manufacturerOutput, err := createManufacturerPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateManufacturerPartInput{PartID: 0, ManufacturerID: 31})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, manufacturerOutput.Status)
	a.Equal("part", manufacturerOutput.Clarification.Field)
	a.True(manufacturerOutput.Clarification.HardError)
	a.False(fake.createdManufacturerPart)

	_, manufacturerOutput, err = createManufacturerPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateManufacturerPartInput{PartID: 10, ManufacturerID: 0})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, manufacturerOutput.Status)
	a.Equal("manufacturer", manufacturerOutput.Clarification.Field)
	a.True(manufacturerOutput.Clarification.HardError)
	a.False(fake.createdManufacturerPart)
}

func TestSetPartParametersUpdatesExistingAndCreatesMissing(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	zero := 0.0
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameters: []inventree.Parameter{
			{PK: 60, Template: 70, ModelType: "part.part", ModelID: 10, Data: "old"},
		},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: 70, Name: "Resistance", Units: dvgoutils.Ptr("ohm"), Choices: "0,10k", Enabled: true},
			{PK: 71, Name: "Tolerance", Units: dvgoutils.Ptr("%"), Enabled: true},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: 70},
			{PK: 81, Category: categoryID, Template: 71},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID: 10,
		Parameters: []ParameterSetInput{
			{Name: "Resistance", NumberValue: &zero},
			{Name: "Tolerance", Value: dvgoutils.Ptr("")},
		},
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	r.Len(output.Record, 2)
	a.Equal(inventree.CategoryParameterTemplateQuery{CategoryID: categoryID}, fake.lastSearchCategoryParameterTemplatesQuery)
	a.Equal(inventree.PartParameterQuery{PartID: 10}, fake.lastSearchPartParametersQuery)
	a.Equal(inventree.PatchFields{"data": inventree.Set("0")}, fake.lastUpdatePartParameterFields)
	a.Equal(inventree.NewPartParameter(10, 71, ""), fake.lastCreatePartParameter)
}

func TestSetPartParametersPreservesExplicitFalse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	falseValue := false
	templateID := 70
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: templateID},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID:     10,
		Parameters: []ParameterSetInput{{TemplateID: &templateID, BoolValue: &falseValue}},
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(inventree.NewPartParameter(10, templateID, "false"), fake.lastCreatePartParameter)
	a.Equal(templateID, fake.lastGetParameterTemplateID)
}

func TestSetPartParametersAsksForAmbiguousTemplate(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameters: []inventree.Parameter{
			{PK: 60, Template: 70, ModelType: "part.part", ModelID: 10, Data: "old"},
		},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: 70, Name: "Resistance", Units: dvgoutils.Ptr("ohm"), Enabled: true},
			{PK: 71, Name: "Resistance", Units: dvgoutils.Ptr("kohm"), Enabled: true},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: 70, DefaultValue: "10k"},
			{PK: 81, Category: categoryID, Template: 71},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID:     10,
		Parameters: []ParameterSetInput{{Name: "Resistance", Value: dvgoutils.Ptr("10k")}},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template_id", output.Clarification.Retry)
	a.Len(output.Clarification.Candidates, 2)
	a.Equal(true, output.Clarification.Candidates[0].Fields["enabled"])
	a.Equal(true, output.Clarification.Candidates[0].Fields["category_linked"])
	a.Equal(80, output.Clarification.Candidates[0].Fields["category_link_id"])
	a.Equal("old", output.Clarification.Candidates[0].Fields["existing_value"])
	a.False(fake.createdPartParameter)
	a.Nil(fake.lastUpdatePartParameterFields)
}

func TestSetPartParametersRefusesDisabledOrUnlinkedTemplates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: 70, Name: "Resistance", Enabled: false},
			{PK: 71, Name: "Resistance", Enabled: true},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: 70},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID:     10,
		Parameters: []ParameterSetInput{{Name: "Resistance", Value: dvgoutils.Ptr("10k")}},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template", output.Clarification.Field)
	a.True(output.Clarification.HardError)
	a.False(fake.createdPartParameter)
	a.Nil(fake.lastUpdatePartParameterFields)

	templateID := 71
	_, output, err = setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID:     10,
		Parameters: []ParameterSetInput{{TemplateID: &templateID, Value: dvgoutils.Ptr("10k")}},
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("template_id", output.Clarification.Field)
	a.True(output.Clarification.HardError)
}

func TestSetPartParametersRefusesDisabledTemplateID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	templateID := 70
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: templateID, Name: "Resistance", Enabled: false},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: templateID},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID:     10,
		Parameters: []ParameterSetInput{{TemplateID: &templateID, Value: dvgoutils.Ptr("10k")}},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template_id", output.Clarification.Field)
	a.True(output.Clarification.HardError)
	a.False(fake.createdPartParameter)
}

func TestSetPartParametersPreflightsBeforeWriting(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: 70, Name: "Resistance", Enabled: true},
			{PK: 71, Name: "Tolerance", Enabled: true},
			{PK: 72, Name: "Tolerance", Enabled: true},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: 70},
			{PK: 81, Category: categoryID, Template: 71},
			{PK: 82, Category: categoryID, Template: 72},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID: 10,
		Parameters: []ParameterSetInput{
			{Name: "Resistance", Value: dvgoutils.Ptr("10k")},
			{Name: "Tolerance", Value: dvgoutils.Ptr("1%")},
		},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template_id", output.Clarification.Retry)
	a.False(fake.createdPartParameter)
	a.Zero(fake.updatePartParameterCount)
}

func TestSetPartParametersRejectsDuplicateTemplatesBeforeWriting(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: 70, Name: "Resistance", Enabled: true},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: 70},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID: 10,
		Parameters: []ParameterSetInput{
			{Name: "Resistance", Value: dvgoutils.Ptr("10k")},
			{Name: "Resistance", Value: dvgoutils.Ptr("22k")},
		},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template_id", output.Clarification.Field)
	a.True(output.Clarification.HardError)
	a.Zero(fake.createPartParameterCount)
	a.Zero(fake.updatePartParameterCount)
}

func TestSetPartParametersAsksForInvalidInputs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakeMilestoneLookupClient{part: inventree.Part{PK: 10, Category: &categoryID}}
	value := "10k"
	falseValue := false

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{PartID: 0, Parameters: []ParameterSetInput{{Value: &value}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)

	_, output, err = setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{PartID: 10})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("parameters", output.Clarification.Field)

	_, output, err = setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{PartID: 10, Parameters: []ParameterSetInput{{Name: "Resistance", Value: &value, BoolValue: &falseValue}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("value", output.Clarification.Field)
	a.False(fake.createdPartParameter)
}
