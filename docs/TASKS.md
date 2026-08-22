# Implementation Tasks

This backlog turns [PLAN.md](PLAN.md) into executable work. Status values are:

- `Done`: acceptance criteria met, validation/review recorded, and committed; when a task-status update is part of the current change, `Done` means ready for the same commit.
- `Active`: implementation has started on a named branch; the linked issue carries matching active progress context and is assigned when repository permissions allow it.
- `Ready`: actionable with current information.
- `Active`: implementation is in progress on an assigned story.
- `Blocked`: needs an explicit decision, external verification, or prerequisite task.
- `Planned`: valid work, but should wait for dependencies.
- `Future`: outside the first beta milestone.

Each story should be completed with tests, documentation updates, and reviewer follow-up. Code, behavior, task-status, operator workflow, or public documentation-contract changes require subagent review from the applicable roles in [reviewers.md](reviewers.md). Use the full Go, QA, product, and infosec panel when acceptance criteria touch auth, upload, Testcontainers, tool-surface behavior, or milestone completion. Manual-only review is reserved for typo-only or formatting-only documentation edits and must say why subagent review was not required.

When selecting the next story, update the Codex thread title to include the story ID and short title. If the active story changes, update the thread title again before continuing. When implementation starts, set both task status surfaces to `Active`; for a linked issue, also set its status context to `Active`, assign it to the operator when permitted, and record the branch plus concise progress context without closing the issue.

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

When a story has an `Issue:` link, keep the GitHub issue aligned with changes to the story's title, status, dependencies, scope, acceptance criteria, and task checklist, and add progress, validation, review, or residual-risk context when it is relevant to the handoff. Ask the operator before creating any GitHub issue for a story, then add the resulting `Issue:` link to the story. Do not close a linked issue merely because the story is marked `Done` locally or a PR is opened; close it through an accurate closing reference in the implementing PR when that PR is merged, or close it only after the merge is verified. If the merged PR does not complete the issue scope, keep the issue open and update it with the remaining work.

When updating an already-pushed branch or existing PR, prefer fresh follow-up commits over amending or force-pushing. Rewrite published history only for an explicit operator request or a concrete repository hygiene issue, and use `--force-with-lease` when a rewrite is unavoidable. Keep existing PR titles, descriptions, checklists, validation notes, review summaries, residual risks, and follow-up lists current whenever follow-up commits change the branch scope or status. Prefer squash merge when merging PRs unless the operator or repository policy requires another strategy.

Remove draft status once the PR is ready for human review: all automated or subagent review feedback has been addressed or explicitly documented, required rerun reviews are complete, the PR title/body/checklist are current, and the pipeline has passed on the latest pushed commit. Do not mark the PR ready while CI is pending, failing, or stale for an older head SHA.

Before `M1C-S04` is complete, mutating, operational, destructive, and upload tools may be registered only on STDIO or in unit-test registries. HTTP registration must filter them out of the exposed tool manifest until per-tool scope enforcement is implemented and tested.

Before assigning a new story ID, inspect `git worktree list --porcelain`, search every registered worktree's `docs/TASKS.md` for unpublished IDs and linked issue numbers, and search both open and closed GitHub issue titles for task IDs already reserved remotely, using an exact title-qualified query such as `F-S34 in:title` so dependency mentions do not look like reservations. Choose an ID unused across all three surfaces. After the operator approves issue creation and the ID is assigned, create or update the issue title with that ID. Before recording its link, refresh the registered-worktree search, verify the newly returned issue number is not already linked to a different unpublished task, and repeat the exact title-qualified task-ID search across open and closed issues. If a concurrent issue or worktree now owns the ID, stop and reconcile the collision, preserving the earliest established reservation; otherwise immediately add the `Issue:` link here before continuing task planning so other agents can see the reservation.

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
| [M1C-S01](#m1c-s01-mcp-sdk-auth-spike) | Spike official MCP SDK auth behavior for HTTP. | Done |
| [M1C-S02](#m1c-s02-chatgpt-connector-compatibility-spike) | Verify ChatGPT connector OAuth compatibility. | Done |
| [M1C-S03](#m1c-s03-oauth-envelope-and-code-storage) | Implement OAuth token envelopes and auth-code storage. | Done |
| [M1C-S04](#m1c-s04-scope-guard-and-credential-propagation) | Enforce per-tool OAuth scopes and credential propagation. | Done |
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
| [M1I-S02](#m1i-s02-final-review-panel) | Run final Go, QA, product, and infosec review panel. | Done |
| [F-S01](#f-s01-evaluate-docker-compose-testcontainers-stack) | Evaluate Docker Compose-based Testcontainers stack. | Done |
| [F-S02](#f-s02-bom-import-workflow) | BOM import workflow. | Blocked |
| [F-S03](#f-s03-purchase-order-write-and-receiving) | Purchase order write and receiving. | Done |
| [F-S04](#f-s04-build-order-workflows) | Build order workflows. | Blocked |
| [F-S05](#f-s05-stocktake-adjustments) | Stocktake adjustments. | Done |
| [F-S06](#f-s06-systemd-notify-and-watchdog-support) | Native systemd notification support for packaged HTTP deployments. | Done |
| [F-S07](#f-s07-production-http-oauth-startup) | Wire production HTTP startup to OAuth configuration and server dependencies. | Done |
| [F-S08](#f-s08-chatgpt-connector-oauth-setup-flow) | Implement ChatGPT connector authorization, token, and setup-page flow. | Done |
| [F-S09](#f-s09-reverse-proxy-canonical-url-enforcement) | Enforce public issuer/resource URLs behind a trusted reverse proxy. | Done |
| [F-S10](#f-s10-packaged-http-deployment-and-live-connector-validation) | Validate packaged HTTP deployment and live ChatGPT connector setup. | Future |
| [F-S11](#f-s11-parameter-template-administration) | Administer parameter templates and safe template merges. | Done |
| [F-S12](#f-s12-global-parameter-value-search-and-delete) | Search parameter values across inventory and delete individual rows safely. | Done |
| [F-S13](#f-s13-category-parameter-defaults) | Manage category parameter defaults using existing templates. | Done |
| [F-S14](#f-s14-bulk-parameter-propagation-and-audit-workflows) | Add dry-run bulk parameter propagation and consistency audits. | Done |
| [F-S15](#f-s15-live-order-entry-tool-hardening) | Close gaps found during live order-entry use of the MCP tools. | Done |
| [F-S16](#f-s16-mcp-go-sdk-v17-and-2026-07-28-protocol-adoption) | Adopt MCP Go SDK v1.7 and the MCP 2026-07-28 protocol safely. | Done |
| [F-S17](#f-s17-native-mcp-elicitation-for-structured-clarifications) | Add native MCP elicitation while preserving structured clarification fallback. | Planned |
| [F-S18](#f-s18-local-cli-self-update) | Add an explicit local CLI self-update workflow for direct binary installs. | Done |
| [F-S19](#f-s19-part-category-administration) | Add guarded part-category retrieval, creation, and editing. | Done |
| [F-S20](#f-s20-company-and-sourcing-link-maintenance) | Add exact reads and guarded maintenance for companies and sourcing links. | Done |
| [F-S21](#f-s21-stock-location-and-stock-record-administration) | Add stock-location administration and constrained stock-record maintenance. | Done |
| [F-S22](#f-s22-reviewable-dry-run-mutation-plans) | Expose the effective field-level mutations behind workflow dry runs. | Done |
| [F-S23](#f-s23-purchase-order-extra-line-items) | Add guarded purchase-order extra-line administration and combined-workflow support. | Done |
| [F-S24](#f-s24-guarded-delete-on-deplete-stock-depletion) | Intentionally deplete and delete one safe delete-on-deplete stock item. | Done |
| [F-S25](#f-s25-inventree-tool-and-server-icons) | Brand MCP tool calls and server identity with the official InvenTree icon. | Done |
| [F-S26](#f-s26-mcp-functionality-gap-guidance) | Guide consuming agents to surface untracked MCP functionality gaps for operator-approved issue creation. | Done |
| [F-S27](#f-s27-guarded-full-stock-item-transfer) | Move one complete safe stock item to an explicit valid destination. | Done |
| [F-S28](#f-s28-partial-stock-item-transfer-and-split-recovery) | Add partial transfers after split identity and recovery semantics are approved. | Done |
| [F-S29](#f-s29-reviewed-multi-item-stock-transfer-batches) | Add reviewed transfer batches after atomicity and failure semantics are verified. | Planned |
| [F-S30](#f-s30-clarify-endpoint-specific-model-type-contracts) | Distinguish attachment and parameter endpoint `model_type` vocabularies without changing behavior. | Done |
| [F-S31](#f-s31-guarded-company-primary-images) | Add guarded upload, replacement, verification, and supported removal of company primary images. | Done |
| [F-S32](#f-s32-guarded-purchase-order-line-deletion) | Add guarded deletion of one unreceived ordinary purchase-order line, distinct from extra-line deletion. | Done |
| [F-S33](#f-s33-guarded-part-deletion) | Add guarded deletion of one unreferenced ordinary part, refusing while the part is active or stock, BOM, build, purchase-order, sales-order, variant, supplier/manufacturer-part, parameter, attachment, or related-part references exist. | Done |
| [F-S34](#f-s34-canonical-user-facing-inventree-object-web-urls) | Return canonical user-facing InvenTree web links across supported object outputs. | Done |
| [F-S35](#f-s35-local-upload-policy-discovery) | Let local agents discover STDIO upload roots and recover safely from allowlist rejections. | Done |
| [F-S36](#f-s36-versioned-outbound-http-user-agent) | Identify every outbound runtime HTTP request with the MCP server name and build version. | Done |
| [F-S37](#f-s37-restore-default-web-prefix-in-canonical-object-links) | Restore InvenTree's default `/web` frontend mount in fallback-generated object links. | Done |
| [F-S38](#f-s38-explicit-purchase-order-completion-after-receiving) | Complete fully received purchase orders explicitly during receipt or later. | Done |
| [F-S39](#f-s39-preserve-complete-external-urls) | Preserve complete functional external URLs while rejecting credentials and retaining safe error redaction. | Done |
| [F-S40](#f-s40-complete-part-exact-reads-and-scalar-maintenance) | Expose complete exact part records and align ordinary scalar create/update fields with verified serializer behavior. | Done |
| [F-S41](#f-s41-guarded-part-revision-and-variant-relationships) | Add guarded assignment, replacement, and clearing of part revision and variant family relationships. | Done |
| [F-S42](#f-s42-related-part-link-administration) | Expose normal related-part reads and guarded create, update, and delete operations. | Done |
| [F-S43](#f-s43-sourcing-link-detail-completeness) | Complete supplier/manufacturer-part exact reads and long-note maintenance while retaining concise searches. | Done |
| [F-S44](#f-s44-company-detail-and-role-completeness) | Complete exact company reads and guarded contact, tax, link, and customer-role maintenance. | Done |
| [F-S45](#f-s45-stock-item-detail-completeness) | Expose complete high-value stock-item exact-read fields while retaining concise searches and guarded mutation boundaries. | Done |
| [F-S46](#f-s46-stock-tracking-and-stocktake-history) | Expose bounded stock tracking events and historical part stocktake snapshots through normal read-only tools. | Done |
| [F-S47](#f-s47-purchase-order-and-line-detail-completeness) | Complete exact purchase-order and ordinary-line reads plus standalone order metadata and external-link maintenance. | Done |
| [F-S48](#f-s48-owner-discovery-and-cross-object-responsibility) | Discover InvenTree owners and support consistent guarded responsibility assignment across applicable objects. | Done |
| [F-S49](#f-s49-structured-contact-and-address-references) | Discover structured company contacts and addresses and support guarded assignment on applicable objects. | Done |
| [F-S50](#f-s50-project-code-discovery-and-assignment) | Discover existing project codes and support consistent guarded assignment across purchase-order records. | Done |
| [F-S51](#f-s51-guarded-delete-on-deplete-policy-updates) | Add a reviewed workflow for enabling or disabling delete-on-deplete behavior on one stock item. | Done |
| [F-S52](#f-s52-stock-serial-number-management) | Add dedicated discovery and guarded mutation workflows for stock serial numbers. | Done |
| [F-S53](#f-s53-guarded-stock-provenance-correction) | Add reviewed correction of supplier, purchase-order, and purchase-price provenance on eligible stock items. | Done |
| [F-S54](#f-s54-stock-install-and-uninstall-workflows) | Add dedicated guarded workflows for parent/child stock installation relationships. | Done |
| [F-S55](#f-s55-barcode-workflow-discovery) | Investigate safe barcode presence, generation, resolution, assignment, removal, and scan-history tooling. | Future |
| [F-S56](#f-s56-cross-object-tag-workflow-discovery) | Investigate tag discovery and consistent assignment/removal across supported object types. | Future |
| [F-S57](#f-s57-part-testing-workflow-discovery) | Investigate part test templates, stock-item test results, attachments, and safe workflow boundaries. | Future |
| [F-S58](#f-s58-pricing-and-price-break-workflow-discovery) | Investigate part pricing, supplier price breaks, internal prices, sale prices, currencies, and safe tool boundaries. | Future |
| [F-S59](#f-s59-part-requirements-visibility-discovery) | Investigate build and order demand visibility through the part requirements API. | Future |
| [F-S60](#f-s60-stocktake-generation-and-reporting-discovery) | Investigate guarded generation of part stocktake snapshots and reports after history reads exist. | Active |
| [F-S61](#f-s61-adopt-inventree-150-api-530-baseline) | Adopt InvenTree 1.5.0 and API 530 as the blocking compatibility baseline. | Done |
| [F-S62](#f-s62-guarded-purchase-order-hold-resume-and-cancellation) | Add explicit current-state-planned hold, resume, and cancellation workflows without generic status editing or whole-order deletion. | Done |
| [F-S63](#f-s63-guarded-purchase-order-duplication-discovery) | Investigate a deferred, low-frequency workflow for safely duplicating selected purchase-order state. | Future |
| [F-S64](#f-s64-cross-object-generic-parameter-values-and-uniqueness) | Add bounded generic parameter values across supported non-part-row object types and administer template uniqueness. | Done |
| [F-S65](#f-s65-guarded-stock-custom-status-management) | Extend the guarded stock-status workflow to assign or clear compatible custom status keys. | Blocked |
| [F-S66](#f-s66-guarded-stock-item-merge-discovery) | Investigate a deferred, fail-closed merge workflow for explicitly selected compatible stock items. | Future |
| [F-S67](#f-s67-stock-location-detail-and-type-administration) | Complete stock-location icon detail and add guarded stock-location-type administration. | Done |
| [F-S68](#f-s68-guarded-stock-location-deletion) | Delete one empty, unreferenced stock location only through a complete fail-closed dependency plan. | Done |
| [F-S69](#f-s69-guarded-part-category-deletion) | Delete one empty, unreferenced leaf part category only through a complete fail-closed dependency plan. | Done |
| [F-S70](#f-s70-inventree-version-compatibility-table) | Add a README table mapping inventree-mcp versions to tested InvenTree versions, kept in sync via a fenced anchor, drift test, and release gate. | Done |
| [F-S71](#f-s71-inventree-instance-info-tool) | Add a read-only InvenTree instance-info tool, gated on an operator-approved curated settings allowlist. | Done |
| [F-S72](#f-s72-porcelain-style-version-cli-format-and-self-update-rewrite) | Replace the CLI `version` output with a versioned porcelain-style format and move self-update onto it, accepting one documented one-time breaking migration. | Done |
| [F-S73](#f-s73-remove-gremlins-mutation-testing-ci-job) | Remove the Gremlins mutation-testing job from CI; keep `.gremlins.yaml` for optional manual runs. | Done |
| [F-S74](#f-s74-guarded-stocktake-generation-and-reporting) | Implement guarded stocktake generation and reporting after F-S60 discovery resolves asynchronous task and report behavior. | Active |

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
- Validation: `go test ./...` passed with workspace-local Go build cache before commit. Follow-up CI alignment with `dvgoutils` added Go coverage reporting after `go test -coverpkg=./... -coverprofile=/private/tmp/inventree-mcp-coverage.out ./...` passed and reported 82.6% total coverage. Coverage badge follow-up passed YAML load validation for CI configs, the same cached Go test and coverage commands, and `git diff --check`. CI cache follow-up persisted the Gremlins and test jobs' Go build and module caches using keys based on runner OS/arch, Go version, and module dependency hash so Go's own cache can invalidate changed packages internally. The follow-up passed GitHub workflow YAML load validation, `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/go.yml`, and `git diff --check`. PR #21 CI passed lint, test, and Gremlins after the dependency-keyed cache change; the first run saved a 195 MB `test-go` cache, restored Gremlins from the prior source-hash cache through a restore key, and saved the new dependency-keyed `gremlins-go` cache. The next run restored both exact final keys before passing with `test` in 27s and Gremlins in 2m36s. Dependabot version-update follow-up passed Ruby YAML load validation for `.github/dependabot.yml` and all workflow YAML files, plus `git diff --check`.
- Review: covered by earlier planning review passes. Follow-up CI alignment reviewed by Senior QA / Test Architect and Senior Product Manager. QA found workflow-level write permissions were too broad for the gremlins job; fixed by moving write permissions to the test job and leaving gremlins read-only. Product found the omitted `dvgoutils` gist-backed badge needed to be explicit; fixed in README setup notes. Focused QA and product reruns found no remaining actionable findings. Coverage badge follow-up was manually reviewed as a narrow completion of the already-reviewed explicit product gap; no subagent rerun was required because it only replaces the documented omitted badge with the configured gist ID and secret note. Gremlins Go-cache follow-up received focused Senior QA / Test Architect and Senior Product Manager reviews with no unresolved actionable findings. Test-job dependency-keyed Go-cache follow-up received focused Senior QA / Test Architect and Senior Product Manager reviews with no unresolved actionable findings. Dependabot version-update follow-up was manually reviewed against the current GitHub Dependabot options reference because the available subagent tooling is limited to explicitly requested delegation; no code paths or task status changed.
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
  - Tool annotation helper tests cover the current SDK's explicit false wire behavior for all annotation hints.

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
- InvenTree 1.4.3 maintenance validation: `GOFLAGS=-trimpath go test -race ./internal/testenv -run '^TestStartInvenTreeStack$' -count=1` passed and confirmed runtime InvenTree `1.4.3` / API `511`; `GOFLAGS=-trimpath go test -race -p=1 ./...` passed with all default-on Docker suites; `golangci-lint run` reported 0 issues; `GOFLAGS=-trimpath go mod tidy -diff` and `git diff --check` passed.
- InvenTree 1.4.3 maintenance review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. QA and infosec found that the initial documentation overstated schema equivalence from the API-version assertion; wording now limits the evidence to runtime/API version and exercised client contracts. Product found ambiguity about minimum-version support and stale issue #46 metadata; docs now identify `1.4.3` only as the blocking integration-test baseline, and the linked issue checklist uses the current pin. Focused QA, product, and infosec follow-up reviews found no remaining actionable findings. This review-bookkeeping-only update did not require another reviewer rerun.
- InvenTree 1.4.3 maintenance residual risk: the checked-in schema was fetched from InvenTree `1.4.0`; runtime/API `511` verification and the blocking integration suite do not prove byte-for-byte schema identity or compatibility for unexercised endpoints. The readable `inventree/inventree:1.4.3` tag is intentionally not digest-pinned and could be republished upstream.

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

- Status: `Done`
- Depends on: M1A-S03
- Scope: prove official MCP SDK `auth`/`oauthex` behavior against the planned HTTP architecture.
- Acceptance:
  - Uses the reviewed SDK baseline (initially `v1.6.1`, upgraded by F-S16 to `v1.7.0`) or records upgrade findings.
  - Confirms `auth.RequireBearerToken`, `auth.RequireBearerTokenOptions`, `auth.TokenVerifier`, and `auth.TokenInfoFromContext` behavior.
  - Proves token info or selected credential carrier reaches tool handlers under stateless HTTP.
  - Updates plan if SDK API assumptions are wrong.

Tasks:

- [x] Add spike tests around HTTP handler auth middleware.
- [x] Verify `TokenVerifier` signature.
- [x] Verify context propagation into `CallTool`.
- [x] Document results in `docs/PLAN.md`.

- Validation: `go test ./internal/server ./docs`; `go test -race ./internal/server`; `go test ./...`; `git diff --check`.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior InfoSec Engineer reviews run. Initial reviews found missing invalid/expired/insufficient-scope SDK failure cases, missing official source links for connector assumptions, unclear first-pass client registration behavior, and missing tool-level auth UI tracking. Follow-up added failure-mode tests, official OpenAI docs links, CIMD public-client `none` as the initial registration model, and M1C-S04 tracking for tool `securitySchemes` plus `_meta["mcp/www_authenticate"]`. Focused QA rerun found a data race from parallel failure subtests; fixed by making those subtests serial, and `go test -race ./internal/server` passed. Final focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: this story proves SDK fit only. It does not implement encrypted token envelopes, authorization-code storage, setup pages, token refresh, or per-tool scope enforcement.

### M1C-S02: ChatGPT Connector Compatibility Spike

- Status: `Done`
- Depends on: M1C-S01
- Scope: confirm redirect URI, metadata, client registration, local/dev callback, and pre-auth discovery expectations.
- Acceptance:
  - Connector assumptions are documented with exact dates and source links.
  - HTTP OAuth implementation tasks are unblocked or revised.

Tasks:

- [x] Verify current OpenAI connector OAuth docs.
- [x] Record redirect URI shape and registration model.
- [x] Record required metadata fields and scopes behavior.
- [x] Decide whether unauthenticated static MCP discovery is required.

- Validation: `go test ./internal/server ./docs`; `go test -race ./internal/server`; `go test ./...`; `git diff --check`.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior InfoSec Engineer reviews run. Initial reviews found missing invalid/expired/insufficient-scope SDK failure cases, missing official source links for connector assumptions, unclear first-pass client registration behavior, and missing tool-level auth UI tracking. Follow-up added failure-mode tests, official OpenAI docs links, CIMD public-client `none` as the initial registration model, and M1C-S04 tracking for tool `securitySchemes` plus `_meta["mcp/www_authenticate"]`. Focused QA rerun found a data race from parallel failure subtests; fixed by making those subtests serial, and `go test -race ./internal/server` passed. Final focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: official docs require HTTPS production metadata and callback URLs. Local development should use an HTTPS tunnel such as ngrok and refresh connector metadata after server changes; no separate local callback URL shape is documented for bypassing the production ChatGPT redirect.

### M1C-S03: OAuth Envelope And Code Storage

- Status: `Done`
- Depends on: M1C-S01, M1C-S02
- Scope: implement encrypted access/refresh envelopes and one-time authorization code storage.
- Validation: `go test ./internal/oauth` passed; `go test ./...` passed; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Initial Go and infosec findings found CIMD origin validation could be left empty and strict metadata decoding risked rejecting future compatible CIMD fields; fixed by requiring configured allowed origins and allowing unknown metadata fields. Initial QA findings requested service-boundary negative tests, bounded-read/timeout/unsafe-redirect tests, credential-forwarding evidence, refresh/envelope negative tests, and code-store expiry coverage; fixed with focused tests. Product review found no actionable issues and confirmed the change stays within M1C-S03 without implying M1C-S04 or production HTTP readiness. Focused Go, QA, and infosec reruns found no remaining actionable findings.
- Residual risk: refresh tokens remain stateless and replayable until expiry or absolute session expiry. This is the planned sealed-envelope tradeoff documented in `docs/PLAN.md`; one-time refresh-token rotation would require additional storage and is not part of M1C-S03.
- Acceptance:
  - CIMD `client_id` metadata documents are fetched with bounded reads, context timeouts, safe redirect policy, expected ChatGPT metadata origin/shape validation, and no credential forwarding.
  - Authorization code issuance rejects bad `client_id`, non-HTTPS metadata URLs, wrong redirect URI, metadata fetch failure, and metadata mismatch before storing or returning a code.
  - Access token default lifetime is 15 minutes.
  - Refresh token default lifetime is 30 days.
  - Absolute connector session default is 90 days.
  - Authorization codes are one-time-use with bounded expiry.
  - Tokens are opaque and not plaintext JWT/JWS.

Tasks:

- [x] Add CIMD client metadata fetch and validation.
- [x] Add redirect URI validation against the fetched CIMD metadata document.
- [x] Add `internal/oauth` envelope codec.
- [x] Add keyring config and validation.
- [x] Add one-time auth-code ID store.
- [x] Add refresh flow.
- [x] Add negative tests for bad `client_id`, non-HTTPS metadata URL, wrong redirect URI, metadata fetch failure, and metadata mismatch.
- [x] Add redaction tests.

### M1C-S04: Scope Guard And Credential Propagation

- Status: `Done`
- Depends on: M1C-S03
- Scope: enforce per-tool OAuth scopes and request-scoped InvenTree credentials.
- Validation: `go test ./internal/oauth ./internal/config ./internal/tools ./internal/server` passed; `INVENTREE_TEST_SKIP_DOCKER=1 go test ./...` passed; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Initial Go review found request-scoped credentials were not wired into a concrete tool dependency path and that the Go SDK lacks a top-level `securitySchemes` field; fixed with `OAuthClientFromContext`, request-scoped credential propagation tests, decoded descriptor metadata tests, and explicit SDK residual-risk docs. Initial QA review requested concurrent credential isolation, multi-scope denial, and per-tool descriptor metadata coverage; fixed and rerun, with a final isolation-test refinement tying each bearer token to its own upstream authorization-derived response. Initial product review requested clearer operator-facing wording that full HTTP tool registration is internal server-construction capability until CLI/setup/deployment wiring exists and stale config messages are refreshed; fixed and rerun. Initial infosec review found the credential carrier redacted JSON but not formatting/logging paths; fixed with `String`, `GoString`, `slog.LogValuer`, and JSON/fmt/slog tests. Focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: the current Go MCP SDK `mcp.Tool` type has no first-class top-level `securitySchemes` field, so scoped tools publish `securitySchemes` and `openai/securitySchemes` through descriptor `_meta` mirrors until SDK support or custom descriptor serialization is added. The earlier production-startup, connector-setup, and canonical-proxy gates were superseded by F-S07, F-S08, and F-S09. Full packaged deployment and live connector validation remain F-S10 work; development HTTP still exposes only its limited read-only surface.
- Acceptance:
  - Global bearer auth only authenticates and populates context.
  - Tool-specific guard checks manifest before handler dispatch.
  - Credential carrier is type-safe and not logged or serialized.
  - Tool descriptors expose OAuth `securitySchemes` and any ChatGPT compatibility mirror metadata required for tool-level OAuth prompts.
  - Tool auth failures that need ChatGPT linking or reauthorization return `_meta["mcp/www_authenticate"]` without leaking credentials or sensitive request details.

Tasks:

- [x] Add tool authorization manifest.
- [x] Add per-tool scope wrapper in `internal/tools` or `internal/server`.
- [x] Add `internal/oauth.CredentialFromTokenInfo` or selected private carrier.
- [x] Add OAuth `securitySchemes` and compatibility metadata to registered tools.
- [x] Add tool auth error results carrying safe `_meta["mcp/www_authenticate"]` challenges where ChatGPT linking or reauthorization is required.
- [x] Add scope rejection tests.
- [x] Add concurrent credential isolation tests.

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

- Status: `Done`
- Depends on: all milestone 1 implementation stories
- Scope: run senior Go, QA, product, and infosec reviews before beta declaration.
- Acceptance:
  - Findings are either fixed or documented as accepted residual risk.
  - Blocking milestone tests pass.
  - No sales/customer workflows ship.

Tasks:

- [x] Run senior Go review.
- [x] Run senior QA review.
- [x] Run senior product review.
- [x] Run senior infosec review.
- [x] Fix or document findings.

Validation:

- `GOFLAGS=-trimpath go test -race -count=1 ./...` passed.
- `git diff --check` passed.

Review:

- Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer final review panel run. Go found no actionable findings and independently passed `INVENTREE_TEST_SKIP_DOCKER=1 go test ./cmd/inventree-mcp ./internal/server ./internal/tools ./internal/upload ./internal/oauth ./internal/inventree ./docs`. Infosec found no actionable beta-blocking security findings. Product found the beta boundary was inconsistent between the plan's HTTP/ChatGPT readiness wording and operator docs that keep production HTTP disabled until startup/setup wiring lands; fixed by clarifying that milestone 1 proves STDIO workflows plus HTTP OAuth/scope primitives, while production ChatGPT/HTTP setup and deployment wiring remains gated follow-up work. Focused product, QA, and infosec reruns found the reverse-proxy blocking-test wording still conflicted with the accepted residual risk; fixed by narrowing the blocking test to configured OAuth issuer/resource URL behavior. Final focused product, QA, and infosec reruns found no remaining actionable findings.

Residual risk:

- The production HTTP CLI startup risk recorded by this milestone was superseded by F-S07's protected-resource startup implementation, F-S08's connector setup flow, and F-S09's canonical URL/trusted-proxy enforcement. Packaged-service validation and live ChatGPT connector deployment remain accepted F-S10 follow-up risk. Milestone 1 beta readiness covers the STDIO operator workflows, registered development HTTP surface, and implemented/tested OAuth and scope primitives, not an end-to-end production ChatGPT connector deployment.

## Future Backlog

### F-S01: Evaluate Docker Compose Testcontainers Stack

- Status: `Done`
- Issue: [#46](https://github.com/davidvanlaatum/inventree-mcp/issues/46)
- Depends on: M1H-S01
- Scope: evaluate whether `github.com/testcontainers/testcontainers-go/modules/compose` can replace or complement the hand-wired InvenTree Testcontainers stack by using official InvenTree Docker Compose files plus test-specific overrides.
- Acceptance:
  - Compare compose-based startup against the current `internal/testenv` stack for startup time, log visibility, cleanup behavior, loopback-only published ports, readiness checks, and deterministic token creation.
  - Determine whether the official compose topology starts all backend services needed for realistic MCP integration tests without introducing unnecessary CI cost.
  - Document whether compose should replace the current stack, become an optional canary/compatibility path, or be rejected with reasons.

Tasks:

- [x] Identify the official InvenTree compose files and required test overrides for pinned `inventree/inventree:1.4.3`.
- [x] Prototype a local compose stack using `testcontainers-go/modules/compose`.
- [x] Verify service logs, `ServiceContainer` inspection, endpoint discovery, and `Down` cleanup semantics.
- [x] Compare findings with the current direct-container `internal/testenv` implementation.
- Decision: reject Compose for the current integration environment. The prototype showed no material speed or coverage benefit and would add a large transitive dependency graph, an upstream Compose snapshot plus merge-sensitive overrides, additional per-service live-log setup, and a second cleanup lifecycle. See [Docker Compose Testcontainers Evaluation](testcontainers-compose-evaluation.md).
- Validation: Compose prototype against `inventree/inventree:1.4.3` passed three times, with core startup from 36.768s to 52.825s and `Down` cleanup from 10.572s to 10.727s; the final strengthened rerun asserted the exact published-port set plus Docker not-found results for all containers, Compose networks, and created volumes. `go test ./internal/testenv -run '^TestStartInvenTreeStack$' -count=1 -v` passed in 55.08s for the direct comparison. `INVENTREE_TEST_SKIP_DOCKER=1 GOFLAGS=-trimpath go test -race -count=1 ./...`, `GOFLAGS=-trimpath go mod tidy -diff`, the retained-harness `gofmt -d`, and `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer panels completed. Initial Go review corrected live-log and transitive-dependency wording. QA and infosec required independently reproducible, sanitized prototype evidence. Follow-up QA and Go reviews tightened the exact published-port set and cleanup proof so unrelated Docker errors cannot masquerade as removal. The retained harness now requires Docker not-found results for every container, network, and created volume. Focused Go, QA, and infosec reruns found no unresolved actionable findings. Product review found the decision and future reconsideration gate clear; after issue #46 alignment and read-back, its focused follow-up also found no unresolved actionable findings.
- Residual risk: single warm-image local runs are not performance benchmarks. The rejection remains appropriate because it does not depend on a small timing difference; it is based primarily on equivalent coverage and materially greater dependency, configuration-drift, logging, and lifecycle cost.

### F-S02: BOM Import Workflow

- Status: `Blocked`
- Issue: [#47](https://github.com/davidvanlaatum/inventree-mcp/issues/47)
- Depends on: milestone 1 complete and product review
- Blocker: operator placed this unused feature on hold. Do not select or implement F-S02 until every other backlog story except F-S02 and F-S04 is `Done`; then revisit both held stories.

Tasks:

- [ ] Define BOM import behavior.
- [ ] Implement structured row validation.
- [ ] Add dry-run and row-level error tests.

### F-S03: Purchase Order Write And Receiving

- Status: `Done`
- Issue: [#48](https://github.com/davidvanlaatum/inventree-mcp/issues/48)
- Depends on: milestone 1 complete and product review
- Progress: purchase-order and line read/search, create/PATCH, retry-recoverable `create_purchase_order_with_lines`, explicitly confirmed issuing, and dry-run/confirmed partial receiving are implemented. The operator approved `(supplier_id, supplier_reference)` as the creation recovery identity and approved receiving as new-stock-only with item-to-line-to-global location fallback; receiving never merges into or updates existing stock and never auto-issues a pending order. Receiving now binds confirmation to a deterministic current-state plan hash, enforces the API decimal bounds, rejects virtual parts, refreshes final order state, and treats ambiguous writes as read-before-retry partial failures.
- Scope: create purchase orders and purchase-order lines from stable supplier-part inputs, then receive purchase-order lines into stock when the operational workflow is explicitly in scope. The highest-value first write workflow is `create_purchase_order_with_lines`: accept a supplier, stable supplier reference, description/date fields, and validated line inputs, then create or update the purchase order and lines after the same preview math and supplier-part checks used by `preview_purchase_order_with_lines`.
- Acceptance:
  - Purchase-order creation uses stable supplier IDs and schema-verified write endpoints, with dry-run planning when lines are supplied.
  - `create_purchase_order_with_lines` supports preview-equivalent validation before mutation, returns the purchase order ID and line IDs, and uses an exact `(supplier_id, supplier_reference)` lookup to recover interrupted sequential attempts.
  - Purchase-order line create/update tools validate supplier-part identity, quantity, price, currency, and supplier consistency before writing.
  - Duplicate and recovery reads cover purchase orders and purchase-order lines by supplier, supplier reference, status/date where schema-supported, and stable ID so operators can recover from partial or interrupted writes.
  - Receiving workflow defines whether stock is created, merged, or updated, and requires explicit operator confirmation before operational stock changes.
  - Tool annotations and OAuth scopes distinguish ordinary purchasing writes from operational stock receiving.
  - Integration coverage exercises successful create, line update, and in-scope receiving paths against Testcontainers before the story is marked `Done`.

Tasks:

- [x] Define purchase order creation workflow.
- [x] Add `create_purchase_order`.
- [x] Add `create_purchase_order_with_lines` with retry-recoverable create/update behavior after preview validation.
- [x] Add purchase-order line create/update tools.
- [x] Add purchase-order and purchase-order-line search/read tools for duplicate checks, retry, and recovery.
- [x] Define receiving workflow.
- [x] Add separately confirmed `issue_purchase_order` because InvenTree accepts receipts only for placed orders.
- [x] Add purchase-order receiving workflow after product scope confirmation.
- [x] Add operational/destructive scope review.
- Validation: `INVENTREE_TEST_SKIP_DOCKER=1 GOFLAGS=-trimpath go test -race -count=1 ./...` passed; `GOFLAGS=-trimpath go test -race ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/po$' -count=1` passed against `inventree/inventree:1.4.3` / API `511`; `GOFLAGS=-trimpath go test -race ./internal/tools -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/purchase_order_create_and_retry_happy_path$' -count=1` passed against `inventree/inventree:1.4.3` with partial receipt, source-PO stock recovery, final receipt, and refreshed `COMPLETE` order assertions; `GOFLAGS=-trimpath go test -race -p=1 ./...` passed with all default-on Docker suites; `go generate ./internal/tools` produced the checked manifest; `golangci-lint run` reported 0 issues; `GOFLAGS=-trimpath go mod tidy -diff` and `git diff --check` passed. Unit coverage includes valid-hash state invalidation, decimal boundaries, supplier pack/base-stock/price/currency/packaging plan binding, virtual-part refusal, definite 4xx rejection, ambiguous transport/empty-result/refresh recovery, order-state guards, duplicate lines/barcodes, invalid locations/status/expiry, and issue idempotency guards.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer panels completed for both the creation and receiving slices. Receiving review findings covered unbound dry-run confirmation, schema-invalid quantities, virtual-part behavior that violated the new-stock-only contract, ambiguous-result blind-retry risk, stale returned order status, incomplete guards, same-line concurrent over-receipt, supplier pack/packaging/price plan gaps, definite 4xx classification, unavailable source-order stock recovery, recovery scope mismatch, and blank-packaging contract wording. Fixes bind confirmation to the complete resolved plan, provide executable PO-filtered recovery under read/write/operational scopes, preserve definite API rejections, refresh final state, and align tests and public/operator docs. Focused Go, QA, product, and infosec reruns found no unresolved actionable findings. The operator accepted the narrow same-line concurrency risk without an MCP-local lock because such a lock would not cover other server replicas, the InvenTree UI, or direct API clients.
- Residual risk: the retry-recoverable workflow updates lines derived from the same supplier reference and one-based input index; it does not delete extra existing lines when a retry supplies fewer lines because removal semantics were not approved. Concurrent creators can still race between the duplicate-recovery read and order creation because InvenTree does not enforce uniqueness on `(supplier, supplier_reference)`; an interrupted caller must retry with the same pair so the workflow can recover the created ID. Issue and receive transitions are non-idempotent and intentionally retain `idempotentHint:false`; the receive tool returns structured read-before-retry recovery guidance for unknown results. InvenTree 1.4.3 locks receipt line rows but does not reject a quantity that becomes greater than outstanding while a previously prepared request waits for that lock. Concurrent receipt of the same line is therefore unsupported and remains an accepted operator risk; no MCP-local lock is added because it would provide only process-local protection. The plan binds source price/currency but InvenTree's global currency-conversion configuration has no revision exposed through this endpoint, so a concurrent administrator change remains outside the confirmation hash.

### F-S04: Build Order Workflows

- Status: `Blocked`
- Issue: [#49](https://github.com/davidvanlaatum/inventree-mcp/issues/49)
- Depends on: milestone 1 complete and product review
- Blocker: operator placed this unused feature on hold. Do not select or implement F-S04 until every other backlog story except F-S02 and F-S04 is `Done`; then revisit both held stories.

Tasks:

- [ ] Define build create/allocate/complete behavior.
- [ ] Add stock consumption safety model.
- [ ] Add integration tests.

### F-S05: Stocktake Adjustments

- Status: `Done`
- Issue: [#50](https://github.com/davidvanlaatum/inventree-mcp/issues/50)
- Depends on: milestone 1 complete and product review
- Progress: completed the approved single-item quantity/status/count tools, mandatory audit reasons and state-bound confirmation, quantity-only stocktake behavior, high-risk decrease/write-off warnings, read/write/operational scopes, registered review prompt, checked manifests, and default-on live coverage. The operator accepted the narrow concurrent same-item race because InvenTree exposes no compare-and-swap primitive; concurrent adjustment of one item is unsupported.
- Scope: add explicit single-stock-item operational tools for relative quantity adjustment, status-only changes, and absolute stocktake counts, plus a registered stocktake review prompt. Stocktake counts change quantity only; location, status, batch, and packaging changes remain separate operations.
- Acceptance:
  - `adjust_stock_quantity` applies a non-zero relative delta to one stable stock-item ID through the native add/remove endpoints and returns before/after state.
  - `set_stock_status` changes only the status of one stable stock-item ID and returns before/after state.
  - `stocktake_adjustment` records one absolute observed quantity through the native count endpoint without implicitly changing location, status, batch, or packaging.
  - Every execution requires a dry-run plan bound to current stock state, an opaque principal-bound five-minute single-use confirmation token returned as `plan_hash`, explicit confirmation, and a nonblank operator audit reason; state changes observed during execution preflight are rejected without writing.
  - Quantity decreases and transitions to `Destroyed`, `Rejected`, or `Lost` are identified as high-risk in plans and confirmations.
  - Tools require read, write, and operational OAuth scopes, remain closed-world and non-destructive in MCP annotations, and do not automatically retry ambiguous mutation results.
  - Unit and default-on Testcontainers integration coverage exercise successful add, remove, count, and status changes plus stale-plan, confirmation, audit-reason, high-risk, and no-hidden-metadata-change behavior.
  - Tool reference, operator recipes, prompt manifest, and generated tool manifest match the implemented workflow.
  - No-op changes, serialized-stock quantity changes, and quantity changes that would implicitly delete a `delete_on_deplete` item are refused without writing; serialized status-only changes remain supported.

Tasks:

- [x] Define the single-item stocktake review workflow and product boundaries.
- [x] Add typed client methods for stock add, remove, count, and status endpoints.
- [x] Add `adjust_stock_quantity`, `set_stock_status`, and `stocktake_adjustment` with dry-run-bound confirmation and audit requirements.
- [x] Register the `stocktake_review` prompt and update operator documentation.
- [x] Add unit, authorization, manifest, and default-on Testcontainers integration coverage.
- Validation: `INVENTREE_TEST_SKIP_DOCKER=1 GOFLAGS=-trimpath go test -race -count=1 ./...` passed; `GOFLAGS=-trimpath go test -race ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/stock_adjustments$' -count=1` passed against `inventree/inventree:1.4.3` / API `511`; `GOFLAGS=-trimpath go test -race ./internal/tools -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/stock_adjustment_happy_path$' -count=1` passed against the same pinned instance; and the final `GOFLAGS=-trimpath go test -race -p=1 ./...` passed with all default-on Docker suites after serialized-stock and token-capacity follow-ups. `go generate ./internal/tools` produced the checked manifest; `golangci-lint run` reported 0 issues; `GOFLAGS=-trimpath go mod tidy -diff` and `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer completed the required full panel and focused reruns. Findings covered the accepted upstream TOCTOU race, decimal normalization, implicit delete-on-deplete deletion, post-write recovery coverage, live metadata preservation, deterministic/replayable confirmation hashes, ambiguous timeout-like 4xx handling, no-op writes, serialized quantity no-ops, unbounded confirmation storage, latest-token wording, and completion bookkeeping. Fixes added opaque principal-bound expiring single-use tokens with supersession and bounded storage, schema-decimal checks, fail-closed quantity guards, recovery/readback handling, expanded unit/live coverage, and aligned public/operator docs. Final focused Go, QA, product, and infosec reruns found no unresolved actionable findings.
- Residual risk: InvenTree does not expose an atomic compare-and-swap operation spanning the execution preflight and stock mutation. Concurrent adjustment of the same item through MCP, another replica, the InvenTree UI, or a direct API client is unsupported. The tool detects an observed preflight change before mutation and returns `partial_failure` on readback mismatch, but a write can still race in the narrow upstream read/write window. The operator accepted this limitation; an MCP-local lock was not added because it would not protect other writers.

### F-S06: Systemd Notify And Watchdog Support

- Status: `Done`
- Issue: [#51](https://github.com/davidvanlaatum/inventree-mcp/issues/51)
- Depends on: F-S07 and product review
- Progress: added native systemd lifecycle notifications around packaged HTTP startup, listener-bound readiness, half-timeout watchdog heartbeats, degraded-but-serving watchdog failure handling, graceful shutdown, and fatal managed-runtime errors. The packaged unit now uses `Type=notify`, `NotifyAccess=main`, and `WatchdogSec=30s`; configuration and logger initialization failures before the managed lifecycle starts remain non-zero exits handled by systemd.
- Scope: add native systemd notification support for packaged HTTP deployments.
- Acceptance:
  - HTTP service startup sends systemd readiness only after the listener is bound, runtime dependencies are initialized, and production startup checks have passed.
  - The process sends watchdog heartbeats at a safe interval when systemd `WatchdogSec` is configured.
  - After the managed HTTP lifecycle starts, the process publishes useful systemd status text for startup, ready, degraded, shutdown, and fatal-error states without logging or exposing secrets; earlier configuration and logger initialization failures exit non-zero for ordinary systemd handling.
  - Packaged systemd unit can safely switch from `Type=simple` to `Type=notify` with `NotifyAccess=main` and an explicit `WatchdogSec`.
  - Tests cover notify readiness ordering, heartbeat cadence, disabled-watchdog behavior, shutdown status, fatal-error status, and non-systemd fallback behavior.
  - README, operator recipes, release packaging docs, and `AGENTS.md` are updated to describe the supported systemd behavior.

Tasks:

- [x] Select and wrap a maintained Go systemd notification library.
- [x] Add injectable notifier/watchdog abstraction for deterministic tests.
- [x] Send startup status transitions and final readiness notification.
- [x] Send watchdog heartbeats only when systemd watchdog is enabled.
- [x] Publish shutdown, degraded, and fatal-error status messages.
- [x] Update packaged systemd unit to `Type=notify` after code support lands.
- [x] Add unit and integration tests for notify/watchdog behavior.
- [x] Update release and operator documentation.

Validation:

- `go test -race ./internal/systemdnotify ./internal/server ./cmd/inventree-mcp ./packaging/systemd ./docs` passed, including real Unix datagram notification, listener-bound readiness, degraded-but-serving watchdog failure, shutdown status, non-systemd fallback, and packaged-unit contract coverage.
- `go test -coverprofile=/tmp/inventree-mcp-cmd-coverage.out ./cmd/inventree-mcp` reported 87.9% statement coverage after adding managed dependency-initialization and fatal-notification failure regressions; `serve` and `notifyFatal` each report 100%.
- `INVENTREE_TEST_SKIP_DOCKER=1 GOFLAGS=-trimpath go test -race -count=1 ./...` passed.
- `GOFLAGS=-trimpath go test -race -p=1 -count=1 ./...` passed with the default-on InvenTree Testcontainers suites.
- `go generate ./internal/tools` produced no unexpected changes; `golangci-lint run`, `go mod tidy -diff`, `goreleaser check`, and `git diff --check` passed.
- `goreleaser release --snapshot --clean` passed, and the generated `deb`, `rpm`, and `apk` payloads contained the expected notify/watchdog unit.

Review:

- Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed. Go and QA found that retrying heartbeats after a send failure could prevent systemd-owned termination; the loop now stops after the first failure while HTTP continues serving. QA also requested explicit shutdown-status assertions and real `NOTIFY_SOCKET` integration coverage; both were added. Product required the fatal-status boundary to distinguish managed-lifecycle failures from earlier configuration/logger failures; the operator chose non-zero systemd handling for the earlier boundary and docs now say so. A coverage follow-up added regressions for managed dependency-initialization failure and fatal-status delivery failure; focused Go and QA reruns found no issues with the injection seam, error preservation, logging, or global restoration. Focused Go, QA, and Product reruns plus Infosec review found no remaining actionable findings. Linked issue #51 was aligned with the clarified acceptance, completed checklist, validation, review, and residual-risk handoff and remains open for merge.

Residual risk:

- Actual systemd-manager startup timeout, watchdog termination/restart, and installed-package operation remain deferred to F-S10 live packaged deployment validation. Notification sends use the maintained library's synchronous Unix-datagram behavior at the trusted systemd boundary. After the first failed heartbeat, the process can continue accepting requests until the 30-second watchdog deadline. Configuration and logger initialization failures before lifecycle initialization do not publish custom status text, but exit non-zero for systemd to record and restart.

### F-S07: Production HTTP OAuth Startup

- Status: `Done`
- Issue: [#52](https://github.com/davidvanlaatum/inventree-mcp/issues/52)
- Depends on: M1C-S04, M1I-S02, product review, and infosec review
- Scope: replace the current development-only HTTP gate with production HTTP startup that constructs OAuth services, keyrings, protected-resource middleware, scoped tool dependencies, and HTTP routes from explicit configuration.
- Validation: `go test ./internal/config ./internal/oauth ./internal/server ./cmd/inventree-mcp ./docs` passed; `go generate ./internal/tools` produced no drift; `go test -race -p=1 ./...` passed, including the default-on InvenTree Testcontainers suites; `golangci-lint run` reported 0 issues; `goreleaser check` validated `.goreleaser.yaml`; `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed on the reconciled SDK `v1.7.0` implementation. Findings covered malformed CLI key disclosure, real production-mux Testcontainers coverage, subtest-local contexts/assertions, path-specific RFC 9728 discovery, bounded signal-driven graceful shutdown and error reporting, production upstream HTTPS, required token subject/issued-at/session claims, authentication before debug body capture, environment-only envelope keys and lifecycle/package guidance, and stale F-S09/milestone/deployment boundaries. Code, tests, task contracts, and operator docs were updated. Focused reruns by all four roles found no remaining actionable findings. This final validation/review evidence update changes only task metadata and did not require another reviewer rerun.
- Residual risk: production HTTP can validate sealed access-token envelopes and protect the configured MCP path; F-S08 supplies connector authorization/setup and F-S09 supplies canonical URL/trusted-proxy enforcement. Live production use still waits for F-S10 packaged deployment validation. Stateless refresh envelopes remain replayable until expiry or absolute session expiry as documented by M1C-S03, and the opt-in debug traffic log remains sensitive operator-controlled output.
- Acceptance:
  - Production HTTP `serve --transport http` starts only when all required OAuth issuer, resource, key, lifetime, client metadata, and InvenTree base URL settings are valid.
  - Raw InvenTree credentials remain rejected as HTTP runtime credentials outside the setup flow.
  - The HTTP MCP endpoint uses MCP-owned bearer tokens, decrypts access envelopes, recovers request-scoped InvenTree credentials, and rejects missing, malformed, expired, wrong-audience, wrong-client, wrong-type, or undecryptable tokens before tool dispatch.
  - Write, upload, operational, and destructive tools are registered in production HTTP mode only behind per-tool scope guards.
  - Development-only `--dev-incomplete-oauth` behavior remains clearly separated from production startup.
  - Tests cover config validation, route wiring, keyring loading and rotation states, token verifier behavior, scope-guarded tool registration, and raw upstream token rejection.
  - README, operator recipes, tool reference, release/package notes, and `AGENTS.md` are updated where production HTTP startup behavior changes.

Tasks:

- [x] Define the production HTTP OAuth config shape and environment variables.
- [x] Load and validate OAuth envelope keys with explicit key IDs and active/decrypt-only states.
- [x] Construct the OAuth token verifier and request-scoped `OAuthClientFromContext` dependencies from production config.
- [x] Wire protected MCP HTTP routes to SDK bearer middleware and protected-resource metadata.
- [x] Enable full HTTP tool registration only when OAuth authorization mode is active.
- [x] Preserve development-only incomplete-OAuth startup as non-production behavior.
- [x] Add unit and integration tests for config, route wiring, verifier failures, and scoped tool exposure.
- [x] Update operator and release documentation.

### F-S08: ChatGPT Connector OAuth Setup Flow

- Status: `Done`
- Issue: [#53](https://github.com/davidvanlaatum/inventree-mcp/issues/53)
- Depends on: F-S07, current official OpenAI connector documentation verification, product review, and infosec review
- Progress: current OpenAI connector auth guidance was refreshed on 2026-08-02. The operator selected CIMD `private_key_jwt` over public-client `none` and asked not to rotate existing InvenTree API keys. Implementation validates signed client assertions against the metadata document's same-origin JWKS, rejects assertion replay, seals one-time setup/code state, and creates a uniquely named dedicated InvenTree connector token with explicit supplied-token fallback.
- Scope: implement the operator-facing OAuth authorization flow for ChatGPT connector setup, including authorization, setup credential collection, authorization-code issuance, token exchange, refresh, and credential-source metadata.
- Acceptance:
  - Authorization requests validate client metadata, redirect URI, PKCE `S256`, `resource`, requested scopes, state, and CIMD `private_key_jwt` token-endpoint client authentication against the client metadata JWKS.
  - Setup pages collect supported InvenTree credentials without persisting raw credentials in browser state or logs.
  - Submitted credentials are validated against the configured InvenTree instance before any authorization code is issued.
  - The setup flow attempts to create or seal a dedicated connector token where the InvenTree API and operator permissions allow it.
  - If dedicated token creation is unavailable after an existing token is supplied, the operator must explicitly choose whether to seal the supplied token or cancel setup.
  - Authorization codes are one-time-use with bounded expiry and storage.
  - Token endpoint supports authorization-code and refresh-token grants with encrypted access and refresh envelopes, configured lifetimes, and absolute authorization/session expiry.
  - Setup, authorization, and token endpoints enforce CSRF protections where applicable, no-store/no-referrer/frame-denial/CSP headers, body-size limits, timeouts, rate limits, generic credential-validation errors, and sensitive query/body redaction.
  - Tests cover happy path, cancellation, permission-denied token creation, invalid credentials, PKCE failures, replayed codes, refresh behavior, expiry, rate limiting, security headers, and redaction.
  - Operator recipes document the supported setup choices and the limits of MCP scopes versus upstream InvenTree credential permissions.

Tasks:

- [x] Refresh and cite current official OpenAI connector OAuth requirements before implementation.
- [x] Add authorization endpoint request validation and state handling.
- [x] Add secure setup-page rendering and form handling.
- [x] Add InvenTree credential validation and connector-token creation or fallback decision handling.
- [x] Add one-time authorization-code issuance bound to setup state.
- [x] Add token endpoint support for authorization-code and refresh-token grants.
- [x] Add setup, authorization, token, security-header, timeout, rate-limit, and redaction tests.
- [x] Update ChatGPT connector setup documentation and operator recipes.
- Validation: `go generate ./internal/tools`; `go mod tidy -diff`; `git diff --check`; `INVENTREE_TEST_SKIP_DOCKER=1 go test -race -count=1 ./...`; `go test -count=1 ./...`; `golangci-lint run`; `go test -race ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/current_user_and_connector_token$' -count=1 -v`; and `go test -race ./internal/server -run '^TestHTTPOAuthFlowAgainstInvenTreeContainer$' -count=1 -v` passed. The live InvenTree `1.4.3` checks prove two uniquely named connector tokens and the submitted credential remain usable, and the production mux completes authorization, dedicated-token creation, `private_key_jwt` code exchange, refresh, and a scoped MCP call with the refreshed access token. The CodeQL SSRF follow-up additionally passed `go test -race ./internal/oauth ./internal/server`, the Docker-skipped full race suite, `go mod tidy -diff`, `golangci-lint run`, and `git diff --check`; its regression test proves an unconfigured same-origin client metadata path is rejected without an HTTP fetch. The branch-wide coverage follow-up passed the focused OAuth/InvenTree race tests and the Docker-skipped full race suite; focused package coverage measured 83.4% for OAuth and 90.1% for InvenTree after adding current-user/token, CIMD security-shape, PS256, and ES256 coverage.
- Review: required Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer panels completed. Initial findings covered limiter/state exhaustion, fixed-name token rotation, missing setup disclosure, endpoint/JWT negative coverage, production-mux integration depth, slow body reads, process-local replay wording, library-fit evidence, and issue alignment. Fixes added bounded cleanup and authorization limiting, unique setup-specific token names, informed consent/cancel behavior, deterministic clock/timeouts, comprehensive endpoint/assertion tests, full live HTTP integration, a documented MCP SDK/Fosite fit refresh, and aligned operator/issue guidance. Focused reruns after the fixes and after the final refreshed-token/non-rotation integration assertions found no unresolved actionable findings. A final focused Go, QA, product, and infosec rerun found no actionable findings in the exact configured-client-ID URL enforcement added for CodeQL. The branch-wide coverage review found subtests using their parent assertion object and missing valid PS256/ES256 verifier cases; both were fixed, and focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: client-assertion replay protection is bounded and process-local, so restart or multi-replica deployments require shared external replay state for equivalent protection. Refresh envelopes remain stateless and replayable until expiry or absolute session expiry. Unique dedicated `inventree-mcp-chatgpt-*` tokens preserve submitted and earlier keys but can remain unused after abandoned, expired, or retired authorizations; operators must revoke them in InvenTree because the database-free server does not retain token IDs for automatic cleanup. F-S09 supplies canonical routing/trusted-proxy enforcement; live packaged connector deployment remains F-S10 work.

### F-S09: Reverse-Proxy Canonical URL Enforcement

- Status: `Done`
- Issue: [#54](https://github.com/davidvanlaatum/inventree-mcp/issues/54)
- Depends on: F-S07, F-S08, deployment design review, product review, and infosec review
- Scope: complete canonical public URL and trusted-proxy enforcement for the F-S08 authorization/setup surfaces while preserving F-S07's configured protected-resource metadata, bearer-challenge, and MCP token-audience behavior without trusting arbitrary inbound host or forwarded headers.
- Approved policy: support path-preserving proxies only; require explicit trusted proxy CIDRs; ignore forwarded host, scheme, and prefix headers; accept `X-Forwarded-For` only from a trusted immediate peer; and use the normalized right-to-left resolved source IP for rate limiting and appropriate request/security logging without recording the raw header.
- Validation: `go generate ./internal/tools`; `go mod tidy -diff`; `git diff --check`; `golangci-lint run`; `go test -tags no_integration_tests -race ./internal/config ./internal/oauth ./internal/server ./docs`; `go test -race ./internal/server -run '^TestHTTPOAuthFlowAgainstInvenTreeContainer$' -count=1 -v`; and `GOFLAGS=-trimpath go test -race -p=1 ./...` passed. The prefixed Docker OAuth flow against InvenTree `1.4.3` proves canonical setup form/cookie paths, hostile internal/forwarded-header resistance, signed `private_key_jwt` exchange, wrong-resource rejection, refresh, and protected MCP use with the normalized forwarded source attached to downstream logs.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer panels completed. Initial findings covered incomplete prefixed OAuth and composed rate-limit evidence, packaged enablement wording, prefix-aware discovery documentation, malformed-chain proxy-bucket denial of service, trust-all CIDRs, and comma-heavy forwarding-header allocation amplification. Fixes added deterministic production-mux tests, a prefixed Testcontainers OAuth flow, explicit F-S10 gating, right-to-left incremental parsing, IPv4/IPv6 trust-all rejection, route-collision validation, and bounded-allocation plus multi-line trust-boundary regressions. Focused reruns by every affected role found no unresolved actionable findings.
- Residual risk: normalized source identity depends on operators configuring only narrow controlled proxy CIDRs and each trusted proxy appending its observed client address. A trusted or compromised proxy can still misstate source addresses used for logging and rate limiting, but cannot influence configured canonical OAuth URLs, token issuer, or audience. Prefix-stripping proxies remain unsupported. Live packaged service and ChatGPT connector validation remain F-S10 work.
- Acceptance:
  - F-S07 protected-resource metadata, bearer challenges, and MCP access-token audiences continue to use configured canonical public HTTPS issuer/resource URLs independent of inbound host headers.
  - Authorization-server metadata, authorization URLs, token URLs, and redirect targets introduced by F-S08 use the same configured canonical public URLs.
  - Internal listener scheme, host, port, container names, and untrusted forwarded headers never appear in public OAuth metadata, challenges, redirects, errors, or tokens.
  - Trusted proxy configuration is explicit, validated, and documented.
  - Path-prefix behavior is defined and tested for supported proxy routing modes.
  - Misconfigured public URLs, non-HTTPS production URLs, untrusted forwarded headers, and redirect URI mismatches fail with actionable non-secret errors.
  - Tests cover direct internal requests, trusted and untrusted `X-Forwarded-*` headers, path prefixes, metadata, challenges, redirects, token audience validation, and internal-host leakage checks.
  - Reverse-proxy operator recipes cover canonical URLs, trusted proxy CIDRs or header policy, and common failure symptoms.

Tasks:

- [x] Define trusted-proxy and path-prefix configuration plus any canonical URL settings still required by F-S08 authorization/setup routes.
- [x] Route authorization-server metadata, authorization, token, and redirect behavior through configured public URLs while preserving F-S07 metadata, challenge, and MCP audience behavior.
- [x] Implement trusted-proxy and path-prefix handling only for explicitly supported deployments.
- [x] Add internal-host leakage and forwarded-header trust tests.
- [x] Add reverse-proxy deployment recipes and troubleshooting notes.

### F-S10: Packaged HTTP Deployment And Live Connector Validation

- Status: `Future`
- Issue: [#55](https://github.com/davidvanlaatum/inventree-mcp/issues/55)
- Depends on: F-S07, F-S08, F-S09, release package availability, and product review
- Scope: validate the installed package and a real ChatGPT connector setup path end to end before documenting production HTTP deployment as supported.
- Acceptance:
  - Packaged `deb` and `rpm` installs can be configured for production HTTP behind a reverse proxy without exposing raw InvenTree credentials in environment, process lists, logs, package files, or metadata responses.
  - The packaged service starts, survives restart, and serves OAuth metadata, authorization, token, and MCP endpoints through the documented reverse-proxy route.
  - A live ChatGPT connector can complete OAuth setup, list tools, call read-only tools, and call at least one scoped write/upload workflow against a disposable or dedicated non-production InvenTree instance.
  - CI or documented release validation covers package file layout, config examples, service startup smoke tests, and reverse-proxy metadata checks.
  - `systemctl enable --now inventree-mcp.service` is documented only after production startup and service lifecycle behavior are validated.
  - Alpine/OpenRC support remains explicitly unsupported unless implemented and tested.
  - README, operator recipes, release instructions, package notes, and `AGENTS.md` accurately describe the supported deployment and remaining limitations.

Tasks:

- [ ] Add package-level production HTTP config examples without secrets.
- [ ] Validate installed `deb` and `rpm` file layout and service startup behavior.
- [ ] Validate reverse-proxy routing to packaged OAuth and MCP endpoints.
- [ ] Run a live ChatGPT connector setup against a non-production InvenTree instance.
- [ ] Record successful read, write, upload, and scope-denial connector evidence.
- [ ] Update release, README, operator recipe, and package documentation.
- [ ] Decide whether Alpine/OpenRC remains unsupported or gets a separate implementation task.

### F-S11: Parameter Template Administration

- Status: `Done`
- Issue: [#56](https://github.com/davidvanlaatum/inventree-mcp/issues/56)
- Depends on: milestone 1 complete, product review, and infosec review
- Scope: administer `/api/parameter/template/` records and provide a guarded template-merge workflow for consolidating duplicate or overlapping templates.
- Validation: `go generate ./internal/tools` refreshed the checked tool manifest; `go test -tags no_integration_tests -cover ./internal/inventree ./internal/tools ./internal/server ./internal/schema ./docs` passed with `internal/inventree` at 91.1% and `internal/tools` at 79.9%; the matching current base-branch package comparison was 91.0% and 78.4%, with all other packages unchanged; `go test -race ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/parameter_template_administration$' -count=1 -v` and `go test -race ./internal/tools -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/parameter_template_admin_and_confirmed_merge$' -count=1 -v` passed against the pinned InvenTree Testcontainers stack; `go test -race -p=1 ./...` passed with every default-on Docker suite; `golangci-lint run ./...` reported 0 issues; `go mod tidy -diff` and `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed. Initial findings identified stale and unbounded category-link preflight, misleading non-part `part_id` output, incomplete live guardrail/partial-failure coverage, insufficient category-link cleanup context, missing non-atomic recovery guidance and model-type values, a read-scope oracle in create/update preflight, and raw upstream update-error disclosure. Fixes add bounded 1,000-link pagination, fresh row/link checks immediately before cleanup, structured model/category identities, live update/delete guardrails, stale-state/fault/read-back/delete tests, explicit supported model types, read+write scopes, sanitized failure reasons, and single-writer fresh-plan recovery guidance. Focused Go, QA, product, and infosec reruns found no unresolved actionable findings.
- Residual risk: merge and delete use non-transactional upstream REST calls. A narrow race remains if another UI/API/server writer creates a reference between the final preflight and template deletion; operators must prevent concurrent template/reference administration, and any partial failure requires inspection plus a fresh dry-run plan rather than replaying the old hash. Parameter and global category-link completeness checks fail closed above 1,000 scanned rows.
- Acceptance:
  - Create/update tools require explicit template fields, preserve omitted versus explicit values, and refuse ambiguous same-name/unit/choices collisions without operator clarification.
  - Delete requires `confirm:true`, preflight checks for existing parameter rows and category-default links, and refusal unless the operator has chosen an allowed cleanup path.
  - Template merge workflow can move rows from an old template to a target template, normalize values where explicitly configured, verify zero rows remain on the old template, and delete the old template only after confirmation.
  - Workflow outputs include dry-run actions, affected part/parameter IDs, skipped rows with reasons, read-back verification results, and residual manual decisions.
  - Tool annotations and OAuth scopes classify template delete and merge cleanup as destructive where rows or templates can be removed.
  - Unit and Testcontainers integration coverage exercise create, update, delete guardrails, and a successful merge path before the story is marked `Done`.

Tasks:

- [x] Define parameter-template create/update/delete input contracts and clarification behavior.
- [x] Add schema-manifest entries and typed client methods for template create/update/delete where missing.
- [x] Implement template create/update tools.
- [x] Implement guarded template delete with preflight and confirmation.
- [x] Implement dry-run template merge planning, row migration, normalization, read-back verification, and confirmed cleanup.
- [x] Update tool reference, operator recipes, and prompts for template administration workflows.

### F-S12: Global Parameter Value Search And Delete

- Status: `Done`
- Issue: [#57](https://github.com/davidvanlaatum/inventree-mcp/issues/57)
- Depends on: milestone 1 complete, product review, and infosec review
- Scope: expose cross-inventory `/api/parameter/` reads and guarded deletion of individual part parameter rows beyond the current part-scoped `get_part_parameters` tool.
- Validation: `go generate ./internal/tools` refreshed the checked tool manifest; `go test -tags no_integration_tests ./internal/inventree ./internal/tools ./internal/schema ./docs` passed; `GOFLAGS=-trimpath go test -race ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/parameter$' -count=1` passed against the pinned InvenTree Testcontainers stack; `GOFLAGS=-trimpath go test -race ./internal/tools -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/global_parameter_search_and_confirmed_delete$' -count=1` passed against the pinned stack; `GOFLAGS=-trimpath go test -race -p=1 ./...` passed with all default-on Docker suites; `golangci-lint run` reported 0 issues; `GOFLAGS=-trimpath go mod tidy -diff` and `git diff --check` passed. Coverage follow-up `go test -tags no_integration_tests -coverprofile=/tmp/inventree-fs12-followup.cov ./internal/inventree` reported 90.8% package coverage, with `listPage` and `DeletePartParameter` each at 100%; the matching race-enabled package test and focused lint passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed. Initial findings identified unbounded full-table/N+1 search, unchecked offset allocation, unstable cross-page row ordering, incomplete pagination/part-filter and destructive-failure tests, missing stable-ID verification, raw non-part refusal, and issue-workflow/doc inconsistencies. Fixes require a narrowing filter, scan complete 100-row pages only up to a 1,000-row fail-closed bound, reject oversized result windows before arithmetic/allocation, sort the complete bounded candidate set by row ID, validate row/part/template identities before deletion, return structured non-part clarification, and cover cross-page ordering plus delete/read-back failures. Focused Go, QA, product, and infosec reruns found no unresolved actionable findings. The package-coverage follow-up received focused Senior Go and Senior QA reruns; both found no actionable findings and confirmed the added cases exercise genuine production error and response-shape paths without concurrency or false-positive concerns.
- Residual risk: deletion confirmation is bound to one stable parameter-row ID rather than a snapshot token. The confirm call re-reads and validates the current row immediately before deletion and returns that deleted snapshot, but a concurrent value edit between the operator's preview and confirmed call can change the value that is removed. Searches that still match more than 1,000 upstream rows fail with clarification and require narrower filters.
- Acceptance:
  - Search supports sensible schema-backed filters, including template ID or unambiguous template name, value, category, part, and pagination where the InvenTree API supports them.
  - Results include stable part, category, template, value, and parameter-row IDs needed for review and retry.
  - Delete requires a stable parameter-row ID, `confirm:true`, and read-back verification that the row no longer exists.
  - Deletion refuses ambiguous natural-language requests, whole-template deletes, and bulk deletes; those remain separate workflows.
  - Unit and Testcontainers integration coverage exercise filtered search and confirmed single-row delete before the story is marked `Done`.

Tasks:

- [x] Define global parameter search filters from `docs/api-schema.yaml` and live API behavior.
- [x] Add typed client query support for cross-inventory parameter values.
- [x] Implement `search_part_parameters` or equivalent global parameter-value lookup.
- [x] Implement confirmed single-row parameter delete with read-back verification.
- [x] Update tool reference and operator recipes for cross-inventory parameter review.

### F-S13: Category Parameter Defaults

- Status: `Done`
- Issue: [#58](https://github.com/davidvanlaatum/inventree-mcp/issues/58)
- Depends on: milestone 1 complete, product review, and infosec review
- Progress: completed on `codex/f-s13-category-parameter-defaults`. The operator approved exact-category administration by default, optional inherited-default viewing, stable direct-link mutations, and removal of the unsupported requirement-metadata claim for the pinned InvenTree 1.4.3 API.
- Scope: manage `/api/part/category/parameters/` category parameter defaults using existing parameter templates by default. Exact-category links are the default administrative view; operators may explicitly include ancestor defaults for an effective inherited view, while mutations target only stable direct-link IDs.
- Acceptance:
  - List/search tools show category parameter defaults with stable link, category, and template IDs, default-value metadata, direct-versus-inherited source context, and pagination.
  - Exact-category administration is the default; callers may explicitly include ancestor defaults when reviewing the effective inherited set.
  - Create/update/delete tools require existing templates; they do not create templates implicitly from category-default requests.
  - Duplicate category/template links are detected before writes, mutations target stable direct-link IDs, and destructive deletes require `confirm:true` plus read-back verification.
  - Tool outputs clarify when the operator must choose an existing template, category, or duplicate-resolution path.
  - Unit and Testcontainers integration coverage exercise list, create, update, and confirmed delete before the story is marked `Done`.

Tasks:

- [x] Define category-default list/search filters from schema and live API behavior.
- [x] Add typed client methods for category parameter default list/create/update/delete.
- [x] Implement list/search category parameter defaults.
- [x] Implement existing-template-only create/update/delete with duplicate and confirmation guardrails.
- [x] Update tool reference, operator recipes, and parameter reuse prompt guidance.

- Validation: `go test -short ./...`, `golangci-lint run ./...`, and `git diff --check` pass. Default-on `go test -v ./internal/inventree ./internal/tools` passes against `inventree/inventree:1.4.3`, including exported client CRUD, exact child-category listing, explicit parent inheritance with the same template linked at both levels, partial update, confirmed delete, and not-found read-back. Short-test package coverage is non-regressing: `internal/tools` is 80.3% versus 79.9% on base `c3ee847`; all other package percentages are unchanged.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Initial findings requested embedded-detail reuse/caching, stable link/detail/mutation identity checks, actionable missing-ID and bounded-scan clarification, honest cross-tool duplicate retry semantics, request/page bounds, exact-view source validation, live parent/child inheritance coverage, delete verification failure tests, and stale operator/auth prose cleanup. All were addressed. Focused reruns after the behavior, tests, docs, and final `retry_tool` contract changes found no remaining actionable findings.
- Residual risk: category-default duplicate preflight and writes are not transactional, so a narrow concurrent-administration race remains; InvenTree's unique category/template constraint is authoritative. The inherited review view intentionally returns both direct and ancestor links with source context rather than collapsing same-template precedence. Filtered scans fail closed beyond 1,000 links or 11 pages.

### F-S14: Bulk Parameter Propagation And Audit Workflows

- Status: `Done`
- Issue: [#59](https://github.com/davidvanlaatum/inventree-mcp/issues/59)
- Depends on: F-S11, F-S12, F-S13, product review, QA review, and infosec review
- Progress: implemented on `codex/f-s14-bulk-parameter-workflows`. The operator approved explicit single-template value propagation, exact-category matching by default with opt-in descendants, create-missing behavior by default with explicit overwrite, no implicit template/category-link creation or deletion, a 100-part plan bound, and principal-bound five-minute single-use confirmation plans.
- Validation: `go test -short -count=1 ./...` passed; `golangci-lint run ./...` reported 0 issues; `go test -v -count=1 ./internal/inventree ./internal/tools` passed against pinned `inventree/inventree:1.4.3`, including exact-category/descendant selection, dry-run no-change, confirmed create/update, read-back, and audit behavior; `git diff --check` passed. Short-test package coverage is `91.3%` for `internal/inventree` versus `91.3%` on `origin/main`, and `81.2%` for `internal/tools` versus `80.9%` on `origin/main`.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed. Initial findings covered ineffective category/template narrowing, hidden same-name peers, propagation-only limits applied to audits, multiplicative scans, missing explicit cascade behavior, update-ID substitution, incomplete drift and verification boundaries, destructive-scope classification, raw upstream error disclosure, value length, ambiguous dry-run execution wording, and inconsistent budget/destructive documentation. Fixes apply both filters server-side, share a 1,000-unit request-and-record audit budget, cache category linkage, send explicit cascade values, preserve stable IDs, revalidate before every action, sanitize output and logs, require destructive scope, enforce the 500-character bound, and add focused unit plus pinned Testcontainers regressions. Final focused reruns from all four roles found no actionable findings.
- Residual risk: confirmation plans are process-local and expire after five minutes, so restarts require a fresh dry run. InvenTree does not expose atomic compare-and-set across the final per-action state check and write; operators must prevent concurrent parameter administration during confirmed execution, and any partial failure requires current-state inspection plus a fresh plan. Audits fail closed when the selected scope exceeds the shared 1,000-unit upstream request-and-record budget; a combined category/template scope has no finer slice in this tool.
- Scope: add safe bulk operator workflows for parameter propagation across matched parts and consistency audits that identify duplicate or overlapping templates and overloaded fields.
- Acceptance:
  - Bulk propagation supports `dry_run:true`, explicit part/category/template filters, duplicate detection, row-level planned actions, and operator confirmation before writes.
  - Execution performs read-back verification for every affected row and reports applied, skipped, failed, and manually required decisions.
  - Audit workflow finds duplicate/overlapping templates, same-name conflicting units/choices, overloaded fields, unlinked parameter usage, and category-default mismatches without writing.
  - Bulk workflows refuse broad unconstrained writes, hidden destructive cleanup, and ambiguous template/category selection.
  - Tests cover dry-run planning, duplicate detection, bounded execution, read-back verification, and no-write audit behavior with representative Testcontainers fixtures.

Tasks:

- [x] Define bounded filter and confirmation policy for bulk propagation.
- [x] Implement parameter consistency audit read-only workflow.
- [x] Implement dry-run bulk propagation planner.
- [x] Implement confirmed bulk propagation executor with read-back verification.
- [x] Update operator recipes and prompts for parameter audit and bulk propagation review.

### F-S15: Live Order Entry Tool Hardening

- Status: `Done`
- Issue: [#60](https://github.com/davidvanlaatum/inventree-mcp/issues/60)
- Depends on: milestone 1 complete, F-S19, F-S20, product review, QA review, and infosec review
- Progress: implementation completed on `codex/f-s15-live-order-entry-hardening`. The operator confirmed that manufacturer part numbers are optional: omitted, null, blank, or whitespace-only input must not invent a fallback MPN and must be normalized to an omitted value before mutation. Pinned InvenTree 1.4.3 rejects direct manufacturer-part creation without an MPN despite the snapshot declaring it optional; the combined workflow therefore reuses one exact part/manufacturer link, clarifies multiple links, or records a skipped step when none exists and continues the supplier-part path, while the direct create tool returns bounded validation.
- Validation: `GOFLAGS=-trimpath go test -race -p=1 ./...` passed, including default-on pinned InvenTree Testcontainers coverage; the focused no-MPN live order-entry subtest passed; `go test -tags no_integration_tests -cover ./...` passed with no package reduction versus `origin/main` and improved `internal/tools` from 81.3% to 81.9%; `go generate ./internal/tools`, `go mod tidy -diff`, `golangci-lint run ./...`, `go vet ./...`, and `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed. Findings covering accumulated-state loss after post-write lookup drift, optional-MPN reuse/clarification/skip behavior, MPN-aware remaining actions, exact nonblank MPN preservation, validation projection caps/deduplication, nested validation documentation, recovery-plan assertions, accurate failure reasons, transport/client error redaction, and cancellation/deadline sentinel preservation were fixed. Focused reruns by every affected role found no remaining actionable findings.
- Residual risk: pinned InvenTree 1.4.3 still rejects direct manufacturer-part creation without an MPN despite the schema declaring it optional; the combined workflow avoids that write when no existing link matches, and the direct tool returns bounded `validation_failed` output. Workflow preflight and execution remain non-transactional, so concurrent upstream changes can still interrupt later resolution; accumulated stable IDs, actions, clarification, remaining work, and recovery guidance are preserved, but earlier writes are not rolled back.
- Scope: turn the first live order-entry run from an eBay order page into regression coverage and cross-cutting tool hardening. F-S03 and F-S11 through F-S14 now own the purchase-order, parameter-template, parameter-value, category-default, and bulk-parameter endpoint families that originally required REST fallbacks. F-S19 and F-S20 own the remaining category and sourcing-link administration surfaces; this story retains end-to-end preflight, error, partial-failure recovery, and operator-workflow behavior.
- Acceptance:
  - Combined write workflows preflight every required clarification before mutation where practical; when a later step can still fail, the error response includes created IDs, completed actions, remaining actions, and a concrete recovery plan.
  - `create_manufacturer_part` and `upsert_part_with_supplier_and_manufacturer` preserve optional MPN semantics and normalize null, blank, or whitespace-only input to omission before calling InvenTree; they never invent a fallback value.
  - InvenTree API validation errors returned through tools include bounded allowlisted response fields with canonical non-echoing messages, such as `MPN: This field may not be blank`, while preserving token, URL, and sensitive-data redaction.
  - Dry-run or preflight behavior is available consistently for write tools that operators use during order entry, including company, supplier-part, manufacturer-part, part, category, parameter, image, and purchase-order writes supplied by this story or its completed dependencies. Existing complete duplicate/reference preflight satisfies this criterion where a separate dry run would add no safety.
  - The completed dependency surfaces provide enough read/search operations to avoid REST fallbacks during duplicate checks and recovery for categories, parts, supplier parts, manufacturer parts, purchase orders, purchase-order lines, parameter templates, parameter values, category parameter defaults, and image/attachment state.
  - Default-on pinned Testcontainers coverage reproduces the sanitized order-entry path without an MPN, uses a subtest-owned run, account, client, and run-prefixed records, proves no hidden partial state is left without recoverable IDs, and verifies through scoped pre/post searches that every intentionally created record is returned or recoverable. A live run may supplement but does not replace this blocking regression.
  - Tool reference, operator recipes, and prompt/operator guidance are updated for any new recovery, dry-run/preflight, read/search, or write behavior added by this hardening story.

Tasks:

- [x] Capture the live eBay order-entry workflow as an operator recipe and test scenario with sanitized fixture values.
- [x] Recheck the registered and documented tools delivered by F-S03, F-S11 through F-S14, F-S19, and F-S20 against the end-to-end order-entry workflow.
- [x] Normalize null, blank, and whitespace-only optional MPN input without inventing a fallback value.
- [x] Include bounded structured InvenTree validation details in tool outputs and errors with redaction tests.
- [x] Add recovery metadata to multi-step workflow failures, including any created part/company/link IDs.
- [x] Add dry-run/preflight support for lower-level write tools used during order entry where it is currently missing.
- [x] Add only cross-cutting read, duplicate-check, or recovery behavior that does not belong to the endpoint-family stories.
- [x] Update tool reference, operator recipes, and prompt/operator guidance for the hardened order-entry workflow.

### F-S16: MCP Go SDK v1.7 And 2026-07-28 Protocol Adoption

- Status: `Done`
- Issue: [#44](https://github.com/davidvanlaatum/inventree-mcp/issues/44)
- Depends on: M1A-S03, M1C-S04, and M1I-S02
- Scope: upgrade the official MCP Go SDK from `v1.6.1` to `v1.7.0`, adopt the MCP `2026-07-28` stateless/sessionless protocol behavior, preserve legacy-client compatibility, and explicitly configure the new HTTP request-safety controls without changing tool business behavior.
- Validation: `go test -race -p=1 ./...` passed, including the default-on InvenTree Testcontainers suites; focused config, protocol, OAuth discovery, cancellation, request-limit, and traffic-log tests passed; `go generate ./internal/tools` produced no unexpected generated changes; `golangci-lint run` reported 0 issues; `git diff --check` passed. One earlier parallel package run hit a transient Testcontainers reaper/network collision; the serial race run completed cleanly afterward.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed. Findings covering STDIO/HTTP config coupling, debug-log request forwarding, stateless POST session IDs, explicit STDIO negotiation, cancellation errors, authenticated discovery, traffic-log truncation docs, elicitation capability wording, and transport-specific operator guidance were fixed. Focused reruns by every affected role reported no remaining actionable findings.
- Residual risk: HTTP debug logging buffers up to the configured request limit per concurrent request, so operators must keep limits and reverse-proxy rate controls conservative. The 1 MiB JSON overhead is a policy allowance rather than a schema-derived maximum. Cancellation of an upstream write remains an unknown-result boundary. Live ChatGPT connector elicitation/handshake validation and its destructive-confirmation decision remain in F-S17; production OAuth startup remains separately gated.
- Acceptance:
  - The module pins `github.com/modelcontextprotocol/go-sdk` `v1.7.0`, and the ordinary test suite passes without compatibility escape flags.
  - STDIO and stateless streamable HTTP negotiate MCP `2026-07-28` through `server/discover` while legacy `initialize` clients remain supported.
  - Stateless HTTP ignores session IDs, rejects unsupported GET/DELETE session operations, and propagates aborted `2026-07-28` POST cancellation to in-flight handlers.
  - Streamable HTTP has an explicit bounded request-body limit large enough for the configured inline-upload limit after base64 and JSON overhead; invalid limit combinations fail configuration validation.
  - Tool annotations serialize explicit `readOnlyHint` and `idempotentHint` booleans and keep explicit false pointer hints for destructive/open-world behavior.
  - Protocol-boundary tests cover discovery, legacy initialization, sessionless behavior, request-body rejection, cancellation, annotations, OAuth scope enforcement, and request-scoped credential isolation.
  - `docs/PLAN.md`, tool reference, operator recipes, and SDK/protocol validation evidence are aligned with the adopted behavior.
  - The existing `_meta["securitySchemes"]` compatibility mirror remains documented because SDK `v1.7.0` still has no first-class top-level `securitySchemes` field on `mcp.Tool`.

Tasks:

- [x] Upgrade the official MCP Go SDK dependency and tidy transitive dependencies.
- [x] Add explicit streamable HTTP body-limit and request-cancellation options.
- [x] Validate the HTTP body limit against the configured inline-upload limit.
- [x] Update protocol and traffic-log tests for `server/discover` plus legacy initialization.
- [x] Add sessionless HTTP, oversized-body, and aborted-request cancellation coverage.
- [x] Update annotation wire-contract tests for explicit false booleans.
- [x] Re-run OAuth scope and request-scoped credential propagation tests on SDK `v1.7.0`.
- [x] Align plan, task, tool-reference, and operator documentation.

### F-S17: Native MCP Elicitation For Structured Clarifications

- Status: `Planned`
- Issue: [#45](https://github.com/davidvanlaatum/inventree-mcp/issues/45)
- Depends on: F-S16, product review, QA review, and live ChatGPT connector capability verification
- Scope: use MCP multi-round-trip requests and structured elicitation for missing or ambiguous operator input while preserving the existing `clarification_required` result contract as a compatibility fallback for clients that cannot complete elicitation.
- Acceptance:
  - A documented capability policy decides when handlers return native elicitation versus the existing structured clarification result.
  - Eligible clarification paths issue focused structured elicitation requests that collect only non-secret values the operator can reasonably supply.
  - Authentication credentials, tokens, uploaded content, and other secrets are never requested through elicitation.
  - MCP `2026-07-28` clients advertising the required elicitation capability can complete the clarification and retry cycle through MRTR without creating partial writes.
  - Legacy clients and clients missing elicitation capability receive the existing `clarification_required` output with stable retry fields and values.
  - Destructive confirmations remain enforced by the server and are not treated as satisfied merely because the client rendered an elicitation UI.
  - Unit and protocol-boundary tests cover accepted, declined, cancelled, malformed, unsupported-capability, legacy-client, retry-state, and no-partial-write paths.
  - Live ChatGPT connector validation proves at least one read ambiguity, one write preflight, and one destructive confirmation path before broad migration.
  - Tool reference, operator recipes, prompts, and public clarification contracts explain native elicitation and fallback behavior.

Tasks:

- [ ] Verify current ChatGPT connector MRTR and elicitation behavior against official docs and a live development connector.
- [ ] Resolve whether accepted native elicitation may supply an explicit destructive-confirmation value on retry or whether destructive confirmation must remain in the structured fallback/tool-input flow; rendering an elicitation UI alone never confirms an action.
- [ ] Define the hybrid native-elicitation and structured-fallback policy.
- [ ] Add reusable clarification-to-elicitation request and retry-state helpers.
- [ ] Convert a narrow representative clarification set before expanding across tools.
- [ ] Preserve destructive confirmation and no-partial-write boundaries.
- [ ] Add MCP `2026-07-28`, legacy-client, unsupported-capability, and cancellation tests.
- [ ] Run live connector validation and record evidence.
- [ ] Update tool reference, operator recipes, prompts, and task evidence.

### F-S18: Local CLI Self-Update

- Status: `Done`
- Issue: [#64](https://github.com/davidvanlaatum/inventree-mcp/issues/64)
- Depends on: M0-S04 and product, Go, QA, and infosec review of the update policy
- Scope: add an explicit local `inventree-mcp self-update` command for supported direct binary/archive installations. The command must remain a local CLI operation: it is not an MCP tool, is never callable through STDIO or HTTP MCP sessions, does not run automatically in the background, and does not let the server replace itself in response to a remote request. Package-managed installations must defer to their package manager instead of overwriting managed files.
- Acceptance:
  - The supported operating-system, architecture, installation-source, privilege, downgrade, prerelease, target-version, and release trust-root policy is decided and documented before implementation; unsupported cases fail without changing the installed binary and give actionable manual-update guidance. The trust decision either accepts canonical GitHub HTTPS and repository release control as the trust root with the residual checksum-only supply-chain risk documented, or requires an independently authenticated signature/attestation with a pinned verification identity or key.
  - The implementation evaluates a maintained Go self-update library before adding custom download, archive, checksum, or executable-replacement plumbing. The recorded decision compares release-asset and checksum compatibility, supported platform replacement semantics, redirect/archive controls, rollback and cross-process locking, injectable test seams, maintenance cadence, and dependency/security cost, and identifies every responsibility that remains custom.
  - Direct-install updates resolve an explicit GitHub release from the canonical repository, select the exact platform/architecture artifact, and verify its published checksum before extracting or replacing anything; metadata mismatch, missing checksum, unexpected archive contents, redirects outside policy, truncated downloads, and verification failures fail closed. Download and expanded-entry sizes are bounded during streaming and decompression; archives must contain exactly one expected regular executable and reject symlink, hardlink, device, duplicate, traversal, and trailing-payload entries.
  - Package-managed `deb`, `rpm`, and `apk` installations are detected conservatively or require an explicit supported installation marker, never overwrite package-owned `/usr/bin/inventree-mcp`, and return the appropriate package-manager/manual-upgrade guidance.
  - Replacement uses exclusively created unpredictable owner-only staging and backup files on a verified destination filesystem plus a deterministic, owner-validated lock path or directory with exclusive acquisition semantics. It defines executable, parent-directory, ownership, symlink, and hardlink policy; revalidates path and ownership state immediately before replacement; and refuses changed or unsafe state without attempting privilege elevation.
  - Before installation, the updater runs only the staged binary's bounded `version` command with disconnected stdin, bounded stdout/stderr, a safe working directory, and a minimal allowlisted environment that excludes auth, proxy, cookie, netrc, InvenTree, and MCP values, and requires the requested build version. Candidate execution occurs only after the artifact satisfies the selected release trust-root/signature policy; the known project `version` path is regression-tested to perform no network or configuration access, but this is not represented as a portable sandbox against a compromised trusted artifact unless the supported platform policy adds and tests an OS-level sandbox. The updater then atomically replaces the executable where the supported operating system permits it, preserves the defined mode and ownership policy plus a recoverable previous binary, repeats the bounded version check through the installed path, and atomically restores the previous binary, mode, and ownership state on failure. Platforms that cannot safely replace a running executable, including Windows unless a tested helper/relaunch design is selected, fail without mutation.
  - The platform-appropriate cross-process lock prevents simultaneous updaters from changing the executable or recovery artifacts, and its documented stale-lock policy permits safe recovery after an updater is killed without allowing overlapping replacements.
  - Update checks use a dedicated bounded HTTP client with no cookie jar or inherited server transport/auth state, allow only the canonical HTTPS GitHub API/release origins plus explicitly reviewed asset/CDN redirects, and never forward an optional GitHub token or other credential across origins. Update checks and replacements do not send InvenTree or MCP credentials or log sensitive environment values.
  - Deterministic tests cover current-version no-op, successful update, requested-version update, downgrade/prerelease policy, unsupported platform/install source, package-managed refusal, unavailable or ambiguous package ownership and install markers, unwritable targets without elevation attempts, release/asset mismatch, missing or incorrect checksums, redirect rejection and cross-origin credential stripping, truncated or oversized compressed downloads, decompression expansion limits, request timeout/cancellation, archive traversal and forbidden/unexpected/duplicate/trailing entries, and proof that no InvenTree or MCP authorization header is sent.
  - Filesystem security tests cover symlink/hardlink substitution, unsafe ownership or parent-directory state, destination state changing before replacement, exclusive staging/backup creation, deterministic owner-validated lock acquisition, and refusal without elevation. Candidate-process tests seed sensitive environment and proxy values and prove the allowlisted environment, disconnected input, output bounds, safe working directory, owner-only staging permissions, and known project `version` no-network/no-config behavior without claiming isolation from an artifact accepted by a compromised trust root.
  - Every no-op, unsupported/package/privilege refusal, network failure, metadata/checksum failure, and archive rejection test proves the installed executable and recovery artifacts remain unchanged and that staging files and locks are absent or deterministically cleaned up.
  - Replacement tests cover staged and installed version mismatch, malformed output, nonzero exit, timeout, interrupted replacement, cleanup failure, successful installed mode/ownership policy, exact binary/mode/ownership restoration after post-install sanity failure, and a platform-appropriate subprocess race proving a second updater cannot mutate the executable or recovery artifacts and that stale-lock recovery is safe after a killed updater.
  - README, release instructions, operator recipes, packaging notes, shell completion/help output if present, and `AGENTS.md` release guidance are aligned with the supported self-update behavior and its package-manager boundary.

Tasks:

- [x] Decide and document the supported direct-install and platform matrix, version-selection policy, release trust root and signature/attestation policy, residual supply-chain risk, and package-manager boundary.
- [x] Evaluate maintained Go self-update libraries against the release archive and checksum layout.
- [x] Add the local-only `self-update` CLI command and injectable release/download/filesystem/process seams.
- [x] Add verified artifact selection, bounded download, safe extraction, staged and installed version checks, atomic replacement, cross-process locking with killed-process recovery, and rollback.
- [x] Add package-managed install refusal and actionable manual-update guidance.
- [x] Add deterministic unit and platform-appropriate integration tests without invoking live updates against the developer's installed binary.
- [x] Update release, packaging, README, operator, CLI help, and agent guidance.
- [x] Run focused Go, QA, product, and infosec review and resolve or document findings.

- Validation: `go generate ./internal/tools`; `go mod tidy -diff`; `go test -race -p=1 ./...`; `go test -race -cover ./internal/selfupdate ./cmd/inventree-mcp`; `go vet ./internal/selfupdate ./cmd/inventree-mcp`; `golangci-lint run ./...`; `goreleaser check`; `goreleaser release --snapshot --clean`; Windows `amd64` updater test compilation; Linux `arm64` CLI build; release-archive and package-layout assertions; and `git diff --check` passed. Focused coverage is 78.0% for the new `internal/selfupdate` package and 90.5% for `cmd/inventree-mcp`, versus 87.9% for the CLI package on the current base. Every supported Linux/macOS direct archive contains exactly one `inventree-mcp` entry, while snapshot `deb` and `apk` packages retain `/usr/bin/inventree-mcp`.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer panels completed. Findings covered exact release-asset binding, Linux fixture portability, direct-install provenance, privileged and ancestor-path refusal, process-group timeouts, cross-process lock races, exhaustive no-mutation evidence, restartable transaction publication/rollback/cleanup states, ambiguous committed-record durability, operator rerun guidance after interrupted recovery, and manual `.previous` recovery. Code, tests, release configuration, task contracts, and operator docs were updated; final focused reruns found no actionable findings.
- Residual risk: release archives and `checksums.txt` share canonical GitHub repository/release control as one trust root, so checksum verification does not protect against compromise of that root. The adjacent adoption marker records the local operator's direct-archive assertion rather than independently proving provenance. Staged artifact execution uses a credential-free bounded process environment and process group, not an operating-system sandbox against an artifact accepted from a compromised trust root. Kernel advisory locking coordinates cooperating updater processes but does not defend against the same local owner intentionally modifying files outside the updater. Windows, package-managed installs, prereleases, downgrades, and automatic rollback remain unsupported by policy.

### F-S19: Part Category Administration

- Status: `Done`
- Issue: [#78](https://github.com/davidvanlaatum/inventree-mcp/issues/78)
- Depends on: milestone 1 complete, product review, QA review, and infosec review
- Scope: expose stable-ID part-category retrieval plus guarded category creation and PATCH-based editing so catalog setup and recovery do not require direct REST calls. Category deletion remains out of scope until a separate destructive-cleanup policy defines dependency handling for parts, child categories, defaults, and other references.
- Acceptance:
  - `get_part_category` retrieves one category by stable ID and returns the hierarchy and default metadata needed to review a create or update decision.
  - `create_part_category` requires an explicit name, resolves an optional existing parent and default location, preserves explicit structural and default-field choices, and refuses product-defined same-parent duplicates before writing. The duplicate policy explicitly decides normalization, case and surrounding-whitespace behavior, root-versus-parent matching, same-name records under another parent, and bounded pagination beyond the first page.
  - `update_part_category` preserves omitted versus explicit empty, false, and null values, validates the resulting parent/default-location identities, and prevents self-parenting or descendant-parent cycles before PATCH.
  - Reparenting and structural-state changes report affected hierarchy context and require an operator-approved policy for categories that already contain parts or child categories; the implementation does not guess when the safe behavior is unclear.
  - Create and update use read-before-write preflight, return stable IDs and recovery guidance for ambiguous outcomes, read back the result, and never delete or implicitly move parts, child categories, parameter defaults, or stock.
  - Exact reads require `inventree.read`; category create/update require `inventree.read` and `inventree.write` because preflight can disclose existing records. Product and infosec review explicitly classify reparenting and structural-state changes and add any higher-impact confirmation or scope requirement before implementation.
  - The endpoint manifest, schema provenance/capability notes, tool annotations, OAuth scopes, tool reference, operator recipes, prompts, and generated tool manifest are aligned.
  - Deterministic injected-transport unit coverage exercises post-persist response loss and recovery behavior. Default-on pinned Testcontainers coverage exercises exact retrieval, root and child creation, partial update, the approved duplicate matrix including later-page matches, invalid references, hierarchy-cycle safeguards, real recovery identities, and read-back verification.

Tasks:

- [x] Confirm the category create/update field set, duplicate normalization/pagination matrix, mutation classification, and the policy for reparenting or changing structural state when descendants or assigned parts exist.
- [x] Add missing endpoint-manifest entries and typed category create/update client methods.
- [x] Expose stable-ID category retrieval plus guarded create/update tools.
- [x] Add duplicate, hierarchy, reference, ambiguous-result, and read-back tests against the pinned InvenTree API.
- [x] Align schema notes, tool reference, operator recipes, prompts, and generated manifests.
- [x] Run focused Go, QA, product, and infosec review and resolve or document findings.

Decision: expose every schema-writable category field; trim and compare names case-insensitively within one exact parent; treat roots separately and allow the same name under another parent; scan at most 1,000 siblings in 100-row pages and fail closed; permit confirmed reparenting with direct parts and/or descendants while refusing cycles; require confirmation for structural changes and refuse promotion while direct parts exist; classify category administration as closed-world non-destructive read/write work.

- Validation: `GOFLAGS=-trimpath go test -race -p=1 ./...`, pinned `TestMilestoneHappyPathToolsAgainstInvenTree/part_category_administration`, focused skipped-Docker package tests, `go vet ./...`, generated-manifest/documentation tests, per-package `go test -p=1 -cover ./...` comparison against the current base, and `git diff --check` passed. Coverage has no package reductions: `internal/tools` remains 81.2%, `internal/testenv` improves from 66.4% to 67.0%, and `internal/inventree` improves from 91.3% to 91.4%.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed. Findings covered exact typed recovery comparisons, stable response identities, full supplied-field create recovery, normalized legacy names, deterministic and live post-persist response loss, bounded cycle traversal, post-write verification, sanitized errors, and the complete duplicate/reference/hierarchy matrix. Focused reruns found no unresolved actionable findings.
- Residual risk: category preflight, mutation, and post-write verification are separate upstream REST operations. Another MCP server, the InvenTree UI, or a direct API client can still race same-parent uniqueness or hierarchy state; operators must use single-writer coordination and inspect current stable-ID state after any `partial_failure`. Simple `confirm:true` is intentionally not state-bound, but the confirmed call freshly repeats preflight before writing.

### F-S20: Company And Sourcing Link Maintenance

- Status: `Done`
- Issue: [#79](https://github.com/davidvanlaatum/inventree-mcp/issues/79)
- Depends on: milestone 1 complete, product review, QA review, and infosec review
- Progress: implementation completed on `codex/f-s20-company-sourcing-maintenance`; publication and merge tracking continue in the linked issue and implementing pull request.
- Scope: expose exact reads and guarded partial updates for companies, supplier-part links, and manufacturer-part links, plus the missing sourcing-link search tools needed for duplicate checks and recovery. Deletion and customer/sales workflows remain out of scope.
- Acceptance:
  - Stable-ID reads are available for companies, supplier parts, and manufacturer parts, and search tools expose schema-backed supplier/manufacturer/part/SKU/MPN filters with bounded pagination and deterministic results.
  - Company updates preserve omitted versus explicit values, exclude `is_customer` from tool input and reject attempted customer-role mutation, and refuse supplier/manufacturer role changes that would invalidate an in-scope operation without operator clarification. Customer/sales, address, and contact administration remain deferred.
  - Supplier-part and manufacturer-part updates validate every referenced part, company, and link identity, preserve explicit false/empty/null values where supported, and preflight duplicate SKU/MPN or equivalent sourcing identities before PATCH.
  - Update error paths return only a minimal allowlisted recovery projection with sanitized upstream field details, stable IDs and current records when known or uniquely recoverable, candidate matches plus read-before-retry guidance otherwise, and URL userinfo/query redaction. Upstream bodies, logs, and tool errors do not expose tokens, unrelated contact/tax/note fields, sensitive URLs, or other operator data. Cross-cutting hardening of the existing create tools remains owned by F-S15.
  - No tool deletes companies or sourcing links, creates customer/sales state, or mutates unrelated purchase-order or attachment records.
  - Exact reads require `inventree.read`; preflighting company and sourcing-link updates require `inventree.read` and `inventree.write`. Product and infosec review explicitly classify supplier/manufacturer role changes and add any higher-impact confirmation or scope requirement before implementation.
  - Endpoint-manifest entries, typed client methods, tool annotations, OAuth scopes, tool reference, operator recipes, prompts, and generated manifests are aligned.
  - Deterministic injected-transport unit coverage exercises post-persist response loss, minimal recovery projection, and redaction. Default-on pinned Testcontainers coverage exercises search/get/update success, role and identity validation, duplicate refusal, omitted-versus-explicit patch behavior, real recovery identities, and read-back verification for all three record families.

Tasks:

- [x] Confirm supported company and sourcing-link patch fields, duplicate identities, role-change classification/policy, and the minimal allowlisted recovery projection.
- [x] Complete endpoint-manifest and typed-client coverage, including stable manufacturer-part retrieval.
- [x] Add company, supplier-part, and manufacturer-part stable-ID read and search tools.
- [x] Add guarded company, supplier-part, and manufacturer-part update tools.
- [x] Add duplicate, role, reference, redaction, ambiguous-result, and read-back tests against the pinned InvenTree API.
- [x] Align tool reference, operator recipes, prompts, schema notes, and generated manifests.
- [x] Run focused Go, QA, product, and infosec review and resolve or document findings.

Decision: company updates may edit name, description, website, currency, active, supplier/manufacturer roles, and notes. Customer-role, contact, tax, tag, and image mutation remain out of scope; exact reads may report customer-role state. Company tags were dropped after pinned live validation showed that the company detail schema and stable GET response do not expose them, so replacement could not be verified or safely recovered. Adding a supplier/manufacturer role is an ordinary guarded update. Removing one requires `confirm:true` and is refused while corresponding sourcing links exist. Supplier-part updates cover identity, description, link, active/primary, manufacturer-part association, packaging, pack quantity, and short note; manufacturer-part updates cover identity, MPN, description, and link. Availability, barcode, long notes, and tags on sourcing links remain out of scope. Duplicate identity is normalized exact supplier plus SKU or manufacturer plus MPN matching. Recovery and error output is allowlisted to stable identity and role/state fields with redacted links; it never exposes notes, contact, tax, or raw upstream bodies.

- Validation: `go generate ./internal/tools` refreshed the checked tool manifest; `go test -tags no_integration_tests -cover ./internal/inventree ./internal/tools` passed with `internal/inventree` at 91.6% and `internal/tools` at 81.3%, above the current base at 91.4% and 81.2%; `go test -race ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$' -count=1` and `go test -race ./internal/tools -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/company_and_sourcing_link_administration$' -count=1` passed against the pinned InvenTree Testcontainers stack; `GOFLAGS=-trimpath go test -race -p=1 ./...` passed with every default-on Docker suite; `go vet ./...`, `golangci-lint run ./...`, `go mod tidy -diff`, and `git diff --check` passed. The unrelated-process-sensitive self-update regression was stabilized by giving ordinary subprocess cases a five-second setup bound while retaining its independent 50 ms timeout and three-second wall-clock assertion; focused repeated race coverage and the full suite passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed. Findings covered sanitized minimal recovery, nullable exact comparisons, bounded deterministic snapshots, duplicate candidates, reference and reverse-link integrity, post-write race detection, complete pinned-live coverage, exact supplier filtering, company-tag verification, and explicit customer-role wording. Company tags were removed from the contract after the operator decision; exact supplier filtering and all recovery, integrity, prompt, and test findings were addressed. Focused Go, QA, and product reruns found no remaining actionable findings, and the final infosec review found no actionable findings.
- Residual risk: preflight, PATCH, and post-write verification remain separate upstream REST operations, so another MCP server, the InvenTree UI, or a direct API client can still race duplicate or relationship state. Detected post-write divergence returns `partial_failure` with minimal stable-ID state; operators must use single-writer coordination and inspect the current record before retrying. Completeness-sensitive duplicate and dependency scans fail closed above the documented 1,000-row bound. Company tags remain intentionally unsupported because the pinned stable schema and detail response cannot verify or recover their replacement state.

### F-S21: Stock Location And Stock Record Administration

- Status: `Done`
- Issue: [#80](https://github.com/davidvanlaatum/inventree-mcp/issues/80)
- Depends on: milestone 1 complete, F-S05, product review, QA review, and infosec review
- Scope: expose exact stock-location and stock-item reads, add guarded stock-location creation and editing, and define a constrained stock-item metadata update surface without bypassing the confirmed quantity, stocktake, status, or receiving workflows. Location or stock-item deletion remains out of scope.
- Acceptance:
  - `get_stock_location` and `get_stock_item` expose stable-ID records and the hierarchy, ownership, quantity, status, serial, batch, packaging, and source-order context needed for administration and recovery.
  - Stock-location create/update validates the optional parent and preserves omitted versus explicit null/false values for the selected nullable and boolean fields. Product review decides whether `owner` and `location_type` are supported; supported references require bounded manifest-backed lookup/get client and tool coverage plus valid/invalid-reference tests, while unsupported fields are omitted from input and refused. Location-type administration itself remains deferred.
  - Stock-location duplicate policy explicitly decides normalization, case and surrounding-whitespace behavior, root-versus-parent matching, same-name records under another parent, and bounded pagination beyond the first page; self-parenting and descendant-parent cycles are always refused.
  - Product review explicitly selects the stock-item metadata fields that may be updated. A generic upstream PATCH is not exposed; quantity, count, status, receipt, serialization, installation, and physical location movement are out of scope for F-S21 and remain routed through existing dedicated current-state-bound operational workflows or a separately approved backlog story.
  - Any approved stock-record metadata update binds confirmation and recovery behavior to current state where the change can affect traceability, availability, or allocation, and reads back the exact record after mutation.
  - No tool deletes locations or stock items, silently relocates stock, edits quantity/status through generic PATCH, or bypasses serialized-stock and `delete_on_deplete` safeguards.
  - Exact reads require `inventree.read`; preflighting location create/update requires `inventree.read` and `inventree.write`; any approved stock-record mutation requires `inventree.read`, `inventree.write`, and `inventree.operational`. Product and infosec review explicitly classify location reparenting and structural/external changes and add any higher-impact confirmation or scope requirement before implementation.
  - Endpoint-manifest entries, typed client methods, mutation classifications, OAuth scopes, tool reference, operator recipes, prompts, and generated manifests are aligned.
  - Deterministic injected-transport unit coverage exercises post-persist response loss and recovery behavior. Default-on pinned Testcontainers coverage exercises exact reads, root and child location creation, partial location update, the approved duplicate matrix including later-page matches, cycle refusal, valid/invalid approved owner and location-type references, approved stock metadata behavior, operational-boundary enforcement, real recovery identities, and read-back verification.

Tasks:

- [x] Decide the supported stock-location fields, duplicate normalization/pagination matrix, mutation classification, owner/location-type lookup policy, and narrow non-location stock-item metadata boundary before implementation; create a separate backlog story if physical relocation is required.
- [x] Add missing location create/update, any approved bounded owner/location-type read dependencies, and any approved stock-item endpoint-manifest and typed-client methods.
- [x] Add stable-ID stock-location and stock-item retrieval tools.
- [x] Add guarded stock-location create/update and only the approved constrained stock-record mutation tools.
- [x] Add hierarchy, duplicate, reference, serialization, operational-boundary, ambiguous-result, and read-back tests against the pinned InvenTree API.
- [x] Align schema notes, tool reference, operator recipes, prompts, and generated manifests.
- [x] Run focused Go, QA, product, and infosec review and resolve or document findings.

Decision: stock-location create/update supports name, description, parent, custom icon, structural, external, owner, and location type. Duplicate identity is a trimmed case-insensitive name under the exact parent; root and child namespaces are distinct, same names under different parents are allowed, scans are bounded at 1,000 records, and completeness-sensitive checks fail closed above that bound. Reparenting and structural/external changes require a current-state plan, `inventree.operational`, and explicit confirmation; other location writes require `inventree.read` and `inventree.write`. Stock-item metadata updates are limited to batch, expiry date, packaging, notes, and external link and always require a current-state-bound confirmation plus `inventree.read`, `inventree.write`, and `inventree.operational`. Location, quantity, status, serial, ownership, supplier/source links, pricing, `delete_on_deplete`, installation, and order/build fields remain excluded.

- Validation: `go test -tags no_integration_tests -cover ./internal/inventree ./internal/tools` passed with `internal/inventree` at 91.8% and `internal/tools` at 81.3%, compared with the current base at 91.6% and 81.3%; `go test -race ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/stock_location_and_metadata_administration$' -count=1` and `go test -race ./internal/tools -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/stock_location_and_metadata_administration$' -count=1` passed against the pinned InvenTree Testcontainers stack. The expanded tool scenario covers actual reparenting, root-versus-child and different-parent namespaces, later-page duplicates, protected-state plan invalidation, and live post-persist response loss for location create, ordinary location PATCH, restructuring PATCH, and stock metadata PATCH. `GOFLAGS=-trimpath go test -race -p=1 ./...` passed with every default-on Docker suite; `go vet ./...`, `golangci-lint run ./...`, `go mod tidy -diff`, and `git diff --check` passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed because F-S21 adds mutating tool surface, operational hierarchy and stock-metadata workflows, pinned-live validation, and public operator contracts. Initial findings covered stale derived hierarchy fields in reparenting plans, incomplete current-state binding for destination hierarchy and custom status, non-positive reference classification, missing owner discovery, stale mutation-class prose, ambiguous ownership wording, incomplete pinned-live namespace/recovery coverage, and missing stale protected-state tests. Fixes introduced a coherent stable-ID hierarchy projection with source/destination context, complete metadata binding with sanitized public links, structured invalid-reference clarification, aligned prompts/docs, and expanded deterministic and pinned-live coverage. Focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: duplicate/reference preflight, PATCH, and read-back remain separate upstream REST operations, so concurrent UI or API writers can still race between them. Current-state plan hashes detect hierarchy or stock-state changes observed before confirmation, and post-write divergence returns `partial_failure`, but InvenTree offers no atomic conditional mutation for the final write interval. Operators should coordinate a single writer and inspect the exact stable ID before preparing a fresh plan after any partial failure. Completeness-sensitive sibling scans fail closed above 1,000 records.

### F-S22: Reviewable Dry-Run Mutation Plans

- Status: `Done`
- Issue: [#88](https://github.com/davidvanlaatum/inventree-mcp/issues/88)
- Depends on: M1G-S01, M1G-S02, and F-S03
- Progress: implementation started on `codex/issue-88-reviewable-dry-runs` from `v0.0.2`. The dry-run audit found the same action-only preview gap in part upsert, initial stock creation, combined purchase-order creation, and the single-state purchase-order issue action; other dry-run tools already expose explicit before/after or row-level plans.
- Dry-run audit: `merge_parameter_templates` exposes per-row source and target values; `bulk_propagate_part_parameters` exposes template/value selectors plus per-part planned/skipped/manual actions; `adjust_stock_quantity`, `stocktake_adjustment`, and `set_stock_status` expose complete before/after stock snapshots; `restructure_stock_location` and `update_stock_item_metadata` expose complete before/after state; `receive_purchase_order_items` exposes resolved per-line quantities, locations, packaging, pricing, and outstanding state. `preview_purchase_order_with_lines` is read-only rather than a mutation dry run. These surfaces need no additive `planned_changes` wrapper.
- Scope: add one additive field-level `planned_changes` contract to workflow dry runs whose action summaries do not expose the effective mutation payload. Keep stable record outputs factual, represent unresolved create dependencies explicitly, and preserve existing action ordering and no-write behavior.
- Acceptance:
  - `upsert_part_with_supplier_and_manufacturer` exposes every effective part, company, supplier-part, and manufacturer-part create or patch field, including explicit false and empty values where the execution path preserves them.
  - `create_initial_stock_entry` exposes the complete planned stock-item create fields after stable part and location resolution.
  - `create_purchase_order_with_lines` exposes complete order and line create or patch fields, including derived references, quantities, prices, currency, destinations, dates, and notes.
  - `issue_purchase_order` exposes the stable target order, complete current line state, proposed `PLACED` status, and a current-state hash required for confirmation.
  - Planned changes identify stable target IDs where available and name earlier planned creates in `depends_on` when a foreign key does not exist yet.
  - Factual record fields never use synthetic zero-ID placeholders for planned creates.
  - Other dry-run workflows are audited; already-complete explicit plan contracts remain unchanged.
  - Tool reference, operator recipes, prompts where applicable, and deterministic regression coverage stay aligned with the response contract.

Tasks:

- [x] Add the shared `planned_changes` output contract.
- [x] Fix part-upsert dry-run previews and the issue #88 regression case.
- [x] Fix analogous initial-stock and purchase-order workflow previews.
- [x] Audit remaining dry-run tools and record why their explicit plans are already reviewable.
- [x] Update public tool/operator documentation and task evidence.
- [x] Run focused Go, QA, product, and infosec review and resolve or document findings.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go vet ./...`, `golangci-lint run ./...`, `GOFLAGS=-trimpath go test -race -p=1 ./...`, and `git diff --check` passed. `go test -tags no_integration_tests -cover ./...` passed with `internal/tools` at 82.5%, compared with 81.9% on the exact `origin/main` base; no package-level coverage percentage decreased. After the final confirmation-contract fix, `go test ./internal/tools` passed again, including the default-on pinned InvenTree Testcontainers suite.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Developer reviews completed because F-S22 changes Go behavior, test and wire contracts, mutation review workflow, and public tool documentation. Initial findings covered synthetic zero-ID company records, missing direct-ID role validation, unfielded create dependencies, omitted purchase-line safety fields, incomplete MCP wire coverage, a missing durable dry-run inventory, an unbound purchase-order issue confirmation, and overstatement of the final upstream race boundary. Fixes introduced factual company reads with role checks, field-addressed dependencies, explicit `auto_pricing:false` and `merge_items:false`, structured-content schema/value tests, the complete audit above, sorted full-order/line hashing, dry-run-only hash disclosure, and explicit single-writer/read-before-retry guidance. Focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: part-upsert, initial-stock, and combined purchase-order `planned_changes` are advisory previews; execution repeats preflight and can observe different upstream state after an intervening writer. Purchase-order issue confirmation rejects order or line changes observed before placement, but InvenTree has no atomic conditional issue operation across the final preflight reads and placement request. Operators should coordinate a single writer while issuing and inspect current order and line state before retrying an ambiguous result.

### F-S23: Purchase Order Extra Line Items

- Status: `Done`
- Issue: [#90](https://github.com/davidvanlaatum/inventree-mcp/issues/90)
- Depends on: F-S03 and F-S22
- Progress: implementation started on `codex/f-s23-po-extra-lines` from `v0.0.3`. Product review selected standalone search/read/create/update/confirmed-delete tools plus extra-line support in `create_purchase_order_with_lines`. MCP-managed creates require a nonblank reference that is unique within the target purchase order so `(purchase_order_id, trimmed reference)` can drive deterministic duplicate detection and response-loss recovery. Signed unit prices are supported, including zero-priced informational lines and negative discount lines. Project-code mutation is excluded until a separate lookup and validation contract is approved.
- Scope: expose InvenTree purchase-order extra lines as non-receivable supplier/invoice context while preserving the existing distinction from supplier-part purchase-order lines. Support schema-backed fields `order`, `quantity`, `price`, `price_currency`, `reference`, `description`, `line`, `link`, `notes`, and `target_date`; do not expose `project_code`. Add field-level advisory dry-run plans for creates and updates, explicit confirmed single-record deletion, stable-ID read-back, bounded duplicate/recovery scans, sanitized links and validation errors, and accumulated recovery state when the combined workflow partially succeeds.
- Acceptance:
  - Search supports purchase-order ID and schema-backed text/pagination filters; exact reads verify the returned stable ID.
  - Create and update validate the target purchase order, signed unit price and currency pairing, nonnegative quantity, dates, URLs, field lengths, and the MCP-managed `(purchase_order_id, trimmed reference)` uniqueness contract before writing.
  - Dry runs expose complete field-level extra-line create or patch plans without writing; confirmed execution repeats preflight and reads back the stable record and exact target purchase order.
  - A retry after ambiguous create response loss recovers one exact matching extra line by purchase order and reference instead of creating a duplicate; ambiguous or conflicting matches return structured clarification and read-before-retry guidance.
  - `create_purchase_order_with_lines` accepts extra lines, includes them in ordered dry-run plans, creates or updates them after normal supplier-part lines, and preserves accumulated order, normal-line, extra-line IDs and actions on partial failure.
  - Signed unit prices include zero and negative values. Currency is required whenever price is supplied, and refreshed purchase-order totals preserve InvenTree's exact server-calculated representation.
  - Delete previews one stable extra line and requires `confirm:true`; success is returned only after stable-ID not-found verification and requires destructive scope.
  - Unit, MCP-boundary, and default-on pinned InvenTree 1.4.3 Testcontainers coverage exercise CRUD, dry-run no-write behavior, validation, currency and signed-price behavior, duplicate and response-loss recovery, combined-workflow partial failure, and confirmed deletion.
  - Tool reference, operator recipes, purchase-order prompt guidance, generated tool manifest, API capability notes, and task/issue evidence distinguish extra lines from receivable supplier-part lines.

Tasks:

- [x] Add typed purchase-order extra-line client list/get/create/PATCH/delete methods.
- [x] Add guarded standalone extra-line tools and annotations/scopes.
- [x] Extend `create_purchase_order_with_lines` dry-run, recovery, and partial-failure behavior.
- [x] Add deterministic and pinned-live coverage, including exact total and currency behavior.
- [x] Update generated and public documentation contracts.
- [x] Run the full Go, QA, product, and infosec review panel and resolve or document findings.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go vet ./...`, `golangci-lint run ./...`, `GOFLAGS=-trimpath go test -race -p=1 ./...`, and `git diff --check` pass. Focused pinned InvenTree 1.4.3 runs for `TestClientMethodsAgainstInvenTree/po` and `TestMilestoneHappyPathToolsAgainstInvenTree/purchase_order_create_and_retry_happy_path` pass with live list/get/create/PATCH/delete, zero and negative prices, exact total deltas/restoration, standalone and combined recovery, deletion, placement, and receipt coverage. `go test -tags no_integration_tests -cover ./...` passes; `internal/tools` is 81.9% versus 82.5% on exact `origin/main`, while every other package percentage is unchanged. Focused tests materially cover the new bounded scans, sanitized errors, exact retry/no-second-create behavior, ambiguous candidate and zero-candidate recovery, later-extra partial failure, uncertain deletion, cross-order total refresh, and stale issue-confirmation state; QA accepted the remaining 0.6-point package dilution as no uncovered acceptance or safety gap.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed because F-S23 adds mutating and destructive tool surface, changes OAuth scopes and operator contracts, and adds pinned-live coverage. Findings covered unusable candidate projections and recovery routing, definite-versus-ambiguous errors, incomplete accumulated-state and bounded-scan tests, stale deletion and cross-order totals, byte-counted references, missing structured identity guidance, exact signed-total assertions, read-scope omissions, planned-link/hash sanitization, and a hidden-URL hash oracle. Fixes add stable sanitized candidates, search-versus-get recovery tools, complete refreshed affected-order output, rune-count validation, structured not-found/clarification paths, exact and failure-focused tests, read-plus-write scope enforcement, sanitized planned links, and confirmation hashes over the operator-visible sanitized projection. Final focused Go, QA, product, and infosec reruns found no actionable findings.
- Residual risk: duplicate preflight, mutation, read-back, and total refresh are separate InvenTree requests, so a concurrent UI or API writer can race the final interval. Ambiguous results preserve accumulated stable IDs, actions, sanitized candidates, and explicit search/read recovery guidance; operators should coordinate a single writer and inspect current state before retrying. Completeness-sensitive scans fail closed above 1,000 extra lines. F-S39 later superseded the original query/fragment-stripping rule: complete valid credential-free links now participate in public issue plans and hashes, while clarification and minimal recovery projections omit them.
- 2026-08-09 follow-up: a review pass found `delete_purchase_order_extra_line` propagated a raw error (opaque `isError:true`) instead of a structured `clarification_required` response when the extra line's parent purchase order could not be found (deleted or otherwise missing) between the extra-line read and the order lookup, unlike create/update's existing `order_id`-not-found handling in the same file. Fixed by checking `isNotFound` on the `loadExtraLineOrder` result in the delete handler and returning a hard-error clarification; `TestPurchaseOrderExtraLineDeleteOrderNotFound` covers the new path. The sibling `delete_purchase_order_line` tool (F-S32) has the same class of gap; it is being addressed separately in another worktree rather than here. Senior Go Developer and Senior QA / Test Architect review of this follow-up found no actionable findings.

### F-S24: Guarded Delete-On-Deplete Stock Depletion

- Status: `Done`
- Issue: [#92](https://github.com/davidvanlaatum/inventree-mcp/issues/92)
- Depends on: F-S05, F-S21, product review, QA review, and infosec review
- Progress: implemented on `codex/f-s24-guarded-stock-depletion` from `947fd503`. Product review selected a dedicated destructive tool which removes the complete current positive quantity of one stable stock item through the native stock-removal endpoint. The tool preserves the existing non-destructive adjustment and stocktake contracts. It may act only on `delete_on_deplete:true` ordinary available stock and rejects allocation, serialization, active build or consumption state, installation, and parent/child stock relationships. Purchase-order, supplier-part, and completed-build provenance is displayed and bound into confirmation state but does not independently block depletion.
- Scope: add one dedicated current-state-bound destructive tool for intentionally removing the entire current quantity of an explicitly identified safe `delete_on_deplete` stock item, with an audit reason, principal-bound short-lived single-use confirmation, absence verification, and response-loss recovery. Do not add search-and-delete mutation, generic stock deletion, partial quantity input, implicit opt-in on the existing quantity tools, or support for linked unsafe stock.
- Acceptance:
  - The tool requires one positive stable stock-item primary key, a nonblank audit reason, `dry_run:true` before execution, `confirm:true`, and the exact opaque principal-bound five-minute single-use token from the matching current-state plan.
  - A dry run for any positive current quantity shows part and stock-item IDs, current quantity, proposed zero quantity, location, status, allocation, serialization, build/consumption, installation and parent/child context, supplier/purchase provenance, `delete_on_deplete:true`, and an explicit `will_delete:true` high-risk outcome.
  - Execution repeats exact-ID preflight, rejects stale or reused confirmation tokens without writing, removes the complete reviewed quantity through `/api/stock/remove/` with the audit reason, and succeeds only after exact-ID not-found verification.
  - The tool rejects zero/depleted stock, `delete_on_deplete:false`, allocated or serialized stock, active build or consumed stock, installed stock, and stock with parent/child relationships using actionable structured clarification.
  - If stock removal succeeds but the response is lost, exact-ID not-found verification recovers the operation as already applied; other ambiguous or unverifiable outcomes return structured partial-failure recovery without blind retry guidance.
  - Context cancellation and deadline error identity are preserved through mutation and recovery paths.
  - The tool is classified as destructive and operational, uses `destructiveHint:true`, remains closed-world and non-idempotent, and requires `inventree.read`, `inventree.write`, `inventree.operational`, and `inventree.destructive` in HTTP OAuth mode.
  - Existing `adjust_stock_quantity` and `stocktake_adjustment` zeroing guards remain unchanged.
  - Typed client, MCP-boundary, authorization, generated-manifest, deterministic response-loss, and default-on pinned InvenTree 1.4.3 Testcontainers coverage exercise successful quantity-one and larger-quantity deletion, unsafe-state refusal, stale/reused tokens, absence verification, and recovered response loss.
  - API capability notes, tool reference, stocktake prompt, operator recipes, and generated tool manifest stay aligned.

Tasks:

- [x] Resolve the tool shape, supported quantities, unsafe-state policy, provenance behavior, scope classification, and response-loss recovery semantics.
- [x] Add the required stock-item safety fields and typed client behavior.
- [x] Add the dedicated guarded depletion tool, confirmation plan, scopes, annotations, and recovery behavior.
- [x] Add deterministic, MCP-boundary, authorization, and pinned-live coverage.
- [x] Align schema notes, tool reference, operator recipes, prompts, and generated manifests.
- [x] Run the full Go, QA, product, and infosec review panel and resolve or document findings.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go vet ./...`, `golangci-lint run ./...`, `GOFLAGS=-trimpath go test -race -p=1 ./...`, and `git diff --check` pass. `go test -tags no_integration_tests -cover ./internal/inventree ./internal/tools` reports 91.8% and 82.1% respectively, versus 91.8% and 82.0% from the exact base commit. Focused pinned InvenTree 1.4.3 runs for `TestClientMethodsAgainstInvenTree/stock_adjustments` and `TestMilestoneHappyPathToolsAgainstInvenTree/stock_adjustment_happy_path` pass with live quantity-two deletion, quantity-one lost-response recovery, absence verification, and serialized-stock refusal.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed because F-S24 adds destructive operational tool surface and changes public operator contracts. Initial findings requested live unsafe-state refusal, recovery-readback cancellation and zero-quantity boundaries, fail-closed negative relationship counts, consistent no-blind-retry prompt guidance, and serial-first refusal compatible with pinned InvenTree behavior. The implementation and focused coverage address each finding. Final focused Go, QA, product, and infosec reruns found no actionable findings.
- Residual risk: preflight, removal, and absence verification are separate InvenTree requests, so a concurrent UI or API writer can race the interval. After an ambiguous removal, exact-ID not-found proves absence but cannot distinguish this operation from a concurrent authorized removal. Operators should coordinate a single writer and inspect the stable stock-item ID before starting a new plan.

### F-S25: InvenTree Tool And Server Icons

- Status: `Done`
- Depends on: F-S16
- Progress: implementation started on `codex/f-s25-inventree-tool-icons` from `v0.0.5`. The selected shared asset is the official documentation-hosted PNG at `https://docs.inventree.org/en/latest/assets/logo.png` so current InvenTree branding can be reused without bundling a duplicate image.
- Scope: publish the official InvenTree logo through standard MCP icon metadata for every registered tool and for the MCP server implementation identity. Keep rendering client-controlled and do not add an Apps SDK widget, Codex plugin package, or new tool behavior.
- Acceptance:
  - Every registered tool descriptor exposes exactly one icon with source `https://docs.inventree.org/en/latest/assets/logo.png` and MIME type `image/png`.
  - The server implementation identity exposes the same icon through standard MCP initialization metadata.
  - One shared definition supplies the URL and icon metadata so server and tool branding cannot drift.
  - Deterministic SDK-boundary tests verify tool-list and initialization metadata, including the serialized `src` and `mimeType` fields.
  - The checked tool manifest and public tool reference record the icon contract and explain that MCP clients decide whether and where to render it.
  - Existing tool names, annotations, OAuth scopes, input/output schemas, and handler behavior remain unchanged.

Tasks:

- [x] Add shared official InvenTree icon metadata.
- [x] Attach the icon to every tool descriptor and the server implementation identity.
- [x] Add deterministic descriptor, initialization, and checked-manifest coverage.
- [x] Align the public tool reference and task evidence.
- [x] Run Go, QA, product, and infosec review and resolve or document findings.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go vet ./...`, `golangci-lint run ./...`, `GOFLAGS=-trimpath go test -race -p=1 ./...`, and `git diff --check` pass. `go test -p=1 -tags no_integration_tests -cover ./...` passes with `internal/server` at 76.2% and `internal/tools` at 82.1%, unchanged from the exact `origin/main` base. The first concurrent current/base coverage attempt hit the known unrelated `TestRunCommandUsesIsolatedBoundedProcess` timeout; the exact-base run passed and the required serial `-p=1` current-tree rerun passed.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed because F-S25 changes the public MCP tool surface and documentation contract. Findings required raw JSON-RPC assertions for the `server/discover` identity and every `tools/list` icon, binding the server assertion specifically to the discovery result, and documenting mutable external-asset availability, caching, content/MIME drift, and cross-origin privacy/trust behavior. The tests and docs address each finding; focused Go, QA, product, and infosec reruns found no actionable findings.
- Residual risk: MCP clients control icon rendering and may ignore standard icon metadata, cache an older image, or continue showing a generic tool glyph. The externally hosted mutable `en/latest` asset intentionally follows upstream branding, so its availability, content, or MIME behavior can change independently of an `inventree-mcp` release. Rendering can make a cross-origin request to `docs.inventree.org` that exposes client network metadata such as egress IP, user agent, and request timing. Clients should fetch without credentials, validate decoded image content independently of the advisory MIME type, and may reject the icon under stricter external-content policy.

### F-S26: MCP Functionality-Gap Guidance

- Status: `Done`
- Issue: [#96](https://github.com/davidvanlaatum/inventree-mcp/issues/96)
- Depends on: F-S16
- Progress: implementation completed on `codex/f-s26-mcp-gap-guidance`; issue #96 remains open until the implementing pull request is verified merged.
- Scope: publish advisory MCP server instructions that guide consuming agents when an operator-requested workflow cannot be completed because `inventree-mcp` lacks a required tool or capability. Do not add a GitHub tool, grant issue-creation authority, or change existing InvenTree tool behavior.
- Acceptance:
  - Current `server/discover` and legacy `initialize` results publish the same model-facing instructions in STDIO and HTTP modes.
  - The instructions distinguish a missing `inventree-mcp` capability from invalid or ambiguous input, insufficient OAuth scopes or InvenTree permissions, server/configuration failures, and upstream InvenTree limitations.
  - The instructions ask agents to explain the gap and safe existing workarounds without silently substituting a materially different operation.
  - Agents with GitHub search access are asked to search open and closed project issues, report existing coverage without duplication, and ask the operator before creating an untracked issue.
  - Agents without GitHub search access are asked to disclose that they cannot verify existing coverage and ask whether the operator wants the gap checked and, if untracked, an issue created.
  - Documentation states that server instructions are advisory and client-controlled, so agent compliance is not guaranteed.
  - Deterministic SDK- and JSON-RPC-boundary tests verify the exact instructions across current discovery and legacy initialization.

Tasks:

- [x] Define the functionality-gap classification and operator-approval wording.
- [x] Publish the guidance through MCP server options.
- [x] Add current-discovery, legacy-initialization, STDIO, and HTTP assertions.
- [x] Align the plan, tool reference, operator recipe, and task contract.
- [x] Run Go, QA, and product review and resolve or document findings.

- Validation: `go test -race ./internal/server`, `GOFLAGS=-trimpath go test -race -p=1 ./...`, `go vet ./...`, `golangci-lint run ./...`, `go mod tidy -diff`, and `git diff --check` pass. `go test -tags no_integration_tests -cover ./internal/server` reports 76.2% statement coverage, unchanged from the exact `origin/main` base.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager reviews completed because F-S26 changes MCP initialization behavior and public operator contracts. Initial Go and QA findings identified missing legacy `initialize` coverage over the STDIO-equivalent transport; the added deterministic fallback test proves the exact instructions in both the typed SDK result and parsed JSON-RPC response. Product review found the operator guidance was incorrectly nested under STDIO setup; it now lives in a transport-neutral section that names current discovery, legacy initialization, STDIO, and HTTP. Focused reruns found no remaining actionable findings.
- Residual risk: MCP clients control whether and how server instructions reach their agents and may ignore or reinterpret this advisory guidance. GitHub searches can be unavailable, incomplete, or stale, and semantic duplicate matching remains agent-dependent. Future MCP SDK behavior may change, so upgrades must retain the current discovery and legacy initialization assertions.

### F-S27: Guarded Full Stock-Item Transfer

- Status: `Done`
- Issue: [#95](https://github.com/davidvanlaatum/inventree-mcp/issues/95)
- Depends on: F-S21, F-S22, product review, QA review, and infosec review
- Progress: implementation completed on `codex/f-s27-guarded-stock-transfer`, originally based on `main` at `0a8efb4d`. The approved first slice accepts one stable stock-item ID and one explicit stable destination-location ID, moves the complete current quantity only, and never intentionally splits or batches stock. Every destination that passes exact-ID read and InvenTree's native transfer validation is eligible; the MCP adds no structural, external, or ownership exclusion. Partial/split behavior is deferred to F-S28 and multi-item batching to F-S29.
- Scope: add one current-state-bound operational tool that transfers the complete current quantity of one ordinary safe stock item through `/api/stock/transfer/`, records a nonblank audit reason, preserves reviewed provenance, and verifies the original stable stock-item ID at the explicit destination. Reject no-op, depleted, unavailable, allocated, serialized, build-related, consumed, installed, parent/child, or otherwise protected source state. Return structured read-before-retry recovery when the result cannot be proved. Do not infer the part default location, accept a caller quantity, create a split, or batch multiple source items.
- Acceptance:
  - Exact positive `stock_item_id` and `destination_location_id` values plus a nonblank audit reason are required; the destination is never inferred.
  - Dry run reports the complete current quantity, source and destination location IDs and paths, affected part, reviewed safety and provenance state, and `will_split:false` without writing.
  - Execution requires `confirm:true` and the exact principal-bound, five-minute, single-use token from the matching current-state dry run; stale, reused, mismatched, or restart-invalidated tokens do not write.
  - Source validation requires a current source location and rejects non-positive or schema-invalid quantity, unavailable stock, unknown or nonzero allocation, serialization, build/consumption, installation, parent/child, and other protected relationship state with structured clarification.
  - Every exact-read destination remains eligible regardless of structural, external, or ownership metadata unless InvenTree rejects it; same-location transfers are refused as no-ops and safe upstream validation errors are returned without mutation claims.
  - Execution posts exactly one `{pk, quantity}` entry containing the complete reviewed quantity to the native transfer endpoint with the audit reason, then verifies the original exact stable ID at the destination with unchanged reviewed quantity and provenance.
  - An ambiguous upstream result is recovered only when exact-ID read-back proves the complete reviewed transfer; other verification outcomes return `partial_failure` with the current safe record when available and explicit no-blind-retry guidance.
  - OAuth authorization requires `inventree.read`, `inventree.write`, and `inventree.operational`; the tool is operational, closed-world, non-destructive, and non-idempotent.
  - API capability notes, endpoint manifest, tool reference, operator recipe, stocktake prompt, generated tool manifest, and task/issue evidence stay aligned.
  - Unit, MCP-boundary, authorization, deterministic response-loss, and default-on pinned InvenTree 1.4.3 Testcontainers coverage exercise full transfer, provenance and tracking retention, all-valid-destination behavior, invalid/no-op/unsafe state refusal, stale/reused confirmation, and recovery paths.

Tasks:

- [x] Add typed native stock-transfer client behavior and endpoint-manifest coverage.
- [x] Add the guarded full-transfer tool, current-state plan, scopes, annotations, and recovery behavior.
- [x] Add deterministic, MCP-boundary, authorization, and pinned-live coverage.
- [x] Align schema notes, tool reference, operator recipe, stocktake prompt, generated manifest, and issue evidence.
- [x] Run the full Go, QA, product, and infosec review panel and resolve or document findings.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go vet ./...`, `golangci-lint run ./...`, `GOFLAGS=-trimpath go test -race -p=1 ./...`, and `git diff --check` pass. Focused default-on InvenTree 1.4.3 runs prove the typed client transfer and guarded tool success/recovery paths against the real API, including an external destination, stable item identity, audit tracking, and nonempty supplier-part, purchase-order, price/currency, and batch provenance retention. `go test -p=1 -tags no_integration_tests -cover ./...` passes with `internal/inventree` at 91.8% and `internal/tools` at 82.4%; the exact pre-F-S27 base reports 91.8% and 82.1%, respectively, so no package-level reduction was introduced.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed because F-S27 adds a mutating operational tool, public contract, and Testcontainers coverage. Initial findings required a non-null source schema, output-type reuse, exact-read failure and identity tests, explicit one-attempt recovery assertions, real nonempty pinned purchase provenance, and consistent destination/source clarification plus exact-ID recovery guidance. The implementation, tests, and docs address every finding; focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: InvenTree exposes no conditional stock-transfer revision, and the native write and exact-ID verification are separate requests. Another writer can race the final interval; a divergent read-back returns `partial_failure`, but an indistinguishable concurrent write could still escape detection. Native destination rules can also reject an exact-read location after preflight, and that definite rejection is returned without a mutation claim.

### F-S28: Partial Stock-Item Transfer And Split Recovery

- Status: `Done`
- Issue: [#97](https://github.com/davidvanlaatum/inventree-mcp/issues/97)
- Depends on: F-S27, pinned InvenTree split-behavior verification, product review, QA review, and infosec review
- Decisions: approved by the operator on 2026-08-21: allow one partial transfer only when `0 < quantity < current quantity`; retain F-S27's in-stock, unallocated, non-serialized, relationship-free source guards; require exact source remainder and distinct destination identity read-back; fail closed with actionable read-before-retry recovery when response loss leaves split identity unresolved; and verify copied batch/status/packaging/supplier/purchase-order/price/currency provenance before reporting success. A disposable pinned InvenTree 1.5.1/API 530 probe established that transferring quantity `2` from a quantity-`6` stock item leaves the original stable stock-item ID at quantity `4` and creates a distinct destination stock-item ID at quantity `2`; the batch value is copied and a native tracking event records the audit note.
- Progress: implemented and validated on `codex/f-s28-partial-stock-transfer` from `origin/main` at `101778e3c0edd59b618560b998f5d5aed1b4e35a`.
- Scope: add a guarded single-source partial-quantity transfer only after pinned InvenTree behavior establishes the source remainder, destination record identity, copied provenance, tracking events, and a response-loss recovery contract that cannot duplicate a split. Preserve F-S27's complete-transfer input and behavior unchanged. Do not add multi-item batching or implicit default-location resolution.
- Acceptance:
  - Product review explicitly approves partial quantity validation, split-result identity, safe source states, and recovery behavior before implementation becomes Active.
  - Pinned-live evidence identifies the source and destination stable records, copied provenance, and tracking behavior for a split.
  - Dry run exposes source remainder, destination quantity, paths, provenance, split behavior, and every recovery assumption.
  - Ambiguous execution never creates a second split or advises a blind retry; unresolved identity returns actionable read-before-retry recovery.
  - OAuth, annotations, public docs, generated manifests, and deterministic plus pinned-live coverage stay aligned without changing F-S27.

Tasks:

- [x] Verify and document pinned InvenTree partial-transfer and split identity behavior.
- [x] Resolve the remaining QA and infosec contract checks.
- [x] Implement only the approved partial-transfer and recovery surface.
- [x] Validate, review, and align public contracts without widening into F-S29.

- Validation: focused transfer unit tests, docs tests, `go generate ./internal/tools`, `go vet ./...`, `go build ./...`, and `git diff --check` passed. The default-on pinned Testcontainers characterization passed against `inventree/inventree:1.5.1` / API `530`: `go test ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/stock_adjustments/partial_transfer_split_characterization$' -v -count=1`. The broader package coverage run remains blocked by the pre-existing macOS `httptest` IPv6 listener restriction in unrelated URL-upload tests.
- Review: full Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer panel completed. Findings addressed: corrected complete/partial schema and deferred-scope wording; added live unique destination, status, and packaging assertions; excluded generated destination `creation_date` from copied-field comparison; and bound recovery to pre-mutation matching destination IDs. Follow-up panel found no actionable Go, QA, product, or infosec findings.
- Residual risk: recovery is bound to the pre-mutation matching-ID baseline and fails closed on incomplete or ambiguous results, but a concurrent indistinguishable split created by another writer remains an unsupported race and requires manual reconciliation.

### F-S29: Reviewed Multi-Item Stock-Transfer Batches

- Status: `Planned`
- Issue: [#98](https://github.com/davidvanlaatum/inventree-mcp/issues/98)
- Depends on: F-S27, pinned InvenTree batch atomicity/failure verification, product review, QA review, and infosec review; also F-S28 if partial quantities are approved for batches
- Scope: add bounded reviewed multi-item transfers only after native atomicity, validation, response-loss, and partial-progress behavior are characterized. Preserve F-S27 as the simple single-item workflow and do not silently include F-S28 partial quantities. Do not add transfer-order workflows or implicit default-location resolution.
- Acceptance:
  - Product review explicitly selects complete-only versus partial-capable batches, duplicate-source handling, maximum size, ordering, and any source/provenance restrictions.
  - Pinned-live tests establish native atomicity and mid-batch failure behavior before implementation becomes Active.
  - One current-state-bound plan lists every stable item, quantity, source/destination path, provenance, split behavior, and deterministic action order.
  - Results distinguish verified, recovered, failed, and unknown per-item outcomes without blind retry guidance and within bounded request/response sizes.
  - OAuth, annotations, public docs, generated manifests, and deterministic plus pinned-live coverage stay aligned without changing F-S27 or F-S28 contracts.

Tasks:

- [ ] Verify and document pinned InvenTree batch atomicity and failure behavior.
- [ ] Resolve complete-only/partial, duplicate, size, ordering, and recovery decisions.
- [ ] Implement only the approved bounded batch surface.
- [ ] Validate, review, and align public contracts.

- Validation: pending implementation selection.
- Review: pending implementation selection.
- Residual risk: native batch atomicity and response-loss semantics are intentionally unresolved until pinned-live evidence exists.

### F-S30: Clarify Endpoint-Specific Model-Type Contracts

- Status: `Done`
- Issue: [#101](https://github.com/davidvanlaatum/inventree-mcp/issues/101)
- Depends on: M1D-S02, M1F-S02, F-S11, product review, and QA review
- Progress: implementation started on `codex/f-s30-model-type-contracts` from `main` at `03e18cb5`. The approved product decision preserves the existing `model_type` field and both upstream endpoint-specific vocabularies without aliases, normalization, or runtime behavior changes.
- Scope: make the attachment endpoint's short model types and the parameter endpoint's qualified `app.model` types explicit in generated MCP input-schema descriptions, tool reference documentation, schema capability notes, and operator recipes. Add contract tests that prevent the descriptions or documentation from conflating or omitting either vocabulary.
- Acceptance:
  - The `list_attachments` and attachment creation/upload input-schema descriptions list the six supported short, unqualified values: `part`, `stockitem`, `company`, `manufacturerpart`, `supplierpart`, and `purchaseorder`.
  - Parameter-template create and update input-schema descriptions list the twelve supported qualified values plus the explicit empty unrestricted value.
  - Schema descriptions warn against supplying the other endpoint's vocabulary, preserve handler-level validation and all currently accepted runtime values, and do not add JSON Schema enum constraints.
  - Shared documentation no longer claims one `model_type` vocabulary applies to both attachment and parameter tools.
  - API capability notes and operator recipes explain that the distinction comes from separate upstream InvenTree endpoint enums and include concrete examples.
  - Focused contract tests inspect the registered MCP schemas and documentation for both complete, disjoint contracts.

Tasks:

- [x] Clarify registered attachment and parameter-template input-schema descriptions.
- [x] Split the shared tool-reference field guidance and align schema, plan, and operator docs.
- [x] Add focused MCP schema and documentation contract tests.
- [x] Run validation, coverage comparison, and the applicable Go, QA, product, and infosec review panel.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go vet ./...`, `golangci-lint run ./...`, focused race-enabled `docs` and `internal/tools` model-type contract tests, `GOFLAGS=-trimpath go test -race -p=1 ./...`, no-integration per-package coverage, and `git diff --check` pass. Compared with exact base `main` at `03e18cb5`, `internal/tools` remains 82.4% and `docs` rises from 16.7% to 57.1%; no package-level reduction was introduced.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed because the change affects registered MCP input-schema descriptions and public operator contracts. Initial Go and QA findings identified substring assertions that overlapping model names could satisfy incorrectly; fixed by requiring each complete ordered vocabulary clause and proving every value in either runtime allowlist is rejected by the other. Product findings requested that issue/schema wording distinguish descriptive lists from JSON Schema enum constraints and precisely name only the attachment tools that accept `model_type`; issue #101, task acceptance, and tool-reference wording were aligned. Infosec found no authorization, validation-boundary, or exposure issues. Focused Go, QA, and product reruns found no remaining actionable findings.
- Residual risk: input-schema descriptions are advisory and handler allowlists remain authoritative. A future upstream InvenTree enum change requires the schema snapshot, runtime allowlists, descriptions, contract tests, and operator docs to be updated together; this task intentionally adds no aliases or automatic normalization.

### F-S31: Guarded Company Primary Images

- Status: `Done`
- Issue: [#105](https://github.com/davidvanlaatum/inventree-mcp/issues/105)
- Depends on: M1F-S01, F-S20, product review, QA review, and infosec review
- Progress: completed on `codex/f-s31-company-primary-images` from `main` at `d751d107`. The delivered contract accepts PNG, JPEG, and WebP raster images up to 5 MiB, at most 4096 pixels per dimension and 16 megapixels total; supports existing companies in all eight supplier, manufacturer, and customer role combinations without expanding role or sales administration; keeps inline/STDIO-local acquisition separate from the dedicated HTTP(S)-URL fetch tool; and provides pinned-live-proven confirmed clear behavior.
- Scope: add guarded company primary-image assignment and replacement for one stable company ID, with exact content validation, same-instance read-back, and response-loss recovery. Add a separate confirmed clear operation only when pinned InvenTree 1.4.3 validation proves the request, read-back, and recovery contract. Keep generic company attachments distinct from the primary company image.
- Acceptance:
  - `set_company_image` accepts exactly one inline base64 or STDIO allowlisted local-file source; `set_company_image_from_url` is the only company-image tool that fetches HTTP(S) content and preserves the existing SSRF, redirect, bounded-read, timeout, and credential-isolation policy.
  - Both assignment tools require one positive stable company ID, allow a company with any existing role combination, preserve every company field and role except `image`, and require `confirm:true` before replacing a non-null current image.
  - Accepted content is a nonempty PNG, JPEG, or WebP raster whose decoded format agrees with its normalized extension and media type, whose encoded size is at most 5 MiB, whose width and height are each at most 4096 pixels, and whose total pixel count is at most 16 megapixels. Invalid, mismatched, oversized, zero-dimension, or unsupported content is rejected before the upstream write.
  - Successful assignment reads the exact company ID, fetches only its resulting schema-exposed same-instance image URL, and verifies the downloaded bytes' SHA-256 digest against the submitted content. Output includes the company ID, sanitized image URL, filename, media type, size, dimensions, digest, replacement state, and verification state without exposing image bytes or sensitive URLs.
  - Definite upstream validation errors return bounded sanitized field details. Ambiguous write results perform one exact-ID read and bounded same-instance content verification; an exact digest match returns recovered success, while missing, divergent, or unverifiable content returns `partial_failure` with current safe image metadata and explicit no-blind-retry guidance.
  - A separate confirmed clear tool is exposed only if pinned-live testing proves the nullable request representation, exact null read-back, response-loss recovery, and storage behavior relevant to the operator contract. If any of those cannot be proved safely, removal remains unsupported and the task, issue, and docs record why.
  - Assignment requires `inventree.read`, `inventree.write`, and `inventree.upload`; clear additionally requires `inventree.destructive`. Annotations, endpoint manifest, generated tool manifest, schema notes, tool reference, operator recipe, and prompt guidance remain aligned.
  - Deterministic unit and MCP-boundary tests cover source selection, local/HTTP transport boundaries, image validation, confirmation, stable identity, redaction, exact digest read-back, and every recovery branch. Default-on pinned InvenTree 1.4.3 tests cover initial upload, replacement with distinct bytes, all company role combinations, invalid content, and supported clear/recovery behavior.

Tasks:

- [x] Characterize pinned InvenTree company-image upload, replacement, clear, media URL, and storage behavior.
- [x] Add typed client methods and same-instance bounded image download behavior.
- [x] Add guarded inline/local and URL company-image tools, plus clear only if live proof supports it.
- [x] Add deterministic, MCP-boundary, authorization, redaction, recovery, and pinned-live coverage.
- [x] Align schema notes, endpoint manifest, generated manifest, tool reference, operator recipe, prompts, and issue evidence.
- [x] Compare per-package coverage, run full validation, and resolve the Go, QA, product, and infosec review panel.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go vet ./...`, `golangci-lint run ./...`, focused no-integration package tests, focused pinned InvenTree 1.4.3 client and tool tests, `GOFLAGS=-trimpath go test -race -p=1 ./...`, and `git diff --check` pass. Pinned tests prove exact-byte PNG, JPEG, and WebP assignment, distinct replacement media, all eight company-role combinations, validation refusal, response-loss recovery, exact-null clear, and old-media removal. Compared with exact base `d751d107`, no package-level coverage percentage decreased: `internal/inventree` rises from 91.8% to 92.3%, `internal/tools` from 82.4% to 82.6%, and `internal/upload` from 76.7% to 78.2%; every other tested package is unchanged.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed because F-S31 adds upload, mutation, destructive, recovery, and public operator contracts. Initial findings covered full-raster validation, complete field preservation, exact-ID checks, stale replacement preflight, truthful recovery status, all role and image-format combinations, safe missing-content-type retry, clear result shape, corrupt decoder coverage, and concurrency guidance. The implementation, tests, and docs address each finding. Focused Go, QA, and product reruns found no remaining actionable findings; infosec found no high- or medium-severity issue and identified only the documented upstream concurrency boundary.
- Residual risk: InvenTree exposes no conditional company-image mutation, so the final fresh exact-company GET and PATCH are separate requests. A concurrent UI, API, or second MCP writer can assign or clear a newer image in that interval. Operators must coordinate a single writer; exact post-write digest or null read-back detects the resulting state after this call, and any ambiguous or divergent result requires inspecting the stable company ID before preparing a fresh request rather than blindly retrying.

### F-S32: Guarded Purchase-Order Line Deletion

- Status: `Done`
- Issue: [#103](https://github.com/davidvanlaatum/inventree-mcp/issues/103)
- Depends on: F-S03, F-S23
- Progress: implementation started on `claude/po-line-item-deletion-bc2a78` from `d751d10`. Pinned InvenTree 1.4.3 empirical probing established that `DELETE /api/order/po-line/{id}/` neither restricts deletion by purchase-order status (PENDING or PLACED) nor rejects a line that already has received quantity; it silently orphans any already-receipted `StockItem`'s line reference while leaving the stock item and its quantity untouched. The new `delete_purchase_order_line` tool therefore enforces a zero-received and no-linked-stock-provenance guard itself before ever calling the upstream endpoint, rather than relying on upstream to reject unsafe deletions. Adding this task's new pinned-live subtest also surfaced and fixed a pre-existing shared-testenv fixture bug: `internal/testenv`'s `getOrCreateRecord` sent a `name` query filter to InvenTree's `/api/part/` list endpoint, but that endpoint has no such filter (only `name_regex`), so the lookup silently returned an unfiltered, `limit:10`-capped result window; once enough total parts existed in the shared container during one test run, a second lookup for an already-created part fixture could miss and attempt a duplicate create, which InvenTree then rejected with `400 Bad Request`. Fixed by using an anchored `name_regex` for that one endpoint; confirmed via a clean `origin/main` full-suite baseline run (passed) versus this branch before the fix (reproducibly failed `TestMilestoneHappyPathToolsAgainstInvenTree/attachment_target_matrix_upload_download_and_max_bytes/supplierpart` under `-race -p=1 ./...`, 3/3 attempts) and after the fix (passed, 1/1).
- Scope: add one guarded destructive tool that deletes exactly one ordinary (receivable, supplier-part) purchase-order line by stable primary key, distinct from the existing `delete_purchase_order_extra_line` tool which only removes non-receivable extra lines. No search-and-delete, no bulk deletion, and no implicit stock adjustment.
- Acceptance:
  - Requires an exact positive purchase-order line primary key; never searches and deletes.
  - Previews the line ID, purchase order (including order status), resolved supplier part and base part, ordered quantity, received quantity, destination, price, currency, reference, and notes before requiring `confirm:true`.
  - Refuses deletion when the line has any received quantity, or when a stock item already references the same purchase order and supplier part, even though InvenTree itself would allow both; never deletes or adjusts received stock.
  - Permits deletion on both PENDING and PLACED orders once the received/linked-stock guard passes, matching pinned InvenTree's own lack of status-based restriction.
  - Returns the refreshed purchase order (including server-calculated `total_price`) after a confirmed deletion, and verifies exact-ID absence via read-back before reporting success.
  - An ambiguous read-back after a confirmed delete call returns `partial_failure` with a read-before-retry recovery plan instead of a false success or silent data loss.
  - Requires `inventree.destructive` scope and publishes `destructiveHint:true`, `idempotentHint:false` MCP annotations.
  - Unit, MCP-boundary, and default-on pinned InvenTree 1.4.3 Testcontainers coverage exercise invalid ID, not-found, received-quantity refusal, linked-stock refusal, preview-then-confirm deletion, PLACED-order deletion, and refreshed-total behavior.
  - Tool reference, operator recipes, and generated tool manifest document the new tool and its distinction from extra-line deletion.

Tasks:

- [x] Add the typed `DeletePurchaseOrderLine` client method.
- [x] Add the guarded `delete_purchase_order_line` tool, annotations, and scopes.
- [x] Empirically pin InvenTree 1.4.3's unrestricted-by-status, unrestricted-by-received-quantity DELETE behavior with client-method integration coverage.
- [x] Add unit and pinned-live MCP-tool coverage for preview, confirmed deletion, received-quantity refusal, linked-stock refusal, and PLACED-order deletion.
- [x] Update generated and public documentation contracts.
- [x] Diagnose and fix the pre-existing `/api/part/` fixture-lookup bug surfaced by the added pinned-live coverage.
- [x] Complete the previously-deferred Senior Go Developer / Senior QA / Test Architect review pass and address findings (2026-08-09 follow-up).

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go vet ./...`, `golangci-lint run ./...`, and `INVENTREE_TEST_SKIP_DOCKER=1 go test -tags no_integration_tests ./...` pass. Focused pinned InvenTree 1.4.3 runs for `TestClientMethodsAgainstInvenTree/po` and `TestMilestoneHappyPathToolsAgainstInvenTree/purchase_order_line_delete_happy_path` pass, proving upstream's permissive DELETE behavior and the tool's own received/linked-stock refusal, PLACED-order deletion, and refreshed-total behavior. `GOFLAGS=-trimpath go test -race -p=1 ./...` (all default-on Docker suites, including full `internal/testenv` and `internal/tools`) passed cleanly after the `/api/part/` fixture-lookup fix.
  2026-08-09 follow-up-review validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./internal/tools/... ./internal/inventree/...` (0 issues), `go mod tidy -diff` (no diff), `go generate ./internal/tools/...` (no diff), and `INVENTREE_TEST_SKIP_DOCKER=1 go test -tags no_integration_tests ./...` all pass, including 13/13 focused `go test -run 'TestDeletePurchaseOrderLine' ./internal/tools/...` unit tests (5 new: order-not-found, supplier-part-not-found, linked-stock search error redaction, linked-stock server-side supplier-part filtering, post-delete refreshed-order-lookup failure). Reran pinned InvenTree 1.4.3 `TestClientMethodsAgainstInvenTree/po` and `TestMilestoneHappyPathToolsAgainstInvenTree/purchase_order_line_delete_happy_path` live against Docker; both pass, the latter now also proving the linked-stock refusal path (`Clarification.Field == "stock"`) against real InvenTree data instead of only a synthetic unit fake.
- Review: completed 2026-08-09 via a Senior Go Developer and Senior QA / Test Architect subagent panel per `docs/reviewers.md`, replacing the "none yet" status this story shipped with. Findings and dispositions:
  - **(Go, fixed)** The order and supplier-part lookups in `deletePurchaseOrderLine` let a not-found upstream error escape the handler as a raw, unstructured tool-call error (`isError:true`) instead of the codebase's established `clarification_required` pattern (the same `isNotFound(err)`-after-load convention used by the sibling `delete_purchase_order_extra_line` tool's `loadExtraLineOrder` callers). Fixed: both now return `clarification_required` (`field: "order"` or `"supplier_part"`, `hard_error:true`) when the line's referenced purchase order or supplier part cannot be found, with new unit tests `TestDeletePurchaseOrderLineOrderNotFound` and `TestDeletePurchaseOrderLineSupplierPartNotFound`.
  - **(Go, fixed)** `purchaseOrderLineHasLinkedStock` fetched every stock item for the order and filtered by supplier part in Go, because `StockItemQuery` had no supplier-part filter. Fixed: added `StockItemQuery.SupplierPartID` (`supplier_part` query param, confirmed present in `docs/api-schema.yaml`'s `StockItem` schema) and narrowed the preflight to filter server-side; added `TestDeletePurchaseOrderLineIgnoresStockWithDifferentSupplierPart` to confirm the narrower query still correctly ignores unrelated stock.
  - **(Go, out of scope, flagged for follow-up)** The sibling `delete_purchase_order_extra_line` tool (`internal/tools/purchase_order_extra_line_tools.go`) has the identical raw-error-propagation gap for its own order lookup. Not fixed here — out of scope for F-S32 — but a follow-up task has been suggested.
  - **(Go, out of scope, informational)** `PurchaseOrderLineItem.SupplierPart` (`internal/inventree/models.go`) is a dead Go field with no corresponding `supplier_part` property in the real `po-line` API schema (only `part`, titled "Supplier Part"); it predates F-S32 and this tool correctly uses `record.Part` instead. Not touched; worth a separate cleanup.
  - **(QA, HIGH, fixed)** This story's own Validation note claimed pinned live coverage proved "linked-stock refusal," but only the received-quantity refusal path was ever exercised against real InvenTree — the linked-stock branch was proven only by a synthetic unit fake, contradicting the acceptance criteria's own integration-coverage requirement. Fixed: `purchase_order_line_delete_happy_path` in `internal/tools/milestone_integration_test.go` now creates a second unreceived line sharing a receipted line's supplier part on the same order and asserts the tool refuses deletion with `Clarification.Field == "stock"`, rerun live and passing.
  - **(QA, fixed)** The post-delete refreshed-purchase-order-lookup failure branch (partial-failure recovery after a successful delete whose order refresh then fails) had no test. Fixed: added `TestDeletePurchaseOrderLineRefreshFailureAfterDeleteIsPartial` and a corresponding fake-client hook.
  - **(QA, fixed)** The `stockSearchErr` fixture field existed but was never exercised by any test, leaving the `SearchStockItems` error path in `purchaseOrderLineHasLinkedStock` untested. Fixed: added `TestDeletePurchaseOrderLineLinkedStockSearchErrorIsSafe`, which also confirms upstream error detail is redacted.
  - **(QA, accepted, not blocking)** The defensive identity-mismatch branches (`record.PK != input.ID`, `order.PK != id`, `supplierPart.PK != record.Part`) remain untested. This matches the same untested pattern elsewhere in the codebase (e.g. other admin/delete tools' identity checks), so it is accepted as consistent existing practice rather than a regression specific to this story.
- Residual risk: the received-quantity and linked-stock checks and the deletion call are separate InvenTree requests, so a concurrent UI or API writer could receive against the line between the guard check and the delete call; operators should coordinate a single writer. The linked-stock check matches on `(purchase_order, supplier_part)` rather than a per-line stock relationship because InvenTree's `StockItem` model has no direct foreign key to a specific purchase-order line, so it can conservatively refuse deletion of an empty line that shares a supplier part with a different, already-received line on the same order; this is treated as an acceptable false-positive in favor of safety. The Go reviewer additionally noted the `received` value driving the received-quantity guard is read once, at the very top of the handler, before the intervening purchase-order and supplier-part detail lookups, marginally widening this same TOCTOU window; not tightened in this pass, since re-reading it immediately before the guard check would require deferring the order/part context that the refusal clarification's preview relies on to give an informative message — a worse trade-off than the marginal race-window reduction. The sibling `delete_purchase_order_extra_line` tool has the same not-found raw-error-propagation gap this pass fixed here; left unfixed as out of scope and tracked as a follow-up. `PurchaseOrderLineItem.SupplierPart` is a pre-existing dead Go field unrelated to this tool; left unfixed as out of scope.

### F-S33: Guarded Part Deletion

- Status: `Done`
- Issue: [#104](https://github.com/davidvanlaatum/inventree-mcp/issues/104)
- Depends on: F-S03, F-S32 (F-S32's own deferred review completed 2026-08-09 via PR #110, after F-S33 flagged the gap)
- Progress: implementation complete on `claude/issue-104-7af2c1` from `22e11f0` (PR [#108](https://github.com/davidvanlaatum/inventree-mcp/pull/108)). Adds the first `BomItem`, `SalesOrderLineItem`, `Build`, and `PartRelation` Go client plumbing anywhere in this codebase, all read-only and used solely by `delete_part`'s safety guard. F-S02 (BOM Import Workflow) and F-S04 (Build Order Workflows) remain `Blocked`; the operator confirmed on 2026-08-09 that this narrow, read-only existence-check use does not count as implementing either held story, and that the read-only `SalesOrderLineItem` existence check does not count as a sales/customer workflow. The issue's own suggested safety contract treated supplier parts, manufacturer parts, parameters, attachments, and related-part links as informational-only, non-blocking context that InvenTree cascades away on its own. Pinned InvenTree 1.4.3 Testcontainers coverage disproved that: InvenTree enforces only two conditions of its own -- a part must be inactive before it can be deleted at all ("Cannot delete this part as it is still active"), and a part currently used as a component in another part's BOM is protected ("Cannot delete this part as it is used in an assembly"). Every other relationship this tool checks (stock, the part's own BOM, builds, purchase-order lines, sales-order lines, variants, supplier parts, manufacturer parts, parameters, attachments, related-part links) is silently permitted once the part is inactive, and the consequences differ by relation: deleting a part with existing stock also destroys that stock item outright, while a referencing purchase-order line is left behind, orphaned. The tool was corrected to treat every one of these categories as blocking (including a new `active`-state check) rather than relying on any InvenTree-side protection or cascade. Two automated review rounds ran against this corrected design; two further PR review passes were then posted through the repository owner's GitHub account on PR #108 (authorship of the review content itself is not asserted here -- see Review) and drove the fixes below, including a genuine ambiguous-mutation-recovery gap, a domain-misnamed shared helper, stale documentation, and a self-decided product choice that should have been asked of the operator directly instead. That confirmation-contract question was then asked directly in chat (not inferred from the PR thread) and the operator chose to keep the existing simple `confirm:true` convention (see Residual risk); the operator also chose to split the F-S32 dependency's own review gap into a separate follow-up task rather than block this story on it, and that follow-up was completed and merged as PR #110 before this story's own PR was finalized.
- Scope: add one guarded destructive tool that deletes exactly one ordinary part by stable primary key, refusing while the part is active or any real usage reference exists, and reporting every blocking dependent record's stable ID without ever cascading a removal itself.
- Acceptance:
  - Requires an exact positive part primary key; never searches and deletes.
  - Previews the part, its category, supplier parts, manufacturer parts, parameters, attachments, and related-part links before requiring `confirm:true`.
  - Refuses deletion while the part is still active, or while stock, bill-of-materials (as assembly or as a component elsewhere), builds, purchase-order lines, sales-order lines, variant parts, supplier parts, manufacturer parts, parameters, attachments, or related-part links reference the part, reporting each blocking category's stable IDs instead of cascading.
  - Deletes only after `confirm:true`, with the full blocking-reference preflight re-run fresh at that call (not bound to a hash or token from an earlier preview), then verifies exact-ID absence via read-back before reporting success.
  - An ambiguous read-back after a confirmed delete call returns `partial_failure` with a read-before-retry recovery plan instead of a false success or silent data loss; an ambiguous *mutation* error (a lost/dropped response, distinct from a definite upstream rejection) is itself verified by read-back and reported as `recovered:true` on success rather than a false failure.
  - Requires `inventree.destructive` scope and publishes `destructiveHint:true`, `idempotentHint:false` MCP annotations.
  - Unit coverage exercises invalid ID, not-found, each individual blocking category (including active-state), preview-then-confirm deletion, validation failure, ambiguous read-back, and ambiguous mutation-error recovery (applied-anyway, genuinely-failed, and the retryable-status boundary); default-on pinned InvenTree 1.4.3 Testcontainers coverage exercises every blocking category's real accept/reject direction and child-record fate.
  - Tool reference, endpoint manifest, and generated tool manifest document the new tool.

Tasks:

- [x] Add read-only `BomItem`, `SalesOrderLineItem`, `Build`, `PartRelation` models, query types, and `Search*` client methods.
- [x] Add the typed `DeletePart` client method.
- [x] Add the guarded `delete_part` tool, annotations, and scopes.
- [x] Empirically pin InvenTree 1.4.3's actual (far more permissive than assumed) referential-integrity behavior with granular, per-category client-method integration coverage, including the active-state precondition and the stock-destruction vs. purchase-order-line-orphaning divergence.
- [x] Add unit coverage for every refusal path (including active-state) and the confirm/delete/verify happy path.
- [x] Update endpoint manifest, tool reference docs, and generated tool manifest.
- [x] Classify `DeletePart` mutation errors as definite-rejection vs. ambiguous, verify ambiguous failures by read-back instead of reporting them as clean failures, and preserve `context.Canceled`/`context.DeadlineExceeded` through every preflight and mutation error path instead of masking them behind a generic message.
- [x] Extract the definite-vs-ambiguous status-code classifier out of `stock_adjustment_tools.go` into a neutrally-named, package-shared `definiteMutationRejection` (`validation_errors.go`) instead of importing a stock-named helper into part-deletion logic; updated all four call sites (`stock_adjustment_tools.go`, `stock_depletion_tools.go`, `stock_transfer_tools.go`, `part_delete_tools.go`).
- [x] Add focused tests for `context.Canceled`/`context.DeadlineExceeded` preservation (one on the mutation call, one representative preflight call) and for a genuinely ambiguous read-back failure (the verification `GetPart` itself erroring, not just finding the part still present).
- [x] Ask the operator directly (not infer) whether to add a plan-hash/token confirmation mechanism or keep the existing simple `confirm:true` convention; record the answer.
- [x] Update `docs/tool-reference.md` and `docs/operator-recipes.md` to document the `recovered` output field and all four post-delete-attempt outcome states.

- Validation: `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run ./...` (0 issues), `go mod tidy -diff` (clean). `go test ./...` passes, including the default-on Docker-backed InvenTree 1.4.3 integration suite (`internal/inventree`, `internal/testenv`, `internal/tools`, `internal/schema`). `TestClientMethodsAgainstInvenTree/part_delete` pins, per category in isolation on an inactive part, that InvenTree 1.4.3 permits deleting a part with existing stock (destroying the stock item), its own BOM, a build, a purchase-order line (orphaned, not destroyed), a sales-order line, a variant, a supplier part, a manufacturer part, a parameter, or an attachment, and rejects only an active part or one used as a component in another part's BOM. Unit tests cover the corrected mutation-error classification (`TestDeletePartRecoversWhenAmbiguousMutationErrorAppliedAnyway`, `TestDeletePartPartialFailureWhenAmbiguousMutationErrorDidNotApply`, `TestDeletePartReturnsSafeErrorOnDefiniteMutationRejection`, `TestDeletePartTreatsRetryableStatusAsAmbiguous`, including the 408/425/429 retryable-status boundary), context-sentinel preservation on both the mutation call and a representative preflight call (`TestDeletePartPreservesContextCanceledOnMutation`, `TestDeletePartPreservesContextDeadlineExceededOnPreflight`), and a genuinely ambiguous read-back failure distinct from a clean not-found or clean success (`TestDeletePartPartialFailureWhenReadBackItselfErrors`). `go generate ./internal/tools` produced the checked manifest with no diff.
- Review: Round 1 (Senior Go Developer, Senior QA/Test Architect, Senior Product Manager, Senior Infosec Reviewer subagent panel, run against the initial "informational-only" design) found an inefficient N+1 purchase-order-line query fan-out (fixed with a dedicated `base_part` filter query), missing integration coverage for the confirmed-delete success path, a missing `variant_of` blocking check, an uncommitted `docs/TASKS.md` audit-trail gap, and a missing operator-recipes entry. Fixing the "confirmed-delete success path" gap led directly to the informational-vs-blocking and active-state discoveries described in Progress, which required redesigning the guard. Round 2 (focused subagent re-review of the corrected design) found the guard logic, `PartDeleteBlockingReferences` wiring, and unit-test coverage were correct, but caught two integration-test/unit-test comment blocks and this story's own Scope/Acceptance text that still described the superseded "informational, not blocking" design (fixed), a stale `PartRelation` model doc comment, and an unverified assumption that InvenTree's `?part=` related-part filter matches either side of the relation (verified by pinning the part-under-test as `part_2` rather than only `part_1`). Round 3 was a PR review posted on #108 (head `b9fd463`) and found the most significant issue of any round: `deletePart` returned immediately on any non-validation `DeletePart` mutation error without verifying whether the deletion had actually applied, contradicting both issue #104's requested recovery behavior and this story's own `partial_failure` claim; also flagged `context.Canceled`/`context.DeadlineExceeded` masking, overclaiming "already-reviewed" confirmation wording, and several alignment gaps. A focused subagent re-review of those fixes (head `79fd1e2`) found no functional defects and three minor test/doc nits, which were fixed. Round 4 was a further PR review on the same head and correctly identified that Round 3's fixes had overstepped: the confirmation-contract question had been answered unilaterally instead of asked of the operator, the F-S32 dependency gap was flagged but not actually resolved, the PR body/issue/docs had not been kept current with the behavior change, the review-completion claim contradicted this story's own "further re-review expected" wording, Round 3's authorship had been mischaracterized as the operator's own review rather than described neutrally, `definiteStockMutationRejection` was imported into part-deletion logic under a stock-specific name, and coverage was missing for context-sentinel preservation and a genuinely-erroring read-back. Round 4's process finding was corrected properly rather than patched over: the confirmation-contract question was put to the operator directly in chat (not inferred from a PR thread), and the F-S32 review gap was spun off as its own task (completed and merged as PR #110) rather than resolved unilaterally inside this story. All other Round 4 findings were fixed: `definiteMutationRejection` extracted to `validation_errors.go` and shared by all four call sites; new focused tests added for context-sentinel preservation and ambiguous-read-back-itself-errors; `docs/tool-reference.md` and `docs/operator-recipes.md` updated for the `recovered` field and all four post-delete-attempt outcomes; this section rewritten to describe every round without asserting review authorship it cannot verify; PR #108 converted to draft pending this round's own re-review. Round 5 (focused subagent re-review of the Round 4 fixes, head `6f975ce`) found one low-severity documentation nit: `docs/tool-reference.md`/`docs/operator-recipes.md` claimed `recovered:false` appears explicitly on an ordinary successful deletion, but `PartDeleteOutput.Recovered` has `json:"recovered,omitempty"`, so `false` is omitted rather than shown -- fixed. All CI checks (CodeQL, both Analyze jobs, goreleaser-snapshot, lint, gremlins, test) passed on that head; PR #108 was taken out of draft.
- Residual risk: preflight checks and the delete call are separate InvenTree requests, so a concurrent writer could create a new reference between the guard check and the delete call; operators should coordinate a single writer, matching the same accepted risk as F-S32. No MCP tool currently exists to remove a supplier-part, manufacturer-part, or related-part link (unlike parameters via `delete_part_parameter` and attachments via `delete_attachment`), so an operator blocked by one of those three categories must clear it through the InvenTree UI directly before retrying `delete_part`. Only stock, the part's own BOM, purchase-order lines, and supplier/manufacturer/related-part links were individually verified for their post-delete survival/destruction behavior; builds, sales-order lines, and variants were confirmed to permit deletion but their child-record fate after that was not independently checked -- delete_part refuses on all of them regardless, so this does not weaken the guard, only the completeness of the documented evidence. Confirmation-contract decision: the operator was asked directly (not inferred) whether `delete_part` should add a state-derived plan-hash/token, matching the stock-adjustment tools (F-S22), or keep the existing stable-ID-plus-fresh-preflight `confirm:true` convention shared by `delete_purchase_order_line`, `delete_part_parameter`, `delete_purchase_order_extra_line`, and `delete_attachment`. The operator chose to keep the existing simple convention. Every call, confirmed or not, re-runs the complete blocking-reference preflight fresh, so the actual safety property is "re-verified immediately before deletion," not "provably matches an operator's earlier preview" -- accepted as sufficient by the operator rather than the heavier plan-hash mechanism. `docs/api-schema.md` records the newly pinned `DELETE /api/part/{id}/` active-state/BOM-component behavior and the guard-only BOM/build/sales-order-line/related-part/variant reads under "Verified Part Deletion And Guard-Only Reads". This story's own review history (Round 3) was described in an earlier revision of this file as "the operator's own PR review" and the corresponding commit trailer as a "human review"; neither claim was verified and both have been corrected to neutral language, since a PR review posted through a GitHub account does not by itself establish who or what authored its content.
### F-S34: Canonical User-Facing InvenTree Object Web URLs

- Status: `Done`
- Issue: [#109](https://github.com/davidvanlaatum/inventree-mcp/issues/109)
- Depends on: M1D-S02, M1E-S01, M1F-S02, F-S03, F-S19, F-S20, F-S21, F-S23, F-S31, F-S32, F-S33
- Progress: implementation started on `codex/f-s34-object-web-urls` from current `origin/main` (`7e0a6d3`) after F-S33 completed and merged through PR #108.
- Decisions: approved by the operator on 2026-08-09, with the missed stock frontend mount corrected by F-S37. (1) `INVENTREE_WEB_URL` is optional in every mode and is the exact operator-configured frontend mount. When omitted, the fallback validates `INVENTREE_URL` as the site/API base and adds InvenTree's pinned stock `/web` mount. Production requires the effective base to use HTTPS; the explicit non-production environment may use HTTP. The configured authority and deployment prefix are operator-authoritative even when internal or not browser-reachable for every MCP user; request/proxy headers, token envelopes, and caller input cannot influence them. (2) A subordinate record without its own stable frontend page omits `web_url` and uses the universal field `parent_web_url` for its immediate owning object when that owner has a stable frontend page. The same projection must expose the owner's stable type and ID through its existing relationship fields; if the immediate owner has no stable page, omit `parent_web_url` rather than walking to a more distant ancestor. A parent page is never presented as the record's own URL. (3) Clarification candidates make a documented breaking change: remove the ambiguous `url` field and add explicit absolute `web_url` plus sanitized relative REST-path `api_url`, with no compatibility alias.
- Scope: define and implement a canonical, absolute, credential-free user-facing link contract for every supported MCP object projection. Keep browser links distinct from REST API URLs and attachment/image/link content URLs. Cover normal reads, searches, writes, workflows, dry-run/recovery records, and clarification candidates without adding new InvenTree mutation capabilities or widening supported object types.
- Acceptance:
  - The approved public web-base, subordinate-record fallback, and breaking clarification-candidate field-migration semantics above remain authoritative through implementation and documentation.
  - The route matrix is verified against the pinned InvenTree frontend version for parts, part categories, companies and supplier/manufacturer views, supplier parts, manufacturer parts, stock locations, stock items, and purchase orders.
  - Every MCP output record type is inventoried and classified as having a direct UI route, an approved parent/context route, or no stable user-facing route.
  - A centralized typed URL resolver constructs frontend routes; individual tool call sites do not infer them from REST endpoint strings.
  - Applicable search, exact-read, create, update, workflow, dry-run/recovery, and clarification outputs return consistent absolute `web_url` values from trusted process configuration only: exact `INVENTREE_WEB_URL` when set, otherwise `INVENTREE_URL` plus the pinned stock `/web` mount in every mode. OAuth token envelopes and request data cannot influence link authority or route selection.
  - `web_url`, clarification-only `api_url`, media URLs, and operator-supplied external `link` values remain semantically distinct in output models and documentation. Clarification `api_url` is a sanitized relative REST path; the existing ambiguous clarification `url` field is removed as an intentional documented breaking change and no compatibility alias remains. F-S34 does not add `api_url` universally.
  - Subordinate records without a stable direct page omit `web_url` and use the universal `parent_web_url` field only for an immediate owning object with a stable frontend page. Existing relationship fields in the same projection identify that owner's stable type and ID; records whose immediate owner has no stable page omit `parent_web_url` rather than linking a more distant ancestor. Exact field presence, omission, and target identity are covered at the JSON and MCP boundaries.
  - URL construction preserves an approved deployment path prefix. Any exact configured `INVENTREE_WEB_URL`, or fallback `INVENTREE_URL` site/API base, fails startup when it has an unsupported scheme, userinfo, query, fragment, or invalid authority. Production requires the effective web base to use HTTPS; the explicit non-production environment may use HTTP. A valid fallback deterministically adds the pinned stock `/web` mount rather than silently starting in a no-link state.
  - Configuration parsing, startup diagnostics, MCP errors, and ordinary logs identify only the configuration key and canonical rejection reason; they never echo a raw rejected base, token, userinfo, query value, fragment, or request-derived authority. Operator-enabled debug traffic logs remain sensitive response captures and may contain returned `web_url` or `parent_web_url` values, including the valid configured internal authority; operator documentation must state that consequence explicitly rather than claiming those debug logs redact response link authorities.
  - Deterministic table-driven route tests, mode-specific process-config tests, negative tests proving token envelopes and requests cannot control link authority, and exact-key JSON/MCP-boundary contract tests cover the approved route matrix and migration behavior.
  - Route evidence is pinned to an immutable InvenTree 1.4.3 frontend source revision and verifies actual router declarations, or uses browser assertions that distinguish the intended object view from the SPA fallback/not-found view and include one deliberately invalid route; an HTTP 200 from the SPA shell alone is insufficient.
  - `docs/PLAN.md`, `docs/api-schema.md`, `docs/tool-reference.md`, `docs/operator-recipes.md`, generated schemas/manifests, and relevant setup guidance are aligned with the final behavior.
  - Package-level coverage is compared with the exact base branch; every reduction is investigated and recovered or documented with size, reason, and residual risk.
  - Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes complete with every actionable finding resolved or explicitly documented.

Tasks:

- [x] Resolve and record the public web-base, subordinate-record fallback, and breaking clarification-candidate migration product decisions.
- [x] Verify and document the pinned frontend route matrix and complete MCP output-type inventory.
- [x] Add optional trusted `INVENTREE_WEB_URL` process configuration, validated all-mode fallback to `INVENTREE_URL`, and a centralized typed resolver for canonical web links and the breaking clarification API-link migration; keep the resolver independent of OAuth credentials and envelopes.
- [x] Project the approved links through every applicable lookup, mutation, workflow, recovery, dry-run, and clarification output.
- [x] Add deterministic unit, exact-key JSON/MCP-boundary, mode-specific startup configuration including every unsafe omitted-web-base fallback case, envelope/request authority-refusal, rejected-value diagnostic and ordinary-log redaction, sensitive debug-response link capture, route-prefix, and immutable pinned-frontend or discriminating live-navigation coverage.
- [x] Align planning, schema, tool-reference, operator, generated-contract, and setup documentation.
- [x] Compare per-package coverage, run full validation, and resolve the Go, QA, product, and infosec review panel.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `GOFLAGS=-trimpath go test -race -p=1 ./...` (including the default-on pinned InvenTree 1.4.3 Testcontainers suites), final no-integration coverage, and `git diff --check` pass. Immutable route evidence pins the complete composed frontend routes to InvenTree 1.4.3 commit `6b237de54e4cbfd7f51daff8403c17869898d965` and router blob `ddeb3a21365761e999568c84d6417915817a9024`. Compared with exact base `origin/main` at `7e0a6d3`, `cmd/inventree-mcp` rises from 90.5% to 91.1%, `internal/config` from 92.8% to 93.0%, and `internal/tools` from 82.9% to 83.1%; new `internal/weblinks` is 94.3%, and no package-level reduction was introduced.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer implementation passes completed because F-S34 changes configuration, the public tool surface, reverse-proxy-visible URLs, and agent/operator link contracts. Findings required complete composed-route evidence, an exhaustive output inventory, bare-query rejection, hostile HTTP/OAuth authority-refusal coverage, derivation-only clarification links, category/location parameter-owner mappings, exact MCP candidate-key checks, unsafe fallback-path coverage, and constraining the reflection walker to current acyclic DTO shapes. All findings were addressed in code, tests, or docs; focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: frontend routes and basenames are not part of the REST OpenAPI contract and can drift between InvenTree versions. F-S37 corrected F-S34's incomplete evidence and missing stock `/web` fallback mount. Under the approved all-mode fallback, the operator-authoritative `INVENTREE_URL` may expose an internal DNS name or topology in returned links to every MCP caller allowed to receive the associated object, may be unreachable for some users, and may appear in operator-enabled sensitive debug traffic logs because those logs capture response bodies. Operators who need externally reachable or non-internal links, or whose frontend basename is not `web`, must set the exact `INVENTREE_WEB_URL`; route pinning, strict validation, and ordinary-log/error redaction do not remove this accepted disclosure and reachability risk.

### F-S35: Local Upload Policy Discovery

- Status: `Done`
- Issue: [#113](https://github.com/davidvanlaatum/inventree-mcp/issues/113)
- Depends on: M1F-S01, M1F-S02, F-S31
- Progress: implementation completed on `codex/f-s35-local-upload-policy` from current `origin/main` (`54d2870`); pull-request review and merge remain external lifecycle steps.
- Decisions: approved by the operator on 2026-08-14. Register a read-only `get_local_upload_policy` tool only in STDIO mode. It returns the canonical configured roots, separate effective attachment and company-image maximum bytes, and concise local-file requirements. The roots describe where the MCP server may read files; they do not claim that the consuming agent can write there. HTTP mode never registers the discovery tool or exposes roots. Local-path policy failures return bounded reason-specific structured recovery instead of embedding roots in errors or static tool descriptions: outside-root failures direct the agent to discovery and permitted staging, while a missing allowlist asks for operator configuration or inline content.
- Scope: let a locally running MCP agent discover the operator-configured local-upload policy before staging an attachment or company image, and recover deterministically from local-path policy failures without weakening filesystem enforcement or disclosing server paths remotely.
- Acceptance:
  - `get_local_upload_policy` is a read-only, closed-world tool registered only when the server upload mode is STDIO.
  - The tool returns structured local-path availability, canonical configured allowed roots, separate effective attachment and company-image maximum sizes, and requirements covering pre-existing regular files, containment, and symlink policy.
  - Policy output explicitly distinguishes server-readable roots from caller write permission; it makes no claim that the consuming agent can create files in a returned root.
  - HTTP registration omits the tool and never exposes configured filesystem roots through discovery, tool output, or local-path rejection.
  - Attachment and company-image local-path policy failures use bounded structured recovery with a stable reason: outside-root failures direct the agent to `get_local_upload_policy`, while a missing allowlist requests operator configuration or inline content; successful uploads and existing filesystem enforcement remain unchanged.
  - Focused tests cover STDIO registration and output, canonical root reporting, empty policy, HTTP non-registration/non-exposure, both upload-tool recovery paths, and preservation of successful local uploads.
  - `docs/PLAN.md`, `docs/tool-reference.md`, and `docs/operator-recipes.md` remain aligned with the behavior.
  - Per-package coverage is compared with the exact base branch, and Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes complete with actionable findings resolved or documented.

Tasks:

- [x] Add the STDIO-only read-only local-upload policy discovery tool and output contract.
- [x] Add bounded structured allowlist recovery to attachment and company-image local upload failures.
- [x] Add registration, output, failure-recovery, and non-exposure tests.
- [x] Align planning, tool-reference, and operator documentation.
- [x] Compare coverage, run validation, and resolve the full review panel.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), focused `go test -tags no_integration_tests ./internal/upload ./internal/tools ./internal/server`, final `GOFLAGS=-trimpath go test -race -p=1 ./...` including the default-on pinned InvenTree 1.4.3 Testcontainers suites, final no-integration coverage, and `git diff --check` pass. The first full-suite run exposed a stale integration assertion that expected the former raw allowlist error; it was updated to assert structured recovery, its focused pinned-live rerun passed, and both subsequent full race suites passed. Compared with exact base `origin/main` at `54d2870`, `internal/upload` rises from 78.2% to 78.5%, while `internal/tools` remains 83.1% and `internal/server` remains 76.2%; no package-level reduction was introduced.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed because F-S35 changes the upload and public tool surfaces. Findings required separate effective attachment and company-image limits, reason-specific missing-allowlist recovery, MCP-boundary structured-output/schema assertions, explicit HTTP secret-root non-exposure checks, and deterministic missing-policy classification before production filesystem path resolution. All findings were addressed in code, tests, and docs; final focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: canonical configured roots are intentionally disclosed to connected STDIO MCP clients and may be captured in operator-enabled sensitive STDIO debug traffic logs. Returned roots describe server-read policy and do not guarantee caller write access. Roots must remain trusted and operator-controlled because production `OsFs` retains the existing time-of-check/time-of-use race between path resolution/policy checks and open if an untrusted party can swap filesystem entries.

### F-S36: Versioned Outbound HTTP User-Agent

- Status: `Done`
- Depends on: M1B-S01, M1F-S02, F-S31
- Progress: implementation completed on `codex/f-s36-inventree-user-agent`, rebased onto current `origin/main` (`72e9584`) after F-S35 merged.
- Decisions: approved by the operator on 2026-08-14. Use the same versioned identity for every HTTP request initiated by the shipped server or CLI, including requests to InvenTree, caller-selected URL-upload targets, OAuth client metadata/JWKS origins, and GitHub self-update endpoints. Do not rewrite inbound client requests or test-only infrastructure probes.
- Scope: identify every outbound HTTP request initiated by the shipped MCP server or CLI with the product name and build version.
- Acceptance:
  - InvenTree JSON API requests and authenticated attachment, part-image, and company-image downloads send `User-Agent: inventree-mcp/<build-version>`.
  - Arbitrary URL-upload fetches, OAuth client-metadata and JWKS fetches, and self-update GitHub requests send the same identity.
  - The version comes from the same build metadata used by the CLI `version` command and MCP `health_version` tool, with the development build reporting `inventree-mcp/dev`.
  - Focused tests cover normal API requests, each same-instance media-download path, URL uploads, both OAuth fetch classes, and self-update requests.
  - `docs/PLAN.md` remains aligned with the behavior; the InvenTree OpenAPI snapshot and MCP tool/operator contracts are unchanged.
  - Per-package coverage is compared with the exact base branch, and Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews complete with actionable findings resolved or documented.

Tasks:

- [x] Add one build-metadata-derived User-Agent identity shared by outbound runtime request paths.
- [x] Apply it to InvenTree requests, URL-upload fetches, OAuth metadata/JWKS retrieval, and self-update traffic.
- [x] Add focused direct and allowed-redirect request-header tests and align planning documentation.
- [x] Compare per-package coverage, run validation, and resolve the Go, QA, product, and infosec review panel.

- Validation: `go test -tags no_integration_tests ./internal/buildinfo ./internal/inventree ./internal/upload ./internal/oauth ./internal/selfupdate`, `go mod tidy -diff`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), final `go test -tags no_integration_tests -cover ./...`, final `GOFLAGS=-trimpath go test -race -p=1 ./...` including the default-on pinned InvenTree 1.4.3 Testcontainers suites, and `git diff --check` pass. Compared with exact base `origin/main` at `72e9584`, `internal/oauth` rises from 83.3% to 83.4%, `internal/selfupdate` from 78.0% to 78.2%, and `internal/upload` from 78.7% to 79.5%; the newly testable `internal/buildinfo` function is covered at 100%, all other packages remain unchanged, and no package-level reduction was introduced.
- Review: Senior Go Developer review found no actionable implementation or package-boundary issues. Senior QA / Test Architect review required allowed-redirect User-Agent coverage for URL uploads, JWKS retrieval, and self-update traffic, and replacement of a fatal assertion inside an HTTP handler goroutine; the tests were corrected and the focused QA rerun found no remaining issues. Senior Product Manager review required an explicit shipped-runtime boundary excluding inbound requests and test-only probes; the planning/task docs were corrected and the focused product rerun found no remaining issues. Senior Infosec review required the exact-build fingerprinting and correlation exposure to be recorded; the residual-risk note was added and the focused infosec rerun found no remaining issues.
- Residual risk: caller-selected URL-upload targets, operator-configured OAuth client metadata/JWKS origins, and their allowed redirect targets can fingerprint the product and exact build version and correlate its requests. This operator-approved identity contains no credential. Existing SSRF, same-origin, allowlist, and redirect controls bound which destinations may receive requests but intentionally do not conceal the identity.

### F-S37: Restore Default `/web` Prefix in Canonical Object Links

- Status: `Done`
- Issue: [#116](https://github.com/davidvanlaatum/inventree-mcp/issues/116)
- Depends on: F-S34
- Progress: implementation completed on `codex/f-s37-web-route-prefix` from current `origin/main` (`d823a0a`). The operator reported that a purchase-order link omitted `/web`; pinned InvenTree 1.4.3 source confirms the nested routes are mounted by `BrowserRouter basename={getBaseUrl()}` and `getBaseUrl()` defaults to `web`. GitHub issue [#116](https://github.com/davidvanlaatum/inventree-mcp/issues/116) tracks the correction and remains open for the PR lifecycle.
- Decisions: the `INVENTREE_URL` fallback is the InvenTree site/API base and therefore adds the pinned stock frontend mount `/web`. An explicit `INVENTREE_WEB_URL` remains the exact operator-supplied frontend mount so custom InvenTree `base_url` values and reverse-proxy path prefixes remain supported without guessing.
- Scope: correct every canonical direct and parent object link produced through the `INVENTREE_URL` fallback without changing object coverage, caller-visible fields, trusted-authority selection, or explicit custom frontend mounts.
- Acceptance:
  - When `INVENTREE_WEB_URL` is omitted, links use `<INVENTREE_URL>/web/...` while preserving any deployment prefix and canonical slash handling.
  - An explicit `INVENTREE_WEB_URL` remains the exact frontend mount and is not modified with an additional `/web` segment.
  - All supported direct `web_url` and subordinate `parent_web_url` projections inherit the corrected resolver base consistently.
  - Immutable InvenTree 1.4.3 evidence pins both the nested object routes and the outer `BrowserRouter` plus default `getBaseUrl()` mount.
  - Focused resolver, configuration, and process-dependency tests cover stock fallback, a deployment prefix, trailing-slash normalization, the reported purchase-order route, and an explicit custom mount.
  - README, planning, schema capability notes, web-link documentation, operator recipes, and packaged environment examples describe the corrected fallback and explicit-base semantics.
  - Package-level coverage is compared with current `main`; Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes complete with every actionable finding resolved or documented.

Tasks:

- [x] Add the pinned default frontend mount to fallback-generated links while preserving exact explicit frontend mounts.
- [x] Extend immutable frontend evidence and focused tests across resolver, configuration, and process dependency construction.
- [x] Align public, operator, planning, schema-capability, setup, packaging, task, and issue documentation.
- [x] Compare coverage, run full validation, and resolve the Go, QA, product, and infosec review panel.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test -tags no_integration_tests ./...`, `GOFLAGS=-trimpath go test -race -p=1 ./...` including the default-on pinned InvenTree 1.4.3 Testcontainers suites, and `git diff --check` pass. Focused assertions cover the reported purchase-order 65 route, deployment-prefix and trailing-slash preservation, exact explicit custom mounts, runtime dependency wiring, and rejected default-base validation. Compared with exact base `d823a0a`, `cmd/inventree-mcp` remains 91.1%, `internal/config` rises from 93.0% to 93.1%, and `internal/weblinks` rises from 94.3% to 94.7%; no package-level reduction remains.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed with no actionable findings. Go confirmed the resolver/config boundary and URL construction are maintainable; QA confirmed compositional projection coverage, unsafe-base/redaction regression coverage, and pinned mount evidence; Product confirmed README, plan, task, schema notes, web-link docs, tool reference, operator recipe, packaging example, and issue semantics align; Infosec confirmed `/web` is added only after existing URL validation, explicit mounts remain exact, request/header/OAuth inputs cannot influence routing, and disclosure/custom-basename risks remain accurately documented.
- Residual risk: frontend basenames are not part of the REST OpenAPI contract and can drift. Explicit `INVENTREE_WEB_URL` remains required for installations whose frontend mount differs from the pinned stock InvenTree 1.4.3 default.

### F-S38: Explicit Purchase-Order Completion After Receiving

- Status: `Done`
- Issue: [#118](https://github.com/davidvanlaatum/inventree-mcp/issues/118)
- Depends on: F-S03
- Progress: implementation started on `codex/f-s38-po-completion` from current `origin/main` (`dd8f588`).
- Decisions: approved by the operator on 2026-08-15. Ordinary receiving respects InvenTree's `PURCHASEORDER_AUTO_COMPLETE` setting. A caller may explicitly request completion as part of a confirmed final receipt or complete the fully received order later through a separate guarded tool. The MCP must not complete an order with outstanding receivable line quantities and must not expose InvenTree's `accept_incomplete` override.
- Scope: add explicit, guarded purchase-order completion without changing the default receipt behavior or repeating a successful receipt when a follow-on completion attempt fails.
- Acceptance:
  - `receive_purchase_order_items` preserves configuration-dependent upstream auto-completion when explicit completion is not requested.
  - A reviewed final-receipt plan can bind an explicit completion request; confirmed execution receives stock once, completes the order when still placed and fully received, and returns refreshed `COMPLETE` state.
  - A separate guarded `complete_purchase_order` workflow supports later completion of a fully received placed order, with current-state review, confirmation, read-back, and safe ambiguous-result recovery.
  - Explicit completion refuses any order with outstanding ordinary line quantity; incomplete completion is not exposed.
  - If receipt succeeds but explicit completion does not produce a verified result, output preserves the created stock and returns actionable completion-only recovery without instructing the caller to repeat receipt.
  - Existing partial-receipt, receipt response-loss recovery, extra-line, stock-provenance, and upstream auto-completion behavior remains unchanged.
  - Pinned InvenTree 1.4.3 Testcontainers coverage verifies configuration-disabled deferred completion, receipt-time explicit completion, and later explicit completion using per-subtest fixtures.
  - `docs/PLAN.md`, `docs/api-schema.md`, `docs/tool-reference.md`, and `docs/operator-recipes.md` describe the verified completion semantics.
  - Per-package coverage is compared with current `main`; Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes complete with every actionable finding resolved or documented.

Tasks:

- [x] Add schema-backed purchase-order completion client behavior and deterministic tests.
- [x] Add guarded later completion and receipt-time explicit completion with safe recovery semantics.
- [x] Add pinned-live coverage for auto-complete-disabled and explicit completion paths.
- [x] Align planning, schema, tool-reference, operator, and generated-contract documentation.
- [x] Compare coverage, run full validation, and resolve the Go, QA, product, and infosec review panel.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test -tags no_integration_tests ./...`, `GOFLAGS=-trimpath go test -race -p=1 ./...`, and `git diff --check` pass. Focused pinned InvenTree 1.4.3 runs pass for `TestClientMethodsAgainstInvenTree/po` and `TestMilestoneHappyPathToolsAgainstInvenTree/purchase_order_completion_with_auto_complete_disabled`. Compared with exact base `dd8f588`, `internal/inventree` remains 88.2% and `internal/tools` rises from 83.1% to 83.2%; no package-level coverage reduction remains.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed, including focused reruns after resolving two findings. Product identified that a failed refresh after a known-success receipt with `complete_order:true` could incorrectly invite receipt recovery; the path now preserves returned stock and permits completion-only recovery, with regression coverage. QA identified that pinned tests restored the global auto-complete setting to a presumed default; both suites now capture, restore, and read-back verify the exact original value. Focused Go, QA, product, and infosec reruns found no remaining actionable findings.
- Residual risk: receipt and explicit completion are separate upstream mutations, so completion can fail after stock was received; the response preserves returned stock and mandates completion-only recovery. Completion preflight and mutation are not atomic against concurrent upstream writers, though the MCP never sends `accept_incomplete:true` and exact read-back is required for verified success.

### F-S39: Preserve Complete External URLs

- Status: `Done`
- Issue: [#120](https://github.com/davidvanlaatum/inventree-mcp/issues/120)
- Depends on: none
- Decisions: approved by the operator on 2026-08-15. Successful reads and writes preserve HTTP(S) query parameters and fragments because removing them can break functional links. URLs containing userinfo or credentials remain invalid. Errors, clarification candidates, ordinary structured logs, and minimal recovery projections remain URL-free. Operator-enabled sensitive traffic logging can capture complete authorized response bodies, including external URLs, under the existing explicit warning.
- Scope: replace the existing query/fragment-stripping response policy across currently exposed company website, supplier-part, manufacturer-part, stock-item, stored attachment-link, and purchase-order extra-line fields. Dependent stories must apply the same policy when they expose part, company-link, purchase-order, and ordinary purchase-order-line fields.
- Acceptance:
  - Existing supplier-part, manufacturer-part, stock-item, company, attachment-link, and purchase-order extra-line successful reads preserve scheme, host, path, query, and fragment.
  - Part, company-link, purchase-order, and ordinary purchase-order-line fields use the same policy when exposed by dependent stories.
  - Writes accept only valid HTTP(S) URLs without userinfo and preserve accepted values through exact read-back.
  - URLs remain absent from errors, clarification candidates, ordinary structured logs, and minimal recovery projections; sensitive traffic-body logging retains its documented opt-in disclosure boundary.
  - Deterministic tests cover credentials, query strings, fragments, malformed URLs, and redaction boundaries.
  - `docs/PLAN.md`, `docs/api-schema.md`, `docs/tool-reference.md`, and `docs/operator-recipes.md` are aligned.
- Tasks:
  - [x] Centralize complete credential-free HTTP(S) validation and successful projection across the currently exposed URL surfaces.
  - [x] Require exact stable-ID read-back for company and sourcing-link creates, updates, and the combined part workflow.
  - [x] Keep errors, clarification records/candidates, ordinary logs, and partial or ambiguous recovery projections URL-free while retaining the explicit sensitive traffic-body logging boundary.
  - [x] Align OAuth scopes, generated metadata, public documentation, deterministic tests, and the canonical issue.
  - [x] Compare exact-base package coverage, run full validation, and resolve the Go, QA, product, and infosec review panel.
- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test -tags no_integration_tests ./...`, `GOFLAGS=-trimpath go test -race -p=1 ./...` against pinned InvenTree 1.5.0, focused `GOFLAGS=-trimpath go test -race ./internal/tools ./docs`, and `git diff --check` pass. Compared with exact base `origin/main` at `ea4cd687`, every no-integration package coverage percentage is unchanged except `internal/tools`, which rises from 83.2% to 83.4%.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed. Findings on exact create/read-back, URL-bearing recovery and clarification records, recovered-success PATCH projections, OAuth read scope, and deferred-scope wording were resolved with deterministic regressions and aligned docs. Focused reruns of all four roles found no remaining actionable findings.
- Residual risk: query strings can contain sensitive data even without userinfo; authorized successful reads intentionally preserve them for functionality, so callers remain responsible for treating returned URLs as inventory data and the MCP must continue preventing secondary disclosure.

### F-S40: Complete Part Exact Reads And Scalar Maintenance

- Status: `Done`
- Issue: [#121](https://github.com/davidvanlaatum/inventree-mcp/issues/121)
- Depends on: F-S39
- Decisions: approved by the operator on 2026-08-15. Exact reads expose all approved default scalar fields already returned by the API, while searches remain concise. `creation_user` is read-only. API 530's nested category/location detail, category path, and parameters remain separate lookups; tags and price breaks remain deferred to F-S56 and F-S58. Raw `barcode_hash` remains excluded pending F-S55.
- Scope: expand `get_part`, `create_part`, and `update_part` for complete default reads and the approved ordinary writable fields without absorbing guarded family relationships, ownership, tags, barcode workflows, pricing, requirements, or test-result administration.
- Acceptance:
  - `get_part` exposes all approved default readable scalar fields, including IPN, external link, revision metadata, stock and allocation aggregates, pricing bounds, creation metadata, template/lock/test flags, units, and notes; `search_parts` retains only high-value selection fields and raw `barcode_hash` is excluded pending F-S55.
  - A checked-in pinned field inventory classifies every default serializer/response field as exposed, separate lookup, deferred, write-only, or excluded; contract tests compare representative raw keys against that inventory so unclassified omissions and schema drift fail.
  - Create/update supports `consumable`, `default_expiry`, `is_template`, keywords, link, locked, minimum/maximum stock, revision text, salable, testable, and Markdown notes with explicit clear/reset semantics where applicable.
  - The existing create/update surfaces retain `inventree.read` plus `inventree.write`, remain closed-world and non-destructive, and preserve their reviewed per-tool idempotency annotations in the authorization and generated-tool manifests.
  - `creation_user` remains read-only and is verified against pinned serializer behavior; primary-image mutation remains in dedicated image tools.
  - Minimum/maximum stock are non-negative decimals and a nonzero maximum below the minimum is rejected; default expiry is non-negative and `0` resets the default.
  - Pinned integration and JSON contract tests verify complete reads, writes, clears, omitted-vs-explicit values, API 530 schema/read-back presence for part notes, and field-inventory drift; public docs remain aligned.
- Validation: `go generate ./internal/tools`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test -tags no_integration_tests ./...`, pinned `GOFLAGS=-trimpath go test -race -p=1 ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/part_category$' -count=1`, pinned `GOFLAGS=-trimpath go test -race -p=1 ./internal/tools -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/part_exact_detail_and_scalar_maintenance$' -count=1`, affected-package coverage comparison, and `git diff --check` pass. Against exact base `origin/main` at `d9c1ce94`, `internal/inventree` rises from 88.2% to 88.3%, `internal/tools` rises from 83.4% to 83.5%, and `internal/server`, `internal/schema`, and `docs` remain unchanged at 76.2%, 64.9%, and 57.1%.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed. Findings on the F-S41 family-field boundary, structured local validation, canonical exact-part links, exhaustive inventory/failure tests, stable-ID recovery, factual partial-output shape, and typed MCP output-schema compatibility were resolved. Focused reruns of all four roles found no remaining actionable findings.
- Residual risk: some computed part values are user- or configuration-dependent and nullable; exact reads must preserve factual nulls without treating unavailable aggregates as zero.

### F-S41: Guarded Part Revision And Variant Relationships

- Status: `Done`
- Issue: [#122](https://github.com/davidvanlaatum/inventree-mcp/issues/122)
- Depends on: F-S40
- Decisions: approved by the operator on 2026-08-15. `revision_of` and `variant_of` are separate from ordinary scalar metadata because they change part-family topology.
- Progress: implementation and initial validation are complete on `codex/f-s41-part-family-relationships` from current `origin/main` at `1094d6aa`. The dedicated guarded tool keeps family topology separate from ordinary scalar `update_part`; review follow-ups added retryable-4xx read-back, fail-closed malformed-topology handling, field-specific missing-target clarification, minimal recovery projections, pinned upstream rejection coverage, and MCP wire-contract coverage.
- Scope: expose complete family references and add guarded stable-ID updates for revision and variant relationships.
- Acceptance:
  - Exact part reads expose `revision_of`, `revision_count`, and `variant_of` with stable IDs.
  - Writes accept stable target part IDs and explicit clear operations, reject self-reference and cycles, and validate target existence.
  - Every assignment, replacement, or clear requires a principal-bound state-bound plan token covering both current family references, the requested after-state targets, relevant eligibility state, and traversal evidence; execution requires `confirm:true`, and stale plans fail before mutation.
  - Cycle and target validation use deterministic traversal, shared request/record budgets, and fail closed when the complete relevant topology cannot be proven within bounds.
  - Because scopes and annotations are descriptor-level, every call requires `inventree.read`, `inventree.write`, `inventree.operational`, and `inventree.destructive`, remains closed-world and non-idempotent, and publishes `destructiveHint:true`.
  - Writes use exact read-back and safe ambiguous-result recovery without leaking unrelated part data.
  - Pinned integration tests establish InvenTree cycle, revision, and variant semantics; plan, task, schema, tool-reference, and operator docs are aligned.
- Tasks:
  - [x] Expose exact revision and variant family fields and add the pinned server-side revision filter.
  - [x] Add the dedicated state-bound family relationship plan and mutation tool with shared bounded traversal.
  - [x] Add deterministic token, cycle, stale-topology, recovery, authorization, and pinned integration coverage.
  - [x] Align plan, schema capability notes, tool reference, operator recipe, prompt, and generated manifest.
  - [x] Complete validation, package coverage comparison, and the required Go, QA, product, and infosec review panel.
- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test -tags no_integration_tests ./internal/tools ./docs`, focused pinned `GOFLAGS=-trimpath go test -race ./internal/tools -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/part_family_relationships$' -count=1`, full `GOFLAGS=-trimpath go test -race -p=1 ./...`, and `git diff --check` pass. Against exact base `origin/main` at `1094d6aa`, `internal/inventree` rises from 88.3% to 88.4% and `internal/tools` rises from 83.5% to 83.8%; no package-level coverage reduction remains.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed. Findings on retryable 408/425/429 recovery, missing and malformed topology, pinned upstream rejection evidence, MCP wire coverage, descriptor-level acceptance wording, and minimal recovery disclosure were resolved with focused reruns. CI then exposed an incorrect test assumption: pinned InvenTree accepts the tested circular revision chain, while the MCP supplies the missing bounded cycle guard. The corrected pinned test and docs received focused Go, QA, and product reruns; all applicable roles report no remaining actionable findings.
- Residual risk: preflight and relationship PATCH are separate requests, so concurrent topology changes remain a single-writer concern unless InvenTree exposes an atomic constraint.

### F-S42: Related-Part Link Administration

- Status: `Done`
- Issue: [#123](https://github.com/davidvanlaatum/inventree-mcp/issues/123)
- Depends on: F-S40
- Decisions: approved by the operator on 2026-08-15. Related-part links must be usable outside `delete_part` preflight and removable without falling back to the InvenTree UI. Relation updates are note-only; changing either linked part requires guarded deletion and creation of a new relation.
- Scope: add bounded list and exact-get tools plus guarded create, note-only update/clear, and confirmed single-link deletion. Endpoint replacement is excluded; callers delete and recreate through separately reviewed operations.
- Acceptance:
  - `list_part_relations` requires a stable part filter and returns bounded results for links where the part appears on either side; `get_part_relation` reads one stable relation.
  - Records expose relation ID, both stable part IDs, and note.
  - Create rejects self-relations and verified duplicates; update is note-only, supports note clearing, and requires a state-bound token covering the current note so changes visible during confirmation preflight invalidate stale plans. Duplicate checks on both directions use deterministic deduplication, shared request/record budgets, and fail closed when completeness cannot be proven.
  - Delete previews the exact relation and returns a token bound to its current endpoints and note; execution requires confirmation plus the matching token, rejects stale plans, verifies removal, and safely handles ambiguous results.
  - Reads require `inventree.read`; create/note update require `inventree.read` and `inventree.write`, remain closed-world and non-idempotent, and are non-destructive. Delete additionally requires `inventree.destructive` and publishes `destructiveHint:true`.
  - Pinned integration coverage verifies endpoint filter semantics, duplicate/direction behavior, CRUD, delete-part unblock behavior, and public documentation.
- Tasks:
  - [x] Pin upstream generic and directional filter semantics plus undirected reversed-duplicate behavior.
  - [x] Add bounded list/exact reads and typed related-part client CRUD methods.
  - [x] Add guarded create, note-only update/clear, and confirmed deletion with exact read-back and bounded recovery.
  - [x] Align scopes, annotations, manifests, schema notes, tool reference, operator recipe, and prompt guidance.
  - [x] Complete pinned integration coverage, package coverage comparison, and the required Go, QA, product, and infosec review panel.
- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), focused `go test -tags no_integration_tests ./internal/tools -run 'PartRelation|RelatedPart' -count=1`, pinned `GOFLAGS=-trimpath go test -race ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/part_relation_crud$' -count=1`, pinned `GOFLAGS=-trimpath go test -race ./internal/tools -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/part_relation_administration$' -count=1`, full `GOFLAGS=-trimpath go test -race -p=1 ./...`, and `git diff --check` pass. Against exact base `origin/main` at `ec35b039`, `internal/inventree` rises from 88.4% to 89.2% and `internal/tools` remains 83.8%; no package-level coverage reduction remains.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed. Findings on exact created-ID read-back, cancellation propagation, truthful partial/recovered outputs, exact shared-budget boundaries, deterministic deduplication, MCP wire contracts, delete-part unblock coverage, stale residual-risk wording, and the non-atomic mutation window were resolved. Focused reruns of all four roles found no remaining actionable findings.
- Residual risk: pinned InvenTree 1.5.0 treats relation identity as undirected and rejects reversed duplicates, but duplicate/confirmation preflight and mutation are separate requests with no upstream compare-and-swap. A concurrent UI, API, or other MCP replica write in that gap can be overwritten or deleted even though postflight verifies the final MCP state. Keep relation administration single-writer, fail closed when either directional scan exceeds its shared budget, and reconcile exact stable-ID read-back before retrying an ambiguous write.

### F-S43: Sourcing-Link Detail Completeness

- Status: `Done`
- Issue: [#124](https://github.com/davidvanlaatum/inventree-mcp/issues/124)
- Depends on: F-S20, F-S39
- Decisions: approved by the operator on 2026-08-15. Exact supplier/manufacturer-part reads expose all approved fields; list/search projections remain concise and embedded company detail remains a separate `get_company` call. Raw `barcode_hash` remains excluded pending F-S55.
- Scope: add missing sourcing-link read fields and long Markdown note writes without expanding into barcode or pricing workflows.
- Acceptance:
  - `get_supplier_part` exposes approved default API fields including computed in-stock quantity, derived MPN, updated timestamp, short note, long Markdown notes, upstream availability and its update timestamp, and on-order quantity.
  - `get_manufacturer_part` exposes all approved ordinary fields including long Markdown notes while retaining manufacturer ID instead of embedding company detail.
  - Checked-in supplier/manufacturer field inventories classify every pinned default response field as exposed, separate lookup, deferred, write-only, or excluded; raw-key contract tests fail on unclassified omissions or drift.
  - API 530's write-only supplier/manufacturer `duplicate` inputs remain excluded from ordinary create/update tools pending separately approved guarded duplication workflows.
  - Supplier/manufacturer long notes are writable and explicitly clearable with exact read-back; existing short supplier note remains distinct. Supplier `available` is writable with verified decimal and explicit-zero semantics plus exact read-back, while computed `availability_updated` and `on_order` remain read-only.
  - Existing sourcing create/update surfaces retain `inventree.read` plus `inventree.write`, remain closed-world and non-destructive, and preserve their reviewed per-tool idempotency annotations in authorization and generated-tool manifests.
  - API 530 nested part, company, and manufacturer-link details remain separate exact lookups; parameters, tags, and price breaks remain in their dedicated parameter, tag, and pricing workflows. Search results retain only high-value selection fields; barcode behavior is deferred to its dedicated story.
  - External links follow F-S39 and tests/docs cover nullable fields, note distinction, sanitization boundaries, and pinned API behavior.
- Tasks:
  - [x] Add exhaustive pinned supplier/manufacturer-part field inventories and complete approved exact-read projections while retaining concise searches.
  - [x] Add long-note and supplier availability create/update/clear handling with exact read-back and explicit null/empty/zero semantics.
  - [x] Preserve URL-, description-, and note-free minimal recovery, including positive-ID exact-get and missing-ID bounded-search create recovery.
  - [x] Align plan, schema notes, tool reference, operator recipe, generated contracts, integration coverage, and the required reviewer panel.
- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test -tags no_integration_tests ./...`, focused pinned `GOFLAGS=-trimpath go test -race ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/writes$' -count=1`, focused pinned `GOFLAGS=-trimpath go test -race ./internal/tools -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/company_and_sourcing_link_administration$' -count=1`, full serialized `GOFLAGS=-trimpath go test -race -p=1 ./...`, and `git diff --check` pass. Against exact base `origin/main` at `81cb57fd`, `internal/inventree` remains 89.2% and `internal/tools` rises from 83.9% to 84.0%; no package-level coverage reduction remains. Concurrent base/current coverage attempts both encountered the same unrelated `internal/selfupdate` timing failure; the serialized race suite passed that package and the full repository.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed. Findings on nullable exact-wire keys, truthful direct-create projection documentation, privacy-minimal partial-create recovery, conditional recovery without a positive stable ID, fail-closed nonpositive IDs, operator guidance, and negative-ID coverage were resolved. Focused reruns of all four roles found no remaining actionable findings.
- Residual risk: derived MPN and in-stock values can change independently of the sourcing-link record; callers must treat them as read-time context rather than stable identity.

### F-S44: Company Detail And Role Completeness

- Status: `Done`
- Issue: [#125](https://github.com/davidvanlaatum/inventree-mcp/issues/125)
- Depends on: F-S20, F-S31, F-S39
- Decisions: approved by the operator on 2026-08-15. Phone, email, free-text contact, business tax ID, external link, image URL, supplied/manufactured counts, and customer role are in scope. Tax ID is for business identifiers such as ABN/ACN, not personal TFNs.
- Scope: expand exact company reads and ordinary metadata updates while preserving separate primary-image workflows and deferring structured contacts/addresses, ownership, parameters, and tags. Customer support is limited to role-flag administration and dependency safety; it adds no sales-order tools, customer contact/billing workflow, CRM behavior, or customer defaults.
- Acceptance:
  - `get_company` exposes complete approved exact fields including phone, email, contact, tax ID, external link, primary-image URL, Markdown notes, customer role, and supplied/manufactured counts; searches remain concise. API 530 `primary_address` remains a separate structured-address lookup and parameters/tags remain in their dedicated stories.
  - A checked-in pinned field inventory classifies every default company response field as exposed, separate lookup, deferred, write-only, or excluded; raw-key contract tests fail on unclassified omissions or drift.
  - API 530's write-only company `duplicate` input remains excluded from ordinary create/update and role tools pending a separately approved guarded company-duplication workflow.
  - Phone, email, contact, tax ID, external link, and Markdown notes are writable and explicitly clearable with exact read-back.
  - Adding/removing customer role is supported; removal requires a state-bound plan token covering the role and a bounded complete dependency audit, refuses if any dependency remains, and fails closed when permissions, pagination, or unsupported surfaces prevent proving completeness.
  - Exact reads require `inventree.read`; ordinary metadata and role addition require `inventree.read` and `inventree.write`. Role removal additionally requires `inventree.destructive`, publishes `destructiveHint:true`, remains closed-world and non-idempotent, and rejects stale plans.
  - Tax IDs and contact data remain absent from logs, errors, clarification candidates, and minimal recovery projections; image mutation remains in dedicated tools.
  - F-S39 URL behavior, pinned role/dependency tests, and plan/schema/tool/operator documentation are aligned.
- Validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go generate ./internal/tools` (no manifest diff), and `git diff --check` all pass. `go test ./...` (full suite, including the blocking `TestClientMethodsAgainstInvenTree/company_detail_and_role_completeness` and `TestMilestoneHappyPathToolsAgainstInvenTree/company_customer_role_removal_happy_path` Testcontainers subtests against pinned `inventree/inventree:1.5.0`) passes; rerun clean after every review-driven fix below. New fast unit coverage in `internal/tools/company_admin_tools_test.go` and `internal/tools/company_role_tools_test.go` (11 new tests) exercises exact-read field exposure, write/clear round trips for phone/email/contact/tax_id/link, `is_customer:true` addition versus unconditional `is_customer:false` rejection, the guarded-removal dry-run/confirm/dependency-block/stale-token/audit-error/ambiguous-recovery/definite-rejection paths, and plan-store single-use/mismatch/expiry behavior, all without Testcontainers. `internal/inventree/company_field_inventory_test.go` pins every API 530 `Company` field's classification against `docs/api-schema.yaml` and a nullable-preservation JSON test. Per-package coverage under `-tags no_integration_tests`: `internal/tools` 84.1% (baseline 84.0%); `internal/inventree` 87.3% (baseline 88.2%, a small expected reduction — see Residual risk).
- Review: full Go, QA, product, and infosec panel completed (required: new mutating destructive workflow tool, `remove_company_customer_role`). Findings addressed: (1) Go — the post-token-consumption re-audit could burn the single-use plan token even when it correctly blocked execution on a dependency race, contradicting the intended "a blocked attempt never spends the token" guarantee; restructured `removeCompanyCustomerRole` so the dependency audit always runs exactly once per call, strictly before token consumption, in both the preview and confirm branches. (2) QA — zero Testcontainers/live-API coverage existed for the two new client methods (`SearchStockItemsPage`, `SearchSalesOrdersPage`), the extended `UpdateCompany` PATCH surface, and the new tool's own guarded workflow, violating the standing rule that every exported client method needs default-on live coverage; added a `company_detail_and_role_completeness` subtest to `internal/inventree/client_methods_integration_test.go` (live field writes/clears, raw-key contract check, and real dependency-count transitions for both new query methods) and a `company_customer_role_removal_happy_path` subtest to `internal/tools/milestone_integration_test.go` (live preview → dependency-blocked-with-no-token-issued → dependency-cleared → confirmed removal → verified read-back), matching the precedent set by F-S24 and F-S32. (3) QA — the ambiguous-mutation-recovery and dependency-audit-error paths in `verifyCompanyRoleRemoval`/`companyCustomerDependencyAudit` had no test coverage; added `TestRemoveCompanyCustomerRoleRecoversAmbiguousMutationErrorAndRejectsDefinite` and `TestRemoveCompanyCustomerRoleFailsClosedWhenDependencyAuditErrors`. A follow-up QA re-verification pass confirmed all three findings closed and independently surfaced the same milestone-coverage gap already fixed in (2). Product and Infosec review passed with no blocking findings; Product's two low-severity observations (the unconditional `is_customer:false` rejection message could be friendlier for the true-no-op case, and `search_stock_items` has no `customer_id` filter for an operator to identify blocking stock items by name) are accepted as-is — both are explicitly documented trade-offs, not gaps in this story's own acceptance criteria.
- Residual risk: derived fields (image URL, supplied/manufactured counts) are recalculated by InvenTree and can change independently; exact reads are factual snapshots. The dependency audit proves a bounded existence count, not identity — an operator blocked by `dependency_stock_items`/`dependency_sales_orders` must use InvenTree directly (or a future `search_stock_items` `customer_id` filter) to find the specific blocking records, consistent with the story's sales/CRM boundary. `internal/inventree` package coverage under the fast unit-test tag dropped slightly (88.2% to 87.3%) because the two new thin `listPage`-wrapping client methods (`SearchStockItemsPage`, `SearchSalesOrdersPage`) are proven only by the new live Testcontainers subtest rather than an additional mocked-HTTP unit test; the underlying generic `listPage` helper they call is already unit-tested via sibling methods, so an additional httptest-only unit test would add coverage percentage without adding real verification, and was judged not worth the risk of it silently drifting from the real endpoint's behavior.

### F-S45: Stock-Item Detail Completeness

- Status: `Done`
- Issue: [#126](https://github.com/davidvanlaatum/inventree-mcp/issues/126)
- Progress: implemented on `codex/f-s45-stock-item-detail-completeness` from `main` at `ccd45e6`.
- Depends on: F-S21, F-S39
- Decisions: approved by the operator on 2026-08-15. Exact stock reads add API-derived SKU, MPN, expired/stale state, and read-only sales-order traceability. Embedded part detail remains a separate `get_part` call and searches stay concise. Raw `barcode_hash` remains excluded pending F-S55.
- Scope: complete `get_stock_item` response coverage without broadening generic stock PATCH into identity, quantity, location, status, provenance, installation, or lifecycle mutation.
- Acceptance:
  - `get_stock_item` exposes SKU, MPN, expired, stale, sales-order reference, and API 530 `location_path` alongside existing quantity, metadata, relationship, pricing, and provenance fields. Nested location and supplier-part detail remain separate exact lookups; tags and tests remain in F-S56 and F-S57.
  - A checked-in pinned field inventory classifies every default stock-item response field as exposed, separate lookup, deferred, write-only, or excluded; raw-key contract tests fail on unclassified omissions or drift.
  - `search_stock_items` retains only high-value selection and recovery fields; expanded part detail remains separate.
  - External stock links follow F-S39; barcode presence and operations remain deferred to the barcode story.
  - Nullable and derived values preserve upstream semantics and sensitive URLs/notes remain absent from errors and recovery projections.
  - Pinned integration and JSON tests plus plan/schema/tool/operator documentation cover the complete exact-read contract, including null, omitted, empty, and populated location-path behavior.
- Validation: `go generate ./internal/tools` (no `docs/tool-manifest.json` diff), `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test -tags no_integration_tests ./...`, `go mod tidy -diff`, and `git diff --check` all pass. `GOFLAGS=-trimpath go test -race -p=1 ./...` passes with every default-on Testcontainers suite against pinned InvenTree 1.5.0, including the new `TestClientMethodsAgainstInvenTree/stock_item_detail` and `TestMilestoneHappyPathToolsAgainstInvenTree/stock_item_detail_completeness` subtests. New fast unit coverage: a "get stock item detail" case in `internal/inventree/read_methods_test.go`'s table-driven `TestReadMethodsUseExpectedEndpoints` (asserts the exact explicit query params sent); `TestStockItemFieldInventoryClassifiesPinnedSerializerExactly` and `TestStockItemDetailPreservesNullableScalarsAndOmitsUnapprovedFields` in `internal/inventree/stock_item_field_inventory_test.go`; `TestStockItemDetailLocationPathHandlesNullOmittedEmptyAndPopulated` covering all four `location_path` JSON states the acceptance criteria calls out. Per-package coverage compared against exact base `origin/main` at `ccd45e6` (`go test -tags no_integration_tests -cover`): `internal/inventree`, `internal/tools`, and `docs` are unchanged at 88.2%, 84.0%, and 57.1% respectively — no package-level reduction.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed (full panel run per the tool-surface-change precedent set by F-S40/F-S43/F-S47). Go and Infosec reviews found no actionable findings. QA found the acceptance criteria's required null/omitted/empty/populated `location_path` coverage was only exercised for the populated case; fixed by adding the JSON-decode unit test above plus a pinned live-Testcontainers case, which also surfaced a genuine live-discovery finding — pinned InvenTree 1.5.0 falls back to the part's `default_location` when `location` is omitted on stock creation rather than leaving the item locationless, so the only reliably reachable null-location state is an explicit `location:null` PATCH; both behaviors are now covered and documented in `docs/api-schema.md`. QA also flagged a harmless unused fixture line in `stock_admin_tools_test.go`, removed. Product found the new `docs/api-schema.md` section made an unverified claim about what upstream setting drives `stale` (conflating it with `expiry_date`/`stocktake_date`), didn't disclose that `stale` (unlike `expired`) is decoded but not asserted by any test, and two minor clarity gaps around the sales-order boundary and the new fields' read-only status; all four addressed with doc wording fixes in `docs/api-schema.md` and `docs/operator-recipes.md`. Focused rerun of the affected areas after fixes found no further actionable findings.
- Residual risk: derived identifiers and stale/expired state can change between reads; they are informational context and must not become mutation preconditions unless explicitly bound by a guarded workflow. `stale`'s exact upstream trigger is instance-configuration-dependent and undocumented in the pinned OpenAPI schema, so this story decodes it without asserting a specific value in tests, unlike `expired`.

### F-S46: Stock Tracking And Stocktake History

- Status: `Done`
- Issue: [#127](https://github.com/davidvanlaatum/inventree-mcp/issues/127)
- Depends on: F-S05, F-S45
- Decisions: approved by the operator on 2026-08-15. Tracking searches require a stock-item or part filter. Historical stocktake reads are part-scoped. Entry/report generation remains a separate future story. Deltas-shape spike (pinned InvenTree 1.5.0/API 530, 2026-08-18): triggered create/add/remove/count/transfer/status-change/PO-receipt tracking events and inspected the raw `/api/stock/track/` payloads. Findings: `deltas` keys and value types vary per event (`{quantity, status}`, `{added, quantity}`, `{removed, quantity}`, `{quantity}`, `{old_status, old_status_logical, status, status_logical}`), and at least two event types (`Location changed`, `Received against Purchase Order`) unconditionally embed full nested `*_detail` records inside `deltas` regardless of top-level `item_detail`/`user_detail`/`part_detail` query flags -- including a `purchaseorder_detail.created_by` object exposing a real user's email and username. An explicit per-event typed union was rejected as unbounded future maintenance (future stories such as F-S52/F-S54/F-S66 will add more event types InvenTree-side without this repository's involvement). The chosen representation is a documented depth/key/byte-bounded opaque JSON object: any key ending in `_detail`, plus the known non-suffixed `created_by` nested-user convention, is stripped recursively from `deltas` before it leaves the client layer (the sibling stable-ID key, e.g. `location`/`purchaseorder`, is always retained alongside the stripped `*_detail`). The redacted result is then bounded to a maximum nesting depth, maximum total key count, and maximum serialized byte size; any deltas payload that still exceeds a bound after redaction, or contains a JSON value type outside object/array/string/number/bool/null, fails the read closed with an error instead of being silently truncated.
- Scope: add list/detail client and MCP tools for stock tracking events and part stocktake history.
- Acceptance:
  - `list_stock_tracking_entries` requires `stock_item_id` or `part_id`, passes that filter server-side, fetches a bounded complete matching snapshot, sorts deterministically by stable event ID, and only then paginates. Concise list results omit full notes; exact detail includes them.
  - `get_stock_tracking_entry` retrieves one exact stable event without exposing unrelated history.
  - API 530 nested item, part, and user detail is projected as stable IDs plus approved safe display fields only; full stock, part, and owner/user records remain separate exact lookups.
  - `list_part_stocktakes` requires a stable part ID, fetches a bounded complete matching snapshot, sorts by stable stocktake ID before pagination, and returns historical snapshots; `get_part_stocktake` retrieves one stable snapshot.
  - Stocktake records expose date, item count, total quantity, minimum/maximum cost, and currencies; tools are read-only and do not generate entries or reports.
  - Before publishing the tool schema, a pinned spike inventories real event-specific `deltas` shapes and selects either an explicit typed union or a documented depth/key/byte-bounded opaque JSON representation; unknown or oversized shapes fail safely instead of being silently truncated.
  - Pinned live integration tests cover filters, ordering/pagination, empty history, exact reads, and audit-note safety; docs and manifest are aligned.
- Validation: `go build ./...`; `go vet ./...`; `golangci-lint run ./...` (0 issues); `go test ./... -short` (all packages pass, including the regenerated `docs/tool-manifest.json` drift test and `TestToolReferenceDocumentsRegisteredLookupTools`); `go generate ./internal/tools/...` to regenerate `docs/tool-manifest.json`; `git diff --check`; pinned live Testcontainers `go test ./internal/inventree/... -run TestClientMethodsAgainstInvenTree -v` (all 21 subtests including the new `stock_tracking_and_stocktake_history` pass against InvenTree 1.5.0/API 530). Rerun clean after every review-driven fix below, including a full `go test ./... -short` pass and a second full live Testcontainers `TestClientMethodsAgainstInvenTree` run (21/21 subtests, ~77s).
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews completed (full panel run per AGENTS.md's tool-surface-change requirement). Go found `get_stock_tracking_entry`/`get_part_stocktake` were missing the mismatched-identity (`record.PK != input.ID`) guard every other exact `get_X` tool in this codebase applies; fixed, with new unit tests (`TestGetStockTrackingEntryRejectsMismatchedIdentity`, `TestGetPartStocktakeRejectsMismatchedIdentity`) proving the guard triggers. QA and Infosec independently flagged the same underlying gap: the `deltas` redaction denylist (`_detail` suffix plus `created_by`) is proven exhaustive against the pinned typed OpenAPI schema but not against `deltas` itself, which is genuinely untyped, so a future/undiscovered event type could embed PII under a differently-named key; fixed by adding a second, content-based redaction layer (`internal/inventree.looksLikeIdentityRecord`) that strips any nested object carrying an `email`/`username`-shaped key regardless of the enclosing key's name, with new unit tests and the residual gap now explicitly documented rather than only implicitly covered by the "shape can vary" sentence. QA also found the pinned raw-create-500 integration assertion only checked "an error occurred" rather than the specific documented failure mode; fixed by asserting the exact `500` status code via `inventree.APIError`. QA flagged missing edge-case unit coverage (non-positive/mismatched IDs, an offset beyond the result count, both `stock_item_id` and `part_id` supplied together); added `TestGetStockTrackingEntryRejectsNonPositiveID`, `TestGetPartStocktakeRejectsNonPositiveID`, `TestListStockTrackingEntriesOffsetBeyondCountReturnsEmpty`, `TestListPartStocktakesOffsetBeyondCountReturnsEmpty`, and `TestListStockTrackingEntriesAndsBothFilters` (proving the two filters are ANDed, not ORed, server-side). QA's fixture-naming nitpick (the empty-history check and the later stock-item-bearing part were the same idempotent fixture record under two different variable names) was cleaned up. Product found two High-severity gaps: the residual "stocktake tools return empty by default on this InvenTree pin" limitation was disclosed only in internal planning docs, never in the tool descriptions, `docs/tool-reference.md`, or `docs/operator-recipes.md` that an operator or driving agent actually sees; and `list_part_stocktakes`/`get_part_stocktake` (InvenTree's periodic `PartStocktake` report snapshots) share the word "stocktake" with the pre-existing, functionally unrelated `stocktake_adjustment` tool (an absolute stock-count correction) with no cross-reference between the two recipe sections. Both fixed with wording added to the tool descriptions, `docs/tool-reference.md`, and a new dedicated bullet in `docs/operator-recipes.md`'s recipe section. Product and QA both separately flagged the `get_part_stocktake` staff-only-403 claim as asserted-as-fact but untestable with this repository's admin-only Testcontainers fixture account; wording softened across `docs/tool-reference.md`, `docs/operator-recipes.md`, and `docs/TASKS.md` to state it is documented from InvenTree's source rather than independently live-verified. One review-driven regression was caught and fixed before it reached CI: the first wording fix for the naming-collision finding literally embedded the write-tool name `stocktake_adjustment` in a read-only tool's registered description string, which broke `TestHTTPHandlerUsesStatelessStreamableServer`'s check that no write-tool name leaks into the tools/list response when write tools are disabled; reworded to describe the other workflow without naming its tool. Focused rerun of the full validation suite after all fixes found no further actionable findings from any of the four reviewers.
- Residual risk: tracking deltas are upstream-structured data whose shape can vary by event type; the MCP must preserve bounded factual content without assuming a single mutation schema. The `deltas` redaction denylist (`_detail` suffix plus `created_by`) is proven exhaustive against every nested-record convention in the pinned typed OpenAPI schema and is backstopped by a second content-based check that strips any nested object carrying an `email`/`username`-shaped key regardless of its enclosing key's name, but neither guarantee formally extends to an undiscovered future event type embedding PII under some other shape; re-spike before any future story (F-S52/F-S54/F-S65/F-S66) publishes a new event type's schema. See `docs/api-schema.md`'s "Verified Stock Tracking And Stocktake Endpoints" for the full denylist-vs-allowlist tradeoff writeup from the 2026-08-18 post-implementation Infosec review. Pinned InvenTree source restricting `get_part_stocktake` to staff/admin accounts is documented from InvenTree's source, not independently live-verified, because this repository's Testcontainers fixture infrastructure only supports a staff/admin account role; a future story adding a non-staff fixture account would let this be pinned live. Pinned InvenTree 1.5.0's raw `POST /api/part/stocktake/` create endpoint 500s on every request (it unconditionally passes an unsupported `user` keyword to a model that declares no such field) and `POST /api/part/stocktake/generate/` only offloads generation to a background worker process this repository's Testcontainers suite does not run; the live integration test therefore cannot populate a real stocktake snapshot and instead pins the empty-history/not-found read paths live plus the create endpoint's exact 500 failure, while the populated-record JSON decode contract is covered by a unit test against the pinned schema shape. A future InvenTree release fixing the create endpoint, or a future story adding a worker container, would allow closing this gap; until then `list_part_stocktakes`/`get_part_stocktake` are only exercisable end-to-end against stocktake data an operator generates through the InvenTree web UI or an external worker deployment.

### F-S47: Purchase-Order And Line Detail Completeness

- Status: `Done`
- Issue: [#128](https://github.com/davidvanlaatum/inventree-mcp/issues/128)
- Depends on: F-S03, F-S23, F-S38, F-S39, F-S61
- Decisions: approved by the operator on 2026-08-15. Exact order and line reads expose all approved fields while searches remain concise. Embedded user, address, contact, project, destination, build, and part records remain separate lookups. Raw `barcode_hash` remains excluded pending F-S55. Existing-order supplier and internal InvenTree reference remain immutable, and `status_custom_key` remains read-only.
- Scope: expand purchase-order and ordinary-line detail projections; add standalone exact-ID order metadata updates and top-level/line external-link update and clear support without broadening supplier identity, internal reference, status, build, project-code, contact, or ownership mutations.
- Acceptance:
  - `get_purchase_order` exposes issue/completion/update dates, line/completed counts, overdue, status text/custom key, supplier name, creator user ID, Markdown notes, and complete ordinary scalar state. API 530 supplier detail remains a separate company lookup; order parameters and tags remain in F-S64 and F-S56.
  - `get_purchase_order_line` exposes SKU, MPN, IPN, internal part name, unit and total price, discount, overdue, build-order ID, and effective `auto_pricing`/`merge_items` flags; API 530's optional nullable total preserves null rather than becoming zero. Nested order, part, and supplier-part detail remains separate exact lookups.
  - Existing purchase-order extra-line reads expose API 530 discount and nullable total price while keeping nested order detail separate.
  - Checked-in order and line field inventories classify every pinned default response field as exposed, separate lookup, deferred, write-only, or excluded; raw-key contract tests fail on unclassified omissions or drift.
  - Standalone `update_purchase_order` supports description, Markdown notes, supplier reference, creation/start/target dates, currency, destination, and external link by exact stable ID, with explicit clearing for every nullable field where pinned serializer behavior permits it.
  - `update_purchase_order_line` adds optional typed external-link and discount fields with omission distinct from explicit clear or zero as applicable, F-S39 validation/redaction, verified API 530 numeric semantics, and exact read-back; existing line mutation behavior remains otherwise unchanged.
  - Existing purchase-order extra-line update supports API 530 discount with the same omission, explicit-zero, numeric, and exact-read-back contract.
  - Supplier and internal InvenTree reference are never accepted by update inputs; searches remain concise.
  - Existing order and line update surfaces retain `inventree.read` plus `inventree.write`, remain closed-world and non-destructive, and preserve their reviewed idempotency annotations in authorization and generated-tool manifests.
  - Build-order ID and custom status remain read-only; lifecycle changes continue through explicit tools, whole-order deletion remains unsupported, and MCP writes continue forcing `auto_pricing:false` and `merge_items:false`.
  - InvenTree 1.5/API 530 pinned integration, JSON contract, recovery/redaction, coverage, and plan/schema/tool/operator documentation are aligned.
- Validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), and `go test ./...` (full suite, including the blocking `TestClientMethodsAgainstInvenTree/po` and `TestMilestoneHappyPathToolsAgainstInvenTree/purchase_order_and_line_detail_completeness` Testcontainers subtests against pinned `inventree/inventree:1.5.0`) all pass, rerun after every review-driven fix below. `go generate ./internal/tools` regenerated `docs/tool-manifest.json`; `TestCheckedToolManifestMatchesGeneratedMetadata`, `TestToolReferenceDocumentsRegisteredWriteTools`, and `TestWriteToolAuthorizationsUseWriteScope` pass against the updated `docs/tool-reference.md`. New fast unit coverage (`TestUpdatePurchaseOrderValidatesAndPatches`, `TestUpdatePurchaseOrderLineRejectsMalformedLink` in `internal/tools/purchasing_tools_test.go`) exercises `update_purchase_order`'s invalid-ID, empty-patch, every value+clear-flag conflict, malformed date, malformed/userinfo `link`, invalid `destination_id`, happy-path patch, notes-clear, and mismatched-identity partial-failure paths without Testcontainers, plus malformed-link rejection for `update_purchase_order_line`.
- Review: full Go, QA, product, and infosec panel completed (required because this story adds a new mutating workflow tool, `update_purchase_order`, per the `docs/reviewers.md` review-timing rule). Findings addressed: (1) Go — `update_purchase_order` was missing the `record.PK != input.ID` post-PATCH identity guard every sibling write tool uses; added, returning `partial_failure` with recovery guidance on mismatch. (2) Go — `updatePurchaseOrderClarification` dropped `retry_values`; now threads `id` through like every sibling clarification helper. (3) Infosec — the pre-existing `delete_purchase_order_line` tool echoed the new unsanitized `Link` field at four output sites because it wasn't in this story's sanitization sweep; fixed by sanitizing the fetched record once, immediately after lookup. (4) QA — `update_purchase_order` had zero fast/mocked unit coverage; added (see Validation). (5) QA — only 1 of 5 nullable `clear_*` fields was exercised against live InvenTree; extended the Testcontainers `po` subtest to cover `clear_start_date`/`clear_target_date`/`clear_destination` together, which is what caught finding (7). (6) QA — extra-line `total_price` null-preservation before any price is set was untested; added a dedicated unpriced-extra-line create/read/delete check to the same subtest. (7) **Live-testing discovery**: pinned InvenTree 1.5.0 does not clear `creation_date` on an explicit null PATCH — it resets the field to the current date instead, unlike `start_date`/`target_date`/`destination`, which do clear correctly. `clear_creation_date` was removed from `UpdatePurchaseOrderInput` entirely (the field can still be *set*, just never cleared through this tool) per the acceptance criteria's own "where pinned serializer behavior permits it" hedge; the quirk itself is now pinned by a dedicated assertion in `TestClientMethodsAgainstInvenTree/po`. (8) Product — `docs/tool-reference.md` and `docs/operator-recipes.md` didn't explain that `link` clears via an explicit empty string rather than a `clear_link` flag; both now say so explicitly, alongside the `creation_date` exception from finding (7). Earlier implementation-time findings, also addressed before review: the pre-existing unused `PurchaseOrderLineItem.SupplierPart` dead field was removed (it would have broken the new field-inventory drift test); the order-level `link` field was corrected from `*string` to plain `string` after live evidence showed InvenTree's schema models it as string-or-empty rather than nullable; `merge_items` is classified `write_only` and never returned by any read because its schema is `writeOnly:true`, despite the acceptance criteria's "effective `auto_pricing`/`merge_items` flags" wording; and `link`/`discount` sanitization was swept across every `PurchaseOrderLineItem` return site (search, add, update, workflow create/update, issue, receive, complete, and now delete).
- Residual risk: several derived fields are recalculated by InvenTree and can change independently; exact reads are factual snapshots and must not imply atomic order-wide consistency. `merge_items`'s absence from every read (rather than an "effective" value) is an InvenTree schema constraint, not an MCP omission. `creation_date` can be set but never cleared through `update_purchase_order`, an accepted InvenTree 1.5.0 serializer limitation rather than an MCP gap.

### F-S48: Owner Discovery And Cross-Object Responsibility

- Status: `Done`
- Issue: [#129](https://github.com/davidvanlaatum/inventree-mcp/issues/129)
- Depends on: F-S40, F-S44, F-S47
- Decisions: approved by the operator on 2026-08-15. Owner references may represent users or groups; opaque IDs without discovery are not sufficient. The story inventories already supported, non-sales object types before finalizing assignments; any newly discovered object domain requires a separate operator checkpoint. Approved by the operator on 2026-08-18: the object matrix is Part (`responsible`), PurchaseOrder (`responsible`), StockItem (`owner`), and StockLocation (`owner`); Company, PartCategory, PurchaseOrderLine, Build, ProjectCode, ReturnOrder, SalesOrder, and TransferOrder are excluded (no owner/responsible field, sales-adjacent, or not yet supported). `update_stock_location`'s existing ordinary, unguarded `owner_id`/`clear_owner` fields move to the new guarded destructive replace/clear tool alongside the other three objects; `create_stock_location` keeps its ordinary `owner_id` for initial assignment since there is no prior owner to replace.
- Scope: add owner search/exact reads and guarded assignment/clearing of responsibility fields across verified already-supported, non-sales objects.
- Acceptance:
  - Inventory schema and pinned responses for every user/group owner and responsibility field before fixing the object matrix.
  - Add bounded `search_owners` and exact `get_owner` tools with stable identity, type, and safe display fields; search requires a narrowing text query or supported object context and excludes email, phone, address, and tax identifiers.
  - Expose stable responsible-owner IDs in exact object reads and allow validated assignment/clearing where supported.
  - Replacement/clear behavior uses a state-bound plan token covering object and current owner, explicit confirmation, stale-plan refusal, exact read-back, and bounded privacy-safe recovery output.
  - Discovery reads require `inventree.read`; assignment requires `inventree.read` and `inventree.write`, remains closed-world and non-idempotent, and replacement/clear additionally requires `inventree.destructive` with `destructiveHint:true`.
  - Tests cover user/group distinction, missing/disabled identities, permissions, cross-object consistency, and aligned public docs.
- Validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `gofmt -l .` (clean), `go generate ./internal/tools` (manifest regenerated and checked in), and `git diff --check` all pass. `go test -p=1 ./...` (full suite, `count=1`, every default-on Docker/Testcontainers subtest included) passes against pinned `inventree/inventree:1.5.0`, including the new `TestMilestoneHappyPathToolsAgainstInvenTree/owner_discovery_and_cross_object_responsibility_assignment` subtest (with `part`/`purchase_order`/`stock_item`/`stock_location` sub-cases each exercising a live preview → confirm assign → read-back → preview → confirm clear → read-back cycle through `assign_owner`, plus live `search_owners` by query, by `object_type` bound, and with `is_active:true`, and `get_owner`) and the updated `stock_location_and_metadata_administration` subtest (now creates its location with a real live-discovered owner and exercises the guarded clear path). New fast unit coverage in `internal/tools/owner_tools_test.go` (14 tests, fake-client-backed: object-type/ID validation, not-found, no-op assign/clear rejection, unresolvable `owner_id`, a cross-object-matrix table test covering all 4 object types' preview/confirm/verify/clear cycle and their distinct underlying PATCH field name (`responsible` vs `owner`), stale/reused/cross-principal plan-token rejection with same-token reuse-once-state-matches-again proof, partial-failure read-back verification, definite-mutation-rejection, `is_active` pass-through, and user/group `type` filtering) and `internal/inventree/read_methods_test.go` (new `SearchOwnersPage`/`GetOwner` table cases). Per-package coverage under `-tags no_integration_tests`: `internal/tools` 84.2% (baseline 84.2%, unchanged); `internal/inventree` 85.4% (baseline 85.2%, improved).
- Review: full Go, QA, product, and infosec panel completed (required: new mutating destructive tool-surface change, Testcontainers coverage, and a breaking change to an existing tool's contract). Findings addressed: (1) Go (medium) — the four parallel per-object-type structures (`ownerObjectTypes`/`ownerFieldByObjectType` maps plus two duplicated `switch` statements) were a shotgun-surgery risk for future object types; collapsed into a single `ownerObjectDescriptors` map keyed by object type, each entry owning its field name and current/patch closures. (2) QA (high) — `assign_owner`'s replace (non-clear) path for `stock_location` had no live Testcontainers coverage (only the clear path was exercised live, in the separate `stock_location_and_metadata_administration` subtest); added `stock_location` to the shared cross-object-matrix live subtest so all four object types now exercise the identical live assign-then-clear cycle. (3) QA (medium) — `is_active` had no assertion of tool-layer pass-through and no live exercise; added `TestSearchOwnersPassesIsActiveFilterToClient` (fake-client) and a live `search_owners` call with `is_active:true` in the milestone subtest. InvenTree's `Owner` serializer carries no per-record active/disabled field (only the list-level `is_active` query filter exists — confirmed against the pinned `docs/api-schema.yaml` schema), so a genuinely disabled identity's per-record visibility cannot be independently proven without provisioning a disabled fixture user/group; accepted as residual risk below. (4) Product (high) — `docs/PLAN.md` claimed "F-S48 is complete on `main`" while this was still an unmerged, `Active`-status branch; reworded to drop the premature completion claim. (5) Product (low) — `docs/operator-recipes.md`'s object-matrix exclusion summary omitted `TransferOrder` by name (the canonical decision text in this section and in `docs/PLAN.md` already listed it); added. (6) Infosec (informational) — `search_owners`'s `object_type` bound only satisfies the "non-empty request" rule and does not itself restrict which owners are visible, since InvenTree's `/api/user/owner/` endpoint imposes no additional permission beyond ordinary read access for the authenticated principal; made this explicit in `docs/PLAN.md` rather than leaving it only as a Go doc comment. QA also noted (accepted as-is, matching pre-existing repo-wide test patterns rather than an owner-specific gap): no live coverage of a real InvenTree `group`-type owner (the user/group distinction is proven live only for the `user` case; the `group` projection path is proven by the fake-client unit test), and no permission-denied (`403`) scenario for any of the three new tools (only 2 other tool files in the whole repo test `403` anywhere). Go, QA, product, and infosec confirmed no further blocking findings after the fixes above.
- Residual risk: owner identifiers and visibility can depend on the authenticated user's permissions; completeness claims must be scoped to records visible to the current InvenTree principal. InvenTree's `Owner` model exposes no per-record active/disabled flag, only a list-level `is_active` query filter, so "disabled identity" acceptance-criterion coverage is necessarily limited to proving that filter reaches the live API rather than independently verifying one specific disabled user/group's visibility. The shared Testcontainers suite has no fixture-provisioned InvenTree group, so the live "user vs group" distinction is proven end-to-end only for the user case. Preview-then-confirm remains two non-atomic upstream REST calls like every other guarded plan-token tool in this codebase: the state-bound token detects a *stale* concurrent change (returns clarification) rather than preventing one atomically, so operators should still coordinate a single writer per object during confirmation. Permission-denied (`403`) behavior for `search_owners`/`get_owner`/`assign_owner` is not independently tested, consistent with the existing repo-wide pattern.

### F-S49: Structured Contact And Address References

- Status: `Done`
- Issue: [#130](https://github.com/davidvanlaatum/inventree-mcp/issues/130)
- Depends on: F-S44, F-S47
- Decisions: approved by the operator on 2026-08-15. Structured contacts/addresses are separate from free-text company contact fields and require discovery before assignment. Implementation scoping assumption (no blocking questions raised): the API 530 schema only exposes structured `contact`/`address` FK references on PurchaseOrder among already-supported objects — ReturnOrder, SalesOrder, and TransferOrder carry the same fields but remain unimplemented/sales-adjacent and out of scope, and Company itself has no writable contact/address FK (only its free-text `contact` string and computed read-only `primary_address`). `assign_contact`/`assign_address` are therefore purchase-order-specific tools rather than a generic multi-object-type matrix; a future story can widen them if another in-scope object gains a contact/address FK. Second scoping assumption (no blocking questions raised): the acceptance bullet below reads "selection results exclude email, phone, street address, and tax identifiers," which could be read as scoping the exclusion to search/disambiguation candidates only; this implementation applies the same exclusion to `get_contact`/`get_address` exact single-record reads too, so there is no MCP path to retrieve those fields for one already-known record either. This follows F-S48's `search_owners`/`get_owner` precedent (which excludes "email, phone, address, or tax identifiers" from both search and exact reads identically) rather than inventing a narrower policy; company-level phone/email remain reachable through `get_company`/`update_company` for general outreach.
- Scope: inventory contact/address endpoints and references on already supported, non-sales objects, add bounded lookup tools, and support guarded assignment/clearing where verified. Any newly discovered object domain requires a separate operator checkpoint.
- Acceptance:
  - Document the stable identity, company ownership, visibility, and lifecycle semantics of contact and address records.
  - Add company-scoped bounded search and exact-read tools returning privacy-conscious projections; selection results exclude email, phone, street address, and tax identifiers unless a separately reviewed disambiguation need is approved.
  - Purchase-order and other verified object assignments validate that selected contacts/addresses belong to the expected company context.
  - Replacement and clearing use a state-bound plan token covering current references and company context, explicit confirmation, stale-plan refusal, exact read-back, and safe recovery; creation/deletion of contact/address records is excluded until separately approved.
  - Discovery reads require `inventree.read`; assignment requires `inventree.read` and `inventree.write`, remains closed-world and non-idempotent, and replacement/clear additionally requires `inventree.destructive` with `destructiveHint:true`.
  - Pinned integration tests cover company mismatch, missing records, nullable clears, permissions, and aligned docs.
- Validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test ./...` (full suite, including the live Testcontainers `structured_contact_and_address_references` subtest against InvenTree 1.5.0/API 530). Per-package coverage held or improved versus `main`: `internal/inventree` 92.4%→92.6%, `internal/tools` 85.7%→85.7%.
- Review: full Go/QA/Product/Infosec subagent panel per `docs/reviewers.md` (new mutating workflow tools). No blocking findings. Addressed: added `get_contact`/`get_address` PK-identity-mismatch guards matching the rest of the lookup surface (Infosec); added an explicit unit test isolating the plan token's `CompanyID` binding from its `CurrentContactID`/`CurrentAddressID` binding (Go); documented the exact-read privacy-exclusion scope as a second explicit assumption in this story's Decisions, parallel to the object-type one (Product, medium); corrected `docs/PLAN.md`'s stale `create_contact`/`create_address` milestone-1 listing to point at this story's deliberate deferral (Product). Not addressed, left as-is: generic `planStore[T]` deduplication across `owner_tools.go`/`contact_tools.go`/`address_tools.go` (Go/QA, low, pre-existing repo-wide pattern, not specific to this story); explicit cross-principal plan-store test coverage (QA, low, pre-existing gap inherited from `owner_tools_test.go`, not a regression).
- Residual risk: contact and address records contain personal/business data and can change independently; errors and recovery projections must remain minimal and assignments cannot guarantee the referenced details stay unchanged. `get_contact`/`get_address` permanently omit phone/email and street-address/postal-code from every projection, including exact single-record reads by known ID — an operator who needs those specific fields for one legitimate lookup has no MCP path to them and must use the InvenTree UI directly.

### F-S50: Project-Code Discovery And Assignment

- Status: `Done`
- Issue: [#131](https://github.com/davidvanlaatum/inventree-mcp/issues/131)
- Depends on: F-S23, F-S47
- Decisions: approved by the operator on 2026-08-15. Project codes move from deliberate exclusion into a separate lookup-and-assignment story rather than opaque numeric inputs. Implementation scoping assumption (no blocking questions raised): the in-scope object matrix is `PurchaseOrder`, `PurchaseOrderLineItem`, and `PurchaseOrderExtraLine` only, matching the story text's explicit "purchase orders, ordinary lines, extra lines." `Build` and `TransferOrder` also carry `project_code` in the API 530 schema but are not implemented as MCP objects (`Build`/build-order workflows are `F-S04`, `Blocked`); `ReturnOrder`/`SalesOrder` are sales-adjacent and excluded per `AGENTS.md`'s standing sales-workflow exclusion and F-S48/F-S49's identical precedent. `assign_project_code` is therefore a three-object-type guarded tool rather than a wider matrix; a future story can widen it if another in-scope object gains a `project_code` FK.
- Scope: add project-code lookup tools and validated assignment/clearing on purchase orders, ordinary lines, extra lines, and other verified already-supported, non-sales objects. Any newly discovered object domain requires a separate operator checkpoint.
- Acceptance:
  - Inventory project-code schema, permissions, active/state behavior, and every applicable object reference.
  - Add bounded `search_project_codes` and exact `get_project_code` tools using stable IDs and high-value fields.
  - Expose project-code ID/label in exact reads and support validated assignment/clearing on verified purchase-order surfaces using a state-bound plan token, explicit confirmation, and stale-plan refusal for replacement/clear.
  - Discovery reads require `inventree.read`; assignment requires `inventree.read` and `inventree.write`, remains closed-world and non-idempotent, and replacement/clear additionally requires `inventree.destructive` with `destructiveHint:true`.
  - Combined purchase-order workflows preserve project-code values in dry-run plans, hashes, read-back, and partial-failure recovery where in scope.
  - Creation/deletion of project-code records remains excluded unless separately approved; pinned tests and all public docs are aligned.
- Validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test ./...` (full suite, `-v`, including the live Testcontainers `project_code_discovery_and_assignment` milestone subtest against InvenTree 1.5.0/API 530, and `go test ./internal/tools/... -race` clean). Per-package coverage held or improved versus `main`: `internal/inventree` 92.6%→92.7%, `internal/tools` 85.7%→85.7%.
- Review: full Go/QA/Product/Infosec subagent panel per `docs/reviewers.md` (new mutating tool surface). Go and Infosec independently flagged that the `assign_project_code` purchase-order-line PATCH workaround (re-supplying `part`/`order`/`quantity` to satisfy a pinned InvenTree 1.5.0 validation quirk) unnecessarily re-supplied `quantity`, opening a narrow fetch-then-patch race that could clobber a concurrent quantity edit undetected by verification; a live Testcontainers probe confirmed `quantity` is not actually required by the endpoint, so it was dropped from the resupply (code, test, and docs updated together) rather than merely documented as accepted risk. Product found one documentation asymmetry (fixed: `update_purchase_order_line`'s tool-reference row now states `project_code` is not a PATCH field there). QA found two worthwhile low-cost test-coverage gaps (fixed: added `TestGetProjectCodeRejectsMismatchedIdentity` and `TestPurchaseOrderLineInputsExcludeProjectCode`) and flagged a pre-existing gap predating this story — `PurchaseOrderExtraLine` has no exhaustive schema-drift field inventory the way `PurchaseOrder`/`PurchaseOrderLineItem`/`Contact`/`ProjectCode` do — spun off as a separate follow-up rather than expanded into this story's scope. QA's initial "docs contradict code" finding reflected files read mid-edit during a long-running review pass, not a real inconsistency; re-verified consistent after the fact. No other blocking findings from any reviewer.
- Residual risk: project-code availability and accounting semantics may be installation-specific; assignment verifies identity and API acceptance but cannot validate external accounting policy. The `assign_project_code` line-object patch re-fetches the line and re-supplies its current `part`/`order` immediately before PATCHing; a legitimate concurrent edit to those two fields in that narrow window is a low-probability, low-impact residual race (accepted, matching the equivalent pre-existing pattern in `update_purchase_order_line`/`purchasing_tools.go`'s own part/order resupply). `PurchaseOrderExtraLine` still has no schema-drift field inventory (pre-existing gap, tracked separately).

### F-S51: Guarded Delete-On-Deplete Policy Updates

- Status: `Done`
- Issue: [#132](https://github.com/davidvanlaatum/inventree-mcp/issues/132)
- Depends on: F-S24, F-S45
- Decisions: approved by the operator on 2026-08-15. `delete_on_deplete` is not ordinary metadata because it controls future record deletion; changes require a dedicated reviewed workflow.
- Branch: `codex/f-s51-guarded-delete-on-deplete-policy`.
- Scope: add dry-run/current-state planning, confirmation, PATCH, and exact read-back for one stock item's delete-on-deplete policy.
- Acceptance:
  - [x] Dry run returns the exact stock identity, quantity, allocations, relationships, current/effective policy, planned change, and a current-state plan token.
  - [x] Execution requires explicit confirmation and matching plan token, validates the same stable item, and verifies the resulting boolean exactly.
  - [x] The workflow requires `inventree.read`, `inventree.write`, `inventree.operational`, and `inventree.destructive`, remains closed-world and non-idempotent, and publishes `destructiveHint:true`.
  - [x] No-op requests are factual and do not mutate; ambiguous PATCH results use read-back before retry guidance.
  - [x] Pinned integration tests establish behavior for positive and zero quantities and interaction with the existing guarded depletion workflow.
  - [x] Tool annotations/scopes, plan/schema/tool/operator docs, and coverage/review evidence are aligned.
- Validation: `go build ./...`, `go vet ./...`, `gofmt -l .` clean; `golangci-lint run ./...` reported 0 issues; `go test ./... -short` passed for every package. `go test ./internal/inventree/... -run TestClientMethodsAgainstInvenTree -v` (real Docker/Testcontainers InvenTree 1.5.0) passed, including the new `stock_delete_on_deplete_policy` subtest covering positive-quantity enable/disable, disable-then-full-depletion survival, enable-at-zero-quantity non-deletion, and restock-then-depletion-after-toggle deletion. Per-package coverage compared against `main` via `go test ./internal/tools/... ./internal/inventree/... -short -coverprofile=... -covermode=atomic`: `internal/tools` 84.2%→84.2%, `internal/inventree` 85.8%→85.8% — no reduction.
- Review: full Go/QA/Product/Infosec panel per AGENTS.md (new destructive tool-surface change). Senior Go Developer found the no-op clarification's `retry` field pointed at `stock_item_id` instead of `delete_on_deplete` (inconsistent with the sibling `set_stock_status`/`stocktake_adjustment` no-op convention); fixed, with a new assertion added. Senior QA / Test Architect found no actionable issues; confirmed every acceptance bullet is covered by an executable test and that `delete_on_deplete` remains excluded from `update_stock_item_metadata`'s patchable fields. Senior Product Manager found `docs/operator-recipes.md` was not updated to surface the tool or the `STOCK_DELETE_ON_DEPLETE` default-true discovery to operators; fixed with a `set_stock_delete_on_deplete` flow bullet in "Review And Apply A Stocktake Adjustment" and a pointer from "Create Initial Stock". Senior Infosec Reviewer found no actionable issues; confirmed the OAuth scope guard covers the new tool through the existing generic `ToolAuthorizations` mechanism with no bypass path, the `StockAdjustmentClient` interface widening leaks no new capability to other callers, and no credential/upload/URL-fetch logic was touched. Focused rerun after the two fixes: unit tests, `go build`/`vet`/`gofmt`/lint, and `git diff --check` on docs all passed.
- Residual risk: enabling the flag authorizes future InvenTree quantity operations to delete the record at depletion; this story can verify the policy change but cannot bind or predict every later upstream mutation. Live Testcontainers verification against pinned InvenTree 1.5.0 found the global `STOCK_DELETE_ON_DEPLETE` setting defaults to enabled, so stock created via `create_stock_item`/`create_initial_stock_entry` (neither exposes this field) already carries `delete_on_deplete:true` unless an operator explicitly disables it with this tool; documented in `docs/api-schema.md` and `docs/operator-recipes.md`, but no MCP-level warning is surfaced at creation time itself.

### F-S52: Stock Serial-Number Management

- Status: `Done`
- Branch: `claude/f-s52-stock-serial-number-management`.
- Issue: [#133](https://github.com/davidvanlaatum/inventree-mcp/issues/133)
- Depends on: F-S45
- Decisions: approved by the operator on 2026-08-15. Serial changes affect stock identity and trackability and therefore remain outside ordinary metadata PATCH.
- Scope: establish serial-number discovery, uniqueness, assignment, replacement, and clearing semantics for existing stock items.
- Acceptance:
  - Verify pinned InvenTree serial-number endpoint behavior, part trackability requirements, uniqueness scope, allowed formats, and serialized quantity constraints.
  - Add the required part-scoped serial discovery/availability tools with bounded outputs.
  - Guard assignment, replacement, and clearing with stable-ID current-state plans, confirmation where identity changes, and exact read-back.
  - Discovery requires `inventree.read`; assignment requires `inventree.read`, `inventree.write`, and `inventree.operational`, remains closed-world and non-idempotent. Replacement/clear additionally requires `inventree.destructive` and publishes `destructiveHint:true`.
  - Refuse unsupported quantity, allocation, parent/child, build, consumption, installation, or provenance states rather than bypassing native workflow constraints.
  - Cover unknown-result recovery, concurrency, privacy-safe errors, Testcontainers behavior, and aligned public docs.
- Implementation notes: `search_stock_serials`/`get_part_next_serial` (read) and `assign_stock_serial`/`set_stock_serial` (guarded write) land in `internal/tools/lookup_tools.go` and the new `internal/tools/stock_serial_admin_tools.go`, reusing the existing `stockPlanStore`/`executeStockPlan` guarded-plan framework shared with `set_stock_delete_on_deplete`/`deplete_stock_item`/`transfer_stock_item`. Both mutation tools share one relationship-safety guard (`unsafeStockSerialChange`) refusing allocated, building, consumed, installed, parent-linked, child-bearing, customer-assigned, or sales-order-linked stock. Bulk-splitting a multi-quantity item into several serialized items via InvenTree's native `POST /api/stock/{id}/serialize/` is explicitly out of scope -- this was an implementation-time scoping assumption (the story's own "existing stock items" wording), not a separately operator-approved Decision; flagging for operator awareness in case bulk-serialize-on-receipt turns out to be a common enough workflow to warrant a future story.
- Validation: `go build ./...`; `go vet ./...`; `golangci-lint run ./...` (0 issues); `go generate ./internal/tools` (no drift); `git diff --check`; `GOFLAGS=-trimpath go test -race ./... -count=1` (all packages, including the live Testcontainers `stock_serial_management` subtest against `inventree/inventree:1.5.1`) passed. Per-package coverage vs `origin/main`: `internal/tools` 85.7%→85.5%, `internal/inventree` 92.6%→92.5%; both reductions are sub-0.2pp from added defensive branches (e.g. `stockSerialCollision`'s hard-fail error paths) and are accepted rather than chased further.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed (full panel, per the "new mutating workflow tools" trigger in `docs/reviewers.md`). Go found a High-severity gap (`assign_stock_serial` had no relationship-safety guard at all) and Medium findings (missing `Customer`/`SalesOrder` checks; the global-uniqueness setting lookup swallowed every error type with no logging instead of following `GetGlobalSetting`'s own `IsOmittableFetchError` contract; missing part-identity mismatch guard) -- all fixed. Infosec independently flagged the same global-uniqueness setting-lookup issue as Medium; confirmed fixed and confirmed OAuth scope wiring, annotation wiring, and error redaction were otherwise correct. QA flagged High-severity test-coverage gaps (`unsafeStockSerialChange` only had one of ten branches tested; the live Testcontainers subtest never exercised `serial_gte`/`serial_lte` or proved real server-side duplicate-serial rejection) and Medium findings (a factually wrong doc comment in `internal/inventree/stock_serial.go` claiming InvenTree rejects the non-trackable-part endpoint, contradicted by the test's own verified finding; missing stale/reused-plan tests) -- all addressed with a full branch-coverage table test, extended live coverage, a corrected doc comment, and new stale/superseded-plan tests (reuse-after-successful-execution could not be tested the same way sibling tools do, since assign/set's own preconditions mask token-reuse once the item's serial state changes; documented in the test comments). Product found no blocking issues and one Moderate finding (the bulk-serialize-endpoint exclusion should have been recorded as an explicit Decision) addressed via the Implementation notes above rather than a retroactively-fabricated operator approval.
- Residual risk: serial identity may be referenced by labels, external systems, or unexposed history; MCP verification can prove InvenTree state but not downstream synchronization. Bulk-serializing multi-quantity stock via InvenTree's native `/api/stock/{id}/serialize/` remains unsupported by this story (see Implementation notes); an operator wanting that workflow must use the InvenTree UI/API directly or request a future story. The `SERIAL_NUMBER_GLOBALLY_UNIQUE` cross-part duplicate scan is skipped (with a logged warning) when the staff-only settings endpoint is unreachable for the calling credential; InvenTree's own write-time validation remains authoritative for that scope regardless, and pinned live testing confirmed InvenTree does reject same-part duplicates at write time.

### F-S53: Guarded Stock Provenance Correction

- Status: `Done`
- Issue: [#134](https://github.com/davidvanlaatum/inventree-mcp/issues/134)
- Depends on: F-S43, F-S45, F-S47
- Decisions: approved by the operator on 2026-08-15. Supplier part, purchase order, purchase price, and currency corrections require a dedicated guarded story; stock base-part reassignment remains unsupported. Confirmation uses the established principal-bound, five-minute, single-use stock plan token. The native PATCH changes current provenance but does not create a stock-tracking event; the operator-facing boundary is documented rather than implying a new audit record.
- Scope: validate and correct eligible stock provenance fields with current-state plans, cross-record consistency, confirmation, and exact recovery.
- Acceptance:
  - Dry run binds stock identity, base part, supplier part, purchase order, price/currency, quantity, serial, allocation, and relevant relationships.
  - Supplier-part corrections match the stock base part and selected order supplier; purchase-order and pricing changes satisfy pinned upstream constraints.
  - Execution requires confirmation and a matching opaque principal-bound five-minute single-use current-state token, supports explicit nullable clears only where proven safe, and verifies exact read-back.
  - The workflow requires `inventree.read`, `inventree.write`, `inventree.operational`, and `inventree.destructive`, remains closed-world and non-idempotent, and publishes `destructiveHint:true` because it rewrites provenance/audit context.
  - Base `part_id`, location, quantity, status, build/consumption, installation, customer, and sales provenance remain outside this tool.
  - Pinned integration tests cover mismatches, clears, definite/ambiguous failures, concurrent changes, audit implications, and aligned docs.
- Validation: focused provenance unit tests cover supplier/base-part and supplier/order mismatches, decimal normalization including leading-integer and signed-zero forms plus invalid inputs, explicit nullable clears, stale plans, definite rejection, ambiguous response recovery, read-back mismatch, token principal binding and single-use replay rejection; the pinned `stock_provenance_correction` Testcontainers subtest passes against InvenTree 1.5.1/API 530 and verifies isolated fixtures, stale confirmation, correction, clears, unchanged tracking-event count, and response-loss recovery. Compile-only package tests, focused package tests, and `git diff --check` pass. `internal/tools` short coverage is flat at 84.2% on the feature branch versus 84.2% on clean `main`; no package-level reduction requires recovery.
- Review: Senior QA / Test Architect review completed during implementation; no unresolved actionable findings remain. The review covered current-state binding, supplier/order mismatch, clears, decimal normalization, stale/concurrent changes, definite and ambiguous failures, audit-history implications, fixture isolation, and package coverage.
- Residual risk: provenance corrections change audit context after stock creation and may not rewrite historical tracking entries; outputs and documentation make that boundary explicit. Preflight and mutation remain separate upstream requests, so concurrent writers must still be coordinated and any partial failure requires a fresh exact read before retry.

### F-S54: Stock Install And Uninstall Workflows

- Status: `Done`
- Branch: `claude/f-s54-stock-install-uninstall-workflows`.
- Issue: [#135](https://github.com/davidvanlaatum/inventree-mcp/issues/135)
- Depends on: F-S45
- Decisions: approved by the operator on 2026-08-15. `belongs_to` and installed-item relationships remain read-only until handled by explicit install/uninstall workflows; build, consumption, customer, and sales provenance remain lifecycle-owned.
- Scope: establish and expose safe installation and removal of stock-item parent/child relationships without generic PATCH.
- Acceptance:
  - Pin InvenTree install/uninstall endpoints or supported mutations, relationship direction, quantity/serialization constraints, and tracking events.
  - Dry runs resolve exact parent and child stock records, reject self/cycles and incompatible states using deterministic traversal with shared request/record budgets, and fail closed when complete ancestor/descendant validation cannot be proven within bounds.
  - Dry run returns a token bound to both complete item states and their current relationship; confirmed execution requires the matching token, rejects stale plans, verifies both sides, and safely recovers ambiguous outcomes. Uninstall confirms the exact existing relationship.
  - Install requires `inventree.read`, `inventree.write`, and `inventree.operational`, remains closed-world, non-destructive, and non-idempotent. Uninstall additionally requires `inventree.destructive` and publishes `destructiveHint:true`.
  - Location, ownership, allocation, build/consumption, and provenance side effects are explicitly validated and documented.
  - Testcontainers coverage exercises install, refusal, uninstall, and history behavior; tool scopes, docs, and coverage/review evidence are aligned.
- Implementation notes: `install_stock_item`/`uninstall_stock_item` land in the new `internal/tools/stock_install_tools.go`, reusing the shared `stockPlanStore`/`StockAdjustmentPlan`/`executeStockPlan`-family framework from `internal/tools/stock_adjustment_tools.go` (registered inside `registerStockAdjustmentTools` itself, alongside `transfer_stock_item`/`assign_stock_serial`/`set_stock_serial`, so all eight stock-mutation tools share one plan store and one per-principal capacity cap) with a new `StockInstallContext` plan side-channel, following `transfer_stock_item`'s precedent of a bespoke `execute*`/`verify*` pair rather than the generic single-item `stockStateMatches` comparison. `internal/inventree.Client.InstallStockItem`/`UninstallStockItem` and their request structs land in `write_methods.go`. Endpoint behavior was pinned against a live InvenTree 1.5.1 instance before locking in the design (a Testcontainers spike, folded into the permanent `stock_install_uninstall` subtest in `internal/inventree/client_methods_integration_test.go`): `install`'s `{id}` URL parameter is the parent (body `stock_item` is the child) while `uninstall`'s `{id}` is the child itself with no separate parent/child field in the body; the 201 response on both endpoints only echoes the request, never resulting state, so both client methods return only `error` and callers must re-fetch; InvenTree enforces BOM membership and availability (quantity not exceeding the child's own available quantity) server-side, but does **not** enforce the endpoint's own docstring claim that the child must be serialized -- an ordinary unserialized quantity-1 item installs successfully -- so this tool intentionally requires quantity exactly 1 but not a serial, an implementation-time scoping call rather than a separately operator-approved Decision, flagged here for operator awareness; InvenTree also does **not** reject uninstalling an item with no current `belongs_to` (it silently accepts the request), so the `belongs_to != nil` precondition in `uninstallStockItem` is a genuinely load-bearing MCP-side safety check, not decorative; and InvenTree records native stock-tracking history on both sides (child: type 30 "Installed into assembly" / type 31 "Removed from assembly"; parent: type 35 "Installed component item"), pinned by dedicated tracking-history assertions in the same subtest. Nested assemblies are explicitly supported -- a parent already installed inside something else, a parent that already has other installed items, and a child that itself already contains further nested installed items are all allowed -- so `unsafeStockInstallChild`/`unsafeStockInstallParent` deliberately do not block on `belongs_to`/`installed_items`, unlike sibling guards; cycle prevention instead uses a bounded ancestor-walk (`stockInstallCycleCheck`, 64-record shared budget matching `part_family_tools.go`'s convention) that fails closed on budget exhaustion, an already-visited ancestor, a missing ancestor, or any other read error along the chain. Partial-quantity install (splitting a bulk item so only part of it installs) remains out of scope, matching `transfer_stock_item`'s complete-quantity-only precedent -- another implementation-time scoping call, not a separately operator-approved Decision.
- Validation: `go build ./...`; `go vet ./...`; `golangci-lint run ./...` (0 issues); `go generate ./internal/tools` (no drift); `git diff --check`; `GOFLAGS=-trimpath go test -race ./... -count=1` (all packages, including the live Testcontainers `stock_install_uninstall` subtest against `inventree/inventree:1.5.1`) passed. Per-package coverage vs `origin/main`: `internal/tools` 85.9%→86.1%, `internal/inventree` 92.6%→92.7%; both improved, no reduction.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed (full panel, per the "new mutating workflow tools" trigger in `docs/reviewers.md`). Go, QA, and Infosec independently found the same High-severity bug: `stockInstallCycleCheck` treated any non-`NotFound` ancestor-lookup error (a transient 5xx, network failure, etc.) as "no cycle found" and let the install proceed, contradicting the story's own "fail closed" acceptance language and leaving the client-side traversal -- the sole control against a circular `belongs_to` relationship, since InvenTree does not itself enforce one -- bypassable on an error path; fixed by failing closed on every non-context-cancellation error, with a new regression test. Go additionally found a Medium-severity bug: `install_stock_item`/`uninstall_stock_item` were registered through a separate `registerStockInstallTools` function that created its own independent `stockPlanStore` instead of sharing the one `registerStockAdjustmentTools` creates, silently doubling the intended per-principal outstanding-plan cap; fixed by folding both tools' registration into `registerStockAdjustmentTools` and deleting the separate registrar. QA found High-severity gaps in Testcontainers coverage of the story's own "tracking events"/"history behavior" acceptance bullet (not pinned at all) and several missing unit-test branches (context-cancellation passthrough on four call sites, uninstall's missing-confirm/token-replay paths, an ancestor chain with a pre-existing cycle not involving the child, and a missing ancestor mid-walk) -- all addressed with new live-subtest tracking assertions and unit tests. Product found the tracking-events gap independently, flagged that three implementation-time scoping calls (no-serial-required, no-partial-quantity, and the child-only-keyed confirmation token) were narrated in code/doc comments but not folded into the story's `Decisions` field, and suggested `verifyStockInstall` should independently re-verify the parent's side of the relationship rather than only the child's -- addressed by documenting the scoping calls explicitly in these Implementation notes (matching F-S52's precedent for an unapproved scoping assumption) rather than fabricating retroactive operator approval, and by extending `verifyStockInstall` to re-fetch the parent and confirm its `installed_items` count incremented. Infosec confirmed OAuth scope-to-tool mapping, token/plan-store principal-binding and cross-tool replay resistance (the digest binds `Action` plus both stock items' complete state), and error redaction were all correct, and concurred with the cycle-check fail-open finding.
- Residual risk: physical installation semantics and external labeling cannot be verified by the API; the MCP can only guard and confirm InvenTree's recorded relationship. Partial-quantity installs (splitting a bulk item so only part of it installs) remain unsupported by this story; an operator wanting that workflow must use the InvenTree UI/API directly or request a future story. The endpoints' own docstrings describe some constraints (e.g. required serialization) that pinned InvenTree 1.5.1 does not actually enforce; a future InvenTree release could tighten server-side validation to match its own documentation, which would surface as an ordinary upstream rejection rather than a silent gap.

### F-S55: Barcode Workflow Discovery

- Status: `Future`
- Issue: [#136](https://github.com/davidvanlaatum/inventree-mcp/issues/136)
- Depends on: F-S40, F-S43, F-S45, F-S47
- Decisions: approved by the operator on 2026-08-15 as a separate future discovery story. Object APIs expose only `barcode_hash`; raw assigned custom barcode read-back is not available through the checked API.
- Scope: investigate barcode endpoint semantics, permissions, data retention, privacy, object coverage, and safe MCP tool boundaries before approving implementation.
- Acceptance:
  - Verify whether non-empty `barcode_hash` reliably supports a derived `has_barcode` signal across each object type.
  - Pin generic resolution, generated barcode, link/unlink, duplicate, plugin, and unsupported-object behavior.
  - Determine whether raw hash values should ever be exposed or only semantic presence, and document that hashes cannot recover assigned barcode data.
  - Assess barcode scan history separately for retention, sensitivity, filtering, and authorization; do not treat history as canonical object read-back.
  - Produce proposed tools, scopes, mutation confirmations, tests, dependencies, and documentation changes for operator approval before implementation.
- Residual risk: barcode plugins and installation settings can materially change formats and behavior; discovery against one pinned environment cannot establish universal plugin compatibility.

### F-S56: Cross-Object Tag Workflow Discovery

- Status: `Future`
- Issue: [#137](https://github.com/davidvanlaatum/inventree-mcp/issues/137)
- Depends on: F-S40, F-S44
- Decisions: approved by the operator on 2026-08-15 as a separate future cross-object story. Tags are opt-in/shared taxonomy data and are not embedded into ordinary exact reads until their lifecycle is defined.
- Scope: inventory tag serializers, endpoint support, permissions, object coverage, and safe discovery/assignment/removal contracts.
- Acceptance:
  - Identify stable tag identity, name/slug behavior, creation ownership, deletion/reference consequences, and supported tagged object types.
  - Define concise tag search/exact-read tools and whether object reads expose tag IDs, summaries, or optional expansions.
  - Define guarded assignment/removal with exact read-back, duplicate normalization, and recovery semantics.
  - Resolve company schema/read-back gaps and any object-specific tag behavior through pinned integration evidence.
  - Return a scoped implementation proposal with dependencies, tests, permissions, and aligned documentation for operator approval.
- Residual risk: tag plugins, permissions, and shared-taxonomy changes can affect many objects; a cross-object design must avoid silently creating or deleting global tags.

### F-S57: Part Testing Workflow Discovery

- Status: `Future`
- Issue: [#138](https://github.com/davidvanlaatum/inventree-mcp/issues/138)
- Depends on: F-S40, F-S45
- Decisions: approved by the operator on 2026-08-15 as a separate future story. The part `testable` flag belongs in ordinary part maintenance, while templates and results require their own workflow design.
- Scope: investigate test-template and stock-result APIs, permissions, attachment handling, evidence integrity, and read/write lifecycle before implementation.
- Acceptance:
  - Inventory part test-template and stock-item test-result fields, filters, stable identities, permissions, and object relationships.
  - Define read tools for templates/results and determine how result attachments compose with existing bounded attachment-download policy.
  - Define guarded creation/update/deletion or append-only constraints, including timestamps, stations, users, values, pass/fail results, and required evidence.
  - Pin behavior for disabled templates, required values/attachments, repeated tests, result correction, and testable-flag changes.
  - Produce an operator-approved implementation proposal with scopes, tests, dependencies, residual risks, and documentation changes.
- Residual risk: test records may be quality or compliance evidence; mutation and deletion must not be exposed until retention, correction, and audit expectations are explicitly approved.

### F-S58: Pricing And Price-Break Workflow Discovery

- Status: `Future`
- Issue: [#139](https://github.com/davidvanlaatum/inventree-mcp/issues/139)
- Depends on: F-S40, F-S43, F-S47
- Decisions: approved by the operator on 2026-08-15 as a generic future pricing discovery story rather than a committed implementation contract.
- Scope: inventory pricing endpoints, calculations, currency behavior, permissions, object lifecycles, and read/write risks before proposing tools.
- Acceptance:
  - Pin the complete part-pricing response, supplier price-break, internal-price, and sale-price serializers and filters.
  - Distinguish computed price ranges from editable price records and establish exact decimal/currency semantics.
  - Define concise list/detail projections, stable recovery identities, duplicate rules, and any guarded mutation or deletion workflows.
  - Assess currency conversion/configuration drift, negative/zero price behavior, quantity-break ordering, and interactions with purchase-order pricing.
  - Return a scoped implementation proposal with dependencies, tests, permissions, documentation, and operator decisions.
- Residual risk: pricing can depend on mutable global currency configuration and plugins with no shared revision token; future tools must not overstate atomicity or financial consistency.

### F-S59: Part Requirements Visibility Discovery

- Status: `Future`
- Issue: [#140](https://github.com/davidvanlaatum/inventree-mcp/issues/140)
- Depends on: F-S40
- Decisions: approved by the operator on 2026-08-15 as future discovery rather than a current `get_part_requirements` implementation.
- Scope: determine part-requirement endpoint semantics, sources, filters, permissions, relationship to aggregate part fields, and useful read-only MCP contracts.
- Acceptance:
  - Pin response shapes and identify build, purchase, sales, allocation, and scheduling sources represented by the endpoint.
  - Establish whether requirements are current-state snapshots, how variants/revisions participate, and what filters/pagination are stable.
  - Define high-value bounded projections without crossing deferred sales/build mutation boundaries.
  - Compare requirements with `get_part` aggregate demand fields and avoid redundant or contradictory contracts.
  - Return a read-only implementation proposal with dependencies, tests, scopes, docs, and known limitations for operator approval.
- Residual risk: requirements are derived from concurrently changing operational records and may include domains whose write workflows remain deferred; any future output is necessarily a non-atomic snapshot.

### F-S60: Stocktake Generation And Reporting Discovery

- Status: `Active`
- Issue: [#141](https://github.com/davidvanlaatum/inventree-mcp/issues/141)
- Depends on: F-S46
- Progress: implementation started on `codex/f-s60-stocktake-generation-reporting` from `origin/main` at `12ee25b` after the operator promoted this future discovery story on 2026-08-21. The initial scope remains discovery-only; the client-method integration stack now starts one pinned worker alongside its existing web stack, while unrelated testenv users remain web-only.
- Discovery findings (2026-08-21): the API 530 schema describes independent nullable `part`, `category`, and `location` selectors; a part includes variant parts, a category includes subcategories, and a location includes sublocations. `generate_entry` and `generate_report` are independent write-only flags, while the read-only `output` is a `DataOutput` task/report descriptor. Against pinned InvenTree 1.5.1, each single-selector request returned HTTP 200 with an `output` response, despite the schema advertising 201; composed selectors and every tested flag combination were also accepted with HTTP 200. The merged client-method Testcontainers stack includes `invoke worker`, bounded `worker-health` readiness, and a per-environment shared signing key. The probes treat the immediate response as enqueue-only: a follow-up terminal-state check showed entry generation can complete, while report/both-flag outputs did not reach a terminal state within 30 seconds, so no completed snapshot/report claim is made. Raw `POST /api/part/stocktake/` still fails HTTP 500 because the serializer passes unsupported `user` data to `PartStocktake`.
- Decisions: the operator approved the recommended MCP boundary on 2026-08-21: require exactly one selector even though upstream accepts composed selectors, keep `generate_entry` and `generate_report` as explicit independent flags, and never treat an HTTP 200 enqueue response as completed work. Expose generation as a separate guarded operational workflow only after worker-backed evidence proves entry/report completion, duplicate behavior, permissions, and output retrieval. Bind confirmation to the complete selector/flag plan; keep historical snapshot reads, per-item quantity adjustment, and report/attachment retrieval as separate contracts. The worker is integrated into the existing client-method stack rather than creating a second stack; unrelated web-only testenv users do not pay the worker cost.
- Decisions: approved by the operator on 2026-08-15 as a separate future operational/reporting story. F-S46 remains read-only. See [F-S71](#f-s71-inventree-instance-info-tool)'s Decisions for a deferred question raised during that story's allowlist review: whether `get_inventree_instance_info` should expose broader plugin visibility (beyond the `plugins_enabled` flag and the `CURRENCY_UPDATE_PLUGIN`/`BARCODE_GENERATION_PLUGIN` settings it does expose) for stocktake-report-generation purposes, since some InvenTree stocktake report behavior depends on a report-generating plugin being enabled. That was deferred to this story's own scoped investigation rather than resolved in F-S71. F-S46 additionally discovered, live against pinned InvenTree 1.5.0, that `POST /api/part/stocktake/` (raw create) 500s unconditionally (an upstream bug: it passes an unsupported `user` keyword to a model with no such field) and `POST /api/part/stocktake/generate/` only enqueues generation on InvenTree's background-worker task queue, which `internal/testenv`'s shared Testcontainers stack does not run (only the `gunicorn` web process starts; see `internal/testenv/testenv.go`). F-S46 deliberately did not add worker-container support to the shared suite to populate test fixtures for this: the tool surface it shipped is read-only and has no write path this would exercise end-to-end anyway, and per `docs/reviewers.md` adding a worker container is a "Testcontainers integration architecture" change requiring its own full review panel plus a shared-suite startup-time cost paid by every test using the environment, not just this story's. If this story's generation/report design ends up calling `POST /api/part/stocktake/generate/` (or any other InvenTree endpoint that offloads to the background-worker queue), add worker-container support to `internal/testenv` as part of this story's own scoped Testcontainers change -- reviewed under this story's own panel -- rather than assuming F-S46 already covered it.
- Scope: pin stocktake generation/report behavior, selection scope, background-task/report artifacts, permissions, and confirmation requirements before implementation.
- Acceptance:
  - Verify part/category/location selection semantics, variant/subcategory inclusion, entry creation, report generation, and background-task responses.
  - Determine whether generation is idempotent, how duplicate same-day snapshots behave, and what current state must be reviewed and bound.
  - Define dry-run and confirmation contracts, output/report retrieval, attachment safety, and ambiguous-result recovery.
  - Keep quantity adjustment behavior separate from aggregate snapshot/report generation.
  - If the chosen design needs to prove real background-worker-generated output live, add worker-container support to `internal/testenv`'s shared Testcontainers stack (InvenTree's `invoke worker`/task-queue process) as part of this story, and confirm it does not measurably slow down unrelated tests that only need the existing web-process-only stack.
  - Return an operator-approved implementation proposal with scopes, tests, dependencies, documentation, and residual risks.
- Validation: `env GOCACHE=/private/tmp/inventree-mcp-fs60-gocache GOFLAGS=-trimpath go test ./internal/inventree -run '^TestClientMethodsAgainstInvenTree$/stock_tracking_and_stocktake_history$' -count=1 -v` passes against pinned InvenTree 1.5.1 in the merged web-plus-worker client stack, including bounded worker-health readiness, per-environment shared signing-key setup, live selector/flag probes for part/category/location, the HTTP 500 raw-create characterization, and existing empty-history/not-found reads (60.8s before the exploratory terminal-state check). The exploratory terminal-state check timed out on report/both-flag outputs after 123.6s and remains documented as residual risk. `env GOCACHE=/private/tmp/inventree-mcp-fs60-gocache GOFLAGS=-trimpath go test -tags no_integration_tests ./internal/testenv ./internal/inventree` and `git diff --check` also pass. The initial sandbox run was blocked by Docker-socket permissions and the escalated reruns passed.
- Review: Senior Go Developer, QA/Test Architect, Product Manager, and Infosec read-only panels completed on 2026-08-21. Readiness, per-environment signing isolation, stale documentation, and enqueue-only scope were addressed. The panel agreed that report/both-flag terminal completion, duplicate cleanup, permissions, and artifact retrieval remain follow-up discovery rather than claims of this story.
- Residual risk: generation may create many records and asynchronous report artifacts from a changing inventory snapshot; the pinned report/both-flag jobs did not reach terminal state within 30 seconds, and the characterization does not yet correlate or clean up generated artifacts. No atomic whole-inventory view should be implied.

### F-S61: Adopt InvenTree 1.5.0 API 530 Baseline

- Status: `Done`
- Issue: [#143](https://github.com/davidvanlaatum/inventree-mcp/issues/143)
- Depends on: F-S34, F-S37, F-S38
- Progress: implementation completed on `codex/f-s61-inventree-1-5-baseline` from `origin/main` (`3daee4e`) and merged by PR [#152](https://github.com/davidvanlaatum/inventree-mcp/pull/152) as commit `638bdf2`. The operator approved the story and issue creation on 2026-08-15 after a disposable compatibility probe established that InvenTree 1.5.0 reports API revision 530 and the complete race-enabled repository suite passes when only the test-harness version expectations are changed. The checked schema, endpoint manifest, blocking Testcontainers pin, immutable frontend route evidence, focused contract tests, and public/operator documentation now use the verified 1.5.0/API 530 baseline.
- Decisions: adopt the explicit released `inventree/inventree:1.5.0` image rather than a floating blocking tag; refresh from an official API 530 schema source with reproducible provenance; audit every implemented endpoint and version-pinned frontend route before changing the baseline. Preserve current MCP behavior unless verified incompatibility requires a separately surfaced product/workflow decision. This story does not claim 1.5.0 is the minimum supported version and does not expose new upstream capabilities solely because they appear in the refreshed schema.
- Scope: refresh the blocking InvenTree version/API pin, checked OpenAPI schema and provenance, endpoint manifest, immutable frontend route evidence, tests, and aligned project/operator documentation for InvenTree 1.5.0.
- Acceptance:
  - Blocking Testcontainers uses explicit `inventree/inventree:1.5.0`, runtime version `1.5.0`, and API revision `530`; no floating blocking tag is introduced.
  - `docs/api-schema.yaml` is refreshed from an official API 530 schema source with reproducible provenance, release/commit identity, fetch time, and SHA-256 recorded in `docs/api-schema.md`.
  - `docs/endpoint-manifest.yaml` matches the refreshed schema hash/version and every implemented endpoint entry is revalidated against the new operation, request, response, query, and status contracts.
  - API 511 to 530 drift and the InvenTree 1.5.0 breaking changes are reviewed for all implemented clients and tools; any required behavior change receives focused tests, while unclear product/workflow decisions are reported rather than guessed.
  - Immutable frontend router, `BrowserRouter`, and default-base evidence is refreshed to the InvenTree 1.5.0 release and typed web-link route assertions remain valid or are corrected with aligned documentation.
  - `docs/PLAN.md`, `docs/TASKS.md`, `docs/api-schema.md`, `docs/web-links.md`, `docs/tool-reference.md`, and `docs/operator-recipes.md` remain aligned with the adopted baseline and verified behavior differences.
  - Focused schema, manifest, version, and route tests plus `GOFLAGS=-trimpath go test -race -p=1 ./...` pass against InvenTree 1.5.0.
  - Per-package coverage is compared with exact base; Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes complete with every actionable finding resolved or documented.

Tasks:

- [x] Fetch and verify the official API 530 schema and InvenTree 1.5.0 source identities; audit schema and release drift against implemented contracts.
- [x] Update the explicit Testcontainers baseline, schema snapshot/provenance, endpoint manifest, and focused version/schema tests.
- [x] Refresh immutable 1.5.0 frontend route evidence and web-link assertions.
- [x] Align planning, schema-capability, task, tool-reference, web-link, and operator documentation.
- [x] Compare coverage, run full validation, and resolve the Go, QA, product, and infosec review panel.

- Validation: `go generate ./internal/tools`, `go mod tidy -diff`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test -tags no_integration_tests ./...`, focused schema/test-environment/web-link tests, `GOFLAGS=-trimpath go test -race -p=1 ./...` against pinned InvenTree 1.5.0, SHA-256 verification, and `git diff --check` pass. The API 511 to 530 audit found no removed manifest-referenced property or new required property and revalidated every manifest path, method, operation, request, response, required query, and status contract. Compared with exact base `origin/main` at `3daee4e`, every no-integration package coverage percentage is unchanged.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed. Go and product identified the mutable schema-refresh URL; it is now commit-pinned with an immediate SHA-256 check. QA additionally required a cross-package contract test coupling the checked OpenAPI version to the explicit Testcontainers API, runtime, and image pins, and identified one stale purchase-price baseline label; both were corrected. Infosec confirmed endpoint auth/scope declarations are unchanged, newly visible auth endpoints and optional fields are not exposed, and existing media/SSRF/token boundaries remain intact. Focused Go, QA, and product reruns found no remaining actionable findings; infosec found none.
- Residual risk: API 530 adds optional fields and representation changes that this story intentionally does not expose. The official API 530 export was generated from an earlier InvenTree commit than the 1.5.0 release tag, so manifest validation proves the checked contract while the complete pinned-live suite proves exercised release behavior; it cannot prove unimplemented upstream surfaces. Frontend routes remain outside OpenAPI and can drift in a later InvenTree release, so both schema and router evidence must be deliberately refreshed before the next blocking baseline change. This story does not establish the minimum supported InvenTree version.
- InvenTree 1.5.1 maintenance validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go generate ./internal/tools` (no manifest drift), `GOFLAGS=-trimpath go mod tidy -diff`, and `git diff --check` pass. `GOFLAGS=-trimpath go test -race ./internal/testenv -run '^TestStartInvenTreeStack$' -count=1 -v` passed against a freshly pulled `inventree/inventree:1.5.1` and confirmed runtime InvenTree `1.5.1` / API `530`. `GOFLAGS=-trimpath go test -race -p=1 ./...` passed with all default-on Docker suites against the same pin, including the `internal/schema` cross-package contract test and `readme_compat_test.go`'s README-row/pin equality check.
- InvenTree 1.5.1 maintenance review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer reviews run. Go found no actionable issues; it confirmed every other test literal referencing `1.5.0` is either arbitrary fixture data unrelated to the pin or a documented-behavior comment unaffected by this patch-only release, not a missed drift site. QA found `internal/testenv/testenv_test.go`'s mocked `/api/version/` JSON body hardcoded the runtime version string separately from the `DefaultVersion`/`DefaultAPIVersion` constants it is asserted against, risking silent drift on a future bump; fixed by building the mock body with `fmt.Sprintf` from those constants instead. Product found the many "Pinned InvenTree 1.5.0 behavior..." references across `docs/operator-recipes.md` and `docs/tool-reference.md` could read as unverified now that the compat table says `1.5.1`, and that the release's upstream security patch (`sqlparse` dependency bump inside InvenTree's own backend) was not called out anywhere; addressed by adding an explicit editorial-policy note to `docs/api-schema.md` stating that patch-only pin bumps with no API/schema change do not require rewriting those prose references, and by naming the security patch in this validation note. Infosec found no findings: the pin still resolves to an explicit, non-digest, non-floating tag; the upstream `sqlparse` bump is InvenTree's own backend dependency, not vendored by this Go module; and no auth, token, or credential-boundary code was touched. Focused reruns were not required; all findings were resolved in the same change before this note was recorded.
- InvenTree 1.5.1 maintenance residual risk: this is a same-API (`530`) patch release (InvenTree `1.5.1`, upstream security patch for `sqlparse` plus unrelated bug fixes), so no schema refresh, endpoint-manifest update, or frontend-route re-verification was performed or is believed necessary; the `docs/api-schema.yaml`/`docs/api-schema.md` schema-fetch provenance still reflects the original `1.5.0`-era fetch. If a future InvenTree patch release turns out to carry an undocumented behavior change despite an unchanged API version, the affected `docs/operator-recipes.md`/`docs/tool-reference.md` prose would need a targeted correction rather than relying on this note's blanket "unchanged" assumption.

### F-S62: Guarded Purchase-Order Hold, Resume, And Cancellation

- Status: `Done`
- Issue: [#144](https://github.com/davidvanlaatum/inventree-mcp/issues/144)
- Progress: implementation completed on `codex/f-s62-po-hold-resume-cancel` from `origin/main` (`930a459`), pushed as PR [#183](https://github.com/davidvanlaatum/inventree-mcp/pull/183).
- Depends on: F-S47, F-S61
- Decisions: approved by the operator on 2026-08-15. Purchase-order hold, resume, and cancellation are dedicated lifecycle workflows. Hold support must not ship unless the native resume transition is also verified and exposed. Generic status editing, custom-status mutation, whole-order deletion, and automatic cancellation are excluded. A live Testcontainers spike against pinned InvenTree 1.5.1/API 530 on 2026-08-19 established: purchase-order status codes are `PENDING=10`, `PLACED=20`, `ON_HOLD=25`, `COMPLETE=30`, `CANCELLED=40`; no native `resume` endpoint exists, so resume reuses `POST /api/order/po/{id}/issue/`, which always transitions to `PLACED` regardless of whether the order was held from `PENDING` or `PLACED`; `hold`/`issue` succeed unconditionally from `PENDING` or `PLACED` with no native source-state validation, and are silent no-ops (200 with unchanged status) when called on a `CANCELLED` order; `cancel` succeeds from `PENDING`, `ON_HOLD`, and `PLACED` — including a `PLACED` order with partially received stock, which InvenTree leaves orphaned but still linked to the cancelled order with no auto-disposal — and is refused (400 "Order cannot be cancelled") only from `COMPLETE`. Given this permissiveness, the MCP layer owns all source-state and receipt gating rather than relying on upstream refusal. The operator additionally decided on 2026-08-19: (1) `hold_purchase_order` is permitted from both `PENDING` and `PLACED` (not restricted to `PLACED`-only), with the dry-run and executed plan required to carry an explicit warning when the pre-hold state was `PENDING`, since resuming will silently place that order with the supplier; (2) `cancel_purchase_order` fails closed on any nonzero receipt on any ordinary line, refusing cancellation whenever any line's received quantity is greater than zero, regardless of the received stock's current disposition. Resume is exposed as a distinct `resume_purchase_order` tool (source-gated to `ON_HOLD` only) rather than folded into the existing `issue_purchase_order` tool, so `issue_purchase_order`'s existing `PENDING`-only scope and `inventree.read`/`inventree.write` authorization remain unchanged.
- Scope: add current-state-planned hold, resume, and cancel operations for one stable purchase order, pinning the InvenTree 1.5/API 530 transition and recovery semantics before implementation.
- Acceptance:
  - Pin allowed source states, hold behavior, the native resume transition (including whether it reuses issue), cancellation effects, line/receipt constraints, dates, tracking, and idempotency against InvenTree 1.5/API 530; refuse to register hold if resume cannot be proven.
  - Lifecycle preflight uses exact server filters or deterministic shared request/record budgets for ordinary lines, extra lines, receipts, and dependencies, and fails closed when completeness or permission coverage cannot be proven.
  - Dry runs return exact order identity/state, bounded sanitized high-value line/receipt context, target transition, warnings, and a principal-bound current-state token; notes, external links, and unrelated order data remain absent from confirmation and recovery output.
  - Execution requires explicit confirmation and the matching token, rejects stale plans, uses the native lifecycle endpoint, and verifies exact refreshed order state.
  - Hold and resume require `inventree.read`, `inventree.write`, and `inventree.operational` and remain closed-world and non-idempotent; cancel additionally requires `inventree.destructive` and publishes `destructiveHint:true`.
  - Cancellation refuses when safe disposition of received stock or other verified dependencies cannot be proven; whole-order DELETE remains unsupported.
  - Pinned integration tests cover hold/resume round trips, every allowed/refused transition, stale confirmation, ambiguous outcomes, and aligned plan/schema/tool/operator documentation.
- Tasks:
  - [x] Live-pin hold/resume/cancel transition semantics against InvenTree 1.5.1/API 530 with a disposable Testcontainers spike; record findings in this story's Decisions.
  - [x] Add `PurchaseOrderStatusOnHold`/`PurchaseOrderStatusCancelled` client constants and `HoldPurchaseOrder`/`CancelPurchaseOrder` client methods; reuse existing `IssuePurchaseOrder` for resume.
  - [x] Implement `hold_purchase_order`, `resume_purchase_order`, and `cancel_purchase_order` tools with a principal-bound plan-store token, source-state gating, bounded line/extra-line/receipt preflight, and exact refreshed-state verification.
  - [x] Wire tool scopes/annotations (`ToolAuthorizations`) and register the three tools.
  - [x] Add unit tests (fake client) and permanent Testcontainers integration tests replacing the disposable spike, covering hold/resume round trips, every allowed/refused transition, the `PENDING`-hold resume warning, the received-quantity cancel refusal, stale/reused tokens, and ambiguous-outcome recovery.
  - [x] Align `docs/PLAN.md`, `docs/api-schema.md`, `docs/tool-reference.md`, and `docs/operator-recipes.md` with the shipped behavior.
  - [x] Run validation and the Go/QA/Product/Infosec review panel; resolve findings.
- Validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./internal/tools/... ./internal/inventree/...` (0 issues), `go generate ./internal/tools` (manifest stable), `GOFLAGS=-trimpath go mod tidy -diff`, `git diff --check`, `go test -tags no_integration_tests ./...`, and the full `internal/tools`/`internal/inventree` unit suite pass. Pinned Testcontainers integration coverage against a freshly pulled `inventree/inventree:1.5.1`: `GOFLAGS=-trimpath go test ./internal/tools/ -run '^TestMilestoneHappyPathToolsAgainstInvenTree$/^purchase_order_hold_resume_and_cancel$' -v` (5 subtests: hold/resume round trip from `PLACED`, hold-from-`PENDING` warning plus resume-places-the-order, cancel of an unreceived order, cancel refusal on a partially received order, cancel refusal on a `COMPLETE` order) and `GOFLAGS=-trimpath go test ./internal/inventree/ -run '^TestClientMethodsAgainstInvenTree$/^po$' -v` (client-layer pin of the exact `ON_HOLD=25`/`CANCELLED=40` transitions, the silent no-op on a `CANCELLED` order, and the `400`/"Order cannot be cancelled" refusal from `COMPLETE`) both pass, rerun a second time after addressing review findings.
- Review: Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer passes completed via independent subagents against the full diff. Go found the mutation-succeeds/refresh-fails and definite-rejection recovery branches were unexercised by tests for all three tools despite fixture scaffolding already present; QA independently found the same gap plus a missing plan-store capacity-limit test and one assertion-style inconsistency; both were fixed by adding `holdErrAfterPersist`/`cancelErrAfterPersist`/`issueErrAfterPersist` and `getOrderErrAfterHold`/`getOrderErrAfterCancel`/`getOrderErrAfterIssue` fixture fields plus three table-driven mutation-recovery tests (`TestHoldPurchaseOrderMutationRecoveryAndDefiniteRejection`, `TestResumePurchaseOrderMutationRecoveryAndDefiniteRejection`, `TestCancelPurchaseOrderMutationRecoveryAndDefiniteRejection`), a `TestPurchaseOrderLifecyclePlanStoreEnforcesCapacityPerPrincipal` test, and normalizing the plan-store test to the `r := require.New(t)`/`a := assert.New(t)` convention. Infosec found purchase-order line/extra-line `Notes` was not redacted from dry-run/execute/recovery output, contradicting this story's own acceptance line that "notes... remain absent from confirmation and recovery output" (a pre-existing gap in the shared `sanitizePurchaseOrderLine`/`sanitizePurchaseOrderExtraLine` helpers that this story re-promised but did not originally deliver); fixed by blanking `Notes` in `loadPurchaseOrderLifecycleContext` specifically for these three tools, with a new `TestPurchaseOrderLifecycleRedactsLineAndExtraLineNotes` regression test, without changing the shared helpers' existing behavior for `issue_purchase_order`/`complete_purchase_order` (out of this story's scope). Infosec confirmed OAuth scope-to-tool mapping, plan-store principal binding, single-use/race safety, and token entropy are all correct with no other findings. Product found two low-severity documentation gaps: `docs/operator-recipes.md` didn't mention using `search_purchase_orders` with `status:25` to discover held orders, and `cancel_purchase_order`'s refusal message didn't state why (that InvenTree leaves received stock orphaned but order-linked); both fixed by wording additions. A focused rerun of all four roles against the follow-up diff, including rerunning the two Testcontainers integration blocks live, confirmed every fix correct and complete with no further actionable findings.
- Residual risk: lifecycle preflight and mutation are separate upstream requests; concurrent UI or API writers remain a single-writer concern and ambiguous outcomes require exact read-before-retry.

### F-S63: Guarded Purchase-Order Duplication Discovery

- Status: `Future`
- Issue: [#145](https://github.com/davidvanlaatum/inventree-mcp/issues/145)
- Depends on: F-S47, F-S61
- Decisions: approved by the operator on 2026-08-15 as a deferred, low-frequency workflow. Duplication is never folded into ordinary purchase-order creation or update.
- Scope: investigate purchase-order duplication behavior and produce a separately approved implementation proposal for copying an explicitly selected subset of one existing order into a new pending order with a new supplier reference; this discovery story does not implement the workflow.
- Acceptance:
  - Pin the write-only upstream `duplicate` serializer, copied metadata, ordinary/extra-line behavior, destination/project/contact/owner behavior, and generated reference semantics against InvenTree 1.5/API 530.
  - Require a new nonblank supplier reference and expose every copied, cleared, overridden, and excluded field in the dry-run plan.
  - Never copy status, receipt state, completion dates, barcode identity, tracking history, or build links.
  - Bind confirmation to the source snapshot and target identity; use deterministic duplicate recovery and exact read-back without creating multiple target orders.
  - Produce final scopes, annotations, tests, and public documentation for operator approval before implementation.
- Residual risk: duplication is a multi-record non-atomic operation and upstream serializer behavior may change; the workflow must preserve partial-result IDs and never invite blind retry.

### F-S64: Cross-Object Generic Parameter Values And Uniqueness

- Status: `Done`
- Issue: [#146](https://github.com/davidvanlaatum/inventree-mcp/issues/146)
- Depends on: F-S11, F-S12, F-S13, F-S20, F-S21, F-S47, F-S61
- Decisions: approved by the operator on 2026-08-15 as one generic cross-object story. API 530 makes generic parameter values visible on purchase orders, stock locations, companies, supplier parts, manufacturer parts, and part categories; all supported non-part families use existing compatible templates. Part-row values remain in F-S12 and category parameter-template/default links remain in F-S13. API 530 parameter-template `unique` is exposed and writable here because value writes must understand and preserve its uniqueness policy.
- Progress: implemented on `claude/f-s64-cross-object-parameters`. Design: one shared `model_type`-gated tool set (`search_object_parameters`, `create_object_parameter`, `delete_object_parameter`) covers all six object families instead of per-object duplication; `create_object_parameter` upserts (zero/one/multiple existing-value resolution) rather than separate create/update tools; template uniqueness-policy changes go through a new dedicated guarded `update_parameter_template_uniqueness` tool (dry_run/confirm/plan_hash), while ordinary `create_parameter_template` gains an optional `unique` field and `update_parameter_template` stays unchanged. Uniqueness-conflict reporting (both `create_object_parameter`'s conflict clarification and `update_parameter_template_uniqueness`'s preview `conflicts`) discloses only an opaque per-response group index plus each conflicting row's stable `parameter_id`/`model_type`/`model_id` — never the shared value itself — per an infosec review finding addressed before merge.
- Scope: extend generic parameter lookup and maintenance to `order.purchaseorder`, `stock.stocklocation`, `company.company`, `company.supplierpart`, `company.manufacturerpart`, and `part.partcategory`, and extend existing parameter-template reads/writes for `unique`, without creating templates implicitly or opening deferred object domains.
- Acceptance:
  - Add bounded object-scoped list and stable exact reads for parameter values on one purchase order, stock location, company, supplier part, manufacturer part, or part category; embedded API 530 parameter arrays are classified as separate lookups and preserve null/omitted/empty behavior in raw-key contracts.
  - Existing parameter-template search/get exposes `unique` as the verified enum `0` (none), `1` (model type), or `2` (global); template creation supports omission versus each explicit value with exact read-back and no implicit policy reset.
  - Changing an existing template's uniqueness policy is a dedicated guarded update: preview scans every linked parameter row through an exact template filter within shared request/record bounds, fails closed on incomplete or unauthorized coverage, computes target-policy conflicts, and returns only privacy-safe counts and stable row/object IDs.
  - The uniqueness-policy plan token binds the current complete template definition, target policy, and complete linked-row/conflict snapshot. Confirmed execution requires the matching principal-bound token, rejects stale state, immediately repeats the bounded scan, refuses while any target-policy conflict remains, patches only `unique`, and performs exact read-back with explicit ambiguous-result recovery.
  - Template uniqueness-policy mutation requires `inventree.read`, `inventree.write`, and `inventree.operational`, remains closed-world and non-idempotent, and publishes `destructiveHint:false` because it neither removes nor overwrites parameter values; ordinary template creation retains its existing read/write classification.
  - Create/update resolves an existing enabled template compatible with the target model, validates typed/choice/checkbox semantics, refuses ambiguous or incompatible templates, and preserves omitted versus explicit value forms.
  - Value create/update honors the selected template's uniqueness policy, performs bounded fail-closed conflict discovery where the API supports complete verification, and treats upstream uniqueness enforcement as authoritative without leaking unrelated record values.
  - Exact server-side target/template filters prove zero or one current value; where unavailable, deterministic bounded pagination fails closed on incomplete scans. Zero creates, one updates/reuses, and multiple matches refuse without mutation; templates and category/default links are never created implicitly.
  - Stable-value deletion returns a principal-bound token covering target, template definition, and current value; execution requires confirmation plus the matching token, rejects stale plans, revalidates immediately before delete, verifies exact-ID absence, and returns only privacy-safe context.
  - Reads require `inventree.read`; create/update require `inventree.read` plus `inventree.write`; delete additionally requires `inventree.destructive` and `destructiveHint:true`.
  - Pinned integration tests cover all six model types, permissions, template compatibility, uniqueness modes and conflicts, every uniqueness-policy transition, later-page conflicts, policy-transition races, stale plans, ambiguous policy-update response loss, zero/one/multiple value matches, typed values, null/omitted/empty expansions, concurrent target/template/value changes, clears/deletes, exact-ID absence, and aligned public docs/manifests.
- Validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go mod tidy -diff` and `git diff --check` (clean), `go generate ./internal/tools/...` (`docs/tool-manifest.json` regenerated and verified by `TestCheckedToolManifestMatchesGeneratedMetadata`), `go test -tags no_integration_tests ./...` and `GOFLAGS=-trimpath go test -race -p=1 ./...` (all default-on Docker suites pass, including the new `object_parameter`/`parameter_template_uniqueness` subtests in `TestClientMethodsAgainstInvenTree` and `object_parameter_and_template_uniqueness` in `TestMilestoneHappyPathToolsAgainstInvenTree`, each independently run live against pinned InvenTree 1.5.1 before the full-suite pass). Per-package coverage vs. `main` (`-short` mode): `internal/tools` 84.2% vs. base 84.6%; `internal/inventree` 83.8% vs. base 84.7% — both dips are fully explained by the new `SearchObjectParametersPage` client method and the destructive/guarded object-parameter code paths being exercised solely by the new Testcontainers subtests (0% in `-short` mode), matching the same pattern already accepted for every other write/delete client method in this codebase (e.g. `DeletePart` is also 0% in `-short` mode); no reduction requiring recovery. CI's full-mode (Docker-included) coverage bot separately flagged `internal/tools` dropping from 86.4% to 86.1%; follow-up added targeted unit tests for the two new plan stores' token-generation-error and outstanding-plan-capacity branches and `delete_object_parameter`'s unsupported-model-type, no-op-delete, and ambiguous-read-back branches, raising local full-mode `internal/tools` coverage from 85.7% to 85.9% against a 85.7%-base local measurement; the small residual gap remaining after these additions is limited to `encoding/json` marshal-error branches in the plan-digest functions, which are not practically triggerable against the plan structs' concrete field types.
- Review: full Go, QA, Product, and Infosec panel per `docs/reviewers.md` (new mutating tool surface). Go: no bugs found; plan-token digest/staleness logic and bounded uniqueness-conflict scanning both verified correct against the established `stockLocationTypeDeletePlanStore` pattern; noted the uniqueness-scan-then-write sequence is non-atomic (TOCTOU) but this matches the pre-existing `exactTemplateNameMatches`-style preflight pattern elsewhere in the codebase, not a new risk class. QA: unit/integration coverage is adequate for a first pass; flagged three cheap gaps (scan-limit fail-closed paths untested, a Global-scope conflict test that could not have caught a model-type/global scoping swap, missing malformed/unknown-token tests) — all four addressed with new tests before merge; recommended documenting residual coverage gaps explicitly, done below. Product: confirmed the design faithfully delivers the approved decisions and stays within dependency stories' boundaries; found the uniqueness-conflict payload lacked enough object identity to actually drive the documented recovery flow, and found only 1 of 6 object-family `get_*` tool-reference rows cross-referenced the new parameter tools — both addressed (conflict rows now carry `model_type`/`model_id`; all five remaining `get_*` rows now cross-reference `search_object_parameters`/`create_object_parameter`). Infosec: found one High-severity finding — both new conflict-clarification paths leaked the shared parameter value (and, for `create_object_parameter`'s cross-object conflict and multiple-existing-rows candidates, fell through `candidateFor`'s untyped default branch, stringifying the entire record into a malformed ID/label) — contradicting this story's own "returns only privacy-safe counts and stable row/object IDs" and "without leaking unrelated record values" acceptance language; fixed by replacing every conflict/multi-match candidate with dedicated ID-only builders (`objectParameterCandidate`, restructured `ParameterTemplateUniquenessConflict`/`ParameterTemplateUniquenessConflictRow`) that never serialize `Data`/value, verified by new tests asserting the JSON payload never contains a planted secret value. OAuth scope/annotation wiring, plan-token entropy/principal-binding/TTL/capacity bounds, and object-level authorization pass-through were all independently verified correct with no other findings. A fixes-applied rerun of Go/QA/Infosec-relevant checks (build/vet/lint/full race suite, plus the specific new privacy-assertion and conflict-shape tests) passed; no further review round was requested.
- Residual risk: parameter-template restrictions and permissions are installation-specific; preflight and writes are non-atomic and must fail closed when compatible-template or duplicate completeness cannot be proven. The uniqueness-conflict scan and the subsequent create/update write are two separate non-transactional calls (and likewise the uniqueness-policy confirm re-scan and the PATCH), so a narrow concurrent-write race remains between two callers racing the same conflict window; InvenTree's own uniqueness enforcement (where configured) remains the backstop. Live Testcontainers coverage of the six supported object types is not exhaustive: `stock.stocklocation` and `company.company` were independently confirmed live end-to-end (client-method and MCP-tool layers); `company.manufacturerpart`, `company.supplierpart`, `order.purchaseorder`, and `part.partcategory` are covered by fake-client unit tests plus the same shared, already-live-proven client methods (`SearchObjectParametersPage`, `CreatePartParameter`, `UpdatePartParameter`, `DeletePartParameter`), not independently live-confirmed per type. No client-side typed/choice/checkbox value validation is implemented for object-parameter values (matching the pre-existing part-parameter convention of treating upstream InvenTree serializer validation as authoritative); this was not separately live-verified by deliberately submitting an invalid typed value against a checkbox/choice-restricted template. Ambiguous policy-update response-loss recovery (`update_parameter_template_uniqueness` re-reading the template when the PATCH response identity/value is unexpected) is implemented but not exercised by a dedicated fault-injection test comparable to F-S67's `loseMutationResponseTransport`.

### F-S65: Guarded Stock Custom-Status Management

- Status: `Blocked`
- Issue: [#147](https://github.com/davidvanlaatum/inventree-mcp/issues/147)
- Depends on: F-S05, F-S45, F-S61
- Decisions: approved by the operator on 2026-08-15. Stock `status_custom_key` is maintained only through the existing guarded status workflow; `is_building` remains read-only while build workflows are deferred.
- Blocker: pinned InvenTree 1.5.1/API 530 accepts custom-status assignment and replacement, but its stock serializer rewrites the logical-status update and does not provide a nullable clear read-back. The implementation therefore fails closed on explicit clear rather than claiming success. Do not select F-S65 until the operator approves a changed contract or an upstream release provides a verified nullable-clear representation.
- Scope: extend `set_stock_status` planning and execution to assign or explicitly clear a compatible custom status key without exposing generic stock PATCH.
- Acceptance:
  - Pin custom-status discovery, logical-status compatibility, permissions, nullable clear behavior, and serializer read-back against InvenTree 1.5/API 530.
  - Add bounded stock custom-status discovery returning stable key, label, logical status, and safe selection fields.
  - Dry run binds current logical/custom stock status and the complete observed target-key definition/compatibility; execution requires the same principal-bound token, immediately re-reads both, and fails stale if the key changed, disappeared, or was remapped.
  - Refuse custom keys incompatible with the selected logical status and preserve current custom status when the field is omitted.
  - Retain `inventree.read`, `inventree.write`, and `inventree.operational`; remain closed-world, non-destructive, and non-idempotent.
  - Pinned tests cover assign, replace, clear, omitted value, incompatible keys, permissions, key removal/remapping between plan and execution, stale plans, read-back, and aligned docs/manifests.
- Residual risk: custom-status definitions can be changed by administrators independently of stock records; the plan binds observed identity and compatibility but cannot atomically lock global configuration.

### F-S66: Guarded Stock-Item Merge Discovery

- Status: `Future`
- Issue: [#148](https://github.com/davidvanlaatum/inventree-mcp/issues/148)
- Depends on: F-S28, F-S29, F-S45, F-S46, F-S61
- Decisions: approved by the operator on 2026-08-15 as a deferred workflow. Direct stock duplication remains unsupported.
- Scope: discover and define a guarded native stock merge workflow for explicitly selected compatible stock items.
- Acceptance:
  - Pin InvenTree 1.5/API 530 merge inputs, destination identity, surviving/deleted record behavior, tracking entries, and ambiguous response semantics.
  - Define compatibility for part, supplier/provenance, status, location, serialization, allocation, ownership, price/currency, parent/child, installation, build/consumption, customer, and sales state.
  - Dry run binds every source and destination item plus effective merged quantity and metadata; confirmation uses a principal-bound single-use token and rejects stale plans.
  - Default to fail-closed exact compatibility; any permitted mismatch requires explicit per-class approval rather than upstream `allow_mismatched_*` passthrough.
  - Classify deleted source records and provenance/audit changes accurately for OAuth scopes and `destructiveHint`.
  - Produce implementation-ready tools, recovery rules, pinned tests, and documentation for operator approval.
- Residual risk: merge can delete source identities and collapse audit/provenance distinctions; InvenTree does not make MCP preflight and execution atomic.

### F-S67: Stock-Location Detail And Type Administration

- Status: `Done`
- Issue: [#149](https://github.com/davidvanlaatum/inventree-mcp/issues/149)
- Depends on: F-S21, F-S61
- Decisions: approved by the operator on 2026-08-15. Exact stock-location reads expose both configured `custom_icon` and computed effective `icon`. Location types support create/update and guarded deletion; raw location barcode hash remains deferred to F-S55 without blocking this story. Implementation-time addition (2026-08-19, per Senior Product Manager review): `delete_stock_location_type`'s guarded deletion is deliberately non-blocking rather than fail-closed-on-references like the F-S68/F-S69 dependency-plan pattern, because a pinned live InvenTree 1.5.0 spike (see `docs/api-schema.md`'s "Verified Stock Location Detail And Type Administration Endpoints") proved `DELETE /api/stock/location-type/{id}/` on a referenced type returns `204` and safely `SET NULL`s `location_type` on every referencing location rather than refusing or cascading; the guarded token still binds and reports the exact referencing-location snapshot so the operator reviews the impact before confirming.
- Scope: complete the approved stock-location exact projection and add ordinary stock-location-type administration without changing location hierarchy or stock.
- Acceptance:
  - `get_stock_location` exposes effective `icon` alongside `custom_icon` and the API 530 hierarchy path; searches remain concise and raw `barcode_hash` remains excluded pending F-S55. API 530 parameters and tags remain in F-S64 and F-S56.
  - A pinned field inventory classifies every default location and location-type response field and raw-key contract tests fail on unclassified drift.
  - Create/update location types supports name, description, and icon with explicit clears where permitted, bounded case-insensitive duplicate preflight, stable-ID recovery, and exact read-back.
  - Type deletion returns a principal-bound token covering the type and a bounded complete location-reference snapshot; execution requires confirmation plus the matching token, rejects stale plans, immediately rechecks references, and verifies exact-ID absence.
  - Reads require `inventree.read`; create/update require `inventree.read` plus `inventree.write` and remain non-destructive; delete additionally requires `inventree.destructive` and `destructiveHint:true`.
  - Pinned tests cover effective icon fallback, CRUD, later-page duplicates/references, stale plans, ambiguous results, and aligned docs/manifests.
- Validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test ./... -short` (all packages pass), `go test ./internal/inventree/... -run TestClientMethodsAgainstInvenTree/stock_location -v` (new `stock_location_effective_icon` and `stock_location_type_administration` Testcontainers subtests pass against pinned InvenTree 1.5.0, live-confirming effective-icon precedence and the delete SET_NULL cascade), `go generate ./...` (`docs/tool-manifest.json` regenerated and verified by `TestCheckedToolManifestMatchesGeneratedMetadata`). Per-package coverage vs. `main`: `internal/tools` flat at 84.2%; `internal/inventree` flat in full Testcontainers-included mode (92.7%→92.6%), with the `-short`-mode-only dip (85.8%→84.7%) fully explained by new client methods exercised solely by the new Testcontainers subtests, matching every other write/delete client method in this codebase (e.g. `DeletePart` is also 0% in `-short` mode) -- no reduction requiring recovery.
- Review: full Go/QA/Product/Infosec panel per `docs/reviewers.md` (new mutating tools including a destructive delete). Go: no bugs found; plan-store, verify, and OAuth-scope wiring all correctly mirror established patterns; added an optional clarifying comment on `StockLocationTypeDeletePlan`'s `LocationCount` binding. QA: coverage genuinely matches the acceptance criterion, including the reference-set-specific stale-plan case and Testcontainers subtest isolation; no blocking gaps. Product: the non-blocking (vs. F-S68/F-S69-style fail-closed) delete design is a defensible reading of this story's own acceptance criteria and is disclosed to the operator/agent in the clarification `reason` text before confirmation is possible; recommended recording the empirical justification in this story's Decisions rather than leaving it only in `docs/api-schema.md` -- done above. Infosec: no security-relevant issues; scope/annotation wiring complete, plan-token binding closes principal/type-identity/reference-drift replay, reference scan fails closed.
- Residual risk: effective icons and reference counts are computed from mutable configuration; preflight and mutation are non-atomic and completeness-sensitive scans must fail closed at documented bounds. Deleting a still-referenced location type is allowed (per the verified InvenTree `SET_NULL` behavior) rather than fail-closed, so a narrow TOCTOU window remains between the confirm-time reference re-scan and the actual upstream delete where a location could start referencing the type without ever being shown to the operator -- structurally the same non-atomic-preflight limitation shared by every plan/confirm tool in this codebase, not something this story introduces or handles worse than its predecessors.

### F-S68: Guarded Stock-Location Deletion

- Status: `Done`
- Issue: [#150](https://github.com/davidvanlaatum/inventree-mcp/issues/150)
- Depends on: F-S21, F-S47, F-S64, F-S67
- Decisions: approved by the operator on 2026-08-15. Stock-location deletion is allowed only through a guarded destructive workflow after proving the exact location has no stock, child locations, or other supported references.
- Scope: add preview and confirmed deletion of one stable stock location without cascading, relocating, clearing, or rewriting dependent records.
- Acceptance:
  - Preflight inventories direct stock items, child locations, part/category defaults, purchase-order and line destinations, location parameters, and every other reference exposed by the pinned InvenTree 1.5/API 530 schema or verified API behavior.
  - A checked-in pinned dependency inventory maps every reference surface to its endpoint/filter, record bound, permission behavior, and blocker test; schema/endpoint-manifest drift tests fail on any unclassified new reference.
  - Reference scans use server-side exact filters where available, deterministic bounded pagination otherwise, and fail closed on permissions, unsupported surfaces, or incomplete results.
  - Dry run returns minimal counts and stable reference IDs plus a principal-bound token covering location state and the complete dependency snapshot.
  - Execution requires explicit confirmation and the matching token, rejects stale plans, rechecks all blockers, calls DELETE for only the stable ID, and verifies exact-ID absence.
  - No cascade, implicit stock transfer, default clearing, parameter deletion, or child reparenting is permitted.
  - Require `inventree.read`, `inventree.write`, `inventree.operational`, and `inventree.destructive`; publish `destructiveHint:true`, closed-world, and non-idempotent annotations.
  - Pinned tests cover every blocker, later-page references, stale plans, response loss, definite refusal, read-back, and aligned docs.
- Progress: Implemented on `codex/f-s68-guarded-stock-location-deletion` and squash-merged to `main` as `4ef9b10` via PR #189. The guarded tool inventories all schema-backed location references, issues principal-bound single-use plans only for empty locations, rechecks blockers before deletion, deletes only the stable ID, and verifies exact-ID absence with recovery guidance for ambiguous outcomes.
- Validation: `go generate ./internal/tools`, focused `go test -tags no_integration_tests ./internal/inventree ./internal/tools` coverage for all F-S68 scans, schema-aware dependency inventory, plan-store, tool metadata, and read-back branches, `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), and `git diff --check` pass. Exact-head CI also passed Go coverage/Testcontainers, golangci-lint, Release Preview, and pre-commit checks.
- Review: Delegated full-scope Go/QA/Product/Infosec review found and drove resolution of missing Build source coverage, missing surviving-location read-back coverage, incomplete schema-drift coverage, and CI-discovered authorization/integration/API-contract test gaps. Final review rerun found no actionable findings. The implementation uses the existing OAuth scope and annotation patterns, bounded fail-closed scans, checked-in dependency inventory, and aligned tool/schema/operator documentation.
- Residual risk: dependency checks and DELETE are non-atomic; a concurrent writer can add a reference after preflight, so operators must coordinate a single writer and inspect exact state after ambiguous outcomes.

### F-S69: Guarded Part-Category Deletion

- Status: `Done`
- Issue: [#151](https://github.com/davidvanlaatum/inventree-mcp/issues/151)
- Depends on: F-S13, F-S19, F-S40, F-S61, F-S64
- Decisions: approved by the operator on 2026-08-15. Part-category deletion is allowed only for an empty leaf category with no parameter-default links or other verified references.
- Scope: add preview and confirmed deletion of one stable part category without cascading, moving parts/children, deleting parameter defaults, or rewriting references.
- Acceptance:
  - Preflight inventories direct parts, direct child categories, category-parameter-template/default links, generic `part.partcategory` parameter-value rows, and every other reference exposed by the pinned InvenTree 1.5/API 530 schema or verified behavior.
  - A checked-in pinned dependency inventory maps every reference surface to its endpoint/filter, record bound, permission behavior, and blocker test; schema/endpoint-manifest drift tests fail on any unclassified new reference.
  - Reference scans use exact server-side filters where available, deterministic bounded pagination otherwise, and fail closed on permissions or incomplete coverage.
  - Dry run returns the exact category path/state, minimal blocker counts and stable IDs, and a principal-bound token covering the complete dependency snapshot.
  - The plan consumes API 530 category `path` through the existing exact category read and pinned JSON contracts preserve null/omitted/empty/populated path semantics without inventing hierarchy data.
  - Execution requires explicit confirmation and the matching token, rejects stale plans, rechecks all blockers, deletes only the stable ID, and verifies exact-ID absence.
  - No cascade, part move, child reparent, parameter-link deletion, or default-location mutation is permitted.
  - Require `inventree.read`, `inventree.write`, and `inventree.destructive`; publish `destructiveHint:true`, closed-world, and non-idempotent annotations.
  - Pinned tests cover each blocker, later-page dependencies, stale plans, response loss, definite errors, read-back, and aligned docs/manifests.
- Progress: Implemented on `codex/f-s69-part-category-deletion`, transplanting and completing the uncommitted implementation from `claude/f-s69-part-category-deletion`. The guarded tool inventories all four pinned reference surfaces, issues principal-bound single-use plans only for empty categories, rechecks blockers before deletion, sends both upstream cascade flags as `false`, and verifies exact-ID absence after the mutation.
- Validation: `go generate ./internal/tools`, `GOFLAGS=-trimpath go test -tags no_integration_tests ./...`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, and `git diff --check` pass. Focused F-S69 package tests pass after the review follow-up, including later-page and inconsistent-final-page cases for all four reference surfaces. The default-on Testcontainers integration subtest remains unrun because Docker is unavailable in this environment.
- Review: The full Senior Go, QA / Test Architect, Product Manager, and Infosec panel ran. Go, Product, and Infosec found no actionable issues on the follow-up. QA found and drove resolution of two safety gaps (unreliable server-side category filtering and inconsistent pagination), then found and drove resolution of one inventory-documentation mismatch; the final QA rerun found no actionable findings. The resulting implementation uses the existing OAuth scope and annotation patterns, has fail-closed bounded scans and stale-plan binding, and does not cascade or rewrite references.
- Residual risk: reference checks and DELETE are non-atomic; concurrent writers can add dependencies after preflight, so operators must coordinate a single writer and inspect exact state after ambiguous outcomes.

### F-S70: InvenTree Version Compatibility Table

- Status: `Done`
- Issue: [#158](https://github.com/davidvanlaatum/inventree-mcp/issues/158)
- Depends on: F-S61
- Progress: implemented on branch `claude/inventree-version-compat-table-ec53cb` from `origin/main` at `3d42330`.
- Decisions: approved by the operator on 2026-08-16. A pin-change commit cannot know its future release tag because AGENTS.md's `## Release Workflow` section only assigns `vX.X.X` at `git tag`/`git push` time (Maintainer Release Flow), after the pin-change commit already exists on `main`; the table therefore tracks the InvenTree pin currently in effect using a `main (unreleased)` placeholder row until a tag is actually cut, resolved to the concrete `vX.X.X` in the same commit that gets tagged. Following the 2026-08-16 pre-implementation review panel: neither the drift test nor the release gate may depend on whole-file text search or parsed Markdown table cells — a whole-file grep for the placeholder text risks matching the README's own prose explaining this mechanism and permanently blocking every future release, and the table's column layout was never pinned down for cell-parsing to be reliable. The table therefore lives inside an explicit HTML-comment-fenced region in README.md, and both the drift test and the release gate operate only on a single, fixed-format anchor line inside that region. This story was split out of a combined table/tool/CLI plan after the panel found the original single story bundled three sub-projects with different risk profiles and reviewers; the new read-only instance-info tool is now [F-S71](#f-s71-inventree-instance-info-tool) and the CLI `version`/`self-update` format change is now [F-S72](#f-s72-porcelain-style-version-cli-format-and-self-update-rewrite).
- Scope: add a README table, inside a fenced anchor region, mapping released `inventree-mcp` versions to the InvenTree version/API revision each was verified against; keep it aligned with the blocking Testcontainers baseline across both the pin-change and release-tagging events via a Go drift test and a tag-triggered release gate.
- Acceptance:
  - README gains a "Supported InvenTree Versions" table wrapped in HTML comment markers (e.g. `<!-- BEGIN inventree-compat-table -->` / `<!-- END inventree-compat-table -->`) built from the blocking Testcontainers pin (`internal/testenv.DefaultInvenTreeImage`/`DefaultVersion`/`DefaultAPIVersion`) recorded at each released `vX.X.X` tag, collapsing consecutive tags with an identical pin into one range row. Evidence gathered from `git show <tag>:internal/testenv/testenv.go`: `v0.0.1` pinned InvenTree `1.4.0`/API `511`; `v0.0.2`–`v0.0.10` pinned InvenTree `1.4.3`/API `511`; `v0.0.11` pinned InvenTree `1.5.0`/API `530`.
  - Table wording documents verified/tested compatibility only and stays consistent with `docs/PLAN.md` Compatibility Decisions' existing statement that the blocking pin "does not establish a minimum supported InvenTree version"; it must not claim a broader support guarantee than what is verified.
  - The table's last row uses a `main (unreleased)` label, not a version number, whenever the InvenTree pin in effect on `main` has not yet been released under a tag, and that row is written as a single, fixed-format line inside the fenced region (a stable literal prefix the drift test and release gate can match unambiguously, distinct from any surrounding explanatory prose). A baseline-pin-change story (e.g., an F-S61-style story) closes the previous row at the last tag that carried the old pin and opens `main (unreleased)` for the new pin in the same change that edits `internal/testenv`'s pin constants.
  - AGENTS.md's `## Release Workflow` section gains an explicit step: before running `git tag`, replace the `main (unreleased)` label with the chosen `vX.X.X` in a commit on `main`, then tag that same commit, so the tagged commit's own README already reports its correct version. If the pin has not changed since the last tag, no new row is needed; the existing tagged row's range simply continues to cover the new tag.
  - AGENTS.md's top-level doc-alignment bullet and `docs/PLAN.md` Compatibility Decisions both cross-reference this two-step update rule (pin-change commit vs. release-tag commit) so neither surface goes stale independently.
  - The repo-root `doc.go` (package `inventreemcp`, currently has no embeds) gains a `//go:embed README.md` and a `ReadmeMarkdown()` accessor, mirroring `docs/doc.go`'s existing embed pattern. A focused Go test parses only the fenced region's anchor line and asserts it matches the same InvenTree version and API revision as the compiled-in `internal/testenv.DefaultVersion`/`DefaultAPIVersion` constants, so a pin-change PR that forgets the README table fails CI instead of silently drifting. This test only catches a values mismatch; it cannot detect a stale `main (unreleased)` label once a tag already exists for that commit, since it does not depend on fetched git tag state.
  - The tag-triggered `.github/workflows/release.yml` Release workflow adds a step, run before the GoReleaser publish step, that fails the release if the fenced region's anchor line still reads `main (unreleased)` rather than a concrete tag — scoped to that one line inside the fenced region, not a whole-file search, so the mechanism's own explanatory prose elsewhere in the README can safely mention the phrase without ever tripping the gate. This is the check that actually catches the "tagged without swapping the label" case; the per-commit Go test above only catches the separate "pin changed without touching the README" case. If this gate fails, the maintainer fixes the label on `main`, deletes the just-pushed tag (safe because no release assets were published yet), and re-tags; this is documented as the expected recovery path in AGENTS.md's Release Workflow section.
- Residual risk: the compatibility table reflects the blocking Testcontainers pin recorded at each tagged release, not exhaustive testing against every InvenTree patch release in between. Nothing enforces that a release is tagged before the next pin-change commit lands; two consecutive pin changes without an intervening release collapse into one `main (unreleased)` row rather than preserving an untagged intermediate pin, which is accepted because no released version ever depended on that intermediate state. At implementation time, `main`'s pin already matched the last tagged release (`v0.0.11`), so this change never exercises the `main (unreleased)` placeholder path end-to-end (write the placeholder on a pin-change commit, have the drift test and release gate both react to it, then resolve it before tagging); it only round-trips against already-known, already-tagged data. The maintainer should double-check the mechanism works as designed the first time it is exercised for real, on the next InvenTree baseline change.
- Validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test -tags no_integration_tests ./...`, `go test -run TestReadmeCompatTableMatchesBlockingPin -v -count=1 .`, and `git diff --check` all pass. The drift test and release-gate script were each manually simulated against both a resolved-tag row and a `main (unreleased)` placeholder row to confirm they pass/fail correctly in both states, and against a mismatched-pin scenario to confirm the drift test fails with a clear message. Per-package coverage is unaffected outside the repo-root package, which gains its first test file (0% → 100%, no prior baseline to compare).
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager reviews run (Senior Infosec Reviewer not required per `docs/reviewers.md`'s Review Timing — this change touches no auth, upload, Testcontainers architecture, or new mutating tool surface). No blocking or major findings from any reviewer. Product's two minor findings were addressed: the README intro sentence was clarified to note the current row is "shown as its shipped tag once released," and this residual-risk note was added to flag that the placeholder path is not yet exercised end-to-end. QA independently re-verified the per-tag pin evidence and the drift-test/release-gate behavior against both states; Go independently verified the cell-parsing logic, the `doc.go` embed pattern, and the release-gate script. Follow-up (post-review, operator caught via the actual GitHub-rendered PR diff): the original design put the `current-row` anchor comment on its own line between two table rows, which broke GitHub's Markdown table renderer — a table row sequence cannot be interrupted by a non-table line, so every row after the anchor fell back to plain text. Fixed by moving the anchor to a trailing HTML comment on the current row's own line instead of a separate line, and updating the Go test and release-gate `awk` pattern to parse that same line directly rather than the line following it. Re-verified: `go build`, `go vet`, `golangci-lint` (0 issues), the full non-integration test suite, and manual simulation of both the resolved and placeholder states for the drift test and release gate all pass unchanged. This is a narrow, mechanical relocation with no behavior change beyond fixing the rendering defect, so it was not sent back through the full subagent panel; it is recorded here for visibility.

Tasks:

- [x] Add the README table inside a fenced anchor region, with a single-line fixed-format current/unreleased-row anchor, from the gathered per-tag pin evidence (`v0.0.1`: 1.4.0/511; `v0.0.2`–`v0.0.10`: 1.4.3/511; `v0.0.11`: 1.5.0/530).
- [x] Add the `//go:embed README.md` and `ReadmeMarkdown()` accessor to the repo-root `doc.go`, mirroring `docs/doc.go`.
- [x] Add the two-step "keep the table in sync" instructions to AGENTS.md's `## Release Workflow` section and `docs/PLAN.md` Compatibility Decisions.
- [x] Add the focused Go test asserting the fenced region's anchor line matches `internal/testenv.DefaultVersion`/`DefaultAPIVersion`.
- [x] Add the tag-triggered Release workflow gate scoped to the anchor line, and document the delete-and-retag recovery path in AGENTS.md.

### F-S71: InvenTree Instance Info Tool

- Status: `Done`
- Issue: [#159](https://github.com/davidvanlaatum/inventree-mcp/issues/159)
- Depends on: F-S61
- Progress: implemented on branch `claude/f-s71-start-d38eb0`. The BLOCKING GATE cleared on 2026-08-16 (see Decisions); the tool, client methods, tests, and docs were built against the approved allowlist, then a full Go/QA/Product/Infosec review panel ran and its actionable feedback was addressed.
- Decisions: approved by the operator on 2026-08-16, split out of the original combined plan (see [F-S70](#f-s70-inventree-version-compatibility-table)'s Decisions for why). The new tool exposes InvenTree server/API version and instance identity plus a curated, explicitly-approved allowlist of non-sensitive global settings and the caller's own user settings; it does not enumerate every global setting, because InvenTree global settings can include operationally sensitive server configuration. The 2026-08-16 pre-implementation review panel's Product and Infosec reviewers independently flagged the same top concern: leaving the concrete allowlist as "decided during implementation" is not acceptable for a story marked `Ready`. The operator's explicit direction was to leave the story `Ready` rather than `Blocked`, but to make allowlist approval a hard, procedurally-enforced gate: the first task below blocks every other task in this story and requires the operator's explicit sign-off before any code is written, per AGENTS.md's "ask the operator specific questions before building" rule and `docs/reviewers.md`'s panel-before-scope-expansion trigger. Infosec additionally found that InvenTree's `GlobalSettings` schema exposes no `protected`/masked flag on setting values, so the allowlist review must check each candidate key's actual live value on a running instance, not just its name or docstring.
  - BLOCKING GATE cleared 2026-08-16: allowlist drafted by starting a pinned InvenTree 1.5.0 Testcontainers instance and inspecting all 158 live global-setting values and all 42 live user-setting values (no credential/SMTP/email keys exist in InvenTree 1.5.0's DB-backed global settings at all; all 42 user settings are UI display/search preferences). The operator reviewed and approved the following final set:
    - Instance identity (from `/api/`, `/api/version/`, no allowlist needed — informational identity fields only): `server`, `version`, `apiVersion`, `instance`, `version.commit_hash`, `version.commit_date`, `up_to_date`, `plugins_enabled`. Excluded as operationally sensitive/fingerprinting: `platform`, `database`, `django_admin`, `installer`, `docker_mode`, `debug_mode`, `worker_*`, `active_plugins` (reveals installed plugin versions), `email_configured`, `customize`/`settings` sub-objects, `system_health`, `target`, `id`, `version.django`, `version.python`.
    - Global settings allowlist (23 keys, fetched individually via `/api/settings/global/{key}/`): instance/business identity — `INVENTREE_INSTANCE`, `INVENTREE_INSTANCE_ID`, `INVENTREE_COMPANY_NAME`, `INVENTREE_DEFAULT_CURRENCY`, `CURRENCY_CODES`; reference number patterns — `BUILDORDER_REFERENCE_PATTERN`, `SALESORDER_REFERENCE_PATTERN`, `PURCHASEORDER_REFERENCE_PATTERN`, `RETURNORDER_REFERENCE_PATTERN`, `TRANSFERORDER_REFERENCE_PATTERN`; workflow/module availability — `RETURNORDER_ENABLED`, `TRANSFERORDER_ENABLED`, `STOCKTAKE_ENABLE`, `PROJECT_CODES_ENABLED`, `PART_ENABLE_REVISION`, `PART_ENABLE_LOCKING`, `SERIAL_NUMBER_GLOBALLY_UNIQUE`, `PARAMETER_ENFORCE_UNITS`, `BARCODE_ENABLE`, `LABEL_ENABLE`, `REPORT_ENABLE`; plugin selection — `CURRENCY_UPDATE_PLUGIN`, `BARCODE_GENERATION_PLUGIN` (the operator asked about broader plugin visibility for a possible future stocktake story; the panel-equivalent judgment here was to keep only these two low-risk feature-selection keys plus the `plugins_enabled` identity flag, and explicitly not expose `/api/plugins/` or `active_plugins`' installed-plugin/version roster, deferring any deeper plugin-status need to that future story's own scoped investigation).
    - User settings allowlist (1 key, fetched via `/api/settings/user/{key}/`): `DATE_DISPLAY_FORMAT`, plus a defensive `TOKEN|SECRET|PASSWORD|KEY` regex filter applied on top even though no observed user-setting key needs it.
    - Excluded, by category, from the global-settings allowlist: auth/security policy (`LOGIN_*`, `SSO_*`, `SIGNUP_GROUP` — reveals MFA/SSO/registration attack-surface config), remaining plugin internals (`ENABLE_PLUGINS_*` mixin toggles, `PLUGIN_ON_STARTUP`, `PLUGIN_UPDATE_CHECK`), backup/retention/log policy (`INVENTREE_BACKUP_*`, `INVENTREE_DELETE_*_DAYS`, `INVENTREE_PROTECT_EMAIL_LOG`, `REPORT_LOG_ERRORS`, `REPORT_DEBUG_MODE`), UI banners (`INVENTREE_SHOW_*_BANNER`), `INVENTREE_BASE_URL` (could reveal an internal-only hostname), and remaining fine-grained business-behavior toggles not tied to identity/capability.
- Scope: add a new read-only MCP tool returning InvenTree instance version/identity plus a curated safe settings subset, gated on an explicit pre-implementation allowlist decision.
- Acceptance:
  - BLOCKING GATE, must clear before any other acceptance criterion below is implemented: a concrete global-settings allowlist is drafted by checking each candidate key's actual value against a running InvenTree instance (not just its name), explicitly excludes credentials, email/SMTP, plugin secrets, and any other operationally sensitive configuration, and is explicitly approved by the operator. The rest of this story's acceptance criteria describe the tool's shape; they do not by themselves satisfy this gate.
  - A new read-only tool (name decided during implementation, e.g. `get_inventree_instance_info`, distinct from the existing `health_version` tool, which returns only MCP server build metadata and is unchanged by this story) calls `/api/` and/or `/api/version/` for InvenTree server version, API version, and instance identity, plus the approved allowlisted `/api/settings/global/{key}/` entries and the caller's own `/api/settings/user/` entries.
  - The tool fetches only the allowlisted keys directly (never enumerates the full settings list and filters client-side), and degrades gracefully — omitting a key rather than failing the whole call — both when an allowlisted key is missing/renamed upstream and when the configured credential lacks the staff access InvenTree's own schema description says `/api/settings/global/{key}/` requires, even though its declared OAuth security scope is only `g:read` (a schema/behavior inconsistency on InvenTree's side, not something this tool can fix, but one it must not fail loudly on).
  - The caller's own `/api/settings/user/` entries get the same defensive treatment despite being self-disclosure: either a documented check that no user-setting key in the covered InvenTree versions can hold a credential/token-like value, or an explicit defensive filter (for example excluding keys matching `TOKEN|SECRET|PASSWORD|KEY`).
  - Tool is registered `read_only` (`readOnlyHint:true`, `destructiveHint:false`, `openWorldHint:false`), requires only `inventree.read` scope, and is added to `docs/tool-reference.md` and `docs/tool-manifest.json`. This is the first `inventree.read` tool that touches staff-scoped administrative data rather than ordinary business data; call that distinction out explicitly in the tool reference so future admin-surface tools get equivalent scrutiny.
  - `docs/endpoint-manifest.yaml` gains entries for `/api/`, `/api/version/`, `/api/settings/global/{key}/`, and `/api/settings/user/{key}/` with request/response schema references, matching the repository's actual drift-enforcement mechanism (the endpoint-manifest contract tests) rather than relying on `docs/api-schema.md` prose alone.
  - `docs/api-schema.md` records the endpoints used and the exact allowlisted global-setting keys with rationale for excluding the rest.
  - Focused unit tests plus at least one Testcontainers live-client exercise per AGENTS.md's default-on integration-coverage rule; the Testcontainers exercise asserts only allowlisted-key presence/shape (never literal values, which can legitimately vary between instances/versions) and includes a case proving a removed/renamed allowlisted key is omitted rather than failing the call.
  - `docs/operator-recipes.md` gets a short usage recipe.
- Residual risk: InvenTree administrators can rename or remove custom global-settings keys independently of this allowlist; the lookup must omit a missing key rather than fail the tool call. Because the allowlist gate defers the actual key list to implementation time rather than pinning it in the story text, there is a real risk of implementation starting before operator sign-off if the gate is not enforced procedurally — the task list below makes it the literal first, blocking task for exactly that reason. The 2026-08-16 post-implementation review panel additionally flagged that the graceful-degradation path for insufficient InvenTree staff privilege (a real `403` from `/api/settings/global/{key}/` or `/api/version/`) is exercised only by unit tests against a fake client, not against a live InvenTree instance, because `internal/testenv`'s shared Testcontainers fixture currently supports only a staff/admin account role (no non-staff `AccountRole` exists to create a live low-privilege credential). This is accepted as a residual risk rather than blocking the story: the omission logic itself (`inventree.IsOmittableFetchError`) is directly unit-tested, and the live Testcontainers exercise still proves the underlying `403`/`404` HTTP classification path is reachable and correctly shaped against real InvenTree responses for the `404` case. Adding a non-staff account role to the shared fixture, if a future story needs it more broadly, would let this be revisited.
- Validation: `go build ./...`, `go vet ./...`, `golangci-lint run ./internal/tools/... ./internal/inventree/...` (0 issues), and `go test ./... -short` all pass. A live Testcontainers run of the new `instance_info` subtest against a pinned InvenTree 1.5.0 container passed both before and after the review-panel fixes (all 22 allowlisted global-setting keys, the 1 allowlisted user-setting key, `/api/`, and `/api/version/` resolved successfully; a deliberately nonexistent global-setting key was correctly classified as an omittable `404`). Per-package coverage vs. `main`: `internal/tools` 84.0% (unchanged), `internal/inventree` 89.1% vs. 89.2% on `main` (noise-level, not a real reduction). `git diff --check` passes.
- Review: full Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer panel run per `docs/reviewers.md` (required here as a new tool-surface change touching staff-scoped administrative data with new Testcontainers coverage). No blocking or high-severity findings from any reviewer; allowlist fidelity, identity-field scoping, log redaction, and scope/annotation wiring were all independently verified exact against the approved spec. Actionable feedback addressed: added an exact-key-identity check on both `GetGlobalSetting`/`GetUserSetting` responses (Go); documented the deliberate sequential (non-concurrent) per-key fetch design with a code comment (Go); broadened the defensive user-setting regex from `TOKEN|SECRET|PASSWORD|KEY` to also match `PASSWD`/`CREDENTIAL`/`PRIVATE` (Infosec); recorded the untested-live-403-path as an explicit residual risk above (Infosec); added a comment documenting why the defensive-filter unit test is safe without `t.Parallel()` (QA); added a cross-reference comment linking the integration test's duplicated allowlist back to its source of truth (Go); added a `docs/reviewers.md` Review Timing bullet requiring the full panel for future tools reading staff-scoped/administrative InvenTree data, closing the gap where this story's "future admin-surface tools get equivalent scrutiny" language had no procedural enforcement (Product); cross-linked the deferred plugin-visibility question to F-S60's Decisions block so it isn't silently lost (Product); and added an explicit "a nonempty `omitted_*` list is not an error" statement to the tool's registered description so an MCP-calling agent that never reads the markdown docs still gets that signal (Product). All fixes reverified with a full rebuild, lint, test, and a second live Testcontainers run.

Tasks:

- [x] BLOCKING: draft the concrete global-settings allowlist by checking candidate keys' live values against a running InvenTree instance, and get explicit operator approval before any other task in this story starts.
- [x] Implement the new read-only instance-info tool with scopes, manifest, and tool-reference entries, including graceful degradation on missing keys and insufficient-privilege responses.
- [x] Add the defensive user-settings filter or the documented no-sensitive-key-exists check.
- [x] Add `docs/endpoint-manifest.yaml` entries for the four endpoints used.
- [x] Add unit tests and a Testcontainers live-client exercise (allowlisted-key presence only, plus the removed-key-omission case) for the new tool.
- [x] Update `docs/operator-recipes.md` with a usage example.

### F-S72: Porcelain-Style Version CLI Format And Self-Update Rewrite

- Status: `Done`
- Issue: [#160](https://github.com/davidvanlaatum/inventree-mcp/issues/160)
- Depends on: none
- Decisions: approved by the operator on 2026-08-16, split out of the original combined plan (see [F-S70](#f-s70-inventree-version-compatibility-table)'s Decisions for why). The bare `inventree-mcp version` command's exact 3-line stdout is required byte-for-byte by `internal/selfupdate.requireVersion` before self-update will replace the running binary, and that parser is compiled into every already-distributed binary and cannot be retroactively patched. The operator directed adopting a git-`--porcelain`-style versioned machine contract now, while the project is still pre-v1.0. The 2026-08-16 pre-implementation review panel found two concrete gaps in that design, both resolved here: (1) the porcelain line parser must split each line on the first `": "` occurrence rather than any colon or a fixed line count, because the existing `date` field is RFC3339 (e.g. `2026-08-16T10:00:00Z`) and itself contains colons; (2) `self-update`'s reuse of the already-fetched candidate response for its result-summary baseline requires concrete signature changes, pinned explicitly in this story's acceptance criteria rather than left to implementer discretion, because `requireVersion` is currently called twice inside `installVerified` (pre-rename on the staged candidate, post-rename on the installed executable) and only the pre-rename call is the source of truth for the summary — without pinning this, an implementer could satisfy vague acceptance text with a second, redundant exec instead of true reuse. The panel also found the originally-worded "graceful degradation when fields are absent" scenario had no reachable input, since every currently-released binary lacks the `porcelain` marker entirely and is rejected outright rather than "passing with fields missing"; this story instead makes `inventree_version`/`inventree_api` optional fields within an otherwise-valid `porcelain: 1` response (only `porcelain`, `version`, `commit`, and `date` are required), giving degradation a real, reachable path.
- Follow-up decision (operator, 2026-08-16, during implementation): the initial implementation matched the pre-implementation panel's design of making porcelain the `version` subcommand's unconditional default output with no separate flag. After reviewing that implementation, the operator directed a mid-implementation change, reasoning that an operator running `inventree-mcp version` interactively at a terminal should see legible human-readable text by default, not raw `key: value` machine lines, and that `self-update`'s own machine-parsed request to a candidate should be an explicit, self-documenting flag invocation rather than a bare command whose meaning depends entirely on unstated convention. `version` now prints human-readable text by default, and a new `--porcelain <N>` flag requests the machine format for an explicit porcelain *format* version `N` (currently only `1`), fully separating format-version negotiation from the InvenTree-baseline/release-version content of the response. `internal/selfupdate.requireVersion` now always requests `internal/buildinfo.PorcelainMarker` (the format version this build itself understands) via `version --porcelain <N>`, rather than the earlier design where `--porcelain` (if it had existed) would have carried the target *release* version; the release-version match check remains a separate Go-side comparison against the parsed response's `version` field, unchanged from the original design. This supersedes the acceptance wording below, which originally specified no separate flag; the Acceptance and Tasks sections below are updated in place to describe the flag-based design actually shipped.
- Scope: replace the `version` subcommand's output with a versioned, key-based porcelain-style machine format carrying the InvenTree baseline, and move `self-update`'s safety check and result reporting onto that format, accepting one documented manual-reinstall release as the migration cost.
- Acceptance:
  - A new lightweight, dependency-free constant pair (e.g. `buildinfo.PinnedInvenTreeVersion`/`PinnedInvenTreeAPIVersion` in `internal/buildinfo`, which already has zero external imports and is already linked into the production binary) becomes the single source of truth for the InvenTree baseline. `internal/testenv.DefaultVersion`/`DefaultAPIVersion`/`DefaultInvenTreeImage` reference or are tested against these same constants so the Testcontainers pin and the CLI output cannot drift from each other independently. The production `cmd/inventree-mcp` binary must not gain a dependency on `internal/testenv` or Testcontainers/Docker packages to obtain this value.
  - The `version` subcommand prints human-readable text by default. `version --porcelain <N>` prints stable `key: value` lines led by an explicit `porcelain: 1` marker for porcelain format version `N` (currently only `1` is supported; an unsupported `N` fails closed with a clear error and no stdout output). Required keys: `porcelain`, `version`, `commit`, `date`. Optional keys: `inventree_version`, `inventree_api` (present whenever the build carries the `internal/buildinfo` baseline constants, but the format itself must not require them, so a well-formed response can validly omit them). Parsers split each line on the first `": "` occurrence (not a fixed line count, and not a naive split on every colon, since `date` is RFC3339 and contains colons itself) and look up known keys by name, tolerating any additional keys a later revision adds. Only an incompatible change to an existing key's meaning requires bumping the porcelain format version.
  - `internal/selfupdate.requireVersion` invokes the candidate as `version --porcelain <N>`, where `N` is always `internal/buildinfo.PorcelainMarker` (the porcelain format version this running build itself understands), and returns its parsed key:value fields (not just `error`, as it did before this story) alongside its existing pass/fail behavior, explicitly checking the response's own `porcelain` marker is a version it understands and failing closed with a clear error otherwise, then separately comparing the parsed `version` field against the target release. `installVerified`'s return type widens to carry the pre-rename staged candidate's parsed fields alongside its existing backup-path return, and `Updater.Run`'s `Result` gains fields for the previous and new InvenTree version/API (exact naming decided during implementation) populated from the already-running process's own compiled-in constants (previous) and the pre-rename staged-candidate parse `requireVersion` already performed (new) — no second exec of the candidate.
  - This is a deliberate, one-time breaking change accepted by the operator: any binary already released (`v0.0.1`–`v0.0.11` and any that follow before this story ships) has a compiled-in `requireVersion` that still invokes bare `version` (no `--porcelain` flag) and expects the old exact-3-line shape in response; the candidate's bare `version` now prints the new human-readable text instead, which the old parser also rejects as "malformed version output," refusing to self-update into the first release containing this change. That specific release must be installed manually; every release after it self-updates normally on the new versioned contract. This is documented prominently in that release's notes, in AGENTS.md's Release Workflow section, in `docs/self-update.md`, and in the "Local CLI self-update" recipe in `docs/operator-recipes.md`, as an explicit, expected one-time migration step, not a defect.
  - `self-update`'s result summary reports the old-to-new InvenTree baseline alongside the old-to-new MCP version, reusing the `Result` fields above; when the new-side fields are legitimately absent from an otherwise-valid response (or a downgrade target predates `internal/buildinfo`'s constants), `self-update` completes the update and omits the baseline line from its summary rather than failing.
  - Regression tests cover: the new `version` output's key-based format, first-`": "` line splitting (including a case with a colon-containing `date` value), and rejection of an unrecognized/malformed `porcelain` marker; `version`'s default human-readable output, `--porcelain <N>` accepting the supported format version and rejecting an unsupported one without stdout output, and rejecting a stray positional argument; `requireVersion` accepting a well-formed response both with and without the optional InvenTree fields, and rejecting a malformed or unsupported one; the exact `--porcelain <N>` invocation `self-update` sends to a staged candidate; `self-update` surfacing the old-to-new baseline when present and omitting it gracefully when the new-side fields are legitimately absent. A separate, explicitly-labeled test documents (rather than silently drops) the accepted one-time break by re-implementing a minimal copy of the old fixed-3-line parsing logic inline in the test itself, since the production code is being replaced rather than kept, and asserting that logic would reject the new format; this test carries a comment noting it should be revisited or removed at the `v1.0.0` boundary AGENTS.md's Release Workflow already treats as a compatibility milestone.
- Residual risk: the `version` output's format change means self-update from any pre-this-story binary into the first release containing it will fail and require a manual reinstall; this is accepted now because the operator is the only current user, but it must not recur casually after `v1.0.0`, at which point the same porcelain version marker should be used to negotiate any future breaking change instead of repeating an undocumented break. The InvenTree-baseline fields are optional in the porcelain format specifically so a genuinely reachable graceful-degradation path exists; this means a future release could in principle omit them without the format itself flagging an error, so their presence is enforced by the separate `internal/testenv` alignment in this story's first acceptance criterion, not by the porcelain schema itself. This session's GitHub App token returned `403 Resource not accessible by integration` when attempting to set issue #160's status/assignee or post a progress comment; `docs/TASKS.md` is the authoritative local status record, but the operator should sync issue #160 (status, assignee, a brief progress note) manually or grant the app broader `issues:write` permission.
- Validation: `go build ./...`; `go vet ./...`; `golangci-lint run ./...` (0 issues); `gofmt -l .` (clean); `git diff --check` (clean); `go test ./...` (all packages pass, including the Testcontainers-backed `internal/testenv` suite against `inventree/inventree:1.5.0`). Per-package coverage compared against `main`: `internal/buildinfo` 100% (matches `main`, after adding a blank-line-skipping `ParsePorcelain` test to close an initial 95.2% gap), `internal/selfupdate` ~78.3% (matches/slightly exceeds `main`'s 78.2%), `internal/testenv` 79.8% (matches `main`), `cmd/inventree-mcp` ~91.2% (matches/slightly exceeds `main`'s 91.1%) — no package-level coverage reduction. Follow-up `--porcelain <N>` flag pivot re-ran `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `gofmt -l .` (clean), `git diff --check` (clean), and `go test ./...`; `cmd/inventree-mcp` coverage moved from ~91.2% to ~92.7% after adding an unknown-flag test found missing by review — no reduction versus either baseline.
- Review: Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager reviews run per `docs/reviewers.md`. Go review found no blocking issues (two informational nits: the CLI baseline line's presence check only gates on `NewInvenTreeVersion`, not also `NewInvenTreeAPI`, unreachable today since both `buildinfo.Pinned*` constants are always non-empty; the optional-field tests exercise both-present/both-absent but not the asymmetric one-field case, also unreachable today). Product review found no blocking issues (one minor: point operators to a concrete manual-install location instead of just saying "install manually"; addressed by referencing README's Install From A Release section in `docs/self-update.md` and `docs/operator-recipes.md`). QA review found one real, high-severity issue: the pre-existing `TestInstallRollsBackRealInstalledSubprocessFailures` real-subprocess test's embedded fixture script emitted the old 3-line format, so it started failing at the new pre-rename porcelain-marker check instead of reaching the post-rename rollback path it exists to exercise, silently losing that integration-level coverage while still reporting green. Fixed by updating the fixture to emit a valid `porcelain: 1` line; confirmed via `-coverprofile` that this restores the test's coverage profile through `install.go`'s post-rename `requireVersion` call to parity with `main`'s own (pre-existing, environment-timing-limited) coverage of that same test, so the fix removes the story-introduced regression without changing pre-existing out-of-scope behavior. No further findings after the fix.
- Follow-up review (`--porcelain <N>` flag pivot): Senior Go Developer, Senior QA / Test Architect, and Senior Product Manager reruns per `docs/reviewers.md`; full-panel/infosec escalation was not warranted because this pivot only changes an already-reviewed subprocess's argument list (`version` → `version --porcelain <N>`), not its sandboxing, trust boundary, or file/network access, and hits none of `docs/reviewers.md`'s Review Timing escalation triggers. Go review found no blocking issues (a low-severity note that `docs/self-update.md` overstated the cross-version resilience the current `--porcelain` negotiation delivers, fixed by clarifying it only establishes the mechanism today; a trivial "format version" vs. "marker" wording mismatch between `cmd/inventree-mcp/main.go` and `internal/buildinfo/porcelain.go`, fixed by aligning both to "marker"). QA review found one real, medium-severity gap: no test covered `version --porcelain` invoked with no trailing value (a Go `flag.String` parse error); added `TestRunVersionPorcelainRequiresValue`. Product review found two medium findings: the Decisions follow-up paragraph explained what changed but not why, fixed by adding the operator's stated rationale (legible interactive output plus a self-documenting, explicit machine-parse request); and this Validation/Review pair had not yet been updated for the follow-up round, addressed in this edit. No further findings after fixes.

Tasks:

- [x] Add the `buildinfo` InvenTree-baseline constants and align `internal/testenv` with them.
- [x] Replace the `version` subcommand's output with the versioned porcelain-style key:value format (required: `porcelain`/`version`/`commit`/`date`; optional: `inventree_version`/`inventree_api`), using first-`": "` line splitting.
- [x] Follow-up (operator-directed mid-implementation): gate the porcelain output behind a `--porcelain <N>` flag requesting an explicit porcelain format version, default `version` to human-readable text, and reject an unsupported requested format version without stdout output.
- [x] Rewrite `internal/selfupdate.requireVersion` to invoke `version --porcelain <N>` with `N` = `internal/buildinfo.PorcelainMarker`, return parsed fields alongside its pass/fail result, widen `installVerified`'s return type, and add the new `Result` fields.
- [x] Update `self-update`'s result reporting to reuse the pre-rename staged-candidate parse for the old-to-new baseline, with graceful-degradation tests when the optional fields are absent.
- [x] Add the explicitly-labeled old-parser-rejects-new-format test with its `v1.0.0`-boundary revisit note.
- [x] Document the one-time manual-reinstall migration in AGENTS.md's Release Workflow section, in `docs/self-update.md`, and in the "Local CLI self-update" recipe in `docs/operator-recipes.md`. The release-notes wording for the first release carrying this change is written when that release is actually tagged, per AGENTS.md's Release Workflow.

### F-S73: Remove Gremlins Mutation-Testing CI Job

- Status: `Done`
- Depends on: none
- Decisions: approved by the operator on 2026-08-16 in chat (no GitHub issue requested). Investigation prompted by the operator noticing the `gremlins` CI job's runtime growing quickly and asked whether it was worth running at all, given neither the operator nor prior agent sessions were reading its output. Findings that drove the decision: (1) actual Go-source growth on the day the runtime jumped from ~2h06m to ~5h10m was negligible (roughly +254 net non-test Go lines across all of that day's merges, against a ~26,000-line codebase); the apparent same-day jump was traced instead to a `gremlins-go` Actions cache eviction (a `Cache not found for input keys` miss despite unchanged `go.mod`/`go.sum`), not codebase size. (2) Across every story in this file and all of `AGENTS.md`, gremlins is never once cited as the source of an actual finding or fix — every reference is only "gremlins passed" alongside lint/test. (3) `.gremlins.yaml` sets no efficacy/mutation-score threshold, so the job could not have failed a PR on weak test coverage even in principle. (4) The `gremlins` context is not among the repository's required branch-protection status checks (`goreleaser-snapshot`, `test`, `lint` only), so removing the job does not affect merge gating. Given the job was costing multiple hours of CI time per merge (it ran the full whole-codebase mutation suite on every push to `main`, not only the weekly schedule) with no evidence it had ever been acted on, the operator directed removing it from CI entirely. The operator asked to keep `.gremlins.yaml` in the repository so mutation testing can still be run manually and on demand with the existing configuration.
- Scope: remove the `gremlins` job from `.github/workflows/go.yml` (both its PR diff-mode and full-suite steps, and its dedicated Go build/module caches). Leave `.gremlins.yaml` in place unchanged. Leave the `no_integration_tests` Go build tag and its `!no_integration_tests` build constraints on Docker-backed integration tests untouched, since that tag is a general-purpose integration-test-skip convention used directly by many `go test -tags no_integration_tests ...` commands throughout this file and is not gremlins-specific.
- Validation: `python3 -m py_compile` was unavailable for YAML, so the workflow was validated with `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/go.yml` (0 issues) and `git diff --check` (clean). Confirmed via `gh api repos/davidvanlaatum/inventree-mcp/rules/branches/main` that `gremlins` is not a required status check before removing it, so no branch-protection follow-up is needed.
- Review: Senior QA / Test Architect and Senior Product Manager reviews run per `docs/reviewers.md`, since this is an operator-facing CI/workflow change with no code, auth, tool-surface, or Testcontainers-architecture change and therefore does not hit any full-panel trigger in `docs/reviewers.md`'s Review Timing section. QA confirmed the workflow removal is clean (no orphaned `needs`/concurrency/cache references), no other workflow or doc depends on the job, `.gremlins.yaml` and the four `no_integration_tests`-tagged integration test files are untouched, and no unacknowledged coverage/CI-signal gap exists beyond the stated residual risk; no actionable findings. Product confirmed `AGENTS.md` and `README.md` have zero gremlins references so nothing is left dangling or contradicted, the decision rationale is clearly captured for a future reader, and skipping GitHub issue creation is reasonable since no documented/advertised CI guarantee is being broken; no actionable findings.
- Residual risk: mutation-testing signal on new/changed code is no longer automatically produced anywhere; if it is wanted again later, the fastest path is re-adding only the fast PR-diff-mode step (`gremlins --diff=origin/<base>`), which historically ran in ~1-2 minutes, rather than restoring the multi-hour full-suite step. `.gremlins.yaml`'s existing `no_integration_tests` unleash-tag exclusion remains correct if the tool is ever run manually or re-added to CI.

Tasks:

- [x] Remove the `gremlins` job (both PR diff-mode and full-suite steps, and its Go cache steps) from `.github/workflows/go.yml`.
- [x] Keep `.gremlins.yaml` unchanged for optional manual runs.
- [x] Verify `gremlins` is not a required branch-protection status check before removing it.
- [x] Validate the edited workflow with `actionlint` and `git diff --check`.

### F-S74: Guarded Stocktake Generation And Reporting

- Status: `Active`
- Issue: [#193](https://github.com/davidvanlaatum/inventree-mcp/issues/193)
- Depends on: F-S60
- Progress: implementation started on `codex/f-s74-guarded-stocktake-generation-reporting` from local `origin/main` at `c4f4b33` (the F-S60 merge commit). Issue #193 is synchronized and assigned while implementation remains active.
- Progress: the client boundary now has typed `PartStocktakeGenerate`/`DataOutput` models, `GeneratePartStocktake`, and `GetDataOutput` methods with focused HTTP contract coverage; the endpoint manifest records both new paths. The MCP guarded workflow is exposed through `generate_stocktake` and `poll_stocktake_generation`.
- Progress: `generate_stocktake` now validates the selector/flags, binds principal-scoped single-use confirmation, enqueues work, and returns a sanitized `DataOutput` task handle immediately. `poll_stocktake_generation` polls only that existing task for a bounded per-call interval, returning `pending`, `ok`, or `partial_failure` with safe report retrieval. Pinned live characterization accepted two identical same-day requests as distinct task IDs, rejected a non-staff credential with HTTP 403, and observed a combined entry/report task remain `complete:false` at `0/1` with no output after a bounded 30-second poll.
- Scope: implement guarded stocktake generation using exactly one selector (part, category, or location); support independent `generate_entry` and `generate_report` choices; enqueue and poll correlated `DataOutput` tasks; define failure, timeout, duplicate, permission, and retry recovery; retrieve generated snapshots and reports safely, including attachment handling where applicable; keep quantity adjustment and historical stocktake reads separate.
- Acceptance: dry-run and confirmation bind the complete selector/flag plan; enqueue returns a usable task handle; bounded polling reaches verified terminal state without starting duplicate work; duplicate and same-day behavior is characterized and handled explicitly; report/plugin prerequisites and permissions are explicit; unit, integration, security, documentation, and reviewer coverage pass.
- Residual risk: generation is a non-atomic inventory snapshot and may change while processing. The guarded tools fail closed on missing or foreign task handles, task errors, identity mismatch, unsafe report URLs, redirects, and oversized content; `pending` requires the agent to retain and reuse the task ID. Duplicate/same-day, non-staff permission, and bounded terminal/report behavior are characterized, but the pinned combined report task did not complete or expose an artifact within 30 seconds; report production remains an upstream operational dependency. Operators should stop and ask after 90 seconds with no progress increase or new output, resetting that stall window when progress/output changes. The read and report-download requests share the 30-second per-call context deadline, provided the configured client transport honors cancellation; a non-cooperative transport is a client-boundary defect requiring correction, not task regeneration. Retry the same handle only for a transient read/download failure; a completed task without its requested report is terminal and requires worker/report reconciliation before any new generation. An ambiguous enqueue result requires task-queue/history reconciliation before any retry. The full review panel's actionable findings were fixed; the final rerun is verifying the final timeout/capacity follow-up.

Tasks:

- [x] Add typed generation/DataOutput client methods, endpoint-manifest entries, and client contract coverage.
- [x] Add guarded selector/flag planning, single-use confirmation, immediate task-handle enqueue, bounded follow-up polling, and sanitized task output.
- [x] Align tool authorization, generated manifest, tool reference, plan, API notes, and operator recipe.
- [x] Characterize worker-backed duplicate/same-day behavior and non-staff permission boundary.
- [x] Complete worker-backed terminal-state and report-artifact characterization (bounded pinned run remained pending at `0/1` with no artifact after 30 seconds).
- [x] Add safe same-instance report retrieval/attachment handling.
- [x] Complete the full review/coverage pass and resolve the panel findings.

- Validation: focused F-S74 unit/client tests, `go vet ./...`, `git diff --check`, and the worker-backed Docker characterization against pinned InvenTree 1.5.1 passed. The live characterization verified distinct same-day task IDs, explicit non-staff HTTP 403 rejection, and a combined entry/report task remaining nonterminal at `0/1` with no artifact after the bounded poll.
- Review: Full Senior Go Developer, Senior QA / Test Architect, Senior Product Manager, and Senior Infosec Reviewer panel completed with actionable findings fixed across reruns. The final rerun found no remaining findings; the documented residual is that the client transport must honor context cancellation for the 30-second request bound.
