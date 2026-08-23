package tools

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// catalog_bulk_tools.go adds the first public MCP tools built on
// internal/batch (F-S76): bounded, independent, scalar-field-only bulk
// updates for parts, companies, part categories, supplier parts, and
// manufacturer parts (F-S77). Each tool reuses the matching single-item
// tool's PATCH-field builder, clear-conflict, duplicate/reference, and
// postflight functions unchanged, so the allowlist and semantics stay
// byte-for-byte identical to update_part/update_company/update_part_category/
// update_supplier_part/update_manufacturer_part for every field it accepts.
//
// Scope is deliberately narrower than each single-item tool: fields that
// require that tool's own extra confirm-gated review (company role removal
// and is_customer, category reparenting/structural changes, supplier/
// manufacturer-part relationship relinking) are not exposed here. Use the
// single-item tools for those. See docs/tool-reference.md.

const (
	bulkUpdateMaxItems    = 25
	bulkUpdateConcurrency = 4

	bulkOutcomePlanned = "planned"
	bulkOutcomeFailed  = "failed"
)

// bulkPlanItemBase is embedded by every resource's batch-plan item type. ID
// is always populated; FailReason is set (with a caller-safe message) when
// plan-build already knows this item cannot proceed, so it is still
// represented in the plan/digest but Preflight rejects it without a write.
type bulkPlanItemBase struct {
	ID         int
	FailReason string
}

func (b bulkPlanItemBase) base() bulkPlanItemBase { return b }

type bulkPlanItem interface {
	base() bulkPlanItemBase
}

// BulkUpdateItemResult reports one item's outcome. Outcome is "planned" or
// "failed" for a dry_run preview, or one of internal/batch's Outcome values
// ("applied", "skipped", "failed", "ambiguous", "unverified") after confirm.
type BulkUpdateItemResult[View any] struct {
	ID           int    `json:"id"`
	Outcome      string `json:"outcome"`
	Message      string `json:"message,omitempty"`
	RecoveryPlan string `json:"recovery_plan,omitempty"`
	Record       *View  `json:"record,omitempty"`
}

type BulkUpdateOutput[View any] struct {
	Status        string                       `json:"status"`
	DryRun        bool                         `json:"dry_run"`
	PlanHash      string                       `json:"plan_hash,omitempty"`
	Items         []BulkUpdateItemResult[View] `json:"items,omitempty"`
	Clarification *ClarificationResponse       `json:"clarification,omitempty"`
}

func bulkClarification[View any](question, field, reason, retry string) (*mcp.CallToolResult, BulkUpdateOutput[View], error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, nil)
	return TextResult(StatusClarificationRequired), BulkUpdateOutput[View]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func bulkItemCountClarification[View any](toolName string, count int) (*mcp.CallToolResult, BulkUpdateOutput[View], error) {
	if count == 0 {
		return bulkClarification[View]("Which records should be updated?", "items", toolName+" requires at least one item", "items")
	}
	return bulkClarification[View]("How should this batch be narrowed?", "items", fmt.Sprintf("%s accepts at most %d items per call", toolName, bulkUpdateMaxItems), "items")
}

func bulkConfirmClarification[View any]() (*mcp.CallToolResult, BulkUpdateOutput[View], error) {
	return bulkClarification[View]("Should this reviewed batch now be executed?", "confirmation", "confirm must be true after reviewing dry_run:true output", "confirm")
}

func bulkPlanClarification[View any]() (*mcp.CallToolResult, BulkUpdateOutput[View], error) {
	return bulkClarification[View]("Which current dry-run plan should authorize this batch?", "confirmation", "plan_hash must be the unexpired single-use token from a matching dry run by the same principal; changed inventory state requires a new dry run", "plan_hash")
}

func bulkCapacityClarification[View any]() (*mcp.CallToolResult, BulkUpdateOutput[View], error) {
	return bulkClarification[View]("When should a new bulk plan be prepared?", "confirmation", "too many bulk plans are outstanding; execute a reviewed plan or wait for a five-minute token to expire", "dry_run")
}

func bulkOutputStatus[View any](items []BulkUpdateItemResult[View]) string {
	for _, item := range items {
		switch item.Outcome {
		case string(batch.OutcomeFailed), string(batch.OutcomeAmbiguous), string(batch.OutcomeUnverified):
			return StatusPartialFailure
		}
	}
	return StatusOK
}

func bulkPreview[Item bulkPlanItem, View any](items []Item) []BulkUpdateItemResult[View] {
	out := make([]BulkUpdateItemResult[View], len(items))
	for i, item := range items {
		b := item.base()
		if b.FailReason != "" {
			out[i] = BulkUpdateItemResult[View]{ID: b.ID, Outcome: bulkOutcomeFailed, Message: b.FailReason}
			continue
		}
		out[i] = BulkUpdateItemResult[View]{ID: b.ID, Outcome: bulkOutcomePlanned}
	}
	return out
}

func bulkResults[Item bulkPlanItem, View any](results []batch.Result[Item], record func(id int) (View, bool)) []BulkUpdateItemResult[View] {
	out := make([]BulkUpdateItemResult[View], len(results))
	for i, result := range results {
		b := result.Item.base()
		entry := BulkUpdateItemResult[View]{ID: b.ID, Outcome: string(result.Outcome), Message: result.Message, RecoveryPlan: result.RecoveryPlan}
		if result.Outcome == batch.OutcomeApplied {
			if view, ok := record(b.ID); ok {
				entry.Record = &view
			}
		}
		out[i] = entry
	}
	return out
}

func bulkSupersedeKey(ids []int) string {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	return fmt.Sprint(sorted)
}

// bulkRecordStore is the mutex-protected map each adapter uses to hand its
// exact-read-back view for an applied item back to the tool handler once
// batch.Execute returns; batch.Result carries no such channel of its own.
type bulkRecordStore[View any] struct {
	mu      sync.Mutex
	records map[int]View
}

