package tools

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransferStockItemPlansAndExecutesCompleteQuantityToAnyValidDestination(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	supplierPartID := 80
	purchaseOrderID := 90
	ownerID := 91
	price := inventree.DecimalString("12.34")
	fake := newFakeStockTransferClient()
	fake.item.SupplierPart = &supplierPartID
	fake.item.PurchaseOrder = &purchaseOrderID
	fake.item.Owner = &ownerID
	fake.item.PurchasePrice = &price
	fake.item.PurchasePriceCurrency = "AUD"
	fake.locations[20] = inventree.StockLocation{
		PK: 20, Name: "External structural destination", Structural: true, External: true,
		Owner: &ownerID, Path: []inventree.TreePath{{PK: 1, Name: "Warehouse"}, {PK: 20, Name: "External structural destination"}},
	}
	input := TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move to corrected drawer"}

	_, planned, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	r.NotNil(planned.Plan)
	r.NotNil(planned.Plan.Transfer)
	a.Equal(3.5, planned.Plan.Before.Quantity)
	a.Equal(10, *planned.Plan.Before.LocationID)
	a.Equal(20, *planned.Plan.After.LocationID)
	a.False(planned.Plan.Transfer.WillSplit)
	a.Equal("Source", planned.Plan.Transfer.Source.Name)
	a.Len(planned.Plan.Transfer.Source.Path, 2)
	a.True(planned.Plan.Transfer.Destination.Structural)
	a.True(planned.Plan.Transfer.Destination.External)
	a.Equal(&supplierPartID, planned.Plan.Transfer.Provenance.SupplierPartID)
	a.Equal(&purchaseOrderID, planned.Plan.Transfer.Provenance.PurchaseOrderID)
	a.Equal(&price, planned.Plan.Transfer.Provenance.PurchasePrice)
	a.NotEmpty(planned.PlanHash)
	a.Zero(fake.transferCalls)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.True(executed.Verified)
	a.False(executed.Recovered)
	r.NotNil(executed.Record)
	a.Equal(20, *executed.Record.Location)
	a.Equal(3.5, executed.Record.Quantity)
	a.Equal(1, fake.transferCalls)
	a.Equal(20, fake.lastTransfer.Location)
	a.Equal("3.5", fake.lastTransfer.Items[0].Quantity)
	a.Equal("move to corrected drawer", fake.lastTransfer.Notes)
	a.Equal(&supplierPartID, executed.Record.SupplierPart)
	a.Equal(&purchaseOrderID, executed.Record.PurchaseOrder)
}

func TestTransferStockItemRejectsInvalidAndUnsafeState(t *testing.T) {
	t.Parallel()
	serial := "S-1"
	allocated := 1.0
	linkedID := 70
	one := 1

	for _, tc := range []struct {
		name      string
		configure func(*inventree.StockItem)
		field     string
	}{
		{name: "missing source location", configure: func(item *inventree.StockItem) { item.Location = nil }, field: "location"},
		{name: "unavailable", configure: func(item *inventree.StockItem) { item.InStock = false }, field: "in_stock"},
		{name: "unknown allocation", configure: func(item *inventree.StockItem) { item.Allocated = nil }, field: "allocated"},
		{name: "allocated", configure: func(item *inventree.StockItem) { item.Allocated = &allocated }, field: "allocated"},
		{name: "serialized", configure: func(item *inventree.StockItem) { item.Serial = &serial }, field: "serial"},
		{name: "building", configure: func(item *inventree.StockItem) { item.IsBuilding = true }, field: "is_building"},
		{name: "build linked", configure: func(item *inventree.StockItem) { item.Build = &linkedID }, field: "build"},
		{name: "consumed", configure: func(item *inventree.StockItem) { item.ConsumedBy = &linkedID }, field: "consumed_by"},
		{name: "installed", configure: func(item *inventree.StockItem) { item.BelongsTo = &linkedID }, field: "belongs_to"},
		{name: "child", configure: func(item *inventree.StockItem) { item.Parent = &linkedID }, field: "parent"},
		{name: "unknown installed items", configure: func(item *inventree.StockItem) { item.InstalledItems = nil }, field: "installed_items"},
		{name: "installed items", configure: func(item *inventree.StockItem) { item.InstalledItems = &one }, field: "installed_items"},
		{name: "unknown child items", configure: func(item *inventree.StockItem) { item.ChildItems = nil }, field: "child_items"},
		{name: "child items", configure: func(item *inventree.StockItem) { item.ChildItems = &one }, field: "child_items"},
		{name: "customer assigned", configure: func(item *inventree.StockItem) { item.Customer = &linkedID }, field: "customer"},
		{name: "sales order linked", configure: func(item *inventree.StockItem) { item.SalesOrder = &linkedID }, field: "sales_order"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockTransferClient()
			tc.configure(&fake.item)
			_, output, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move"})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, output.Status)
			r.NotNil(output.Clarification)
			a.Equal(tc.field, output.Clarification.Field)
			a.Zero(fake.transferCalls)
		})
	}
}

