package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxPurchaseOrderExtraLineScan = 1000

var (
	extraLinePricePattern    = regexp.MustCompile(`^-?(?:\d{1,13}(?:\.\d{1,6})?|\.\d{1,6})$`)
	extraLineQuantityPattern = regexp.MustCompile(`^\d{1,10}(?:\.\d{1,5})?$`)
)

type PurchaseOrderExtraLineReadClient interface {
	GetPurchaseOrder(context.Context, int) (inventree.PurchaseOrder, error)
	SearchPurchaseOrderExtraLinesPage(context.Context, inventree.PurchaseOrderExtraLineQuery) (inventree.Page[inventree.PurchaseOrderExtraLine], error)
	GetPurchaseOrderExtraLine(context.Context, int) (inventree.PurchaseOrderExtraLine, error)
}

type PurchaseOrderExtraLineClient interface {
	PurchaseOrderExtraLineReadClient
	CreatePurchaseOrderExtraLine(context.Context, inventree.PurchaseOrderExtraLineCreate) (inventree.PurchaseOrderExtraLine, error)
	UpdatePurchaseOrderExtraLine(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrderExtraLine, error)
	DeletePurchaseOrderExtraLine(context.Context, int) error
}

type PurchaseOrderExtraLineSearchInput struct {
	Search  string `json:"search,omitempty" jsonschema:"Search description, notes, quantity, or reference."`
	OrderID int    `json:"order_id,omitempty" jsonschema:"Optional purchase-order primary key filter."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum records to return. Defaults to 20 and is capped at 100."`
	Offset  int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type CreatePurchaseOrderExtraLineInput struct {
	DryRun      bool    `json:"dry_run,omitempty" jsonschema:"Return the complete create plan without writing."`
	OrderID     int     `json:"order_id" jsonschema:"Existing purchase-order primary key."`
	Reference   string  `json:"reference" jsonschema:"Required reference, unique within this purchase order for duplicate and retry recovery."`
	Description *string `json:"description,omitempty" jsonschema:"Optional informational line description."`
	Line        *string `json:"line,omitempty" jsonschema:"Optional supplier invoice line number."`
	Link        *string `json:"link,omitempty" jsonschema:"Optional complete HTTP(S) supplier product link without userinfo; query parameters and fragments are preserved."`
	Notes       *string `json:"notes,omitempty" jsonschema:"Optional invoice or operator context."`
	Quantity    float64 `json:"quantity" jsonschema:"Nonnegative extra-line quantity."`
	UnitPrice   *string `json:"unit_price,omitempty" jsonschema:"Optional exact signed unit price with at most six decimal places."`
	Currency    *string `json:"currency,omitempty" jsonschema:"Three-letter currency required whenever unit_price is supplied."`
	TargetDate  *string `json:"target_date,omitempty" jsonschema:"Optional target date in YYYY-MM-DD form."`
}

type UpdatePurchaseOrderExtraLineInput struct {
	DryRun          bool     `json:"dry_run,omitempty" jsonschema:"Return the complete patch plan without writing."`
	ID              int      `json:"id" jsonschema:"Existing purchase-order extra-line primary key."`
	OrderID         *int     `json:"order_id,omitempty" jsonschema:"Optional replacement purchase-order primary key."`
	Reference       *string  `json:"reference,omitempty" jsonschema:"Optional replacement nonblank reference, unique within the target purchase order."`
	Description     *string  `json:"description,omitempty" jsonschema:"Optional replacement description, including an explicit empty string."`
	Line            *string  `json:"line,omitempty" jsonschema:"Optional replacement supplier invoice line number, including an explicit empty string."`
	Link            *string  `json:"link,omitempty" jsonschema:"Optional replacement complete HTTP(S) link without userinfo; query parameters and fragments are preserved, and an explicit empty string clears it."`
	Notes           *string  `json:"notes,omitempty" jsonschema:"Optional replacement notes, including an explicit empty string."`
	Quantity        *float64 `json:"quantity,omitempty" jsonschema:"Optional replacement nonnegative quantity."`
	UnitPrice       *string  `json:"unit_price,omitempty" jsonschema:"Optional replacement exact signed unit price."`
	Currency        *string  `json:"currency,omitempty" jsonschema:"Optional replacement three-letter price currency; required with unit_price."`
	ClearUnitPrice  bool     `json:"clear_unit_price,omitempty" jsonschema:"Explicitly set price to null; conflicts with unit_price and currency."`
	TargetDate      *string  `json:"target_date,omitempty" jsonschema:"Optional replacement target date in YYYY-MM-DD form."`
	ClearTargetDate bool     `json:"clear_target_date,omitempty" jsonschema:"Explicitly set target_date to null; conflicts with target_date."`
}

type DeletePurchaseOrderExtraLineInput struct {
	ID      int  `json:"id" jsonschema:"Stable purchase-order extra-line primary key."`
	Confirm bool `json:"confirm,omitempty" jsonschema:"Required true after reviewing the exact extra line."`
}

type PurchaseOrderExtraLineOutput struct {
	Status                 string                            `json:"status"`
	DryRun                 bool                              `json:"dry_run,omitempty"`
	Record                 *inventree.PurchaseOrderExtraLine `json:"record,omitempty"`
	PurchaseOrder          *inventree.PurchaseOrder          `json:"purchase_order,omitempty"`
	AffectedPurchaseOrders []inventree.PurchaseOrder         `json:"affected_purchase_orders,omitempty"`
	PlannedChanges         []PlannedChange                   `json:"planned_changes,omitempty"`
	Verified               bool                              `json:"verified,omitempty"`
	Recovered              bool                              `json:"recovered,omitempty"`
	Validation             *ValidationFailure                `json:"validation,omitempty"`
	RecoveryPlan           string                            `json:"recovery_plan,omitempty"`
	Clarification          *ClarificationResponse            `json:"clarification,omitempty"`
}

func registerPurchaseOrderExtraLineLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchPurchaseOrderExtraLinesToolName, "Search purchase order extra lines", "Searches non-receivable purchase-order extra lines for review and recovery.", searchPurchaseOrderExtraLines(deps))
	addReadOnlyTool(server, deps, GetPurchaseOrderExtraLineToolName, "Get purchase order extra line", "Retrieves one non-receivable purchase-order extra line by stable ID.", getPurchaseOrderExtraLine(deps))
}

func registerPurchaseOrderExtraLineWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, CreatePurchaseOrderExtraLineToolName, "Create purchase order extra line", "Plans or retry-recoverably creates one non-receivable purchase-order extra line.", createPurchaseOrderExtraLine(deps))
	addWriteTool(server, deps, UpdatePurchaseOrderExtraLineToolName, "Update purchase order extra line", "Plans or safely updates one purchase-order extra line by stable ID.", updatePurchaseOrderExtraLine(deps))
	addWriteTool(server, deps, DeletePurchaseOrderExtraLineToolName, "Delete purchase order extra line", "Previews and explicitly deletes one stable purchase-order extra line with read-back verification.", deletePurchaseOrderExtraLine(deps))
}

func searchPurchaseOrderExtraLines(deps Dependencies) mcp.ToolHandlerFor[PurchaseOrderExtraLineSearchInput, LookupOutput[inventree.PurchaseOrderExtraLine]] {
	return LookupHandler[PurchaseOrderExtraLineClient, PurchaseOrderExtraLineSearchInput, LookupOutput[inventree.PurchaseOrderExtraLine]](deps, SearchPurchaseOrderExtraLinesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderExtraLineClient, input PurchaseOrderExtraLineSearchInput) (*mcp.CallToolResult, LookupOutput[inventree.PurchaseOrderExtraLine], error) {
			if input.OrderID < 0 || input.Offset < 0 {
				return extraLineLookupClarification("Which valid extra-line filters should be used?", "order_id and offset cannot be negative")
			}
			page, err := client.SearchPurchaseOrderExtraLinesPage(ctx, inventree.PurchaseOrderExtraLineQuery{Search: input.Search, Order: input.OrderID, Limit: NormalizeLookupLimit(input.Limit), Offset: input.Offset})
			if err != nil {
				return nil, LookupOutput[inventree.PurchaseOrderExtraLine]{}, safeExtraLineError("extra-line search")
			}
			for i := range page.Results {
				if input.OrderID > 0 && page.Results[i].Order != input.OrderID {
					return nil, LookupOutput[inventree.PurchaseOrderExtraLine]{}, safeExtraLineError("extra-line order identity verification")
				}
				page.Results[i] = sanitizePurchaseOrderExtraLine(page.Results[i])
			}
			if len(page.Results) == 0 {
				return TextResult(StatusNotFound), LookupOutput[inventree.PurchaseOrderExtraLine]{Status: StatusNotFound}, nil
			}
			return TextResult(StatusOK), LookupOutput[inventree.PurchaseOrderExtraLine]{Status: StatusOK, Count: page.Count, Results: page.Results}, nil
		})
}

func getPurchaseOrderExtraLine(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.PurchaseOrderExtraLine]] {
	return LookupHandler[PurchaseOrderExtraLineClient, IDInput, RecordOutput[inventree.PurchaseOrderExtraLine]](deps, GetPurchaseOrderExtraLineToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderExtraLineClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.PurchaseOrderExtraLine], error) {
			if input.ID <= 0 {
				return TextResult(StatusNotFound), RecordOutput[inventree.PurchaseOrderExtraLine]{Status: StatusNotFound}, nil
			}
			record, err := client.GetPurchaseOrderExtraLine(ctx, input.ID)
			if err != nil {
				return recordOutput(inventree.PurchaseOrderExtraLine{}, err)
			}
			if record.PK != input.ID {
				return nil, RecordOutput[inventree.PurchaseOrderExtraLine]{}, safeExtraLineError("extra-line stable identity verification")
			}
			return TextResult(StatusOK), RecordOutput[inventree.PurchaseOrderExtraLine]{Status: StatusOK, Record: sanitizePurchaseOrderExtraLine(record)}, nil
		})
}

