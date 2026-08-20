package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PurchaseOrderLifecycleClient is the narrow client surface needed to plan
// and execute purchase-order hold, resume, and cancel transitions. InvenTree
// 1.5.1/API 530 validates almost none of these transitions itself (hold and
// issue succeed unconditionally from PENDING or PLACED and silently no-op on
// a CANCELLED order; cancel succeeds even with partially received stock and
// is refused only from COMPLETE), so every allowed-source-state and
// received-quantity guard below is enforced at this layer rather than
// relying on upstream refusal.
type PurchaseOrderLifecycleClient interface {
	GetPurchaseOrder(context.Context, int) (inventree.PurchaseOrder, error)
	SearchPurchaseOrderLines(context.Context, inventree.PurchaseOrderLineQuery) ([]inventree.PurchaseOrderLineItem, error)
	SearchPurchaseOrderExtraLinesPage(context.Context, inventree.PurchaseOrderExtraLineQuery) (inventree.Page[inventree.PurchaseOrderExtraLine], error)
	GetPurchaseOrderExtraLine(context.Context, int) (inventree.PurchaseOrderExtraLine, error)
	HoldPurchaseOrder(context.Context, int) error
	IssuePurchaseOrder(context.Context, int) error
	CancelPurchaseOrder(context.Context, int) error
}

type HoldPurchaseOrderInput struct {
	DryRun   bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound hold plan without writing."`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Required true to place the reviewed purchase order on hold."`
	PlanHash string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound, single-use confirmation token returned by dry_run:true."`
	OrderID  int    `json:"order_id" jsonschema:"Existing pending or placed purchase-order primary key."`
}

type ResumePurchaseOrderInput struct {
	DryRun   bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound resume plan without writing."`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Required true to resume the reviewed on-hold purchase order."`
	PlanHash string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound, single-use confirmation token returned by dry_run:true."`
	OrderID  int    `json:"order_id" jsonschema:"Existing on-hold purchase-order primary key. Resuming always transitions the order to PLACED, placing it with its supplier; InvenTree has no native transition back to PENDING."`
}

type CancelPurchaseOrderInput struct {
	DryRun   bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound cancellation plan without writing."`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Required true to cancel the reviewed purchase order."`
	PlanHash string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound, single-use confirmation token returned by dry_run:true."`
	OrderID  int    `json:"order_id" jsonschema:"Existing pending, on-hold, or placed purchase-order primary key. Cancellation is refused while any ordinary line has received quantity greater than zero."`
}

type PurchaseOrderLifecycleOutput struct {
	Status         string                             `json:"status"`
	DryRun         bool                               `json:"dry_run"`
	Order          *inventree.PurchaseOrder           `json:"order,omitempty"`
	Lines          []inventree.PurchaseOrderLineItem  `json:"lines,omitempty"`
	ExtraLines     []inventree.PurchaseOrderExtraLine `json:"extra_lines,omitempty"`
	Action         string                             `json:"action"`
	PlannedChanges []PlannedChange                    `json:"planned_changes,omitempty"`
	PlanHash       string                             `json:"plan_hash,omitempty"`
	Warning        string                             `json:"warning,omitempty"`
	Recovered      bool                               `json:"recovered,omitempty"`
	Failure        *PurchaseOrderWorkflowFailure      `json:"failure,omitempty"`
	Clarification  *ClarificationResponse             `json:"clarification,omitempty"`
}

// PurchaseOrderLifecyclePlan is the principal-bound current-state token
// payload for hold, resume, and cancel. Order carries the complete order
// state (including Status) observed at dry-run time, so any metadata or
// status change observed before confirmation makes the plan stale.
type PurchaseOrderLifecyclePlan struct {
	Action       string                             `json:"action"`
	OrderID      int                                `json:"order_id"`
	Order        inventree.PurchaseOrder            `json:"order"`
	Lines        []inventree.PurchaseOrderLineItem  `json:"lines"`
	ExtraLines   []inventree.PurchaseOrderExtraLine `json:"extra_lines"`
	TargetStatus int                                `json:"target_status"`
}

