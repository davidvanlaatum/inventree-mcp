package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	stockLocationTypeScanLimit          = 1000
	stockLocationTypePageSize           = 100
	stockLocationTypeReferenceScanLimit = 1000
	stockLocationTypeReferencePageSize  = 100

	stockLocationTypeDeletePlanLifetime               = 5 * time.Minute
	stockLocationTypeDeletePlanMaxEntries             = 4096
	stockLocationTypeDeletePlanMaxEntriesPerPrincipal = 64
)

var (
	errStockLocationTypeScanLimit          = errors.New("stock-location-type duplicate scan safety limit exceeded")
	errStockLocationTypeReferenceScanLimit = errors.New("stock-location-type reference scan safety limit exceeded")
)

// StockLocationTypeAdminClient is shared by create, update, and guarded
// delete for stock-location types. SearchStockLocationsPage backs the
// bounded location-reference snapshot that guards deletion.
type StockLocationTypeAdminClient interface {
	GetStockLocationType(context.Context, int) (inventree.StockLocationType, error)
	SearchStockLocationTypesPage(context.Context, inventree.SearchQuery) (inventree.StockLocationTypePage, error)
	CreateStockLocationType(context.Context, inventree.StockLocationTypeCreate) (inventree.StockLocationType, error)
	UpdateStockLocationType(context.Context, int, inventree.PatchFields) (inventree.StockLocationType, error)
	DeleteStockLocationType(context.Context, int) error
	SearchStockLocationsPage(context.Context, inventree.StockLocationQuery) (inventree.StockLocationPage, error)
}

type CreateStockLocationTypeInput struct {
	Name        string  `json:"name" jsonschema:"Required stock-location-type name; surrounding whitespace is removed. A case-insensitive duplicate name is refused."`
	Description *string `json:"description,omitempty" jsonschema:"Optional description; an explicit empty string is preserved."`
	Icon        *string `json:"icon,omitempty" jsonschema:"Optional default icon applied to locations of this type that have no custom icon of their own; an explicit empty string is preserved."`
}

type UpdateStockLocationTypeInput struct {
	ID          int     `json:"id" jsonschema:"Stable InvenTree stock-location-type primary key."`
	Name        *string `json:"name,omitempty" jsonschema:"Optional replacement name; surrounding whitespace is removed. A case-insensitive duplicate name is refused."`
	Description *string `json:"description,omitempty" jsonschema:"Optional replacement description; an explicit empty string is preserved."`
	Icon        *string `json:"icon,omitempty" jsonschema:"Optional replacement default icon; an explicit empty string is preserved."`
}