func createPurchaseOrderExtraLine(deps Dependencies) mcp.ToolHandlerFor[CreatePurchaseOrderExtraLineInput, PurchaseOrderExtraLineOutput] {
	return LookupHandler[PurchaseOrderExtraLineClient, CreatePurchaseOrderExtraLineInput, PurchaseOrderExtraLineOutput](deps, CreatePurchaseOrderExtraLineToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderExtraLineClient, input CreatePurchaseOrderExtraLineInput) (*mcp.CallToolResult, PurchaseOrderExtraLineOutput, error) {
			prepared, err := prepareExtraLineCreate(input)
			if err != nil {
				return extraLineClarification(input.DryRun, "Which valid purchase-order extra line should be created?", err.Error(), map[string]any{"order_id": input.OrderID, "reference": strings.TrimSpace(input.Reference)})
			}
			order, err := loadExtraLineOrder(ctx, client, input.OrderID)
			if err != nil {
				if isNotFound(err) {
					return extraLineClarification(input.DryRun, "Which existing purchase order should own this extra line?", "order_id does not identify an existing purchase order", map[string]any{"order_id": input.OrderID})
				}
				return nil, PurchaseOrderExtraLineOutput{}, err
			}
			matches, err := findExtraLinesByReference(ctx, client, input.OrderID, prepared.Reference, 0)
			if err != nil {
				return nil, PurchaseOrderExtraLineOutput{}, err
			}
			if len(matches) > 1 || len(matches) == 1 && !extraLineMatchesCreate(matches[0], prepared) {
				return extraLineDuplicateClarification(input.DryRun, input.OrderID, prepared.Reference, matches)
			}
			if len(matches) == 1 {
				record := sanitizePurchaseOrderExtraLine(matches[0])
				return TextResult(StatusOK), PurchaseOrderExtraLineOutput{Status: StatusOK, DryRun: input.DryRun, Record: &record, PurchaseOrder: &order, AffectedPurchaseOrders: []inventree.PurchaseOrder{order}, Verified: true, Recovered: true}, nil
			}
			fields := extraLineCreateFields(prepared)
			if input.DryRun {
				return TextResult(StatusOK), PurchaseOrderExtraLineOutput{Status: StatusOK, DryRun: true, PurchaseOrder: &order, AffectedPurchaseOrders: []inventree.PurchaseOrder{order}, PlannedChanges: []PlannedChange{plannedChange("create_purchase_order_extra_line", "purchase_order_extra_line", 0, fields)}}, nil
			}
			created, createErr := client.CreatePurchaseOrderExtraLine(ctx, prepared)
			if createErr != nil {
				if validation, ok := safeValidationFailure(createErr); ok {
					return TextResult(StatusValidationFailed), PurchaseOrderExtraLineOutput{Status: StatusValidationFailed, Validation: validation}, nil
				}
				if !ambiguousCategoryMutation(createErr) {
					return nil, PurchaseOrderExtraLineOutput{}, safeExtraLineError("extra-line creation")
				}
				return recoverExtraLineCreate(ctx, client, order, prepared)
			}
			return verifyExtraLineCreate(ctx, client, order, created.PK, prepared, false)
		})
}

func updatePurchaseOrderExtraLine(deps Dependencies) mcp.ToolHandlerFor[UpdatePurchaseOrderExtraLineInput, PurchaseOrderExtraLineOutput] {
	return LookupHandler[PurchaseOrderExtraLineClient, UpdatePurchaseOrderExtraLineInput, PurchaseOrderExtraLineOutput](deps, UpdatePurchaseOrderExtraLineToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderExtraLineClient, input UpdatePurchaseOrderExtraLineInput) (*mcp.CallToolResult, PurchaseOrderExtraLineOutput, error) {
			if input.ID <= 0 {
				return extraLineClarification(input.DryRun, "Which purchase-order extra line should be updated?", "id must be positive", map[string]any{"id": input.ID})
			}
			before, err := client.GetPurchaseOrderExtraLine(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PurchaseOrderExtraLineOutput{Status: StatusNotFound}, nil
			}
			if err != nil || before.PK != input.ID {
				return nil, PurchaseOrderExtraLineOutput{}, safeExtraLineError("extra-line lookup")
			}
			fields, targetOrderID, targetReference, err := prepareExtraLinePatch(input, before)
			if err != nil {
				return extraLineClarification(input.DryRun, "Which valid extra-line fields should be updated?", err.Error(), map[string]any{"id": input.ID})
			}
			order, err := loadExtraLineOrder(ctx, client, targetOrderID)
			if err != nil {
				if isNotFound(err) {
					return extraLineClarification(input.DryRun, "Which existing purchase order should own this extra line?", "order_id does not identify an existing purchase order", map[string]any{"order_id": targetOrderID})
				}
				return nil, PurchaseOrderExtraLineOutput{}, err
			}
			matches, err := findExtraLinesByReference(ctx, client, targetOrderID, targetReference, input.ID)
			if err != nil {
				return nil, PurchaseOrderExtraLineOutput{}, err
			}
			if len(matches) > 0 {
				return extraLineDuplicateClarification(input.DryRun, targetOrderID, targetReference, matches)
			}
			if input.DryRun {
				record := sanitizePurchaseOrderExtraLine(before)
				affectedOrders := []inventree.PurchaseOrder{order}
				if before.Order != targetOrderID {
					sourceOrder, sourceErr := loadExtraLineOrder(ctx, client, before.Order)
					if sourceErr != nil {
						return nil, PurchaseOrderExtraLineOutput{}, sourceErr
					}
					affectedOrders = append(affectedOrders, sourceOrder)
					sort.Slice(affectedOrders, func(i, j int) bool { return affectedOrders[i].PK < affectedOrders[j].PK })
				}
				return TextResult(StatusOK), PurchaseOrderExtraLineOutput{Status: StatusOK, DryRun: true, Record: &record, PurchaseOrder: &order, AffectedPurchaseOrders: affectedOrders, PlannedChanges: []PlannedChange{plannedChange("update_purchase_order_extra_line", "purchase_order_extra_line", input.ID, extraLinePatchFieldValues(fields))}}, nil
			}
			updated, updateErr := client.UpdatePurchaseOrderExtraLine(ctx, input.ID, fields)
			if updateErr != nil {
				if validation, ok := safeValidationFailure(updateErr); ok {
					return TextResult(StatusValidationFailed), PurchaseOrderExtraLineOutput{Status: StatusValidationFailed, Validation: validation}, nil
				}
				if !ambiguousCategoryMutation(updateErr) {
					return nil, PurchaseOrderExtraLineOutput{}, safeExtraLineError("extra-line update")
				}
				return verifyExtraLinePatch(ctx, client, order, before, fields, true)
			}
			if updated.PK != input.ID {
				return extraLinePartial(before, "PATCH returned a mismatched stable identity; read the original ID before retrying")
			}
			return verifyExtraLinePatch(ctx, client, order, before, fields, false)
		})
}

