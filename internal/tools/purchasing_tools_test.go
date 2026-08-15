package tools

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPurchaseOrderLookupToolsUseTypedQueries(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	status := 10
	pending := true
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120, Reference: "checkout-42", Supplier: 30}},
		lines:  []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Quantity: 2}},
	}
	deps := purchasingDeps(fake)

	_, orders, err := searchPurchaseOrders(deps)(ctx, &mcp.CallToolRequest{}, PurchaseOrderSearchInput{Search: "checkout", SupplierID: 30, Reference: "checkout-42", Status: &status, TargetDateAfter: "2026-08-01", Limit: 7, Offset: 2})
	r.NoError(err)
	a.Equal(StatusOK, orders.Status)
	a.Equal(inventree.PurchaseOrderQuery{Search: "checkout", Supplier: 30, Reference: "checkout-42", Status: &status, TargetDateAfter: "2026-08-01", Limit: 7, Offset: 2}, fake.lastOrderQuery)

	_, lines, err := searchPurchaseOrderLines(deps)(ctx, &mcp.CallToolRequest{}, PurchaseOrderLineSearchInput{Search: "SKU", OrderID: 120, SupplierPartID: 40, Pending: &pending, Limit: 8})
	r.NoError(err)
	a.Equal(StatusOK, lines.Status)
	a.Equal(inventree.PurchaseOrderLineQuery{Search: "SKU", Order: 120, SupplierPart: 40, Pending: &pending, Limit: 8}, fake.lastLineQuery)
}

func TestCreatePurchaseOrderWithLinesDryRunPreflightsWithoutWrites(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	price := 1.25
	fake := &fakePurchasingClient{
		company:       inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"}},
	}

	_, output, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, PurchaseOrderWorkflowInput{
		DryRun:            true,
		SupplierID:        30,
		SupplierReference: "EBAY-42",
		Lines:             []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 2, UnitPrice: &price, Currency: "AUD"}},
	})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.DryRun)
	a.Equal("EBAY-42", output.SupplierReference)
	r.Len(output.Actions, 2)
	a.Equal("planned", output.Actions[0].Status)
	a.Equal("EBAY-42-1", output.Actions[1].Reference)
	a.Equal([]PlannedChange{
		{Action: "create_purchase_order", RecordType: "purchase_order", Fields: map[string]any{"supplier": 30, "supplier_reference": "EBAY-42"}},
		{Action: "create_purchase_order_line", RecordType: "purchase_order_line", Fields: map[string]any{
			"part": 40, "reference": "EBAY-42-1", "notes": "", "quantity": float64(2), "purchase_price": "1.25", "purchase_price_currency": "AUD", "auto_pricing": false, "merge_items": false,
		}, DependsOn: []PlannedChangeDependency{{Field: "order", Action: "create_purchase_order"}}},
	}, output.PlannedChanges)
	a.Zero(fake.createOrderCalls)
	a.Zero(fake.createLineCalls)
}

func TestCreatePurchaseOrderWithLinesDryRunExposesEffectiveUpdates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	price := 2.5
	description := "updated order"
	fake := &fakePurchasingClient{
		company:       inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"}},
		orders:        []inventree.PurchaseOrder{{PK: 120, Reference: "PO-120", Supplier: 30, SupplierReference: "EBAY-42"}},
		lines:         []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Reference: "EBAY-42-1", Quantity: 1}},
	}

	_, output, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, PurchaseOrderWorkflowInput{
		DryRun: true, SupplierID: 30, SupplierReference: "EBAY-42", Description: &description,
		Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 3, UnitPrice: &price, Currency: "AUD", Notes: "updated line"}},
	})

	r.NoError(err)
	a.Equal([]PlannedChange{
		{Action: "update_purchase_order", RecordType: "purchase_order", ID: 120, Fields: map[string]any{"description": description}},
		{Action: "update_purchase_order_line", RecordType: "purchase_order_line", ID: 130, Fields: map[string]any{
			"order": 120, "part": 40, "reference": "EBAY-42-1", "notes": "updated line", "quantity": float64(3), "purchase_price": "2.5", "purchase_price_currency": "AUD",
		}},
	}, output.PlannedChanges)
	a.Zero(fake.updateOrderCalls)
	a.Zero(fake.updateLineCalls)
}

func TestReceivePurchaseOrderDryRunResolvesLocationsWithoutWriting(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	lineDestination := 41
	itemDestination := 42
	globalDestination := 43
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120, Reference: "PO-1", Supplier: 30, Status: inventree.PurchaseOrderStatusPlaced}},
		lines: []inventree.PurchaseOrderLineItem{
			{PK: 130, Order: 120, Part: 40, Quantity: 5, Received: 1, Destination: &lineDestination},
			{PK: 131, Order: 120, Part: 41, Quantity: 4, Received: 0},
			{PK: 132, Order: 120, Part: 42, Quantity: 3, Received: 0},
		},
	}

	_, output, err := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, ReceivePurchaseOrderInput{
		DryRun: true, OrderID: 120, LocationID: &globalDestination,
		Items: []ReceivePurchaseOrderItem{
			{LineItemID: 130, Quantity: 2},
			{LineItemID: 131, Quantity: 1, LocationID: &itemDestination},
			{LineItemID: 132, Quantity: 1},
		},
	})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.DryRun)
	r.Len(output.Plan, 3)
	a.NotEmpty(output.PlanHash)
	a.Equal(lineDestination, output.Plan[0].LocationID)
	a.Equal(4.0, output.Plan[0].OutstandingBefore)
	a.Equal(2.0, output.Plan[0].OutstandingAfter)
	a.Equal(itemDestination, output.Plan[1].LocationID)
	a.Equal(globalDestination, output.Plan[2].LocationID)
	a.Zero(fake.receiveCalls)
}

