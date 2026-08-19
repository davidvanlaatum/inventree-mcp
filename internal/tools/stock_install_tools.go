package tools

import (
	"context"
	"errors"
	"strings"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stockInstallTopologyMaxRecords bounds the ancestor walk used to detect a
// belongs_to cycle before install_stock_item writes a new relationship,
// mirroring part_family_tools.go's shared-budget traversal convention.
const stockInstallTopologyMaxRecords = 64

type StockInstallClient interface {
	GetStockItem(context.Context, int) (inventree.StockItem, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	SearchBomItems(context.Context, inventree.BomItemQuery) ([]inventree.BomItem, error)
	InstallStockItem(context.Context, int, inventree.InstallStockItem) error
	UninstallStockItem(context.Context, int, inventree.UninstallStockItem) error
}

type InstallStockItemInput struct {
	DryRun            bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound install plan without writing."`
	Confirm           bool   `json:"confirm,omitempty" jsonschema:"Required true for execution after reviewing a dry run."`
	PlanHash          string `json:"plan_hash,omitempty" jsonschema:"Opaque single-use confirmation token returned by the latest dry run."`
	ParentStockItemID int    `json:"parent_stock_item_id" jsonschema:"Existing target stock-item primary key that will receive the installed child."`
	ChildStockItemID  int    `json:"child_stock_item_id" jsonschema:"Existing quantity-1 stock-item primary key to install into the parent."`
	Reason            string `json:"reason" jsonschema:"Nonblank operator audit reason recorded with the install."`
}

type UninstallStockItemInput struct {
	DryRun                bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound uninstall plan without writing."`
	Confirm               bool   `json:"confirm,omitempty" jsonschema:"Required true for execution after reviewing a dry run."`
	PlanHash              string `json:"plan_hash,omitempty" jsonschema:"Opaque single-use confirmation token returned by the latest dry run."`
	StockItemID           int    `json:"stock_item_id" jsonschema:"Existing currently-installed child stock-item primary key to uninstall."`
	DestinationLocationID int    `json:"destination_location_id" jsonschema:"Explicit existing destination stock-location primary key for the removed item."`
	Reason                string `json:"reason" jsonschema:"Nonblank operator audit reason recorded with the uninstall."`
}

func installStockItem(deps Dependencies) mcp.ToolHandlerFor[InstallStockItemInput, StockTransferOutput] {
	return LookupHandler[StockInstallClient, InstallStockItemInput, StockTransferOutput](deps, InstallStockItemToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockInstallClient, input InstallStockItemInput) (*mcp.CallToolResult, StockTransferOutput, error) {
			out := StockTransferOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.ParentStockItemID <= 0 {
				return stockInstallClarification(out, "Which stock item should receive the installed child?", "parent_stock_item", "parent_stock_item_id must be positive", "parent_stock_item_id", map[string]any{"parent_stock_item_id": input.ParentStockItemID})
			}
			if input.ChildStockItemID <= 0 {
				return stockInstallClarification(out, "Which stock item should be installed?", "child_stock_item", "child_stock_item_id must be positive", "child_stock_item_id", map[string]any{"child_stock_item_id": input.ChildStockItemID})
			}
			if input.ParentStockItemID == input.ChildStockItemID {
				return stockInstallClarification(out, "Which distinct parent and child stock items should be linked?", "child_stock_item", "a stock item cannot be installed into itself", "child_stock_item_id", map[string]any{"parent_stock_item_id": input.ParentStockItemID, "child_stock_item_id": input.ChildStockItemID})
			}
			reason := strings.TrimSpace(input.Reason)
			if reason == "" {
				return stockInstallClarification(out, "What audit reason should be recorded for this install?", "reason", "a nonblank operator reason is required", "reason", map[string]any{"parent_stock_item_id": input.ParentStockItemID, "child_stock_item_id": input.ChildStockItemID})
			}

			child, err := client.GetStockItem(ctx, input.ChildStockItemID)
			if err != nil {
				if isNotFound(err) {
					out.Status = StatusNotFound
					return TextResult(StatusNotFound), out, nil
				}
				return nil, out, safeToolError(err)
			}
			if child.PK != input.ChildStockItemID {
				return nil, out, errors.New("InvenTree returned a mismatched child stock-item identity")
			}
			quantity, _, ok := normalizedStockDecimal(child.Quantity, false)
			if !ok || quantity != "1" {
				return stockInstallClarification(out, "Which single-quantity stock item should be installed?", "child_stock_item", "the child stock item must have exactly quantity 1; partial-quantity installs are outside this workflow", "child_stock_item_id", map[string]any{"child_stock_item_id": input.ChildStockItemID, "child_quantity": child.Quantity})
			}
			if child.BelongsTo != nil {
				return stockInstallClarification(out, "How should the already-installed child be uninstalled first?", "child_stock_item", "the child stock item is already installed in another stock item", "child_stock_item_id", map[string]any{"child_stock_item_id": input.ChildStockItemID, "belongs_to_id": *child.BelongsTo})
			}
			if !child.InStock {
				return stockInstallClarification(out, "Which currently in-stock child should be installed?", "child_stock_item", "the child stock item is not currently in stock", "child_stock_item_id", map[string]any{"child_stock_item_id": input.ChildStockItemID})
			}
			if result, clarified, unsafe := unsafeStockInstallChild(out, child); unsafe {
				return result, clarified, nil
			}

			parent, err := client.GetStockItem(ctx, input.ParentStockItemID)
			if err != nil {
				if isNotFound(err) {
					return stockInstallClarification(out, "Which existing stock item should receive the installed child?", "parent_stock_item", "parent_stock_item_id was not found", "parent_stock_item_id", map[string]any{"parent_stock_item_id": input.ParentStockItemID})
				}
				return nil, out, safeToolError(err)
			}
			if parent.PK != input.ParentStockItemID {
				return nil, out, errors.New("InvenTree returned a mismatched parent stock-item identity")
			}
			if result, clarified, unsafe := unsafeStockInstallParent(out, parent); unsafe {
				return result, clarified, nil
			}

			if result, clarified, cyclic, cycleErr := stockInstallCycleCheck(ctx, client, out, child.PK, parent); cyclic {
				return result, clarified, cycleErr
			}

			bomItems, err := client.SearchBomItems(ctx, inventree.BomItemQuery{Uses: child.Part, Limit: DefaultLookupLimit})
			if err != nil {
				return nil, out, safeToolError(err)
			}
			if !bomIncludesAssembly(bomItems, parent.Part) {
				return stockInstallClarification(out, "Which BOM-eligible child part should be installed?", "child_stock_item", "the child part is not in the parent part's Bill of Materials", "child_stock_item_id", map[string]any{"parent_stock_item_id": input.ParentStockItemID, "parent_part_id": parent.Part, "child_stock_item_id": input.ChildStockItemID, "child_part_id": child.Part})
			}

			after := snapshotStockItem(child)
			after.LocationID = nil
			plan := StockAdjustmentPlan{
				Action: InstallStockItemToolName,
				Before: snapshotStockItem(child),
				After:  after,
				Reason: reason,
				Install: &StockInstallContext{
					ParentID: parent.PK, ParentPartID: parent.Part, ParentInstalledItemsBefore: parent.InstalledItems,
					ChildPartID: child.Part, ChildBelongsToBefore: child.BelongsTo, ChildBelongsToAfter: &parent.PK,
					BOMConfirmed: true, Safety: stockTransferSafety(child),
				},
			}
			return executeStockInstall(ctx, deps.stockPlanStore, client, input.DryRun, input.Confirm, input.PlanHash, plan)
		})
}

func uninstallStockItem(deps Dependencies) mcp.ToolHandlerFor[UninstallStockItemInput, StockTransferOutput] {
	return LookupHandler[StockInstallClient, UninstallStockItemInput, StockTransferOutput](deps, UninstallStockItemToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockInstallClient, input UninstallStockItemInput) (*mcp.CallToolResult, StockTransferOutput, error) {
			out := StockTransferOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.StockItemID <= 0 {
				return stockInstallClarification(out, "Which installed stock item should be uninstalled?", "stock_item", "stock_item_id must be positive", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID})
			}
			if input.DestinationLocationID <= 0 {
				return stockInstallClarification(out, "Which explicit destination should receive the uninstalled item?", "destination_location", "destination_location_id must be positive", "destination_location_id", map[string]any{"stock_item_id": input.StockItemID, "destination_location_id": input.DestinationLocationID})
			}
			reason := strings.TrimSpace(input.Reason)
			if reason == "" {
				return stockInstallClarification(out, "What audit reason should be recorded for this uninstall?", "reason", "a nonblank operator reason is required", "reason", map[string]any{"stock_item_id": input.StockItemID})
			}

			item, err := client.GetStockItem(ctx, input.StockItemID)
			if err != nil {
				if isNotFound(err) {
					out.Status = StatusNotFound
					return TextResult(StatusNotFound), out, nil
				}
				return nil, out, safeToolError(err)
			}
			if item.PK != input.StockItemID {
				return nil, out, errors.New("InvenTree returned a mismatched stock-item identity")
			}
			if item.BelongsTo == nil {
				return stockInstallClarification(out, "Which currently-installed stock item should be uninstalled?", "stock_item", "InvenTree does not itself reject uninstalling an item that is not currently installed anywhere, so this workflow requires an existing belongs_to relationship first", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID})
			}
			parentID := *item.BelongsTo
			if result, clarified, unsafe := unsafeStockInstallChild(out, item); unsafe {
				return result, clarified, nil
			}

			destination, err := client.GetStockLocation(ctx, input.DestinationLocationID)
			if err != nil {
				if isNotFound(err) {
					return stockInstallClarification(out, "Which existing stock location should receive the item?", "destination_location", "destination_location_id was not found", "destination_location_id", map[string]any{"stock_item_id": input.StockItemID, "destination_location_id": input.DestinationLocationID})
				}
				return nil, out, safeToolError(err)
			}
			if destination.PK != input.DestinationLocationID {
				return nil, out, errors.New("InvenTree returned a mismatched destination-location identity")
			}

			destinationID := destination.PK
			after := snapshotStockItem(item)
			after.LocationID = &destinationID
			plan := StockAdjustmentPlan{
				Action: UninstallStockItemToolName,
				Before: snapshotStockItem(item),
				After:  after,
				Reason: reason,
				Install: &StockInstallContext{
					ParentID: parentID, ChildPartID: item.Part, ChildBelongsToBefore: item.BelongsTo, ChildBelongsToAfter: nil,
					Destination: dvgoutils.Ptr(stockTransferLocation(destination)), Safety: stockTransferSafety(item),
				},
			}
			return executeStockUninstall(ctx, deps.stockPlanStore, client, input.DryRun, input.Confirm, input.PlanHash, plan)
		})
}

