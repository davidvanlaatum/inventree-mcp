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
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	partRelationMaxRecords                 = 1000
	partRelationPageSize                   = 100
	partRelationMaxRequests                = 20
	partRelationPlanLifetime               = 5 * time.Minute
	partRelationPlanMaxEntries             = 4096
	partRelationPlanMaxEntriesPerPrincipal = 64
)

type PartRelationLookupClient interface {
	SearchPartRelationsPage(context.Context, inventree.PartRelationQuery) (inventree.PartRelationPage, error)
	GetPartRelation(context.Context, int) (inventree.PartRelation, error)
}

type PartRelationWriteClient interface {
	SearchPartRelationsPage(context.Context, inventree.PartRelationQuery) (inventree.PartRelationPage, error)
	GetPartRelation(context.Context, int) (inventree.PartRelation, error)
	CreatePartRelation(context.Context, inventree.PartRelationCreate) (inventree.PartRelation, error)
	UpdatePartRelation(context.Context, int, inventree.PatchFields) (inventree.PartRelation, error)
	DeletePartRelation(context.Context, int) error
}

type ListPartRelationsInput struct {
	PartID int `json:"part_id" jsonschema:"Stable part ID which must appear on either side of every returned relation."`
	Limit  int `json:"limit,omitempty" jsonschema:"Maximum records to return; defaults to 50 and cannot exceed 1000."`
	Offset int `json:"offset,omitempty" jsonschema:"Zero-based result offset."`
}

type PartRelationListOutput struct {
	Status        string                   `json:"status"`
	Count         int                      `json:"count"`
	Results       []inventree.PartRelation `json:"results"`
	Clarification *ClarificationResponse   `json:"clarification,omitempty"`
}

type CreatePartRelationInput struct {
	Part1ID int    `json:"part_1_id" jsonschema:"First stable part ID; endpoint order is preserved but relation identity is undirected."`
	Part2ID int    `json:"part_2_id" jsonschema:"Second stable part ID; endpoint order is preserved but relation identity is undirected."`
	Note    string `json:"note,omitempty" jsonschema:"Optional relation note, at most 500 characters."`
}

type UpdatePartRelationInput struct {
	ID        int     `json:"id" jsonschema:"Stable relation ID."`
	Note      *string `json:"note,omitempty" jsonschema:"Replacement relation note, at most 500 characters."`
	ClearNote bool    `json:"clear_note,omitempty" jsonschema:"Set the note to an empty string; mutually exclusive with note."`
	Confirm   bool    `json:"confirm,omitempty" jsonschema:"Confirm the exact current-state plan."`
	PlanHash  string  `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

type DeletePartRelationInput struct {
	ID       int    `json:"id" jsonschema:"Stable relation ID."`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Confirm deletion of the exact previewed relation."`
	PlanHash string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

type PartRelationPlan struct {
	Action string                  `json:"action"`
	Before inventree.PartRelation  `json:"before"`
	After  *inventree.PartRelation `json:"after,omitempty"`
}

type PartRelationMutationOutput struct {
	Status        string                   `json:"status"`
	Record        *inventree.PartRelation  `json:"record,omitempty"`
	Candidates    []inventree.PartRelation `json:"candidates,omitempty"`
	Plan          *PartRelationPlan        `json:"plan,omitempty"`
	PlanHash      string                   `json:"plan_hash,omitempty"`
	Verified      bool                     `json:"verified,omitempty"`
	Recovered     bool                     `json:"recovered,omitempty"`
	Validation    *ValidationFailure       `json:"validation,omitempty"`
	Clarification *ClarificationResponse   `json:"clarification,omitempty"`
	RecoveryPlan  string                   `json:"recovery_plan,omitempty"`
}

type partRelationConfirmation struct {
	digest     string
	principal  string
	relationID int
	expiresAt  time.Time
}

type partRelationPlanStore struct {
	mu                     sync.Mutex
	entries                map[string]partRelationConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newPartRelationPlanStore(now func() time.Time, token func() (string, error)) *partRelationPlanStore {
	return &partRelationPlanStore{entries: map[string]partRelationConfirmation{}, now: now, token: token, principal: stockPlanPrincipal, maxEntries: partRelationPlanMaxEntries, maxEntriesPerPrincipal: partRelationPlanMaxEntriesPerPrincipal}
}

func (s *partRelationPlanStore) issue(ctx context.Context, plan PartRelationPlan) (string, error) {
	digest, err := partRelationPlanDigest(plan)
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
		if !now.Before(entry.expiresAt) || entry.principal == principal && entry.relationID == plan.Before.PK {
			delete(s.entries, key)
			continue
		}
		if entry.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errors.New("too many outstanding related-part confirmation plans")
	}
	s.entries[token] = partRelationConfirmation{digest: digest, principal: principal, relationID: plan.Before.PK, expiresAt: now.Add(partRelationPlanLifetime)}
	return token, nil
}

func (s *partRelationPlanStore) consume(ctx context.Context, token string, plan PartRelationPlan) bool {
	digest, err := partRelationPlanDigest(plan)
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
	if !ok || entry.digest != digest || entry.principal != s.principal(ctx) || entry.relationID != plan.Before.PK || !now.Before(entry.expiresAt) {
		return false
	}
	delete(s.entries, token)
	return true
}

func partRelationPlanDigest(plan PartRelationPlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerPartRelationLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, ListPartRelationsToolName, "List part relations", "Lists bounded related-part links for one stable part ID.", listPartRelations(deps))
	addReadOnlyTool(server, deps, GetPartRelationToolName, "Get part relation", "Retrieves one related-part link by stable relation ID.", getPartRelation(deps))
}

func registerPartRelationWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.partRelationPlanStore == nil {
		deps.partRelationPlanStore = newPartRelationPlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, CreatePartRelationToolName, "Create part relation", "Creates one verified undirected related-part link.", createPartRelation(deps))
	addWriteTool(server, deps, UpdatePartRelationToolName, "Update part relation", "Previews and confirms a note-only related-part update.", updatePartRelation(deps))
	addWriteTool(server, deps, DeletePartRelationToolName, "Delete part relation", "Previews and confirms deletion of one exact related-part link.", deletePartRelation(deps))
}

func listPartRelations(deps Dependencies) mcp.ToolHandlerFor[ListPartRelationsInput, PartRelationListOutput] {
	return LookupHandler[PartRelationLookupClient, ListPartRelationsInput, PartRelationListOutput](deps, ListPartRelationsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartRelationLookupClient, input ListPartRelationsInput) (*mcp.CallToolResult, PartRelationListOutput, error) {
			if input.PartID <= 0 || input.Offset < 0 || input.Limit < 0 || input.Limit > partRelationMaxRecords {
				clarification := NewClarification("Which bounded related-part list should be returned?", "part_id", "part_id must be positive, offset non-negative, and limit at most 1000", "part_id", true, nil, map[string]any{})
				return TextResult(StatusClarificationRequired), PartRelationListOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			limit := input.Limit
			if limit == 0 {
				limit = 50
			}
			records, err := loadPartRelationsForPart(ctx, client, input.PartID)
			if err != nil {
				return nil, PartRelationListOutput{}, err
			}
			sort.Slice(records, func(i, j int) bool { return records[i].PK < records[j].PK })
			count := len(records)
			start := input.Offset
			if start > count {
				start = count
			}
			end := start + limit
			if end > count {
				end = count
			}
			return TextResult(StatusOK), PartRelationListOutput{Status: StatusOK, Count: count, Results: records[start:end]}, nil
		})
}

func loadPartRelationsForPart(ctx context.Context, client PartRelationLookupClient, partID int) ([]inventree.PartRelation, error) {
	result := []inventree.PartRelation{}
	for offset, requests := 0, 0; ; offset, requests = offset+partRelationPageSize, requests+1 {
		if requests >= partRelationMaxRequests || len(result) >= partRelationMaxRecords {
			return nil, errors.New("related-part list exceeded the bounded request or record budget")
		}
		page, err := client.SearchPartRelationsPage(ctx, inventree.PartRelationQuery{Part: partID, Limit: partRelationPageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		result = append(result, page.Results...)
		if len(result) > partRelationMaxRecords {
			return nil, errors.New("related-part list exceeded the bounded record budget")
		}
		if !page.HasMore {
			return result, nil
		}
	}
}

func getPartRelation(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.PartRelation]] {
	return LookupHandler[PartRelationLookupClient, IDInput, RecordOutput[inventree.PartRelation]](deps, GetPartRelationToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartRelationLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.PartRelation], error) {
			if input.ID <= 0 {
				return recordOutput(inventree.PartRelation{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound})
			}
			record, err := client.GetPartRelation(ctx, input.ID)
			if err == nil && record.PK != input.ID {
				return nil, RecordOutput[inventree.PartRelation]{}, errors.New("InvenTree returned a mismatched part-relation identity")
			}
			return recordOutput(record, err)
		})
}

