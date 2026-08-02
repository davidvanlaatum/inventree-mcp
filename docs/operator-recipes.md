# Operator Recipes

This file is the source of truth for first-release operator workflows. README should link here instead of duplicating full recipes.

Each recipe should preserve omitted fields versus explicit zero/false/empty values, prefer existing InvenTree records, and return a structured clarification instead of guessing when lookup results are ambiguous.

## Local CLI Self-Update

This recipe applies only to a binary installed directly from a GitHub release archive on supported Linux or macOS `amd64`/`arm64` systems. It is a local CLI operation, not an MCP workflow.

1. Confirm the binary did not come from `deb`, `rpm`, `apk`, Homebrew, or another package manager. Package-managed `/usr/bin/inventree-mcp` must be upgraded with the package manager.
2. Confirm `inventree-mcp version` reports an exact stable `vX.Y.Z` release. Development builds cannot self-update.
3. Before the first update only, run `inventree-mcp self-update --adopt-direct-install`. This creates an owner-only marker bound to the canonical repository and executable path without contacting GitHub or changing the binary. Never adopt a package-managed or uncertain installation.
4. Run `inventree-mcp self-update` for the latest stable release, or `inventree-mcp self-update --version vX.Y.Z` for an exact newer stable release.
5. On success, run `inventree-mcp version` and retain `<installed-path>.previous` until the new binary has completed the operator's normal smoke test.
6. On refusal, follow the reported package-manager/manual-upgrade guidance. Do not use `sudo` to bypass ownership, directory, symlink, hardlink, marker, platform, or package boundaries.

The updater verifies the canonical GitHub archive against `checksums.txt`, sanitizes the staged version probe, uses a persistent owner-only kernel-locked lock file and durable transaction record, rolls back failed installed probes, and never sends InvenTree/MCP credentials. It deliberately rejects the multi-file `v0.0.1` archive; the next single-binary release is the first valid target.

If the command reports that it recovered an interrupted self-update, that invocation has restored the pre-update executable and exited non-zero without selecting or downloading a release. Run `inventree-mcp version`, inspect the restored version, then rerun the original `self-update` command so release selection uses that restored executable. Cleanup-only records left after a completed install or rollback are removed automatically without changing the current executable.

If the later smoke test requires manual recovery, stop every process using the binary and confirm no updater is running. Verify `<installed-path>.previous` is an owner-controlled regular executable in the same protected directory. Copy it to a newly created owner-only staging file in that directory, run the staging file's `version` command, then atomically rename the staging file over the current executable and re-run `version` before restarting services. Do not perform this recovery on package-managed paths or use privilege elevation to bypass a refusal. See [Local CLI self-update policy](self-update.md) for the detailed trust, transaction, and recovery behavior.

## First-Release Tool Surface

- STDIO mode registers read-only lookup/download tools, prompt checklists, write workflow tools, attachment/image tools, and the read-only `health_version` tool.
- HTTP development mode from the CLI registers read-only tools and `health_version`. Production HTTP mode registers the full tool surface only behind OAuth authorization mode; every scoped call is checked against the tool's required scopes before the handler runs.
- The checked machine-readable source is `docs/tool-manifest.json`, generated with `go generate ./internal/tools`.
- Use `docs/tool-reference.md` for field-level contracts, mutation classes, upload sources, required scopes, MCP annotations, and clarification retry fields.

## ChatGPT Connector OAuth Setup

