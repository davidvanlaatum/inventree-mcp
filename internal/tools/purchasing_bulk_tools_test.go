package tools

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test fake
// ---------------------------------------------------------------------------

// bulkFakePurchaseOrders backs all three F-S79 bulk purchase-order tools with
// one map-keyed fake, mirroring bulkFakeStock's single-fake-covers-related-
// resources shape. It implements PurchaseOrderBulkUpdateClient,
// PurchaseOrderLineWriteClient, and PurchaseOrderExtraLineClient so it
// satisfies whichever interface LookupHandler resolves per tool; methods
// unused by these bulk tools are unreachable stubs.
type bulkFakePurchaseOrders struct {
	mu                  sync.Mutex
	orders              map[int]inventree.PurchaseOrderDetail
	lines               map[int]inventree.PurchaseOrderLineItem
	extraLines          map[int]inventree.PurchaseOrderExtraLine
	supplierParts       map[int]inventree.SupplierPart
	locations           map[int]inventree.StockLocation
	orderUpdateErr      map[int]error
	orderApplyBeforeErr map[int]bool
	lineUpdateErr       map[int]error
	lineApplyBeforeErr  map[int]bool
	extraUpdateErr      map[int]error
	extraApplyBeforeErr map[int]bool
	orderUpdateCalls    int
	lineUpdateCalls     int
	extraUpdateCalls    int
}

func newBulkFakePurchaseOrders() *bulkFakePurchaseOrders {
	return &bulkFakePurchaseOrders{
		orders: map[int]inventree.PurchaseOrderDetail{}, lines: map[int]inventree.PurchaseOrderLineItem{},
		extraLines: map[int]inventree.PurchaseOrderExtraLine{}, supplierParts: map[int]inventree.SupplierPart{},
		locations: map[int]inventree.StockLocation{}, orderUpdateErr: map[int]error{}, orderApplyBeforeErr: map[int]bool{},
		lineUpdateErr: map[int]error{}, lineApplyBeforeErr: map[int]bool{}, extraUpdateErr: map[int]error{}, extraApplyBeforeErr: map[int]bool{},
	}
}

func (f *bulkFakePurchaseOrders) GetPurchaseOrder(_ context.Context, id int) (inventree.PurchaseOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	order, ok := f.orders[id]
	if !ok {
		return inventree.PurchaseOrder{}, notFoundErr()
	}
	return order.PurchaseOrder, nil
}

func (f *bulkFakePurchaseOrders) GetPurchaseOrderDetail(_ context.Context, id int) (inventree.PurchaseOrderDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	order, ok := f.orders[id]
	if !ok {
		return inventree.PurchaseOrderDetail{}, notFoundErr()
	}
	return order, nil
}

func (f *bulkFakePurchaseOrders) UpdatePurchaseOrderDetail(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orderUpdateCalls++
	order := f.orders[id]
	applyPurchaseOrderPatchForTest(&order, fields)
	if err := f.orderUpdateErr[id]; err != nil {
		if f.orderApplyBeforeErr[id] {
			f.orders[id] = order
		}
		return inventree.PurchaseOrderDetail{}, err
	}
	f.orders[id] = order
	return order, nil
}

func (f *bulkFakePurchaseOrders) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	loc, ok := f.locations[id]
	if !ok {
		return inventree.StockLocation{}, notFoundErr()
	}
	return loc, nil
}

func (f *bulkFakePurchaseOrders) GetPurchaseOrderLine(_ context.Context, id int) (inventree.PurchaseOrderLineItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line, ok := f.lines[id]
	if !ok {
		return inventree.PurchaseOrderLineItem{}, notFoundErr()
	}
	return line, nil
}

func (f *bulkFakePurchaseOrders) GetSupplierPart(_ context.Context, id int) (inventree.SupplierPart, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	part, ok := f.supplierParts[id]
	if !ok {
		return inventree.SupplierPart{}, notFoundErr()
	}
	return part, nil
}

func (f *bulkFakePurchaseOrders) CreatePurchaseOrderLine(context.Context, inventree.PurchaseOrderLineCreate) (inventree.PurchaseOrderLineItem, error) {
	return inventree.PurchaseOrderLineItem{}, errors.New("not implemented")
}

