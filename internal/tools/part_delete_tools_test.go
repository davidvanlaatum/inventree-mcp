package tools

import (
	"context"
	"errors"
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
	variantParts       []inventree.Part

	deleteErr              error
	keepAfterDelete        bool
	ambiguousDeleteErr     error
	ambiguousDeleteApplied bool
	deleteCalls            int
	stockSearchErr         error
	// verifyGetPartErr, when set, is returned by GetPart instead of the
	// normal lookup once a delete has been attempted (deleteCalls > 0),
	// simulating an ambiguous post-delete read-back failure distinct from
	// both a clean not-found and a clean success.
	verifyGetPartErr error
}

func (f *fakePartDeleteClient) GetPart(_ context.Context, id int) (inventree.Part, error) {
	if f.verifyGetPartErr != nil && f.deleteCalls > 0 {
		return inventree.Part{}, f.verifyGetPartErr
	}
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
	if f.stockSearchErr != nil {
		return nil, f.stockSearchErr
	}
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
		if query.BasePart != 0 && (line.InternalPart == nil || *line.InternalPart != query.BasePart) {
			continue
		}
		result = append(result, line)
	}
	return result, nil
}

func (f *fakePartDeleteClient) SearchPartsByQuery(_ context.Context, query inventree.PartQuery) ([]inventree.Part, error) {
	result := make([]inventree.Part, 0, len(f.variantParts))
	for _, part := range f.variantParts {
		if query.VariantOf == 0 || (part.VariantOf != nil && *part.VariantOf == query.VariantOf) {
			result = append(result, part)
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
	if f.ambiguousDeleteErr != nil {
		if f.ambiguousDeleteApplied {
			delete(f.parts, id)
		}
		return f.ambiguousDeleteErr
	}
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

// InvenTree 1.5.0 itself refuses to delete any active part ("Cannot delete
// this part as it is still active"), independent of every other blocking
// category; this tool refuses on the same condition up front instead of
// letting an otherwise-clean confirmed deletion fail with a generic
// upstream validation error.
func TestDeletePartRefusesActivePart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	activePart := fake.parts[369]
	activePart.Active = true
	fake.parts[369] = activePart

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.True(out.Blocking.Active)
	a.Zero(fake.deleteCalls)
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

func TestDeletePartRefusesPurchaseOrderLine(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	basePart := 369
	fake.supplierParts = []inventree.SupplierPart{{PK: 40, Part: 369, Supplier: 30, SKU: "SKU-40"}}
	fake.purchaseOrderLines = []inventree.PurchaseOrderLineItem{{PK: 800, Order: 120, Part: 40, InternalPart: &basePart, Quantity: 5}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{800}, out.Blocking.PurchaseOrderLineIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesPurchaseOrderLineAcrossMultipleSupplierParts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	basePart := 369
	fake.supplierParts = []inventree.SupplierPart{
		{PK: 40, Part: 369, Supplier: 30, SKU: "SKU-40"},
		{PK: 41, Part: 369, Supplier: 31, SKU: "SKU-41"},
	}
	fake.purchaseOrderLines = []inventree.PurchaseOrderLineItem{
		{PK: 800, Order: 120, Part: 40, InternalPart: &basePart, Quantity: 5},
		{PK: 801, Order: 121, Part: 41, InternalPart: &basePart, Quantity: 2},
	}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.ElementsMatch([]int{800, 801}, out.Blocking.PurchaseOrderLineIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesVariantPart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	templateID := 369
	fake.variantParts = []inventree.Part{{PK: 371, Name: "Solder Variant", VariantOf: &templateID}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{371}, out.Blocking.VariantPartIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesMultipleBlockingCategoriesSimultaneously(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.stockItems = []inventree.StockItem{{PK: 500, Part: 369, Quantity: 5}}
	fake.builds = []inventree.Build{{PK: 700, Part: 369, Reference: "BO-0001"}}
	partID := 369
	fake.salesOrderLines = []inventree.SalesOrderLineItem{{PK: 900, Order: 55, Part: &partID, Quantity: 1}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{500}, out.Blocking.StockItemIDs)
	a.Equal([]int{700}, out.Blocking.BuildIDs)
	a.Equal([]int{900}, out.Blocking.SalesOrderLineIDs)
	a.Empty(out.Blocking.BomAsAssemblyIDs)
	a.Empty(out.Blocking.BomAsComponentIDs)
	a.Empty(out.Blocking.PurchaseOrderLineIDs)
	a.Empty(out.Blocking.VariantPartIDs)
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

// InvenTree 1.5.0's own DELETE /api/part/{id}/ silently permits deleting a
// part while a supplier part, manufacturer part, parameter, attachment, or
// related-part link still exists (pinned by the "part_delete" client-method
// integration subtest) -- there is no upstream protection to rely on for
// any of these. This tool's own guard therefore blocks on every one of them
// itself, exactly like stock, BOM, builds, and orders.
func TestDeletePartRefusesSupplierPart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.supplierParts = []inventree.SupplierPart{{PK: 40, Part: 369, Supplier: 30, SKU: "SKU-40"}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{40}, out.Blocking.SupplierPartIDs)
	r.Len(out.SupplierParts, 1, "the full record stays available for review even though it's also reported as blocking")
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesManufacturerPart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.manufacturerParts = []inventree.ManufacturerPart{{PK: 41, Part: 369, Manufacturer: 31, MPN: "MPN-41"}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{41}, out.Blocking.ManufacturerPartIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesParameter(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.parameters = []inventree.Parameter{{PK: 42, Template: 1, ModelType: "part", ModelID: 369, Data: "1"}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{42}, out.Blocking.ParameterIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesAttachment(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.attachments = []inventree.Attachment{{PK: 43, ModelType: "part", ModelID: 369, Filename: "datasheet.pdf"}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{43}, out.Blocking.AttachmentIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartRefusesRelatedPart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	otherPartID := 370
	fake.partRelations = []inventree.PartRelation{{PK: 44, Part1: 369, Part2: otherPartID}}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{44}, out.Blocking.RelatedPartIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartPreviewReportsFullDependencyDetail(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	a.Equal("confirm", out.Clarification.Field)
	a.Nil(out.Blocking)
	r.NotNil(out.Category)
	a.Equal(10, out.Category.PK)
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
	a.False(deleted.Recovered, "an ordinary clean delete must not be reported as recovered from an ambiguous error")
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

// A non-validation, non-timeout DeletePart error (e.g. a dropped connection
// or a 5xx) is ambiguous, not a proof of failure: InvenTree may have already
// applied the deletion before the response was lost. This must be recovered
// by read-back rather than reported as a clean failure.
func TestDeletePartRecoversWhenAmbiguousMutationErrorAppliedAnyway(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.ambiguousDeleteErr = errors.New("connection reset by peer")
	fake.ambiguousDeleteApplied = true

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(out.Verified)
	a.True(out.Recovered, "a successful read-back after an ambiguous mutation error must be reported as recovered")
	a.Equal(1, fake.deleteCalls)
	_, stillThere := fake.parts[369]
	a.False(stillThere)
}

// The same ambiguous error, but the deletion genuinely never applied, must
// still surface as partial_failure with recovery guidance rather than a
// false success.
func TestDeletePartPartialFailureWhenAmbiguousMutationErrorDidNotApply(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.ambiguousDeleteErr = errors.New("connection reset by peer")
	fake.ambiguousDeleteApplied = false

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	r.NotNil(out.Record)
	a.Equal(369, out.Record.PK)
	a.NotEmpty(out.RecoveryPlan)
	_, stillThere := fake.parts[369]
	a.True(stillThere)
}

// A definite rejection (a real 4xx status the upstream returned before ever
// touching the record, distinct from a dropped/ambiguous response) must be
// reported directly without a read-back verification attempt, since the
// deletion is already known not to have applied.
func TestDeletePartReturnsSafeErrorOnDefiniteMutationRejection(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.deleteErr = &inventree.APIError{StatusCode: 403, Kind: inventree.ErrorKindPermission}

	_, _, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.Error(err)
	a.Equal(1, fake.deleteCalls)
	_, stillThere := fake.parts[369]
	a.True(stillThere)
}

// 429 is a 4xx status, but definiteMutationRejection deliberately
// excludes it (along with 408 and 425) because a rate-limited or too-early
// request may still have been applied upstream; this must go through
// read-back verification rather than being treated as a definite rejection.
func TestDeletePartTreatsRetryableStatusAsAmbiguous(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.ambiguousDeleteErr = &inventree.APIError{StatusCode: 429, Kind: inventree.ErrorKindRateLimit}
	fake.ambiguousDeleteApplied = true

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(out.Verified)
	a.True(out.Recovered, "a 429 must be verified by read-back, not treated as a definite rejection")
	a.Equal(1, fake.deleteCalls)
	_, stillThere := fake.parts[369]
	a.False(stillThere)
}

// context.Canceled from the DeletePart mutation call must propagate as the
// tool's own returned error unwrapped, not be replaced by the generic
// safePartDeleteError message.
func TestDeletePartPreservesContextCanceledOnMutation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.deleteErr = context.Canceled

	_, _, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.Error(err)
	a.ErrorIs(err, context.Canceled)
}

// The same sentinel-preservation requirement applies to every preflight
// read, not just the mutation call; SearchStockItems is representative of
// the ~13 other safePartDeleteError call sites.
func TestDeletePartPreservesContextDeadlineExceededOnPreflight(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.stockSearchErr = context.DeadlineExceeded

	_, _, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.Error(err)
	a.ErrorIs(err, context.DeadlineExceeded)
	a.Zero(fake.deleteCalls, "a preflight failure must never reach the delete call")
}

// A genuinely ambiguous read-back failure (neither a clean not-found nor a
// clean success) after a confirmed delete must surface as partial_failure
// with recovery guidance, never a false success.
func TestDeletePartPartialFailureWhenReadBackItselfErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPartDeleteFixture()
	fake.verifyGetPartErr = &inventree.APIError{StatusCode: 500, Kind: inventree.ErrorKindServer}

	_, out, err := deletePart(partDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: 369, Confirm: true})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	r.NotNil(out.Record)
	a.Equal(369, out.Record.PK)
	a.NotEmpty(out.RecoveryPlan)
	a.Equal(1, fake.deleteCalls)
}
