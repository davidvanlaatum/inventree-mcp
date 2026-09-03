package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	priceBreakPageSize    = 100
	priceBreakMaxRequests = 20
	priceBreakMaxRecords  = 1000

	priceBreakQuantityEpsilon = 1e-9
)

// partPricingRefreshPollInterval and partPricingRefreshTimeout are vars
// rather than consts so tests can shrink the bounded poll instead of
// actually waiting on it (see internal/testenv's workerHealthProbeTimeout
// for the same pattern).
var (
	partPricingRefreshPollInterval = 500 * time.Millisecond
	partPricingRefreshTimeout      = 30 * time.Second
)

type InternalPriceBreakLookupClient interface {
	SearchPartInternalPriceBreaksPage(context.Context, inventree.PartInternalPriceBreakQuery) (inventree.PartInternalPriceBreakPage, error)
	GetPartInternalPriceBreak(context.Context, int) (inventree.PartInternalPriceBreak, error)
}

type InternalPriceBreakWriteClient interface {
	InternalPriceBreakLookupClient
	CreatePartInternalPriceBreak(context.Context, inventree.PartInternalPriceBreakCreate) (inventree.PartInternalPriceBreak, error)
	UpdatePartInternalPriceBreak(context.Context, int, inventree.PatchFields) (inventree.PartInternalPriceBreak, error)
	DeletePartInternalPriceBreak(context.Context, int) error
}

type SalePriceBreakLookupClient interface {
	SearchPartSalePriceBreaksPage(context.Context, inventree.PartSalePriceBreakQuery) (inventree.PartSalePriceBreakPage, error)
	GetPartSalePriceBreak(context.Context, int) (inventree.PartSalePriceBreak, error)
}

type SalePriceBreakWriteClient interface {
	SalePriceBreakLookupClient
	GetPart(context.Context, int) (inventree.Part, error)
	CreatePartSalePriceBreak(context.Context, inventree.PartSalePriceBreakCreate) (inventree.PartSalePriceBreak, error)
	UpdatePartSalePriceBreak(context.Context, int, inventree.PatchFields) (inventree.PartSalePriceBreak, error)
	DeletePartSalePriceBreak(context.Context, int) error
}

type SupplierPriceBreakLookupClient interface {
	SearchSupplierPriceBreaksPage(context.Context, inventree.SupplierPriceBreakQuery) (inventree.SupplierPriceBreakPage, error)
	GetSupplierPriceBreak(context.Context, int) (inventree.SupplierPriceBreak, error)
}

type SupplierPriceBreakWriteClient interface {
	SupplierPriceBreakLookupClient
	CreateSupplierPriceBreak(context.Context, inventree.SupplierPriceBreakCreate) (inventree.SupplierPriceBreak, error)
	UpdateSupplierPriceBreak(context.Context, int, inventree.PatchFields) (inventree.SupplierPriceBreak, error)
	DeleteSupplierPriceBreak(context.Context, int) error
}

type PartPricingLookupClient interface {
	GetPartPricing(context.Context, int) (inventree.PartPricing, error)
}

type PartPricingWriteClient interface {
	GetPartPricing(context.Context, int) (inventree.PartPricing, error)
	UpdatePartPricing(context.Context, int, inventree.PatchFields) (inventree.PartPricing, error)
	RefreshPartPricing(context.Context, int) (inventree.PartPricing, error)
}

// PriceBreakResult is the concise, family-agnostic projection returned by
// every price-break search/mutation tool: a stable row ID, the quantity
// break threshold, and its price. Supplier price breaks additionally carry
// SupplierID and Updated, both zero-valued/omitted for internal and sale
// price breaks.
type PriceBreakResult struct {
	PK            int     `json:"pk"`
	Quantity      float64 `json:"quantity"`
	Price         string  `json:"price"`
	PriceCurrency string  `json:"price_currency"`
	SupplierID    int     `json:"supplier_id,omitempty"`
	Updated       string  `json:"updated,omitempty"`
}

func internalPriceBreakResult(record inventree.PartInternalPriceBreak) PriceBreakResult {
	return PriceBreakResult{PK: record.PK, Quantity: record.Quantity, Price: string(record.Price), PriceCurrency: record.PriceCurrency}
}

func salePriceBreakResult(record inventree.PartSalePriceBreak) PriceBreakResult {
	return PriceBreakResult{PK: record.PK, Quantity: record.Quantity, Price: string(record.Price), PriceCurrency: record.PriceCurrency}
}

func supplierPriceBreakResult(record inventree.SupplierPriceBreak) PriceBreakResult {
	updated := ""
	if record.Updated != nil {
		updated = *record.Updated
	}
	return PriceBreakResult{PK: record.PK, Quantity: record.Quantity, Price: string(record.Price), PriceCurrency: record.PriceCurrency, SupplierID: record.Supplier, Updated: updated}
}

