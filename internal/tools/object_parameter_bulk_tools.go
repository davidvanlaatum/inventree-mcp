package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// object_parameter_bulk_tools.go adds F-S80's second bulk tool built on
// internal/batch (F-S76): bounded, independent-item parameter-value
// maintenance across F-S64's non-part object-parameter model types. Each item
// binds template identity (template_id only — no ambiguous name-based
// resolution), target object/row identity (model_type/model_id), an explicit
// value, and F-S14's overwrite_existing convention (false leaves an existing
// differing value unwritten and reported instead of updated).
//
// This is complementary to, not a replacement for, F-S14's
// bulk_propagate_part_parameters (one template/value fanned out across many
// parts by selector) and create_object_parameter/delete_object_parameter
// (single-row create/update/delete). Template merges, implicit template
// creation, and part.part rows remain out of scope; part parameters continue
// to use set_part_parameters, bulk_propagate_part_parameters, and
// delete_part_parameter.

const (
	objectParameterBulkOutcomePlanned = "planned"
	objectParameterBulkOutcomeFailed  = "failed"

	objectParameterBulkReasonDuplicateKey              = "this model_type/model_id/template_id combination appears more than once in the batch; each combination may appear only once per call"
	objectParameterBulkReasonUniquenessConflictInBatch = "this value conflicts with another item in the same batch under this template's uniqueness policy"
	objectParameterBulkReasonReadFailed                = "current state could not be re-read before this item's write"
	objectParameterBulkReasonDrifted                   = "current state drifted since the plan was reviewed"
	objectParameterBulkReasonAmbiguous                 = "multiple existing parameter rows now use this template on this object"
)

type BulkUpdateObjectParameterItem struct {
	ModelType         string  `json:"model_type" jsonschema:"Qualified InvenTree app.model value of the target object. One of company.company, company.manufacturerpart, company.supplierpart, order.purchaseorder, part.partcategory, or stock.stocklocation."`
	ModelID           int     `json:"model_id" jsonschema:"Stable primary key of the target object."`
	TemplateID        int     `json:"template_id" jsonschema:"Existing parameter-template primary key. Ambiguous name-based template resolution is not supported here; use template_id."`
	Value             *string `json:"value" jsonschema:"Explicit new parameter value, including an explicit empty string."`
	OverwriteExisting bool    `json:"overwrite_existing,omitempty" jsonschema:"Explicitly permit updating one existing differing row; false reports it instead of writing."`
}

