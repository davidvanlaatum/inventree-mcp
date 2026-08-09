# InvenTree API Schema Notes

The local OpenAPI schema is stored at `docs/api-schema.yaml`. Endpoint coverage for implemented and milestone-planned InvenTree client methods is tracked in `docs/endpoint-manifest.yaml`.

Source:

```sh
curl -fsSL https://inventory.internal.vanlaatum.id.au/api/schema/ -o docs/api-schema.yaml
```

Current fetched schema:

- OpenAPI: `3.0.3`
- API title: `InvenTree API`
- API version: `511`
- Runtime InvenTree version: `1.4.0`
- Runtime commit: `0a9a8b1`
- Runtime commit date: `2026-06-24`
- Fetched at: `2026-07-03T23:21:00+09:30` approximately
- Source instance: `inventory.internal.vanlaatum.id.au`
- Authentication used for schema fetch: none from this workspace
- SHA256: `a574d8c055e36e2efa16dfaad093b77b4126f3a230c12a56c31b90f224d526a1`
- Runtime evidence: operator-provided InvenTree About dialog screenshot on `2026-07-04`, confirmed upstream Git tag `1.4.0` resolves to commit `0a9a8b1c54d0811ede0a61ffe3b0427f82ee28e5`.

When `docs/api-schema.yaml` changes, update this provenance block and any endpoint capability tables in the same change.
The manifest stores the same schema SHA256 and API version; schema drift tests fail until both this provenance block and `docs/endpoint-manifest.yaml` are updated. The blocking Testcontainers suite pins `inventree/inventree:1.4.3` and fails if the runtime API version differs from this provenance. A Docker-backed validation on `2026-08-01` confirmed InvenTree `1.4.3` still reports API version `511` and passes the blocking integration suite against the existing client contracts. This does not establish byte-for-byte identity with the checked-in schema snapshot originally fetched from InvenTree `1.4.0`.

The checked REST schema exposes stable primary keys and endpoint/media/link fields, but it does not define a canonical user-facing frontend URL for each object. F-S34 therefore treats frontend route construction as a separate version-pinned MCP contract: browser `web_url` values come from optional `INVENTREE_WEB_URL` or all-mode fallback to `INVENTREE_URL` after the same credential-free validation, use the reviewed InvenTree 1.4.3 frontend route map pinned to commit `6b237de54e4cbfd7f51daff8403c17869898d965` and router blob `ddeb3a21365761e999568c84d6417915817a9024`, and remain distinct from REST and media URLs. Records without their own stable frontend page omit `web_url` and use universal `parent_web_url` only for an immediate owning object with a stable frontend page and identity present in the same projection. The complete matrix and output inventory are in [`docs/web-links.md`](web-links.md).

## Verified Auth and Token Endpoints

These are InvenTree upstream authentication endpoints and schemes. They are not the MCP server's HTTP OAuth endpoints.

The MCP server's ChatGPT-facing OAuth issuer is separate from these InvenTree endpoints. Do not point ChatGPT directly at InvenTree `/o/authorize/` or `/o/token/` unless the product plan is explicitly changed.

Security schemes:

- `tokenAuth` uses `Authorization: Token <token>`.
- The schema also describes OAuth2 endpoints with `authorizationUrl: /o/authorize/`, `tokenUrl: /o/token/`, and `refreshUrl: /o/revoke_token/`.

Current-user validation endpoints:

- `GET /api/user/me/` retrieves the authenticated user's record and is suitable as a cheap credential validity check.
- `GET /api/user/me/roles/` retrieves the authenticated user's roles and is also suitable as a cheap credential validity check.

Current-user API token endpoints:

- `GET /api/user/me/token/?name=<name>` is schema-confirmed for token issuance/lookup behavior, but implementation must verify whether the response includes a usable secret only at creation time. Do not rely on any InvenTree endpoint to recover an already-created token secret.
- `GET /api/user/tokens/` lists current-user API tokens.
- `POST /api/user/tokens/` creates a current-user API token.
- `GET /api/user/tokens/{id}/` retrieves current-user API token metadata.
- `DELETE /api/user/tokens/{id}/` revokes or deletes a current-user API token.

