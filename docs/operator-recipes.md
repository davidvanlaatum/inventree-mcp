# Operator Recipes

This file is the source of truth for first-release operator workflows. README should link here instead of duplicating full recipes.

Each recipe should preserve omitted fields versus explicit zero/false/empty values, prefer existing InvenTree records, and return a structured clarification instead of guessing when lookup results are ambiguous.

## ChatGPT Connector OAuth Setup

- Required inputs: public connector URL, configured canonical HTTPS issuer/resource URLs, InvenTree credential supplied during setup.
- Preferred flow: verify connector metadata, start OAuth authorization, collect InvenTree credential on the setup page, validate with `/api/user/me/` or `/api/user/me/roles/`, create or seal a dedicated connector token, exchange authorization code for MCP OAuth tokens.
- Clarify when: redirect URI/client registration behavior has not been verified against current OpenAI docs, token creation is permission-denied, or the operator must choose between canceling setup and sealing a supplied token.
- Expected output: connector authorization success with non-sensitive credential-source metadata.

## STDIO Setup

- Required inputs: `INVENTREE_URL`, `INVENTREE_TOKEN`, optional `INVENTREE_AUTH_SCHEME`.
- Preferred flow: validate configuration, seed logging context, run `inventree-mcp serve --transport stdio`, perform a read-only smoke test.
- Clarify when: auth scheme is neither `Token` nor `Bearer`, URL is missing, or TLS skip verify is requested outside local/test use.
- Expected output: STDIO MCP server ready for local clients.

## Reverse-Proxy HTTP Deployment

- Required inputs: internal listen address, public canonical HTTPS issuer/resource URLs, trusted proxy settings, envelope keys, rate-limit settings.
- Preferred flow: configure reverse proxy TLS, expose only the proxy-facing listener, set canonical URLs explicitly, configure trusted forwarded headers, validate metadata/challenge URLs.
- Clarify when: public URL differs from proxy routing, path prefix handling is unclear, or production config enables TLS skip verify.
- Expected output: HTTP MCP endpoint with OAuth metadata that never leaks internal hostnames or ports.

## Packaged Systemd Deployment

- Required inputs: release package for the target Linux distribution, private HTTP listen address, public reverse-proxy route, and OAuth/key settings once the HTTP OAuth milestone is complete.
- Preferred flow: install the `deb`, `rpm`, or `apk` artifact from the GitHub release, edit `/etc/inventree-mcp/inventree-mcp.env`, keep `INVENTREE_MCP_LISTEN` bound to loopback or a private service network, and enable `inventree-mcp.service` only after production OAuth support exists.
- Clarify when: the operator expects STDIO mode from the packaged service, wants to expose the Go listener directly to the internet, asks to enable production HTTP mode before OAuth is implemented, or expects Alpine/OpenRC service management from the `apk` package.
- Expected output: installed package files now, and a systemd-managed `inventree-mcp serve --transport http` process behind the deployment's reverse proxy once production OAuth is available. Pre-OAuth smoke tests should run the binary directly in development mode and expect only the skeleton MCP server plus read-only health/version tool.

## Maintainer Release

- Required inputs: clean `main`, selected semantic version tag `vX.X.X`, passing local validation, and GitHub Actions permissions that allow `contents: write`.
- Preferred flow: run `GOFLAGS=-trimpath go test -race ./...`, run `goreleaser check` and `goreleaser release --snapshot --clean` when the CLI is installed, confirm the `Release Preview` workflow passed on the release PR, create and push the `vX.X.X` tag, watch the GitHub `Release` workflow, then verify the GitHub release assets and `checksums.txt`.
- Clarify when: the version number is unclear, the tag already exists, GitHub Actions or `GITHUB_TOKEN` release permissions are disabled, snapshot package validation has not passed, or the release should include signing, SBOMs, containers, Homebrew, OpenRC packaging, or package repositories beyond GitHub release assets.
- Expected output: GitHub release containing Linux/macOS/Windows binary archives, Linux `deb`/`rpm`/`apk` packages, and checksums.

## Add Or Update A Purchasable Part