func deletePurchaseOrderExtraLine(deps Dependencies) mcp.ToolHandlerFor[DeletePurchaseOrderExtraLineInput, PurchaseOrderExtraLineOutput] {
	return LookupHandler[PurchaseOrderExtraLineClient, DeletePurchaseOrderExtraLineInput, PurchaseOrderExtraLineOutput](deps, DeletePurchaseOrderExtraLineToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderExtraLineClient, input DeletePurchaseOrderExtraLineInput) (*mcp.CallToolResult, PurchaseOrderExtraLineOutput, error) {
			if input.ID <= 0 {
				return extraLineClarification(false, "Which purchase-order extra line should be deleted?", "id must be positive", map[string]any{"id": input.ID})
			}
			record, err := client.GetPurchaseOrderExtraLine(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PurchaseOrderExtraLineOutput{Status: StatusNotFound}, nil
			}
			if err != nil || record.PK != input.ID {
				return nil, PurchaseOrderExtraLineOutput{}, safeExtraLineError("extra-line delete lookup")
			}
			order, err := loadExtraLineOrder(ctx, client, record.Order)
			if err != nil {
				if isNotFound(err) {
					return extraLineClarification(false, "Which existing purchase order should own this extra line?", "the extra line's purchase order no longer exists; re-check the stable ID before retrying", map[string]any{"id": input.ID})
				}
				return nil, PurchaseOrderExtraLineOutput{}, err
			}
			safeRecord := sanitizePurchaseOrderExtraLine(record)
			if !input.Confirm {
				safeRecord = purchaseOrderExtraLineRecovery(safeRecord)
				clarification := NewClarification("Delete this non-receivable purchase-order extra line?", "confirm", "delete_purchase_order_extra_line requires confirm:true after reviewing the stable line", "confirm", true, candidatesFor([]inventree.PurchaseOrderExtraLine{safeRecord}), map[string]any{"id": input.ID, "confirm": true})
				return TextResult(StatusClarificationRequired), PurchaseOrderExtraLineOutput{Status: StatusClarificationRequired, Record: &safeRecord, PurchaseOrder: &order, Clarification: &clarification}, nil
			}
			if err := client.DeletePurchaseOrderExtraLine(ctx, input.ID); err != nil {
				return nil, PurchaseOrderExtraLineOutput{}, safeExtraLineError("extra-line deletion")
			}
			_, err = client.GetPurchaseOrderExtraLine(ctx, input.ID)
			if err == nil {
				return extraLinePartial(record, "extra line still exists after deletion; inspect the stable ID before retrying")
			}
			if !isNotFound(err) {
				return extraLinePartial(record, "deletion read-back could not prove absence; inspect the stable ID before retrying")
			}
			refreshedOrder, err := loadExtraLineOrder(ctx, client, order.PK)
			if err != nil {
				return extraLinePartial(record, "extra line was deleted but the refreshed purchase-order total is unavailable; read the purchase order before continuing")
			}
			return TextResult(StatusOK), PurchaseOrderExtraLineOutput{Status: StatusOK, Record: &safeRecord, PurchaseOrder: &refreshedOrder, AffectedPurchaseOrders: []inventree.PurchaseOrder{refreshedOrder}, Verified: true}, nil
		})
}

func prepareExtraLineCreate(input CreatePurchaseOrderExtraLineInput) (inventree.PurchaseOrderExtraLineCreate, error) {
	reference := strings.TrimSpace(input.Reference)
	price, currency, link, err := validateExtraLineFields(input.OrderID, reference, input.Description, input.Line, input.Link, input.Notes, input.Quantity, input.UnitPrice, input.Currency, input.TargetDate)
	if err != nil {
		return inventree.PurchaseOrderExtraLineCreate{}, err
	}
	return inventree.PurchaseOrderExtraLineCreate{Order: input.OrderID, Reference: reference, Description: input.Description, Line: input.Line, Link: link, Notes: input.Notes, Quantity: input.Quantity, Price: price, PriceCurrency: currency, TargetDate: input.TargetDate}, nil
}

