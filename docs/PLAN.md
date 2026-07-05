# InvenTree MCP Server Plan

## Goal

Build an InvenTree MCP server in Go using the official Model Context Protocol Go SDK. The server should cover the common InvenTree data-entry paths through both precise low-level tools and safer workflow-level tools. It must run in both STDIO mode and HTTP mode.

## Non-Goals

- Do not implement a full InvenTree UI replacement.
- Do not guess product or workflow decisions when comments or input are ambiguous.
- Do not silently perform destructive or irreversible operations.
- Do not bypass InvenTree permissions; all writes should use the configured or request-provided InvenTree credentials.

## Technology Choices

- Language: Go.
- MCP SDK: `github.com/modelcontextprotocol/go-sdk/mcp`.
- MCP SDK version: reviewed baseline is `github.com/modelcontextprotocol/go-sdk` `v1.6.1`. Pin this or a later current official SDK release only after re-running the MCP transport, auth, and annotation spike because auth middleware and annotation field shapes may change.
- MCP STDIO transport: `mcp.StdioTransport`.
- MCP HTTP transport: `mcp.NewStreamableHTTPHandler`.
- HTTP auth support: implement a ChatGPT Developer Connector-compatible OAuth 2.1 layer owned by the MCP server. HTTP clients authenticate to `/mcp` with MCP-issued OAuth bearer tokens, not raw InvenTree tokens.
- OAuth implementation: first spike the official MCP Go SDK `auth` and `oauthex` packages for protected-resource middleware, bearer-token verification hooks, and metadata handlers. Use a maintained OAuth2/OIDC authorization-server library such as `github.com/ory/fosite` only for authorization-server endpoints the SDK does not provide, after a spike confirms it fits stateless encrypted token envelopes. `golang.org/x/oauth2` is useful for OAuth clients, but should not be treated as sufficient for implementing the authorization server.
- Upstream InvenTree auth header forms: `Authorization: Token <token>` and `Authorization: Bearer <token>`, recovered from encrypted MCP OAuth token envelopes.
- InvenTree API access: small internal REST client using `net/http`, typed request/response structs, pagination helpers, and endpoint-specific methods.
- Shared Go utilities: use `github.com/davidvanlaatum/dvgoutils` where it fits local code style, especially `github.com/davidvanlaatum/dvgoutils/logging` for context-carried `slog` loggers and `logging.Err`.
- Filesystem abstraction: use an injectable filesystem such as `github.com/spf13/afero` for local file access, fixtures, and allowlist tests.
- Integration test infrastructure: `testcontainers-go` module that starts an isolated InvenTree test environment.
- Project automation: GitHub Actions for Go tests with coverage reporting, lint, dependency submission, tag-driven GoReleaser releases, and pre-commit checks; Dependabot for Go modules and GitHub Actions.
- Local quality gate: pre-commit using `pre-commit-hooks` and Go hooks for `go mod tidy`, imports, golangci-lint, tests, and build.
- API schema source: keep a local copy of the OpenAPI schema at `docs/api-schema.yaml`, refreshed from `https://inventory.internal.vanlaatum.id.au/api/schema/` when endpoint behavior needs verification. The current fetched schema is OpenAPI 3.0.3 for InvenTree API version `511`.

## Implementation Libraries and Abstractions

Use established libraries for protocol-heavy or environment-heavy concerns, but keep them behind narrow internal interfaces so tests can swap implementations and future library changes stay localized.

- OAuth server behavior: prefer the official MCP Go SDK `auth` and `oauthex` primitives for protected-resource middleware and metadata where they fit. Prefer `ory/fosite` or an equivalent maintained authorization-server library for authorization-code, PKCE, token endpoint, and refresh behavior not supplied by the SDK. Keep MCP-specific setup UI and InvenTree credential sealing in `internal/oauth`.
- OAuth client behavior: use `golang.org/x/oauth2` only where the server must act as a client to another OAuth provider; do not build the HTTP authorization server around it.
- Token envelope crypto: use Go standard-library primitives where practical, such as AEAD via `crypto/cipher`, and wrap encryption, signing, key lookup, and key rotation behind an `EnvelopeCodec` interface.
- Default envelope profile: use a versioned base64url token containing a clear key ID plus AEAD ciphertext. Prefer AES-256-GCM or XChaCha20-Poly1305 with random nonces, associated data for issuer, audience/resource, client, and token type, and a keyring that supports decrypt-old/encrypt-new rotation.
- JWT/JWS/JWE: do not use plain signed JWT access tokens by default. OAuth does not require JWTs, and readable JWT claims are a poor fit for sealing upstream InvenTree credentials. The default should be an opaque bearer token whose contents are encrypted and authenticated by `EnvelopeCodec`. If a library requires or strongly favors JWT-style tokens, use a JWE or equivalent encrypted-token profile and document why it is safer than the opaque envelope design.
- Filesystem access: use `afero.Fs` for STDIO local uploads, fixture reads, and generated documentation checks. Production should use `afero.NewOsFs`; tests should use memory or temp-backed filesystems.
- Time: inject a small clock interface for token expiry, refresh lifetimes, retry backoff, signed timestamp validation, and Testcontainers readiness polling.
- HTTP transport: inject `*http.Client` or `http.RoundTripper` for the InvenTree client and URL fetcher so tests can use `httptest`, fake transports, and SSRF guard checks without real network access.
- URL fetching: keep DNS resolution, dial policy, redirect policy, proxy behavior, content sniffing, and byte-limit enforcement behind a URL fetcher interface owned by `internal/upload`.
- ID and token generation: inject randomness and ID generation for authorization codes, state, nonces, request IDs, and test determinism. Production must use cryptographically secure randomness for secrets and token material.
- Logging: use `log/slog` with `github.com/davidvanlaatum/dvgoutils/logging` as the standard context logger mechanism. Request, transport, tool, workflow, and client code should get loggers from `logging.FromContext(ctx)`, attach request/tool/object attributes by deriving a child logger with `logger.With(...)`, and pass the child logger via `logging.WithLogger(ctx, logger.With(...))`. Code should fetch the logger from the updated context rather than reusing a logger captured before scoped attributes were attached. Use `logging.Err(err)` for error attributes. The process entrypoint and tests must seed contexts with a logger; missing loggers should fail visibly rather than silently discarding logs.
- Logging tests: use `github.com/davidvanlaatum/dvgoutils/logging/testhandler.SetupTestHandler` for deterministic log capture and redaction assertions where code expects a logger in context.
- Other `dvgoutils` helpers: use `dvgoutils.Ptr` for pointer values such as explicit false tool annotation fields, and use `MapSlice`, `FilterSlice`, or `Must` only where they improve clarity without hiding control flow or error handling.
- Configuration and secrets: keep config parsing separate from runtime dependencies. Key material, InvenTree credentials, and token lifetimes should enter through a typed config object, not scattered environment lookups.
- Schema access: parse `docs/api-schema.yaml` through a schema helper for endpoint-manifest checks instead of ad hoc string matching.

OAuth spike acceptance criteria:

- Prove the official MCP Go SDK protected-resource middleware and metadata handlers can be used with stateless streamable HTTP.
- Prove SDK `TokenInfoFromContext` or the selected private context carrier is visible to `CallTool` handlers under `mcp.NewStreamableHTTPHandler` with `Stateless: true`.
- Verify ChatGPT Developer Connector compatibility from current official OpenAI documentation, including redirect URI format, client registration mode, required metadata fields, supported scopes, and local/dev callback constraints.
- If `fosite` is used, prove configured/static clients, PKCE S256, token endpoint validation, refresh grants, custom opaque token generation, envelope validation, and no persistent access-token store.
- Assume some authorization-code or setup-session storage may be required. Prove whether access and refresh tokens can remain sealed stateless envelopes while authorization codes use only a bounded in-memory or optional external store. Reject any design that requires a persistent access-token lookup table unless the product plan changes.

## Operating Modes

### STDIO Mode

STDIO mode is intended for local MCP clients that launch the server as a subprocess.

Expected command shape:

```sh
inventree-mcp serve --transport stdio
```

Authentication in STDIO mode should come from process configuration:

- `INVENTREE_URL`
- `INVENTREE_TOKEN`
- optional `INVENTREE_AUTH_SCHEME`, defaulting to `Token` for InvenTree API tokens and allowing `Bearer`
- optional `INVENTREE_TIMEOUT`
- optional `INVENTREE_TLS_SKIP_VERIFY`, only for local/test deployments

Production HTTP mode must fail startup if `INVENTREE_TLS_SKIP_VERIFY` or equivalent upstream TLS verification bypass is enabled.

### HTTP Mode

HTTP mode is intended for remote MCP clients using streamable HTTP.

Expected command shape:

```sh
inventree-mcp serve --transport http --listen 127.0.0.1:28686 --path /mcp
```

The default HTTP listen address is `127.0.0.1:28686`: loopback-only for reverse-proxy deployments, outside common HTTP development ports, and below common Linux ephemeral ranges.

Authentication model:

1. HTTP mode is primarily for ChatGPT Developer Connector and other remote MCP clients. It must not accept raw InvenTree credentials as the protected `/mcp` bearer token.
2. The MCP server acts as an OAuth protected resource server for `/mcp`, a lightweight OAuth authorization/token issuer for ChatGPT, and a setup broker for acquiring an InvenTree credential.
3. Protected MCP requests require `Authorization: Bearer <mcp-oauth-access-token>`.
4. The access token is an encrypted, authenticated token envelope. The server decrypts and validates the envelope, verifies issuer, audience/resource, expiry, scopes, token type, `client_id`, key ID/version, and subject, then recovers the embedded upstream InvenTree credential.
5. The recovered InvenTree credential is sent upstream as `Authorization: Token <token>` or `Authorization: Bearer <token>`.
6. ChatGPT sees normal OAuth bearer tokens only. The embedded InvenTree credential must never be exposed as a readable claim, log field, tool error, or resource value.
7. Only OAuth metadata, authorization, token, setup, and health endpoints are public by default. `/mcp` must require a valid MCP OAuth access token before dispatching any MCP method unless the ChatGPT connector compatibility spike proves pre-auth MCP discovery is required. If pre-auth discovery is required, restrict it to static `initialize` or capability data, never include request-specific InvenTree data, and document the exact allowed unauthenticated methods.

HTTP session mode: run streamable HTTP in stateless mode using `mcp.StreamableHTTPOptions{Stateless: true}`. Do not bind a long-lived MCP session to process-global credentials. All InvenTree authorization must be resolved from the current OAuth token envelope.

OAuth discovery and challenge endpoints:

- `/.well-known/oauth-protected-resource` describes the `/mcp` resource.
- `/.well-known/oauth-authorization-server` describes authorization, token, supported grant, PKCE, issuer, and metadata behavior.
- Unauthenticated protected requests return `401` with `WWW-Authenticate: Bearer resource_metadata="<metadata-url>"`.
- The authorization endpoint supports authorization-code flow with PKCE for ChatGPT.
- The token endpoint supports `authorization_code` and `refresh_token` grants.
- ChatGPT redirect URI compatibility must be verified against current official OpenAI documentation during implementation. If the exact redirect URI shape or registration model is not confirmed, keep it as an open product decision instead of guessing.
- Before implementing HTTP OAuth, complete the connector-compatibility spike and record redirect URI format, client registration mode, required metadata fields, supported scopes, and local/dev callback constraints. This is a blocking prerequisite for Phase 2 HTTP OAuth work and the first beta milestone.
- Production deployments are expected to run behind a reverse proxy that terminates HTTPS. The public issuer, authorization, token, redirect, and `/mcp` resource URLs must be configured as HTTPS canonical URLs, even if the Go process receives HTTP from the proxy. Issuer and resource URLs must come from explicit configuration, not untrusted `Host` headers. `X-Forwarded-*` headers may only be used from configured trusted proxies. Metadata, token envelopes, redirects, and audience validation must use the configured canonical URLs exactly. Production deployments must expose the Go HTTP listener only to the trusted reverse proxy or private service network; do not publish the internal HTTP port directly.

OAuth setup flow:

1. ChatGPT starts OAuth authorization.
2. The MCP server presents an MCP-hosted setup/login page.
3. The setup page offers explicit supported credential methods for the first release: paste an existing InvenTree API token, or authenticate to InvenTree only if a schema-verified/browser-verified flow is implemented. It must recommend a dedicated least-privilege InvenTree API token for this connector.
4. The page explains that the MCP server will validate the credential and attempt to create a dedicated connector token where the InvenTree API allows it. MCP OAuth scopes restrict which MCP tools run, but they do not reduce the upstream permissions of the sealed InvenTree credential once a permitted tool calls InvenTree.
5. The setup page binds form submissions to the OAuth authorization request using state/session data, requires CSRF protection, sets no-store cache headers, avoids persisting submitted credentials, and redacts request bodies from access logs, error logs, panic recovery, and audit events. It must set `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY` or equivalent CSP `frame-ancestors 'none'`, a restrictive `Content-Security-Policy`, and secure SameSite cookies for any setup session state. Production HSTS is enforced at the reverse proxy. Setup, authorization, and token endpoints must enforce per-IP and per-client rate limits, maximum request body sizes, context-aware timeouts, and generic error responses for credential validation failures.
6. The MCP server validates the credential with a cheap authenticated endpoint such as `/api/user/me/` or `/api/user/me/roles/`.
7. During setup, create a new dedicated InvenTree API token when the server does not already hold a sealed usable credential for this connector. Do not assume existing token values can be retrieved from InvenTree after creation. Token list/retrieve endpoints may be used for metadata, duplicate detection, or revocation, not for recovering a lost token secret.
8. First beta setup should default to sealing a dedicated connector token. If token creation is unavailable or permission-denied after the operator pasted an existing API token, the setup page must explain the tradeoff and offer two explicit choices: `Use the supplied token for this connector` or `Cancel setup`. The default/recommended action is cancel unless the operator confirms. The resulting credential source must be recorded in non-sensitive setup metadata returned to the operator.
9. The MCP server returns an authorization code, then exchanges it for encrypted OAuth access and refresh token envelopes.

Setup state must be represented either as short-lived encrypted authenticated setup envelopes or cookies containing only non-secret authorization-request state, or as a bounded process-local store. Raw InvenTree credentials must not be stored in browser state. If process-local state is used, document restart behavior, single-instance behavior, and HA limitations. Authorization-code state and setup-page CSRF state must use the same explicit storage or envelope strategy.

Authorization codes, setup state, and OAuth errors containing request identifiers must be treated as sensitive. Redact query strings from access logs for authorization redirects, avoid embedding sensitive values in error descriptions, and use no-referrer headers on setup pages.

Token envelope requirements:

- Use authenticated encryption or a signed-then-encrypted construction with explicit key IDs.
- Treat issued access and refresh tokens as opaque bearer strings from the client's point of view, not as readable JWTs.
- Do not put the upstream InvenTree credential, upstream token scheme, operator identity details, or instance URL in plaintext JWT claims.
- Deployment requires encryption/signing key material, not a database.
- Envelope keys must be supplied through explicit secret configuration or a deployment secret manager. Fail startup if required keys are missing, weak, duplicated across incompatible purposes, or have unsupported algorithms.
- Use key IDs, allow a bounded decrypt-only grace window for old keys, and issue new tokens only with the active key.
- Document key compromise response: rotate keys, invalidate outstanding stateless envelopes encrypted with compromised keys by removing old decrypt keys, and require connector reauthorization if upstream credentials may have leaked.
- Access tokens are short-lived. Initial default: 15 minutes.
- Refresh tokens are longer-lived, distinct from access tokens, and must have `type=refresh`. Initial default: 30 days.
- Connector authorizations have an absolute session lifetime after which refresh stops and ChatGPT must restart setup. Initial default: 90 days.
- Envelopes include token type, issuer, audience/resource, subject/user, `client_id`, scopes, issued-at, expiry, absolute authorization/session expiry where applicable, key ID/token version, and the encrypted upstream InvenTree credential envelope containing scheme and token.
- Include the InvenTree base URL only if multi-instance operation is explicitly supported. Otherwise the base URL comes from server configuration and is not request-controlled.

Authorization-code envelope requirements:

- Authorization codes must be encrypted authenticated envelopes bound to `client_id`, exact `redirect_uri`, PKCE challenge, issuer, audience/resource, setup subject, expiry, and state/nonce where applicable.
- Authorization codes must be one-time-use before beta. Use bounded process-local or optional external storage for authorization code IDs and expiry. Do not ship reusable stateless authorization codes in HTTP mode.
- Do not add a database-backed access-token mapping unless the product plan changes; access and refresh tokens should remain sealed envelopes where feasible.

OAuth scopes:

- Define initial scopes before implementation: `inventree.read`, `inventree.write`, `inventree.upload`, `inventree.operational`, and `inventree.destructive`.
- Scopes are additive and least-privilege. `inventree.write` does not imply `inventree.upload`, `inventree.operational`, or `inventree.destructive`; operationally sensitive stock/order/build tools require `inventree.operational` plus any relevant write/upload scope. Destructive tools require `inventree.destructive` plus any relevant write/upload/operational scope and normal `confirm:true` gates. Read-only tools require only `inventree.read`.
- Tool registration must declare required OAuth scopes alongside MCP mutation annotations.
- The OAuth guard must reject requests with insufficient scopes before invoking handlers. Use global bearer validation only to authenticate and populate request context; enforce tool-specific scopes through a wrapper in `internal/tools` or `internal/server` that checks the tool authorization manifest before dispatch.

Refresh flow:

1. The token endpoint accepts only the `refresh_token` grant for refresh.
2. It decrypts and validates the refresh envelope, including type, issuer, audience/resource, expiry, scopes, `client_id`, and key ID/version.
3. It verifies the embedded InvenTree credential still works with a cheap authenticated endpoint before issuing new tokens.
4. It issues fresh, distinct access and refresh envelopes only until the absolute authorization/session expiry is reached. After that, ChatGPT must restart the OAuth setup flow.
5. Stateless mode cannot provide one-time refresh-token rotation or replay detection without storage. Compensate with shorter lifetimes, key rotation, client/audience binding, and explicit documentation of the replay limitation.
6. Document default lifetimes for access token, refresh token, and maximum connector session age before implementation.

Expected HTTP handler shape:

```go
srv := buildServer(tools.Dependencies{
    ClientFromContext: clientFromContext,
})

handler := mcp.NewStreamableHTTPHandler(
    func(req *http.Request) *mcp.Server {
        return srv
    },
    &mcp.StreamableHTTPOptions{Stateless: true},
)

verifier := oauth.NewTokenVerifier(envelopeCodec, credentialResolver)
httpHandler := auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
    ResourceMetadataURL: configuredProtectedResourceMetadataURL,
})(handler)
```

Prefer the official MCP Go SDK `auth.RequireBearerToken`, `auth.ProtectedResourceMetadataHandler`, `auth.TokenVerifier`, and `auth.TokenInfoFromContext` primitives for protected-resource behavior. `internal/oauth` should provide the SDK token verifier, envelope codec, metadata construction, setup page, and authorization-server endpoints. Only implement custom middleware where the SDK auth package cannot express the required behavior.

For SDK `v1.6.1`, `TokenVerifier` has the shape `func(context.Context, string, *http.Request) (*auth.TokenInfo, error)`. The verifier should decrypt the envelope and return SDK `auth.TokenInfo` with scopes, expiry, subject, and a non-serializable internal credential reference in `Extra`, or a documented private context key if `Extra` is unsuitable. If `Extra` is used, expose it only through a typed `internal/oauth.CredentialFromTokenInfo(*auth.TokenInfo)` accessor with an unexported key/type, and add tests proving the credential object is never serialized or logged. `ClientFromContext` must read exactly one selected carrier inside `CallTool` handlers; do not duplicate credentials into multiple context locations. Tool handlers and resource handlers must resolve credentials from `context.Context`; do not store credentials in server-global state.

## Releases And Packages

Releases are tag-driven through GitHub Actions and GoReleaser. Pushing a `vX.X.X` tag runs `.github/workflows/release.yml`, executes `GOFLAGS=-trimpath go test -v -race ./...`, and publishes a GitHub release with checksums, Linux/macOS/Windows binary archives for `amd64` and `arm64`, and Linux `deb`, `rpm`, and `apk` packages.

The Linux packages install the `inventree-mcp` binary to `/usr/bin`, install `packaging/systemd/inventree-mcp.service` as `inventree-mcp.service`, and install `/etc/inventree-mcp/inventree-mcp.env` as a noreplace configuration file. Package maintainer scripts reload systemd and restart the service only when it is already enabled or active. The `apk` package carries the same files for artifact parity; Alpine/OpenRC service management is not implemented in the first release package.

The packaged service is for HTTP mode behind a reverse proxy. Production HTTP mode remains disabled until OAuth is implemented. The current server runtime can run the skeleton streamable HTTP server in development mode with only static MCP metadata and the read-only health/version tool. Packages can be installed for file layout testing, but the systemd service should not be enabled yet. The unit uses `Type=simple` because the current command does not implement systemd notify or watchdog support. Do not switch the unit to `Type=notify` or add `WatchdogSec` until the Go process sends systemd readiness/watchdog notifications and tests cover that behavior.

## Project Structure

