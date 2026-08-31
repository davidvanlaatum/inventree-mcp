package tools

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type StockTransferClient interface {
	GetStockItem(context.Context, int) (inventree.StockItem, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	SearchStockItemsPage(context.Context, inventree.StockItemQuery) (inventree.StockItemPage, error)
	TransferStock(context.Context, inventree.StockTransfer) error
}

func transferStockItem(deps Dependencies) mcp.ToolHandlerFor[TransferStockItemInput, StockTransferOutput] {
	return LookupHandler[StockTransferClient, TransferStockItemInput, StockTransferOutput](deps, TransferStockItemToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockTransferClient, input TransferStockItemInput) (*mcp.CallToolResult, StockTransferOutput, error) {
			out := StockTransferOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.StockItemID <= 0 {
				return stockTransferClarification(out, "Which stock item should be transferred?", "stock_item", "stock_item_id must be positive", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID})
			}
			if input.DestinationLocationID <= 0 {
				return stockTransferClarification(out, "Which explicit destination should receive the stock item?", "destination_location", "destination_location_id must be positive", "destination_location_id", map[string]any{"stock_item_id": input.StockItemID, "destination_location_id": input.DestinationLocationID})
			}
			reason := strings.TrimSpace(input.Reason)
			if reason == "" {
				return stockTransferClarification(out, "What audit reason should be recorded for this stock transfer?", "reason", "a nonblank operator reason is required", "reason", map[string]any{"stock_item_id": input.StockItemID, "destination_location_id": input.DestinationLocationID})
			}

			before, err := client.GetStockItem(ctx, input.StockItemID)
			if err != nil {
				if isNotFound(err) {
					out.Status = StatusNotFound
					return TextResult(StatusNotFound), out, nil
				}
				return nil, out, safeToolError(ctx, err)
			}
			if before.PK != input.StockItemID {
				return nil, out, errors.New("InvenTree returned a mismatched stock-item identity")
			}
			normalizedQuantityString, normalizedQuantity, ok := normalizedStockDecimal(before.Quantity, false)
			if !ok {
				return stockTransferClarification(out, "Which positive stock item should be transferred?", "quantity", "the complete current stock quantity must be positive and fit the InvenTree decimal bounds", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID, "current_quantity": before.Quantity})
			}
			before.Quantity = normalizedQuantity
			if result, clarified, unsafe := unsafeStockTransfer(out, before); unsafe {
				return result, clarified, nil
			}

			destination, err := client.GetStockLocation(ctx, input.DestinationLocationID)
			if err != nil {
				if isNotFound(err) {
					return stockTransferClarification(out, "Which existing stock location should receive the item?", "destination_location", "destination_location_id was not found", "destination_location_id", map[string]any{"stock_item_id": input.StockItemID, "destination_location_id": input.DestinationLocationID})
				}
				return nil, out, safeToolError(ctx, err)
			}
			if destination.PK != input.DestinationLocationID {
				return nil, out, errors.New("InvenTree returned a mismatched destination-location identity")
			}
			if before.Location != nil && *before.Location == destination.PK {
				return stockTransferClarification(out, "Which different destination should receive the stock item?", "destination_location", "the selected stock item is already at this location; no transfer is needed", "destination_location_id", map[string]any{"stock_item_id": input.StockItemID, "destination_location_id": destination.PK})
			}

			source, err := client.GetStockLocation(ctx, *before.Location)
			if err != nil {
				return nil, out, safeToolError(ctx, err)
			}
			if source.PK != *before.Location {
				return nil, out, errors.New("InvenTree returned a mismatched source-location identity")
			}

			destinationID := destination.PK
			after := snapshotStockItem(before)
			transferQuantity := normalizedQuantity
			transferQuantityString := normalizedQuantityString
			willSplit := input.Quantity != nil
			var sourceRemainder, destinationQuantity *float64
			var preexistingDestinationStockItemIDs []int
			if willSplit {
				requested, normalizedRequested, validRequested := normalizedStockDecimal(*input.Quantity, false)
				if !validRequested || normalizedRequested >= normalizedQuantity {
					return stockTransferClarification(out, "What positive partial quantity should be transferred?", "quantity", "quantity must be positive, fit the InvenTree decimal bounds, and be smaller than the current stock quantity; omit quantity for a complete transfer", "quantity", map[string]any{"stock_item_id": input.StockItemID, "current_quantity": normalizedQuantity})
				}
				remaining := normalizedQuantity - normalizedRequested
				_, normalizedRemaining, validRemaining := normalizedStockDecimal(remaining, false)
				if !validRemaining {
					return stockTransferClarification(out, "What partial quantity leaves a valid source remainder?", "quantity", "the requested quantity does not leave a schema-valid positive source remainder", "quantity", map[string]any{"stock_item_id": input.StockItemID, "current_quantity": normalizedQuantity})
				}
				transferQuantity = normalizedRequested
				transferQuantityString = requested
				sourceRemainder = &normalizedRemaining
				destinationQuantity = &normalizedRequested
				after.Quantity = normalizedRemaining
				page, searchErr := client.SearchStockItemsPage(ctx, inventree.StockItemQuery{PartID: before.Part, Limit: 1000})
				if searchErr != nil || page.HasMore {
					return stockTransferClarification(out, "Which partial-transfer destination can be reconciled safely?", "quantity", "the current stock search is unavailable or incomplete, so an existing destination baseline cannot be captured safely; retry the dry run after the stock search is complete", "dry_run", map[string]any{"stock_item_id": input.StockItemID, "destination_location_id": destination.PK, "quantity": normalizedRequested})
				}
				for _, item := range page.Results {
					if item.PK != before.PK && item.Location != nil && *item.Location == destination.PK && math.Abs(item.Quantity-normalizedRequested) <= 1e-9 && stockTransferCopiedProjectionEqual(item, &StockTransferContext{Provenance: stockTransferProvenance(before), Safety: stockTransferSafety(before)}) {
						// Bind all matching pre-existing IDs into the plan so recovery
						// can require a newly created destination record.
						preexistingDestinationStockItemIDs = append(preexistingDestinationStockItemIDs, item.PK)
					}
				}
			} else {
				after.LocationID = &destinationID
			}
			plan := StockAdjustmentPlan{
				Action: TransferStockItemToolName,
				Before: snapshotStockItem(before),
				After:  after,
				Reason: reason,
				Transfer: &StockTransferContext{
					Source:                             stockTransferLocation(source),
					Destination:                        stockTransferLocation(destination),
					Provenance:                         stockTransferProvenance(before),
					Safety:                             stockTransferSafety(before),
					WillSplit:                          willSplit,
					Quantity:                           optionalFloat(willSplit, transferQuantity),
					SourceRemainder:                    sourceRemainder,
					DestinationQuantity:                destinationQuantity,
					PreexistingDestinationStockItemIDs: preexistingDestinationStockItemIDs,
				},
			}
			return executeStockTransfer(ctx, deps.stockPlanStore, client, input, plan, transferQuantityString)
		})
}

