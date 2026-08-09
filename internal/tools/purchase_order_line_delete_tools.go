package tools

import (
	"context"
	"fmt"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// InvenTree's DELETE /api/order/po-line/{id}/ endpoint does not itself refuse
// to remove a line that already has received quantity, nor does it restrict
// deletion by purchase-order status: deleting a line on a PLACED order, or one
// with received > 0, succeeds upstream and silently orphans any StockItem
// that was already receipted against it (the item survives with its
// purchase_order still set, but the line it came from is gone). This tool
// therefore enforces the zero-received and no-linked-stock guard itself
// rather than relying on upstream to reject the request.
type PurchaseOrderLineDeleteClient interface {
	GetPurchaseOrder(context.Context, int) (inventree.PurchaseOrder, error)
	GetPurchaseOrderLine(context.Context, int) (inventree.PurchaseOrderLineItem, error)
	GetSupplierPart(context.Context, int) (inventree.SupplierPart, error)
	SearchStockItems(context.Context, inventree.StockItemQuery) ([]inventree.StockItem, error)
	DeletePurchaseOrderLine(context.Context, int) error
}

type DeletePurchaseOrderLineInput struct {
	ID      int  `json:"id" jsonschema:"Stable purchase-order line primary key."`
	Confirm bool `json:"confirm,omitempty" jsonschema:"Required true after reviewing the exact current line, order, and receipt state."`
}

type PurchaseOrderLineDeleteOutput struct {
	Status        string                           `json:"status"`
	Record        *inventree.PurchaseOrderLineItem `json:"record,omitempty"`
	PurchaseOrder *inventree.PurchaseOrder         `json:"purchase_order,omitempty"`
	PartID        int                              `json:"part_id,omitempty"`
	Verified      bool                             `json:"verified,omitempty"`
	Validation    *ValidationFailure               `json:"validation,omitempty"`
	RecoveryPlan  string                           `json:"recovery_plan,omitempty"`
	Clarification *ClarificationResponse           `json:"clarification,omitempty"`
}

func registerPurchaseOrderLineDeleteTool(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, DeletePurchaseOrderLineToolName, "Delete purchase order line", "Previews and explicitly deletes one unreceived ordinary purchase-order line with read-back verification.", deletePurchaseOrderLine(deps))
}

func deletePurchaseOrderLine(deps Dependencies) mcp.ToolHandlerFor[DeletePurchaseOrderLineInput, PurchaseOrderLineDeleteOutput] {
	return LookupHandler[PurchaseOrderLineDeleteClient, DeletePurchaseOrderLineInput, PurchaseOrderLineDeleteOutput](deps, DeletePurchaseOrderLineToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLineDeleteClient, input DeletePurchaseOrderLineInput) (*mcp.CallToolResult, PurchaseOrderLineDeleteOutput, error) {
			if input.ID <= 0 {
				return purchaseOrderLineDeleteClarification("Which purchase-order line should be deleted?", "id", "id must be positive", "id", true, nil, map[string]any{"id": input.ID})
			}
			record, err := client.GetPurchaseOrderLine(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PurchaseOrderLineDeleteOutput{Status: StatusNotFound}, nil
			}
			if err != nil || record.PK != input.ID {
				return nil, PurchaseOrderLineDeleteOutput{}, safePurchaseOrderLineDeleteError("purchase-order line delete lookup")
			}
			order, err := loadPurchaseOrderLineDeleteOrder(ctx, client, record.Order)
			if err != nil {
				return nil, PurchaseOrderLineDeleteOutput{}, err
			}
			partID := 0
			if record.Part > 0 {
				supplierPart, err := client.GetSupplierPart(ctx, record.Part)
				if err != nil {
					return nil, PurchaseOrderLineDeleteOutput{}, err
				}
				if supplierPart.PK != record.Part {
					return nil, PurchaseOrderLineDeleteOutput{}, safePurchaseOrderLineDeleteError("supplier-part identity verification")
				}
				partID = supplierPart.Part
			}
			if record.Received != 0 {
				return purchaseOrderLineUnsafeClarification(record, order, partID, "received", "the line has received quantity; InvenTree does not block this deletion upstream, and completing it would silently orphan received stock provenance")
			}
			linked, err := purchaseOrderLineHasLinkedStock(ctx, client, record)
			if err != nil {
				return nil, PurchaseOrderLineDeleteOutput{}, err
			}
			if linked {
				return purchaseOrderLineUnsafeClarification(record, order, partID, "stock", "stock items already reference this purchase order and supplier part; deletion would silently orphan linked stock provenance")
			}
			if !input.Confirm {
				clarification := NewClarification("Delete this unreceived purchase-order line?", "confirm", "delete_purchase_order_line requires confirm:true after reviewing the exact current line, order, and receipt state", "confirm", true, candidatesFor([]inventree.PurchaseOrderLineItem{record}), map[string]any{"id": input.ID, "confirm": true})
				return TextResult(StatusClarificationRequired), PurchaseOrderLineDeleteOutput{Status: StatusClarificationRequired, Record: &record, PurchaseOrder: &order, PartID: partID, Clarification: &clarification}, nil
			}
			if err := client.DeletePurchaseOrderLine(ctx, input.ID); err != nil {
				if validation, ok := safeValidationFailure(err); ok {
					return TextResult(StatusValidationFailed), PurchaseOrderLineDeleteOutput{Status: StatusValidationFailed, Validation: validation}, nil
				}
				return nil, PurchaseOrderLineDeleteOutput{}, safePurchaseOrderLineDeleteError("purchase-order line deletion")
			}
			_, err = client.GetPurchaseOrderLine(ctx, input.ID)
			if err == nil {
				return purchaseOrderLineDeletePartial(record, "purchase-order line still exists after deletion; read the exact stable ID before retrying")
			}
			if !isNotFound(err) {
				return purchaseOrderLineDeletePartial(record, "deletion read-back could not prove absence; read the exact stable ID before retrying")
			}
			refreshedOrder, err := loadPurchaseOrderLineDeleteOrder(ctx, client, order.PK)
			if err != nil {
				return purchaseOrderLineDeletePartial(record, "purchase-order line was deleted but the refreshed purchase-order total is unavailable; read the purchase order before continuing")
			}
			return TextResult(StatusOK), PurchaseOrderLineDeleteOutput{Status: StatusOK, Record: &record, PurchaseOrder: &refreshedOrder, PartID: partID, Verified: true}, nil
		})
}

