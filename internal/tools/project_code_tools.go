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
	ProjectCodeObjectPurchaseOrder          = "purchase_order"
	ProjectCodeObjectPurchaseOrderLine      = "purchase_order_line"
	ProjectCodeObjectPurchaseOrderExtraLine = "purchase_order_extra_line"

	projectCodePlanLifetime               = 5 * time.Minute
	projectCodePlanMaxEntries             = 4096
	projectCodePlanMaxEntriesPerPrincipal = 64
)

// projectCodeObjectDescriptor centralizes everything project_code_tools.go
// needs to know about one object type: its project_code field, how to read
// the current value, and how to patch it. Adding an object type means adding
// one entry here, rather than keeping several parallel switches/maps in sync.
type projectCodeObjectDescriptor struct {
	current func(ctx context.Context, client ProjectCodeAssignmentClient, objectID int) (*int, error)
	patch   func(ctx context.Context, client ProjectCodeAssignmentClient, objectID int, fields inventree.PatchFields) error
}

var projectCodeObjectDescriptors = map[string]projectCodeObjectDescriptor{
	ProjectCodeObjectPurchaseOrder: {
		current: func(ctx context.Context, client ProjectCodeAssignmentClient, objectID int) (*int, error) {
			record, err := client.GetPurchaseOrderDetail(ctx, objectID)
			if err != nil {
				return nil, err
			}
			if record.PK != objectID {
				return nil, fmt.Errorf("purchase order %d returned a mismatched identity", objectID)
			}
			return record.ProjectCode, nil
		},
		patch: func(ctx context.Context, client ProjectCodeAssignmentClient, objectID int, fields inventree.PatchFields) error {
			_, err := client.UpdatePurchaseOrderDetail(ctx, objectID, fields)
			return err
		},
	},
	ProjectCodeObjectPurchaseOrderLine: {
		current: func(ctx context.Context, client ProjectCodeAssignmentClient, objectID int) (*int, error) {
			record, err := client.GetPurchaseOrderLine(ctx, objectID)
			if err != nil {
				return nil, err
			}
			if record.PK != objectID {
				return nil, fmt.Errorf("purchase order line %d returned a mismatched identity", objectID)
			}
			return record.ProjectCode, nil
		},
		// Pinned InvenTree 1.5.0/API 530 behavior: PATCHing project_code alone
		// on /api/order/po-line/{id}/ is rejected with "Supplier part must be
		// specified" (and, if part is also supplied, "Purchase order must be
		// specified") — touching project_code makes the line serializer
		// re-validate part/order as if they were being replaced, even though
		// they are unchanged. Re-supplying the line's own current part and
		// order alongside project_code satisfies that validation without
		// altering their values. quantity was confirmed unnecessary (live
		// Testcontainers probe against InvenTree 1.5.0) and is deliberately
		// left out: resupplying it would risk silently clobbering a
		// concurrent quantity edit made between this fetch and the PATCH,
		// since verifyProjectCodeAssignment only re-checks project_code.
		patch: func(ctx context.Context, client ProjectCodeAssignmentClient, objectID int, fields inventree.PatchFields) error {
			line, err := client.GetPurchaseOrderLine(ctx, objectID)
			if err != nil {
				return err
			}
			fields["part"] = inventree.Set(line.Part)
			fields["order"] = inventree.Set(line.Order)
			_, err = client.UpdatePurchaseOrderLine(ctx, objectID, fields)
			return err
		},
	},
	ProjectCodeObjectPurchaseOrderExtraLine: {
		current: func(ctx context.Context, client ProjectCodeAssignmentClient, objectID int) (*int, error) {
			record, err := client.GetPurchaseOrderExtraLine(ctx, objectID)
			if err != nil {
				return nil, err
			}
			if record.PK != objectID {
				return nil, fmt.Errorf("purchase order extra line %d returned a mismatched identity", objectID)
			}
			return record.ProjectCode, nil
		},
		patch: func(ctx context.Context, client ProjectCodeAssignmentClient, objectID int, fields inventree.PatchFields) error {
			_, err := client.UpdatePurchaseOrderExtraLine(ctx, objectID, fields)
			return err
		},
	},
}

