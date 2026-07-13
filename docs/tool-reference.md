# Tool Reference

This file is the operator-facing and agent-facing reference for registered MCP tools. Keep it aligned with the checked tool authorization manifest in `docs/tool-manifest.json`, `docs/endpoint-manifest.yaml`, and the registered Go structs.

## Checked Tool Manifest

`docs/tool-manifest.json` is generated from `internal/tools.ToolAuthorizations` and `internal/tools.PromptManifest` with:

```sh
go generate ./internal/tools
```

The manifest is checked in and tested. It records each registered tool's `milestone_1` status, mutation class (`read_only`, `write`, `operational`, or `destructive`), required scopes, MCP annotation booleans, upload source class, and HTTP registration state. Read-only tools and `health_version` are `registered` for HTTP development mode. Production HTTP startup registers write, operational, upload, image, and destructive tools only with OAuth authorization mode enabled, where they are `registered_with_oauth_scope_guard`.

## Manifest Fields

Each registered tool must have:

- Tool name.
- Workflow group.
- Milestone status: `milestone_1`, `future`, or `deferred`.
- Mutation class: `read_only`, `write`, `operational`, or `destructive`.
- MCP annotations: read-only, destructive, idempotent, and open-world behavior.
- Required OAuth scopes.
- Accepted upload sources, when relevant: `inline_base64`, `stdio_local_path`, `http_url_fetch`, `http_url_link`, or `existing_attachment_image`.
- Stable retry fields for clarification responses.
- "Ask operator when..." guidance.

Endpoint-backed tools must also map to a `docs/endpoint-manifest.yaml` entry whose path, method, operation ID, selected query filters, request schema, and response schema are validated against `docs/api-schema.yaml`.

In OAuth authorization mode, scoped tools publish per-tool OAuth metadata in descriptor `_meta["securitySchemes"]` and `_meta["openai/securitySchemes"]`, matching the checked tool authorization manifest. The current Go MCP SDK `mcp.Tool` type does not expose a first-class top-level `securitySchemes` field; replace the mirror-only descriptor wiring with the canonical field when the SDK adds support or when the server owns custom tool descriptor serialization.

## Lookup Tool Framework

Read-only lookup handlers use a context-resolved InvenTree client supplied through the tool dependency struct. Handlers depend on the lookup client interface instead of constructing a concrete HTTP client, so STDIO credentials and future HTTP OAuth credentials stay in the server layer.

Common lookup inputs:

| Field | Applies to | Behavior |
| --- | --- | --- |
| `search` | search tools | Optional text passed to the schema-backed InvenTree endpoint. |
| `limit` | list/search tools | Optional maximum result count. Defaults to `20` and is capped at `100`. |
| `offset` | list/search tools | Optional pagination offset for deterministic retries. |
| `id` | get-by-id tools | Stable InvenTree primary key. |
| `model_type` | object-scoped attachment/parameter tools | In-scope InvenTree object type such as `part`, `stockitem`, `company`, `manufacturerpart`, `supplierpart`, or `purchaseorder`. |
| `model_id` | object-scoped attachment/parameter tools | Stable primary key for the selected object type. |
| `part_id` | part parameter and stock-item tools | Stable part primary key for part-scoped reads or stock duplicate checks. |
| `location_id` | stock-item tools | Stable stock location primary key. Required for `create_stock_item`; optional for stock duplicate-check searches. |
| `quantity` | `create_stock_item` | Initial stock quantity. Must be greater than zero. |
| `status` | `create_stock_item` | Optional InvenTree stock status code when local convention requires one. |
| `batch`, `serial`, `notes` | `create_stock_item` | Optional stock-item metadata passed through to the schema-backed stock endpoint. |
| `dry_run` | workflow tools | Return planned actions without writing when true. |
| `supplier_name`, `manufacturer_name` | `upsert_part_with_supplier_and_manufacturer` | Company names used to prefer existing supplier/manufacturer records or create them when unambiguous. |
| `supplier_currency`, `manufacturer_currency` | `upsert_part_with_supplier_and_manufacturer` | Currency required before creating a supplier or manufacturer company. |
| `supplier_sku`, `mpn` | `upsert_part_with_supplier_and_manufacturer` | Stable supplier and manufacturer identifiers for supplier/manufacturer part links. |
| `actions`, `record_type`, `reason` | workflow outputs | Ordered plan or execution summary for workflow-level tools. In `dry_run` responses, planned creates are authoritative here because they do not have stable IDs yet. |
| `part`, `supplier`, `manufacturer`, `supplier_part`, `manufacturer_part` | `upsert_part_with_supplier_and_manufacturer` output | Stable records selected, reused, updated, or created by the workflow. Dry-run planned creates may appear only in `actions` until the write is executed. |
| `part_search`, `location_search` | `create_initial_stock_entry` | Human-readable lookup text used only when stable `part_id` or `location_id` is omitted. Ambiguous matches return clarification. |
| `location`, `stock_item` | `create_initial_stock_entry` output | Stable location and created stock item selected or written by the workflow. Dry-run planned stock creates appear only in `actions`. |
| `lines`, `supplier_part_id`, `supplier_sku`, `unit_price`, `currency`, `line_total`, `warnings`, `index` | `preview_purchase_order_with_lines` | No-write purchase preview line inputs and outputs. Supplier-part IDs are validated directly; part/supplier/SKU lookup must resolve to exactly one supplier-part link. |
| `omitted_recommended_fields` | workflow outputs | Recommended fields the caller did not provide, such as IPN, units, purchaseability, default location, supplier SKU, or MPN. |
| `mode` | download tools | Optional download mode. `original` is the default; `thumbnail` is supported for generic attachment downloads when metadata exposes a thumbnail URL and for part-image downloads through `/api/part/thumbs/{id}/`. |
| `max_bytes` | download tools | Optional maximum response content size. Defaults to `5242880` and is capped at `26214400`. |
| `inline_base64` | `upload_attachment` | Base64-encoded bytes for an inline upload source. Mutually exclusive with `local_path`. |
| `local_path` | `upload_attachment` | STDIO-only local file path under a configured upload allowlist. Mutually exclusive with `inline_base64`. |
| `url` | URL upload and link tools | HTTP(S) URL. `upload_attachment_from_url` fetches bytes; `create_link_attachment` stores the link without fetching. |
| `filename`, `content_type`, `comment`, `tags` | attachment write tools | Optional or required attachment metadata, preserving explicit empty values on metadata updates. For `create_link_attachment`, `filename` is duplicate-preflight-only because InvenTree assigns stored-link filename metadata. |
| `allow_duplicate` | attachment create tools | Explicit intent to add a matching filename, size, or link duplicate after duplicate preflight. |
| `confirm` | destructive tools | Required true before `delete_attachment` removes an attachment. |
| `part_id` | `set_primary_image` | Stable part primary key that will receive the primary image. |
| `attachment_id` | `set_primary_image` | Stable image attachment primary key already attached to the same part. |
| `source_kind` | attachment write outputs | Source classification such as `inline`, `local_path`, `url`, or `link`. |
| `image_url`, `replaced` | `set_primary_image` output | Redacted resulting image URL and whether an existing primary image was replaced. |

## Upload Source Resolver

`internal/upload` owns upload source acquisition for attachment write tools and any upload step that prepares a primary-image candidate. It resolves three source kinds before any InvenTree multipart upload code receives bytes:

| Source | Current behavior |
| --- | --- |
| Inline bytes | Copies caller-provided bytes, cleans the supplied filename, preserves the supplied content type, and rejects content above the configured maximum size. |
| STDIO local path | Available only when the caller's mode is STDIO. It uses direct Afero access in `internal/upload/local_file.go`, canonicalizes allowlisted roots and requested paths, rejects paths outside the allowlist, resolves symlink escapes on `OsFs`, opens only after policy checks, and rejects non-regular files. Allowlisted roots must be trusted operator-controlled paths; `OsFs` still has a residual OS-level race if an untrusted user can swap files between policy checks and open. |
| URL fetch | Accepts only HTTP(S) URLs without userinfo, resolves hostnames before fetch and every redirect, blocks local/private/link-local/multicast/reserved/documentation/cloud-metadata-style address ranges by default, caps redirects, does not forward MCP or InvenTree authorization headers, and enforces the configured maximum size and timeout. Private/internal URL targets require an explicit normalized scheme/host/port allowlist entry. |