type BulkUpdateObjectParametersInput struct {
	Items    []BulkUpdateObjectParameterItem `json:"items" jsonschema:"Ordered batch of independent object-parameter updates, 1 to 25 items."`
	DryRun   bool                            `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                            `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                          `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

// BulkObjectParameterItemResult reports one item's outcome, identified by its
// (model_type, model_id, template_id) key rather than a single int id, since
// a target row may not exist yet when the plan is built.
type BulkObjectParameterItemResult struct {
	ModelType    string                 `json:"model_type"`
	ModelID      int                    `json:"model_id"`
	TemplateID   int                    `json:"template_id"`
	Outcome      string                 `json:"outcome"`
	Message      string                 `json:"message,omitempty"`
	RecoveryPlan string                 `json:"recovery_plan,omitempty"`
	Record       *ObjectParameterResult `json:"record,omitempty"`
}

type BulkUpdateObjectParametersOutput struct {
	Status        string                          `json:"status"`
	DryRun        bool                            `json:"dry_run"`
	PlanHash      string                          `json:"plan_hash,omitempty"`
	Items         []BulkObjectParameterItemResult `json:"items,omitempty"`
	Clarification *ClarificationResponse          `json:"clarification,omitempty"`
}

// objectParameterBulkPlanItem's Before is the exact existing row captured at
// plan-build time (nil when none existed, meaning this item plans a create).
// Preflight re-reads current state fresh and compares it against Before to
// detect drift, mirroring catalog_bulk_tools.go's adapters.
type objectParameterBulkPlanItem struct {
	ModelType         string
	ModelID           int
	TemplateID        int
	Value             string
	OverwriteExisting bool
	FailReason        string
	Before            *inventree.Parameter
	// Unique is the item's template's uniqueness policy, captured at
	// plan-build time so buildObjectParameterBulkPlan can reject same-batch
	// items that would race each other into a uniqueness-policy violation
	// (see objectParameterBulkUniquenessGroupKey) without an extra read.
	Unique inventree.ParameterUniqueness
}

type objectParameterBulkPlan struct {
	Items []objectParameterBulkPlanItem
}

func (p objectParameterBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }

func objectParameterBulkKey(modelType string, modelID, templateID int) string {
	return fmt.Sprintf("%s|%d|%d", modelType, modelID, templateID)
}

func (p objectParameterBulkPlan) keys() []string {
	keys := make([]string, len(p.Items))
	for i, item := range p.Items {
		keys[i] = objectParameterBulkKey(item.ModelType, item.ModelID, item.TemplateID)
	}
	return keys
}

// supersedeKey returns a deterministic, order-independent key over every
// item's identity so a fresh dry_run over the same set of targets (in any
// order) supersedes an earlier outstanding plan for that same set.
func (p objectParameterBulkPlan) supersedeKey() string {
	keys := p.keys()
	slices.Sort(keys)
	return strings.Join(keys, ",")
}

func duplicateObjectParameterKeys(keys []string) map[string]bool {
	seen := map[string]int{}
	for _, key := range keys {
		seen[key]++
	}
	dup := map[string]bool{}
	for key, count := range seen {
		if count > 1 {
			dup[key] = true
		}
	}
	return dup
}

// objectParameterBulkUniquenessGroupKey returns the key under which item's
// value would conflict with another item's value under its template's
// uniqueness policy, and whether that policy applies at all. Two items with
// different (model_type, model_id, template_id) identities — so not caught
// by the plain identity-duplicate check — can still race each other past
// per-item plan-build validation and into batch.Execute's concurrent Mutate
// calls, each independently unaware the other is about to create or update a
// row with the same value under the same global- or model-type-scoped
// uniqueness template. Grouping same-batch items by this key at plan-build
// time closes that gap without an extra upstream read.
func objectParameterBulkUniquenessGroupKey(item objectParameterBulkPlanItem) (string, bool) {
	switch item.Unique {
	case inventree.ParameterUniquenessGlobal:
		return fmt.Sprintf("global|%d|%s", item.TemplateID, item.Value), true
	case inventree.ParameterUniquenessModelType:
		return fmt.Sprintf("model_type|%d|%s|%s", item.TemplateID, item.ModelType, item.Value), true
	default:
		return "", false
	}
}

func registerObjectParameterBulkWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.objectParameterBulkPlanStore == nil {
		deps.objectParameterBulkPlanStore = mustBulkStore(batch.Options[objectParameterBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p objectParameterBulkPlan) string { return p.supersedeKey() },
		})
	}
	addWriteTool(server, deps, BulkUpdateObjectParametersToolName, "Bulk update object parameters", "Plans or confirms a bounded independent-item parameter-value batch across company, supplier-part, manufacturer-part, purchase-order, part-category, or stock-location objects, using create_object_parameter's own template-compatibility and uniqueness rules. part.part rows, ambiguous template-name resolution, and template merges are not supported here.", bulkUpdateObjectParameters(deps))
}

func buildObjectParameterBulkPlan(ctx context.Context, client ObjectParameterWriteClient, items []BulkUpdateObjectParameterItem) objectParameterBulkPlan {
	plan := objectParameterBulkPlan{Items: make([]objectParameterBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildObjectParameterBulkPlanItem(ctx, client, item)
	}
	dup := duplicateObjectParameterKeys(plan.keys())
	for i := range plan.Items {
		key := objectParameterBulkKey(plan.Items[i].ModelType, plan.Items[i].ModelID, plan.Items[i].TemplateID)
		if dup[key] && plan.Items[i].FailReason == "" {
			plan.Items[i].FailReason = objectParameterBulkReasonDuplicateKey
		}
	}
	uniquenessGroups := map[string]int{}
	for _, item := range plan.Items {
		if item.FailReason != "" {
			continue
		}
		if key, applies := objectParameterBulkUniquenessGroupKey(item); applies {
			uniquenessGroups[key]++
		}
	}
	for i := range plan.Items {
		if plan.Items[i].FailReason != "" {
			continue
		}
		if key, applies := objectParameterBulkUniquenessGroupKey(plan.Items[i]); applies && uniquenessGroups[key] > 1 {
			plan.Items[i].FailReason = objectParameterBulkReasonUniquenessConflictInBatch
		}
	}
	return plan
}

func buildObjectParameterBulkPlanItem(ctx context.Context, client ObjectParameterWriteClient, item BulkUpdateObjectParameterItem) objectParameterBulkPlanItem {
	base := objectParameterBulkPlanItem{ModelType: strings.TrimSpace(item.ModelType), ModelID: item.ModelID, TemplateID: item.TemplateID, OverwriteExisting: item.OverwriteExisting}
	if item.Value != nil {
		base.Value = *item.Value
	}
	if !validObjectParameterModelType(base.ModelType) {
		base.FailReason = "model_type must be one of " + objectParameterModelTypes
		return base
	}
	if item.ModelID <= 0 {
		base.FailReason = "model_id must be positive"
		return base
	}
	if item.TemplateID <= 0 {
		base.FailReason = "template_id must be positive"
		return base
	}
	if item.Value == nil {
		base.FailReason = "value must be explicitly supplied, including for an empty value"
		return base
	}
	if len(*item.Value) > objectParameterMaxValueLength {
		base.FailReason = objectParameterValueTooLongMessage
		return base
	}
	template, err := client.GetParameterTemplate(ctx, item.TemplateID)
	if err != nil || template.PK != item.TemplateID {
		base.FailReason = "template_id does not identify a readable parameter template"
		return base
	}
	base.Unique = template.Unique
	if !objectParameterTemplateCompatible(template, base.ModelType) {
		base.FailReason = "template must be enabled and unrestricted or restricted to " + base.ModelType
		return base
	}
	existing, err := client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: base.ModelType, ModelID: item.ModelID, TemplateID: item.TemplateID, Limit: 3})
	if err != nil {
		base.FailReason = "current object-parameter state could not be read"
		return base
	}
	if len(existing.Results) > 1 {
		base.FailReason = "multiple existing parameter rows already use this template on this object"
		return base
	}
	excludeID := 0
	if len(existing.Results) == 1 {
		row := existing.Results[0]
		base.Before = &row
		excludeID = row.PK
		if row.Data == base.Value {
			return base
		}
		if !item.OverwriteExisting {
			base.FailReason = "existing differing value requires overwrite_existing=true"
			return base
		}
	}
	if template.Unique != inventree.ParameterUniquenessNone {
		conflict, err := objectParameterUniquenessConflict(ctx, client, base.ModelType, template.PK, template.Unique, base.Value, excludeID)
		if err != nil {
			base.FailReason = "parameter uniqueness could not be verified"
			return base
		}
		if conflict != nil {
			base.FailReason = fmt.Sprintf("the requested value already exists on parameter row %d under this template's uniqueness policy", conflict.PK)
			return base
		}
	}
	return base
}

type objectParameterBulkAdapter struct {
	client ObjectParameterWriteClient
	mu     sync.Mutex
	views  map[string]ObjectParameterResult
}

func (a *objectParameterBulkAdapter) store(key string, view ObjectParameterResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.views == nil {
		a.views = map[string]ObjectParameterResult{}
	}
	a.views[key] = view
}

func (a *objectParameterBulkAdapter) get(key string) (ObjectParameterResult, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	view, ok := a.views[key]
	return view, ok
}

func (a *objectParameterBulkAdapter) Preflight(ctx context.Context, item objectParameterBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: item.ModelType, ModelID: item.ModelID, TemplateID: item.TemplateID, Limit: 3})
	if err != nil {
		return false, objectParameterBulkReasonReadFailed, err
	}
	if len(current.Results) > 1 {
		return false, objectParameterBulkReasonAmbiguous, errors.New(objectParameterBulkReasonAmbiguous)
	}
	if len(current.Results) == 1 {
		row := current.Results[0]
		if row.Data == item.Value {
			return true, "already at target state", nil
		}
		if item.Before == nil || item.Before.PK != row.PK || item.Before.Data != row.Data {
			return false, objectParameterBulkReasonDrifted, errors.New(objectParameterBulkReasonDrifted)
		}
		return false, "", nil
	}
	if item.Before != nil {
		return false, objectParameterBulkReasonDrifted, errors.New(objectParameterBulkReasonDrifted)
	}
	return false, "", nil
}

func (a *objectParameterBulkAdapter) Mutate(ctx context.Context, item objectParameterBulkPlanItem) error {
	var err error
	if item.Before != nil {
		_, err = a.client.UpdatePartParameter(ctx, item.Before.PK, inventree.PatchFields{"data": inventree.Set(item.Value)})
	} else {
		_, err = a.client.CreatePartParameter(ctx, inventree.ParameterCreate{Template: item.TemplateID, ModelType: item.ModelType, ModelID: item.ModelID, Data: item.Value})
	}
	if err != nil {
		if !ambiguousAdminMutation(err) {
			return err
		}
		current, getErr := a.client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: item.ModelType, ModelID: item.ModelID, TemplateID: item.TemplateID, Limit: 3})
		if getErr != nil || len(current.Results) != 1 || current.Results[0].Data != item.Value {
			return err
		}
	}
	return nil
}

func (a *objectParameterBulkAdapter) Verify(ctx context.Context, item objectParameterBulkPlanItem) error {
	current, err := a.client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: item.ModelType, ModelID: item.ModelID, TemplateID: item.TemplateID, Limit: 3})
	if err != nil || len(current.Results) != 1 || current.Results[0].Data != item.Value {
		return errors.New("read-back does not match the reviewed plan")
	}
	template, err := a.client.GetParameterTemplate(ctx, item.TemplateID)
	if err != nil || template.PK != item.TemplateID {
		return errors.New("read-back template could not be re-read")
	}
	a.store(objectParameterBulkKey(item.ModelType, item.ModelID, item.TemplateID), objectParameterResult(current.Results[0], template))
	return nil
}

func objectParameterBulkPreview(items []objectParameterBulkPlanItem) []BulkObjectParameterItemResult {
	out := make([]BulkObjectParameterItemResult, len(items))
	for i, item := range items {
		entry := BulkObjectParameterItemResult{ModelType: item.ModelType, ModelID: item.ModelID, TemplateID: item.TemplateID}
		if item.FailReason != "" {
			entry.Outcome = objectParameterBulkOutcomeFailed
			entry.Message = item.FailReason
		} else {
			entry.Outcome = objectParameterBulkOutcomePlanned
		}
		out[i] = entry
	}
	return out
}

func objectParameterBulkResults(results []batch.Result[objectParameterBulkPlanItem], adapter *objectParameterBulkAdapter) []BulkObjectParameterItemResult {
	out := make([]BulkObjectParameterItemResult, len(results))
	for i, result := range results {
		item := result.Item
		entry := BulkObjectParameterItemResult{
			ModelType: item.ModelType, ModelID: item.ModelID, TemplateID: item.TemplateID,
			Outcome: string(result.Outcome), Message: result.Message, RecoveryPlan: result.RecoveryPlan,
		}
		if result.Outcome == batch.OutcomeApplied {
			if view, ok := adapter.get(objectParameterBulkKey(item.ModelType, item.ModelID, item.TemplateID)); ok {
				entry.Record = &view
			}
		}
		out[i] = entry
	}
	return out
}

func objectParameterBulkOutputStatus(items []BulkObjectParameterItemResult) string {
	for _, item := range items {
		switch item.Outcome {
		case string(batch.OutcomeFailed), string(batch.OutcomeAmbiguous), string(batch.OutcomeUnverified):
			return StatusPartialFailure
		}
	}
	return StatusOK
}

func objectParameterBulkClarification(question, field, reason, retry string) (*mcp.CallToolResult, BulkUpdateObjectParametersOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, nil)
	return TextResult(StatusClarificationRequired), BulkUpdateObjectParametersOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func objectParameterBulkItemCountClarification(count int) (*mcp.CallToolResult, BulkUpdateObjectParametersOutput, error) {
	if count == 0 {
		return objectParameterBulkClarification("Which object parameters should be updated?", "items", BulkUpdateObjectParametersToolName+" requires at least one item", "items")
	}
	return objectParameterBulkClarification("How should this batch be narrowed?", "items", fmt.Sprintf("%s accepts at most %d items per call", BulkUpdateObjectParametersToolName, bulkUpdateMaxItems), "items")
}

func bulkUpdateObjectParameters(deps Dependencies) mcp.ToolHandlerFor[BulkUpdateObjectParametersInput, BulkUpdateObjectParametersOutput] {
	return LookupHandler[ObjectParameterWriteClient, BulkUpdateObjectParametersInput, BulkUpdateObjectParametersOutput](deps, BulkUpdateObjectParametersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ObjectParameterWriteClient, input BulkUpdateObjectParametersInput) (*mcp.CallToolResult, BulkUpdateObjectParametersOutput, error) {
			if len(input.Items) == 0 || len(input.Items) > bulkUpdateMaxItems {
				return objectParameterBulkItemCountClarification(len(input.Items))
			}
			plan := buildObjectParameterBulkPlan(ctx, client, input.Items)
			out := BulkUpdateObjectParametersOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = objectParameterBulkPreview(plan.Items)
				token, err := deps.objectParameterBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return objectParameterBulkClarification("When should a new bulk plan be prepared?", "confirmation", "too many bulk plans are outstanding; execute a reviewed plan or wait for a five-minute token to expire", "dry_run")
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return objectParameterBulkClarification("Should this reviewed batch now be executed?", "confirmation", "confirm must be true after reviewing dry_run:true output", "confirm")
			}
			if input.PlanHash == "" || !deps.objectParameterBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return objectParameterBulkClarification("Which current dry-run plan should authorize this batch?", "confirmation", "plan_hash must be the unexpired single-use token from a matching dry run by the same principal; changed inventory state requires a new dry run", "plan_hash")
			}
			adapter := &objectParameterBulkAdapter{client: client}
			results := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: bulkUpdateConcurrency})
			out.Items = objectParameterBulkResults(results, adapter)
			out.Status = objectParameterBulkOutputStatus(out.Items)
			return TextResult(out.Status), out, nil
		})
}
