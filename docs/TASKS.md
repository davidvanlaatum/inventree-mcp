# Implementation Tasks

This backlog turns [PLAN.md](PLAN.md) into executable work. Status values are:

- `Done`: acceptance criteria met, validation/review recorded, and committed; when a task-status update is part of the current change, `Done` means ready for the same commit.
- `Ready`: actionable with current information.
- `Blocked`: needs an explicit decision, external verification, or prerequisite task.
- `Planned`: valid work, but should wait for dependencies.
- `Future`: outside the first beta milestone.

Each story should be completed with tests, documentation updates, and reviewer follow-up. Code, behavior, task-status, operator workflow, or public documentation-contract changes require subagent review from the applicable roles in [reviewers.md](reviewers.md). Use the full Go, QA, product, and infosec panel when acceptance criteria touch auth, upload, Testcontainers, tool-surface behavior, or milestone completion. Manual-only review is reserved for typo-only or formatting-only documentation edits and must say why subagent review was not required.

Before marking a story `Done`, add or update story-local completion notes:

- `Validation`: commands/checks run, or why a check was not applicable.
- `Review`: reviewer roles run, findings addressed, or why subagent review was not required for a typo-only or formatting-only documentation edit.
- `Residual risk`: accepted unresolved risk, or `none`.

When updating an already-pushed branch or existing PR, prefer fresh follow-up commits over amending or force-pushing. Rewrite published history only for an explicit operator request or a concrete repository hygiene issue, and use `--force-with-lease` when a rewrite is unavoidable. Prefer squash merge when merging PRs unless the operator or repository policy requires another strategy.

Before `M1C-S04` is complete, mutating, operational, destructive, and upload tools may be registered only on STDIO or in unit-test registries. HTTP registration must filter them out of the exposed tool manifest until per-tool scope enforcement is implemented and tested.

## Milestone 0: Repository And Planning

### M0-S01: Initialize Repository Scaffold

- Status: `Done`
- Depends on: none
- Scope: create repository baseline, docs, schema snapshot, GitHub remote, and initial commits.
- Validation: committed in prior repository setup changes.
- Review: covered by earlier planning review passes.
- Residual risk: none.
- Acceptance:
  - Git repository exists with clean `main`.
  - `origin` remote points at `git@github.com:davidvanlaatum/inventree-mcp.git`.
  - Planning docs live under `docs/`.

Tasks:

- [x] Initialize git repository.
- [x] Add `.gitignore`.
- [x] Move API schema under `docs/`.
- [x] Add `docs/PLAN.md`, `docs/api-schema.md`, `docs/tool-reference.md`, and `docs/operator-recipes.md`.
- [x] Add reviewer roster in `docs/reviewers.md`.
- [x] Add GitHub remote.

### M0-S02: Add Project Automation

- Status: `Done`
- Depends on: M0-S01
- Scope: add minimal Go module, GitHub Actions, Dependabot, golangci-lint, and pre-commit.
- Validation: `go test ./...` passed with workspace-local Go build cache before commit.
- Review: covered by earlier planning review passes.
- Residual risk: none.
- Acceptance:
  - `go test ./...` passes with an allowed Go build cache.
  - GitHub Actions workflows exist for tests, lint, and dependency submission.
  - Pre-commit config matches the intended Go quality gate.

Tasks:

- [x] Add `go.mod`.
- [x] Add minimal root package stub.
- [x] Add `.pre-commit-config.yaml`.
- [x] Add `.golangci.yml`.
- [x] Add GitHub Actions workflows.
- [x] Add Dependabot config.
- [x] Update README and agent instructions.

### M0-S03: First-Beta Documentation Contracts

- Status: `Done`
- Depends on: M0-S01
- Scope: create first-beta tool reference and operator recipe skeletons before implementation so workflow behavior does not drift.
- Validation: documentation reviewed in prior planning review pass.
- Review: product review requested concrete tool-reference and operator-recipe skeletons; feedback incorporated.
- Residual risk: docs will still need generated-manifest reconciliation as tools are implemented.
- Acceptance:
  - `docs/tool-reference.md` lists milestone 1 tools, scopes, mutation class, upload sources, and operator clarification guidance.
  - `docs/operator-recipes.md` includes first-release recipe skeletons for setup, part entry, parameter reuse, stock, attachments/images, purchase preview, and clarification handling.
  - Later implementation tasks keep these docs aligned.