The resolver returns in-memory content, filename, content type, size, and source kind. Registered attachment tools are responsible for target object validation, duplicate handling, multipart upload, tool annotations, and OAuth scope registration.

Structured lookup outputs must include `status`. Successful lookups use `ok`; absent stable records use `not_found`; missing part primary images use `no_image`; ambiguous lookups use `clarification_required`.

Clarification outputs must include:

| Field | Behavior |
| --- | --- |
| `status` | Always `clarification_required`. |
| `question` | Specific operator question to resolve the ambiguity. |
| `field` | Field or relationship that is ambiguous or missing. |
| `reason` | Why the tool cannot safely continue. |
| `candidates` | Candidate records with stable IDs, labels, optional summaries, URLs, and extra fields needed for the operator decision. |
| `retry` | Stable field the caller should provide on retry, such as `part_id`, `company_id`, `location_id`, `template_id`, or `attachment_id`. |
| `hard_error` | Whether the API would reject the request, distinct from a recommended-field warning. |
| `retry_values` | Optional non-sensitive prior input values that should be preserved on retry. |

Clarification `candidates` entries use:

| Field | Behavior |
| --- | --- |
| `id` | Stable object ID to provide on retry. |
| `label` | Human-readable object name or identifier. |
| `summary` | Optional short disambiguating detail. |
| `url` | Optional InvenTree URL for operator inspection. |
| `fields` | Optional non-sensitive structured details needed for the decision. |

The Milestone 1 table below summarizes the registered first-release surface. `docs/tool-manifest.json` is the checked machine-readable source for mutation classes, scopes, annotations, upload sources, and HTTP registration state.

## Registered Lookup Tools

All tools in this section are implemented and registered. They use class `read_only`, milestone status `milestone_1`, MCP annotations `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, and `openWorldHint:false`. They require OAuth scope `inventree.read` when OAuth authorization mode is enabled. Upload sources are `None`.

| Tool | Group | Inputs | Output | Ask operator when |
| --- | --- | --- | --- | --- |
| `search_parts` | Part lookup | `search`, `limit`, `offset` | `status`, `count`, `results`, optional `clarification` with retry `part_id` | Search returns multiple plausible parts. |
| `get_part` | Part lookup | `id` | `status`, `record` | Provided ID does not exist. |
| `search_part_categories` | Part lookup | `search`, `limit`, `offset` | `status`, `count`, `results`, optional `clarification` with retry `category_id` | Category path/name is ambiguous. |
| `search_parameter_templates` | Parameters | `search`, `limit`, `offset` | `status`, `count`, `results`, optional `clarification` with retry `template_id` | Same-name templates differ by unit, choices, checkbox behavior, or category link. |
| `get_part_parameters` | Parameters | `part_id`, `limit`, `offset` | `status`, `count`, `results` | Part ID is missing or ambiguous. |
| `search_companies` | Company lookup | `search`, `limit`, `offset` | `status`, `count`, `results`, optional `clarification` with retry `company_id` | Supplier/manufacturer identity is ambiguous. |
| `search_suppliers` | Company lookup | `search`, `limit`, `offset` | `status`, `count`, `results`, optional `clarification` with retry `supplier_id` | Supplier role is unclear. |
| `search_manufacturers` | Company lookup | `search`, `limit`, `offset` | `status`, `count`, `results`, optional `clarification` with retry `manufacturer_id` | Manufacturer role is unclear. |
| `search_stock_locations` | Stock lookup | `search`, `limit`, `offset` | `status`, `count`, `results`, optional `clarification` with retry `location_id` | Location name/path is ambiguous. |
| `search_stock_items` | Stock lookup | `search`, `part_id`, `location_id`, `limit`, `offset` | `status`, `count`, `results` | Existing stock may duplicate the requested initial stock. |
| `list_attachments` | Attachments | `model_type`, `model_id`, `search`, `limit`, `offset` | `status`, `count`, `results` | Target object is ambiguous. |
| `get_attachment_metadata` | Attachments | `id` | `status`, `record` | Attachment ID is missing or ambiguous. |
| `download_attachment` | Attachments | `id`, `mode`, `max_bytes` | `status`, `id`, `filename`, `content_type`, `size`, `sha256`, `mode`, `source_url`, plus `text` or `base64` content | Operator may mean stored-link metadata versus an external link target, or original file versus explicit thumbnail mode. |
| `download_part_image` | Attachments | `id`, `mode`, `max_bytes` | `status`, `id`, `filename`, `content_type`, `size`, `sha256`, `mode`, `source_url`, plus `text` or `base64` content | Operator may mean a generic attachment rather than the current primary image, or original image versus explicit thumbnail mode. |
| `preview_purchase_order_with_lines` | Purchasing preview | `supplier_id`, `lines` with `supplier_part_id` or `part_id` plus supplier context, `quantity`, optional `unit_price`, `currency`, `notes` | `status`, `supplier_id`, `lines`, optional `warnings`, optional `clarification` | Supplier part, supplier, part, quantity, or price currency is ambiguous. |

## Registered Write Tools

The tools in this section are implemented and registered only when write tools are explicitly enabled by the server dependency configuration. The current CLI enables them for STDIO mode. HTTP registration requires OAuth authorization mode so every call passes through the per-tool scope guard before the handler runs.

Tools in this section are milestone status `milestone_1`. Ordinary write tools use mutation class `write` with MCP annotations `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, and `openWorldHint:false`. Stock creation tools use mutation class `operational`. `upload_attachment_from_url` uses `openWorldHint:true`; `delete_attachment` uses mutation class `destructive` with `destructiveHint:true`. Most tools require OAuth scope `inventree.write` when OAuth authorization mode is enabled. Operational stock tools require both `inventree.write` and `inventree.operational`. Attachment tools require `inventree.write` and `inventree.upload`; delete also requires `inventree.destructive`.