func TestTransferStockItemRejectsNoOpMissingInputsAndDestination(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTransferClient()

	for _, input := range []TransferStockItemInput{
		{DryRun: true, DestinationLocationID: 20, Reason: "move"},
		{DryRun: true, StockItemID: 50, Reason: "move"},
		{DryRun: true, StockItemID: 50, DestinationLocationID: 20},
	} {
		_, output, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusClarificationRequired, output.Status)
	}

	_, noOp, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 10, Reason: "move"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, noOp.Status)
	a.Equal("destination_location", noOp.Clarification.Field)

	fake.locationErrors[20] = &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	_, missing, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, missing.Status)
	a.Equal("destination_location", missing.Clarification.Field)
	a.Zero(fake.transferCalls)
}

func TestTransferStockItemFailsClosedOnExactReadErrorsAndIdentityMismatch(t *testing.T) {
	t.Parallel()

	t.Run("stock item not found", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newFakeStockTransferClient()
		fake.itemErr = &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
		_, output, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move"})
		r.NoError(err)
		a.Equal(StatusNotFound, output.Status)
		a.Zero(fake.transferCalls)
	})

	for _, tc := range []struct {
		name      string
		configure func(*fakeStockTransferClient)
	}{
		{name: "stock item identity", configure: func(fake *fakeStockTransferClient) { fake.item.PK = 51 }},
		{name: "source location identity", configure: func(fake *fakeStockTransferClient) {
			location := fake.locations[10]
			location.PK = 11
			fake.locations[10] = location
		}},
		{name: "destination location identity", configure: func(fake *fakeStockTransferClient) {
			location := fake.locations[20]
			location.PK = 21
			fake.locations[20] = location
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockTransferClient()
			tc.configure(fake)
			_, _, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move"})
			r.Error(err)
			r.Zero(fake.transferCalls)
		})
	}

	t.Run("source location read error", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newFakeStockTransferClient()
		fake.locationErrors[10] = errors.New("source read failed")
		_, _, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move"})
		r.Error(err)
		r.Zero(fake.transferCalls)
	})
}

func TestTransferStockItemRejectsStaleAndReusedPlan(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTransferClient()
	input := TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move"}

	_, planned, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	fake.item.Quantity = 4
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, stale, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Zero(fake.transferCalls)

	fake.item.Quantity = 3.5
	input.DryRun = true
	input.Confirm = false
	input.PlanHash = ""
	_, destinationPlan, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	destination := fake.locations[20]
	destination.External = true
	fake.locations[20] = destination
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = destinationPlan.PlanHash
	_, staleDestination, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, staleDestination.Status)
	a.Zero(fake.transferCalls)
	destination.External = false
	fake.locations[20] = destination

	input.DryRun = true
	input.Confirm = false
	input.PlanHash = ""
	_, planned, err = transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	fake.item.Location = intPointer(10)
	_, reused, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, reused.Status)
	a.Equal(1, fake.transferCalls)
}

func TestTransferStockItemRecoversLostResponseAndReturnsPartialForMismatch(t *testing.T) {
	t.Parallel()
	t.Run("recovered", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newFakeStockTransferClient()
		fake.responseLoss = true
		input := TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move"}
		_, planned, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = planned.PlanHash
		_, output, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusOK, output.Status)
		a.True(output.Verified)
		a.True(output.Recovered)
		a.Equal(1, fake.transferCalls)
	})

	t.Run("mismatch", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newFakeStockTransferClient()
		fake.afterTransfer = func(f *fakeStockTransferClient) { f.item.Packaging = stringPointer("changed") }
		input := TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move"}
		_, planned, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = planned.PlanHash
		_, output, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusPartialFailure, output.Status)
		r.NotNil(output.Failure)
		a.Contains(output.Failure.RecoveryPlan, "do not retry blindly")
		a.Equal(1, fake.transferCalls)
	})
}

func TestTransferStockItemPreservesContextErrorsAndClassifiesDefiniteRejection(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockTransferClient()
			input := TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move"}
			_, planned, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			fake.mutateErr = sentinel
			input.DryRun = false
			input.Confirm = true
			input.PlanHash = planned.PlanHash
			_, _, err = transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			r.ErrorIs(err, sentinel)
		})
	}

	t.Run("definite rejection", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newFakeStockTransferClient()
		input := TransferStockItemInput{DryRun: true, StockItemID: 50, DestinationLocationID: 20, Reason: "move"}
		_, planned, err := transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		fake.mutateErr = &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = planned.PlanHash
		_, _, err = transferStockItem(stockTransferDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.Error(err)
	})
}