func TestReceivePurchaseOrderRequiresConfirmationThenCreatesStock(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	status := 10
	batch := "B-1"
	fake := &fakePurchasingClient{
		orders:     []inventree.PurchaseOrder{{PK: 120, Reference: "PO-1", Supplier: 30, Status: inventree.PurchaseOrderStatusPlaced}},
		lines:      []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Quantity: 3, Destination: &locationID}},
		stockItems: []inventree.StockItem{{PK: 50, Part: 10, Location: &locationID, Quantity: 1, Status: status, Batch: &batch}},
	}
	input := ReceivePurchaseOrderInput{OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1, Status: &status, BatchCode: &batch}}}

	_, unconfirmed, err := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, unconfirmed.Status)
	r.NotNil(unconfirmed.Clarification)
	a.Equal("dry_run", unconfirmed.Clarification.Retry)
	a.Zero(fake.receiveCalls)

	input.DryRun = true
	_, plan, err := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	r.NotEmpty(plan.PlanHash)
	input.DryRun = false
	input.ConfirmReceive = true
	input.PlanHash = plan.PlanHash
	_, confirmed, err := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, confirmed.Status)
	r.Len(confirmed.StockItems, 1)
	a.Equal(50, confirmed.StockItems[0].PK)
	a.Equal(1, fake.receiveCalls)
	r.Len(fake.lastReceive.Items, 1)
	a.Equal(130, fake.lastReceive.Items[0].LineItem)
	a.Equal("1", fake.lastReceive.Items[0].Quantity)
	r.NotNil(fake.lastReceive.Items[0].Location)
	a.Equal(locationID, *fake.lastReceive.Items[0].Location)
}

func TestReceivePurchaseOrderCanExplicitlyCompleteFinalReceipt(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}},
		lines: []inventree.PurchaseOrderLineItem{
			{PK: 130, Order: 120, Part: 40, Quantity: 2, Received: 1, Destination: &locationID},
			{PK: 131, Order: 120, Part: 41, Quantity: 3, Received: 2, Destination: &locationID},
		},
		stockItems: []inventree.StockItem{{PK: 50, Part: 10, Quantity: 1}, {PK: 51, Part: 11, Quantity: 1}},
	}
	input := ReceivePurchaseOrderInput{DryRun: true, CompleteOrder: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}, {LineItemID: 131, Quantity: 1}}}
	handler := receivePurchaseOrderItems(purchasingDeps(fake))

	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.True(plan.CompleteOrder)
	r.Len(plan.CompletionLines, 2)
	r.NotEmpty(plan.PlanHash)
	input.DryRun = false
	input.ConfirmReceive = true
	input.PlanHash = plan.PlanHash
	_, completed, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, completed.Status)
	r.NotNil(completed.Order)
	a.Equal(inventree.PurchaseOrderStatusComplete, completed.Order.Status)
	a.Equal(CompletePurchaseOrderToolName, completed.CompletionAction)
	a.False(completed.CompletionRecovered)
	a.Equal(1, fake.receiveCalls)
	a.Equal(1, fake.completeCalls)
}

func TestReceivePurchaseOrderCompletionPlanRequiresAllOutstandingLinesAndBindsIntent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}},
		lines: []inventree.PurchaseOrderLineItem{
			{PK: 130, Order: 120, Part: 40, Quantity: 1, Destination: &locationID},
			{PK: 131, Order: 120, Part: 41, Quantity: 1, Destination: &locationID},
		},
	}
	handler := receivePurchaseOrderItems(purchasingDeps(fake))
	base := ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}}}
	_, ordinary, err := handler(ctx, &mcp.CallToolRequest{}, base)
	r.NoError(err)
	base.CompleteOrder = true
	_, incomplete, err := handler(ctx, &mcp.CallToolRequest{}, base)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, incomplete.Status)
	a.Contains(incomplete.Clarification.Reason, "every ordinary line")
	a.NotEqual(ordinary.PlanHash, incomplete.PlanHash)
	a.Zero(fake.receiveCalls)

	base.Items = append(base.Items, ReceivePurchaseOrderItem{LineItemID: 131, Quantity: 1})
	_, completionPlan, err := handler(ctx, &mcp.CallToolRequest{}, base)
	r.NoError(err)
	r.NotEmpty(completionPlan.PlanHash)
	base.DryRun = false
	base.ConfirmReceive = true
	base.CompleteOrder = false
	base.PlanHash = completionPlan.PlanHash
	_, stale, err := handler(ctx, &mcp.CallToolRequest{}, base)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Zero(fake.receiveCalls)
}

func TestReceivePurchaseOrderExplicitCompletionHonorsAutoCompleteAndPreservesReceiptOnFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		autoComplete     bool
		completionErr    error
		afterPersistErr  error
		wantPartial      bool
		wantRecovered    bool
		wantCompleteCall int
	}{
		{name: "upstream already auto completed", autoComplete: true},
		{name: "completion failure preserves receipt", completionErr: errors.New("timeout"), wantPartial: true, wantCompleteCall: 1},
		{name: "lost completion response recovered", afterPersistErr: errors.New("response lost"), wantRecovered: true, wantCompleteCall: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			locationID := 40
			fake := &fakePurchasingClient{
				orders:     []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}},
				lines:      []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Quantity: 1, Destination: &locationID}},
				stockItems: []inventree.StockItem{{PK: 50, Part: 10, Quantity: 1}}, autoCompleteOnReceive: test.autoComplete,
				completeErr: test.completionErr, completeErrAfterPersist: test.afterPersistErr,
			}
			input := ReceivePurchaseOrderInput{DryRun: true, CompleteOrder: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}}}
			handler := receivePurchaseOrderItems(purchasingDeps(fake))
			_, plan, err := handler(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			input.DryRun = false
			input.ConfirmReceive = true
			input.PlanHash = plan.PlanHash
			_, output, err := handler(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			r.Len(output.StockItems, 1)
			if test.wantPartial {
				a.Equal(StatusPartialFailure, output.Status)
				r.NotNil(output.Failure)
				a.Equal(CompletePurchaseOrderToolName, output.Failure.Action)
				a.Contains(output.Failure.RecoveryPlan, "Do not repeat the receipt")
			} else {
				a.Equal(StatusOK, output.Status)
			}
			a.Equal(test.wantRecovered, output.CompletionRecovered)
			a.Equal(1, fake.receiveCalls)
			a.Equal(test.wantCompleteCall, fake.completeCalls)
		})
	}
}

func TestReceivePurchaseOrderExplicitCompletionRefreshFailureRequiresCompletionOnlyRecovery(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakePurchasingClient{
		orders:                  []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}},
		lines:                   []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Quantity: 1, Destination: &locationID}},
		stockItems:              []inventree.StockItem{{PK: 50, Part: 10, Quantity: 1}},
		getOrderErrAfterReceive: errors.New("refresh unavailable"),
	}
	input := ReceivePurchaseOrderInput{DryRun: true, CompleteOrder: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}}}
	handler := receivePurchaseOrderItems(purchasingDeps(fake))
	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.ConfirmReceive = true
	input.PlanHash = plan.PlanHash
	_, output, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.Len(output.StockItems, 1)
	r.NotNil(output.Failure)
	a.Equal(CompletePurchaseOrderToolName, output.Failure.Action)
	a.Contains(output.Failure.RecoveryPlan, "Do not repeat the receipt")
	a.NotContains(output.Failure.RecoveryPlan, "preparing a new dry-run plan")
	a.Equal(1, fake.receiveCalls)
	a.Zero(fake.completeCalls)
}

