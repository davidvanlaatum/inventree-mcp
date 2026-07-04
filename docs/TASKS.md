# Implementation Tasks

This backlog turns [PLAN.md](PLAN.md) into executable work. Status values are:

- `Done`: acceptance criteria met, validation/review recorded, and committed; when a task-status update is part of the current change, `Done` means ready for the same commit.
- `Ready`: actionable with current information.
- `Blocked`: needs an explicit decision, external verification, or prerequisite task.
- `Planned`: valid work, but should wait for dependencies.
- `Future`: outside the first beta milestone.

Each story should be completed with tests, documentation updates, and reviewer follow-up. Code, behavior, task-status, operator workflow, or public documentation-contract changes require subagent review from the applicable roles in [reviewers.md](reviewers.md). Use the full Go, QA, product, and infosec panel when acceptance criteria touch auth, upload, Testcontainers, tool-surface behavior, or milestone completion. Manual-only review is reserved for typo-only or formatting-only documentation edits and must say why subagent review was not required.

Go tests should use `github.com/stretchr/testify` assertion objects. Prefer `require.New(t)` and `assert.New(t)` instances over package-level free functions, with `require` for test-stopping preconditions and `assert` for related checks where collecting multiple failures helps.

Interface mocks are generated with Mockery when needed. Mark interfaces with `//mockery:generate: true`, keep generation config in `.mockery.yml`, and generate all marked mocks in one run. Generated mocks live beside source packages under `mock/`, use package name `<parent>mock`, and use filenames shaped as `<InterfaceName>_mock.go`.

Give review subagents read-only workspace access when available so they can inspect relevant code, docs, and tests without writing files. If the available tooling only provides a writable fork, reviewers must be told not to edit files, and the parent checkout must be checked afterward. Unexpected subagent edits are not automatically trusted; inspect, validate, and rerun review on any such changes before committing them. Diff-only review is acceptable only as a fallback for narrow follow-ups or when workspace access is not available.

When PR or subagent review feedback is addressed after an initial review, rerun the applicable reviewer roles before final handoff if the follow-up changes code, tests, behavior, operator workflow, or public documentation contracts. Keep reruns focused on the follow-up diff. Typos and formatting-only documentation follow-ups do not need rerun review, but the completion note should say why.

Before marking a story `Done`, add or update story-local completion notes:

- `Validation`: commands/checks run, or why a check was not applicable.
- `Review`: reviewer roles run, findings addressed, or why subagent review was not required for a typo-only or formatting-only documentation edit.
- `Residual risk`: accepted unresolved risk, or `none`.

When updating an already-pushed branch or existing PR, prefer fresh follow-up commits over amending or force-pushing. Rewrite published history only for an explicit operator request or a concrete repository hygiene issue, and use `--force-with-lease` when a rewrite is unavoidable. Keep existing PR titles, descriptions, checklists, validation notes, review summaries, residual risks, and follow-up lists current whenever follow-up commits change the branch scope or status. Prefer squash merge when merging PRs unless the operator or repository policy requires another strategy.

Remove draft status once the PR is ready for human review: all automated or subagent review feedback has been addressed or explicitly documented, required rerun reviews are complete, the PR title/body/checklist are current, and the pipeline has passed on the latest pushed commit. Do not mark the PR ready while CI is pending, failing, or stale for an older head SHA.

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
- Validation: `go test ./...` passed with workspace-local Go build cache before commit. Follow-up CI alignment with `dvgoutils` added Go coverage reporting after `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache go test -coverpkg=./... -coverprofile=/private/tmp/inventree-mcp-coverage.out ./...` passed and reported 82.6% total coverage. Coverage badge follow-up passed YAML load validation for CI configs, the same cached Go test and coverage commands, and `git diff --check`.
- Review: covered by earlier planning review passes. Follow-up CI alignment reviewed by Senior QA / Test Architect and Senior Product Manager. QA found workflow-level write permissions were too broad for the gremlins job; fixed by moving write permissions to the test job and leaving gremlins read-only. Product found the omitted `dvgoutils` gist-backed badge needed to be explicit; fixed in README setup notes. Focused QA and product reruns found no remaining actionable findings. Coverage badge follow-up was manually reviewed as a narrow completion of the already-reviewed explicit product gap; no subagent rerun was required because it only replaces the documented omitted badge with the configured gist ID and secret note.
- Residual risk: Go coverage reporting writes git notes and may comment on pull requests, so repository workflow permissions must allow read/write Actions tokens for the test job. Coverage badge publishing depends on `COVERAGE_GIST_SECRET` retaining permission to update gist `709e99cf973e064f68cf3937b3d5c633`.
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

