package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	companyRolePlanLifetime               = 5 * time.Minute
	companyRolePlanMaxEntries             = 4096
	companyRolePlanMaxEntriesPerPrincipal = 64
)

// CompanyRoleClient is the narrow client surface needed to preview, audit,
// and execute guarded company customer-role removal. The stock-item and
// sales-order dependency checks are bounded single-request existence
// counts, not general search/list capability.
type CompanyRoleClient interface {
	GetCompanyDetail(context.Context, int) (inventree.CompanyDetail, error)
	UpdateCompany(context.Context, int, inventree.PatchFields) (inventree.CompanyDetail, error)
	SearchStockItemsPage(context.Context, inventree.StockItemQuery) (inventree.StockItemPage, error)
	SearchSalesOrdersPage(context.Context, inventree.SalesOrderQuery) (inventree.SalesOrderPage, error)
}

type RemoveCompanyCustomerRoleInput struct {
	CompanyID int    `json:"company_id" jsonschema:"Stable company primary key currently holding the customer role."`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"Confirm removal of the exact previewed customer role."`
	PlanHash  string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

// CompanyRolePlan is the state bound into the confirmation token. It
// captures only the company identity, name, and role being removed;
// dependency counts are re-audited fresh at execution time rather than
// bound into the token, so any dependency that appears after the preview
// still blocks execution even with a valid token.
type CompanyRolePlan struct {
	Action    string `json:"action"`
	CompanyID int    `json:"company_id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
}

type CompanyRoleMutationOutput struct {
	Status                string                 `json:"status"`
	Record                *CompanyView           `json:"record,omitempty"`
	Plan                  *CompanyRolePlan       `json:"plan,omitempty"`
	PlanHash              string                 `json:"plan_hash,omitempty"`
	DependencyStockItems  int                    `json:"dependency_stock_items,omitempty"`
	DependencySalesOrders int                    `json:"dependency_sales_orders,omitempty"`
	Verified              bool                   `json:"verified,omitempty"`
	Recovered             bool                   `json:"recovered,omitempty"`
	Validation            *ValidationFailure     `json:"validation,omitempty"`
	Clarification         *ClarificationResponse `json:"clarification,omitempty"`
	RecoveryPlan          string                 `json:"recovery_plan,omitempty"`
}

type companyRoleConfirmation struct {
	digest    string
	principal string
	companyID int
	expiresAt time.Time
}

type companyRolePlanStore struct {
	mu                     sync.Mutex
	entries                map[string]companyRoleConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newCompanyRolePlanStore(now func() time.Time, token func() (string, error)) *companyRolePlanStore {
	return &companyRolePlanStore{
		entries: map[string]companyRoleConfirmation{}, now: now, token: token, principal: stockPlanPrincipal,
		maxEntries: companyRolePlanMaxEntries, maxEntriesPerPrincipal: companyRolePlanMaxEntriesPerPrincipal,
	}
}

func (s *companyRolePlanStore) issue(ctx context.Context, plan CompanyRolePlan) (string, error) {
	digest, err := companyRolePlanDigest(plan)
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
		if !now.Before(entry.expiresAt) || entry.principal == principal && entry.companyID == plan.CompanyID {
			delete(s.entries, key)
			continue
		}
		if entry.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errors.New("too many outstanding company-role confirmation plans")
	}
	s.entries[token] = companyRoleConfirmation{digest: digest, principal: principal, companyID: plan.CompanyID, expiresAt: now.Add(companyRolePlanLifetime)}
	return token, nil
}

func (s *companyRolePlanStore) consume(ctx context.Context, token string, plan CompanyRolePlan) bool {
	digest, err := companyRolePlanDigest(plan)
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
	if !ok || entry.digest != digest || entry.principal != s.principal(ctx) || entry.companyID != plan.CompanyID || !now.Before(entry.expiresAt) {
		return false
	}
	delete(s.entries, token)
	return true
}

func companyRolePlanDigest(plan CompanyRolePlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerCompanyRoleWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.companyRolePlanStore == nil {
		deps.companyRolePlanStore = newCompanyRolePlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, RemoveCompanyCustomerRoleToolName, "Remove company customer role",
		"Previews and confirms removal of the customer role after a bounded stock and sales-order dependency audit.",
		removeCompanyCustomerRole(deps))
}