func holdPurchaseOrder(deps Dependencies) mcp.ToolHandlerFor[HoldPurchaseOrderInput, PurchaseOrderLifecycleOutput] {
	return LookupHandler[PurchaseOrderLifecycleClient, HoldPurchaseOrderInput, PurchaseOrderLifecycleOutput](deps, HoldPurchaseOrderToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLifecycleClient, input HoldPurchaseOrderInput) (*mcp.CallToolResult, PurchaseOrderLifecycleOutput, error) {
			out := PurchaseOrderLifecycleOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.OrderID <= 0 {
				return purchaseOrderLifecycleClarification(out, "Which pending or placed purchase order should be held?", "order_id must be positive", "order_id", map[string]any{"order_id": input.OrderID})
			}
			order, err := client.GetPurchaseOrder(ctx, input.OrderID)
			if err != nil {
				return nil, out, err
			}
			out.Order = &order
			if order.Status == inventree.PurchaseOrderStatusOnHold {
				out.Action = "already_on_hold"
				return TextResult(StatusOK), out, nil
			}
			if order.Status != inventree.PurchaseOrderStatusPending && order.Status != inventree.PurchaseOrderStatusPlaced {
				return purchaseOrderLifecycleClarification(out, "Which pending or placed purchase order should be held?", "only a pending or placed purchase order can be held", "order_id", map[string]any{"order_id": input.OrderID, "status": order.Status})
			}
			lines, extraLines, err := loadPurchaseOrderLifecycleContext(ctx, client, order.PK)
			if err != nil {
				return nil, out, err
			}
			out.Lines = lines
			out.ExtraLines = extraLines
			warning := ""
			if order.Status == inventree.PurchaseOrderStatusPending {
				warning = "This order has not been placed with its supplier. InvenTree has no native transition back to PENDING, so resuming this hold will place the order with its supplier."
			}
			plan := PurchaseOrderLifecyclePlan{Action: HoldPurchaseOrderToolName, OrderID: order.PK, Order: order, Lines: lines, ExtraLines: extraLines, TargetStatus: inventree.PurchaseOrderStatusOnHold}
			return executePurchaseOrderLifecyclePlan(ctx, deps.purchaseOrderLifecyclePlanStore, input.DryRun, input.Confirm, input.PlanHash, plan, warning,
				func() error { return client.HoldPurchaseOrder(ctx, order.PK) },
				func() (inventree.PurchaseOrder, error) { return client.GetPurchaseOrder(ctx, order.PK) },
			)
		})
}

func resumePurchaseOrder(deps Dependencies) mcp.ToolHandlerFor[ResumePurchaseOrderInput, PurchaseOrderLifecycleOutput] {
	return LookupHandler[PurchaseOrderLifecycleClient, ResumePurchaseOrderInput, PurchaseOrderLifecycleOutput](deps, ResumePurchaseOrderToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLifecycleClient, input ResumePurchaseOrderInput) (*mcp.CallToolResult, PurchaseOrderLifecycleOutput, error) {
			out := PurchaseOrderLifecycleOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.OrderID <= 0 {
				return purchaseOrderLifecycleClarification(out, "Which on-hold purchase order should be resumed?", "order_id must be positive", "order_id", map[string]any{"order_id": input.OrderID})
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
			if order.Status != inventree.PurchaseOrderStatusOnHold {
				return purchaseOrderLifecycleClarification(out, "Which on-hold purchase order should be resumed?", "only an on-hold purchase order can be resumed", "order_id", map[string]any{"order_id": input.OrderID, "status": order.Status})
			}
			lines, extraLines, err := loadPurchaseOrderLifecycleContext(ctx, client, order.PK)
			if err != nil {
				return nil, out, err
			}
			out.Lines = lines
			out.ExtraLines = extraLines
			plan := PurchaseOrderLifecyclePlan{Action: ResumePurchaseOrderToolName, OrderID: order.PK, Order: order, Lines: lines, ExtraLines: extraLines, TargetStatus: inventree.PurchaseOrderStatusPlaced}
			return executePurchaseOrderLifecyclePlan(ctx, deps.purchaseOrderLifecyclePlanStore, input.DryRun, input.Confirm, input.PlanHash, plan, "",
				func() error { return client.IssuePurchaseOrder(ctx, order.PK) },
				func() (inventree.PurchaseOrder, error) { return client.GetPurchaseOrder(ctx, order.PK) },
			)
		})
}

