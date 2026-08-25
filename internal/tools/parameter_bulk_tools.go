package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/telemetry"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	parameterAuditScanLimit         = 1000
	parameterBulkPartLimit          = 100
	parameterPlanLifetime           = 5 * time.Minute
	parameterPlanMaxEntries         = 4096
	parameterPlanMaxEntriesPerOwner = 64
	parameterBulkPageSize           = 100
)

var (
	errParameterAuditScanLimit = errors.New("parameter audit scan safety limit exceeded")
	errParameterPlanCapacity   = errors.New("too many outstanding parameter propagation plans")
)

type parameterScanBudget struct {
	remaining int
}

func newParameterScanBudget() *parameterScanBudget {
	return &parameterScanBudget{remaining: parameterAuditScanLimit}
}

func (b *parameterScanBudget) charge(records int) error {
	if records < 0 || b.remaining < records+1 {
		return errParameterAuditScanLimit
	}
	b.remaining -= records + 1 // one upstream request plus every returned record
	return nil
}

func (b *parameterScanBudget) pageSize() int {
	return min(parameterBulkPageSize, max(0, b.remaining-1))
}

type ParameterAuditClient interface {
	GetPartCategory(context.Context, int) (inventree.Category, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
	SearchParameterTemplatesPage(context.Context, inventree.SearchQuery) (inventree.Page[inventree.ParameterTemplate], error)
	SearchTemplateParametersPage(context.Context, inventree.TemplateParameterQuery) (inventree.PartParameterPage, error)
	SearchPartParametersPage(context.Context, inventree.PartParameterQuery) (inventree.PartParameterPage, error)
	SearchPartsPage(context.Context, inventree.PartQuery) (inventree.PartPage, error)
	SearchCategoryParameterTemplatesPage(context.Context, inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error)
	GetPart(context.Context, int) (inventree.Part, error)
}

type BulkParameterClient interface {
	GetPart(context.Context, int) (inventree.Part, error)
	GetPartCategory(context.Context, int) (inventree.Category, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
	SearchPartsPage(context.Context, inventree.PartQuery) (inventree.PartPage, error)
	SearchPartParametersPage(context.Context, inventree.PartParameterQuery) (inventree.PartParameterPage, error)
	SearchCategoryParameterTemplatesPage(context.Context, inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error)
	GetPartParameter(context.Context, int) (inventree.Parameter, error)
	CreatePartParameter(context.Context, inventree.ParameterCreate) (inventree.Parameter, error)
	UpdatePartParameter(context.Context, int, inventree.PatchFields) (inventree.Parameter, error)
}

type AuditParameterConsistencyInput struct {
	TemplateID int `json:"template_id,omitempty" jsonschema:"Optional existing template ID used to narrow the read-only audit."`
	CategoryID int `json:"category_id,omitempty" jsonschema:"Optional exact part-category ID used to narrow part-row and category-default findings."`
}

type ParameterAuditFinding struct {
	Kind           string   `json:"kind"`
	Severity       string   `json:"severity"`
	Message        string   `json:"message"`
	TemplateIDs    []int    `json:"template_ids,omitempty"`
	ParameterIDs   []int    `json:"parameter_ids,omitempty"`
	CategoryIDs    []int    `json:"category_ids,omitempty"`
	PartIDs        []int    `json:"part_ids,omitempty"`
	CurrentValue   string   `json:"current_value,omitempty"`
	ExpectedValues []string `json:"expected_values,omitempty"`
}

type ParameterAuditOutput struct {
	Status        string                  `json:"status"`
	TemplatesRead int                     `json:"templates_read"`
	RowsRead      int                     `json:"rows_read"`
	LinksRead     int                     `json:"links_read"`
	Findings      []ParameterAuditFinding `json:"findings"`
	Clarification *ClarificationResponse  `json:"clarification,omitempty"`
}

type BulkPropagatePartParametersInput struct {
	DryRun               bool    `json:"dry_run,omitempty" jsonschema:"Required true to prepare a current-state-bound propagation plan before execution."`
	Confirm              bool    `json:"confirm,omitempty" jsonschema:"Required true to execute the matching reviewed plan."`
	PlanHash             string  `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound five-minute single-use token returned by dry_run:true."`
	TemplateID           int     `json:"template_id" jsonschema:"One existing enabled parameter-template primary key."`
	Value                *string `json:"value,omitempty" jsonschema:"Explicit value to propagate, including an explicitly empty string."`
	PartIDs              []int   `json:"part_ids,omitempty" jsonschema:"Explicit stable part IDs; mutually exclusive with category_id and limited to 100 unique parts."`
	CategoryID           int     `json:"category_id,omitempty" jsonschema:"Exact category selector; mutually exclusive with part_ids."`
	IncludeSubcategories bool    `json:"include_subcategories,omitempty" jsonschema:"Include descendant categories when category_id is used."`
	OverwriteExisting    bool    `json:"overwrite_existing,omitempty" jsonschema:"Explicitly permit updates of one existing differing row; false reports it for manual review."`
}

type BulkParameterAction struct {
	inventree.WebLinkFields
	PartID        int     `json:"part_id"`
	PartName      string  `json:"part_name"`
	CategoryID    *int    `json:"category_id,omitempty"`
	TemplateID    int     `json:"template_id"`
	ParameterID   int     `json:"parameter_id,omitempty"`
	Action        string  `json:"action"`
	Status        string  `json:"status"`
	BeforeValue   *string `json:"before_value,omitempty"`
	ProposedValue string  `json:"proposed_value"`
	Verified      bool    `json:"verified,omitempty"`
	Reason        string  `json:"reason,omitempty"`
}

type BulkParameterFailure struct {
	PartID       int    `json:"part_id"`
	Action       string `json:"action"`
	Message      string `json:"message"`
	RecoveryPlan string `json:"recovery_plan"`
}

type BulkParameterPlan struct {
	TemplateID           int                   `json:"template_id"`
	TemplateName         string                `json:"template_name"`
	Value                string                `json:"value"`
	PartIDs              []int                 `json:"part_ids,omitempty"`
	CategoryID           int                   `json:"category_id,omitempty"`
	IncludeSubcategories bool                  `json:"include_subcategories"`
	OverwriteExisting    bool                  `json:"overwrite_existing"`
	Actions              []BulkParameterAction `json:"actions"`
}

type BulkParameterOutput struct {
	Status         string                 `json:"status"`
	DryRun         bool                   `json:"dry_run"`
	PlanHash       string                 `json:"plan_hash,omitempty"`
	Plan           *BulkParameterPlan     `json:"plan,omitempty"`
	Applied        int                    `json:"applied"`
	Skipped        int                    `json:"skipped"`
	Failed         int                    `json:"failed"`
	ManualRequired int                    `json:"manual_required"`
	Failures       []BulkParameterFailure `json:"failures,omitempty"`
	Clarification  *ClarificationResponse `json:"clarification,omitempty"`
}

type parameterPlanConfirmation struct {
	digest    string
	principal string
	expiresAt time.Time
	selector  string
}

type parameterPlanStore struct {
	mu                     sync.Mutex
	entries                map[string]parameterPlanConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newParameterPlanStore(now func() time.Time, token func() (string, error)) *parameterPlanStore {
	return &parameterPlanStore{
		entries:                map[string]parameterPlanConfirmation{},
		now:                    now,
		token:                  token,
		principal:              stockPlanPrincipal,
		maxEntries:             parameterPlanMaxEntries,
		maxEntriesPerPrincipal: parameterPlanMaxEntriesPerOwner,
	}
}

func (s *parameterPlanStore) issue(ctx context.Context, plan BulkParameterPlan) (string, error) {
	digest, err := bulkParameterPlanDigest(plan)
	if err != nil {
		return "", err
	}
	token, err := s.token()
	if err != nil {
		return "", err
	}
	now := s.now()
	principal := s.principal(ctx)
	selector := bulkParameterSelector(plan)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpired(now)
	principalEntries := 0
	for existingToken, confirmation := range s.entries {
		if confirmation.principal == principal && confirmation.selector == selector {
			delete(s.entries, existingToken)
			continue
		}
		if confirmation.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errParameterPlanCapacity
	}
	s.entries[token] = parameterPlanConfirmation{digest: digest, principal: principal, expiresAt: now.Add(parameterPlanLifetime), selector: selector}
	return token, nil
}

func (s *parameterPlanStore) consume(ctx context.Context, token string, plan BulkParameterPlan) bool {
	digest, err := bulkParameterPlanDigest(plan)
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
	valid := confirmation.digest == digest && confirmation.principal == s.principal(ctx) && confirmation.selector == bulkParameterSelector(plan) && now.Before(confirmation.expiresAt)
	if valid {
		delete(s.entries, token)
	}
	return valid
}

func (s *parameterPlanStore) deleteExpired(now time.Time) {
	for token, confirmation := range s.entries {
		if !now.Before(confirmation.expiresAt) {
			delete(s.entries, token)
		}
	}
}

func registerParameterBulkLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, AuditParameterConsistencyToolName, "Audit parameter consistency", "Audits bounded parameter-template, category-default, and part-row consistency without writing.", auditParameterConsistency(deps))
}

func registerParameterBulkWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.parameterPlanStore == nil {
		deps.parameterPlanStore = newParameterPlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, BulkPropagatePartParametersToolName, "Bulk propagate part parameters", "Plans or confirms bounded single-template propagation across explicit parts or categories.", bulkPropagatePartParameters(deps))
}

