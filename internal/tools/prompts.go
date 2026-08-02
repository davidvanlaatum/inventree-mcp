package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	PromptMilestone1 = "milestone_1"
	PromptFuture     = "future"

	NewPartEntryChecklistPromptName         = "new_part_entry_checklist"
	ParameterReuseChecklistPromptName       = "parameter_reuse_checklist"
	AttachmentImageChecklistPromptName      = "attachment_image_checklist"
	InitialStockEntryChecklistPromptName    = "initial_stock_entry_checklist"
	PurchasePreviewChecklistPromptName      = "purchase_preview_checklist"
	ReceivePurchaseOrderChecklistPromptName = "receive_purchase_order_checklist"
	StocktakeReviewPromptName               = "stocktake_review"
	ParameterConsistencyReviewPromptName    = "parameter_consistency_review"
)

type PromptManifestEntry struct {
	Name        string
	Title       string
	Description string
	Status      string
	Checklist   string
}

var PromptManifest = []PromptManifestEntry{
	{
		Name:        NewPartEntryChecklistPromptName,
		Title:       "New part entry checklist",
		Description: "Checklist for adding or updating a purchasable part without guessing identity, category, supplier, or manufacturer data.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before adding or updating a purchasable part:
- Search for existing parts, categories, suppliers, manufacturers, supplier parts, and manufacturer parts before writing.
- Ask for stable IDs when search results are ambiguous; retry with part_id, category_id, supplier_id, manufacturer_id, supplier_part_id, or manufacturer_part_id.
- Do not invent categories, units, supplier SKUs, manufacturer part numbers, compliance status, or revision data.
- Prefer upsert_part_with_supplier_and_manufacturer with dry_run:true before any write.
- Treat omitted recommended fields separately from API-required fields and return a structured clarification when required values are missing.`,
	},
	{
		Name:        ParameterReuseChecklistPromptName,
		Title:       "Parameter reuse checklist",
		Description: "Checklist for reusing existing parameter templates and asking for clarification before creating parameter values.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before setting part parameters:
- Search existing parameter templates and read the part's current parameters first.
- Prefer templates already linked to the part category; show global or unlinked matches as context only.
- Return structured clarification and ask the operator to choose a stable template_id when same-name templates differ by unit, choices, checkbox behavior, or category link.
- Reuse an existing template whenever it is suitable. Use create_parameter_template only when the operator supplies every explicit template field and the collision preflight finds no same-name template.
- Manage category defaults with stable direct link IDs. Search exact-category defaults by default; use include_parent_defaults:true only to review inherited values, and update or delete an inherited link through its reported source category.
- Use merge_parameter_templates with dry_run:true before consolidating duplicates. Never overwrite a part that already has both source and target rows; resolve reported conflicts and category-default links explicitly.
- Retry set_part_parameters only with stable part_id, template_id or parameter_id, and an explicit value shape.`,
	},
	{
		Name:        ParameterConsistencyReviewPromptName,
		Title:       "Parameter consistency review",
		Description: "Checklist for bounded read-only parameter audits and confirmed single-template propagation.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before auditing or bulk-propagating part parameters:
- Run audit_parameter_consistency without filters only when the combined upstream request-and-record workload fits the 1,000-unit scan budget; otherwise narrow by template_id or exact category_id.
- Review duplicate template names, incompatible units or choices, duplicate rows, overloaded same-name fields, unlinked usage, and category-default mismatches. The audit never writes or cleans up records.
- For propagation, choose one existing enabled part-compatible template_id, one explicit value, and exactly one selector: at most 100 stable part_ids or one category_id. Category selection is exact unless include_subcategories:true.
- Return structured clarification instead of guessing when template, value, part, category, descendant scope, overwrite intent, or current-state confirmation is missing or ambiguous.
- Templates and category links are never created implicitly, and parameter rows are never deleted. Existing differing values remain manual decisions unless overwrite_existing:true is explicit.
- Call bulk_propagate_part_parameters with dry_run:true and review every planned, skipped, and manual_required action. To execute, preserve the business fields, set dry_run:false or omit it, and add confirm:true plus the returned plan_hash within five minutes from the same principal.
- Propagation is conservatively destructive because overwrite_existing:true can replace values, so HTTP mode requires inventree.destructive as well as read and write scopes. Treat execution as single-writer work: every action is rechecked immediately before mutation, but the upstream API offers no atomic compare-and-set across that last check and write.
- A newer matching dry run supersedes the older token. Any inventory change, restart, expired or reused token, or write/read-back failure requires current-state inspection and a fresh dry run; never replay the old plan.`,
	},
	{
		Name:        AttachmentImageChecklistPromptName,
		Title:       "Attachment and image checklist",
		Description: "Checklist for attachment/image reads, uploads, links, metadata updates, deletes, and primary-image replacement.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before attachment/image reads, writes, or primary-image replacement work:
- Resolve the target object type and stable object ID before listing or downloading attachments and part images.
- Current milestone tools can list metadata, download schema-exposed attachment or part-image content, upload inline or allowlisted local files, upload URL copies, create stored links, update metadata, delete confirmed attachments, and set a primary part image from a stable same-part image attachment.
- Keep upload sources distinct: inline bytes, STDIO allowlisted local paths, URL-upload copy, and stored links are separate intents.
- Ask for structured clarification when target object identity, URL intent, original versus thumbnail mode, filename/content/link duplicates, primary-image attachment selection, or replacement confirmation is ambiguous.
- Do not fetch stored link targets; download only schema-exposed InvenTree file, thumbnail, or part-image URLs.
- Require confirm:true before deleting attachments and before replacing an existing primary part image.`,
	},
	{
		Name:        InitialStockEntryChecklistPromptName,
		Title:       "Initial stock entry checklist",
		Description: "Checklist for creating initial stock with stable part/location IDs, duplicate checks, and dry-run planning.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before creating initial stock:
- Resolve the part and stock location to stable part_id and location_id.
- Confirm quantity is positive and stock status follows the operator's local convention.
- Search existing stock items for the same part and location before writing.
- Prefer create_initial_stock_entry with dry_run:true so the operator can review duplicate-preflight results.
- Return structured clarification instead of creating stock when part, location, quantity, status, or duplicate intent is ambiguous.`,
	},
	{
		Name:        PurchasePreviewChecklistPromptName,
		Title:       "Purchase preview checklist",
		Description: "Checklist for no-write purchase previews with explicit supplier-part validation.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before previewing purchase order lines:
- Resolve one supplier and each line's supplier part by stable supplier_part_id when available.
- When using part_id and supplier_sku instead, require the lookup to match exactly one supplier-part link for the selected supplier.
- Confirm each quantity is positive and pair any unit_price with currency.
- Treat missing prices, package multiples, minimum order quantities, and supplier price breaks as preview warnings or structured clarification, not guessed values.
- Use preview_purchase_order_with_lines only for no-write output; it must not create purchase orders or purchase-order lines.`,
	},
	{
		Name:        ReceivePurchaseOrderChecklistPromptName,
		Title:       "Receive purchase order checklist",
		Description: "Checklist for dry-run purchase-order receiving with stable line and location IDs, outstanding-quantity checks, and explicit confirmation.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before receiving purchase-order items:
- Resolve the purchase order, each purchase-order line, and every effective stock location to stable IDs.
- Use dry_run:true first and verify each requested quantity is schema-valid, positive, and no greater than the line's outstanding quantity; virtual parts are excluded because they do not create stock.
- Resolve receiving location in this order: item location_id, purchase-order line destination, then global location_id.
- Receiving creates new stock items through InvenTree's native purchase-order receive endpoint; it never merges into or updates existing stock.
- Submit the exact dry-run plan_hash with confirm_receive:true for the operational write; invalid input returns structured clarification and changed state requires a new dry run.
- If execution returns partial_failure, read line and stock state before preparing a new plan and never retry the receipt blindly.`,
	},
	{
		Name:        "bom_import_review",
		Title:       "BOM import review",
		Description: "Future checklist for BOM import review workflows.",
		Status:      PromptFuture,
	},
	{
		Name:        StocktakeReviewPromptName,
		Title:       "Stocktake review",
		Description: "Checklist for current-state-bound single-item stock adjustments with audit reasons and explicit confirmation.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before changing an existing stock item:
- Resolve exactly one stable stock_item_id and read its current quantity, location, status, batch, serial, packaging, and delete_on_deplete state.
- Return structured clarification instead of guessing when stock identity, observed quantity, target status, or audit reason is missing or ambiguous.
- Use adjust_stock_quantity for a relative non-zero delta, stocktake_adjustment for an absolute physical count, and set_stock_status only for a status change; do not combine hidden location or metadata changes.
- Supply a nonblank operator audit reason and run dry_run:true before every execution.
- Review the before/after plan and high-risk warning. Quantity decreases and Destroyed, Rejected, or Lost statuses require especially careful review.
- Refuse no-op changes, refuse relative or absolute quantity changes for serialized stock, and refuse a target quantity of zero when delete_on_deplete is true because this workflow must not implicitly delete stock. Status-only changes remain supported for serialized stock.
- Execute only with confirm:true and the opaque plan_hash token from the same principal within five minutes. The token is single-use; a newer dry run for the same action and item supersedes it, and a restart invalidates it.
- Do not adjust the same stock item concurrently through MCP, the InvenTree UI, another server replica, or a direct API client. If state changed, prepare a new dry run.
- If execution returns partial_failure, do not retry blindly; run a new dry run for the same stable stock_item_id to inspect current state and adjust only if still needed.`,
	},
}

func registerPrompts(server *mcp.Server) {
	for _, entry := range PromptManifest {
		if entry.Status != PromptMilestone1 {
			continue
		}
		entry := entry
		server.AddPrompt(&mcp.Prompt{
			Name:        entry.Name,
			Title:       entry.Title,
			Description: entry.Description,
		}, promptHandler(entry))
	}
}

func promptHandler(entry PromptManifestEntry) mcp.PromptHandler {
	return func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: entry.Description,
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: entry.Checklist},
				},
			},
		}, nil
	}
}
