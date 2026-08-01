package tools

import (
	"context"
	"encoding/json"
	"errors"
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
	a.Zero(fake.createOrderCalls)
	a.Zero(fake.createLineCalls)
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
	company                    inventree.Company
	orders                     []inventree.PurchaseOrder
	lines                      []inventree.PurchaseOrderLineItem
	supplierParts              []inventree.SupplierPart
	supplierPart               inventree.SupplierPart
	lastOrderQuery             inventree.PurchaseOrderQuery
	lastLineQuery              inventree.PurchaseOrderLineQuery
	createOrderCalls           int
	createOrderErrAfterPersist error
	updateOrderCalls           int
	createLineCalls            int
	updateLineCalls            int
	createLineErr              error
	failCreateLineAt           int
}

func (f *fakePurchasingClient) GetCompany(_ context.Context, id int) (inventree.Company, error) {
	if f.company.PK == 0 {
		return inventree.Company{PK: id, IsSupplier: true}, nil
	}
	return f.company, nil
}

func (f *fakePurchasingClient) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
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
