package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const StatusPartialFailure = "partial_failure"

type PurchaseOrderLookupClient interface {
	SearchPurchaseOrders(context.Context, inventree.PurchaseOrderQuery) ([]inventree.PurchaseOrder, error)
	GetPurchaseOrder(context.Context, int) (inventree.PurchaseOrder, error)
	SearchPurchaseOrderLines(context.Context, inventree.PurchaseOrderLineQuery) ([]inventree.PurchaseOrderLineItem, error)
	GetPurchaseOrderLine(context.Context, int) (inventree.PurchaseOrderLineItem, error)
}

type PurchaseOrderWriteClient interface {
	GetCompany(context.Context, int) (inventree.Company, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	SearchPurchaseOrders(context.Context, inventree.PurchaseOrderQuery) ([]inventree.PurchaseOrder, error)
	CreatePurchaseOrder(context.Context, inventree.PurchaseOrderCreate) (inventree.PurchaseOrder, error)
}

type PurchaseOrderLineWriteClient interface {
	GetPurchaseOrder(context.Context, int) (inventree.PurchaseOrder, error)
	GetPurchaseOrderLine(context.Context, int) (inventree.PurchaseOrderLineItem, error)
	GetSupplierPart(context.Context, int) (inventree.SupplierPart, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	CreatePurchaseOrderLine(context.Context, inventree.PurchaseOrderLineCreate) (inventree.PurchaseOrderLineItem, error)
	UpdatePurchaseOrderLine(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrderLineItem, error)
}

type PurchaseOrderWorkflowClient interface {
	PurchasePreviewClient
	GetCompany(context.Context, int) (inventree.Company, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	GetPurchaseOrder(context.Context, int) (inventree.PurchaseOrder, error)
	SearchPurchaseOrders(context.Context, inventree.PurchaseOrderQuery) ([]inventree.PurchaseOrder, error)
	UpdatePurchaseOrder(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrder, error)
	CreatePurchaseOrder(context.Context, inventree.PurchaseOrderCreate) (inventree.PurchaseOrder, error)
	SearchPurchaseOrderLines(context.Context, inventree.PurchaseOrderLineQuery) ([]inventree.PurchaseOrderLineItem, error)
	CreatePurchaseOrderLine(context.Context, inventree.PurchaseOrderLineCreate) (inventree.PurchaseOrderLineItem, error)
	UpdatePurchaseOrderLine(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrderLineItem, error)
}

type PurchaseOrderSearchInput struct {
	Search           string `json:"search,omitempty" jsonschema:"Search text across order description, reference, supplier, and supplier reference."`
	SupplierID       int    `json:"supplier_id,omitempty" jsonschema:"Optional supplier company primary key filter."`
	Reference        string `json:"reference,omitempty" jsonschema:"Optional exact InvenTree purchase-order reference filter."`
	Status           *int   `json:"status,omitempty" jsonschema:"Optional exact InvenTree purchase-order status filter."`
	StartDateAfter   string `json:"start_date_after,omitempty" jsonschema:"Optional inclusive scheduled start date lower bound in YYYY-MM-DD form."`
	StartDateBefore  string `json:"start_date_before,omitempty" jsonschema:"Optional inclusive scheduled start date upper bound in YYYY-MM-DD form."`
	TargetDateAfter  string `json:"target_date_after,omitempty" jsonschema:"Optional inclusive target date lower bound in YYYY-MM-DD form."`
	TargetDateBefore string `json:"target_date_before,omitempty" jsonschema:"Optional inclusive target date upper bound in YYYY-MM-DD form."`
	Limit            int    `json:"limit,omitempty" jsonschema:"Maximum records to return. Defaults to 20 and is capped at 100."`
	Offset           int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type PurchaseOrderLineSearchInput struct {
	Search         string `json:"search,omitempty" jsonschema:"Search line supplier SKU, MPN, part text, or line reference."`
	OrderID        int    `json:"order_id,omitempty" jsonschema:"Optional purchase-order primary key filter."`
	SupplierPartID int    `json:"supplier_part_id,omitempty" jsonschema:"Optional supplier-part primary key filter."`
	Pending        *bool  `json:"pending,omitempty" jsonschema:"Optional pending-line filter."`
	Received       *bool  `json:"received,omitempty" jsonschema:"Optional received-line filter."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum records to return. Defaults to 20 and is capped at 100."`
	Offset         int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type CreatePurchaseOrderInput struct {
	Reference         string  `json:"reference,omitempty" jsonschema:"Optional pattern-compliant InvenTree purchase-order reference; InvenTree generates one when omitted."`
	SupplierID        int     `json:"supplier_id" jsonschema:"Existing supplier company primary key."`
	SupplierReference *string `json:"supplier_reference,omitempty" jsonschema:"Optional supplier or external order identifier."`
	Description       *string `json:"description,omitempty" jsonschema:"Optional order description."`
	CreationDate      *string `json:"creation_date,omitempty" jsonschema:"Optional creation date in YYYY-MM-DD form."`
	StartDate         *string `json:"start_date,omitempty" jsonschema:"Optional scheduled start date in YYYY-MM-DD form."`
	TargetDate        *string `json:"target_date,omitempty" jsonschema:"Optional target delivery date in YYYY-MM-DD form."`
	Currency          *string `json:"currency,omitempty" jsonschema:"Optional order currency; the supplier default is used when omitted."`
	DestinationID     *int    `json:"destination_id,omitempty" jsonschema:"Optional receiving stock-location primary key."`
}

type AddPurchaseOrderLineInput struct {
	OrderID        int      `json:"order_id" jsonschema:"Existing purchase-order primary key."`
	SupplierPartID int      `json:"supplier_part_id" jsonschema:"Existing supplier-part primary key."`
	Line           *string  `json:"line,omitempty" jsonschema:"Optional display line number."`
	Reference      *string  `json:"reference,omitempty" jsonschema:"Optional stable line reference."`
	Notes          *string  `json:"notes,omitempty" jsonschema:"Optional line notes."`
	Quantity       float64  `json:"quantity" jsonschema:"Ordered quantity. Must be greater than zero."`
	UnitPrice      *float64 `json:"unit_price,omitempty" jsonschema:"Optional purchase unit price."`
	Currency       *string  `json:"currency,omitempty" jsonschema:"Currency required when unit_price is supplied."`
	TargetDate     *string  `json:"target_date,omitempty" jsonschema:"Optional line target date in YYYY-MM-DD form."`
	DestinationID  *int     `json:"destination_id,omitempty" jsonschema:"Optional receiving stock-location primary key."`
}

type UpdatePurchaseOrderLineInput struct {
	ID             int      `json:"id" jsonschema:"Existing purchase-order line primary key."`
	SupplierPartID *int     `json:"supplier_part_id,omitempty" jsonschema:"Optional replacement supplier-part primary key."`
	Line           *string  `json:"line,omitempty" jsonschema:"Optional replacement display line number."`
	Reference      *string  `json:"reference,omitempty" jsonschema:"Optional replacement stable line reference."`
	Notes          *string  `json:"notes,omitempty" jsonschema:"Optional replacement line notes."`
	Quantity       *float64 `json:"quantity,omitempty" jsonschema:"Optional replacement quantity. Must be greater than zero."`
	UnitPrice      *float64 `json:"unit_price,omitempty" jsonschema:"Optional replacement purchase unit price."`
	Currency       *string  `json:"currency,omitempty" jsonschema:"Optional replacement purchase price currency."`
	TargetDate     *string  `json:"target_date,omitempty" jsonschema:"Optional replacement target date in YYYY-MM-DD form."`
	DestinationID  *int     `json:"destination_id,omitempty" jsonschema:"Optional replacement receiving stock-location primary key."`
}

type PurchaseOrderWorkflowInput struct {
	DryRun            bool                        `json:"dry_run,omitempty" jsonschema:"Return the complete create/update plan without writing."`
	SupplierID        int                         `json:"supplier_id" jsonschema:"Existing supplier company primary key."`
	SupplierReference string                      `json:"supplier_reference" jsonschema:"Stable supplier or external order identifier used with supplier_id for retry recovery."`
	PurchaseOrderID   int                         `json:"purchase_order_id,omitempty" jsonschema:"Existing purchase-order primary key selected after an ambiguous exact retry lookup."`
	Description       *string                     `json:"description,omitempty" jsonschema:"Optional order description."`
	CreationDate      *string                     `json:"creation_date,omitempty" jsonschema:"Optional creation date in YYYY-MM-DD form."`
	StartDate         *string                     `json:"start_date,omitempty" jsonschema:"Optional scheduled start date in YYYY-MM-DD form."`
	TargetDate        *string                     `json:"target_date,omitempty" jsonschema:"Optional target delivery date in YYYY-MM-DD form."`
	Currency          *string                     `json:"currency,omitempty" jsonschema:"Optional order currency."`
	DestinationID     *int                        `json:"destination_id,omitempty" jsonschema:"Optional receiving stock-location primary key."`
	Lines             []PurchaseOrderWorkflowLine `json:"lines" jsonschema:"Validated supplier-part lines to create or update."`
}

type PurchaseOrderWorkflowLine struct {
	PartID         int      `json:"part_id,omitempty" jsonschema:"Existing part primary key when supplier_part_id is omitted."`
	SupplierPartID int      `json:"supplier_part_id,omitempty" jsonschema:"Existing supplier-part primary key."`
	SupplierSKU    string   `json:"supplier_sku,omitempty" jsonschema:"Supplier SKU used with part_id and supplier_id for lookup."`
	Quantity       float64  `json:"quantity" jsonschema:"Ordered quantity. Must be greater than zero."`
	UnitPrice      *float64 `json:"unit_price,omitempty" jsonschema:"Optional purchase unit price."`
	Currency       string   `json:"currency,omitempty" jsonschema:"Currency required when unit_price is supplied."`
	Notes          string   `json:"notes,omitempty" jsonschema:"Optional line notes."`
	TargetDate     *string  `json:"target_date,omitempty" jsonschema:"Optional line target date in YYYY-MM-DD form."`
	DestinationID  *int     `json:"destination_id,omitempty" jsonschema:"Optional receiving stock-location primary key."`
}

type PurchaseOrderWorkflowAction struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	RecordType string `json:"record_type"`
	ID         int    `json:"id,omitempty"`
	Reference  string `json:"reference,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type PurchaseOrderWorkflowFailure struct {
	Action       string `json:"action"`
	Message      string `json:"message"`
	RecoveryPlan string `json:"recovery_plan"`
}

type PurchaseOrderWorkflowOutput struct {
	Status            string                            `json:"status"`
	DryRun            bool                              `json:"dry_run"`
	SupplierReference string                            `json:"supplier_reference"`
	PurchaseOrder     *inventree.PurchaseOrder          `json:"purchase_order,omitempty"`
	Lines             []inventree.PurchaseOrderLineItem `json:"lines,omitempty"`
	Actions           []PurchaseOrderWorkflowAction     `json:"actions"`
	Failure           *PurchaseOrderWorkflowFailure     `json:"failure,omitempty"`
	Clarification     *ClarificationResponse            `json:"clarification,omitempty"`
}

func registerPurchasingLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchPurchaseOrdersToolName, "Search purchase orders", "Searches purchase orders for duplicate checks and recovery.", searchPurchaseOrders(deps))
	addReadOnlyTool(server, deps, GetPurchaseOrderToolName, "Get purchase order", "Retrieves one purchase order by stable ID.", getPurchaseOrder(deps))
	addReadOnlyTool(server, deps, SearchPurchaseOrderLinesToolName, "Search purchase order lines", "Searches purchase-order lines for duplicate checks and recovery.", searchPurchaseOrderLines(deps))
	addReadOnlyTool(server, deps, GetPurchaseOrderLineToolName, "Get purchase order line", "Retrieves one purchase-order line by stable ID.", getPurchaseOrderLine(deps))
}

func registerPurchasingWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, CreatePurchaseOrderToolName, "Create purchase order", "Creates a purchase order for an existing supplier.", createPurchaseOrder(deps))
	addWriteTool(server, deps, AddPurchaseOrderLineToolName, "Add purchase order line", "Adds a validated supplier-part line to an existing purchase order.", addPurchaseOrderLine(deps))
	addWriteTool(server, deps, UpdatePurchaseOrderLineToolName, "Update purchase order line", "Partially updates a purchase-order line after supplier consistency validation.", updatePurchaseOrderLine(deps))
	addWriteTool(server, deps, CreatePurchaseOrderWorkflowToolName, "Create purchase order with lines", "Plans or retry-recoverably creates or updates a purchase order and validated lines.", createPurchaseOrderWithLines(deps))
}

func searchPurchaseOrders(deps Dependencies) mcp.ToolHandlerFor[PurchaseOrderSearchInput, LookupOutput[inventree.PurchaseOrder]] {
	return LookupHandler[PurchaseOrderLookupClient, PurchaseOrderSearchInput, LookupOutput[inventree.PurchaseOrder]](deps, SearchPurchaseOrdersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLookupClient, input PurchaseOrderSearchInput) (*mcp.CallToolResult, LookupOutput[inventree.PurchaseOrder], error) {
			records, err := client.SearchPurchaseOrders(ctx, inventree.PurchaseOrderQuery{Search: input.Search, Supplier: input.SupplierID, Reference: input.Reference, Status: input.Status, StartDateAfter: input.StartDateAfter, StartDateBefore: input.StartDateBefore, TargetDateAfter: input.TargetDateAfter, TargetDateBefore: input.TargetDateBefore, Limit: NormalizeLookupLimit(input.Limit), Offset: input.Offset})
			return listOutput(records, err)
		})
}

func getPurchaseOrder(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.PurchaseOrder]] {
	return LookupHandler[PurchaseOrderLookupClient, IDInput, RecordOutput[inventree.PurchaseOrder]](deps, GetPurchaseOrderToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.PurchaseOrder], error) {
			record, err := client.GetPurchaseOrder(ctx, input.ID)
			return recordOutput(record, err)
		})
}

func searchPurchaseOrderLines(deps Dependencies) mcp.ToolHandlerFor[PurchaseOrderLineSearchInput, LookupOutput[inventree.PurchaseOrderLineItem]] {
	return LookupHandler[PurchaseOrderLookupClient, PurchaseOrderLineSearchInput, LookupOutput[inventree.PurchaseOrderLineItem]](deps, SearchPurchaseOrderLinesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLookupClient, input PurchaseOrderLineSearchInput) (*mcp.CallToolResult, LookupOutput[inventree.PurchaseOrderLineItem], error) {
			records, err := client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{Search: input.Search, Order: input.OrderID, SupplierPart: input.SupplierPartID, Pending: input.Pending, Received: input.Received, Limit: NormalizeLookupLimit(input.Limit), Offset: input.Offset})
			return listOutput(records, err)
		})
}

func getPurchaseOrderLine(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.PurchaseOrderLineItem]] {
	return LookupHandler[PurchaseOrderLookupClient, IDInput, RecordOutput[inventree.PurchaseOrderLineItem]](deps, GetPurchaseOrderLineToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.PurchaseOrderLineItem], error) {
			record, err := client.GetPurchaseOrderLine(ctx, input.ID)
			return recordOutput(record, err)
		})
}

func createPurchaseOrder(deps Dependencies) mcp.ToolHandlerFor[CreatePurchaseOrderInput, WriteRecordOutput[inventree.PurchaseOrder]] {
	return LookupHandler[PurchaseOrderWriteClient, CreatePurchaseOrderInput, WriteRecordOutput[inventree.PurchaseOrder]](deps, CreatePurchaseOrderToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderWriteClient, input CreatePurchaseOrderInput) (*mcp.CallToolResult, WriteRecordOutput[inventree.PurchaseOrder], error) {
			if clarification := validateOrderInput(input.SupplierID, input.CreationDate, input.StartDate, input.TargetDate, input.DestinationID); clarification != nil {
				return TextResult(StatusClarificationRequired), WriteRecordOutput[inventree.PurchaseOrder]{Status: StatusClarificationRequired, Clarification: clarification}, nil
			}
			supplier, err := client.GetCompany(ctx, input.SupplierID)
			if err != nil {
				return writeRecordOutput(inventree.PurchaseOrder{}, err)
			}
			if !supplier.IsSupplier {
				return purchaseOrderClarification("Which supplier should own this purchase order?", "selected company does not have the supplier role", input.SupplierID)
			}
			if input.DestinationID != nil {
				if _, err := client.GetStockLocation(ctx, *input.DestinationID); err != nil {
					return writeRecordOutput(inventree.PurchaseOrder{}, err)
				}
			}
			if strings.TrimSpace(input.Reference) != "" {
				existing, err := client.SearchPurchaseOrders(ctx, inventree.PurchaseOrderQuery{Reference: strings.TrimSpace(input.Reference), Limit: 2})
				if err != nil {
					return nil, WriteRecordOutput[inventree.PurchaseOrder]{}, err
				}
				if len(existing) > 0 {
					clarification := NewClarification("Should the existing purchase order be reused instead of creating a duplicate?", "purchase_order", "purchase-order reference already exists", "purchase_order_id", false, candidatesFor(existing), map[string]any{"reference": strings.TrimSpace(input.Reference)})
					return TextResult(StatusClarificationRequired), WriteRecordOutput[inventree.PurchaseOrder]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
				}
			}
			record, err := client.CreatePurchaseOrder(ctx, purchaseOrderCreate(input.Reference, input.SupplierID, input.SupplierReference, input.Description, input.CreationDate, input.StartDate, input.TargetDate, input.Currency, input.DestinationID))
			return writeRecordOutput(record, err)
		})
}

func addPurchaseOrderLine(deps Dependencies) mcp.ToolHandlerFor[AddPurchaseOrderLineInput, WriteRecordOutput[inventree.PurchaseOrderLineItem]] {
	return LookupHandler[PurchaseOrderLineWriteClient, AddPurchaseOrderLineInput, WriteRecordOutput[inventree.PurchaseOrderLineItem]](deps, AddPurchaseOrderLineToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLineWriteClient, input AddPurchaseOrderLineInput) (*mcp.CallToolResult, WriteRecordOutput[inventree.PurchaseOrderLineItem], error) {
			if clarification := validateLineWrite(input.OrderID, input.SupplierPartID, input.Quantity, input.UnitPrice, input.Currency, input.TargetDate, input.DestinationID); clarification != nil {
				return TextResult(StatusClarificationRequired), WriteRecordOutput[inventree.PurchaseOrderLineItem]{Status: StatusClarificationRequired, Clarification: clarification}, nil
			}
			order, supplierPart, err := loadOrderAndSupplierPart(ctx, client, input.OrderID, input.SupplierPartID)
			if err != nil {
				return writeRecordOutput(inventree.PurchaseOrderLineItem{}, err)
			}
			if order.Supplier != supplierPart.Supplier {
				return purchaseOrderLineClarification("Which supplier part should be added?", "supplier_part does not belong to the purchase-order supplier", input.OrderID, input.SupplierPartID)
			}
			if input.DestinationID != nil {
				if _, err := client.GetStockLocation(ctx, *input.DestinationID); err != nil {
					return writeRecordOutput(inventree.PurchaseOrderLineItem{}, err)
				}
			}
			record, err := client.CreatePurchaseOrderLine(ctx, purchaseOrderLineCreate(input.OrderID, input.SupplierPartID, input.Line, input.Reference, input.Notes, input.Quantity, input.UnitPrice, input.Currency, input.TargetDate, input.DestinationID))
			return writeRecordOutput(record, err)
		})
}

func updatePurchaseOrderLine(deps Dependencies) mcp.ToolHandlerFor[UpdatePurchaseOrderLineInput, WriteRecordOutput[inventree.PurchaseOrderLineItem]] {
	return LookupHandler[PurchaseOrderLineWriteClient, UpdatePurchaseOrderLineInput, WriteRecordOutput[inventree.PurchaseOrderLineItem]](deps, UpdatePurchaseOrderLineToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLineWriteClient, input UpdatePurchaseOrderLineInput) (*mcp.CallToolResult, WriteRecordOutput[inventree.PurchaseOrderLineItem], error) {
			if input.ID <= 0 {
				return purchaseOrderLineClarification("Which purchase-order line should be updated?", "id must be positive", 0, 0)
			}
			if input.Quantity != nil && *input.Quantity <= 0 {
				return purchaseOrderLineClarification("What quantity should be ordered?", "quantity must be greater than zero", 0, 0)
			}
			if input.UnitPrice != nil && (input.Currency == nil || strings.TrimSpace(*input.Currency) == "") {
				return purchaseOrderLineClarification("Which currency applies to this line price?", "currency is required when unit_price is supplied", 0, 0)
			}
			if input.TargetDate != nil && !validDate(*input.TargetDate) {
				return purchaseOrderLineClarification("What target date should be used?", "target_date must use YYYY-MM-DD", 0, 0)
			}
			line, err := client.GetPurchaseOrderLine(ctx, input.ID)
			if err != nil {
				return writeRecordOutput(inventree.PurchaseOrderLineItem{}, err)
			}
			fields := purchaseOrderLinePatch(input)
			if len(fields) == 0 {
				return purchaseOrderLineClarification("Which purchase-order line fields should be updated?", "update_purchase_order_line requires at least one PATCH field", line.Order, line.Part)
			}
			fields["order"] = inventree.Set(line.Order)
			if input.SupplierPartID == nil {
				fields["part"] = inventree.Set(line.Part)
			}
			if input.SupplierPartID != nil {
				order, supplierPart, err := loadOrderAndSupplierPart(ctx, client, line.Order, *input.SupplierPartID)
				if err != nil {
					return writeRecordOutput(inventree.PurchaseOrderLineItem{}, err)
				}
				if order.Supplier != supplierPart.Supplier {
					return purchaseOrderLineClarification("Which supplier part should replace this line?", "supplier_part does not belong to the purchase-order supplier", line.Order, *input.SupplierPartID)
				}
			}
			if input.DestinationID != nil {
				if _, err := client.GetStockLocation(ctx, *input.DestinationID); err != nil {
					return writeRecordOutput(inventree.PurchaseOrderLineItem{}, err)
				}
			}
			record, err := client.UpdatePurchaseOrderLine(ctx, input.ID, fields)
			return writeRecordOutput(record, err)
		})
}

func createPurchaseOrderWithLines(deps Dependencies) mcp.ToolHandlerFor[PurchaseOrderWorkflowInput, PurchaseOrderWorkflowOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input PurchaseOrderWorkflowInput) (*mcp.CallToolResult, PurchaseOrderWorkflowOutput, error) {
		base := PurchaseOrderWorkflowOutput{DryRun: input.DryRun, SupplierReference: strings.TrimSpace(input.SupplierReference)}
		if clarification := validateOrderInput(input.SupplierID, input.CreationDate, input.StartDate, input.TargetDate, input.DestinationID); clarification != nil {
			base.Status = StatusClarificationRequired
			base.Clarification = clarification
			return TextResult(StatusClarificationRequired), base, nil
		}
		if base.SupplierReference == "" || len(base.SupplierReference) > 64 {
			base.Status = StatusClarificationRequired
			clarification := NewClarification("Which supplier reference should identify this purchase order?", "supplier_reference", "supplier_reference is required and must be at most 64 characters", "supplier_reference", true, nil, nil)
			base.Clarification = &clarification
			return TextResult(StatusClarificationRequired), base, nil
		}
		if input.PurchaseOrderID < 0 {
			base.Status = StatusClarificationRequired
			clarification := NewClarification("Which purchase order should be reused?", "purchase_order", "purchase_order_id must be positive when provided", "purchase_order_id", true, nil, nil)
			base.Clarification = &clarification
			return TextResult(StatusClarificationRequired), base, nil
		}
		previewInput := PurchasePreviewInput{SupplierID: input.SupplierID, Lines: make([]PurchasePreviewLineInput, 0, len(input.Lines))}
		for _, line := range input.Lines {
			if line.TargetDate != nil && !validDate(*line.TargetDate) {
				base.Status = StatusClarificationRequired
				clarification := NewClarification("What target date should be used for this purchase-order line?", "target_date", "target_date must use YYYY-MM-DD", "target_date", true, nil, nil)
				base.Clarification = &clarification
				return TextResult(StatusClarificationRequired), base, nil
			}
			previewInput.Lines = append(previewInput.Lines, PurchasePreviewLineInput{PartID: line.PartID, SupplierPartID: line.SupplierPartID, SupplierSKU: line.SupplierSKU, Quantity: line.Quantity, UnitPrice: line.UnitPrice, Currency: line.Currency, Notes: line.Notes})
		}
		previewResult, preview, err := previewPurchaseOrder(deps)(ctx, req, previewInput)
		if err != nil {
			return nil, base, err
		}
		if preview.Status != StatusOK {
			base.Status = preview.Status
			base.Clarification = preview.Clarification
			return previewResult, base, nil
		}
		return LookupHandler[PurchaseOrderWorkflowClient, PurchaseOrderWorkflowInput, PurchaseOrderWorkflowOutput](deps, CreatePurchaseOrderWorkflowToolName,
			func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderWorkflowClient, input PurchaseOrderWorkflowInput) (*mcp.CallToolResult, PurchaseOrderWorkflowOutput, error) {
				return executePurchaseOrderWorkflow(ctx, client, input, preview)
			})(ctx, req, input)
	}
}

func executePurchaseOrderWorkflow(ctx context.Context, client PurchaseOrderWorkflowClient, input PurchaseOrderWorkflowInput, preview PurchasePreviewOutput) (*mcp.CallToolResult, PurchaseOrderWorkflowOutput, error) {
	out := PurchaseOrderWorkflowOutput{Status: StatusOK, DryRun: input.DryRun, SupplierReference: strings.TrimSpace(input.SupplierReference)}
	supplier, err := client.GetCompany(ctx, input.SupplierID)
	if err != nil {
		return nil, out, err
	}
	if !supplier.IsSupplier {
		clarification := NewClarification("Which supplier should own this purchase order?", "supplier", "selected company does not have the supplier role", "supplier_id", true, nil, map[string]any{"supplier_id": input.SupplierID})
		out.Status = StatusClarificationRequired
		out.Clarification = &clarification
		return TextResult(StatusClarificationRequired), out, nil
	}
	if input.DestinationID != nil {
		if _, err := client.GetStockLocation(ctx, *input.DestinationID); err != nil {
			return nil, out, err
		}
	}
	for _, line := range input.Lines {
		if line.DestinationID != nil {
			if _, err := client.GetStockLocation(ctx, *line.DestinationID); err != nil {
				return nil, out, err
			}
		}
	}
	var existing []inventree.PurchaseOrder
	if input.PurchaseOrderID > 0 {
		selected, err := client.GetPurchaseOrder(ctx, input.PurchaseOrderID)
		if err != nil {
			return nil, out, err
		}
		if selected.Supplier != input.SupplierID || selected.SupplierReference != out.SupplierReference {
			clarification := NewClarification("Which purchase order should be reused?", "purchase_order", "purchase_order_id does not match the supplied supplier and supplier_reference", "purchase_order_id", true, candidatesFor([]inventree.PurchaseOrder{selected}), map[string]any{"purchase_order_id": input.PurchaseOrderID, "supplier_id": input.SupplierID, "supplier_reference": out.SupplierReference})
			out.Status = StatusClarificationRequired
			out.Clarification = &clarification
			return TextResult(StatusClarificationRequired), out, nil
		}
		existing = []inventree.PurchaseOrder{selected}
	} else {
		searchResults, err := client.SearchPurchaseOrders(ctx, inventree.PurchaseOrderQuery{Search: out.SupplierReference, Supplier: input.SupplierID, Limit: 100})
		if err != nil {
			return nil, out, err
		}
		existing = exactSupplierReferenceMatches(searchResults, input.SupplierID, out.SupplierReference)
	}
	if len(existing) > 1 {
		clarification := NewClarification("Which purchase order should be reused?", "purchase_order", "multiple orders have the same supplier and supplier_reference", "purchase_order_id", false, candidatesFor(existing), map[string]any{"supplier_reference": out.SupplierReference, "supplier_id": input.SupplierID})
		out.Status = StatusClarificationRequired
		out.Clarification = &clarification
		return TextResult(StatusClarificationRequired), out, nil
	}
	var order inventree.PurchaseOrder
	var orderPatch inventree.PatchFields
	if len(existing) == 1 {
		order = existing[0]
		out.Actions = append(out.Actions, PurchaseOrderWorkflowAction{Name: "reuse_purchase_order", Status: actionStatus(input.DryRun, "reused"), RecordType: "purchase_order", ID: order.PK, Reference: order.Reference, Reason: "exact supplier and supplier_reference match"})
		orderPatch = purchaseOrderWorkflowPatch(input)
	} else {
		out.Actions = append(out.Actions, PurchaseOrderWorkflowAction{Name: "create_purchase_order", Status: pendingActionStatus(input.DryRun), RecordType: "purchase_order", Reason: "InvenTree will generate the internal reference"})
		if !input.DryRun {
			supplierReference := out.SupplierReference
			order, err = client.CreatePurchaseOrder(ctx, purchaseOrderCreate("", input.SupplierID, &supplierReference, input.Description, input.CreationDate, input.StartDate, input.TargetDate, input.Currency, input.DestinationID))
			if err != nil {
				out.Actions[len(out.Actions)-1].Status = "failed"
				return workflowFailure(out, "create_purchase_order", "purchase-order creation failed", "Search purchase orders by the same supplier_id and supplier_reference before retrying because the remote result may be unknown.")
			}
			out.Actions[len(out.Actions)-1].ID = order.PK
			out.Actions[len(out.Actions)-1].Status = "created"
		}
	}
	if order.PK > 0 {
		out.PurchaseOrder = &order
	}
	var existingLines []inventree.PurchaseOrderLineItem
	if order.PK > 0 {
		existingLines, err = client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{Order: order.PK})
		if err != nil {
			return workflowFailure(out, "search_purchase_order_lines", "purchase-order line recovery read failed", "Retry search_purchase_order_lines with the returned purchase_order ID before writing more lines.")
		}
	}
	byReference := make(map[string]inventree.PurchaseOrderLineItem, len(existingLines))
	for _, line := range existingLines {
		if prior, found := byReference[line.Reference]; found && strings.HasPrefix(line.Reference, out.SupplierReference+"-") {
			clarification := NewClarification("Which duplicate purchase-order line reference should be changed before retrying?", "purchase_order_line", "multiple existing lines have the same workflow reference; use update_purchase_order_line to assign one candidate a unique reference, then retry this workflow", "id", true, candidatesFor([]inventree.PurchaseOrderLineItem{prior, line}), map[string]any{"reference": line.Reference, "purchase_order_id": order.PK})
			out.Status = StatusClarificationRequired
			out.Clarification = &clarification
			return TextResult(StatusClarificationRequired), out, nil
		}
		byReference[line.Reference] = line
	}
	for index, previewLine := range preview.Lines {
		lineReference := fmt.Sprintf("%s-%d", out.SupplierReference, index+1)
		existingLine, found := byReference[lineReference]
		if found && existingLine.Part != previewLine.SupplierPartID {
			clarification := NewClarification("Which supplier part should be used for this existing purchase-order line?", "supplier_part", "workflow line reference already belongs to a different supplier part", "supplier_part_id", true, candidatesFor([]inventree.PurchaseOrderLineItem{existingLine}), map[string]any{"line_index": index, "reference": lineReference, "supplier_part_id": previewLine.SupplierPartID})
			out.Status = StatusClarificationRequired
			out.Clarification = &clarification
			return TextResult(StatusClarificationRequired), out, nil
		}
	}
	if len(orderPatch) > 0 {
		out.Actions = append(out.Actions, PurchaseOrderWorkflowAction{Name: "update_purchase_order", Status: pendingActionStatus(input.DryRun), RecordType: "purchase_order", ID: order.PK, Reference: order.Reference})
		if !input.DryRun {
			order, err = client.UpdatePurchaseOrder(ctx, order.PK, orderPatch)
			if err != nil {
				out.Actions[len(out.Actions)-1].Status = "failed"
				return workflowFailure(out, "update_purchase_order", "purchase-order metadata update failed", "Retry with the same supplier_id and supplier_reference; search_purchase_orders can recover the existing order ID.")
			}
			out.Actions[len(out.Actions)-1].Status = "updated"
			out.PurchaseOrder = &order
		}
	}
	for index, previewLine := range preview.Lines {
		lineReference := fmt.Sprintf("%s-%d", out.SupplierReference, index+1)
		inputLine := input.Lines[index]
		existingLine, found := byReference[lineReference]
		if found {
			out.Actions = append(out.Actions, PurchaseOrderWorkflowAction{Name: "update_purchase_order_line", Status: pendingActionStatus(input.DryRun), RecordType: "purchase_order_line", ID: existingLine.PK, Reference: lineReference})
			if input.DryRun {
				continue
			}
			fields := workflowLinePatch(previewLine, inputLine, order.PK, lineReference)
			updated, updateErr := client.UpdatePurchaseOrderLine(ctx, existingLine.PK, fields)
			if updateErr != nil {
				out.Actions[len(out.Actions)-1].Status = "failed"
				return workflowFailure(out, "update_purchase_order_line", "purchase-order line update failed", "Use search_purchase_order_lines with the returned purchase_order ID and line reference, then retry with the same supplier_id and supplier_reference.")
			}
			out.Actions[len(out.Actions)-1].Status = "updated"
			out.Lines = append(out.Lines, updated)
			continue
		}
		out.Actions = append(out.Actions, PurchaseOrderWorkflowAction{Name: "create_purchase_order_line", Status: pendingActionStatus(input.DryRun), RecordType: "purchase_order_line", Reference: lineReference})
		if input.DryRun {
			continue
		}
		created, createErr := client.CreatePurchaseOrderLine(ctx, purchaseOrderLineCreate(order.PK, previewLine.SupplierPartID, nil, &lineReference, &inputLine.Notes, previewLine.Quantity, previewLine.UnitPrice, &previewLine.Currency, inputLine.TargetDate, inputLine.DestinationID))
		if createErr != nil {
			out.Actions[len(out.Actions)-1].Status = "failed"
			return workflowFailure(out, "create_purchase_order_line", "purchase-order line creation failed", "Use search_purchase_order_lines with the returned purchase_order ID and derived line references, then retry with the same supplier_id and supplier_reference.")
		}
		out.Actions[len(out.Actions)-1].ID = created.PK
		out.Actions[len(out.Actions)-1].Status = "created"
		out.Lines = append(out.Lines, created)
	}
	return TextResult(StatusOK), out, nil
}

func validateOrderInput(supplierID int, creationDate, startDate, targetDate *string, destinationID *int) *ClarificationResponse {
	if supplierID <= 0 {
		clarification := NewClarification("Which supplier should own this purchase order?", "supplier", "supplier_id must be positive", "supplier_id", true, nil, map[string]any{"supplier_id": supplierID})
		return &clarification
	}
	for field, value := range map[string]*string{"creation_date": creationDate, "start_date": startDate, "target_date": targetDate} {
		if value != nil && !validDate(*value) {
			clarification := NewClarification("What "+strings.ReplaceAll(field, "_", " ")+" should be used?", field, field+" must use YYYY-MM-DD", field, true, nil, map[string]any{field: *value})
			return &clarification
		}
	}
	if destinationID != nil && *destinationID <= 0 {
		clarification := NewClarification("Which receiving destination should be used?", "destination", "destination_id must be positive when provided", "destination_id", true, nil, map[string]any{"destination_id": *destinationID})
		return &clarification
	}
	return nil
}

func validateLineWrite(orderID, supplierPartID int, quantity float64, unitPrice *float64, currency *string, targetDate *string, destinationID *int) *ClarificationResponse {
	if orderID <= 0 || supplierPartID <= 0 || quantity <= 0 {
		clarification := NewClarification("Which purchase order, supplier part, and positive quantity should be used?", "purchase_order_line", "order_id and supplier_part_id must be positive and quantity must be greater than zero", "order_id", true, nil, map[string]any{"order_id": orderID, "supplier_part_id": supplierPartID, "quantity": quantity})
		return &clarification
	}
	if unitPrice != nil && (currency == nil || strings.TrimSpace(*currency) == "") {
		clarification := NewClarification("Which currency applies to this line price?", "currency", "currency is required when unit_price is supplied", "currency", true, nil, nil)
		return &clarification
	}
	if targetDate != nil && !validDate(*targetDate) {
		clarification := NewClarification("What target date should be used?", "target_date", "target_date must use YYYY-MM-DD", "target_date", true, nil, nil)
		return &clarification
	}
	if destinationID != nil && *destinationID <= 0 {
		clarification := NewClarification("Which receiving destination should be used?", "destination", "destination_id must be positive when provided", "destination_id", true, nil, nil)
		return &clarification
	}
	return nil
}

func validDate(value string) bool {
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func purchaseOrderCreate(reference string, supplierID int, supplierReference, description, creationDate, startDate, targetDate, currency *string, destinationID *int) inventree.PurchaseOrderCreate {
	return inventree.PurchaseOrderCreate{Reference: strings.TrimSpace(reference), Supplier: supplierID, SupplierReference: supplierReference, Description: description, CreationDate: creationDate, StartDate: startDate, TargetDate: targetDate, OrderCurrency: currency, Destination: destinationID}
}

func exactSupplierReferenceMatches(orders []inventree.PurchaseOrder, supplierID int, supplierReference string) []inventree.PurchaseOrder {
	matches := make([]inventree.PurchaseOrder, 0, len(orders))
	for _, order := range orders {
		if order.Supplier == supplierID && order.SupplierReference == supplierReference {
			matches = append(matches, order)
		}
	}
	return matches
}

func purchaseOrderLineCreate(orderID, supplierPartID int, line, reference, notes *string, quantity float64, unitPrice *float64, currency *string, targetDate *string, destinationID *int) inventree.PurchaseOrderLineCreate {
	var price *string
	if unitPrice != nil {
		formatted := strconv.FormatFloat(*unitPrice, 'f', -1, 64)
		price = &formatted
	}
	return inventree.PurchaseOrderLineCreate{Order: orderID, SupplierPart: supplierPartID, Line: line, Reference: reference, Notes: notes, Quantity: quantity, TargetDate: targetDate, PurchasePrice: price, PurchasePriceCurrency: currency, Destination: destinationID}
}

func purchaseOrderLinePatch(input UpdatePurchaseOrderLineInput) inventree.PatchFields {
	fields := inventree.PatchFields{}
	if input.SupplierPartID != nil {
		fields["part"] = inventree.Set(*input.SupplierPartID)
	}
	if input.Line != nil {
		fields["line"] = inventree.Set(*input.Line)
	}
	if input.Reference != nil {
		fields["reference"] = inventree.Set(*input.Reference)
	}
	if input.Notes != nil {
		fields["notes"] = inventree.Set(*input.Notes)
	}
	if input.Quantity != nil {
		fields["quantity"] = inventree.Set(*input.Quantity)
	}
	if input.UnitPrice != nil {
		fields["purchase_price"] = inventree.Set(strconv.FormatFloat(*input.UnitPrice, 'f', -1, 64))
	}
	if input.Currency != nil {
		fields["purchase_price_currency"] = inventree.Set(*input.Currency)
	}
	if input.TargetDate != nil {
		fields["target_date"] = inventree.Set(*input.TargetDate)
	}
	if input.DestinationID != nil {
		fields["destination"] = inventree.Set(*input.DestinationID)
	}
	return fields
}

func purchaseOrderWorkflowPatch(input PurchaseOrderWorkflowInput) inventree.PatchFields {
	fields := inventree.PatchFields{}
	if input.Description != nil {
		fields["description"] = inventree.Set(*input.Description)
	}
	if input.CreationDate != nil {
		fields["creation_date"] = inventree.Set(*input.CreationDate)
	}
	if input.StartDate != nil {
		fields["start_date"] = inventree.Set(*input.StartDate)
	}
	if input.TargetDate != nil {
		fields["target_date"] = inventree.Set(*input.TargetDate)
	}
	if input.Currency != nil {
		fields["order_currency"] = inventree.Set(*input.Currency)
	}
	if input.DestinationID != nil {
		fields["destination"] = inventree.Set(*input.DestinationID)
	}
	return fields
}

func workflowLinePatch(preview PurchasePreviewLineOutput, input PurchaseOrderWorkflowLine, orderID int, reference string) inventree.PatchFields {
	fields := inventree.PatchFields{"order": inventree.Set(orderID), "part": inventree.Set(preview.SupplierPartID), "reference": inventree.Set(reference), "quantity": inventree.Set(preview.Quantity), "notes": inventree.Set(input.Notes)}
	if preview.UnitPrice != nil {
		fields["purchase_price"] = inventree.Set(strconv.FormatFloat(*preview.UnitPrice, 'f', -1, 64))
		fields["purchase_price_currency"] = inventree.Set(preview.Currency)
	}
	if input.TargetDate != nil {
		fields["target_date"] = inventree.Set(*input.TargetDate)
	}
	if input.DestinationID != nil {
		fields["destination"] = inventree.Set(*input.DestinationID)
	}
	return fields
}

func loadOrderAndSupplierPart(ctx context.Context, client PurchaseOrderLineWriteClient, orderID, supplierPartID int) (inventree.PurchaseOrder, inventree.SupplierPart, error) {
	order, err := client.GetPurchaseOrder(ctx, orderID)
	if err != nil {
		return inventree.PurchaseOrder{}, inventree.SupplierPart{}, err
	}
	supplierPart, err := client.GetSupplierPart(ctx, supplierPartID)
	return order, supplierPart, err
}

func purchaseOrderClarification(question, reason string, supplierID int) (*mcp.CallToolResult, WriteRecordOutput[inventree.PurchaseOrder], error) {
	clarification := NewClarification(question, "supplier", reason, "supplier_id", true, nil, map[string]any{"supplier_id": supplierID})
	return TextResult(StatusClarificationRequired), WriteRecordOutput[inventree.PurchaseOrder]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func purchaseOrderLineClarification(question, reason string, orderID, supplierPartID int) (*mcp.CallToolResult, WriteRecordOutput[inventree.PurchaseOrderLineItem], error) {
	clarification := NewClarification(question, "purchase_order_line", reason, "id", true, nil, map[string]any{"order_id": orderID, "supplier_part_id": supplierPartID})
	return TextResult(StatusClarificationRequired), WriteRecordOutput[inventree.PurchaseOrderLineItem]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func workflowFailure(out PurchaseOrderWorkflowOutput, action, message, recovery string) (*mcp.CallToolResult, PurchaseOrderWorkflowOutput, error) {
	out.Status = StatusPartialFailure
	out.Failure = &PurchaseOrderWorkflowFailure{Action: action, Message: message, RecoveryPlan: recovery}
	return TextResult(StatusPartialFailure), out, nil
}

func actionStatus(dryRun bool, executed string) string {
	if dryRun {
		return "planned"
	}
	return executed
}

func pendingActionStatus(dryRun bool) string {
	if dryRun {
		return "planned"
	}
	return "pending"
}