F-S08 uses `GET /api/user/me/` to bind setup to the authenticated InvenTree user and `GET /api/user/me/token/?name=inventree-mcp-chatgpt-<random-setup-id>` to create a uniquely named dedicated connector token without rotating the supplied credential or an earlier connector token. Default-on Testcontainers coverage verifies that the endpoint returns a one-time usable token secret on the pinned InvenTree baseline. Abandoned or expired authorizations can leave unused dedicated tokens that the operator must revoke through InvenTree's token management UI/API. If token creation is unavailable, setup discards the submitted secret and requires explicit operator consent before sealing a re-entered supplied credential.

HTTP MCP connector auth mapping:

- STDIO mode may use configured `Token` or `Bearer` upstream credentials directly.
- HTTP mode should not pass raw InvenTree `Authorization` headers through unchanged. The MCP server should validate its own OAuth access-token envelope, recover the sealed upstream credential, and then call InvenTree using `Authorization: Token ...` or `Authorization: Bearer ...`.

## Verified Parameter Template Endpoints

- `GET /api/parameter/template/` searches parameter templates.
- `POST /api/parameter/template/` creates a template from the `ParameterTemplate` schema.
- `GET /api/parameter/template/{id}/` reads one template.
- `PATCH /api/parameter/template/{id}/` preserves omitted fields through `PatchedParameterTemplate`, including explicit empty/false values and explicit null selection-list removal.
- `DELETE /api/parameter/template/{id}/` deletes one template only after tool-layer reference preflight.
- `GET /api/parameter/` with exact `template` scans template references across model types. Template administration bounds this scan to 1,000 rows and fails closed above that limit.
- `GET /api/part/category/parameters/` supplies category-default reference preflight. Because the pinned API does not filter this endpoint by template, F-S11 scans at most 1,000 links, fails closed if completeness cannot be established, and leaves mutation to the category-default administration story.

The tool layer refuses direct deletion while any parameter row or category-default link remains. Template merge migrates only non-conflicting `part.part` rows, preserves unmapped values, normalizes only explicit `value_map` entries, and refreshes both reference sets immediately before deleting an empty source. These REST operations are not atomic; operators must prevent concurrent template/reference administration because a narrow reference-creation race remains between final preflight and delete.

## Verified Attachment Endpoints

- `GET /api/attachment/` lists attachments.
- `POST /api/attachment/` creates attachments and supports `multipart/form-data`.
- `DELETE /api/attachment/` performs bulk delete.
- `GET /api/attachment/{id}/` retrieves attachment metadata.
- `PUT /api/attachment/{id}/` updates attachment data.
- `PATCH /api/attachment/{id}/` partially updates attachment data.
- `DELETE /api/attachment/{id}/` deletes one attachment.

Useful list filters:

- `model_type`
- `model_id`
- `is_file`
- `is_image`
- `is_link`
- `has_thumbnail`
- `tags`
- `upload_user`
- `search`
- `limit`
- `offset`

Attachment fields include:

- `attachment`
- `thumbnail`
- `filename`
- `link`
- `comment`
- `is_image`
- `upload_date`
- `upload_user`
- `file_size`

- `model_type`
- `model_id`
- `tags`

Attachment content download is not a separate schema endpoint in the current schema snapshot. Implement `download_attachment` by resolving metadata first, rejecting out-of-scope attachment `model_type` values, then fetching the schema-exposed `attachment` URL by default or `thumbnail` URL in explicit thumbnail mode through the InvenTree client, scoped to the configured InvenTree base URL and authenticated as the current InvenTree user. Do not treat arbitrary user-provided URLs or stored link targets as downloadable attachment content.

Workflow mapping:

- `upload_attachment` posts a file attachment using the `attachment` field and never accepts HTTP(S) URLs.
- `download_attachment` reads an existing file attachment using the metadata `attachment` field by default, or `thumbnail` field in explicit thumbnail mode, and returns bounded content to the caller.
- `download_part_image` reads a part's primary image by resolving only that part's readable schema-exposed `image` field or the part thumbnail endpoint and returns bounded content to the caller. It does not accept arbitrary URLs and does not require the operator to know a generic attachment ID.
- `upload_attachment_from_url` fetches remote bytes under the server's URL-fetch policy, then posts a file attachment using the `attachment` field.
- `create_link_attachment` stores a URL in the `link` field without fetching the URL.

Attachment model types in the schema include:

- `build`
- `company`
- `manufacturerpart`
- `supplierpart`
- `purchaseorder`
- `returnorder`
- `salesorder`
- `salesordershipment`
- `transferorder`
- `part`
- `stockitem`

