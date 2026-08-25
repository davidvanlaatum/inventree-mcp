![Go Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/davidvanlaatum/709e99cf973e064f68cf3937b3d5c633/raw/coverage.json)
[![Go](https://github.com/davidvanlaatum/inventree-mcp/actions/workflows/go.yml/badge.svg)](https://github.com/davidvanlaatum/inventree-mcp/actions/workflows/go.yml)

# InvenTree MCP Server

Go-based Model Context Protocol server for common InvenTree data-entry workflows.

Current status: milestone 1 STDIO workflows are implemented for part/company entry, parameters, initial stock, attachments/images, purchase previews, and prompt checklists. Production HTTP startup serves the protected streamable HTTP `/mcp` endpoint plus an MCP-owned ChatGPT connector authorization/setup flow using CIMD `private_key_jwt`, PKCE S256, encrypted token envelopes, and per-tool scope checks. Reverse-proxy hardening and live packaged deployment validation remain follow-up work.

## Supported InvenTree Versions

This table records the InvenTree version and API revision the blocking Testcontainers integration suite verified against for each released `inventree-mcp` version. It documents tested compatibility only; it is not a claim of minimum supported InvenTree version. See [Compatibility Decisions](docs/PLAN.md#compatibility-decisions) for the full policy. The last row always tracks the InvenTree pin currently in effect on `main`, shown as its shipped tag once released; it instead reads `` `main` (unreleased) `` whenever that pin has not yet shipped in a tagged release.

<!-- BEGIN inventree-compat-table -->
| inventree-mcp version | InvenTree version | API revision |
| --- | --- | --- |
| `v0.0.1` | 1.4.0 | 511 |
| `v0.0.2`–`v0.0.10` | 1.4.3 | 511 |
| `v0.0.11`–`v0.0.12` | 1.5.0 | 530 |
| `v0.0.13+` | 1.5.1 | 530 | <!-- inventree-compat-table:current-row -->
<!-- END inventree-compat-table -->

## Quick Start

Run STDIO mode:

```sh
INVENTREE_URL=https://inventory.example.test \
INVENTREE_TOKEN=redacted \
go run ./cmd/inventree-mcp serve --transport stdio
```

Useful STDIO options:

- `--config /path/to/inventree-mcp.yml` loads an operator YAML configuration file. Without `--config`, the server uses the first existing file in this order: `./inventree-mcp.yml`, `./inventree-mcp.yaml`, `os.UserConfigDir()/inventree-mcp/config.yml`, `os.UserConfigDir()/inventree-mcp/config.yaml`, then on Unix `/etc/inventree-mcp/config.yml` and `/etc/inventree-mcp/config.yaml`.
- `--inventree-web-url https://inventory.example.test/web` or `INVENTREE_WEB_URL=https://inventory.example.test/web`; optional exact browser-frontend mount for returned object links. When omitted, every mode uses `INVENTREE_URL` plus InvenTree's stock `/web` frontend mount.
- `--inventree-auth-scheme Token` or `--inventree-auth-scheme Bearer`; default is `Token`.
- `--inventree-timeout 30s`; default is `30s`.
- `--upload-allow-root /trusted/path` or `INVENTREE_UPLOAD_ALLOW_ROOTS=/trusted/path`; enables STDIO local-file uploads from trusted operator-controlled roots.
- `--upload-max-bytes 10485760` or `INVENTREE_UPLOAD_MAX_BYTES=10485760`; raises or lowers the upload byte limit.
- `--debug-traffic-log /secure/path/mcp-traffic.jsonl` or `INVENTREE_MCP_DEBUG_TRAFFIC_LOG=/secure/path/mcp-traffic.jsonl`; appends MCP request/response traffic for local debugging. Treat the file as sensitive because it can contain tool arguments, results, and credentials supplied by the MCP client.
- OpenTelemetry tracing is disabled by default. Enable it with `--otel-enabled` or `INVENTREE_MCP_OTEL_ENABLED=true`, then configure `--otel-exporter otlpgrpc|otlphttp`, `--otel-endpoint`, optional repeated `--otel-header key=value`, `--otel-insecure` for a trusted local non-TLS collector, `--otel-sample-ratio`, `--otel-batch-timeout`, and `--otel-export-timeout`. Equivalent YAML keys are `otel_enabled`, `otel_exporter`, `otel_endpoint`, `otel_headers`, `otel_insecure`, `otel_sample_ratio`, `otel_batch_timeout`, and `otel_export_timeout`. Use a `host:port` endpoint for either exporter or a full `http(s)://...` URL for OTLP/HTTP; header values may contain exporter credentials, so protect the YAML file and process environment accordingly. Tracing carries W3C trace context through MCP methods and outbound InvenTree/OAuth HTTP requests without recording arguments or response payloads; numeric top-level tool identifiers are recorded only when supplied under an `id` or `*_id` field. Metrics/Prometheus export remains a follow-up slice.
- `--inventree-tls-skip-verify`; intended only for local/test deployments and requires `--environment development`.

YAML configuration uses the same typed settings as the environment variables and flags. Precedence is defaults < YAML < environment < CLI flags; higher-precedence list values replace lower-precedence lists, while repeated CLI list flags are combined. YAML may contain InvenTree and OAuth secrets. On Linux and macOS, protect loaded config files with owner-only permissions such as `chmod 600`; Windows deployments must protect them with ACLs. See the [commented YAML example](docs/examples/inventree-mcp.yml) and [operator recipes](docs/operator-recipes.md) for the complete path and security rules.

For first-release workflow details, use [Operator recipes](docs/operator-recipes.md). For exact registered tool metadata, use [Tool reference](docs/tool-reference.md) and the checked [tool manifest](docs/tool-manifest.json).

HTTP production mode requires MCP-owned OAuth settings and rejects raw `INVENTREE_TOKEN` runtime credentials. Required settings are:

- `INVENTREE_MCP_OAUTH_ISSUER_URL`: public HTTPS issuer URL.
- `INVENTREE_MCP_OAUTH_RESOURCE_URL`: public HTTPS MCP resource URL, normally the public `/mcp` URL.
- `INVENTREE_MCP_OAUTH_KEYS`: comma-separated `key-id:active|decrypt_only:base64-32-byte-key` entries supplied through protected environment configuration; OAuth key material is not accepted as a CLI flag.
- `INVENTREE_MCP_OAUTH_CLIENT_IDS`: comma-separated allowed OAuth `client_id` metadata URLs.
- `INVENTREE_MCP_TRUSTED_PROXY_CIDRS`: comma-separated CIDRs for reverse proxies allowed to supply `X-Forwarded-For` client hops.
- `INVENTREE_URL`: HTTPS InvenTree base URL used for upstream API calls after the MCP OAuth envelope is validated.
- Optional `INVENTREE_WEB_URL`: exact canonical public HTTPS InvenTree frontend mount for returned `web_url` and `parent_web_url` values, normally ending in `/web` but supporting an operator-configured custom InvenTree basename. When omitted, links use `INVENTREE_URL` plus the stock `/web` mount; any deployment prefix in `INVENTREE_URL` is preserved.
- Optional `INVENTREE_MCP_OAUTH_ACCESS_LIFETIME`, `INVENTREE_MCP_OAUTH_REFRESH_LIFETIME`, and `INVENTREE_MCP_OAUTH_SESSION_LIFETIME`.

The configured client metadata document must advertise `private_key_jwt`, the ChatGPT redirect URI shown by its app management page, and a same-origin HTTPS `jwks_uri`. The server validates each token request's signed client assertion against that JWKS and rejects assertion replay within one running process; shared replay state is required across restarts or multiple replicas. It does not accept an unsigned public-client downgrade.

Setup creates a uniquely named `inventree-mcp-chatgpt-*` InvenTree token rather than rotating the submitted credential or an earlier connector token. Revoke unused dedicated tokens in InvenTree after abandoned, expired, or retired connector authorizations; the database-free server does not retain token IDs for automatic cleanup.

Production supports path-preserving reverse proxies only. `INVENTREE_MCP_PATH` must exactly match the path in `INVENTREE_MCP_OAUTH_RESOURCE_URL`, including any public prefix, and the proxy must forward that path unchanged. Canonical OAuth URLs always come from the configured issuer/resource URLs; `X-Forwarded-Host`, `X-Forwarded-Proto`, and `X-Forwarded-Prefix` are ignored. `X-Forwarded-For` is used only when the immediate peer is in `INVENTREE_MCP_TRUSTED_PROXY_CIDRS`; the normalized client address is then used for rate limiting and request-scoped logging without recording the raw forwarding header.

Development-only HTTP startup remains available with `--environment development --dev-incomplete-oauth`; it registers only the development server surface and still rejects configured raw InvenTree tokens.

HTTP mode bounds each MCP request with `--mcp-max-request-body-bytes 15000000` or `INVENTREE_MCP_MAX_REQUEST_BODY_BYTES=15000000`. The limit must cover inline upload base64 plus JSON overhead and does not constrain STDIO uploads.

The debug traffic log option also applies to development HTTP mode. HTTP logging records request URIs, request bodies, response bodies, and streaming response chunks. Request bodies and non-streaming responses are captured up to 1 MiB with `body_truncated:true` when more data was forwarded; streaming response chunks are capped individually. Valid returned `web_url` and `parent_web_url` authorities, including internal fallback authorities, can appear in this sensitive response capture. Requests exceeding the configured MCP request limit fail closed.

## Install From A Release

GitHub releases are produced by GoReleaser when a `vX.X.X` tag is pushed. Each release includes checksums, archived binaries for Linux, macOS, and Windows on `amd64` and `arm64`, plus Linux `deb`, `rpm`, and `apk` packages. The same release publishes the multi-architecture container image [`ghcr.io/davidvanlaatum/inventree-mcp`](https://github.com/davidvanlaatum/inventree-mcp/pkgs/container/inventree-mcp) with `vX.X.X` and `latest` tags for Linux `amd64` and `arm64`.

The container runs as a non-root user and defaults to the HTTP transport on port `28686` at `/mcp`. Configure the production OAuth and InvenTree environment variables before deploying it; the image does not embed credentials or deployment configuration.

Direct release-archive installations on Linux and macOS can update explicitly with:

```sh
inventree-mcp self-update
inventree-mcp self-update --version vX.Y.Z
```

Before the first update, a direct archive installation must be explicitly recorded once with `inventree-mcp self-update --adopt-direct-install`. Adoption performs no network request or update and must be used only after confirming the current binary came from this repository's canonical GitHub release archive. The command supports stable direct-archive binaries on `amd64` and `arm64`; it refuses prereleases, downgrades, Windows, known package paths, missing/unsafe adoption markers, unsafe ownership/link/directory state, and development builds without changing the installed binary. It verifies the canonical GitHub release archive against `checksums.txt`, retains the prior binary as `inventree-mcp.previous`, and never runs automatically or through MCP. Release `v0.0.1` predates the required single-binary archive format, so the next release is the first valid self-update target. See [Local CLI self-update policy](docs/self-update.md) for the trust, manual recovery, and residual-risk details.

Linux packages install:

- `/usr/bin/inventree-mcp`
- `/etc/systemd/system/inventree-mcp.service`
- `/etc/inventree-mcp/config.yml`

The packaged service is intended for HTTP mode behind a path-preserving reverse proxy. Production HTTP startup validates OAuth envelope keys, issuer/resource URLs, exact public/internal MCP path alignment, trusted proxy CIDRs, allowed client IDs, and token lifetimes before serving protected MCP traffic and connector authorization routes. The systemd unit uses `Type=notify`, reports ready only after those checks pass and the HTTP listener is bound, and sends watchdog heartbeats every half of systemd's configured 30-second watchdog interval. Once the managed HTTP lifecycle is initialized, graceful shutdown and fatal runtime failures publish sanitized service status. Configuration or logger initialization failures that occur before that lifecycle starts still exit non-zero, allowing systemd to record the failure and apply `Restart=on-failure` without a separate status notification path. If a heartbeat cannot be delivered, the server reports a degraded status and keeps serving; systemd's watchdog timeout terminates it and starts a replacement. Install packages now for file layout testing, but do not enable the systemd service for a live ChatGPT connector until F-S10 live packaged deployment validation lands.

Edit `/etc/inventree-mcp/config.yml` (mode `0600`, owner-only) to set the InvenTree URL, OAuth key material, and any other packaged HTTP-mode settings; the packaged unit runs `inventree-mcp serve --config /etc/inventree-mcp/config.yml`.

For a development-only pre-OAuth HTTP runtime smoke test, run the binary directly. This starts the skeleton streamable HTTP server with only static MCP metadata and the read-only health/version tool.

```sh
INVENTREE_URL=https://inventory.example.test \
INVENTREE_MCP_ENVIRONMENT=development \
INVENTREE_MCP_DEV_INCOMPLETE_OAUTH=true \
/usr/bin/inventree-mcp serve --transport http --listen 127.0.0.1:28686 --path /mcp
```

The default HTTP listen address is `127.0.0.1:28686`. The port is intentionally outside common HTTP development ports, below common Linux ephemeral ranges, and loopback-only by default.

The `apk` package installs the same binary, YAML config template, and systemd unit as the `deb` and `rpm` packages. Alpine/OpenRC service management is not implemented yet; use the binary directly or add an operator-specific OpenRC unit outside the package.

Package installations must be upgraded with their package manager. `inventree-mcp self-update` deliberately refuses `/usr/bin/inventree-mcp` and never overwrites package-owned files.

## Maintainer Release Flow

From an up-to-date `main` commit:

```sh
git tag vX.X.X
git push origin vX.X.X
```

The `Release` GitHub Actions workflow runs tests, invokes GoReleaser, creates the GitHub release for the tag, uploads the binary archives, packages, and checksums, and publishes the container image to GHCR. Verify the completed release before announcing it:

```sh
gh release view vX.X.X --repo davidvanlaatum/inventree-mcp
```

GitHub repository setup required for first release:

- Actions are enabled for the repository.
- Dependabot version updates are enabled by `.github/dependabot.yml` for Go modules, GitHub Actions, and pre-commit hooks.
- Workflow permissions allow the Go workflow to write coverage baselines to git notes and comment on pull requests.
- `COVERAGE_GIST_SECRET` is configured with permission to update gist `709e99cf973e064f68cf3937b3d5c633` for the coverage badge.
- Workflow permissions allow `GITHUB_TOKEN` to create releases with `contents: write`.
- Workflow permissions allow `GITHUB_TOKEN` to publish packages with `packages: write`; make the GHCR package public in its package settings when the image should be publicly pullable.
- The `Release Preview` workflow passes on the release PR, including the GoReleaser snapshot package build.

Key documents:

- [Plan](docs/PLAN.md)
- [Implementation tasks](docs/TASKS.md)
- [API schema notes](docs/api-schema.md)
- [Reviewer roster](docs/reviewers.md)
- [Tool reference](docs/tool-reference.md)
- [Checked tool manifest](docs/tool-manifest.json)
- [Operator recipes](docs/operator-recipes.md)

The local OpenAPI schema snapshot is stored in `docs/api-schema.yaml`.