func prepareExtraLinePatch(input UpdatePurchaseOrderExtraLineInput, before inventree.PurchaseOrderExtraLine) (inventree.PatchFields, int, string, error) {
	if input.UnitPrice != nil && input.ClearUnitPrice || input.Currency != nil && input.ClearUnitPrice {
		return nil, 0, "", errors.New("clear_unit_price conflicts with unit_price and currency")
	}
	if input.TargetDate != nil && input.ClearTargetDate {
		return nil, 0, "", errors.New("clear_target_date conflicts with target_date")
	}
	targetOrderID := before.Order
	if input.OrderID != nil {
		targetOrderID = *input.OrderID
	}
	targetReference := before.Reference
	if input.Reference != nil {
		targetReference = strings.TrimSpace(*input.Reference)
	}
	quantity := before.Quantity
	if input.Quantity != nil {
		quantity = *input.Quantity
	}
	var price, currency *string
	if input.UnitPrice != nil || input.Currency != nil {
		price = input.UnitPrice
		currency = input.Currency
		if price == nil && before.Price != nil {
			value := string(*before.Price)
			price = &value
		}
	}
	link := input.Link
	validatedPrice, validatedCurrency, validatedLink, err := validateExtraLineFields(targetOrderID, targetReference, input.Description, input.Line, link, input.Notes, quantity, price, currency, input.TargetDate)
	if err != nil {
		return nil, 0, "", err
	}
	fields := inventree.PatchFields{}
	setPatchInt(fields, "order", input.OrderID)
	if input.Reference != nil {
		fields["reference"] = inventree.Set(targetReference)
	}
	setPatchString(fields, "description", input.Description)
	setPatchString(fields, "line", input.Line)
	if input.Link != nil {
		fields["link"] = inventree.Set(*validatedLink)
	}
	setPatchString(fields, "notes", input.Notes)
	if input.Quantity != nil {
		fields["quantity"] = inventree.Set(*input.Quantity)
	}
	if input.ClearUnitPrice {
		fields["price"] = inventree.Null()
	} else if input.UnitPrice != nil {
		fields["price"] = inventree.Set(*validatedPrice)
		fields["price_currency"] = inventree.Set(*validatedCurrency)
	} else if input.Currency != nil {
		fields["price_currency"] = inventree.Set(*validatedCurrency)
	}
	if input.ClearTargetDate {
		fields["target_date"] = inventree.Null()
	} else {
		setPatchString(fields, "target_date", input.TargetDate)
	}
	if len(fields) == 0 {
		return nil, 0, "", errors.New("at least one patch field is required")
	}
	return fields, targetOrderID, targetReference, nil
}

func validateExtraLineFields(orderID int, reference string, description, line, link, notes *string, quantity float64, price, currency, targetDate *string) (*string, *string, *string, error) {
	if orderID <= 0 {
		return nil, nil, nil, errors.New("order_id must be positive")
	}
	if reference == "" || utf8.RuneCountInString(reference) > 100 {
		return nil, nil, nil, errors.New("reference is required and must be at most 100 characters")
	}
	if math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity < 0 {
		return nil, nil, nil, errors.New("quantity must be finite and nonnegative")
	}
	if !extraLineQuantityPattern.MatchString(strconv.FormatFloat(quantity, 'f', -1, 64)) {
		return nil, nil, nil, errors.New("quantity must fit InvenTree's 15-digit, 5-decimal extra-line limit")
	}
	for name, value := range map[string]struct {
		value *string
		max   int
	}{"description": {description, 250}, "line": {line, 20}, "link": {link, 2000}, "notes": {notes, 500}} {
		if value.value != nil && utf8.RuneCountInString(*value.value) > value.max {
			return nil, nil, nil, fmt.Errorf("%s must be at most %d characters", name, value.max)
		}
	}
	if targetDate != nil && !validDate(*targetDate) {
		return nil, nil, nil, errors.New("target_date must use YYYY-MM-DD")
	}
	var normalizedLink *string
	if link != nil {
		validated, err := validateExternalURL(*link)
		if err != nil {
			return nil, nil, nil, errors.New("link must be an absolute HTTP(S) URL without userinfo or credentials")
		}
		normalizedLink = &validated
	}
	if price == nil {
		if currency != nil && strings.TrimSpace(*currency) != "" {
			return nil, nil, nil, errors.New("currency requires unit_price")
		}
		return nil, nil, normalizedLink, nil
	}
	normalizedPrice := strings.TrimSpace(*price)
	if !extraLinePricePattern.MatchString(normalizedPrice) {
		return nil, nil, nil, errors.New("unit_price must be an exact signed decimal with at most 13 integer and 6 fractional digits")
	}
	if currency == nil {
		return nil, nil, nil, errors.New("currency is required when unit_price is supplied")
	}
	normalizedCurrency := strings.ToUpper(strings.TrimSpace(*currency))
	if len(normalizedCurrency) != 3 || normalizedCurrency[0] < 'A' || normalizedCurrency[0] > 'Z' || normalizedCurrency[1] < 'A' || normalizedCurrency[1] > 'Z' || normalizedCurrency[2] < 'A' || normalizedCurrency[2] > 'Z' {
		return nil, nil, nil, errors.New("currency must be a three-letter code")
	}
	return &normalizedPrice, &normalizedCurrency, normalizedLink, nil
}