func createPartRelation(deps Dependencies) mcp.ToolHandlerFor[CreatePartRelationInput, PartRelationMutationOutput] {
	return LookupHandler[PartRelationWriteClient, CreatePartRelationInput, PartRelationMutationOutput](deps, CreatePartRelationToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartRelationWriteClient, input CreatePartRelationInput) (*mcp.CallToolResult, PartRelationMutationOutput, error) {
			if err := validatePartRelationPair(input.Part1ID, input.Part2ID, input.Note); err != nil {
				return partRelationValidation(err.Error())
			}
			matches, err := findPartRelationPair(ctx, client, input.Part1ID, input.Part2ID)
			if err != nil {
				return nil, PartRelationMutationOutput{}, err
			}
			if len(matches) > 0 {
				return partRelationDuplicate(matches)
			}
			created, mutationErr := client.CreatePartRelation(ctx, inventree.PartRelationCreate{Part1: input.Part1ID, Part2: input.Part2ID, Note: input.Note})
			if errors.Is(mutationErr, context.Canceled) || errors.Is(mutationErr, context.DeadlineExceeded) {
				return nil, PartRelationMutationOutput{}, mutationErr
			}
			if mutationErr != nil {
				var apiErr *inventree.APIError
				if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
					if validation, ok := safeValidationFailure(mutationErr); ok {
						return TextResult(StatusValidationFailed), PartRelationMutationOutput{Status: StatusValidationFailed, Validation: validation}, nil
					}
					return nil, PartRelationMutationOutput{}, fmt.Errorf("part-relation creation was rejected")
				}
				return recoverPartRelationCreate(ctx, client, input)
			}
			if created.PK <= 0 {
				return TextResult(StatusPartialFailure), PartRelationMutationOutput{Status: StatusPartialFailure, RecoveryPlan: "Search both endpoint directions and reconcile the canonical unordered part pair before retrying."}, nil
			}
			current, err := client.GetPartRelation(ctx, created.PK)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, PartRelationMutationOutput{}, err
			}
			if err == nil && current.PK == created.PK && relationMatches(current, input.Part1ID, input.Part2ID, input.Note) {
				return TextResult(StatusOK), PartRelationMutationOutput{Status: StatusOK, Record: &current, Verified: true}, nil
			}
			return TextResult(StatusPartialFailure), PartRelationMutationOutput{Status: StatusPartialFailure, RecoveryPlan: fmt.Sprintf("Read relation_id %d and reconcile its endpoints and note before retrying.", created.PK)}, nil
		})
}

func updatePartRelation(deps Dependencies) mcp.ToolHandlerFor[UpdatePartRelationInput, PartRelationMutationOutput] {
	return LookupHandler[PartRelationWriteClient, UpdatePartRelationInput, PartRelationMutationOutput](deps, UpdatePartRelationToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartRelationWriteClient, input UpdatePartRelationInput) (*mcp.CallToolResult, PartRelationMutationOutput, error) {
			if input.ID <= 0 || input.Note != nil && input.ClearNote || input.Note == nil && !input.ClearNote {
				return partRelationValidation("provide one positive id and exactly one of note or clear_note")
			}
			note := ""
			if input.Note != nil {
				note = *input.Note
			}
			if len([]rune(note)) > 500 {
				return partRelationValidation("note must not exceed 500 characters")
			}
			before, err := client.GetPartRelation(ctx, input.ID)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, PartRelationMutationOutput{}, err
			}
			if isNotFound(err) {
				return TextResult(StatusNotFound), PartRelationMutationOutput{Status: StatusNotFound}, nil
			}
			if err != nil || before.PK != input.ID {
				return nil, PartRelationMutationOutput{}, errors.New("part-relation lookup failed")
			}
			if before.Note == note {
				return partRelationValidation("requested note already matches current state")
			}
			after := before
			after.Note = note
			plan := PartRelationPlan{Action: "update_note", Before: before, After: &after}
			if !input.Confirm {
				return issuePartRelationPlan(ctx, deps.partRelationPlanStore, plan, "Apply this exact note-only related-part update?", map[string]any{"id": input.ID, "note": input.Note, "clear_note": input.ClearNote, "confirm": true})
			}
			if deps.partRelationPlanStore == nil || input.PlanHash == "" || !deps.partRelationPlanStore.consume(ctx, input.PlanHash, plan) {
				return partRelationPlanClarification(plan, "The plan is missing, stale, expired, reused, or belongs to another principal; preview the current relation again.")
			}
			_, mutationErr := client.UpdatePartRelation(ctx, input.ID, inventree.PatchFields{"note": inventree.Set(note)})
			return verifyPartRelationUpdate(ctx, client, plan, mutationErr)
		})
}