func cancelPurchaseOrder(deps Dependencies) mcp.ToolHandlerFor[CancelPurchaseOrderInput, PurchaseOrderLifecycleOutput] {
	return LookupHandler[PurchaseOrderLifecycleClient, CancelPurchaseOrderInput, PurchaseOrderLifecycleOutput](deps, CancelPurchaseOrderToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLifecycleClient, input CancelPurchaseOrderInput) (*mcp.CallToolResult, PurchaseOrderLifecycleOutput, error) {
			out := PurchaseOrderLifecycleOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.OrderID <= 0 {
				return purchaseOrderLifecycleClarification(out, "Which purchase order should be cancelled?", "order_id must be positive", "order_id", map[string]any{"order_id": input.OrderID})
			}
			order, err := client.GetPurchaseOrder(ctx, input.OrderID)
			if err != nil {
				return nil, out, err
			}
			out.Order = &order
			if order.Status == inventree.PurchaseOrderStatusCancelled {
				out.Action = "already_cancelled"
				return TextResult(StatusOK), out, nil
			}
			if order.Status == inventree.PurchaseOrderStatusComplete {
				return purchaseOrderLifecycleClarification(out, "Which pending, on-hold, or placed purchase order should be cancelled?", "a completed purchase order cannot be cancelled", "order_id", map[string]any{"order_id": input.OrderID, "status": order.Status})
			}
			lines, extraLines, err := loadPurchaseOrderLifecycleContext(ctx, client, order.PK)
			if err != nil {
				return nil, out, err
			}
			out.Lines = lines
			out.ExtraLines = extraLines
			if line, received, found := firstReceivedPurchaseOrderLine(lines); found {
				return purchaseOrderLifecycleClarification(out, "Should the received stock on this purchase order be resolved before cancelling?", "cancellation is refused while any ordinary line has received quantity greater than zero; InvenTree would otherwise leave that received stock orphaned but still linked to the cancelled order with no auto-disposal", "line_item_id", map[string]any{"order_id": input.OrderID, "line_item_id": line.PK, "received": received})
			}
			plan := PurchaseOrderLifecyclePlan{Action: CancelPurchaseOrderToolName, OrderID: order.PK, Order: order, Lines: lines, ExtraLines: extraLines, TargetStatus: inventree.PurchaseOrderStatusCancelled}
			return executePurchaseOrderLifecyclePlan(ctx, deps.purchaseOrderLifecyclePlanStore, input.DryRun, input.Confirm, input.PlanHash, plan, "",
				func() error { return client.CancelPurchaseOrder(ctx, order.PK) },
				func() (inventree.PurchaseOrder, error) { return client.GetPurchaseOrder(ctx, order.PK) },
			)
		})
}

// loadPurchaseOrderLifecycleContext returns the bounded line/extra-line
// context carried in hold/resume/cancel dry-run and confirmation output. Per
// this story's acceptance criteria, notes remain absent from that
// confirmation and recovery surface even though the shared
// sanitizePurchaseOrderLine/sanitizePurchaseOrderExtraLine helpers (used by
// other purchase-order tools that do return notes) only redact links, so
// Notes is cleared here rather than in those shared helpers.
func loadPurchaseOrderLifecycleContext(ctx context.Context, client PurchaseOrderLifecycleClient, orderID int) ([]inventree.PurchaseOrderLineItem, []inventree.PurchaseOrderExtraLine, error) {
	lines, err := client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{Order: orderID})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].PK < lines[j].PK })
	lines = sanitizePurchaseOrderLines(lines)
	for i := range lines {
		lines[i].Notes = ""
	}
	extraLines, err := scanPurchaseOrderExtraLines(ctx, client, orderID)
	if err != nil {
		return nil, nil, err
	}
	safeExtraLines := make([]inventree.PurchaseOrderExtraLine, len(extraLines))
	for i := range extraLines {
		safeExtraLines[i] = sanitizePurchaseOrderExtraLine(extraLines[i])
		safeExtraLines[i].Notes = ""
	}
	return lines, safeExtraLines, nil
}