func (f *bulkFakePurchaseOrders) UpdatePurchaseOrderLine(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderLineItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lineUpdateCalls++
	line := f.lines[id]
	applyPurchaseOrderLinePatchForTest(&line, fields)
	if err := f.lineUpdateErr[id]; err != nil {
		if f.lineApplyBeforeErr[id] {
			f.lines[id] = line
		}
		return inventree.PurchaseOrderLineItem{}, err
	}
	f.lines[id] = line
	return line, nil
}

func (f *bulkFakePurchaseOrders) SearchPurchaseOrderExtraLinesPage(_ context.Context, query inventree.PurchaseOrderExtraLineQuery) (inventree.Page[inventree.PurchaseOrderExtraLine], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]int, 0, len(f.extraLines))
	for id := range f.extraLines {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	results := make([]inventree.PurchaseOrderExtraLine, 0, len(ids))
	for _, id := range ids {
		line := f.extraLines[id]
		if query.Order != 0 && line.Order != query.Order {
			continue
		}
		if query.Search != "" && !strings.Contains(line.Reference, query.Search) {
			continue
		}
		results = append(results, line)
	}
	return inventree.Page[inventree.PurchaseOrderExtraLine]{Count: len(results), Results: results}, nil
}

func (f *bulkFakePurchaseOrders) GetPurchaseOrderExtraLine(_ context.Context, id int) (inventree.PurchaseOrderExtraLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line, ok := f.extraLines[id]
	if !ok {
		return inventree.PurchaseOrderExtraLine{}, notFoundErr()
	}
	return line, nil
}

func (f *bulkFakePurchaseOrders) CreatePurchaseOrderExtraLine(context.Context, inventree.PurchaseOrderExtraLineCreate) (inventree.PurchaseOrderExtraLine, error) {
	return inventree.PurchaseOrderExtraLine{}, errors.New("not implemented")
}

func (f *bulkFakePurchaseOrders) UpdatePurchaseOrderExtraLine(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderExtraLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.extraUpdateCalls++
	line := f.extraLines[id]
	applyExtraLinePatchForTest(&line, fields)
	if err := f.extraUpdateErr[id]; err != nil {
		if f.extraApplyBeforeErr[id] {
			f.extraLines[id] = line
		}
		return inventree.PurchaseOrderExtraLine{}, err
	}
	f.extraLines[id] = line
	return line, nil
}

func (f *bulkFakePurchaseOrders) DeletePurchaseOrderExtraLine(context.Context, int) error {
	return errors.New("not implemented")
}

func applyPurchaseOrderPatchForTest(order *inventree.PurchaseOrderDetail, fields inventree.PatchFields) {
	for name, field := range fields {
		value := field.Value()
		switch name {
		case "description":
			order.Description = value.(string)
		case "supplier_reference":
			order.SupplierReference = value.(string)
		case "order_currency":
			currency := value.(string)
			order.OrderCurrency = &currency
		case "link":
			order.Link = value.(string)
		case "creation_date":
			date := value.(string)
			order.CreationDate = &date
		case "notes":
			if value == nil {
				order.Notes = nil
			} else {
				text := value.(string)
				order.Notes = &text
			}
		case "start_date":
			if value == nil {
				order.StartDate = nil
			} else {
				date := value.(string)
				order.StartDate = &date
			}
		case "target_date":
			if value == nil {
				order.TargetDate = nil
			} else {
				date := value.(string)
				order.TargetDate = &date
			}
		case "destination":
			if value == nil {
				order.Destination = nil
			} else {
				id := value.(int)
				order.Destination = &id
			}
		}
	}
}

func applyPurchaseOrderLinePatchForTest(line *inventree.PurchaseOrderLineItem, fields inventree.PatchFields) {
	for name, field := range fields {
		value := field.Value()
		switch name {
		case "order":
			line.Order = value.(int)
		case "part":
			line.Part = value.(int)
		case "line":
			line.Line = value.(string)
		case "reference":
			line.Reference = value.(string)
		case "notes":
			line.Notes = value.(string)
		case "quantity":
			line.Quantity = value.(float64)
		case "purchase_price":
			price := inventree.DecimalString(value.(string))
			line.PurchasePrice = &price
		case "purchase_price_currency":
			line.PurchasePriceCurrency = value.(string)
		case "target_date":
			date := value.(string)
			line.TargetDate = &date
		case "destination":
			id := value.(int)
			line.Destination = &id
		case "link":
			line.Link = value.(string)
		case "discount":
			line.Discount = value.(float64)
		}
	}
}