// unsafeStockInstallChild guards fields that must be clear on the CHILD
// stock item for both install and uninstall. installed_items is
// deliberately not blocked here: this workflow explicitly supports
// installing an item that is itself a nested sub-assembly. belongs_to and
// in_stock are intentionally NOT checked here -- install and uninstall
// require opposite states for those two fields, so each caller checks them
// directly before calling this shared guard.
func unsafeStockInstallChild(out StockTransferOutput, item inventree.StockItem) (*mcp.CallToolResult, StockTransferOutput, bool) {
	retry := map[string]any{"stock_item_id": item.PK, "dry_run": true}
	clarify := func(question, subject, reason string) (*mcp.CallToolResult, StockTransferOutput, bool) {
		result, clarified, _ := stockInstallClarification(out, question, subject, reason, "stock_item_id", retry)
		return result, clarified, true
	}
	switch {
	case item.Allocated == nil:
		return clarify("What is the current stock allocation state?", "allocated", "allocation context is unavailable, so a safe relationship change cannot be proven")
	case *item.Allocated != 0:
		retry["allocated"] = *item.Allocated
		return clarify("How should the allocated stock be released first?", "allocated", "allocated stock cannot be installed or uninstalled through this workflow")
	case item.IsBuilding:
		return clarify("How should the in-production stock item be completed or cancelled first?", "is_building", "stock currently being built cannot be installed or uninstalled through this workflow")
	case item.ConsumedBy != nil:
		retry["consumed_by_id"] = *item.ConsumedBy
		return clarify("How should the consumed stock history be handled?", "consumed_by", "stock consumed by a build cannot be installed or uninstalled")
	case item.Parent != nil:
		retry["parent_id"] = *item.Parent
		return clarify("How should the split-stock parent relationship be removed first?", "parent", "a split-stock child item cannot be installed or uninstalled through this workflow")
	case item.ChildItems == nil:
		return clarify("What is the current split-stock child-item state?", "child_items", "split-stock child-item context is unavailable, so a safe relationship change cannot be proven")
	case *item.ChildItems != 0:
		retry["child_items"] = *item.ChildItems
		return clarify("How should split-stock child items be separated first?", "child_items", "stock with split-stock child items cannot be installed or uninstalled through this workflow")
	case item.Customer != nil:
		retry["customer_id"] = *item.Customer
		return clarify("How should the customer-assigned stock be released first?", "customer", "customer-assigned stock cannot be installed or uninstalled")
	case item.SalesOrder != nil:
		retry["sales_order_id"] = *item.SalesOrder
		return clarify("How should the sales-order stock be released first?", "sales_order", "sales-order-linked stock cannot be installed or uninstalled")
	}
	return nil, out, false
}

