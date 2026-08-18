package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	OwnerObjectPart          = "part"
	OwnerObjectPurchaseOrder = "purchase_order"
	OwnerObjectStockItem     = "stock_item"
	OwnerObjectStockLocation = "stock_location"

	ownerPlanLifetime               = 5 * time.Minute
	ownerPlanMaxEntries             = 4096
	ownerPlanMaxEntriesPerPrincipal = 64
)

// ownerObjectDescriptor centralizes everything owner_tools.go needs to know
// about one object type: its owner/responsible field name, how to read the
// current value, and how to patch it. Adding an object type means adding one
// entry here, rather than keeping several parallel switches/maps in sync.
type ownerObjectDescriptor struct {
	field   string
	current func(ctx context.Context, client OwnerAssignmentClient, objectID int) (*int, error)
	patch   func(ctx context.Context, client OwnerAssignmentClient, objectID int, fields inventree.PatchFields) error
}

var ownerObjectDescriptors = map[string]ownerObjectDescriptor{
	OwnerObjectPart: {
		field: "responsible",
		current: func(ctx context.Context, client OwnerAssignmentClient, objectID int) (*int, error) {
			record, err := client.GetPartDetail(ctx, objectID)
			if err != nil {
				return nil, err
			}
			if record.PK != objectID {
				return nil, fmt.Errorf("part %d returned a mismatched identity", objectID)
			}
			return record.Responsible, nil
		},
		patch: func(ctx context.Context, client OwnerAssignmentClient, objectID int, fields inventree.PatchFields) error {
			_, err := client.UpdatePart(ctx, objectID, fields)
			return err
		},
	},
	OwnerObjectPurchaseOrder: {
		field: "responsible",
		current: func(ctx context.Context, client OwnerAssignmentClient, objectID int) (*int, error) {
			record, err := client.GetPurchaseOrderDetail(ctx, objectID)
			if err != nil {
				return nil, err
			}
			if record.PK != objectID {
				return nil, fmt.Errorf("purchase order %d returned a mismatched identity", objectID)
			}
			return record.Responsible, nil
		},
		patch: func(ctx context.Context, client OwnerAssignmentClient, objectID int, fields inventree.PatchFields) error {
			_, err := client.UpdatePurchaseOrderDetail(ctx, objectID, fields)
			return err
		},
	},
	OwnerObjectStockItem: {
		field: "owner",
		current: func(ctx context.Context, client OwnerAssignmentClient, objectID int) (*int, error) {
			record, err := client.GetStockItemDetail(ctx, objectID)
			if err != nil {
				return nil, err
			}
			if record.PK != objectID {
				return nil, fmt.Errorf("stock item %d returned a mismatched identity", objectID)
			}
			return record.Owner, nil
		},
		patch: func(ctx context.Context, client OwnerAssignmentClient, objectID int, fields inventree.PatchFields) error {
			_, err := client.UpdateStockItem(ctx, objectID, fields)
			return err
		},
	},
	OwnerObjectStockLocation: {
		field: "owner",
		current: func(ctx context.Context, client OwnerAssignmentClient, objectID int) (*int, error) {
			record, err := client.GetStockLocation(ctx, objectID)
			if err != nil {
				return nil, err
			}
			if record.PK != objectID {
				return nil, fmt.Errorf("stock location %d returned a mismatched identity", objectID)
			}
			return record.Owner, nil
		},
		patch: func(ctx context.Context, client OwnerAssignmentClient, objectID int, fields inventree.PatchFields) error {
			_, err := client.UpdateStockLocation(ctx, objectID, fields)
			return err
		},
	},
}

func validOwnerObjectType(objectType string) bool {
	_, ok := ownerObjectDescriptors[objectType]
	return ok
}

type OwnerView struct {
	PK    int    `json:"pk"`
	Type  string `json:"type"`
	Name  string `json:"name"`
	Label string `json:"label"`
}

func ownerView(record inventree.Owner) OwnerView {
	return OwnerView{PK: record.PK, Type: record.OwnerModel, Name: record.Name, Label: record.Label}
}

type OwnerLookupClient interface {
	SearchOwnersPage(context.Context, inventree.OwnerQuery) (inventree.Page[inventree.Owner], error)
	GetOwner(context.Context, int) (inventree.Owner, error)
}