func testPurchaseOrderBulkDeps(client any) Dependencies {
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }}
	deps.purchaseOrderBulkPlanStore = mustBulkStore(batch.Options[purchaseOrderBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p purchaseOrderBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	deps.purchaseOrderLineBulkPlanStore = mustBulkStore(batch.Options[purchaseOrderLineBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p purchaseOrderLineBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	deps.purchaseOrderExtraLineBulkPlanStore = mustBulkStore(batch.Options[purchaseOrderExtraLineBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p purchaseOrderExtraLineBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	return deps
}

// ---------------------------------------------------------------------------
// bulk_update_purchase_orders
// ---------------------------------------------------------------------------

func TestBuildPurchaseOrderBulkPlanRejectsUnknownID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()

	plan := buildPurchaseOrderBulkPlan(ctx, client, []BulkUpdatePurchaseOrderItem{{ID: 1, Description: dvgoutils.Ptr("x")}})
	require.Len(t, plan.Items, 1)
	assert.NotEmpty(t, plan.Items[0].FailReason)
}

func TestBuildPurchaseOrderBulkPlanRejectsDuplicateIDWithinBatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1}}

	plan := buildPurchaseOrderBulkPlan(ctx, client, []BulkUpdatePurchaseOrderItem{
		{ID: 1, Description: dvgoutils.Ptr("a")},
		{ID: 1, Description: dvgoutils.Ptr("b")},
	})
	require.Len(t, plan.Items, 2)
	assert.Equal(t, bulkReasonDuplicateID, plan.Items[0].FailReason)
	assert.Equal(t, bulkReasonDuplicateID, plan.Items[1].FailReason)
}

func TestBulkUpdatePurchaseOrdersDryRunThenConfirmApplies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Description: "Old"}}
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrders(deps)

	items := []BulkUpdatePurchaseOrderItem{{ID: 1, Description: dvgoutils.Ptr("New")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: items, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, dryOut.Status)
	r.Len(dryOut.Items, 1)
	a.Equal(bulkOutcomePlanned, dryOut.Items[0].Outcome)
	r.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, confirmOut.Status)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("New", confirmOut.Items[0].Record.Description)
	a.Equal(1, client.orderUpdateCalls)
}

func TestBulkUpdatePurchaseOrdersRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Description: "Old"}}
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrders(deps)

	items := []BulkUpdatePurchaseOrderItem{{ID: 1, Description: dvgoutils.Ptr("New")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: items, DryRun: true})
	r.NoError(err)

	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Description: "SomeoneElseChangedIt"}}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.orderUpdateCalls)
}

func TestBulkUpdatePurchaseOrdersMixedOutcomesAreIndependent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Description: "Applies"}}
	client.orders[2] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 2, Description: "AlreadyThere"}}
	// no client.orders[3]: fails plan-build entirely (unknown ID).
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrders(deps)

	items := []BulkUpdatePurchaseOrderItem{
		{ID: 1, Description: dvgoutils.Ptr("Applied")},
		{ID: 2, Description: dvgoutils.Ptr("AlreadyThere")},
		{ID: 3, Description: dvgoutils.Ptr("Missing")},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: items, DryRun: true})
	r.NoError(err)
	r.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, confirmOut.Status)
	r.Len(confirmOut.Items, 3)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	a.Equal(string(batch.OutcomeSkipped), confirmOut.Items[1].Outcome)
	a.Equal(string(batch.OutcomeFailed), confirmOut.Items[2].Outcome)
}

func TestBulkUpdatePurchaseOrdersRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Description: "Old"}}
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrders(deps)

	items := []BulkUpdatePurchaseOrderItem{{ID: 1, Description: dvgoutils.Ptr("Recovered")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: items, DryRun: true})
	r.NoError(err)

	client.orderUpdateErr[1] = &inventree.APIError{StatusCode: http.StatusBadGateway}
	client.orderApplyBeforeErr[1] = true

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("Recovered", confirmOut.Items[0].Record.Description)
}