func optionalFloat(enabled bool, value float64) *float64 {
	if !enabled {
		return nil
	}
	return &value
}

func unsafeStockTransfer(out StockTransferOutput, item inventree.StockItem) (*mcp.CallToolResult, StockTransferOutput, bool) {
	retry := map[string]any{"stock_item_id": item.PK, "dry_run": true}
	clarify := func(question, subject, reason string) (*mcp.CallToolResult, StockTransferOutput, bool) {
		result, clarified, _ := stockTransferClarification(out, question, subject, reason, "stock_item_id", retry)
		return result, clarified, true
	}
	switch {
	case item.Location == nil:
		return clarify("Which currently located stock item should be transferred?", "location", "the selected stock item has no current source location, so a complete source-to-destination plan cannot be prepared")
	case stockItemHasSerial(item):
		retry["serial"] = item.Serial
		return clarify("How should the serialized stock item be moved?", "serial", "serialized stock is outside the complete ordinary-stock transfer contract")
	case !item.InStock:
		return clarify("Which available stock item should be transferred?", "in_stock", "the selected stock item is not currently in stock")
	case item.Allocated == nil:
		return clarify("What is the current stock allocation state?", "allocated", "allocation context is unavailable, so a safe transfer cannot be proven")
	case *item.Allocated != 0:
		retry["allocated"] = *item.Allocated
		return clarify("How should the allocated stock be released first?", "allocated", "allocated stock cannot be transferred through this workflow")
	case item.IsBuilding:
		return clarify("How should the in-production stock be completed or cancelled first?", "is_building", "stock currently being built cannot be transferred")
	case item.Build != nil:
		retry["build_id"] = *item.Build
		return clarify("How should the build-linked stock be handled?", "build", "build-linked stock is outside the ordinary-stock transfer contract")
	case item.ConsumedBy != nil:
		retry["consumed_by_id"] = *item.ConsumedBy
		return clarify("How should the consumed stock history be handled?", "consumed_by", "stock consumed by a build cannot be transferred")
	case item.BelongsTo != nil:
		retry["belongs_to_id"] = *item.BelongsTo
		return clarify("How should the installed stock item be uninstalled first?", "belongs_to", "stock installed in another item cannot be transferred")
	case item.Parent != nil:
		retry["parent_id"] = *item.Parent
		return clarify("How should the child stock relationship be removed first?", "parent", "a child stock item cannot be transferred")
	case item.InstalledItems == nil:
		return clarify("What is the current installed-item state?", "installed_items", "installed-item context is unavailable, so a safe transfer cannot be proven")
	case *item.InstalledItems != 0:
		retry["installed_items"] = *item.InstalledItems
		return clarify("How should installed child items be removed first?", "installed_items", "stock containing installed items cannot be transferred")
	case item.ChildItems == nil:
		return clarify("What is the current child-item state?", "child_items", "child-item context is unavailable, so a safe transfer cannot be proven")
	case *item.ChildItems != 0:
		retry["child_items"] = *item.ChildItems
		return clarify("How should child stock items be separated first?", "child_items", "stock with child items cannot be transferred")
	case item.Customer != nil:
		retry["customer_id"] = *item.Customer
		return clarify("How should the customer-assigned stock be released first?", "customer", "customer-assigned stock cannot be transferred")
	case item.SalesOrder != nil:
		retry["sales_order_id"] = *item.SalesOrder
		return clarify("How should the sales-order stock be released first?", "sales_order", "sales-order-linked stock cannot be transferred")
	default:
		return nil, out, false
	}
}