type SearchOwnersInput struct {
	Query      string `json:"query,omitempty" jsonschema:"Narrowing search text matched against owner name, username, or group. Required unless object_type is supplied."`
	ObjectType string `json:"object_type,omitempty" jsonschema:"Optional bound: part, purchase_order, stock_item, or stock_location. Does not filter results server-side; accepted as an alternative bound to query."`
	Type       string `json:"type,omitempty" jsonschema:"Optional owner kind filter: user or group, matched against the owner's InvenTree owner_model."`
	IsActive   *bool  `json:"is_active,omitempty" jsonschema:"Optional filter for active versus disabled owners."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset     int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

func registerOwnerLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchOwnersToolName, "Search owners", "Searches InvenTree users and groups eligible as a responsible owner.", searchOwners(deps))
	addReadOnlyTool(server, deps, GetOwnerToolName, "Get owner", "Retrieves one InvenTree owner (user or group) by stable ID.", getOwner(deps))
}

func searchOwners(deps Dependencies) mcp.ToolHandlerFor[SearchOwnersInput, LookupOutput[OwnerView]] {
	return LookupHandler[OwnerLookupClient, SearchOwnersInput, LookupOutput[OwnerView]](deps, SearchOwnersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client OwnerLookupClient, input SearchOwnersInput) (*mcp.CallToolResult, LookupOutput[OwnerView], error) {
			query := strings.TrimSpace(input.Query)
			if query == "" && !validOwnerObjectType(input.ObjectType) {
				return nil, LookupOutput[OwnerView]{}, errors.New("query or a supported object_type is required to bound owner search")
			}
			page, err := client.SearchOwnersPage(ctx, inventree.OwnerQuery{Search: query, IsActive: input.IsActive, Limit: NormalizeLookupLimit(input.Limit), Offset: input.Offset})
			if err != nil {
				return nil, LookupOutput[OwnerView]{}, err
			}
			results := make([]OwnerView, 0, len(page.Results))
			for _, record := range page.Results {
				if input.Type != "" && !strings.EqualFold(record.OwnerModel, input.Type) {
					continue
				}
				results = append(results, ownerView(record))
			}
			return listOutput(results, nil)
		})
}

func getOwner(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[OwnerView]] {
	return LookupHandler[OwnerLookupClient, IDInput, RecordOutput[OwnerView]](deps, GetOwnerToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client OwnerLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[OwnerView], error) {
			record, err := client.GetOwner(ctx, input.ID)
			if err != nil {
				return recordOutput(OwnerView{}, err)
			}
			return recordOutput(ownerView(record), nil)
		})
}

// OwnerAssignmentClient is the narrow client surface needed to preview,
// validate, and execute guarded responsible-owner assignment or clearing
// across the F-S48 object matrix: Part, PurchaseOrder, StockItem, and
// StockLocation.
type OwnerAssignmentClient interface {
	GetOwner(context.Context, int) (inventree.Owner, error)
	GetPartDetail(context.Context, int) (inventree.PartDetail, error)
	UpdatePart(context.Context, int, inventree.PatchFields) (inventree.Part, error)
	GetPurchaseOrderDetail(context.Context, int) (inventree.PurchaseOrderDetail, error)
	UpdatePurchaseOrderDetail(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrderDetail, error)
	GetStockItemDetail(context.Context, int) (inventree.StockItemDetail, error)
	UpdateStockItem(context.Context, int, inventree.PatchFields) (inventree.StockItem, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	UpdateStockLocation(context.Context, int, inventree.PatchFields) (inventree.StockLocation, error)
}

type AssignOwnerInput struct {
	ObjectType string `json:"object_type" jsonschema:"Object type owning the responsible-owner field: part, purchase_order, stock_item, or stock_location."`
	ObjectID   int    `json:"object_id" jsonschema:"Stable primary key of the object."`
	OwnerID    *int   `json:"owner_id,omitempty" jsonschema:"New owner primary key from search_owners or get_owner. Omit to clear the current owner."`
	Confirm    bool   `json:"confirm,omitempty" jsonschema:"Confirm the exact previewed owner assignment or clear."`
	PlanHash   string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

// AssignOwnerPlan is the state bound into the confirmation token. Binding
// CurrentOwnerID means a fresh preview is required whenever the object's
// owner changed since the last preview, so a confirm can never silently
// replace a different owner than the one actually reviewed.
type AssignOwnerPlan struct {
	Action         string `json:"action"`
	ObjectType     string `json:"object_type"`
	ObjectID       int    `json:"object_id"`
	CurrentOwnerID *int   `json:"current_owner_id,omitempty"`
	NewOwnerID     *int   `json:"new_owner_id,omitempty"`
}

type AssignOwnerOutput struct {
	Status        string                 `json:"status"`
	ObjectType    string                 `json:"object_type,omitempty"`
	ObjectID      int                    `json:"object_id,omitempty"`
	OwnerID       *int                   `json:"owner_id,omitempty"`
	Owner         *OwnerView             `json:"owner,omitempty"`
	Plan          *AssignOwnerPlan       `json:"plan,omitempty"`
	PlanHash      string                 `json:"plan_hash,omitempty"`
	Verified      bool                   `json:"verified,omitempty"`
	Recovered     bool                   `json:"recovered,omitempty"`
	Validation    *ValidationFailure     `json:"validation,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
	RecoveryPlan  string                 `json:"recovery_plan,omitempty"`
}