| Tool | Group | Inputs | Output | Ask operator when |
| --- | --- | --- | --- | --- |
| `create_part` | Part entry | `name`, `category_id`, optional `description`, `ipn`, `units`, `active`, `assembly`, `component`, `purchaseable`, `trackable`, `virtual`, `default_location_id` | `status`, `record`, optional `clarification` with retry `part_id`, `category_id`, or `default_location_id` | Category ID or default location ID is missing/invalid, or matching parts already exist. |
| `update_part` | Part entry | `id`, optional `name`, `description`, `category_id`, `ipn`, `units`, `active`, `assembly`, `component`, `purchaseable`, `trackable`, `virtual`, `default_location_id` | `status`, `record`, optional `clarification` with retry `id`, `category_id`, or `default_location_id` | Part ID or referenced IDs are invalid, caller provides names instead of stable IDs, or no PATCH fields are supplied. |
| `set_part_parameters` | Parameters | `part_id`, `parameters` array with `template_id` or `name`, and exactly one of `value`, `bool_value`, or `number_value` | `status`, `record[]`, optional `clarification` with retry `template_id`, `parameter_id`, `category_id`, or `value` | Template name is ambiguous, matching templates differ by unit/choices/checkbox behavior, the template is disabled, the template is not linked to the part category, multiple existing part parameters use the same template, or creating a new template/category link would be required. |
| `create_company` | Company entry | `name`, `currency`, at least one of `is_supplier` or `is_manufacturer`, optional `description`, `website` | `status`, `record`, optional `clarification` with retry `company_id`, `currency`, or `is_supplier` | Matching companies already exist, currency is missing, no supported role is selected, or the caller asks for a customer/sales workflow. |
| `create_supplier_part` | Supplier link | `part_id`, `supplier_id`, `sku`, optional `description`, `link`, `active`, `primary`, `manufacturer_part_id`, `packaging`, `note` | `status`, `record`, optional `clarification` with retry `supplier_part_id`, `part_id`, `supplier_id`, or `manufacturer_part_id` | Part, supplier, or manufacturer-part ID is invalid, or matching supplier-part links already exist. |
| `create_manufacturer_part` | Manufacturer link | `part_id`, `manufacturer_id`, optional `mpn`, `description`, `link` | `status`, `record`, optional `clarification` with retry `manufacturer_part_id`, `part_id`, or `manufacturer_id` | Part or manufacturer ID is invalid, or matching manufacturer-part links already exist. |
| `upsert_part_with_supplier_and_manufacturer` | Part workflow | `dry_run`, `part_id` or `name`, optional part fields, optional supplier/manufacturer IDs or names, `supplier_sku`, `mpn`, and currencies when creating companies | `status`, `dry_run`, `actions`, selected/reused/created `part`, `supplier`, `manufacturer`, `supplier_part`, `manufacturer_part` when stable, `omitted_recommended_fields`, optional `clarification` | Part, supplier, manufacturer, supplier-part, or manufacturer-part matches are ambiguous; category or currency is missing before creation; supplier SKU is missing before linking. |
| `create_stock_item` | Initial stock | `part_id`, `location_id`, `quantity`, optional `status`, `batch`, `serial`, `notes` | `status`, `record`, optional `clarification` with retry `stock_item_id`, `part_id`, `location_id`, `quantity`, or `status` | Part, location, quantity, or status is invalid, or existing stock already matches the requested part and location. |
| `create_initial_stock_entry` | Initial stock workflow | `dry_run`, `part_id` or `part_search`, `location_id` or `location_search`, `quantity`, optional `status`, `batch`, `serial`, `notes` | `status`, `dry_run`, `actions`, selected `part`, selected `location`, optional created `stock_item`, optional `clarification` | Part or location search is ambiguous, quantity/status is invalid, or existing stock already matches the requested part and location. |
| `upload_attachment` | Attachments | `model_type`, `model_id`, `filename`, `content_type`, exactly one of `inline_base64` or `local_path`, optional `comment`, `tags`, `allow_duplicate` | `status`, `record`, `source_kind`, optional `clarification` with retry `inline_base64`, `filename`, `content_type`, `model_id`, `url`, or `allow_duplicate` | Target object is out of scope, source is missing/ambiguous, filename or content type is missing, local path is not allowlisted, URL intent is ambiguous, or duplicate preflight matches existing attachments. |
| `upload_attachment_from_url` | Attachments | `model_type`, `model_id`, `url`, optional `filename`, `comment`, `tags`, `allow_duplicate` | `status`, `record`, `source_kind`, optional `clarification` with retry `filename`, `model_id`, or `allow_duplicate` | URL policy rejects the target, filename cannot be determined, or duplicate preflight matches existing attachments. |
| `create_link_attachment` | Attachments | `model_type`, `model_id`, `url`, optional duplicate-preflight `filename`, `comment`, `tags`, `allow_duplicate` | `status`, `record`, `source_kind`, optional `clarification` with retry `model_id` or `allow_duplicate` | URL is not HTTP(S), includes credentials or a fragment, target object is out of scope, or duplicate preflight matches an existing link. InvenTree assigns stored-link filename metadata. |
| `update_attachment_metadata` | Attachments | `id`, optional `filename`, `comment`, `tags` | `status`, `record`, optional `clarification` with retry `id` | Stable attachment ID is missing, target object is out of scope, or no PATCH fields are supplied. |
| `delete_attachment` | Attachments | `id`, `confirm` | `status`, `record`, optional `clarification` with retry `confirm` | Stable attachment ID is missing, target object is out of scope, or `confirm:true` is missing. |
| `set_primary_image` | Attachments | `part_id`, `attachment_id`, `confirm` | `status`, `record`, `part_id`, `image_url`, `replaced`, optional `clarification` with retry `attachment_id` or `confirm` | Attachment is not an image file on the requested part, multiple candidate images exist before a stable `attachment_id` is supplied, or replacement lacks `confirm:true`. |

