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
	CategoryAdministrationPromptName        = "category_administration_checklist"
	StockAdministrationPromptName           = "stock_administration_checklist"
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
		Name:        StockAdministrationPromptName,
		Title:       "Stock administration checklist",
		Description: "Checklist for exact stock reads, guarded location administration, and constrained stock metadata changes.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before creating or changing stock locations or stock metadata:
- Search locations and location types first, and use search_owners plus get_owner before selecting a location owner. Then use get_stock_location or get_stock_item for exact current hierarchy and traceability state.
- Location identity is a trimmed case-insensitive name under one exact parent. Roots match roots only; the same name under another parent is allowed. Completeness-sensitive scans fail closed above 1,000 records.
- Use create_stock_location for new roots or children, including its initial owner_id. Use update_stock_location only for ordinary name, description, custom-icon, or location-type fields; it no longer accepts owner_id. Use explicit clear flags for nullable fields.
- Use restructure_stock_location with dry_run:true for parent, structural, or external changes. Review the current-state plan, then provide its exact plan_hash with confirm:true. Self-parenting and descendant cycles are refused.
- get_stock_location returns InvenTree's computed effective icon alongside the writable custom_icon; only custom_icon is ever written.
- Use create_stock_location_type/update_stock_location_type for location-type name, description, and icon after a bounded duplicate-name preflight (InvenTree does not enforce this itself); supply an explicit empty string to clear a field. Use delete_stock_location_type with a preview first — review its reported referencing_location_ids, then provide the exact plan_hash with confirm:true; deleting a referenced type is allowed and safely clears location_type on those locations.
- Use update_stock_item_metadata only for batch, expiry date, packaging, notes, or an HTTP(S) external link. Run dry_run:true, review the complete bound stock state, then provide its exact plan_hash with confirm:true.
- Use assign_owner to replace or clear the owner on an existing stock location, part, purchase order, or stock item. Preview it first, then provide its exact plan_hash with confirm:true.
- Return structured clarification instead of guessing when identity, references, hierarchy intent, metadata intent, or current confirmation is missing or unsafe.
- Never use these tools to delete or move stock, or change stock-item quantity, status, serial, supplier/source links, pricing, delete_on_deplete, installation, or order/build fields. Use the dedicated operational workflow or request a separate approved backlog story.
- If a mutation returns partial_failure, read the stable ID and current siblings or stock state before preparing a fresh plan; never retry blindly.`,
	},
	{
		Name:        CategoryAdministrationPromptName,
		Title:       "Part category administration checklist",
		Description: "Checklist for exact category reads and guarded category creation, editing, reparenting, and structural-state changes.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before creating or changing a part category:
- Search categories first, then use get_part_category for exact hierarchy, defaults, direct part count, and child context.
- Category identity is the trimmed case-insensitive name under one exact parent. Root categories match roots only; the same name under another parent is allowed.
- Create and update preflight at most 1,000 same-parent categories in 100-row pages and fail closed when a complete duplicate decision cannot be made.
- Return structured clarification instead of guessing when category identity, references, hierarchy intent, structural intent, or confirmation is missing or unsafe.
- Supply only stable parent_id and default_location_id references. Use clear_parent, clear_default_location, clear_default_keywords, or clear_icon for explicit null PATCH values.
- Never delete categories or implicitly move parts, children, parameter defaults, or stock.
- Reparenting may include categories with direct parts or descendants, but requires confirm:true after reviewing current and target hierarchy context. Self-parenting and descendant cycles are always refused.
- Any structural-state change requires confirm:true. Changing structural from false to true is refused while parts are directly assigned.
- If a create or update returns partial_failure, use the returned stable ID and recovery plan, read current state, and do not retry blindly.`,
	},
	{
		Name:        NewPartEntryChecklistPromptName,
		Title:       "New part entry checklist",
		Description: "Checklist for adding or updating a purchasable part without guessing identity, category, supplier, or manufacturer data.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before adding or updating a purchasable part:
- Search for existing parts, categories, suppliers, manufacturers, supplier parts, and manufacturer parts before writing.
- Ask for stable IDs when search results are ambiguous; retry with part_id, category_id, supplier_id, manufacturer_id, supplier_part_id, or manufacturer_part_id.
- Use get_company, get_supplier_part, and get_manufacturer_part before stable-ID maintenance. Use the bounded sourcing searches for duplicate and recovery review.
- Supplier-part duplicate identity is normalized supplier plus SKU. MPN is optional for order entry: omit null, blank, or whitespace-only input and never invent a fallback. The combined workflow reuses one exact part/manufacturer link, clarifies multiple links, or skips link creation when none exists while continuing the supplier-part path. Pinned InvenTree rejects direct manufacturer-part creation without an MPN. Never guess through a collision.
- Company updates never change the customer role (is_customer), contact, tax, tag, or image state; exact reads may report is_customer. Removing a supplier/manufacturer role requires confirmation and is refused while corresponding links exist.
- Use explicit clear flags for nullable company and sourcing-link fields. Recovery projections intentionally omit notes and redact external links.
- Do not invent categories, units, supplier SKUs, manufacturer part numbers, compliance status, or revision data.
- Do not change revision_of or variant_of through ordinary update_part. Use update_part_family_relationships with dry_run:true, review its complete bounded topology evidence, then provide the same principal-bound plan_hash with confirm:true. Never retry a partial_failure blindly.
- Related-part links are undirected. Use list_part_relations or get_part_relation for stable reads; create rejects either endpoint order as a duplicate, note-only update and single-link delete require a current principal-bound plan_hash, and endpoint replacement requires a separately reviewed delete then create. Serialize related-part writers because confirmation reads and mutations are not atomic upstream.
- Prefer upsert_part_with_supplier_and_manufacturer with dry_run:true before any write. Review every field in planned_changes; depends_on identifies unresolved IDs from earlier planned creates, while record fields contain only factual stable records.
- If the workflow reports validation_failed or partial_failure, preserve returned stable IDs and completed actions, inspect the named records, and continue only the reported remaining_actions after current-state verification.
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
- Resolve the target object type and stable object ID before listing or downloading attachments and part images, or changing a company primary image.
- Route image intent to the dedicated tools: use download_part_image to read a part's current primary image; use list_attachments and set_primary_image to assign or replace it; use set_company_image, set_company_image_from_url, or clear_company_image for a company's primary image; use generic attachment tools only for separately addressable files or stored links.
- Current milestone tools can list metadata, download schema-exposed attachment or part-image content, upload inline or allowlisted local files, upload URL copies, create stored links, update metadata, delete confirmed attachments, set a primary part image from a stable same-part image attachment, and assign, replace, or clear an existing company's primary image.
- Keep upload sources distinct: inline bytes, STDIO allowlisted local paths, URL-upload copy, and stored links are separate intents. Company-image URL fetches use only set_company_image_from_url; generic company attachments never become the company logo.
- Company images must be matching PNG, JPEG, or WebP raster content no larger than 5 MiB, 4096 pixels per dimension, or 16 megapixels total.
- Ask for structured clarification when target object identity, URL intent, original versus thumbnail mode, filename/content/link duplicates, primary-image attachment selection, raster metadata, or replacement confirmation is ambiguous.
- Do not fetch stored link targets; download only schema-exposed InvenTree file, thumbnail, part-image, or company-image URLs.
- Require confirm:true before deleting attachments, replacing an existing primary part or company image, or clearing a company image. Exact digest or null read-back is required; after partial_failure, inspect current state and never retry blindly.`,
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
- Prefer create_initial_stock_entry with dry_run:true so the operator can review duplicate-preflight results and every effective stock-item field in planned_changes.
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
- Distinguish receivable supplier-part lines from purchase-order extra lines: extra lines preserve invoice, surcharge, discount, or product-link context but are never received into stock.
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
- Return structured clarification instead of guessing when stock identity, current source location, explicit transfer destination, observed quantity, target status, or audit reason is missing or ambiguous.
- Use adjust_stock_quantity for a relative non-zero delta, stocktake_adjustment for an absolute physical count, set_stock_status only for a status change, deplete_stock_item only for reviewed complete removal of a safe delete-on-deplete record, and transfer_stock_item for reviewed relocation of one safe item's complete or explicitly requested partial quantity to an explicit destination; do not combine hidden quantity, location, status, or metadata changes.
- Supply a nonblank operator audit reason and run dry_run:true before every execution.
- Review the before/after plan and high-risk warning. Quantity decreases and Destroyed, Rejected, or Lost statuses require especially careful review.
- Refuse no-op changes, refuse relative or absolute quantity changes for serialized stock, and refuse a target quantity of zero when delete_on_deplete is true because this workflow must not implicitly delete stock. Status-only changes remain supported for serialized stock.
- For intentional depletion, require delete_on_deplete:true and positive in-stock quantity; review allocation, serialization, build/consumption, installation, parent/child, supplier, and order context; reject linked unsafe states; and verify exact-ID absence after removing the complete current quantity.
- For physical relocation, supply one explicit destination_location_id and either omit quantity for a complete transfer or provide a positive quantity smaller than current stock for a guarded split; review source/destination paths, source remainder, destination quantity, provenance, safety context, and will_split. Reject same-location and unsafe source state. Accept every exact-read destination for native InvenTree validation without imposing structural, external, ownership, or type exclusions. Multi-item batches remain a separate future workflow.
- Execute only with confirm:true and the opaque plan_hash token from the same principal within five minutes. The token is single-use; a newer dry run for the same action and item supersedes it, and a restart invalidates it.
- Do not adjust the same stock item concurrently through MCP, the InvenTree UI, another server replica, or a direct API client. If state changed, prepare a new dry run.
- If deplete_stock_item returns partial_failure, call get_stock_item for the exact stable ID before preparing any new plan because deletion may already have completed. If transfer_stock_item returns partial_failure, call get_stock_item for the source and search_stock_items for the destination split before preparing another transfer plan because a partial transfer may already have created a second stock item; never retry blindly. For other stock adjustments, do not retry blindly; run a new dry run for the same stable stock_item_id to inspect current state and adjust only if still needed.`,
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