func TestReceivePurchaseOrderRejectsStalePlanAndSchemaInvalidQuantity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}},
		lines:  []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Quantity: 3, Destination: &locationID}},
	}

	input := ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}}}
	handler := receivePurchaseOrderItems(purchasingDeps(fake))
	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	r.NotEmpty(plan.PlanHash)
	fake.lines[0].Received = 1
	input.DryRun = false
	input.ConfirmReceive = true
	input.PlanHash = plan.PlanHash
	_, stale, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Equal("plan_hash", stale.Clarification.Retry)
	a.Zero(fake.receiveCalls)

	for _, quantity := range []float64{0, -1, 0.000001, 10000000000, math.NaN(), math.Inf(1)} {
		_, invalid, callErr := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, ReceivePurchaseOrderInput{
			DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: quantity}},
		})
		r.NoError(callErr)
		a.Equal(StatusClarificationRequired, invalid.Status)
	}
	a.Zero(fake.receiveCalls)
}

func TestReceivePurchaseOrderPlanBindsSupplierPackConversionAndPackaging(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	packaging := "reel"
	price := inventree.DecimalString("4.00")
	fake := &fakePurchasingClient{
		orders:       []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}},
		lines:        []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Quantity: 3, Destination: &locationID, PurchasePrice: &price, PurchasePriceCurrency: "AUD"}},
		supplierPart: inventree.SupplierPart{PK: 40, Part: 10, Packaging: &packaging, PackQuantityNative: 2},
		stockItems:   []inventree.StockItem{{PK: 50, Part: 10, Quantity: 2}},
	}
	input := ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1, Packaging: dvgoutils.Ptr("")}}}
	handler := receivePurchaseOrderItems(purchasingDeps(fake))
	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	r.Len(plan.Plan, 1)
	a.Equal(2.0, plan.Plan[0].SupplierPackQuantity)
	a.Equal(2.0, plan.Plan[0].BaseStockQuantity)
	r.NotNil(plan.Plan[0].Packaging)
	a.Equal(packaging, *plan.Plan[0].Packaging)
	r.NotNil(plan.Plan[0].SourcePurchasePrice)
	a.Equal(price, *plan.Plan[0].SourcePurchasePrice)
	a.Equal("AUD", plan.Plan[0].SourcePriceCurrency)

	fake.supplierPart.PackQuantityNative = 3
	input.DryRun = false
	input.ConfirmReceive = true
	input.PlanHash = plan.PlanHash
	_, stale, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Zero(fake.receiveCalls)

	fake.supplierPart.PackQuantityNative = 2
	input.DryRun = true
	input.ConfirmReceive = false
	input.PlanHash = ""
	_, pricePlan, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	changedPrice := inventree.DecimalString("5.00")
	fake.lines[0].PurchasePrice = &changedPrice
	input.DryRun = false
	input.ConfirmReceive = true
	input.PlanHash = pricePlan.PlanHash
	_, stalePrice, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stalePrice.Status)
	a.Zero(fake.receiveCalls)

	fake.lines[0].PurchasePrice = &price
	input.DryRun = true
	input.ConfirmReceive = false
	input.PlanHash = ""
	_, currentPlan, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.ConfirmReceive = true
	input.PlanHash = currentPlan.PlanHash
	_, confirmed, err := handler(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, confirmed.Status)
	r.NotNil(fake.lastReceive.Items[0].Packaging)
	a.Equal(packaging, *fake.lastReceive.Items[0].Packaging)
}

func TestReceivePurchaseOrderRejectsVirtualPart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakePurchasingClient{
		orders:       []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}},
		lines:        []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Quantity: 1, Destination: &locationID}},
		supplierPart: inventree.SupplierPart{PK: 40, Part: 10},
		part:         inventree.Part{PK: 10, Virtual: true},
	}

	_, output, err := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, ReceivePurchaseOrderInput{
		DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}},
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Contains(output.Clarification.Reason, "virtual parts")
	a.Zero(fake.receiveCalls)
}

func TestReceivePurchaseOrderReturnsUnknownResultWithoutBlindRetry(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakePurchasingClient{
		orders:     []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}},
		lines:      []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Quantity: 1, Destination: &locationID}},
		receiveErr: errors.New("ambiguous timeout"),
	}
	input := ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}}}
	_, plan, err := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.ConfirmReceive = true
	input.PlanHash = plan.PlanHash

	_, output, err := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.Failure)
	a.Contains(output.Failure.RecoveryPlan, "Do not retry")
	a.Equal(1, fake.receiveCalls)
}

func TestReceivePurchaseOrderDistinguishesRejectedAndAmbiguousResults(t *testing.T) {
	t.Parallel()
	locationID := 40
	tests := []struct {
		name              string
		stockItems        []inventree.StockItem
		receiveErr        error
		refreshErr        error
		wantError         bool
		wantPartial       bool
		wantReturnedStock bool
	}{
		{name: "definite validation rejection", receiveErr: &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation, Detail: "invalid serial"}, wantError: true},
		{name: "successful response without stock", wantPartial: true},
		{name: "refresh failure after returned stock", stockItems: []inventree.StockItem{{PK: 50, Part: 10, Quantity: 1}}, refreshErr: errors.New("refresh unavailable"), wantPartial: true, wantReturnedStock: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakePurchasingClient{
				orders:     []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}},
				lines:      []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Quantity: 1, Destination: &locationID}},
				stockItems: test.stockItems, receiveErr: test.receiveErr, getOrderErrAfterReceive: test.refreshErr,
			}
			input := ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}}}
			handler := receivePurchaseOrderItems(purchasingDeps(fake))
			_, plan, err := handler(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			input.DryRun = false
			input.ConfirmReceive = true
			input.PlanHash = plan.PlanHash
			_, output, err := handler(ctx, &mcp.CallToolRequest{}, input)
			if test.wantError {
				r.Error(err)
				a.Nil(output.Failure)
			} else {
				r.NoError(err)
			}
			if test.wantPartial {
				a.Equal(StatusPartialFailure, output.Status)
				r.NotNil(output.Failure)
				a.Contains(output.Failure.RecoveryPlan, "purchase_order_id")
			}
			if test.wantReturnedStock {
				r.Len(output.StockItems, 1)
			}
			a.Equal(1, fake.receiveCalls)
		})
	}
}