type DeleteStockLocationTypeInput struct {
	ID       int    `json:"id" jsonschema:"Stable InvenTree stock-location-type primary key."`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Required true after reviewing the exact current type and every referencing location, together with plan_hash."`
	PlanHash string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

type StockLocationTypeMutationOutput struct {
	Status        string                       `json:"status"`
	Record        *inventree.StockLocationType `json:"record,omitempty"`
	Recovered     bool                         `json:"recovered,omitempty"`
	RecoveryPlan  string                       `json:"recovery_plan,omitempty"`
	Clarification *ClarificationResponse       `json:"clarification,omitempty"`
}

// StockLocationTypeDeletePlan binds the exact current type and every
// referencing location's stable ID. Any change to either between preview and
// confirm changes this plan's digest, so a stale token is rejected rather
// than silently deleting against state the operator never reviewed.
// Embedding the whole Type record (rather than just its identity) also binds
// mutable fields such as LocationCount as defense-in-depth, even though
// ReferencingLocationIDs already tracks the same underlying reference set.
type StockLocationTypeDeletePlan struct {
	Action                 string                      `json:"action"`
	Type                   inventree.StockLocationType `json:"type"`
	ReferencingLocationIDs []int                       `json:"referencing_location_ids"`
}

type StockLocationTypeDeleteOutput struct {
	Status                 string                       `json:"status"`
	Record                 *inventree.StockLocationType `json:"record,omitempty"`
	ReferencingLocationIDs []int                        `json:"referencing_location_ids,omitempty"`
	PlanHash               string                       `json:"plan_hash,omitempty"`
	Verified               bool                         `json:"verified,omitempty"`
	Recovered              bool                         `json:"recovered,omitempty"`
	RecoveryPlan           string                       `json:"recovery_plan,omitempty"`
	Clarification          *ClarificationResponse       `json:"clarification,omitempty"`
}

type stockLocationTypeDeleteConfirmation struct {
	digest    string
	principal string
	typeID    int
	expiresAt time.Time
}

type stockLocationTypeDeletePlanStore struct {
	mu                     sync.Mutex
	entries                map[string]stockLocationTypeDeleteConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newStockLocationTypeDeletePlanStore(now func() time.Time, token func() (string, error)) *stockLocationTypeDeletePlanStore {
	return &stockLocationTypeDeletePlanStore{
		entries: map[string]stockLocationTypeDeleteConfirmation{}, now: now, token: token, principal: stockPlanPrincipal,
		maxEntries: stockLocationTypeDeletePlanMaxEntries, maxEntriesPerPrincipal: stockLocationTypeDeletePlanMaxEntriesPerPrincipal,
	}
}

func (s *stockLocationTypeDeletePlanStore) issue(ctx context.Context, plan StockLocationTypeDeletePlan) (string, error) {
	digest, err := stockLocationTypeDeletePlanDigest(plan)
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
		if !now.Before(entry.expiresAt) || entry.principal == principal && entry.typeID == plan.Type.PK {
			delete(s.entries, key)
			continue
		}
		if entry.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errors.New("too many outstanding stock-location-type deletion confirmation plans")
	}
	s.entries[token] = stockLocationTypeDeleteConfirmation{digest: digest, principal: principal, typeID: plan.Type.PK, expiresAt: now.Add(stockLocationTypeDeletePlanLifetime)}
	return token, nil
}

func (s *stockLocationTypeDeletePlanStore) consume(ctx context.Context, token string, plan StockLocationTypeDeletePlan) bool {
	digest, err := stockLocationTypeDeletePlanDigest(plan)
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
	if !ok || entry.digest != digest || entry.principal != s.principal(ctx) || entry.typeID != plan.Type.PK || !now.Before(entry.expiresAt) {
		return false
	}
	delete(s.entries, token)
	return true
}

func stockLocationTypeDeletePlanDigest(plan StockLocationTypeDeletePlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerStockLocationTypeAdminTools(server *mcp.Server, deps Dependencies) {
	if deps.stockLocationTypeDeletePlanStore == nil {
		deps.stockLocationTypeDeletePlanStore = newStockLocationTypeDeletePlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, CreateStockLocationTypeToolName, "Create stock location type", "Creates a guarded stock-location type after a bounded case-insensitive duplicate-name preflight.", createStockLocationType(deps))
	addWriteTool(server, deps, UpdateStockLocationTypeToolName, "Update stock location type", "Partially updates a stable stock-location type's name, description, or icon with duplicate-name preflight and exact read-back.", updateStockLocationType(deps))
	addWriteTool(server, deps, DeleteStockLocationTypeToolName, "Delete stock location type", "Previews and confirms deleting one stock-location type, reporting every referencing location before execution.", deleteStockLocationType(deps))
}

func createStockLocationType(deps Dependencies) mcp.ToolHandlerFor[CreateStockLocationTypeInput, StockLocationTypeMutationOutput] {
	return LookupHandler[StockLocationTypeAdminClient, CreateStockLocationTypeInput, StockLocationTypeMutationOutput](deps, CreateStockLocationTypeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockLocationTypeAdminClient, input CreateStockLocationTypeInput) (*mcp.CallToolResult, StockLocationTypeMutationOutput, error) {
			name := strings.TrimSpace(input.Name)
			if name == "" {
				return stockLocationTypeClarification("What nonblank name should the stock-location type use?", "name", "create_stock_location_type requires an explicit nonblank name", "name", nil, map[string]any{"name": name})
			}
			matches, err := stockLocationTypeDuplicates(ctx, client, name, 0)
			if err != nil {
				return stockLocationTypeDuplicateFailure(err, stockLocationTypeCreateRetry(input, name))
			}
			if len(matches) > 0 {
				return stockLocationTypeCandidates("Should an existing stock-location type be used?", "a case-insensitive duplicate name already exists", matches, stockLocationTypeCreateRetry(input, name))
			}
			create := inventree.StockLocationTypeCreate{Name: name}
			if input.Description != nil {
				create.Description = *input.Description
			}
			if input.Icon != nil {
				create.Icon = *input.Icon
			}
			created, err := client.CreateStockLocationType(ctx, create)
			if err != nil {
				if !ambiguousCategoryMutation(err) {
					return nil, StockLocationTypeMutationOutput{}, safeStockLocationTypeError("stock-location-type creation")
				}
				return recoverStockLocationTypeCreate(ctx, client, input, name)
			}
			return verifyStockLocationTypeWrite(ctx, client, created.PK, stockLocationTypeCreateFields(input, name), false)
		})
}

func updateStockLocationType(deps Dependencies) mcp.ToolHandlerFor[UpdateStockLocationTypeInput, StockLocationTypeMutationOutput] {
	return LookupHandler[StockLocationTypeAdminClient, UpdateStockLocationTypeInput, StockLocationTypeMutationOutput](deps, UpdateStockLocationTypeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockLocationTypeAdminClient, input UpdateStockLocationTypeInput) (*mcp.CallToolResult, StockLocationTypeMutationOutput, error) {
			if input.ID <= 0 {
				return stockLocationTypeClarification("Which stock-location type should be updated?", "id", "id must be positive", "id", nil, map[string]any{"id": input.ID})
			}
			before, err := client.GetStockLocationType(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), StockLocationTypeMutationOutput{Status: StatusNotFound}, nil
			}
			if err != nil || before.PK != input.ID {
				return nil, StockLocationTypeMutationOutput{}, safeStockLocationTypeError("stock-location-type lookup")
			}
			fields, name, err := stockLocationTypeUpdateFields(input, before)
			if err != nil {
				return stockLocationTypeClarification("Which stock-location-type fields should change?", "patch", err.Error(), "id", []ClarificationCandidate{candidateFor(before)}, map[string]any{"id": input.ID})
			}
			if name != before.Name {
				matches, dupErr := stockLocationTypeDuplicates(ctx, client, name, input.ID)
				if dupErr != nil {
					return stockLocationTypeDuplicateFailure(dupErr, map[string]any{"id": input.ID})
				}
				if len(matches) > 0 {
					return stockLocationTypeCandidates("Which stock-location type should remain?", "the requested name collides with an existing type", matches, map[string]any{"id": input.ID})
				}
			}
			updated, err := client.UpdateStockLocationType(ctx, input.ID, fields)
			if err != nil {
				if !ambiguousCategoryMutation(err) {
					return nil, StockLocationTypeMutationOutput{}, safeStockLocationTypeError("stock-location-type update")
				}
				return verifyStockLocationTypeWrite(ctx, client, input.ID, fields, true)
			}
			if updated.PK != input.ID {
				return TextResult(StatusPartialFailure), StockLocationTypeMutationOutput{Status: StatusPartialFailure, Record: &before, RecoveryPlan: fmt.Sprintf("PATCH for type_id %d returned a mismatched identity; read the stable ID before retrying", input.ID)}, nil
			}
			return verifyStockLocationTypeWrite(ctx, client, input.ID, fields, false)
		})
}