func TestBulkUpdatePurchaseOrdersRejectsEmptyAndOversizedBatches(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	deps := testPurchaseOrderBulkDeps(newBulkFakePurchaseOrders())
	handler := bulkUpdatePurchaseOrders(deps)

	_, empty, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, empty.Status)

	oversized := make([]BulkUpdatePurchaseOrderItem, bulkUpdateMaxItems+1)
	for i := range oversized {
		oversized[i] = BulkUpdatePurchaseOrderItem{ID: i + 1, Description: dvgoutils.Ptr("x")}
	}
	_, tooMany, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: oversized})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, tooMany.Status)
}

// ---------------------------------------------------------------------------
// bulk_update_purchase_order_lines
// ---------------------------------------------------------------------------

func TestBuildPurchaseOrderLineBulkPlanRejectsSupplierMismatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Supplier: 10}}
	client.lines[1] = inventree.PurchaseOrderLineItem{PK: 1, Order: 1, Part: 5, Quantity: 2}
	client.supplierParts[6] = inventree.SupplierPart{PK: 6, Supplier: 99}

	plan := buildPurchaseOrderLineBulkPlan(ctx, client, []BulkUpdatePurchaseOrderLineItem{{ID: 1, SupplierPartID: dvgoutils.Ptr(6)}})
	require.Len(t, plan.Items, 1)
	assert.Contains(t, plan.Items[0].FailReason, "does not belong to the purchase-order supplier")
}

func TestBulkUpdatePurchaseOrderLinesDryRunThenConfirmApplies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Supplier: 10}}
	client.lines[1] = inventree.PurchaseOrderLineItem{PK: 1, Order: 1, Part: 5, Quantity: 2}
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrderLines(deps)

	items := []BulkUpdatePurchaseOrderLineItem{{ID: 1, Quantity: dvgoutils.Ptr(4.0)}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: items, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, dryOut.Status)
	r.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, confirmOut.Status)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.InDelta(4.0, confirmOut.Items[0].Record.Quantity, 1e-9)
	a.Equal(1, client.lineUpdateCalls)
}

func TestBulkUpdatePurchaseOrderLinesRejectsInvalidQuantity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Supplier: 10}}
	client.lines[1] = inventree.PurchaseOrderLineItem{PK: 1, Order: 1, Part: 5, Quantity: 2}
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrderLines(deps)

	items := []BulkUpdatePurchaseOrderLineItem{{ID: 1, Quantity: dvgoutils.Ptr(0.0)}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 1)
	a.Equal(bulkOutcomeFailed, dryOut.Items[0].Outcome)
	a.Contains(dryOut.Items[0].Message, "quantity must be greater than zero")
}

func TestBulkUpdatePurchaseOrderLinesRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Supplier: 10}}
	client.lines[1] = inventree.PurchaseOrderLineItem{PK: 1, Order: 1, Part: 5, Quantity: 2}
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrderLines(deps)

	items := []BulkUpdatePurchaseOrderLineItem{{ID: 1, Quantity: dvgoutils.Ptr(9.0)}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: items, DryRun: true})
	r.NoError(err)

	client.lineUpdateErr[1] = &inventree.APIError{StatusCode: http.StatusBadGateway}
	client.lineApplyBeforeErr[1] = true

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.InDelta(9.0, confirmOut.Items[0].Record.Quantity, 1e-9)
}

// ---------------------------------------------------------------------------
// bulk_update_purchase_order_extra_lines
// ---------------------------------------------------------------------------

func TestBuildPurchaseOrderExtraLineBulkPlanRejectsDuplicateReferenceWithinBatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.extraLines[1] = inventree.PurchaseOrderExtraLine{PK: 1, Order: 1, Reference: "r1", Quantity: 1}
	client.extraLines[2] = inventree.PurchaseOrderExtraLine{PK: 2, Order: 1, Reference: "r2", Quantity: 1}

	plan := buildPurchaseOrderExtraLineBulkPlan(ctx, client, []BulkUpdatePurchaseOrderExtraLineItem{
		{ID: 1, Reference: dvgoutils.Ptr("shared")},
		{ID: 2, Reference: dvgoutils.Ptr("shared")},
	})
	require.Len(t, plan.Items, 2)
	assert.Equal(t, bulkReasonDuplicateExtraLineReference, plan.Items[0].FailReason)
	assert.Equal(t, bulkReasonDuplicateExtraLineReference, plan.Items[1].FailReason)
}

