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
	categoryDeleteReferenceScanLimit = 1000
	categoryDeleteReferencePageSize  = 100

	categoryDeletePlanLifetime               = 5 * time.Minute
	categoryDeletePlanMaxEntries             = 4096
	categoryDeletePlanMaxEntriesPerPrincipal = 64
)

var errCategoryDeleteReferenceScanLimit = errors.New("part-category reference scan safety limit exceeded")

// CategoryDeleteClient is deliberately read-heavy, mirroring delete_part: it
// must prove every reference surface listed in
// inventree.CategoryDeleteReferenceInventory is empty before it will ever
// consider deleting a category, and refuses outright -- without issuing a
// confirmation token -- while any of them is non-empty. Unlike
// delete_stock_location_type (which InvenTree itself safely SET_NULLs
// references for), no live spike is needed to justify blocking here: the
// operator-approved scope for this tool is deletion of an already-empty
// leaf category, so DELETE is never attempted against a referenced one.
type CategoryDeleteClient interface {
	GetPartCategory(context.Context, int) (inventree.Category, error)
	SearchPartsPage(context.Context, inventree.PartQuery) (inventree.PartPage, error)
	SearchPartCategoriesPage(context.Context, inventree.CategoryQuery) (inventree.CategoryPage, error)
	SearchCategoryParameterTemplatesPage(context.Context, inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error)
	SearchObjectParametersPage(context.Context, inventree.ObjectParameterQuery) (inventree.PartParameterPage, error)
	DeletePartCategory(context.Context, int) error
}

