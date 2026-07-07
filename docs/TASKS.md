# Implementation Tasks

This backlog turns [PLAN.md](PLAN.md) into executable work. Status values are:

- `Done`: acceptance criteria met, validation/review recorded, and committed; when a task-status update is part of the current change, `Done` means ready for the same commit.
- `Ready`: actionable with current information.
- `Blocked`: needs an explicit decision, external verification, or prerequisite task.
- `Planned`: valid work, but should wait for dependencies.
- `Future`: outside the first beta milestone.

Each story should be completed with tests, documentation updates, and reviewer follow-up. Code, behavior, task-status, operator workflow, or public documentation-contract changes require subagent review from the applicable roles in [reviewers.md](reviewers.md). Use the full Go, QA, product, and infosec panel when acceptance criteria touch auth, upload, Testcontainers, tool-surface behavior, or milestone completion. Manual-only review is reserved for typo-only or formatting-only documentation edits and must say why subagent review was not required.

When selecting the next story, update the Codex thread title to include the story ID and short title. If the active story changes, update the thread title again before continuing.

Local test commands do not need `-v` by default. Use verbose local test output when diagnosing failures, checking expected logs, or recording evidence that depends on test logs. CI, release, and other pipeline test commands should always run Go tests with `-v` so successful pipeline logs retain integration-test and container-output evidence.

Go tests should use `github.com/stretchr/testify` assertion objects. Prefer `require.New(t)` and `assert.New(t)` instances over package-level free functions, with `require` for test-stopping preconditions and `assert` for related checks where collecting multiple failures helps.

Go tests that call code accepting `context.Context` should pass a context from `github.com/davidvanlaatum/dvgoutils/logging/testhandler.SetupTestHandler(t)` so the test's `testing.T` owns cancellation and log capture. Create that context inside each subtest that uses it, especially parallel subtests. Cleanup callbacks that run after `t.Context()` cancellation may use bounded cleanup contexts or `context.WithoutCancel(ctx)` when they still need context-carried test logger values.

Interface mocks are generated with Mockery when needed. Mark interfaces with `//mockery:generate: true`, keep generation config in `.mockery.yml`, and generate all marked mocks in one run. Generated mocks live beside source packages under `mock/`, use package name `<parent>mock`, and use filenames shaped as `<InterfaceName>_mock.go`.

Give review subagents read-only workspace access when available so they can inspect relevant code, docs, and tests without writing files. If the available tooling only provides a writable fork, reviewers must be told not to edit files, and the parent checkout must be checked afterward. Unexpected subagent edits are not automatically trusted; inspect, validate, and rerun review on any such changes before committing them. Diff-only review is acceptable only as a fallback for narrow follow-ups or when workspace access is not available.

When requesting review, include validation commands already run, their results, and any known failures or fixes when that evidence is available. Reviewers should use that evidence and rerun only the checks they need for independent confidence, changed follow-up diffs, or unresolved risk.

When PR or subagent review feedback is addressed after an initial review, rerun the applicable reviewer roles before final handoff if the follow-up changes code, tests, behavior, operator workflow, or public documentation contracts. Keep reruns focused on the follow-up diff. Typos and formatting-only documentation follow-ups do not need rerun review, but the completion note should say why.

Before marking a story `Done`, add or update story-local completion notes:

- `Validation`: commands/checks run, or why a check was not applicable.
- `Review`: reviewer roles run, findings addressed, or why subagent review was not required for a typo-only or formatting-only documentation edit.
- `Residual risk`: accepted unresolved risk, or `none`.

When any story status changes, update both the Task Index row and the story-local `Status:` line in the same change. Before handoff, re-read both locations for every edited story and fix any mismatch.

When updating an already-pushed branch or existing PR, prefer fresh follow-up commits over amending or force-pushing. Rewrite published history only for an explicit operator request or a concrete repository hygiene issue, and use `--force-with-lease` when a rewrite is unavoidable. Keep existing PR titles, descriptions, checklists, validation notes, review summaries, residual risks, and follow-up lists current whenever follow-up commits change the branch scope or status. Prefer squash merge when merging PRs unless the operator or repository policy requires another strategy.

Remove draft status once the PR is ready for human review: all automated or subagent review feedback has been addressed or explicitly documented, required rerun reviews are complete, the PR title/body/checklist are current, and the pipeline has passed on the latest pushed commit. Do not mark the PR ready while CI is pending, failing, or stale for an older head SHA.

Before `M1C-S04` is complete, mutating, operational, destructive, and upload tools may be registered only on STDIO or in unit-test registries. HTTP registration must filter them out of the exposed tool manifest until per-tool scope enforcement is implemented and tested.

## Task Index