func validProjectCodeObjectType(objectType string) bool {
	_, ok := projectCodeObjectDescriptors[objectType]
	return ok
}

// ProjectCodeLookupClient is the narrow client surface needed for bounded
// project-code discovery.
type ProjectCodeLookupClient interface {
	SearchProjectCodesPage(context.Context, inventree.ProjectCodeQuery) (inventree.Page[inventree.ProjectCode], error)
	GetProjectCode(context.Context, int) (inventree.ProjectCode, error)
}

type SearchProjectCodesInput struct {
	Query    string `json:"query" jsonschema:"Narrowing search text matched against project code and description."`
	IsActive *bool  `json:"is_active,omitempty" jsonschema:"Optional filter for active versus disabled project codes."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset   int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

func registerProjectCodeLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchProjectCodesToolName, "Search project codes", "Searches InvenTree project codes eligible for assignment.", searchProjectCodes(deps))
	addReadOnlyTool(server, deps, GetProjectCodeToolName, "Get project code", "Retrieves one InvenTree project code by stable ID.", getProjectCode(deps))
}

func searchProjectCodes(deps Dependencies) mcp.ToolHandlerFor[SearchProjectCodesInput, LookupOutput[inventree.ProjectCode]] {
	return LookupHandler[ProjectCodeLookupClient, SearchProjectCodesInput, LookupOutput[inventree.ProjectCode]](deps, SearchProjectCodesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ProjectCodeLookupClient, input SearchProjectCodesInput) (*mcp.CallToolResult, LookupOutput[inventree.ProjectCode], error) {
			query := strings.TrimSpace(input.Query)
			if query == "" {
				return nil, LookupOutput[inventree.ProjectCode]{}, errors.New("query is required to bound project-code search")
			}
			page, err := client.SearchProjectCodesPage(ctx, inventree.ProjectCodeQuery{Search: query, IsActive: input.IsActive, Limit: NormalizeLookupLimit(input.Limit), Offset: input.Offset})
			if err != nil {
				return nil, LookupOutput[inventree.ProjectCode]{}, err
			}
			return listOutput(page.Results, nil)
		})
}

func getProjectCode(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.ProjectCode]] {
	return LookupHandler[ProjectCodeLookupClient, IDInput, RecordOutput[inventree.ProjectCode]](deps, GetProjectCodeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ProjectCodeLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.ProjectCode], error) {
			record, err := client.GetProjectCode(ctx, input.ID)
			if err == nil && record.PK != input.ID {
				return nil, RecordOutput[inventree.ProjectCode]{}, errors.New("InvenTree returned a mismatched project-code identity")
			}
			return recordOutput(record, err)
		})
}

// ProjectCodeAssignmentClient is the narrow client surface needed to preview,
// validate, and execute guarded project-code replacement or clearing across
// the F-S50 object matrix: PurchaseOrder, PurchaseOrderLineItem, and
// PurchaseOrderExtraLine.
type ProjectCodeAssignmentClient interface {
	GetProjectCode(context.Context, int) (inventree.ProjectCode, error)
	GetPurchaseOrderDetail(context.Context, int) (inventree.PurchaseOrderDetail, error)
	UpdatePurchaseOrderDetail(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrderDetail, error)
	GetPurchaseOrderLine(context.Context, int) (inventree.PurchaseOrderLineItem, error)
	UpdatePurchaseOrderLine(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrderLineItem, error)
	GetPurchaseOrderExtraLine(context.Context, int) (inventree.PurchaseOrderExtraLine, error)
	UpdatePurchaseOrderExtraLine(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrderExtraLine, error)
}

type AssignProjectCodeInput struct {
	ObjectType    string `json:"object_type" jsonschema:"Object type owning the project_code field: purchase_order, purchase_order_line, or purchase_order_extra_line."`
	ObjectID      int    `json:"object_id" jsonschema:"Stable primary key of the object."`
	ProjectCodeID *int   `json:"project_code_id,omitempty" jsonschema:"New project-code primary key from search_project_codes or get_project_code. Omit to clear the current project code."`
	Confirm       bool   `json:"confirm,omitempty" jsonschema:"Confirm the exact previewed project-code assignment or clear."`
	PlanHash      string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

// AssignProjectCodePlan is the state bound into the confirmation token.
// Binding CurrentProjectCodeID means a fresh preview is required whenever the
// object's project code changed since the last preview, so a confirm can
// never silently replace a different project code than the one actually
// reviewed.
type AssignProjectCodePlan struct {
	Action               string `json:"action"`
	ObjectType           string `json:"object_type"`
	ObjectID             int    `json:"object_id"`
	CurrentProjectCodeID *int   `json:"current_project_code_id,omitempty"`
	NewProjectCodeID     *int   `json:"new_project_code_id,omitempty"`
}

type AssignProjectCodeOutput struct {
	Status        string                 `json:"status"`
	ObjectType    string                 `json:"object_type,omitempty"`
	ObjectID      int                    `json:"object_id,omitempty"`
	ProjectCodeID *int                   `json:"project_code_id,omitempty"`
	ProjectCode   *inventree.ProjectCode `json:"project_code,omitempty"`
	Plan          *AssignProjectCodePlan `json:"plan,omitempty"`
	PlanHash      string                 `json:"plan_hash,omitempty"`
	Verified      bool                   `json:"verified,omitempty"`
	Recovered     bool                   `json:"recovered,omitempty"`
	Validation    *ValidationFailure     `json:"validation,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
	RecoveryPlan  string                 `json:"recovery_plan,omitempty"`
}

