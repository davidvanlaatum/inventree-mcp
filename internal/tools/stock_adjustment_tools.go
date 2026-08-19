package tools

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	stockStatusOK         = 10
	stockStatusAttention  = 50
	stockStatusDamaged    = 55
	stockStatusDestroyed  = 60
	stockStatusRejected   = 65
	stockStatusLost       = 70
	stockStatusQuarantine = 75
	stockStatusReturned   = 85
)

var stockStatusNames = map[int]string{
	stockStatusOK:         "OK",
	stockStatusAttention:  "Attention needed",
	stockStatusDamaged:    "Damaged",
	stockStatusDestroyed:  "Destroyed",
	stockStatusRejected:   "Rejected",
	stockStatusLost:       "Lost",
	stockStatusQuarantine: "Quarantined",
	stockStatusReturned:   "Returned",
}

const (
	stockPlanLifetime               = 5 * time.Minute
	stockPlanMaxEntries             = 4096
	stockPlanMaxEntriesPerPrincipal = 64
)

var errStockPlanCapacity = errors.New("too many outstanding stock confirmation plans")

type stockPlanConfirmation struct {
	digest      string
	principal   string
	expiresAt   time.Time
	action      string
	stockItemID int
}

type stockPlanStore struct {
	mu                     sync.Mutex
	entries                map[string]stockPlanConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newStockPlanStore(now func() time.Time, token func() (string, error)) *stockPlanStore {
	return &stockPlanStore{
		entries:                map[string]stockPlanConfirmation{},
		now:                    now,
		token:                  token,
		principal:              stockPlanPrincipal,
		maxEntries:             stockPlanMaxEntries,
		maxEntriesPerPrincipal: stockPlanMaxEntriesPerPrincipal,
	}
}

func (s *stockPlanStore) issue(ctx context.Context, plan StockAdjustmentPlan) (string, error) {
	digest, err := stockPlanHash(plan)
	if err != nil {
		return "", err
	}
	token, err := s.token()
	if err != nil {
		return "", err
	}
	now := s.now()
	principal := s.principal(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpired(now)
	principalEntries := 0
	for existingToken, confirmation := range s.entries {
		if confirmation.principal == principal && confirmation.action == plan.Action && confirmation.stockItemID == plan.Before.StockItemID {
			delete(s.entries, existingToken)
			continue
		}
		if confirmation.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errStockPlanCapacity
	}
	s.entries[token] = stockPlanConfirmation{digest: digest, principal: principal, expiresAt: now.Add(stockPlanLifetime), action: plan.Action, stockItemID: plan.Before.StockItemID}
	return token, nil
}

func (s *stockPlanStore) consume(ctx context.Context, token string, plan StockAdjustmentPlan) bool {
	digest, err := stockPlanHash(plan)
	if err != nil {
		return false
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpired(now)
	confirmation, ok := s.entries[token]
	if !ok {
		return false
	}
	valid := confirmation.digest == digest && confirmation.principal == s.principal(ctx) && confirmation.action == plan.Action && confirmation.stockItemID == plan.Before.StockItemID && now.Before(confirmation.expiresAt)
	if valid {
		delete(s.entries, token)
	}
	return valid
}

func (s *stockPlanStore) deleteExpired(now time.Time) {
	for token, confirmation := range s.entries {
		if !now.Before(confirmation.expiresAt) {
			delete(s.entries, token)
		}
	}
}

func randomStockPlanToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func stockPlanPrincipal(ctx context.Context) string {
	if info := auth.TokenInfoFromContext(ctx); info != nil {
		return "oauth:" + info.UserID
	}
	return "stdio"
}

type StockAdjustmentClient interface {
	GetStockItem(context.Context, int) (inventree.StockItem, error)
	AddStock(context.Context, inventree.StockAdjustment) error
	RemoveStock(context.Context, inventree.StockAdjustment) error
	CountStock(context.Context, inventree.StockAdjustment) error
	ChangeStockStatus(context.Context, inventree.StockStatusChange) error
	UpdateStockItem(context.Context, int, inventree.PatchFields) (inventree.StockItem, error)
}

type AdjustStockQuantityInput struct {
	DryRun      bool    `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound quantity adjustment plan without writing."`
	Confirm     bool    `json:"confirm,omitempty" jsonschema:"Required true for execution after reviewing a dry run."`
	PlanHash    string  `json:"plan_hash,omitempty" jsonschema:"Opaque single-use confirmation token returned by the latest dry run."`
	StockItemID int     `json:"stock_item_id" jsonschema:"Existing stock-item primary key."`
	Delta       float64 `json:"delta" jsonschema:"Non-zero relative quantity change; positive adds stock and negative removes stock."`
	Reason      string  `json:"reason" jsonschema:"Nonblank operator audit reason recorded with the stock transaction."`
}

type SetStockStatusInput struct {
	DryRun      bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound status change plan without writing."`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"Required true for execution after reviewing a dry run."`
	PlanHash    string `json:"plan_hash,omitempty" jsonschema:"Opaque single-use confirmation token returned by the latest dry run."`
	StockItemID int    `json:"stock_item_id" jsonschema:"Existing stock-item primary key."`
	Status      int    `json:"status" jsonschema:"Target InvenTree stock status code."`
	Reason      string `json:"reason" jsonschema:"Nonblank operator audit reason recorded with the stock transaction."`
}

type StocktakeAdjustmentInput struct {
	DryRun           bool    `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound stocktake plan without writing."`
	Confirm          bool    `json:"confirm,omitempty" jsonschema:"Required true for execution after reviewing a dry run."`
	PlanHash         string  `json:"plan_hash,omitempty" jsonschema:"Opaque single-use confirmation token returned by the latest dry run."`
	StockItemID      int     `json:"stock_item_id" jsonschema:"Existing stock-item primary key."`
	ObservedQuantity float64 `json:"observed_quantity" jsonschema:"Absolute quantity physically observed during stocktake; must be non-negative."`
	Reason           string  `json:"reason" jsonschema:"Nonblank operator audit reason recorded with the stock transaction."`
}

type DepleteStockItemInput struct {
	DryRun      bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound deletion plan without writing."`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"Required true for execution after reviewing a dry run."`
	PlanHash    string `json:"plan_hash,omitempty" jsonschema:"Opaque single-use confirmation token returned by the latest dry run."`
	StockItemID int    `json:"stock_item_id" jsonschema:"Existing delete-on-deplete stock-item primary key."`
	Reason      string `json:"reason" jsonschema:"Nonblank operator audit reason recorded with the stock removal transaction."`
}

type TransferStockItemInput struct {
	DryRun                bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound full-transfer plan without writing."`
	Confirm               bool   `json:"confirm,omitempty" jsonschema:"Required true for execution after reviewing a dry run."`
	PlanHash              string `json:"plan_hash,omitempty" jsonschema:"Opaque single-use confirmation token returned by the latest dry run."`
	StockItemID           int    `json:"stock_item_id" jsonschema:"Existing stock-item primary key whose complete quantity will be transferred."`
	DestinationLocationID int    `json:"destination_location_id" jsonschema:"Explicit existing destination stock-location primary key."`
	Reason                string `json:"reason" jsonschema:"Nonblank operator audit reason recorded with the stock transfer."`
}

type StockStateSnapshot struct {
	inventree.WebLinkFields
	StockItemID     int     `json:"stock_item_id"`
	PartID          int     `json:"part_id"`
	Quantity        float64 `json:"quantity"`
	LocationID      *int    `json:"location_id"`
	Status          int     `json:"status"`
	Batch           *string `json:"batch,omitempty"`
	Serial          *string `json:"serial,omitempty"`
	Packaging       *string `json:"packaging,omitempty"`
	DeleteOnDeplete bool    `json:"delete_on_deplete"`
}

type StockDepletionContext struct {
	InStock         bool     `json:"in_stock"`
	Allocated       *float64 `json:"allocated,omitempty"`
	IsBuilding      bool     `json:"is_building"`
	BuildID         *int     `json:"build_id,omitempty"`
	ConsumedByID    *int     `json:"consumed_by_id,omitempty"`
	BelongsToID     *int     `json:"belongs_to_id,omitempty"`
	ParentID        *int     `json:"parent_id,omitempty"`
	InstalledItems  *int     `json:"installed_items,omitempty"`
	ChildItems      *int     `json:"child_items,omitempty"`
	TrackingItems   *int     `json:"tracking_items,omitempty"`
	OwnerID         *int     `json:"owner_id,omitempty"`
	SupplierPartID  *int     `json:"supplier_part_id,omitempty"`
	PurchaseOrderID *int     `json:"purchase_order_id,omitempty"`
	CustomerID      *int     `json:"customer_id,omitempty"`
	SalesOrderID    *int     `json:"sales_order_id,omitempty"`
}

type StockTransferLocation struct {
	inventree.WebLinkFields
	ID           int                  `json:"id"`
	Name         string               `json:"name"`
	ParentID     *int                 `json:"parent_id,omitempty"`
	Path         []inventree.TreePath `json:"path,omitempty"`
	Structural   bool                 `json:"structural"`
	External     bool                 `json:"external"`
	OwnerID      *int                 `json:"owner_id,omitempty"`
	LocationType *int                 `json:"location_type_id,omitempty"`
}

type StockTransferProvenance struct {
	PartID                int                      `json:"part_id"`
	Status                int                      `json:"status"`
	StatusCustomKey       *int                     `json:"status_custom_key,omitempty"`
	Batch                 *string                  `json:"batch,omitempty"`
	ExpiryDate            *string                  `json:"expiry_date,omitempty"`
	Packaging             *string                  `json:"packaging,omitempty"`
	DeleteOnDeplete       bool                     `json:"delete_on_deplete"`
	OwnerID               *int                     `json:"owner_id,omitempty"`
	SupplierPartID        *int                     `json:"supplier_part_id,omitempty"`
	PurchaseOrderID       *int                     `json:"purchase_order_id,omitempty"`
	PurchasePrice         *inventree.DecimalString `json:"purchase_price,omitempty"`
	PurchasePriceCurrency string                   `json:"purchase_price_currency,omitempty"`
	CreationDate          *string                  `json:"creation_date,omitempty"`
}

type StockTransferSafety struct {
	InStock        bool     `json:"in_stock"`
	Allocated      *float64 `json:"allocated,omitempty"`
	Serial         *string  `json:"serial,omitempty"`
	IsBuilding     bool     `json:"is_building"`
	BuildID        *int     `json:"build_id,omitempty"`
	ConsumedByID   *int     `json:"consumed_by_id,omitempty"`
	BelongsToID    *int     `json:"belongs_to_id,omitempty"`
	ParentID       *int     `json:"parent_id,omitempty"`
	InstalledItems *int     `json:"installed_items,omitempty"`
	ChildItems     *int     `json:"child_items,omitempty"`
	CustomerID     *int     `json:"customer_id,omitempty"`
	SalesOrderID   *int     `json:"sales_order_id,omitempty"`
}

type StockTransferContext struct {
	Source      StockTransferLocation   `json:"source"`
	Destination StockTransferLocation   `json:"destination"`
	Provenance  StockTransferProvenance `json:"provenance"`
	Safety      StockTransferSafety     `json:"safety"`
	WillSplit   bool                    `json:"will_split"`
}

type StockAdjustmentPlan struct {
	Action     string                 `json:"action"`
	Before     StockStateSnapshot     `json:"before"`
	After      StockStateSnapshot     `json:"after"`
	Reason     string                 `json:"reason"`
	HighRisk   bool                   `json:"high_risk"`
	RiskReason string                 `json:"risk_reason,omitempty"`
	WillDelete bool                   `json:"will_delete,omitempty"`
	Depletion  *StockDepletionContext `json:"depletion,omitempty"`
	Transfer   *StockTransferContext  `json:"transfer,omitempty"`
}

type StockAdjustmentFailure struct {
	Action       string `json:"action"`
	Message      string `json:"message"`
	RecoveryPlan string `json:"recovery_plan"`
}

type StockAdjustmentOutput struct {
	Status        string                  `json:"status"`
	DryRun        bool                    `json:"dry_run"`
	Plan          *StockAdjustmentPlan    `json:"plan,omitempty"`
	PlanHash      string                  `json:"plan_hash,omitempty"`
	Record        *inventree.StockItem    `json:"record,omitempty"`
	Failure       *StockAdjustmentFailure `json:"failure,omitempty"`
	Clarification *ClarificationResponse  `json:"clarification,omitempty"`
	Verified      bool                    `json:"verified,omitempty"`
	Recovered     bool                    `json:"recovered,omitempty"`
}

type StockTransferOutput = StockAdjustmentOutput

func registerStockAdjustmentTools(server *mcp.Server, deps Dependencies) {
	if deps.stockPlanStore == nil {
		deps.stockPlanStore = newStockPlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, AdjustStockQuantityToolName, "Adjust stock quantity", "Plans or confirms one relative stock quantity adjustment with an audit reason.", adjustStockQuantity(deps))
	addWriteTool(server, deps, SetStockStatusToolName, "Set stock status", "Plans or confirms one status-only stock change with an audit reason.", setStockStatus(deps))
	addWriteTool(server, deps, StocktakeAdjustmentToolName, "Record stocktake adjustment", "Plans or confirms one absolute quantity-only stocktake count with an audit reason.", stocktakeAdjustment(deps))
	addWriteTool(server, deps, SetStockDeleteOnDepleteToolName, "Set stock delete-on-deplete policy", "Plans or confirms one delete-on-deplete policy change for a stock item with an audit reason.", setStockDeleteOnDeplete(deps))
	addWriteTool(server, deps, DepleteStockItemToolName, "Deplete delete-on-deplete stock item", "Plans or confirms complete removal of one safe delete-on-deplete stock item with an audit reason.", depleteStockItem(deps))
	addWriteTool(server, deps, TransferStockItemToolName, "Transfer complete stock item", "Plans or confirms relocation of one safe stock item's complete quantity to an explicit destination.", transferStockItem(deps))
}

func adjustStockQuantity(deps Dependencies) mcp.ToolHandlerFor[AdjustStockQuantityInput, StockAdjustmentOutput] {
	return LookupHandler[StockAdjustmentClient, AdjustStockQuantityInput, StockAdjustmentOutput](deps, AdjustStockQuantityToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdjustmentClient, input AdjustStockQuantityInput) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
			before, out, result, err := prepareStockAdjustment(ctx, client, input.DryRun, input.StockItemID, input.Reason)
			if result != nil || err != nil {
				return result, out, err
			}
			if before.Serial != nil {
				return stockClarification(out, "How should this serialized stock item be handled?", "delta", "InvenTree does not apply relative quantity adjustments to serialized stock; adjust the individual serialized items instead", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID, "serial": before.Serial})
			}
			delta, normalizedDelta, ok := normalizedStockDecimal(math.Abs(input.Delta), false)
			if !ok {
				return stockClarification(out, "What non-zero schema-valid quantity change should be applied?", "delta", "delta must be finite, non-zero, and fit the InvenTree decimal bounds", "delta", map[string]any{"stock_item_id": input.StockItemID})
			}
			_, normalizedBefore, ok := normalizedStockDecimal(before.Quantity, true)
			if !ok {
				return nil, out, errors.New("InvenTree returned stock quantity outside schema bounds")
			}
			before.Quantity = normalizedBefore
			afterQuantity := normalizedBefore + math.Copysign(normalizedDelta, input.Delta)
			_, afterQuantity, ok = normalizedStockDecimal(afterQuantity, true)
			if !ok {
				return stockClarification(out, "What quantity change keeps the stock item non-negative?", "delta", "the requested delta would produce an invalid stock quantity", "delta", map[string]any{"stock_item_id": input.StockItemID, "before_quantity": before.Quantity})
			}
			if afterQuantity == 0 && before.DeleteOnDeplete {
				return stockClarification(out, "How should this delete-on-deplete stock item be handled?", "delta", "reducing this item to zero would delete it, which is outside the non-destructive stock adjustment contract", "delta", map[string]any{"stock_item_id": input.StockItemID, "delete_on_deplete": true, "before_quantity": before.Quantity})
			}
			after := snapshotStockItem(before)
			after.Quantity = afterQuantity
			plan := StockAdjustmentPlan{Action: AdjustStockQuantityToolName, Before: snapshotStockItem(before), After: after, Reason: strings.TrimSpace(input.Reason), HighRisk: input.Delta < 0}
			if plan.HighRisk {
				plan.RiskReason = "quantity decrease removes stock"
			}
			return executeStockPlan(ctx, deps.stockPlanStore, client, input.DryRun, input.Confirm, input.PlanHash, plan, func() error {
				adjustment := inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: input.StockItemID, Quantity: delta}}, Notes: plan.Reason}
				if input.Delta > 0 {
					return client.AddStock(ctx, adjustment)
				}
				return client.RemoveStock(ctx, adjustment)
			})
		})
}