func auditParameterConsistency(deps Dependencies) mcp.ToolHandlerFor[AuditParameterConsistencyInput, ParameterAuditOutput] {
	return LookupHandler[ParameterAuditClient, AuditParameterConsistencyInput, ParameterAuditOutput](deps, AuditParameterConsistencyToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ParameterAuditClient, input AuditParameterConsistencyInput) (*mcp.CallToolResult, ParameterAuditOutput, error) {
			budget := newParameterScanBudget()
			if input.TemplateID < 0 || input.CategoryID < 0 {
				return parameterAuditClarification("Which valid audit filters should be used?", "filters", "template_id and category_id cannot be negative", input)
			}
			if input.CategoryID > 0 {
				if err := budget.charge(0); err != nil {
					return parameterAuditResultForError(input, err)
				}
				category, err := client.GetPartCategory(ctx, input.CategoryID)
				if err != nil {
					if isNotFound(err) {
						return parameterAuditClarification("Which existing category should narrow the audit?", "category_id", "category_id does not exist", input)
					}
					return nil, ParameterAuditOutput{}, err
				}
				if category.PK != input.CategoryID {
					return nil, ParameterAuditOutput{}, fmt.Errorf("part category identity mismatch: requested %d, received %d", input.CategoryID, category.PK)
				}
			}
			if input.TemplateID > 0 {
				if err := budget.charge(0); err != nil {
					return parameterAuditResultForError(input, err)
				}
				template, err := client.GetParameterTemplate(ctx, input.TemplateID)
				if err != nil {
					if isNotFound(err) {
						return parameterAuditClarification("Which existing template should narrow the audit?", "template_id", "template_id does not exist", input)
					}
					return nil, ParameterAuditOutput{}, err
				}
				if template.PK != input.TemplateID {
					return nil, ParameterAuditOutput{}, fmt.Errorf("parameter template identity mismatch: requested %d, received %d", input.TemplateID, template.PK)
				}
			}
			var templates []inventree.ParameterTemplate
			var err error
			if input.TemplateID > 0 || input.CategoryID == 0 {
				templates, err = auditTemplates(ctx, client, input.TemplateID, budget)
				if err != nil {
					return parameterAuditResultForError(input, err)
				}
			}
			rows, parts, err := auditParameterRows(ctx, client, templates, input.CategoryID, budget)
			if err != nil {
				return parameterAuditResultForError(input, err)
			}
			links, err := auditCategoryLinks(ctx, client, templates, input.CategoryID, budget)
			if err != nil {
				return parameterAuditResultForError(input, err)
			}
			if len(templates) == 0 {
				templates, err = auditRelevantTemplates(ctx, client, rows, links, budget)
				if err != nil {
					return parameterAuditResultForError(input, err)
				}
			}
			findings, filteredRows, err := parameterAuditFindings(ctx, client, templates, rows, links, parts, budget)
			if err != nil {
				return parameterAuditResultForError(input, err)
			}
			out := ParameterAuditOutput{Status: StatusOK, TemplatesRead: len(templates), RowsRead: filteredRows, LinksRead: len(links), Findings: findings}
			return TextResult(StatusOK), out, nil
		})
}

