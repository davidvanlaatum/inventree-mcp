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
- Tool sequence: `search_parts`, `search_part_categories`, `search_companies` or role-specific search, then `create_part`/`update_part`, `create_supplier_part`, `create_manufacturer_part`.
- Expected output: `status` plus the created or updated part, supplier-part, and manufacturer-part records. If a required stable ID, currency, supported company role, or duplicate decision is missing, the tool returns structured clarification.
- HTTP note: write tools are STDIO-only until per-tool OAuth scope enforcement is implemented.

## Add Or Update Part Parameters

- Required inputs: part ID, requested parameter names/values, units where relevant.
- Preferred lookup order: `search_parameter_templates`, existing `get_part_parameters`, category parameter links, then update existing values or create new values against unambiguous existing templates.
- Clarify when: same-name templates differ by unit/choices/checkbox settings, category-linked versus global template choice is unclear, or creating a new template/category link would be required.
- Tool sequence: `search_parameter_templates`, `get_part_parameters`, `set_part_parameters`.
- Expected output: parameter IDs updated/created and any unresolved parameter questions.

## Create Initial Stock

- Required inputs: part ID, stock location ID, quantity, status when required by local convention.
- Preferred lookup order: `get_part`, `search_stock_locations`, `search_stock_items` for duplicate detection.
- Clarify when: location is ambiguous, quantity/status is unclear, or existing stock at the same location may duplicate the requested initial stock.
- Tool sequence: `search_parts` or `get_part`, `search_stock_locations`, `search_stock_items`, then `create_stock_item`.
- Planned output when `create_stock_item` is implemented: stock item record and duplicate-detection status.

## Attach Datasheet Or Photo

- Required inputs: target object type and ID, filename, content type, and exactly one upload source.
- Accepted sources: inline bytes in any mode; STDIO allowlisted local path; HTTP(S) URL only through `upload_attachment_from_url`; stored link only through `create_link_attachment`.
- Clarify when: target object is ambiguous, URL intent could mean upload-copy or store-link, duplicate filename/content exists, or source policy rejects the input.
- Tool sequence: `list_attachments`, then `upload_attachment`, `upload_attachment_from_url`, or `create_link_attachment`.
- Expected output: attachment ID, target object, filename, size or link classification, content type, and thumbnail/image state when available.

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

- Required inputs: part ID and attachment/image ID, plus `confirm:true` when replacing an existing primary image.
- Preferred lookup order: `list_attachments`, inspect image-capable attachments, then set primary image only when the candidate is unambiguous.
- Clarify when: multiple images are plausible, the image is already attached elsewhere, or replacement lacks confirmation.
- Tool sequence: `list_attachments`, optionally upload an image, then `set_primary_image`.
- Planned output when `set_primary_image` is implemented: part record, selected attachment/image ID, and replacement confirmation status.

## Preview Purchase Order Lines

- Required inputs: supplier ID or supplier part IDs, quantities, and any known pricing/currency.
- Preferred lookup order: search supplier, search supplier parts for requested part IDs, validate purchasability, then produce a no-write preview.
- Clarify when: supplier part is ambiguous, price/currency is missing and required for the operator's decision, or quantities conflict with package multiples/minimum order quantities.
- Tool sequence: `search_suppliers`, `search_parts`, `preview_purchase_order_with_lines`.
- Expected output: proposed lines, supplier part IDs, warnings, and confirmation that no purchase order was created.

## Resolve Structured Clarification Prompts

- Required inputs: the stable retry field requested by the prior tool response.
- Preferred flow: show the exact `question`, candidate IDs/URLs, and retry field to the operator; retry the original tool with the selected stable ID.
- Clarify when: the operator chooses a free-form value that still does not identify a stable record.
- Expected output: successful retry or a narrower clarification response.
