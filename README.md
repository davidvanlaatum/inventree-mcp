[![Go](https://github.com/davidvanlaatum/inventree-mcp/actions/workflows/go.yml/badge.svg)](https://github.com/davidvanlaatum/inventree-mcp/actions/workflows/go.yml)

# InvenTree MCP Server

Go-based Model Context Protocol server for common InvenTree data-entry workflows.

Current status: buildable MCP server skeleton with STDIO and development-only HTTP transports, plus a read-only health/version tool. HTTP OAuth and InvenTree workflow tools are still planned work.

## Quick Start

Run STDIO mode:

```sh
INVENTREE_URL=https://inventory.example.test \
INVENTREE_TOKEN=redacted \
go run ./cmd/inventree-mcp serve --transport stdio
```

Useful STDIO options:

- `--inventree-auth-scheme Token` or `--inventree-auth-scheme Bearer`; default is `Token`.
- `--inventree-timeout 30s`; default is `30s`.
- `--inventree-tls-skip-verify`; intended only for local/test deployments and requires `--environment development`.

HTTP mode currently runs only the pre-OAuth skeleton. Production HTTP mode is intentionally disabled until the OAuth milestone is complete. Development-only HTTP startup requires `--environment development --dev-incomplete-oauth` and rejects configured raw InvenTree tokens.

## Install From A Release

GitHub releases are produced by GoReleaser when a `vX.X.X` tag is pushed. Each release includes checksums, archived binaries for Linux, macOS, and Windows on `amd64` and `arm64`, plus Linux `deb`, `rpm`, and `apk` packages.

Linux packages install:

- `/usr/bin/inventree-mcp`
- `/etc/systemd/system/inventree-mcp.service`
- `/etc/inventree-mcp/inventree-mcp.env`

The packaged service is intended for HTTP mode behind a reverse proxy. Production HTTP mode will not start until OAuth support is implemented. Install packages now for file layout testing, but do not enable the systemd service until the OAuth milestone lands.

For a development-only pre-OAuth HTTP runtime smoke test, run the binary directly. This starts the skeleton streamable HTTP server with only static MCP metadata and the read-only health/version tool.

```sh
INVENTREE_URL=https://inventory.example.test \
INVENTREE_MCP_ENVIRONMENT=development \
INVENTREE_MCP_DEV_INCOMPLETE_OAUTH=true \
/usr/bin/inventree-mcp serve --transport http --listen 127.0.0.1:28686 --path /mcp
```

The default HTTP listen address is `127.0.0.1:28686`. The port is intentionally outside common HTTP development ports, below common Linux ephemeral ranges, and loopback-only by default.

The `apk` package installs the same binary, config template, and systemd unit as the `deb` and `rpm` packages. Alpine/OpenRC service management is not implemented yet; use the binary directly or add an operator-specific OpenRC unit outside the package.

## Maintainer Release Flow

From an up-to-date `main` commit:

```sh
git tag vX.X.X
git push origin vX.X.X
```

The `Release` GitHub Actions workflow runs tests, invokes GoReleaser, creates the GitHub release for the tag, and uploads the binary archives, packages, and checksums. Verify the completed release before announcing it:

```sh
gh release view vX.X.X --repo davidvanlaatum/inventree-mcp
```

GitHub repository setup required for first release:

- Actions are enabled for the repository.
- Workflow permissions allow the Go workflow to write coverage baselines to git notes and comment on pull requests.
- Coverage reporting uses GitHub job summaries, pull request comments, and git notes only; the `dvgoutils` gist-backed coverage badge and `COVERAGE_GIST_SECRET` were intentionally not copied.
- Workflow permissions allow `GITHUB_TOKEN` to create releases with `contents: write`.
- The `Release Preview` workflow passes on the release PR, including the GoReleaser snapshot package build.

Key documents:

- [Plan](docs/PLAN.md)
- [Implementation tasks](docs/TASKS.md)
- [API schema notes](docs/api-schema.md)
- [Reviewer roster](docs/reviewers.md)
- [Tool reference](docs/tool-reference.md)
- [Operator recipes](docs/operator-recipes.md)

The local OpenAPI schema snapshot is stored in `docs/api-schema.yaml`.