func findExtraLinesByReference(ctx context.Context, client PurchaseOrderExtraLineReadClient, orderID int, reference string, excludeID int) ([]inventree.PurchaseOrderExtraLine, error) {
	page, err := client.SearchPurchaseOrderExtraLinesPage(ctx, inventree.PurchaseOrderExtraLineQuery{Search: reference, Order: orderID, Limit: MaxLookupLimit})
	if err != nil {
		return nil, safeExtraLineError("extra-line duplicate preflight")
	}
	if page.Count > maxPurchaseOrderExtraLineScan {
		return nil, fmt.Errorf("extra-line duplicate preflight exceeds the %d-row safety bound; narrow or clean up the purchase order", maxPurchaseOrderExtraLineScan)
	}
	matches := make([]inventree.PurchaseOrderExtraLine, 0)
	offset := 0
	for {
		for _, record := range page.Results {
			if record.Order != orderID {
				return nil, safeExtraLineError("extra-line duplicate order verification")
			}
			if record.PK != excludeID && strings.TrimSpace(record.Reference) == reference {
				matches = append(matches, record)
			}
		}
		offset += len(page.Results)
		if page.Next == nil || *page.Next == "" || offset >= page.Count {
			break
		}
		if len(page.Results) == 0 || offset >= maxPurchaseOrderExtraLineScan {
			return nil, fmt.Errorf("extra-line duplicate preflight could not prove a complete bounded result")
		}
		page, err = client.SearchPurchaseOrderExtraLinesPage(ctx, inventree.PurchaseOrderExtraLineQuery{Search: reference, Order: orderID, Limit: MaxLookupLimit, Offset: offset})
		if err != nil {
			return nil, safeExtraLineError("extra-line duplicate preflight")
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].PK < matches[j].PK })
	return matches, nil
}

func scanPurchaseOrderExtraLines(ctx context.Context, client PurchaseOrderExtraLineReadClient, orderID int) ([]inventree.PurchaseOrderExtraLine, error) {
	page, err := client.SearchPurchaseOrderExtraLinesPage(ctx, inventree.PurchaseOrderExtraLineQuery{Order: orderID, Limit: MaxLookupLimit})
	if err != nil {
		return nil, safeExtraLineError("extra-line recovery scan")
	}
	if page.Count > maxPurchaseOrderExtraLineScan {
		return nil, fmt.Errorf("purchase order has more than the %d-extra-line safety bound", maxPurchaseOrderExtraLineScan)
	}
	all := make([]inventree.PurchaseOrderExtraLine, 0, page.Count)
	offset := 0
	for {
		for _, record := range page.Results {
			if record.Order != orderID {
				return nil, safeExtraLineError("extra-line recovery order verification")
			}
			all = append(all, record)
		}
		offset += len(page.Results)
		if page.Next == nil || *page.Next == "" || offset >= page.Count {
			break
		}
		if len(page.Results) == 0 || offset >= maxPurchaseOrderExtraLineScan {
			return nil, fmt.Errorf("extra-line recovery scan could not prove a complete bounded result")
		}
		page, err = client.SearchPurchaseOrderExtraLinesPage(ctx, inventree.PurchaseOrderExtraLineQuery{Order: orderID, Limit: MaxLookupLimit, Offset: offset})
		if err != nil {
			return nil, safeExtraLineError("extra-line recovery scan")
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].PK < all[j].PK })
	return all, nil
}

func verifyExtraLineCreate(ctx context.Context, client PurchaseOrderExtraLineReadClient, order inventree.PurchaseOrder, id int, requested inventree.PurchaseOrderExtraLineCreate, recovered bool) (*mcp.CallToolResult, PurchaseOrderExtraLineOutput, error) {
	if id <= 0 {
		return recoverExtraLineCreate(ctx, client, order, requested)
	}
	record, err := client.GetPurchaseOrderExtraLine(ctx, id)
	if err != nil || record.PK != id || !extraLineMatchesCreate(record, requested) {
		return extraLinePartial(record, "create read-back did not verify every requested field and target order; inspect the returned stable ID and reference before retrying")
	}
	refreshedOrder, err := loadExtraLineOrder(ctx, client, order.PK)
	if err != nil {
		return extraLinePartial(record, "extra line was created but purchase-order total refresh failed; inspect both stable IDs before retrying")
	}
	safeRecord := sanitizePurchaseOrderExtraLine(record)
	return TextResult(StatusOK), PurchaseOrderExtraLineOutput{Status: StatusOK, Record: &safeRecord, PurchaseOrder: &refreshedOrder, AffectedPurchaseOrders: []inventree.PurchaseOrder{refreshedOrder}, Verified: true, Recovered: recovered}, nil
}