func deletePartRelation(deps Dependencies) mcp.ToolHandlerFor[DeletePartRelationInput, PartRelationMutationOutput] {
	return LookupHandler[PartRelationWriteClient, DeletePartRelationInput, PartRelationMutationOutput](deps, DeletePartRelationToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartRelationWriteClient, input DeletePartRelationInput) (*mcp.CallToolResult, PartRelationMutationOutput, error) {
			if input.ID <= 0 {
				return partRelationValidation("id must be a positive stable relation ID")
			}
			before, err := client.GetPartRelation(ctx, input.ID)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, PartRelationMutationOutput{}, err
			}
			if isNotFound(err) {
				return TextResult(StatusNotFound), PartRelationMutationOutput{Status: StatusNotFound}, nil
			}
			if err != nil || before.PK != input.ID {
				return nil, PartRelationMutationOutput{}, errors.New("part-relation lookup failed")
			}
			plan := PartRelationPlan{Action: "delete", Before: before}
			if !input.Confirm {
				return issuePartRelationPlan(ctx, deps.partRelationPlanStore, plan, "Delete this exact related-part link?", map[string]any{"id": input.ID, "confirm": true})
			}
			if deps.partRelationPlanStore == nil || input.PlanHash == "" || !deps.partRelationPlanStore.consume(ctx, input.PlanHash, plan) {
				return partRelationPlanClarification(plan, "The plan is missing, stale, expired, reused, or belongs to another principal; preview the current relation again.")
			}
			mutationErr := client.DeletePartRelation(ctx, input.ID)
			if errors.Is(mutationErr, context.Canceled) || errors.Is(mutationErr, context.DeadlineExceeded) {
				return nil, PartRelationMutationOutput{}, mutationErr
			}
			if mutationErr != nil {
				var apiErr *inventree.APIError
				if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
					return nil, PartRelationMutationOutput{}, errors.New("part-relation deletion was rejected")
				}
			}
			current, readErr := client.GetPartRelation(ctx, input.ID)
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				return nil, PartRelationMutationOutput{}, readErr
			}
			if isNotFound(readErr) {
				return TextResult(StatusOK), PartRelationMutationOutput{Status: StatusOK, Record: &before, Verified: true, Recovered: mutationErr != nil}, nil
			}
			out := PartRelationMutationOutput{Status: StatusPartialFailure, RecoveryPlan: "Read the exact relation ID before preparing another deletion plan."}
			if readErr == nil && current.PK == input.ID {
				out.Record = &current
			}
			return TextResult(StatusPartialFailure), out, nil
		})
}

func validatePartRelationPair(part1, part2 int, note string) error {
	if part1 <= 0 || part2 <= 0 {
		return errors.New("both part IDs must be positive")
	}
	if part1 == part2 {
		return errors.New("a part cannot be related to itself")
	}
	if len([]rune(note)) > 500 {
		return errors.New("note must not exceed 500 characters")
	}
	return nil
}

func findPartRelationPair(ctx context.Context, client PartRelationWriteClient, part1, part2 int) ([]inventree.PartRelation, error) {
	queries := []inventree.PartRelationQuery{{Part1: part1, Part2: part2}, {Part1: part2, Part2: part1}}
	seen := map[int]inventree.PartRelation{}
	records, requests := 0, 0
	for _, base := range queries {
		for offset := 0; ; offset += partRelationPageSize {
			if requests >= partRelationMaxRequests || records >= partRelationMaxRecords {
				return nil, errors.New("related-part duplicate preflight exceeded the shared bounded request or record budget")
			}
			base.Limit, base.Offset = partRelationPageSize, offset
			page, err := client.SearchPartRelationsPage(ctx, base)
			if err != nil {
				return nil, err
			}
			requests++
			records += len(page.Results)
			if records > partRelationMaxRecords {
				return nil, errors.New("related-part duplicate preflight exceeded the shared bounded record budget")
			}
			for _, relation := range page.Results {
				if samePartPair(relation.Part1, relation.Part2, part1, part2) {
					seen[relation.PK] = relation
				}
			}
			if !page.HasMore {
				break
			}
		}
	}
	result := make([]inventree.PartRelation, 0, len(seen))
	for _, relation := range seen {
		result = append(result, relation)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PK < result[j].PK })
	return result, nil
}