Initial implementation should expose only non-sales model types relevant to the current product scope.

The attachment endpoint's `AttachmentModelTypeEnum` uses short, unqualified values such as `part`, `stockitem`, and `purchaseorder`. This is distinct from the parameter endpoint's `ModelTypeD42Enum`, which uses qualified Django-style `app.model` values such as `part.part`, `stock.stocklocation`, and `order.purchaseorder`. MCP tools preserve both schema-defined vocabularies: attachment tools accept only the in-scope short values, while parameter-template administration accepts the qualified values or an explicit empty unrestricted value. Neither vocabulary is an alias for the other.

## Verified Image Fields

- `Part` exposes readable `image` and write-only `existing_image`.
- `Company` exposes `image`.
- Generic attachments expose `is_image`, `thumbnail`, and `file_size`.
- `/api/part/thumbs/` and `/api/part/thumbs/{id}/` expose part thumbnail listing and update behavior using `PartThumb` and `PartThumbSerializerUpdate`.
- `/api/notes-image-upload/` exposes note image upload behavior, which is separate from inventory object attachments.

Primary-image behavior must be implemented per object type from schema-verified fields rather than assumed generically.
Part primary image download is part-specific for the first release. It should use the part record's readable `image` value or the part thumbnail endpoint, scope the fetch to the configured InvenTree base URL, authenticate as the current InvenTree user, and apply the same maximum-size and redaction controls as attachment downloads. `existing_image` is write-only and is only valid as assignment/update input.
Part primary image assignment uses multipart `PATCH /api/part/{id}/` with the `image` file field after the tool resolves and downloads an existing same-part image attachment. The live InvenTree 1.4.0/API 511 integration check rejected using a generic attachment URL with `PATCH /api/part/thumbs/{id}/`; that endpoint remains verified for thumbnail retrieval/update schema behavior, while the tool contract keeps assignment behind `set_primary_image`. `existing_image` is write-only and is not used as a download source or caller-facing shortcut.
Company primary-image assignment uses multipart `PATCH /api/company/{id}/` with the `image` file field. Pinned InvenTree 1.4.3/API 511 validation proves that first assignment stores the original bytes, replacement returns a distinct collision-safe image URL and removes the prior media file, and JSON `image:null` clears the exact company association and removes the current media file. F-S31 verifies assignment and replacement by downloading only the resulting same-instance schema-exposed URL and comparing its SHA-256 digest with the submitted bytes; clear requires confirmed exact-null read-back. These operations preserve all company roles and unrelated detail fields and are distinct from generic company attachments.
Notes image upload, generated report attachments, stock test-result attachments, and other app-specific file surfaces are out of first-release scope unless a later task explicitly adds them.

## Attachment and Image Capability Table

| Object type | Generic attachment support | Upload field / storage | Metadata PATCH | Primary image support | Initial scope |
| --- | --- | --- | --- | --- | --- |
| `part` | `/api/attachment/` with `model_type=part`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | multipart `PATCH /api/part/{id}/` with `image` file field after resolving an existing same-part image attachment | yes |
| `stockitem` | `/api/attachment/` with `model_type=stockitem`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | yes |
| `company` | `/api/attachment/` with `model_type=company`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | multipart `PATCH /api/company/{id}/` with `image`; JSON null clears | yes |
| `manufacturerpart` | `/api/attachment/` with `model_type=manufacturerpart`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | yes |
| `supplierpart` | `/api/attachment/` with `model_type=supplierpart`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | yes |
| `purchaseorder` | `/api/attachment/` with `model_type=purchaseorder`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | yes |
| `build` | `/api/attachment/` with `model_type=build`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | later |
| `returnorder` | `/api/attachment/` with `model_type=returnorder`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | no, sales/returns deferred |
| `salesorder` | `/api/attachment/` with `model_type=salesorder`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | no, sales deferred |
| `salesordershipment` | `/api/attachment/` with `model_type=salesordershipment`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | no, sales deferred |
| `transferorder` | `/api/attachment/` with `model_type=transferorder`, `model_id=<id>` | `attachment` file field or `link` URL field | `/api/attachment/{id}/` with `PatchedAttachment` | no schema-confirmed primary-image field | later |

Registered attachment/image tools must only expose object types marked in scope, and tests should fail if a tool exposes an object type not listed here.