func deleteStockLocationType(deps Dependencies) mcp.ToolHandlerFor[DeleteStockLocationTypeInput, StockLocationTypeDeleteOutput] {
	return LookupHandler[StockLocationTypeAdminClient, DeleteStockLocationTypeInput, StockLocationTypeDeleteOutput](deps, DeleteStockLocationTypeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockLocationTypeAdminClient, input DeleteStockLocationTypeInput) (*mcp.CallToolResult, StockLocationTypeDeleteOutput, error) {
			if input.ID <= 0 {
				clarification := NewClarification("Which stock-location type should be deleted?", "id", "id must be positive", "id", true, nil, map[string]any{"id": input.ID})
				return TextResult(StatusClarificationRequired), StockLocationTypeDeleteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			record, err := client.GetStockLocationType(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), StockLocationTypeDeleteOutput{Status: StatusNotFound}, nil
			}
			if err != nil || record.PK != input.ID {
				return nil, StockLocationTypeDeleteOutput{}, safeStockLocationTypeError("stock-location-type delete lookup")
			}
			referencingIDs, err := stockLocationTypeReferences(ctx, client, record.PK)
			if err != nil {
				if errors.Is(err, errStockLocationTypeReferenceScanLimit) {
					clarification := NewClarification("How should this completeness-sensitive reference scan be narrowed or reviewed?", "referencing_location_ids", err.Error(), "id", true, nil, map[string]any{"id": input.ID})
					return TextResult(StatusClarificationRequired), StockLocationTypeDeleteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
				}
				return nil, StockLocationTypeDeleteOutput{}, safeStockLocationTypeError("stock-location-type reference preflight")
			}
			plan := StockLocationTypeDeletePlan{Action: DeleteStockLocationTypeToolName, Type: record, ReferencingLocationIDs: referencingIDs}
			if !input.Confirm {
				return issueStockLocationTypeDeletePlan(ctx, deps.stockLocationTypeDeletePlanStore, plan)
			}
			if deps.stockLocationTypeDeletePlanStore == nil || input.PlanHash == "" || !deps.stockLocationTypeDeletePlanStore.consume(ctx, input.PlanHash, plan) {
				return stockLocationTypeDeleteStaleClarification(plan)
			}
			mutationErr := client.DeleteStockLocationType(ctx, record.PK)
			return verifyStockLocationTypeDeletion(ctx, client, record, referencingIDs, mutationErr)
		})
}