- Required inputs: part name or IPN/SKU, category or category ID, units where required, supplier/manufacturer details when available.
- Preferred lookup order: search parts, search categories, search companies, search supplier/manufacturer part records, then create or update only the missing pieces.
- Clarify when: part/category/company matches are ambiguous, an existing part may already represent the requested item, or supplier/manufacturer identifiers conflict.
- Tool sequence: use `upsert_part_with_supplier_and_manufacturer` with `dry_run:true` first when the operator wants one safer workflow-level plan, then retry without `dry_run` after reviewing the plan. Use lower-level `search_parts`, `search_part_categories`, role-specific company searches, `create_part`/`update_part`, `create_supplier_part`, and `create_manufacturer_part` when the operator needs step-by-step control.
- Expected output: `status`, `actions`, stable selected or created part, supplier, manufacturer, supplier-part, and manufacturer-part records when available, plus `omitted_recommended_fields` for missing recommended values. In `dry_run` responses, planned creates are represented by `actions` because stable IDs do not exist until the write runs. If a required stable ID, currency, supported company role, SKU, or duplicate decision is missing, the tool returns structured clarification.
- HTTP note: write tools are STDIO-only until per-tool OAuth scope enforcement is implemented.

## Add Or Update Part Parameters

- Required inputs: part ID, requested parameter names/values, units where relevant.
- Preferred lookup order: `search_parameter_templates`, existing `get_part_parameters`, category parameter links, then update existing values or create new values against unambiguous existing templates.
- Clarify when: same-name linked templates differ by unit/choices/checkbox settings, only global/unlinked matches exist, or creating a new template/category link would be required. The milestone tool reports unlinked/global matches as context but does not write them.
- Tool sequence: `search_parameter_templates`, `get_part_parameters`, `set_part_parameters`.
- Expected output: parameter IDs updated/created and any unresolved parameter questions.

## Create Initial Stock

- Required inputs: part ID, stock location ID, quantity, status when required by local convention.
- Preferred lookup order: `get_part`, `search_stock_locations`, `search_stock_items` for duplicate detection.
- Clarify when: location is ambiguous, quantity/status is unclear, or existing stock at the same location may duplicate the requested initial stock.
- Tool sequence: use `create_initial_stock_entry` with `dry_run:true` when the operator wants a single workflow-level plan, then retry without `dry_run` after reviewing the duplicate preflight. Use `search_parts` or `get_part`, `search_stock_locations`, `search_stock_items`, then `create_stock_item` when the operator needs step-by-step control.
- Expected output: `status`, `dry_run`, ordered `actions`, selected part and location records, and the created stock item record when executed, or a structured duplicate clarification with candidate stock item IDs and retry values. In `dry_run` responses, the planned stock create appears in `actions` because the stock item has no stable ID yet.

## Attach Datasheet Or Photo

- Current status: upload, URL-copy, stored-link, metadata update, and delete tools are planned but not registered yet. Use this recipe today only to gather stable target IDs and attachment/image decisions before the M1F attachment tools land.
- Required inputs: target object type and ID, filename, content type, and exactly one upload source.
- Accepted sources: inline bytes in any mode; STDIO allowlisted local path; HTTP(S) URL only through `upload_attachment_from_url`; stored link only through `create_link_attachment`.
- Source resolver behavior: inline bytes are size-capped before upload, STDIO local paths must sit under trusted operator-controlled allowlisted roots and are rejected in HTTP mode before filesystem access, and URL-copy sources must pass SSRF checks without forwarding MCP or InvenTree auth headers.
- Clarify when: target object is ambiguous, URL intent could mean upload-copy or store-link, duplicate filename/content exists, or source policy rejects the input.
- Current tool sequence: `list_attachments`, then stop with the stable target ID, existing attachment context, and the explicit upload/link decision for the later M1F tools.
- Planned M1F tool sequence: `list_attachments`, then `upload_attachment`, `upload_attachment_from_url`, or `create_link_attachment`.
- Expected output after M1F registration: attachment ID, target object, filename, size or link classification, content type, and thumbnail/image state when available.

## Download Attachment Content

- Required inputs: stable attachment ID.
- Preferred lookup order: `get_attachment_metadata`, then `download_attachment` only when metadata identifies an in-scope attachment with a file URL on the configured InvenTree instance. Request explicit thumbnail mode when the operator wants the thumbnail rather than the original file.
- Clarify when: the attachment is a stored link and the operator might mean stored-link metadata versus an external link target, or the operator asks for a thumbnail but the target has both original and thumbnail content.
- Structured non-success when: content exceeds the configured download limit, metadata URL redirects or points outside the configured InvenTree instance, or the attachment target object type is out of milestone scope.
- Tool sequence: `get_attachment_metadata`, then `download_attachment`.
- Expected output: filename, content type when known, size, SHA-256 hash, selected download mode, and base64 content for binary files or text for allowlisted textual content types.

