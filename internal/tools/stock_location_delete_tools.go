package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	stockLocationDeleteScanLimit                  = 1000
	stockLocationDeletePageSize                   = 100
	stockLocationDeletePlanLifetime               = 5 * time.Minute
	stockLocationDeletePlanMaxEntries             = 4096
	stockLocationDeletePlanMaxEntriesPerPrincipal = 64
)

var errStockLocationDeleteScanLimit = errors.New("stock-location dependency scan safety limit exceeded")

// StockLocationDeleteClient contains only the bounded reads and stable-ID
// deletion required by the guarded workflow.
type StockLocationDeleteClient interface {
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	SearchStockItemsPage(context.Context, inventree.StockItemQuery) (inventree.StockItemPage, error)
	SearchStockLocationsPage(context.Context, inventree.StockLocationQuery) (inventree.StockLocationPage, error)
	SearchPartsPage(context.Context, inventree.PartQuery) (inventree.PartPage, error)
	SearchPartCategoriesPage(context.Context, inventree.CategoryQuery) (inventree.CategoryPage, error)
	SearchObjectParametersPage(context.Context, inventree.ObjectParameterQuery) (inventree.PartParameterPage, error)
	SearchPurchaseOrdersPage(context.Context, inventree.PurchaseOrderQuery) (inventree.PurchaseOrderPage, error)
	SearchPurchaseOrderLinesPage(context.Context, inventree.PurchaseOrderLineQuery) (inventree.PurchaseOrderLinePage, error)
	SearchBuildsPage(context.Context, inventree.BuildQuery) (inventree.BuildPage, error)
	SearchTransferOrdersPage(context.Context, inventree.TransferOrderQuery) (inventree.TransferOrderPage, error)
	DeleteStockLocation(context.Context, int) error
}

type DeleteStockLocationInput struct {
	ID       int    `json:"id" jsonschema:"Stable InvenTree stock-location primary key."`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Required true after reviewing the exact location and every reported dependency, together with plan_hash."`
	PlanHash string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

type StockLocationDeleteReferences struct {
	StockItemIDs         []int `json:"stock_item_ids,omitempty"`
	ChildLocationIDs     []int `json:"child_location_ids,omitempty"`
	PartIDs              []int `json:"part_ids,omitempty"`
	CategoryIDs          []int `json:"category_ids,omitempty"`
	PurchaseOrderIDs     []int `json:"purchase_order_ids,omitempty"`
	PurchaseOrderLineIDs []int `json:"purchase_order_line_ids,omitempty"`
	ParameterValueIDs    []int `json:"parameter_value_ids,omitempty"`
	BuildIDs             []int `json:"build_ids,omitempty"`
	TransferOrderIDs     []int `json:"transfer_order_ids,omitempty"`
}

func (r StockLocationDeleteReferences) empty() bool {
	return len(r.StockItemIDs) == 0 && len(r.ChildLocationIDs) == 0 && len(r.PartIDs) == 0 && len(r.CategoryIDs) == 0 && len(r.PurchaseOrderIDs) == 0 && len(r.PurchaseOrderLineIDs) == 0 && len(r.ParameterValueIDs) == 0 && len(r.BuildIDs) == 0 && len(r.TransferOrderIDs) == 0
}

type StockLocationDeletePlan struct {
	Action     string                        `json:"action"`
	Location   inventree.StockLocation       `json:"location"`
	References StockLocationDeleteReferences `json:"references"`
}

type StockLocationDeleteOutput struct {
	Status        string                         `json:"status"`
	Record        *inventree.StockLocation       `json:"record,omitempty"`
	References    *StockLocationDeleteReferences `json:"references,omitempty"`
	PlanHash      string                         `json:"plan_hash,omitempty"`
	Verified      bool                           `json:"verified,omitempty"`
	Recovered     bool                           `json:"recovered,omitempty"`
	RecoveryPlan  string                         `json:"recovery_plan,omitempty"`
	Clarification *ClarificationResponse         `json:"clarification,omitempty"`
}

type stockLocationDeleteConfirmation struct {
	digest, principal string
	locationID        int
	expiresAt         time.Time
}
type stockLocationDeletePlanStore struct {
	mu                                 sync.Mutex
	entries                            map[string]stockLocationDeleteConfirmation
	now                                func() time.Time
	token                              func() (string, error)
	principal                          func(context.Context) string
	maxEntries, maxEntriesPerPrincipal int
}

func newStockLocationDeletePlanStore(now func() time.Time, token func() (string, error)) *stockLocationDeletePlanStore {
	return &stockLocationDeletePlanStore{entries: map[string]stockLocationDeleteConfirmation{}, now: now, token: token, principal: stockPlanPrincipal, maxEntries: stockLocationDeletePlanMaxEntries, maxEntriesPerPrincipal: stockLocationDeletePlanMaxEntriesPerPrincipal}
}

func (s *stockLocationDeletePlanStore) issue(ctx context.Context, plan StockLocationDeletePlan) (string, error) {
	digest, err := stockLocationDeletePlanDigest(plan)
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
	count := 0
	for key, entry := range s.entries {
		if !now.Before(entry.expiresAt) || entry.principal == principal && entry.locationID == plan.Location.PK {
			delete(s.entries, key)
			continue
		}
		if entry.principal == principal {
			count++
		}
	}
	if len(s.entries) >= s.maxEntries || count >= s.maxEntriesPerPrincipal {
		return "", errors.New("too many outstanding stock-location deletion confirmation plans")
	}
	s.entries[token] = stockLocationDeleteConfirmation{digest: digest, principal: principal, locationID: plan.Location.PK, expiresAt: now.Add(stockLocationDeletePlanLifetime)}
	return token, nil
}

func (s *stockLocationDeletePlanStore) consume(ctx context.Context, token string, plan StockLocationDeletePlan) bool {
	digest, err := stockLocationDeletePlanDigest(plan)
	if err != nil {
		return false
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			delete(s.entries, key)
		}
	}
	entry, ok := s.entries[token]
	if !ok || entry.digest != digest || entry.principal != s.principal(ctx) || entry.locationID != plan.Location.PK || !now.Before(entry.expiresAt) {
		return false
	}
	delete(s.entries, token)
	return true
}