Tasks:

- [x] Add milestone 1 planned-tool catalog.
- [x] Add first-release operator recipe skeletons.
- [x] Link docs from README.

## Milestone 1A: Buildable Skeleton

### M1A-S01: Command And Config Skeleton

- Status: `Done`
- Depends on: M0-S02
- Scope: add the first buildable `inventree-mcp` command with typed config.
- Validation: `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache go test ./...` passed; `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache go build ./cmd/inventree-mcp` passed; `git diff --check` passed. Initial plain `go test ./...` failed because the default macOS Go build cache is outside the writable sandbox.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager subagent reviews run. Go review found STDIO `serve` wrote a success banner to stdout; fixed by keeping successful `serve` silent and adding a regression test. QA and product findings on missing durable subagent review evidence were addressed in this note and the workflow wording updates. No unresolved actionable findings.
- Residual risk: HTTP command still only validates development-mode config until `M1C` defines final OAuth config and server behavior.
- Acceptance:
  - `cmd/inventree-mcp` builds.
  - `inventree-mcp serve --transport stdio` and `--transport http` parse config and fail gracefully for missing required values.
  - Production HTTP mode rejects upstream TLS skip-verify.
  - Configured InvenTree token/scheme credentials are STDIO-only until HTTP OAuth is complete.
  - Production HTTP mode is disabled until OAuth is implemented unless an explicit development-only incomplete-OAuth flag is set.
  - The skeleton must not invent final OAuth config shape; `M1C` owns real OAuth config validation.
  - Tests cover env/flag precedence and invalid config.

Tasks:

- [x] Add `cmd/inventree-mcp/main.go`.
- [x] Add `internal/config`.
- [x] Define transport, listen/path, STDIO InvenTree URL/token/scheme, timeout, TLS, and logging config.
- [x] Add HTTP config validation that blocks production HTTP mode until OAuth tasks define the final config.
- [x] Add config validation tests.
- [x] Update README quick start.

### M1A-S02: Logging, Clock, IDs, And Randomness

- Status: `Planned`
- Depends on: M1A-S01
- Scope: add deterministic platform seams and context logging.
- Acceptance:
  - Root contexts are seeded with `dvgoutils/logging.WithLogger`.
  - Request/tool scoped loggers are derived and reattached to context.
  - Clock, ID, and randomness are injectable where needed.
  - Tests use `dvgoutils/logging/testhandler.SetupTestHandler`.

Tasks:

- [ ] Add logging setup in command/server construction.
- [ ] Add `internal/platform` clock and ID/randomness seams.
- [ ] Add log redaction conventions.
- [ ] Add tests proving scoped attributes survive through context.

### M1A-S03: MCP Server Skeleton

- Status: `Planned`
- Depends on: M1A-S01, M1A-S02
- Scope: create server construction, STDIO transport, HTTP transport, and a health/version tool.
- Acceptance:
  - STDIO server can initialize and list tools.
  - HTTP streamable server runs stateless.
  - Health/version tool is read-only.
  - Tool annotation helper tests cover SDK `v1.6.1` pointer false behavior for `destructiveHint` and `openWorldHint`.

Tasks:

- [ ] Add `internal/server`.
- [ ] Add `internal/tools` registration entrypoint.
- [ ] Register health/version tool.
- [ ] Wire `mcp.StdioTransport`.
- [ ] Wire `mcp.NewStreamableHTTPHandler` with stateless options.
- [ ] Add tool annotation helpers and tests.

## Milestone 1B: InvenTree Client Foundation

### M1B-S01: REST Client Core