func TestReceivePurchaseOrderRejectsOverReceiptAndWrongOrder(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120, Reference: "PO-1", Supplier: 30, Status: inventree.PurchaseOrderStatusPlaced}},
		lines:  []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Quantity: 3, Received: 2, Destination: &locationID}, {PK: 131, Order: 121, Quantity: 3, Destination: &locationID}},
	}

	_, over, err := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 2}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, over.Status)
	a.Contains(over.Clarification.Reason, "outstanding")

	_, wrongOrder, err := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 131, Quantity: 1}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, wrongOrder.Status)
	a.Contains(wrongOrder.Clarification.Reason, "different purchase order")
	a.Zero(fake.receiveCalls)
}

func TestReceivePurchaseOrderPreflightGuardsPreventWrites(t *testing.T) {
	t.Parallel()
	locationID := 40
	barcode := " SAME "
	tests := []struct {
		name        string
		orderStatus int
		line        inventree.PurchaseOrderLineItem
		input       ReceivePurchaseOrderInput
		locationErr error
	}{
		{name: "order not placed", orderStatus: inventree.PurchaseOrderStatusPending, line: inventree.PurchaseOrderLineItem{PK: 130, Order: 120, Part: 40, Quantity: 2, Destination: &locationID}, input: ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}}}},
		{name: "fully received", orderStatus: inventree.PurchaseOrderStatusPlaced, line: inventree.PurchaseOrderLineItem{PK: 130, Order: 120, Part: 40, Quantity: 2, Received: 2, Destination: &locationID}, input: ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}}}},
		{name: "duplicate line", orderStatus: inventree.PurchaseOrderStatusPlaced, line: inventree.PurchaseOrderLineItem{PK: 130, Order: 120, Part: 40, Quantity: 2, Destination: &locationID}, input: ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}, {LineItemID: 130, Quantity: 1}}}},
		{name: "missing location", orderStatus: inventree.PurchaseOrderStatusPlaced, line: inventree.PurchaseOrderLineItem{PK: 130, Order: 120, Part: 40, Quantity: 2}, input: ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}}}},
		{name: "invalid location", orderStatus: inventree.PurchaseOrderStatusPlaced, line: inventree.PurchaseOrderLineItem{PK: 130, Order: 120, Part: 40, Quantity: 2, Destination: &locationID}, input: ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1}}}, locationErr: errors.New("location not found")},
		{name: "negative status", orderStatus: inventree.PurchaseOrderStatusPlaced, line: inventree.PurchaseOrderLineItem{PK: 130, Order: 120, Part: 40, Quantity: 2, Destination: &locationID}, input: ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1, Status: dvgoutils.Ptr(-1)}}}},
		{name: "invalid expiry", orderStatus: inventree.PurchaseOrderStatusPlaced, line: inventree.PurchaseOrderLineItem{PK: 130, Order: 120, Part: 40, Quantity: 2, Destination: &locationID}, input: ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 1, ExpiryDate: dvgoutils.Ptr("01-08-2026")}}}},
		{name: "duplicate trimmed barcode", orderStatus: inventree.PurchaseOrderStatusPlaced, line: inventree.PurchaseOrderLineItem{PK: 130, Order: 120, Part: 40, Quantity: 2, Destination: &locationID}, input: ReceivePurchaseOrderInput{DryRun: true, OrderID: 120, Items: []ReceivePurchaseOrderItem{{LineItemID: 130, Quantity: 0.5, Barcode: &barcode}, {LineItemID: 131, Quantity: 0.5, Barcode: dvgoutils.Ptr("SAME")}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			lines := []inventree.PurchaseOrderLineItem{test.line}
			if test.name == "duplicate trimmed barcode" {
				lines = append(lines, inventree.PurchaseOrderLineItem{PK: 131, Order: 120, Part: 41, Quantity: 2, Destination: &locationID})
			}
			fake := &fakePurchasingClient{
				orders:      []inventree.PurchaseOrder{{PK: 120, Status: test.orderStatus}},
				lines:       lines,
				locationErr: test.locationErr,
			}
			_, output, err := receivePurchaseOrderItems(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, test.input)
			if test.locationErr != nil {
				r.Error(err)
			} else {
				r.NoError(err)
				a.Equal(StatusClarificationRequired, output.Status)
			}
			a.Zero(fake.receiveCalls)
		})
	}
}

func TestReceiveQuantityStringSchemaBounds(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	for _, test := range []struct {
		quantity float64
		want     string
		valid    bool
	}{
		{quantity: 0.00001, want: "0.00001", valid: true},
		{quantity: 9999999999, want: "9999999999", valid: true},
		{quantity: 1.23456, want: "1.23456", valid: true},
		{quantity: 0.000001, valid: false},
		{quantity: 1.234567, valid: false},
		{quantity: 10000000000, valid: false},
	} {
		actual, valid := receiveQuantityString(test.quantity)
		r.Equal(test.valid, valid)
		r.Equal(test.want, actual)
	}
}

func TestIssuePurchaseOrderRequiresConfirmationAndPlacesPendingOrder(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		orders:     []inventree.PurchaseOrder{{PK: 120, Reference: "PO-1", Supplier: 30, Status: inventree.PurchaseOrderStatusPending}},
		lines:      []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Reference: "LINE-1", Quantity: 2}},
		extraLines: []inventree.PurchaseOrderExtraLine{{PK: 140, Order: 120, Reference: "FREIGHT", Link: "https://supplier.test/freight?token=secret#details", Quantity: 1, Price: dvgoutils.Ptr(inventree.DecimalString("-2.50")), PriceCurrency: "AUD"}},
	}

	_, dryRun, err := issuePurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{DryRun: true, OrderID: 120})
	r.NoError(err)
	a.Equal(StatusOK, dryRun.Status)
	a.Equal("issue_purchase_order", dryRun.Action)
	a.NotEmpty(dryRun.PlanHash)
	a.Equal(fake.lines, dryRun.Lines)
	r.Len(dryRun.ExtraLines, 1)
	a.Equal("https://supplier.test/freight", dryRun.ExtraLines[0].Link)
	a.Equal([]PlannedChange{{
		Action: "issue_purchase_order", RecordType: "purchase_order", ID: 120,
		Fields: map[string]any{"status": inventree.PurchaseOrderStatusPlaced},
	}}, dryRun.PlannedChanges)
	a.Zero(fake.issueCalls)

	_, unconfirmed, err := issuePurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{OrderID: 120})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, unconfirmed.Status)
	r.NotNil(unconfirmed.Clarification)
	a.Equal("dry_run", unconfirmed.Clarification.Retry)
	a.Empty(unconfirmed.PlanHash)
	a.NotContains(unconfirmed.Clarification.RetryValues, "plan_hash")
	a.Zero(fake.issueCalls)

	_, issued, err := issuePurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{OrderID: 120, ConfirmIssue: true, PlanHash: dryRun.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, issued.Status)
	r.NotNil(issued.Order)
	a.Equal(inventree.PurchaseOrderStatusPlaced, issued.Order.Status)
	a.Equal(1, fake.issueCalls)
}