Bulk attachment delete (`DELETE /api/attachment/`) is schema-confirmed but out of scope for the initial implementation. If exposed later, it needs a separate destructive tool, dry-run listing, object/prefix scoping, and stricter confirmation than single attachment delete.

## Milestone Endpoint Coverage

The initial endpoint manifest covers schema-confirmed paths, methods, operation IDs, selected query filters, request schemas, response schemas, and PATCH support for every milestone client-method dependency in these areas:

- Part search/get/create/update plus category search/get/create/PATCH administration delivered by F-S19.
- Company search/get/create/update and role filters, plus supplier-part and manufacturer-part link search/get/create/update dependencies implemented by F-S20. Company tags are excluded because the checked company schema and pinned stable-detail response do not expose them for read-back verification. The manufacturer-part schema requires `part` and `manufacturer` but declares `MPN` nullable and optional. Pinned InvenTree 1.4.3 nevertheless rejects POST creation when `MPN` is omitted; F-S15 preserves the schema-facing optional input by omitting null, blank, and whitespace-only values, returning bounded validation from the direct create tool, and making the combined workflow reuse one exact part/manufacturer link, clarify multiple links, or skip creation when none exists rather than inventing a value. The supplier list schema supports the generic `company` filter, `manufacturer_part` reverse-link filter, and `SKU` ordering; the manufacturer list supports `manufacturer` filtering and `MPN` ordering. Role-removal postflight uses the generic supplier `company` filter and a bounded unfiltered manufacturer snapshot because the role-specific filters are rejected after the corresponding role is removed. Because neither ordering enum exposes the stable primary key, F-S20 fetches one complete snapshot of at most 1,000 matching rows and performs stable-ID sorting locally rather than offset-scanning an unstable ordering. Public tools expose only approved patch fields and sanitize links and recovery projections instead of mirroring every writable serializer field.
- Stock location search/get/create/PATCH, location-type search/get references, stock item search/get/create/constrained PATCH, and native add, remove, count, transfer, and status-change operations. F-S21 exposes location name, description, parent, owner, custom icon, structural, external, and location-type fields, but separates operational hierarchy/structural/external changes from ordinary metadata. Its stock-item PATCH surface is limited to batch, expiry date, packaging, notes, and HTTP(S) link with current-state confirmation; generic PATCH and location/quantity/status/serial/source/ownership/pricing/deletion/install/order/build fields remain unavailable. F-S27 relocates one complete safe item only through the dedicated native transfer endpoint rather than generic PATCH.
- Purchase-order extra-line list/detail/create/PATCH/delete at `/api/order/po-extra-line/`. F-S23 exposes non-receivable order, quantity, signed unit price/currency, reference, description, supplier line number, link, notes, and target date fields with dry-run planning, trimmed case-sensitive per-order reference recovery, exact read-back, and confirmed single-record deletion. `project_code` remains schema-visible but is excluded until a separate lookup and validation contract is approved. The pinned model permits nonnegative quantities and signed prices; InvenTree recalculates purchase-order `total_price` as quantity times unit price in the order currency, so zero-priced informational lines do not change the total and negative lines represent discounts.
- Parameter values, parameter templates, and category parameter template links.
- Purchase order preview plus F-S03 order/line search, get, create, PATCH, issue, and receive dependencies, including direct supplier and supplier-part retrieval for stable-ID validation.
- Attachment, link attachment, URL upload, part primary-image update, and guarded company primary-image assignment/replacement/clear behavior.

Future endpoint-specific client methods must use manifest entries rather than ad hoc path strings. Adding a method without a manifest entry or changing `docs/api-schema.yaml` without updating the manifest/provenance should fail the schema checks.
The manifest is endpoint-level schema coverage, not a complete upload authorization boundary. Attachment `model_type`, accepted file fields, URL sources, and primary-image object rules remain enforced by the attachment/image client and tool tests when those tools are implemented. The manifest records in-scope and deferred attachment model types so those later tests have a machine-readable scope source.

## Verified Part Category Administration Endpoints