type DeletePartCategoryInput struct {
	ID       int    `json:"id" jsonschema:"Stable InvenTree part-category primary key."`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Required true after reviewing the exact current category and every reported dependency, together with plan_hash."`
	PlanHash string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

// PartCategoryDeleteBlockingReferences reports the stable IDs of every
// record from inventree.CategoryDeleteReferenceInventory that refuses this
// deletion, so an operator can inspect and remove them explicitly instead of
// the tool ever cascading, reparenting, or rewriting references on their
// behalf.
type PartCategoryDeleteBlockingReferences struct {
	PartIDs                  []int `json:"part_ids,omitempty"`
	ChildCategoryIDs         []int `json:"child_category_ids,omitempty"`
	ParameterTemplateLinkIDs []int `json:"parameter_template_link_ids,omitempty"`
	ParameterValueIDs        []int `json:"parameter_value_ids,omitempty"`
}

func (b PartCategoryDeleteBlockingReferences) empty() bool {
	return len(b.PartIDs) == 0 && len(b.ChildCategoryIDs) == 0 && len(b.ParameterTemplateLinkIDs) == 0 && len(b.ParameterValueIDs) == 0
}

// PartCategoryDeletePlan binds the exact current category record and the
// complete (necessarily empty, since a non-empty plan is never issued)
// dependency snapshot. Any drift in either between preview and confirm --
// including a reference reappearing -- changes this plan's digest, so a
// stale token is rejected rather than silently deleting against state the
// operator never reviewed.
type PartCategoryDeletePlan struct {
	Action   string                               `json:"action"`
	Category inventree.Category                   `json:"category"`
	Blocking PartCategoryDeleteBlockingReferences `json:"blocking"`
}

type PartCategoryDeleteOutput struct {
	Status        string                                `json:"status"`
	Record        *inventree.Category                   `json:"record,omitempty"`
	Blocking      *PartCategoryDeleteBlockingReferences `json:"blocking,omitempty"`
	PlanHash      string                                `json:"plan_hash,omitempty"`
	Verified      bool                                  `json:"verified,omitempty"`
	Recovered     bool                                  `json:"recovered,omitempty"`
	RecoveryPlan  string                                `json:"recovery_plan,omitempty"`
	Clarification *ClarificationResponse                `json:"clarification,omitempty"`
}

type categoryDeleteConfirmation struct {
	digest     string
	principal  string
	categoryID int
	expiresAt  time.Time
}

type categoryDeletePlanStore struct {
	mu                     sync.Mutex
	entries                map[string]categoryDeleteConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newCategoryDeletePlanStore(now func() time.Time, token func() (string, error)) *categoryDeletePlanStore {
	return &categoryDeletePlanStore{
		entries: map[string]categoryDeleteConfirmation{}, now: now, token: token, principal: stockPlanPrincipal,
		maxEntries: categoryDeletePlanMaxEntries, maxEntriesPerPrincipal: categoryDeletePlanMaxEntriesPerPrincipal,
	}
}

func (s *categoryDeletePlanStore) issue(ctx context.Context, plan PartCategoryDeletePlan) (string, error) {
	digest, err := categoryDeletePlanDigest(plan)
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
	principalEntries := 0
	for key, entry := range s.entries {
		if !now.Before(entry.expiresAt) || entry.principal == principal && entry.categoryID == plan.Category.PK {
			delete(s.entries, key)
			continue
		}
		if entry.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errors.New("too many outstanding part-category deletion confirmation plans")
	}
	s.entries[token] = categoryDeleteConfirmation{digest: digest, principal: principal, categoryID: plan.Category.PK, expiresAt: now.Add(categoryDeletePlanLifetime)}
	return token, nil
}

func (s *categoryDeletePlanStore) consume(ctx context.Context, token string, plan PartCategoryDeletePlan) bool {
	digest, err := categoryDeletePlanDigest(plan)
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
	if !ok || entry.digest != digest || entry.principal != s.principal(ctx) || entry.categoryID != plan.Category.PK || !now.Before(entry.expiresAt) {
		return false
	}
	delete(s.entries, token)
	return true
}

func categoryDeletePlanDigest(plan PartCategoryDeletePlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerCategoryDeleteTool(server *mcp.Server, deps Dependencies) {
	if deps.categoryDeletePlanStore == nil {
		deps.categoryDeletePlanStore = newCategoryDeletePlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, DeletePartCategoryToolName, "Delete part category", "Previews and confirms deleting one empty, unreferenced leaf part category, refusing while any dependency remains.", deletePartCategory(deps))
}

func deletePartCategory(deps Dependencies) mcp.ToolHandlerFor[DeletePartCategoryInput, PartCategoryDeleteOutput] {
	return LookupHandler[CategoryDeleteClient, DeletePartCategoryInput, PartCategoryDeleteOutput](deps, DeletePartCategoryToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CategoryDeleteClient, input DeletePartCategoryInput) (*mcp.CallToolResult, PartCategoryDeleteOutput, error) {
			if input.ID <= 0 {
				clarification := NewClarification("Which part category should be deleted?", "id", "id must be positive", "id", true, nil, map[string]any{"id": input.ID})
				return TextResult(StatusClarificationRequired), PartCategoryDeleteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			record, err := client.GetPartCategory(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PartCategoryDeleteOutput{Status: StatusNotFound}, nil
			}
			if err != nil || record.PK != input.ID {
				return nil, PartCategoryDeleteOutput{}, safeCategoryError("part-category delete lookup")
			}

			blocking, err := loadCategoryDeleteBlockingReferences(ctx, client, record.PK)
			if err != nil {
				if errors.Is(err, errCategoryDeleteReferenceScanLimit) {
					clarification := NewClarification("How should this completeness-sensitive reference scan be narrowed or reviewed?", "blocking", err.Error(), "id", true, nil, map[string]any{"id": input.ID})
					return TextResult(StatusClarificationRequired), PartCategoryDeleteOutput{Status: StatusClarificationRequired, Record: &record, Clarification: &clarification}, nil
				}
				return nil, PartCategoryDeleteOutput{}, safeCategoryError("part-category reference preflight")
			}
			if !blocking.empty() {
				return categoryDeleteUnsafeClarification(record, blocking)
			}

			plan := PartCategoryDeletePlan{Action: DeletePartCategoryToolName, Category: record, Blocking: blocking}
			if !input.Confirm {
				return issueCategoryDeletePlan(ctx, deps.categoryDeletePlanStore, plan)
			}
			if deps.categoryDeletePlanStore == nil || input.PlanHash == "" || !deps.categoryDeletePlanStore.consume(ctx, input.PlanHash, plan) {
				return categoryDeleteStaleClarification(record)
			}

			mutationErr := client.DeletePartCategory(ctx, record.PK)
			return verifyCategoryDeletion(ctx, client, record, mutationErr)
		})
}

// verifyCategoryDeletion reads the category back by its exact stable ID
// after a delete attempt and classifies the result: verified absence
// (success, optionally recovered from an ambiguous mutation response),
// confirmed survival, or an unverifiable read-back -- both of the latter two
// return partial_failure with read-before-retry guidance rather than a
// false success or silent data loss.
func verifyCategoryDeletion(ctx context.Context, client CategoryDeleteClient, record inventree.Category, mutationErr error) (*mcp.CallToolResult, PartCategoryDeleteOutput, error) {
	if mutationErr != nil {
		if errors.Is(mutationErr, context.Canceled) {
			return nil, PartCategoryDeleteOutput{}, context.Canceled
		}
		if errors.Is(mutationErr, context.DeadlineExceeded) {
			return nil, PartCategoryDeleteOutput{}, context.DeadlineExceeded
		}
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, PartCategoryDeleteOutput{}, safeCategoryError("part-category deletion")
		}
	}
	_, err := client.GetPartCategory(ctx, record.PK)
	if isNotFound(err) {
		return TextResult(StatusOK), PartCategoryDeleteOutput{Status: StatusOK, Record: &record, Verified: true, Recovered: mutationErr != nil}, nil
	}
	if errors.Is(err, context.Canceled) {
		return nil, PartCategoryDeleteOutput{}, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, PartCategoryDeleteOutput{}, context.DeadlineExceeded
	}
	if err == nil {
		return TextResult(StatusPartialFailure), PartCategoryDeleteOutput{Status: StatusPartialFailure, Record: &record, RecoveryPlan: "The category still exists after deletion; read the exact stable ID before retrying."}, nil
	}
	return TextResult(StatusPartialFailure), PartCategoryDeleteOutput{Status: StatusPartialFailure, Record: &record, RecoveryPlan: "Deletion read-back could not prove absence; read the exact stable ID before retrying."}, nil
}

func issueCategoryDeletePlan(ctx context.Context, store *categoryDeletePlanStore, plan PartCategoryDeletePlan) (*mcp.CallToolResult, PartCategoryDeleteOutput, error) {
	if store == nil {
		return nil, PartCategoryDeleteOutput{}, errors.New("part-category deletion confirmation store is unavailable")
	}
	token, err := store.issue(ctx, plan)
	if err != nil {
		return nil, PartCategoryDeleteOutput{}, err
	}
	clarification := NewClarification("Delete this exact, unreferenced part category?", "confirmation", "delete_part_category requires confirm:true with this plan_hash after reviewing the exact current category and its empty dependency snapshot", "plan_hash", false, []ClarificationCandidate{candidateFor(plan.Category)}, map[string]any{"id": plan.Category.PK, "confirm": true, "plan_hash": token})
	return TextResult(StatusClarificationRequired), PartCategoryDeleteOutput{Status: StatusClarificationRequired, Record: &plan.Category, Blocking: &plan.Blocking, PlanHash: token, Clarification: &clarification}, nil
}

func categoryDeleteStaleClarification(record inventree.Category) (*mcp.CallToolResult, PartCategoryDeleteOutput, error) {
	clarification := NewClarification("Which current deletion plan should authorize this part-category deletion?", "confirmation", "the plan is missing, stale, expired, reused, or belongs to another principal; preview the current category and its dependencies again", "plan_hash", false, nil, map[string]any{"id": record.PK})
	return TextResult(StatusClarificationRequired), PartCategoryDeleteOutput{Status: StatusClarificationRequired, Record: &record, Clarification: &clarification}, nil
}

func categoryDeleteUnsafeClarification(record inventree.Category, blocking PartCategoryDeleteBlockingReferences) (*mcp.CallToolResult, PartCategoryDeleteOutput, error) {
	clarification := NewClarification("Which safe action addresses this category instead of deleting it?", "blocking", "direct parts, direct child categories, category-parameter-template/default links, or generic part.partcategory parameter values still reference it; move, relink, or remove the reported records explicitly first -- this tool never cascades, reparents, or rewrites references", "id", true, []ClarificationCandidate{candidateFor(record)}, map[string]any{"id": record.PK})
	return TextResult(StatusClarificationRequired), PartCategoryDeleteOutput{Status: StatusClarificationRequired, Record: &record, Blocking: &blocking, Clarification: &clarification}, nil
}

// loadCategoryDeleteBlockingReferences scans every reference surface listed
// in inventree.CategoryDeleteReferenceInventory: direct parts, direct child
// categories, category-parameter-template/default links, and generic
// part.partcategory parameter values. Every scan is bounded and fails closed
// so an incomplete scan can never understate the impact of deleting the
// category.
func loadCategoryDeleteBlockingReferences(ctx context.Context, client CategoryDeleteClient, categoryID int) (PartCategoryDeleteBlockingReferences, error) {
	parts, err := categoryDeleteDirectPartIDs(ctx, client, categoryID)
	if err != nil {
		return PartCategoryDeleteBlockingReferences{}, err
	}
	children, err := categoryDeleteChildCategoryIDs(ctx, client, categoryID)
	if err != nil {
		return PartCategoryDeleteBlockingReferences{}, err
	}
	templateLinks, err := categoryDeleteParameterTemplateLinkIDs(ctx, client, categoryID)
	if err != nil {
		return PartCategoryDeleteBlockingReferences{}, err
	}
	parameterValues, err := categoryDeleteParameterValueIDs(ctx, client, categoryID)
	if err != nil {
		return PartCategoryDeleteBlockingReferences{}, err
	}
	return PartCategoryDeleteBlockingReferences{
		PartIDs:                  parts,
		ChildCategoryIDs:         children,
		ParameterTemplateLinkIDs: templateLinks,
		ParameterValueIDs:        parameterValues,
	}, nil
}

func categoryDeleteDirectPartIDs(ctx context.Context, client CategoryDeleteClient, categoryID int) ([]int, error) {
	cascade := false
	ids := make([]int, 0)
	for offset := 0; offset < categoryDeleteReferenceScanLimit; offset += categoryDeleteReferencePageSize {
		page, err := client.SearchPartsPage(ctx, inventree.PartQuery{CategoryID: categoryID, Cascade: &cascade, Limit: categoryDeleteReferencePageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		if page.Count > categoryDeleteReferenceScanLimit {
			return nil, errCategoryDeleteReferenceScanLimit
		}
		for _, part := range page.Results {
			ids = append(ids, part.PK)
		}
		if err := validateCategoryDeletePage(page.Count, len(page.Results), len(ids), page.HasMore); err != nil {
			return nil, err
		}
		if !page.HasMore {
			slices.Sort(ids)
			return ids, nil
		}
	}
	return nil, errCategoryDeleteReferenceScanLimit
}

func categoryDeleteChildCategoryIDs(ctx context.Context, client CategoryDeleteClient, categoryID int) ([]int, error) {
	ids := make([]int, 0)
	for offset := 0; offset < categoryDeleteReferenceScanLimit; offset += categoryDeleteReferencePageSize {
		page, err := client.SearchPartCategoriesPage(ctx, inventree.CategoryQuery{Parent: &categoryID, Limit: categoryDeleteReferencePageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		if page.Count > categoryDeleteReferenceScanLimit {
			return nil, errCategoryDeleteReferenceScanLimit
		}
		for _, child := range page.Results {
			ids = append(ids, child.PK)
		}
		if err := validateCategoryDeletePage(page.Count, len(page.Results), len(ids), page.HasMore); err != nil {
			return nil, err
		}
		if !page.HasMore {
			slices.Sort(ids)
			return ids, nil
		}
	}
	return nil, errCategoryDeleteReferenceScanLimit
}

func categoryDeleteParameterTemplateLinkIDs(ctx context.Context, client CategoryDeleteClient, categoryID int) ([]int, error) {
	fetchParent := false
	ids := make([]int, 0)
	seen := 0
	for offset := 0; offset < categoryDeleteReferenceScanLimit; offset += categoryDeleteReferencePageSize {
		// InvenTree's list endpoint has historically ignored the category
		// filter in some versions. Scan the complete bounded result set and
		// filter exact category IDs locally so an ignored filter cannot hide a
		// link that would be cascade-deleted by the upstream DELETE.
		page, err := client.SearchCategoryParameterTemplatesPage(ctx, inventree.CategoryParameterTemplateQuery{FetchParent: &fetchParent, Limit: categoryDeleteReferencePageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		if page.Count > categoryDeleteReferenceScanLimit {
			return nil, errCategoryDeleteReferenceScanLimit
		}
		for _, link := range page.Results {
			if link.Category == categoryID {
				ids = append(ids, link.PK)
			}
		}
		seen += len(page.Results)
		hasMore := page.Next != nil && *page.Next != ""
		if err := validateCategoryDeletePage(page.Count, len(page.Results), seen, hasMore); err != nil {
			return nil, err
		}
		if !hasMore {
			slices.Sort(ids)
			return ids, nil
		}
	}
	return nil, errCategoryDeleteReferenceScanLimit
}

func categoryDeleteParameterValueIDs(ctx context.Context, client CategoryDeleteClient, categoryID int) ([]int, error) {
	ids := make([]int, 0)
	for offset := 0; offset < categoryDeleteReferenceScanLimit; offset += categoryDeleteReferencePageSize {
		page, err := client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: "part.partcategory", ModelID: categoryID, Limit: categoryDeleteReferencePageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		if page.Count > categoryDeleteReferenceScanLimit {
			return nil, errCategoryDeleteReferenceScanLimit
		}
		for _, parameter := range page.Results {
			ids = append(ids, parameter.PK)
		}
		if err := validateCategoryDeletePage(page.Count, len(page.Results), len(ids), page.HasMore); err != nil {
			return nil, err
		}
		if !page.HasMore {
			slices.Sort(ids)
			return ids, nil
		}
	}
	return nil, errCategoryDeleteReferenceScanLimit
}

func validateCategoryDeletePage(count, pageResults, seen int, hasMore bool) error {
	if count < 0 || pageResults > count || seen > count || (hasMore && pageResults == 0) {
		return errCategoryDeleteReferenceScanLimit
	}
	if !hasMore && seen != count {
		return errCategoryDeleteReferenceScanLimit
	}
	return nil
}