```text
AGENTS.md
cmd/inventree-mcp/
  main.go
docs/
  api-schema.yaml
  api-schema.md
  endpoint-manifest.yaml
  reviewers.md
  tool-reference.md
  operator-recipes.md
internal/config/
  config.go
internal/server/
  server.go
  context.go
  tools.go
  resources.go
  transport_stdio.go
  transport_http.go
internal/oauth/
  metadata.go
  authorize.go
  token.go
  envelope.go
  setup.go
  pkce.go
internal/inventree/
  client.go
  auth.go
  errors.go
  pagination.go
  attachments.go
  part.go
  stock.go
  company.go
  bom.go
  purchase_order.go
  build_order.go
internal/upload/
  sources.go
  base64.go
  local_file.go
  url.go
internal/platform/
  clock.go
  ids.go
internal/tools/
  common.go
  annotations.go
  part_tools.go
  stock_tools.go
  company_tools.go
  attachment_tools.go
  bom_tools.go
  purchasing_tools.go
  build_tools.go
  import_tools.go
internal/workflows/
  catalog.go
  purchasing.go
  build.go
internal/testenv/
  inventree.go
  postgres.go
tests/
  fixtures/
```

## Tool Design Principles

- Prefer explicit structured inputs over free-form strings.
- Support human-readable lookup fields, but fail on ambiguous matches.
- Return InvenTree object IDs and URLs for every created or updated object.
- Make all high-risk write workflows support `dry_run`.
- Do not make irreversible changes without an explicit tool argument such as `confirm: true`.
- Destructive operations are allowed when the InvenTree API supports them, but only behind explicit confirmation and accurate tool annotations.
- Use PATCH for partial updates wherever the InvenTree API supports it, so the AI can provide only changed fields.
- Model update inputs with optional fields or pointer fields so omitted fields are not serialized.
- Mark every tool as read-only or mutating using MCP tool annotations where the official Go SDK supports them.
- For mutating tools, also mark destructive, idempotent, and open-world behavior accurately so clients can auto-prompt correctly.
- Normalize InvenTree validation errors into actionable MCP tool errors.
- For unclear comments, part names, units, categories, locations, order states, or workflow choices, return the specific question instead of guessing.
- Expose missing recommended fields separately from hard validation errors so instance-specific conventions can guide the AI without blocking API-valid writes.
- Prefer existing InvenTree records over creating new ones. This is especially important for parameter templates, category parameter templates, companies, locations, and categories.
- When a suitable existing parameter/template is unclear, return a structured clarification question instead of creating a new template or inventing a field.

## Architecture Boundaries

- `internal/inventree` owns low-level REST endpoint methods, request construction, upstream auth header injection, pagination, PATCH helpers, and error mapping.
- `internal/server` owns MCP server construction, transport selection, request context setup, logging, and client factory wiring.
- `internal/oauth` owns HTTP OAuth metadata, protected-resource challenges, authorization-code and PKCE handling, token endpoint grants, encrypted envelope creation/validation, and setup-page credential exchange.
- `internal/tools` owns MCP tool registration, input/output schemas, annotations, and thin handler glue.
- `internal/workflows` owns multi-step planning, dry-run behavior, confirmation gates, ambiguity handling, and business workflow orchestration.
- `internal/upload` owns upload source resolution for base64 byte blobs, STDIO allowlisted local files, and URL fetches. It must enforce source-mode policy and SSRF controls before content reaches InvenTree.
- `internal/platform` owns small interfaces and adapters for clock, ID generation, and randomness where a package needs test seams. It may provide constructors or config wiring for `afero.Fs`, but packages should accept `afero.Fs` directly instead of a second filesystem abstraction unless a concrete need appears. Logging should use `dvgoutils/logging` directly rather than a second internal logging abstraction.
- Tool handlers should depend on a narrow `inventree.Client` interface or domain-specific interfaces, not concrete HTTP client construction. This keeps STDIO and HTTP OAuth credential resolution in the server layer and makes tool tests cheap.
- `internal/server` should construct dependencies such as `tools.Dependencies{ClientFromContext func(context.Context) (inventree.Client, error)}` and call `tools.Register(mcpServer, deps)`.
- Tool-specific OAuth scope enforcement belongs in `internal/tools` or `internal/server` as a handler wrapper generated from the tool authorization manifest. `auth.RequireBearerToken` should not be treated as sufficient for per-tool authorization because it only validates the bearer token and populates context.
- `internal/tools` and `internal/workflows` must not import `internal/server`; dependencies should flow inward through interfaces.
- `internal/tools` may call `internal/workflows` through constructors or interfaces. `internal/workflows` should depend only on domain client interfaces and upload source interfaces, not HTTP clients or transport state.

## Tool Mutation Classification

Every tool registration must include explicit behavior metadata using SDK-native MCP tool annotations, including `ReadOnlyHint`, `DestructiveHint`, `IdempotentHint`, and `OpenWorldHint` where applicable. Keep a local classification table only as the source for registering and testing annotations, not as a replacement for SDK metadata.

For the reviewed SDK baseline `v1.6.1`, `DestructiveHint` and `OpenWorldHint` are pointer booleans, while `ReadOnlyHint` and `IdempotentHint` are plain booleans with `omitempty`. Annotation helpers must set explicit false pointer values for `destructiveHint:false` and `openWorldHint:false` where appropriate. Tests for `readOnlyHint:false` and `idempotentHint:false` should assert local classification and registration behavior, not require JSON emission the SDK cannot produce.

`openWorldHint` must be decided per tool. In particular, `upload_attachment_from_url` is open-world because it fetches caller-provided URLs, while `upload_attachment` is not open-world when it only accepts inline bytes or STDIO allowlisted local files.

Read-only tools:

- `search_parts`
- `get_part`
- `search_part_categories`
- `search_companies`
- `search_manufacturers`
- `search_suppliers`
- `search_stock_locations`
- `search_stock_items`
- `search_parameter_templates`
- `get_part_parameters`
- `list_attachments`
- `get_attachment_metadata`
- `preview_purchase_order_with_lines`
- `get_bom`
- `search_purchase_orders`
- `search_build_orders`
- `validate_bom`

Mutating non-destructive tools:

- `create_part`
- `update_part`
- `set_part_parameters`
- `create_part_category`
- `create_manufacturer_part`
- `create_supplier_part`
- `upsert_part_with_supplier_and_manufacturer`
- `create_company`
- `update_company`
- `create_contact`
- `create_address`
- `link_supplier_to_part`
- `link_manufacturer_to_part`
- `create_stock_location`
- `create_stock_item`
- `move_stock_item`
- `add_stock_note`
- `upload_attachment`
- `upload_attachment_from_url`
- `create_link_attachment`
- `update_attachment_metadata`
- `set_primary_image`
- `create_purchase_order`
- `add_purchase_order_line`
- `update_purchase_order_line`
- `create_purchase_order_with_lines`
- `create_build_order`
- `import_parts`
- `import_supplier_parts`
- `import_stock_items`
- `import_bom_rows`
- `import_purchase_order_rows`

Mutating operationally sensitive tools:

- `adjust_stock_quantity`
- `set_stock_status`
- `stocktake_adjustment`
- `add_bom_item`
- `update_bom_item`
- `receive_purchase_order_items`
- `allocate_build_stock`
- `issue_build_outputs_to_stock`

Mutating destructive or irreversible tools:

- `remove_bom_item`
- `delete_attachment`
- `close_purchase_order`
- `complete_build_order`

Destructive or irreversible tools should require `confirm: true` and should expose `dry_run` where the workflow can be planned safely.

Classification tests should fail if a tool appears in conflicting categories unless the annotation model explicitly represents multiple facets. Operationally sensitive tools may still be non-destructive, but they must be treated as inventory-affecting and require stronger prompting/audit behavior.

## Common Data-Entry Coverage

### Discovery and Lookup Tools

These tools help agents find stable IDs before writing data.

- `search_parts`
- `get_part`
- `search_part_categories`
- `search_companies`
- `search_manufacturers`
- `search_suppliers`
- `search_stock_locations`
- `search_stock_items`
- `search_parameter_templates`
- `get_part_parameters`
- `get_bom`
- `search_purchase_orders`
- `search_build_orders`
- `list_attachments`
- `get_attachment_metadata`

### Part and Catalog Tools

- `create_part`
- `update_part`
- `set_part_parameters`
- `create_part_category`
- `create_manufacturer_part`
- `create_supplier_part`
- `upsert_part_with_supplier_and_manufacturer`

Important behaviors:

- Support IPN/SKU/name/category lookup.
- Support variant/template relationships if enabled by the InvenTree instance.
- Validate units, category, default location, and supplier/manufacturer references before write.
- `update_part` should use PATCH and serialize only supplied fields.
- Return recommended-but-missing field warnings for conventions such as IPN format, units, revision, default location, purchaseability, assembly flags, templates, and custom parameters when they can be detected.
- `set_part_parameters` should search `/api/parameter/template/`, `/api/parameter/`, and `/api/part/category/parameters/` first and reuse matching enabled templates where possible. Do not blindly create parameter templates from natural language.
- If multiple parameter templates could match by name, units, choices, checkbox state, or category association, ask the operator which existing template to use.
- Candidate ranking should prefer enabled category-linked templates with matching name and units, then enabled global templates with matching name and units, then name-only matches. Disabled templates should be reported but not selected automatically.
- Clarification candidates should include template ID, name, units, choices, checkbox state, category association, existing value if present, and URL.

### Company Tools

- `create_company`
- `update_company`
- `create_contact`
- `create_address`
- `link_supplier_to_part`
- `link_manufacturer_to_part`

Important behaviors:

- Treat supplier and manufacturer roles explicitly. Do not add customer-specific assumptions while sales is out of scope.
- `search_companies`, `search_suppliers`, and `search_manufacturers` should operate on the same InvenTree Company model with explicit role filters.
- `create_contact` and `create_address` are included only for supplier/manufacturer operational data needed by catalog and purchasing workflows. They must not introduce customer-role defaults, sales contacts, billing workflows, or CRM-style customer management in milestone 1.
- Fail if an existing company match is ambiguous.
- `update_company` should use PATCH and serialize only supplied fields.

### Stock Tools

- `create_stock_location`
- `create_stock_item`
- `adjust_stock_quantity`
- `move_stock_item`
- `add_stock_note`
- `set_stock_status`
- `stocktake_adjustment`

Important behaviors:

- Always return before/after quantity and location.
- Require explicit confirmation for quantity decreases or scrap/write-off states.
- Support serial/batch metadata where available.
- Stock item metadata/status updates should use PATCH when the API supports it.

### Attachment and Image Tools

Attachment support should cover files and images where the InvenTree API exposes upload, metadata update, primary image, and delete behavior for supported object types.