func executeStockTransfer(ctx context.Context, store *stockPlanStore, client StockTransferClient, input TransferStockItemInput, plan StockAdjustmentPlan, quantity string) (*mcp.CallToolResult, StockTransferOutput, error) {
	out := StockTransferOutput{Status: StatusOK, DryRun: input.DryRun, Plan: &plan}
	if store == nil {
		return nil, out, errors.New("stock plan store is unavailable")
	}
	if input.DryRun {
		token, err := store.issue(ctx, plan)
		if err != nil {
			if errors.Is(err, errStockPlanCapacity) {
				return stockTransferClarification(out, "When should a new stock-transfer plan be prepared?", "confirmation", "too many confirmation plans are outstanding; wait for a token to expire or execute a reviewed plan", "dry_run", map[string]any{"stock_item_id": input.StockItemID, "destination_location_id": input.DestinationLocationID})
			}
			return nil, out, err
		}
		out.PlanHash = token
		return TextResult(StatusOK), out, nil
	}
	if !input.Confirm {
		return stockTransferClarification(out, "Should this reviewed stock item now be transferred?", "confirmation", "confirm must be true after reviewing the latest stock-transfer dry-run plan", "confirm", map[string]any{"stock_item_id": input.StockItemID, "destination_location_id": input.DestinationLocationID, "dry_run": true, "will_split": plan.Transfer != nil && plan.Transfer.WillSplit})
	}
	if input.PlanHash == "" || !store.consume(ctx, input.PlanHash, plan) {
		return stockTransferClarification(out, "Which current dry-run plan should authorize this stock transfer?", "confirmation", "plan_hash must be the unexpired, single-use token from a matching dry run by the same principal", "plan_hash", map[string]any{"stock_item_id": input.StockItemID, "destination_location_id": input.DestinationLocationID, "dry_run": true})
	}

	mutationErr := client.TransferStock(ctx, inventree.StockTransfer{
		Items:    []inventree.StockAdjustmentItem{{PK: input.StockItemID, Quantity: quantity}},
		Notes:    plan.Reason,
		Location: input.DestinationLocationID,
	})
	if errors.Is(mutationErr, context.Canceled) {
		return nil, out, context.Canceled
	}
	if errors.Is(mutationErr, context.DeadlineExceeded) {
		return nil, out, context.DeadlineExceeded
	}
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, out, safeToolError(ctx, mutationErr)
		}
		return verifyStockTransfer(ctx, client, out, true)
	}
	return verifyStockTransfer(ctx, client, out, false)
}