func bulkPropagatePartParameters(deps Dependencies) mcp.ToolHandlerFor[BulkPropagatePartParametersInput, BulkParameterOutput] {
	return LookupHandler[BulkParameterClient, BulkPropagatePartParametersInput, BulkParameterOutput](deps, BulkPropagatePartParametersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client BulkParameterClient, input BulkPropagatePartParametersInput) (*mcp.CallToolResult, BulkParameterOutput, error) {
			plan, result, out, err := buildBulkParameterPlan(ctx, client, input)
			if result != nil || err != nil {
				return result, out, err
			}
			out.Plan = &plan
			out.DryRun = input.DryRun
			countBulkParameterActions(&out)
			planned := plannedBulkParameterActions(plan.Actions)
			if input.DryRun {
				if planned == 0 {
					return TextResult(StatusOK), out, nil
				}
				if deps.parameterPlanStore == nil {
					return nil, out, errors.New("parameter propagation plan store is unavailable")
				}
				token, err := deps.parameterPlanStore.issue(ctx, plan)
				if err != nil {
					if errors.Is(err, errParameterPlanCapacity) {
						return bulkParameterClarification(out, "When should a new parameter propagation plan be prepared?", "confirmation", "too many parameter plans are outstanding; execute a reviewed plan or wait for a five-minute token to expire", "dry_run", bulkParameterRetryValues(input))
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkParameterClarification(out, "Should this reviewed parameter propagation now be executed?", "confirmation", "confirm must be true after reviewing dry_run:true output", "confirm", bulkParameterRetryValues(input))
			}
			if deps.parameterPlanStore == nil || input.PlanHash == "" || !deps.parameterPlanStore.consume(ctx, input.PlanHash, plan) {
				return bulkParameterClarification(out, "Which current dry-run plan should authorize this propagation?", "confirmation", "plan_hash must be the unexpired single-use token from a matching dry run by the same principal; changed inventory state requires a new dry run", "plan_hash", bulkParameterRetryValues(input))
			}
			telemetry.RecordBulkOperation(ctx, BulkPropagatePartParametersToolName, "started")
			executeBulkParameterPlan(ctx, client, &out)
			countBulkParameterActions(&out)
			if out.Failed > 0 {
				out.Status = StatusPartialFailure
				return TextResult(StatusPartialFailure), out, nil
			}
			return TextResult(StatusOK), out, nil
		})
}

func buildBulkParameterPlan(ctx context.Context, client BulkParameterClient, input BulkPropagatePartParametersInput) (BulkParameterPlan, *mcp.CallToolResult, BulkParameterOutput, error) {
	out := BulkParameterOutput{Status: StatusOK, DryRun: input.DryRun}
	if input.TemplateID <= 0 || input.Value == nil {
		result, clarified, err := bulkParameterClarification(out, "Which existing template and explicit value should be propagated?", "template_value", "template_id must be positive and value must be explicitly supplied, including for an empty value", "template_value", bulkParameterRetryValues(input))
		return BulkParameterPlan{}, result, clarified, err
	}
	if len(*input.Value) > 500 {
		result, clarified, err := bulkParameterClarification(out, "Which schema-valid parameter value should be propagated?", "value", "value exceeds the InvenTree 500-character parameter data limit", "value", bulkParameterRetryValues(input))
		return BulkParameterPlan{}, result, clarified, err
	}
	if (len(input.PartIDs) == 0) == (input.CategoryID == 0) || input.CategoryID < 0 {
		result, clarified, err := bulkParameterClarification(out, "Which bounded part selector should be used?", "parts", "provide exactly one of nonempty part_ids or a positive category_id", "parts", bulkParameterRetryValues(input))
		return BulkParameterPlan{}, result, clarified, err
	}
	if input.IncludeSubcategories && input.CategoryID == 0 {
		result, clarified, err := bulkParameterClarification(out, "Which category should include descendants?", "category_id", "include_subcategories is valid only with category_id", "category_id", bulkParameterRetryValues(input))
		return BulkParameterPlan{}, result, clarified, err
	}
	template, err := client.GetParameterTemplate(ctx, input.TemplateID)
	if err != nil {
		if isNotFound(err) {
			result, clarified, clarifyErr := bulkParameterClarification(out, "Which existing parameter template should be propagated?", "template_id", "template_id does not exist", "template_id", bulkParameterRetryValues(input))
			return BulkParameterPlan{}, result, clarified, clarifyErr
		}
		return BulkParameterPlan{}, nil, out, err
	}
	if template.PK != input.TemplateID {
		return BulkParameterPlan{}, nil, out, fmt.Errorf("parameter template identity mismatch: requested %d, received %d", input.TemplateID, template.PK)
	}
	if !template.Enabled || template.ModelType != nil && *template.ModelType != "" && *template.ModelType != "part.part" {
		result, clarified, clarifyErr := bulkParameterClarification(out, "Which enabled part-compatible template should be propagated?", "template_id", "template must be enabled and unrestricted or restricted to part.part", "template_id", bulkParameterRetryValues(input))
		return BulkParameterPlan{}, result, clarified, clarifyErr
	}
	parts, result, clarified, err := resolveBulkParameterParts(ctx, client, input, out)
	if result != nil || err != nil {
		return BulkParameterPlan{}, result, clarified, err
	}
	plan := BulkParameterPlan{
		TemplateID: input.TemplateID, TemplateName: template.Name, Value: *input.Value,
		CategoryID: input.CategoryID, IncludeSubcategories: input.IncludeSubcategories,
		OverwriteExisting: input.OverwriteExisting, Actions: make([]BulkParameterAction, 0, len(parts)),
	}
	linkBudget := newParameterScanBudget()
	linkCache := map[int]bool{}
	for _, part := range parts {
		plan.PartIDs = append(plan.PartIDs, part.PK)
		action, err := planBulkParameterPart(ctx, client, part, input.TemplateID, *input.Value, input.OverwriteExisting, linkCache, linkBudget)
		if err != nil {
			if errors.Is(err, errParameterAuditScanLimit) {
				result, clarified, clarifyErr := bulkParameterClarification(out, "Which narrower part set should be propagated?", "parts", "category-link preflight exceeds the shared 1,000 request-and-record safety budget", "parts", bulkParameterRetryValues(input))
				return BulkParameterPlan{}, result, clarified, clarifyErr
			}
			return BulkParameterPlan{}, nil, out, err
		}
		plan.Actions = append(plan.Actions, action)
	}
	return plan, nil, out, nil
}

func resolveBulkParameterParts(ctx context.Context, client BulkParameterClient, input BulkPropagatePartParametersInput, out BulkParameterOutput) ([]inventree.Part, *mcp.CallToolResult, BulkParameterOutput, error) {
	if input.CategoryID > 0 {
		category, err := client.GetPartCategory(ctx, input.CategoryID)
		if err != nil {
			if isNotFound(err) {
				result, clarified, clarifyErr := bulkParameterClarification(out, "Which existing category should select parts?", "category_id", "category_id does not exist", "category_id", bulkParameterRetryValues(input))
				return nil, result, clarified, clarifyErr
			}
			return nil, nil, out, err
		}
		if category.PK != input.CategoryID {
			return nil, nil, out, fmt.Errorf("part category identity mismatch: requested %d, received %d", input.CategoryID, category.PK)
		}
		page, err := client.SearchPartsPage(ctx, inventree.PartQuery{CategoryID: input.CategoryID, Cascade: dvgoutils.Ptr(input.IncludeSubcategories), Limit: parameterBulkPartLimit + 1})
		if err != nil {
			return nil, nil, out, err
		}
		if page.HasMore || page.Count > parameterBulkPartLimit || len(page.Results) > parameterBulkPartLimit {
			result, clarified, clarifyErr := bulkParameterClarification(out, "Which narrower category or explicit part list should be propagated?", "parts", "the category selector matches more than the 100-part plan bound", "parts", bulkParameterRetryValues(input))
			return nil, result, clarified, clarifyErr
		}
		slices.SortFunc(page.Results, func(a, b inventree.Part) int { return a.PK - b.PK })
		return page.Results, nil, out, nil
	}
	ids := append([]int(nil), input.PartIDs...)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > parameterBulkPartLimit {
		result, clarified, err := bulkParameterClarification(out, "Which at most 100 parts should be propagated?", "part_ids", "part_ids exceeds the 100-part plan bound", "part_ids", bulkParameterRetryValues(input))
		return nil, result, clarified, err
	}
	parts := make([]inventree.Part, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			result, clarified, err := bulkParameterClarification(out, "Which valid part IDs should be propagated?", "part_ids", "every part_id must be positive", "part_ids", bulkParameterRetryValues(input))
			return nil, result, clarified, err
		}
		part, err := client.GetPart(ctx, id)
		if err != nil {
			if isNotFound(err) {
				result, clarified, clarifyErr := bulkParameterClarification(out, "Which existing parts should be propagated?", "part_ids", fmt.Sprintf("part_id %d does not exist", id), "part_ids", bulkParameterRetryValues(input))
				return nil, result, clarified, clarifyErr
			}
			return nil, nil, out, err
		}
		if part.PK != id {
			return nil, nil, out, fmt.Errorf("part identity mismatch: requested %d, received %d", id, part.PK)
		}
		parts = append(parts, part)
	}
	return parts, nil, out, nil
}