func TestIssuePurchaseOrderRejectsStaleLinePlan(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120, Reference: "PO-1", Supplier: 30, Status: inventree.PurchaseOrderStatusPending}},
		lines:  []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Reference: "LINE-1", Quantity: 2}},
	}
	handler := issuePurchaseOrder(purchasingDeps(fake))

	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{DryRun: true, OrderID: 120})
	r.NoError(err)
	r.NotEmpty(plan.PlanHash)
	fake.lines[0].Quantity = 3

	_, stale, err := handler(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{OrderID: 120, ConfirmIssue: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	r.NotNil(stale.Clarification)
	a.Equal("dry_run", stale.Clarification.Retry)
	a.Empty(stale.PlanHash)
	a.NotContains(stale.Clarification.RetryValues, "plan_hash")
	a.Zero(fake.issueCalls)
}

func TestIssuePurchaseOrderRejectsStaleExtraLinePlan(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		orders:     []inventree.PurchaseOrder{{PK: 120, Reference: "PO-1", Supplier: 30, Status: inventree.PurchaseOrderStatusPending}},
		lines:      []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Reference: "LINE-1", Quantity: 2}},
		extraLines: []inventree.PurchaseOrderExtraLine{{PK: 140, Order: 120, Reference: "FREIGHT", Link: "https://supplier.test/freight-one?token=secret", Quantity: 1, Price: dvgoutils.Ptr(inventree.DecimalString("12.50")), PriceCurrency: "AUD"}},
	}
	handler := issuePurchaseOrder(purchasingDeps(fake))

	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{DryRun: true, OrderID: 120})
	r.NoError(err)
	r.NotEmpty(plan.PlanHash)
	fake.extraLines[0].Link = "https://supplier.test/freight-one?token=changed"
	_, queryOnlyPlan, err := handler(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{DryRun: true, OrderID: 120})
	r.NoError(err)
	a.Equal(plan.PlanHash, queryOnlyPlan.PlanHash, "hidden URL metadata must not create a public hash oracle")
	fake.extraLines[0].Link = "https://supplier.test/freight-two?token=secret"

	_, stale, err := handler(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{OrderID: 120, ConfirmIssue: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	r.NotNil(stale.Clarification)
	a.Equal("dry_run", stale.Clarification.Retry)
	a.Zero(fake.issueCalls)
}

func TestIssuePurchaseOrderGuardsNonPendingOrders(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}}}

	_, placed, err := issuePurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{OrderID: 120, ConfirmIssue: true})
	r.NoError(err)
	a.Equal(StatusOK, placed.Status)
	a.Equal("already_placed", placed.Action)
	a.Zero(fake.issueCalls)

	fake.orders[0].Status = inventree.PurchaseOrderStatusComplete
	_, complete, err := issuePurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{OrderID: 120, ConfirmIssue: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, complete.Status)
	a.Zero(fake.issueCalls)
}

func TestCreatePurchaseOrderWithLinesCreatesAndIdempotentlyUpdates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	price := 2.5
	fake := &fakePurchasingClient{
		company:       inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"}},
	}
	input := PurchaseOrderWorkflowInput{SupplierReference: "checkout-42", SupplierID: 30, Description: dvgoutils.Ptr("first"), Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 2, UnitPrice: &price, Currency: "AUD", Notes: "line"}}}

	_, created, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, created.Status)
	r.NotNil(created.PurchaseOrder)
	a.Equal(120, created.PurchaseOrder.PK)
	r.Len(created.Lines, 1)
	a.Equal(130, created.Lines[0].PK)
	a.Equal("checkout-42-1", created.Lines[0].Reference)
	a.Equal(1, fake.createOrderCalls)
	a.Equal(1, fake.createLineCalls)

	input.Description = dvgoutils.Ptr("retry")
	input.Lines[0].Quantity = 3
	_, retried, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, retried.Status)
	a.Equal(1, fake.createOrderCalls)
	a.Equal(1, fake.createLineCalls)
	a.Equal(1, fake.updateOrderCalls)
	a.Equal(1, fake.updateLineCalls)
	a.Equal(3.0, fake.lines[0].Quantity)
}

func TestCreatePurchaseOrderWithLinesReturnsRecoverablePartialFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		company:       inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"}},
		createLineErr: errors.New("remote line failure"), failCreateLineAt: 2,
	}

	input := PurchaseOrderWorkflowInput{SupplierReference: "checkout-42", SupplierID: 30, Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 2}, {SupplierPartID: 40, Quantity: 3}}}
	_, output, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.PurchaseOrder)
	a.Equal(120, output.PurchaseOrder.PK)
	r.Len(output.Lines, 1)
	a.Equal("checkout-42-1", output.Lines[0].Reference)
	r.NotNil(output.Failure)
	a.Equal("create_purchase_order_line", output.Failure.Action)
	a.Contains(output.Failure.RecoveryPlan, "search_purchase_order_lines")
	a.NotContains(output.Failure.Message, "remote line failure")
	r.Len(output.Actions, 3)
	a.Equal("created", output.Actions[1].Status)
	a.Equal("failed", output.Actions[2].Status)

	fake.failCreateLineAt = 0
	fake.createLineErr = nil
	_, retried, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, retried.Status)
	r.Len(retried.Lines, 2)
	a.Equal(output.Lines[0].PK, retried.Lines[0].PK)
	a.Equal("checkout-42-2", retried.Lines[1].Reference)
	a.Equal(1, fake.updateLineCalls)
	a.Equal(3, fake.createLineCalls)
}