func TestBuildPurchaseOrderExtraLineBulkPlanRejectsCollisionWithExistingLine(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.extraLines[1] = inventree.PurchaseOrderExtraLine{PK: 1, Order: 1, Reference: "r1", Quantity: 1}
	client.extraLines[2] = inventree.PurchaseOrderExtraLine{PK: 2, Order: 1, Reference: "taken", Quantity: 1}

	plan := buildPurchaseOrderExtraLineBulkPlan(ctx, client, []BulkUpdatePurchaseOrderExtraLineItem{{ID: 1, Reference: dvgoutils.Ptr("taken")}})
	require.Len(t, plan.Items, 1)
	assert.Contains(t, plan.Items[0].FailReason, "collides with an existing extra line")
}

func TestBulkUpdatePurchaseOrderExtraLinesDryRunThenConfirmApplies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.extraLines[1] = inventree.PurchaseOrderExtraLine{PK: 1, Order: 1, Reference: "r1", Quantity: 1}
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrderExtraLines(deps)

	items := []BulkUpdatePurchaseOrderExtraLineItem{{ID: 1, Description: dvgoutils.Ptr("Updated")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderExtraLinesInput{Items: items, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, dryOut.Status)
	r.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderExtraLinesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, confirmOut.Status)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("Updated", confirmOut.Items[0].Record.Description)
	a.Equal(1, client.extraUpdateCalls)
}

func TestBulkUpdatePurchaseOrderExtraLinesRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.extraLines[1] = inventree.PurchaseOrderExtraLine{PK: 1, Order: 1, Reference: "r1", Quantity: 1}
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrderExtraLines(deps)

	items := []BulkUpdatePurchaseOrderExtraLineItem{{ID: 1, Notes: dvgoutils.Ptr("recovered notes")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderExtraLinesInput{Items: items, DryRun: true})
	r.NoError(err)

	client.extraUpdateErr[1] = &inventree.APIError{StatusCode: http.StatusBadGateway}
	client.extraApplyBeforeErr[1] = true

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderExtraLinesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("recovered notes", confirmOut.Items[0].Record.Notes)
}

func TestBulkUpdatePurchaseOrderExtraLinesMixedOutcomesAreIndependent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.extraLines[1] = inventree.PurchaseOrderExtraLine{PK: 1, Order: 1, Reference: "applies", Quantity: 1}
	client.extraLines[2] = inventree.PurchaseOrderExtraLine{PK: 2, Order: 1, Reference: "already-there", Description: "same", Quantity: 1}
	// no client.extraLines[3]: fails plan-build entirely (unknown ID).
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrderExtraLines(deps)

	items := []BulkUpdatePurchaseOrderExtraLineItem{
		{ID: 1, Description: dvgoutils.Ptr("Applied")},
		{ID: 2, Description: dvgoutils.Ptr("same")},
		{ID: 3, Description: dvgoutils.Ptr("Missing")},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderExtraLinesInput{Items: items, DryRun: true})
	r.NoError(err)
	r.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderExtraLinesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, confirmOut.Status)
	r.Len(confirmOut.Items, 3)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	a.Equal(string(batch.OutcomeSkipped), confirmOut.Items[1].Outcome)
	a.Equal(string(batch.OutcomeFailed), confirmOut.Items[2].Outcome)
}

func TestBulkUpdatePurchaseOrderLinesRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePurchaseOrders()
	client.orders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Supplier: 10}}
	client.lines[1] = inventree.PurchaseOrderLineItem{PK: 1, Order: 1, Part: 5, Quantity: 2}
	deps := testPurchaseOrderBulkDeps(client)
	handler := bulkUpdatePurchaseOrderLines(deps)

	items := []BulkUpdatePurchaseOrderLineItem{{ID: 1, Quantity: dvgoutils.Ptr(7.0)}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: items, DryRun: true})
	r.NoError(err)

	// State drifts after the dry run but before confirm: the digest embedded
	// in the freshly rebuilt plan at confirm time no longer matches, so the
	// stored token must be rejected rather than silently applied.
	client.lines[1] = inventree.PurchaseOrderLineItem{PK: 1, Order: 1, Part: 5, Quantity: 99}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.lineUpdateCalls)
}

