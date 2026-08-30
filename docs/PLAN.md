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
- MCP SDK version: reviewed baseline is `github.com/modelcontextprotocol/go-sdk` `v1.7.0`, supporting MCP protocol `2026-07-28` while retaining legacy protocol negotiation. Future upgrades must re-run the MCP transport, auth, request-limit, cancellation, and annotation checks because protocol and wire-shape behavior may change.
- MCP branding: server identity and every registered tool publish the official InvenTree documentation logo through standard MCP icon metadata. Clients decide whether and where to render that metadata.
- MCP server instructions: initialization and discovery tell consuming agents how to handle a missing `inventree-mcp` capability. Agents distinguish server gaps from input, authorization, configuration, and upstream limitations; explain the gap and safe workarounds; search open and closed project issues when GitHub access is available; and ask the operator before creating an untracked issue. Clients decide how to apply this advisory model guidance.
- MCP STDIO transport: `mcp.StdioTransport`.
- MCP HTTP transport: `mcp.NewStreamableHTTPHandler`.
- HTTP auth support: implement a ChatGPT Developer Connector-compatible OAuth 2.1 layer owned by the MCP server. HTTP clients authenticate to `/mcp` with MCP-issued OAuth bearer tokens, not raw InvenTree tokens.
- OAuth implementation: first spike the official MCP Go SDK `auth` and `oauthex` packages for protected-resource middleware, bearer-token verification hooks, and metadata handlers. Use a maintained OAuth2/OIDC authorization-server library such as `github.com/ory/fosite` only for authorization-server endpoints the SDK does not provide, after a spike confirms it fits stateless encrypted token envelopes. `golang.org/x/oauth2` is useful for OAuth clients, but should not be treated as sufficient for implementing the authorization server.
- Upstream InvenTree auth header forms: `Authorization: Token <token>` and `Authorization: Bearer <token>`, recovered from encrypted MCP OAuth token envelopes.
- Outbound HTTP identity: every HTTP request initiated by the shipped `inventree-mcp` server or CLI identifies the client as `inventree-mcp/<build-version>` through `User-Agent`, including InvenTree API/media requests, URL-upload fetches, OAuth client metadata/JWKS retrieval, and GitHub self-update traffic. This does not rewrite inbound client requests or test-only infrastructure probes.
- InvenTree API access: small internal REST client using `net/http`, typed request/response structs, pagination helpers, and endpoint-specific methods.
- Shared Go utilities: use `github.com/davidvanlaatum/dvgoutils` where it fits local code style, especially `github.com/davidvanlaatum/dvgoutils/logging` for context-carried `slog` loggers and `logging.Err`.
- Filesystem abstraction: use an injectable filesystem such as `github.com/spf13/afero` for local file access, fixtures, and allowlist tests.
- Integration test infrastructure: `testcontainers-go` module that starts an isolated InvenTree test environment.
- Project automation: GitHub Actions for Go tests on the stable Go toolchain with coverage reporting, lint, dependency submission, tag-driven GoReleaser releases, and pre-commit checks; Dependabot version updates for Go modules, GitHub Actions, and pre-commit hooks.
- Local quality gate: pre-commit using `pre-commit-hooks` and Go hooks for `go mod tidy`, imports, golangci-lint, tests, and build.
- API schema source: keep a local copy of the official exported OpenAPI schema at `docs/api-schema.yaml`, refreshed from the commit-pinned `https://raw.githubusercontent.com/inventree/schema/37b2bb9fe2be3462d6a08e577b15892b2ce10fd3/export/530/api.yaml` for the blocking InvenTree baseline. The current fetched schema is OpenAPI 3.0.3 for InvenTree API version `530`; record the schema-repository commit/blob, generating InvenTree commit, release commit, fetch time, and SHA-256 in `docs/api-schema.md`.

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
- MCP traffic debug logging: `--debug-traffic-log` / `INVENTREE_MCP_DEBUG_TRAFFIC_LOG` may append MCP JSON-RPC request and response payloads to a local JSON Lines file for client interoperability debugging. This is intentionally unredacted debug evidence and must be documented as sensitive operator-controlled output. HTTP debug capture must reject unreadable or oversized request bodies before dispatch, cap captured response bodies, and avoid retaining unbounded streaming response data in memory.
- Logging tests: use `github.com/davidvanlaatum/dvgoutils/logging/testhandler.SetupTestHandler(t)` for deterministic log capture, redaction assertions, and ordinary test contexts passed into code under test. Create the logger context inside each subtest that uses it so logs are attached to the correct `testing.T`; use a bounded cleanup context or `context.WithoutCancel(ctx)` for cleanup callbacks that run after `t.Context()` is canceled.
- OpenTelemetry observability: use the maintained OpenTelemetry Go SDK behind `internal/telemetry`. Tracing and Prometheus metrics are independently disabled by default. Tracing supports OTLP/gRPC and OTLP/HTTP, W3C propagation, bounded tool/identifier attributes, and graceful flush. Metrics use the Prometheus exporter on the existing HTTP listener at a configurable unauthenticated path; STDIO mode rejects enabling metrics rather than binding a second listener. Production metrics require a private loopback/RFC-private listener; route collisions fail validation before mux registration, and a reverse proxy may add a scraper allowlist. Metric labels are limited to normalized protocol/HTTP methods, registered tool names, allowlisted InvenTree resource names, fixed bulk-start values, outcomes, status classes, and durations; in-flight gauges use only the stable tool/resource label. No credentials, payloads, record IDs, or upstream URLs are recorded.
- Other `dvgoutils` helpers: use `dvgoutils.Ptr` for pointer values such as explicit false tool annotation fields, and use `MapSlice`, `FilterSlice`, or `Must` only where they improve clarity without hiding control flow or error handling.
- Configuration and secrets: keep config parsing separate from runtime dependencies. Key material, InvenTree credentials, and token lifetimes should enter through a typed config object, not scattered environment lookups.
- Schema access: parse `docs/api-schema.yaml` through a schema helper for endpoint-manifest checks instead of ad hoc string matching.
- Deterministic image rendering (F-S90): `internal/render` uses `github.com/fogleman/gg` (MIT) for 2D path/shape drawing and `golang.org/x/image/font/basicfont` (already an indirect dependency, no embedded font asset or font license) for markings text. Every template is a fixed Go drawing routine over a validated parameter struct; there is no AI generation and no general-purpose SVG/vector output contract. PNG encoding uses the standard library `image/png` encoder, which is deterministic given identical pixel data.

OAuth spike acceptance criteria:

- Prove the official MCP Go SDK protected-resource middleware and metadata handlers can be used with stateless streamable HTTP.
- Prove SDK `TokenInfoFromContext` or the selected private context carrier is visible to `CallTool` handlers under `mcp.NewStreamableHTTPHandler` with `Stateless: true`.
- Verify ChatGPT Developer Connector compatibility from current official OpenAI documentation, including redirect URI format, client registration mode, required metadata fields, supported scopes, and local/dev callback constraints.
- If `fosite` is used, prove configured/static clients, PKCE S256, token endpoint validation, refresh grants, custom opaque token generation, envelope validation, and no persistent access-token store.
- Assume some authorization-code or setup-session storage may be required. Prove whether access and refresh tokens can remain sealed stateless envelopes while authorization codes use only a bounded in-memory or optional external store. Reject any design that requires a persistent access-token lookup table unless the product plan changes.

OAuth spike results verified on 2026-07-07 and refreshed on 2026-08-02 from official OpenAI and upstream library documentation:

- The first auth implementation pass used `github.com/modelcontextprotocol/go-sdk` `v1.6.1`; the current reviewed runtime baseline is `v1.7.0` and retains the verified bearer-middleware behavior.
- `auth.TokenVerifier` has signature `func(context.Context, string, *http.Request) (*auth.TokenInfo, error)`, so the verifier can validate the bearer token against request URL/resource context before dispatch.
- `auth.RequireBearerToken` rejects missing, invalid, expired, or insufficient-scope bearer tokens before the streamable HTTP handler runs, and emits a `WWW-Authenticate: Bearer` challenge with configured `resource_metadata` and `scope` parameters.
- `auth.TokenInfoFromContext` is visible inside `tools/call` handlers when `auth.RequireBearerToken` wraps `mcp.NewStreamableHTTPHandler` running with `mcp.StreamableHTTPOptions{Stateless: true}`.
- `auth.ProtectedResourceMetadataHandler` can serve RFC 9728 protected-resource metadata with resource, authorization server, supported scope, and resource-name fields. It does not validate metadata correctness, so production issuer/resource URL validation remains an `internal/oauth` responsibility.
- [OpenAI Authentication](https://developers.openai.com/plugins/build/auth) says ChatGPT expects OAuth 2.1-compatible MCP authorization: protected-resource metadata on the MCP server, authorization-server metadata, authorization-code flow with PKCE `S256`, and the `resource` parameter echoed through authorization and token requests.
- ChatGPT supports Client ID Metadata Documents as the preferred client registration path when supported and selected, dynamic client registration when `registration_endpoint` is advertised, and predefined OAuth clients. For CIMD, ChatGPT supports public-client token exchange with `none` and signed client assertions with `private_key_jwt`.
- F-S08 implementation decision: support CIMD with `private_key_jwt` and do not advertise a public-client `none` downgrade. Do not advertise DCR or predefined-client support in metadata until those paths are implemented and tested. Authorization-code and token envelopes bind to the CIMD `client_id` URL.
- Validate the HTTPS client metadata document, exact redirect URI, signed assertion claims, signature, and assertion replay before exchanging codes or refresh tokens. Fetch client metadata and its same-origin JWKS with bounded reads, timeouts, and safe redirects; accept only configured client metadata origins and approved asymmetric algorithms; reject bad `client_id`, redirect, audience, times, assertion ID, key, signature, fetch, metadata shape, or non-HTTPS URLs.
- The production ChatGPT redirect URI is `https://chatgpt.com/connector/oauth/{callback_id}` as shown in the app management page. Previously published apps may still use the legacy `https://chatgpt.com/connector_platform_oauth_redirect` redirect.
- [OpenAI Apps SDK Deploy](https://developers.openai.com/apps-sdk/deploy) says local development should expose the local MCP server through an HTTPS tunnel such as ngrok and refresh connector metadata after server changes. No separate local callback URL shape is documented for bypassing the ChatGPT redirect URI.
- The docs do not require unauthenticated MCP method dispatch for connector discovery. The implementation should keep `/mcp` protected by default and expose only OAuth metadata/challenge endpoints unauthenticated unless later live connector testing proves a specific static discovery exception is required.
- Tool-level auth UI depends on per-tool `securitySchemes`, protected-resource metadata, and runtime error results carrying `_meta["mcp/www_authenticate"]`. This belongs with M1C-S04 scope enforcement and tool authorization metadata; M1C-S03 should avoid implementing tool dispatch behavior beyond envelope validation and auth-code/token issuance.

F-S08 authorization-server library fit refresh on 2026-08-02:

- The pinned [MCP Go SDK v1.7.0 `auth`](https://github.com/modelcontextprotocol/go-sdk/tree/v1.7.0/auth) and [`oauthex`](https://github.com/modelcontextprotocol/go-sdk/tree/v1.7.0/oauthex) packages provide protected-resource server middleware/metadata and OAuth client behavior; they do not provide authorization-server authorization, setup, code, token, or refresh handlers.
- [Fosite v0.49.0](https://github.com/ory/fosite/tree/v0.49.0) supports PKCE, authorization-code/refresh handlers, custom token strategies, and `private_key_jwt`. Its handler/storage contracts retain authorization request, PKCE, refresh, and client-assertion sessions. Preserving this project's stronger boundary—only encrypted credential-bearing envelopes plus bounded identifier/expiry replay stores—would require a custom encrypted Fosite requester/session store, a remote CIMD client manager, and the same bounded same-origin JWKS fetch policy already owned by `internal/oauth`.
- F-S08 therefore retains a narrow internal authorization server around the existing envelope codec and bounded ID stores rather than adding Fosite plus parallel adapters. Custom code is covered by endpoint-level protocol/security tests and a production-mux Testcontainers flow. Re-evaluate this decision if the MCP SDK adds authorization-server primitives or if an external authorization server replaces MCP-owned credential setup.

## Operating Modes

### STDIO Mode

STDIO mode is intended for local MCP clients that launch the server as a subprocess.

Expected command shape:

```sh
inventree-mcp serve --transport stdio
```

Authentication in STDIO mode should come from process configuration:

- optional YAML configuration selected with `--config <path>`, or discovered from the first existing working-directory, `os.UserConfigDir()/inventree-mcp/`, or (on Unix) `/etc/inventree-mcp/` file; precedence is defaults, YAML, environment, then CLI flags
- `INVENTREE_URL`
- optional exact-frontend-mount `INVENTREE_WEB_URL`, falling back in every mode to `INVENTREE_URL` plus InvenTree's pinned stock `/web` mount for canonical object links
- `INVENTREE_TOKEN`
- optional `INVENTREE_AUTH_SCHEME`, defaulting to `Token` for InvenTree API tokens and allowing `Bearer`
- optional `INVENTREE_TIMEOUT`
- optional `INVENTREE_TLS_SKIP_VERIFY`, only for local/test deployments

YAML configuration may contain secrets. Linux and macOS startup rejects loaded config files with group/world-readable mode bits after opening the file (including symlink targets); Windows relies on operator-configured ACLs. A relative `XDG_CONFIG_HOME` is invalid, and `os.UserConfigDir()` supplies the platform-appropriate user config directory or returns an error when it cannot determine one.

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
7. Only OAuth metadata, authorization, token, setup, and health endpoints are public by default. `/mcp` must require a valid MCP OAuth access token before dispatching any MCP method unless the ChatGPT connector compatibility spike proves pre-auth MCP discovery is required. If pre-auth discovery is required, restrict it to static `server/discover`, legacy `initialize`, or capability data, never include request-specific InvenTree data, and document the exact allowed unauthenticated methods. Mutating tools may register for HTTP only when OAuth authorization mode is enabled, and each call must pass the per-tool scope guard before the handler runs.

HTTP session mode: run streamable HTTP in stateless mode using `mcp.StreamableHTTPOptions{Stateless: true}`. SDK `v1.7.0` negotiates MCP `2026-07-28` through `server/discover`, uses per-request client/protocol metadata, ignores session IDs, and rejects standalone GET/DELETE session operations while retaining legacy `initialize` compatibility. Do not bind a long-lived MCP session to process-global credentials. All InvenTree authorization must be resolved from the current OAuth token envelope.

Set `PropagateRequestCancellation: true` so an aborted MCP `2026-07-28` POST cancels its in-flight tool handler. Set `MaxRequestBodyBytes` from validated configuration rather than relying on the SDK default. The configured limit must cover `INVENTREE_UPLOAD_MAX_BYTES` after base64 expansion plus bounded JSON/tool-argument overhead, and must remain finite for untrusted HTTP clients.

Production startup configuration:

- `INVENTREE_MCP_OAUTH_ISSUER_URL` / `--oauth-issuer-url`: configured public HTTPS issuer URL. Production startup rejects missing, non-HTTPS, query-bearing, or fragment-bearing values.
- `INVENTREE_MCP_OAUTH_RESOURCE_URL` / `--oauth-resource-url`: configured public HTTPS resource URL used as token audience and protected-resource metadata `resource`.
- `INVENTREE_MCP_OAUTH_KEYS`: comma-separated `key-id:active|decrypt_only:base64-32-byte-key` entries supplied through protected process environment or the packaged owner-only YAML config file (`oauth_keys`). Startup requires exactly one active key and accepts decrypt-only keys for rotation. Secret key material is intentionally not accepted through CLI flags because process arguments and shell history are not secret storage.
- `INVENTREE_MCP_OAUTH_CLIENT_IDS` / repeated `--oauth-client-id`: comma-separated or repeated allowed HTTPS client metadata URLs. Access-token validation tries the configured client IDs as associated data and rejects tokens for unconfigured clients.
- `INVENTREE_MCP_TRUSTED_PROXY_CIDRS` / repeated `--trusted-proxy-cidr`: required production CIDRs for controlled reverse-proxy peers. Only those immediate peers can supply an `X-Forwarded-For` chain.
- Optional lifetimes: `INVENTREE_MCP_OAUTH_ACCESS_LIFETIME`, `INVENTREE_MCP_OAUTH_REFRESH_LIFETIME`, and `INVENTREE_MCP_OAUTH_SESSION_LIFETIME`, defaulting to 15 minutes, 30 days, and 90 days. Startup requires positive lifetimes, access shorter than refresh, and refresh not longer than session.
- Production HTTP rejects `INVENTREE_TOKEN`, non-default `INVENTREE_AUTH_SCHEME`, `INVENTREE_TLS_SKIP_VERIFY`, and `--dev-incomplete-oauth`. Raw InvenTree credentials are accepted only by the setup form, validated against the configured instance, and sealed into OAuth envelopes before `/mcp` use.

OAuth protected-resource discovery and challenge endpoints implemented by production startup:

- `/.well-known/oauth-protected-resource<resource-path>` describes the configured MCP resource, following the RFC 9728 path-specific well-known URL shape used by the MCP SDK; the ordinary `/mcp` resource therefore uses `/.well-known/oauth-protected-resource/mcp`. The bearer challenge advertises this exact configured metadata URL.
- Unauthenticated protected requests return `401` with `WWW-Authenticate: Bearer resource_metadata="<metadata-url>"`.
- The protected-resource metadata advertises the configured authorization server issuer, supported scopes, bearer method, resource URL, and resource name.
- The protected-resource metadata URL is derived from the configured resource URL's scheme and host, not from request `Host` headers.

Implemented authorization-server and setup endpoints follow the current [OpenAI authentication guide](https://developers.openai.com/plugins/build/auth):

- `/.well-known/oauth-authorization-server<issuer-path>` describes authorization, token, supported grant, PKCE, issuer, and metadata behavior; an issuer with no path uses `/.well-known/oauth-authorization-server`.
- The authorization endpoint supports authorization-code flow with PKCE for ChatGPT.
- The token endpoint supports `authorization_code` and `refresh_token` grants.
- ChatGPT redirects authorization responses to `https://chatgpt.com/connector/oauth/{callback_id}`. Add that production redirect URI from the app management page to the authorization server allowlist. Previously published apps may still use the legacy `https://chatgpt.com/connector_platform_oauth_redirect` redirect.
- Support Client ID Metadata Documents with `private_key_jwt`. The token endpoint verifies `iss`, `sub`, audience, issued/expiry times, unique assertion ID, algorithm, and signature against the same-origin HTTPS JWKS advertised by the allowed CIMD document. Assertion IDs are replay-protected by bounded process-local state; restart and multi-replica deployments require an external shared replay store for equivalent protection. Do not advertise public-client `none`, dynamic client registration, or predefined clients until those paths are explicitly approved, implemented, and tested.
- Echo the `resource` parameter through authorization and token requests, bind issued tokens to the configured resource audience, and reject tokens missing the expected resource/audience.
- Production deployments are expected to run behind a path-preserving reverse proxy that terminates HTTPS. The public issuer, authorization, token, redirect, and MCP resource URLs must be configured as HTTPS canonical URLs, even if the Go process receives HTTP from the proxy. `INVENTREE_MCP_PATH` must exactly equal the resource URL path, including any prefix, and the proxy must forward canonical paths unchanged; prefix stripping and `X-Forwarded-Prefix` reconstruction are unsupported. Issuer and resource URLs must come from explicit configuration, not request `Host`, `X-Forwarded-Host`, or `X-Forwarded-Proto` headers. `X-Forwarded-For` is accepted only from configured trusted proxy CIDRs, resolved right-to-left through trusted hops, and used as the normalized source for rate limits and request-scoped logs without logging the raw header. Metadata, bearer challenges, token envelopes, and audience validation use the configured canonical URLs exactly. Production deployments must expose the Go HTTP listener only to the trusted reverse proxy or private service network; do not publish the internal HTTP port directly.

OAuth setup flow:

1. ChatGPT starts OAuth authorization.
2. The MCP server presents an MCP-hosted setup/login page.
3. The setup page offers explicit supported credential methods for the first release: paste an existing InvenTree API token, or authenticate to InvenTree only if a schema-verified/browser-verified flow is implemented. It must recommend a dedicated least-privilege InvenTree API token for this connector.
4. The page explains that the MCP server will validate the credential and attempt to create a dedicated connector token where the InvenTree API allows it. MCP OAuth scopes restrict which MCP tools run, but they do not reduce the upstream permissions of the sealed InvenTree credential once a permitted tool calls InvenTree.
5. The setup page binds form submissions to the OAuth authorization request using state/session data, requires CSRF protection, sets no-store cache headers, avoids persisting submitted credentials, and redacts request bodies from access logs, error logs, panic recovery, and audit events. It must set `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY` or equivalent CSP `frame-ancestors 'none'`, a restrictive `Content-Security-Policy`, and secure SameSite cookies for any setup session state. Production HSTS is enforced at the reverse proxy. Setup, authorization, and token endpoints must enforce per-IP and per-client rate limits, maximum request body sizes, context-aware timeouts, and generic error responses for credential validation failures. The Go HTTP server bounds request-body reads to 30 seconds; upstream credential work has a shorter setup timeout.
6. The MCP server validates the credential with a cheap authenticated endpoint such as `/api/user/me/` or `/api/user/me/roles/`.
7. During setup, create a uniquely named dedicated InvenTree API token without rotating the credential submitted to setup or an earlier connector token. The first database-free implementation uses a random setup-specific suffix because it cannot determine whether an earlier sealed credential remains usable. Do not assume existing token values can be retrieved from InvenTree after creation. Abandoned or expired authorizations can therefore leave unused connector tokens; disclose that tradeoff and require operator cleanup through InvenTree token management. Token list/retrieve endpoints may be used for metadata, duplicate detection, or revocation, not for recovering a lost token secret.
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

For SDK `v1.7.0`, `TokenVerifier` has the shape `func(context.Context, string, *http.Request) (*auth.TokenInfo, error)`. The verifier should decrypt the envelope and return SDK `auth.TokenInfo` with scopes, expiry, subject, and a non-serializable internal credential reference in `Extra`, or a documented private context key if `Extra` is unsuitable. If `Extra` is used, expose it only through a typed `internal/oauth.CredentialFromTokenInfo(*auth.TokenInfo)` accessor with an unexported key/type, and add tests proving the credential object is never serialized or logged. `ClientFromContext` must read exactly one selected carrier inside `CallTool` handlers; do not duplicate credentials into multiple context locations. Tool handlers and resource handlers must resolve credentials from `context.Context`; do not store credentials in server-global state.

## Releases And Packages

Releases are tag-driven through GitHub Actions and GoReleaser. Pushing a `vX.X.X` tag runs `.github/workflows/release.yml`, executes `GOFLAGS=-trimpath go test -v -race ./...`, and publishes a GitHub release with checksums, Linux/macOS/Windows binary archives for `amd64` and `arm64`, Linux `deb`, `rpm`, and `apk` packages, and a multi-architecture `ghcr.io/davidvanlaatum/inventree-mcp` image tagged with the release and `latest`. The image reuses GoReleaser's prebuilt Linux binaries, runs as a non-root user, and defaults to HTTP on port `28686` at `/mcp`; production deployment still requires the documented OAuth and InvenTree configuration.

The Linux packages install the `inventree-mcp` binary to `/usr/bin`, install `packaging/systemd/inventree-mcp.service` as `inventree-mcp.service`, and install `/etc/inventree-mcp/config.yml` as a noreplace YAML configuration file (F-S86), which the packaged unit loads with `inventree-mcp serve --config /etc/inventree-mcp/config.yml`. Package maintainer scripts reload systemd and restart the service only when it is already enabled or active, including on upgrade, which fails closed (leaving the service down rather than starting insecurely) if `config.yml` still holds the packaged placeholder values. The `apk` package carries the same files for artifact parity; Alpine/OpenRC service management is not implemented in the first release package.

The packaged unit runs as a dedicated non-root `inventree-mcp` system user/group instead of root (F-S87). A package-created static system user/group was chosen over `DynamicUser=yes`: a `DynamicUser=yes` identity's allocated UID/GID is not guaranteed to persist across reboots unless the unit also declares a `StateDirectory=`/`CacheDirectory=`/`LogsDirectory=`, which this service does not otherwise need, so it risked `config.yml` ownership silently drifting out from under the service after a reboot. `packaging/scripts/preinstall.sh` creates the `inventree-mcp` system user/group before package files are unpacked (using `useradd`/`groupadd` where available, falling back to Alpine's `adduser`/`addgroup`), so nfpm's per-file `owner`/`group` metadata for `/etc/inventree-mcp/config.yml` resolves correctly at install time. `postinstall.sh` additionally re-applies `inventree-mcp:inventree-mcp` ownership and mode `0600` to `config.yml` on every install and upgrade, so an existing operator-modified conffile that a package manager's conffile-preservation logic left untouched (and therefore still root-owned from a pre-F-S87 install) still ends up owned by the service identity. `postremove.sh` removes the system user/group only on final package removal — `dpkg purge`, the final `rpm` erase (`$1 = 0`), or any `apk` removal (which has no separate purge concept) — never on a version upgrade.

The packaged service is for HTTP mode behind a path-preserving reverse proxy. Production HTTP startup can serve protected streamable HTTP with connector authorization/setup, signed-client token exchange, OAuth envelope validation, request-scoped InvenTree credential recovery, protected-resource metadata, explicit trusted-proxy source resolution, and the full tool surface behind per-tool scope guards. Live packaged deployment and connector validation in F-S10 still gate operator-ready ChatGPT deployment. Packages can be installed for file layout testing, but the systemd service should not be enabled for live connector use until that validation lands. F-S06 adds native systemd lifecycle support through `github.com/coreos/go-systemd/v22/daemon`: the unit uses `Type=notify`, `NotifyAccess=main`, and `WatchdogSec=30s`; the process reports ready after runtime construction and listener binding, derives heartbeat cadence as half the systemd-provided watchdog timeout, and publishes sanitized startup, ready, degraded, stopping, and fatal status text after the managed HTTP lifecycle starts. Configuration and logger initialization failures before that boundary exit non-zero for systemd to record and restart without duplicating transport parsing in an early notification path. A failed heartbeat stops further heartbeats but leaves the HTTP process serving so systemd remains responsible for terminating it at the watchdog deadline and restarting it under `Restart=on-failure`.

## Local CLI Self-Update

F-S18 adds an explicit local `inventree-mcp self-update` command for direct GitHub release-archive installations on Linux and macOS `amd64`/`arm64`. It remains outside MCP registration, server startup, and background operation. The approved initial policy uses latest or exact newer stable releases, rejects prereleases/downgrades/Windows/package-managed paths without mutation or privilege elevation, and trusts canonical GitHub release control plus the published SHA-256 checksum with the shared-trust-root residual risk documented in [self-update.md](self-update.md).

`internal/selfupdate` owns one-time direct-install adoption markers, bounded GitHub release discovery, exact asset selection, credential-isolated HTTP, checksum verification, strict single-executable archive parsing, current-target ownership/link/identity and ancestor checks, kernel-released cross-process locking, isolated staged/installed version probes, atomic replacement, durable transaction recovery, rollback, and the `.previous` recovery binary. The CLI only parses local flags and renders the result. GoReleaser direct-install archives intentionally exclude README/license defaults so future archives contain only the expected executable; packages retain their existing metadata and must be upgraded through their package manager.

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
internal/selfupdate/
  selfupdate.go
  http.go
  archive.go
  install.go
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
- Keep list and search projections bounded to high-value selection fields. Exact `get_*` tools are the canonical approved complete-record surface, backed by pinned inventories that classify every default response field; sensitive, deferred, write-only, and separately retrieved fields remain explicit exclusions. Optional nested expansions remain separate lookups unless a reviewed workflow needs them atomically for validation.
- Return InvenTree object IDs and URLs for every created or updated object. F-S34 and its F-S37 default-mount correction make this contract explicit across reads and writes: absolute user-facing frontend `web_url` values are distinct from sanitized relative REST-path `api_url` values, media URLs, and operator-supplied external links. Build web links only from trusted process configuration through a centralized typed route map; OAuth token envelopes, request/proxy headers, and caller input must not control link authority or route selection. Optional `INVENTREE_WEB_URL` is the exact operator-configured frontend mount; when omitted, every mode preserves the `INVENTREE_URL` site/API base and adds InvenTree's version-pinned stock `/web` frontend mount. Both paths receive the same credential-free web-base validation, production requires HTTPS, and invalid effective bases fail startup with redacted diagnostics. The configured authority and deployment prefix remain operator-authoritative even if internal or not browser-reachable for every MCP user; returned links can disclose that authority to authorized callers and operator-enabled sensitive debug traffic logs that capture response bodies, while rejected raw configuration and request-derived authorities remain absent from diagnostics and ordinary logs. Objects without a stable dedicated frontend page omit `web_url` and use universal `parent_web_url` only for an immediate owning object with a stable frontend page and identity exposed in the same projection; they omit that field rather than walking to a more distant ancestor. Clarification candidates intentionally make a documented breaking change from ambiguous `url` to explicit absolute `web_url` and sanitized relative `api_url`, with no compatibility alias; F-S34 does not add `api_url` universally.
- Treat operator-supplied external links as functional inventory data, not browser-link authority. Successful supported reads, dry-run plans, and verified write results preserve the complete trimmed absolute HTTP(S) URL, including query parameters and fragments; writes and upstream projections reject or omit malformed values, unsupported schemes, and userinfo/credentials instead of repairing them into different URLs. Clarification candidates, ordinary structured logs, errors, and minimal ambiguous-recovery projections remain URL-free. Query strings can contain sensitive values, so callers must protect returned records accordingly; the explicitly enabled sensitive debug traffic log may capture complete authorized response bodies under its existing operator-local warning.
- Make all high-risk write workflows support `dry_run`.
- When planning any new mutating tool, explicitly assess whether a bulk counterpart is practical for the same record type or operation. Prefer a shared internal batch executor with resource- or operation-specific public tools when independent records can be preflighted and mutated safely; document why a bulk variant is out of scope when relationship, ordering, atomicity, or partial-failure semantics make it unsafe or require a separate workflow.
- Dry-run workflows must expose every effective mutation field in an explicit plan. Action names alone are not a reviewable plan; keep selected existing records factual and map each unresolved foreign-key field to the earlier planned create that supplies it. A field-level preview without a confirmation token is advisory: execution must repeat preflight and callers must review again after intervening upstream changes.
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
- `internal/workflows` owns multi-step planning, dry-run behavior, confirmation gates, ambiguity handling, and business workflow orchestration. This package was never built: today's single-item dry-run/confirmation logic lives inline in `internal/tools` (e.g. `stockPlanStore`, `parameterPlanStore`) instead, and F-S76's `internal/batch` (below) is a distinct new package for future bulk tools, not a renaming of this aspirational one.
- `internal/batch` owns shared bounded batch planning and execution primitives for future resource- or operation-specific bulk MCP tools: a generic single-use confirmation-token store (`batch.Store[P]`) generalizing the existing single-item plan-store pattern, and a bounded-concurrency executor (`batch.Execute`) giving every batch item an independent applied/skipped/failed/ambiguous/unverified outcome rather than the sequential stop-on-first-failure behavior F-S14's `bulk_propagate_part_parameters` uses. It claims no cross-item atomicity and exposes no generic arbitrary-record PATCH tool; F-S77 through F-S81 each implement a resource-specific `batch.Adapter` on top of it rather than hand-rolling another bespoke plan-store. F-S77 (`internal/tools/catalog_bulk_tools.go`) is the first concrete consumer: `bulk_update_parts`, `bulk_update_companies`, `bulk_update_part_categories`, `bulk_update_supplier_parts`, and `bulk_update_manufacturer_parts` each reuse the matching single-item tool's PATCH-field builder, clear-conflict, duplicate/reference, and postflight functions unchanged, accepting only the subset of fields that tool applies without its own extra `confirm`-gated review. F-S81 replaced the fixed `bulkUpdateMaxItems`/`bulkUpdateConcurrency` constants every bulk tool shared with operator-configurable `Config.BulkMaxItems`/`Config.BulkConcurrency` (YAML/env/CLI, following the F-S75 layering pattern, threaded through `tools.Dependencies`), and gave `batch.Execute` an `OnProgress` callback plus a `Timing{Orchestration, Upstream}` result so every bulk tool can emit best-effort real-time MCP progress notifications (`ServerSession.NotifyProgress`, only when the caller attaches a progress token) and report aggregate client/orchestration time separately from aggregate upstream request time in its output.
- `internal/upload` owns upload source resolution for base64 byte blobs, STDIO allowlisted local files, and URL fetches. It must enforce source-mode policy and SSRF controls before content reaches InvenTree.
- `internal/platform` owns small interfaces and adapters for clock, ID generation, and randomness where a package needs test seams. It may provide constructors or config wiring for `afero.Fs`, but packages should accept `afero.Fs` directly instead of a second filesystem abstraction unless a concrete need appears. Logging should use `dvgoutils/logging` directly rather than a second internal logging abstraction.
- Tool handlers should depend on a narrow `inventree.Client` interface or domain-specific interfaces, not concrete HTTP client construction. This keeps STDIO and HTTP OAuth credential resolution in the server layer and makes tool tests cheap.
- `internal/server` should construct dependencies such as `tools.Dependencies{ClientFromContext func(context.Context) (inventree.Client, error)}` and call `tools.Register(mcpServer, deps)`.
- Tool-specific OAuth scope enforcement belongs in `internal/tools` or `internal/server` as a handler wrapper generated from the tool authorization manifest. `auth.RequireBearerToken` should not be treated as sufficient for per-tool authorization because it only validates the bearer token and populates context.
- `internal/tools` and `internal/workflows` must not import `internal/server`; dependencies should flow inward through interfaces.
- `internal/tools` may call `internal/workflows` through constructors or interfaces. `internal/workflows` should depend only on domain client interfaces and upload source interfaces, not HTTP clients or transport state.

## Tool Mutation Classification

Every tool registration must include explicit behavior metadata using SDK-native MCP tool annotations, including `ReadOnlyHint`, `DestructiveHint`, `IdempotentHint`, and `OpenWorldHint` where applicable. Keep a local classification table only as the source for registering and testing annotations, not as a replacement for SDK metadata.

For the reviewed SDK baseline `v1.7.0`, `DestructiveHint` and `OpenWorldHint` remain pointer booleans, while `ReadOnlyHint` and `IdempotentHint` are plain booleans that now always serialize. Annotation helpers must set explicit false pointer values for `destructiveHint:false` and `openWorldHint:false` where appropriate, and JSON-level tests must require explicit `readOnlyHint:false` and `idempotentHint:false` for mutating non-idempotent tools.

`openWorldHint` must be decided per tool. In particular, `upload_attachment_from_url` is open-world because it fetches caller-provided URLs, while `upload_attachment` is not open-world when it only accepts inline bytes or STDIO allowlisted local files.

Read-only tools:

- `search_parts`
- `get_part`
- `search_part_categories`
- `get_part_category`
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
- `update_part_category`
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
- `set_company_image`
- `set_company_image_from_url`
- `create_purchase_order`
- `update_purchase_order`
- `add_purchase_order_line`
- `update_purchase_order_line`
- `create_purchase_order_with_lines`
- `issue_purchase_order`
- `complete_purchase_order`
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
- `generate_stocktake`
- `poll_stocktake_generation`
- `deplete_stock_item`
- `transfer_stock_item`
- `update_part_family_relationships`
- `add_bom_item`
- `update_bom_item`
- `receive_purchase_order_items`
- `allocate_build_stock`
- `issue_build_outputs_to_stock`

Mutating destructive or irreversible tools:

- `remove_bom_item`
- `delete_attachment`
- `clear_company_image`
- `update_part_family_relationships`
- `complete_build_order`

Destructive or irreversible tools should require `confirm: true` and should expose `dry_run` where the workflow can be planned safely.

Classification tests should fail if a tool appears in conflicting categories unless the annotation model explicitly represents multiple facets. Operationally sensitive tools may still be non-destructive, but they must be treated as inventory-affecting and require stronger prompting/audit behavior.

## Common Data-Entry Coverage

### Discovery and Lookup Tools

These tools help agents find stable IDs before writing data.

- `search_parts`
- `get_part`
- `search_part_categories`
- `get_part_category`
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
- `update_part_family_relationships`
- `set_part_parameters`
- `create_part_category`
- `update_part_category`
- `create_manufacturer_part`
- `create_supplier_part`
- `upsert_part_with_supplier_and_manufacturer`

Important behaviors:

- Support IPN/SKU/name/category lookup.
- Expose exact `revision_of`, `revision_count`, and `variant_of` values. Change family topology only through `update_part_family_relationships`, never ordinary scalar `update_part`.
- Validate units, category, default location, and supplier/manufacturer references before write.
- `update_part` should use PATCH and serialize only supplied fields.
- Keep `search_parts` concise and use `get_part` as the exhaustive approved API 530 scalar projection. A checked field inventory must classify every default serializer field so nested detail, deferred workflows, write-only commands, and raw barcode data cannot leak into the exact surface through schema drift.
- Part scalar create/update supports consumable/template/lock/salable/testable flags, default expiry, keywords, complete credential-free HTTP(S) link, minimum/maximum stock, revision text, and Markdown notes. Preserve omission versus false/zero, use explicit null-clearing controls for nullable additions, reject invalid effective stock bounds, treat `creation_user` as read-only, and perform exact stable-ID read-back under `inventree.read` plus `inventree.write`.
- Family relationship assignment, replacement, and clearing require a principal-bound five-minute single-use plan token. Revisions require a nonblank revision code, a non-template target, and the same variant template on both records; variants require a template target. The plan binds both current relationship values, requested target IDs, relevant eligibility state, and deterministic exact-read traversal evidence under one shared 64-record budget; self-reference, cycles, incomplete traversal, and stale topology fail before PATCH. The dedicated tool is closed-world, non-idempotent, destructive, and requires read, write, operational, and destructive OAuth scopes.
- Part-category administration trims names, refuses case-insensitive same-parent duplicates within a fail-closed 1,000-record scan, validates stable parent/default-location IDs, and uses PATCH with explicit null-clearing controls.
- Category reparenting may include direct parts and descendants only after `confirm:true` hierarchy review; self/descendant cycles are refused. Structural-state changes also require confirmation, and promotion to structural is refused while direct parts exist.
- Return recommended-but-missing field warnings for conventions such as IPN format, units, revision, default location, purchaseability, assembly flags, templates, and custom parameters when they can be detected.
- `set_part_parameters` should search `/api/parameter/template/`, `/api/parameter/`, and `/api/part/category/parameters/` first and reuse matching enabled templates where possible. Do not blindly create parameter templates from natural language.
- If multiple parameter templates could match by name, units, choices, checkbox state, or category association, ask the operator which existing template to use.
- Candidate ranking should prefer enabled category-linked templates with matching name and units. Global or otherwise unlinked template matches may be reported as context, but the milestone `set_part_parameters` tool must not select or write them; it must refuse the request unless the template is linked to the part's category or inherited from an ancestor category, matching `bulk_propagate_part_parameters` and `search_category_parameter_defaults(include_parent_defaults: true)` (F-S14/F-S13) ancestor-resolution semantics. Disabled templates should be reported but not selected automatically.
- Clarification candidates should include template ID, name, units, choices, checkbox state, category association, existing value if present, absolute `web_url` when a stable frontend page exists, and sanitized relative `api_url` when a REST identity is available.

### Company Tools

- `create_company`
- `update_company`
- `create_contact`
- `create_address`
- `link_supplier_to_part`
- `link_manufacturer_to_part`

Important behaviors:

- Treat supplier and manufacturer roles explicitly. Do not add customer-specific assumptions while sales is out of scope. F-S44 added customer-role administration limited to role state and dependency safety: `update_company` accepts `is_customer:true` alongside supplier/manufacturer role addition, and the dedicated `remove_company_customer_role` tool guards role removal behind a state-bound plan token and a bounded stock/sales-order dependency audit. Neither adds sales tools, customer contacts/billing, CRM behavior, or customer defaults.
- `search_companies`, `search_suppliers`, and `search_manufacturers` should operate on the same InvenTree Company model with explicit role filters.
- `create_contact` and `create_address` (still not implemented as of F-S49) would be for supplier/manufacturer operational data needed by catalog and purchasing workflows only, and must not introduce customer-role defaults, sales contacts, billing workflows, or CRM-style customer management in milestone 1. F-S49 deliberately implemented discovery (`search_contacts`/`get_contact`/`search_addresses`/`get_address`) and guarded purchase-order assignment (`assign_contact`/`assign_address`) without creation/deletion tools, per its own operator decision; these two entries in this list remain aspirational until a separately approved story adds them.
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
- `adjust_stock_quantity` applies one non-zero relative delta through the native add/remove endpoints, while `stocktake_adjustment` records one absolute observed quantity through the native count endpoint and `set_stock_status` changes only status through the native status endpoint.
- Initial stocktake scope is one stable stock-item ID per operation. Stocktake counts are quantity-only and do not implicitly change location, status, batch, or packaging.
- Aggregate stocktake generation is a separate operational workflow: it requires exactly one part/category/location selector, explicit independent entry/report flags, a complete current-state-bound dry run, and a principal-bound single-use confirmation. The upstream response is enqueue-only; the workflow polls the returned `DataOutput`, verifies its stable identity and terminal state, fails closed on timeout/errors, and downloads completed report content only through the client’s same-instance URL, no-redirect, bounded-read policy.
- Every stock adjustment execution requires a dry-run plan bound to current stock state, explicit confirmation, and a nonblank operator audit reason. The returned `plan_hash` field is an opaque, principal-bound, single-use confirmation token that expires after five minutes; a newer dry run for the same principal, action, and stock item supersedes its earlier token, and a restart invalidates all outstanding tokens. Outstanding confirmation storage is bounded globally and per principal. Plans call out quantity decreases and `Destroyed`, `Rejected`, or `Lost` status transitions as high-risk.
- No-op changes are refused. Relative and absolute quantity changes are refused for serialized stock because InvenTree does not apply those operations to serialized items; status-only changes remain supported. A quantity change that would reduce an item with `delete_on_deplete:true` to zero is also refused because implicit deletion is outside this non-destructive workflow.
- Changing whether a stock item deletes itself at depletion uses the separate destructive `set_stock_delete_on_deplete` tool rather than `update_stock_item_metadata`, because it controls future record deletion. It plans and confirms only the `delete_on_deplete` boolean through the native stock-item PATCH endpoint, requires read, write, operational, and destructive scopes plus the standard principal-bound confirmation token and audit reason, and refuses a no-op request where the item already has the requested policy. It never deletes stock itself and is valid at any quantity including zero; enabling the flag is flagged high-risk because it authorizes deletion at a later depletion, while disabling it is not.
- Intentional delete-on-deplete removal uses the separate destructive `deplete_stock_item` tool. It removes the complete current positive quantity rather than accepting a caller-selected partial amount, requires read, write, operational, and destructive scopes plus the standard principal-bound confirmation token and audit reason, and rejects allocated, serialized, building, consumed, installed, parent-linked, or child-bearing stock. Supplier, purchase-order, and completed-build provenance is shown and bound into the plan but does not independently block removal. Exact-ID not-found verification is the only success condition, including recovery after a lost successful removal response.
- Physical relocation uses the separate operational `transfer_stock_item` tool and native stock-transfer endpoint. The first F-S27 contract accepts one stable source item and one explicit destination, always moves the complete current quantity, reports `will_split:false`, preserves reviewed provenance, and verifies the original stable ID at the destination. It applies conservative source-state safeguards but no MCP-specific structural, external, ownership, or type restriction to an exact-read destination; InvenTree remains authoritative for destination validity. F-S28 separately owns partial quantities and split recovery, and F-S29 owns reviewed multi-item batches.
- Do not adjust the same stock item concurrently through MCP, the InvenTree UI, another server replica, or a direct API client. Execution performs a fresh preflight and rejects state changes it observes, but InvenTree exposes no compare-and-swap primitive across the subsequent mutation. A concurrent change in that narrow read/write window can still race; readback mismatch is returned as `partial_failure` with read-before-retry guidance.
- F-S52 adds part-scoped serial-number discovery plus guarded assignment, replacement, and clearing on existing single-quantity stock items. `search_stock_serials` requires a `part_id` and reads the native `/api/stock/` `serial`/`serial_gte`/`serial_lte`/`serialized` filters; `get_part_next_serial` reads `/api/part/{id}/serial-numbers/` for the part's `latest`/`next` serial after confirming the part is trackable (returning `not_trackable` rather than forwarding InvenTree's own no-op response for a non-trackable part). `assign_stock_serial` requires `inventree.read`, `inventree.write`, and `inventree.operational`; it PATCHes the nullable `serial` field directly (no `/api/stock/{id}/serialize/` bulk-split call, which remains out of scope), refusing an already-serialized item, a quantity other than exactly 1 (InvenTree requires quantity 1 for a serialized item), a non-trackable base part, or a mismatched part identity from the trackability lookup. `set_stock_serial` combines replacement and clearing behind a single guarded tool that additionally requires `inventree.destructive` and publishes `destructiveHint:true`, since either operation rewrites stock identity and downstream audit context; it refuses an unserialized item (directing the caller to `assign_stock_serial`) and a no-op replacement. Both `assign_stock_serial` and `set_stock_serial` share one relationship-safety guard refusing allocated, building, consumed, installed, parent-linked, child-bearing, customer-assigned, or sales-order-linked stock -- reusing `deplete_stock_item`/`transfer_stock_item`'s field set plus the customer/sales-order fields `transfer_stock_item` also checks, minus the `in_stock`/`delete_on_deplete` checks that do not apply to a serial-only change. Both mutation tools preflight duplicate serials with a same-part `search_stock_items` scan, and additionally scan across every part when the `SERIAL_NUMBER_GLOBALLY_UNIQUE` global setting reads `true`; that setting lookup follows the `get_inventree_instance_info` convention exactly (`inventree.IsOmittableFetchError` plus a logged warning skips only the cross-part scan on a staff-only permission/not-found response, while any other error -- network failure, timeout -- hard-fails the tool rather than silently disabling the check), and InvenTree's own write-time validation remains authoritative for the global scope either way. Stock-item serial identity may be referenced by labels, external systems, or unexposed history that MCP verification cannot prove synchronized.

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

- Preserve InvenTree's endpoint-specific `model_type` contracts. Attachment tools use the attachment endpoint's short values (`part`, `stockitem`, `company`, `manufacturerpart`, `supplierpart`, and `purchaseorder` in current scope), while parameter templates use the parameter endpoint's qualified `app.model` values such as `part.part` and `order.purchaseorder`. Tool schemas and operator docs must list the applicable vocabulary explicitly and must not present either enum as valid for the other endpoint.
- For milestone 1, expose attachment tools only for `part`, `stockitem`, `company`, `supplierpart`, `manufacturerpart`, and existing `purchaseorder` records. Build, transfer, return, sales, and BOM-related attachment workflows are deferred even if the generic attachment schema can represent them.
- Support image uploads for object types that expose image fields or attachment-backed images.
- Support attachment download through `download_attachment` using a stable attachment ID. It is read-only, requires `inventree.read`, must resolve attachment metadata first, must reject metadata whose `model_type` is outside the milestone attachment object allowlist before fetching bytes, and must fetch only schema-supported attachment or thumbnail URLs belonging to the configured InvenTree instance. It must not fetch arbitrary caller-provided URLs and must not use the URL-upload fetcher.
- `download_attachment` should default to the original `attachment` file URL when present. Thumbnail retrieval should require an explicit thumbnail mode so original-byte hash tests remain deterministic. Return filename, content type when known, size, SHA-256 hash, selected download mode, and content as base64 for binary files or optionally text for allowlisted textual content types. Apply a configured maximum download size and return a structured error when content is too large.
- Support primary part image download through `download_part_image` using a stable part ID. It is read-only, requires `inventree.read`, resolves only the readable schema-exposed `Part.image` field or part thumbnail endpoint for that part, and applies the same configured-instance, maximum-size, bounded-read, hash, selected-mode, and redaction controls as `download_attachment`. `Part.existing_image` is write-only and must be treated as assignment/update input only, not as a download source.
- `download_part_image` should return a structured no-image result when the part has no primary image. If the part image is backed by a generic attachment and the caller already has the attachment ID, `download_attachment` may be used instead.
- Treat the part thumbnail API as part of the primary part image implementation. For schema version `530`, `set_primary_image` resolves an existing same-part image attachment, downloads it through the scoped InvenTree attachment path, then uploads those bytes with multipart `PATCH /api/part/{id}/` using the `image` file field. Live integration rejected using a generic attachment URL with `PATCH /api/part/thumbs/{id}/`; keep `existing_image` write-only and outside download behavior.
- Company primary images use direct multipart `PATCH /api/company/{id}/` after the dedicated tool resolves and validates PNG, JPEG, or WebP bytes. Enforce a fixed 5 MiB encoded limit, 4096-pixel per-dimension limit, 16-megapixel total limit, and agreement between decoded format, extension, and media type. First assignment is unconfirmed; replacement requires `confirm:true`. Exact same-instance download and SHA-256 comparison must prove assignment or response-loss recovery while preserving every unrelated company detail field and role.
- `clear_company_image` is a separate destructive tool requiring `confirm:true` and exact null read-back. Pinned InvenTree 1.5.0 deletes the current stored media file when the nullable image association is cleared. The tool applies to an existing company regardless of supplier, manufacturer, or customer role and does not add customer-role or sales administration.
- Keep notes image upload, generated report attachments, stock test-result attachments, and other app-specific file surfaces out of the first release unless the plan is explicitly changed.
- Define upload input forms explicitly:
  - `upload_attachment` accepts inline byte blobs encoded as base64 in HTTP and STDIO mode, with required filename and content type.
  - `upload_attachment` may additionally accept local file paths in STDIO mode only when a configured allowlist permits the path.
- `upload_attachment_from_url` is the only generic attachment tool that accepts HTTP(S) URLs; `set_company_image_from_url` is the only company-image tool that does so.
- `create_link_attachment` creates an InvenTree link attachment without fetching remote bytes.
  - HTTP mode must not read arbitrary server-local paths supplied by a client.
  - URL fetching must reject non-HTTP(S) schemes, local file URLs, and responses that exceed the configured maximum size.
- Use PATCH for attachment metadata and image-field updates where supported.
- Treat file upload as mutating non-destructive, metadata/image changes as mutating non-destructive, and attachment delete as destructive.
- Treat URL upload as open-world. `upload_attachment_from_url` must have `openWorldHint:true`; ordinary byte/local-path `upload_attachment` should not inherit that hint.
- Treat link attachments as mutating non-destructive. They store the URL in InvenTree's `link` field and do not fetch content.
- `create_link_attachment` must validate allowed URL schemes and may optionally apply a separate link allowlist policy. It must not fetch the URL. Operator-facing responses should make clear that link attachments are stored references, not uploaded files.
- Require `confirm: true` for deletes, clearing a company primary image, and replacing an existing primary image.
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
- Link attachment URL policy is separate from URL-fetch SSRF policy because the server does not fetch the link target. Allow only complete absolute `http` and `https` links without credentials/userinfo; preserve functional query parameters and fragments, and reject unsupported schemes and path-like local file references. Make optional link allowlists visible in operator docs.
- `upload_attachment` must reject HTTP(S) URLs with a clear error directing callers to `upload_attachment_from_url` or `create_link_attachment`, depending on intent.
- When an operator provides a URL with ambiguous intent such as "attach this", return a structured clarification asking whether to upload a copy of the remote file or store a link reference. Do not choose between `upload_attachment_from_url` and `create_link_attachment` automatically unless the caller's intent is explicit.
- Dry-run URL uploads must not fetch remote content. They may validate URL syntax and policy configuration only. Actual URL fetches happen only during confirmed execution of `upload_attachment_from_url`.

Before implementing attachment tools, update the endpoint capability table in `docs/api-schema.md` for each target object type. The table should record the InvenTree endpoint, upload field names, supported methods, whether primary-image behavior exists, whether PATCH is supported, and any object-specific constraints. Tool schemas should only expose object types verified in that table.

### Deterministic Component Image Rendering

`render_component_image` (F-S90) is a single generic tool, not five per-family tools: a `family` discriminator (`resistor`, `diode`, `led`, `capacitor`, `fuse`) selects which one nested, family-specific parameter object is required. It is entirely local rendering — no InvenTree API call, no upload, no primary-image assignment — implemented by `internal/render`, a fixed set of Go drawing templates over validated parameter structs. There is no AI generation and no general-purpose SVG/vector output contract; templates may use vector-like primitives internally before rasterizing to a bounded PNG.

Key design points, matching the acceptance criteria in `docs/TASKS.md`'s F-S90 story:

- Determinism: identical input on the same build (same Go toolchain, same GOARCH) always produces byte-identical PNG bytes (same SHA-256). No `time.Now`, randomness, or map-iteration-order-dependent drawing is used anywhere in a template's happy path. This does not extend across different CPU architectures: `gg`'s floating-point anti-aliasing rasterizer can round a handful of edge pixels by a couple of least-significant bits differently between, for example, arm64 and amd64 builds of the identical source — confirmed by rendering the checked-in gallery under an emulated linux/amd64 build and diffing against the arm64-built originals (at most a few pixels per image, each off by a small fraction of full color range, invisible to the eye). `TestRenderSamplesMatchCheckedInGallery` therefore compares decoded pixels within a small bounded tolerance rather than raw PNG bytes; `TestRenderDeterministic`'s same-process, same-architecture byte-identity check is unaffected and still exact.
- Fail-closed validation: unsupported or ambiguous parameter combinations (an unrepresentable resistor value/tolerance/band-count combination, two colors that must contrast being identical, dimensions supplied for only one of a paired length/diameter, an out-of-range canvas size) are rejected with a validation error rather than guessing a value or silently clamping.
- No claimed physical scale: package-variant `size` presets (`small`/`medium`/`large`, or family-appropriate equivalents like `3mm`/`5mm`/`10mm` for LEDs and `5x20mm`/`6x30mm` for fuses) are illustrative layout choices only. Only explicitly supplied paired dimensions (for example a resistor's `body_length_mm`/`body_diameter_mm`) influence the drawn aspect ratio, and even then the renderer does not claim absolute canvas-to-millimeter scale.
- Resistor color bands are always derived, never supplied directly, from `resistance_ohms`, `band_count`, and `tolerance_label` using the IEC 60062 color code. Coverage is 4-band and 5-band; 6-band (adds a temperature-coefficient band) is deferred as a documented future extension, not implemented by this story.
- Orientation is family-agnostic: `horizontal` is each template's own native drawing frame (whatever that naturally is — elongated left-right for the three axial families, upright for the two radial-view families), and `vertical` rotates the fully rendered glyph 90 degrees clockwise as an exact, pixel-copy operation (no interpolation blur) rather than a per-family geometry change.
- Markings and caption text render with Go's own "Go Sans" TTF font (`golang.org/x/image/font/gofont`, already available through the existing `golang.org/x/image` dependency, plus `github.com/golang/freetype/truetype` to rasterize it — no new go.mod entry beyond promoting `github.com/fogleman/gg` itself, and no separate font license) rather than a fixed bitmap font, so derived captions can use real Unicode glyphs such as Ω and ± at any point size with anti-aliasing. Caller-supplied free-text `markings`/`markings_text` fields remain bounded to printable ASCII and a small maximum length; server-derived caption text is not subject to that check since it is not caller input.
- Output is bounded: canvas width/height are limited to 64-1024 pixels, and the encoded PNG has an explicit maximum-byte-size contract, even though the flat-shaded templates in scope never approach it.
- The tool requires `inventree.read` even though it makes zero InvenTree API calls. This is a deliberate choice to reuse the existing OAuth authenticate-and-scope-check path (`GuardTool`) so the tool is not reachable unauthenticated over HTTP, rather than inventing a new "authenticated but scope-free" tool-authorization mode.
- Extensibility: a future low-dimensional template (another axial or radial passive, for example) should follow the same pattern — a new `internal/render` file with a validated parameter struct and a `renderCanvas`-driven template function, a matching nested input struct in `internal/tools/component_render_tools.go`, and a new `render.Family` constant — not a shift toward AI generation or arbitrary product-image synthesis.
- Visual regression coverage: `internal/render.Samples` is a fixed, ordered list of named example configurations across all five families. `cmd/generate-render-samples` renders it to produce both the checked-in gallery under `docs/images/render-samples/` and `docs/render-samples.md`; `internal/render`'s `TestRenderSamplesMatchCheckedInGallery` re-renders the same list and compares decoded pixels against those checked-in PNGs within a small bounded tolerance (see the package doc comment and the determinism bullet above for why an exact byte comparison is too strict across CPU architectures). This exists because none of the package's other tests inspect actual pixel/artwork content beyond a few structural spot checks (background fill, output dimensions, the rotation helper's own pixel mapping) — a rendering change that silently altered a family's drawn appearance would otherwise pass every test. Run `go generate ./internal/render/...` and review the resulting image/markdown diff whenever a rendering change is intentional; commit the regenerated PNGs and `docs/render-samples.md` together with the code change.
- A color.RGBA value with alpha < 255 must be built through `straightRGBA` (`internal/render/colors.go`), not a direct struct literal: Go's `color.RGBA` is defined as already alpha-premultiplied, so every component must be <= alpha, and a literal built from straight RGB values (for example `color.RGBA{R: 0xf6, G: 0xf6, B: 0xf2, A: 0xf0}`, where R/G/B all exceed A) is an invalid premultiplied color that can blend into badly wrong pixels — this was found live in this story (a fuse's glass tint and marking backing panel) and is exactly the kind of regression `TestRenderSamplesMatchCheckedInGallery` is meant to catch going forward.

Out of scope for this story: AI-generated or photorealistic images; MOSFET, IC, connector, USB, or other families with highly variable package/marking conventions; datasheet generation, authoritative electrical interpretation, schematic capture, pinout inference, or automatic category-based parameter inference; automatic attachment upload or primary-image replacement (the caller passes the returned bytes to an existing attachment/image tool for that).

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
- `update_purchase_order`
- `add_purchase_order_line`
- `update_purchase_order_line`
- `preview_purchase_order_with_lines`
- `create_purchase_order_with_lines`
- `issue_purchase_order`
- `receive_purchase_order_items`
- `complete_purchase_order`
- `hold_purchase_order`
- `resume_purchase_order`
- `cancel_purchase_order`

Important behaviors:

- Use supplier-part links when receiving purchasable items.
- Require `issue_purchase_order` with `confirm_issue:true` and the exact current-state hash from its dry run before a pending order is placed with its supplier. The hash binds the complete order metadata and sorted purchase-order line state visible in the preview and rejects changes observed by the confirmation preflight; receiving never issues an order implicitly. InvenTree does not provide an atomic conditional issue operation across the final reads and placement request, so operators must coordinate a single writer while issuing. If placement returns an ambiguous result, inspect the current order and line state before preparing any retry.
- `receive_purchase_order_items` accepts schema-valid partial outstanding quantities only for a placed order, rejects virtual parts because they do not create stock, resolves location from item override to line destination to global fallback, and creates new stock items without merging into or updating existing stock.
- Return a deterministic `plan_hash` with each dry run. The plan includes the supplier pack conversion, resulting base-stock quantity, resolved packaging, source line purchase price/currency, and explicit `complete_order` intent. When completion is requested, the hash also binds every current ordinary line and the request is rejected unless the planned receipt leaves them all fully received. The operational call requires both `confirm_receive:true` and the exact hash for the current preflight plan; changed order, line, supplier-pack, packaging, source price, or completion intent invalidates the confirmation. InvenTree's global currency conversion configuration is not revisioned through this endpoint, so a concurrent administrator change remains outside the hash boundary.
- By default, return the refreshed purchase order exactly as InvenTree leaves it, respecting the server's `PURCHASEORDER_AUTO_COMPLETE` setting. With explicit `complete_order:true`, treat an upstream auto-completed result as success or call the native completion endpoint after the successful receipt. If receipt succeeds but completion cannot be verified, preserve the returned stock items and provide completion-only recovery; never invite the caller to repeat the receipt.
- `complete_purchase_order` separately dry-runs and confirms later completion of one placed order. Its current-state hash binds the complete order metadata, every sorted ordinary line, target `COMPLETE` status, and `accept_incomplete:false`. It returns success for an already-complete order, rejects any outstanding ordinary line quantity, verifies refreshed status after mutation, and recovers a lost completion response only when exact read-back proves `COMPLETE`.
- Treat concurrent receipt of the same purchase-order line as unsupported. InvenTree 1.5.0 serializes its line updates but does not atomically cap a previously prepared receipt to the newly outstanding quantity; an MCP-process lock would not protect against the InvenTree UI, direct API clients, or other MCP replicas, so the operator accepted this narrow residual risk without local locking.
- Require explicit current-state confirmation before completing an order; never expose InvenTree's incomplete-line completion override.
- `update_purchase_order_line` should use PATCH and serialize only supplied fields.
- F-S47 adds standalone `update_purchase_order`, using PATCH by exact stable ID for description, Markdown notes, supplier reference, creation/start/target dates, currency, destination, and external link, with explicit `clear_*` flags for every nullable field except `creation_date`: pinned InvenTree 1.5.0 resets `creation_date` to the current date rather than clearing it when sent JSON `null`, so no `clear_creation_date` flag is offered and the field can only be set, never cleared, through this tool. Supplier company and internal InvenTree `reference` remain immutable through this tool; `status`, `status_custom_key`, and `project_code` remain read-only or deferred pending their own stories. `responsible` is exposed on `get_purchase_order` and mutated only through F-S48's guarded `assign_owner` tool; `contact` and `address` are likewise exposed on `get_purchase_order` (F-S49) and mutated only through the guarded `assign_contact`/`assign_address` tools — none of the three through `update_purchase_order`. `get_purchase_order` and `get_purchase_order_line` expose the complete approved exact-read field set behind checked-in pinned field inventories (`PurchaseOrderFieldInventory`, `PurchaseOrderLineFieldInventory`) that fail raw-key contract tests on unclassified schema drift; nested order/part/supplier-part/contact/address detail and staff-only fields (project code, parameters, tags) remain separate lookups or deferred. `update_purchase_order_line` and `update_purchase_order_extra_line` additionally accept `link` and `discount`, using the same F-S39 external-link validation/redaction as order-level `link`.
- `preview_purchase_order_with_lines` is the milestone dry-run tool. It must be read-only, reject write intent, and perform supplier-part validation without creating a purchase order.
- `create_purchase_order_with_lines` was not registered in the original milestone 1 delivery. Its post-milestone F-S03 workflow takes a supplier, stable supplier reference, description/date fields, and receivable supplier-part line inputs; runs preview-equivalent validation first; then creates or updates the purchase order and lines while returning stable purchase-order and line IDs for retry/recovery. F-S23 adds optional non-receivable extra lines after the normal-line phase for invoice, surcharge, discount, and supplier-product context. Extra lines require a trimmed case-sensitive reference unique within the purchase order, accept exact signed unit prices including zero and negative values, and never create stock. Dry runs return field-level `planned_changes` for order, normal-line, and extra-line creates or patches, including references and dependencies on a planned order create. The exact `(supplier_id, supplier_reference)` pair is the order retry identity, while InvenTree generates its pattern-compliant internal reference. The completed purchasing tools are classified as implemented in the checked manifest.
- Purchase-order write tools must include read/search support for purchase orders and lines so duplicate checks and recovery after interrupted writes do not require raw REST calls.
- Purchase-order extra-line administration includes bounded order-filtered search, stable-ID read, dry-run create/update, exact response-loss recovery, refreshed server-calculated order totals, and confirmed destructive single-record deletion. It excludes schema-visible `project_code` until project-code lookup and validation semantics are approved.
- F-S62 adds `hold_purchase_order`, `resume_purchase_order`, and `cancel_purchase_order` as dedicated lifecycle workflows, each a current-state-planned dry-run/confirm tool bound to a principal-bound, single-use, five-minute `plan_hash` token rather than the stateless hash used by `issue_purchase_order`/`complete_purchase_order`. A live Testcontainers spike against pinned InvenTree 1.5.1/API 530 established that native `hold`/`issue`/`cancel` validate almost no source state themselves: `hold` and `issue` succeed unconditionally from `PENDING` or `PLACED`, are silent no-ops (200 with unchanged status) when called on a `CANCELLED` order, and `cancel` succeeds even from a `PLACED` order with partially received stock — which InvenTree leaves orphaned but still order-linked with no auto-disposal — refusing only from `COMPLETE`. No native resume endpoint exists; `resume_purchase_order` reuses `POST /api/order/po/{id}/issue/`, which always transitions to `PLACED` regardless of whether the order was held from `PENDING` or `PLACED`, so `hold_purchase_order`'s dry-run and executed plan carry an explicit `warning` when the pre-hold state is `PENDING`, since resuming will place that order with its supplier. `hold_purchase_order` and `resume_purchase_order` are permitted from both `PENDING` and `PLACED`/`ON_HOLD` respectively and require `inventree.read`, `inventree.write`, and `inventree.operational`; `cancel_purchase_order` additionally requires `inventree.destructive`, publishes `destructiveHint:true`, and fails closed whenever any ordinary line has received quantity greater than zero, regardless of the received stock's current disposition. All three return an `already_on_hold`/`already_placed`/`already_cancelled` no-op for an order already in its target state, refuse a `COMPLETE` order for hold or cancel, verify exact refreshed order status after mutation, and never expose generic status editing, custom-status mutation, or whole-order `DELETE`.

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
- `receive_purchase_order_checklist` (`milestone_1`)
- `bom_import_review` (`future`)
- `stocktake_review` (`milestone_1`)

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
- `candidates`: candidate IDs, names, absolute `web_url` values when stable frontend pages exist, and sanitized relative `api_url` values when REST identities are available.
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

Local agents need a deterministic way to discover that process-owned policy before staging a file. Register `get_local_upload_policy` only in STDIO mode and return canonical configured roots, the effective attachment and company-image byte limits, and concise regular-file/containment requirements. Returned roots mean only that the MCP server may read a qualifying file; they do not assert caller write permission. HTTP mode must not register the tool or expose roots. Attachment and company-image allowlist rejections should return bounded reason-specific recovery: an outside path directs the agent to discovery and permitted staging, while a missing allowlist asks for operator configuration or inline content. Canonicalization, symlink, regular-file, and size enforcement remain authoritative in `internal/upload`.

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
- Provide attachment and image methods such as `ListAttachments`, `DownloadAttachment`, `DownloadPartImage`, `DownloadCompanyImage`, `UploadAttachment`, `PatchAttachment`, `SetPartPrimaryImage`, `SetCompanyPrimaryImage`, `ClearCompanyPrimaryImage`, and `DeleteAttachment` where the API supports them. For current schema version `530`, generic attachment support maps to `/api/attachment/` and `/api/attachment/{id}/`; attachment content download uses the live schema-exposed `attachment` URL by default or `thumbnail` URL in explicit thumbnail mode, part-image download uses readable `Part.image` or the part thumbnail endpoint, and company-image verification uses readable `Company.image`. Shared multipart schemas describe image/file inputs as binary, while pinned live detail responses continue to return same-instance URL strings. Every media download remains scoped to the configured InvenTree base URL.
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
- Read operations may retry on transient failures. Non-idempotent writes must not be automatically retried unless the workflow has a stable retry identity or performs safe duplicate-detection reads.
- Request timeouts should be explicit and context-aware for both MCP handlers and upstream InvenTree API calls.
- Bulk attachment delete is out of scope initially. If added later, require stricter confirmation, dry-run listing, object/prefix scoping, and destructive annotations.

## Compatibility Decisions

- InvenTree blocking integration-test baseline: InvenTree `1.5.2`, which reports API version `530` and passes the blocking integration suite against the existing client contracts. This pin does not define the minimum supported InvenTree version.
- Docker image for blocking Testcontainers tests: `inventree/inventree:1.5.2`, targeting the checked-in `docs/api-schema.yaml` OpenAPI 3.0.3 / API version `530` client contract. Do not use a digest as the primary pin because the version should be clear in config, logs, and failure output. Do not use floating tags such as `stable` for blocking tests.
- Separate stable-canary compatibility job: use `inventree/inventree:stable`, record the resolved InvenTree version and image digest/tag, and report schema drift as non-blocking until the schema/provenance update workflow is run.
- Integration startup should fetch `/api/schema/` and record the API version. Blocking schema-sensitive tests must fail when the runtime schema version differs from checked-in `docs/api-schema.yaml`, unless they run against the recorded image version/schema pair known to match the checked-in schema.
- Schema update workflow: refresh `docs/api-schema.yaml`, update `docs/api-schema.md` provenance and capability tables, update the pinned InvenTree version tag or recorded tag/schema pair, then run the blocking integration suite.
- API schema source baseline: current local `docs/api-schema.yaml` is the official `inventree/schema` API 530 export associated with an InvenTree source commit before the 1.5.0 release tag. Blocking Testcontainers coverage confirms that the released InvenTree `1.5.2` image still reports API `530` and satisfies the exercised client contracts. Provenance records both source identities; the pin does not establish a minimum supported InvenTree version.
- README compatibility table: the README's [Supported InvenTree Versions](../README.md#supported-inventree-versions) table records the tested InvenTree version/API revision for each released `inventree-mcp` version, using the same "does not establish a minimum supported InvenTree version" framing as this section. AGENTS.md's Release Workflow section owns the two-step process that keeps it aligned with the blocking pin above across both a pin-change commit and a release-tag commit; a focused Go test compares the table's current row against `internal/testenv`'s pin constants, and a tag-triggered release-workflow gate blocks tagging while that row still shows an unresolved `` `main` (unreleased) `` placeholder.
- Upstream InvenTree auth schemes: `Token` and `Bearer` only.
- STDIO auth behavior: read the upstream InvenTree token only from `INVENTREE_TOKEN`. Non-secret connection settings, such as URL, auth scheme, and timeouts, may come from environment or flags.
- HTTP auth behavior: use MCP-owned OAuth bearer tokens with encrypted upstream InvenTree credential envelopes.
- HTTP statelessness: no database-backed access-token mapping is required for the initial implementation. Authorization codes still require bounded one-time-use code ID storage before beta.
- ChatGPT connector compatibility: official docs refreshed on 2026-08-02. Use OAuth 2.1-compatible MCP auth with protected-resource metadata, authorization-server metadata, authorization-code + PKCE `S256`, `resource` parameter binding, CIMD `private_key_jwt` token endpoint authentication, production redirect `https://chatgpt.com/connector/oauth/{callback_id}`, and HTTPS tunnel-based local development.
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
- Manual HTTP `server/discover`/list-tools smoke test plus legacy `initialize` compatibility check.
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

Live order-entry hardening normalizes omitted, null, blank, and whitespace-only MPN input to omission without inventing a manufacturer identifier. With no MPN, the combined workflow searches the exact part/manufacturer pair, reuses one existing link, clarifies multiple links, or records a skipped manufacturer-part action when none exists, while still allowing the supplier-part path to complete. A direct manufacturer-part create still omits the field and reports the pinned server's safe validation rejection rather than fabricating a value. Ordinary writes return bounded allowlisted validation fields with canonical non-echoing messages. The combined part/supplier/manufacturer workflow preserves completed actions and stable records, identifies remaining actions, and returns a read/search recovery plan when a later mutation is rejected or has an ambiguous result. Existing duplicate/reference preflight satisfies the lower-level safety contract where a separate dry run would not add protection.

### Future Workflow Tools

- BOM import workflow.
- Build order create/allocate/complete workflow.
- Stocktake adjustment workflow.
- F-S39 through F-S47 close approved external-URL, part, family-relation, related-part, sourcing, company, stock-detail/history, and purchasing exact-read or maintenance gaps while retaining concise search projections.
- F-S42 exposes related-part links as undirected stable-ID records with bounded reads, guarded create, note-only state-bound update, and confirmed single-link deletion; endpoint replacement remains an explicit delete-then-create workflow.
- F-S48's object matrix is Part `responsible`, PurchaseOrder `responsible`, StockItem `owner`, and StockLocation `owner`; Company, PartCategory, PurchaseOrderLine, Build, ProjectCode, ReturnOrder, SalesOrder, and TransferOrder are excluded because they have no owner/responsible field, are sales-adjacent, or are not yet supported. `search_owners`/`get_owner` read InvenTree's read-only `/api/user/owner/` endpoint and project only stable `pk`, `type` (`owner_model`), `name`, and `label`; `search_owners` requires either a narrowing `query` or a supported `object_type`, since InvenTree's owner list has no per-object-type server-side filter — `object_type` only satisfies this non-empty-request bound and does not itself restrict which owners are visible, since InvenTree's `/api/user/owner/` endpoint imposes no additional permission beyond ordinary read access for the authenticated principal. A single guarded `assign_owner` tool covers replace and clear for every object type through a state-bound plan token that binds the object identity and its current owner, so a drifted owner between preview and confirm makes the token stale rather than silently overwriting an unreviewed value; it requires `inventree.destructive`. `update_stock_location` previously exposed an ordinary, unguarded `owner_id`/`clear_owner` pair that (pre-F-S48) validated against `/api/company/` rather than the real Owner endpoint; the operator decided to move location owner replacement/clear onto the new guarded `assign_owner` tool for consistency with Part, PurchaseOrder, and StockItem, and `create_stock_location`'s `owner_id` was corrected to validate against the real Owner endpoint. `create_stock_location` keeps its ordinary `owner_id` for initial assignment since there is no prior owner to replace.
- F-S49 through F-S54 add separately reviewed contact/address, project-code, delete-on-deplete policy, serial, stock-provenance, and install/uninstall workflows instead of broad generic PATCH surfaces.
- F-S49 adds structured company contact/address discovery and guarded purchase-order assignment. API 530 exposes structured `contact`/`address` FK references on PurchaseOrder, ReturnOrder, SalesOrder, and TransferOrder; among already-supported objects only PurchaseOrder carries them (Company itself has no writable contact/address FK, only its free-text `contact` string and computed read-only `primary_address`), so `assign_contact`/`assign_address` are purchase-order-specific tools rather than a generic multi-object-type matrix like F-S48's `assign_owner`. `search_contacts`/`get_contact` and `search_addresses`/`get_address` are company-scoped (`company_id` is required) bounded lookups reading InvenTree's `/api/company/contact/` and `/api/company/address/` endpoints; per the operator decision, every projection (search and exact read alike) permanently excludes phone, email, and street-address lines/postal code so contact/address PII never reaches agent context, leaving enough identity (name, role, title, city/region, country, shipping notes) to select the right record. `get_purchase_order` now exposes nullable `contact`/`address` stable IDs (previously deferred); nested `contact_detail`/`address_detail` remain separate `get_contact`/`get_address` lookups, matching the `responsible`/`responsible_detail` precedent. `assign_contact` and `assign_address` each use a state-bound five-minute single-use plan token binding the purchase order, its supplier company, and its current contact/address, so either the order's supplier or its current reference changing since the preview makes the plan stale; the resolved target's `company` field is validated against the order's supplier before a plan is even issued. Creation and deletion of contact/address records remain excluded from the MCP tool surface; Testcontainers fixtures create them directly through the InvenTree API for test setup only.
- F-S50 adds project-code discovery and guarded assignment. API 530 exposes a `project_code` FK on `PurchaseOrder`, `PurchaseOrderLineItem`, `PurchaseOrderExtraLine`, `Build`, and `TransferOrder`, plus sales-adjacent `ReturnOrder`/`SalesOrder`; among already-supported, non-sales objects the field applies to `PurchaseOrder`, `PurchaseOrderLineItem`, and `PurchaseOrderExtraLine` only (`Build`/`TransferOrder` are not implemented as MCP objects), so `assign_project_code` covers exactly those three object types through a descriptor-map matrix like F-S48's `assign_owner`, rather than the single-object shape of F-S49's `assign_contact`/`assign_address`. `search_project_codes`/`get_project_code` are bounded lookups reading InvenTree's `/api/project-code/` endpoint and project only stable `pk`, `code`, `description`, and `active`; per the F-S48 precedent excluding `ProjectCode` from the owner matrix, `responsible`/`responsible_detail` on the project code itself remain deferred. `get_purchase_order`, `get_purchase_order_line`, and `get_purchase_order_extra_line` now expose nullable `project_code` stable IDs and `project_code_label`; nested `project_code_detail` remains a separate `get_project_code` lookup, matching the `responsible`/`responsible_detail` precedent. `project_code`/`project_code_label` were added to the base line and extra-line records (not only their Detail-only order-level counterpart) specifically so `create_purchase_order_with_lines`, `issue_purchase_order`, and `receive_purchase_order_items` carry a line's current project code through their dry-run plans, hashes, and read-back without any bespoke plumbing — this preservation is verified by tests that reassign a line's project code between preview and confirm and assert the reviewed plan goes stale. `assign_project_code` uses a state-bound five-minute single-use plan token binding the object and its current project code, so a drifted project code since the preview makes the plan stale. Pinned InvenTree 1.5.0/API 530 rejects a `project_code`-only PATCH on `/api/order/po-line/{id}/` with a `part`/`order` validation error even though the OpenAPI schema does not mark the field read-only there; `assign_project_code`'s line descriptor works around this by re-supplying the line's own current `part` and `order` in the same PATCH; `quantity` was confirmed unnecessary via a live Testcontainers probe and is deliberately omitted, since resupplying it would risk silently clobbering a concurrent quantity edit made between the descriptor's fetch and its PATCH (verification only re-checks `project_code`, not `quantity`). The order and extra-line endpoints accept a `project_code`-only PATCH directly. Creation and deletion of project-code records remain excluded from the MCP tool surface; Testcontainers fixtures create them directly through the InvenTree API for test setup only.
- F-S55 through F-S60 are discovery-only stories for barcode, tags, testing, pricing, requirements, and stocktake generation/reporting; each requires explicit operator approval of its resulting implementation contract.
- F-S61 is complete on `main` and establishes the InvenTree 1.5/API 530 baseline. The post-merge optional-field audit uses these explicit routes:
  - F-S40 owns part `consumable` and notes; nested category/default-location detail and `category_path` remain separate exact lookups, existing part parameters remain under F-S12, tags were added by F-S91, and price breaks remain F-S58.
  - F-S43 owns complete approved supplier/manufacturer-part exact detail, supplier availability/read metadata, and distinct long Markdown notes while keeping searches concise. Supplier `available` and both long-note fields are ordinary verified writes; computed supplier availability timestamps, in-stock, and on-order values remain read-only. Embedded records stay separate lookups, parameters/price breaks remain in their dedicated workflows (tags were added by F-S91), raw `barcode_hash` remains excluded, and API 530 `duplicate` inputs are write-only commands excluded pending separately approved guarded duplication workflows. F-S44 owns complete company exact reads (phone, email, contact, tax ID, external `link`, primary-image URL, notes, supplied/manufactured counts, customer role) and their guarded writes under the same duplication boundary; `primary_address` remains a separate structured-address lookup, parameters remain F-S64, and tags were added by F-S91.
  - F-S45 owns stock-item SKU, MPN, `expired`, `stale`, `sales_order_reference`, and `location_path` on the separate exact `get_stock_item` projection while `search_stock_items` stays concise; nested stock location/part/supplier-part detail remains separate, stock tags were added by F-S91 (`get_stock_item` only; no update tool), tests remain F-S57, and raw `barcode_hash` remains excluded pending F-S55. F-S46 projects new tracking detail through stable IDs and safe display fields rather than embedded full records.
  - F-S47 owns purchase-order notes, line/extra-line discount, and nullable total-price reads; supplier/order/part detail remains separate, tags were added by F-S91, and PO duplication remains F-S63.
  - Expanded F-S64 owns generic parameter values for purchase orders, stock locations, companies, supplier parts, manufacturer parts, and part categories, plus API 530 parameter-template `unique` administration; F-S67 owns location `path`, while category `path` remains part of the existing exact category read and F-S69 deletion plan.
  - F-S56 investigated all newly schema-visible tags and its implementation follow-up F-S91 added them across the seven covered object types; F-S58 owns embedded price-break/pricing workflows. Pinned inventories must classify null, omission, read-only, write-only, embedded-detail, and deferred behavior so future API drift cannot silently widen a tool.
  No additional story is required for the API 530 additions. F-S62 through F-S69 extend the operational inventory focus with purchase-order hold/resume/cancel and metadata maintenance, deferred PO duplication, cross-object parameters, stock custom status, deferred stock merge, location detail/type administration, and guarded location/category deletion.
- Build orders, BOM mutation, sales orders, return orders, and related sales/customer workflows remain deferred and are not implied by the PO, stock, location, and category coverage stories.
- F-S88 adds `global_search`, a bounded cross-object read-only tool over InvenTree's `POST /api/search/`. A live spike against pinned InvenTree 1.5.2/API 530 found the endpoint recognizes eleven object-type keys (`part`, `partcategory`, `stockitem`, `stocklocation`, `company`, `supplierpart`, `manufacturerpart`, `purchaseorder`, `salesorder`, `returnorder`, `build`) and returns each requested type's bucket as the same full list-serializer payload its own dedicated list endpoint would; `global_search` reuses each already-approved type's existing `search_*` projection unchanged rather than inventing a new reduced shape, and each bucket additionally reports `detail_tool`, the exact `get_*` tool name for a complete read. `salesorder`, `returnorder`, and `build` are excluded from the supported object-type list: inventree-mcp has no `get_*` tool for any of those object families, so a match could never route to an exact read, and exposing them would repeat the standing sales/customer/build exclusion without a concrete consumer. `search_regex`, `search_whole`, and `search_notes` are exposed as ordinary optional booleans; `search_notes` only changes which records match (verified live against a notes-only hit), not what a matched record's existing field-inventoried projection already returns. Because InvenTree silently omits any request key it does not recognize rather than erroring, `global_search` treats a requested-but-missing response bucket as upstream schema drift and fails the call closed instead of silently returning fewer types than asked for.

- F-S91 adds `search_tags`, a bounded read-only lookup over InvenTree's shared cross-object `/api/tag/` taxonomy (optional `model_type` scope, name `search`, explicit `limit`/`offset`, single bounded page rather than a full scan), and adds a `tags` field to the seven object types that already have `get_*`/`update_*` MCP tool coverage (`Part`, `Company`, `StockLocation`, `StockItem` read-only, `SupplierPart`, `ManufacturerPart`, `PurchaseOrder`), implementing F-S56's operator-approved proposal. Each covered exact-read client method now requests the underlying `?tags=true` query flag (a plain GET/PATCH omits `tags` entirely, confirmed live by F-S56 and re-confirmed for `SupplierPart`/`ManufacturerPart`/`PurchaseOrder` by this story); list/search client methods for the same objects do not request that flag, so `tags` stays absent from concise search results even where the search and exact-read tools share a Go struct (`StockLocation`). Each covered object's `update_*` tool accepts an ordinary optional `tags` field using the existing whole-array-replace PATCH convention already shipped for `Attachment` tags (`inventree.Set(tags)`; an explicit `[]` clears every tag) — no new OAuth scope or destructive annotation, matching F-S56's finding that InvenTree does not gate object-level tag writes beyond ordinary object write access. `StockItem` gets `tags` on `get_stock_item` only; no `update_stock_item` tag-assignment tool exists. Direct `/api/tag/` entity mutation (rename/delete a `Tag` row) stays out of MCP scope (staff-only upstream). `Build`, `SalesOrder`, `SalesOrderShipment`, `ReturnOrder`, and `TransferOrder` stay deferred until those object families get MCP tools of their own. `update_purchase_order`'s underlying `UpdatePurchaseOrderDetail` client method is the one Update*Detail method that requests `?tags=true` on its own PATCH request (rather than relying on a later `GetPurchaseOrderDetail` read-back) because that tool returns the PATCH response directly as its exact-read view.

Future workflows require a new product review pass before implementation.

### Testcontainers InvenTree Module

Create a small internal module that starts a disposable InvenTree stack for integration tests. Tests should share one container set per package or suite and run individual cases as isolated subtests instead of starting a full InvenTree stack for each test.

Target API:

```go
func TestIntegration(t *testing.T) {
    ctx, _, _ := testhandler.SetupTestHandler(t)
    shared, err := testenv.StartSharedInvenTree(ctx, testenv.Options{
        Image: "inventree/inventree:1.5.0",
    })
    require.NoError(t, err)
    t.Cleanup(func() {
        require.NoError(t, shared.Close(context.WithoutCancel(ctx)))
    })

    t.Run("create part", func(t *testing.T) {
        t.Parallel()
        ctx, _, _ := testhandler.SetupTestHandler(t)
        run, err := shared.NewRun(t)
        require.NoError(t, err)
        account, err := shared.Account(ctx, run, testenv.AccountAdmin)
        require.NoError(t, err)
        t.Logf("InvenTree test account username=%s run_prefix=%s", account.Username, run.Prefix)
        client, err := shared.Client(account)
        require.NoError(t, err)
        category, err := shared.EnsureFixture(ctx, account, run, testenv.FixtureCategory)
        require.NoError(t, err)
        // Use run.Prefix for every created object.
    })
}
```

Responsibilities:

- Start database dependencies, likely PostgreSQL, with `testcontainers-go`.
- Start the InvenTree services required for realistic API behavior. This may include the server, worker, proxy, and optional Redis/cache depending on the official stable deployment shape.
- Start containers with deterministic admin credentials.
- Run any required InvenTree setup, migrations, or startup commands.
- Create or retrieve per-run InvenTree users and API tokens for integration tests. Each subtest should request its own account before requesting its client or fixtures.
- Wait until authenticated API calls work before returning.
- Expose `BaseURL`, `Token`, and environment cleanup helpers.
- Provide helpers that create run-prefixed lookup fixtures for categories, locations, companies, parts, supplier parts, and BOMs only when a subtest asks for them.
- Share the container set across subtests while ensuring each subtest uses a unique run prefix.
- `SharedInvenTree` is owned by a parent suite test: the parent starts the environment once, runs child subtests underneath it, and cleans up only after those subtests complete. Do not add package-level or cross-package environment sharing unless the plan is explicitly changed.
- Subtests request the InvenTree user account/token, client, and run-scoped fixtures they need. The shared helper must not pre-create unrelated fixtures before subtests start.
- Ensure fixture helpers are idempotent within a run and prefix-isolated for parallel runs; sibling subtests should not need fixture-level coordination because every account and fixture name carries that subtest's run prefix.
- Provide helpers for unique names and run-scoped fixture lookup. Prefix format should be deterministic and collision-resistant, for example `IT_<runid>_<pkg>_<test>_`.
- Every account, mutating, or fixture helper must take a per-test `Run` object and refuse to create records without the current run prefix.
- Redact admin password and API token from logs and failure output.

Implementation notes:

- Prefer official InvenTree container images.
- Keep the module internal to avoid committing to a public testing API too early.
- Use fixture names with the per-test run prefix.
- Use a suite-root ownership model for shared environment lifecycle. Avoid first-caller-owned teardown for shared containers.
- Teardown is owned only by `TestMain` or the suite root. No subtest cleanup may stop shared containers.
- Cross-package container sharing is out of scope unless explicitly implemented.
- Validate Testcontainers options before startup and treat them as immutable after the shared environment starts.
- Use `t.Cleanup` only for per-run artifacts when safe, not for tearing down a shared container set that other subtests may still use.
- Design integration tests so subtests can call `t.Parallel()` without sharing mutable InvenTree records unless the test explicitly owns those records.
- Run-scoped fixtures are lookup data owned by the subtest's prefix. Every mutating subtest must create and own its own prefixed records.
- Do not provide shared destructive cleanup helpers for run-scoped records in the disposable Testcontainers environment. Leave data in place by default; tests that truly need cleanup because their data affects later assertions should be non-parallel and own narrowly scoped cleanup locally.
- Avoid global mutable test data that would make parallel subtests order-dependent.
- Keep destructive tests scoped to records created by that subtest's unique prefix.
- Log each generated InvenTree test username with the Go test run prefix so InvenTree logs that include usernames can be traced back to the owning subtest.
- Keep production credentials and user-provided `INVENTREE_TEST_URL` out of Testcontainers logs.
- If InvenTree requires multiple services for a realistic setup, wrap them behind one `StartInvenTree` helper rather than leaking container wiring into tests.
- Integration tests that require the shared InvenTree stack should live in one package or suite for milestone 1 so `GOFLAGS=-trimpath go test -race ./...` starts at most one shared stack. If additional packages need integration coverage, they should call into the same suite entrypoint or remain unit/fake-client tests until cross-package sharing is deliberately designed.
- Every exported InvenTree client method must have default-on Testcontainers integration coverage against the real InvenTree API before its implementation task is marked done. Unit and `httptest` coverage should still cover edge cases, error mapping, redaction, payload shape, and policy branches, but it is not a substitute for at least one live successful API-path exercise per client method.
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
- `docs/operator-recipes.md` must include first-release recipes for ChatGPT connector OAuth setup, STDIO setup, reverse-proxy HTTP deployment, add/update purchasable part, reuse existing parameters, add supplier/manufacturer links, create initial stock, upload/link attachment, set/replace primary part image, set/replace/clear a company primary image, preview purchase order lines, and resolve structured clarification prompts.
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
- HTTP OAuth metadata endpoint tests for the resource-derived path-specific route such as `/.well-known/oauth-protected-resource/mcp` and the issuer-derived authorization-server metadata route such as `/.well-known/oauth-authorization-server`.
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
- Tests for unauthenticated static `server/discover` or legacy `initialize`/`tools/list` behavior versus authenticated InvenTree-contacting tool execution, aligned with the MCP SDK and OAuth protected-resource behavior.
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
- Ambiguity tests proving duplicate matches return structured clarification responses with candidate IDs, applicable absolute `web_url` values, and sanitized relative `api_url` values.
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
- Link attachment URL-policy tests proving complete query/fragment preservation plus rejection of unsupported schemes, credentials/userinfo, malformed values, and local file references; optional link allowlist policy is enforced when configured.
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

These commands are local defaults. When the same suite runs in CI, release, or another pipeline context, add `-v` to the `go test` invocation or configure the pipeline wrapper to pass verbose test arguments.

| Suite | Command | Purpose |
| --- | --- | --- |
| Default | `GOFLAGS=-trimpath go test -race ./...` | Unit, contract, docs, and default-on pinned Testcontainers integration tests. |
| Unit-only | `GOFLAGS=-trimpath INVENTREE_TEST_SKIP_DOCKER=1 go test -race ./...` or `GOFLAGS=-trimpath go test -race -short ./...` | Fast tests with Docker-backed integration explicitly excluded. |
| Contract/docs | `GOFLAGS=-trimpath INVENTREE_TEST_SKIP_DOCKER=1 go test -race ./...` plus generated manifest checks | Tool annotations, scopes, schema references, and documentation drift without starting Docker. |
| HTTP auth | `GOFLAGS=-trimpath go test -race ./internal/server/... ./internal/oauth/...` | OAuth metadata, bearer challenge, token envelopes, and scope guards using fakes. |
| Integration | `GOFLAGS=-trimpath go test -race ./internal/testenv ./internal/integration/...` | Shared Testcontainers suite with pinned version-tag/schema pair. |
| Stable canary | CI-specific `inventree/inventree:stable` integration run | Non-blocking latest-stable compatibility and schema drift signal. |

Local commands should add `-v` only when verbose logs are useful for diagnosis or evidence. CI, release, and other pipeline commands should always include `-v` so successful logs preserve integration-test and container-output evidence.

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

Do not expand beyond the full beta list until the MVP loop and OAuth setup primitives are verified.

The full first beta milestone should include:

- Go module and buildable command.
- STDIO and HTTP transports.
- Stateless HTTP mode.
- HTTP OAuth protected resource, authorization-code with PKCE, token, refresh, encrypted envelope support, and per-tool scope enforcement as implementation primitives.
- Tool mutation metadata for every registered tool.
- PATCH-based partial update support in the client and first update tools.
- Part/category tools: `search_parts`, `get_part`, `search_part_categories`, `get_part_category`, `create_part_category`, `update_part_category`, `create_part`, `update_part`, `update_part_family_relationships`, `search_parameter_templates`, `get_part_parameters`, `set_part_parameters`.
- Company tools: `search_companies`, `search_suppliers`, `search_manufacturers`, `create_company`, `create_supplier_part`, `create_manufacturer_part`.
- Stock tools: `search_stock_locations`, `search_stock_items`, `create_stock_item`.
- Attachment/image tools: `list_attachments`, `get_attachment_metadata`, `download_attachment`, `download_part_image`, `upload_attachment`, `upload_attachment_from_url`, `create_link_attachment`, `update_attachment_metadata`, `delete_attachment`, `set_primary_image`, `set_company_image`, `set_company_image_from_url`, `clear_company_image`.
- Milestone attachment object scope: `part`, `stockitem`, `company`, `supplierpart`, `manufacturerpart`, and `purchaseorder`. Sales/return/transfer/build attachment support is deferred unless explicitly added later.
- Purchase-order attachment support in milestone 1 applies only to existing purchase orders found by ID/search. The milestone does not create purchase orders except through later explicitly enabled mutating workflows.
- Milestone primary image scope covers parts through an existing same-part attachment and existing companies through guarded direct PNG/JPEG/WebP sources. Company-image operations do not mutate company roles or add customer/sales workflows.
- Parameter behavior: `set_part_parameters` searches and reuses existing templates before writing and asks for operator clarification when unsure.
- Purchasing preview: `preview_purchase_order_with_lines`, with supplier-part validation and no writes.
- Structured clarification responses for ambiguous part/category/company/location lookups.
- Initial Testcontainers InvenTree test environment.
- GitHub Actions CI, Dependabot, golangci-lint config, and pre-commit config.
- README with quick-start links and minimal setup examples.

This milestone proves the transport, auth, client, schema, and data-entry patterns while completing a useful operator loop: add or update a purchasable part, associate supplier/manufacturer data, create initial stock, and preview a purchase order. F-S07 subsequently completed OAuth-protected production HTTP startup for already sealed access-token envelopes, and F-S08 added connector authorization/setup plus signed-client exchange. The milestone still does not declare production ChatGPT/HTTP connector deployment ready; remaining reverse-proxy enforcement and packaged-service validation remain gated follow-up work.

Blocking milestone tests:

- Connector-compatibility spike documented from current official OpenAI documentation before HTTP OAuth implementation starts.
- HTTP OAuth metadata and bearer challenge behavior.
- HTTP OAuth authorization-code with PKCE, token exchange, refresh, and encrypted envelope validation.
- Auth-code replay behavior tested according to the selected state strategy.
- Refresh absolute authorization/session expiry behavior.
- Configured OAuth issuer/resource URL positive and negative behavior for metadata, token audience, and protected-resource challenges.
- OAuth scope enforcement for read, write, upload, and destructive tool classes.
- Protected `/mcp` unauthenticated behavior verified: no MCP method dispatch without a valid access token unless the connector spike explicitly requires pre-auth static discovery; if allowed, only the documented static methods succeed and InvenTree-contacting tools fail before handler dispatch.
- Concurrent HTTP OAuth request isolation with different sealed InvenTree credentials.
- PATCH omission and zero-value table tests for `update_part`.
- State-bound assignment, replacement, clear, cycle, traversal-budget, response-loss recovery, and OAuth/annotation tests for `update_part_family_relationships`.
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

- Blocking tests must have deterministic local execution paths for milestone-scope behavior. Docker-backed integration tests run by default and can be explicitly excluded for unit-only, fast, or Docker-unavailable runs with `INVENTREE_TEST_SKIP_DOCKER=1` or `GOFLAGS=-trimpath go test -race -short`.
- Non-blocking tests may cover optional live external InvenTree instances, canary compatibility checks, and extended stress runs.
- Future tests must be tied to deferred scope such as production HTTP setup/deployment wiring, sales workflows, return orders, transfer orders, and build attachment support.
- Future image/file tests must cover deferred surfaces only when they enter scope, including notes image upload, generated report attachments, and stock test-result attachments.

Milestone README recipes:

- README should link to the corresponding `docs/operator-recipes.md` entries rather than duplicating full recipes.
- Required README links include ChatGPT OAuth setup, STDIO setup, reverse-proxy HTTP deployment, add/update purchasable part, add stock for an existing part, dry-run a purchase order, attach a datasheet or photo to a part, set or change a primary image, and add or update part parameters using existing templates.

## Resolved Product Decisions

- HTTP mode uses MCP-owned OAuth bearer tokens for ChatGPT Developer Connector compatibility and does not pass raw InvenTree `Authorization` headers through unchanged.
- HTTP OAuth tokens are encrypted, authenticated, stateless envelopes that seal the upstream InvenTree credential.
- STDIO mode supports configured `Token` or `Bearer` upstream InvenTree auth only.
- Blocking integration tests target InvenTree `1.5.2`, which reports API version `530` and passes the exercised client contracts from the checked-in schema baseline. This test pin does not define the minimum supported InvenTree version.
- Latest stable InvenTree is covered only by a non-blocking `inventree/inventree:stable` canary until schema/provenance updates are applied.
- Destructive operations are allowed behind confirmation and accurate MCP annotations.
- Tool inputs require API-required fields only, unless additional fields are needed to avoid ambiguous writes.
- F-S19 category identity is a trimmed case-insensitive name under one exact parent, bounded to 1,000 sibling candidates with fail-closed pagination. Reparenting with any direct-part or descendant content is allowed only after explicit hierarchy confirmation; structural promotion is refused while direct parts exist. Category writes remain non-destructive closed-world operations requiring read and write scopes.
