package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
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

type PurchaseOrderReceiveClient interface {
	GetPart(context.Context, int) (inventree.Part, error)
	GetSupplierPart(context.Context, int) (inventree.SupplierPart, error)
	GetPurchaseOrder(context.Context, int) (inventree.PurchaseOrder, error)
	GetPurchaseOrderLine(context.Context, int) (inventree.PurchaseOrderLineItem, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	ReceivePurchaseOrder(context.Context, int, inventree.PurchaseOrderReceive) ([]inventree.StockItem, error)
}

type PurchaseOrderIssueClient interface {
	GetPurchaseOrder(context.Context, int) (inventree.PurchaseOrder, error)
	SearchPurchaseOrderLines(context.Context, inventree.PurchaseOrderLineQuery) ([]inventree.PurchaseOrderLineItem, error)
	IssuePurchaseOrder(context.Context, int) error
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
	PlannedChanges    []PlannedChange                   `json:"planned_changes,omitempty"`
	Failure           *PurchaseOrderWorkflowFailure     `json:"failure,omitempty"`
	Clarification     *ClarificationResponse            `json:"clarification,omitempty"`
}

type ReceivePurchaseOrderInput struct {
	DryRun         bool                       `json:"dry_run,omitempty" jsonschema:"Validate and return the complete receiving plan without creating stock."`
	ConfirmReceive bool                       `json:"confirm_receive,omitempty" jsonschema:"Required true for the operational write after reviewing a dry run."`
	PlanHash       string                     `json:"plan_hash,omitempty" jsonschema:"Exact plan hash returned by the latest dry run; required with confirm_receive."`
	OrderID        int                        `json:"order_id" jsonschema:"Existing purchase-order primary key."`
	LocationID     *int                       `json:"location_id,omitempty" jsonschema:"Fallback stock-location primary key used when an item and its order line have no destination."`
	Items          []ReceivePurchaseOrderItem `json:"items" jsonschema:"Purchase-order line quantities to receive into newly created stock items."`
}

type ReceivePurchaseOrderItem struct {
	LineItemID    int     `json:"line_item_id" jsonschema:"Existing purchase-order line primary key belonging to order_id."`
	Quantity      float64 `json:"quantity" jsonschema:"Positive quantity no greater than the line's outstanding quantity."`
	LocationID    *int    `json:"location_id,omitempty" jsonschema:"Optional item-specific destination overriding the line and global locations."`
	Status        *int    `json:"status,omitempty" jsonschema:"Optional non-negative InvenTree stock status code; defaults to InvenTree's OK status."`
	BatchCode     *string `json:"batch_code,omitempty" jsonschema:"Optional batch code for the new stock item."`
	ExpiryDate    *string `json:"expiry_date,omitempty" jsonschema:"Optional expiry date in YYYY-MM-DD form."`
	SerialNumbers *string `json:"serial_numbers,omitempty" jsonschema:"Optional InvenTree serial-number expression for tracked stock."`
	Packaging     *string `json:"packaging,omitempty" jsonschema:"Optional non-blank packaging override for the received stock; omitted, empty, or whitespace-only values use the supplier-part packaging."`
	Note          *string `json:"note,omitempty" jsonschema:"Optional note for the received stock."`
	Barcode       *string `json:"barcode,omitempty" jsonschema:"Optional unique barcode for the received stock."`
}

type ReceivePurchaseOrderPlanItem struct {
	LineItemID           int                      `json:"line_item_id"`
	SupplierPartID       int                      `json:"supplier_part_id"`
	PartID               int                      `json:"part_id"`
	OrderedQuantity      float64                  `json:"ordered_quantity"`
	PreviouslyReceived   float64                  `json:"previously_received"`
	OutstandingBefore    float64                  `json:"outstanding_before"`
	ReceiveQuantity      float64                  `json:"receive_quantity"`
	SupplierPackQuantity float64                  `json:"supplier_pack_quantity"`
	BaseStockQuantity    float64                  `json:"base_stock_quantity"`
	SourcePurchasePrice  *inventree.DecimalString `json:"source_purchase_price,omitempty"`
	SourcePriceCurrency  string                   `json:"source_price_currency,omitempty"`
	OutstandingAfter     float64                  `json:"outstanding_after"`
	LocationID           int                      `json:"location_id"`
	Status               *int                     `json:"status,omitempty"`
	BatchCode            *string                  `json:"batch_code,omitempty"`
	ExpiryDate           *string                  `json:"expiry_date,omitempty"`
	SerialNumbers        *string                  `json:"serial_numbers,omitempty"`
	Packaging            *string                  `json:"packaging,omitempty"`
	Note                 *string                  `json:"note,omitempty"`
	Barcode              *string                  `json:"barcode,omitempty"`
}

type ReceivePurchaseOrderOutput struct {
	Status        string                         `json:"status"`
	DryRun        bool                           `json:"dry_run"`
	Order         *inventree.PurchaseOrder       `json:"order,omitempty"`
	Plan          []ReceivePurchaseOrderPlanItem `json:"plan"`
	PlanHash      string                         `json:"plan_hash,omitempty"`
	StockItems    []inventree.StockItem          `json:"stock_items,omitempty"`
	Failure       *PurchaseOrderWorkflowFailure  `json:"failure,omitempty"`
	Clarification *ClarificationResponse         `json:"clarification,omitempty"`
}

type IssuePurchaseOrderInput struct {
	DryRun       bool   `json:"dry_run,omitempty" jsonschema:"Validate and return the issue plan without placing the order."`
	ConfirmIssue bool   `json:"confirm_issue,omitempty" jsonschema:"Required true to place the purchase order with its supplier."`
	PlanHash     string `json:"plan_hash,omitempty" jsonschema:"Exact current-state hash returned by dry_run:true; required with confirm_issue."`
	OrderID      int    `json:"order_id" jsonschema:"Existing pending purchase-order primary key."`
}

type IssuePurchaseOrderOutput struct {
	Status         string                            `json:"status"`
	DryRun         bool                              `json:"dry_run"`
	Order          *inventree.PurchaseOrder          `json:"order,omitempty"`
	Lines          []inventree.PurchaseOrderLineItem `json:"lines,omitempty"`
	Action         string                            `json:"action"`
	PlannedChanges []PlannedChange                   `json:"planned_changes,omitempty"`
	PlanHash       string                            `json:"plan_hash,omitempty"`
	Clarification  *ClarificationResponse            `json:"clarification,omitempty"`
}

type issuePurchaseOrderPlan struct {
	Order        inventree.PurchaseOrder           `json:"order"`
	Lines        []inventree.PurchaseOrderLineItem `json:"lines"`
	TargetStatus int                               `json:"target_status"`
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
	addWriteTool(server, deps, IssuePurchaseOrderToolName, "Issue purchase order", "Plans or explicitly confirms placing a pending purchase order with its supplier.", issuePurchaseOrder(deps))
	addWriteTool(server, deps, ReceivePurchaseOrderToolName, "Receive purchase order items", "Plans or explicitly confirms creation of new stock items from outstanding purchase-order line quantities.", receivePurchaseOrderItems(deps))
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

func receivePurchaseOrderItems(deps Dependencies) mcp.ToolHandlerFor[ReceivePurchaseOrderInput, ReceivePurchaseOrderOutput] {
	return LookupHandler[PurchaseOrderReceiveClient, ReceivePurchaseOrderInput, ReceivePurchaseOrderOutput](deps, ReceivePurchaseOrderToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderReceiveClient, input ReceivePurchaseOrderInput) (*mcp.CallToolResult, ReceivePurchaseOrderOutput, error) {
			out := ReceivePurchaseOrderOutput{Status: StatusOK, DryRun: input.DryRun, Plan: []ReceivePurchaseOrderPlanItem{}}
			if input.OrderID <= 0 || len(input.Items) == 0 {
				return receiveClarification(out, "Which purchase order lines should be received?", "purchase_order_receipt", "order_id must be positive and at least one receipt item is required", "order_id", map[string]any{"order_id": input.OrderID})
			}
			if input.LocationID != nil && *input.LocationID <= 0 {
				return receiveClarification(out, "Which fallback stock location should receive the items?", "location", "location_id must be positive when provided", "location_id", map[string]any{"location_id": *input.LocationID})
			}

			order, err := client.GetPurchaseOrder(ctx, input.OrderID)
			if err != nil {
				return nil, out, err
			}
			out.Order = &order
			if order.Status != inventree.PurchaseOrderStatusPlaced {
				return receiveClarification(out, "Should this purchase order be issued before receiving stock?", "purchase_order", "purchase order must be in PLACED status before items can be received; use issue_purchase_order separately", "order_id", map[string]any{"order_id": input.OrderID, "status": order.Status})
			}
			seenLines := make(map[int]bool, len(input.Items))
			seenBarcodes := make(map[string]bool, len(input.Items))
			locations := map[int]bool{}
			supplierParts := map[int]inventree.SupplierPart{}
			partsBySupplierPart := map[int]inventree.Part{}
			requestItems := make([]inventree.PurchaseOrderReceiveItem, 0, len(input.Items))

			for _, item := range input.Items {
				quantityText, quantityOK := receiveQuantityString(item.Quantity)
				if item.LineItemID <= 0 || !quantityOK {
					return receiveClarification(out, "Which purchase-order line and schema-valid positive quantity should be received?", "purchase_order_line", "line_item_id must be positive and quantity must be finite, greater than zero, and contain at most 10 integer and 5 fractional digits", "line_item_id", map[string]any{"line_item_id": item.LineItemID, "quantity": item.Quantity})
				}
				if seenLines[item.LineItemID] {
					return receiveClarification(out, "Should duplicate receipt rows be combined?", "purchase_order_line", "each line_item_id may appear only once per receiving request", "items", map[string]any{"line_item_id": item.LineItemID})
				}
				seenLines[item.LineItemID] = true
				if item.LocationID != nil && *item.LocationID <= 0 {
					return receiveClarification(out, "Which item-specific stock location should receive this line?", "location", "item location_id must be positive when provided", "items", map[string]any{"line_item_id": item.LineItemID, "location_id": *item.LocationID})
				}
				if item.Status != nil && *item.Status < 0 {
					return receiveClarification(out, "Which stock status should be assigned to the new item?", "status", "status must be a non-negative InvenTree stock status code", "items", map[string]any{"line_item_id": item.LineItemID, "status": *item.Status})
				}
				if item.ExpiryDate != nil && !validDate(*item.ExpiryDate) {
					return receiveClarification(out, "What expiry date should be assigned to the new item?", "expiry_date", "expiry_date must use YYYY-MM-DD", "items", map[string]any{"line_item_id": item.LineItemID, "expiry_date": *item.ExpiryDate})
				}
				if item.Barcode != nil && strings.TrimSpace(*item.Barcode) != "" {
					barcode := strings.TrimSpace(*item.Barcode)
					if seenBarcodes[barcode] {
						return receiveClarification(out, "Which unique barcode should be used for each new stock item?", "barcode", "barcode values must be unique within a receiving request", "items", map[string]any{"barcode": barcode})
					}
					seenBarcodes[barcode] = true
				}

				line, err := client.GetPurchaseOrderLine(ctx, item.LineItemID)
				if err != nil {
					return nil, out, err
				}
				if line.Order != input.OrderID {
					return receiveClarification(out, "Which purchase-order line belongs to this order?", "purchase_order_line", "line_item_id belongs to a different purchase order", "line_item_id", map[string]any{"order_id": input.OrderID, "line_item_id": item.LineItemID, "actual_order_id": line.Order})
				}
				supplierPart, found := supplierParts[line.Part]
				if !found {
					supplierPart, err = client.GetSupplierPart(ctx, line.Part)
					if err != nil {
						return nil, out, err
					}
					supplierParts[line.Part] = supplierPart
				}
				basePart, found := partsBySupplierPart[line.Part]
				if !found {
					basePart, err = client.GetPart(ctx, supplierPart.Part)
					if err != nil {
						return nil, out, err
					}
					partsBySupplierPart[line.Part] = basePart
				}
				if basePart.Virtual {
					return receiveClarification(out, "Should this virtual part line be handled outside stock receiving?", "part", "virtual parts do not create stock items when received and are excluded from the new-stock-only workflow", "line_item_id", map[string]any{"line_item_id": item.LineItemID, "supplier_part_id": line.Part, "part_id": basePart.PK})
				}
				packQuantity := supplierPart.PackQuantityNative
				if packQuantity <= 0 {
					packQuantity = 1
				}
				var resolvedPackaging *string
				if item.Packaging != nil && strings.TrimSpace(*item.Packaging) != "" {
					packaging := strings.TrimSpace(*item.Packaging)
					resolvedPackaging = &packaging
				} else {
					resolvedPackaging = supplierPart.Packaging
				}
				outstanding := line.Quantity - line.Received
				if outstanding <= 0 || item.Quantity > outstanding+1e-9 {
					return receiveClarification(out, "What quantity should be received from this outstanding line?", "quantity", "receive quantity must not exceed the line's outstanding quantity", "items", map[string]any{"line_item_id": item.LineItemID, "quantity": item.Quantity, "outstanding_quantity": outstanding})
				}
				locationID := 0
				switch {
				case item.LocationID != nil:
					locationID = *item.LocationID
				case line.Destination != nil:
					locationID = *line.Destination
				case input.LocationID != nil:
					locationID = *input.LocationID
				default:
					return receiveClarification(out, "Which stock location should receive this line?", "location", "no item, purchase-order line, or global receiving location is available", "location_id", map[string]any{"line_item_id": item.LineItemID})
				}
				if !locations[locationID] {
					if _, err := client.GetStockLocation(ctx, locationID); err != nil {
						return nil, out, err
					}
					locations[locationID] = true
				}
				out.Plan = append(out.Plan, ReceivePurchaseOrderPlanItem{
					LineItemID: item.LineItemID, SupplierPartID: line.Part, PartID: basePart.PK, OrderedQuantity: line.Quantity, PreviouslyReceived: line.Received,
					OutstandingBefore: outstanding, ReceiveQuantity: item.Quantity, SupplierPackQuantity: packQuantity,
					BaseStockQuantity: item.Quantity * packQuantity, SourcePurchasePrice: line.PurchasePrice,
					SourcePriceCurrency: line.PurchasePriceCurrency, OutstandingAfter: outstanding - item.Quantity,
					LocationID: locationID, Status: item.Status, BatchCode: item.BatchCode, ExpiryDate: item.ExpiryDate,
					SerialNumbers: item.SerialNumbers, Packaging: resolvedPackaging, Note: item.Note, Barcode: item.Barcode,
				})
				resolvedLocation := locationID
				requestItems = append(requestItems, inventree.PurchaseOrderReceiveItem{
					LineItem: item.LineItemID, Location: &resolvedLocation, Quantity: quantityText,
					BatchCode: item.BatchCode, ExpiryDate: item.ExpiryDate, SerialNumbers: item.SerialNumbers,
					Status: item.Status, Packaging: resolvedPackaging, Note: item.Note, Barcode: item.Barcode,
				})
			}

			if input.DryRun {
				planHash, err := receivePlanHash(input.OrderID, order.Status, out.Plan)
				if err != nil {
					return nil, out, err
				}
				out.PlanHash = planHash
				return TextResult(StatusOK), out, nil
			}
			if !input.ConfirmReceive {
				return receiveClarification(out, "Should a dry-run plan be reviewed before these quantities are received?", "confirmation", "run with dry_run:true first, then provide its plan_hash with confirm_receive:true", "dry_run", map[string]any{"order_id": input.OrderID, "dry_run": true})
			}
			planHash, err := receivePlanHash(input.OrderID, order.Status, out.Plan)
			if err != nil {
				return nil, out, err
			}
			if input.PlanHash == "" || input.PlanHash != planHash {
				return receiveClarification(out, "Which current dry-run plan should authorize this receipt?", "confirmation", "plan_hash must match the latest dry-run plan for the current order and line state", "plan_hash", map[string]any{"order_id": input.OrderID, "dry_run": true})
			}
			out.PlanHash = planHash
			stockItems, err := client.ReceivePurchaseOrder(ctx, input.OrderID, inventree.PurchaseOrderReceive{Items: requestItems})
			if err != nil {
				var apiErr *inventree.APIError
				if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
					return nil, out, err
				}
				return receiveUnknownResult(out, "purchase-order receipt result is unknown")
			}
			out.StockItems = stockItems
			if len(stockItems) == 0 {
				return receiveUnknownResult(out, "purchase-order receipt returned no stock items")
			}
			refreshed, err := client.GetPurchaseOrder(ctx, input.OrderID)
			if err != nil {
				return receiveUnknownResult(out, "purchase-order receipt succeeded but the refreshed order state is unavailable")
			}
			out.Order = &refreshed
			return TextResult(StatusOK), out, nil
		})
}