The fetched schema facts and attachment/image capability table live in `docs/api-schema.md`. Treat that document as authoritative for endpoint paths, supported object types, upload fields, PATCH support, primary-image fields, and sales-scope exclusions.

- `list_attachments`
- `get_attachment_metadata`
- `download_attachment`
- `download_part_image`
- `upload_attachment`
- `upload_attachment_from_url`
- `create_link_attachment`
- `update_attachment_metadata`
- `set_primary_image`
- `delete_attachment`

Important behaviors:

- For milestone 1, expose attachment tools only for `part`, `stockitem`, `company`, `supplierpart`, `manufacturerpart`, and existing `purchaseorder` records. Build, transfer, return, sales, and BOM-related attachment workflows are deferred even if the generic attachment schema can represent them.
- Support image uploads for object types that expose image fields or attachment-backed images.
- Support attachment download through `download_attachment` using a stable attachment ID. It is read-only, requires `inventree.read`, must resolve attachment metadata first, must reject metadata whose `model_type` is outside the milestone attachment object allowlist before fetching bytes, and must fetch only schema-supported attachment or thumbnail URLs belonging to the configured InvenTree instance. It must not fetch arbitrary caller-provided URLs and must not use the URL-upload fetcher.
- `download_attachment` should default to the original `attachment` file URL when present. Thumbnail retrieval should require an explicit thumbnail mode so original-byte hash tests remain deterministic. Return filename, content type when known, size, SHA-256 hash, selected download mode, and content as base64 for binary files or optionally text for allowlisted textual content types. Apply a configured maximum download size and return a structured error when content is too large.
- Support primary part image download through `download_part_image` using a stable part ID. It is read-only, requires `inventree.read`, resolves only the readable schema-exposed `Part.image` field or part thumbnail endpoint for that part, and applies the same configured-instance, maximum-size, bounded-read, hash, selected-mode, and redaction controls as `download_attachment`. `Part.existing_image` is write-only and must be treated as assignment/update input only, not as a download source.
- `download_part_image` should return a structured no-image result when the part has no primary image. If the part image is backed by a generic attachment and the caller already has the attachment ID, `download_attachment` may be used instead.
- Treat the part thumbnail API as part of the primary part image implementation. Verify `/api/part/{id}/` and `/api/part/thumbs/{id}/` behavior before implementing `set_primary_image`; keep the public MCP tool contract stable even if the client endpoint differs by InvenTree version.
- Keep notes image upload, generated report attachments, stock test-result attachments, and other app-specific file surfaces out of the first release unless the plan is explicitly changed.
- Define upload input forms explicitly:
  - `upload_attachment` accepts inline byte blobs encoded as base64 in HTTP and STDIO mode, with required filename and content type.
  - `upload_attachment` may additionally accept local file paths in STDIO mode only when a configured allowlist permits the path.
  - `upload_attachment_from_url` is the only tool that accepts HTTP(S) URLs.
- `create_link_attachment` creates an InvenTree link attachment without fetching remote bytes.
  - HTTP mode must not read arbitrary server-local paths supplied by a client.
  - URL fetching must reject non-HTTP(S) schemes, local file URLs, and responses that exceed the configured maximum size.
- Use PATCH for attachment metadata and image-field updates where supported.
- Treat file upload as mutating non-destructive, metadata/image changes as mutating non-destructive, and attachment delete as destructive.
- Treat URL upload as open-world. `upload_attachment_from_url` must have `openWorldHint:true`; ordinary byte/local-path `upload_attachment` should not inherit that hint.
- Treat link attachments as mutating non-destructive. They store the URL in InvenTree's `link` field and do not fetch content.
- `create_link_attachment` must validate allowed URL schemes and may optionally apply a separate link allowlist policy. It must not fetch the URL. Operator-facing responses should make clear that link attachments are stored references, not uploaded files.
- Require `confirm: true` for deletes and for replacing an existing primary image.
- Validate content type, filename, size, upload source, and target object before upload.
- Enforce configured maximum attachment size before buffering the entire file in memory.
- Return attachment ID, object type, object ID, filename, content type, size, URL, and whether the uploaded image became primary.
- Attachment list and metadata responses should include stable attachment ID, filename, comment, tags, file size, target object, image/file/link classification, thumbnail URL when present, and primary-image state when applicable.
- Attachment and part-image download responses must not log file contents, auth tokens, attachment bytes, image bytes, or sensitive URLs. Downloading a stored link attachment should return link metadata only unless a future explicit link-fetch feature is added.
- Download redirects must be disabled or revalidated on every hop against the configured InvenTree base URL. InvenTree auth headers must never be sent to an off-instance redirect target.
- Duplicate filename/content handling must be explicit: if the target object already has a matching attachment and the caller did not provide `attachment_id`, replacement intent, or metadata-only intent, return a structured clarification. Metadata updates require a stable attachment ID. Replacing an existing primary image requires `confirm:true`.
- Do not infer image meaning or choose a primary image when multiple plausible images are supplied; return a structured clarification question.
- Do not infer part identity, revision, compliance status, manufacturer part number, or supplier SKU from uploaded images or datasheets unless the operator confirms the extracted value.
- Tests should avoid committing large binary fixtures. Use tiny generated PNG/text/PDF fixtures in test code or small files under `tests/fixtures`.

URL upload safety:

- URL fetching belongs in `internal/upload`, not `internal/inventree`.
- The InvenTree client should only receive already-resolved byte streams and metadata for multipart upload.
- Resolve hostnames before each request and redirect.
- Reject loopback, private, link-local, multicast, and cloud metadata IP ranges by default.
- Do not forward inbound MCP or InvenTree auth headers to fetched URLs.
- Cap redirects and re-apply DNS/IP checks after every redirect.
- Allow private or internal URL targets only through an explicit upload URL allowlist.
- Upload URL allowlist entries must match normalized scheme, IDNA/punycode-normalized host, and explicit port policy. Reject userinfo URLs, wildcard suffix rules that can match attacker-controlled parent domains, and ambiguous default-port behavior. Re-resolve DNS before each request and redirect.
- Block unspecified, loopback, private, link-local, multicast, reserved, documentation/test ranges, CGNAT, IPv4-mapped IPv6 private forms, and known cloud metadata aliases by default.
- Apply timeout, maximum byte, content-type, filename, and extension checks before forwarding content to InvenTree.
- Use a dedicated URL-fetch `http.Client` and `Transport`.
- Do not use ambient proxy settings unless explicitly configured.
- Use a custom `DialContext` that connects only to a vetted IP address and verifies the connected remote address.
- Never forward cookies, MCP auth headers, or InvenTree auth headers to fetched URLs.

URL upload and link attachments are distinct workflows:

- `upload_attachment_from_url` fetches remote bytes and uploads a file attachment to InvenTree.
- `create_link_attachment` stores a URL in the InvenTree attachment `link` field without fetching the URL.
- Link attachment URL policy is separate from URL-fetch SSRF policy because the server does not fetch the link target. By default, allow only `http` and `https`, reject credentials/userinfo, fragments where they are not useful, unsupported schemes, and path-like local file references. Make optional link allowlists visible in operator docs.
- `upload_attachment` must reject HTTP(S) URLs with a clear error directing callers to `upload_attachment_from_url` or `create_link_attachment`, depending on intent.
- When an operator provides a URL with ambiguous intent such as "attach this", return a structured clarification asking whether to upload a copy of the remote file or store a link reference. Do not choose between `upload_attachment_from_url` and `create_link_attachment` automatically unless the caller's intent is explicit.
- Dry-run URL uploads must not fetch remote content. They may validate URL syntax and policy configuration only. Actual URL fetches happen only during confirmed execution of `upload_attachment_from_url`.

Before implementing attachment tools, update the endpoint capability table in `docs/api-schema.md` for each target object type. The table should record the InvenTree endpoint, upload field names, supported methods, whether primary-image behavior exists, whether PATCH is supported, and any object-specific constraints. Tool schemas should only expose object types verified in that table.

### BOM Tools

- `get_bom`
- `add_bom_item`
- `update_bom_item`
- `remove_bom_item`
- `validate_bom`
- `import_bom_rows`

Important behaviors:

- `import_bom_rows` should support dry-run and row-level validation.
- Resolve child parts by IPN/SKU/name using strict ambiguity handling.
- Report missing parts as structured follow-up work rather than creating them automatically unless the caller uses a dedicated create/upsert workflow.
- `update_bom_item` should use PATCH and serialize only supplied fields.

### Purchasing Tools

- `create_purchase_order`
- `add_purchase_order_line`
- `update_purchase_order_line`
- `preview_purchase_order_with_lines`
- `create_purchase_order_with_lines`
- `receive_purchase_order_items`
- `close_purchase_order`

Important behaviors:

- Use supplier-part links when receiving purchasable items.
- Return created stock items and received quantities.
- Require explicit confirmation before closing an order.
- `update_purchase_order_line` should use PATCH and serialize only supplied fields.
- `preview_purchase_order_with_lines` is the milestone dry-run tool. It must be read-only, reject write intent, and perform supplier-part validation without creating a purchase order.
- `create_purchase_order_with_lines` is a later mutating workflow and should not be registered in milestone 1.

### Sales Tools

Sales order tools are intentionally out of scope for now. Do not implement sales tools in the initial server. Keep the internal package layout open enough to add sales later without mixing sales-specific assumptions into stock, company, or part workflows.

### Build and Manufacturing Tools

- `create_build_order`
- `allocate_build_stock`
- `complete_build_order`
- `issue_build_outputs_to_stock`

Important behaviors:

- Validate the BOM before build allocation.
- Return component shortages.
- Require explicit confirmation before completing a build or consuming stock.

### Bulk Import Tools

- `import_parts`
- `import_supplier_parts`
- `import_stock_items`
- `import_bom_rows`
- `import_purchase_order_rows`

Important behaviors:

- Accept structured rows, not raw CSV text.
- Support `dry_run`.
- Return row-level errors with stable row identifiers.
- Make duplicate matching rules explicit in the request.

## Resources and Prompts

Resources can expose read-only, low-risk snapshots:

- `inventree://part/{id}`
- `inventree://stock-item/{id}`
- `inventree://attachment/{id}`
- `inventree://purchase-order/{id}`
- `inventree://build-order/{id}`
- `inventree://bom/{part_id}`

Prompts can encode common operator workflows. Mark each prompt as `milestone_1`, `future`, or `deferred` in the tool reference and generated prompt manifest:

- `new_part_entry_checklist` (`milestone_1`)
- `parameter_reuse_checklist` (`milestone_1`)
- `attachment_image_checklist` (`milestone_1`)
- `initial_stock_entry_checklist` (`milestone_1`)
- `purchase_preview_checklist` (`milestone_1`)
- `receive_purchase_order_checklist` (`future`)
- `bom_import_review` (`future`)
- `stocktake_review` (`future`)

