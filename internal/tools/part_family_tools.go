package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	partFamilyPlanLifetime               = 5 * time.Minute
	partFamilyPlanMaxEntries             = 4096
	partFamilyPlanMaxEntriesPerPrincipal = 64
	partFamilyTopologyMaxRecords         = 64
)

var errPartFamilyPlanCapacity = errors.New("too many outstanding part-family confirmation plans")

type PartFamilyClient interface {
	GetPartDetail(context.Context, int) (inventree.PartDetail, error)
	UpdatePart(context.Context, int, inventree.PatchFields) (inventree.Part, error)
}

type UpdatePartFamilyRelationshipsInput struct {
	ID              int    `json:"id" jsonschema:"Stable InvenTree part primary key."`
	RevisionOfID    *int   `json:"revision_of_id,omitempty" jsonschema:"Stable target part primary key for the revision relationship."`
	ClearRevisionOf bool   `json:"clear_revision_of,omitempty" jsonschema:"Explicitly clear the revision relationship; mutually exclusive with revision_of_id."`
	VariantOfID     *int   `json:"variant_of_id,omitempty" jsonschema:"Stable target part primary key for the variant relationship."`
	ClearVariantOf  bool   `json:"clear_variant_of,omitempty" jsonschema:"Explicitly clear the variant relationship; mutually exclusive with variant_of_id."`
	DryRun          bool   `json:"dry_run,omitempty" jsonschema:"Return a current-state-bound plan without writing."`
	Confirm         bool   `json:"confirm,omitempty" jsonschema:"Explicitly confirm the matching dry-run plan."`
	PlanHash        string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token returned by dry_run:true."`
}

type PartFamilyState struct {
	PartID        int  `json:"part_id"`
	RevisionOf    *int `json:"revision_of"`
	RevisionCount *int `json:"revision_count"`
	VariantOf     *int `json:"variant_of"`
}

type PartFamilyTopologyNode struct {
	PartID          int  `json:"part_id"`
	RevisionOf      *int `json:"revision_of"`
	VariantOf       *int `json:"variant_of"`
	IsTemplate      bool `json:"is_template"`
	HasRevisionCode bool `json:"has_revision_code"`
}

type PartFamilyPlan struct {
	Before           PartFamilyState          `json:"before"`
	After            PartFamilyState          `json:"after"`
	TopologyEvidence []PartFamilyTopologyNode `json:"topology_evidence"`
}