func setStockStatus(deps Dependencies) mcp.ToolHandlerFor[SetStockStatusInput, StockAdjustmentOutput] {
	return LookupHandler[StockAdjustmentClient, SetStockStatusInput, StockAdjustmentOutput](deps, SetStockStatusToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdjustmentClient, input SetStockStatusInput) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
			before, out, result, err := prepareStockAdjustment(ctx, client, input.DryRun, input.StockItemID, input.Reason)
			if result != nil || err != nil {
				return result, out, err
			}
			statusName, ok := stockStatusNames[input.Status]
			if !ok {
				return stockClarification(out, "Which schema-supported stock status should be used?", "status", "status must be one of the InvenTree stock status codes documented for this API version", "status", map[string]any{"stock_item_id": input.StockItemID, "supported_statuses": stockStatusNames})
			}
			after := snapshotStockItem(before)
			after.Status = input.Status
			if after.Status == before.Status {
				return stockClarification(out, "Which different stock status should be applied?", "status", "the selected stock item already has this status; no mutation is needed", "status", map[string]any{"stock_item_id": input.StockItemID, "status": input.Status})
			}
			plan := StockAdjustmentPlan{Action: SetStockStatusToolName, Before: snapshotStockItem(before), After: after, Reason: strings.TrimSpace(input.Reason), HighRisk: highRiskStockStatus(input.Status)}
			if plan.HighRisk {
				plan.RiskReason = "status transition to " + statusName + " is a write-off state"
			}
			return executeStockPlan(ctx, deps.stockPlanStore, client, input.DryRun, input.Confirm, input.PlanHash, plan, func() error {
				return client.ChangeStockStatus(ctx, inventree.StockStatusChange{Items: []int{input.StockItemID}, Status: input.Status, Note: plan.Reason})
			})
		})
}