func TestCreatePurchaseOrderWithLinesRecoversUnknownCreateResult(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		company:                    inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		supplierParts:              []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"}},
		createOrderErrAfterPersist: errors.New("connection dropped after create"),
	}
	input := PurchaseOrderWorkflowInput{SupplierReference: "checkout-42", SupplierID: 30, Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 2}}}

	_, interrupted, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, interrupted.Status)
	a.Equal("create_purchase_order", interrupted.Failure.Action)
	a.Contains(interrupted.Failure.RecoveryPlan, "same supplier_id and supplier_reference")
	a.Equal(1, fake.createOrderCalls)

	_, recovered, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, recovered.Status)
	r.NotNil(recovered.PurchaseOrder)
	a.Equal(120, recovered.PurchaseOrder.PK)
	a.Equal(1, fake.createOrderCalls, "retry must reuse the persisted order")
}

func TestCreatePurchaseOrderWithLinesPreflightsAllConflictsBeforePatching(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		company: inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		supplierParts: []inventree.SupplierPart{
			{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"},
			{PK: 41, Part: 11, Supplier: 30, SKU: "SKU-2"},
		},
		orders: []inventree.PurchaseOrder{{PK: 120, Reference: "PO-0001", Supplier: 30, SupplierReference: "checkout-42"}},
		lines: []inventree.PurchaseOrderLineItem{
			{PK: 130, Order: 120, Part: 40, Reference: "checkout-42-1", Quantity: 1},
			{PK: 131, Order: 120, Part: 99, Reference: "checkout-42-2", Quantity: 1},
		},
	}
	input := PurchaseOrderWorkflowInput{SupplierReference: "checkout-42", SupplierID: 30, Description: dvgoutils.Ptr("must not be written"), Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 2}, {SupplierPartID: 41, Quantity: 3}}}

	_, output, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Zero(fake.updateOrderCalls)
	a.Zero(fake.updateLineCalls)
	a.Zero(fake.createLineCalls)
}

func TestCreatePurchaseOrderWithLinesExplainsDuplicateLineRepair(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		company:       inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"}},
		orders:        []inventree.PurchaseOrder{{PK: 120, Reference: "PO-0001", Supplier: 30, SupplierReference: "checkout-42"}},
		lines: []inventree.PurchaseOrderLineItem{
			{PK: 130, Order: 120, Part: 40, Reference: "checkout-42-1", Quantity: 1},
			{PK: 131, Order: 120, Part: 40, Reference: "checkout-42-1", Quantity: 1},
		},
	}

	_, output, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, PurchaseOrderWorkflowInput{SupplierReference: "checkout-42", SupplierID: 30, Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 2}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Contains(output.Clarification.Reason, "update_purchase_order_line")
	a.Equal("id", output.Clarification.Retry)
	a.True(output.Clarification.HardError)
	a.Zero(fake.updateOrderCalls)
	a.Zero(fake.updateLineCalls)
}

func TestCreatePurchaseOrderWithLinesRequiresUniqueExactSupplierReference(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		company:       inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"}},
		orders: []inventree.PurchaseOrder{
			{PK: 120, Reference: "PO-0001", Supplier: 30, SupplierReference: "checkout-42"},
			{PK: 121, Reference: "PO-0002", Supplier: 30, SupplierReference: "checkout-42-extra"},
		},
	}
	input := PurchaseOrderWorkflowInput{SupplierReference: "checkout-42", SupplierID: 30, Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 2}}}

	_, reused, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, reused.Status)
	r.NotNil(reused.PurchaseOrder)
	a.Equal(120, reused.PurchaseOrder.PK, "generic search results must be filtered to an exact supplier_reference match")
	a.Zero(fake.createOrderCalls)

	fake.orders = append(fake.orders, inventree.PurchaseOrder{PK: 122, Reference: "PO-0003", Supplier: 30, SupplierReference: "checkout-42"})
	_, ambiguous, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, ambiguous.Status)
	r.NotNil(ambiguous.Clarification)
	a.Contains(ambiguous.Clarification.Reason, "multiple orders")
	a.Zero(fake.createOrderCalls)

	input.PurchaseOrderID = 122
	_, selected, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, selected.Status)
	r.NotNil(selected.PurchaseOrder)
	a.Equal(122, selected.PurchaseOrder.PK)
	a.Zero(fake.createOrderCalls)
}

func TestPurchaseOrderWritesRejectSupplierMismatchAndInvalidInputs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		company:      inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		orders:       []inventree.PurchaseOrder{{PK: 120, Reference: "PO-1", Supplier: 30}},
		supplierPart: inventree.SupplierPart{PK: 40, Part: 10, Supplier: 31},
	}
	deps := purchasingDeps(fake)

	_, orderOutput, err := createPurchaseOrder(deps)(ctx, &mcp.CallToolRequest{}, CreatePurchaseOrderInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, orderOutput.Status)

	_, orderWithoutSupplierReference, err := createPurchaseOrder(deps)(ctx, &mcp.CallToolRequest{}, CreatePurchaseOrderInput{SupplierID: 30})
	r.NoError(err)
	a.Equal(StatusOK, orderWithoutSupplierReference.Status)
	a.Equal("", orderWithoutSupplierReference.Record.SupplierReference)

	_, lineOutput, err := addPurchaseOrderLine(deps)(ctx, &mcp.CallToolRequest{}, AddPurchaseOrderLineInput{OrderID: 120, SupplierPartID: 40, Quantity: 1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, lineOutput.Status)
	a.Contains(lineOutput.Clarification.Reason, "does not belong")
}

