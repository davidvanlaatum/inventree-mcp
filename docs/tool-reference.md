# Tool Reference

This file is the planned operator-facing and agent-facing reference for registered MCP tools. Once implementation begins, keep it aligned with the generated tool authorization manifest and the registered Go structs.

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
| `download_attachment` | Attachments | Read-only | `inventree.read` | None | Attachment is a stored link, content exceeds download limit, or metadata URL is outside the configured InvenTree instance. |
| `download_part_image` | Attachments | Read-only | `inventree.read` | None | Part has no primary image, content exceeds download limit, or image URL is outside the configured InvenTree instance. |
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
