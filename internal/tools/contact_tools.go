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
	contactPlanLifetime               = 5 * time.Minute
	contactPlanMaxEntries             = 4096
	contactPlanMaxEntriesPerPrincipal = 64
)

// ContactLookupClient is the narrow client surface needed for bounded,
// company-scoped contact discovery.
type ContactLookupClient interface {
	SearchContactsPage(context.Context, inventree.ContactQuery) (inventree.Page[inventree.Contact], error)
	GetContact(context.Context, int) (inventree.Contact, error)
}

type SearchContactsInput struct {
	CompanyID int    `json:"company_id" jsonschema:"Stable company primary key. Contact search is always scoped to one company."`
	Search    string `json:"search,omitempty" jsonschema:"Optional narrowing search text matched against the contact name."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset    int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

func registerContactLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchContactsToolName, "Search contacts",
		"Searches one company's structured contact records. Never returns phone, email, or other contact PII.", searchContacts(deps))
	addReadOnlyTool(server, deps, GetContactToolName, "Get contact",
		"Retrieves one structured company contact by stable ID. Never returns phone, email, or other contact PII.", getContact(deps))
}

func searchContacts(deps Dependencies) mcp.ToolHandlerFor[SearchContactsInput, LookupOutput[inventree.Contact]] {
	return LookupHandler[ContactLookupClient, SearchContactsInput, LookupOutput[inventree.Contact]](deps, SearchContactsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ContactLookupClient, input SearchContactsInput) (*mcp.CallToolResult, LookupOutput[inventree.Contact], error) {
			if input.CompanyID <= 0 {
				return nil, LookupOutput[inventree.Contact]{}, errors.New("company_id is required to bound contact search")
			}
			page, err := client.SearchContactsPage(ctx, inventree.ContactQuery{
				CompanyID: input.CompanyID, Search: input.Search, Limit: NormalizeLookupLimit(input.Limit), Offset: input.Offset,
			})
			if err != nil {
				return nil, LookupOutput[inventree.Contact]{}, err
			}
			return listOutput(page.Results, nil)
		})
}

func getContact(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.Contact]] {
	return LookupHandler[ContactLookupClient, IDInput, RecordOutput[inventree.Contact]](deps, GetContactToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ContactLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.Contact], error) {
			record, err := client.GetContact(ctx, input.ID)
			if err == nil && record.PK != input.ID {
				return nil, RecordOutput[inventree.Contact]{}, errors.New("InvenTree returned a mismatched contact identity")
			}
			return recordOutput(record, err)
		})
}

// AssignContactClient is the narrow client surface needed to preview,
// validate, and execute guarded purchase-order contact replacement/clearing.
// Contact assignment is purchase-order-specific: API 530 only exposes a
// structured `contact` FK on PurchaseOrder among already-supported objects.
type AssignContactClient interface {
	GetPurchaseOrderDetail(context.Context, int) (inventree.PurchaseOrderDetail, error)
	UpdatePurchaseOrderDetail(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrderDetail, error)
	GetContact(context.Context, int) (inventree.Contact, error)
}

type AssignContactInput struct {
	PurchaseOrderID int    `json:"purchase_order_id" jsonschema:"Stable purchase-order primary key."`
	ContactID       *int   `json:"contact_id,omitempty" jsonschema:"New contact primary key from search_contacts/get_contact, belonging to the order's supplier company. Omit to clear the current contact."`
	Confirm         bool   `json:"confirm,omitempty" jsonschema:"Confirm the exact previewed contact assignment or clear."`
	PlanHash        string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

// AssignContactPlan is the state bound into the confirmation token. Binding
// CompanyID means a fresh preview is required whenever the order's supplier
// changed since the last preview, and binding CurrentContactID means a fresh
// preview is required whenever the order's contact changed since the last
// preview, so a confirm can never silently replace a different contact or
// apply to a different supplier context than the one actually reviewed.
type AssignContactPlan struct {
	Action           string `json:"action"`
	PurchaseOrderID  int    `json:"purchase_order_id"`
	CompanyID        int    `json:"company_id"`
	CurrentContactID *int   `json:"current_contact_id,omitempty"`
	NewContactID     *int   `json:"new_contact_id,omitempty"`
}

type AssignContactOutput struct {
	Status          string                 `json:"status"`
	PurchaseOrderID int                    `json:"purchase_order_id,omitempty"`
	ContactID       *int                   `json:"contact_id,omitempty"`
	Contact         *inventree.Contact     `json:"contact,omitempty"`
	Plan            *AssignContactPlan     `json:"plan,omitempty"`
	PlanHash        string                 `json:"plan_hash,omitempty"`
	Verified        bool                   `json:"verified,omitempty"`
	Recovered       bool                   `json:"recovered,omitempty"`
	Validation      *ValidationFailure     `json:"validation,omitempty"`
	Clarification   *ClarificationResponse `json:"clarification,omitempty"`
	RecoveryPlan    string                 `json:"recovery_plan,omitempty"`
}

type contactConfirmation struct {
	digest          string
	principal       string
	purchaseOrderID int
	expiresAt       time.Time
}

type contactPlanStore struct {
	mu                     sync.Mutex
	entries                map[string]contactConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newContactPlanStore(now func() time.Time, token func() (string, error)) *contactPlanStore {
	return &contactPlanStore{
		entries: map[string]contactConfirmation{}, now: now, token: token, principal: stockPlanPrincipal,
		maxEntries: contactPlanMaxEntries, maxEntriesPerPrincipal: contactPlanMaxEntriesPerPrincipal,
	}
}

func (s *contactPlanStore) issue(ctx context.Context, plan AssignContactPlan) (string, error) {
	digest, err := contactPlanDigest(plan)
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
		if !now.Before(entry.expiresAt) || entry.principal == principal && entry.purchaseOrderID == plan.PurchaseOrderID {
			delete(s.entries, key)
			continue
		}
		if entry.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errors.New("too many outstanding contact confirmation plans")
	}
	s.entries[token] = contactConfirmation{digest: digest, principal: principal, purchaseOrderID: plan.PurchaseOrderID, expiresAt: now.Add(contactPlanLifetime)}
	return token, nil
}

func (s *contactPlanStore) consume(ctx context.Context, token string, plan AssignContactPlan) bool {
	digest, err := contactPlanDigest(plan)
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
	if !ok || entry.digest != digest || entry.principal != s.principal(ctx) || entry.purchaseOrderID != plan.PurchaseOrderID || !now.Before(entry.expiresAt) {
		return false
	}
	delete(s.entries, token)
	return true
}

func contactPlanDigest(plan AssignContactPlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerContactWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.contactPlanStore == nil {
		deps.contactPlanStore = newContactPlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, AssignContactToolName, "Assign or clear purchase-order contact",
		"Previews and confirms assigning or clearing the point-of-contact on a purchase order.",
		assignContact(deps))
}

func assignContact(deps Dependencies) mcp.ToolHandlerFor[AssignContactInput, AssignContactOutput] {
	return LookupHandler[AssignContactClient, AssignContactInput, AssignContactOutput](deps, AssignContactToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AssignContactClient, input AssignContactInput) (*mcp.CallToolResult, AssignContactOutput, error) {
			if input.PurchaseOrderID <= 0 {
				return contactAssignmentValidation("purchase_order_id must be a positive stable primary key")
			}
			order, err := client.GetPurchaseOrderDetail(ctx, input.PurchaseOrderID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), AssignContactOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, AssignContactOutput{}, safeContactAssignmentError("purchase order lookup")
			}
			if order.PK != input.PurchaseOrderID {
				return nil, AssignContactOutput{}, errors.New("purchase order returned a mismatched identity")
			}
			currentContactID := order.Contact
			if input.ContactID == nil && currentContactID == nil {
				return contactAssignmentValidation("the purchase order has no contact to clear")
			}
			if input.ContactID != nil && currentContactID != nil && *input.ContactID == *currentContactID {
				return contactAssignmentValidation("the requested contact_id already matches the current contact")
			}
			var resolvedContact *inventree.Contact
			if input.ContactID != nil {
				record, contactErr := client.GetContact(ctx, *input.ContactID)
				if isNotFound(contactErr) {
					return contactAssignmentValidation(fmt.Sprintf("contact_id %d does not identify a readable contact", *input.ContactID))
				}
				if contactErr != nil {
					return nil, AssignContactOutput{}, safeContactAssignmentError("contact lookup")
				}
				if record.PK != *input.ContactID {
					return contactAssignmentValidation(fmt.Sprintf("contact_id %d returned a mismatched identity", *input.ContactID))
				}
				if record.Company != order.Supplier {
					return contactAssignmentValidation(fmt.Sprintf("contact_id %d belongs to a different company than the purchase order's supplier", *input.ContactID))
				}
				resolvedContact = &record
			}
			action := "assign_contact"
			if input.ContactID == nil {
				action = "clear_contact"
			}
			plan := AssignContactPlan{Action: action, PurchaseOrderID: input.PurchaseOrderID, CompanyID: order.Supplier, CurrentContactID: currentContactID, NewContactID: input.ContactID}
			if !input.Confirm {
				return issueContactPlan(ctx, deps.contactPlanStore, plan, resolvedContact)
			}
			if deps.contactPlanStore == nil || input.PlanHash == "" || !deps.contactPlanStore.consume(ctx, input.PlanHash, plan) {
				return contactPlanClarification(plan, "The plan is missing, stale, expired, reused, or belongs to another principal; preview the current purchase order again.")
			}
			mutationErr := patchPurchaseOrderContact(ctx, client, input.PurchaseOrderID, input.ContactID)
			return verifyContactAssignment(ctx, client, input.PurchaseOrderID, input.ContactID, resolvedContact, mutationErr)
		})
}

func patchPurchaseOrderContact(ctx context.Context, client AssignContactClient, orderID int, contactID *int) error {
	fields := inventree.PatchFields{}
	if contactID != nil {
		fields["contact"] = inventree.Set(*contactID)
	} else {
		fields["contact"] = inventree.Null()
	}
	_, err := client.UpdatePurchaseOrderDetail(ctx, orderID, fields)
	return err
}

func verifyContactAssignment(ctx context.Context, client AssignContactClient, orderID int, requestedContactID *int, resolvedContact *inventree.Contact, mutationErr error) (*mcp.CallToolResult, AssignContactOutput, error) {
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, AssignContactOutput{}, errors.New("contact assignment was rejected")
		}
	}
	order, err := client.GetPurchaseOrderDetail(ctx, orderID)
	if err != nil || !sameOptionalInt(order.Contact, requestedContactID) {
		return TextResult(StatusPartialFailure), AssignContactOutput{Status: StatusPartialFailure, PurchaseOrderID: orderID, RecoveryPlan: "Read the purchase order again before retrying; the contact change could not be verified."}, nil
	}
	return TextResult(StatusOK), AssignContactOutput{Status: StatusOK, PurchaseOrderID: orderID, ContactID: order.Contact, Contact: resolvedContact, Verified: true, Recovered: mutationErr != nil}, nil
}

func issueContactPlan(ctx context.Context, store *contactPlanStore, plan AssignContactPlan, resolvedContact *inventree.Contact) (*mcp.CallToolResult, AssignContactOutput, error) {
	if store == nil {
		return nil, AssignContactOutput{}, errors.New("contact confirmation store is unavailable")
	}
	token, err := store.issue(ctx, plan)
	if err != nil {
		return nil, AssignContactOutput{}, err
	}
	question := "Assign this exact contact to the purchase order?"
	if plan.NewContactID == nil {
		question = "Clear the current contact from this exact purchase order?"
	}
	clarification := NewClarification(question, "confirmation", "review the exact current purchase order and contact state, then provide confirm:true with this plan_hash", "plan_hash", false, nil, map[string]any{"purchase_order_id": plan.PurchaseOrderID, "contact_id": plan.NewContactID, "confirm": true, "plan_hash": token})
	return TextResult(StatusClarificationRequired), AssignContactOutput{Status: StatusClarificationRequired, PurchaseOrderID: plan.PurchaseOrderID, ContactID: plan.NewContactID, Contact: resolvedContact, Plan: &plan, PlanHash: token, Clarification: &clarification}, nil
}

func contactPlanClarification(plan AssignContactPlan, reason string) (*mcp.CallToolResult, AssignContactOutput, error) {
	clarification := NewClarification("Which current contact plan should authorize this change?", "confirmation", reason, "plan_hash", false, nil, map[string]any{"purchase_order_id": plan.PurchaseOrderID})
	return TextResult(StatusClarificationRequired), AssignContactOutput{Status: StatusClarificationRequired, PurchaseOrderID: plan.PurchaseOrderID, Plan: &plan, Clarification: &clarification}, nil
}

func contactAssignmentValidation(message string) (*mcp.CallToolResult, AssignContactOutput, error) {
	return TextResult(StatusValidationFailed), AssignContactOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "contact", Messages: []string{message}}}}}, nil
}

func safeContactAssignmentError(operation string) error {
	return fmt.Errorf("%s failed; inspect InvenTree availability and permissions before retrying", operation)
}