// verifyStockLocationTypeDeletion reads the type back by its exact stable ID
// after a delete attempt and classifies the result: verified absence
// (success, optionally recovered from an ambiguous mutation response),
// confirmed survival, or an unverifiable read-back.
func verifyStockLocationTypeDeletion(ctx context.Context, client StockLocationTypeAdminClient, record inventree.StockLocationType, referencingIDs []int, mutationErr error) (*mcp.CallToolResult, StockLocationTypeDeleteOutput, error) {
	if mutationErr != nil {
		if errors.Is(mutationErr, context.Canceled) {
			return nil, StockLocationTypeDeleteOutput{}, context.Canceled
		}
		if errors.Is(mutationErr, context.DeadlineExceeded) {
			return nil, StockLocationTypeDeleteOutput{}, context.DeadlineExceeded
		}
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, StockLocationTypeDeleteOutput{}, safeStockLocationTypeError("stock-location-type deletion")
		}
	}
	_, err := client.GetStockLocationType(ctx, record.PK)
	if isNotFound(err) {
		return TextResult(StatusOK), StockLocationTypeDeleteOutput{Status: StatusOK, Record: &record, ReferencingLocationIDs: referencingIDs, Verified: true, Recovered: mutationErr != nil}, nil
	}
	if errors.Is(err, context.Canceled) {
		return nil, StockLocationTypeDeleteOutput{}, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, StockLocationTypeDeleteOutput{}, context.DeadlineExceeded
	}
	if err == nil {
		return TextResult(StatusPartialFailure), StockLocationTypeDeleteOutput{Status: StatusPartialFailure, Record: &record, RecoveryPlan: "The type still exists after deletion; read the exact stable ID before retrying."}, nil
	}
	return TextResult(StatusPartialFailure), StockLocationTypeDeleteOutput{Status: StatusPartialFailure, Record: &record, RecoveryPlan: "Deletion read-back could not prove absence; read the exact stable ID before retrying."}, nil
}

func issueStockLocationTypeDeletePlan(ctx context.Context, store *stockLocationTypeDeletePlanStore, plan StockLocationTypeDeletePlan) (*mcp.CallToolResult, StockLocationTypeDeleteOutput, error) {
	if store == nil {
		return nil, StockLocationTypeDeleteOutput{}, errors.New("stock-location-type deletion confirmation store is unavailable")
	}
	token, err := store.issue(ctx, plan)
	if err != nil {
		return nil, StockLocationTypeDeleteOutput{}, err
	}
	reason := "review the exact current type, then provide confirm:true with this plan_hash"
	if len(plan.ReferencingLocationIDs) > 0 {
		reason = "this type is currently referenced by the reported locations, which will lose their type assignment on deletion; review them, then provide confirm:true with this plan_hash"
	}
	clarification := NewClarification("Delete this exact stock-location type?", "confirmation", reason, "plan_hash", false, []ClarificationCandidate{candidateFor(plan.Type)}, map[string]any{"id": plan.Type.PK, "confirm": true, "plan_hash": token})
	return TextResult(StatusClarificationRequired), StockLocationTypeDeleteOutput{Status: StatusClarificationRequired, Record: &plan.Type, ReferencingLocationIDs: plan.ReferencingLocationIDs, PlanHash: token, Clarification: &clarification}, nil
}