func removeCompanyCustomerRole(deps Dependencies) mcp.ToolHandlerFor[RemoveCompanyCustomerRoleInput, CompanyRoleMutationOutput] {
	return LookupHandler[CompanyRoleClient, RemoveCompanyCustomerRoleInput, CompanyRoleMutationOutput](deps, RemoveCompanyCustomerRoleToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyRoleClient, input RemoveCompanyCustomerRoleInput) (*mcp.CallToolResult, CompanyRoleMutationOutput, error) {
			if input.CompanyID <= 0 {
				return companyRoleValidation("company_id must be a positive stable company primary key")
			}
			before, err := client.GetCompanyDetail(ctx, input.CompanyID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), CompanyRoleMutationOutput{Status: StatusNotFound}, nil
			}
			if err != nil || before.PK != input.CompanyID {
				return nil, CompanyRoleMutationOutput{}, safeCompanyAdminError("company lookup", err)
			}
			if !before.IsCustomer {
				return companyRoleValidation("company does not currently hold the customer role")
			}
			plan := CompanyRolePlan{Action: "remove_customer_role", CompanyID: before.PK, Name: before.Name, Role: "customer"}
			if !input.Confirm {
				stockCount, soCount, auditErr := companyCustomerDependencyAudit(ctx, client, input.CompanyID)
				if auditErr != nil {
					return nil, CompanyRoleMutationOutput{}, auditErr
				}
				if stockCount > 0 || soCount > 0 {
					return companyRoleDependencyBlocked(stockCount, soCount)
				}
				return issueCompanyRolePlan(ctx, deps.companyRolePlanStore, plan)
			}
			if deps.companyRolePlanStore == nil || input.PlanHash == "" {
				return companyRolePlanClarification(plan, "The plan is missing, stale, expired, reused, or belongs to another principal; preview the current company again.")
			}
			// Audit fresh, immediately before consuming the token and mutating,
			// rather than reusing the preview-time result: the token binds
			// identity and role, not the dependency snapshot, so a dependency
			// created after the preview must still block execution even with a
			// valid token. Auditing before consume (not after) means a blocked
			// confirm attempt never spends the single-use token, so the same
			// plan_hash remains usable once the dependency clears.
			stockCount, soCount, auditErr := companyCustomerDependencyAudit(ctx, client, input.CompanyID)
			if auditErr != nil {
				return nil, CompanyRoleMutationOutput{}, auditErr
			}
			if stockCount > 0 || soCount > 0 {
				return companyRoleDependencyBlocked(stockCount, soCount)
			}
			if !deps.companyRolePlanStore.consume(ctx, input.PlanHash, plan) {
				return companyRolePlanClarification(plan, "The plan is missing, stale, expired, reused, or belongs to another principal; preview the current company again.")
			}
			mutationErr := updateCompanyCustomerRole(ctx, client, input.CompanyID)
			return verifyCompanyRoleRemoval(ctx, client, before, mutationErr)
		})
}

func updateCompanyCustomerRole(ctx context.Context, client CompanyRoleClient, companyID int) error {
	_, err := client.UpdateCompany(ctx, companyID, inventree.PatchFields{"is_customer": inventree.Set(false)})
	return err
}

// companyCustomerDependencyAudit proves whether any stock item or sales
// order still references the company as customer using bounded
// single-request existence counts: InvenTree's paginated Count is exact
// regardless of the requested page size, so a limit-1 request is sufficient
// to prove completeness without scanning every matching record.
func companyCustomerDependencyAudit(ctx context.Context, client CompanyRoleClient, companyID int) (int, int, error) {
	stockPage, err := client.SearchStockItemsPage(ctx, inventree.StockItemQuery{Customer: companyID, Limit: 1})
	if err != nil {
		return 0, 0, fmt.Errorf("customer-role stock dependency audit failed: %w", err)
	}
	soPage, err := client.SearchSalesOrdersPage(ctx, inventree.SalesOrderQuery{Customer: companyID, Limit: 1})
	if err != nil {
		return 0, 0, fmt.Errorf("customer-role sales-order dependency audit failed: %w", err)
	}
	return stockPage.Count, soPage.Count, nil
}

func verifyCompanyRoleRemoval(ctx context.Context, client CompanyRoleClient, before inventree.CompanyDetail, mutationErr error) (*mcp.CallToolResult, CompanyRoleMutationOutput, error) {
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, CompanyRoleMutationOutput{}, errors.New("company customer-role removal was rejected")
		}
	}
	current, err := client.GetCompanyDetail(ctx, before.PK)
	if err != nil || current.PK != before.PK || current.IsCustomer {
		return TextResult(StatusPartialFailure), CompanyRoleMutationOutput{Status: StatusPartialFailure, RecoveryPlan: fmt.Sprintf("Read company_id %d before retrying; the customer-role removal could not be verified.", before.PK)}, nil
	}
	view := companyView(current)
	return TextResult(StatusOK), CompanyRoleMutationOutput{Status: StatusOK, Record: &view, Verified: true, Recovered: mutationErr != nil}, nil
}

func issueCompanyRolePlan(ctx context.Context, store *companyRolePlanStore, plan CompanyRolePlan) (*mcp.CallToolResult, CompanyRoleMutationOutput, error) {
	if store == nil {
		return nil, CompanyRoleMutationOutput{}, errors.New("company-role confirmation store is unavailable")
	}
	token, err := store.issue(ctx, plan)
	if err != nil {
		return nil, CompanyRoleMutationOutput{}, err
	}
	clarification := NewClarification("Remove the customer role from this exact company?", "confirmation", "review the exact current company state, then provide confirm:true with this plan_hash", "plan_hash", false, nil, map[string]any{"company_id": plan.CompanyID, "confirm": true, "plan_hash": token})
	return TextResult(StatusClarificationRequired), CompanyRoleMutationOutput{Status: StatusClarificationRequired, Plan: &plan, PlanHash: token, Clarification: &clarification}, nil
}

func companyRolePlanClarification(plan CompanyRolePlan, reason string) (*mcp.CallToolResult, CompanyRoleMutationOutput, error) {
	clarification := NewClarification("Which current company-role plan should authorize this change?", "confirmation", reason, "plan_hash", false, nil, map[string]any{"company_id": plan.CompanyID})
	return TextResult(StatusClarificationRequired), CompanyRoleMutationOutput{Status: StatusClarificationRequired, Plan: &plan, Clarification: &clarification}, nil
}

func companyRoleDependencyBlocked(stockCount, soCount int) (*mcp.CallToolResult, CompanyRoleMutationOutput, error) {
	return TextResult(StatusClarificationRequired), CompanyRoleMutationOutput{
		Status: StatusClarificationRequired, DependencyStockItems: stockCount, DependencySalesOrders: soCount,
		RecoveryPlan: "The customer role cannot be removed while stock items or sales orders still reference this company as customer. Reassign or resolve those dependencies before retrying.",
	}, nil
}

func companyRoleValidation(message string) (*mcp.CallToolResult, CompanyRoleMutationOutput, error) {
	return TextResult(StatusValidationFailed), CompanyRoleMutationOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "company", Messages: []string{message}}}}}, nil
}