- Status: `Planned`
- Depends on: M1A-S01
- Scope: implement low-level InvenTree HTTP client with auth, pagination, errors, and PATCH helpers.
- Acceptance:
  - Supports `Authorization: Token ...` and `Authorization: Bearer ...`.
  - Pagination helpers are covered by tests.
  - PATCH serialization preserves omitted fields versus explicit zero/false/empty/null.
  - Error mapping normalizes common InvenTree API failures.

Tasks:

- [ ] Add `internal/inventree` client.
- [ ] Add auth header model.
- [ ] Add pagination helpers.
- [ ] Add error mapping.
- [ ] Add PATCH helper and zero-value tests.

### M1B-S02: Schema Endpoint Manifest

- Status: `Planned`
- Depends on: M1B-S01
- Scope: add a generated or maintained manifest tying implemented endpoints to `docs/api-schema.yaml`.
- Acceptance:
  - Implemented client methods reference schema-known path/method/request/response data.
  - Schema drift checks require `docs/api-schema.md` provenance updates.
  - Attachment and parameter endpoint capability tables remain authoritative.
  - Attachment/image manifest checks reject deferred app-specific file surfaces such as notes image upload, generated report attachments, and stock test-result attachments unless a later task explicitly brings them into scope.

Tasks:

- [ ] Add schema parsing/check helper.
- [ ] Add endpoint manifest format.
- [ ] Add docs drift check.
- [ ] Cover parts, categories, companies, stock, parameters, attachments, and purchasing preview dependencies.

### M1B-S03: Read-Only Client Methods

- Status: `Planned`
- Depends on: M1B-S01, M1B-S02
- Scope: implement read-only API methods needed by milestone 1.
- Acceptance:
  - Methods exist for part, category, company, stock location/item, parameter, attachment, and supplier-part lookup.
  - Tests use fake transports, not live network.

Tasks:

- [ ] Add part/category lookup methods.
- [ ] Add company/supplier/manufacturer lookup methods.
- [ ] Add stock location/item lookup methods.
- [ ] Add parameter template/value lookup methods.
- [ ] Add attachment metadata/list/download methods.
- [ ] Add supplier-part lookup methods for purchase preview.

## Milestone 1C: HTTP OAuth Spike And Auth Layer

### M1C-S01: MCP SDK Auth Spike

- Status: `Planned`
- Depends on: M1A-S03
- Scope: prove official MCP SDK `auth`/`oauthex` behavior against the planned HTTP architecture.
- Acceptance:
  - Uses reviewed SDK baseline `v1.6.1` or records upgrade findings.
  - Confirms `auth.RequireBearerToken`, `auth.RequireBearerTokenOptions`, `auth.TokenVerifier`, and `auth.TokenInfoFromContext` behavior.
  - Proves token info or selected credential carrier reaches tool handlers under stateless HTTP.
  - Updates plan if SDK API assumptions are wrong.

Tasks:

- [ ] Add spike tests around HTTP handler auth middleware.
- [ ] Verify `TokenVerifier` signature.
- [ ] Verify context propagation into `CallTool`.
- [ ] Document results in `docs/PLAN.md`.

### M1C-S02: ChatGPT Connector Compatibility Spike

- Status: `Blocked`
- Depends on: M1C-S01
- Blocker: requires current official OpenAI connector/OAuth documentation verification.
- Scope: confirm redirect URI, metadata, client registration, local/dev callback, and pre-auth discovery expectations.
- Acceptance:
  - Connector assumptions are documented with exact dates and source links.
  - HTTP OAuth implementation tasks are unblocked or revised.

Tasks:

- [ ] Verify current OpenAI connector OAuth docs.
- [ ] Record redirect URI shape and registration model.
- [ ] Record required metadata fields and scopes behavior.
- [ ] Decide whether unauthenticated static MCP discovery is required.

### M1C-S03: OAuth Envelope And Code Storage

- Status: `Planned`
- Depends on: M1C-S01, M1C-S02
- Scope: implement encrypted access/refresh envelopes and one-time authorization code storage.
- Acceptance:
  - Access token default lifetime is 15 minutes.
  - Refresh token default lifetime is 30 days.
  - Absolute connector session default is 90 days.
  - Authorization codes are one-time-use with bounded expiry.
  - Tokens are opaque and not plaintext JWT/JWS.