func stocktakeAdjustment(deps Dependencies) mcp.ToolHandlerFor[StocktakeAdjustmentInput, StockAdjustmentOutput] {
	return LookupHandler[StockAdjustmentClient, StocktakeAdjustmentInput, StockAdjustmentOutput](deps, StocktakeAdjustmentToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdjustmentClient, input StocktakeAdjustmentInput) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
			before, out, result, err := prepareStockAdjustment(ctx, client, input.DryRun, input.StockItemID, input.Reason)
			if result != nil || err != nil {
				return result, out, err
			}
			if before.Serial != nil {
				return stockClarification(out, "How should this serialized stock item be counted?", "observed_quantity", "InvenTree does not apply absolute quantity counts to serialized stock; count the individual serialized items instead", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID, "serial": before.Serial})
			}
			quantity, normalizedQuantity, ok := normalizedStockDecimal(input.ObservedQuantity, true)
			if !ok {
				return stockClarification(out, "What non-negative schema-valid quantity was physically observed?", "observed_quantity", "observed_quantity must be finite, non-negative, and fit the InvenTree decimal bounds", "observed_quantity", map[string]any{"stock_item_id": input.StockItemID})
			}
			_, normalizedBefore, ok := normalizedStockDecimal(before.Quantity, true)
			if !ok {
				return nil, out, errors.New("InvenTree returned stock quantity outside schema bounds")
			}
			before.Quantity = normalizedBefore
			if normalizedQuantity == 0 && before.DeleteOnDeplete {
				return stockClarification(out, "How should this delete-on-deplete stock item be handled?", "observed_quantity", "counting this item to zero would delete it, which is outside the non-destructive stocktake contract", "observed_quantity", map[string]any{"stock_item_id": input.StockItemID, "delete_on_deplete": true, "before_quantity": before.Quantity})
			}
			after := snapshotStockItem(before)
			after.Quantity = normalizedQuantity
			if math.Abs(after.Quantity-before.Quantity) < 1e-9 {
				return stockClarification(out, "What different absolute quantity was physically observed?", "observed_quantity", "the observed quantity already matches current stock; no mutation is needed", "observed_quantity", map[string]any{"stock_item_id": input.StockItemID, "observed_quantity": normalizedQuantity})
			}
			plan := StockAdjustmentPlan{Action: StocktakeAdjustmentToolName, Before: snapshotStockItem(before), After: after, Reason: strings.TrimSpace(input.Reason), HighRisk: normalizedQuantity < normalizedBefore}
			if plan.HighRisk {
				plan.RiskReason = "observed quantity is lower than recorded quantity"
			}
			return executeStockPlan(ctx, deps.stockPlanStore, client, input.DryRun, input.Confirm, input.PlanHash, plan, func() error {
				return client.CountStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: input.StockItemID, Quantity: quantity}}, Notes: plan.Reason})
			})
		})
}