type ownerConfirmation struct {
	digest     string
	principal  string
	objectType string
	objectID   int
	expiresAt  time.Time
}

type ownerPlanStore struct {
	mu                     sync.Mutex
	entries                map[string]ownerConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newOwnerPlanStore(now func() time.Time, token func() (string, error)) *ownerPlanStore {
	return &ownerPlanStore{
		entries: map[string]ownerConfirmation{}, now: now, token: token, principal: stockPlanPrincipal,
		maxEntries: ownerPlanMaxEntries, maxEntriesPerPrincipal: ownerPlanMaxEntriesPerPrincipal,
	}
}

func (s *ownerPlanStore) issue(ctx context.Context, plan AssignOwnerPlan) (string, error) {
	digest, err := ownerPlanDigest(plan)
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
		if !now.Before(entry.expiresAt) || entry.principal == principal && entry.objectType == plan.ObjectType && entry.objectID == plan.ObjectID {
			delete(s.entries, key)
			continue
		}
		if entry.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errors.New("too many outstanding owner confirmation plans")
	}
	s.entries[token] = ownerConfirmation{digest: digest, principal: principal, objectType: plan.ObjectType, objectID: plan.ObjectID, expiresAt: now.Add(ownerPlanLifetime)}
	return token, nil
}

func (s *ownerPlanStore) consume(ctx context.Context, token string, plan AssignOwnerPlan) bool {
	digest, err := ownerPlanDigest(plan)
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
	if !ok || entry.digest != digest || entry.principal != s.principal(ctx) || entry.objectType != plan.ObjectType || entry.objectID != plan.ObjectID || !now.Before(entry.expiresAt) {
		return false
	}
	delete(s.entries, token)
	return true
}

func ownerPlanDigest(plan AssignOwnerPlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerOwnerWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.ownerPlanStore == nil {
		deps.ownerPlanStore = newOwnerPlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, AssignOwnerToolName, "Assign or clear owner",
		"Previews and confirms assigning or clearing the responsible owner on a part, purchase order, stock item, or stock location.",
		assignOwner(deps))
}

func assignOwner(deps Dependencies) mcp.ToolHandlerFor[AssignOwnerInput, AssignOwnerOutput] {
	return LookupHandler[OwnerAssignmentClient, AssignOwnerInput, AssignOwnerOutput](deps, AssignOwnerToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client OwnerAssignmentClient, input AssignOwnerInput) (*mcp.CallToolResult, AssignOwnerOutput, error) {
			if !validOwnerObjectType(input.ObjectType) {
				return ownerAssignmentValidation("object_type must be one of part, purchase_order, stock_item, or stock_location")
			}
			if input.ObjectID <= 0 {
				return ownerAssignmentValidation("object_id must be a positive stable primary key")
			}
			currentOwnerID, err := ownerObjectCurrent(ctx, client, input.ObjectType, input.ObjectID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), AssignOwnerOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, AssignOwnerOutput{}, safeOwnerAssignmentError("object lookup")
			}
			if input.OwnerID == nil && currentOwnerID == nil {
				return ownerAssignmentValidation("the object has no owner to clear")
			}
			if input.OwnerID != nil && currentOwnerID != nil && *input.OwnerID == *currentOwnerID {
				return ownerAssignmentValidation("the requested owner_id already matches the current owner")
			}
			var resolvedOwner *OwnerView
			if input.OwnerID != nil {
				record, ownerErr := client.GetOwner(ctx, *input.OwnerID)
				if isNotFound(ownerErr) {
					return ownerAssignmentValidation(fmt.Sprintf("owner_id %d does not identify a readable owner", *input.OwnerID))
				}
				if ownerErr != nil {
					return nil, AssignOwnerOutput{}, safeOwnerAssignmentError("owner lookup")
				}
				if record.PK != *input.OwnerID {
					return ownerAssignmentValidation(fmt.Sprintf("owner_id %d returned a mismatched identity", *input.OwnerID))
				}
				view := ownerView(record)
				resolvedOwner = &view
			}
			action := "assign_owner"
			if input.OwnerID == nil {
				action = "clear_owner"
			}
			plan := AssignOwnerPlan{Action: action, ObjectType: input.ObjectType, ObjectID: input.ObjectID, CurrentOwnerID: currentOwnerID, NewOwnerID: input.OwnerID}
			if !input.Confirm {
				return issueOwnerPlan(ctx, deps.ownerPlanStore, plan, resolvedOwner)
			}
			if deps.ownerPlanStore == nil || input.PlanHash == "" || !deps.ownerPlanStore.consume(ctx, input.PlanHash, plan) {
				return ownerPlanClarification(plan, "The plan is missing, stale, expired, reused, or belongs to another principal; preview the current object again.")
			}
			mutationErr := patchOwnerObject(ctx, client, input.ObjectType, input.ObjectID, input.OwnerID)
			return verifyOwnerAssignment(ctx, client, input.ObjectType, input.ObjectID, input.OwnerID, resolvedOwner, mutationErr)
		})
}