type projectCodeConfirmation struct {
	digest     string
	principal  string
	objectType string
	objectID   int
	expiresAt  time.Time
}

type projectCodePlanStore struct {
	mu                     sync.Mutex
	entries                map[string]projectCodeConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newProjectCodePlanStore(now func() time.Time, token func() (string, error)) *projectCodePlanStore {
	return &projectCodePlanStore{
		entries: map[string]projectCodeConfirmation{}, now: now, token: token, principal: stockPlanPrincipal,
		maxEntries: projectCodePlanMaxEntries, maxEntriesPerPrincipal: projectCodePlanMaxEntriesPerPrincipal,
	}
}

func (s *projectCodePlanStore) issue(ctx context.Context, plan AssignProjectCodePlan) (string, error) {
	digest, err := projectCodePlanDigest(plan)
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
		return "", errors.New("too many outstanding project-code confirmation plans")
	}
	s.entries[token] = projectCodeConfirmation{digest: digest, principal: principal, objectType: plan.ObjectType, objectID: plan.ObjectID, expiresAt: now.Add(projectCodePlanLifetime)}
	return token, nil
}

func (s *projectCodePlanStore) consume(ctx context.Context, token string, plan AssignProjectCodePlan) bool {
	digest, err := projectCodePlanDigest(plan)
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

func projectCodePlanDigest(plan AssignProjectCodePlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerProjectCodeWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.projectCodePlanStore == nil {
		deps.projectCodePlanStore = newProjectCodePlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, AssignProjectCodeToolName, "Assign or clear project code",
		"Previews and confirms assigning or clearing the project code on a purchase order, purchase-order line, or purchase-order extra line.",
		assignProjectCode(deps))
}

func assignProjectCode(deps Dependencies) mcp.ToolHandlerFor[AssignProjectCodeInput, AssignProjectCodeOutput] {
	return LookupHandler[ProjectCodeAssignmentClient, AssignProjectCodeInput, AssignProjectCodeOutput](deps, AssignProjectCodeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ProjectCodeAssignmentClient, input AssignProjectCodeInput) (*mcp.CallToolResult, AssignProjectCodeOutput, error) {
			if !validProjectCodeObjectType(input.ObjectType) {
				return projectCodeAssignmentValidation("object_type must be one of purchase_order, purchase_order_line, or purchase_order_extra_line")
			}
			if input.ObjectID <= 0 {
				return projectCodeAssignmentValidation("object_id must be a positive stable primary key")
			}
			currentProjectCodeID, err := projectCodeObjectCurrent(ctx, client, input.ObjectType, input.ObjectID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), AssignProjectCodeOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, AssignProjectCodeOutput{}, safeProjectCodeAssignmentError("object lookup")
			}
			if input.ProjectCodeID == nil && currentProjectCodeID == nil {
				return projectCodeAssignmentValidation("the object has no project code to clear")
			}
			if input.ProjectCodeID != nil && currentProjectCodeID != nil && *input.ProjectCodeID == *currentProjectCodeID {
				return projectCodeAssignmentValidation("the requested project_code_id already matches the current project code")
			}
			var resolvedProjectCode *inventree.ProjectCode
			if input.ProjectCodeID != nil {
				record, projectCodeErr := client.GetProjectCode(ctx, *input.ProjectCodeID)
				if isNotFound(projectCodeErr) {
					return projectCodeAssignmentValidation(fmt.Sprintf("project_code_id %d does not identify a readable project code", *input.ProjectCodeID))
				}
				if projectCodeErr != nil {
					return nil, AssignProjectCodeOutput{}, safeProjectCodeAssignmentError("project-code lookup")
				}
				if record.PK != *input.ProjectCodeID {
					return projectCodeAssignmentValidation(fmt.Sprintf("project_code_id %d returned a mismatched identity", *input.ProjectCodeID))
				}
				resolvedProjectCode = &record
			}
			action := "assign_project_code"
			if input.ProjectCodeID == nil {
				action = "clear_project_code"
			}
			plan := AssignProjectCodePlan{Action: action, ObjectType: input.ObjectType, ObjectID: input.ObjectID, CurrentProjectCodeID: currentProjectCodeID, NewProjectCodeID: input.ProjectCodeID}
			if !input.Confirm {
				return issueProjectCodePlan(ctx, deps.projectCodePlanStore, plan, resolvedProjectCode)
			}
			if deps.projectCodePlanStore == nil || input.PlanHash == "" || !deps.projectCodePlanStore.consume(ctx, input.PlanHash, plan) {
				return projectCodePlanClarification(plan, "The plan is missing, stale, expired, reused, or belongs to another principal; preview the current object again.")
			}
			mutationErr := patchProjectCodeObject(ctx, client, input.ObjectType, input.ObjectID, input.ProjectCodeID)
			return verifyProjectCodeAssignment(ctx, client, input.ObjectType, input.ObjectID, input.ProjectCodeID, resolvedProjectCode, mutationErr)
		})
}