func (s *bulkRecordStore[View]) store(id int, view View) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = map[int]View{}
	}
	s.records[id] = view
}

func (s *bulkRecordStore[View]) get(id int) (View, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	view, ok := s.records[id]
	return view, ok
}

// valuesMatchForKeys reports whether a and b agree on every key present in
// fields. It is used to detect drift between a batch plan item's captured
// "before" state and freshly re-fetched current state during Preflight.
func valuesMatchForKeys(fields inventree.PatchFields, a, b map[string]any) bool {
	for key := range fields {
		if !reflect.DeepEqual(comparablePatchValue(a[key]), comparablePatchValue(b[key])) {
			return false
		}
	}
	return true
}

func registerCatalogBulkWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.partBulkPlanStore == nil {
		deps.partBulkPlanStore = mustBulkStore(batch.Options[partBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p partBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	if deps.companyBulkPlanStore == nil {
		deps.companyBulkPlanStore = mustBulkStore(batch.Options[companyBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p companyBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	if deps.categoryBulkPlanStore == nil {
		deps.categoryBulkPlanStore = mustBulkStore(batch.Options[categoryBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p categoryBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	if deps.supplierPartBulkPlanStore == nil {
		deps.supplierPartBulkPlanStore = mustBulkStore(batch.Options[supplierPartBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p supplierPartBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	if deps.manufacturerPartBulkPlanStore == nil {
		deps.manufacturerPartBulkPlanStore = mustBulkStore(batch.Options[manufacturerPartBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p manufacturerPartBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	addWriteTool(server, deps, BulkUpdatePartsToolName, "Bulk update parts", "Plans or confirms a bounded independent-item scalar update batch across existing parts, using update_part's own field allowlist.", bulkUpdateParts(deps))
	addWriteTool(server, deps, BulkUpdateCompaniesToolName, "Bulk update companies", "Plans or confirms a bounded independent-item scalar update batch across existing companies. Role removal and is_customer are not supported here; use update_company.", bulkUpdateCompanies(deps))
	addWriteTool(server, deps, BulkUpdatePartCategoriesToolName, "Bulk update part categories", "Plans or confirms a bounded independent-item scalar update batch across existing part categories. Reparenting and structural changes are not supported here; use update_part_category.", bulkUpdatePartCategories(deps))
	addWriteTool(server, deps, BulkUpdateSupplierPartsToolName, "Bulk update supplier parts", "Plans or confirms a bounded independent-item scalar update batch across existing supplier-part links. Relinking part_id, supplier_id, or manufacturer_part_id is not supported here; use update_supplier_part.", bulkUpdateSupplierParts(deps))
	addWriteTool(server, deps, BulkUpdateManufacturerPartsToolName, "Bulk update manufacturer parts", "Plans or confirms a bounded independent-item scalar update batch across existing manufacturer-part links. Relinking part_id or manufacturer_id is not supported here; use update_manufacturer_part.", bulkUpdateManufacturerParts(deps))
}

func mustBulkStore[P batch.Digestible](opts batch.Options[P]) *batch.Store[P] {
	store, err := batch.NewStore(opts)
	if err != nil {
		panic(fmt.Sprintf("catalog bulk tools: %v", err))
	}
	return store
}

// ---------------------------------------------------------------------------
// Parts
// ---------------------------------------------------------------------------

type BulkUpdatePartItem struct {
	ID              int      `json:"id" jsonschema:"Stable InvenTree part primary key."`
	Name            *string  `json:"name,omitempty" jsonschema:"Optional replacement part name."`
	Description     *string  `json:"description,omitempty" jsonschema:"Optional replacement description."`
	CategoryID      *int     `json:"category_id,omitempty" jsonschema:"Optional existing category primary key."`
	IPN             *string  `json:"ipn,omitempty" jsonschema:"Optional replacement internal part number."`
	Units           *string  `json:"units,omitempty" jsonschema:"Optional replacement units."`
	Active          *bool    `json:"active,omitempty" jsonschema:"Optional explicit active flag."`
	Assembly        *bool    `json:"assembly,omitempty" jsonschema:"Optional explicit assembly flag."`
	Component       *bool    `json:"component,omitempty" jsonschema:"Optional explicit component flag."`
	Purchaseable    *bool    `json:"purchaseable,omitempty" jsonschema:"Optional explicit purchasable flag."`
	Trackable       *bool    `json:"trackable,omitempty" jsonschema:"Optional explicit trackable flag."`
	Virtual         *bool    `json:"virtual,omitempty" jsonschema:"Optional explicit virtual flag."`
	DefaultLocation *int     `json:"default_location_id,omitempty" jsonschema:"Optional existing stock location primary key."`
	Consumable      *bool    `json:"consumable,omitempty" jsonschema:"Optional explicit consumable flag."`
	DefaultExpiry   *int     `json:"default_expiry,omitempty" jsonschema:"Optional non-negative default stock expiry in days; 0 resets the default."`
	IsTemplate      *bool    `json:"is_template,omitempty" jsonschema:"Optional explicit template flag."`
	Keywords        *string  `json:"keywords,omitempty" jsonschema:"Replacement search keywords."`
	ClearKeywords   bool     `json:"clear_keywords,omitempty" jsonschema:"Explicitly PATCH keywords to null; mutually exclusive with keywords."`
	Link            *string  `json:"link,omitempty" jsonschema:"Replacement complete HTTP(S) external link without userinfo."`
	ClearLink       bool     `json:"clear_link,omitempty" jsonschema:"Explicitly PATCH link to null; mutually exclusive with link."`
	Locked          *bool    `json:"locked,omitempty" jsonschema:"Optional explicit locked flag."`
	MinimumStock    *float64 `json:"minimum_stock,omitempty" jsonschema:"Optional non-negative minimum stock quantity."`
	MaximumStock    *float64 `json:"maximum_stock,omitempty" jsonschema:"Optional non-negative maximum stock quantity; 0 disables the maximum."`
	Revision        *string  `json:"revision,omitempty" jsonschema:"Replacement revision text."`
	ClearRevision   bool     `json:"clear_revision,omitempty" jsonschema:"Explicitly PATCH revision to null; mutually exclusive with revision."`
	Salable         *bool    `json:"salable,omitempty" jsonschema:"Optional explicit salable flag."`
	Testable        *bool    `json:"testable,omitempty" jsonschema:"Optional explicit testable flag."`
	Notes           *string  `json:"notes,omitempty" jsonschema:"Replacement markdown notes."`
	ClearNotes      bool     `json:"clear_notes,omitempty" jsonschema:"Explicitly PATCH notes to null; mutually exclusive with notes."`
}

type BulkUpdatePartsInput struct {
	Items    []BulkUpdatePartItem `json:"items" jsonschema:"Ordered batch of independent part updates, 1 to 25 items."`
	DryRun   bool                 `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                 `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string               `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type partBulkPlanItem struct {
	bulkPlanItemBase
	Before inventree.PartDetail
	Fields inventree.PatchFields
}

type partBulkPlan struct {
	Items []partBulkPlanItem
}

func (p partBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p partBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

func partValues(record inventree.PartDetail) map[string]any {
	return map[string]any{
		"name": record.Name, "description": record.Description, "category": record.Category, "IPN": record.IPN,
		"units": record.Units, "active": record.Active, "assembly": record.Assembly, "component": record.Component,
		"purchaseable": record.Purchaseable, "trackable": record.Trackable, "virtual": record.Virtual,
		"default_location": record.DefaultLocation, "consumable": record.Consumable, "default_expiry": record.DefaultExpiry,
		"is_template": record.IsTemplate, "keywords": record.Keywords, "link": record.Link, "locked": record.Locked,
		"minimum_stock": record.MinimumStock, "maximum_stock": record.MaximumStock, "revision": record.Revision,
		"salable": record.Salable, "testable": record.Testable, "notes": record.Notes,
	}
}

func buildPartBulkPlan(ctx context.Context, client PartWriteClient, items []BulkUpdatePartItem) partBulkPlan {
	plan := partBulkPlan{Items: make([]partBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildPartBulkPlanItem(ctx, client, item)
	}
	return plan
}

func buildPartBulkPlanItem(ctx context.Context, client PartWriteClient, item BulkUpdatePartItem) partBulkPlanItem {
	if item.ID <= 0 {
		return partBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	if item.CategoryID != nil && *item.CategoryID <= 0 {
		return partBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "category_id must be positive when provided"}}
	}
	if item.DefaultLocation != nil && *item.DefaultLocation <= 0 {
		return partBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "default_location_id must be positive when provided"}}
	}
	before, err := client.GetPartDetail(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return partBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable part"}}
	}
	// BulkUpdatePartItem mirrors UpdatePartInput's exact field set (update_part
	// has no fields that need excluding for bulk use), so this is a plain
	// field-preserving type conversion, not a lossy or reordering one.
	fields, err := partPatchFields(UpdatePartInput(item), before)
	if err != nil {
		return partBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	if len(fields) == 0 {
		return partBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "at least one approved PATCH field is required"}, Before: before}
	}
	return partBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, Fields: fields}
}

type partBulkAdapter struct {
	client PartWriteClient
	bulkRecordStore[PartDetailView]
}

func (a *partBulkAdapter) Preflight(ctx context.Context, item partBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetPartDetail(ctx, item.ID)
	if err != nil {
		return false, "", err
	}
	if current.PK != item.ID {
		return false, "", errors.New("part identity verification failed")
	}
	if patchMatches(item.Fields, partValues(current)) {
		return true, "already at target state", nil
	}
	if !valuesMatchForKeys(item.Fields, partValues(current), partValues(item.Before)) {
		return false, "", errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *partBulkAdapter) Mutate(ctx context.Context, item partBulkPlanItem) error {
	if _, err := a.client.UpdatePart(ctx, item.ID, item.Fields); err != nil {
		if !ambiguousAdminMutation(err) {
			return err
		}
		current, getErr := a.client.GetPartDetail(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || !patchMatches(item.Fields, partValues(current)) {
			return err
		}
	}
	return nil
}

func (a *partBulkAdapter) Verify(ctx context.Context, item partBulkPlanItem) error {
	current, err := a.client.GetPartDetail(ctx, item.ID)
	if err != nil || current.PK != item.ID || !patchMatches(item.Fields, partValues(current)) {
		return errors.New("read-back does not match the reviewed plan")
	}
	a.store(item.ID, partDetailView(current))
	return nil
}

func bulkUpdateParts(deps Dependencies) mcp.ToolHandlerFor[BulkUpdatePartsInput, BulkUpdateOutput[PartDetailView]] {
	return LookupHandler[PartWriteClient, BulkUpdatePartsInput, BulkUpdateOutput[PartDetailView]](deps, BulkUpdatePartsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartWriteClient, input BulkUpdatePartsInput) (*mcp.CallToolResult, BulkUpdateOutput[PartDetailView], error) {
			if len(input.Items) == 0 || len(input.Items) > bulkUpdateMaxItems {
				return bulkItemCountClarification[PartDetailView](BulkUpdatePartsToolName, len(input.Items))
			}
			plan := buildPartBulkPlan(ctx, client, input.Items)
			out := BulkUpdateOutput[PartDetailView]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[partBulkPlanItem, PartDetailView](plan.Items)
				token, err := deps.partBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[PartDetailView]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[PartDetailView]()
			}
			if input.PlanHash == "" || !deps.partBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[PartDetailView]()
			}
			adapter := &partBulkAdapter{client: client}
			results := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: bulkUpdateConcurrency})
			out.Items = bulkResults[partBulkPlanItem, PartDetailView](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			return TextResult(out.Status), out, nil
		})
}

// ---------------------------------------------------------------------------
// Companies
// ---------------------------------------------------------------------------

type BulkUpdateCompanyItem struct {
	ID             int     `json:"id" jsonschema:"Stable company primary key."`
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	Website        *string `json:"website,omitempty" jsonschema:"Complete HTTP(S) company website URL without userinfo; query parameters and fragments are preserved. An explicit empty string clears it."`
	Phone          *string `json:"phone,omitempty" jsonschema:"Replacement contact phone number. An explicit empty string clears it."`
	Email          *string `json:"email,omitempty" jsonschema:"Replacement contact email. Mutually exclusive with clear_email."`
	ClearEmail     bool    `json:"clear_email,omitempty" jsonschema:"Explicitly PATCH email to null; mutually exclusive with email."`
	Contact        *string `json:"contact,omitempty" jsonschema:"Replacement free-text point-of-contact name. An explicit empty string clears it."`
	TaxID          *string `json:"tax_id,omitempty" jsonschema:"Replacement business tax identifier. An explicit empty string clears it."`
	Link           *string `json:"link,omitempty" jsonschema:"Complete HTTP(S) external company-information URL without userinfo, distinct from website."`
	Currency       *string `json:"currency,omitempty"`
	Active         *bool   `json:"active,omitempty"`
	IsSupplier     *bool   `json:"is_supplier,omitempty" jsonschema:"May only be set true here. Removing the supplier role requires update_company's guarded review."`
	IsManufacturer *bool   `json:"is_manufacturer,omitempty" jsonschema:"May only be set true here. Removing the manufacturer role requires update_company's guarded review."`
	Notes          *string `json:"notes,omitempty" jsonschema:"Replacement markdown notes."`
	ClearNotes     bool    `json:"clear_notes,omitempty" jsonschema:"Explicitly PATCH notes to null; mutually exclusive with notes."`
}

type BulkUpdateCompaniesInput struct {
	Items    []BulkUpdateCompanyItem `json:"items" jsonschema:"Ordered batch of independent company updates, 1 to 25 items."`
	DryRun   bool                    `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                    `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                  `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type companyBulkPlanItem struct {
	bulkPlanItemBase
	Before inventree.CompanyDetail
	Fields inventree.PatchFields
}

type companyBulkPlan struct {
	Items []companyBulkPlanItem
}

func (p companyBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p companyBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

func buildCompanyBulkPlan(ctx context.Context, client CompanyAdminClient, items []BulkUpdateCompanyItem) companyBulkPlan {
	plan := companyBulkPlan{Items: make([]companyBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildCompanyBulkPlanItem(ctx, client, item)
	}
	return plan
}

func buildCompanyBulkPlanItem(ctx context.Context, client CompanyAdminClient, item BulkUpdateCompanyItem) companyBulkPlanItem {
	if item.ID <= 0 {
		return companyBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	before, err := client.GetCompanyDetail(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return companyBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable company"}}
	}
	if item.IsSupplier != nil && !*item.IsSupplier {
		return companyBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "bulk_update_companies cannot remove the supplier role; use update_company"}, Before: before}
	}
	if item.IsManufacturer != nil && !*item.IsManufacturer {
		return companyBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "bulk_update_companies cannot remove the manufacturer role; use update_company"}, Before: before}
	}
	mapped := UpdateCompanyInput{
		ID: item.ID, Name: item.Name, Description: item.Description, Website: item.Website, Phone: item.Phone,
		Email: item.Email, ClearEmail: item.ClearEmail, Contact: item.Contact, TaxID: item.TaxID, Link: item.Link,
		Currency: item.Currency, Active: item.Active, IsSupplier: item.IsSupplier, IsManufacturer: item.IsManufacturer,
		Notes: item.Notes, ClearNotes: item.ClearNotes,
	}
	fields, err := companyPatch(mapped)
	if err != nil {
		return companyBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	if len(fields) == 0 {
		return companyBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "at least one approved PATCH field is required"}, Before: before}
	}
	return companyBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, Fields: fields}
}

type companyBulkAdapter struct {
	client CompanyAdminClient
	bulkRecordStore[CompanyView]
}

func (a *companyBulkAdapter) Preflight(ctx context.Context, item companyBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetCompanyDetail(ctx, item.ID)
	if err != nil {
		return false, "", err
	}
	if current.PK != item.ID {
		return false, "", errors.New("company identity verification failed")
	}
	if companyFieldsMatch(current, item.Fields) {
		return true, "already at target state", nil
	}
	if !valuesMatchForKeys(item.Fields, companyValues(current), companyValues(item.Before)) {
		return false, "", errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *companyBulkAdapter) Mutate(ctx context.Context, item companyBulkPlanItem) error {
	if _, err := a.client.UpdateCompany(ctx, item.ID, item.Fields); err != nil {
		if !ambiguousAdminMutation(err) {
			return err
		}
		current, getErr := a.client.GetCompanyDetail(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || !companyFieldsMatch(current, item.Fields) {
			return err
		}
	}
	return nil
}

func (a *companyBulkAdapter) Verify(ctx context.Context, item companyBulkPlanItem) error {
	current, err := a.client.GetCompanyDetail(ctx, item.ID)
	if err != nil || current.PK != item.ID || !companyFieldsMatch(current, item.Fields) {
		return errors.New("read-back does not match the reviewed plan")
	}
	// companyRoleIntegrity's only check (a removed role must stay free of
	// sourcing links) can never trigger here: bulk_update_companies rejects
	// an explicit false is_supplier/is_manufacturer at plan-build time.
	a.store(item.ID, companyView(current))
	return nil
}

func bulkUpdateCompanies(deps Dependencies) mcp.ToolHandlerFor[BulkUpdateCompaniesInput, BulkUpdateOutput[CompanyView]] {
	return LookupHandler[CompanyAdminClient, BulkUpdateCompaniesInput, BulkUpdateOutput[CompanyView]](deps, BulkUpdateCompaniesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input BulkUpdateCompaniesInput) (*mcp.CallToolResult, BulkUpdateOutput[CompanyView], error) {
			if len(input.Items) == 0 || len(input.Items) > bulkUpdateMaxItems {
				return bulkItemCountClarification[CompanyView](BulkUpdateCompaniesToolName, len(input.Items))
			}
			plan := buildCompanyBulkPlan(ctx, client, input.Items)
			out := BulkUpdateOutput[CompanyView]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[companyBulkPlanItem, CompanyView](plan.Items)
				token, err := deps.companyBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[CompanyView]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[CompanyView]()
			}
			if input.PlanHash == "" || !deps.companyBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[CompanyView]()
			}
			adapter := &companyBulkAdapter{client: client}
			results := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: bulkUpdateConcurrency})
			out.Items = bulkResults[companyBulkPlanItem, CompanyView](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			return TextResult(out.Status), out, nil
		})
}

// ---------------------------------------------------------------------------
// Part categories
// ---------------------------------------------------------------------------

type BulkUpdatePartCategoryItem struct {
	ID                   int     `json:"id" jsonschema:"Stable InvenTree category primary key."`
	Name                 *string `json:"name,omitempty" jsonschema:"Optional replacement name; surrounding whitespace is removed."`
	Description          *string `json:"description,omitempty" jsonschema:"Optional replacement description; an explicit empty string is preserved."`
	DefaultLocationID    *int    `json:"default_location_id,omitempty" jsonschema:"Optional existing replacement default stock-location primary key."`
	ClearDefaultLocation bool    `json:"clear_default_location,omitempty" jsonschema:"Explicitly PATCH default_location to null; mutually exclusive with default_location_id."`
	DefaultKeywords      *string `json:"default_keywords,omitempty" jsonschema:"Optional replacement default keywords; an explicit empty string is preserved."`
	ClearDefaultKeywords bool    `json:"clear_default_keywords,omitempty" jsonschema:"Explicitly PATCH default_keywords to null; mutually exclusive with default_keywords."`
	Icon                 *string `json:"icon,omitempty" jsonschema:"Optional replacement icon identifier; an explicit empty string is preserved."`
	ClearIcon            bool    `json:"clear_icon,omitempty" jsonschema:"Explicitly PATCH icon to null; mutually exclusive with icon."`
}

type BulkUpdatePartCategoriesInput struct {
	Items    []BulkUpdatePartCategoryItem `json:"items" jsonschema:"Ordered batch of independent category updates, 1 to 25 items."`
	DryRun   bool                         `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                         `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                       `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type categoryBulkPlanItem struct {
	bulkPlanItemBase
	Before inventree.Category
	Fields inventree.PatchFields
}

type categoryBulkPlan struct {
	Items []categoryBulkPlanItem
}

func (p categoryBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p categoryBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

// categoryBeforeFields projects before's own values for exactly the keys
// touched by fields into a PatchFields value, so categoryMatchesPatch (which
// compares an inventree.Category against a PatchFields) can also be reused
// to detect drift between the captured "before" state and fresh current
// state, not just to confirm a post-write read-back.
func categoryBeforeFields(before inventree.Category, fields inventree.PatchFields) inventree.PatchFields {
	result := inventree.PatchFields{}
	for key := range fields {
		switch key {
		case "name":
			result[key] = inventree.Set(before.Name)
		case "description":
			result[key] = inventree.Set(before.Description)
		case "default_location":
			if before.DefaultLocation != nil {
				result[key] = inventree.Set(*before.DefaultLocation)
			} else {
				result[key] = inventree.Null()
			}
		case "default_keywords":
			if before.DefaultKeywords != nil {
				result[key] = inventree.Set(*before.DefaultKeywords)
			} else {
				result[key] = inventree.Null()
			}
		case "icon":
			if before.Icon != nil {
				result[key] = inventree.Set(*before.Icon)
			} else {
				result[key] = inventree.Null()
			}
		}
	}
	return result
}

func buildCategoryBulkPlan(ctx context.Context, client CategoryAdminClient, items []BulkUpdatePartCategoryItem) categoryBulkPlan {
	plan := categoryBulkPlan{Items: make([]categoryBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildCategoryBulkPlanItem(ctx, client, item)
	}
	return plan
}

func buildCategoryBulkPlanItem(ctx context.Context, client CategoryAdminClient, item BulkUpdatePartCategoryItem) categoryBulkPlanItem {
	if item.ID <= 0 {
		return categoryBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	mapped := UpdatePartCategoryInput{
		ID: item.ID, Name: item.Name, Description: item.Description,
		DefaultLocationID: item.DefaultLocationID, ClearDefaultLocation: item.ClearDefaultLocation,
		DefaultKeywords: item.DefaultKeywords, ClearDefaultKeywords: item.ClearDefaultKeywords,
		Icon: item.Icon, ClearIcon: item.ClearIcon,
	}
	if reason := categoryClearConflict(mapped); reason != "" {
		return categoryBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: reason}}
	}
	before, err := client.GetPartCategory(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return categoryBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable category"}}
	}
	fields, targetName, _, err := categoryPatch(mapped, before)
	if err != nil {
		return categoryBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	if len(fields) == 0 {
		return categoryBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "at least one approved PATCH field is required"}, Before: before}
	}
	if item.DefaultLocationID != nil {
		location, err := client.GetStockLocation(ctx, *item.DefaultLocationID)
		if err != nil || location.PK != *item.DefaultLocationID {
			return categoryBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "default_location_id does not identify a readable stock location"}, Before: before}
		}
	}
	if targetName != before.Name {
		matches, err := categoryDuplicates(ctx, client, targetName, before.Parent, item.ID)
		if err != nil {
			return categoryBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "category duplicate preflight failed"}, Before: before}
		}
		if len(matches) > 0 {
			return categoryBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "the requested name collides with an existing same-parent category"}, Before: before}
		}
	}
	return categoryBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, Fields: fields}
}

type categoryBulkAdapter struct {
	client CategoryAdminClient
	bulkRecordStore[inventree.Category]
}

func (a *categoryBulkAdapter) Preflight(ctx context.Context, item categoryBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetPartCategory(ctx, item.ID)
	if err != nil {
		return false, "", err
	}
	if current.PK != item.ID {
		return false, "", errors.New("category identity verification failed")
	}
	if categoryMatchesPatch(current, item.Fields) {
		return true, "already at target state", nil
	}
	if !categoryMatchesPatch(current, categoryBeforeFields(item.Before, item.Fields)) {
		return false, "", errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *categoryBulkAdapter) Mutate(ctx context.Context, item categoryBulkPlanItem) error {
	if _, err := a.client.UpdatePartCategory(ctx, item.ID, item.Fields); err != nil {
		if !ambiguousCategoryMutation(err) {
			return err
		}
		current, getErr := a.client.GetPartCategory(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || !categoryMatchesPatch(current, item.Fields) {
			return err
		}
	}
	return nil
}

func (a *categoryBulkAdapter) Verify(ctx context.Context, item categoryBulkPlanItem) error {
	current, err := a.client.GetPartCategory(ctx, item.ID)
	if err != nil || current.PK != item.ID || !categoryMatchesPatch(current, item.Fields) {
		return errors.New("read-back does not match the reviewed plan")
	}
	if recovery := verifyCategoryPostWrite(ctx, a.client, current); recovery != "" {
		return errors.New("postflight verification failed")
	}
	a.store(item.ID, current)
	return nil
}

func bulkUpdatePartCategories(deps Dependencies) mcp.ToolHandlerFor[BulkUpdatePartCategoriesInput, BulkUpdateOutput[inventree.Category]] {
	return LookupHandler[CategoryAdminClient, BulkUpdatePartCategoriesInput, BulkUpdateOutput[inventree.Category]](deps, BulkUpdatePartCategoriesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CategoryAdminClient, input BulkUpdatePartCategoriesInput) (*mcp.CallToolResult, BulkUpdateOutput[inventree.Category], error) {
			if len(input.Items) == 0 || len(input.Items) > bulkUpdateMaxItems {
				return bulkItemCountClarification[inventree.Category](BulkUpdatePartCategoriesToolName, len(input.Items))
			}
			plan := buildCategoryBulkPlan(ctx, client, input.Items)
			out := BulkUpdateOutput[inventree.Category]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[categoryBulkPlanItem, inventree.Category](plan.Items)
				token, err := deps.categoryBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[inventree.Category]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[inventree.Category]()
			}
			if input.PlanHash == "" || !deps.categoryBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[inventree.Category]()
			}
			adapter := &categoryBulkAdapter{client: client}
			results := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: bulkUpdateConcurrency})
			out.Items = bulkResults[categoryBulkPlanItem, inventree.Category](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			return TextResult(out.Status), out, nil
		})
}

// ---------------------------------------------------------------------------
// Supplier parts
// ---------------------------------------------------------------------------

type BulkUpdateSupplierPartItem struct {
	ID               int      `json:"id" jsonschema:"Stable supplier-part primary key."`
	SKU              *string  `json:"sku,omitempty" jsonschema:"Replacement supplier SKU; cannot be blank."`
	Description      *string  `json:"description,omitempty"`
	ClearDescription bool     `json:"clear_description,omitempty"`
	Link             *string  `json:"link,omitempty" jsonschema:"Complete HTTP(S) supplier-part URL without userinfo."`
	ClearLink        bool     `json:"clear_link,omitempty"`
	Active           *bool    `json:"active,omitempty"`
	Primary          *bool    `json:"primary,omitempty"`
	Packaging        *string  `json:"packaging,omitempty"`
	ClearPackaging   bool     `json:"clear_packaging,omitempty"`
	PackQuantity     *string  `json:"pack_quantity,omitempty"`
	Note             *string  `json:"note,omitempty"`
	ClearNote        bool     `json:"clear_note,omitempty"`
	Notes            *string  `json:"notes,omitempty" jsonschema:"Replacement long Markdown notes, distinct from the short note field."`
	ClearNotes       bool     `json:"clear_notes,omitempty"`
	Available        *float64 `json:"available,omitempty" jsonschema:"Replacement upstream availability quantity. Explicit zero is preserved."`
}

type BulkUpdateSupplierPartsInput struct {
	Items    []BulkUpdateSupplierPartItem `json:"items" jsonschema:"Ordered batch of independent supplier-part updates, 1 to 25 items."`
	DryRun   bool                         `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                         `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                       `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type supplierPartBulkPlanItem struct {
	bulkPlanItemBase
	Before inventree.SupplierPartDetail
	Fields inventree.PatchFields
}

type supplierPartBulkPlan struct {
	Items []supplierPartBulkPlanItem
}

func (p supplierPartBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p supplierPartBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

func buildSupplierPartBulkPlan(ctx context.Context, client CompanyAdminClient, items []BulkUpdateSupplierPartItem) supplierPartBulkPlan {
	plan := supplierPartBulkPlan{Items: make([]supplierPartBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildSupplierPartBulkPlanItem(ctx, client, item)
	}
	return plan
}

func buildSupplierPartBulkPlanItem(ctx context.Context, client CompanyAdminClient, item BulkUpdateSupplierPartItem) supplierPartBulkPlanItem {
	if item.ID <= 0 {
		return supplierPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	before, err := client.GetSupplierPartDetail(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return supplierPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable supplier part"}}
	}
	mapped := UpdateSupplierPartInput{
		ID: item.ID, SKU: item.SKU, Description: item.Description, ClearDescription: item.ClearDescription,
		Link: item.Link, ClearLink: item.ClearLink, Active: item.Active, Primary: item.Primary,
		Packaging: item.Packaging, ClearPackaging: item.ClearPackaging, PackQuantity: item.PackQuantity,
		Note: item.Note, ClearNote: item.ClearNote, Notes: item.Notes, ClearNotes: item.ClearNotes,
		Available: item.Available,
	}
	fields, target, err := supplierPartPatch(mapped, before)
	if err != nil {
		return supplierPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	if len(fields) == 0 {
		return supplierPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "at least one approved PATCH field is required"}, Before: before}
	}
	if err := validatePartAndCompany(ctx, client, target.Part, target.Supplier, true); err != nil {
		return supplierPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	matches, err := supplierDuplicates(ctx, client, target.Supplier, target.SKU, item.ID)
	if err != nil {
		return supplierPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "supplier-part duplicate preflight failed"}, Before: before}
	}
	if len(matches) > 0 {
		return supplierPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "the requested SKU collides with an existing supplier+SKU pair"}, Before: before}
	}
	return supplierPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, Fields: fields}
}

type supplierPartBulkAdapter struct {
	client CompanyAdminClient
	bulkRecordStore[SupplierPartView]
}

func (a *supplierPartBulkAdapter) Preflight(ctx context.Context, item supplierPartBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetSupplierPartDetail(ctx, item.ID)
	if err != nil {
		return false, "", err
	}
	if current.PK != item.ID {
		return false, "", errors.New("supplier-part identity verification failed")
	}
	if supplierPartFieldsMatch(current, item.Fields) {
		return true, "already at target state", nil
	}
	if !valuesMatchForKeys(item.Fields, supplierPartValues(current), supplierPartValues(item.Before)) {
		return false, "", errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *supplierPartBulkAdapter) Mutate(ctx context.Context, item supplierPartBulkPlanItem) error {
	if _, err := a.client.UpdateSupplierPart(ctx, item.ID, item.Fields); err != nil {
		if !ambiguousAdminMutation(err) {
			return err
		}
		current, getErr := a.client.GetSupplierPartDetail(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || !supplierPartFieldsMatch(current, item.Fields) {
			return err
		}
	}
	return nil
}

func (a *supplierPartBulkAdapter) Verify(ctx context.Context, item supplierPartBulkPlanItem) error {
	current, err := a.client.GetSupplierPartDetail(ctx, item.ID)
	if err != nil || current.PK != item.ID || !supplierPartFieldsMatch(current, item.Fields) {
		return errors.New("read-back does not match the reviewed plan")
	}
	if err := supplierPartIntegrity(ctx, a.client, current); err != nil {
		return err
	}
	matches, err := supplierDuplicates(ctx, a.client, current.Supplier, current.SKU, current.PK)
	if err != nil || len(matches) > 0 {
		return errors.New("postflight duplicate verification failed")
	}
	a.store(item.ID, supplierPartView(current))
	return nil
}

func bulkUpdateSupplierParts(deps Dependencies) mcp.ToolHandlerFor[BulkUpdateSupplierPartsInput, BulkUpdateOutput[SupplierPartView]] {
	return LookupHandler[CompanyAdminClient, BulkUpdateSupplierPartsInput, BulkUpdateOutput[SupplierPartView]](deps, BulkUpdateSupplierPartsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input BulkUpdateSupplierPartsInput) (*mcp.CallToolResult, BulkUpdateOutput[SupplierPartView], error) {
			if len(input.Items) == 0 || len(input.Items) > bulkUpdateMaxItems {
				return bulkItemCountClarification[SupplierPartView](BulkUpdateSupplierPartsToolName, len(input.Items))
			}
			plan := buildSupplierPartBulkPlan(ctx, client, input.Items)
			out := BulkUpdateOutput[SupplierPartView]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[supplierPartBulkPlanItem, SupplierPartView](plan.Items)
				token, err := deps.supplierPartBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[SupplierPartView]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[SupplierPartView]()
			}
			if input.PlanHash == "" || !deps.supplierPartBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[SupplierPartView]()
			}
			adapter := &supplierPartBulkAdapter{client: client}
			results := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: bulkUpdateConcurrency})
			out.Items = bulkResults[supplierPartBulkPlanItem, SupplierPartView](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			return TextResult(out.Status), out, nil
		})
}

// ---------------------------------------------------------------------------
// Manufacturer parts
// ---------------------------------------------------------------------------

type BulkUpdateManufacturerPartItem struct {
	ID               int     `json:"id" jsonschema:"Stable manufacturer-part primary key."`
	MPN              *string `json:"mpn,omitempty"`
	ClearMPN         bool    `json:"clear_mpn,omitempty"`
	Description      *string `json:"description,omitempty"`
	ClearDescription bool    `json:"clear_description,omitempty"`
	Link             *string `json:"link,omitempty" jsonschema:"Complete HTTP(S) manufacturer-part URL without userinfo."`
	ClearLink        bool    `json:"clear_link,omitempty"`
	Notes            *string `json:"notes,omitempty" jsonschema:"Replacement long Markdown notes."`
	ClearNotes       bool    `json:"clear_notes,omitempty"`
}

type BulkUpdateManufacturerPartsInput struct {
	Items    []BulkUpdateManufacturerPartItem `json:"items" jsonschema:"Ordered batch of independent manufacturer-part updates, 1 to 25 items."`
	DryRun   bool                             `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                             `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                           `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type manufacturerPartBulkPlanItem struct {
	bulkPlanItemBase
	Before inventree.ManufacturerPartDetail
	Fields inventree.PatchFields
}

type manufacturerPartBulkPlan struct {
	Items []manufacturerPartBulkPlanItem
}

func (p manufacturerPartBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p manufacturerPartBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

func buildManufacturerPartBulkPlan(ctx context.Context, client CompanyAdminClient, items []BulkUpdateManufacturerPartItem) manufacturerPartBulkPlan {
	plan := manufacturerPartBulkPlan{Items: make([]manufacturerPartBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildManufacturerPartBulkPlanItem(ctx, client, item)
	}
	return plan
}

func buildManufacturerPartBulkPlanItem(ctx context.Context, client CompanyAdminClient, item BulkUpdateManufacturerPartItem) manufacturerPartBulkPlanItem {
	if item.ID <= 0 {
		return manufacturerPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	before, err := client.GetManufacturerPartDetail(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return manufacturerPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable manufacturer part"}}
	}
	mapped := UpdateManufacturerPartInput{
		ID: item.ID, MPN: item.MPN, ClearMPN: item.ClearMPN, Description: item.Description,
		ClearDescription: item.ClearDescription, Link: item.Link, ClearLink: item.ClearLink,
		Notes: item.Notes, ClearNotes: item.ClearNotes,
	}
	fields, target, err := manufacturerPartPatch(mapped, before)
	if err != nil {
		return manufacturerPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	if len(fields) == 0 {
		return manufacturerPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "at least one approved PATCH field is required"}, Before: before}
	}
	if err := validatePartAndCompany(ctx, client, target.Part, target.Manufacturer, false); err != nil {
		return manufacturerPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	if mpn := normalized(derefString(target.MPN)); mpn != "" {
		matches, err := manufacturerDuplicates(ctx, client, target.Manufacturer, mpn, item.ID)
		if err != nil {
			return manufacturerPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "manufacturer-part duplicate preflight failed"}, Before: before}
		}
		if len(matches) > 0 {
			return manufacturerPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "the requested MPN collides with an existing manufacturer+MPN pair"}, Before: before}
		}
	}
	return manufacturerPartBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, Fields: fields}
}

type manufacturerPartBulkAdapter struct {
	client CompanyAdminClient
	bulkRecordStore[ManufacturerPartView]
}

func (a *manufacturerPartBulkAdapter) Preflight(ctx context.Context, item manufacturerPartBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetManufacturerPartDetail(ctx, item.ID)
	if err != nil {
		return false, "", err
	}
	if current.PK != item.ID {
		return false, "", errors.New("manufacturer-part identity verification failed")
	}
	if manufacturerPartFieldsMatch(current, item.Fields) {
		return true, "already at target state", nil
	}
	if !valuesMatchForKeys(item.Fields, manufacturerPartValues(current), manufacturerPartValues(item.Before)) {
		return false, "", errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *manufacturerPartBulkAdapter) Mutate(ctx context.Context, item manufacturerPartBulkPlanItem) error {
	if _, err := a.client.UpdateManufacturerPart(ctx, item.ID, item.Fields); err != nil {
		if !ambiguousAdminMutation(err) {
			return err
		}
		current, getErr := a.client.GetManufacturerPartDetail(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || !manufacturerPartFieldsMatch(current, item.Fields) {
			return err
		}
	}
	return nil
}

func (a *manufacturerPartBulkAdapter) Verify(ctx context.Context, item manufacturerPartBulkPlanItem) error {
	current, err := a.client.GetManufacturerPartDetail(ctx, item.ID)
	if err != nil || current.PK != item.ID || !manufacturerPartFieldsMatch(current, item.Fields) {
		return errors.New("read-back does not match the reviewed plan")
	}
	blocking, err := manufacturerPartIntegrity(ctx, a.client, current)
	if err != nil || len(blocking) > 0 {
		return errors.New("postflight relationship verification failed")
	}
	if mpn := normalized(derefString(current.MPN)); mpn != "" {
		matches, err := manufacturerDuplicates(ctx, a.client, current.Manufacturer, mpn, current.PK)
		if err != nil || len(matches) > 0 {
			return errors.New("postflight duplicate verification failed")
		}
	}
	a.store(item.ID, manufacturerPartView(current))
	return nil
}

func bulkUpdateManufacturerParts(deps Dependencies) mcp.ToolHandlerFor[BulkUpdateManufacturerPartsInput, BulkUpdateOutput[ManufacturerPartView]] {
	return LookupHandler[CompanyAdminClient, BulkUpdateManufacturerPartsInput, BulkUpdateOutput[ManufacturerPartView]](deps, BulkUpdateManufacturerPartsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyAdminClient, input BulkUpdateManufacturerPartsInput) (*mcp.CallToolResult, BulkUpdateOutput[ManufacturerPartView], error) {
			if len(input.Items) == 0 || len(input.Items) > bulkUpdateMaxItems {
				return bulkItemCountClarification[ManufacturerPartView](BulkUpdateManufacturerPartsToolName, len(input.Items))
			}
			plan := buildManufacturerPartBulkPlan(ctx, client, input.Items)
			out := BulkUpdateOutput[ManufacturerPartView]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[manufacturerPartBulkPlanItem, ManufacturerPartView](plan.Items)
				token, err := deps.manufacturerPartBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[ManufacturerPartView]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[ManufacturerPartView]()
			}
			if input.PlanHash == "" || !deps.manufacturerPartBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[ManufacturerPartView]()
			}
			adapter := &manufacturerPartBulkAdapter{client: client}
			results := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: bulkUpdateConcurrency})
			out.Items = bulkResults[manufacturerPartBulkPlanItem, ManufacturerPartView](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			return TextResult(out.Status), out, nil
		})
}
