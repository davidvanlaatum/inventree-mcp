# Tool Reference

This file is the planned operator-facing and agent-facing reference for registered MCP tools. Once implementation begins, keep it aligned with the generated tool authorization manifest, `docs/endpoint-manifest.yaml`, and the registered Go structs.

## Manifest Fields

Each registered tool must have:

- Tool name.
- Workflow group.
- Milestone status: `milestone_1`, `future`, or `deferred`.
- Mutation class: `read_only`, `write`, `operational`, or `destructive`.
- MCP annotations: read-only, destructive, idempotent, and open-world behavior.
- Required OAuth scopes.
- Accepted upload sources, when relevant.
- Stable retry fields for clarification responses.
- "Ask operator when..." guidance.

Endpoint-backed tools must also map to a `docs/endpoint-manifest.yaml` entry whose path, method, operation ID, selected query filters, request schema, and response schema are validated against `docs/api-schema.yaml`.

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
| `location_id` | stock-item tools | Optional stable stock location primary key for stock duplicate checks. |
| `mode` | download tools | Optional download mode. `original` is the default; `thumbnail` is supported for generic attachment downloads when metadata exposes a thumbnail URL and for part-image downloads through `/api/part/thumbs/{id}/`. |
| `max_bytes` | download tools | Optional maximum response content size. Defaults to `5242880` and is capped at `26214400`. |

Structured lookup outputs must include `status`. Successful lookups use `ok`; absent stable records use `not_found`; ambiguous lookups use `clarification_required`.

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

The Milestone 1 table below is a planning summary until each tool is registered. When a tool is implemented, its authoritative row must include every manifest field above, including milestone status and MCP annotations.

## Registered Lookup Tools

All tools in this section are implemented and registered. They use class `read_only`, milestone status `milestone_1`, MCP annotations `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, and `openWorldHint:false`. They require OAuth scope `inventree.read` when HTTP OAuth scope enforcement lands. Upload sources are `None`.

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
| `download_part_image` | Attachments | `id`, `mode`, `max_bytes` | `status`, `id`, `content_type`, `size`, `sha256`, `mode`, `source_url`, plus `text` or `base64` content | Operator may mean a generic attachment rather than the current primary image, or original image versus explicit thumbnail mode. |

## Skeleton Tools

| Tool | Group | Milestone status | Class | MCP annotations | Scopes | Upload sources | Ask operator when |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `health_version` | Server health | `milestone_1` | `read_only` | `readOnlyHint:true`, `destructiveHint:false`, `idempotentHint:true`, `openWorldHint:false` | None until HTTP OAuth scope enforcement lands | None | Never; returns static server health and build metadata. |

## Milestone 1 Tools

| Tool | Group | Class | Scopes | Upload sources | Ask operator when |
| --- | --- | --- | --- | --- | --- |
| `search_parts` | Part lookup | Read-only | `inventree.read` | None | Search returns multiple plausible parts. |
| `get_part` | Part lookup | Read-only | `inventree.read` | None | Provided ID does not exist. |
| `search_part_categories` | Part lookup | Read-only | `inventree.read` | None | Category path/name is ambiguous. |
| `search_parameter_templates` | Parameters | Read-only | `inventree.read` | None | Same-name templates differ by unit, choices, checkbox behavior, or category link. |
| `get_part_parameters` | Parameters | Read-only | `inventree.read` | None | Part ID is missing or ambiguous. |
| `search_companies` | Company lookup | Read-only | `inventree.read` | None | Supplier/manufacturer identity is ambiguous. |
| `search_suppliers` | Company lookup | Read-only | `inventree.read` | None | Supplier role is unclear. |
| `search_manufacturers` | Company lookup | Read-only | `inventree.read` | None | Manufacturer role is unclear. |
| `search_stock_locations` | Stock lookup | Read-only | `inventree.read` | None | Location name/path is ambiguous. |
| `search_stock_items` | Stock lookup | Read-only | `inventree.read` | None | Existing stock may duplicate the requested initial stock. |
| `list_attachments` | Attachments | Read-only | `inventree.read` | None | Target object is ambiguous. |
| `get_attachment_metadata` | Attachments | Read-only | `inventree.read` | None | Attachment ID is missing or ambiguous. |
| `download_attachment` | Attachments | Read-only | `inventree.read` | None | Operator may mean stored-link metadata versus an external link target, or original file versus explicit thumbnail mode. |
| `download_part_image` | Attachments | Read-only | `inventree.read` | None | Operator may mean a generic attachment rather than the current primary image, or original image versus explicit thumbnail mode. |
| `preview_purchase_order_with_lines` | Purchasing preview | Read-only | `inventree.read` | None | Supplier part, price, quantity, or currency is ambiguous. |
| `create_part` | Part entry | Write | `inventree.write` | None | Category, units, supplier/manufacturer data, or required API fields are unclear. |
| `update_part` | Part entry | Write | `inventree.write` | None | Caller provides human names instead of stable IDs and lookup is ambiguous. |
| `set_part_parameters` | Parameters | Write | `inventree.write` | None | Existing template match is ambiguous or creation of a new template/category link would be required. |
| `create_company` | Company entry | Write | `inventree.write` | None | Role should be supplier/manufacturer but existing company may already match. |
| `create_supplier_part` | Supplier link | Write | `inventree.write` | None | Supplier company or purchasable part match is ambiguous. |
| `create_manufacturer_part` | Manufacturer link | Write | `inventree.write` | None | Manufacturer company or part match is ambiguous. |
| `create_stock_item` | Initial stock | Operational | `inventree.write`, `inventree.operational` | None | Existing stock at the requested location may duplicate the new item. |
| `upload_attachment` | Attachments | Write | `inventree.write`, `inventree.upload` | Inline bytes; STDIO allowlisted local path | Filename/content duplicates an existing attachment without explicit replacement or metadata-update intent. |
| `upload_attachment_from_url` | Attachments | Write, open-world | `inventree.write`, `inventree.upload` | HTTP(S) URL only | Intent could be upload-copy versus store-link, or URL policy rejects the target. |
| `create_link_attachment` | Attachments | Write | `inventree.write`, `inventree.upload` | HTTP(S) link only, no fetch | URL has unsupported scheme, credentials/userinfo, local path shape, or allowlist ambiguity. |
| `update_attachment_metadata` | Attachments | Write | `inventree.write`, `inventree.upload` | None | Stable attachment ID is missing. |
| `set_primary_image` | Attachments | Write | `inventree.write`, `inventree.upload` | Existing attachment/image ID | Multiple candidate images exist or replacement lacks `confirm:true`. |

## Future Tools

These tools are planned but outside milestone 1 unless the plan is explicitly changed:

- BOM tools: `get_bom`, `validate_bom`, `add_bom_item`, `update_bom_item`, `remove_bom_item`, `import_bom_rows`.
- Purchase-order write tools: `create_purchase_order`, `add_purchase_order_line`, `update_purchase_order_line`, `create_purchase_order_with_lines`, `receive_purchase_order_items`, `close_purchase_order`.
- Build tools: `search_build_orders`, `create_build_order`, `allocate_build_stock`, `issue_build_outputs_to_stock`, `complete_build_order`.
- Bulk imports: `import_parts`, `import_supplier_parts`, `import_stock_items`, `import_purchase_order_rows`.
- Stock adjustment workflows: `adjust_stock_quantity`, `set_stock_status`, `stocktake_adjustment`.

## Deferred Scope

Sales/customer workflows, sales-order tools, return-order tools, transfer-order tools, customer-role defaults, build attachments, return attachments, transfer attachments, and company primary-image support are deferred for the first release.