// projectCodeObjectCurrent returns the object's current project_code field
// value. A nil error with a nil result means the object exists and currently
// has no project code assigned.
func projectCodeObjectCurrent(ctx context.Context, client ProjectCodeAssignmentClient, objectType string, objectID int) (*int, error) {
	descriptor, ok := projectCodeObjectDescriptors[objectType]
	if !ok {
		return nil, fmt.Errorf("unsupported project_code object_type %q", objectType)
	}
	return descriptor.current(ctx, client, objectID)
}

func patchProjectCodeObject(ctx context.Context, client ProjectCodeAssignmentClient, objectType string, objectID int, projectCodeID *int) error {
	descriptor, ok := projectCodeObjectDescriptors[objectType]
	if !ok {
		return fmt.Errorf("unsupported project_code object_type %q", objectType)
	}
	fields := inventree.PatchFields{}
	if projectCodeID != nil {
		fields["project_code"] = inventree.Set(*projectCodeID)
	} else {
		fields["project_code"] = inventree.Null()
	}
	return descriptor.patch(ctx, client, objectID, fields)
}

func verifyProjectCodeAssignment(ctx context.Context, client ProjectCodeAssignmentClient, objectType string, objectID int, requestedProjectCodeID *int, resolvedProjectCode *inventree.ProjectCode, mutationErr error) (*mcp.CallToolResult, AssignProjectCodeOutput, error) {
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, AssignProjectCodeOutput{}, errors.New("project-code assignment was rejected")
		}
	}
	currentProjectCodeID, err := projectCodeObjectCurrent(ctx, client, objectType, objectID)
	if err != nil || !sameOptionalInt(currentProjectCodeID, requestedProjectCodeID) {
		return TextResult(StatusPartialFailure), AssignProjectCodeOutput{Status: StatusPartialFailure, ObjectType: objectType, ObjectID: objectID, RecoveryPlan: fmt.Sprintf("Read the %s again before retrying; the project-code change could not be verified.", objectType)}, nil
	}
	return TextResult(StatusOK), AssignProjectCodeOutput{Status: StatusOK, ObjectType: objectType, ObjectID: objectID, ProjectCodeID: currentProjectCodeID, ProjectCode: resolvedProjectCode, Verified: true, Recovered: mutationErr != nil}, nil
}