func prepareStockAdjustment(ctx context.Context, client StockAdjustmentClient, dryRun bool, stockItemID int, reason string) (inventree.StockItem, StockAdjustmentOutput, *mcp.CallToolResult, error) {
	out := StockAdjustmentOutput{Status: StatusOK, DryRun: dryRun}
	if stockItemID <= 0 {
		result, clarified, err := stockClarification(out, "Which stock item should be adjusted?", "stock_item", "stock_item_id must be positive", "stock_item_id", map[string]any{"stock_item_id": stockItemID})
		return inventree.StockItem{}, clarified, result, err
	}
	if strings.TrimSpace(reason) == "" {
		result, clarified, err := stockClarification(out, "What audit reason should be recorded for this stock change?", "reason", "a nonblank operator reason is required for every stock adjustment", "reason", map[string]any{"stock_item_id": stockItemID})
		return inventree.StockItem{}, clarified, result, err
	}
	before, err := client.GetStockItem(ctx, stockItemID)
	return before, out, nil, err
}

func executeStockPlan(ctx context.Context, store *stockPlanStore, client StockAdjustmentClient, dryRun, confirm bool, suppliedToken string, plan StockAdjustmentPlan, mutate func() error) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
	out := StockAdjustmentOutput{Status: StatusOK, DryRun: dryRun, Plan: &plan}
	if store == nil {
		return nil, out, errors.New("stock plan store is unavailable")
	}
	if dryRun {
		token, err := store.issue(ctx, plan)
		if err != nil {
			if errors.Is(err, errStockPlanCapacity) {
				return stockClarification(out, "When should a new stock confirmation plan be prepared?", "confirmation", "too many confirmation plans are outstanding; wait for an existing five-minute token to expire or execute a reviewed plan before preparing another", "dry_run", map[string]any{"stock_item_id": plan.Before.StockItemID})
			}
			return nil, out, err
		}
		out.PlanHash = token
		return TextResult(StatusOK), out, nil
	}
	if !confirm {
		return stockClarification(out, "Should this reviewed stock adjustment now be executed?", "confirmation", "confirm must be true after reviewing the latest dry-run plan", "confirm", map[string]any{"stock_item_id": plan.Before.StockItemID, "dry_run": true, "high_risk": plan.HighRisk})
	}
	if suppliedToken == "" || !store.consume(ctx, suppliedToken, plan) {
		return stockClarification(out, "Which current dry-run plan should authorize this stock adjustment?", "confirmation", "plan_hash must be the unexpired, single-use token from a matching dry run by the same principal", "plan_hash", map[string]any{"stock_item_id": plan.Before.StockItemID, "dry_run": true})
	}
	if err := mutate(); err != nil {
		var apiErr *inventree.APIError
		if errors.As(err, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, out, err
		}
		return stockUnknownResult(out, "stock adjustment result is unknown")
	}
	after, err := client.GetStockItem(ctx, plan.Before.StockItemID)
	if err != nil {
		return stockUnknownResult(out, "stock adjustment completed but refreshed stock state is unavailable")
	}
	sanitized := sanitizedStockItem(after)
	out.Record = &sanitized
	if !stockStateMatches(after, plan.After) {
		return stockUnknownResult(out, "refreshed stock state does not match the reviewed plan")
	}
	return TextResult(StatusOK), out, nil
}