| Task | Brief description | Status |
| --- | --- | --- |
| [M0-S01](#m0-s01-initialize-repository-scaffold) | Create repository baseline, docs, schema snapshot, GitHub remote, and initial commits. | Done |
| [M0-S02](#m0-s02-add-project-automation) | Add minimal Go module, GitHub Actions, Dependabot, golangci-lint, and pre-commit. | Done |
| [M0-S03](#m0-s03-first-beta-documentation-contracts) | Create first-beta tool reference and operator recipe skeletons. | Done |
| [M0-S04](#m0-s04-release-automation-and-packages) | Add tag-driven releases, GoReleaser assets, Linux packages, and systemd packaging. | Done |
| [M0-S05](#m0-s05-test-context-and-stable-ci-hygiene) | Standardize test contexts and simplify Go CI to the stable toolchain. | Done |
| [M1A-S01](#m1a-s01-command-and-config-skeleton) | Add the first buildable `inventree-mcp` command with typed config. | Done |
| [M1A-S02](#m1a-s02-logging-clock-ids-and-randomness) | Add deterministic platform seams and context logging. | Done |
| [M1A-S03](#m1a-s03-mcp-server-skeleton) | Create MCP server construction, STDIO/HTTP transports, and health/version tool. | Done |
| [M1B-S01](#m1b-s01-rest-client-core) | Implement the low-level InvenTree REST client core. | Done |
| [M1B-S02](#m1b-s02-schema-endpoint-manifest) | Add the schema-backed endpoint manifest. | Done |
| [M1H-S01](#m1h-s01-testcontainers-stack-spike) | Prove the pinned InvenTree Testcontainers stack. | Done |
| [M1H-S02](#m1h-s02-shared-suite-fixtures-and-isolation) | Add shared suite fixtures, per-run accounts, and isolation checks. | Done |
| [M1B-S03](#m1b-s03-read-only-client-methods) | Implement read-only client methods needed by milestone 1. | Done |
| [M1C-S01](#m1c-s01-mcp-sdk-auth-spike) | Spike official MCP SDK auth behavior for HTTP. | Planned |
| [M1C-S02](#m1c-s02-chatgpt-connector-compatibility-spike) | Verify ChatGPT connector OAuth compatibility. | Blocked |
| [M1C-S03](#m1c-s03-oauth-envelope-and-code-storage) | Implement OAuth token envelopes and auth-code storage. | Planned |
| [M1C-S04](#m1c-s04-scope-guard-and-credential-propagation) | Enforce per-tool OAuth scopes and credential propagation. | Planned |
| [M1D-S01](#m1d-s01-lookup-tool-framework) | Add common lookup tool framework and clarification contracts. | Done |
| [M1D-S02](#m1d-s02-part-company-stock-parameter-and-attachment-lookup-tools) | Add read-only part, company, stock, parameter, and attachment lookup tools. | Done |
| [M1E-S01](#m1e-s01-part-and-company-writes) | Add part and company write tools. | Done |
| [M1E-S02](#m1e-s02-parameter-writes) | Add existing-template-only parameter writes. | Done |
| [M1E-S03](#m1e-s03-initial-stock-writes) | Create initial stock items with duplicate detection. | Done |
| [M1F-S01](#m1f-s01-upload-source-resolver) | Resolve inline, STDIO local-path, and URL upload sources safely. | Done |
| [M1F-S02](#m1f-s02-attachment-tools) | Add attachment upload, link, update, and delete tools. | Done |
| [M1F-S03](#m1f-s03-primary-part-image) | Add part primary image download and assignment/replacement. | Done |
| [M1G-S01](#m1g-s01-part-upsert-workflow) | Add safer part upsert workflow with supplier/manufacturer data. | Done |
| [M1G-S02](#m1g-s02-initial-stock-and-purchase-preview-workflows) | Add initial-stock workflow helper and no-write purchase preview. | Done |
| [M1G-S03](#m1g-s03-milestone-prompts) | Add milestone 1 prompts and prompt contract tests. | Done |
| [M1H-S03](#m1h-s03-milestone-integration-happy-paths) | Prove milestone catalog, stock, supplier, attachment, image, and preview happy paths. | Done |
| [M1H-S04](#m1h-s04-delete-attachment-confirmation-clarification) | Preserve structured delete confirmation clarification through MCP. | Done |
| [M1I-S01](#m1i-s01-operator-docs-finalization) | Finalize README, operator recipes, and generated tool reference alignment. | Done |
| [M1I-S02](#m1i-s02-final-review-panel) | Run final Go, QA, product, and infosec review panel. | Ready |
| [F-S01](#f-s01-evaluate-docker-compose-testcontainers-stack) | Evaluate Docker Compose-based Testcontainers stack. | Future |
| [F-S02](#f-s02-bom-import-workflow) | BOM import workflow. | Future |
| [F-S03](#f-s03-purchase-order-write-and-receiving) | Purchase order write and receiving. | Future |
| [F-S04](#f-s04-build-order-workflows) | Build order workflows. | Future |
| [F-S05](#f-s05-stocktake-adjustments) | Stocktake adjustments. | Future |
| [F-S06](#f-s06-systemd-notify-and-watchdog-support) | Native systemd notification support for packaged HTTP deployments. | Future |

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
- Validation: `go test ./...` passed with workspace-local Go build cache before commit. Follow-up CI alignment with `dvgoutils` added Go coverage reporting after `go test -coverpkg=./... -coverprofile=/private/tmp/inventree-mcp-coverage.out ./...` passed and reported 82.6% total coverage. Coverage badge follow-up passed YAML load validation for CI configs, the same cached Go test and coverage commands, and `git diff --check`. CI cache follow-up persisted the Gremlins and test jobs' Go build and module caches using keys based on runner OS/arch, Go version, and module dependency hash so Go's own cache can invalidate changed packages internally. The follow-up passed GitHub workflow YAML load validation, `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/go.yml`, and `git diff --check`. PR #21 CI passed lint, test, and Gremlins after the dependency-keyed cache change; the first run saved a 195 MB `test-go` cache, restored Gremlins from the prior source-hash cache through a restore key, and saved the new dependency-keyed `gremlins-go` cache. The next run restored both exact final keys before passing with `test` in 27s and Gremlins in 2m36s.
- Review: covered by earlier planning review passes. Follow-up CI alignment reviewed by Senior QA / Test Architect and Senior Product Manager. QA found workflow-level write permissions were too broad for the gremlins job; fixed by moving write permissions to the test job and leaving gremlins read-only. Product found the omitted `dvgoutils` gist-backed badge needed to be explicit; fixed in README setup notes. Focused QA and product reruns found no remaining actionable findings. Coverage badge follow-up was manually reviewed as a narrow completion of the already-reviewed explicit product gap; no subagent rerun was required because it only replaces the documented omitted badge with the configured gist ID and secret note. Gremlins Go-cache follow-up received focused Senior QA / Test Architect and Senior Product Manager reviews with no unresolved actionable findings. Test-job dependency-keyed Go-cache follow-up received focused Senior QA / Test Architect and Senior Product Manager reviews with no unresolved actionable findings.
- Residual risk: Go coverage reporting writes git notes and may comment on pull requests, so repository workflow permissions must allow read/write Actions tokens for the test job. Coverage badge publishing depends on `COVERAGE_GIST_SECRET` retaining permission to update gist `709e99cf973e064f68cf3937b3d5c633`. Gremlins keeps the existing `version: latest` behavior, so future Gremlins releases can still change runtime behavior independently of the Go-cache key.
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
- Validation: `git diff --check` passed; `goreleaser check` passed; `go test ./...` passed; `goreleaser release --snapshot --clean` passed and generated Linux/macOS/Windows archives plus Linux `deb`, `rpm`, and `apk` packages. Plain `go test ./...` failed before the cached rerun because the sandbox could not write to the default macOS Go build cache.
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

### M0-S05: Test Context And Stable CI Hygiene

- Status: `Done`
- Depends on: M0-S02, M1A-S02
- Scope: standardize test contexts on `dvgoutils/logging/testhandler` and simplify the Go CI workflow to the stable toolchain only.
- Validation: `go test ./...` passed; `golangci-lint run` passed; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager reviews run. Reviewers found no unresolved actionable findings after the context/logger test cleanup, stable-only Go workflow update, and aligned instructions/backlog/plan wording.
- Residual risk: none.
- Acceptance:
  - Existing tests that call context-aware code pass contexts created from the active `testing.T` through `testhandler.SetupTestHandler(t)` where possible.
  - Parallel and independent subtests create their own logger contexts instead of reusing a parent test context.
  - Cleanup paths avoid raw `context.Background()` while preserving cleanup execution after `t.Context()` cancellation.
  - Go CI uses the stable Go toolchain rather than a version matrix.
  - Agent instructions and planning docs document the convention.

Tasks:

- [x] Update test context and logger guidance.
- [x] Replace raw test `context.Background()` calls with test logger contexts where applicable.
- [x] Keep subtest contexts scoped to the active `testing.T`.
- [x] Simplify the Go workflow to `stable`.
- [x] Run validation and review.

## Milestone 1A: Buildable Skeleton

### M1A-S01: Command And Config Skeleton

- Status: `Done`
- Depends on: M0-S02
- Scope: add the first buildable `inventree-mcp` command with typed config.
- Validation: `go test ./...` passed; `go build ./cmd/inventree-mcp` passed; `git diff --check` passed. Initial plain `go test ./...` failed because the default macOS Go build cache was outside the writable sandbox before cache write access was granted.
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
- Validation: `go test ./...` passed; `go build -o /private/tmp/inventree-mcp-build/inventree-mcp ./cmd/inventree-mcp` passed after downloading `dvgoutils` into the writable module cache; `git diff --check` passed.
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
- Validation: `go test ./...` passed after the new MCP SDK dependency and transitive modules were downloaded into the workspace-local module cache; focused `go test ./internal/server -run 'TestHTTPHandlerUsesStatelessStreamableServer|TestHealthVersionToolReturnsReadOnlyStatus' -count=1 -v` passed after QA follow-up coverage; `golangci-lint run` passed with 0 issues after the CI errcheck follow-up; `git diff --check` passed.
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
- Validation: `go test ./...` passed. `golangci-lint run` passed with 0 issues after sandbox cache-write warnings. `go test -covermode count -coverpkg ./... -coverprofile /private/tmp/inventree-mcp-cover.out ./...` plus `go tool cover -func` reported 85.8% total coverage after the CI coverage-threshold follow-up.
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
- Validation: `go test ./...` passed. `git diff --check` passed.
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

These integration-environment stories are intentionally pulled forward before read-only client methods so new client and tool behavior can gain real InvenTree coverage as it lands. Docker-backed integration coverage runs in the default test path unless explicitly excluded with `INVENTREE_TEST_SKIP_DOCKER=1` or `GOFLAGS=-trimpath go test -race -short`. The broad milestone happy-path integration suite remains later, after the corresponding workflow, upload, and image behavior exists.

### M1H-S01: Testcontainers Stack Spike

- Status: `Done`
- Depends on: M1B-S01, M1B-S02
- Scope: prove InvenTree startup, migrations, admin token creation, and readiness with Testcontainers.
- Acceptance:
  - Uses explicit InvenTree version tag matching schema snapshot.
  - Pinned InvenTree image tag is declared in testenv config or a single constant and appears in test logs.
  - `docs/api-schema.md` provenance records the matching runtime InvenTree version and API version.
  - Records runtime InvenTree version and API version.
  - Docker-backed integration tests run by default and can be explicitly excluded with `INVENTREE_TEST_SKIP_DOCKER=1` or `GOFLAGS=-trimpath go test -race -short`.

Tasks:

- [x] Add `internal/testenv`.
- [x] Choose and record the explicit InvenTree version tag matching `docs/api-schema.yaml`.
- [x] Start required database and InvenTree services.
- [x] Create deterministic admin/test token.
- [x] Add readiness polling.
- [x] Add default-on Docker integration test and explicit Docker skip behavior.

- Validation: `GOFLAGS=-trimpath go test -race ./...` passed with the default Docker-backed Testcontainers stack; `GOFLAGS=-trimpath go test -race ./internal/testenv -run TestStartInvenTreeStack -count=1 -v` passed and logged `inventree/inventree:1.4.0`, runtime version `1.4.0`, API `511`, and forwarded Postgres, Redis, and InvenTree container stdout/stderr with `container[name][stream]` prefixes; GitHub Actions Go coverage now uses `cover-mode: atomic` and passes `test-args: '["-v", "-race"]'`, and release/release-preview tests run `go test -v -race ./...`, so successful CI test runs also show forwarded container logs with race detection enabled; `GOFLAGS=-trimpath INVENTREE_TEST_SKIP_DOCKER=1 go test -race ./internal/testenv` passed; `GOFLAGS=-trimpath go test -race -tags no_integration_tests ./internal/testenv` passed with the Docker-backed integration test excluded by build tag; `GOFLAGS=-trimpath go test -race -covermode atomic -coverpkg ./... -coverprofile /private/tmp/inventree-mcp-cover.out ./...` passed and `go tool cover -func` reported 80.7% total coverage; `golangci-lint run` passed with 0 issues; `GOFLAGS=-trimpath go mod tidy -diff` passed; `git diff --check` passed. PR-comment follow-up validation: `GOFLAGS=-trimpath go test -race -tags no_integration_tests ./internal/testenv` passed; `GOFLAGS=-trimpath INVENTREE_TEST_SKIP_DOCKER=1 go test -race ./internal/testenv` passed; `GOFLAGS=-trimpath go test -race ./internal/testenv -run TestStartInvenTreeStack -count=1 -v` passed and confirmed initial `relation does not exist` Postgres lines occur during InvenTree `migrate` before update completion and before the HTTP readiness wait passes. Container-log lifecycle follow-up validation: `GOFLAGS=-trimpath go test -race ./internal/testenv -run TestSharedInvenTreeFixturesAndParallelRuns -count=1 -v` passed with a regression subtest proving an InvenTree access log emitted after `StartSharedInvenTree` returned is received by the configured container-log callback.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Go requested bounded cleanup contexts and test-log visibility for the pinned image/version/API; fixed by terminating containers with the caller context, adding bounded cleanup helpers, and logging the runtime pin before stack startup. QA requested excluding Docker-backed integration from Gremlins mutation testing while preserving default `go test ./...` integration coverage; fixed with a Gremlins config that sets the `no_integration_tests` build tag and a matching `!no_integration_tests` build constraint on the Docker-backed integration test. Product found stale optional-integration, skip-policy, and latest-stable compatibility wording; fixed so docs state default-on Docker integration, explicit skip paths, blocking InvenTree `1.4.0`, and non-blocking latest-stable canary coverage. Infosec found host-port exposure with fixed test credentials; fixed by forcing Postgres, Redis, and InvenTree host bindings to `127.0.0.1` and asserting runtime bindings in the Docker-backed test. Initial GitHub Actions runs then exposed Testcontainers' default 60-second outer `WithWaitStrategy` deadline around InvenTree readiness; fixed by using `WithWaitStrategyAndDeadline(opts.StartupTimeout, ...)` for the server wait. The next GitHub Actions run passed Release Preview but failed Go coverage at 79.5%; fixed with deterministic `internal/testenv` unit coverage for option validation, version/token helper auth, token proof, and JSON error paths. Focused reruns for all four initial roles, Go/QA reruns for the CI wait-deadline follow-up, Go/QA reruns for the coverage follow-up, and focused Go/QA/Product reruns for the Gremlins build-tag follow-up found no remaining actionable findings. The container-log follow-up forwards Postgres, Redis, and InvenTree stdout/stderr to verbose integration test output; Go review requested making successful CI runs verbose and serializing callback calls, fixed with coverage-action `test-args: '["-v"]'` and a synchronized log callback wrapper. QA review then found release workflows still used non-verbose tests; fixed by changing release and release-preview test steps to `go test -v ./...`. Race/trimpath follow-up made CI and operator test commands use `GOFLAGS=-trimpath` plus `-race` wherever supported, with atomic coverage mode for race-enabled coverage. PR-comment follow-up grouped Testcontainers Dependabot updates, added `DefaultTestOptions(t)` for default test-log forwarding, made `Start` return a bounded cleanup function plus `CleanupForTest(t, cleanup)` for direct `t.Cleanup` registration with visible cleanup errors, and documented that observed initial Postgres relation errors occur during InvenTree migrations rather than after server readiness. Container-log lifecycle follow-up review found the request-level Testcontainers log producer was tied to the startup timeout context, and the first fix needed log-context cancellation before `StopLogProducer`; fixed with an environment-owned log context, explicit producer stop during cleanup, and a Docker-backed post-start log-forwarding regression. Focused Go and QA reruns on the final diff found no actionable findings.
- Residual risk: default test runs now require Docker unless explicitly excluded with `INVENTREE_TEST_SKIP_DOCKER=1` or `GOFLAGS=-trimpath go test -race -short`. The test stack uses fixed disposable credentials bound to loopback-only published ports. Postgres and Redis remain pinned to readable major/family tags (`postgres:17`, `redis:7-alpine`) for this spike; future shared-fixture work can tighten supporting-service pins if drift becomes noisy. Container log forwarding uses Testcontainers' deprecated manual log producer lifecycle so forwarding can outlive the startup timeout context; a future Testcontainers major upgrade may require moving this back to supported request-level log consumer configuration. Total coverage is 80.7%, leaving a narrow margin over the 80% CI threshold.

### M1H-S02: Shared Suite Fixtures And Isolation

- Status: `Done`
- Depends on: M1H-S01
- Scope: add suite-owned container lifecycle, per-run InvenTree test accounts, on-demand run-prefixed fixtures, and mutable-record ownership checks.
- Validation: `GOFLAGS=-trimpath INVENTREE_TEST_SKIP_DOCKER=1 go test -race -count=1 ./...` passed; `GOFLAGS=-trimpath INVENTREE_TEST_SKIP_DOCKER=1 go test -race -covermode atomic -coverpkg ./... -coverprofile /private/tmp/inventree-mcp-cover.out ./...` passed; `GOFLAGS=-trimpath go test -race ./internal/testenv -run 'Test(StartInvenTreeStack|SharedInvenTreeFixturesAndParallelRuns)$' -count=1` passed with both top-level Docker integration tests marked parallel; focused `-v` Docker validation for `TestSharedInvenTreeFixturesAndParallelRuns` passed and logged both account create/retrieve intent and returned usernames for alpha and beta run prefixes while exercising live category, supplier-part, BOM, and mutable-company paths; `GOFLAGS=-trimpath go test -race -count=1 ./...` passed with the default Docker-backed Testcontainers stack; `GOFLAGS=-trimpath go mod tidy -diff` passed; `golangci-lint run` passed with 0 issues; `git diff --check` passed. Docker-backed validation requires sandbox escalation for the Docker socket.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Initial Go/QA/Infosec reviews found account/run mismatch gaps, duplicate-account idempotency gaps, shared destructive cleanup risk, missing BOM fixture coverage, deterministic per-test passwords, and pending task metadata; fixes added account/run binding checks, create-or-retrieve account behavior, random per-account passwords, removed the shared destructive cleanup helper, added unit and live Docker BOM fixture coverage, and updated task metadata. Go final review found no actionable findings. Product and Infosec final reviews only found stale pending review/residual-risk notes; fixed in this note. QA final review requested BOM fixture coverage; fixed with unit and Docker-backed coverage. Focused QA follow-up requested stronger log-filter assertions; fixed by extracting the container-log filter callback and asserting the dropped startup-noise summary plus forwarded lines. Focused product/workflow review of the validation-evidence cleanup found one duplicate historical command after cache-path removal; fixed by collapsing the duplicate.
- Residual risk: per-test accounts are admin-scoped for this Testcontainers helper so read/write permission isolation remains deferred to later auth-isolation work. Run-scoped users, tokens, fixtures, and mutable records are left in the disposable InvenTree environment until container teardown by design. `Environment()` still exposes the bootstrap admin token for setup and low-level testenv assertions; tests should prefer `shared.Account`, `shared.Client`, and run-scoped helpers for normal integration coverage.
- Acceptance:
  - Parent test acquires environment before parallel subtests.
  - Subtests request their own InvenTree user account/token, client, and only the run-prefixed fixtures they need.
  - Every account, mutating, or fixture helper requires a `Run` object.
  - Shared helpers leave run-scoped records in the disposable environment by default instead of providing destructive cleanup.
  - Integration tests log generated InvenTree usernames with the owning run prefix for log correlation.

Tasks:

- [x] Add `SharedInvenTree`.
- [x] Add per-run InvenTree test account helpers.
- [x] Add on-demand run-prefixed fixture helpers.
- [x] Add `Run` prefix format `IT_<runid>_<pkg>_<test>_`.
- [x] Add mutable-record ownership checks.
- [x] Add parallel isolation tests.

### M1B-S03: Read-Only Client Methods

- Status: `Done`
- Depends on: M1B-S01, M1B-S02, M1H-S02
- Scope: implement read-only API methods needed by milestone 1.
- Validation: `INVENTREE_TEST_SKIP_DOCKER=1 go test ./...` passed; `golangci-lint run` passed with 0 issues; `GOFLAGS=-trimpath go test -race ./...` passed with the Docker-backed shared InvenTree suite; `go mod tidy -diff` passed; `git diff --check` passed. Focused live validation `go test ./internal/inventree -run TestReadOnlyClientReads -count=1` passed after adding real parameter, link-attachment, and file-attachment fixtures. Client integration coverage follow-up validation: `INVENTREE_TEST_SKIP_DOCKER=1 go test ./internal/inventree` passed; `go test ./internal/inventree -run TestClientMethodsAgainstInvenTree -count=1` passed with live coverage for the exported client read/write methods; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Infosec Reviewer reviews run. Initial Go and QA findings found schema-shape mismatches for part parameters, category parameter template fields/filtering, and parameter-template choices; fixes made part-parameter lookup use schema-backed `model_type=part.part` plus `model_id`, corrected `template` decoding, represented template choices as the schema's comma-separated string, and filtered category parameter templates client-side because the schema exposes no category query filter. Infosec found sensitive attachment URL leakage risk and missing bounded read time; fixes redacted returned source URLs, hid transport URL details, rejected URL userinfo, and added a default download timeout when neither context nor client timeout is set. QA follow-up requested real file-attachment integration coverage for successful downloads; fixed with a multipart file attachment fixture. Focused Go, QA, and infosec reruns found no remaining actionable findings. PR comment follow-up split the live read-only client integration test into lookup-area subtests; focused QA review found no actionable findings. Operator follow-up then corrected the subtest ownership model; fixed so the parent only starts the shared environment and every subtest creates its own run, account, client, and run-prefixed fixtures, with `AGENTS.md` updated to make that rule explicit. Client integration coverage follow-up review: initial Go review requested typed purchase-order query structs instead of raw `url.Values`, and initial QA review requested live `SearchStockItems` row decoding instead of empty-list coverage; both were fixed. Focused Go and QA reruns found no actionable findings.
- Residual risk: read-only client structs include the milestone fields needed by planned tools rather than full InvenTree response models; later tool work may add fields as contracts become concrete. Attachment download byte/time bounds are client-level safeguards, while final tool-level output limits and redaction policy remain owned by attachment tool tasks.
- Acceptance:
  - Methods exist for part, category, company, stock location/item, parameter, attachment, and supplier-part lookup.
  - Default tests use fake transports, not live network.
  - Integration tests use the shared Testcontainers environment by default where real API behavior materially improves confidence.
  - Default `GOFLAGS=-trimpath go test -race ./...` may start the shared Testcontainers environment; use `INVENTREE_TEST_SKIP_DOCKER=1` or `GOFLAGS=-trimpath go test -race -short` only when explicitly excluding Docker-backed integration.

Tasks:

- [x] Add part/category lookup methods.
- [x] Add company/supplier/manufacturer lookup methods.
- [x] Add stock location/item lookup methods.
- [x] Add parameter template/value lookup methods.
- [x] Add attachment metadata/list/download methods.
- [x] Add supplier-part lookup methods for purchase preview.

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

- Status: `Done`
- Depends on: M1A-S03, M1B-S03
- Scope: implement common tool schemas, structured outputs, ambiguity responses, and fake-client handler tests.
- Validation: `go test ./cmd/inventree-mcp ./internal/tools` passed; `go test ./internal/tools` passed after the docs-drift follow-up; `go test ./...` passed; `golangci-lint run` passed with 0 issues; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager reviews run. Initial Go review found the real `serve` path did not wire lookup client dependencies and the first interface was too broad; fixed with STDIO InvenTree client construction in the CLI layer, HTTP lookup dependencies left unavailable until OAuth, and generic handler wrapping that lets each tool require a narrow interface. Initial QA review found the ambiguous lookup path was not exercised through a fake handler, docs drift checks were too weak, and `retry_values` documentation conflicted with `omitempty`; fixed with a fake ambiguous handler test, reflection-backed docs drift checks tied to framework schema structs and constants, and optional `retry_values` wording. Initial product review found pending completion metadata, a mismatch with the plan's structured clarification contract, and unclear Milestone 1 table authority; fixed by using the plan's `field`/`reason`/`retry`/`hard_error` clarification shape and documenting that the Milestone 1 table is a planning summary until tools are registered. Focused Go, final focused QA, and final focused product reruns found no remaining actionable findings.
- Residual risk: individual lookup tools, per-tool read-only annotations/scopes, no-result behavior, and full per-tool manifest rows remain deferred to M1D-S02.
- Acceptance:
  - Tool handlers depend on interfaces, not concrete HTTP clients.
  - Ambiguous lookup returns structured clarification with candidates and stable retry fields.
  - Tool schemas are documented in `docs/tool-reference.md`.

Tasks:

- [x] Add common tool dependency struct.
- [x] Add structured clarification response type.
- [x] Add fake-client test helpers.
- [x] Add docs generation or drift check.

### M1D-S02: Part, Company, Stock, Parameter, And Attachment Lookup Tools

- Status: `Done`
- Depends on: M1D-S01
- Scope: add read-only milestone lookup tools.
- Validation: `go test ./internal/tools ./internal/server ./internal/inventree` passed; `go test ./...` passed; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Initial Go/QA/product reviews found get-by-ID tools returned API 404s as tool errors instead of structured `not_found`; fixed with shared API not-found mapping and handler coverage. Initial product review found clarification candidates lacked operator inspection URLs; fixed with stable API-path URLs in candidates and test coverage. Initial QA review found part-image download lacked negative safety coverage and thumbnail-mode docs drift; fixed with missing-image, external URL, userinfo, redirect, oversized, thumbnail external URL, and transport-error redaction tests plus updated docs. Initial product/infosec reviews found attachment metadata tools could expose deferred model types and raw URL-bearing attachment records; fixed with in-scope model validation, sanitized metadata DTOs, URL query/userinfo/fragment redaction, and regression coverage. Focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: HTTP OAuth and per-tool scope enforcement remain deferred to M1C-S04; registered lookup tool scope metadata declares `inventree.read` but is not enforced until that task. Testcontainers coverage exercises the lower-level InvenTree read client, while the new registered tool handlers are covered with fake-client unit tests. The in-scope attachment model list is duplicated between client download and tool metadata layers until a later shared manifest source is added.
- Acceptance:
  - Implements milestone 1 read-only tools in `docs/tool-reference.md`.
  - Read-only annotations and `inventree.read` scopes are correct.
  - Tests cover ambiguous and no-result behavior.

Tasks:

- [x] Add part/category lookup tools.
- [x] Add company/supplier/manufacturer lookup tools.
- [x] Add stock location/item lookup tools.
- [x] Add parameter template/part parameter lookup tools.
- [x] Add attachment list/metadata/download tools.

## Milestone 1E: Basic Write Tools

### M1E-S01: Part And Company Writes

- Status: `Done`
- Depends on: M1B-S01, M1D-S02
- Scope: create/update parts and create supplier/manufacturer companies or links.
- Validation: `go test ./...` passed; `golangci-lint run` passed with 0 issues; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Initial findings requested direct HTTP `tools/list` exclusion checks, structured no-op `update_part` clarification, broader sales/customer boundary tests, role-less company rejection, positive-ID validation before writes, explicit company currency instead of defaulting to USD, structured missing-category clarification, removal of the low-level `PartCreate.salable` field, a server-layer HTTP/write-tool guard, and docs alignment for registered write-tool behavior. Fixes addressed those findings with tests and docs updates. Final Go, QA, product, and infosec reruns found no actionable findings.
- Residual risk: write tools are intentionally STDIO/test-registry only until `M1C-S04` implements per-tool OAuth scope enforcement. Client create/update payloads cover milestone fields rather than every schema field; later workflow tools may add richer success summaries.
- Acceptance:
  - PATCH is used where schema supports it.
  - Existing companies/categories are preferred over creating new records.
  - No customer-role defaults are introduced.
  - Tool registration includes `inventree.write` scope tests.
  - HTTP registration is disabled or rejected until `M1C-S04` per-tool scope enforcement is complete.
  - Infosec review has no unresolved actionable findings before any mutating HTTP tool is exposed.

Tasks:

- [x] Add `create_part`.
- [x] Add `update_part`.
- [x] Add `create_company`.
- [x] Add `create_supplier_part`.
- [x] Add `create_manufacturer_part`.
- [x] Add sales/customer boundary tests.

### M1E-S02: Parameter Writes

- Status: `Done`
- Depends on: M1D-S02
- Scope: set part parameters using existing templates only for milestone 1.
- Validation: `go test ./...` passed with the default Docker-backed Testcontainers suite; `golangci-lint run` passed with 0 issues; `go mod tidy -diff` passed; `git diff --check` passed. Focused validation also passed for `go test ./internal/schema ./internal/inventree ./internal/tools` after the parameter-template detail endpoint and preflight fixes, `go test ./internal/tools` passed after the documentation follow-up, and `go test ./internal/inventree -run 'TestReadOnlyClientReads/parameter$' -count=1` passed after adding live parameter create/update/get-template coverage.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager reviews run. Initial reviews found that multi-parameter requests could partially write before a later clarification, duplicate same-template inputs could create duplicate part parameters, explicit `template_id` could bypass disabled-template refusal, clarification candidates lacked enabled/category-link/existing-value context, and task/docs metadata needed completion updates. Fixes added `GetParameterTemplate` with endpoint-manifest coverage, refused disabled or unlinked templates for both name and ID paths, split `set_part_parameters` into preflight and apply phases, rejected duplicate requested templates before writes, enriched template clarification candidates with enabled/category/default/existing-value details, and aligned plan/operator/tool docs. Focused Go and QA reruns found no remaining actionable findings. Focused product rerun found stale linked-template wording in docs; after the documentation follow-up, the narrow product rerun found no remaining actionable findings.
- Residual risk: `set_part_parameters` preflights all clarification and duplicate-template decisions before writing, but the apply phase is not transactional if an InvenTree API write succeeds and a later API write fails. Live Testcontainers coverage now exercises the underlying parameter create/update/get-template client methods, while the tool handler's orchestration and clarification branches remain covered with fake-client unit tests rather than an end-to-end live MCP tool call.
- Acceptance:
  - Searches templates, existing parameters, and category parameter links before writing.
  - Ambiguous template match asks the operator.
  - New template/category-link creation is refused unless a later explicit workflow is added.
  - Tool registration includes `inventree.write` scope tests.
  - HTTP registration is disabled or rejected until `M1C-S04` per-tool scope enforcement is complete.

Tasks:

- [x] Add parameter match logic.
- [x] Add `set_part_parameters`.
- [x] Add tests for disabled templates and same-name templates with different units/choices.
- [x] Add explicit empty/false/zero value tests.

### M1E-S03: Initial Stock Writes

- Status: `Done`
- Depends on: M1D-S02
- Scope: create initial stock item with duplicate detection.
- Validation:
  - `go test ./internal/inventree ./internal/tools` passed.
  - `go test ./...` passed.
  - `git diff --check` passed.
- Review:
  - Senior Go Developer review: no actionable findings.
  - Senior QA / Test Architect review found HTTP non-read-only exposure and stock-location filter coverage gaps; both were fixed, and focused QA rerereview reported no unresolved actionable findings.
  - Senior Product Manager review found registered tool-reference gaps for `create_stock_item` and operational scope wording; both were fixed, and focused product rerereview reported no unresolved actionable findings.
  - Senior Infosec review found the same HTTP exposure and scope-documentation gaps; both were fixed, and focused infosec rerereview reported no unresolved actionable findings.
- Residual risk: duplicate detection is a preflight guard using same part and location, not a transactional uniqueness guarantee if another writer creates matching stock between preflight and create. `M1G-S02` moved to `Ready` after `M1G-S01` completed.
- Acceptance:
  - Requires `inventree.operational` plus write scope.
  - Searches existing stock before creation.
  - Potential duplicate returns structured clarification.
  - HTTP registration is disabled or rejected until `M1C-S04` per-tool scope enforcement is complete.
  - Infosec review has no unresolved actionable findings before any operational HTTP tool is exposed.

Tasks:

- [x] Add `create_stock_item`.
- [x] Add duplicate detection.
- [x] Add operational scope tests.

## Milestone 1F: Uploads, Attachments, And Images

### M1F-S01: Upload Source Resolver

- Status: `Done`
- Depends on: M1A-S02
- Scope: implement inline bytes, STDIO allowlisted local paths, and URL fetch source handling.
- Validation:
  - `go test ./internal/upload` passed.
  - `go test ./...` passed.
  - `git diff --check` passed.
- Review:
  - Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Initial Go, QA, and Infosec findings found stale URL tests after secure-dialer changes, an unsafe arbitrary `http.Client` injection seam, missing bounded-read timeout coverage, non-default HTTPS URL allowlist handling during dial checks, and an undocumented local-file OS filesystem time-of-check/time-of-use residual. Product found incomplete validation evidence, pending-review wording, and unclear M1F-S02 remaining scope. Fixes removed arbitrary client injection, made URL fetches use a resolver-backed safe transport with request-scheme-aware dial checks, updated URL tests for allowlisted local listeners, added timeout coverage, documented the local-file residual risk, recorded full validation evidence, and narrowed M1F-S02 wording to the remaining attachment write/upload surface. Focused reruns for Go, QA, Infosec, and Product found no remaining actionable findings.
- Residual risk: STDIO local-path resolution on `afero.OsFs` still has an OS-level time-of-check/time-of-use race between symlink resolution/policy checks and open. Operators must configure local upload allowlisted roots as trusted, operator-controlled paths that untrusted users cannot write to.
- Acceptance:
  - HTTP mode rejects local paths before filesystem open/stat.
  - STDIO local path logic uses direct Afero in `internal/upload/local_file.go`.
  - URL fetcher enforces SSRF controls and never forwards auth headers.
  - Inline byte uploads, local file reads, and URL fetches enforce configured maximum sizes and bounded read time.
  - Redaction tests using `dvgoutils/logging/testhandler` prove auth tokens, uploaded bytes, sensitive local paths, and URLs with query secrets are not logged.
  - Infosec review has no unresolved actionable findings before URL or local-file upload sources are enabled.

Tasks:

- [x] Add inline byte source resolver.
- [x] Add STDIO local file source resolver with Afero.
- [x] Add URL fetcher interface and policy.
- [x] Add maximum-size and bounded-read enforcement.
- [x] Add upload redaction tests.
- [x] Add SSRF bypass table tests.
- [x] Add local path canonicalization and symlink tests.

### M1F-S02: Attachment Tools

- Status: `Done`
- Depends on: M1B-S02, M1F-S01
- Scope: implement upload, URL-copy, stored-link, metadata update, and delete attachment behavior for milestone object types. Attachment list/get/download reads are already registered and may be extended only where needed to support duplicate detection or write workflows.
- Validation:
  - `go test ./internal/inventree ./internal/tools ./cmd/inventree-mcp ./internal/config` passed.
  - `go test ./internal/tools ./internal/inventree` passed after review-finding fixes.
  - `go test ./internal/inventree ./internal/tools` passed after adding default-on Testcontainers coverage for the attachment write client methods and fixing live stored-link filename behavior.
  - `go test ./internal/tools ./internal/inventree` passed after normalizing stored-link duplicate-preflight filenames.
  - `go test ./internal/tools` passed after URL duplicate preflight normalization.
  - `go test ./docs` passed after task-status alignment.
  - `go test ./...` passed.
  - `go mod tidy -diff` passed.
  - `golangci-lint run` passed with 0 issues after fixing one staticcheck switch-style finding.
  - `git diff --check` passed.
- Review:
  - Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Initial findings requested bounded inline base64 size checks before decode, URL duplicate preflight before open-world fetch when filename is known, recipe wording split by source type, required content type clarification for inline/local uploads, explicit URL-shaped `local_path` guidance, local-path tool wiring coverage, and multipart filename/content-type header sanitization. Fixes added pre-decode max-byte checks, duplicate filename preflight before URL fetch, source-specific recipe wording, content-type and URL-intent clarifications, local-path and HTTP-mode tests, and multipart control-character/media-type validation.
  - Focused follow-up reviews found no remaining infosec findings. Go follow-up found URL duplicate preflight used the raw supplied filename before normalization; fixed by normalizing URL-provided filenames before duplicate checks and adding regression coverage. QA follow-up found the tool reference omitted the new `content_type` and URL-intent clarification retries; fixed in the attachment tool contract row. Product follow-up found missing STDIO upload configuration docs, stale planned-tool wording, and missing `delete_attachment` in the milestone plan inventory; fixed in the operator recipes, tool reference, and plan. Operator follow-up required integration tests for all client methods; fixed by documenting the rule and exercising attachment upload, stored-link create, metadata update, and delete client methods against the default-on Testcontainers InvenTree suite. The live test exposed that InvenTree 1.4.0 rejects custom filename fields on stored-link creation; fixed by treating link filenames as duplicate-preflight-only metadata and documenting that InvenTree assigns stored-link filename metadata. Final follow-up reviews requested mandatory integration-test wording in the plan, stored-link filename clarification in the tool-reference glossary, and normalized stored-link filename duplicate preflight; fixes added those docs and regression coverage. Final Go and QA reruns found no remaining actionable findings.
- Residual risk: none.
- Acceptance:
  - Existing `list_attachments`, `get_attachment_metadata`, and `download_attachment` behavior remains registered and may be reused or extended only as needed for duplicate detection and attachment write workflows.
  - `upload_attachment` accepts inline bytes and STDIO allowlisted paths only.
  - `upload_attachment_from_url` is the only URL-fetch upload tool and has `openWorldHint:true`.
  - `create_link_attachment` stores links without fetching.
  - `update_attachment_metadata` requires a stable attachment ID and uses PATCH-compatible partial updates.
  - Attachment write client methods have default-on Testcontainers integration coverage against the real InvenTree API.
  - Duplicate attachment behavior returns structured clarification unless intent is explicit.
  - Tool registration includes `inventree.upload` scope tests and `inventree.destructive` tests for delete.
  - HTTP registration is disabled or rejected until `M1C-S04` per-tool scope enforcement is complete.
  - Infosec review has no unresolved actionable findings before upload tools are exposed over HTTP.

Tasks:

- [x] Add attachment client methods.
- [x] Add attachment client method integration coverage.
- [x] Add `upload_attachment`.
- [x] Add `upload_attachment_from_url`.
- [x] Add `create_link_attachment`.
- [x] Add `update_attachment_metadata`.
- [x] Add `delete_attachment` behind `confirm:true`.

### M1F-S03: Primary Part Image

- Status: `Done`
- Depends on: M1F-S02
- Scope: implement part primary image download and assignment/replacement.
- Validation: `go test ./internal/inventree ./internal/tools ./internal/server ./docs` passed with Docker-backed InvenTree client integration coverage; `go test ./...` passed; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run because this adds primary-image write behavior, upload/download safety behavior, tool-surface registration, and operator-facing docs. Initial Go review found `set_primary_image` used the generic download limit instead of the configured upload limit before re-uploading bytes; fixed by using the configured upload cap and adding regression coverage. QA and product found missing filename output for `download_part_image`, stale task status, missing first-assignment coverage, and live replacement coverage that did not prove the public same-part attachment workflow with distinct bytes; fixed with filename derivation, task/doc updates, a first-assignment assertion, and live replacement coverage that downloads distinct uploaded attachment bytes before patching the part image. Product also found stale prompt wording that still described replacement as planned; fixed in the registered prompt text. Infosec found non-2xx media fetch responses could surface raw response body text; fixed with generic redacted media-fetch errors and regression coverage. Focused Go, QA, product, and infosec reruns found no remaining actionable findings; a final narrow product rerun after the tool-reference filename row fix also found no actionable findings.
- Residual risk: `set_primary_image` validates the image attachment metadata before downloading and re-uploading it, so a normal attachment-change race remains possible between preflight and download; the final content fetch is still scoped through the InvenTree attachment download path and replacement still requires `confirm:true`.
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

- [x] Verify part image endpoint behavior against `docs/api-schema.yaml`.
- [x] Verify `/api/part/{id}/` image fields and `/api/part/thumbs/{id}/` behavior, and document which endpoint `set_primary_image` uses.
- [x] Add part image download and update client methods.
- [x] Add `download_part_image`.
- [x] Add `set_primary_image`.
- [x] Add no-image, present-image, too-large, URL-scope, first-assignment, and replacement tests.

## Milestone 1G: Workflow Tools And Prompts

### M1G-S01: Part Upsert Workflow

- Status: `Done`
- Depends on: M1E-S01, M1E-S02
- Scope: safer multi-step workflow for adding/updating a purchasable part with supplier/manufacturer data.
- Validation:
  - `go test ./internal/tools` passed.
  - `go test ./internal/tools ./docs` passed after review follow-ups.
  - `go test ./...` passed.
  - `golangci-lint run` passed with 0 issues.
  - `go mod tidy -diff` passed.
  - `git diff --check` passed.
- Review:
  - Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec reviews run because this adds a mutating workflow tool.
  - Initial reviews found non-dry-run could partially write before later clarification, a single name-matched part ignored supplied update fields, negative explicit workflow IDs could fall back to lookup/create paths, dry-run docs overpromised stable records for planned creates, task status metadata was stale, and the status-sync rule lacked executable coverage.
  - Fixes added non-dry-run dry-run preflight before writes, patched single name matches with supplied part fields, rejected negative explicit IDs with clarification, clarified dry-run docs, completed task metadata, and added a docs test that enforces Task Index and story-local status sync.
  - Focused Senior Infosec rerun found no actionable issues in the preflight-before-write safety fix or HTTP write-tool boundary.
- Residual risk: duplicate/preference checks remain preflight guards rather than transactional guarantees if another writer creates or changes matching records between lookup and write.
- Acceptance:
  - Supports `dry_run`.
  - Prefers existing records.
  - Returns stable IDs and omitted recommended fields.
  - Remains behind the existing write-tool HTTP registration boundary until `M1C-S04` scope enforcement is complete.

Tasks:

- [x] Add workflow planner.
- [x] Add `upsert_part_with_supplier_and_manufacturer`.
- [x] Add dry-run no-write tests.

### M1G-S02: Initial Stock And Purchase Preview Workflows

- Status: `Done`
- Depends on: M1D-S02, M1E-S03, M1G-S01
- Scope: finish the useful operator loop with initial stock and no-write purchase preview.
- Validation: `go test ./internal/inventree ./internal/tools ./internal/schema ./docs` passed; `go test ./internal/tools` passed after supplier consistency follow-up; `go test ./internal/tools ./docs` passed after review fixes; `go test ./... && git diff --check` passed before review and again after review fixes.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer subagent reviews run. Initial review found stale task completion notes, purchase-preview recipe wording that overpromised purchasability/package checks, missing stable-ID validation in the initial-stock workflow, missing direct supplier-part contradiction checks, and a missing milestone matrix row for `create_initial_stock_entry`. Fixes narrowed the recipe wording, resolved stable part/location IDs with `GetPart` and `GetStockLocation`, rejected negative/mismatched direct supplier-part identity fields, documented the new operational workflow in the milestone matrix, and added deterministic tests. Focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: initial stock duplicate detection remains a preflight guard rather than a transactional uniqueness guarantee if another writer creates matching stock between the duplicate search and create. Purchase preview validates supplier-part identity, single-supplier consistency, positive quantity, and price/currency pairing, but it intentionally does not validate supplier price breaks, package multiples, or minimum-order constraints because those fields are not modeled for this milestone.
- Acceptance:
  - Purchase preview performs no writes.
  - Supplier-part validation is explicit.
  - Ambiguous supplier/part/quantity data asks the operator.

Tasks:

- [x] Add `preview_purchase_order_with_lines`.
- [x] Add initial stock workflow helper.
- [x] Add purchase preview no-write tests.

### M1G-S03: Milestone Prompts

- Status: `Done`
- Depends on: M1D-S01
- Scope: add milestone 1 prompts and prompt contract tests.
- Validation: `go test ./internal/tools ./internal/server ./docs` passed; `go test ./... && git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager reviews run because this adds MCP prompt behavior and operator-facing documentation. Initial review found protocol-boundary prompt tests covered only one prompt and did not assert future prompt fetch failures, and attachment/image wording could steer operators toward planned M1F write tools before registration. Fixes added table-driven MCP prompt fetch checks, negative future prompt fetch checks, current-versus-planned attachment/image wording, a separate planned M1F attachment/image tool table, and current/planned operator recipe sequences. Focused QA and product reruns found no remaining actionable prompt behavior findings; QA also flagged the new prompt files must be included in publication, which is addressed by staging the complete change.
- Residual risk: prompt checklists are static guidance and do not inspect live InvenTree state; tool handlers remain responsible for enforcing clarification, dry-run, duplicate, and write-boundary contracts.
- Acceptance:
  - Prompts are marked `milestone_1`.
  - Future prompts remain hidden or marked future.
  - Prompts prefer clarification/dry-run over guessing.

Tasks:

- [x] Add `new_part_entry_checklist`.
- [x] Add `parameter_reuse_checklist`.
- [x] Add `attachment_image_checklist`.
- [x] Add `initial_stock_entry_checklist`.
- [x] Add `purchase_preview_checklist`.
- [x] Add prompt manifest tests.

## Milestone 1H: Integration Happy Paths

### M1H-S03: Milestone Integration Happy Paths

- Status: `Done`
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

- [x] Add catalog and initial stock happy path.
- [x] Add supplier/manufacturer purchase preview happy path.
- [x] Add inline/local-path attachment readback tests.
- [x] Add live `download_attachment` original-mode content, hash, size, in-scope matrix, and max-byte limit tests; retain existing unit coverage for attachment thumbnail-mode, out-of-scope model type, redirect, and lower-level limit behavior.
- [x] Add live `download_part_image` original-mode content, thumbnail-mode behavior, hash, size, no-image, and max-byte limit tests; keep existing unit coverage for write-only `existing_image` exclusion and lower-level limit behavior.
- [x] Add URL upload readback tests.
- [x] Add link attachment tests.
- [x] Add primary image tests.
- [x] Add sales/customer and deferred file-surface boundary integration test.

Validation:

- `go test ./...` passed.
- `go test ./internal/testenv ./internal/tools` passed after restoring descriptive long test names and addressing review findings, proving hashed Testcontainers run prefixes avoid InvenTree field-length failures.
- `INVENTREE_TEST_SKIP_DOCKER=1 go test ./internal/testenv ./internal/tools`, `GOFLAGS=-trimpath go test -race ./internal/testenv -run TestStartInvenTreeStack -count=1`, and `go test ./docs` passed after the CI timeout follow-up; `GOFLAGS=-trimpath go test -v -race -covermode atomic -coverprofile /tmp/inventree-full.cov -coverpkg ./... ./...` passed locally before the timeout increase, while the first GitHub `test` run on the pushed branch timed out multiple concurrent InvenTree stacks at the prior 3-minute startup deadline. GitHub CI then passed on the timeout follow-up with `test` in 3m59s, `gremlins` in 13m36s, `lint`, and `goreleaser-snapshot`.

Review:

- Initial Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer findings addressed: status no longer marked `Done` before review completion; raw test names feed Testcontainers run-prefix hashes; purchase-order setup moved into `internal/testenv`; attachment matrix subtests use isolated runs/accounts/fixtures; live tool-level max-bytes, deferred model type, and local-path negative checks were added.
- Focused rerun review completed. Senior Product Manager and Senior Infosec Reviewer final rereads reported no actionable findings after docs/status wording fixes. Senior Go Developer and Senior QA / Test Architect follow-up findings were addressed before the final docs/security reread.
- Manual follow-up renamed shortened subtest labels after run-prefix hashing made descriptive test names safe. A CI timeout follow-up raised the default Testcontainers startup deadline from 3 minutes to 5 minutes after the new default-on milestone suite made GitHub start multiple InvenTree stacks concurrently.
- Focused Senior Go Developer review found no actionable findings in the timeout default change. Focused Senior QA / Test Architect review requested Docker-backed validation evidence for the timeout follow-up; addressed with the focused `TestStartInvenTreeStack` race run and a passing GitHub CI rerun.

Residual risk:

- none.

### M1H-S04: Delete Attachment Confirmation Clarification

- Status: `Done`
- Depends on: M1F-S02, M1H-S03
- Scope: fix the MCP protocol-boundary regression where calling `delete_attachment` without `confirm:true` returned a tool error before the handler could return the structured confirmation clarification promised by the tool contract.
- Validation: `go test ./internal/tools -run TestDeleteAttachmentMissingConfirmReturnsStructuredClarificationThroughMCP -count=1` passed; `GOFLAGS=-trimpath go test ./internal/tools -run 'TestMilestoneHappyPathToolsAgainstInvenTree/delete_attachment_missing_confirm_returns_structured_clarification_through_mcp' -count=1` passed against Docker-backed Testcontainers InvenTree; `INVENTREE_TEST_SKIP_DOCKER=1 go test ./internal/tools ./docs` passed; `go test ./internal/server ./internal/inventree` passed; `INVENTREE_TEST_SKIP_DOCKER=1 go test ./...` passed; `git diff --check` passed.
- Review: Focused Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager subagent reviews found no actionable findings. Go noted residual risk is limited to broader MCP SDK schema behavior outside the covered `delete_attachment` missing-confirm path. QA noted the live MCP-boundary proof depends on Docker/Testcontainers being enabled, with the in-memory MCP regression covering the same protocol behavior when Docker is skipped. Product noted the diff preserves the intended operator contract.
- Residual risk: none for the fixed `delete_attachment` missing-confirm path.
- Acceptance:
  - Omitting `confirm` from `delete_attachment` reaches the handler and returns structured `clarification_required` output with retry `confirm`.
  - `confirm:true` remains required before any attachment delete occurs.
  - A Docker-backed integration regression covers the missing-confirm path against a real Testcontainers InvenTree attachment through an MCP client session.
  - Tool reference and operator recipe semantics remain unchanged: destructive delete still requires explicit confirmation.

Tasks:

- [x] Make `confirm` optional in the input schema while preserving destructive behavior only for `confirm:true`.
- [x] Add an MCP protocol-boundary regression test.
- [x] Add a Testcontainers integration regression for the live missing-confirm path.
- [x] Run validation and review.

## Milestone 1I: Documentation And Release Readiness

### M1I-S01: Operator Docs Finalization

- Status: `Done`
- Depends on: M1G-S03, M1F-S03
- Scope: finalize README links, operator recipes, and tool reference from implemented behavior.
- Acceptance:
  - README stays concise and links to recipes.
  - `docs/tool-reference.md` matches generated manifest.
  - `docs/operator-recipes.md` includes first-release workflows.

Tasks:

- [x] Add generated or checked tool manifest.
- [x] Update tool reference from manifest.
- [x] Update operator recipes from implemented tools.
- [x] Add README quick-start links.

Validation:

- `go test ./internal/server ./internal/tools ./docs` passed.
- `git diff --check` passed.

Review:

- Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager subagent reviews run. Go found no actionable issues. Product found the completion metadata was premature and the OAuth/reverse-proxy recipes read like implemented HTTP OAuth workflows; fixed by recording final notes only after review and labeling those recipes as future/post-`M1C` work with milestone 1 limitations. QA found manifest drift checks were not tied to actual registered tools and were too loose for row-local docs fields; fixed by checking `tools/list` output against manifest-derived expectations and comparing tool-reference table cells exactly for class, scopes, upload sources, MCP annotations, and HTTP registration. Focused reruns found no remaining actionable findings.

Residual risk:

- none.

### M1I-S02: Final Review Panel

- Status: `Ready`
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

### F-S01: Evaluate Docker Compose Testcontainers Stack

- Status: `Future`
- Depends on: M1H-S01
- Scope: evaluate whether `github.com/testcontainers/testcontainers-go/modules/compose` can replace or complement the hand-wired InvenTree Testcontainers stack by using official InvenTree Docker Compose files plus test-specific overrides.
- Acceptance:
  - Compare compose-based startup against the current `internal/testenv` stack for startup time, log visibility, cleanup behavior, loopback-only published ports, readiness checks, and deterministic token creation.
  - Determine whether the official compose topology starts all backend services needed for realistic MCP integration tests without introducing unnecessary CI cost.
  - Document whether compose should replace the current stack, become an optional canary/compatibility path, or be rejected with reasons.

Tasks:

- [ ] Identify the official InvenTree compose files and required test overrides for pinned `inventree/inventree:1.4.0`.
- [ ] Prototype a local compose stack using `testcontainers-go/modules/compose`.
- [ ] Verify service logs, `ServiceContainer` inspection, endpoint discovery, and `Down` cleanup semantics.
- [ ] Compare findings with the current direct-container `internal/testenv` implementation.

### F-S02: BOM Import Workflow

- Status: `Future`
- Depends on: milestone 1 complete and product review

Tasks:

- [ ] Define BOM import behavior.
- [ ] Implement structured row validation.
- [ ] Add dry-run and row-level error tests.

### F-S03: Purchase Order Write And Receiving

- Status: `Future`
- Depends on: milestone 1 complete and product review

Tasks:

- [ ] Define purchase order creation workflow.
- [ ] Define receiving workflow.
- [ ] Add operational/destructive scope review.

### F-S04: Build Order Workflows

- Status: `Future`
- Depends on: milestone 1 complete and product review

Tasks:

- [ ] Define build create/allocate/complete behavior.
- [ ] Add stock consumption safety model.
- [ ] Add integration tests.

### F-S05: Stocktake Adjustments

- Status: `Future`
- Depends on: milestone 1 complete and product review

Tasks:

- [ ] Define stocktake review workflow.
- [ ] Add confirmation and audit requirements.
- [ ] Add operational scope tests.

### F-S06: Systemd Notify And Watchdog Support

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