func recoverExtraLineCreate(ctx context.Context, client PurchaseOrderExtraLineReadClient, order inventree.PurchaseOrder, requested inventree.PurchaseOrderExtraLineCreate) (*mcp.CallToolResult, PurchaseOrderExtraLineOutput, error) {
	matches, err := findExtraLinesByReference(ctx, client, requested.Order, requested.Reference, 0)
	if err != nil {
		return extraLinePartial(inventree.PurchaseOrderExtraLine{Order: requested.Order, Reference: requested.Reference}, "creation result is unknown and bounded recovery failed; search by the exact order_id and reference before retrying")
	}
	if len(matches) == 1 && extraLineMatchesCreate(matches[0], requested) {
		return verifyExtraLineCreate(ctx, client, order, matches[0].PK, requested, true)
	}
	return extraLineRecoveryPartial(inventree.PurchaseOrderExtraLine{Order: requested.Order, Reference: requested.Reference}, matches, "creation result is unknown; search by the exact order_id and reference and inspect every candidate before retrying")
}

func verifyExtraLinePatch(ctx context.Context, client PurchaseOrderExtraLineReadClient, order inventree.PurchaseOrder, before inventree.PurchaseOrderExtraLine, fields inventree.PatchFields, recovered bool) (*mcp.CallToolResult, PurchaseOrderExtraLineOutput, error) {
	record, err := client.GetPurchaseOrderExtraLine(ctx, before.PK)
	if err != nil || record.PK != before.PK || !extraLineMatchesPatch(record, fields) {
		return extraLinePartial(before, "PATCH result could not be verified exactly; read the stable ID before retrying")
	}
	refreshedOrder, err := loadExtraLineOrder(ctx, client, record.Order)
	if err != nil {
		return extraLinePartial(record, "extra line was updated but purchase-order total refresh failed; inspect both stable IDs before retrying")
	}
	safeRecord := sanitizePurchaseOrderExtraLine(record)
	affectedOrders := []inventree.PurchaseOrder{refreshedOrder}
	if before.Order != record.Order {
		sourceOrder, sourceErr := loadExtraLineOrder(ctx, client, before.Order)
		if sourceErr != nil {
			return extraLinePartial(record, "extra line was moved but the source purchase-order total refresh failed; inspect both purchase orders before retrying")
		}
		affectedOrders = append(affectedOrders, sourceOrder)
		sort.Slice(affectedOrders, func(i, j int) bool { return affectedOrders[i].PK < affectedOrders[j].PK })
	}
	return TextResult(StatusOK), PurchaseOrderExtraLineOutput{Status: StatusOK, Record: &safeRecord, PurchaseOrder: &refreshedOrder, AffectedPurchaseOrders: affectedOrders, Verified: true, Recovered: recovered}, nil
}

func extraLineMatchesCreate(record inventree.PurchaseOrderExtraLine, requested inventree.PurchaseOrderExtraLineCreate) bool {
	return record.Order == requested.Order && strings.TrimSpace(record.Reference) == requested.Reference && optionalStringMatches(requested.Description, record.Description) && optionalStringMatches(requested.Line, record.Line) && optionalStringMatches(requested.Link, record.Link) && optionalStringMatches(requested.Notes, record.Notes) && math.Abs(record.Quantity-requested.Quantity) < 1e-9 && decimalPointerMatches(requested.Price, record.Price) && optionalStringMatches(requested.PriceCurrency, record.PriceCurrency) && optionalNullableStringMatches(requested.TargetDate, record.TargetDate)
}

func extraLineMatchesPatch(record inventree.PurchaseOrderExtraLine, fields inventree.PatchFields) bool {
	for name, field := range fields {
		value := field.Value()
		switch name {
		case "order":
			if record.Order != value.(int) {
				return false
			}
		case "reference":
			if record.Reference != value.(string) {
				return false
			}
		case "description":
			if record.Description != value.(string) {
				return false
			}
		case "line":
			if record.Line != value.(string) {
				return false
			}
		case "link":
			if record.Link != value.(string) {
				return false
			}
		case "notes":
			if record.Notes != value.(string) {
				return false
			}
		case "quantity":
			if math.Abs(record.Quantity-value.(float64)) >= 1e-9 {
				return false
			}
		case "price":
			if value == nil {
				if record.Price != nil {
					return false
				}
			} else if !decimalPointerMatches(stringPointer(value.(string)), record.Price) {
				return false
			}
		case "price_currency":
			if record.PriceCurrency != value.(string) {
				return false
			}
		case "target_date":
			if value == nil {
				if record.TargetDate != nil {
					return false
				}
			} else if record.TargetDate == nil || *record.TargetDate != value.(string) {
				return false
			}
		}
	}
	return true
}

func optionalStringMatches(requested *string, actual string) bool {
	return requested == nil || *requested == actual
}

func optionalNullableStringMatches(requested, actual *string) bool {
	return requested == nil || actual != nil && *requested == *actual
}

func decimalPointerMatches(requested *string, actual *inventree.DecimalString) bool {
	if requested == nil {
		return actual == nil
	}
	if actual == nil {
		return false
	}
	left, leftOK := new(big.Rat).SetString(*requested)
	right, rightOK := new(big.Rat).SetString(string(*actual))
	return leftOK && rightOK && left.Cmp(right) == 0
}

func extraLineCreateFields(input inventree.PurchaseOrderExtraLineCreate) map[string]any {
	fields := map[string]any{"order": input.Order, "reference": input.Reference, "quantity": input.Quantity}
	setOptionalField(fields, "description", input.Description)
	setOptionalField(fields, "line", input.Line)
	if input.Link != nil {
		fields["link"] = projectExternalURL(input.Link)
	}
	setOptionalField(fields, "notes", input.Notes)
	setOptionalField(fields, "price", input.Price)
	setOptionalField(fields, "price_currency", input.PriceCurrency)
	setOptionalField(fields, "target_date", input.TargetDate)
	return fields
}