func stockLocationDeletePlanDigest(plan StockLocationDeletePlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerStockLocationDeleteTool(server *mcp.Server, deps Dependencies) {
	if deps.stockLocationDeletePlanStore == nil {
		deps.stockLocationDeletePlanStore = newStockLocationDeletePlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, DeleteStockLocationToolName, "Delete stock location", "Previews and confirms deleting one empty, unreferenced stock location without cascading or rewriting dependencies.", deleteStockLocation(deps))
}

func deleteStockLocation(deps Dependencies) mcp.ToolHandlerFor[DeleteStockLocationInput, StockLocationDeleteOutput] {
	return LookupHandler[StockLocationDeleteClient, DeleteStockLocationInput, StockLocationDeleteOutput](deps, DeleteStockLocationToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockLocationDeleteClient, input DeleteStockLocationInput) (*mcp.CallToolResult, StockLocationDeleteOutput, error) {
			if input.ID <= 0 {
				c := NewClarification("Which stock location should be deleted?", "id", "id must be positive", "id", true, nil, map[string]any{"id": input.ID})
				return TextResult(StatusClarificationRequired), StockLocationDeleteOutput{Status: StatusClarificationRequired, Clarification: &c}, nil
			}
			record, err := client.GetStockLocation(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), StockLocationDeleteOutput{Status: StatusNotFound}, nil
			}
			if err != nil || record.PK != input.ID {
				return nil, StockLocationDeleteOutput{}, safeStockLocationDeleteError("stock-location delete lookup")
			}
			references, err := stockLocationDeleteReferences(ctx, client, record.PK)
			if err != nil {
				if errors.Is(err, errStockLocationDeleteScanLimit) {
					c := NewClarification("How should this completeness-sensitive dependency scan be narrowed or reviewed?", "references", err.Error(), "id", true, nil, map[string]any{"id": input.ID})
					return TextResult(StatusClarificationRequired), StockLocationDeleteOutput{Status: StatusClarificationRequired, Record: &record, Clarification: &c}, nil
				}
				return nil, StockLocationDeleteOutput{}, safeStockLocationDeleteError("stock-location dependency preflight")
			}
			if !references.empty() {
				c := NewClarification("Which dependencies must be removed before this stock location can be deleted?", "references", "the location is still referenced; this tool never cascades, transfers, clears, reparents, or rewrites those records", "id", true, []ClarificationCandidate{candidateFor(record)}, map[string]any{"id": input.ID})
				return TextResult(StatusClarificationRequired), StockLocationDeleteOutput{Status: StatusClarificationRequired, Record: &record, References: &references, Clarification: &c}, nil
			}
			plan := StockLocationDeletePlan{Action: DeleteStockLocationToolName, Location: record, References: references}
			if !input.Confirm {
				return issueStockLocationDeletePlan(ctx, deps.stockLocationDeletePlanStore, plan)
			}
			if deps.stockLocationDeletePlanStore == nil || input.PlanHash == "" || !deps.stockLocationDeletePlanStore.consume(ctx, input.PlanHash, plan) {
				c := NewClarification("Which current deletion plan should authorize this stock-location deletion?", "confirmation", "the plan is missing, stale, expired, reused, or belongs to another principal; preview the current location and dependencies again", "plan_hash", false, nil, map[string]any{"id": record.PK})
				return TextResult(StatusClarificationRequired), StockLocationDeleteOutput{Status: StatusClarificationRequired, Record: &record, References: &references, Clarification: &c}, nil
			}
			return verifyStockLocationDeletion(ctx, client, record, references, client.DeleteStockLocation(ctx, record.PK))
		})
}