func verifyStockTransfer(ctx context.Context, client StockTransferClient, out StockTransferOutput, recovered bool) (*mcp.CallToolResult, StockTransferOutput, error) {
	if out.Plan.Transfer != nil && out.Plan.Transfer.WillSplit {
		return verifyPartialStockTransfer(ctx, client, out, recovered)
	}
	current, err := client.GetStockItem(ctx, out.Plan.Before.StockItemID)
	if errors.Is(err, context.Canceled) {
		return nil, out, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, out, context.DeadlineExceeded
	}
	if err != nil {
		return stockTransferPartial(out, nil, "the transfer may have completed, but exact-ID verification failed; use get_stock_item before preparing any new plan")
	}
	safe := sanitizedStockItem(current)
	out.Record = &safe
	_, normalizedQuantity, validQuantity := normalizedStockDecimal(current.Quantity, false)
	if current.PK == out.Plan.Before.StockItemID && validQuantity && math.Abs(normalizedQuantity-out.Plan.After.Quantity) < 1e-9 && equalIntPtr(current.Location, out.Plan.After.LocationID) && stockTransferProjectionEqual(current, out.Plan.Transfer) {
		out.Verified = true
		out.Recovered = recovered
		return TextResult(StatusOK), out, nil
	}
	return stockTransferPartial(out, out.Record, "the exact stock item does not match the reviewed destination, quantity, safety, or provenance state; inspect it with get_stock_item and do not retry blindly")
}