func TestPurchaseOrderLineFieldsMatchAndBeforeFieldsCoverEveryKey(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	price := inventree.DecimalString("1.5")
	targetDate := "2026-01-02"
	before := inventree.PurchaseOrderLineItem{
		PK: 1, Order: 10, Part: 20, Line: "L1", Reference: "R1", Notes: "before notes",
		Quantity: 3, PurchasePrice: &price, PurchasePriceCurrency: "AUD", TargetDate: &targetDate,
		Destination: dvgoutils.Ptr(30), Link: "https://example.com/before", Discount: 1.5,
	}
	fields := inventree.PatchFields{
		"order": inventree.Set(10), "part": inventree.Set(20), "line": inventree.Set("L1"),
		"reference": inventree.Set("R1"), "notes": inventree.Set("before notes"), "quantity": inventree.Set(3.0),
		"purchase_price": inventree.Set("1.5"), "purchase_price_currency": inventree.Set("AUD"),
		"target_date": inventree.Set(targetDate), "destination": inventree.Set(30),
		"link": inventree.Set("https://example.com/before"), "discount": inventree.Set(1.5),
	}
	a.True(purchaseOrderLineFieldsMatch(before, fields))

	beforeFields := purchaseOrderLineBeforeFields(before, fields)
	a.True(purchaseOrderLineFieldsMatch(before, beforeFields))

	noPriceOrDates := before
	noPriceOrDates.PurchasePrice = nil
	noPriceOrDates.TargetDate = nil
	noPriceOrDates.Destination = nil
	nullableFields := inventree.PatchFields{"purchase_price": inventree.Null(), "target_date": inventree.Null(), "destination": inventree.Null()}
	a.True(purchaseOrderLineFieldsMatch(noPriceOrDates, nullableFields))
	nullBeforeFields := purchaseOrderLineBeforeFields(noPriceOrDates, nullableFields)
	a.True(purchaseOrderLineFieldsMatch(noPriceOrDates, nullBeforeFields))

	for key, mismatched := range map[string]inventree.PatchValue{
		"order": inventree.Set(11), "part": inventree.Set(21), "line": inventree.Set("L2"),
		"reference": inventree.Set("R2"), "notes": inventree.Set("other notes"), "quantity": inventree.Set(4.0),
		"purchase_price": inventree.Set("9.9"), "purchase_price_currency": inventree.Set("USD"),
		"target_date": inventree.Set("2026-02-02"), "destination": inventree.Set(31),
		"link": inventree.Set("https://example.com/other"), "discount": inventree.Set(2.5),
	} {
		a.False(purchaseOrderLineFieldsMatch(before, inventree.PatchFields{key: mismatched}), "expected mismatch for %s", key)
	}
}

func TestExtraLineBeforeFieldsCoversEveryKey(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	price := inventree.DecimalString("2.5")
	targetDate := "2026-03-04"
	before := inventree.PurchaseOrderExtraLine{
		PK: 1, Order: 10, Reference: "R1", Description: "D1", Line: "L1", Link: "https://example.com/before",
		Notes: "N1", Quantity: 4, Price: &price, PriceCurrency: "AUD", TargetDate: &targetDate, Discount: 0.5,
	}
	fields := inventree.PatchFields{
		"order": inventree.Set(0), "reference": inventree.Set(0), "description": inventree.Set(0),
		"line": inventree.Set(0), "link": inventree.Set(0), "notes": inventree.Set(0), "quantity": inventree.Set(0),
		"price": inventree.Set(0), "price_currency": inventree.Set(0), "target_date": inventree.Set(0), "discount": inventree.Set(0),
	}
	beforeFields := extraLineBeforeFields(before, fields)
	a.True(extraLineMatchesPatch(before, beforeFields))

	noPriceOrDate := before
	noPriceOrDate.Price = nil
	noPriceOrDate.TargetDate = nil
	nullFields := inventree.PatchFields{"price": inventree.Set(0), "target_date": inventree.Set(0)}
	nullBeforeFields := extraLineBeforeFields(noPriceOrDate, nullFields)
	a.True(extraLineMatchesPatch(noPriceOrDate, nullBeforeFields))
}