func purchasingDeps(fake *fakePurchasingClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

type fakePurchasingClient struct {
	company                              inventree.Company
	part                                 inventree.Part
	orders                               []inventree.PurchaseOrder
	lines                                []inventree.PurchaseOrderLineItem
	extraLines                           []inventree.PurchaseOrderExtraLine
	supplierParts                        []inventree.SupplierPart
	supplierPart                         inventree.SupplierPart
	lastOrderQuery                       inventree.PurchaseOrderQuery
	lastLineQuery                        inventree.PurchaseOrderLineQuery
	createOrderCalls                     int
	createOrderErrAfterPersist           error
	updateOrderCalls                     int
	createLineCalls                      int
	updateLineCalls                      int
	createExtraLineCalls                 int
	updateExtraLineCalls                 int
	createExtraLineErr                   error
	createExtraLineErrAfterPersist       error
	createExtraLineDuplicateAfterPersist bool
	failCreateExtraLineAt                int
	updateExtraLineErrAfterPersist       error
	extraLinePageSize                    int
	extraLineCountOverride               int
	extraLineSearchErr                   error
	deleteExtraLineKeep                  bool
	getOrderCalls                        int
	createLineErr                        error
	failCreateLineAt                     int
	receiveCalls                         int
	receiveErr                           error
	autoCompleteOnReceive                bool
	issueCalls                           int
	completeCalls                        int
	completeErr                          error
	completeErrAfterPersist              error
	completeKeepPlaced                   bool
	lastReceive                          inventree.PurchaseOrderReceive
	stockItems                           []inventree.StockItem
	locationErr                          error
	getOrderErrAfterReceive              error
	getOrderErrAfterComplete             error
	missingOrderIDs                      map[int]bool
}

func (f *fakePurchasingClient) IssuePurchaseOrder(_ context.Context, id int) error {
	f.issueCalls++
	for index := range f.orders {
		if f.orders[index].PK == id {
			f.orders[index].Status = inventree.PurchaseOrderStatusPlaced
		}
	}
	return nil
}

func (f *fakePurchasingClient) ReceivePurchaseOrder(_ context.Context, id int, input inventree.PurchaseOrderReceive) ([]inventree.StockItem, error) {
	f.receiveCalls++
	f.lastReceive = input
	if f.autoCompleteOnReceive {
		for index := range f.orders {
			if f.orders[index].PK == id {
				f.orders[index].Status = inventree.PurchaseOrderStatusComplete
			}
		}
	}
	return f.stockItems, f.receiveErr
}

func (f *fakePurchasingClient) CompletePurchaseOrder(_ context.Context, id int) error {
	f.completeCalls++
	if f.completeErr != nil {
		return f.completeErr
	}
	if !f.completeKeepPlaced {
		for index := range f.orders {
			if f.orders[index].PK == id {
				f.orders[index].Status = inventree.PurchaseOrderStatusComplete
			}
		}
	}
	return f.completeErrAfterPersist
}

func (f *fakePurchasingClient) GetPart(_ context.Context, id int) (inventree.Part, error) {
	if f.part.PK != 0 {
		return f.part, nil
	}
	return inventree.Part{PK: id}, nil
}

func (f *fakePurchasingClient) GetCompany(_ context.Context, id int) (inventree.Company, error) {
	if f.company.PK == 0 {
		return inventree.Company{PK: id, IsSupplier: true}, nil
	}
	return f.company, nil
}

func (f *fakePurchasingClient) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
	if f.locationErr != nil {
		return inventree.StockLocation{}, f.locationErr
	}
	return inventree.StockLocation{PK: id, Name: "Receiving"}, nil
}

func (f *fakePurchasingClient) SearchPurchaseOrders(_ context.Context, query inventree.PurchaseOrderQuery) ([]inventree.PurchaseOrder, error) {
	f.lastOrderQuery = query
	if query.Reference == "" && query.Search == "" {
		return f.orders, nil
	}
	result := make([]inventree.PurchaseOrder, 0, len(f.orders))
	for _, order := range f.orders {
		referenceMatch := query.Reference == "" || order.Reference == query.Reference
		searchMatch := query.Search == "" || strings.Contains(order.Reference, query.Search) || strings.Contains(order.SupplierReference, query.Search)
		if referenceMatch && searchMatch && (query.Supplier == 0 || order.Supplier == query.Supplier) {
			result = append(result, order)
		}
	}
	return result, nil
}