// unsafeStockInstallParent guards fields that must be clear on the PARENT
// (install target) stock item. belongs_to and installed_items are
// deliberately not blocked: an assembly may already be nested inside a
// larger assembly, or already contain other installed items.
func unsafeStockInstallParent(out StockTransferOutput, item inventree.StockItem) (*mcp.CallToolResult, StockTransferOutput, bool) {
	retry := map[string]any{"parent_stock_item_id": item.PK, "dry_run": true}
	clarify := func(question, subject, reason string) (*mcp.CallToolResult, StockTransferOutput, bool) {
		result, clarified, _ := stockInstallClarification(out, question, subject, reason, "parent_stock_item_id", retry)
		return result, clarified, true
	}
	switch {
	case item.Allocated == nil:
		return clarify("What is the current stock allocation state?", "allocated", "allocation context is unavailable, so a safe install cannot be proven")
	case *item.Allocated != 0:
		retry["allocated"] = *item.Allocated
		return clarify("How should the allocated parent stock be released first?", "allocated", "allocated stock cannot receive an installed item through this workflow")
	case item.IsBuilding:
		return clarify("How should the in-production build be completed or cancelled first?", "is_building", "a stock item currently being built is lifecycle-owned by the build order, not this workflow")
	case item.ConsumedBy != nil:
		retry["consumed_by_id"] = *item.ConsumedBy
		return clarify("How should the consumed stock history be handled?", "consumed_by", "stock consumed by a build cannot receive an installed item")
	case item.Parent != nil:
		retry["parent_id"] = *item.Parent
		return clarify("How should the split-stock parent relationship be removed first?", "parent", "a split-stock child item cannot receive an installed item through this workflow")
	case item.ChildItems == nil:
		return clarify("What is the current split-stock child-item state?", "child_items", "split-stock child-item context is unavailable, so a safe install cannot be proven")
	case *item.ChildItems != 0:
		retry["child_items"] = *item.ChildItems
		return clarify("How should split-stock child items be separated first?", "child_items", "stock with split-stock child items cannot receive an installed item through this workflow")
	case item.Customer != nil:
		retry["customer_id"] = *item.Customer
		return clarify("How should the customer-assigned stock be released first?", "customer", "customer-assigned stock cannot receive an installed item")
	case item.SalesOrder != nil:
		retry["sales_order_id"] = *item.SalesOrder
		return clarify("How should the sales-order stock be released first?", "sales_order", "sales-order-linked stock cannot receive an installed item")
	}
	return nil, out, false
}