// ownerObjectCurrent returns the object's current owner/responsible field
// value. A nil error with a nil result means the object exists and
// currently has no owner assigned.
func ownerObjectCurrent(ctx context.Context, client OwnerAssignmentClient, objectType string, objectID int) (*int, error) {
	descriptor, ok := ownerObjectDescriptors[objectType]
	if !ok {
		return nil, fmt.Errorf("unsupported owner object_type %q", objectType)
	}
	return descriptor.current(ctx, client, objectID)
}

func patchOwnerObject(ctx context.Context, client OwnerAssignmentClient, objectType string, objectID int, ownerID *int) error {
	descriptor, ok := ownerObjectDescriptors[objectType]
	if !ok {
		return fmt.Errorf("unsupported owner object_type %q", objectType)
	}
	fields := inventree.PatchFields{}
	if ownerID != nil {
		fields[descriptor.field] = inventree.Set(*ownerID)
	} else {
		fields[descriptor.field] = inventree.Null()
	}
	return descriptor.patch(ctx, client, objectID, fields)
}

func verifyOwnerAssignment(ctx context.Context, client OwnerAssignmentClient, objectType string, objectID int, requestedOwnerID *int, resolvedOwner *OwnerView, mutationErr error) (*mcp.CallToolResult, AssignOwnerOutput, error) {
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, AssignOwnerOutput{}, errors.New("owner assignment was rejected")
		}
	}
	currentOwnerID, err := ownerObjectCurrent(ctx, client, objectType, objectID)
	if err != nil || !sameOptionalInt(currentOwnerID, requestedOwnerID) {
		return TextResult(StatusPartialFailure), AssignOwnerOutput{Status: StatusPartialFailure, ObjectType: objectType, ObjectID: objectID, RecoveryPlan: fmt.Sprintf("Read the %s again before retrying; the owner change could not be verified.", objectType)}, nil
	}
	return TextResult(StatusOK), AssignOwnerOutput{Status: StatusOK, ObjectType: objectType, ObjectID: objectID, OwnerID: currentOwnerID, Owner: resolvedOwner, Verified: true, Recovered: mutationErr != nil}, nil
}

func issueOwnerPlan(ctx context.Context, store *ownerPlanStore, plan AssignOwnerPlan, resolvedOwner *OwnerView) (*mcp.CallToolResult, AssignOwnerOutput, error) {
	if store == nil {
		return nil, AssignOwnerOutput{}, errors.New("owner confirmation store is unavailable")
	}
	token, err := store.issue(ctx, plan)
	if err != nil {
		return nil, AssignOwnerOutput{}, err
	}
	question := "Assign this exact owner to the object?"
	if plan.NewOwnerID == nil {
		question = "Clear the current owner from this exact object?"
	}
	clarification := NewClarification(question, "confirmation", "review the exact current object and owner state, then provide confirm:true with this plan_hash", "plan_hash", false, nil, map[string]any{"object_type": plan.ObjectType, "object_id": plan.ObjectID, "owner_id": plan.NewOwnerID, "confirm": true, "plan_hash": token})
	return TextResult(StatusClarificationRequired), AssignOwnerOutput{Status: StatusClarificationRequired, ObjectType: plan.ObjectType, ObjectID: plan.ObjectID, OwnerID: plan.NewOwnerID, Owner: resolvedOwner, Plan: &plan, PlanHash: token, Clarification: &clarification}, nil
}

func ownerPlanClarification(plan AssignOwnerPlan, reason string) (*mcp.CallToolResult, AssignOwnerOutput, error) {
	clarification := NewClarification("Which current owner plan should authorize this change?", "confirmation", reason, "plan_hash", false, nil, map[string]any{"object_type": plan.ObjectType, "object_id": plan.ObjectID})
	return TextResult(StatusClarificationRequired), AssignOwnerOutput{Status: StatusClarificationRequired, ObjectType: plan.ObjectType, ObjectID: plan.ObjectID, Plan: &plan, Clarification: &clarification}, nil
}

func ownerAssignmentValidation(message string) (*mcp.CallToolResult, AssignOwnerOutput, error) {
	return TextResult(StatusValidationFailed), AssignOwnerOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "object", Messages: []string{message}}}}}, nil
}

func safeOwnerAssignmentError(operation string) error {
	return fmt.Errorf("%s failed; inspect InvenTree availability and permissions before retrying", operation)
}
