package tools

import (
	"context"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePartDeleteClient struct {
	parts              map[int]inventree.Part
	categories         map[int]inventree.Category
	stockItems         []inventree.StockItem
	supplierParts      []inventree.SupplierPart
	manufacturerParts  []inventree.ManufacturerPart
	parameters         []inventree.Parameter
	attachments        []inventree.Attachment
	bomItems           []inventree.BomItem
	builds             []inventree.Build
	purchaseOrderLines []inventree.PurchaseOrderLineItem
	salesOrderLines    []inventree.SalesOrderLineItem
	partRelations      []inventree.PartRelation

	deleteErr       error
	keepAfterDelete bool
	deleteCalls     int
}

func (f *fakePartDeleteClient) GetPart(_ context.Context, id int) (inventree.Part, error) {
	part, ok := f.parts[id]
	if !ok {
		return inventree.Part{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	return part, nil
}

func (f *fakePartDeleteClient) GetPartCategory(_ context.Context, id int) (inventree.Category, error) {
	category, ok := f.categories[id]
	if !ok {
		return inventree.Category{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	return category, nil
}

func (f *fakePartDeleteClient) SearchStockItems(_ context.Context, query inventree.StockItemQuery) ([]inventree.StockItem, error) {
	result := make([]inventree.StockItem, 0, len(f.stockItems))
	for _, item := range f.stockItems {
		if query.PartID == 0 || item.Part == query.PartID {
			result = append(result, item)
		}
	}
	return result, nil
}

func (f *fakePartDeleteClient) SearchSupplierParts(_ context.Context, query inventree.SupplierPartQuery) ([]inventree.SupplierPart, error) {
	result := make([]inventree.SupplierPart, 0, len(f.supplierParts))
	for _, part := range f.supplierParts {
		if query.Part == 0 || part.Part == query.Part {
			result = append(result, part)
		}
	}
	return result, nil
}

func (f *fakePartDeleteClient) SearchManufacturerParts(_ context.Context, query inventree.ManufacturerPartQuery) ([]inventree.ManufacturerPart, error) {
	result := make([]inventree.ManufacturerPart, 0, len(f.manufacturerParts))
	for _, part := range f.manufacturerParts {
		if query.Part == 0 || part.Part == query.Part {
			result = append(result, part)
		}
	}
	return result, nil
}

func (f *fakePartDeleteClient) SearchPartParameters(_ context.Context, query inventree.PartParameterQuery) ([]inventree.Parameter, error) {
	result := make([]inventree.Parameter, 0, len(f.parameters))
	for _, parameter := range f.parameters {
		if query.PartID == 0 || parameter.ModelID == query.PartID {
			result = append(result, parameter)
		}
	}
	return result, nil
}

func (f *fakePartDeleteClient) ListAttachments(_ context.Context, query inventree.AttachmentQuery) ([]inventree.Attachment, error) {
	result := make([]inventree.Attachment, 0, len(f.attachments))
	for _, attachment := range f.attachments {
		if query.ModelID == 0 || attachment.ModelID == query.ModelID {
			result = append(result, attachment)
		}
	}
	return result, nil
}

func (f *fakePartDeleteClient) SearchBomItems(_ context.Context, query inventree.BomItemQuery) ([]inventree.BomItem, error) {
	result := make([]inventree.BomItem, 0, len(f.bomItems))
	for _, item := range f.bomItems {
		switch {
		case query.Part != 0 && item.Part == query.Part:
			result = append(result, item)
		case query.Uses != 0 && item.SubPart == query.Uses:
			result = append(result, item)
		}
	}
	return result, nil
}

func (f *fakePartDeleteClient) SearchBuilds(_ context.Context, query inventree.BuildQuery) ([]inventree.Build, error) {
	result := make([]inventree.Build, 0, len(f.builds))
	for _, build := range f.builds {
		if query.Part == 0 || build.Part == query.Part {
			result = append(result, build)
		}
	}
	return result, nil
}

func (f *fakePartDeleteClient) SearchPurchaseOrderLines(_ context.Context, query inventree.PurchaseOrderLineQuery) ([]inventree.PurchaseOrderLineItem, error) {
	result := make([]inventree.PurchaseOrderLineItem, 0, len(f.purchaseOrderLines))
	for _, line := range f.purchaseOrderLines {
		if query.SupplierPart == 0 || line.Part == query.SupplierPart {
			result = append(result, line)
		}
	}
	return result, nil
}

func (f *fakePartDeleteClient) SearchSalesOrderLines(_ context.Context, query inventree.SalesOrderLineQuery) ([]inventree.SalesOrderLineItem, error) {
	result := make([]inventree.SalesOrderLineItem, 0, len(f.salesOrderLines))
	for _, line := range f.salesOrderLines {
		if query.Part == 0 || (line.Part != nil && *line.Part == query.Part) {
			result = append(result, line)
		}
	}
	return result, nil
}

func (f *fakePartDeleteClient) SearchPartRelations(_ context.Context, query inventree.PartRelationQuery) ([]inventree.PartRelation, error) {
	result := make([]inventree.PartRelation, 0, len(f.partRelations))
	for _, relation := range f.partRelations {
		if query.Part == 0 || relation.Part1 == query.Part || relation.Part2 == query.Part {
			result = append(result, relation)
		}
	}
	return result, nil
}

func (f *fakePartDeleteClient) DeletePart(_ context.Context, id int) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if f.keepAfterDelete {
		return nil
	}
	delete(f.parts, id)
	return nil
}

func partDeleteDeps(fake *fakePartDeleteClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func newPartDeleteFixture() *fakePartDeleteClient {
	categoryID := 10
	return &fakePartDeleteClient{
		parts: map[int]inventree.Part{
			369: {PK: 369, Name: "Solder", Category: &categoryID},
			370: {PK: 370, Name: "Assembly", Category: &categoryID},
		},
		categories: map[int]inventree.Category{
			10: {PK: 10, Name: "Consumables"},
		},
	}
}

func TestDeletePartInvalidID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 0})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	a.Equal("id", out.Clarification.Field)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartNotFound(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 999})
	r.NoError(err)
	a.Equal(StatusNotFound, out.Status)
}

func TestDeletePartRefusesStock(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.stockItems = []inventree.StockItem{{PK: 500, Part: 369, Quantity: 5}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{500}, out.Blocking.StockItemIDs)
	a.Zero(fake.deleteCalls)
	_, stillThere := fake.parts[369]
	a.True(stillThere)
}

func TestDeletePartRefusesBomAsAssembly(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.bomItems = []inventree.BomItem{{PK: 600, Part: 369, SubPart: 371, Quantity: 1}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{600}, out.Blocking.BomAsAssemblyIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesBomAsComponent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.bomItems = []inventree.BomItem{{PK: 601, Part: 370, SubPart: 369, Quantity: 2}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{601}, out.Blocking.BomAsComponentIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesBuild(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.builds = []inventree.Build{{PK: 700, Part: 369, Reference: "BO-0001"}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{700}, out.Blocking.BuildIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesPurchaseOrderLineViaSupplierPart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.supplierParts = []inventree.SupplierPart{{PK: 40, Part: 369, Supplier: 30, SKU: "SKU-40"}}
	fake.purchaseOrderLines = []inventree.PurchaseOrderLineItem{{PK: 800, Order: 120, Part: 40, Quantity: 5}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{800}, out.Blocking.PurchaseOrderLineIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesSalesOrderLine(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	partID := 369
	fake.salesOrderLines = []inventree.SalesOrderLineItem{{PK: 900, Order: 55, Part: &partID, Quantity: 1}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{900}, out.Blocking.SalesOrderLineIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartPreviewReportsInformationalContext(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.supplierParts = []inventree.SupplierPart{{PK: 40, Part: 369, Supplier: 30, SKU: "SKU-40"}}
	fake.manufacturerParts = []inventree.ManufacturerPart{{PK: 41, Part: 369, Manufacturer: 31, MPN: "MPN-41"}}
	fake.parameters = []inventree.Parameter{{PK: 42, Template: 1, ModelType: "part", ModelID: 369, Data: "1"}}
	fake.attachments = []inventree.Attachment{{PK: 43, ModelType: "part", ModelID: 369, Filename: "datasheet.pdf"}}
	otherPartID := 370
	fake.partRelations = []inventree.PartRelation{{PK: 44, Part1: 369, Part2: otherPartID}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	a.Equal("confirm", out.Clarification.Field)
	a.Nil(out.Blocking)
	r.NotNil(out.Category)
	a.Equal(10, out.Category.PK)
	r.Len(out.SupplierParts, 1)
	r.Len(out.ManufacturerParts, 1)
	r.Len(out.Parameters, 1)
	r.Len(out.Attachments, 1)
	r.Len(out.RelatedParts, 1)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartPreviewThenConfirmedDelete(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()

	_, preview, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, preview.Status)
	r.NotNil(preview.Clarification)
	a.Equal("confirm", preview.Clarification.Field)
	r.NotNil(preview.Record)
	a.Equal(369, preview.Record.PK)
	a.Zero(fake.deleteCalls)

	_, deleted, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, deleted.Status)
	a.True(deleted.Verified)
	r.NotNil(deleted.Record)
	a.Equal(369, deleted.Record.PK)
	a.Equal(1, fake.deleteCalls)
	_, stillThere := fake.parts[369]
	a.False(stillThere)
}

func TestDeletePartValidationFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.deleteErr = &inventree.APIError{StatusCode: 400, Kind: inventree.ErrorKindValidation, FieldErrors: map[string][]string{"non_field_errors": {"cannot delete"}}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
	r.NotNil(out.Validation)
}

func TestDeletePartAmbiguousReadBackAfterDelete(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.keepAfterDelete = true

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	r.NotNil(out.Record)
	a.Equal(369, out.Record.PK)
	a.NotEmpty(out.RecoveryPlan)
}