func TestTransferStockItemAuthorizationAndMCPWireContract(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	a.Equal("operational", ToolAuthorizations[TransferStockItemToolName].MutationClass)
	a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational}, ToolAuthorizations[TransferStockItemToolName].Scopes)
	a.Equal(WriteAnnotations, ToolAuthorizations[TransferStockItemToolName].Annotations)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTransferClient()
	session, closeSession := plannedChangesSession(t, ctx, fake)
	defer closeSession()
	listed, err := session.ListTools(ctx, nil)
	r.NoError(err)
	found := false
	for _, tool := range listed.Tools {
		if tool.Name != TransferStockItemToolName {
			continue
		}
		found = true
		inputProperties := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		a.NotContains(inputProperties, "quantity")
		a.Contains(inputProperties, "stock_item_id")
		a.Contains(inputProperties, "destination_location_id")
		outputProperties := tool.OutputSchema.(map[string]any)["properties"].(map[string]any)
		a.Contains(outputProperties, "verified")
		a.Contains(outputProperties, "recovered")
	}
	a.True(found)

	plannedResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: TransferStockItemToolName, Arguments: map[string]any{
		"dry_run": true, "stock_item_id": 50, "destination_location_id": 20, "reason": "move",
	}})
	r.NoError(err)
	a.False(plannedResult.IsError)
	planned := plannedResult.StructuredContent.(map[string]any)
	plan := planned["plan"].(map[string]any)
	transfer := plan["transfer"].(map[string]any)
	a.Equal(false, transfer["will_split"])
	a.Equal(float64(20), plan["after"].(map[string]any)["location_id"])

	executedResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: TransferStockItemToolName, Arguments: map[string]any{
		"stock_item_id": 50, "destination_location_id": 20, "reason": "move", "confirm": true, "plan_hash": planned["plan_hash"],
	}})
	r.NoError(err)
	a.False(executedResult.IsError)
	executed := executedResult.StructuredContent.(map[string]any)
	a.Equal(StatusOK, executed["status"])
	a.Equal(true, executed["verified"])
}

type fakeStockTransferClient struct {
	item           inventree.StockItem
	itemErr        error
	locations      map[int]inventree.StockLocation
	locationErrors map[int]error
	lastTransfer   inventree.StockTransfer
	transferCalls  int
	mutateErr      error
	responseLoss   bool
	afterTransfer  func(*fakeStockTransferClient)
	planStore      *stockPlanStore
}

func newFakeStockTransferClient() *fakeStockTransferClient {
	zeroFloat := 0.0
	zeroInt := 0
	return &fakeStockTransferClient{
		item: inventree.StockItem{
			PK: 50, Part: 5, Location: intPointer(10), Quantity: 3.5, Status: stockStatusOK, InStock: true,
			Allocated: &zeroFloat, InstalledItems: &zeroInt, ChildItems: &zeroInt, TrackingItems: &zeroInt,
			Batch: stringPointer("B-1"), Packaging: stringPointer("reel"),
		},
		locations: map[int]inventree.StockLocation{
			10: {PK: 10, Name: "Source", Path: []inventree.TreePath{{PK: 1, Name: "Warehouse"}, {PK: 10, Name: "Source"}}},
			20: {PK: 20, Name: "Destination", Path: []inventree.TreePath{{PK: 1, Name: "Warehouse"}, {PK: 20, Name: "Destination"}}},
		},
		locationErrors: map[int]error{},
	}
}

func (f *fakeStockTransferClient) GetStockItem(context.Context, int) (inventree.StockItem, error) {
	return f.item, f.itemErr
}

func (f *fakeStockTransferClient) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
	if err := f.locationErrors[id]; err != nil {
		return inventree.StockLocation{}, err
	}
	location, ok := f.locations[id]
	if !ok {
		return inventree.StockLocation{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return location, nil
}

func (f *fakeStockTransferClient) TransferStock(_ context.Context, input inventree.StockTransfer) error {
	f.transferCalls++
	f.lastTransfer = input
	if f.mutateErr != nil {
		return f.mutateErr
	}
	f.item.Location = intPointer(input.Location)
	if f.afterTransfer != nil {
		f.afterTransfer(f)
	}
	if f.responseLoss {
		return errors.New("injected response loss after stock transfer")
	}
	return nil
}

func stockTransferDeps(fake *fakeStockTransferClient) Dependencies {
	if fake.planStore == nil {
		fake.planStore = newStockPlanStore(time.Now, randomStockPlanToken)
	}
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }, stockPlanStore: fake.planStore}
}

func intPointer(value int) *int { return &value }