Tasks:

- [ ] Add `internal/oauth` envelope codec.
- [ ] Add keyring config and validation.
- [ ] Add one-time auth-code ID store.
- [ ] Add refresh flow.
- [ ] Add redaction tests.

### M1C-S04: Scope Guard And Credential Propagation

- Status: `Planned`
- Depends on: M1C-S03
- Scope: enforce per-tool OAuth scopes and request-scoped InvenTree credentials.
- Acceptance:
  - Global bearer auth only authenticates and populates context.
  - Tool-specific guard checks manifest before handler dispatch.
  - Credential carrier is type-safe and not logged or serialized.

Tasks:

- [ ] Add tool authorization manifest.
- [ ] Add per-tool scope wrapper in `internal/tools` or `internal/server`.
- [ ] Add `internal/oauth.CredentialFromTokenInfo` or selected private carrier.
- [ ] Add scope rejection tests.
- [ ] Add concurrent credential isolation tests.

## Milestone 1D: Discovery Tools

### M1D-S01: Lookup Tool Framework

- Status: `Planned`
- Depends on: M1A-S03, M1B-S03
- Scope: implement common tool schemas, structured outputs, ambiguity responses, and fake-client handler tests.
- Acceptance:
  - Tool handlers depend on interfaces, not concrete HTTP clients.
  - Ambiguous lookup returns structured clarification with candidates and stable retry fields.
  - Tool schemas are documented in `docs/tool-reference.md`.

Tasks:

- [ ] Add common tool dependency struct.
- [ ] Add structured clarification response type.
- [ ] Add fake-client test helpers.
- [ ] Add docs generation or drift check.

### M1D-S02: Part, Company, Stock, Parameter, And Attachment Lookup Tools

- Status: `Planned`
- Depends on: M1D-S01
- Scope: add read-only milestone lookup tools.
- Acceptance:
  - Implements milestone 1 read-only tools in `docs/tool-reference.md`.
  - Read-only annotations and `inventree.read` scopes are correct.
  - Tests cover ambiguous and no-result behavior.

Tasks:

- [ ] Add part/category lookup tools.
- [ ] Add company/supplier/manufacturer lookup tools.
- [ ] Add stock location/item lookup tools.
- [ ] Add parameter template/part parameter lookup tools.
- [ ] Add attachment list/metadata/download tools.

## Milestone 1E: Basic Write Tools

### M1E-S01: Part And Company Writes

- Status: `Planned`
- Depends on: M1B-S01, M1D-S02
- Scope: create/update parts and create supplier/manufacturer companies or links.
- Acceptance:
  - PATCH is used where schema supports it.
  - Existing companies/categories are preferred over creating new records.
  - No customer-role defaults are introduced.
  - Tool registration includes `inventree.write` scope tests.
  - HTTP registration is disabled or rejected until `M1C-S04` per-tool scope enforcement is complete.
  - Infosec review has no unresolved actionable findings before any mutating HTTP tool is exposed.

Tasks:

- [ ] Add `create_part`.
- [ ] Add `update_part`.
- [ ] Add `create_company`.
- [ ] Add `create_supplier_part`.
- [ ] Add `create_manufacturer_part`.
- [ ] Add sales/customer boundary tests.

### M1E-S02: Parameter Writes

- Status: `Planned`
- Depends on: M1D-S02
- Scope: set part parameters using existing templates only for milestone 1.
- Acceptance:
  - Searches templates, existing parameters, and category parameter links before writing.
  - Ambiguous template match asks the operator.
  - New template/category-link creation is refused unless a later explicit workflow is added.
  - Tool registration includes `inventree.write` scope tests.
  - HTTP registration is disabled or rejected until `M1C-S04` per-tool scope enforcement is complete.

Tasks:

- [ ] Add parameter match logic.
- [ ] Add `set_part_parameters`.
- [ ] Add tests for disabled templates and same-name templates with different units/choices.
- [ ] Add explicit empty/false/zero value tests.

### M1E-S03: Initial Stock Writes