func purchaseOrderLineHasLinkedStock(ctx context.Context, client PurchaseOrderLineDeleteClient, line inventree.PurchaseOrderLineItem) (bool, error) {
	items, err := client.SearchStockItems(ctx, inventree.StockItemQuery{PurchaseOrderID: line.Order})
	if err != nil {
		return false, safePurchaseOrderLineDeleteError("linked-stock provenance preflight")
	}
	for _, item := range items {
		if item.SupplierPart != nil && *item.SupplierPart == line.Part {
			return true, nil
		}
	}
	return false, nil
}

func loadPurchaseOrderLineDeleteOrder(ctx context.Context, client PurchaseOrderLineDeleteClient, id int) (inventree.PurchaseOrder, error) {
	order, err := client.GetPurchaseOrder(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return inventree.PurchaseOrder{}, err
		}
		return inventree.PurchaseOrder{}, safePurchaseOrderLineDeleteError("purchase-order lookup and identity verification")
	}
	if order.PK != id {
		return inventree.PurchaseOrder{}, safePurchaseOrderLineDeleteError("purchase-order lookup and identity verification")
	}
	return order, nil
}

func purchaseOrderLineUnsafeClarification(record inventree.PurchaseOrderLineItem, order inventree.PurchaseOrder, partID int, field, reason string) (*mcp.CallToolResult, PurchaseOrderLineDeleteOutput, error) {
	clarification := NewClarification("Which safe action addresses this purchase-order line instead of deleting it?", field, reason, "id", true, candidatesFor([]inventree.PurchaseOrderLineItem{record}), map[string]any{"id": record.PK})
	return TextResult(StatusClarificationRequired), PurchaseOrderLineDeleteOutput{Status: StatusClarificationRequired, Record: &record, PurchaseOrder: &order, PartID: partID, Clarification: &clarification}, nil
}

func purchaseOrderLineDeleteClarification(question, field, reason, retry string, hardError bool, candidates []ClarificationCandidate, retryValues map[string]any) (*mcp.CallToolResult, PurchaseOrderLineDeleteOutput, error) {
	clarification := NewClarification(question, field, reason, retry, hardError, candidates, retryValues)
	return TextResult(StatusClarificationRequired), PurchaseOrderLineDeleteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func purchaseOrderLineDeletePartial(record inventree.PurchaseOrderLineItem, recovery string) (*mcp.CallToolResult, PurchaseOrderLineDeleteOutput, error) {
	return TextResult(StatusPartialFailure), PurchaseOrderLineDeleteOutput{Status: StatusPartialFailure, Record: &record, RecoveryPlan: recovery}, nil
}

func safePurchaseOrderLineDeleteError(operation string) error {
	return fmt.Errorf("%s failed; inspect InvenTree availability and permissions before retrying", operation)
}