### M0-S04: Release Automation And Packages

- Status: `Done`
- Depends on: M0-S02
- Scope: add tag-driven GitHub releases with GoReleaser, binary archives, Linux packages, and systemd packaging.
- Validation: `git diff --check` passed; `goreleaser check` passed; `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache go test ./...` passed; `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache goreleaser release --snapshot --clean` passed and generated Linux/macOS/Windows archives plus Linux `deb`, `rpm`, and `apk` packages. Plain `go test ./...` failed before the cached rerun because the sandbox could not write to the default macOS Go build cache.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager reviews run. Initial findings asked to avoid documenting a start path for the currently non-running HTTP service, expose full stamped version metadata, align usage text, document Alpine/OpenRC limits for `apk`, add GitHub release setup instructions, add deterministic snapshot package validation before tag releases, and reject non-`vX.X.X` tags. Follow-up changes addressed those findings with direct config smoke-test docs, `Restart=on-failure`, full `version` output, `Release Preview`, strict release-tag validation, and aligned README/PLAN/operator/agent docs. Focused Go, QA, and product reruns found no remaining actionable findings; final narrow Go and product reruns on the service-startup correction also found no actionable findings.
- Residual risk: first production service start still waits for the HTTP server runtime and OAuth milestones; `apk` installs package files but does not provide OpenRC service management; GitHub repository Actions/release permissions must be confirmed in GitHub before the first real tag release.
- Acceptance:
  - Pushing a `vX.X.X` tag runs a GitHub Actions release workflow.
  - GoReleaser publishes GitHub release assets containing checksums, binary archives, and Linux `deb`, `rpm`, and `apk` packages.
  - Packaged installs include a systemd unit, environment-file template, and maintainer scripts following the repository release-packaging conventions.
  - User and agent documentation explains release, install, and systemd setup behavior.

Tasks:

- [x] Add `.goreleaser.yaml`.
- [x] Add `.github/workflows/release.yml`.
- [x] Add packaged systemd unit and maintainer scripts.
- [x] Add release version metadata to the CLI.
- [x] Update README, plan, operator recipes, and agent instructions.

## Milestone 1A: Buildable Skeleton

### M1A-S01: Command And Config Skeleton

