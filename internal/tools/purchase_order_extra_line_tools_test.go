package tools

import (
	"errors"
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

func TestPurchaseOrderExtraLineCreateDryRunAndSignedPriceExecution(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 120, Reference: "PO-120", Supplier: 30}}}
	input := CreatePurchaseOrderExtraLineInput{DryRun: true, OrderID: 120, Reference: " CE05129 ", Description: dvgoutils.Ptr("100 Pack of Diodes"), Link: dvgoutils.Ptr("https://supplier.test/item?tracking=secret#details"), Quantity: 1, UnitPrice: dvgoutils.Ptr("-1.250000"), Currency: dvgoutils.Ptr("aud")}

	_, planned, err := createPurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	r.Len(planned.PlannedChanges, 1)
	a.Equal(map[string]any{"order": 120, "reference": "CE05129", "description": "100 Pack of Diodes", "link": "https://supplier.test/item", "quantity": float64(1), "price": "-1.250000", "price_currency": "AUD"}, planned.PlannedChanges[0].Fields)
	a.Zero(fake.createExtraLineCalls)

	input.DryRun = false
	_, created, err := createPurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, created.Status)
	a.True(created.Verified)
	r.NotNil(created.Record)
	a.Equal("CE05129", created.Record.Reference)
	a.Equal("https://supplier.test/item", created.Record.Link, "public read-back must remove query and fragment")
	r.NotNil(created.Record.Price)
	a.Equal(inventree.DecimalString("-1.250000"), *created.Record.Price)
	a.Equal("AUD", created.Record.PriceCurrency)
	a.Equal(1, fake.createExtraLineCalls)
}

func TestPurchaseOrderExtraLineLookupAndValidationGuards(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	price := inventree.DecimalString("1.25")
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120}, {PK: 121}},
		extraLines: []inventree.PurchaseOrderExtraLine{
			{PK: 201, Order: 120, Reference: "FREIGHT", Description: "shipping", Link: "https://supplier.test/item?token=secret#section", Quantity: 1, Price: &price, PriceCurrency: "AUD"},
			{PK: 202, Order: 121, Reference: "FREIGHT", Quantity: 2},
		},
	}

	_, found, err := searchPurchaseOrderExtraLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, PurchaseOrderExtraLineSearchInput{Search: "shipping", OrderID: 120})
	r.NoError(err)
	a.Equal(StatusOK, found.Status)
	r.Len(found.Results, 1)
	a.Equal("https://supplier.test/item", found.Results[0].Link)
	_, invalidSearch, err := searchPurchaseOrderExtraLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, PurchaseOrderExtraLineSearchInput{Offset: -1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, invalidSearch.Status)
	r.NotNil(invalidSearch.Clarification)
	_, got, err := getPurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 201})
	r.NoError(err)
	a.Equal(StatusOK, got.Status)
	a.Equal(201, got.Record.PK)
	_, missing, err := getPurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 999})
	r.NoError(err)
	a.Equal(StatusNotFound, missing.Status)

	invalidInputs := []CreatePurchaseOrderExtraLineInput{
		{OrderID: 120, Reference: " ", Quantity: 1},
		{OrderID: 120, Reference: "REF", Quantity: -1},
		{OrderID: 120, Reference: "REF", Quantity: 0.000001},
		{OrderID: 120, Reference: "REF", Quantity: 1, UnitPrice: dvgoutils.Ptr("1.1234567"), Currency: dvgoutils.Ptr("AUD")},
		{OrderID: 120, Reference: "REF", Quantity: 1, UnitPrice: dvgoutils.Ptr("1")},
		{OrderID: 120, Reference: "REF", Quantity: 1, Currency: dvgoutils.Ptr("AUD")},
		{OrderID: 120, Reference: "REF", Quantity: 1, UnitPrice: dvgoutils.Ptr("1"), Currency: dvgoutils.Ptr("AU")},
		{OrderID: 120, Reference: "REF", Quantity: 1, Link: dvgoutils.Ptr("https://user:secret@supplier.test/item")},
		{OrderID: 120, Reference: "REF", Quantity: 1, TargetDate: dvgoutils.Ptr("04-08-2026")},
	}
	for _, input := range invalidInputs {
		_, output, callErr := createPurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(callErr)
		a.Equal(StatusClarificationRequired, output.Status)
		r.NotNil(output.Clarification)
	}
	a.Zero(fake.createExtraLineCalls)
}

