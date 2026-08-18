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
	addressPlanLifetime               = 5 * time.Minute
	addressPlanMaxEntries             = 4096
	addressPlanMaxEntriesPerPrincipal = 64
)

// AddressLookupClient is the narrow client surface needed for bounded,
// company-scoped address discovery.
type AddressLookupClient interface {
	SearchAddressesPage(context.Context, inventree.AddressQuery) (inventree.Page[inventree.Address], error)
	GetAddress(context.Context, int) (inventree.Address, error)
}

type SearchAddressesInput struct {
	CompanyID int    `json:"company_id" jsonschema:"Stable company primary key. Address search is always scoped to one company."`
	Search    string `json:"search,omitempty" jsonschema:"Optional narrowing search text matched against the address title."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset    int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

// AddressView projects inventree.Address for MCP output. It exists
// separately from the client-layer Address type solely to apply the same
// F-S39 credential-rejecting external-URL policy to Link that every other
// MCP-projected URL field uses: an invalid or credentialed value is omitted
// rather than passed through raw from InvenTree.
type AddressView struct {
	PK                    int    `json:"pk"`
	Company               int    `json:"company"`
	Title                 string `json:"title"`
	Primary               bool   `json:"primary"`
	PostalCity            string `json:"postal_city"`
	Province              string `json:"province"`
	Country               string `json:"country"`
	ShippingNotes         string `json:"shipping_notes"`
	InternalShippingNotes string `json:"internal_shipping_notes"`
	Link                  string `json:"link,omitempty"`
}

func addressView(record inventree.Address) AddressView {
	return AddressView{
		PK: record.PK, Company: record.Company, Title: record.Title, Primary: record.Primary,
		PostalCity: record.PostalCity, Province: record.Province, Country: record.Country,
		ShippingNotes: record.ShippingNotes, InternalShippingNotes: record.InternalShippingNotes,
		Link: projectExternalURL(stringPointer(record.Link)),
	}
}

func registerAddressLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchAddressesToolName, "Search addresses",
		"Searches one company's structured address records. Never returns street-address lines or postal code.", searchAddresses(deps))
	addReadOnlyTool(server, deps, GetAddressToolName, "Get address",
		"Retrieves one structured company address by stable ID. Never returns street-address lines or postal code.", getAddress(deps))
}

func searchAddresses(deps Dependencies) mcp.ToolHandlerFor[SearchAddressesInput, LookupOutput[AddressView]] {
	return LookupHandler[AddressLookupClient, SearchAddressesInput, LookupOutput[AddressView]](deps, SearchAddressesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AddressLookupClient, input SearchAddressesInput) (*mcp.CallToolResult, LookupOutput[AddressView], error) {
			if input.CompanyID <= 0 {
				return nil, LookupOutput[AddressView]{}, errors.New("company_id is required to bound address search")
			}
			page, err := client.SearchAddressesPage(ctx, inventree.AddressQuery{
				CompanyID: input.CompanyID, Search: input.Search, Limit: NormalizeLookupLimit(input.Limit), Offset: input.Offset,
			})
			if err != nil {
				return nil, LookupOutput[AddressView]{}, err
			}
			views := make([]AddressView, 0, len(page.Results))
			for _, record := range page.Results {
				views = append(views, addressView(record))
			}
			return listOutput(views, nil)
		})
}

func getAddress(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[AddressView]] {
	return LookupHandler[AddressLookupClient, IDInput, RecordOutput[AddressView]](deps, GetAddressToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AddressLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[AddressView], error) {
			record, err := client.GetAddress(ctx, input.ID)
			if err != nil {
				return recordOutput(AddressView{}, err)
			}
			if record.PK != input.ID {
				return nil, RecordOutput[AddressView]{}, errors.New("InvenTree returned a mismatched address identity")
			}
			return recordOutput(addressView(record), nil)
		})
}

// AssignAddressClient is the narrow client surface needed to preview,
// validate, and execute guarded purchase-order address replacement/clearing.
// Address assignment is purchase-order-specific: API 530 only exposes a
// structured `address` FK on PurchaseOrder among already-supported objects.
type AssignAddressClient interface {
	GetPurchaseOrderDetail(context.Context, int) (inventree.PurchaseOrderDetail, error)
	UpdatePurchaseOrderDetail(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrderDetail, error)
	GetAddress(context.Context, int) (inventree.Address, error)
}

type AssignAddressInput struct {
	PurchaseOrderID int    `json:"purchase_order_id" jsonschema:"Stable purchase-order primary key."`
	AddressID       *int   `json:"address_id,omitempty" jsonschema:"New address primary key from search_addresses/get_address, belonging to the order's supplier company. Omit to clear the current address."`
	Confirm         bool   `json:"confirm,omitempty" jsonschema:"Confirm the exact previewed address assignment or clear."`
	PlanHash        string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

// AssignAddressPlan is the state bound into the confirmation token. Binding
// CompanyID means a fresh preview is required whenever the order's supplier
// changed since the last preview, and binding CurrentAddressID means a fresh
// preview is required whenever the order's address changed since the last
// preview, so a confirm can never silently replace a different address or
// apply to a different supplier context than the one actually reviewed.
type AssignAddressPlan struct {
	Action           string `json:"action"`
	PurchaseOrderID  int    `json:"purchase_order_id"`
	CompanyID        int    `json:"company_id"`
	CurrentAddressID *int   `json:"current_address_id,omitempty"`
	NewAddressID     *int   `json:"new_address_id,omitempty"`
}

type AssignAddressOutput struct {
	Status          string                 `json:"status"`
	PurchaseOrderID int                    `json:"purchase_order_id,omitempty"`
	AddressID       *int                   `json:"address_id,omitempty"`
	Address         *AddressView           `json:"address,omitempty"`
	Plan            *AssignAddressPlan     `json:"plan,omitempty"`
	PlanHash        string                 `json:"plan_hash,omitempty"`
	Verified        bool                   `json:"verified,omitempty"`
	Recovered       bool                   `json:"recovered,omitempty"`
	Validation      *ValidationFailure     `json:"validation,omitempty"`
	Clarification   *ClarificationResponse `json:"clarification,omitempty"`
	RecoveryPlan    string                 `json:"recovery_plan,omitempty"`
}

type addressConfirmation struct {
	digest          string
	principal       string
	purchaseOrderID int
	expiresAt       time.Time
}

type addressPlanStore struct {
	mu                     sync.Mutex
	entries                map[string]addressConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newAddressPlanStore(now func() time.Time, token func() (string, error)) *addressPlanStore {
	return &addressPlanStore{
		entries: map[string]addressConfirmation{}, now: now, token: token, principal: stockPlanPrincipal,
		maxEntries: addressPlanMaxEntries, maxEntriesPerPrincipal: addressPlanMaxEntriesPerPrincipal,
	}
}

func (s *addressPlanStore) issue(ctx context.Context, plan AssignAddressPlan) (string, error) {
	digest, err := addressPlanDigest(plan)
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
		return "", errors.New("too many outstanding address confirmation plans")
	}
	s.entries[token] = addressConfirmation{digest: digest, principal: principal, purchaseOrderID: plan.PurchaseOrderID, expiresAt: now.Add(addressPlanLifetime)}
	return token, nil
}

func (s *addressPlanStore) consume(ctx context.Context, token string, plan AssignAddressPlan) bool {
	digest, err := addressPlanDigest(plan)
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

func addressPlanDigest(plan AssignAddressPlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerAddressWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.addressPlanStore == nil {
		deps.addressPlanStore = newAddressPlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, AssignAddressToolName, "Assign or clear purchase-order address",
		"Previews and confirms assigning or clearing the company address on a purchase order.",
		assignAddress(deps))
}

func assignAddress(deps Dependencies) mcp.ToolHandlerFor[AssignAddressInput, AssignAddressOutput] {
	return LookupHandler[AssignAddressClient, AssignAddressInput, AssignAddressOutput](deps, AssignAddressToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AssignAddressClient, input AssignAddressInput) (*mcp.CallToolResult, AssignAddressOutput, error) {
			if input.PurchaseOrderID <= 0 {
				return addressAssignmentValidation("purchase_order_id must be a positive stable primary key")
			}
			order, err := client.GetPurchaseOrderDetail(ctx, input.PurchaseOrderID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), AssignAddressOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, AssignAddressOutput{}, safeAddressAssignmentError("purchase order lookup")
			}
			if order.PK != input.PurchaseOrderID {
				return nil, AssignAddressOutput{}, errors.New("purchase order returned a mismatched identity")
			}
			currentAddressID := order.Address
			if input.AddressID == nil && currentAddressID == nil {
				return addressAssignmentValidation("the purchase order has no address to clear")
			}
			if input.AddressID != nil && currentAddressID != nil && *input.AddressID == *currentAddressID {
				return addressAssignmentValidation("the requested address_id already matches the current address")
			}
			var resolvedAddress *AddressView
			if input.AddressID != nil {
				record, addressErr := client.GetAddress(ctx, *input.AddressID)
				if isNotFound(addressErr) {
					return addressAssignmentValidation(fmt.Sprintf("address_id %d does not identify a readable address", *input.AddressID))
				}
				if addressErr != nil {
					return nil, AssignAddressOutput{}, safeAddressAssignmentError("address lookup")
				}
				if record.PK != *input.AddressID {
					return addressAssignmentValidation(fmt.Sprintf("address_id %d returned a mismatched identity", *input.AddressID))
				}
				if record.Company != order.Supplier {
					return addressAssignmentValidation(fmt.Sprintf("address_id %d belongs to a different company than the purchase order's supplier", *input.AddressID))
				}
				view := addressView(record)
				resolvedAddress = &view
			}
			action := "assign_address"
			if input.AddressID == nil {
				action = "clear_address"
			}
			plan := AssignAddressPlan{Action: action, PurchaseOrderID: input.PurchaseOrderID, CompanyID: order.Supplier, CurrentAddressID: currentAddressID, NewAddressID: input.AddressID}
			if !input.Confirm {
				return issueAddressPlan(ctx, deps.addressPlanStore, plan, resolvedAddress)
			}
			if deps.addressPlanStore == nil || input.PlanHash == "" || !deps.addressPlanStore.consume(ctx, input.PlanHash, plan) {
				return addressPlanClarification(plan, "The plan is missing, stale, expired, reused, or belongs to another principal; preview the current purchase order again.")
			}
			mutationErr := patchPurchaseOrderAddress(ctx, client, input.PurchaseOrderID, input.AddressID)
			return verifyAddressAssignment(ctx, client, input.PurchaseOrderID, input.AddressID, resolvedAddress, mutationErr)
		})
}

func patchPurchaseOrderAddress(ctx context.Context, client AssignAddressClient, orderID int, addressID *int) error {
	fields := inventree.PatchFields{}
	if addressID != nil {
		fields["address"] = inventree.Set(*addressID)
	} else {
		fields["address"] = inventree.Null()
	}
	_, err := client.UpdatePurchaseOrderDetail(ctx, orderID, fields)
	return err
}

func verifyAddressAssignment(ctx context.Context, client AssignAddressClient, orderID int, requestedAddressID *int, resolvedAddress *AddressView, mutationErr error) (*mcp.CallToolResult, AssignAddressOutput, error) {
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, AssignAddressOutput{}, errors.New("address assignment was rejected")
		}
	}
	order, err := client.GetPurchaseOrderDetail(ctx, orderID)
	if err != nil || !sameOptionalInt(order.Address, requestedAddressID) {
		return TextResult(StatusPartialFailure), AssignAddressOutput{Status: StatusPartialFailure, PurchaseOrderID: orderID, RecoveryPlan: "Read the purchase order again before retrying; the address change could not be verified."}, nil
	}
	return TextResult(StatusOK), AssignAddressOutput{Status: StatusOK, PurchaseOrderID: orderID, AddressID: order.Address, Address: resolvedAddress, Verified: true, Recovered: mutationErr != nil}, nil
}

func issueAddressPlan(ctx context.Context, store *addressPlanStore, plan AssignAddressPlan, resolvedAddress *AddressView) (*mcp.CallToolResult, AssignAddressOutput, error) {
	if store == nil {
		return nil, AssignAddressOutput{}, errors.New("address confirmation store is unavailable")
	}
	token, err := store.issue(ctx, plan)
	if err != nil {
		return nil, AssignAddressOutput{}, err
	}
	question := "Assign this exact address to the purchase order?"
	if plan.NewAddressID == nil {
		question = "Clear the current address from this exact purchase order?"
	}
	clarification := NewClarification(question, "confirmation", "review the exact current purchase order and address state, then provide confirm:true with this plan_hash", "plan_hash", false, nil, map[string]any{"purchase_order_id": plan.PurchaseOrderID, "address_id": plan.NewAddressID, "confirm": true, "plan_hash": token})
	return TextResult(StatusClarificationRequired), AssignAddressOutput{Status: StatusClarificationRequired, PurchaseOrderID: plan.PurchaseOrderID, AddressID: plan.NewAddressID, Address: resolvedAddress, Plan: &plan, PlanHash: token, Clarification: &clarification}, nil
}

func addressPlanClarification(plan AssignAddressPlan, reason string) (*mcp.CallToolResult, AssignAddressOutput, error) {
	clarification := NewClarification("Which current address plan should authorize this change?", "confirmation", reason, "plan_hash", false, nil, map[string]any{"purchase_order_id": plan.PurchaseOrderID})
	return TextResult(StatusClarificationRequired), AssignAddressOutput{Status: StatusClarificationRequired, PurchaseOrderID: plan.PurchaseOrderID, Plan: &plan, Clarification: &clarification}, nil
}

func addressAssignmentValidation(message string) (*mcp.CallToolResult, AssignAddressOutput, error) {
	return TextResult(StatusValidationFailed), AssignAddressOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "address", Messages: []string{message}}}}}, nil
}

func safeAddressAssignmentError(operation string) error {
	return fmt.Errorf("%s failed; inspect InvenTree availability and permissions before retrying", operation)
}
