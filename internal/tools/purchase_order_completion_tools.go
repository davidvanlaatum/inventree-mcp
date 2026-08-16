package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PurchaseOrderCompleteClient interface {
	GetPurchaseOrder(context.Context, int) (inventree.PurchaseOrder, error)
	SearchPurchaseOrderLines(context.Context, inventree.PurchaseOrderLineQuery) ([]inventree.PurchaseOrderLineItem, error)
	CompletePurchaseOrder(context.Context, int) error
}

type CompletePurchaseOrderInput struct {
	DryRun          bool   `json:"dry_run,omitempty" jsonschema:"Validate and return the complete current-state completion plan without changing the order."`
	ConfirmComplete bool   `json:"confirm_complete,omitempty" jsonschema:"Required true to complete the fully received purchase order after reviewing a dry run."`
	PlanHash        string `json:"plan_hash,omitempty" jsonschema:"Exact current-state hash returned by dry_run:true; required with confirm_complete."`
	OrderID         int    `json:"order_id" jsonschema:"Existing fully received purchase-order primary key."`
}

type CompletePurchaseOrderOutput struct {
	Status         string                            `json:"status"`
	DryRun         bool                              `json:"dry_run"`
	Order          *inventree.PurchaseOrder          `json:"order,omitempty"`
	Lines          []inventree.PurchaseOrderLineItem `json:"lines,omitempty"`
	Action         string                            `json:"action"`
	PlannedChanges []PlannedChange                   `json:"planned_changes,omitempty"`
	PlanHash       string                            `json:"plan_hash,omitempty"`
	Recovered      bool                              `json:"recovered,omitempty"`
	Failure        *PurchaseOrderWorkflowFailure     `json:"failure,omitempty"`
	Clarification  *ClarificationResponse            `json:"clarification,omitempty"`
}

type completePurchaseOrderPlan struct {
	Order        inventree.PurchaseOrder           `json:"order"`
	Lines        []inventree.PurchaseOrderLineItem `json:"lines"`
	TargetStatus int                               `json:"target_status"`
}

func completePurchaseOrder(deps Dependencies) mcp.ToolHandlerFor[CompletePurchaseOrderInput, CompletePurchaseOrderOutput] {
	return LookupHandler[PurchaseOrderCompleteClient, CompletePurchaseOrderInput, CompletePurchaseOrderOutput](deps, CompletePurchaseOrderToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderCompleteClient, input CompletePurchaseOrderInput) (*mcp.CallToolResult, CompletePurchaseOrderOutput, error) {
			out := CompletePurchaseOrderOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.OrderID <= 0 {
				return completePurchaseOrderClarification(out, "Which fully received purchase order should be completed?", "order_id must be positive", "order_id", map[string]any{"order_id": input.OrderID})
			}

			order, err := client.GetPurchaseOrder(ctx, input.OrderID)
			if err != nil {
				return nil, out, err
			}
			out.Order = &order
			if order.Status == inventree.PurchaseOrderStatusComplete {
				out.Action = "already_complete"
				return TextResult(StatusOK), out, nil
			}
			if order.Status != inventree.PurchaseOrderStatusPlaced {
				return completePurchaseOrderClarification(out, "Which placed purchase order should be completed?", "only a PLACED purchase order can be completed", "order_id", map[string]any{"order_id": input.OrderID, "status": order.Status})
			}

			lines, err := client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{Order: input.OrderID})
			if err != nil {
				return nil, out, err
			}
			sort.Slice(lines, func(i, j int) bool { return lines[i].PK < lines[j].PK })
			lines = sanitizePurchaseOrderLines(lines)
			out.Lines = lines
			if line, outstanding, found := firstOutstandingPurchaseOrderLine(lines); found {
				return completePurchaseOrderClarification(out, "Should the outstanding purchase-order lines be received before completion?", "purchase order still has outstanding ordinary line quantity; incomplete completion is not supported", "line_item_id", map[string]any{"order_id": input.OrderID, "line_item_id": line.PK, "outstanding_quantity": outstanding})
			}

			planHash, err := completePurchaseOrderPlanHash(completePurchaseOrderPlan{Order: order, Lines: lines, TargetStatus: inventree.PurchaseOrderStatusComplete})
			if err != nil {
				return nil, out, err
			}
			out.Action = CompletePurchaseOrderToolName
			if input.DryRun {
				out.PlanHash = planHash
				out.PlannedChanges = []PlannedChange{plannedChange(CompletePurchaseOrderToolName, "purchase_order", order.PK, map[string]any{"status": inventree.PurchaseOrderStatusComplete, "accept_incomplete": false})}
				return TextResult(StatusOK), out, nil
			}
			if !input.ConfirmComplete {
				return completePurchaseOrderClarification(out, "Should this fully received purchase order now be completed?", "confirm_complete must be true after reviewing the current-state completion plan", "dry_run", map[string]any{"order_id": input.OrderID, "dry_run": true})
			}
			if input.PlanHash == "" || input.PlanHash != planHash {
				return completePurchaseOrderClarification(out, "Which current completion plan should authorize this status transition?", "plan_hash must match a dry run for the current order metadata and every ordinary line", "dry_run", map[string]any{"order_id": input.OrderID, "dry_run": true})
			}

			mutationErr := client.CompletePurchaseOrder(ctx, input.OrderID)
			if mutationErr != nil {
				var apiErr *inventree.APIError
				if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
					return nil, out, mutationErr
				}
			}
			refreshed, readErr := client.GetPurchaseOrder(ctx, input.OrderID)
			if readErr != nil {
				return completePurchaseOrderUnknown(out, "purchase-order completion result is unknown because refreshed order state is unavailable")
			}
			out.Order = &refreshed
			if refreshed.Status != inventree.PurchaseOrderStatusComplete {
				return completePurchaseOrderUnknown(out, "purchase-order completion did not produce verified COMPLETE state")
			}
			out.Recovered = mutationErr != nil
			return TextResult(StatusOK), out, nil
		})
}

func firstOutstandingPurchaseOrderLine(lines []inventree.PurchaseOrderLineItem) (inventree.PurchaseOrderLineItem, float64, bool) {
	for _, line := range lines {
		if outstanding := line.Quantity - line.Received; outstanding > 1e-9 {
			return line, outstanding, true
		}
	}
	return inventree.PurchaseOrderLineItem{}, 0, false
}

func completePurchaseOrderPlanHash(plan completePurchaseOrderPlan) (string, error) {
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func completePurchaseOrderClarification(out CompletePurchaseOrderOutput, question, reason, retry string, fields map[string]any) (*mcp.CallToolResult, CompletePurchaseOrderOutput, error) {
	clarification := NewClarification(question, "purchase_order", reason, retry, true, nil, fields)
	out.Status = StatusClarificationRequired
	out.Clarification = &clarification
	return TextResult(StatusClarificationRequired), out, nil
}

func completePurchaseOrderUnknown(out CompletePurchaseOrderOutput, message string) (*mcp.CallToolResult, CompletePurchaseOrderOutput, error) {
	out.Status = StatusPartialFailure
	out.Failure = &PurchaseOrderWorkflowFailure{
		Action:       CompletePurchaseOrderToolName,
		Message:      message,
		RecoveryPlan: "Do not repeat any purchase-order receipt. Read the purchase order and every ordinary line; if the order is not COMPLETE and all lines remain fully received, prepare a fresh complete_purchase_order dry-run plan.",
	}
	return TextResult(StatusPartialFailure), out, nil
}