func stockLocationTypeDeleteStaleClarification(plan StockLocationTypeDeletePlan) (*mcp.CallToolResult, StockLocationTypeDeleteOutput, error) {
	clarification := NewClarification("Which current deletion plan should authorize this stock-location-type deletion?", "confirmation", "the plan is missing, stale, expired, reused, or belongs to another principal; preview the current type and its references again", "plan_hash", false, nil, map[string]any{"id": plan.Type.PK})
	return TextResult(StatusClarificationRequired), StockLocationTypeDeleteOutput{Status: StatusClarificationRequired, Record: &plan.Type, ReferencingLocationIDs: plan.ReferencingLocationIDs, Clarification: &clarification}, nil
}

// stockLocationTypeReferences scans every location referencing typeID,
// bounded and failing closed so an incomplete scan can never understate the
// impact of deleting the type.
func stockLocationTypeReferences(ctx context.Context, client StockLocationTypeAdminClient, typeID int) ([]int, error) {
	ids := make([]int, 0)
	for offset := 0; offset < stockLocationTypeReferenceScanLimit; offset += stockLocationTypeReferencePageSize {
		query := inventree.StockLocationQuery{LocationType: &typeID, Limit: stockLocationTypeReferencePageSize, Offset: offset}
		page, err := client.SearchStockLocationsPage(ctx, query)
		if err != nil {
			return nil, err
		}
		if page.Count > stockLocationTypeReferenceScanLimit {
			return nil, errStockLocationTypeReferenceScanLimit
		}
		for _, location := range page.Results {
			ids = append(ids, location.PK)
		}
		if !page.HasMore {
			slices.Sort(ids)
			return ids, nil
		}
	}
	return nil, errStockLocationTypeReferenceScanLimit
}

func stockLocationTypeDuplicates(ctx context.Context, client StockLocationTypeAdminClient, name string, exclude int) ([]inventree.StockLocationType, error) {
	matches := make([]inventree.StockLocationType, 0)
	for offset := 0; offset < stockLocationTypeScanLimit; offset += stockLocationTypePageSize {
		page, err := client.SearchStockLocationTypesPage(ctx, inventree.SearchQuery{Limit: stockLocationTypePageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		if page.Count > stockLocationTypeScanLimit {
			return nil, errStockLocationTypeScanLimit
		}
		for _, candidate := range page.Results {
			if candidate.PK != exclude && strings.EqualFold(strings.TrimSpace(candidate.Name), strings.TrimSpace(name)) {
				matches = append(matches, candidate)
			}
		}
		if !page.HasMore {
			slices.SortFunc(matches, func(a, b inventree.StockLocationType) int { return a.PK - b.PK })
			return matches, nil
		}
	}
	return nil, errStockLocationTypeScanLimit
}

func stockLocationTypeUpdateFields(input UpdateStockLocationTypeInput, before inventree.StockLocationType) (inventree.PatchFields, string, error) {
	fields := inventree.PatchFields{}
	name := before.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, "", errors.New("name must be nonblank when provided")
		}
		fields["name"] = inventree.Set(name)
	}
	if input.Description != nil {
		fields["description"] = inventree.Set(*input.Description)
	}
	if input.Icon != nil {
		fields["icon"] = inventree.Set(*input.Icon)
	}
	if len(fields) == 0 {
		return nil, "", errors.New("update_stock_location_type requires at least one field to change")
	}
	return fields, name, nil
}

func stockLocationTypeCreateFields(input CreateStockLocationTypeInput, name string) inventree.PatchFields {
	fields := inventree.PatchFields{"name": inventree.Set(name)}
	if input.Description != nil {
		fields["description"] = inventree.Set(*input.Description)
	}
	if input.Icon != nil {
		fields["icon"] = inventree.Set(*input.Icon)
	}
	return fields
}