// stockInstallCycleCheck walks the parent's belongs_to ancestor chain within
// a shared record budget, refusing the install if the child stock item
// already appears among the parent's ancestors -- which would make the
// child both an ancestor and, after the new link, a descendant of itself.
func stockInstallCycleCheck(ctx context.Context, client StockInstallClient, out StockTransferOutput, childID int, parent inventree.StockItem) (*mcp.CallToolResult, StockTransferOutput, bool, error) {
	retryFields := map[string]any{"parent_stock_item_id": parent.PK, "child_stock_item_id": childID}
	failClosed := func(reason string) (*mcp.CallToolResult, StockTransferOutput, bool, error) {
		result, clarified, err := stockInstallClarification(out, "When should this install be retried?", "confirmation", reason, "dry_run", retryFields)
		return result, clarified, true, err
	}
	seen := map[int]bool{parent.PK: true}
	current := parent
	for steps := 0; ; steps++ {
		if current.BelongsTo == nil {
			return nil, out, false, nil
		}
		if *current.BelongsTo == childID {
			result, clarified, err := stockInstallClarification(out, "Which non-circular parent/child pair should be linked?", "child_stock_item", "installing this child would create a cycle: the parent is already nested inside the child", "child_stock_item_id", retryFields)
			return result, clarified, true, err
		}
		if steps >= stockInstallTopologyMaxRecords {
			return failClosed("the parent's installed-in ancestor chain exceeded the shared traversal budget, so a complete cycle-free relationship cannot be proven")
		}
		if seen[*current.BelongsTo] {
			return failClosed("the parent's existing installed-in ancestor chain already contains a cycle and cannot be proven safe")
		}
		next, err := client.GetStockItem(ctx, *current.BelongsTo)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, out, true, err
			}
			if isNotFound(err) {
				return failClosed("an ancestor in the parent's installed-in chain no longer exists, so a complete cycle-free relationship cannot be proven")
			}
			// Any other error (network failure, 5xx, etc.) also fails closed:
			// the ancestor chain could not be completely read, so a
			// cycle-free relationship cannot be proven, regardless of why
			// the read failed.
			return failClosed("the parent's installed-in ancestor chain could not be completely read, so a cycle-free relationship cannot be proven")
		}
		seen[next.PK] = true
		current = next
	}
}