func issueStockLocationDeletePlan(ctx context.Context, store *stockLocationDeletePlanStore, plan StockLocationDeletePlan) (*mcp.CallToolResult, StockLocationDeleteOutput, error) {
	if store == nil {
		return nil, StockLocationDeleteOutput{}, errors.New("stock-location deletion confirmation store is unavailable")
	}
	token, err := store.issue(ctx, plan)
	if err != nil {
		return nil, StockLocationDeleteOutput{}, err
	}
	c := NewClarification("Delete this exact, empty stock location?", "confirmation", "review the exact location and empty dependency snapshot, then provide confirm:true with this plan_hash", "plan_hash", false, []ClarificationCandidate{candidateFor(plan.Location)}, map[string]any{"id": plan.Location.PK, "confirm": true, "plan_hash": token})
	return TextResult(StatusClarificationRequired), StockLocationDeleteOutput{Status: StatusClarificationRequired, Record: &plan.Location, References: &plan.References, PlanHash: token, Clarification: &c}, nil
}

func verifyStockLocationDeletion(ctx context.Context, client StockLocationDeleteClient, record inventree.StockLocation, references StockLocationDeleteReferences, mutationErr error) (*mcp.CallToolResult, StockLocationDeleteOutput, error) {
	if mutationErr != nil {
		if errors.Is(mutationErr, context.Canceled) || errors.Is(mutationErr, context.DeadlineExceeded) {
			return nil, StockLocationDeleteOutput{}, mutationErr
		}
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, StockLocationDeleteOutput{}, safeStockLocationDeleteError("stock-location deletion")
		}
	}
	_, err := client.GetStockLocation(ctx, record.PK)
	if isNotFound(err) {
		return TextResult(StatusOK), StockLocationDeleteOutput{Status: StatusOK, Record: &record, References: &references, Verified: true, Recovered: mutationErr != nil}, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, StockLocationDeleteOutput{}, err
	}
	if err == nil {
		return TextResult(StatusPartialFailure), StockLocationDeleteOutput{Status: StatusPartialFailure, Record: &record, References: &references, RecoveryPlan: "The stock location still exists after deletion; read the exact stable ID before retrying."}, nil
	}
	return TextResult(StatusPartialFailure), StockLocationDeleteOutput{Status: StatusPartialFailure, Record: &record, References: &references, RecoveryPlan: "Deletion read-back could not prove absence; read the exact stable ID before retrying."}, nil
}