func issuePurchaseOrder(deps Dependencies) mcp.ToolHandlerFor[IssuePurchaseOrderInput, IssuePurchaseOrderOutput] {
	return LookupHandler[PurchaseOrderIssueClient, IssuePurchaseOrderInput, IssuePurchaseOrderOutput](deps, IssuePurchaseOrderToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderIssueClient, input IssuePurchaseOrderInput) (*mcp.CallToolResult, IssuePurchaseOrderOutput, error) {
			out := IssuePurchaseOrderOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.OrderID <= 0 {
				clarification := NewClarification("Which pending purchase order should be issued?", "purchase_order", "order_id must be positive", "order_id", true, nil, map[string]any{"order_id": input.OrderID})
				out.Status = StatusClarificationRequired
				out.Clarification = &clarification
				return TextResult(StatusClarificationRequired), out, nil
			}
			order, err := client.GetPurchaseOrder(ctx, input.OrderID)
			if err != nil {
				return nil, out, err
			}
			out.Order = &order
			if order.Status == inventree.PurchaseOrderStatusPlaced {
				out.Action = "already_placed"
				return TextResult(StatusOK), out, nil
			}
			if order.Status != inventree.PurchaseOrderStatusPending {
				clarification := NewClarification("Which pending purchase order should be issued?", "purchase_order", "only a PENDING purchase order can be issued by this tool", "order_id", true, candidatesFor([]inventree.PurchaseOrder{order}), map[string]any{"order_id": input.OrderID, "status": order.Status})
				out.Status = StatusClarificationRequired
				out.Clarification = &clarification
				return TextResult(StatusClarificationRequired), out, nil
			}
			lines, err := client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{Order: order.PK})
			if err != nil {
				return nil, out, err
			}
			sort.Slice(lines, func(i, j int) bool { return lines[i].PK < lines[j].PK })
			out.Lines = lines
			planHash, err := issuePlanHash(issuePurchaseOrderPlan{Order: order, Lines: lines, TargetStatus: inventree.PurchaseOrderStatusPlaced})
			if err != nil {
				return nil, out, err
			}
			out.Action = "issue_purchase_order"
			if input.DryRun {
				out.PlanHash = planHash
				out.PlannedChanges = append(out.PlannedChanges, plannedChange("issue_purchase_order", "purchase_order", order.PK, map[string]any{"status": inventree.PurchaseOrderStatusPlaced}))
				return TextResult(StatusOK), out, nil
			}
			if !input.ConfirmIssue {
				clarification := NewClarification("Should this purchase order now be placed with its supplier?", "confirmation", "confirm_issue must be true after reviewing the current-state issue plan", "dry_run", true, nil, map[string]any{"order_id": input.OrderID, "dry_run": true})
				out.Status = StatusClarificationRequired
				out.Clarification = &clarification
				return TextResult(StatusClarificationRequired), out, nil
			}
			if input.PlanHash == "" || input.PlanHash != planHash {
				clarification := NewClarification("Which current issue plan should authorize placing this purchase order?", "plan_hash", "plan_hash must match a dry run for the current order metadata and purchase-order lines", "dry_run", true, nil, map[string]any{"order_id": input.OrderID, "dry_run": true})
				out.Status = StatusClarificationRequired
				out.Clarification = &clarification
				return TextResult(StatusClarificationRequired), out, nil
			}
			if err := client.IssuePurchaseOrder(ctx, input.OrderID); err != nil {
				return nil, out, err
			}
			issued, err := client.GetPurchaseOrder(ctx, input.OrderID)
			if err != nil {
				return nil, out, err
			}
			out.Order = &issued
			return TextResult(StatusOK), out, nil
		})
}

