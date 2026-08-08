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

type fakePurchaseOrderLineDeleteClient struct {
	orders          map[int]inventree.PurchaseOrder
	lines           map[int]inventree.PurchaseOrderLineItem
	supplierParts   map[int]inventree.SupplierPart
	stockItems      []inventree.StockItem
	deleteErr       error
	keepAfterDelete bool
	deleteCalls     int
	stockSearchErr  error
}

func (f *fakePurchaseOrderLineDeleteClient) GetPurchaseOrder(_ context.Context, id int) (inventree.PurchaseOrder, error) {
	order, ok := f.orders[id]
	if !ok {
		return inventree.PurchaseOrder{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	return order, nil
}

func (f *fakePurchaseOrderLineDeleteClient) GetPurchaseOrderLine(_ context.Context, id int) (inventree.PurchaseOrderLineItem, error) {
	line, ok := f.lines[id]
	if !ok {
		return inventree.PurchaseOrderLineItem{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	return line, nil
}

func (f *fakePurchaseOrderLineDeleteClient) GetSupplierPart(_ context.Context, id int) (inventree.SupplierPart, error) {
	part, ok := f.supplierParts[id]
	if !ok {
		return inventree.SupplierPart{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	return part, nil
}

func (f *fakePurchaseOrderLineDeleteClient) SearchStockItems(_ context.Context, query inventree.StockItemQuery) ([]inventree.StockItem, error) {
	if f.stockSearchErr != nil {
		return nil, f.stockSearchErr
	}
	result := make([]inventree.StockItem, 0, len(f.stockItems))
	for _, item := range f.stockItems {
		if query.PurchaseOrderID == 0 || (item.PurchaseOrder != nil && *item.PurchaseOrder == query.PurchaseOrderID) {
			result = append(result, item)
		}
	}
	return result, nil
}

func (f *fakePurchaseOrderLineDeleteClient) DeletePurchaseOrderLine(_ context.Context, id int) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if f.keepAfterDelete {
		return nil
	}
	delete(f.lines, id)
	return nil
}

func purchaseOrderLineDeleteDeps(fake *fakePurchaseOrderLineDeleteClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func newPurchaseOrderLineDeleteFixture() *fakePurchaseOrderLineDeleteClient {
	return &fakePurchaseOrderLineDeleteClient{
		orders: map[int]inventree.PurchaseOrder{
			120: {PK: 120, Reference: "PO-120", Supplier: 30, Status: inventree.PurchaseOrderStatusPending},
			121: {PK: 121, Reference: "PO-121", Supplier: 30, Status: inventree.PurchaseOrderStatusPlaced},
		},
		lines: map[int]inventree.PurchaseOrderLineItem{
			201: {PK: 201, Order: 120, Part: 40, Quantity: 5, Received: 0},
			202: {PK: 202, Order: 120, Part: 40, Quantity: 3, Received: 1.5},
			203: {PK: 203, Order: 121, Part: 40, Quantity: 2, Received: 0},
		},
		supplierParts: map[int]inventree.SupplierPart{
			40: {PK: 40, Part: 900, Supplier: 30, SKU: "SKU-40"},
		},
	}
}

func TestDeletePurchaseOrderLineInvalidID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPurchaseOrderLineDeleteFixture()

	_, out, err := deletePurchaseOrderLine(purchaseOrderLineDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: 0})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	a.Equal("id", out.Clarification.Field)
	a.Zero(fake.deleteCalls)
}

func TestDeletePurchaseOrderLineNotFound(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPurchaseOrderLineDeleteFixture()

	_, out, err := deletePurchaseOrderLine(purchaseOrderLineDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: 999})
	r.NoError(err)
	a.Equal(StatusNotFound, out.Status)
}

func TestDeletePurchaseOrderLineRefusesReceivedLine(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPurchaseOrderLineDeleteFixture()

	_, out, err := deletePurchaseOrderLine(purchaseOrderLineDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: 202, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	a.Equal("received", out.Clarification.Field)
	a.True(out.Clarification.HardError)
	r.NotNil(out.Record)
	a.Equal(202, out.Record.PK)
	r.NotNil(out.PurchaseOrder)
	a.Equal(120, out.PurchaseOrder.PK)
	a.Zero(fake.deleteCalls)
	_, stillThere := fake.lines[202]
	a.True(stillThere, "received line must not be deleted")
}

func TestDeletePurchaseOrderLineRefusesLinkedStock(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPurchaseOrderLineDeleteFixture()
	supplierPartID := 40
	orderID := 120
	fake.stockItems = []inventree.StockItem{{PK: 500, PurchaseOrder: &orderID, SupplierPart: &supplierPartID, Quantity: 2}}

	_, out, err := deletePurchaseOrderLine(purchaseOrderLineDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: 201, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	a.Equal("stock", out.Clarification.Field)
	a.Zero(fake.deleteCalls)
	_, stillThere := fake.lines[201]
	a.True(stillThere, "line with linked stock provenance must not be deleted")
}

func TestDeletePurchaseOrderLinePreviewThenConfirmedDelete(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPurchaseOrderLineDeleteFixture()

	_, preview, err := deletePurchaseOrderLine(purchaseOrderLineDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: 201})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, preview.Status)
	r.NotNil(preview.Clarification)
	a.Equal("confirm", preview.Clarification.Field)
	r.NotNil(preview.Record)
	a.Equal(201, preview.Record.PK)
	r.NotNil(preview.PurchaseOrder)
	a.Equal(120, preview.PurchaseOrder.PK)
	a.Equal(900, preview.PartID)
	a.Zero(fake.deleteCalls)

	_, deleted, err := deletePurchaseOrderLine(purchaseOrderLineDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: 201, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, deleted.Status)
	a.True(deleted.Verified)
	r.NotNil(deleted.Record)
	a.Equal(201, deleted.Record.PK)
	r.NotNil(deleted.PurchaseOrder)
	a.Equal(120, deleted.PurchaseOrder.PK)
	a.Equal(900, deleted.PartID)
	a.Equal(1, fake.deleteCalls)
	_, stillThere := fake.lines[201]
	a.False(stillThere)
}

func TestDeletePurchaseOrderLineOnPlacedOrderSucceeds(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPurchaseOrderLineDeleteFixture()

	_, deleted, err := deletePurchaseOrderLine(purchaseOrderLineDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: 203, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, deleted.Status)
	a.True(deleted.Verified)
	r.NotNil(deleted.PurchaseOrder)
	a.Equal(inventree.PurchaseOrderStatusPlaced, deleted.PurchaseOrder.Status)
}

func TestDeletePurchaseOrderLineValidationFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPurchaseOrderLineDeleteFixture()
	fake.deleteErr = &inventree.APIError{StatusCode: 400, Kind: inventree.ErrorKindValidation, FieldErrors: map[string][]string{"non_field_errors": {"cannot delete"}}}

	_, out, err := deletePurchaseOrderLine(purchaseOrderLineDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: 201, Confirm: true})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
	r.NotNil(out.Validation)
}

func TestDeletePurchaseOrderLineAmbiguousReadBackAfterDelete(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newPurchaseOrderLineDeleteFixture()
	fake.keepAfterDelete = true

	_, out, err := deletePurchaseOrderLine(purchaseOrderLineDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: 201, Confirm: true})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	r.NotNil(out.Record)
	a.Equal(201, out.Record.PK)
	a.NotEmpty(out.RecoveryPlan)
}