func TestPurchaseOrderExtraLineUpdateAllFieldsAndDuplicateGuard(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	oldPrice := inventree.DecimalString("2")
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120}, {PK: 121}},
		extraLines: []inventree.PurchaseOrderExtraLine{
			{PK: 201, Order: 120, Reference: "OLD", Quantity: 1, Price: &oldPrice, PriceCurrency: "AUD"},
			{PK: 202, Order: 121, Reference: "TAKEN", Quantity: 1},
		},
	}
	duplicateReference := " TAKEN "
	_, duplicate, err := updatePurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePurchaseOrderExtraLineInput{ID: 201, OrderID: dvgoutils.Ptr(121), Reference: &duplicateReference})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, duplicate.Status)
	a.Zero(fake.updateExtraLineCalls)

	input := UpdatePurchaseOrderExtraLineInput{
		ID: 201, OrderID: dvgoutils.Ptr(121), Reference: dvgoutils.Ptr(" NEW "),
		Description: dvgoutils.Ptr("handling"), Line: dvgoutils.Ptr("INV-4"),
		Link: dvgoutils.Ptr("https://supplier.test/handling?secret=value"), Notes: dvgoutils.Ptr("invoice note"),
		Quantity: dvgoutils.Ptr(2.5), UnitPrice: dvgoutils.Ptr("-0.75"), Currency: dvgoutils.Ptr("aud"), TargetDate: dvgoutils.Ptr("2026-08-10"),
	}
	_, updated, err := updatePurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, updated.Status)
	r.NotNil(updated.Record)
	a.Equal(121, updated.Record.Order)
	a.Equal("NEW", updated.Record.Reference)
	a.Equal("handling", updated.Record.Description)
	a.Equal("INV-4", updated.Record.Line)
	a.Equal("https://supplier.test/handling", updated.Record.Link)
	a.Equal("invoice note", updated.Record.Notes)
	a.Equal(2.5, updated.Record.Quantity)
	a.Equal("AUD", updated.Record.PriceCurrency)
	r.NotNil(updated.Record.TargetDate)
	r.Len(updated.AffectedPurchaseOrders, 2)
	a.Equal([]int{120, 121}, []int{updated.AffectedPurchaseOrders[0].PK, updated.AffectedPurchaseOrders[1].PK})

	_, cleared, err := updatePurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePurchaseOrderExtraLineInput{ID: 201, ClearUnitPrice: true, ClearTargetDate: true})
	r.NoError(err)
	a.Equal(StatusOK, cleared.Status)
	r.NotNil(cleared.Record)
	a.Nil(cleared.Record.Price)
	a.Nil(cleared.Record.TargetDate)
}

func TestPurchaseOrderExtraLineCreateRecoversAmbiguousResponseLoss(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 120, Reference: "PO-120", Supplier: 30}}, createExtraLineErrAfterPersist: errors.New("connection reset after write")}

	_, output, err := createPurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePurchaseOrderExtraLineInput{OrderID: 120, Reference: "CE05129", Quantity: 1, UnitPrice: dvgoutils.Ptr("0"), Currency: dvgoutils.Ptr("AUD")})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.Recovered)
	a.True(output.Verified)
	r.NotNil(output.Record)
	a.Equal(201, output.Record.PK)
	a.Equal(1, fake.createExtraLineCalls)

	_, retry, err := createPurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePurchaseOrderExtraLineInput{OrderID: 120, Reference: "CE05129", Quantity: 1, UnitPrice: dvgoutils.Ptr("0"), Currency: dvgoutils.Ptr("AUD")})
	r.NoError(err)
	a.Equal(StatusOK, retry.Status)
	a.True(retry.Recovered)
	a.Equal(1, fake.createExtraLineCalls, "an identical retry must recover without a second POST")
}

func TestPurchaseOrderExtraLineDuplicateScanPaginatesAndReturnsStableCandidates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		orders:            []inventree.PurchaseOrder{{PK: 120}},
		extraLinePageSize: 1,
		extraLines: []inventree.PurchaseOrderExtraLine{
			{PK: 201, Order: 120, Reference: "FREIGHT", Description: "first", Quantity: 1},
			{PK: 202, Order: 120, Reference: "FREIGHT", Description: "second", Quantity: 2},
		},
	}

	_, output, err := createPurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePurchaseOrderExtraLineInput{OrderID: 120, Reference: "FREIGHT", Quantity: 1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	r.Len(output.Clarification.Candidates, 2)
	a.Equal("201", output.Clarification.Candidates[0].ID)
	a.Equal("202", output.Clarification.Candidates[1].ID)
	a.Equal("FREIGHT", output.Clarification.Candidates[0].Label)
	a.Zero(fake.createExtraLineCalls)
}