Prompt guardrails:

- Prompts must not invent categories, units, supplier SKUs, manufacturer part numbers, order states, prices, dates, locations, stock status, or quantities.
- Prompts must not infer part identity, revision, compliance status, supplier SKU, or manufacturer part number from uploaded images or datasheets without explicit operator confirmation or reviewed extracted data.
- Prompts must prefer existing parameter templates and category parameters where possible. If the right parameter is unclear, ask the operator to select an existing template or confirm creation of a new template.
- Prompts should prefer structured clarification questions or `dry_run` plans over filling defaults.
- Prompts should distinguish API-required fields from recommended fields and instance-specific conventions.
- Prompts should direct the caller to retry with stable IDs when a lookup is ambiguous.

## Structured Clarification Contract

When a tool cannot proceed safely because input is ambiguous or incomplete, return a structured clarification response instead of guessing. The response should include:

- `question`: the exact question the AI should ask the operator.
- `field`: the field or relationship that is ambiguous or missing.
- `reason`: why the tool cannot safely continue.
- `candidates`: candidate IDs, names, and URLs when a lookup matched multiple objects.
- `retry`: the preferred stable field to provide on retry, such as part ID, category ID, company ID, stock location ID, or supplier-part ID.
- `hard_error`: whether the API would reject the request, as distinct from a recommended-field warning.

Clarification questions should be one decision at a time, include the smallest useful candidate list, prefer stable IDs plus human-readable names, and avoid asking the operator to understand raw API schema names unless no better label exists.

Missing referenced objects should be reported as structured follow-up work. They should not be created implicitly unless the caller invokes an explicit create/upsert workflow.

Attachment and image ambiguity should use the same contract for duplicate filenames on one object, multiple candidate target objects, multiple supplied images, an image already attached to the object, unclear requests such as "make this the photo", and metadata updates where the target attachment is ambiguous. Attachment retry fields should include target object ID, attachment ID, and explicit primary-image confirmation where relevant.

## InvenTree Client Design

The internal client should provide:

- Base URL normalization.
- `INVENTREE_URL` is process configuration only and must not be request-controlled.
- If request-selected InvenTree instances are ever added later, require a default-deny allowlist with normalized scheme/host/port matching and SSRF tests.
- Upstream auth header injection supporting both `Bearer` and `Token` schemes from resolved server configuration or OAuth envelopes.
- Context-aware requests and timeouts.
- JSON request/response handling.
- Pagination helpers.
- PATCH helpers that omit unset fields and preserve zero values when explicitly supplied.
- Typed errors for authentication, authorization, validation, not found, conflict, and server errors.
- Endpoint methods grouped by InvenTree domain.
- Multipart upload helpers for attachment and image endpoints.
- No URL fetching or local file reading. Upload source acquisition belongs in `internal/upload`; the InvenTree client should only send already-resolved content streams and metadata upstream.

STDIO local file access should use `afero.Fs` directly unless a concrete implementation issue proves a small helper is needed. Centralize direct-Afero local upload logic in `internal/upload/local_file.go`: clean the requested path, canonicalize configured allowlist roots and requested paths before open, resolve symlinks where the filesystem exposes symlink metadata, verify the resolved or cleaned path is under an allowlisted root, open it, and reject non-regular files from `File.Stat()`. Unit tests may use Afero memory or temp-backed filesystems; production should use `afero.NewOsFs`. Document residual OS-level time-of-check/time-of-use risk for `OsFs`; do not add a broader filesystem wrapper unless tests expose duplicated or unsafe call sites.

The client should not expose raw HTTP details to tool handlers except where a workflow genuinely needs response metadata.

## HTTP OAuth Design

HTTP mode should use MCP-owned OAuth credentials:

1. The protected `/mcp` endpoint accepts only `Authorization: Bearer <mcp-oauth-access-token>`.
2. The OAuth layer decrypts and validates the access-token envelope before any InvenTree-contacting tool runs.
3. MCP tool handlers build the InvenTree client from the validated envelope context.
4. The InvenTree client sends the recovered upstream credential to InvenTree using `Authorization: Token ...` or `Authorization: Bearer ...`.
5. Missing, malformed, expired, wrong-audience, wrong-scope, wrong-type, or undecryptable MCP OAuth tokens fail before any InvenTree request is attempted.

The MCP server must never pass raw inbound InvenTree `Authorization` headers through unchanged in HTTP mode. Raw InvenTree credentials are only accepted during the setup/authorization step, validated against InvenTree, and then sealed into opaque OAuth token envelopes.

The OAuth layer should treat access and refresh tokens as separate envelope types. Access envelopes can authorize `/mcp` requests. Refresh envelopes can only be used at the token endpoint with the refresh grant.

## Partial Update Design

For update tools, prefer PATCH over PUT wherever InvenTree supports PATCH. The implementation should:

- Define update input structs with pointer fields, nullable wrapper types, or explicit field-set tracking.
- Use `omitempty` only where it does not erase an intentional zero value.
- Preserve the distinction between omitted, empty string, false, zero, and null when the API supports those states.
- Provide endpoint-specific PATCH methods such as `PatchPart`, `PatchCompany`, `PatchStockItem`, `PatchBOMItem`, and `PatchPurchaseOrderLine`.
- Provide attachment and image methods such as `ListAttachments`, `DownloadAttachment`, `DownloadPartImage`, `UploadAttachment`, `PatchAttachment`, `PatchPrimaryImage`, `PatchPartThumbnail`, and `DeleteAttachment` where the API supports them. For current schema version `511`, generic attachment support should map to `/api/attachment/` and `/api/attachment/{id}/`; attachment content download should use the schema-supported `attachment` URL by default or `thumbnail` URL in explicit thumbnail mode, and part-image download should use readable `Part.image` or the part thumbnail endpoint. Both download paths must remain scoped to the configured InvenTree base URL.
- Fall back to full update only when the API lacks PATCH for that endpoint, and document that exception in the tool description.
- Include tests proving that omitted fields are absent from the JSON payload.

Stock movement, purchase receiving, build allocation, and build completion should use endpoint-specific command methods rather than generic PATCH helpers. These methods should perform before/after reads where practical and should not retry non-idempotent writes automatically.

## Safety Controls

- `dry_run` for all workflow tools that perform multiple writes.
- `confirm` for irreversible or operationally significant actions.
- Ambiguous lookup failures instead of first-match behavior.
- Request IDs in logs.
- Structured audit logs for writes without sensitive values.
- No token logging.
- Upstream InvenTree base URL must not be derived from request data.
- Mutating tools should be auditable by method name, object type, object ID, dry-run state, and confirmation state.
- Read operations may retry on transient failures. Non-idempotent writes must not be automatically retried unless the workflow has an idempotency key or performs safe duplicate-detection reads.
- Request timeouts should be explicit and context-aware for both MCP handlers and upstream InvenTree API calls.
- Bulk attachment delete is out of scope initially. If added later, require stricter confirmation, dry-run listing, object/prefix scoping, and destructive annotations.

## Compatibility Decisions

- InvenTree compatibility baseline: InvenTree `1.4.0` for the checked-in schema snapshot.
- Docker image for blocking Testcontainers tests: `inventree/inventree:1.4.0`, matching checked-in `docs/api-schema.yaml` OpenAPI 3.0.3 / API version `511`. Do not use a digest as the primary pin because the version should be clear in config, logs, and failure output. Do not use floating tags such as `stable` for blocking tests.
- Separate stable-canary compatibility job: use `inventree/inventree:stable`, record the resolved InvenTree version and image digest/tag, and report schema drift as non-blocking until the schema/provenance update workflow is run.
- Integration startup should fetch `/api/schema/` and record the API version. Blocking schema-sensitive tests must fail when the runtime schema version differs from checked-in `docs/api-schema.yaml`, unless they run against the recorded image version/schema pair known to match the checked-in schema.
- Schema update workflow: refresh `docs/api-schema.yaml`, update `docs/api-schema.md` provenance and capability tables, update the pinned InvenTree version tag or recorded tag/schema pair, then run the blocking integration suite.
- API schema compatibility baseline: current local `docs/api-schema.yaml` fetched from the internal InvenTree instance, OpenAPI 3.0.3 / API version `511`, with runtime InvenTree version `1.4.0`.
- Upstream InvenTree auth schemes: `Token` and `Bearer` only.
- STDIO auth behavior: read the upstream InvenTree token only from `INVENTREE_TOKEN`. Non-secret connection settings, such as URL, auth scheme, and timeouts, may come from environment or flags.
- HTTP auth behavior: use MCP-owned OAuth bearer tokens with encrypted upstream InvenTree credential envelopes.
- HTTP statelessness: no database-backed access-token mapping is required for the initial implementation. Authorization codes still require bounded one-time-use code ID storage before beta.
- Open product decision: exact ChatGPT Developer Connector client registration and redirect URI shape must be verified from current official OpenAI documentation before implementation.
- Blocking compatibility decision: resolve ChatGPT Developer Connector registration, redirect, metadata, and local/dev callback behavior before starting HTTP OAuth implementation.
- Production deployment assumes HTTPS is terminated by a reverse proxy. The server must be configured with canonical public HTTPS issuer/resource URLs and trusted-proxy configuration for any forwarded headers.
- Required fields: only require fields that the InvenTree API requires, plus fields needed to disambiguate lookups safely.
- Destructive operations: allowed when supported by the API, but gated by `confirm: true`, dry-run where practical, and destructive tool annotations.

## Implementation Phases

### Phase 1: Scaffold

- Create Go module.
- Add command entry point.
- Add config package.
- Add server construction package.
- Add platform adapters for clock, ID generation, and randomness.
- Add logging setup that seeds root contexts with `dvgoutils/logging.WithLogger` and derives request/tool scoped loggers via context.
- Register a health/version tool.
- Wire STDIO transport.
- Wire HTTP streamable transport.
- Configure HTTP as stateless streamable HTTP.
- Add shared tool annotation helpers for read-only, mutating, destructive, and idempotent behavior.
- Add early proof test for request-context or SDK token-info propagation into tool handlers under streamable HTTP stateless mode.

Validation:

- `GOFLAGS=-trimpath go test -race ./...`
- Manual MCP STDIO smoke test.
- Manual HTTP initialize/list-tools smoke test.
- Test that listed tools expose the expected mutation metadata.
- Unit tests proving platform adapters can be replaced with fakes.

### Phase 2: InvenTree Client