type PartFamilyMutationOutput struct {
	Status        string                 `json:"status"`
	DryRun        bool                   `json:"dry_run"`
	PlanHash      string                 `json:"plan_hash,omitempty"`
	Plan          *PartFamilyPlan        `json:"plan,omitempty"`
	Record        *PartFamilyState       `json:"record,omitempty"`
	Recovery      *PartFamilyRecovery    `json:"recovery,omitempty"`
	Recovered     bool                   `json:"recovered,omitempty"`
	Validation    *ValidationFailure     `json:"validation,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
	RecoveryPlan  string                 `json:"recovery_plan,omitempty"`
}

type PartFamilyRecovery struct {
	Before PartFamilyRecoveryState `json:"before"`
	After  PartFamilyRecoveryState `json:"after"`
}

type PartFamilyRecoveryState struct {
	PartID     int  `json:"part_id"`
	RevisionOf *int `json:"revision_of"`
	VariantOf  *int `json:"variant_of"`
}

type partFamilyPlanConfirmation struct {
	digest    string
	principal string
	partID    int
	expiresAt time.Time
}

type partFamilyPlanStore struct {
	mu                     sync.Mutex
	entries                map[string]partFamilyPlanConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newPartFamilyPlanStore(now func() time.Time, token func() (string, error)) *partFamilyPlanStore {
	return &partFamilyPlanStore{
		entries:                map[string]partFamilyPlanConfirmation{},
		now:                    now,
		token:                  token,
		principal:              stockPlanPrincipal,
		maxEntries:             partFamilyPlanMaxEntries,
		maxEntriesPerPrincipal: partFamilyPlanMaxEntriesPerPrincipal,
	}
}

func (s *partFamilyPlanStore) issue(ctx context.Context, plan PartFamilyPlan) (string, error) {
	digest, err := partFamilyPlanDigest(plan)
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
		if confirmation.principal == principal && confirmation.partID == plan.Before.PartID {
			delete(s.entries, existingToken)
			continue
		}
		if confirmation.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errPartFamilyPlanCapacity
	}
	s.entries[token] = partFamilyPlanConfirmation{digest: digest, principal: principal, partID: plan.Before.PartID, expiresAt: now.Add(partFamilyPlanLifetime)}
	return token, nil
}

func (s *partFamilyPlanStore) consume(ctx context.Context, token string, plan PartFamilyPlan) bool {
	digest, err := partFamilyPlanDigest(plan)
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
	valid := confirmation.digest == digest && confirmation.principal == s.principal(ctx) && confirmation.partID == plan.Before.PartID && now.Before(confirmation.expiresAt)
	if valid {
		delete(s.entries, token)
	}
	return valid
}

func (s *partFamilyPlanStore) deleteExpired(now time.Time) {
	for token, confirmation := range s.entries {
		if !now.Before(confirmation.expiresAt) {
			delete(s.entries, token)
		}
	}
}

func partFamilyPlanDigest(plan PartFamilyPlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerPartFamilyTools(server *mcp.Server, deps Dependencies) {
	if deps.partFamilyPlanStore == nil {
		deps.partFamilyPlanStore = newPartFamilyPlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, UpdatePartFamilyRelationshipsToolName, "Update part family relationships", "Plans or confirms guarded revision and variant relationship assignments, replacements, or clears.", updatePartFamilyRelationships(deps))
}

func updatePartFamilyRelationships(deps Dependencies) mcp.ToolHandlerFor[UpdatePartFamilyRelationshipsInput, PartFamilyMutationOutput] {
	return LookupHandler[PartFamilyClient, UpdatePartFamilyRelationshipsInput, PartFamilyMutationOutput](deps, UpdatePartFamilyRelationshipsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartFamilyClient, input UpdatePartFamilyRelationshipsInput) (*mcp.CallToolResult, PartFamilyMutationOutput, error) {
			if input.ID <= 0 {
				return partFamilyClarification("Which part family relationship should be updated?", "id", "id must be a positive stable part ID", "id", map[string]any{})
			}
			if (input.RevisionOfID != nil && input.ClearRevisionOf) || (input.VariantOfID != nil && input.ClearVariantOf) {
				return partFamilyValidation("relationship target and clear flag are mutually exclusive")
			}
			if input.RevisionOfID == nil && !input.ClearRevisionOf && input.VariantOfID == nil && !input.ClearVariantOf {
				return partFamilyClarification("Which family relationship should change?", "relationship", "provide revision_of_id, clear_revision_of, variant_of_id, or clear_variant_of", "relationship", map[string]any{"id": input.ID})
			}
			if input.RevisionOfID != nil && *input.RevisionOfID <= 0 || input.VariantOfID != nil && *input.VariantOfID <= 0 {
				return partFamilyValidation("relationship target IDs must be positive")
			}
			plan, fields, err := buildPartFamilyPlan(ctx, client, input)
			if err != nil {
				var topologyErr *partFamilyTopologyError
				if errors.As(err, &topologyErr) {
					return partFamilyClarification("Which safe part-family relationship should be applied?", topologyErr.field, topologyErr.Error(), topologyErr.field, partFamilyRetry(input))
				}
				var apiErr *inventree.APIError
				if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
					return TextResult(StatusNotFound), PartFamilyMutationOutput{Status: StatusNotFound}, nil
				}
				return nil, PartFamilyMutationOutput{}, err
			}
			if input.DryRun {
				if deps.partFamilyPlanStore == nil {
					return nil, PartFamilyMutationOutput{}, errors.New("part-family confirmation store is unavailable")
				}
				token, err := deps.partFamilyPlanStore.issue(ctx, plan)
				if errors.Is(err, errPartFamilyPlanCapacity) {
					return partFamilyClarification("When should a new part-family plan be prepared?", "confirmation", "too many confirmation plans are outstanding; execute a reviewed plan or wait for expiry", "dry_run", partFamilyRetry(input))
				}
				if err != nil {
					return nil, PartFamilyMutationOutput{}, err
				}
				return TextResult(StatusOK), PartFamilyMutationOutput{Status: StatusOK, DryRun: true, PlanHash: token, Plan: &plan}, nil
			}
			if !input.Confirm {
				return partFamilyClarification("Apply this reviewed part-family change?", "confirmation", "confirm must be true after reviewing dry_run:true output", "confirm", partFamilyRetry(input))
			}
			if deps.partFamilyPlanStore == nil || input.PlanHash == "" || !deps.partFamilyPlanStore.consume(ctx, input.PlanHash, plan) {
				return partFamilyClarification("Which current dry-run plan should authorize this change?", "confirmation", "plan_hash must be the unexpired single-use token from a matching dry run by the same principal; changed topology requires a new dry run", "plan_hash", partFamilyRetry(input))
			}
			_, mutationErr := client.UpdatePart(ctx, input.ID, fields)
			if errors.Is(mutationErr, context.Canceled) {
				return nil, PartFamilyMutationOutput{}, context.Canceled
			}
			if errors.Is(mutationErr, context.DeadlineExceeded) {
				return nil, PartFamilyMutationOutput{}, context.DeadlineExceeded
			}
			var apiErr *inventree.APIError
			if mutationErr != nil && errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
				if validation, ok := safeValidationFailure(mutationErr); ok {
					return TextResult(StatusValidationFailed), PartFamilyMutationOutput{Status: StatusValidationFailed, Validation: validation}, nil
				}
				return nil, PartFamilyMutationOutput{}, mutationErr
			}
			current, readErr := client.GetPartDetail(ctx, input.ID)
			if errors.Is(readErr, context.Canceled) {
				return nil, PartFamilyMutationOutput{}, context.Canceled
			}
			if errors.Is(readErr, context.DeadlineExceeded) {
				return nil, PartFamilyMutationOutput{}, context.DeadlineExceeded
			}
			if readErr == nil && current.PK == input.ID && partFamilyMatches(current, plan.After) {
				state := partFamilyState(current)
				return TextResult(StatusOK), PartFamilyMutationOutput{Status: StatusOK, Record: &state, Recovered: mutationErr != nil}, nil
			}
			recovery := PartFamilyRecovery{Before: partFamilyRecoveryState(plan.Before), After: partFamilyRecoveryState(plan.After)}
			out := PartFamilyMutationOutput{Status: StatusPartialFailure, Recovery: &recovery, RecoveryPlan: "Call get_part for this stable ID and compare revision_of and variant_of with the reviewed target before preparing a new plan. Do not blindly retry the PATCH."}
			if readErr == nil && current.PK == input.ID {
				state := partFamilyState(current)
				out.Record = &state
			}
			return TextResult(StatusPartialFailure), out, nil
		})
}

type partFamilyTopologyError struct {
	field   string
	message string
}

func (e *partFamilyTopologyError) Error() string { return e.message }

type partFamilyTraversal struct {
	client    PartFamilyClient
	cache     map[int]inventree.PartDetail
	remaining int
}

func buildPartFamilyPlan(ctx context.Context, client PartFamilyClient, input UpdatePartFamilyRelationshipsInput) (PartFamilyPlan, inventree.PatchFields, error) {
	traversal := &partFamilyTraversal{client: client, cache: map[int]inventree.PartDetail{}, remaining: partFamilyTopologyMaxRecords}
	before, err := traversal.get(ctx, input.ID)
	if err != nil {
		return PartFamilyPlan{}, nil, err
	}
	if before.PK != input.ID {
		return PartFamilyPlan{}, nil, fmt.Errorf("part detail identity mismatch: requested %d, received %d", input.ID, before.PK)
	}
	after := partFamilyState(before)
	fields := inventree.PatchFields{}
	if input.RevisionOfID != nil {
		after.RevisionOf = cloneInt(input.RevisionOfID)
		fields["revision_of"] = inventree.Set(*input.RevisionOfID)
	} else if input.ClearRevisionOf {
		after.RevisionOf = nil
		fields["revision_of"] = inventree.Null()
	}
	if input.VariantOfID != nil {
		after.VariantOf = cloneInt(input.VariantOfID)
		fields["variant_of"] = inventree.Set(*input.VariantOfID)
	} else if input.ClearVariantOf {
		after.VariantOf = nil
		fields["variant_of"] = inventree.Null()
	}
	if equalInt(before.RevisionOf, after.RevisionOf) && equalInt(before.VariantOf, after.VariantOf) {
		return PartFamilyPlan{}, nil, &partFamilyTopologyError{field: "relationship", message: "requested family relationships already match current state"}
	}
	if after.RevisionOf != nil {
		if err := traversal.validateChain(ctx, input.ID, *after.RevisionOf, "revision_of"); err != nil {
			return PartFamilyPlan{}, nil, err
		}
		target := traversal.cache[*after.RevisionOf]
		if strings.TrimSpace(pointerString(before.Revision)) == "" {
			return PartFamilyPlan{}, nil, &partFamilyTopologyError{field: "revision", message: "a part assigned to revision_of must have a nonblank revision code"}
		}
		if target.IsTemplate {
			return PartFamilyPlan{}, nil, &partFamilyTopologyError{field: "revision_of_id", message: "a template part cannot be the target of revision_of"}
		}
		if !equalInt(after.VariantOf, target.VariantOf) {
			return PartFamilyPlan{}, nil, &partFamilyTopologyError{field: "revision_of_id", message: "the part and revision target must point to the same variant template"}
		}
	}
	if after.VariantOf != nil {
		if err := traversal.validateChain(ctx, input.ID, *after.VariantOf, "variant_of"); err != nil {
			return PartFamilyPlan{}, nil, err
		}
		target := traversal.cache[*after.VariantOf]
		if !target.IsTemplate {
			return PartFamilyPlan{}, nil, &partFamilyTopologyError{field: "variant_of_id", message: "variant_of must target a template part"}
		}
	}
	evidence := make([]PartFamilyTopologyNode, 0, len(traversal.cache))
	for _, record := range traversal.cache {
		evidence = append(evidence, PartFamilyTopologyNode{PartID: record.PK, RevisionOf: cloneInt(record.RevisionOf), VariantOf: cloneInt(record.VariantOf), IsTemplate: record.IsTemplate, HasRevisionCode: strings.TrimSpace(pointerString(record.Revision)) != ""})
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].PartID < evidence[j].PartID })
	return PartFamilyPlan{Before: partFamilyState(before), After: after, TopologyEvidence: evidence}, fields, nil
}

func (t *partFamilyTraversal) get(ctx context.Context, id int) (inventree.PartDetail, error) {
	if record, ok := t.cache[id]; ok {
		return record, nil
	}
	if t.remaining <= 0 {
		return inventree.PartDetail{}, &partFamilyTopologyError{field: "topology", message: fmt.Sprintf("part-family traversal exceeded the shared %d-record budget; the complete topology cannot be proven", partFamilyTopologyMaxRecords)}
	}
	t.remaining--
	record, err := t.client.GetPartDetail(ctx, id)
	if err != nil {
		return inventree.PartDetail{}, err
	}
	if record.PK != id {
		return inventree.PartDetail{}, fmt.Errorf("part detail identity mismatch: requested %d, received %d", id, record.PK)
	}
	t.cache[id] = record
	return record, nil
}

func (t *partFamilyTraversal) validateChain(ctx context.Context, subjectID, targetID int, field string) error {
	if subjectID == targetID {
		return &partFamilyTopologyError{field: field + "_id", message: field + " cannot reference the same part"}
	}
	seen := map[int]bool{}
	currentID := targetID
	for currentID > 0 {
		if currentID == subjectID {
			return &partFamilyTopologyError{field: field + "_id", message: field + " would create a cycle"}
		}
		if seen[currentID] {
			return &partFamilyTopologyError{field: field + "_id", message: "the existing " + field + " topology contains a cycle and cannot be proven safe"}
		}
		seen[currentID] = true
		record, err := t.get(ctx, currentID)
		if err != nil {
			var apiErr *inventree.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return &partFamilyTopologyError{field: field + "_id", message: fmt.Sprintf("%s target or ancestor part %d does not exist; the complete topology cannot be proven", field, currentID)}
			}
			return err
		}
		var next *int
		if field == "revision_of" {
			next = record.RevisionOf
		} else {
			next = record.VariantOf
		}
		if next == nil {
			return nil
		}
		if *next <= 0 {
			return &partFamilyTopologyError{field: field + "_id", message: fmt.Sprintf("the existing %s topology contains invalid part ID %d and cannot be proven safe", field, *next)}
		}
		currentID = *next
	}
	return nil
}

func partFamilyState(record inventree.PartDetail) PartFamilyState {
	return PartFamilyState{PartID: record.PK, RevisionOf: cloneInt(record.RevisionOf), RevisionCount: cloneInt(record.RevisionCount), VariantOf: cloneInt(record.VariantOf)}
}

func partFamilyRecoveryState(state PartFamilyState) PartFamilyRecoveryState {
	return PartFamilyRecoveryState{PartID: state.PartID, RevisionOf: cloneInt(state.RevisionOf), VariantOf: cloneInt(state.VariantOf)}
}

func partFamilyMatches(record inventree.PartDetail, expected PartFamilyState) bool {
	return record.PK == expected.PartID && equalInt(record.RevisionOf, expected.RevisionOf) && equalInt(record.VariantOf, expected.VariantOf)
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func equalInt(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func partFamilyRetry(input UpdatePartFamilyRelationshipsInput) map[string]any {
	values := map[string]any{"id": input.ID, "dry_run": true}
	if input.RevisionOfID != nil {
		values["revision_of_id"] = *input.RevisionOfID
	}
	if input.ClearRevisionOf {
		values["clear_revision_of"] = true
	}
	if input.VariantOfID != nil {
		values["variant_of_id"] = *input.VariantOfID
	}
	if input.ClearVariantOf {
		values["clear_variant_of"] = true
	}
	return values
}

func partFamilyClarification(question, field, reason, expected string, retry map[string]any) (*mcp.CallToolResult, PartFamilyMutationOutput, error) {
	clarification := NewClarification(question, field, reason, expected, true, nil, retry)
	return TextResult(StatusClarificationRequired), PartFamilyMutationOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func partFamilyValidation(message string) (*mcp.CallToolResult, PartFamilyMutationOutput, error) {
	validation := &ValidationFailure{StatusCode: http.StatusBadRequest, Fields: []ValidationFieldError{{Field: "relationship", Messages: []string{message}}}}
	return TextResult(StatusValidationFailed), PartFamilyMutationOutput{Status: StatusValidationFailed, Validation: validation}, nil
}