func TestPurchaseOrderExtraLineScansFailClosedAndSanitizeErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	bounded := &fakePurchasingClient{extraLines: []inventree.PurchaseOrderExtraLine{{PK: 201, Order: 120, Reference: "FREIGHT"}}, extraLineCountOverride: maxPurchaseOrderExtraLineScan + 1}
	_, err := findExtraLinesByReference(ctx, bounded, 120, "FREIGHT", 0)
	r.Error(err)
	a.Contains(err.Error(), "safety bound")
	_, err = scanPurchaseOrderExtraLines(ctx, bounded, 120)
	r.Error(err)
	a.Contains(err.Error(), "safety bound")

	failing := &fakePurchasingClient{extraLineSearchErr: errors.New("upstream secret detail")}
	_, _, err = searchPurchaseOrderExtraLines(purchasingDeps(failing))(ctx, &mcp.CallToolRequest{}, PurchaseOrderExtraLineSearchInput{})
	r.Error(err)
	a.NotContains(err.Error(), "secret")
	a.Contains(err.Error(), "availability and permissions")
}

func TestPurchaseOrderExtraLineAmbiguousRecoveryPreservesStableCandidates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 120}}, createExtraLineErrAfterPersist: errors.New("timeout after write"), createExtraLineDuplicateAfterPersist: true}

	_, output, err := createPurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePurchaseOrderExtraLineInput{OrderID: 120, Reference: "FREIGHT", Quantity: 1})
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	a.Contains(output.RecoveryPlan, "inspect every candidate")
	r.NotNil(output.Clarification)
	r.Len(output.Clarification.Candidates, 2)
	a.Equal(GetPurchaseOrderExtraLineToolName, output.Clarification.RetryTool)
	a.Equal("201", output.Clarification.Candidates[0].ID)
	a.Equal("202", output.Clarification.Candidates[1].ID)
}

func TestPurchaseOrderExtraLineAmbiguousRecoveryWithoutCandidateRoutesToSearch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 120}}, createExtraLineErr: errors.New("connection reset before response")}

	_, output, err := createPurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePurchaseOrderExtraLineInput{OrderID: 120, Reference: "FREIGHT", Quantity: 1})
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.Clarification)
	a.Empty(output.Clarification.Candidates)
	a.Equal("filters", output.Clarification.Retry)
	a.Equal(SearchPurchaseOrderExtraLinesToolName, output.Clarification.RetryTool)
	a.Equal(map[string]any{"order_id": 120, "search": "FREIGHT"}, output.Clarification.RetryValues)
}

func TestPurchaseOrderExtraLineUpdatePlansAndRecoversStablePatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	zero := inventree.DecimalString("0.000000")
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 120, Reference: "PO-120", Supplier: 30}}, extraLines: []inventree.PurchaseOrderExtraLine{{PK: 201, Order: 120, Reference: "CE05129", Quantity: 1, Price: &zero, PriceCurrency: "AUD"}}, updateExtraLineErrAfterPersist: errors.New("timeout after patch")}
	input := UpdatePurchaseOrderExtraLineInput{DryRun: true, ID: 201, Notes: dvgoutils.Ptr("invoice context"), UnitPrice: dvgoutils.Ptr("-0.5"), Currency: dvgoutils.Ptr("AUD")}

	_, planned, err := updatePurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	r.Len(planned.PlannedChanges, 1)
	a.Equal(map[string]any{"notes": "invoice context", "price": "-0.5", "price_currency": "AUD"}, planned.PlannedChanges[0].Fields)
	a.Zero(fake.updateExtraLineCalls)

	input.DryRun = false
	_, updated, err := updatePurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, updated.Status)
	a.True(updated.Recovered)
	a.True(updated.Verified)
	r.NotNil(updated.Record)
	a.Equal("invoice context", updated.Record.Notes)
}