func issuePlanHash(plan issuePurchaseOrderPlan) (string, error) {
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func receiveClarification(out ReceivePurchaseOrderOutput, question, subject, reason, retry string, fields map[string]any) (*mcp.CallToolResult, ReceivePurchaseOrderOutput, error) {
	clarification := NewClarification(question, subject, reason, retry, true, nil, fields)
	out.Status = StatusClarificationRequired
	out.Clarification = &clarification
	return TextResult(StatusClarificationRequired), out, nil
}

func receiveUnknownResult(out ReceivePurchaseOrderOutput, message string) (*mcp.CallToolResult, ReceivePurchaseOrderOutput, error) {
	out.Status = StatusPartialFailure
	out.Failure = &PurchaseOrderWorkflowFailure{
		Action:       ReceivePurchaseOrderToolName,
		Message:      message,
		RecoveryPlan: "Do not retry the receipt blindly. Read every purchase-order line and call search_stock_items with purchase_order_id to determine whether the mutation succeeded before preparing a new dry-run plan.",
	}
	return TextResult(StatusPartialFailure), out, nil
}

func receiveQuantityString(quantity float64) (string, bool) {
	if math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity <= 0 {
		return "", false
	}
	formatted := strconv.FormatFloat(quantity, 'f', -1, 64)
	parts := strings.SplitN(formatted, ".", 2)
	if len(parts[0]) > 10 || (len(parts) == 2 && len(parts[1]) > 5) {
		return "", false
	}
	return formatted, true
}

func receivePlanHash(orderID, orderStatus int, plan []ReceivePurchaseOrderPlanItem) (string, error) {
	payload, err := json.Marshal(struct {
		OrderID     int                            `json:"order_id"`
		OrderStatus int                            `json:"order_status"`
		Plan        []ReceivePurchaseOrderPlanItem `json:"plan"`
	}{OrderID: orderID, OrderStatus: orderStatus, Plan: plan})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
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
		if input.DryRun {
			fields := map[string]any{"supplier": input.SupplierID, "supplier_reference": out.SupplierReference}
			setOptionalField(fields, "description", input.Description)
			setOptionalField(fields, "creation_date", input.CreationDate)
			setOptionalField(fields, "start_date", input.StartDate)
			setOptionalField(fields, "target_date", input.TargetDate)
			setOptionalField(fields, "order_currency", input.Currency)
			setOptionalField(fields, "destination", input.DestinationID)
			out.PlannedChanges = append(out.PlannedChanges, plannedChange("create_purchase_order", "purchase_order", 0, fields))
		}
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
		if input.DryRun {
			out.PlannedChanges = append(out.PlannedChanges, plannedChange("update_purchase_order", "purchase_order", order.PK, patchFieldValues(orderPatch)))
		}
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
				fields := workflowLinePatch(previewLine, inputLine, order.PK, lineReference)
				out.PlannedChanges = append(out.PlannedChanges, plannedChange("update_purchase_order_line", "purchase_order_line", existingLine.PK, patchFieldValues(fields)))
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
			fields := map[string]any{"part": previewLine.SupplierPartID, "reference": lineReference, "notes": inputLine.Notes, "quantity": previewLine.Quantity}
			dependencies := []PlannedChangeDependency{}
			setPlannedReference(fields, "order", order.PK, "create_purchase_order", &dependencies)
			fields["purchase_price_currency"] = previewLine.Currency
			fields["auto_pricing"] = false
			fields["merge_items"] = false
			if previewLine.UnitPrice != nil {
				fields["purchase_price"] = strconv.FormatFloat(*previewLine.UnitPrice, 'f', -1, 64)
			}
			setOptionalField(fields, "target_date", inputLine.TargetDate)
			setOptionalField(fields, "destination", inputLine.DestinationID)
			out.PlannedChanges = append(out.PlannedChanges, plannedChangeWithDependencies("create_purchase_order_line", "purchase_order_line", 0, fields, dependencies))
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