func samePartPair(a1, a2, b1, b2 int) bool { return a1 == b1 && a2 == b2 || a1 == b2 && a2 == b1 }
func relationMatches(record inventree.PartRelation, part1, part2 int, note string) bool {
	return record.PK > 0 && samePartPair(record.Part1, record.Part2, part1, part2) && record.Note == note
}

func recoverPartRelationCreate(ctx context.Context, client PartRelationWriteClient, input CreatePartRelationInput) (*mcp.CallToolResult, PartRelationMutationOutput, error) {
	matches, err := findPartRelationPair(ctx, client, input.Part1ID, input.Part2ID)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, PartRelationMutationOutput{}, err
	}
	if err == nil && len(matches) == 1 && matches[0].Note == input.Note {
		return TextResult(StatusOK), PartRelationMutationOutput{Status: StatusOK, Record: &matches[0], Verified: true, Recovered: true}, nil
	}
	return TextResult(StatusPartialFailure), PartRelationMutationOutput{Status: StatusPartialFailure, Candidates: matches, RecoveryPlan: "Inspect both directions of the canonical unordered endpoint pair and reconcile notes before retrying creation."}, nil
}

func verifyPartRelationUpdate(ctx context.Context, client PartRelationWriteClient, plan PartRelationPlan, mutationErr error) (*mcp.CallToolResult, PartRelationMutationOutput, error) {
	if errors.Is(mutationErr, context.Canceled) || errors.Is(mutationErr, context.DeadlineExceeded) {
		return nil, PartRelationMutationOutput{}, mutationErr
	}
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			if validation, ok := safeValidationFailure(mutationErr); ok {
				return TextResult(StatusValidationFailed), PartRelationMutationOutput{Status: StatusValidationFailed, Validation: validation}, nil
			}
			return nil, PartRelationMutationOutput{}, errors.New("part-relation update was rejected")
		}
	}
	current, readErr := client.GetPartRelation(ctx, plan.Before.PK)
	if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
		return nil, PartRelationMutationOutput{}, readErr
	}
	if readErr == nil && plan.After != nil && current == *plan.After {
		return TextResult(StatusOK), PartRelationMutationOutput{Status: StatusOK, Record: &current, Verified: true, Recovered: mutationErr != nil}, nil
	}
	out := PartRelationMutationOutput{Status: StatusPartialFailure, RecoveryPlan: "Read the exact relation ID and compare endpoints and note before preparing another update plan."}
	if readErr == nil && current.PK == plan.Before.PK {
		out.Record = &current
	}
	return TextResult(StatusPartialFailure), out, nil
}

func issuePartRelationPlan(ctx context.Context, store *partRelationPlanStore, plan PartRelationPlan, question string, retry map[string]any) (*mcp.CallToolResult, PartRelationMutationOutput, error) {
	if store == nil {
		return nil, PartRelationMutationOutput{}, errors.New("part-relation confirmation store is unavailable")
	}
	token, err := store.issue(ctx, plan)
	if err != nil {
		return nil, PartRelationMutationOutput{}, err
	}
	retry["plan_hash"] = token
	clarification := NewClarification(question, "confirmation", "review the exact current endpoints and note, then provide confirm:true with this plan_hash", "plan_hash", false, nil, retry)
	return TextResult(StatusClarificationRequired), PartRelationMutationOutput{Status: StatusClarificationRequired, Plan: &plan, PlanHash: token, Clarification: &clarification}, nil
}

func partRelationPlanClarification(plan PartRelationPlan, reason string) (*mcp.CallToolResult, PartRelationMutationOutput, error) {
	clarification := NewClarification("Which current related-part plan should authorize this change?", "confirmation", reason, "plan_hash", false, nil, map[string]any{"id": plan.Before.PK})
	return TextResult(StatusClarificationRequired), PartRelationMutationOutput{Status: StatusClarificationRequired, Plan: &plan, Clarification: &clarification}, nil
}

func partRelationDuplicate(matches []inventree.PartRelation) (*mcp.CallToolResult, PartRelationMutationOutput, error) {
	clarification := NewClarification("Should the existing undirected related-part link be used?", "duplicate", "InvenTree permits only one relation for an unordered part pair, including reversed endpoint order", "relation_id", false, nil, map[string]any{})
	return TextResult(StatusClarificationRequired), PartRelationMutationOutput{Status: StatusClarificationRequired, Candidates: matches, Clarification: &clarification}, nil
}

func partRelationValidation(message string) (*mcp.CallToolResult, PartRelationMutationOutput, error) {
	return TextResult(StatusValidationFailed), PartRelationMutationOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "relation", Messages: []string{message}}}}}, nil
}