func planBulkParameterPart(ctx context.Context, client BulkParameterClient, part inventree.Part, templateID int, value string, overwrite bool, linkCache map[int]bool, budget *parameterScanBudget) (BulkParameterAction, error) {
	action := BulkParameterAction{PartID: part.PK, PartName: part.Name, CategoryID: part.Category, TemplateID: templateID, ProposedValue: value, Action: "none", Status: "manual_required"}
	if part.Category == nil {
		action.Reason = "part has no category, so template linkage cannot be verified"
		return action, nil
	}
	linked, ok := linkCache[*part.Category]
	if !ok {
		var err error
		linked, err = partCategoryHasTemplate(ctx, client, *part.Category, templateID, budget)
		if err != nil {
			return BulkParameterAction{}, err
		}
		linkCache[*part.Category] = linked
	}
	if !linked {
		action.Reason = "template is not linked to the part category or its ancestors; category links are never created implicitly"
		return action, nil
	}
	page, err := client.SearchPartParametersPage(ctx, inventree.PartParameterQuery{PartID: part.PK, TemplateID: templateID, Limit: 3})
	if err != nil {
		return BulkParameterAction{}, err
	}
	if page.HasMore || len(page.Results) > 1 {
		action.ParameterID = firstParameterID(page.Results)
		action.Reason = "multiple existing rows use the same template; resolve duplicate rows manually"
		return action, nil
	}
	if len(page.Results) == 0 {
		action.Action = "create"
		action.Status = "planned"
		return action, nil
	}
	row := page.Results[0]
	if row.ModelType != "part.part" || row.ModelID != part.PK || row.Template != templateID {
		return BulkParameterAction{}, errors.New("parameter search returned a row with mismatched identity")
	}
	action.ParameterID = row.PK
	action.BeforeValue = dvgoutils.Ptr(row.Data)
	if row.Data == value {
		action.Status = "skipped"
		action.Reason = "existing value already matches"
		return action, nil
	}
	if !overwrite {
		action.Reason = "existing value differs and overwrite_existing is false"
		return action, nil
	}
	action.Action = "update"
	action.Status = "planned"
	return action, nil
}