- `GET /api/part/category/` supports parent and top-level filters plus `limit` and `offset`; F-S19 scans 100-row pages and fails closed above 1,000 same-parent candidates when proving duplicate identity.
- `POST /api/part/category/` creates one category from the schema-writable `name`, `description`, `default_location`, `default_keywords`, `parent`, `structural`, and `icon` fields.
- `GET /api/part/category/{id}/?path_detail=true` retrieves stable identity, parent/path hierarchy, direct part and child counts, structural state, category/default-location metadata, inherited parent default location, keywords, and icon.
- `PATCH /api/part/category/{id}/` uses `PatchedCategory`; tool inputs preserve omitted fields, explicit empty/false values, and explicit null clearing for nullable parent, default-location, default-keywords, and icon fields.
- `GET /api/part/` with exact `category` and explicit `cascade=false` establishes the direct-part count used by structural-state safeguards.
- Category names are trimmed before writing and duplicate comparison uses case-insensitive same-parent identity. Roots compare only with roots; the same name under another parent is allowed. The same policy applies to create, rename, and reparent operations.
- Parent validation walks stable parent IDs and refuses self-parenting and descendant cycles. Operator-approved reparenting may include direct parts and descendants, but requires explicit confirmation after hierarchy preflight. Reparenting changes the category hierarchy only; the MCP workflow does not separately move or delete parts, children, defaults, or stock.
- Structural-state changes require explicit confirmation. A non-structural category with directly assigned parts cannot be promoted to structural through this workflow.
- Category create/update require `inventree.read` and `inventree.write`, are closed-world non-destructive writes, read back the exact stable record, recheck duplicate/hierarchy policy after successful responses, and return read-before-retry recovery guidance for ambiguous mutation results. `update_part_category` is idempotent for absolute fields on one stable ID; category creation remains non-idempotent. These preflight and post-write checks are not atomic with InvenTree mutations, so operators must prevent concurrent category administration across MCP servers, the UI, and direct API clients.

## Verified Stock Adjustment Endpoints

- `POST /api/stock/add/` adds a positive relative quantity to one or more stock items through `StockAdd`.
- `POST /api/stock/remove/` removes a positive relative quantity through `StockRemove`.
- `POST /api/stock/count/` records an absolute stocktake quantity through `StockCount`. The schema permits optional location and per-item metadata changes, but F-S05 intentionally sends only one item ID, absolute quantity, and audit notes so stocktake remains quantity-only.
- `POST /api/stock/change_status/` changes stock status through `StockChangeStatus` and records the operator reason as the transaction note.
- `POST /api/stock/transfer/` accepts `StockTransfer` with an `items` array of stock-item IDs and decimal quantities, transaction `notes`, and one destination `location`. F-S27 intentionally sends exactly one item with its complete reviewed current quantity and an explicit destination. Pinned InvenTree 1.4.3 verification confirms a full transfer preserves the original stable stock-item ID, quantity, batch, packaging, status, supplier/purchase provenance, and price/currency while adding an exact-item tracking entry containing the audit reason.
- F-S05 exposes only single-item operations. Every tool reads current state, produces a state-bound dry-run plan, returns an opaque principal-bound five-minute single-use confirmation token in `plan_hash`, requires that token with `confirm:true` and a nonblank audit reason, then reads back the stock item. Quantity decreases and `Destroyed` (`60`), `Rejected` (`65`), or `Lost` (`70`) statuses are flagged as high-risk.
- The stock adjustment tools are operational, closed-world, non-destructive, and require `inventree.read`, `inventree.write`, and `inventree.operational`. They refuse no-op changes, quantity changes to serialized stock, and zeroing a `delete_on_deplete` item; serialized status-only changes remain supported. Ambiguous mutation results, including HTTP `408`, `425`, and `429`, are not retried automatically.
- F-S24 adds the separate destructive `deplete_stock_item` workflow on the same native `POST /api/stock/remove/` endpoint. It removes the complete current positive quantity only when `delete_on_deplete:true` and the exact stock item is in stock, unallocated, non-serialized, not building or consumed, not installed, and has no parent, child, or installed-item relationship. The high-risk dry run binds safe stock, allocation, relationship, and supplier/order/build provenance fields into a principal-bound confirmation token. Execution records the audit reason and treats exact-ID `404` after removal—including after an ambiguous response—as verified deletion.
- F-S27 adds the operational `transfer_stock_item` workflow. It requires one exact stock-item ID with a current source location, one explicit exact destination-location ID, a nonblank reason, a reviewed plan, and `inventree.read`, `inventree.write`, plus `inventree.operational`. The MCP does not exclude structural, external, owned, or typed destinations during preflight; every exact-read destination is passed to native InvenTree validation, while same-location requests are refused as no-ops. Source stock must be available, positive, unallocated, unserialized, and free of build, consumption, installation, customer/sales, and parent/child relationships. The schema's `tracking_items` value is an audit-event count rather than a child-stock relationship, so it remains visible through stock reads but does not block transfer or enter the current-state verification fingerprint. The tool has no quantity input and reports `will_split:false`. F-S28 owns partial/split behavior and F-S29 owns multi-item batches.
- InvenTree exposes no conditional stock mutation or revision value across these endpoints. Concurrent adjustment of the same stock item by any writer is unsupported: execution rejects state changes observed during its preflight, while a change in the subsequent read/write window can still race and is reported as `partial_failure` when readback diverges.