func stockLocationDeleteReferences(ctx context.Context, client StockLocationDeleteClient, locationID int) (StockLocationDeleteReferences, error) {
	var out StockLocationDeleteReferences
	var err error
	out.StockItemIDs, err = stockLocationDeleteStockItems(ctx, client, locationID)
	if err != nil {
		return out, err
	}
	out.ChildLocationIDs, err = stockLocationDeleteChildLocations(ctx, client, locationID)
	if err != nil {
		return out, err
	}
	out.PartIDs, err = stockLocationDeleteParts(ctx, client, locationID)
	if err != nil {
		return out, err
	}
	out.CategoryIDs, err = stockLocationDeleteCategories(ctx, client, locationID)
	if err != nil {
		return out, err
	}
	out.PurchaseOrderIDs, err = stockLocationDeletePurchaseOrders(ctx, client, locationID)
	if err != nil {
		return out, err
	}
	out.PurchaseOrderLineIDs, err = stockLocationDeletePurchaseOrderLines(ctx, client, locationID)
	if err != nil {
		return out, err
	}
	out.ParameterValueIDs, err = stockLocationDeleteParameters(ctx, client, locationID)
	if err != nil {
		return out, err
	}
	out.BuildIDs, err = stockLocationDeleteBuilds(ctx, client, locationID)
	if err != nil {
		return out, err
	}
	out.TransferOrderIDs, err = stockLocationDeleteTransferOrders(ctx, client, locationID)
	return out, err
}

func stockLocationDeleteStockItems(ctx context.Context, client StockLocationDeleteClient, id int) ([]int, error) {
	return scanStockLocationDeletePages(func(offset int) (int, []int, int, bool, error) {
		p, err := client.SearchStockItemsPage(ctx, inventree.StockItemQuery{LocationID: id, Limit: stockLocationDeletePageSize, Offset: offset})
		ids := make([]int, 0, len(p.Results))
		for _, v := range p.Results {
			ids = append(ids, v.PK)
		}
		return p.Count, ids, len(p.Results), p.HasMore, err
	})
}
func stockLocationDeleteChildLocations(ctx context.Context, client StockLocationDeleteClient, id int) ([]int, error) {
	return scanStockLocationDeletePages(func(offset int) (int, []int, int, bool, error) {
		p, err := client.SearchStockLocationsPage(ctx, inventree.StockLocationQuery{Parent: &id, Limit: stockLocationDeletePageSize, Offset: offset})
		ids := make([]int, 0, len(p.Results))
		for _, v := range p.Results {
			ids = append(ids, v.PK)
		}
		return p.Count, ids, len(p.Results), p.HasMore, err
	})
}
func stockLocationDeleteParts(ctx context.Context, client StockLocationDeleteClient, id int) ([]int, error) {
	return scanStockLocationDeletePages(func(offset int) (int, []int, int, bool, error) {
		p, err := client.SearchPartsPage(ctx, inventree.PartQuery{DefaultLocationID: id, Limit: stockLocationDeletePageSize, Offset: offset})
		ids := make([]int, 0, len(p.Results))
		for _, v := range p.Results {
			ids = append(ids, v.PK)
		}
		return p.Count, ids, len(p.Results), p.HasMore, err
	})
}
func stockLocationDeleteCategories(ctx context.Context, client StockLocationDeleteClient, id int) ([]int, error) {
	return scanStockLocationDeletePages(func(offset int) (int, []int, int, bool, error) {
		p, err := client.SearchPartCategoriesPage(ctx, inventree.CategoryQuery{Limit: stockLocationDeletePageSize, Offset: offset})
		ids := make([]int, 0)
		for _, v := range p.Results {
			if v.DefaultLocation != nil && *v.DefaultLocation == id {
				ids = append(ids, v.PK)
			}
		}
		return p.Count, ids, len(p.Results), p.HasMore, err
	})
}
func stockLocationDeletePurchaseOrders(ctx context.Context, client StockLocationDeleteClient, id int) ([]int, error) {
	return scanStockLocationDeletePages(func(offset int) (int, []int, int, bool, error) {
		p, err := client.SearchPurchaseOrdersPage(ctx, inventree.PurchaseOrderQuery{Limit: stockLocationDeletePageSize, Offset: offset})
		ids := make([]int, 0)
		for _, v := range p.Results {
			if v.Destination != nil && *v.Destination == id {
				ids = append(ids, v.PK)
			}
		}
		return p.Count, ids, len(p.Results), p.HasMore, err
	})
}
func stockLocationDeletePurchaseOrderLines(ctx context.Context, client StockLocationDeleteClient, id int) ([]int, error) {
	return scanStockLocationDeletePages(func(offset int) (int, []int, int, bool, error) {
		p, err := client.SearchPurchaseOrderLinesPage(ctx, inventree.PurchaseOrderLineQuery{Limit: stockLocationDeletePageSize, Offset: offset})
		ids := make([]int, 0)
		for _, v := range p.Results {
			if v.Destination != nil && *v.Destination == id {
				ids = append(ids, v.PK)
			}
		}
		return p.Count, ids, len(p.Results), p.HasMore, err
	})
}
func stockLocationDeleteParameters(ctx context.Context, client StockLocationDeleteClient, id int) ([]int, error) {
	return scanStockLocationDeletePages(func(offset int) (int, []int, int, bool, error) {
		p, err := client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: "stock.stocklocation", ModelID: id, Limit: stockLocationDeletePageSize, Offset: offset})
		ids := make([]int, 0, len(p.Results))
		for _, v := range p.Results {
			ids = append(ids, v.PK)
		}
		return p.Count, ids, len(p.Results), p.HasMore, err
	})
}