- Status: `Planned`
- Depends on: M1D-S02
- Scope: create initial stock item with duplicate detection.
- Acceptance:
  - Requires `inventree.operational` plus write scope.
  - Searches existing stock before creation.
  - Potential duplicate returns structured clarification.
  - HTTP registration is disabled or rejected until `M1C-S04` per-tool scope enforcement is complete.
  - Infosec review has no unresolved actionable findings before any operational HTTP tool is exposed.

Tasks:

- [ ] Add `create_stock_item`.
- [ ] Add duplicate detection.
- [ ] Add operational scope tests.

## Milestone 1F: Uploads, Attachments, And Images

### M1F-S01: Upload Source Resolver

- Status: `Planned`
- Depends on: M1A-S02
- Scope: implement inline bytes, STDIO allowlisted local paths, and URL fetch source handling.
- Acceptance:
  - HTTP mode rejects local paths before filesystem open/stat.
  - STDIO local path logic uses direct Afero in `internal/upload/local_file.go`.
  - URL fetcher enforces SSRF controls and never forwards auth headers.
  - Inline byte uploads, local file reads, and URL fetches enforce configured maximum sizes and bounded read time.
  - Redaction tests using `dvgoutils/logging/testhandler` prove auth tokens, uploaded bytes, sensitive local paths, and URLs with query secrets are not logged.
  - Infosec review has no unresolved actionable findings before URL or local-file upload sources are enabled.

Tasks:

- [ ] Add inline byte source resolver.
- [ ] Add STDIO local file source resolver with Afero.
- [ ] Add URL fetcher interface and policy.
- [ ] Add maximum-size and bounded-read enforcement.
- [ ] Add upload redaction tests.
- [ ] Add SSRF bypass table tests.
- [ ] Add local path canonicalization and symlink tests.

### M1F-S02: Attachment Tools

- Status: `Planned`
- Depends on: M1B-S02, M1F-S01
- Scope: implement list/get/download/upload-url/link/update/delete attachment behavior for milestone object types.
- Acceptance:
  - `download_attachment` is read-only, requires `inventree.read`, and only downloads schema-supported file or thumbnail URLs belonging to the configured InvenTree instance.
  - `download_attachment` resolves attachment metadata first and rejects out-of-scope `model_type` values before fetching content.
  - `download_attachment` defaults to the original file URL and uses thumbnail URLs only in explicit thumbnail mode.
  - `download_attachment` returns filename, content type when known, size, SHA-256 hash, selected download mode, and base64 content for binary files or text for allowlisted textual content types.
  - Download limits enforce maximum size and bounded read time, redirects are blocked or revalidated against the configured InvenTree instance, and redaction tests prove downloaded bytes and sensitive URLs are not logged.
  - `upload_attachment` accepts inline bytes and STDIO allowlisted paths only.
  - `upload_attachment_from_url` is the only URL-fetch upload tool and has `openWorldHint:true`.
  - `create_link_attachment` stores links without fetching.
  - Duplicate attachment behavior returns structured clarification unless intent is explicit.
  - Tool registration includes `inventree.upload` scope tests and `inventree.destructive` tests for delete.
  - HTTP registration is disabled or rejected until `M1C-S04` per-tool scope enforcement is complete.
  - Infosec review has no unresolved actionable findings before upload tools are exposed over HTTP.

Tasks:

- [ ] Add attachment client methods.
- [ ] Add `download_attachment`.
- [ ] Add `upload_attachment`.
- [ ] Add `upload_attachment_from_url`.
- [ ] Add `create_link_attachment`.
- [ ] Add `update_attachment_metadata`.
- [ ] Add `delete_attachment` behind `confirm:true`.

### M1F-S03: Primary Part Image

- Status: `Planned`
- Depends on: M1F-S02
- Scope: implement part primary image download and assignment/replacement.
- Acceptance:
  - Milestone primary image scope is part only.
  - `download_part_image` is read-only, requires `inventree.read`, and downloads only the schema-exposed readable primary image URL or explicit thumbnail endpoint for the requested part.
  - `download_part_image` never treats write-only `existing_image` as a download source.
  - `download_part_image` returns filename when known, content type when known, size, SHA-256 hash, selected download mode, and base64 content.
  - Primary image downloads enforce maximum size, bounded read time, configured InvenTree instance URL restrictions, and redaction.
  - Missing primary image returns a structured no-image result.
  - Replacement requires `confirm:true`.
  - Ambiguous image selection asks the operator.

