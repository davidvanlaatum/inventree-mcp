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

func TestWorkflowPlannedChangesMCPWireContract(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	catalogFake := &fakeMilestoneLookupClient{
		companies:      []inventree.Company{{PK: 3, Name: "Supplier", IsSupplier: true}},
		stockLocations: []inventree.StockLocation{{PK: 40, Name: "Bin"}},
	}
	catalogSession, closeCatalog := plannedChangesSession(t, ctx, catalogFake)
	defer closeCatalog()

	listed, err := catalogSession.ListTools(ctx, nil)
	r.NoError(err)
	seenSchemas := map[string]bool{}
	for _, tool := range listed.Tools {
		switch tool.Name {
		case UpsertPartWorkflowToolName, InitialStockWorkflowToolName, CreatePurchaseOrderWorkflowToolName, IssuePurchaseOrderToolName, CompletePurchaseOrderToolName, CreatePurchaseOrderExtraLineToolName, UpdatePurchaseOrderExtraLineToolName:
			schema := tool.OutputSchema.(map[string]any)
			properties := schema["properties"].(map[string]any)
			a.Contains(properties, "planned_changes", tool.Name)
			seenSchemas[tool.Name] = true
		}
	}
	a.Equal(map[string]bool{
		UpsertPartWorkflowToolName:           true,
		InitialStockWorkflowToolName:         true,
		CreatePurchaseOrderWorkflowToolName:  true,
		IssuePurchaseOrderToolName:           true,
		CompletePurchaseOrderToolName:        true,
		CreatePurchaseOrderExtraLineToolName: true,
		UpdatePurchaseOrderExtraLineToolName: true,
	}, seenSchemas)

	partResult, err := catalogSession.CallTool(ctx, &mcp.CallToolParams{Name: UpsertPartWorkflowToolName, Arguments: map[string]any{
		"dry_run": true, "name": "wire part", "category_id": 12, "units": "", "purchaseable": false,
		"supplier_id": 3, "supplier_sku": "WIRE-SKU",
	}})
	r.NoError(err)
	a.False(partResult.IsError)
	partOutput := partResult.StructuredContent.(map[string]any)
	a.NotContains(partOutput, "part")
	partChanges := partOutput["planned_changes"].([]any)
	partCreate := partChanges[0].(map[string]any)
	a.NotContains(partCreate, "id")
	partFields := partCreate["fields"].(map[string]any)
	a.Equal(false, partFields["purchaseable"])
	a.Equal("", partFields["units"])
	supplierCreate := partChanges[1].(map[string]any)
	dependencies := supplierCreate["depends_on"].([]any)
	a.Equal(map[string]any{"field": "part", "action": "create_part"}, dependencies[0])

	stockResult, err := catalogSession.CallTool(ctx, &mcp.CallToolParams{Name: InitialStockWorkflowToolName, Arguments: map[string]any{
		"dry_run": true, "part_id": 10, "location_id": 40, "quantity": 7, "status": 0, "batch": "", "serial": "", "notes": "",
	}})
	r.NoError(err)
	a.False(stockResult.IsError)
	stockOutput := stockResult.StructuredContent.(map[string]any)
	stockChanges := stockOutput["planned_changes"].([]any)
	stockFields := stockChanges[0].(map[string]any)["fields"].(map[string]any)
	a.Equal(float64(0), stockFields["status"])
	a.Equal("", stockFields["batch"])
	a.Equal("", stockFields["serial"])
	a.Equal("", stockFields["notes"])

	purchasingFake := &fakePurchasingClient{
		company:       inventree.Company{PK: 30, Name: "Supplier", IsSupplier: true},
		supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"}},
		orders:        []inventree.PurchaseOrder{{PK: 120, Supplier: 30, Reference: "PO-120", Status: inventree.PurchaseOrderStatusPending}},
	}
	purchasingSession, closePurchasing := plannedChangesSession(t, ctx, purchasingFake)
	defer closePurchasing()

	orderResult, err := purchasingSession.CallTool(ctx, &mcp.CallToolParams{Name: CreatePurchaseOrderWorkflowToolName, Arguments: map[string]any{
		"dry_run": true, "supplier_id": 30, "supplier_reference": "WIRE-ORDER",
		"lines":       []any{map[string]any{"supplier_part_id": 40, "quantity": 2}},
		"extra_lines": []any{map[string]any{"reference": "SUPPLIER-SKU", "quantity": 1, "unit_price": "0", "currency": "AUD"}},
	}})
	r.NoError(err)
	a.False(orderResult.IsError)
	orderOutput := orderResult.StructuredContent.(map[string]any)
	orderChanges := orderOutput["planned_changes"].([]any)
	lineCreate := orderChanges[1].(map[string]any)
	a.NotContains(lineCreate, "id")
	lineFields := lineCreate["fields"].(map[string]any)
	a.Equal(false, lineFields["auto_pricing"])
	a.Equal(false, lineFields["merge_items"])
	lineDependencies := lineCreate["depends_on"].([]any)
	a.Equal(map[string]any{"field": "order", "action": "create_purchase_order"}, lineDependencies[0])
	extraCreate := orderChanges[2].(map[string]any)
	a.Equal("create_purchase_order_extra_line", extraCreate["action"])
	extraFields := extraCreate["fields"].(map[string]any)
	a.Equal("0", extraFields["price"])
	a.Equal("AUD", extraFields["price_currency"])
	a.NotContains(extraFields, "project_code")
	extraDependencies := extraCreate["depends_on"].([]any)
	a.Equal(map[string]any{"field": "order", "action": "create_purchase_order"}, extraDependencies[0])

	issueResult, err := purchasingSession.CallTool(ctx, &mcp.CallToolParams{Name: IssuePurchaseOrderToolName, Arguments: map[string]any{"dry_run": true, "order_id": 120}})
	r.NoError(err)
	a.False(issueResult.IsError)
	issueOutput := issueResult.StructuredContent.(map[string]any)
	issueChanges := issueOutput["planned_changes"].([]any)
	issueChange := issueChanges[0].(map[string]any)
	a.Equal(float64(120), issueChange["id"])
	a.Equal(float64(inventree.PurchaseOrderStatusPlaced), issueChange["fields"].(map[string]any)["status"])

	purchasingFake.orders[0].Status = inventree.PurchaseOrderStatusPlaced
	purchasingFake.lines = []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Quantity: 2, Received: 2}}
	completeResult, err := purchasingSession.CallTool(ctx, &mcp.CallToolParams{Name: CompletePurchaseOrderToolName, Arguments: map[string]any{"dry_run": true, "order_id": 120}})
	r.NoError(err)
	a.False(completeResult.IsError)
	completeOutput := completeResult.StructuredContent.(map[string]any)
	completeChanges := completeOutput["planned_changes"].([]any)
	completeChange := completeChanges[0].(map[string]any)
	a.Equal(float64(120), completeChange["id"])
	completeFields := completeChange["fields"].(map[string]any)
	a.Equal(float64(inventree.PurchaseOrderStatusComplete), completeFields["status"])
	a.Equal(false, completeFields["accept_incomplete"])
}

func plannedChangesSession(t *testing.T, ctx context.Context, client any) (*mcp.ClientSession, func()) {
	t.Helper()
	r := require.New(t)
	serverCtx, cancel := context.WithCancel(ctx)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		server := mcp.NewServer(&mcp.Implementation{Name: "planned-changes-test", Version: "v0.0.0"}, nil)
		Register(server, Dependencies{EnableWriteTools: true, ClientFromContext: func(context.Context) (any, error) { return client, nil }})
		serverDone <- server.Run(serverCtx, serverTransport)
	}()
	caller := mcp.NewClient(&mcp.Implementation{Name: "planned-changes-client", Version: "v0.0.0"}, nil)
	session, err := caller.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	return session, func() {
		r.NoError(session.Close())
		cancel()
		<-serverDone
	}
}