- Status: `Done`
- Depends on: M0-S02
- Scope: add the first buildable `inventree-mcp` command with typed config.
- Validation: `go test ./...` passed; `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache go test ./...` passed; `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache go build ./cmd/inventree-mcp` passed; `git diff --check` passed. Initial plain `go test ./...` failed because the default macOS Go build cache was outside the writable sandbox before cache write access was granted.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager subagent reviews run. Go review found STDIO `serve` wrote a success banner to stdout; fixed by keeping successful `serve` silent and adding a regression test. QA and product findings on missing durable subagent review evidence were addressed in this note and the workflow wording updates. Follow-up Go and QA reviews after PR comments found stale token-source wording in `AGENTS.md`, stale token-source wording in `docs/PLAN.md`, and missing durable rerun evidence in this note; those findings were addressed. Final focused Go, QA, and product reruns on the follow-up diff found no actionable findings. A speculative write-error handling change introduced during review was removed before final handoff because it was outside the review-comment scope; focused Go and QA cleanup reviews found no actionable findings. Fresh full-panel Go, QA, product, and infosec review of the full PR found `serve --help` returned an error, empty HTTP `--path`/`--listen` branches lacked explicit tests, production STDIO accepted TLS skip-verify, and non-HTTP(S) InvenTree URLs were accepted; these findings were fixed with regression tests. Follow-up Go, QA, and infosec reviews found no actionable findings; product follow-up requested README clarification for development-only TLS skip-verify and this durable review note. Narrow product follow-up review of the README/TASKS docs fixes found no actionable findings. Test assertions were converted to Testify assertion objects after operator feedback; focused Go, QA, and product reviews found no actionable findings. Mockery marker/config conventions were aligned with repository conventions; focused Go, QA, and product reviews found no actionable findings.
- Residual risk: HTTP command still only validates development-mode config until `M1C` defines final OAuth config and server behavior. The command output helper intentionally ignores stdout/stderr write failures.
- Acceptance:
  - `cmd/inventree-mcp` builds.
  - `inventree-mcp serve --transport stdio` and `--transport http` parse config and fail gracefully for missing required values.
  - Production mode rejects upstream TLS skip-verify.
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

- Status: `Done`
- Depends on: M1A-S01
- Scope: add deterministic platform seams and context logging.
- Validation: `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache go test ./...` passed; `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache go build -o /private/tmp/inventree-mcp-build/inventree-mcp ./cmd/inventree-mcp` passed after downloading `dvgoutils` into the writable module cache; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager subagent reviews run. Initial review found the root logger context was discarded, scoped logging risked becoming a second logging API, redaction conventions were only helper functions, command/server logging setup was not executable, deterministic clock coverage was weak, and task notes needed to clarify the platform-seam boundary. Fixes passed the seeded context into the `serve` path, removed the extra scoped-logging wrapper in favor of direct `dvgoutils/logging` use, installed a `slog.ReplaceAttr` redaction policy for sensitive auth/setup keys, added deterministic clock coverage, and recorded this completion boundary. Focused Go and QA reruns found no actionable findings. Focused product rerun found that request/tool scoped logging adoption belonged to the server/tool task; the acceptance criterion was moved to `M1A-S03`, and final product rerun found no actionable findings.
- Residual risk: actual request/tool scoped logging adoption and runtime use of the clock, ID, and randomness seams will occur in later server, OAuth, upload, and tool tasks because this story only adds the command/root-context and platform-seam foundation.
- Acceptance:
  - Root contexts are seeded with `dvgoutils/logging.WithLogger`.
  - Scoped logger derivation and context reattachment pattern is covered before request/tool paths exist.
  - Clock, ID, and randomness are injectable where needed.
  - Tests use `dvgoutils/logging/testhandler.SetupTestHandler`.

Tasks:

- [x] Add logging setup in command/server construction.
- [x] Add `internal/platform` clock and ID/randomness seams.
- [x] Add log redaction conventions.
- [x] Add tests proving scoped attributes survive through context.

### M1A-S03: MCP Server Skeleton

- Status: `Done`
- Depends on: M1A-S01, M1A-S02
- Scope: create server construction, STDIO transport, HTTP transport, and a health/version tool.
- Validation: `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache go test ./...` passed after the new MCP SDK dependency and transitive modules were downloaded into the workspace-local module cache; focused `go test ./internal/server -run 'TestHTTPHandlerUsesStatelessStreamableServer|TestHealthVersionToolReturnsReadOnlyStatus' -count=1 -v` passed after QA follow-up coverage; `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache golangci-lint run` passed with 0 issues after the CI errcheck follow-up; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager reviews run. Go findings to preserve request context in the HTTP handler, keep the MCP SDK as a direct dependency, and exercise CLI serve wiring were fixed; the first Go rerun requested moving the CLI seam lower, and final narrow Go rerun found no actionable findings after the test routed through `serve` before the `server.Run` seam. QA findings to prove sessionless stateless HTTP `tools/list` behavior and assert all health/version fields were fixed, and focused QA rerun found no actionable findings. Product findings to document the registered skeleton tool's manifest fields and replace placeholder review notes were fixed, and focused product rerun found no actionable findings.
- Residual risk: production HTTP mode remains disabled until OAuth lands; the skeleton HTTP handler is tested without binding a local port because the sandbox blocks test listeners.
- Acceptance:
  - STDIO server can initialize and list tools.
  - HTTP streamable server runs stateless.
  - Request/tool scoped loggers are derived and reattached to context.
  - Health/version tool is read-only.
  - Tool annotation helper tests cover SDK `v1.6.1` pointer false behavior for `destructiveHint` and `openWorldHint`.