func firstReceivedPurchaseOrderLine(lines []inventree.PurchaseOrderLineItem) (inventree.PurchaseOrderLineItem, float64, bool) {
	for _, line := range lines {
		if line.Received > 1e-9 {
			return line, line.Received, true
		}
	}
	return inventree.PurchaseOrderLineItem{}, 0, false
}

func executePurchaseOrderLifecyclePlan(
	ctx context.Context,
	store *purchaseOrderLifecyclePlanStore,
	dryRun, confirm bool,
	suppliedToken string,
	plan PurchaseOrderLifecyclePlan,
	warning string,
	mutate func() error,
	refresh func() (inventree.PurchaseOrder, error),
) (*mcp.CallToolResult, PurchaseOrderLifecycleOutput, error) {
	order := plan.Order
	out := PurchaseOrderLifecycleOutput{Status: StatusOK, DryRun: dryRun, Order: &order, Lines: plan.Lines, ExtraLines: plan.ExtraLines, Action: plan.Action, Warning: warning}
	if store == nil {
		return nil, out, errors.New("purchase-order lifecycle plan store is unavailable")
	}
	if dryRun {
		token, err := store.issue(ctx, plan)
		if err != nil {
			if errors.Is(err, errStockPlanCapacity) {
				return purchaseOrderLifecycleClarification(out, "When should a new purchase-order lifecycle plan be prepared?", "too many confirmation plans are outstanding; wait for an existing five-minute token to expire or execute a reviewed plan before preparing another", "dry_run", map[string]any{"order_id": plan.OrderID})
			}
			return nil, out, err
		}
		out.PlanHash = token
		out.PlannedChanges = []PlannedChange{plannedChange(plan.Action, "purchase_order", plan.OrderID, map[string]any{"status": plan.TargetStatus})}
		return TextResult(StatusOK), out, nil
	}
	if !confirm {
		return purchaseOrderLifecycleClarification(out, "Should this reviewed purchase-order lifecycle change now be executed?", "confirm must be true after reviewing the latest dry-run plan", "confirm", map[string]any{"order_id": plan.OrderID, "dry_run": true})
	}
	if suppliedToken == "" || !store.consume(ctx, suppliedToken, plan) {
		return purchaseOrderLifecycleClarification(out, "Which current dry-run plan should authorize this purchase-order lifecycle change?", "plan_hash must be the unexpired, single-use token from a matching dry run by the same principal", "plan_hash", map[string]any{"order_id": plan.OrderID, "dry_run": true})
	}
	mutationErr := mutate()
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, out, mutationErr
		}
	}
	refreshed, readErr := refresh()
	if readErr != nil {
		return purchaseOrderLifecycleUnknown(out, "purchase-order lifecycle result is unknown because refreshed order state is unavailable")
	}
	out.Order = &refreshed
	if refreshed.Status != plan.TargetStatus {
		return purchaseOrderLifecycleUnknown(out, "refreshed order status does not match the reviewed lifecycle plan")
	}
	out.Recovered = mutationErr != nil
	return TextResult(StatusOK), out, nil
}

func purchaseOrderLifecycleClarification(out PurchaseOrderLifecycleOutput, question, reason, retry string, fields map[string]any) (*mcp.CallToolResult, PurchaseOrderLifecycleOutput, error) {
	clarification := NewClarification(question, "purchase_order", reason, retry, true, nil, fields)
	out.Status = StatusClarificationRequired
	out.Clarification = &clarification
	return TextResult(StatusClarificationRequired), out, nil
}

func purchaseOrderLifecycleUnknown(out PurchaseOrderLifecycleOutput, message string) (*mcp.CallToolResult, PurchaseOrderLifecycleOutput, error) {
	out.Status = StatusPartialFailure
	out.Failure = &PurchaseOrderWorkflowFailure{
		Action:       out.Action,
		Message:      message,
		RecoveryPlan: "Do not retry the mutation blindly. Read the purchase order again with a fresh dry_run:true plan before preparing another confirmation.",
	}
	return TextResult(StatusPartialFailure), out, nil
}