func stockClarification(out StockAdjustmentOutput, question, subject, reason, retry string, fields map[string]any) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
	clarification := NewClarification(question, subject, reason, retry, true, nil, fields)
	out.Status = StatusClarificationRequired
	out.Clarification = &clarification
	return TextResult(StatusClarificationRequired), out, nil
}

func stockUnknownResult(out StockAdjustmentOutput, message string) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
	out.Status = StatusPartialFailure
	if out.Record != nil {
		recovery := stockItemRecoveryProjection(*out.Record)
		out.Record = &recovery
	}
	out.Failure = &StockAdjustmentFailure{
		Action:       out.Plan.Action,
		Message:      message,
		RecoveryPlan: "Do not retry the mutation blindly. Run the same tool again with dry_run:true and the stable stock_item_id to read current state and prepare a new plan only if needed.",
	}
	return TextResult(StatusPartialFailure), out, nil
}

func snapshotStockItem(item inventree.StockItem) StockStateSnapshot {
	return StockStateSnapshot{StockItemID: item.PK, PartID: item.Part, Quantity: item.Quantity, LocationID: item.Location, Status: item.Status, Batch: item.Batch, Serial: item.Serial, Packaging: item.Packaging, DeleteOnDeplete: item.DeleteOnDeplete}
}