func TestPurchaseOrderExtraLineDeleteRequiresConfirmationAndVerifiesAbsence(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 120, Reference: "PO-120", Supplier: 30}}, extraLines: []inventree.PurchaseOrderExtraLine{{PK: 201, Order: 120, Reference: "CE05129", Quantity: 1}}}

	_, preview, err := deletePurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderExtraLineInput{ID: 201})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, preview.Status)
	r.NotNil(preview.Record)
	r.NotNil(preview.Clarification)
	a.Equal("confirm", preview.Clarification.Retry)
	r.Len(fake.extraLines, 1)

	_, deleted, err := deletePurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderExtraLineInput{ID: 201, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, deleted.Status)
	a.True(deleted.Verified)
	a.Empty(fake.extraLines)
	r.Len(deleted.AffectedPurchaseOrders, 1)
	a.Equal(3, fake.getOrderCalls, "confirmed deletion must refresh the order after the preview and pre-delete reads")
}

func TestPurchaseOrderExtraLineDeleteOrderNotFound(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{extraLines: []inventree.PurchaseOrderExtraLine{{PK: 201, Order: 120, Reference: "FREIGHT", Quantity: 1}}, missingOrderIDs: map[int]bool{120: true}}

	_, output, err := deletePurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderExtraLineInput{ID: 201, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.True(output.Clarification.HardError)
	a.Len(fake.extraLines, 1, "the extra line must not be deleted when its parent order cannot be found")
}

func TestPurchaseOrderExtraLineDeleteReportsUncertainVerification(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 120}}, extraLines: []inventree.PurchaseOrderExtraLine{{PK: 201, Order: 120, Reference: "FREIGHT", Quantity: 1}}, deleteExtraLineKeep: true}

	_, output, err := deletePurchaseOrderExtraLine(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderExtraLineInput{ID: 201, Confirm: true})
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	a.Contains(output.RecoveryPlan, "still exists")
	r.NotNil(output.Record)
	a.Equal(201, output.Record.PK)
}

func TestCreatePurchaseOrderWithLinesIncludesExtraLinesAfterReceivableLines(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	price := 0.8
	fake := &fakePurchasingClient{company: inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true}, supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "DIODE"}}}
	input := PurchaseOrderWorkflowInput{DryRun: true, SupplierID: 30, SupplierReference: "CORE-42", Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 8, UnitPrice: &price, Currency: "AUD"}}, ExtraLines: []PurchaseOrderWorkflowExtraLine{{Reference: "CE05129", Description: dvgoutils.Ptr("100 Pack of Diodes"), Quantity: 1, UnitPrice: dvgoutils.Ptr("0"), Currency: dvgoutils.Ptr("AUD")}}}

	_, planned, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	r.Len(planned.PlannedChanges, 3)
	a.Equal("create_purchase_order_line", planned.PlannedChanges[1].Action)
	a.Equal("create_purchase_order_extra_line", planned.PlannedChanges[2].Action)
	a.Equal([]PlannedChangeDependency{{Field: "order", Action: "create_purchase_order"}}, planned.PlannedChanges[2].DependsOn)

	input.DryRun = false
	_, created, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, created.Status)
	r.Len(created.Lines, 1)
	r.Len(created.ExtraLines, 1)
	a.Equal("CE05129", created.ExtraLines[0].Reference)
	a.Equal([]string{"create_purchase_order", "create_purchase_order_line", "create_purchase_order_extra_line"}, []string{created.Actions[0].Name, created.Actions[1].Name, created.Actions[2].Name})
}

func TestCreatePurchaseOrderWithLinesUpdatesRecoveredExtraLine(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	oldPrice := inventree.DecimalString("1")
	fake := &fakePurchasingClient{
		company:       inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "DIODE"}},
		orders:        []inventree.PurchaseOrder{{PK: 120, Supplier: 30, SupplierReference: "CORE-42"}},
		lines:         []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Reference: "CORE-42-1", Quantity: 8}},
		extraLines:    []inventree.PurchaseOrderExtraLine{{PK: 201, Order: 120, Reference: "FREIGHT", Quantity: 1, Price: &oldPrice, PriceCurrency: "AUD"}},
	}
	input := PurchaseOrderWorkflowInput{
		SupplierID: 30, SupplierReference: "CORE-42",
		Lines:      []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 8}},
		ExtraLines: []PurchaseOrderWorkflowExtraLine{{Reference: "FREIGHT", Description: dvgoutils.Ptr("freight charge"), Notes: dvgoutils.Ptr("invoice 42"), Link: dvgoutils.Ptr("https://supplier.test/freight"), Line: dvgoutils.Ptr("4"), Quantity: 2, UnitPrice: dvgoutils.Ptr("-0.5"), Currency: dvgoutils.Ptr("AUD"), TargetDate: dvgoutils.Ptr("2026-08-10")}},
	}

	_, output, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	r.Len(output.ExtraLines, 1)
	a.Equal(201, output.ExtraLines[0].PK)
	a.Equal("freight charge", output.ExtraLines[0].Description)
	a.Equal("invoice 42", output.ExtraLines[0].Notes)
	a.Equal(2.0, output.ExtraLines[0].Quantity)
	a.Equal(1, fake.updateExtraLineCalls)
	a.Equal("updated", output.Actions[len(output.Actions)-1].Status)
}