## Verified Purchase Order Endpoints

- `GET /api/company/{id}/` validates the stable supplier company and supplier role before order creation.
- `GET` and `POST /api/order/po/` search and create purchase orders. F-S03 searches by supplier plus the generic `search` field, then requires an exact client-side `supplier_reference` match for retry recovery; InvenTree generates its pattern-compliant internal `reference` when the workflow creates an order.
- `GET` and `PATCH /api/order/po/{id}/` retrieve and partially update purchase-order metadata.
- `GET` and `POST /api/order/po-line/` search and create supplier-part-backed lines.
- `GET` and `PATCH /api/order/po-line/{id}/` retrieve and partially update individual lines.
- `POST /api/order/po/{id}/issue/` explicitly places a pending order with its supplier. This status transition is separate from receiving and requires operator confirmation at the tool layer.
- `POST /api/order/po/{id}/receive/` receives line quantities only after the order is placed and returns newly created stock items for non-virtual parts. The tool enforces the schema's 15-digit, 5-decimal-place quantity bounds, resolves supplier `pack_quantity_native` into the resulting base-stock quantity, resolves default packaging, validates every item destination, rejects virtual parts, binds confirmation to the current dry-run plan, and never merges into or updates existing stock. A definite API 4xx remains an actionable rejection; an ambiguous transport, decode, or server result is treated as an unknown non-idempotent result requiring line and source-order stock readback before retry.
- `GET /api/stock/` supports the schema-backed `purchase_order` filter and exposes each result's `purchase_order` and `purchase_order_reference`; `search_stock_items` publishes these fields so ambiguous receipt results can be reconciled through the authorized MCP surface.
- The pinned 1.4.0 API schema declares line `purchase_price` as a decimal string, but the live create response encodes it as a JSON number. The typed client accepts both encodings and preserves the value as a decimal string.
- Purchase-order line creates explicitly send `auto_pricing:false` and `merge_items:false` so InvenTree does not replace previewed prices or combine separately referenced workflow lines.
- The order list schema exposes supplier, exact reference, status, search, scheduled-start date, and target-date filters. The line list exposes order, supplier-part (`part`), pending, received, and search filters.
- Receiving is classified as operational and requires `inventree.read`, `inventree.write`, and `inventree.operational`; read scope guarantees that the same caller can perform source-order stock recovery after an ambiguous result. Issuing is an ordinary confirmed purchasing write and requires `inventree.write`.

## Verified Parameter Endpoints

- `GET /api/parameter/` lists parameter values.
- `GET /api/parameter/{id}/` retrieves one parameter value.
- `POST /api/parameter/` creates parameter values.
- `PATCH /api/parameter/{id}/` partially updates parameter values.
- `DELETE /api/parameter/{id}/` deletes one parameter value.
- `GET /api/parameter/template/` lists parameter templates.
- `POST /api/parameter/template/` creates parameter templates.
- `PATCH /api/parameter/template/{id}/` partially updates parameter templates.
- `GET /api/part/category/parameters/` lists category parameter template links.
- `POST /api/part/category/parameters/` creates category parameter template links.
- `PATCH /api/part/category/parameters/{id}/` partially updates category parameter template links.
- `GET /api/part/category/parameters/{id}/` retrieves one category parameter template link.
- `DELETE /api/part/category/parameters/{id}/` deletes one category parameter template link.

Parameter-template and category-default administration are registered milestone tools. The pinned 1.4.3 category-default representation contains `category`, `template`, and `default_value`; it has no requirement flag. Live 1.4.3 behavior also supports the undocumented `category` and `fetch_parent` list parameters: `fetch_parent` defaults true upstream, so MCP administration sends it explicitly and defaults to exact-category results. `set_part_parameters` continues to use existing templates and category links rather than creating either implicitly.