Production HTTP includes protected-resource and authorization-server metadata, an operator-facing setup page, CIMD `private_key_jwt` client authentication, PKCE S256 authorization-code exchange, refresh, encrypted MCP token envelopes, request-scoped credential recovery, and per-tool scope guards. The implementation follows the current [OpenAI authentication guide](https://developers.openai.com/plugins/build/auth) and does not allow an unsigned `none` downgrade.

- Required inputs: public connector URL, configured canonical HTTPS issuer/resource URLs, an allowed ChatGPT client metadata URL advertising the exact callback and same-origin JWKS, and an InvenTree Token or Bearer credential supplied only on the setup form.
- Preferred flow: ChatGPT discovers OAuth metadata; the server validates the CIMD document and PKCE request; the operator submits an InvenTree credential; the server validates `/api/user/me/`, creates a uniquely named dedicated connector token through `/api/user/me/token/` without rotating the submitted or earlier connector token, and returns a one-time code; ChatGPT authenticates the token exchange with a signed `private_key_jwt` assertion; the server issues encrypted access and refresh envelopes.
- Fallback: if dedicated-token creation is unavailable, the page discards the submitted secret and requires the operator to re-enter it plus explicitly select `Use this supplied credential`. The explicit `Cancel` action returns `access_denied` without using the credential. No authorization code is issued before that choice.
- Security boundary: MCP scopes restrict which handlers may run, but they do not reduce the upstream permissions carried by the sealed InvenTree credential. Setup state and CSRF cookies are short-lived and one-time; client assertion IDs are replay-protected within one running process, so restart or multi-replica replay protection requires external shared state; credential bodies are excluded from the MCP traffic log; browser responses use no-store, no-referrer, frame denial, and restrictive CSP headers. The HTTP server bounds request-body reads to 30 seconds and credential validation/token creation to 15 seconds.
- Dedicated-token cleanup: every successful creation uses a random setup-specific name so existing API keys and connector sessions are not rotated. If setup is abandoned after creation, code exchange never completes, or an authorization is no longer needed, revoke the unused `inventree-mcp-chatgpt-*` token in InvenTree. The database-free MCP server does not retain token IDs for automatic cleanup.
- Expected output: connector authorization success with `credential_source` set to `dedicated_token` or `supplied_token`, without returning the upstream credential.

## STDIO Setup

- Required inputs: `INVENTREE_URL`, `INVENTREE_TOKEN`, optional `INVENTREE_AUTH_SCHEME`, `INVENTREE_UPLOAD_ALLOW_ROOTS`, `INVENTREE_UPLOAD_MAX_BYTES`, and optional `INVENTREE_MCP_DEBUG_TRAFFIC_LOG`.
- Preferred flow: validate configuration, seed logging context, run `inventree-mcp serve --transport stdio`, perform a read-only smoke test.
- Local upload flow: configure trusted operator-controlled upload roots with `INVENTREE_UPLOAD_ALLOW_ROOTS` or repeated `--upload-allow-root`; tune the byte limit with `INVENTREE_UPLOAD_MAX_BYTES` or `--upload-max-bytes` when the default limit is too small.
- MCP protocol flow: current clients use MCP `2026-07-28` discovery. Legacy initialization remains supported.
- Debug traffic flow: set `--debug-traffic-log /secure/path/mcp-traffic.jsonl` or `INVENTREE_MCP_DEBUG_TRAFFIC_LOG` only while diagnosing MCP client behavior. The JSON Lines file records full MCP request and response payloads, including structured clarification results, tool arguments, and any sensitive data the MCP client sends.
- Clarify when: auth scheme is neither `Token` nor `Bearer`, URL is missing, upload allowlisted roots are not trusted, or TLS skip verify is requested outside local/test use.
- Expected output: STDIO MCP server ready for local clients.

## Reverse-Proxy HTTP Deployment

Production reverse-proxy HTTP deployment has configured canonical protected-resource and authorization-server metadata, bearer challenges, token audiences, setup/token endpoints, and OAuth-protected startup wiring. It supports path-preserving proxies with explicit trusted proxy CIDRs. Live packaged connector validation remains F-S10 work before this deployment is treated as operator-supported.

HTTP request bodies are bounded by `INVENTREE_MCP_MAX_REQUEST_BODY_BYTES` or `--mcp-max-request-body-bytes`, which defaults to 8 MiB. If the HTTP inline-upload limit is raised, raise this limit enough for base64 expansion plus JSON overhead; HTTP startup rejects inconsistent values. The setting does not constrain STDIO uploads. Keep the limit finite on untrusted HTTP endpoints.

Current HTTP clients use MCP `2026-07-28` sessionless POST requests, while legacy initialization remains supported. Client cancellation of a current-protocol POST propagates to the active tool handler; operators should treat cancellation as an unknown-result boundary for upstream writes unless the tool response or InvenTree read-back proves the outcome.

HTTP mode accepts the same debug traffic log option as STDIO. HTTP debug entries include request URIs including query strings, request bodies, response bodies, and streaming response chunks. In production, bearer authentication runs before full-body traffic capture, so unauthenticated request bodies are rejected without being written to the debug log. Authenticated request bodies and non-streaming responses are captured up to 1 MiB and use `body_truncated:true` when more data was forwarded; streaming response chunks are capped individually. Requests above the configured MCP request limit fail closed. Treat this file as sensitive and operator-local.

- Required inputs: internal listen address, `INVENTREE_URL`, public HTTPS issuer/resource URLs, exact public/internal MCP path, envelope keys, allowed client metadata URLs, trusted proxy CIDRs, and token lifetimes.
- Preferred flow: bind `INVENTREE_MCP_LISTEN` to loopback or a private service network; set `INVENTREE_MCP_PATH` to the exact path in `INVENTREE_MCP_OAUTH_RESOURCE_URL`; set `INVENTREE_MCP_TRUSTED_PROXY_CIDRS` to only the controlled proxy hops that can connect to the listener; and route the public path to the listener without stripping or rewriting its prefix. A same-host proxy normally uses `127.0.0.1/32,::1/128`; containers or private networks require the deployment's narrow actual proxy CIDRs.
- Forwarding policy: the configured issuer/resource URLs are authoritative. The server ignores `X-Forwarded-Host`, `X-Forwarded-Proto`, and `X-Forwarded-Prefix`. It accepts `X-Forwarded-For` only when the socket peer is trusted, walks the chain right-to-left through trusted hops, and uses the first untrusted address as the normalized source for per-IP limits and request-scoped logs. It never logs the raw forwarding header. Configure each trusted proxy to append its observed client address and prevent untrusted clients from connecting directly to the listener.
- Prefix example: issuer `https://mcp.example.test/connectors/inventree`, resource `https://mcp.example.test/connectors/inventree/mcp`, and `INVENTREE_MCP_PATH=/connectors/inventree/mcp` require the proxy to preserve `/connectors/inventree` on metadata, authorization, token, setup, and MCP requests. Prefix stripping and `X-Forwarded-Prefix` reconstruction are unsupported.
- Verify through the proxy: fetch `/.well-known/oauth-protected-resource<resource-path>` and `/.well-known/oauth-authorization-server<issuer-path>`, check their issuer/resource/endpoints against configured public URLs, then confirm the MCP resource returns a bearer challenge containing the canonical protected-resource metadata URL. Internal listener names, ports, and forwarding-header values must not appear.
- Troubleshooting: a startup error that the resource path does not match the HTTP path indicates a prefix mismatch; a prefixed public route returning `404` usually means the proxy stripped or rewrote the path; every request sharing the proxy IP for rate limits/logs means the peer CIDR is absent or the forwarding chain is malformed; unexpected public metadata must be corrected in explicit OAuth configuration rather than forwarded headers.
- Clarify when: the proxy cannot preserve paths, the trusted hop CIDRs are dynamic or unknown, the listener is reachable by untrusted clients, production config enables TLS skip verify, the requested client ID is not an HTTPS CIMD URL, or the document does not advertise `private_key_jwt` and a same-origin JWKS.
- Expected output now: HTTP MCP endpoint with connector setup, configured canonical OAuth metadata, signed-client token exchange, trusted source-IP resolution, and no MCP dispatch without a valid encrypted access-token envelope. Live packaged connector validation remains F-S10 work.

### OAuth Envelope Key Lifecycle

- Generate each 32-byte key with a cryptographically secure generator, for example `openssl rand -base64 32`, and assign a non-secret unique key ID. Store the resulting `INVENTREE_MCP_OAUTH_KEYS` value only in protected process environment or `/etc/inventree-mcp/inventree-mcp.env`, which packaged installs create with mode `0600`; never pass key material on the command line, commit it, or write it to the debug traffic log.
- Keep exactly one key in `active` state. To rotate normally, add a new active key and change the former active key to `decrypt_only`, restart, and retain the old key only for the bounded grace period needed by outstanding envelopes—never longer than the configured absolute session lifetime. Remove it after that grace period and restart again.
- If an envelope key may be compromised, remove it immediately instead of granting a grace period, install a new active key, restart the service, and require connector reauthorization because outstanding envelopes using the removed key become invalid. Rotate the sealed upstream InvenTree credentials as well if they may have been exposed.
- A startup failure about missing, duplicate, weak, or state-invalid keys is fail-closed. Correct the protected configuration rather than weakening validation.

## Packaged Systemd Deployment

- Required inputs: release package for the target Linux distribution, private HTTP listen address, path-preserving public reverse-proxy route, `INVENTREE_URL`, OAuth issuer/resource URLs, trusted proxy CIDRs, envelope keys, allowed client IDs, and token lifetimes.
- Preferred flow: install the `deb`, `rpm`, or `apk` artifact from the GitHub release, edit `/etc/inventree-mcp/inventree-mcp.env`, and keep `INVENTREE_MCP_LISTEN` bound to loopback or a private service network. Do not enable the service for live connector use until F-S10 is complete; during that validation, enable it only after canonical reverse-proxy routing has been verified. The unit waits for native readiness after runtime checks and listener binding, then expects watchdog heartbeats every half of its configured 30-second timeout.
- Clarify when: the operator expects STDIO mode from the packaged service, wants to expose the Go listener directly to the internet, has not validated canonical proxy routing, or expects Alpine/OpenRC service management from the `apk` package.
- Expected output: installed package files now, and a systemd-managed `inventree-mcp serve --transport http` process behind the deployment's validated path-preserving reverse proxy once F-S10 packaged live deployment validation is complete. After the managed HTTP lifecycle starts, `systemctl status inventree-mcp.service` reports sanitized startup, ready, degraded, stopping, or fatal text. Earlier configuration or logger initialization failures exit non-zero and are visible through ordinary systemd status/journal and restart handling. A missed heartbeat does not make the process self-exit: the server stops heartbeats, continues serving, and systemd terminates it when the watchdog expires before restarting it under `Restart=on-failure`. Development-only smoke tests can still run the binary directly with `--environment development --dev-incomplete-oauth` and expect only the skeleton MCP server plus read-only health/version tool; outside systemd, notification and watchdog behavior is a no-op.

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
- HTTP note: write tools require OAuth authorization mode and the `inventree.write` scope before handler dispatch.

## Add Or Update Part Parameters

- Required inputs: part ID, requested parameter names/values, units where relevant.
- Preferred lookup order: `search_parameter_templates`, existing `get_part_parameters`, category parameter links, then update existing values or create new values against unambiguous existing templates.
- Clarify when: same-name linked templates differ by unit/choices/checkbox settings, only global/unlinked matches exist, or creating a new template/category link would be required. The milestone tool reports unlinked/global matches as context but does not write them.
- Tool sequence: `search_parameter_templates`, `get_part_parameters`, `set_part_parameters`.
- Expected output: parameter IDs updated/created and any unresolved parameter questions.

## Review Or Delete A Part Parameter Value

- Required inputs: one or more exact review filters (`template_id` or an unambiguous exact `template_name`, `value`, `category_id`, or `part_id`). Deletion additionally requires the stable `parameter_id` returned by search.
- Preferred flow: call `search_part_parameters`, review the returned part, category, template, value, and parameter-row IDs, then call `delete_part_parameter` without confirmation to obtain the exact deletion candidate. Retry the same single row with `confirm:true` only after review.
- Clarify when: no narrowing filter is supplied, an exact template name identifies zero or multiple templates, a template ID conflicts with the supplied name, the requested offset/limit window exceeds 1,000 rows, complete row-ID ordering cannot be established within the 1,000-row scan bound, or the request describes a whole-template or bulk deletion instead of one stable parameter row.
- Expected output: deterministic paginated search results; confirmed deletion returns the deleted row snapshot and `verified:true` only after a detail read proves the row no longer exists.
- HTTP note: search requires `inventree.read`; deletion requires `inventree.read`, `inventree.write`, and `inventree.destructive`.

## Create, Update, Delete, Or Merge Parameter Templates

- Create: search first, then call `create_parameter_template` only with explicit `name`, `units`, `description`, `model_type`, `checkbox`, `choices`, and `enabled`. Use empty strings intentionally for unitless, unrestricted, no-description, or free-form templates. Nonempty `model_type` must be one of `build.build`, `company.company`, `company.manufacturerpart`, `company.supplierpart`, `order.purchaseorder`, `order.returnorder`, `order.salesorder`, `order.salesordershipment`, `order.transferorder`, `part.part`, `part.partcategory`, or `stock.stocklocation`. A case-insensitive same-name collision always requires choosing or reconciling an existing template.
- Update: call `update_parameter_template` with a stable `template_id` and only the fields that should change. Omitted fields remain unchanged; empty strings and false are explicit replacements. Use `clear_selection_list:true` to write null and never combine it with `selection_list_id`.
- Delete: call `delete_parameter_template` without confirmation to review the template and reference IDs. Direct deletion is available only when no parameter row or category-default link remains; then repeat with `confirm:true`. The tool never cascades references.
- Merge: call `merge_parameter_templates` with distinct stable source/target IDs and `dry_run:true`. Optionally provide an exact `value_map`; all unmapped values remain unchanged. Review every planned/skipped row, category-link ID, and residual decision, then submit the unchanged inputs with `confirm:true` and the returned `plan_hash`.
- Conflict policy: when a part already has source and target rows, neither value is overwritten and the source row remains for explicit manual resolution. Non-part rows are reported with their actual `model_type` and `model_id`. Category-default references include both link and category IDs; use the category-default tools below to remove or migrate them, then obtain a fresh merge plan. Non-conflicting part rows can still move, but the source template is deleted only after a fresh read proves it has zero rows and zero category links.
- Recovery: treat confirmed merge/delete as single-writer operations and prevent concurrent template/reference administration. If merge returns `partial_failure`, do not replay the old confirmation: inspect every applied/failed action and current source/target row, discard the old hash, fix any manual decision, and run `dry_run:true` again. The upstream REST sequence is not transactional, so a narrow reference-creation race remains between the final preflight and delete.
- HTTP note: create/update require `inventree.read` and `inventree.write`; delete/merge additionally require `inventree.destructive`.

## Manage Category Parameter Defaults

- Review: call `search_category_parameter_defaults` with `category_id`. Exact-category links are returned by default. Add `include_parent_defaults:true` only to review the effective set inherited from ancestors; each result reports the requested category, actual source category, `inherited`, stable `link_id`, template identity, and default value.
- Create: reuse an existing template and call `create_category_parameter_default` with stable `category_id`, `template_id`, and an explicit default (empty is allowed). The tool never creates templates implicitly and refuses a duplicate direct category/template pair.
- Update: call `update_category_parameter_default` with the direct `link_id` and only fields that should change. An inherited result belongs to its reported source category; updating its link changes that source default for every descendant that inherits it.
- Delete: call `delete_category_parameter_default` without confirmation to review the direct source link, then retry that same `link_id` with `confirm:true`. Success is reported only after a detail read returns not found.
- HTTP note: search requires `inventree.read`; create/update require `inventree.read` and `inventree.write`; delete additionally requires `inventree.destructive`.

## Audit Or Bulk-Propagate Part Parameters

- Audit: call `audit_parameter_consistency` without filters only when the combined upstream requests and returned records fit its 1,000-unit safety budget. Narrow by one existing `template_id` or exact `category_id` for larger inventories. A template filter includes same-normalized-name peers; a category filter selects exact-category parts and direct links before row scanning. Review duplicate normalized names, incompatible units/choices/checkbox/selection-list/model-type definitions, duplicate rows or defaults, overloaded same-name fields, unlinked parameter usage, and non-empty category-default mismatches. The audit never writes or performs cleanup.
- Select propagation scope: choose one enabled unrestricted or `part.part` template, an explicit value (including explicit empty), and exactly one selector: at most 100 stable `part_ids`, or one `category_id`. Category selection is exact unless `include_subcategories:true` is explicit.
- Safety policy: the tool never creates templates or category links and never deletes parameter rows. Missing values are planned for creation. Equal existing values are skipped. Differing values are reported as `manual_required` unless `overwrite_existing:true`; duplicate rows always require manual resolution.
- Execute: call `bulk_propagate_part_parameters` with `dry_run:true` and review every ordered action. Preserve the template, value, selector, descendant, and overwrite fields; set `dry_run:false` or omit it, then add `confirm:true` and the returned `plan_hash` within five minutes from the same principal. A newer matching dry run supersedes the prior token, and tokens are single-use and process-local.
- Verification and recovery: treat execution as a single-writer operation. Every action's reviewed before-state is checked again immediately before mutation, and every applied create/update is read back. Execution stops after the first drift, write, identity, or verification failure and marks remaining planned actions as manually required. Do not replay the old plan; search current rows by stable part/template IDs, resolve any ambiguous result, and prepare a fresh dry run only for the remainder. InvenTree does not expose an atomic compare-and-set across the final check and write, so a narrow concurrent-administrator race remains.
- HTTP note: audit requires `inventree.read`; propagation is conservatively classified as destructive because `overwrite_existing:true` can replace data, so it requires `inventree.read`, `inventree.write`, and `inventree.destructive` even for create-only plans.

## Create Initial Stock

- Required inputs: part ID, stock location ID, quantity, status when required by local convention.
- Preferred lookup order: `get_part`, `search_stock_locations`, `search_stock_items` for duplicate detection.
- Clarify when: location is ambiguous, quantity/status is unclear, or existing stock at the same location may duplicate the requested initial stock.
- Tool sequence: use `create_initial_stock_entry` with `dry_run:true` when the operator wants a single workflow-level plan, then retry without `dry_run` after reviewing the duplicate preflight. Use `search_parts` or `get_part`, `search_stock_locations`, `search_stock_items`, then `create_stock_item` when the operator needs step-by-step control.
- Expected output: `status`, `dry_run`, ordered `actions`, selected part and location records, and the created stock item record when executed, or a structured duplicate clarification with candidate stock item IDs and retry values. In `dry_run` responses, the planned stock create appears in `actions` because the stock item has no stable ID yet.

## Attach Datasheet Or Photo

- Required inputs: target object type and ID plus exactly one upload source. Inline uploads require filename and content type; local-file uploads require content type and may derive filename from the path; URL-copy uploads may derive filename and content type from the HTTP response; stored links require only the target URL, with any supplied filename used only for duplicate preflight because InvenTree assigns stored-link filename metadata.
- Accepted sources: inline bytes in any mode; STDIO allowlisted local path; HTTP(S) URL only through `upload_attachment_from_url`; stored link only through `create_link_attachment`.
- Source resolver behavior: inline bytes are size-capped before upload, STDIO local paths must sit under trusted operator-controlled allowlisted roots and are rejected in HTTP mode before filesystem access, and URL-copy sources must pass SSRF checks without forwarding MCP or InvenTree auth headers.
- Clarify when: target object is ambiguous, URL intent could mean upload-copy or store-link, duplicate filename/content/link exists, or source policy rejects the input.
- Tool sequence: `list_attachments`, then `upload_attachment`, `upload_attachment_from_url`, or `create_link_attachment`. Use `allow_duplicate:true` only after reviewing duplicate candidates and deciding a new matching attachment is intentional.
- Expected output: attachment ID, target object, filename, size or link classification, content type, source kind, and thumbnail/image state when available.
- HTTP note: attachment write tools require OAuth authorization mode plus `inventree.write` and `inventree.upload` before handler dispatch.

## Update Or Delete Attachment Metadata

- Required inputs: stable attachment ID. Deletion also requires `confirm:true`.
- Preferred lookup order: `get_attachment_metadata`, then `update_attachment_metadata` or `delete_attachment`.
- Clarify when: attachment ID is missing, the existing attachment belongs to an out-of-scope object type, no metadata fields are supplied for PATCH, or delete confirmation is missing.
- Expected output: updated or deleted attachment metadata with target object details.
- HTTP note: attachment write and destructive tools require OAuth authorization mode plus their declared write/upload/destructive scopes before handler dispatch.

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
- Tool sequence: `list_attachments`, optionally upload an image through `upload_attachment` or `upload_attachment_from_url`, then `set_primary_image`.
- Expected output: selected attachment/image ID, redacted resulting image URL, and replacement confirmation status.

## Preview Purchase Order Lines

- Required inputs: supplier ID or supplier part IDs, quantities, and any known pricing/currency.
- Preferred lookup order: search supplier, search supplier parts for requested part IDs, validate that each line resolves to exactly one supplier-part link for a single supplier, then produce a no-write preview.
- Clarify when: supplier part is ambiguous, a supplier-part ID conflicts with the requested supplier or part, quantity is missing or non-positive, or price/currency is missing and required for the operator's decision.
- Tool sequence: `search_suppliers`, `search_parts`, then `preview_purchase_order_with_lines`. Provide `supplier_part_id` when known; otherwise provide `supplier_id`, `part_id`, and optional `supplier_sku` so the preview can validate that exactly one supplier-part link matches.
- Expected output: proposed lines, supplier part IDs, optional line totals when price and currency are supplied, warnings for omitted preview-only pricing, and confirmation by tool class that no purchase order was created. The tool does not create purchase orders or purchase-order lines.

## Create Purchase Order From Order Page

- Required inputs: stable supplier ID, a stable supplier reference or external order identifier, and at least one validated line with a supplier-part ID and quantity. Description, dates, unit prices, and currencies are optional, except that currency is required when a unit price is supplied.
- Preferred lookup order: search supplier, search categories and parts for missing catalog entries, search supplier/manufacturer links for duplicates, run purchase-line preview validation, use `search_purchase_orders` and `search_purchase_order_lines` for duplicate/recovery checks, then call `create_purchase_order_with_lines` with `dry_run:true` before execution.
- Clarify when: category, part, supplier part, manufacturer part, parameter template, image, purchase-order identity, or blank manufacturer part number handling is ambiguous.
- Expected output: dry-run actions before writes; on execution, a stable purchase-order ID, its generated InvenTree reference, stable line IDs, retry-recoverable create/update results, and structured partial-failure output with completed actions and a concrete recovery plan. The exact `(supplier_id, supplier_reference)` pair is the retry identity; InvenTree generates the internal purchase-order `reference`, derived line references use `<supplier_reference>-<one-based index>`, and retries do not delete extra existing lines. If more than one order has the exact pair, the workflow asks which order to reuse. The MCP annotation remains `idempotentHint:false` because concurrent creators can race across the non-atomic lookup/create boundary.
- Duplicate-line recovery: if existing lines already share one derived workflow reference, use the returned candidates with `update_purchase_order_line` to give one duplicate a unique reference, then retry the combined workflow. No order or line PATCH is performed before all existing-line conflicts pass preflight.
- After creation, keep placement and receipt separate: dry-run `issue_purchase_order`, then call it with `confirm_issue:true` only when the order is ready to be placed with the supplier.
- Receiving: call `receive_purchase_order_items` with `dry_run:true`, stable line IDs, schema-valid partial quantities no greater than each outstanding quantity, and item or global locations only when line destinations do not suffice. Review the resolved location and outstanding-before/after values, then submit the exact returned `plan_hash` with `confirm_receive:true`. A changed order or line state requires a new dry run.
- Receiving creates new stock items through InvenTree's native PO receive endpoint. It rejects virtual-part lines, does not merge into or update existing stock, and never auto-issues a pending order.
- Omitted, empty, or whitespace-only receipt packaging uses the supplier-part packaging; provide a non-blank override to change the created stock item's packaging.
- If receipt execution returns `partial_failure`, do not retry blindly. Read every purchase-order line and call `search_stock_items` with the order's `purchase_order_id`; use the returned source-order fields to determine whether the first mutation succeeded, then prepare a new dry-run plan only for any confirmed remainder.
- Do not have multiple operators or integrations receive the same purchase-order line concurrently. InvenTree 1.4.3 serializes line updates but can still accept a previously prepared quantity after the outstanding amount changes; this narrow race is an accepted operational limitation.
- Remaining gap: broader live-order-entry recovery surfaces remain tracked separately in F-S15.
- One-shot option: `create_purchase_order` may omit `supplier_reference`, in which case InvenTree still generates the internal reference. Use that lower-level tool only for intentional one-shot creation; recovery after a client or tool interruption is provided by `create_purchase_order_with_lines` and requires a stable supplier reference.

## Resolve Structured Clarification Prompts

- Required inputs: the stable retry field requested by the prior tool response.
- Preferred flow: show the exact `question`, candidate IDs/URLs, and retry field to the operator. Retry the original tool with the selected stable ID unless `retry_tool` names an explicit destination tool; in that case pass the supplied `retry_values` and selected retry field to that tool.
- Clarify when: the operator chooses a free-form value that still does not identify a stable record.
- Expected output: successful retry or a narrower clarification response.

## Review And Apply A Stocktake Adjustment

- Required inputs: one stable stock-item ID, a nonblank audit reason, and exactly one intended operation: relative `delta`, absolute `observed_quantity`, or target `status`.
- Preferred flow: fetch `stocktake_review`; call the selected tool with `dry_run:true`; review current and proposed quantity, location, status, batch, serial, packaging, `delete_on_deplete`, and any high-risk warning; then submit the returned opaque `plan_hash` token with `confirm:true` within five minutes from the same principal. The token is single-use; a newer dry run for the same action and item supersedes it, and a server restart invalidates it. If outstanding-plan capacity is reached, execute or wait for reviewed tokens to expire before preparing more plans.
- Clarify when: stock identity, measured quantity, relative-versus-absolute intent, target status, or audit reason is missing or ambiguous. Never combine a quantity-only stocktake with an implicit location, status, batch, serial, or packaging change.
- Tool selection: use `adjust_stock_quantity` for relative add/remove corrections, `stocktake_adjustment` for an absolute physical count, and `set_stock_status` only for status. Quantity decreases and `Destroyed`, `Rejected`, or `Lost` transitions are high-risk. No-op changes are refused. Relative and absolute quantity changes are refused for serialized stock; change individual serialized items instead, while status-only changes remain supported. If `delete_on_deplete` is true, a target quantity of zero is refused because it would implicitly delete the stock item.
- Expected output: a current-state-bound before/after plan during dry run and a refreshed stock item after execution. Do not allow another operator or integration to adjust the same stock item concurrently. Execution rejects state changes seen during preflight, but InvenTree provides no atomic conditional mutation for the narrow interval before the write. If state changes or execution returns `partial_failure`, do not retry blindly; run a new dry run for the same stable stock-item ID to inspect current state first.

## Use Prompt Checklists

- Required inputs: one of the registered prompt names: `new_part_entry_checklist`, `parameter_reuse_checklist`, `attachment_image_checklist`, `initial_stock_entry_checklist`, `purchase_preview_checklist`, `receive_purchase_order_checklist`, or `stocktake_review`.
- Preferred flow: fetch the checklist before starting the workflow, run the listed searches or dry-run planner, show any structured clarification to the operator, and retry with the requested stable IDs.
- Clarify when: the checklist exposes missing required fields, conflicting supplier/part identity, ambiguous parameter templates, duplicate stock, duplicate attachments, unclear upload/link intent, primary-image replacement without `confirm:true`, or purchase preview lines that do not resolve to exactly one supplier-part link.
- Expected output: a stable-ID retry request, a dry-run plan for write-capable workflows, a no-write purchase preview, or a structured clarification object. The future prompt `bom_import_review` is not exposed until its workflow is implemented.