func TestCreatePurchaseOrderWithLinesPreservesNormalLinesWhenExtraLineFails(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{company: inventree.Company{PK: 30, IsSupplier: true}, supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30}}, createExtraLineErr: &inventree.APIError{StatusCode: 400, Kind: inventree.ErrorKindValidation}, failCreateExtraLineAt: 2}
	input := PurchaseOrderWorkflowInput{SupplierID: 30, SupplierReference: "CORE-43", Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 8}}, ExtraLines: []PurchaseOrderWorkflowExtraLine{{Reference: "FREIGHT", Quantity: 1, UnitPrice: dvgoutils.Ptr("0"), Currency: dvgoutils.Ptr("AUD")}, {Reference: "CE05129", Quantity: 1, UnitPrice: dvgoutils.Ptr("0"), Currency: dvgoutils.Ptr("AUD")}}}

	_, output, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.PurchaseOrder)
	r.Len(output.Lines, 1)
	r.Len(output.ExtraLines, 1)
	a.Equal("FREIGHT", output.ExtraLines[0].Reference)
	r.NotNil(output.Failure)
	a.Equal("create_purchase_order_extra_line", output.Failure.Action)
	a.Nil(output.Clarification, "definite pre-write failures must not be labeled as ambiguous recovery")
	a.Equal("created", output.Actions[1].Status)
	a.Equal("created", output.Actions[2].Status)
	a.Equal("failed", output.Actions[3].Status)
}

func TestCreatePurchaseOrderWithLinesRecoversExtraLineAfterPersistedResponseLoss(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{company: inventree.Company{PK: 30, IsSupplier: true}, supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30}}, createExtraLineErrAfterPersist: errors.New("connection reset after write")}
	input := PurchaseOrderWorkflowInput{SupplierID: 30, SupplierReference: "CORE-44", Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 8}}, ExtraLines: []PurchaseOrderWorkflowExtraLine{{Reference: "FREIGHT", Quantity: 1, UnitPrice: dvgoutils.Ptr("0"), Currency: dvgoutils.Ptr("AUD")}}}

	_, output, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	r.Len(output.ExtraLines, 1)
	a.Equal("recovered", output.Actions[len(output.Actions)-1].Status)
	a.Equal(1, fake.createExtraLineCalls)
}

func TestCreatePurchaseOrderWithLinesAmbiguousExtraFailureRoutesToSearch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{company: inventree.Company{PK: 30, IsSupplier: true}, supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30}}, createExtraLineErr: errors.New("connection reset before response")}
	input := PurchaseOrderWorkflowInput{SupplierID: 30, SupplierReference: "CORE-45", Lines: []PurchaseOrderWorkflowLine{{SupplierPartID: 40, Quantity: 8}}, ExtraLines: []PurchaseOrderWorkflowExtraLine{{Reference: "FREIGHT", Quantity: 1}}}

	_, output, err := createPurchaseOrderWithLines(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.Clarification)
	a.Empty(output.Clarification.Candidates)
	a.Equal(SearchPurchaseOrderExtraLinesToolName, output.Clarification.RetryTool)
	a.Equal("filters", output.Clarification.Retry)
}

func TestPurchaseOrderExtraLineInputsExcludeProjectCode(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	for _, schemaType := range []reflect.Type{reflect.TypeOf(CreatePurchaseOrderExtraLineInput{}), reflect.TypeOf(UpdatePurchaseOrderExtraLineInput{}), reflect.TypeOf(PurchaseOrderWorkflowExtraLine{}), reflect.TypeOf(inventree.PurchaseOrderExtraLineCreate{})} {
		for _, field := range reflect.VisibleFields(schemaType) {
			a.NotContains(strings.ToLower(field.Name), "project")
			a.NotContains(strings.ToLower(jsonFieldName(field.Tag.Get("json"))), "project")
		}
	}
}