func executeBulkParameterPlan(ctx context.Context, client BulkParameterClient, out *BulkParameterOutput) {
	if out.Plan == nil {
		return
	}
	stopped := false
	for index := range out.Plan.Actions {
		action := &out.Plan.Actions[index]
		if action.Status != "planned" {
			continue
		}
		if stopped {
			action.Status = "manual_required"
			action.Reason = "not attempted after an earlier write or verification failure"
			continue
		}
		var record inventree.Parameter
		var err error
		failureReason := "parameter action failed; inspect current state before retry"
		expectedID := action.ParameterID
		switch action.Action {
		case "create":
			var current inventree.PartParameterPage
			current, err = client.SearchPartParametersPage(ctx, inventree.PartParameterQuery{PartID: action.PartID, TemplateID: action.TemplateID, Limit: 2})
			if err != nil {
				failureReason = "create preflight failed; inspect current state before retry"
			}
			if err == nil && (current.HasMore || len(current.Results) != 0) {
				err = errors.New("parameter state drifted after review: a row now exists")
				failureReason = "parameter state drifted after review; prepare a fresh dry run"
			}
			if err == nil {
				record, err = client.CreatePartParameter(ctx, inventree.NewPartParameter(action.PartID, action.TemplateID, action.ProposedValue))
				if err != nil {
					failureReason = "upstream parameter create failed; inspect current state before retry"
				}
			}
		case "update":
			var current inventree.Parameter
			current, err = client.GetPartParameter(ctx, expectedID)
			if err != nil {
				failureReason = "update preflight failed; inspect current state before retry"
			}
			if err == nil && (action.BeforeValue == nil || current.PK != expectedID || current.ModelType != "part.part" || current.ModelID != action.PartID || current.Template != action.TemplateID || current.Data != *action.BeforeValue) {
				err = errors.New("parameter state drifted after review: current row does not match the reviewed before-state")
				failureReason = "parameter state drifted after review; prepare a fresh dry run"
			}
			if err == nil {
				record, err = client.UpdatePartParameter(ctx, expectedID, inventree.PatchFields{"data": inventree.Set(action.ProposedValue)})
				if err != nil {
					failureReason = "upstream parameter update failed; inspect current state before retry"
				}
			}
		default:
			err = fmt.Errorf("unsupported planned action %q", action.Action)
		}
		if err == nil && record.PK <= 0 {
			err = errors.New("InvenTree returned a parameter row without a stable ID")
			failureReason = "upstream write returned no stable parameter ID; inspect current state before retry"
		}
		if err == nil && action.Action == "update" && record.PK != expectedID {
			err = errors.New("InvenTree update response did not preserve the reviewed parameter ID")
			failureReason = "upstream update returned an unexpected parameter ID; inspect current state before retry"
		}
		if err == nil {
			if action.Action == "create" {
				action.ParameterID = record.PK
			}
			var refreshed inventree.Parameter
			refreshed, err = client.GetPartParameter(ctx, action.ParameterID)
			if err != nil {
				failureReason = "parameter read-back failed; inspect current state before retry"
			}
			if err == nil && (refreshed.PK != action.ParameterID || refreshed.ModelType != "part.part" || refreshed.ModelID != action.PartID || refreshed.Template != action.TemplateID || refreshed.Data != action.ProposedValue) {
				err = errors.New("read-back parameter state does not match the reviewed plan")
				failureReason = "parameter read-back did not match the reviewed plan; inspect current state before retry"
			}
		}
		if err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "parameter propagation action failed", slog.Int("part_id", action.PartID), slog.Int("template_id", action.TemplateID), slog.String("action", action.Action), slog.String("reason", failureReason), slog.String("error_type", fmt.Sprintf("%T", err)))
			action.Status = "failed"
			action.Reason = failureReason
			out.Failures = append(out.Failures, BulkParameterFailure{
				PartID: action.PartID, Action: action.Action, Message: "parameter write or read-back verification failed",
				RecoveryPlan: "Do not replay the old plan. Search this part's current parameter rows by stable part_id and template_id, resolve any ambiguous write outcome, then prepare a new dry run only for remaining parts.",
			})
			stopped = true
			continue
		}
		action.Status = "applied"
		action.Verified = true
	}
}

func partCategoryHasTemplate(ctx context.Context, client interface {
	SearchCategoryParameterTemplatesPage(context.Context, inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error)
}, categoryID, templateID int, budget *parameterScanBudget) (bool, error) {
	links, err := scanCategoryParameterLinks(ctx, client, inventree.CategoryParameterTemplateQuery{CategoryID: categoryID, FetchParent: dvgoutils.Ptr(true)}, budget)
	if err != nil {
		return false, err
	}
	for _, link := range links {
		if link.Template == templateID {
			return true, nil
		}
	}
	return false, nil
}

func auditTemplates(ctx context.Context, client ParameterAuditClient, templateID int, budget *parameterScanBudget) ([]inventree.ParameterTemplate, error) {
	search := ""
	if templateID > 0 {
		if err := budget.charge(0); err != nil {
			return nil, err
		}
		template, err := client.GetParameterTemplate(ctx, templateID)
		if err != nil {
			return nil, err
		}
		search = strings.TrimSpace(template.Name)
	}
	var templates []inventree.ParameterTemplate
	for offset := 0; ; {
		limit := budget.pageSize()
		if limit == 0 {
			return nil, errParameterAuditScanLimit
		}
		page, err := client.SearchParameterTemplatesPage(ctx, inventree.SearchQuery{Search: search, Limit: limit, Offset: offset})
		if err != nil {
			return nil, err
		}
		if err := budget.charge(len(page.Results)); err != nil {
			return nil, err
		}
		templates = append(templates, page.Results...)
		offset += len(page.Results)
		if len(page.Results) == 0 && page.Next != nil && *page.Next != "" {
			return nil, errParameterAuditScanLimit
		}
		if page.Next == nil || *page.Next == "" {
			if templateID > 0 {
				selected, ok := findTemplate(templates, templateID)
				if !ok {
					return nil, errors.New("template search did not return the selected stable template")
				}
				normalized := normalizedParameterName(selected.Name)
				templates = slices.DeleteFunc(templates, func(candidate inventree.ParameterTemplate) bool {
					return normalizedParameterName(candidate.Name) != normalized
				})
			}
			slices.SortFunc(templates, func(a, b inventree.ParameterTemplate) int { return a.PK - b.PK })
			return templates, nil
		}
	}
}