- Implement base REST client.
- Implement pagination.
- Implement error mapping.
- Add typed methods for read-only part, company, stock, order, and BOM lookup.
- Add upstream auth header model for `Token` and `Bearer`.
- Add HTTP OAuth metadata, challenge, authorization-code with PKCE, token, refresh, and encrypted envelope components.
- Complete the blocking OAuth spike before implementation: official MCP SDK `auth`/`oauthex` fit, ChatGPT connector compatibility, selected authorization-server library fit, auth-code state strategy, refresh behavior, scope model, and token envelope profile.
- Add PATCH helper support for partial updates.
- Add schema-derived endpoint notes for attachments/images and parameters before implementing write tools.

Validation:

- Unit tests with `httptest.Server`.
- Pagination tests.
- Auth header propagation tests.
- Fake clock tests for OAuth token expiry and refresh windows.
- Fake randomness/ID tests for authorization-code and state generation without weakening production randomness.
- PATCH payload omission tests.
- Schema-reference tests or docs checks proving implemented endpoint paths match `docs/api-schema.yaml` for attachments and parameters.
- Generated endpoint manifest checks should cover every milestone endpoint, including parts, categories, companies, stock, supplier parts, manufacturer parts, purchase preview dependencies, attachments, and parameters.

### Early Testcontainers Foundation

After the REST client core and schema endpoint manifest are in place, build the reusable Testcontainers environment before adding read-only client methods. This gives client and tool implementation tasks a disposable authenticated InvenTree instance for default-on integration coverage as real endpoint behavior becomes useful to verify.

- Add a reusable `internal/testenv` package backed by Testcontainers.
- Prove InvenTree startup, migrations, admin or test-token creation, and readiness polling.
- Pin the blocking integration suite to an explicit InvenTree version tag that matches the checked-in schema snapshot.
- Record the runtime InvenTree version and API version in `docs/api-schema.md` provenance.
- Add the shared-suite fixture and run-prefix model before broad client/tool integration tests depend on it.

Do not let broad workflow happy-path tests depend on the Testcontainers environment until startup, migrations, token creation, fixture seeding, and cleanup are deterministic.

### Phase 3: Discovery Tools

- Add search/get tools across parts, companies, stock locations, stock items, attachments, attachment downloads, part-image downloads, orders, and BOMs.
- Add resource templates for core read-only objects.
- Add parameter discovery tools that search existing `/api/parameter/template/`, `/api/parameter/`, and `/api/part/category/parameters/` data before any parameter write flow.

Validation:

- Unit tests for tool input schemas.
- Mock InvenTree responses.
- Ambiguous lookup tests.

### Phase 4: Basic Write Tools

- Add create/update tools for parts, companies, locations, stock items, parameters, supplier parts, manufacturer parts, attachments, and images. Attachment download remains read-only.
- Add confirmation handling for risky stock changes.
- Parameter writes must prefer existing templates and require explicit confirmation before creating new parameter templates or category-parameter-template links.

Validation:

- Mock write tests.
- InvenTree validation error tests.
- HTTP OAuth challenge, metadata, token-envelope, and protected-resource tests.

### Phase 5: Milestone 1 Workflow Tools

- Implement part upsert workflow.
- Implement parameter reuse workflow using existing templates only unless creation is explicitly confirmed in a separate workflow.
- Implement attachment/image workflows for byte/path upload, URL upload, link attachment, metadata update, primary part image download, and primary part image replacement.
- Implement initial stock creation workflow with duplicate detection.
- Implement purchase-order preview workflow with no writes.

Validation:

- Dry-run tests.
- Partial failure tests.
- Structured clarification tests for duplicate stock, duplicate attachments, ambiguous parameters, and ambiguous supplier/manufacturer links.
- Purchase preview no-write tests.

### Future Workflow Tools

- BOM import workflow.
- Purchase order create/receive workflow.
- Build order create/allocate/complete workflow.
- Stocktake adjustment workflow.

Future workflows require a new product review pass before implementation.

### Testcontainers InvenTree Module

Create a small internal module that starts a disposable InvenTree stack for integration tests. Tests should share one container set per package or suite and run individual cases as isolated subtests instead of starting a full InvenTree stack for each test.

Target API:

```go
func TestIntegration(t *testing.T) {
    env := testenv.SharedInvenTree(t, testenv.Options{
        Image: "inventree/inventree:1.4.0",
    })

    client := inventree.NewClient(inventree.Config{
        BaseURL: env.BaseURL,
        Auth: inventree.Auth{
            Scheme: "Token",
            Token:  env.Token,
        },
    })

    t.Run("create part", func(t *testing.T) {
        t.Parallel()
        run := env.NewRun(t)
        // Use run.Prefix for all created objects.
    })
}
```

Responsibilities:

- Start database dependencies, likely PostgreSQL, with `testcontainers-go`.
- Start the InvenTree services required for realistic API behavior. This may include the server, worker, proxy, and optional Redis/cache depending on the official stable deployment shape.
- Start containers with deterministic admin credentials.
- Run any required InvenTree setup, migrations, or startup commands.
- Create or retrieve API tokens for integration tests, including at least two distinct users/tokens for auth-isolation tests.
- Wait until authenticated API calls work before returning.
- Expose `BaseURL`, `Token`, and cleanup helpers.
- Seed minimal immutable lookup fixtures for categories, locations, companies, parts, supplier parts, and BOMs.
- Share the container set across subtests while ensuring each subtest uses a unique run prefix.
- `SharedInvenTree` must be concurrency-safe, usually via `sync.Once` per package or suite. Parent tests must acquire the environment before parallel subtests start.
- Ensure fixture seeding is idempotent and prefix-isolated for parallel runs.
- Provide helpers for unique names, fixture lookup, and cleanup where cleanup is safe and useful. Prefix format should be deterministic and collision-resistant, for example `IT_<runid>_<pkg>_<test>_`.
- Every mutating helper must take a per-test `Run` object and refuse to create or clean up mutable records without the current run prefix. Suite-level cleanup assertions must fail if destructive cleanup would touch unprefixed or foreign-prefixed records.
- Redact admin password and API token from logs and failure output.

Implementation notes:

- Prefer official InvenTree container images.
- Keep the module internal to avoid committing to a public testing API too early.
- Use fixed fixture names with a unique test run prefix.
- Use a package-level suite root or `TestMain` ownership model for shared environment lifecycle. Avoid first-caller-owned teardown for shared containers.
- Teardown is owned only by `TestMain` or the suite root. No subtest cleanup may stop shared containers.
- Cross-package container sharing is out of scope unless explicitly implemented.
- Validate Testcontainers options before startup and treat them as immutable after the shared environment starts.
- Use `t.Cleanup` only for per-run artifacts when safe, not for tearing down a shared container set that other subtests may still use.
- Design integration tests so subtests can call `t.Parallel()` without sharing mutable InvenTree records unless the test explicitly owns those records.
- Shared fixtures are immutable lookup-only data. Every mutating subtest must create and own its own prefixed records.
- Avoid global mutable test data that would make parallel subtests order-dependent.
- Keep destructive tests scoped to records created by that subtest's unique prefix.
- Keep production credentials and user-provided `INVENTREE_TEST_URL` out of Testcontainers logs.
- If InvenTree requires multiple services for a realistic setup, wrap them behind one `StartInvenTree` helper rather than leaking container wiring into tests.
- Integration tests that require the shared InvenTree stack should live in one package or suite for milestone 1 so `GOFLAGS=-trimpath go test -race ./...` starts at most one shared stack. If additional packages need integration coverage, they should call into the same suite entrypoint or remain unit/fake-client tests until cross-package sharing is deliberately designed.
- Invocation contract: `GOFLAGS=-trimpath go test -race ./...` starts the pinned Testcontainers InvenTree stack by default. Local and CI runs may explicitly exclude Docker-backed integration tests with `INVENTREE_TEST_SKIP_DOCKER=1` or `GOFLAGS=-trimpath go test -race -short`; otherwise missing Docker or failed container startup fails the test.

### Phase 6: Integration Happy Paths

- Add optional integration tests gated by environment variables:
  - `INVENTREE_TEST_URL`
  - `INVENTREE_TEST_TOKEN`
  - `INVENTREE_TEST_ENABLE_WRITES`
- External write tests must refuse to run against `INVENTREE_TEST_URL` unless a separate dangerous opt-in is set and the base URL matches an explicit test allowlist or marker.
- Reuse the early `internal/testenv` Testcontainers package and shared fixture/run-prefix model.
- Ensure tests can run read-only by default where the workflow allows it.
- Ensure write-enabled integration tests run against the disposable Testcontainers InvenTree environment by default, not a shared production-like instance.

### Phase 7: Documentation

- README with install and MCP client configuration. README should contain only quick-start links and minimal examples; `docs/operator-recipes.md` is the source of truth for operator workflows.
- Reviewer roster in `docs/reviewers.md` for repeatable senior Go, QA, product, and infosec review passes.
- Tool reference generated or maintained from Go structs.
- Examples for STDIO and HTTP mode.
- Security notes for STDIO credentials, HTTP OAuth envelope keys, token lifetimes, replay limitations, and deployment.
- Operator recipes for common data entry.
- `docs/operator-recipes.md` must include first-release recipes for ChatGPT connector OAuth setup, STDIO setup, reverse-proxy HTTP deployment, add/update purchasable part, reuse existing parameters, add supplier/manufacturer links, create initial stock, upload/link attachment, set/replace primary part image, preview purchase order lines, and resolve structured clarification prompts.
- The reverse-proxy HTTP deployment recipe must cover canonical public issuer URL, public MCP resource URL, authorization/token endpoint URLs, trusted proxy CIDRs or header policy, and common failure symptoms such as redirect URI mismatch, wrong audience, and internal-host metadata leakage.
- `AGENTS.md` with implementation rules for ambiguity handling, parameter reuse, schema verification, auth safety, Testcontainers isolation, and documentation upkeep.
- `docs/api-schema.md` summarizing the schema source, refresh command, verified endpoint facts, and current schema version.
- Documentation must be updated in the same change as tool-surface, auth, endpoint, test, or workflow behavior changes.

## Testing Strategy