Tasks:

- [x] Add `internal/server`.
- [x] Add `internal/tools` registration entrypoint.
- [x] Register health/version tool.
- [x] Wire `mcp.StdioTransport`.
- [x] Wire `mcp.NewStreamableHTTPHandler` with stateless options.
- [x] Add tool annotation helpers and tests.

## Milestone 1B: InvenTree Client Foundation

### M1B-S01: REST Client Core

- Status: `Done`
- Depends on: M1A-S01
- Scope: implement low-level InvenTree HTTP client with auth, pagination, errors, and PATCH helpers.
- Validation: `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache go test ./...` passed. `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache golangci-lint run` passed with 0 issues after sandbox cache-write warnings. `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache go test -covermode count -coverpkg ./... -coverprofile /private/tmp/inventree-mcp-cover.out ./...` plus `go tool cover -func` reported 85.8% total coverage after the CI coverage-threshold follow-up.
- Review: Senior QA / Test Architect, Senior Product Manager, and Senior Go Developer reviews run. QA found common API error mapping was under-tested; fixed with coverage for validation, authentication, permission, not found, conflict, rate limit, server, unexpected, non-JSON, and list-detail responses. Product found empty PATCH no-ops were not rejected; fixed with a pre-request guard and regression test. Go found 409 Conflict needed its own typed error kind; fixed with `ErrorKindConflict`. Follow-up Go, QA, and product reviews found no remaining actionable findings. CI lint then found unchecked response body discard and close errors; fixed with checked discard and an explicit ignored deferred close. Narrow Go and QA reruns on the CI follow-up found no actionable findings. CI coverage then failed at exactly 80.0%; fixed with targeted client error-path tests, and a narrow QA rerun found no actionable findings.
- Residual risk: endpoint-specific client methods and schema-manifest enforcement are intentionally deferred to M1B-S02 and M1B-S03.
- Acceptance:
  - Supports `Authorization: Token ...` and `Authorization: Bearer ...`.
  - Pagination helpers are covered by tests.
  - PATCH serialization preserves omitted fields versus explicit zero/false/empty/null.
  - Error mapping normalizes common InvenTree API failures.

Tasks:

- [x] Add `internal/inventree` client.
- [x] Add auth header model.
- [x] Add pagination helpers.
- [x] Add error mapping.
- [x] Add PATCH helper and zero-value tests.

### M1B-S02: Schema Endpoint Manifest