type PriceBreakMutationOutput struct {
	Status        string                 `json:"status"`
	Record        *PriceBreakResult      `json:"record,omitempty"`
	Candidates    []PriceBreakResult     `json:"candidates,omitempty"`
	Validation    *ValidationFailure     `json:"validation,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
}

func priceBreakValidation(message string) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
	return TextResult(StatusValidationFailed), PriceBreakMutationOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "price_break", Messages: []string{message}}}}}, nil
}

func priceBreakDuplicate(existing PriceBreakResult) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
	clarification := NewClarification("Should the existing price-break row at this quantity be updated instead?", "quantity", "InvenTree enforces one row per (part, quantity) pair", "id", false, nil, map[string]any{"id": existing.PK})
	return TextResult(StatusClarificationRequired), PriceBreakMutationOutput{Status: StatusClarificationRequired, Candidates: []PriceBreakResult{existing}, Clarification: &clarification}, nil
}

func priceBreakMutationRejected(err error) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
	if validation, ok := safeValidationFailure(err); ok {
		return TextResult(StatusValidationFailed), PriceBreakMutationOutput{Status: StatusValidationFailed, Validation: validation}, nil
	}
	return nil, PriceBreakMutationOutput{}, err
}

func validatePriceBreakFields(id int, quantity float64, price, priceCurrency string) error {
	if id <= 0 {
		return errors.New("a positive part or supplier-part id is required")
	}
	if quantity <= 0 {
		return errors.New("quantity must be positive")
	}
	if strings.TrimSpace(price) == "" {
		return errors.New("price is required")
	}
	if _, err := strconv.ParseFloat(price, 64); err != nil {
		return errors.New("price must be a valid decimal string")
	}
	if strings.TrimSpace(priceCurrency) == "" {
		return errors.New("price_currency is required")
	}
	return nil
}

func quantityMatches(a, b float64) bool { return math.Abs(a-b) < priceBreakQuantityEpsilon }

// --- Internal price breaks ---

type SearchInternalPriceBreaksInput struct {
	PartID int `json:"part_id" jsonschema:"Stable part primary key."`
	Limit  int `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset int `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type CreateInternalPriceBreakInput struct {
	PartID        int     `json:"part_id" jsonschema:"Stable part primary key."`
	Quantity      float64 `json:"quantity" jsonschema:"Quantity break threshold; must be positive."`
	Price         string  `json:"price" jsonschema:"Exact decimal price string, e.g. 12.50. Must be zero or positive."`
	PriceCurrency string  `json:"price_currency" jsonschema:"Three-letter currency code, e.g. USD."`
}

type UpdateInternalPriceBreakInput struct {
	ID            int     `json:"id" jsonschema:"Stable internal-price-break row ID."`
	Price         *string `json:"price,omitempty" jsonschema:"Replacement decimal price string. Provide price, price_currency, or both."`
	PriceCurrency *string `json:"price_currency,omitempty" jsonschema:"Replacement three-letter currency code. Provide price, price_currency, or both."`
}

type DeleteInternalPriceBreakInput struct {
	ID      int  `json:"id" jsonschema:"Stable internal-price-break row ID."`
	Confirm bool `json:"confirm,omitempty" jsonschema:"Required true before deleting the selected row."`
}

func searchInternalPriceBreaks(deps Dependencies) mcp.ToolHandlerFor[SearchInternalPriceBreaksInput, LookupOutput[PriceBreakResult]] {
	return LookupHandler[InternalPriceBreakLookupClient, SearchInternalPriceBreaksInput, LookupOutput[PriceBreakResult]](deps, SearchInternalPriceBreaksToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client InternalPriceBreakLookupClient, input SearchInternalPriceBreaksInput) (*mcp.CallToolResult, LookupOutput[PriceBreakResult], error) {
			if input.PartID <= 0 {
				return priceBreakSearchClarification[PriceBreakResult]("Which part's internal price breaks should be searched?", "part_id", "part_id must be a positive part primary key")
			}
			limit := NormalizeLookupLimit(input.Limit)
			records, err := loadInternalPriceBreaks(ctx, client, input.PartID)
			if err != nil {
				return nil, LookupOutput[PriceBreakResult]{}, err
			}
			results := make([]PriceBreakResult, len(records))
			for i, record := range records {
				results[i] = internalPriceBreakResult(record)
			}
			return listOutput(paginatePriceBreaks(results, input.Offset, limit), nil)
		})
}

func loadInternalPriceBreaks(ctx context.Context, client InternalPriceBreakLookupClient, partID int) ([]inventree.PartInternalPriceBreak, error) {
	result := []inventree.PartInternalPriceBreak{}
	for offset, requests := 0, 0; ; offset, requests = offset+priceBreakPageSize, requests+1 {
		if requests >= priceBreakMaxRequests || len(result) >= priceBreakMaxRecords {
			return nil, errors.New("internal price break list exceeded the bounded request or record budget")
		}
		page, err := client.SearchPartInternalPriceBreaksPage(ctx, inventree.PartInternalPriceBreakQuery{Part: partID, Limit: priceBreakPageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		result = append(result, page.Results...)
		if !page.HasMore {
			return result, nil
		}
	}
}

func findInternalPriceBreakByQuantity(ctx context.Context, client InternalPriceBreakLookupClient, partID int, quantity float64) (*inventree.PartInternalPriceBreak, error) {
	records, err := loadInternalPriceBreaks(ctx, client, partID)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if quantityMatches(record.Quantity, quantity) {
			return &record, nil
		}
	}
	return nil, nil
}

func createInternalPriceBreak(deps Dependencies) mcp.ToolHandlerFor[CreateInternalPriceBreakInput, PriceBreakMutationOutput] {
	return LookupHandler[InternalPriceBreakWriteClient, CreateInternalPriceBreakInput, PriceBreakMutationOutput](deps, CreateInternalPriceBreakToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client InternalPriceBreakWriteClient, input CreateInternalPriceBreakInput) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
			if err := validatePriceBreakFields(input.PartID, input.Quantity, input.Price, input.PriceCurrency); err != nil {
				return priceBreakValidation(err.Error())
			}
			existing, err := findInternalPriceBreakByQuantity(ctx, client, input.PartID, input.Quantity)
			if err != nil {
				return nil, PriceBreakMutationOutput{}, err
			}
			if existing != nil {
				return priceBreakDuplicate(internalPriceBreakResult(*existing))
			}
			created, mutationErr := client.CreatePartInternalPriceBreak(ctx, inventree.PartInternalPriceBreakCreate{Part: input.PartID, Quantity: input.Quantity, Price: input.Price, PriceCurrency: input.PriceCurrency})
			if mutationErr != nil {
				return priceBreakMutationRejected(mutationErr)
			}
			result := internalPriceBreakResult(created)
			return TextResult(StatusOK), PriceBreakMutationOutput{Status: StatusOK, Record: &result}, nil
		})
}

func updateInternalPriceBreak(deps Dependencies) mcp.ToolHandlerFor[UpdateInternalPriceBreakInput, PriceBreakMutationOutput] {
	return LookupHandler[InternalPriceBreakWriteClient, UpdateInternalPriceBreakInput, PriceBreakMutationOutput](deps, UpdateInternalPriceBreakToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client InternalPriceBreakWriteClient, input UpdateInternalPriceBreakInput) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
			if input.ID <= 0 || (input.Price == nil && input.PriceCurrency == nil) {
				return priceBreakValidation("provide one positive id and at least one of price or price_currency")
			}
			if input.Price != nil {
				if _, parseErr := strconv.ParseFloat(*input.Price, 64); parseErr != nil {
					return priceBreakValidation("price must be a valid decimal string")
				}
			}
			existing, err := client.GetPartInternalPriceBreak(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PriceBreakMutationOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, PriceBreakMutationOutput{}, err
			}
			if existing.PK != input.ID {
				return nil, PriceBreakMutationOutput{}, fmt.Errorf("internal price break identity mismatch: requested %d, received %d", input.ID, existing.PK)
			}
			fields := inventree.PatchFields{}
			if input.Price != nil {
				fields["price"] = inventree.Set(*input.Price)
			}
			if input.PriceCurrency != nil {
				fields["price_currency"] = inventree.Set(*input.PriceCurrency)
			}
			updated, mutationErr := client.UpdatePartInternalPriceBreak(ctx, input.ID, fields)
			if mutationErr != nil {
				return priceBreakMutationRejected(mutationErr)
			}
			result := internalPriceBreakResult(updated)
			return TextResult(StatusOK), PriceBreakMutationOutput{Status: StatusOK, Record: &result}, nil
		})
}

func deleteInternalPriceBreak(deps Dependencies) mcp.ToolHandlerFor[DeleteInternalPriceBreakInput, PriceBreakMutationOutput] {
	return LookupHandler[InternalPriceBreakWriteClient, DeleteInternalPriceBreakInput, PriceBreakMutationOutput](deps, DeleteInternalPriceBreakToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client InternalPriceBreakWriteClient, input DeleteInternalPriceBreakInput) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
			if input.ID <= 0 {
				return priceBreakValidation("id must be a positive stable row ID")
			}
			existing, err := client.GetPartInternalPriceBreak(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PriceBreakMutationOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, PriceBreakMutationOutput{}, err
			}
			if existing.PK != input.ID {
				return nil, PriceBreakMutationOutput{}, fmt.Errorf("internal price break identity mismatch: requested %d, received %d", input.ID, existing.PK)
			}
			result := internalPriceBreakResult(existing)
			if !input.Confirm {
				clarification := NewClarification("Delete this exact internal price break row?", "confirm", "delete_internal_price_break requires confirm:true after reviewing the stable row ID", "confirm", false, nil, map[string]any{"id": input.ID, "confirm": true})
				return TextResult(StatusClarificationRequired), PriceBreakMutationOutput{Status: StatusClarificationRequired, Record: &result, Clarification: &clarification}, nil
			}
			if mutationErr := client.DeletePartInternalPriceBreak(ctx, input.ID); mutationErr != nil {
				return priceBreakMutationRejected(mutationErr)
			}
			if _, readErr := client.GetPartInternalPriceBreak(ctx, input.ID); !isNotFound(readErr) {
				if readErr == nil {
					return nil, PriceBreakMutationOutput{}, fmt.Errorf("internal price break %d still exists after deletion", input.ID)
				}
				return nil, PriceBreakMutationOutput{}, fmt.Errorf("verify internal price break %d deletion: %w", input.ID, readErr)
			}
			return TextResult(StatusOK), PriceBreakMutationOutput{Status: StatusOK, Record: &result}, nil
		})
}

// --- Sale price breaks ---

type SearchSalePriceBreaksInput struct {
	PartID int `json:"part_id" jsonschema:"Stable part primary key."`
	Limit  int `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset int `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type CreateSalePriceBreakInput struct {
	PartID        int     `json:"part_id" jsonschema:"Stable part primary key. The part must already have salable:true; use update_part to set it first."`
	Quantity      float64 `json:"quantity" jsonschema:"Quantity break threshold; must be positive."`
	Price         string  `json:"price" jsonschema:"Exact decimal price string, e.g. 12.50. Must be zero or positive."`
	PriceCurrency string  `json:"price_currency" jsonschema:"Three-letter currency code, e.g. USD."`
}

type UpdateSalePriceBreakInput struct {
	ID            int     `json:"id" jsonschema:"Stable sale-price-break row ID."`
	Price         *string `json:"price,omitempty" jsonschema:"Replacement decimal price string. Provide price, price_currency, or both."`
	PriceCurrency *string `json:"price_currency,omitempty" jsonschema:"Replacement three-letter currency code. Provide price, price_currency, or both."`
}

type DeleteSalePriceBreakInput struct {
	ID      int  `json:"id" jsonschema:"Stable sale-price-break row ID."`
	Confirm bool `json:"confirm,omitempty" jsonschema:"Required true before deleting the selected row."`
}

func searchSalePriceBreaks(deps Dependencies) mcp.ToolHandlerFor[SearchSalePriceBreaksInput, LookupOutput[PriceBreakResult]] {
	return LookupHandler[SalePriceBreakLookupClient, SearchSalePriceBreaksInput, LookupOutput[PriceBreakResult]](deps, SearchSalePriceBreaksToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client SalePriceBreakLookupClient, input SearchSalePriceBreaksInput) (*mcp.CallToolResult, LookupOutput[PriceBreakResult], error) {
			if input.PartID <= 0 {
				return priceBreakSearchClarification[PriceBreakResult]("Which part's sale price breaks should be searched?", "part_id", "part_id must be a positive part primary key")
			}
			limit := NormalizeLookupLimit(input.Limit)
			records, err := loadSalePriceBreaks(ctx, client, input.PartID)
			if err != nil {
				return nil, LookupOutput[PriceBreakResult]{}, err
			}
			results := make([]PriceBreakResult, len(records))
			for i, record := range records {
				results[i] = salePriceBreakResult(record)
			}
			return listOutput(paginatePriceBreaks(results, input.Offset, limit), nil)
		})
}

func loadSalePriceBreaks(ctx context.Context, client SalePriceBreakLookupClient, partID int) ([]inventree.PartSalePriceBreak, error) {
	result := []inventree.PartSalePriceBreak{}
	for offset, requests := 0, 0; ; offset, requests = offset+priceBreakPageSize, requests+1 {
		if requests >= priceBreakMaxRequests || len(result) >= priceBreakMaxRecords {
			return nil, errors.New("sale price break list exceeded the bounded request or record budget")
		}
		page, err := client.SearchPartSalePriceBreaksPage(ctx, inventree.PartSalePriceBreakQuery{Part: partID, Limit: priceBreakPageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		result = append(result, page.Results...)
		if !page.HasMore {
			return result, nil
		}
	}
}

func findSalePriceBreakByQuantity(ctx context.Context, client SalePriceBreakLookupClient, partID int, quantity float64) (*inventree.PartSalePriceBreak, error) {
	records, err := loadSalePriceBreaks(ctx, client, partID)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if quantityMatches(record.Quantity, quantity) {
			return &record, nil
		}
	}
	return nil, nil
}

func createSalePriceBreak(deps Dependencies) mcp.ToolHandlerFor[CreateSalePriceBreakInput, PriceBreakMutationOutput] {
	return LookupHandler[SalePriceBreakWriteClient, CreateSalePriceBreakInput, PriceBreakMutationOutput](deps, CreateSalePriceBreakToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client SalePriceBreakWriteClient, input CreateSalePriceBreakInput) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
			if err := validatePriceBreakFields(input.PartID, input.Quantity, input.Price, input.PriceCurrency); err != nil {
				return priceBreakValidation(err.Error())
			}
			part, err := client.GetPart(ctx, input.PartID)
			if isNotFound(err) {
				return priceBreakValidation("part_id does not refer to an existing part")
			}
			if err != nil {
				return nil, PriceBreakMutationOutput{}, err
			}
			if !part.Salable {
				return priceBreakValidation("the part must have salable:true before a sale price break can be created; use update_part to set it first")
			}
			existing, err := findSalePriceBreakByQuantity(ctx, client, input.PartID, input.Quantity)
			if err != nil {
				return nil, PriceBreakMutationOutput{}, err
			}
			if existing != nil {
				return priceBreakDuplicate(salePriceBreakResult(*existing))
			}
			created, mutationErr := client.CreatePartSalePriceBreak(ctx, inventree.PartSalePriceBreakCreate{Part: input.PartID, Quantity: input.Quantity, Price: input.Price, PriceCurrency: input.PriceCurrency})
			if mutationErr != nil {
				return priceBreakMutationRejected(mutationErr)
			}
			result := salePriceBreakResult(created)
			return TextResult(StatusOK), PriceBreakMutationOutput{Status: StatusOK, Record: &result}, nil
		})
}

func updateSalePriceBreak(deps Dependencies) mcp.ToolHandlerFor[UpdateSalePriceBreakInput, PriceBreakMutationOutput] {
	return LookupHandler[SalePriceBreakWriteClient, UpdateSalePriceBreakInput, PriceBreakMutationOutput](deps, UpdateSalePriceBreakToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client SalePriceBreakWriteClient, input UpdateSalePriceBreakInput) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
			if input.ID <= 0 || (input.Price == nil && input.PriceCurrency == nil) {
				return priceBreakValidation("provide one positive id and at least one of price or price_currency")
			}
			if input.Price != nil {
				if _, parseErr := strconv.ParseFloat(*input.Price, 64); parseErr != nil {
					return priceBreakValidation("price must be a valid decimal string")
				}
			}
			existing, err := client.GetPartSalePriceBreak(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PriceBreakMutationOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, PriceBreakMutationOutput{}, err
			}
			if existing.PK != input.ID {
				return nil, PriceBreakMutationOutput{}, fmt.Errorf("sale price break identity mismatch: requested %d, received %d", input.ID, existing.PK)
			}
			fields := inventree.PatchFields{}
			if input.Price != nil {
				fields["price"] = inventree.Set(*input.Price)
			}
			if input.PriceCurrency != nil {
				fields["price_currency"] = inventree.Set(*input.PriceCurrency)
			}
			updated, mutationErr := client.UpdatePartSalePriceBreak(ctx, input.ID, fields)
			if mutationErr != nil {
				return priceBreakMutationRejected(mutationErr)
			}
			result := salePriceBreakResult(updated)
			return TextResult(StatusOK), PriceBreakMutationOutput{Status: StatusOK, Record: &result}, nil
		})
}

func deleteSalePriceBreak(deps Dependencies) mcp.ToolHandlerFor[DeleteSalePriceBreakInput, PriceBreakMutationOutput] {
	return LookupHandler[SalePriceBreakWriteClient, DeleteSalePriceBreakInput, PriceBreakMutationOutput](deps, DeleteSalePriceBreakToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client SalePriceBreakWriteClient, input DeleteSalePriceBreakInput) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
			if input.ID <= 0 {
				return priceBreakValidation("id must be a positive stable row ID")
			}
			existing, err := client.GetPartSalePriceBreak(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PriceBreakMutationOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, PriceBreakMutationOutput{}, err
			}
			if existing.PK != input.ID {
				return nil, PriceBreakMutationOutput{}, fmt.Errorf("sale price break identity mismatch: requested %d, received %d", input.ID, existing.PK)
			}
			result := salePriceBreakResult(existing)
			if !input.Confirm {
				clarification := NewClarification("Delete this exact sale price break row?", "confirm", "delete_sale_price_break requires confirm:true after reviewing the stable row ID", "confirm", false, nil, map[string]any{"id": input.ID, "confirm": true})
				return TextResult(StatusClarificationRequired), PriceBreakMutationOutput{Status: StatusClarificationRequired, Record: &result, Clarification: &clarification}, nil
			}
			if mutationErr := client.DeletePartSalePriceBreak(ctx, input.ID); mutationErr != nil {
				return priceBreakMutationRejected(mutationErr)
			}
			if _, readErr := client.GetPartSalePriceBreak(ctx, input.ID); !isNotFound(readErr) {
				if readErr == nil {
					return nil, PriceBreakMutationOutput{}, fmt.Errorf("sale price break %d still exists after deletion", input.ID)
				}
				return nil, PriceBreakMutationOutput{}, fmt.Errorf("verify sale price break %d deletion: %w", input.ID, readErr)
			}
			return TextResult(StatusOK), PriceBreakMutationOutput{Status: StatusOK, Record: &result}, nil
		})
}

// --- Supplier price breaks ---

type SearchSupplierPriceBreaksInput struct {
	SupplierPartID int `json:"supplier_part_id" jsonschema:"Stable supplier-part primary key."`
	Limit          int `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset         int `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type CreateSupplierPriceBreakInput struct {
	SupplierPartID int     `json:"supplier_part_id" jsonschema:"Stable supplier-part primary key."`
	Quantity       float64 `json:"quantity" jsonschema:"Quantity break threshold; must be positive."`
	Price          string  `json:"price" jsonschema:"Exact decimal price string, e.g. 12.50. Must be zero or positive."`
	PriceCurrency  string  `json:"price_currency" jsonschema:"Three-letter currency code, e.g. USD."`
}

type UpdateSupplierPriceBreakInput struct {
	ID            int     `json:"id" jsonschema:"Stable supplier-price-break row ID."`
	Price         *string `json:"price,omitempty" jsonschema:"Replacement decimal price string. Provide price, price_currency, or both."`
	PriceCurrency *string `json:"price_currency,omitempty" jsonschema:"Replacement three-letter currency code. Provide price, price_currency, or both."`
}

type DeleteSupplierPriceBreakInput struct {
	ID      int  `json:"id" jsonschema:"Stable supplier-price-break row ID."`
	Confirm bool `json:"confirm,omitempty" jsonschema:"Required true before deleting the selected row."`
}

func searchSupplierPriceBreaks(deps Dependencies) mcp.ToolHandlerFor[SearchSupplierPriceBreaksInput, LookupOutput[PriceBreakResult]] {
	return LookupHandler[SupplierPriceBreakLookupClient, SearchSupplierPriceBreaksInput, LookupOutput[PriceBreakResult]](deps, SearchSupplierPriceBreaksToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client SupplierPriceBreakLookupClient, input SearchSupplierPriceBreaksInput) (*mcp.CallToolResult, LookupOutput[PriceBreakResult], error) {
			if input.SupplierPartID <= 0 {
				return priceBreakSearchClarification[PriceBreakResult]("Which supplier part's price breaks should be searched?", "supplier_part_id", "supplier_part_id must be a positive supplier-part primary key")
			}
			limit := NormalizeLookupLimit(input.Limit)
			records, err := loadSupplierPriceBreaks(ctx, client, input.SupplierPartID)
			if err != nil {
				return nil, LookupOutput[PriceBreakResult]{}, err
			}
			results := make([]PriceBreakResult, len(records))
			for i, record := range records {
				results[i] = supplierPriceBreakResult(record)
			}
			return listOutput(paginatePriceBreaks(results, input.Offset, limit), nil)
		})
}

func loadSupplierPriceBreaks(ctx context.Context, client SupplierPriceBreakLookupClient, supplierPartID int) ([]inventree.SupplierPriceBreak, error) {
	result := []inventree.SupplierPriceBreak{}
	for offset, requests := 0, 0; ; offset, requests = offset+priceBreakPageSize, requests+1 {
		if requests >= priceBreakMaxRequests || len(result) >= priceBreakMaxRecords {
			return nil, errors.New("supplier price break list exceeded the bounded request or record budget")
		}
		page, err := client.SearchSupplierPriceBreaksPage(ctx, inventree.SupplierPriceBreakQuery{SupplierPart: supplierPartID, Limit: priceBreakPageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		result = append(result, page.Results...)
		if !page.HasMore {
			return result, nil
		}
	}
}

func findSupplierPriceBreakByQuantity(ctx context.Context, client SupplierPriceBreakLookupClient, supplierPartID int, quantity float64) (*inventree.SupplierPriceBreak, error) {
	records, err := loadSupplierPriceBreaks(ctx, client, supplierPartID)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if quantityMatches(record.Quantity, quantity) {
			return &record, nil
		}
	}
	return nil, nil
}

// createSupplierPriceBreak: InvenTree gates SupplierPriceBreak writes under
// purchase_order-family permissions upstream (r:add/change/delete:purchase_order),
// not company or part scopes, per F-S58's discovery findings -- an MCP token
// scoped only for inventree.write can still see this specific family
// rejected if the underlying InvenTree account lacks purchase-order write.
func createSupplierPriceBreak(deps Dependencies) mcp.ToolHandlerFor[CreateSupplierPriceBreakInput, PriceBreakMutationOutput] {
	return LookupHandler[SupplierPriceBreakWriteClient, CreateSupplierPriceBreakInput, PriceBreakMutationOutput](deps, CreateSupplierPriceBreakToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client SupplierPriceBreakWriteClient, input CreateSupplierPriceBreakInput) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
			if err := validatePriceBreakFields(input.SupplierPartID, input.Quantity, input.Price, input.PriceCurrency); err != nil {
				return priceBreakValidation(err.Error())
			}
			existing, err := findSupplierPriceBreakByQuantity(ctx, client, input.SupplierPartID, input.Quantity)
			if err != nil {
				return nil, PriceBreakMutationOutput{}, err
			}
			if existing != nil {
				return priceBreakDuplicate(supplierPriceBreakResult(*existing))
			}
			created, mutationErr := client.CreateSupplierPriceBreak(ctx, inventree.SupplierPriceBreakCreate{SupplierPart: input.SupplierPartID, Quantity: input.Quantity, Price: input.Price, PriceCurrency: input.PriceCurrency})
			if mutationErr != nil {
				return priceBreakMutationRejected(mutationErr)
			}
			result := supplierPriceBreakResult(created)
			return TextResult(StatusOK), PriceBreakMutationOutput{Status: StatusOK, Record: &result}, nil
		})
}

func updateSupplierPriceBreak(deps Dependencies) mcp.ToolHandlerFor[UpdateSupplierPriceBreakInput, PriceBreakMutationOutput] {
	return LookupHandler[SupplierPriceBreakWriteClient, UpdateSupplierPriceBreakInput, PriceBreakMutationOutput](deps, UpdateSupplierPriceBreakToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client SupplierPriceBreakWriteClient, input UpdateSupplierPriceBreakInput) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
			if input.ID <= 0 || (input.Price == nil && input.PriceCurrency == nil) {
				return priceBreakValidation("provide one positive id and at least one of price or price_currency")
			}
			if input.Price != nil {
				if _, parseErr := strconv.ParseFloat(*input.Price, 64); parseErr != nil {
					return priceBreakValidation("price must be a valid decimal string")
				}
			}
			existing, err := client.GetSupplierPriceBreak(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PriceBreakMutationOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, PriceBreakMutationOutput{}, err
			}
			if existing.PK != input.ID {
				return nil, PriceBreakMutationOutput{}, fmt.Errorf("supplier price break identity mismatch: requested %d, received %d", input.ID, existing.PK)
			}
			fields := inventree.PatchFields{}
			if input.Price != nil {
				fields["price"] = inventree.Set(*input.Price)
			}
			if input.PriceCurrency != nil {
				fields["price_currency"] = inventree.Set(*input.PriceCurrency)
			}
			updated, mutationErr := client.UpdateSupplierPriceBreak(ctx, input.ID, fields)
			if mutationErr != nil {
				return priceBreakMutationRejected(mutationErr)
			}
			result := supplierPriceBreakResult(updated)
			return TextResult(StatusOK), PriceBreakMutationOutput{Status: StatusOK, Record: &result}, nil
		})
}

func deleteSupplierPriceBreak(deps Dependencies) mcp.ToolHandlerFor[DeleteSupplierPriceBreakInput, PriceBreakMutationOutput] {
	return LookupHandler[SupplierPriceBreakWriteClient, DeleteSupplierPriceBreakInput, PriceBreakMutationOutput](deps, DeleteSupplierPriceBreakToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client SupplierPriceBreakWriteClient, input DeleteSupplierPriceBreakInput) (*mcp.CallToolResult, PriceBreakMutationOutput, error) {
			if input.ID <= 0 {
				return priceBreakValidation("id must be a positive stable row ID")
			}
			existing, err := client.GetSupplierPriceBreak(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PriceBreakMutationOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, PriceBreakMutationOutput{}, err
			}
			if existing.PK != input.ID {
				return nil, PriceBreakMutationOutput{}, fmt.Errorf("supplier price break identity mismatch: requested %d, received %d", input.ID, existing.PK)
			}
			result := supplierPriceBreakResult(existing)
			if !input.Confirm {
				clarification := NewClarification("Delete this exact supplier price break row?", "confirm", "delete_supplier_price_break requires confirm:true after reviewing the stable row ID", "confirm", false, nil, map[string]any{"id": input.ID, "confirm": true})
				return TextResult(StatusClarificationRequired), PriceBreakMutationOutput{Status: StatusClarificationRequired, Record: &result, Clarification: &clarification}, nil
			}
			if mutationErr := client.DeleteSupplierPriceBreak(ctx, input.ID); mutationErr != nil {
				return priceBreakMutationRejected(mutationErr)
			}
			if _, readErr := client.GetSupplierPriceBreak(ctx, input.ID); !isNotFound(readErr) {
				if readErr == nil {
					return nil, PriceBreakMutationOutput{}, fmt.Errorf("supplier price break %d still exists after deletion", input.ID)
				}
				return nil, PriceBreakMutationOutput{}, fmt.Errorf("verify supplier price break %d deletion: %w", input.ID, readErr)
			}
			return TextResult(StatusOK), PriceBreakMutationOutput{Status: StatusOK, Record: &result}, nil
		})
}

// --- Part pricing ---

type GetPartPricingInput struct {
	PartID int `json:"part_id" jsonschema:"Stable part primary key."`
}

func getPartPricing(deps Dependencies) mcp.ToolHandlerFor[GetPartPricingInput, RecordOutput[inventree.PartPricing]] {
	return LookupHandler[PartPricingLookupClient, GetPartPricingInput, RecordOutput[inventree.PartPricing]](deps, GetPartPricingToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartPricingLookupClient, input GetPartPricingInput) (*mcp.CallToolResult, RecordOutput[inventree.PartPricing], error) {
			if input.PartID <= 0 {
				return recordOutput(inventree.PartPricing{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound})
			}
			record, err := client.GetPartPricing(ctx, input.PartID)
			return recordOutput(record, err)
		})
}

type UpdatePartPricingOverrideInput struct {
	PartID              int     `json:"part_id" jsonschema:"Stable part primary key."`
	OverrideMin         *string `json:"override_min,omitempty" jsonschema:"Replacement minimum-price override decimal string. Mutually exclusive with clear_override_min."`
	OverrideMinCurrency *string `json:"override_min_currency,omitempty" jsonschema:"Three-letter currency code for override_min."`
	ClearOverrideMin    bool    `json:"clear_override_min,omitempty" jsonschema:"Clear the minimum-price override; mutually exclusive with override_min."`
	OverrideMax         *string `json:"override_max,omitempty" jsonschema:"Replacement maximum-price override decimal string. Mutually exclusive with clear_override_max."`
	OverrideMaxCurrency *string `json:"override_max_currency,omitempty" jsonschema:"Three-letter currency code for override_max."`
	ClearOverrideMax    bool    `json:"clear_override_max,omitempty" jsonschema:"Clear the maximum-price override; mutually exclusive with override_max."`
}

func updatePartPricingOverride(deps Dependencies) mcp.ToolHandlerFor[UpdatePartPricingOverrideInput, RecordOutput[inventree.PartPricing]] {
	return LookupHandler[PartPricingWriteClient, UpdatePartPricingOverrideInput, RecordOutput[inventree.PartPricing]](deps, UpdatePartPricingOverrideToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartPricingWriteClient, input UpdatePartPricingOverrideInput) (*mcp.CallToolResult, RecordOutput[inventree.PartPricing], error) {
			if input.PartID <= 0 {
				return recordOutput(inventree.PartPricing{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound})
			}
			if input.OverrideMin != nil && input.ClearOverrideMin {
				return nil, RecordOutput[inventree.PartPricing]{}, errors.New("override_min and clear_override_min are mutually exclusive")
			}
			if input.OverrideMax != nil && input.ClearOverrideMax {
				return nil, RecordOutput[inventree.PartPricing]{}, errors.New("override_max and clear_override_max are mutually exclusive")
			}
			if input.OverrideMinCurrency != nil && input.OverrideMin == nil {
				return nil, RecordOutput[inventree.PartPricing]{}, errors.New("override_min_currency requires override_min to also be provided")
			}
			if input.OverrideMaxCurrency != nil && input.OverrideMax == nil {
				return nil, RecordOutput[inventree.PartPricing]{}, errors.New("override_max_currency requires override_max to also be provided")
			}
			fields := inventree.PatchFields{}
			switch {
			case input.OverrideMin != nil:
				fields["override_min"] = inventree.Set(*input.OverrideMin)
			case input.ClearOverrideMin:
				fields["override_min"] = inventree.Null()
			}
			if input.OverrideMinCurrency != nil {
				fields["override_min_currency"] = inventree.Set(*input.OverrideMinCurrency)
			}
			switch {
			case input.OverrideMax != nil:
				fields["override_max"] = inventree.Set(*input.OverrideMax)
			case input.ClearOverrideMax:
				fields["override_max"] = inventree.Null()
			}
			if input.OverrideMaxCurrency != nil {
				fields["override_max_currency"] = inventree.Set(*input.OverrideMaxCurrency)
			}
			if len(fields) == 0 {
				return nil, RecordOutput[inventree.PartPricing]{}, errors.New("provide at least one override field or clear flag to change")
			}
			updated, err := client.UpdatePartPricing(ctx, input.PartID, fields)
			return recordOutput(updated, err)
		})
}

type RefreshPartPricingInput struct {
	PartID int `json:"part_id" jsonschema:"Stable part primary key."`
}

type RefreshPartPricingOutput struct {
	Status  string                `json:"status"`
	Settled bool                  `json:"settled"`
	Record  inventree.PartPricing `json:"record"`
}

func refreshPartPricing(deps Dependencies) mcp.ToolHandlerFor[RefreshPartPricingInput, RefreshPartPricingOutput] {
	return LookupHandler[PartPricingWriteClient, RefreshPartPricingInput, RefreshPartPricingOutput](deps, RefreshPartPricingToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartPricingWriteClient, input RefreshPartPricingInput) (*mcp.CallToolResult, RefreshPartPricingOutput, error) {
			if input.PartID <= 0 {
				return TextResult(StatusNotFound), RefreshPartPricingOutput{Status: StatusNotFound}, nil
			}
			if _, err := client.RefreshPartPricing(ctx, input.PartID); err != nil {
				if isNotFound(err) {
					return TextResult(StatusNotFound), RefreshPartPricingOutput{Status: StatusNotFound}, nil
				}
				return nil, RefreshPartPricingOutput{}, err
			}
			deadline := time.Now().Add(partPricingRefreshTimeout)
			var latest inventree.PartPricing
			for {
				current, err := client.GetPartPricing(ctx, input.PartID)
				if err != nil {
					if isNotFound(err) {
						return TextResult(StatusNotFound), RefreshPartPricingOutput{Status: StatusNotFound}, nil
					}
					return nil, RefreshPartPricingOutput{}, err
				}
				latest = current
				if !current.ScheduledForUpdate {
					return TextResult(StatusOK), RefreshPartPricingOutput{Status: StatusOK, Settled: true, Record: latest}, nil
				}
				if !time.Now().Before(deadline) {
					return TextResult(StatusPending), RefreshPartPricingOutput{Status: StatusPending, Settled: false, Record: latest}, nil
				}
				select {
				case <-ctx.Done():
					return nil, RefreshPartPricingOutput{}, ctx.Err()
				case <-time.After(partPricingRefreshPollInterval):
				}
			}
		})
}

// --- shared helpers ---

func priceBreakSearchClarification[T any](question, field, reason string) (*mcp.CallToolResult, LookupOutput[T], error) {
	clarification := NewClarification(question, field, reason, field, true, nil, map[string]any{})
	return TextResult(StatusClarificationRequired), LookupOutput[T]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func paginatePriceBreaks(records []PriceBreakResult, offset, limit int) []PriceBreakResult {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(records) {
		return nil
	}
	end := min(len(records), offset+limit)
	return records[offset:end]
}

func registerPricingLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchInternalPriceBreaksToolName, "Search internal price breaks", "Lists a part's internal price-break rows ordered by quantity.", searchInternalPriceBreaks(deps))
	addReadOnlyTool(server, deps, SearchSalePriceBreaksToolName, "Search sale price breaks", "Lists a part's sale price-break rows ordered by quantity.", searchSalePriceBreaks(deps))
	addReadOnlyTool(server, deps, SearchSupplierPriceBreaksToolName, "Search supplier price breaks", "Lists a supplier part's price-break rows ordered by quantity.", searchSupplierPriceBreaks(deps))
	addReadOnlyTool(server, deps, GetPartPricingToolName, "Get part pricing", "Retrieves one part's computed pricing snapshot.", getPartPricing(deps))
}

func registerPricingWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, CreateInternalPriceBreakToolName, "Create internal price break", "Creates one internal price-break row after checking for an existing row at the same quantity.", createInternalPriceBreak(deps))
	addWriteTool(server, deps, UpdateInternalPriceBreakToolName, "Update internal price break", "Updates the price and/or currency of one internal price-break row by stable ID.", updateInternalPriceBreak(deps))
	addWriteTool(server, deps, DeleteInternalPriceBreakToolName, "Delete internal price break", "Deletes one internal price-break row by stable ID after confirm:true.", deleteInternalPriceBreak(deps))
	addWriteTool(server, deps, CreateSalePriceBreakToolName, "Create sale price break", "Creates one sale price-break row after checking the part is salable and has no existing row at the same quantity.", createSalePriceBreak(deps))
	addWriteTool(server, deps, UpdateSalePriceBreakToolName, "Update sale price break", "Updates the price and/or currency of one sale price-break row by stable ID.", updateSalePriceBreak(deps))
	addWriteTool(server, deps, DeleteSalePriceBreakToolName, "Delete sale price break", "Deletes one sale price-break row by stable ID after confirm:true.", deleteSalePriceBreak(deps))
	addWriteTool(server, deps, CreateSupplierPriceBreakToolName, "Create supplier price break", "Creates one supplier price-break row after checking for an existing row at the same quantity.", createSupplierPriceBreak(deps))
	addWriteTool(server, deps, UpdateSupplierPriceBreakToolName, "Update supplier price break", "Updates the price and/or currency of one supplier price-break row by stable ID.", updateSupplierPriceBreak(deps))
	addWriteTool(server, deps, DeleteSupplierPriceBreakToolName, "Delete supplier price break", "Deletes one supplier price-break row by stable ID after confirm:true.", deleteSupplierPriceBreak(deps))
	addWriteTool(server, deps, UpdatePartPricingOverrideToolName, "Update part pricing override", "Sets or clears a part's override_min/override_max computed-pricing bounds.", updatePartPricingOverride(deps))
	addWriteTool(server, deps, RefreshPartPricingToolName, "Refresh part pricing", "Schedules pricing recalculation for one part and bounded-polls for a settled snapshot.", refreshPartPricing(deps))
}