- Unit tests for configuration, client request building, pagination, and error mapping.
- Tool handler tests with fake InvenTree clients.
- Upload source resolver tests for base64 byte blobs, STDIO allowlisted local files, and URL fetches.
- Filesystem abstraction tests proving STDIO local upload behavior works with an in-memory or temp-backed `afero.Fs` and never depends on process-global working directory state.
- Fake clock tests for OAuth access expiry, refresh expiry, setup-code expiry, retry backoff, and Testcontainers readiness deadlines.
- Fake randomness/ID tests proving deterministic unit tests do not require weakening production entropy.
- Fake `http.RoundTripper` tests for InvenTree client request construction, upstream auth headers, retry policy, and error mapping.
- URL fetcher interface tests proving SSRF policy can be tested without real external network access.
- Structured logger tests using `dvgoutils/logging/testhandler` proving auth tokens, OAuth envelopes, uploaded file contents, and sensitive operator data are redacted, and that request/tool attributes attached with `logging.WithLogger` are present on downstream logs.
- HTTP OAuth metadata endpoint tests for `/.well-known/oauth-protected-resource` and `/.well-known/oauth-authorization-server`.
- Metadata tests must assert issuer, authorization endpoint, token endpoint, supported grants, supported PKCE methods, resource identifier, scopes, and no internal host leakage.
- HTTP protected-resource tests proving unauthenticated `/mcp` requests return `401` with the required `WWW-Authenticate` bearer challenge and `resource_metadata` reference.
- Authorization-code and PKCE tests covering code challenge verification, redirect URI validation, state preservation, invalid verifier rejection, expired code rejection, wrong redirect URI rejection, cross-client code rejection, and reused-code rejection.
- Authorization-code tests must prove codes are one-time-use before beta and that bounded code ID storage expires entries.
- Authorization endpoint tests must reject unregistered redirect URIs, scheme/host/path variants, wildcard-like matches, CRLF, userinfo, fragment abuse, and open redirect parameters.
- Token endpoint tests for `authorization_code` and `refresh_token` grants.
- Setup, authorization, and token endpoint tests for rate limiting, maximum body size, timeout behavior, and generic credential-validation failures.
- Token envelope tests proving encryption/decryption, authentication failure on tamper, key ID/version handling, and redaction in errors/logs.
- Token format tests proving access and refresh tokens are opaque to clients, are not plaintext signed JWTs, and do not expose InvenTree credentials or sensitive metadata in decodable claims.
- Key-management tests proving startup fails for missing, weak, duplicated, or unsupported keys; old keys are decrypt-only during a bounded grace window; and new tokens use only the active key.
- Config validation tests for insecure production issuer/resource URLs, untrusted forwarded headers, and host-header injection.
- Canonical URL tests must include positive reverse-proxy cases where the Go server receives internal HTTP but emits configured public HTTPS issuer/resource/authorization/token URLs. Include path-prefix cases for `/mcp`, proxy-stripped versus preserved prefixes if supported, trusted versus untrusted `X-Forwarded-*`, and assertions that metadata/challenges never contain internal host, port, scheme, or container names.
- Production exposure tests or config validation should warn or fail when production mode uses HTTPS canonical public URLs but the listener is configured for broad external exposure without a trusted proxy boundary.
- Access versus refresh type-enforcement tests proving refresh tokens cannot call `/mcp` and access tokens cannot be used for refresh.
- Expiry, issuer, audience/resource, scope, subject, and `client_id` rejection tests.
- Refresh path tests proving the embedded InvenTree credential is validated with `/api/user/me/` or `/api/user/me/roles/` before new tokens are issued.
- Tests proving ChatGPT-visible OAuth responses never expose readable InvenTree credentials.
- HTTP transport tests proving concurrent requests with different OAuth envelopes cannot leak credentials across handlers.
- Shared-suite auth isolation tests using two distinct InvenTree users/tokens sealed into separate OAuth envelopes for parallel HTTP MCP calls.
- Tests documenting stateless refresh replay limitations and the configured lifetime/key-rotation mitigations.
- Tests for unauthenticated `initialize`/`tools/list` behavior versus authenticated InvenTree-contacting tool execution, aligned with the MCP SDK and OAuth protected-resource behavior.
- HTTP tests proving raw inbound `Authorization: Token ...` is rejected for protected `/mcp` access and is never forwarded unchanged.
- HTTP tests proving raw inbound `Authorization: Bearer ...` is accepted only when it is a valid MCP OAuth access envelope.
- OAuth scope tests proving each tool's required scopes are enforced before handlers run.
- Maintain a checked-in or generated tool authorization manifest listing each tool, mutation class, required OAuth scopes, destructive/idempotent/open-world annotations, and whether auth is required. Tests must fail if any registered tool is missing from the manifest, if implementation scopes differ from the manifest, or if a handler can run before scope checks pass.
- Setup-page tests proving CSRF binding, no-store cache headers, credential redaction, invalid credential handling, permission-denied token creation handling, and repeated install behavior when existing token metadata is visible but the token secret cannot be recovered.
- Setup-page browser security tests proving no-store, no-referrer, frame denial or CSP frame-ancestors, restrictive CSP, secure SameSite cookies, authorization-code query redaction, and no sensitive OAuth error descriptions.
- PATCH tests proving only changed fields are sent.
- PATCH tests proving explicit `""`, `false`, `0`, empty arrays, and nullable fields are serialized correctly.
- PATCH tests proving no-op updates are rejected before sending an empty PATCH.
- Tool metadata tests proving read-only, mutating, destructive, idempotent, and open-world annotations are correct.
- JSON-level annotation tests proving pointer false values such as `destructiveHint:false` and `openWorldHint:false` are emitted.
- Local safety-policy tests proving every tool has exactly one mutation class and the required gates for that class.
- Dry-run tests proving lookup/validation calls happen but zero POST/PATCH/DELETE calls happen.
- Confirm-gate tests proving missing `confirm` and `confirm:false` both block irreversible actions.
- Confirm-gate tests for operationally sensitive non-destructive writes, including quantity decreases, scrap/write-off status, stock consumption, and build allocation.
- Milestone test proving `preview_purchase_order_with_lines` is annotated read-only and performs no writes.
- Ambiguity tests proving duplicate matches return structured clarification responses with candidate IDs and URLs.
- Attachment ambiguity tests for duplicate filenames, ambiguous target objects, multiple image candidates, existing matching attachments, and unclear primary-image requests.
- Error mapping tests for InvenTree 400, 401, 403, 404, 409, 429, and 5xx responses.
- Log/audit redaction tests proving auth tokens do not appear in logs, audit entries, tool errors, or panic recovery output.
- Attachment negative tests for unsupported object type, nonexistent target object, invalid filename or path-like filename, content-type mismatch, zero-byte file, oversize file, unsupported image type, and delete scoped to a prefixed record only.
- Attachment download tests for original binary base64 output, allowlisted text output, explicit thumbnail mode, maximum download size, hash/size reporting, selected-mode reporting, missing file URL, stored-link behavior, out-of-scope `model_type` rejection before content fetch, redirect revalidation or blocking, and refusal to fetch URLs outside the configured InvenTree base URL.
- Upload source tests for inline byte blobs, STDIO allowlisted local paths, rejected HTTP local paths, rejected non-HTTP URL schemes, timeout, redirect limit, DNS/IP SSRF rejection, URL allowlist behavior, and maximum-size enforcement.
- SSRF bypass table tests for IPv6 loopback/link-local/ULA, unspecified/reserved/documentation ranges, CGNAT, IPv4-mapped IPv6, encoded IP forms supported by Go parsing, DNS rebinding, public-to-private redirects, allowlist edge cases, IDNA/punycode host normalization, wildcard suffix pitfalls, userinfo URLs, cloud metadata aliases, timeout, and streaming size cutoff before full buffering.
- URL fetch implementation tests proving no ambient proxy use unless explicitly configured, vetted-IP dialing, remote-address verification, redirect revalidation, and no cookies/auth headers forwarded.
- STDIO local file tests proving canonical path validation, symlink rejection where supported by the filesystem, non-regular file rejection after open via `File.Stat()`, directory/device/FIFO/socket rejection, and cleaned/resolved path containment under the allowlist.
- Local file tests must distinguish Afero-memory behavior from production `OsFs` behavior and state which checks each filesystem can prove.
- Tests proving `upload_attachment` rejects HTTP(S) URLs and points callers to `upload_attachment_from_url` or `create_link_attachment`.
- Tests proving URL uploads do not forward MCP or InvenTree auth headers.
- Tests proving link attachments do not fetch remote URLs.
- Link attachment URL-policy tests proving unsupported schemes, credentials/userinfo, unwanted fragments, and local file references are rejected, and optional link allowlist policy is enforced when configured.
- Link attachment tests must assert returned metadata clearly classifies the record as a stored link, not an uploaded file, including `is_link`/`is_file` behavior where available, absence of fetched byte metadata, and operator-facing text that no remote content was downloaded.
- Primary image tests for first assignment, replacement blocked without `confirm`, replacement allowed with `confirm:true`, ambiguous image selection, returned URL/thumbnail/image-state, and endpoint selection between part PATCH and part thumbnail PATCH where both are schema-visible.
- PATCH tests for attachment metadata and primary-image update behavior where the API supports PATCH, with documented exceptions where it does not.
- Parameter reuse tests proving existing templates are selected when unambiguous, ambiguity returns a clarification response, and new template creation requires a separate explicit workflow.
- Parameter matcher tests for disabled templates, same-name templates with different units/choices/checkbox settings, category-linked versus global templates, existing value update versus create, explicit empty/false/zero values, and refusal to create category links without an explicit separate workflow.
- Documentation checks proving `AGENTS.md`, `docs/api-schema.md`, tool reference, and operator recipes are updated when relevant behavior changes.
- Documentation checks covering the split between byte/path upload, URL ingestion, and link attachments.
- Schema drift check proving `docs/api-schema.yaml` changes require corresponding `docs/api-schema.md` provenance and capability updates.
- Generated endpoint manifest test proving implemented tools and client methods map to schema-known paths, HTTP methods, request schemas, response schemas, PATCH support, multipart fields, and object scopes.
- Schema drift tests must fail if an implemented endpoint is absent from `docs/api-schema.yaml`, if any capability table entry no longer matches the schema, or if `docs/api-schema.yaml` hash/version changes without `docs/api-schema.md` provenance updates.
- Documentation/generated-manifest checks comparing registered tools, auth modes, mutation gates, upload sources, and schema endpoint references against docs.
- Attachment readback/hash integration tests proving inline bytes, STDIO local-path uploads, and URL uploads are retrievable through the schema-supported download path and match expected size, hash, and content type.
- STDIO smoke test at command level where practical.
- Optional live integration tests against a test InvenTree instance.
- Testcontainers integration tests for write workflows.

Test suite classes:

| Suite | Command | Purpose |
| --- | --- | --- |
| Default | `GOFLAGS=-trimpath go test -race ./...` | Unit, contract, docs, and default-on pinned Testcontainers integration tests. |
| Unit-only | `GOFLAGS=-trimpath INVENTREE_TEST_SKIP_DOCKER=1 go test -race ./...` or `GOFLAGS=-trimpath go test -race -short ./...` | Fast tests with Docker-backed integration explicitly excluded. |
| Contract/docs | `GOFLAGS=-trimpath INVENTREE_TEST_SKIP_DOCKER=1 go test -race ./...` plus generated manifest checks | Tool annotations, scopes, schema references, and documentation drift without starting Docker. |
| HTTP auth | `GOFLAGS=-trimpath go test -race ./internal/server/... ./internal/oauth/...` | OAuth metadata, bearer challenge, token envelopes, and scope guards using fakes. |
| Integration | `GOFLAGS=-trimpath go test -race ./internal/testenv ./internal/integration/...` | Shared Testcontainers suite with pinned version-tag/schema pair. |
| Stable canary | CI-specific `inventree/inventree:stable` integration run | Non-blocking latest-stable compatibility and schema drift signal. |