func issueProjectCodePlan(ctx context.Context, store *projectCodePlanStore, plan AssignProjectCodePlan, resolvedProjectCode *inventree.ProjectCode) (*mcp.CallToolResult, AssignProjectCodeOutput, error) {
	if store == nil {
		return nil, AssignProjectCodeOutput{}, errors.New("project-code confirmation store is unavailable")
	}
	token, err := store.issue(ctx, plan)
	if err != nil {
		return nil, AssignProjectCodeOutput{}, err
	}
	question := "Assign this exact project code to the object?"
	if plan.NewProjectCodeID == nil {
		question = "Clear the current project code from this exact object?"
	}
	clarification := NewClarification(question, "confirmation", "review the exact current object and project-code state, then provide confirm:true with this plan_hash", "plan_hash", false, nil, map[string]any{"object_type": plan.ObjectType, "object_id": plan.ObjectID, "project_code_id": plan.NewProjectCodeID, "confirm": true, "plan_hash": token})
	return TextResult(StatusClarificationRequired), AssignProjectCodeOutput{Status: StatusClarificationRequired, ObjectType: plan.ObjectType, ObjectID: plan.ObjectID, ProjectCodeID: plan.NewProjectCodeID, ProjectCode: resolvedProjectCode, Plan: &plan, PlanHash: token, Clarification: &clarification}, nil
}

func projectCodePlanClarification(plan AssignProjectCodePlan, reason string) (*mcp.CallToolResult, AssignProjectCodeOutput, error) {
	clarification := NewClarification("Which current project-code plan should authorize this change?", "confirmation", reason, "plan_hash", false, nil, map[string]any{"object_type": plan.ObjectType, "object_id": plan.ObjectID})
	return TextResult(StatusClarificationRequired), AssignProjectCodeOutput{Status: StatusClarificationRequired, ObjectType: plan.ObjectType, ObjectID: plan.ObjectID, Plan: &plan, Clarification: &clarification}, nil
}

func projectCodeAssignmentValidation(message string) (*mcp.CallToolResult, AssignProjectCodeOutput, error) {
	return TextResult(StatusValidationFailed), AssignProjectCodeOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "object", Messages: []string{message}}}}}, nil
}

func safeProjectCodeAssignmentError(operation string) error {
	return fmt.Errorf("%s failed; inspect InvenTree availability and permissions before retrying", operation)
}