// purchaseOrderLifecycleConfirmation and purchaseOrderLifecyclePlanStore
// reuse stockPlanPrincipal, stockPlanLifetime, stockPlanMaxEntries,
// stockPlanMaxEntriesPerPrincipal, and errStockPlanCapacity from
// stock_adjustment_tools.go, matching the reuse convention already followed
// by ownerPlanStore and projectCodePlanStore.
type purchaseOrderLifecycleConfirmation struct {
	digest    string
	principal string
	orderID   int
	action    string
	expiresAt time.Time
}

type purchaseOrderLifecyclePlanStore struct {
	mu                     sync.Mutex
	entries                map[string]purchaseOrderLifecycleConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newPurchaseOrderLifecyclePlanStore(now func() time.Time, token func() (string, error)) *purchaseOrderLifecyclePlanStore {
	return &purchaseOrderLifecyclePlanStore{
		entries:                map[string]purchaseOrderLifecycleConfirmation{},
		now:                    now,
		token:                  token,
		principal:              stockPlanPrincipal,
		maxEntries:             stockPlanMaxEntries,
		maxEntriesPerPrincipal: stockPlanMaxEntriesPerPrincipal,
	}
}

func (s *purchaseOrderLifecyclePlanStore) issue(ctx context.Context, plan PurchaseOrderLifecyclePlan) (string, error) {
	digest, err := purchaseOrderLifecyclePlanDigest(plan)
	if err != nil {
		return "", err
	}
	token, err := s.token()
	if err != nil {
		return "", err
	}
	now, principal := s.now(), s.principal(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpired(now)
	principalEntries := 0
	for existingToken, entry := range s.entries {
		if entry.principal == principal && entry.orderID == plan.OrderID && entry.action == plan.Action {
			delete(s.entries, existingToken)
			continue
		}
		if entry.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errStockPlanCapacity
	}
	s.entries[token] = purchaseOrderLifecycleConfirmation{digest: digest, principal: principal, orderID: plan.OrderID, action: plan.Action, expiresAt: now.Add(stockPlanLifetime)}
	return token, nil
}

func (s *purchaseOrderLifecyclePlanStore) consume(ctx context.Context, token string, plan PurchaseOrderLifecyclePlan) bool {
	digest, err := purchaseOrderLifecyclePlanDigest(plan)
	if err != nil {
		return false
	}
	now, principal := s.now(), s.principal(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpired(now)
	entry, ok := s.entries[token]
	if !ok || entry.digest != digest || entry.principal != principal || entry.orderID != plan.OrderID || entry.action != plan.Action || !now.Before(entry.expiresAt) {
		return false
	}
	delete(s.entries, token)
	return true
}

func (s *purchaseOrderLifecyclePlanStore) deleteExpired(now time.Time) {
	for token, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			delete(s.entries, token)
		}
	}
}

func purchaseOrderLifecyclePlanDigest(plan PurchaseOrderLifecyclePlan) (string, error) {
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func registerPurchaseOrderLifecycleTools(server *mcp.Server, deps Dependencies) {
	if deps.purchaseOrderLifecyclePlanStore == nil {
		deps.purchaseOrderLifecyclePlanStore = newPurchaseOrderLifecyclePlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, HoldPurchaseOrderToolName, "Hold purchase order", "Plans or explicitly places a pending or placed purchase order on hold.", holdPurchaseOrder(deps))
	addWriteTool(server, deps, ResumePurchaseOrderToolName, "Resume purchase order", "Plans or explicitly resumes an on-hold purchase order, placing it with its supplier.", resumePurchaseOrder(deps))
	addWriteTool(server, deps, CancelPurchaseOrderToolName, "Cancel purchase order", "Plans or explicitly cancels a pending, on-hold, or placed purchase order with no received stock.", cancelPurchaseOrder(deps))
}