func stockLocationDeleteBuilds(ctx context.Context, client StockLocationDeleteClient, id int) ([]int, error) {
	return scanStockLocationDeletePages(func(offset int) (int, []int, int, bool, error) {
		p, err := client.SearchBuildsPage(ctx, inventree.BuildQuery{Limit: stockLocationDeletePageSize, Offset: offset})
		ids := make([]int, 0)
		for _, v := range p.Results {
			if v.TakeFrom != nil && *v.TakeFrom == id || v.Destination != nil && *v.Destination == id {
				ids = append(ids, v.PK)
			}
		}
		return p.Count, ids, len(p.Results), p.HasMore, err
	})
}

func stockLocationDeleteTransferOrders(ctx context.Context, client StockLocationDeleteClient, id int) ([]int, error) {
	return scanStockLocationDeletePages(func(offset int) (int, []int, int, bool, error) {
		p, err := client.SearchTransferOrdersPage(ctx, inventree.TransferOrderQuery{Limit: stockLocationDeletePageSize, Offset: offset})
		ids := make([]int, 0)
		for _, v := range p.Results {
			if v.TakeFrom != nil && *v.TakeFrom == id || v.Destination != nil && *v.Destination == id {
				ids = append(ids, v.PK)
			}
		}
		return p.Count, ids, len(p.Results), p.HasMore, err
	})
}

func scanStockLocationDeletePages(fetch func(int) (int, []int, int, bool, error)) ([]int, error) {
	ids := make([]int, 0)
	seen := 0
	for offset := 0; offset < stockLocationDeleteScanLimit; offset += stockLocationDeletePageSize {
		count, pageIDs, pageSeen, more, err := fetch(offset)
		if err != nil {
			return nil, err
		}
		seen += pageSeen
		if count < 0 || count > stockLocationDeleteScanLimit || pageSeen < len(pageIDs) || seen > count || more && pageSeen == 0 || !more && seen != count {
			return nil, errStockLocationDeleteScanLimit
		}
		ids = append(ids, pageIDs...)
		if !more {
			slices.Sort(ids)
			return ids, nil
		}
	}
	return nil, errStockLocationDeleteScanLimit
}

func safeStockLocationDeleteError(operation string) error { return errors.New(operation + " failed") }
