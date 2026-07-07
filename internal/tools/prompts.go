package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	PromptMilestone1 = "milestone_1"
	PromptFuture     = "future"

	NewPartEntryChecklistPromptName      = "new_part_entry_checklist"
	ParameterReuseChecklistPromptName    = "parameter_reuse_checklist"
	AttachmentImageChecklistPromptName   = "attachment_image_checklist"
	InitialStockEntryChecklistPromptName = "initial_stock_entry_checklist"
	PurchasePreviewChecklistPromptName   = "purchase_preview_checklist"
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
- Do not create new parameter templates or category links unless a later tool explicitly supports that workflow.
- Retry set_part_parameters only with stable part_id, template_id or parameter_id, and an explicit value shape.`,
	},
	{
		Name:        AttachmentImageChecklistPromptName,
		Title:       "Attachment and image checklist",
		Description: "Checklist for attachment/image reads, uploads, links, metadata updates, deletes, and planned primary-image replacement.",
		Status:      PromptMilestone1,
		Checklist: `Use this checklist before attachment/image reads, writes, or planned replacement work:
- Resolve the target object type and stable object ID before listing or downloading attachments and part images.
- Current milestone tools can list metadata, download schema-exposed attachment or part-image content, upload inline or allowlisted local files, upload URL copies, create stored links, update metadata, and delete confirmed attachments.
- Keep upload sources distinct: inline bytes, STDIO allowlisted local paths, URL-upload copy, and stored links are separate intents.
- Ask for structured clarification when target object identity, URL intent, original versus thumbnail mode, filename/content/link duplicates, or future image selection is ambiguous.
- Do not fetch stored link targets; download only schema-exposed InvenTree file, thumbnail, or part-image URLs.
- Require confirm:true before deleting attachments and before replacing an existing primary part image once the replacement tool exists.`,
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
		Name:        "receive_purchase_order_checklist",
		Title:       "Receive purchase order checklist",
		Description: "Future checklist for purchase-order receiving workflows.",
		Status:      PromptFuture,
	},
	{
		Name:        "bom_import_review",
		Title:       "BOM import review",
		Description: "Future checklist for BOM import review workflows.",
		Status:      PromptFuture,
	},
	{
		Name:        "stocktake_review",
		Title:       "Stocktake review",
		Description: "Future checklist for stocktake workflows.",
		Status:      PromptFuture,
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
