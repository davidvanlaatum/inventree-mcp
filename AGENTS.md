# Agent Instructions

- If comments are unclear or require a product/workflow decision, report the specific question instead of guessing.
- Keep `docs/PLAN.md`, `docs/TASKS.md`, `docs/api-schema.md`, tool reference docs, and operator recipes aligned with behavior changes.
- Use `docs/reviewers.md` as the standard reviewer roster for future review passes and keep it aligned when reviewer responsibilities change.

## Task Workflow

When picking up an implementation task from `docs/TASKS.md`:

- Read the task, its dependencies, acceptance criteria, and current status before editing code.
- Review all applicable docs before planning: at minimum `docs/PLAN.md`, `docs/TASKS.md`, `docs/api-schema.md` when API behavior is involved, `docs/tool-reference.md` when tool behavior changes, `docs/operator-recipes.md` when operator workflow changes, and `docs/reviewers.md` when a review pass is needed.
- After reviewing the task and applicable docs, identify any unclear requirements, conflicting docs, missing product/workflow decisions, or unsafe assumptions. Ask the operator the specific questions before building the implementation plan or editing files. If there are no blocking questions, state the assumptions the plan will use.
- If the task status is `Blocked`, do not implement around the blocker. Resolve the blocker, ask the operator the specific question, or update the task with why it remains blocked.
- Build a short implementation plan that maps the task acceptance criteria to concrete files, tests, and documentation updates.
- Before implementation, start from `main` unless the operator explicitly specifies another base branch. Verify `main` is up to date with its remote so the feature branch starts from a known good and current state.
- Create or switch to a feature branch for the task unless the operator explicitly asks to work on the current branch. Use a descriptive branch name, preferably with the `codex/` prefix.
- Implement in small, reviewable steps. Keep docs, task status, tool references, recipes, and agent instructions aligned with behavior changes in the same change.
- Run the relevant validation for the task. At minimum run unit tests or targeted package tests for code changes and `git diff --check` for documentation-only changes.
- Run subagent review after implementation for every `docs/TASKS.md` implementation story that changes code, behavior, task status to `Done`, operator workflow, or public documentation contracts. Use the applicable roles from `docs/reviewers.md`; at minimum use Senior Go Developer for Go code changes, Senior QA / Test Architect for test or validation changes, and Senior Product Manager for operator-facing workflow or task-contract changes. Use the full Go, QA, product, and infosec panel for auth, upload, Testcontainers, tool-surface, or milestone-completion changes. Manual-only review is acceptable only for typo-only or formatting-only documentation edits; record why subagent review was not required.
- Give review subagents read-only access to the workspace whenever the tooling supports it so they can inspect surrounding code, docs, and constraints without writing files. If read-only access is unavailable and a forked workspace is used, explicitly instruct reviewers not to edit files, verify the parent checkout after they return, and treat any unexpected subagent edits as untrusted until independently inspected, validated, and reviewed. Diff-only review is a fallback for narrow follow-ups, not the default.
- Address actionable review feedback with code, tests, or docs. If feedback is rejected, document the reason and residual risk in the relevant task's `Review` or `Residual risk` note.
- After addressing PR or subagent review feedback, rerun the applicable reviewer roles before final handoff whenever the follow-up changes code, tests, behavior, operator workflow, or public documentation contracts. Keep the rerun focused on the follow-up diff. Do not rerun subagents for typo-only or formatting-only documentation follow-ups; record why rerun review was not required.
- Repeat validation and review until there are no unresolved actionable findings for the task scope.
- Update `docs/TASKS.md` task status, checkboxes, `Validation`, `Review`, and `Residual risk` notes as part of completion. Mark a task `Done` only when its acceptance criteria are met, tests/docs are updated, and review feedback is resolved or explicitly documented.
- Commit completed task work on the feature branch with a focused message. Do not commit directly to `main` unless the operator explicitly asks for that workflow.
- When updating an already-pushed branch or existing PR, prefer fresh follow-up commits over amending or force-pushing. Rewrite published history only when the operator explicitly asks for it or when a concrete repository hygiene issue requires it, and use `--force-with-lease` if a rewrite is unavoidable.
- Add appropriate commit trailers when committing or merging PRs. Use `Task: <task-id or docs/TASKS.md section>`, `Validation: <command/result>`, `Review: <subagent/manual review summary or none>`, and `Risk: <residual risk or none>` for task work. Add `AI-Assisted-by: Codex` for commits materially authored or edited by Codex. Keep trailers as one contiguous final block with no blank lines between trailer lines, and verify them with `git interpret-trailers --parse` when adding or changing trailer policy. Use standard identity trailers such as `Co-authored-by: <name> <email>`, `Reviewed-by: <name> <email>`, or `Signed-off-by: <name> <email>` only for real identities where the name and email are accurate. Add issue trailers such as `Fixes: #<issue>` or `Refs: #<issue>` only when they are accurate and useful.
- Push the feature branch and create a merge request or pull request with appropriate details: task ID/title, scope summary, validation commands and results, subagent review summary, unresolved questions, residual risks, and any follow-up tasks. For GitHub-hosted repos, treat this as a pull request even if the operator says merge request.
- When merging PRs, prefer squash merge unless the operator or repository policy explicitly requires another merge strategy.
- After pushing, monitor CI until it passes or produces a concrete failure. If CI fails, inspect the failure, fix actionable issues in the branch, and repeat validation/review as needed. If the failure is transient and cannot be made deterministic by adjusting the tests, retry the failing job and record the evidence.
- Do not widen into `Future` tasks unless the operator explicitly changes the plan.

## Technical Rules