func bomIncludesAssembly(items []inventree.BomItem, assemblyPartID int) bool {
	for _, item := range items {
		if item.Part == assemblyPartID {
			return true
		}
	}
	return false
}

func executeStockInstall(ctx context.Context, store *stockPlanStore, client StockInstallClient, dryRun, confirm bool, suppliedToken string, plan StockAdjustmentPlan) (*mcp.CallToolResult, StockTransferOutput, error) {
	out, result, done, err := stockInstallIssueOrConfirm(ctx, store, dryRun, confirm, suppliedToken, plan)
	if done {
		return result, out, err
	}
	mutationErr := client.InstallStockItem(ctx, plan.Install.ParentID, inventree.InstallStockItem{StockItem: plan.Before.StockItemID, Quantity: 1, Note: plan.Reason})
	if errors.Is(mutationErr, context.Canceled) {
		return nil, out, context.Canceled
	}
	if errors.Is(mutationErr, context.DeadlineExceeded) {
		return nil, out, context.DeadlineExceeded
	}
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, out, safeToolError(mutationErr)
		}
		return verifyStockInstall(ctx, client, out, true)
	}
	return verifyStockInstall(ctx, client, out, false)
}

func executeStockUninstall(ctx context.Context, store *stockPlanStore, client StockInstallClient, dryRun, confirm bool, suppliedToken string, plan StockAdjustmentPlan) (*mcp.CallToolResult, StockTransferOutput, error) {
	out, result, done, err := stockInstallIssueOrConfirm(ctx, store, dryRun, confirm, suppliedToken, plan)
	if done {
		return result, out, err
	}
	destinationID := 0
	if plan.After.LocationID != nil {
		destinationID = *plan.After.LocationID
	}
	mutationErr := client.UninstallStockItem(ctx, plan.Before.StockItemID, inventree.UninstallStockItem{Location: destinationID, Note: plan.Reason})
	if errors.Is(mutationErr, context.Canceled) {
		return nil, out, context.Canceled
	}
	if errors.Is(mutationErr, context.DeadlineExceeded) {
		return nil, out, context.DeadlineExceeded
	}
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
			return nil, out, safeToolError(mutationErr)
		}
		return verifyStockUninstall(ctx, client, out, true)
	}
	return verifyStockUninstall(ctx, client, out, false)
}

func stockInstallIssueOrConfirm(ctx context.Context, store *stockPlanStore, dryRun, confirm bool, suppliedToken string, plan StockAdjustmentPlan) (StockTransferOutput, *mcp.CallToolResult, bool, error) {
	out := StockTransferOutput{Status: StatusOK, DryRun: dryRun, Plan: &plan}
	if store == nil {
		return out, nil, true, errors.New("stock plan store is unavailable")
	}
	if dryRun {
		token, err := store.issue(ctx, plan)
		if err != nil {
			if errors.Is(err, errStockPlanCapacity) {
				result, clarified, clarifyErr := stockInstallClarification(out, "When should a new install/uninstall confirmation plan be prepared?", "confirmation", "too many confirmation plans are outstanding; wait for a token to expire or execute a reviewed plan", "dry_run", map[string]any{"stock_item_id": plan.Before.StockItemID})
				return clarified, result, true, clarifyErr
			}
			return out, nil, true, err
		}
		out.PlanHash = token
		return out, TextResult(StatusOK), true, nil
	}
	if !confirm {
		result, clarified, err := stockInstallClarification(out, "Should this reviewed plan now be executed?", "confirmation", "confirm must be true after reviewing the latest dry-run plan", "confirm", map[string]any{"stock_item_id": plan.Before.StockItemID, "dry_run": true})
		return clarified, result, true, err
	}
	if suppliedToken == "" || !store.consume(ctx, suppliedToken, plan) {
		result, clarified, err := stockInstallClarification(out, "Which current dry-run plan should authorize this change?", "confirmation", "plan_hash must be the unexpired, single-use token from a matching dry run by the same principal", "plan_hash", map[string]any{"stock_item_id": plan.Before.StockItemID, "dry_run": true})
		return clarified, result, true, err
	}
	return out, nil, false, nil
}