func (f *fakePurchasingClient) GetPurchaseOrder(_ context.Context, id int) (inventree.PurchaseOrder, error) {
	f.getOrderCalls++
	if f.receiveCalls > 0 && f.getOrderErrAfterReceive != nil {
		return inventree.PurchaseOrder{}, f.getOrderErrAfterReceive
	}
	if f.completeCalls > 0 && f.getOrderErrAfterComplete != nil {
		return inventree.PurchaseOrder{}, f.getOrderErrAfterComplete
	}
	if f.missingOrderIDs[id] {
		return inventree.PurchaseOrder{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	for _, order := range f.orders {
		if order.PK == id {
			return order, nil
		}
	}
	return inventree.PurchaseOrder{PK: id, Supplier: 30}, nil
}

func (f *fakePurchasingClient) CreatePurchaseOrder(_ context.Context, input inventree.PurchaseOrderCreate) (inventree.PurchaseOrder, error) {
	f.createOrderCalls++
	reference := input.Reference
	if reference == "" {
		reference = "PO-0001"
	}
	supplierReference := ""
	if input.SupplierReference != nil {
		supplierReference = *input.SupplierReference
	}
	order := inventree.PurchaseOrder{PK: 120, Reference: reference, Supplier: input.Supplier, SupplierReference: supplierReference}
	f.orders = append(f.orders, order)
	if f.createOrderErrAfterPersist != nil {
		return inventree.PurchaseOrder{}, f.createOrderErrAfterPersist
	}
	return order, nil
}

func (f *fakePurchasingClient) UpdatePurchaseOrder(_ context.Context, id int, _ inventree.PatchFields) (inventree.PurchaseOrder, error) {
	f.updateOrderCalls++
	return f.GetPurchaseOrder(context.Background(), id)
}

func (f *fakePurchasingClient) SearchPurchaseOrderLines(_ context.Context, query inventree.PurchaseOrderLineQuery) ([]inventree.PurchaseOrderLineItem, error) {
	f.lastLineQuery = query
	result := make([]inventree.PurchaseOrderLineItem, 0, len(f.lines))
	for _, line := range f.lines {
		if query.Order == 0 || line.Order == query.Order {
			result = append(result, line)
		}
	}
	return result, nil
}

func (f *fakePurchasingClient) GetPurchaseOrderLine(_ context.Context, id int) (inventree.PurchaseOrderLineItem, error) {
	for _, line := range f.lines {
		if line.PK == id {
			return line, nil
		}
	}
	return inventree.PurchaseOrderLineItem{PK: id, Order: 120, Part: 40}, nil
}

func (f *fakePurchasingClient) CreatePurchaseOrderLine(_ context.Context, input inventree.PurchaseOrderLineCreate) (inventree.PurchaseOrderLineItem, error) {
	f.createLineCalls++
	if f.createLineErr != nil && (f.failCreateLineAt == 0 || f.createLineCalls == f.failCreateLineAt) {
		return inventree.PurchaseOrderLineItem{}, f.createLineErr
	}
	reference := ""
	if input.Reference != nil {
		reference = *input.Reference
	}
	line := inventree.PurchaseOrderLineItem{PK: 129 + f.createLineCalls, Order: input.Order, Part: input.SupplierPart, Reference: reference, Quantity: input.Quantity}
	f.lines = append(f.lines, line)
	return line, nil
}

func (f *fakePurchasingClient) UpdatePurchaseOrderLine(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderLineItem, error) {
	f.updateLineCalls++
	payload, err := json.Marshal(fields)
	if err != nil {
		return inventree.PurchaseOrderLineItem{}, err
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return inventree.PurchaseOrderLineItem{}, err
	}
	for index := range f.lines {
		if f.lines[index].PK != id {
			continue
		}
		if value, ok := decoded["quantity"]; ok {
			f.lines[index].Quantity = value.(float64)
		}
		return f.lines[index], nil
	}
	return inventree.PurchaseOrderLineItem{}, errors.New("line not found")
}

func (f *fakePurchasingClient) SearchPurchaseOrderExtraLinesPage(_ context.Context, query inventree.PurchaseOrderExtraLineQuery) (inventree.Page[inventree.PurchaseOrderExtraLine], error) {
	if f.extraLineSearchErr != nil {
		return inventree.Page[inventree.PurchaseOrderExtraLine]{}, f.extraLineSearchErr
	}
	results := make([]inventree.PurchaseOrderExtraLine, 0, len(f.extraLines))
	for _, line := range f.extraLines {
		if (query.Order == 0 || line.Order == query.Order) && (query.Search == "" || strings.Contains(line.Reference, query.Search) || strings.Contains(line.Description, query.Search)) {
			results = append(results, line)
		}
	}
	start := min(query.Offset, len(results))
	end := len(results)
	limit := query.Limit
	if f.extraLinePageSize > 0 && (limit == 0 || f.extraLinePageSize < limit) {
		limit = f.extraLinePageSize
	}
	if limit > 0 {
		end = min(start+limit, len(results))
	}
	count := len(results)
	if f.extraLineCountOverride > 0 {
		count = f.extraLineCountOverride
	}
	page := inventree.Page[inventree.PurchaseOrderExtraLine]{Count: count, Results: results[start:end]}
	if end < len(results) {
		next := "next"
		page.Next = &next
	}
	return page, nil
}

func (f *fakePurchasingClient) GetPurchaseOrderExtraLine(_ context.Context, id int) (inventree.PurchaseOrderExtraLine, error) {
	for _, line := range f.extraLines {
		if line.PK == id {
			return line, nil
		}
	}
	return inventree.PurchaseOrderExtraLine{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
}

func (f *fakePurchasingClient) CreatePurchaseOrderExtraLine(_ context.Context, input inventree.PurchaseOrderExtraLineCreate) (inventree.PurchaseOrderExtraLine, error) {
	f.createExtraLineCalls++
	if f.createExtraLineErr != nil && (f.failCreateExtraLineAt == 0 || f.createExtraLineCalls == f.failCreateExtraLineAt) {
		return inventree.PurchaseOrderExtraLine{}, f.createExtraLineErr
	}
	line := inventree.PurchaseOrderExtraLine{PK: 200 + f.createExtraLineCalls, Order: input.Order, Reference: input.Reference, Quantity: input.Quantity}
	if input.Description != nil {
		line.Description = *input.Description
	}
	if input.Line != nil {
		line.Line = *input.Line
	}
	if input.Link != nil {
		line.Link = *input.Link
	}
	if input.Notes != nil {
		line.Notes = *input.Notes
	}
	if input.Price != nil {
		value := inventree.DecimalString(*input.Price)
		line.Price = &value
	}
	if input.PriceCurrency != nil {
		line.PriceCurrency = *input.PriceCurrency
	}
	line.TargetDate = input.TargetDate
	f.extraLines = append(f.extraLines, line)
	if f.createExtraLineDuplicateAfterPersist {
		duplicate := line
		duplicate.PK++
		duplicate.Quantity++
		f.extraLines = append(f.extraLines, duplicate)
	}
	if f.createExtraLineErrAfterPersist != nil {
		return inventree.PurchaseOrderExtraLine{}, f.createExtraLineErrAfterPersist
	}
	return line, nil
}

func (f *fakePurchasingClient) UpdatePurchaseOrderExtraLine(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderExtraLine, error) {
	f.updateExtraLineCalls++
	for index := range f.extraLines {
		if f.extraLines[index].PK == id {
			applyExtraLinePatchForTest(&f.extraLines[index], fields)
			if f.updateExtraLineErrAfterPersist != nil {
				return inventree.PurchaseOrderExtraLine{}, f.updateExtraLineErrAfterPersist
			}
			return f.extraLines[index], nil
		}
	}
	return inventree.PurchaseOrderExtraLine{}, errors.New("extra line not found")
}

func (f *fakePurchasingClient) DeletePurchaseOrderExtraLine(_ context.Context, id int) error {
	for index := range f.extraLines {
		if f.extraLines[index].PK == id {
			if f.deleteExtraLineKeep {
				return nil
			}
			f.extraLines = append(f.extraLines[:index], f.extraLines[index+1:]...)
			return nil
		}
	}
	return &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
}

func applyExtraLinePatchForTest(line *inventree.PurchaseOrderExtraLine, fields inventree.PatchFields) {
	for name, field := range fields {
		value := field.Value()
		switch name {
		case "order":
			line.Order = value.(int)
		case "reference":
			line.Reference = value.(string)
		case "description":
			line.Description = value.(string)
		case "line":
			line.Line = value.(string)
		case "link":
			line.Link = value.(string)
		case "notes":
			line.Notes = value.(string)
		case "quantity":
			line.Quantity = value.(float64)
		case "price":
			if value == nil {
				line.Price = nil
			} else {
				decimal := inventree.DecimalString(value.(string))
				line.Price = &decimal
			}
		case "price_currency":
			line.PriceCurrency = value.(string)
		case "target_date":
			if value == nil {
				line.TargetDate = nil
			} else {
				target := value.(string)
				line.TargetDate = &target
			}
		}
	}
}

func (f *fakePurchasingClient) SearchSupplierParts(_ context.Context, query inventree.SupplierPartQuery) ([]inventree.SupplierPart, error) {
	result := make([]inventree.SupplierPart, 0, len(f.supplierParts))
	for _, part := range f.supplierParts {
		if (query.Part == 0 || part.Part == query.Part) && (query.Supplier == 0 || part.Supplier == query.Supplier) {
			result = append(result, part)
		}
	}
	return result, nil
}

func (f *fakePurchasingClient) GetSupplierPart(_ context.Context, id int) (inventree.SupplierPart, error) {
	if f.supplierPart.PK != 0 {
		return f.supplierPart, nil
	}
	for _, part := range f.supplierParts {
		if part.PK == id {
			return part, nil
		}
	}
	return inventree.SupplierPart{PK: id, Part: 10, Supplier: 30}, nil
}