- Verify endpoint behavior against `docs/api-schema.yaml` before implementing or changing InvenTree API calls.
- When `docs/api-schema.yaml` changes, update `docs/api-schema.md` provenance and capability notes in the same change.
- Prefer PATCH for partial updates where the schema supports it, and preserve omitted fields versus explicit zero/false/empty/null values.
- Prefer existing InvenTree records over creating new ones, especially parameter templates, category parameter templates, categories, companies, and locations.
- For parameter entry, search existing `/api/parameter/template/`, `/api/parameter/`, and `/api/part/category/parameters/` data first. If the right parameter is ambiguous, ask the operator instead of creating a new template.
- STDIO mode uses the configured InvenTree token from `INVENTREE_TOKEN`; non-secret connection settings may come from environment or flags, including `Token` and `Bearer` upstream auth schemes.
- HTTP mode must use MCP-owned OAuth bearer tokens for ChatGPT Developer Connector compatibility. Do not pass raw inbound InvenTree `Authorization: Token ...` or `Authorization: Bearer ...` headers through unchanged.
- HTTP OAuth tokens must be encrypted, authenticated envelopes containing the upstream InvenTree credential. ChatGPT must only see opaque OAuth bearer tokens, never readable InvenTree credentials.
- HTTP mode keeps access and refresh tokens as sealed envelopes where feasible. Do not add a database-backed access-token mapping unless the plan is explicitly changed. Authorization codes must still be one-time-use with bounded code ID storage before beta.
- Do not use plaintext signed JWTs for HTTP OAuth access or refresh tokens. If a JWT-family token is required, use an encrypted JWE-style profile and document the decision.
- Spike the official MCP Go SDK `auth` and `oauthex` packages before adding parallel OAuth plumbing. Prefer maintained libraries for OAuth authorization-server behavior and keep them behind `internal/oauth` interfaces. Do not hand-roll protocol details such as PKCE validation unless the SDK/library cannot cover them.
- Resolve ChatGPT Developer Connector redirect URI, client registration, metadata, and local/dev callback behavior from current official OpenAI documentation before implementing HTTP OAuth.
- Production HTTP mode is expected to sit behind a reverse proxy that terminates HTTPS. Use explicitly configured canonical public HTTPS issuer/resource URLs and trusted-proxy configuration; do not derive OAuth URLs from untrusted `Host` or forwarded headers.
- Do not publish the internal Go HTTP listener directly in production; expose it only to the trusted reverse proxy or private service network.
- Define and enforce OAuth scopes for tool classes before handlers run.
- Treat OAuth scopes as additive and least-privilege; `inventree.write` does not imply upload, operational inventory access, or destructive access.
- Envelope keys must have explicit key IDs, active/decrypt-only rotation states, startup validation, and documented compromise response.
- Use Afero directly for local file access unless a concrete issue justifies a small helper. HTTP mode must not read arbitrary local paths.
- STDIO local path reads must centralize direct-Afero logic in `internal/upload/local_file.go`, canonicalize allowlisted roots, reject symlinks where supported, reject non-regular files after open, and enforce that cleaned/resolved paths remain under the allowlist.
- Use `github.com/davidvanlaatum/dvgoutils` where appropriate. At minimum, use `github.com/davidvanlaatum/dvgoutils/logging` for context-carried `slog` loggers: seed contexts with `logging.WithLogger`, read loggers with `logging.FromContext`, derive child loggers with attributes using `logger.With(...)`, and attach errors with `logging.Err`.
- Use `github.com/davidvanlaatum/dvgoutils/logging/testhandler.SetupTestHandler` for tests that need a logger in context; after deriving a child logger with `logging.WithLogger`, fetch it again with `logging.FromContext(ctx)` so scoped attributes are present.
- Use `github.com/stretchr/testify` for test assertions. Prefer assertion objects such as `r := require.New(t)` and `a := assert.New(t)` over package-level free functions. Use `require` for checks that must stop the test and `assert` when collecting multiple related failures is useful. When mocks become necessary, use `mockery` where it fits the interface under test.
- Use `dvgoutils.Ptr` for pointer values such as explicit false MCP `destructiveHint` and `openWorldHint` annotation fields where it improves clarity. Do not require JSON emission of false `readOnlyHint` or `idempotentHint` values when the SDK models them as non-pointer `omitempty` booleans.
- Inject clock, randomness/ID generation, HTTP transports, and URL fetchers where needed so tests can be deterministic and can assert redaction and safety behavior.
- Do not log auth tokens, uploaded file contents, or sensitive operator data.
- Attachment and part-image downloads must fetch only schema-exposed file/thumbnail or readable `Part.image` URLs belonging to the configured InvenTree instance, enforce maximum size/bounded reads, revalidate or block redirects, reject out-of-scope attachment model types before content fetch, and never log downloaded bytes, image bytes, or sensitive URLs.
- `upload_attachment` may accept inline byte blobs and STDIO-mode allowlisted local paths. Only the dedicated URL-upload tool may accept HTTP(S) URLs. HTTP mode must not read arbitrary local paths.
- URL upload code must enforce SSRF controls and must not forward MCP or InvenTree auth headers to fetched URLs.
- Link attachments must not fetch remote bytes and should default to HTTP(S) links without credentials/userinfo.
- STDIO local file uploads must reject symlink escapes and non-regular files.
- Blocking Testcontainers integration tests should use an explicit InvenTree version tag, not a digest or floating tag, so the pinned version is readable in config and logs. Latest `inventree/inventree:stable` belongs in a non-blocking canary until schema/provenance updates are applied.
- Testcontainers integration tests should share a suite-owned container set, use immutable shared fixtures, and create prefixed records for every mutating subtest.
- Keep GitHub Actions CI, Dependabot, `.pre-commit-config.yaml`, and `.golangci.yml` aligned with the Go module as implementation is added.
- Keep sales/customer workflows out of the initial implementation unless the plan is explicitly changed.