func verifyStockInstall(ctx context.Context, client StockInstallClient, out StockTransferOutput, recovered bool) (*mcp.CallToolResult, StockTransferOutput, error) {
	child, err := client.GetStockItem(ctx, out.Plan.Before.StockItemID)
	if errors.Is(err, context.Canceled) {
		return nil, out, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, out, context.DeadlineExceeded
	}
	if err != nil {
		return stockInstallPartial(out, nil, "the install may have completed, but exact-ID verification failed; use get_stock_item before preparing any new plan")
	}
	safe := sanitizedStockItem(child)
	out.Record = &safe
	if child.PK != out.Plan.Before.StockItemID || child.BelongsTo == nil || *child.BelongsTo != out.Plan.Install.ParentID || child.Location != nil {
		return stockInstallPartial(out, out.Record, "the exact child stock item does not match the reviewed install relationship; inspect it with get_stock_item and do not retry blindly")
	}
	// The relationship itself lives entirely on the child's belongs_to
	// field, already proven above; this second lookup additionally proves
	// the parent's own installed_items count reflects it, verifying "both
	// sides" of the relationship rather than only the child.
	parent, parentErr := client.GetStockItem(ctx, out.Plan.Install.ParentID)
	if errors.Is(parentErr, context.Canceled) {
		return nil, out, context.Canceled
	}
	if errors.Is(parentErr, context.DeadlineExceeded) {
		return nil, out, context.DeadlineExceeded
	}
	if parentErr != nil {
		return stockInstallPartial(out, out.Record, "the child side of the install verified, but the parent's installed-item count could not be re-read; inspect the parent with get_stock_item before preparing any new plan")
	}
	if parent.PK != out.Plan.Install.ParentID {
		return stockInstallPartial(out, out.Record, "the parent stock item does not match the reviewed install relationship; inspect it with get_stock_item and do not retry blindly")
	}
	if parent.InstalledItems != nil && out.Plan.Install.ParentInstalledItemsBefore != nil && *parent.InstalledItems != *out.Plan.Install.ParentInstalledItemsBefore+1 {
		return stockInstallPartial(out, out.Record, "the parent's installed-item count does not match the reviewed install relationship; inspect it with get_stock_item and do not retry blindly")
	}
	out.Verified = true
	out.Recovered = recovered
	return TextResult(StatusOK), out, nil
}

func verifyStockUninstall(ctx context.Context, client StockInstallClient, out StockTransferOutput, recovered bool) (*mcp.CallToolResult, StockTransferOutput, error) {
	item, err := client.GetStockItem(ctx, out.Plan.Before.StockItemID)
	if errors.Is(err, context.Canceled) {
		return nil, out, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, out, context.DeadlineExceeded
	}
	if err != nil {
		return stockInstallPartial(out, nil, "the uninstall may have completed, but exact-ID verification failed; use get_stock_item before preparing any new plan")
	}
	safe := sanitizedStockItem(item)
	out.Record = &safe
	if item.PK != out.Plan.Before.StockItemID || item.BelongsTo != nil || !equalIntPtr(item.Location, out.Plan.After.LocationID) {
		return stockInstallPartial(out, out.Record, "the exact stock item does not match the reviewed uninstall destination; inspect it with get_stock_item and do not retry blindly")
	}
	out.Verified = true
	out.Recovered = recovered
	return TextResult(StatusOK), out, nil
}

func stockInstallPartial(out StockTransferOutput, record *inventree.StockItem, recovery string) (*mcp.CallToolResult, StockTransferOutput, error) {
	out.Status = StatusPartialFailure
	if record != nil {
		safe := stockItemRecoveryProjection(*record)
		out.Record = &safe
	} else {
		out.Record = nil
	}
	out.Failure = &StockAdjustmentFailure{Action: out.Plan.Action, Message: "install/uninstall result could not be verified", RecoveryPlan: recovery}
	return TextResult(StatusPartialFailure), out, nil
}

func stockInstallClarification(out StockTransferOutput, question, subject, reason, retry string, fields map[string]any) (*mcp.CallToolResult, StockTransferOutput, error) {
	clarification := NewClarification(question, subject, reason, retry, true, nil, fields)
	out.Status = StatusClarificationRequired
	out.Clarification = &clarification
	return TextResult(StatusClarificationRequired), out, nil
}