func extraLinePatchFieldValues(fields inventree.PatchFields) map[string]any {
	values := patchFieldValues(fields)
	if link, ok := values["link"].(string); ok {
		values["link"] = projectExternalURL(&link)
	}
	return values
}

func loadExtraLineOrder(ctx context.Context, client PurchaseOrderExtraLineReadClient, id int) (inventree.PurchaseOrder, error) {
	order, err := client.GetPurchaseOrder(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return inventree.PurchaseOrder{}, err
		}
		return inventree.PurchaseOrder{}, safeExtraLineError("purchase-order lookup and identity verification")
	}
	if order.PK != id {
		return inventree.PurchaseOrder{}, safeExtraLineError("purchase-order lookup and identity verification")
	}
	return order, nil
}

func sanitizePurchaseOrderExtraLine(record inventree.PurchaseOrderExtraLine) inventree.PurchaseOrderExtraLine {
	if record.Link != "" {
		record.Link = projectExternalURL(&record.Link)
	}
	return record
}

func purchaseOrderExtraLineRecovery(record inventree.PurchaseOrderExtraLine) inventree.PurchaseOrderExtraLine {
	record.Link = ""
	return record
}

func extraLineDuplicateClarification(dryRun bool, orderID int, reference string, matches []inventree.PurchaseOrderExtraLine) (*mcp.CallToolResult, PurchaseOrderExtraLineOutput, error) {
	safe := make([]inventree.PurchaseOrderExtraLine, len(matches))
	for i := range matches {
		safe[i] = sanitizePurchaseOrderExtraLine(matches[i])
	}
	clarification := NewClarification("Which unique extra-line reference should identify this line?", "reference", "the normalized reference already belongs to a different or ambiguous extra line in this purchase order", "reference", true, candidatesFor(safe), map[string]any{"order_id": orderID, "reference": reference})
	return TextResult(StatusClarificationRequired), PurchaseOrderExtraLineOutput{Status: StatusClarificationRequired, DryRun: dryRun, Clarification: &clarification}, nil
}

func extraLineClarification(dryRun bool, question, reason string, retryValues map[string]any) (*mcp.CallToolResult, PurchaseOrderExtraLineOutput, error) {
	clarification := NewClarification(question, "purchase_order_extra_line", reason, "input", true, nil, retryValues)
	return TextResult(StatusClarificationRequired), PurchaseOrderExtraLineOutput{Status: StatusClarificationRequired, DryRun: dryRun, Clarification: &clarification}, nil
}

func extraLineLookupClarification(question, reason string) (*mcp.CallToolResult, LookupOutput[inventree.PurchaseOrderExtraLine], error) {
	clarification := NewClarification(question, "filters", reason, "filters", true, nil, nil)
	return TextResult(StatusClarificationRequired), LookupOutput[inventree.PurchaseOrderExtraLine]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func extraLinePartial(record inventree.PurchaseOrderExtraLine, recovery string) (*mcp.CallToolResult, PurchaseOrderExtraLineOutput, error) {
	safe := purchaseOrderExtraLineRecovery(record)
	return TextResult(StatusPartialFailure), PurchaseOrderExtraLineOutput{Status: StatusPartialFailure, Record: &safe, RecoveryPlan: recovery}, nil
}

func extraLineRecoveryPartial(record inventree.PurchaseOrderExtraLine, matches []inventree.PurchaseOrderExtraLine, recovery string) (*mcp.CallToolResult, PurchaseOrderExtraLineOutput, error) {
	safe := purchaseOrderExtraLineRecovery(record)
	safeMatches := make([]inventree.PurchaseOrderExtraLine, len(matches))
	for i := range matches {
		safeMatches[i] = purchaseOrderExtraLineRecovery(matches[i])
	}
	clarification := extraLineRecoveryClarification(record.Order, record.Reference, safeMatches)
	return TextResult(StatusPartialFailure), PurchaseOrderExtraLineOutput{Status: StatusPartialFailure, Record: &safe, RecoveryPlan: recovery, Clarification: &clarification}, nil
}

func extraLineRecoveryClarification(orderID int, reference string, matches []inventree.PurchaseOrderExtraLine) ClarificationResponse {
	if len(matches) == 0 {
		clarification := NewClarification("Which current extra lines match the unknown create result?", "purchase_order_extra_line", "the ambiguous create response produced no stable candidate in the bounded recovery scan", "filters", true, nil, map[string]any{"order_id": orderID, "search": reference})
		clarification.RetryTool = SearchPurchaseOrderExtraLinesToolName
		return clarification
	}
	clarification := NewClarification("Which recovered extra-line candidate reflects the unknown create result?", "purchase_order_extra_line", "the ambiguous create response could not be matched to one exact requested extra line", "id", true, candidatesFor(matches), map[string]any{"order_id": orderID, "reference": reference})
	clarification.RetryTool = GetPurchaseOrderExtraLineToolName
	return clarification
}

func safeExtraLineError(operation string) error {
	return fmt.Errorf("%s failed; inspect InvenTree availability and permissions before retrying", operation)
}
