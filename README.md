# InvenTree MCP Server

Go-based Model Context Protocol server for common InvenTree data-entry workflows.

Current status: buildable command and typed configuration skeleton. MCP server runtime, tool registration, and HTTP OAuth are still planned work.

## Quick Start

Validate STDIO configuration:

```sh
INVENTREE_URL=https://inventory.example.test \
INVENTREE_TOKEN=redacted \
go run ./cmd/inventree-mcp serve --transport stdio
```

Useful STDIO options:

- `--inventree-auth-scheme Token` or `--inventree-auth-scheme Bearer`; default is `Token`.
- `--inventree-timeout 30s`; default is `30s`.
- `--inventree-tls-skip-verify`; intended only for local/test deployments and requires `--environment development`.

HTTP mode currently validates only the pre-OAuth skeleton. Production HTTP mode is intentionally disabled until the OAuth milestone is complete. Development-only HTTP config parsing requires `--environment development --dev-incomplete-oauth` and rejects configured raw InvenTree tokens.

Key documents:

- [Plan](docs/PLAN.md)
- [Implementation tasks](docs/TASKS.md)
- [API schema notes](docs/api-schema.md)
- [Reviewer roster](docs/reviewers.md)
- [Tool reference](docs/tool-reference.md)
- [Operator recipes](docs/operator-recipes.md)

The local OpenAPI schema snapshot is stored in `docs/api-schema.yaml`.