## Download Part Primary Image

- Required inputs: stable part ID.
- Preferred lookup order: `get_part`, then `download_part_image` when the part has a readable schema-exposed primary image. Request explicit thumbnail mode when the operator wants the generated part thumbnail rather than the original primary image.
- Clarify when: the operator might mean a generic attachment rather than the current primary image, or asks for a thumbnail but both original and thumbnail content are available.
- Structured non-success when: the part has no primary image, content exceeds the configured download limit, or the image URL redirects or points outside the configured InvenTree instance.
- Tool sequence: `get_part`, then `download_part_image`.
- Expected output: part ID, filename when known, content type when known, size, SHA-256 hash, selected download mode, and base64 image content.

## Set Or Replace Primary Part Image

- Current status: primary-image replacement is planned but not registered yet. Use this recipe today only to inspect existing attachments/images and collect the explicit replacement decision before the M1F image tools land.
- Required inputs: part ID and attachment/image ID, plus `confirm:true` when replacing an existing primary image.
- Preferred lookup order: `list_attachments`, inspect image-capable attachments, then set primary image only when the candidate is unambiguous.
- Clarify when: multiple images are plausible, the image is already attached elsewhere, or replacement lacks confirmation.
- Current tool sequence: `list_attachments`, inspect current image-capable candidates, then stop with the stable part ID, candidate attachment/image ID, and explicit replacement decision for the later M1F tool.
- Planned M1F tool sequence: `list_attachments`, optionally upload an image through a registered upload tool, then `set_primary_image`.
- Expected output after `set_primary_image` is implemented: part record, selected attachment/image ID, and replacement confirmation status.

## Preview Purchase Order Lines

- Required inputs: supplier ID or supplier part IDs, quantities, and any known pricing/currency.
- Preferred lookup order: search supplier, search supplier parts for requested part IDs, validate that each line resolves to exactly one supplier-part link for a single supplier, then produce a no-write preview.
- Clarify when: supplier part is ambiguous, a supplier-part ID conflicts with the requested supplier or part, quantity is missing or non-positive, or price/currency is missing and required for the operator's decision.
- Tool sequence: `search_suppliers`, `search_parts`, then `preview_purchase_order_with_lines`. Provide `supplier_part_id` when known; otherwise provide `supplier_id`, `part_id`, and optional `supplier_sku` so the preview can validate that exactly one supplier-part link matches.
- Expected output: proposed lines, supplier part IDs, optional line totals when price and currency are supplied, warnings for omitted preview-only pricing, and confirmation by tool class that no purchase order was created. The tool does not create purchase orders or purchase-order lines.

## Resolve Structured Clarification Prompts

- Required inputs: the stable retry field requested by the prior tool response.
- Preferred flow: show the exact `question`, candidate IDs/URLs, and retry field to the operator; retry the original tool with the selected stable ID.
- Clarify when: the operator chooses a free-form value that still does not identify a stable record.
- Expected output: successful retry or a narrower clarification response.

## Use Prompt Checklists

- Required inputs: one of the registered prompt names: `new_part_entry_checklist`, `parameter_reuse_checklist`, `attachment_image_checklist`, `initial_stock_entry_checklist`, or `purchase_preview_checklist`.
- Preferred flow: fetch the checklist before starting the workflow, run the listed searches or dry-run planner, show any structured clarification to the operator, and retry with the requested stable IDs.
- Clarify when: the checklist exposes missing required fields, conflicting supplier/part identity, ambiguous parameter templates, duplicate stock, duplicate attachments, unclear upload/link intent, primary-image replacement without `confirm:true`, or purchase preview lines that do not resolve to exactly one supplier-part link.
- Expected output: a stable-ID retry request, a dry-run plan for write-capable workflows, a no-write purchase preview, or a structured clarification object. Future prompt names such as `receive_purchase_order_checklist`, `bom_import_review`, and `stocktake_review` are not exposed until their workflows are implemented.