func auditParameterRows(ctx context.Context, client ParameterAuditClient, templates []inventree.ParameterTemplate, categoryID int, budget *parameterScanBudget) ([]inventree.Parameter, map[int]inventree.Part, error) {
	templateIDs := make(map[int]bool, len(templates))
	for _, template := range templates {
		templateIDs[template.PK] = true
	}
	parts := map[int]inventree.Part{}
	var rows []inventree.Parameter
	if categoryID > 0 {
		categoryParts, err := scanAuditCategoryParts(ctx, client, categoryID, budget)
		if err != nil {
			return nil, nil, err
		}
		for _, part := range categoryParts {
			parts[part.PK] = part
			ids := make([]int, 0, len(templateIDs))
			for id := range templateIDs {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			if len(ids) == 0 {
				ids = append(ids, 0)
			}
			for _, templateID := range ids {
				partRows, err := scanPartParameterRows(ctx, client, part.PK, templateID, budget)
				if err != nil {
					return nil, nil, err
				}
				rows = append(rows, partRows...)
			}
		}
	} else {
		ids := make([]int, 0, len(templateIDs))
		for id := range templateIDs {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		if len(ids) == 0 {
			ids = append(ids, 0)
		}
		for _, templateID := range ids {
			templateRows, err := scanTemplateParameterRows(ctx, client, templateID, budget)
			if err != nil {
				return nil, nil, err
			}
			rows = append(rows, templateRows...)
		}
	}
	slices.SortFunc(rows, func(a, b inventree.Parameter) int { return a.PK - b.PK })
	return rows, parts, nil
}

func scanAuditCategoryParts(ctx context.Context, client ParameterAuditClient, categoryID int, budget *parameterScanBudget) ([]inventree.Part, error) {
	var parts []inventree.Part
	for offset := 0; ; {
		limit := budget.pageSize()
		if limit == 0 {
			return nil, errParameterAuditScanLimit
		}
		page, err := client.SearchPartsPage(ctx, inventree.PartQuery{CategoryID: categoryID, Cascade: dvgoutils.Ptr(false), Limit: limit, Offset: offset})
		if err != nil {
			return nil, err
		}
		if err := budget.charge(len(page.Results)); err != nil {
			return nil, err
		}
		parts = append(parts, page.Results...)
		offset += len(page.Results)
		if len(page.Results) == 0 && page.HasMore {
			return nil, errParameterAuditScanLimit
		}
		if !page.HasMore {
			slices.SortFunc(parts, func(a, b inventree.Part) int { return a.PK - b.PK })
			return parts, nil
		}
	}
}

func scanPartParameterRows(ctx context.Context, client ParameterAuditClient, partID, templateID int, budget *parameterScanBudget) ([]inventree.Parameter, error) {
	var rows []inventree.Parameter
	for offset := 0; ; {
		limit := budget.pageSize()
		if limit == 0 {
			return nil, errParameterAuditScanLimit
		}
		page, err := client.SearchPartParametersPage(ctx, inventree.PartParameterQuery{PartID: partID, TemplateID: templateID, Limit: limit, Offset: offset})
		if err != nil {
			return nil, err
		}
		if err := budget.charge(len(page.Results)); err != nil {
			return nil, err
		}
		rows = append(rows, page.Results...)
		offset += len(page.Results)
		if len(page.Results) == 0 && page.HasMore {
			return nil, errParameterAuditScanLimit
		}
		if !page.HasMore {
			return rows, nil
		}
	}
}

func scanTemplateParameterRows(ctx context.Context, client ParameterAuditClient, templateID int, budget *parameterScanBudget) ([]inventree.Parameter, error) {
	var rows []inventree.Parameter
	for offset := 0; ; {
		limit := budget.pageSize()
		if limit == 0 {
			return nil, errParameterAuditScanLimit
		}
		page, err := client.SearchTemplateParametersPage(ctx, inventree.TemplateParameterQuery{TemplateID: templateID, Limit: limit, Offset: offset})
		if err != nil {
			return nil, err
		}
		if err := budget.charge(len(page.Results)); err != nil {
			return nil, err
		}
		rows = append(rows, page.Results...)
		offset += len(page.Results)
		if len(page.Results) == 0 && page.HasMore {
			return nil, errParameterAuditScanLimit
		}
		if !page.HasMore {
			return rows, nil
		}
	}
}

func findTemplate(templates []inventree.ParameterTemplate, id int) (inventree.ParameterTemplate, bool) {
	for _, template := range templates {
		if template.PK == id {
			return template, true
		}
	}
	return inventree.ParameterTemplate{}, false
}

func auditRelevantTemplates(ctx context.Context, client ParameterAuditClient, rows []inventree.Parameter, links []inventree.CategoryParameterTemplate, budget *parameterScanBudget) ([]inventree.ParameterTemplate, error) {
	ids := map[int]bool{}
	for _, row := range rows {
		ids[row.Template] = true
	}
	for _, link := range links {
		ids[link.Template] = true
	}
	ordered := make([]int, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	slices.Sort(ordered)
	templates := make([]inventree.ParameterTemplate, 0, len(ordered))
	for _, id := range ordered {
		if err := budget.charge(0); err != nil {
			return nil, err
		}
		template, err := client.GetParameterTemplate(ctx, id)
		if err != nil {
			return nil, err
		}
		if template.PK != id {
			return nil, fmt.Errorf("parameter template identity mismatch: requested %d, received %d", id, template.PK)
		}
		templates = append(templates, template)
	}
	return templates, nil
}

func auditCategoryLinks(ctx context.Context, client ParameterAuditClient, templates []inventree.ParameterTemplate, categoryID int, budget *parameterScanBudget) ([]inventree.CategoryParameterTemplate, error) {
	query := inventree.CategoryParameterTemplateQuery{CategoryID: categoryID, FetchParent: dvgoutils.Ptr(false)}
	links, err := scanCategoryParameterLinks(ctx, client, query, budget)
	if err != nil {
		return nil, err
	}
	templateIDs := make(map[int]bool, len(templates))
	for _, template := range templates {
		templateIDs[template.PK] = true
	}
	filtered := links[:0]
	for _, link := range links {
		if len(templateIDs) > 0 && !templateIDs[link.Template] {
			continue
		}
		filtered = append(filtered, link)
	}
	return filtered, nil
}

func scanCategoryParameterLinks(ctx context.Context, client interface {
	SearchCategoryParameterTemplatesPage(context.Context, inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error)
}, query inventree.CategoryParameterTemplateQuery, budget *parameterScanBudget) ([]inventree.CategoryParameterTemplate, error) {
	var links []inventree.CategoryParameterTemplate
	for offset := 0; ; {
		query.Limit = budget.pageSize()
		if query.Limit == 0 {
			return nil, errParameterAuditScanLimit
		}
		query.Offset = offset
		page, err := client.SearchCategoryParameterTemplatesPage(ctx, query)
		if err != nil {
			return nil, err
		}
		if err := budget.charge(len(page.Results)); err != nil {
			return nil, err
		}
		links = append(links, page.Results...)
		offset += len(page.Results)
		if len(page.Results) == 0 && page.Next != nil && *page.Next != "" {
			return nil, errParameterAuditScanLimit
		}
		if page.Next == nil || *page.Next == "" {
			slices.SortFunc(links, func(a, b inventree.CategoryParameterTemplate) int { return a.PK - b.PK })
			return links, nil
		}
	}
}

func parameterAuditFindings(ctx context.Context, client ParameterAuditClient, templates []inventree.ParameterTemplate, rows []inventree.Parameter, links []inventree.CategoryParameterTemplate, parts map[int]inventree.Part, budget *parameterScanBudget) ([]ParameterAuditFinding, int, error) {
	findings := templateAuditFindings(templates)
	templateByID := make(map[int]inventree.ParameterTemplate, len(templates))
	for _, template := range templates {
		templateByID[template.PK] = template
	}
	filteredRows := make([]inventree.Parameter, 0, len(rows))
	for _, row := range rows {
		if row.ModelType != "part.part" {
			continue
		}
		_, ok := parts[row.ModelID]
		if !ok {
			if err := budget.charge(0); err != nil {
				return nil, 0, err
			}
			part, err := client.GetPart(ctx, row.ModelID)
			if err != nil {
				return nil, 0, err
			}
			parts[row.ModelID] = part
		}
		filteredRows = append(filteredRows, row)
	}
	findings = append(findings, duplicateCategoryLinkFindings(links)...)
	findings = append(findings, duplicateParameterRowFindings(filteredRows)...)
	findings = append(findings, overloadedParameterNameFindings(filteredRows, templateByID)...)
	effectiveLinks := map[int][]inventree.CategoryParameterTemplate{}
	for _, row := range filteredRows {
		part := parts[row.ModelID]
		if part.Category == nil {
			findings = append(findings, ParameterAuditFinding{Kind: "unlinked_parameter", Severity: "warning", Message: "part has no category, so parameter linkage cannot be established", TemplateIDs: []int{row.Template}, ParameterIDs: []int{row.PK}, PartIDs: []int{row.ModelID}})
			continue
		}
		categoryLinks, ok := effectiveLinks[*part.Category]
		if !ok {
			var err error
			categoryLinks, err = scanCategoryParameterLinks(ctx, client, inventree.CategoryParameterTemplateQuery{CategoryID: *part.Category, FetchParent: dvgoutils.Ptr(true)}, budget)
			if err != nil {
				return nil, 0, err
			}
			effectiveLinks[*part.Category] = categoryLinks
		}
		defaults := make([]string, 0)
		for _, link := range categoryLinks {
			if link.Template == row.Template {
				defaults = append(defaults, link.DefaultValue)
			}
		}
		defaults = compactSortedStrings(defaults)
		if len(defaults) == 0 {
			findings = append(findings, ParameterAuditFinding{Kind: "unlinked_parameter", Severity: "warning", Message: "part parameter template is not linked to the part category or its ancestors", TemplateIDs: []int{row.Template}, ParameterIDs: []int{row.PK}, CategoryIDs: []int{*part.Category}, PartIDs: []int{row.ModelID}, CurrentValue: row.Data})
			continue
		}
		nonEmptyDefaults := defaults[:0]
		for _, value := range defaults {
			if value != "" {
				nonEmptyDefaults = append(nonEmptyDefaults, value)
			}
		}
		if len(nonEmptyDefaults) > 0 && !slices.Contains(nonEmptyDefaults, row.Data) {
			findings = append(findings, ParameterAuditFinding{Kind: "category_default_mismatch", Severity: "info", Message: "part parameter value differs from every non-empty effective category default", TemplateIDs: []int{row.Template}, ParameterIDs: []int{row.PK}, CategoryIDs: []int{*part.Category}, PartIDs: []int{row.ModelID}, CurrentValue: row.Data, ExpectedValues: nonEmptyDefaults})
		}
	}
	slices.SortFunc(findings, compareParameterAuditFindings)
	return findings, len(filteredRows), nil
}

func templateAuditFindings(templates []inventree.ParameterTemplate) []ParameterAuditFinding {
	groups := map[string][]inventree.ParameterTemplate{}
	for _, template := range templates {
		groups[normalizedParameterName(template.Name)] = append(groups[normalizedParameterName(template.Name)], template)
	}
	var findings []ParameterAuditFinding
	for name, group := range groups {
		if name == "" || len(group) < 2 {
			continue
		}
		ids := make([]int, 0, len(group))
		definitions := map[string]bool{}
		for _, template := range group {
			ids = append(ids, template.PK)
			definitions[parameterTemplateDefinition(template)] = true
		}
		slices.Sort(ids)
		findings = append(findings, ParameterAuditFinding{Kind: "duplicate_template_name", Severity: "warning", Message: fmt.Sprintf("multiple templates normalize to name %q", name), TemplateIDs: ids})
		if len(definitions) > 1 {
			findings = append(findings, ParameterAuditFinding{Kind: "incompatible_template_definition", Severity: "warning", Message: fmt.Sprintf("same-name templates %q have conflicting units, choices, checkbox, selection-list, or model-type definitions", name), TemplateIDs: ids})
		}
	}
	return findings
}

func duplicateCategoryLinkFindings(links []inventree.CategoryParameterTemplate) []ParameterAuditFinding {
	groups := map[string][]inventree.CategoryParameterTemplate{}
	for _, link := range links {
		groups[fmt.Sprintf("%d:%d", link.Category, link.Template)] = append(groups[fmt.Sprintf("%d:%d", link.Category, link.Template)], link)
	}
	var findings []ParameterAuditFinding
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		findings = append(findings, ParameterAuditFinding{Kind: "duplicate_category_default", Severity: "warning", Message: "category has multiple direct defaults for one template", TemplateIDs: []int{group[0].Template}, CategoryIDs: []int{group[0].Category}})
	}
	return findings
}

func duplicateParameterRowFindings(rows []inventree.Parameter) []ParameterAuditFinding {
	groups := map[string][]inventree.Parameter{}
	for _, row := range rows {
		key := fmt.Sprintf("%d:%d", row.ModelID, row.Template)
		groups[key] = append(groups[key], row)
	}
	var findings []ParameterAuditFinding
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		ids := make([]int, 0, len(group))
		for _, row := range group {
			ids = append(ids, row.PK)
		}
		slices.Sort(ids)
		findings = append(findings, ParameterAuditFinding{Kind: "duplicate_parameter_row", Severity: "warning", Message: "part has multiple rows for one template", TemplateIDs: []int{group[0].Template}, ParameterIDs: ids, PartIDs: []int{group[0].ModelID}})
	}
	return findings
}

func overloadedParameterNameFindings(rows []inventree.Parameter, templates map[int]inventree.ParameterTemplate) []ParameterAuditFinding {
	type group struct {
		partID     int
		templates  map[int]bool
		parameters []int
	}
	groups := map[string]*group{}
	for _, row := range rows {
		name := normalizedParameterName(templates[row.Template].Name)
		key := fmt.Sprintf("%d:%s", row.ModelID, name)
		if groups[key] == nil {
			groups[key] = &group{partID: row.ModelID, templates: map[int]bool{}}
		}
		groups[key].templates[row.Template] = true
		groups[key].parameters = append(groups[key].parameters, row.PK)
	}
	var findings []ParameterAuditFinding
	for _, group := range groups {
		if len(group.templates) < 2 {
			continue
		}
		templateIDs := make([]int, 0, len(group.templates))
		for id := range group.templates {
			templateIDs = append(templateIDs, id)
		}
		slices.Sort(templateIDs)
		slices.Sort(group.parameters)
		findings = append(findings, ParameterAuditFinding{Kind: "overloaded_parameter_name", Severity: "warning", Message: "part uses multiple same-name templates for one logical field", TemplateIDs: templateIDs, ParameterIDs: group.parameters, PartIDs: []int{group.partID}})
	}
	return findings
}

func parameterTemplateDefinition(template inventree.ParameterTemplate) string {
	units := ""
	if template.Units != nil {
		units = strings.TrimSpace(*template.Units)
	}
	modelType := ""
	if template.ModelType != nil {
		modelType = strings.TrimSpace(*template.ModelType)
	}
	selection := 0
	if template.SelectionList != nil {
		selection = *template.SelectionList
	}
	return fmt.Sprintf("%s|%s|%t|%d|%s", strings.ToLower(units), strings.TrimSpace(template.Choices), template.Checkbox, selection, modelType)
}

func normalizedParameterName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func compactSortedStrings(values []string) []string {
	slices.Sort(values)
	return slices.Compact(values)
}

func compareParameterAuditFindings(a, b ParameterAuditFinding) int {
	if cmp := strings.Compare(a.Kind, b.Kind); cmp != 0 {
		return cmp
	}
	left, right := 0, 0
	if len(a.PartIDs) > 0 {
		left = a.PartIDs[0]
	}
	if len(b.PartIDs) > 0 {
		right = b.PartIDs[0]
	}
	if left != right {
		return left - right
	}
	return strings.Compare(a.Message, b.Message)
}

func parameterAuditResultForError(input AuditParameterConsistencyInput, err error) (*mcp.CallToolResult, ParameterAuditOutput, error) {
	if errors.Is(err, errParameterAuditScanLimit) {
		reason := "the audit exceeds the 1,000-unit upstream request-and-record budget; supply template_id or exact category_id"
		switch {
		case input.TemplateID > 0 && input.CategoryID > 0:
			reason = "this exact template and category audit cannot complete within the 1,000-unit upstream request-and-record budget; the current tool exposes no narrower slice for this scope"
		case input.TemplateID > 0:
			reason = "the template audit exceeds the 1,000-unit upstream request-and-record budget; add one exact category_id"
		case input.CategoryID > 0:
			reason = "the category audit exceeds the 1,000-unit upstream request-and-record budget; add one existing template_id"
		}
		return parameterAuditClarification("Which supported narrower audit scope should be run?", "filters", reason, input)
	}
	return nil, ParameterAuditOutput{}, err
}

func parameterAuditClarification(question, subject, reason string, input AuditParameterConsistencyInput) (*mcp.CallToolResult, ParameterAuditOutput, error) {
	clarification := NewClarification(question, subject, reason, "filters", true, nil, map[string]any{"template_id": input.TemplateID, "category_id": input.CategoryID})
	return TextResult(StatusClarificationRequired), ParameterAuditOutput{Status: StatusClarificationRequired, Findings: []ParameterAuditFinding{}, Clarification: &clarification}, nil
}

func bulkParameterClarification(out BulkParameterOutput, question, subject, reason, retry string, values map[string]any) (*mcp.CallToolResult, BulkParameterOutput, error) {
	clarification := NewClarification(question, subject, reason, retry, true, nil, values)
	out.Status = StatusClarificationRequired
	out.Clarification = &clarification
	return TextResult(StatusClarificationRequired), out, nil
}

func bulkParameterRetryValues(input BulkPropagatePartParametersInput) map[string]any {
	return map[string]any{
		"template_id": input.TemplateID, "value": input.Value, "part_ids": input.PartIDs,
		"category_id": input.CategoryID, "include_subcategories": input.IncludeSubcategories,
		"overwrite_existing": input.OverwriteExisting, "dry_run": true,
	}
}

func countBulkParameterActions(out *BulkParameterOutput) {
	out.Applied, out.Skipped, out.Failed, out.ManualRequired = 0, 0, 0, 0
	if out.Plan == nil {
		return
	}
	for _, action := range out.Plan.Actions {
		switch action.Status {
		case "applied":
			out.Applied++
		case "skipped":
			out.Skipped++
		case "failed":
			out.Failed++
		case "manual_required":
			out.ManualRequired++
		}
	}
}

func plannedBulkParameterActions(actions []BulkParameterAction) int {
	count := 0
	for _, action := range actions {
		if action.Status == "planned" {
			count++
		}
	}
	return count
}

func firstParameterID(rows []inventree.Parameter) int {
	if len(rows) == 0 {
		return 0
	}
	return rows[0].PK
}

func bulkParameterPlanDigest(plan BulkParameterPlan) (string, error) {
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func bulkParameterSelector(plan BulkParameterPlan) string {
	return fmt.Sprintf("%d:%d:%t:%v", plan.TemplateID, plan.CategoryID, plan.IncludeSubcategories, plan.PartIDs)
}