- Status: `Done`
- Depends on: M1B-S01
- Scope: add a generated or maintained manifest tying implemented endpoints to `docs/api-schema.yaml`.
- Validation: `GOCACHE=/Users/david/Projects/inventree-mcp/.gocache GOMODCACHE=/private/tmp/inventree-mcp-gomodcache go test ./...` passed. `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Go found optional schema fields, loose YAML decoding, and unused schema path validation; fixed with strict request/response contract checks, `KnownFields(true)`, regression tests, and schema path validation. QA found missing role-filter coverage, incomplete provenance assertions, and ambiguous parameter-template scope; fixed with `required_query` checks, OpenAPI/API version assertions, and explicit template/category-link scope notes. Product found parameter write scope ambiguity, company role-filter gaps, and missing attachment object-type scope metadata; fixed by removing template/category-link mutation entries, adding supplier/manufacturer query filters, and recording attachment model-type scope. Infosec found missing forbidden report detail endpoints and endpoint-vs-upload-boundary overclaiming; fixed by forbidding report detail paths and documenting that upload/file authorization remains in later attachment/image tool enforcement. Focused reruns for all four roles found no remaining actionable findings.
- Residual risk: the manifest is maintained YAML rather than generated code; the tests enforce strict manifest fields, schema existence, operation IDs, selected query filters, request/response schema refs, schema/provenance drift, attachment model-type scope metadata, and deferred file-surface exclusion, but future client method enforcement still depends on later endpoint-specific client code consulting the manifest. Upload/file-source authorization remains owned by later attachment/image client and tool tests, not this endpoint-level manifest.
- Acceptance:
  - Implemented client methods reference schema-known path/method/request/response data.
  - Schema drift checks require `docs/api-schema.md` provenance updates.
  - Attachment and parameter endpoint capability tables remain authoritative.
  - Attachment/image manifest checks reject deferred app-specific file surfaces such as notes image upload, generated report attachments, and stock test-result attachments unless a later task explicitly brings them into scope.

Tasks:

- [x] Add schema parsing/check helper.
- [x] Add endpoint manifest format.
- [x] Add docs drift check.
- [x] Cover parts, categories, companies, stock, parameters, attachments, and purchasing preview dependencies.

## Milestone 1H: Early Integration Test Environment

These integration-environment stories are intentionally pulled forward before read-only client methods so new client and tool behavior can gain optional real InvenTree coverage as it lands. The broad milestone happy-path integration suite remains later, after the corresponding workflow, upload, and image behavior exists.

### M1H-S01: Testcontainers Stack Spike

- Status: `Planned`
- Depends on: M1B-S01, M1B-S02
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

### M1B-S03: Read-Only Client Methods

- Status: `Planned`
- Depends on: M1B-S01, M1B-S02, M1H-S02
- Scope: implement read-only API methods needed by milestone 1.
- Acceptance:
  - Methods exist for part, category, company, stock location/item, parameter, attachment, and supplier-part lookup.
  - Default tests use fake transports, not live network.
  - Optional integration-tag tests use the shared Testcontainers environment where real API behavior materially improves confidence.
  - `go test ./...` does not start Docker.

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

## Milestone 1H: Integration Happy Paths

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

### F-S05: Systemd Notify And Watchdog Support

- Status: `Future`
- Depends on: long-running HTTP server runtime, production HTTP OAuth startup behavior, and product review
- Scope: add native systemd notification support for packaged HTTP deployments.
- Acceptance:
  - HTTP service startup sends systemd readiness only after the listener is bound, runtime dependencies are initialized, and production startup checks have passed.
  - The process sends watchdog heartbeats at a safe interval when systemd `WatchdogSec` is configured.
  - The process publishes useful systemd status text for startup, ready, degraded, shutdown, and fatal-error states without logging or exposing secrets.
  - Packaged systemd unit can safely switch from `Type=simple` to `Type=notify` with `NotifyAccess=main` and an explicit `WatchdogSec`.
  - Tests cover notify readiness ordering, heartbeat cadence, disabled-watchdog behavior, shutdown status, fatal-error status, and non-systemd fallback behavior.
  - README, operator recipes, release packaging docs, and `AGENTS.md` are updated to describe the supported systemd behavior.

Tasks:

- [ ] Select and wrap a maintained Go systemd notification library.
- [ ] Add injectable notifier/watchdog abstraction for deterministic tests.
- [ ] Send startup status transitions and final readiness notification.
- [ ] Send watchdog heartbeats only when systemd watchdog is enabled.
- [ ] Publish shutdown, degraded, and fatal-error status messages.
- [ ] Update packaged systemd unit to `Type=notify` after code support lands.
- [ ] Add unit and integration tests for notify/watchdog behavior.
- [ ] Update release and operator documentation.