func stockStateMatches(item inventree.StockItem, expected StockStateSnapshot) bool {
	actual := snapshotStockItem(item)
	return actual.StockItemID == expected.StockItemID && actual.PartID == expected.PartID && math.Abs(actual.Quantity-expected.Quantity) < 1e-9 && equalIntPtr(actual.LocationID, expected.LocationID) && actual.Status == expected.Status && equalStringPtr(actual.Batch, expected.Batch) && equalStringPtr(actual.Serial, expected.Serial) && equalStringPtr(actual.Packaging, expected.Packaging) && actual.DeleteOnDeplete == expected.DeleteOnDeplete
}

func equalIntPtr(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func equalStringPtr(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func stockPlanHash(plan StockAdjustmentPlan) (string, error) {
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func normalizedStockDecimal(quantity float64, allowZero bool) (string, float64, bool) {
	if math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity < 0 || !allowZero && quantity == 0 {
		return "", 0, false
	}
	fixed := strconv.FormatFloat(quantity, 'f', 5, 64)
	normalized, err := strconv.ParseFloat(fixed, 64)
	if err != nil || !allowZero && normalized == 0 || math.Abs(normalized-quantity) > 1e-9 {
		return "", 0, false
	}
	formatted := strings.TrimRight(strings.TrimRight(fixed, "0"), ".")
	if formatted == "" {
		formatted = "0"
	}
	parts := strings.SplitN(formatted, ".", 2)
	if len(parts[0]) > 10 || len(parts) == 2 && len(parts[1]) > 5 {
		return "", 0, false
	}
	return formatted, normalized, true
}

func highRiskStockStatus(status int) bool {
	return status == stockStatusDestroyed || status == stockStatusRejected || status == stockStatusLost
}