func verifyPartialStockTransfer(ctx context.Context, client StockTransferClient, out StockTransferOutput, recovered bool) (*mcp.CallToolResult, StockTransferOutput, error) {
	current, err := client.GetStockItem(ctx, out.Plan.Before.StockItemID)
	if errors.Is(err, context.Canceled) {
		return nil, out, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, out, context.DeadlineExceeded
	}
	if err != nil {
		return stockTransferPartial(out, nil, "the partial transfer may have completed, but the preserved source item could not be read; use get_stock_item and search_stock_items before retrying")
	}
	safe := sanitizedStockItem(current)
	out.Record = &safe
	expectedRemainder := out.Plan.Transfer.SourceRemainder
	if current.PK != out.Plan.Before.StockItemID || expectedRemainder == nil || math.Abs(current.Quantity-*expectedRemainder) > 1e-9 || !equalIntPtr(current.Location, out.Plan.Before.LocationID) || !stockTransferProjectionEqual(current, out.Plan.Transfer) {
		return stockTransferPartial(out, out.Record, "the preserved source item does not match the reviewed remainder or provenance; inspect it with get_stock_item and do not retry blindly")
	}
	if out.Plan.Transfer.DestinationQuantity == nil {
		return stockTransferPartial(out, out.Record, "the reviewed partial-transfer plan has no destination quantity; inspect the source and destination before retrying")
	}
	page, err := client.SearchStockItemsPage(ctx, inventree.StockItemQuery{PartID: out.Plan.Before.PartID, Limit: 1000})
	if err != nil {
		return stockTransferPartial(out, out.Record, "the source remainder was verified but destination split identity could not be searched; use search_stock_items and get_stock_item before retrying")
	}
	if page.HasMore {
		return stockTransferPartial(out, out.Record, "the source remainder was verified but the bounded destination split search was incomplete; use search_stock_items and get_stock_item before retrying")
	}
	matches := make([]inventree.StockItem, 0, 1)
	for _, item := range page.Results {
		if item.PK == out.Plan.Before.StockItemID || item.Location == nil || *item.Location != out.Plan.Transfer.Destination.ID || math.Abs(item.Quantity-*out.Plan.Transfer.DestinationQuantity) > 1e-9 {
			continue
		}
		if containsStockItemID(out.Plan.Transfer.PreexistingDestinationStockItemIDs, item.PK) {
			continue
		}
		if stockTransferCopiedProjectionEqual(item, out.Plan.Transfer) {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		return stockTransferPartial(out, out.Record, "the source remainder was verified but the destination split identity was not unique; use search_stock_items and get_stock_item to reconcile before retrying")
	}
	destination := sanitizedStockItem(matches[0])
	out.SplitRecord = &destination
	out.Plan.Transfer.DestinationStockItemID = &destination.PK
	out.Verified = true
	out.Recovered = recovered
	return TextResult(StatusOK), out, nil
}

func stockTransferPartial(out StockTransferOutput, record *inventree.StockItem, recovery string) (*mcp.CallToolResult, StockTransferOutput, error) {
	out.Status = StatusPartialFailure
	if record != nil {
		safe := stockItemRecoveryProjection(*record)
		out.Record = &safe
	} else {
		out.Record = nil
	}
	out.Failure = &StockAdjustmentFailure{Action: TransferStockItemToolName, Message: "stock transfer result could not be verified", RecoveryPlan: recovery}
	return TextResult(StatusPartialFailure), out, nil
}

func stockTransferClarification(out StockTransferOutput, question, subject, reason, retry string, fields map[string]any) (*mcp.CallToolResult, StockTransferOutput, error) {
	clarification := NewClarification(question, subject, reason, retry, true, nil, fields)
	out.Status = StatusClarificationRequired
	out.Clarification = &clarification
	return TextResult(StatusClarificationRequired), out, nil
}

func stockTransferLocation(location inventree.StockLocation) StockTransferLocation {
	path := append([]inventree.TreePath(nil), location.Path...)
	return StockTransferLocation{ID: location.PK, Name: location.Name, ParentID: location.Parent, Path: path, Structural: location.Structural, External: location.External, OwnerID: location.Owner, LocationType: location.LocationType}
}

func stockTransferProvenance(item inventree.StockItem) StockTransferProvenance {
	return StockTransferProvenance{
		PartID: item.Part, Status: item.Status, StatusCustomKey: item.StatusCustomKey, Batch: item.Batch,
		ExpiryDate: item.ExpiryDate, Packaging: item.Packaging, DeleteOnDeplete: item.DeleteOnDeplete,
		OwnerID: item.Owner, SupplierPartID: item.SupplierPart, PurchaseOrderID: item.PurchaseOrder,
		PurchasePrice: item.PurchasePrice, PurchasePriceCurrency: item.PurchasePriceCurrency, CreationDate: item.CreationDate,
	}
}

func stockTransferSafety(item inventree.StockItem) StockTransferSafety {
	return StockTransferSafety{
		InStock: item.InStock, Allocated: item.Allocated, Serial: item.Serial, IsBuilding: item.IsBuilding,
		BuildID: item.Build, ConsumedByID: item.ConsumedBy, BelongsToID: item.BelongsTo, ParentID: item.Parent,
		InstalledItems: item.InstalledItems, ChildItems: item.ChildItems,
		CustomerID: item.Customer, SalesOrderID: item.SalesOrder,
	}
}

func stockTransferProjectionEqual(item inventree.StockItem, expected *StockTransferContext) bool {
	return stockTransferProjectionEqualWithCreationDate(item, expected, true)
}

func stockTransferCopiedProjectionEqual(item inventree.StockItem, expected *StockTransferContext) bool {
	return stockTransferProjectionEqualWithCreationDate(item, expected, false)
}

func stockTransferProjectionEqualWithCreationDate(item inventree.StockItem, expected *StockTransferContext, includeCreationDate bool) bool {
	if expected == nil {
		return false
	}
	provenance := stockTransferProvenance(item)
	wantProvenance := expected.Provenance
	if !includeCreationDate {
		provenance.CreationDate = nil
		wantProvenance.CreationDate = nil
	}
	actual := struct {
		Provenance StockTransferProvenance `json:"provenance"`
		Safety     StockTransferSafety     `json:"safety"`
	}{Provenance: provenance, Safety: stockTransferSafety(item)}
	want := struct {
		Provenance StockTransferProvenance `json:"provenance"`
		Safety     StockTransferSafety     `json:"safety"`
	}{Provenance: wantProvenance, Safety: expected.Safety}
	actualJSON, _ := json.Marshal(actual)
	wantJSON, _ := json.Marshal(want)
	return string(actualJSON) == string(wantJSON)
}

func containsStockItemID(ids []int, want int) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