## Skeleton Tools

| Tool | Group | Milestone status | Class | MCP annotations | Scopes | Upload sources | HTTP registration | Ask operator when |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `health_version` | Server health | `milestone_1` | `read_only` | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | None | None | `registered` | Never; returns static server health and build metadata. |

## Registered Prompts

Prompts are static operator checklists registered through the MCP prompt surface. They do not call InvenTree directly and do not replace tool-level clarification responses. Milestone 1 prompts prefer existing records, stable ID retries, dry-run plans, and structured clarification over guessed categories, units, supplier SKUs, manufacturer part numbers, order states, prices, locations, stock status, or quantities.

| Prompt | Milestone status | Purpose | Guardrail |
| --- | --- | --- | --- |
| `new_part_entry_checklist` | `milestone_1` | Add or update a purchasable part with supplier/manufacturer context. | Search existing records first, ask for stable IDs on ambiguity, and prefer `upsert_part_with_supplier_and_manufacturer` with `dry_run:true` before writing. |
| `parameter_reuse_checklist` | `milestone_1` | Reuse existing parameter templates when setting part parameters. | Prefer category-linked templates and ask for `template_id` when same-name templates differ by unit, choices, checkbox behavior, or category link. |
| `attachment_image_checklist` | `milestone_1` | Prepare attachment/image reads, uploads, links, metadata updates, deletes, and primary-image replacement workflows. | Keep upload-copy, stored-link, metadata update, delete, and primary-image replacement intents distinct; require a stable image attachment ID and `confirm:true` before replacement. |
| `initial_stock_entry_checklist` | `milestone_1` | Create initial stock after duplicate preflight. | Resolve stable part/location IDs, require positive quantity, and prefer `create_initial_stock_entry` with `dry_run:true`. |
| `purchase_preview_checklist` | `milestone_1` | Produce no-write purchase-order line previews. | Validate supplier-part identity and positive quantities; never create purchase orders or purchase-order lines. |