F-S14 bulk propagation uses the schema-backed `category` and `cascade` filters on `GET /api/part/` to establish an exact-category or explicitly descendant-inclusive part set before any write; MCP sends `cascade=false` explicitly for exact selection. The MCP planner reads at most 101 rows to enforce its 100-part bound, validates the schema's 500-character parameter-data maximum, then binds the selected stable part IDs, current parameter rows, and requested value into a principal-bound confirmation plan. Parameter audits and propagation reuse the verified parameter and category-default endpoints above; no bulk-write endpoint or implicit cleanup path is used. Audit filters are applied server-side where the endpoint supports them: exact category first selects parts and direct category links, while a template filter searches and includes same-normalized-name peers. All audit pagination, detail reads, and returned records share one 1,000-unit request-and-record budget.

Parameter guidance:

- Parameter rows and templates use the schema's qualified `ModelTypeD42Enum` values, such as `part.part` and `order.purchaseorder`; the attachment endpoint instead uses a separate short `AttachmentModelTypeEnum`, such as `part` and `purchaseorder`. MCP documentation and input-schema descriptions keep those endpoint contracts separate rather than translating or aliasing them.
- The list endpoint exposes schema-backed `model_type`, `model_id`, `template`, `search`, `limit`, and `offset` filters. It does not expose a direct part-category filter; `search_part_parameters` requires at least one narrowing filter, reads bounded 100-row pages, resolves part records, and applies exact category/value filtering before returning deterministic row-ID-ordered pagination. It refuses searches whose complete filtered ordering cannot be established within a 1,000-row scan bound and asks for narrower filters.
- Search and reuse existing parameter templates before creating new ones.
- Use category parameter links to understand expected parameters for a category.
- Use `search_category_parameter_defaults` for exact-category administration; set `include_parent_defaults:true` only when an effective inherited view is required. Mutate the stable direct `link_id` owned by its source category.
- Ask the operator when multiple templates match by name, units, choices, checkbox state, or category association.
- Do not create new parameter templates from natural language unless the caller explicitly confirms that a new template is required.

## Verified Part Deletion And Guard-Only Reads

`DELETE /api/part/{id}/` is far more permissive than its blast radius suggests. Pinned live behavior against InvenTree 1.4.3 (`TestClientMethodsAgainstInvenTree/part_delete`), isolating one reference category per part rather than combining several:

- InvenTree itself enforces exactly two conditions: the part must be **inactive** first (`non_field_errors: ["Cannot delete this part as it is still active"]`), and a part currently used as a **component in another part's BOM** is protected (`non_field_errors: ["Cannot delete this part as it is used in an assembly"]`).
- Every other reference this schema exposes is **silently permitted** once the part is inactive, and the consequence differs by relation: deleting a part with an existing stock item also **destroys that stock item** (not merely orphans it); a referencing purchase-order line **survives, orphaned**; a part's own BOM (as assembly), a build where it is the top-level part, a sales-order line, a variant (`variant_of`), a supplier part, a manufacturer part, a parameter, an attachment, and a related-part link are all permitted with no independently verified child-record fate beyond "the part itself deletes."
- `delete_part` therefore treats every one of these categories as blocking in its own preflight rather than relying on any upstream protection or cascade.

Four resources had no Go client plumbing before `delete_part` needed them for read-only existence checks. Each is used solely by that tool's guard, not exposed as a standalone MCP tool:

- `GET /api/bom/` -- `part` filters a part's own BOM (as assembly); `uses` filters where a part is consumed as a component elsewhere (including template/variant expansion; exact semantics pinned by the integration test rather than assumed).
- `GET /api/order/so-line/` -- `part` is the direct `Part.PK` (unlike `/api/order/po-line/`, where the response's `part` field is actually the supplier-part PK; `delete_part` uses the dedicated `base_part` filter to query the base `Part.PK` directly instead of fanning out over supplier parts).
- `GET /api/build/` -- `part` filters builds where the part is the top-level built part.
- `GET /api/part/related/` -- `part` matches a relation where the part is on *either* side (`part_1` or `part_2`), pinned by deliberately placing the tested part as `part_2` in the integration test rather than assuming symmetry.
- `GET /api/part/?variant_of=<id>` finds parts that are variants of a given template part; deleting a template with existing variants is blocked.