func verifyStockLocationTypeWrite(ctx context.Context, client StockLocationTypeAdminClient, id int, fields inventree.PatchFields, recovered bool) (*mcp.CallToolResult, StockLocationTypeMutationOutput, error) {
	current, err := client.GetStockLocationType(ctx, id)
	if err != nil || current.PK != id {
		return TextResult(StatusPartialFailure), StockLocationTypeMutationOutput{Status: StatusPartialFailure, RecoveryPlan: fmt.Sprintf("Type %d may have changed; call get_stock_location_type before retrying", id)}, nil
	}
	if !stockLocationTypeFieldsMatch(current, fields) {
		return TextResult(StatusPartialFailure), StockLocationTypeMutationOutput{Status: StatusPartialFailure, Record: &current, RecoveryPlan: fmt.Sprintf("Type %d read-back does not match every requested field; inspect it before another write", id)}, nil
	}
	matches, dupErr := stockLocationTypeDuplicates(ctx, client, current.Name, current.PK)
	if dupErr != nil || len(matches) > 0 {
		return TextResult(StatusPartialFailure), StockLocationTypeMutationOutput{Status: StatusPartialFailure, Record: &current, RecoveryPlan: fmt.Sprintf("Type %d was written but duplicate-name uniqueness could not be verified; inspect other types before another write", id)}, nil
	}
	return TextResult(StatusOK), StockLocationTypeMutationOutput{Status: StatusOK, Record: &current, Recovered: recovered}, nil
}

func recoverStockLocationTypeCreate(ctx context.Context, client StockLocationTypeAdminClient, input CreateStockLocationTypeInput, name string) (*mcp.CallToolResult, StockLocationTypeMutationOutput, error) {
	fields := stockLocationTypeCreateFields(input, name)
	matches, err := stockLocationTypeDuplicates(ctx, client, name, 0)
	if err != nil || len(matches) != 1 || !stockLocationTypeFieldsMatch(matches[0], fields) {
		return TextResult(StatusPartialFailure), StockLocationTypeMutationOutput{Status: StatusPartialFailure, RecoveryPlan: "Creation result is unknown. Search for the exact normalized name and inspect every supplied field before retrying."}, nil
	}
	return verifyStockLocationTypeWrite(ctx, client, matches[0].PK, fields, true)
}

func stockLocationTypeFieldsMatch(t inventree.StockLocationType, fields inventree.PatchFields) bool {
	data, err := json.Marshal(fields)
	if err != nil {
		return false
	}
	var patch map[string]json.RawMessage
	if json.Unmarshal(data, &patch) != nil {
		return false
	}
	checks := map[string]any{"name": t.Name, "description": t.Description, "icon": t.Icon}
	for key, raw := range patch {
		actual, ok := checks[key]
		if !ok {
			return false
		}
		actualJSON, _ := json.Marshal(actual)
		if string(actualJSON) != string(raw) {
			return false
		}
	}
	return true
}

func stockLocationTypeCreateRetry(input CreateStockLocationTypeInput, name string) map[string]any {
	return map[string]any{"name": name, "description": input.Description, "icon": input.Icon}
}

func stockLocationTypeClarification(question, field, reason, retry string, candidates []ClarificationCandidate, retryValues map[string]any) (*mcp.CallToolResult, StockLocationTypeMutationOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, candidates, retryValues)
	return TextResult(StatusClarificationRequired), StockLocationTypeMutationOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func stockLocationTypeCandidates(question, reason string, matches []inventree.StockLocationType, values map[string]any) (*mcp.CallToolResult, StockLocationTypeMutationOutput, error) {
	clarification := NewClarification(question, "stock_location_type", reason, "location_type_id", false, candidatesFor(matches), values)
	return TextResult(StatusClarificationRequired), StockLocationTypeMutationOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func stockLocationTypeDuplicateFailure(err error, values map[string]any) (*mcp.CallToolResult, StockLocationTypeMutationOutput, error) {
	if errors.Is(err, errStockLocationTypeScanLimit) {
		return stockLocationTypeClarification("How should this duplicate-name scan be narrowed or reviewed?", "stock_location_type", err.Error(), "name", nil, values)
	}
	return nil, StockLocationTypeMutationOutput{}, safeStockLocationTypeError("stock-location-type duplicate preflight")
}

func safeStockLocationTypeError(operation string) error {
	return fmt.Errorf("%s failed; inspect InvenTree availability and permissions before retrying", operation)
}