Tasks:

- [ ] Verify part image endpoint behavior against `docs/api-schema.yaml`.
- [ ] Verify `/api/part/{id}/` image fields and `/api/part/thumbs/{id}/` behavior, and document which endpoint `set_primary_image` uses.
- [ ] Add part image download and update client methods.
- [ ] Add `download_part_image`.
- [ ] Add `set_primary_image`.
- [ ] Add no-image, present-image, too-large, URL-scope, first-assignment, and replacement tests.

## Milestone 1G: Workflow Tools And Prompts

### M1G-S01: Part Upsert Workflow

- Status: `Planned`
- Depends on: M1E-S01, M1E-S02
- Scope: safer multi-step workflow for adding/updating a purchasable part with supplier/manufacturer data.
- Acceptance:
  - Supports `dry_run`.
  - Prefers existing records.
  - Returns stable IDs and omitted recommended fields.

Tasks:

- [ ] Add workflow planner.
- [ ] Add `upsert_part_with_supplier_and_manufacturer`.
- [ ] Add dry-run no-write tests.

### M1G-S02: Initial Stock And Purchase Preview Workflows

- Status: `Planned`
- Depends on: M1D-S02, M1E-S03, M1G-S01
- Scope: finish the useful operator loop with initial stock and no-write purchase preview.
- Acceptance:
  - Purchase preview performs no writes.
  - Supplier-part validation is explicit.
  - Ambiguous supplier/part/quantity data asks the operator.

Tasks:

- [ ] Add `preview_purchase_order_with_lines`.
- [ ] Add initial stock workflow helper.
- [ ] Add purchase preview no-write tests.

### M1G-S03: Milestone Prompts

- Status: `Planned`
- Depends on: M1D-S01
- Scope: add milestone 1 prompts and prompt contract tests.
- Acceptance:
  - Prompts are marked `milestone_1`.
  - Future prompts remain hidden or marked future.
  - Prompts prefer clarification/dry-run over guessing.

Tasks:

- [ ] Add `new_part_entry_checklist`.
- [ ] Add `parameter_reuse_checklist`.
- [ ] Add `attachment_image_checklist`.
- [ ] Add `initial_stock_entry_checklist`.
- [ ] Add `purchase_preview_checklist`.
- [ ] Add prompt manifest tests.

## Milestone 1H: Integration Test Environment

### M1H-S01: Testcontainers Stack Spike

- Status: `Planned`
- Depends on: M1B-S01
- Scope: prove InvenTree startup, migrations, admin token creation, and readiness with Testcontainers.
- Acceptance:
  - Uses explicit InvenTree version tag matching schema snapshot.
  - Pinned InvenTree image tag is declared in testenv config or a single constant and appears in test logs.
  - `docs/api-schema.md` provenance records the matching runtime InvenTree version and API version.
  - Records runtime InvenTree version and API version.
  - `go test ./...` never starts Docker.

Tasks:

- [ ] Add `internal/testenv`.
- [ ] Choose and record the explicit InvenTree version tag matching `docs/api-schema.yaml`.
- [ ] Start required database and InvenTree services.
- [ ] Create deterministic admin/test token.
- [ ] Add readiness polling.
- [ ] Add integration tag and Docker skip behavior.

### M1H-S02: Shared Suite Fixtures And Isolation

- Status: `Planned`
- Depends on: M1H-S01
- Scope: add suite-owned container lifecycle, immutable fixtures, run prefixes, and cleanup safety.
- Acceptance:
  - Parent test acquires environment before parallel subtests.
  - Every mutating helper requires a `Run` object.
  - Cleanup refuses unprefixed or foreign-prefixed records.

Tasks:

- [ ] Add `SharedInvenTree`.
- [ ] Add immutable fixture seeding.
- [ ] Add `Run` prefix format `IT_<runid>_<pkg>_<test>_`.
- [ ] Add cleanup safety checks.
- [ ] Add parallel isolation tests.

### M1H-S03: Milestone Integration Happy Paths

- Status: `Planned`
- Depends on: M1H-S02, M1G-S02, M1F-S03
- Scope: prove catalog, stock, supplier/manufacturer, attachment, URL upload, link, image, and purchase preview flows.
- Acceptance:
  - Byte uploads are read back and hash-validated.
  - `download_attachment` original mode returns the uploaded fixture bytes with matching size/hash and rejects content outside configured limits.
  - `download_attachment` coverage includes the in-scope target-object matrix: `part`, `stockitem`, `company`, `supplierpart`, `manufacturerpart`, and existing `purchaseorder`.
  - `download_part_image` original mode returns the assigned primary part image bytes with matching size/hash, tests thumbnail mode separately, and rejects content outside configured limits.
  - URL upload uses local fixture server and does not forward auth headers.
  - Link attachments are stored without fetch.
  - Sales/customer workflows, notes image upload, report attachments, stock test-result attachments, and other deferred app-specific file surfaces remain absent.

Tasks:

- [ ] Add catalog and initial stock happy path.
- [ ] Add supplier/manufacturer purchase preview happy path.
- [ ] Add inline/local-path attachment readback tests.
- [ ] Add explicit `download_attachment` original-mode content, thumbnail-mode behavior, hash, size, out-of-scope model type, redirect, and limit tests.
- [ ] Add explicit `download_part_image` original-mode content, thumbnail-mode behavior, hash, size, no-image, write-only `existing_image` exclusion, and limit tests.
- [ ] Add URL upload readback tests.
- [ ] Add link attachment tests.
- [ ] Add primary image tests.
- [ ] Add sales/customer and deferred file-surface boundary integration test.

## Milestone 1I: Documentation And Release Readiness

### M1I-S01: Operator Docs Finalization

- Status: `Planned`
- Depends on: M1G-S03, M1F-S03
- Scope: finalize README links, operator recipes, and tool reference from implemented behavior.
- Acceptance:
  - README stays concise and links to recipes.
  - `docs/tool-reference.md` matches generated manifest.
  - `docs/operator-recipes.md` includes first-release workflows.

Tasks:

- [ ] Add generated or checked tool manifest.
- [ ] Update tool reference from manifest.
- [ ] Update operator recipes from implemented tools.
- [ ] Add README quick-start links.

### M1I-S02: Final Review Panel

- Status: `Planned`
- Depends on: all milestone 1 implementation stories
- Scope: run senior Go, QA, product, and infosec reviews before beta declaration.
- Acceptance:
  - Findings are either fixed or documented as accepted residual risk.
  - Blocking milestone tests pass.
  - No sales/customer workflows ship.

Tasks:

- [ ] Run senior Go review.
- [ ] Run senior QA review.
- [ ] Run senior product review.
- [ ] Run senior infosec review.
- [ ] Fix or document findings.

## Future Backlog

### F-S01: BOM Import Workflow

- Status: `Future`
- Depends on: milestone 1 complete and product review

Tasks:

- [ ] Define BOM import behavior.
- [ ] Implement structured row validation.
- [ ] Add dry-run and row-level error tests.

### F-S02: Purchase Order Write And Receiving

- Status: `Future`
- Depends on: milestone 1 complete and product review

Tasks:

- [ ] Define purchase order creation workflow.
- [ ] Define receiving workflow.
- [ ] Add operational/destructive scope review.

### F-S03: Build Order Workflows

- Status: `Future`
- Depends on: milestone 1 complete and product review

Tasks:

- [ ] Define build create/allocate/complete behavior.
- [ ] Add stock consumption safety model.
- [ ] Add integration tests.

### F-S04: Stocktake Adjustments

- Status: `Future`
- Depends on: milestone 1 complete and product review

Tasks:

- [ ] Define stocktake review workflow.
- [ ] Add confirmation and audit requirements.
- [ ] Add operational scope tests.
