# Reviewers

Use these reviewer roles when `AGENTS.md` and [TASKS.md](TASKS.md) require a review pass. This file defines reviewer responsibilities and output shape; it does not narrow the task workflow's review requirement. Reviewers should return findings ordered by severity, with concrete document paths and line references where possible. They should not edit files unless explicitly assigned an implementation task.

Prefer read-only workspace access for review agents so they can inspect surrounding implementation, tests, and documentation without mutating files. If review tooling only supports writable forked workspaces, the reviewer prompt must explicitly say not to edit files, and the parent agent must verify the checkout afterward. Treat unexpected reviewer edits as untrusted until they are independently inspected, validated, and reviewed. Use diff-only review only as a fallback for narrow follow-ups or unavailable workspace access.

## Standard Review Panel

### Senior Go Developer

Focus:

- Go implementation practicality and package boundaries.
- Official MCP Go SDK usage, including transport setup and tool annotations.
- OAuth library fit, especially authorization-code, PKCE, metadata, and token endpoint behavior.
- Official MCP Go SDK `auth` and `oauthex` API usage before introducing custom OAuth plumbing.
- Stateless token and authorization-code feasibility, including any storage boundary required by the selected OAuth library.
- Opaque token envelope feasibility and isolation behind `internal/oauth`.
- Testability seams for filesystem, clock, randomness, HTTP transport, URL fetcher, and context logging via `dvgoutils/logging`.
- Go idioms, dependency injection seams, and concrete spike acceptance criteria.
- Maintainability, dependency risk, and avoiding unnecessary framework spread.

Expected output:

- Findings ordered by severity.
- Suggested wording or implementation-shape changes.
- Risks that should be validated by a spike before implementation.

### Senior QA / Test Architect

Focus:

- Unit, integration, and end-to-end test coverage.
- Testcontainers shared-suite lifecycle, fixture immutability, prefixed records, and parallel subtest isolation.
- OAuth tests for metadata, protected-resource challenge, PKCE, token envelope validation, expiry, audience, scope, refresh, and redaction.
- Upload tests for inline bytes, STDIO allowlisted paths, URL ingestion, link attachments, SSRF controls, and primary-image behavior.
- Schema drift and generated endpoint-manifest checks.
- Deterministic seams such as Afero, clock, randomness, HTTP transports, URL fetchers, and structured log capture via `dvgoutils/logging/testhandler`.
- Whether acceptance criteria are executable and deterministic.
- Whether milestone tests are classified as blocking, non-blocking, or future.

Expected output:

- Findings ordered by severity.
- Missing test cases or unclear acceptance criteria.
- Suggested test organization and fixture strategy.

### Senior Product Manager

Focus:

- Product scope and milestone clarity.
- Common InvenTree data-entry workflows and operator ergonomics.
- ChatGPT Developer Connector OAuth setup experience.
- Unresolved product decisions that are listed but not milestone-gated.
- Structured clarification behavior when the agent should ask the operator instead of guessing.
- Sales/customer boundary enforcement.
- Documentation needed for operators, agent instructions, and tool reference.

Expected output:

- Findings ordered by severity.
- Workflow gaps and operator-facing ambiguity.
- Suggested wording for product decisions, recipes, and milestone gates.

### Senior Infosec Reviewer

Focus:

- OAuth threat model, protected-resource behavior, and authorization-server metadata.
- Authorization-code lifecycle, setup-page CSRF, and stateless replay tradeoffs.
- Canonical public HTTPS issuer/resource URL handling behind a reverse proxy, plus trusted proxy boundaries.
- Token envelope crypto, key management, rotation, and replay limitations.
- Key lifecycle: entropy, storage, rotation, decrypt-only grace windows, and compromise response.
- OAuth scope-to-tool authorization mapping.
- No plaintext JWT/JWS access or refresh tokens; JWE-style tokens only if a JWT-family profile is explicitly justified.
- InvenTree credential sealing, no token leakage, and log/error redaction.
- SSRF, upload, filesystem, and local-path boundaries.
- Link-attachment URL policy distinct from URL-fetch SSRF policy.
- Deployment guardrails, TLS assumptions, and secret handling.

Expected output:

- Findings ordered by severity.
- Security assumptions that need explicit documentation.
- Tests or deployment controls required before release.

## Review Timing

Run the full panel before:

- Changing HTTP auth, OAuth, token envelope, or setup flow behavior.
- Adding new mutating workflow tools.
- Expanding upload, URL fetch, or filesystem access behavior.
- Changing Testcontainers integration architecture.
- Declaring a milestone complete.

For small documentation corrections, use only the roles directly affected by the change.