Future prompts remain in the internal prompt manifest with status `future` and are not registered through MCP until their workflows are implemented:

- `receive_purchase_order_checklist`
- `bom_import_review`
- `stocktake_review`

Future purchasing and live order-entry work is tracked in `docs/TASKS.md` rather than registered in milestone 1. The next purchasing write workflow should expose `create_purchase_order_with_lines` with preview-equivalent validation, an idempotency key, purchase-order and line read/search support for duplicate checks and recovery, and structured failure output that includes any created IDs. Live order-entry hardening should also make lower-level write tools consistently support dry-run/preflight where practical, reject blank/null MPN before manufacturer-part writes or document an operator-approved fallback convention, and include redacted InvenTree response-body details in tool errors.

## Milestone 1 Tools

| Tool | Group | Class | Scopes | Upload sources | MCP annotations | HTTP registration | Ask operator when |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `search_parts` | Part lookup | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Search returns multiple plausible parts. |
| `get_part` | Part lookup | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Provided ID does not exist. |
| `search_part_categories` | Part lookup | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Category path/name is ambiguous. |
| `search_parameter_templates` | Parameters | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Same-name templates differ by unit, choices, checkbox behavior, or category link. |
| `get_part_parameters` | Parameters | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Part ID is missing or ambiguous. |
| `search_companies` | Company lookup | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Supplier/manufacturer identity is ambiguous. |
| `search_suppliers` | Company lookup | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Supplier role is unclear. |
| `search_manufacturers` | Company lookup | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Manufacturer role is unclear. |
| `search_stock_locations` | Stock lookup | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Location name/path is ambiguous. |
| `search_stock_items` | Stock lookup | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Existing stock may duplicate the requested initial stock. |
| `list_attachments` | Attachments | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Target object is ambiguous. |
| `get_attachment_metadata` | Attachments | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Attachment ID is missing or ambiguous. |
| `download_attachment` | Attachments | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Operator may mean stored-link metadata versus an external link target, or original file versus explicit thumbnail mode. |
| `download_part_image` | Attachments | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Operator may mean a generic attachment rather than the current primary image, or original image versus explicit thumbnail mode. |
| `preview_purchase_order_with_lines` | Purchasing preview | Read-only | `inventree.read` | None | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | `registered` | Supplier part, price, quantity, or currency is ambiguous. |
| `create_part` | Part entry | Write | `inventree.write` | None | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | See Registered Write Tools. |
| `update_part` | Part entry | Write | `inventree.write` | None | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | See Registered Write Tools. |
| `set_part_parameters` | Parameters | Write | `inventree.write` | None | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | See Registered Write Tools. |
| `create_company` | Company entry | Write | `inventree.write` | None | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | See Registered Write Tools. |
| `create_supplier_part` | Supplier link | Write | `inventree.write` | None | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | See Registered Write Tools. |
| `create_manufacturer_part` | Manufacturer link | Write | `inventree.write` | None | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | See Registered Write Tools. |
| `upsert_part_with_supplier_and_manufacturer` | Part workflow | Write | `inventree.write` | None | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | See Registered Write Tools. |
| `create_stock_item` | Initial stock | Operational | `inventree.write`, `inventree.operational` | None | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | Existing stock at the requested location may duplicate the new item. |
| `create_initial_stock_entry` | Initial stock workflow | Operational | `inventree.write`, `inventree.operational` | None | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | Part or location search is ambiguous, quantity/status is invalid, or existing stock at the requested location may duplicate the new item. |
| `upload_attachment` | Attachments | Write | `inventree.write`, `inventree.upload` | Inline bytes; STDIO allowlisted local path | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | Filename/content duplicates an existing attachment without explicit duplicate intent. |
| `upload_attachment_from_url` | Attachments | Write, open-world | `inventree.write`, `inventree.upload` | HTTP(S) URL only | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:true` | `registered_with_oauth_scope_guard` | URL policy rejects the target or filename/content duplicates an existing attachment without explicit duplicate intent. |
| `create_link_attachment` | Attachments | Write | `inventree.write`, `inventree.upload` | HTTP(S) link only, no fetch | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | URL has unsupported scheme, credentials/userinfo, fragment, local path shape, or duplicates an existing link without explicit duplicate intent. |
| `update_attachment_metadata` | Attachments | Write | `inventree.write`, `inventree.upload` | None | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | Stable attachment ID is missing or no PATCH fields are supplied. |
| `delete_attachment` | Attachments | Destructive | `inventree.write`, `inventree.upload`, `inventree.destructive` | None | `readOnlyHint:false`, `destructiveHint:true`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | Stable attachment ID is missing or `confirm:true` is missing. |
| `set_primary_image` | Attachments | Write | `inventree.write`, `inventree.upload` | Existing attachment/image ID | `readOnlyHint:false`, `destructiveHint:false`, `idempotentHint:false`, `openWorldHint:false` | `registered_with_oauth_scope_guard` | Multiple candidate images exist or replacement lacks `confirm:true`. |

## Future Tools

These tools are planned but outside milestone 1 unless the plan is explicitly changed:

- Parameter-template administration: `create_parameter_template`, `update_parameter_template`, `delete_parameter_template`, `merge_parameter_templates`.
- Cross-inventory parameter values: `search_part_parameters`, `delete_part_parameter`.
- Category parameter defaults: `list_category_parameter_defaults`, `create_category_parameter_default`, `update_category_parameter_default`, `delete_category_parameter_default`.
- Bulk parameter workflows: `audit_parameter_consistency`, `bulk_propagate_part_parameters`.
- BOM tools: `get_bom`, `validate_bom`, `add_bom_item`, `update_bom_item`, `remove_bom_item`, `import_bom_rows`.
- Purchase-order write tools: `create_purchase_order`, `add_purchase_order_line`, `update_purchase_order_line`, `create_purchase_order_with_lines`, `receive_purchase_order_items`, `close_purchase_order`.
- Build tools: `search_build_orders`, `create_build_order`, `allocate_build_stock`, `issue_build_outputs_to_stock`, `complete_build_order`.
- Bulk imports: `import_parts`, `import_supplier_parts`, `import_stock_items`, `import_purchase_order_rows`.
- Stock adjustment workflows: `adjust_stock_quantity`, `set_stock_status`, `stocktake_adjustment`.

## Deferred Scope

Sales/customer workflows, sales-order tools, return-order tools, transfer-order tools, customer-role defaults, build attachments, return attachments, transfer attachments, and company primary-image support are deferred for the first release.