## Required Test Matrix

- Config parsing.
- STDIO auth configuration.
- Filesystem abstraction and local upload policy.
- Clock, randomness, ID generation, context logging, and HTTP transport injection.
- HTTP OAuth metadata and protected-resource challenges.
- Authorization-code and PKCE handling.
- Authorization-code one-time-use state and replay rejection.
- OAuth token envelope validation and upstream credential recovery.
- OAuth key management and canonical issuer/resource URL validation.
- OAuth scope-to-tool authorization, including operational scopes for inventory-affecting writes.
- Setup-page CSRF, no-store, redaction, and token-creation fallback behavior.
- Request-scoped OAuth credential propagation.
- Client request construction.
- Pagination.
- Error mapping.
- PATCH serialization.
- Tool schema validation.
- Tool annotation registry.
- Dry-run planning.
- Confirmation gates.
- Structured clarification and ambiguous lookup handling.
- Parameter template discovery and reuse.
- Audit/log redaction.
- Testcontainers bootstrap.
- Shared-container parallel subtest isolation.
- Multi-user auth isolation in shared Testcontainers suite.
- Attachment and image upload/download/update/delete behavior.
- Upload source handling for byte blobs, dedicated URL upload tools, and STDIO local paths.
- Link attachment behavior.
- Primary image download and assignment/replacement behavior.
- Sales/customer boundary enforcement.
- End-to-end catalog and stock write workflows.
- End-to-end purchasing dry-run workflow.
- Schema endpoint manifest coverage for every implemented InvenTree API path/method/request body.

## Initial Milestone Definition

The first beta milestone should be a coherent "catalog and initial stock entry" release with a purchasing dry-run preview. It is intentionally broader than the first implementation slice.

The smaller MVP pass inside this milestone is:

- Search/create/update part.
- Supplier/manufacturer company and part links.
- Initial stock creation with duplicate detection.
- Part parameters using existing templates only.
- One inline or STDIO allowlisted local-file attachment.

Delivery order inside milestone 1:

1. Ship the MVP loop over STDIO with inline/allowlisted attachment support.
2. Add HTTP OAuth connector compatibility after the blocking connector/OAuth spike is complete.
3. Add URL upload, link attachment, metadata update, and primary-image support.

Do not expand beyond the full beta list until the MVP loop and OAuth setup are verified.

The full first beta milestone should include:

- Go module and buildable command.
- STDIO and HTTP transports.
- Stateless HTTP mode.
- HTTP OAuth protected resource, authorization-code with PKCE, token, refresh, and encrypted envelope support.
- Tool mutation metadata for every registered tool.
- PATCH-based partial update support in the client and first update tools.
- Part/category tools: `search_parts`, `get_part`, `search_part_categories`, `create_part`, `update_part`, `search_parameter_templates`, `get_part_parameters`, `set_part_parameters`.
- Company tools: `search_companies`, `search_suppliers`, `search_manufacturers`, `create_company`, `create_supplier_part`, `create_manufacturer_part`.
- Stock tools: `search_stock_locations`, `search_stock_items`, `create_stock_item`.
- Attachment/image tools: `list_attachments`, `get_attachment_metadata`, `download_attachment`, `download_part_image`, `upload_attachment`, `upload_attachment_from_url`, `create_link_attachment`, `update_attachment_metadata`, `set_primary_image`.
- Milestone attachment object scope: `part`, `stockitem`, `company`, `supplierpart`, `manufacturerpart`, and `purchaseorder`. Sales/return/transfer/build attachment support is deferred unless explicitly added later.
- Purchase-order attachment support in milestone 1 applies only to existing purchase orders found by ID/search. The milestone does not create purchase orders except through later explicitly enabled mutating workflows.
- Milestone primary image scope: `part` only. Company image endpoint notes are recorded in `docs/api-schema.md` for later implementation, but company primary-image support is deferred.
- Parameter behavior: `set_part_parameters` searches and reuses existing templates before writing and asks for operator clarification when unsure.
- Purchasing preview: `preview_purchase_order_with_lines`, with supplier-part validation and no writes.
- Structured clarification responses for ambiguous part/category/company/location lookups.
- Initial Testcontainers InvenTree test environment.
- GitHub Actions CI, Dependabot, golangci-lint config, and pre-commit config.
- README with quick-start links and minimal setup examples.

This milestone proves the transport, auth, client, schema, and data-entry patterns while completing a useful operator loop: add or update a purchasable part, associate supplier/manufacturer data, create initial stock, and preview a purchase order.

Blocking milestone tests:

- Connector-compatibility spike documented from current official OpenAI documentation before HTTP OAuth implementation starts.
- HTTP OAuth metadata and bearer challenge behavior.
- HTTP OAuth authorization-code with PKCE, token exchange, refresh, and encrypted envelope validation.
- Auth-code replay behavior tested according to the selected state strategy.
- Refresh absolute authorization/session expiry behavior.
- Reverse-proxy canonical URL positive and negative behavior.
- OAuth scope enforcement for read, write, upload, and destructive tool classes.
- Protected `/mcp` unauthenticated behavior verified: no MCP method dispatch without a valid access token unless the connector spike explicitly requires pre-auth static discovery; if allowed, only the documented static methods succeed and InvenTree-contacting tools fail before handler dispatch.
- Concurrent HTTP OAuth request isolation with different sealed InvenTree credentials.
- PATCH omission and zero-value table tests for `update_part`.
- Annotation golden test for all milestone tools.
- Attachment/image object capability table coverage check proving registered object types are a subset of `docs/api-schema.md`.
- Attachment download test proving original-mode returned content matches uploaded fixture bytes, thumbnail-mode behavior is tested separately, out-of-scope attachment model types are rejected before content fetch, redirects are blocked or revalidated, and non-InvenTree URLs are refused.
- HTTP local-path upload rejection before filesystem open/stat, including when STDIO allowlist is configured.
- STDIO allowlist canonicalization tests for `..` and symlink escape.
- Dry-run no-write test for `preview_purchase_order_with_lines`.
- Structured clarification test for at least one ambiguous lookup.
- Testcontainers bootstrap to a usable authenticated API.
- Testcontainers shared-suite happy path for catalog and initial stock entry.
- Testcontainers parallel subtests proving prefix isolation.
- Testcontainers happy path proving supplier/manufacturer part links are usable by `preview_purchase_order_with_lines`.
- Attachment upload, metadata update, and byte readback/hash test using a tiny generated fixture across the in-scope target-object matrix: `part`, `stockitem`, `company`, `supplierpart`, `manufacturerpart`, and existing `purchaseorder`.
- `upload_attachment_from_url` test using a local HTTP fixture server and STDIO local-path test with an allowlisted temp fixture, including readback/hash validation.
- `create_link_attachment` test proving the URL is stored without fetching remote bytes.
- Test proving ordinary `upload_attachment` is `openWorldHint:false`, `upload_attachment_from_url` is `openWorldHint:true`, and URL input to ordinary upload is rejected.
- Primary image assignment and replacement-confirmation tests.
- Primary image download tests for present, missing, too-large, and non-InvenTree URL states.
- Attachment listing or metadata test proving returned fields include thumbnail/image state, link/file classification, file size, object target, stable attachment ID, and relevant primary-image state.
- Registered tool/prompt/resource list test proving no sales-order tools, customer-oriented workflows, customer-role defaults, notes image upload, report attachment, stock test-result attachment, or other deferred app-specific file surfaces are present in milestone 1.
- Existing-parameter reuse test and ambiguous-parameter clarification test.
- Category-parameter-link reuse, ambiguity, and confirmation-gate tests.
- Documentation drift check from tool registry/schema metadata to `docs/tool-reference.md`, `docs/operator-recipes.md`, and `docs/api-schema.md`.
- Initial stock duplicate-detection test using `search_stock_items`, returning a clarification instead of blindly creating duplicate stock.
- Test proving `create_company` does not default companies to customer role and supplier/manufacturer prompts do not mention customer workflows.
- Prompt output contract tests proving prompts return a stable-ID retry request, dry-run plan, or structured clarification object.
- Sales/customer boundary tests proving `salesorder`, `salesordershipment`, `returnorder`, and customer-role defaults are rejected or hidden even though generic attachment schema exposes those model types.
- Duplicate attachment handling test proving duplicate filename/content without `attachment_id` or explicit replacement intent returns structured clarification; metadata updates require stable attachment ID.

Milestone test classification:

- Blocking tests must have deterministic local execution paths. Docker-backed integration tests run by default and can be explicitly excluded for unit-only, fast, or Docker-unavailable runs with `INVENTREE_TEST_SKIP_DOCKER=1` or `GOFLAGS=-trimpath go test -race -short`.
- Non-blocking tests may cover optional live external InvenTree instances, canary compatibility checks, and extended stress runs.
- Future tests must be tied to deferred scope such as sales workflows, return orders, transfer orders, company primary images, and build attachment support.
- Future image/file tests must cover deferred surfaces only when they enter scope, including notes image upload, generated report attachments, and stock test-result attachments.

Milestone README recipes:

- README should link to the corresponding `docs/operator-recipes.md` entries rather than duplicating full recipes.
- Required README links include ChatGPT OAuth setup, STDIO setup, reverse-proxy HTTP deployment, add/update purchasable part, add stock for an existing part, dry-run a purchase order, attach a datasheet or photo to a part, set or change a primary image, and add or update part parameters using existing templates.

## Resolved Product Decisions

- HTTP mode uses MCP-owned OAuth bearer tokens for ChatGPT Developer Connector compatibility and does not pass raw InvenTree `Authorization` headers through unchanged.
- HTTP OAuth tokens are encrypted, authenticated, stateless envelopes that seal the upstream InvenTree credential.
- STDIO mode supports configured `Token` or `Bearer` upstream InvenTree auth only.
- Blocking compatibility targets InvenTree `1.4.0` for the checked-in schema snapshot.
- Latest stable InvenTree is covered only by a non-blocking `inventree/inventree:stable` canary until schema/provenance updates are applied.
- Destructive operations are allowed behind confirmation and accurate MCP annotations.
- Tool inputs require API-required fields only, unless additional fields are needed to avoid ambiguous writes.
