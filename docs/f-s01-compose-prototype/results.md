# Sanitized Prototype Evidence

Environment: Docker Desktop 29.6.2 on macOS, warm local images, Testcontainers for Go v0.43.0, `inventree/inventree:1.4.3`, 2026-08-01. Token values and routine container log content are intentionally omitted.

## Compose prototype

Command:

```sh
INVENTREE_TEST_COMPOSE=1 go test ./internal/testenv -run '^TestComposeInvenTreeEvaluation$' -count=1 -v
```

Relevant output:

```text
=== RUN   TestComposeInvenTreeEvaluation
Compose core topology startup completed in 36.768s
Compose service inventree-cache exposed 598 bytes of logs
Compose service inventree-db exposed 110288 bytes of logs
Compose service inventree-server exposed 3731 bytes of logs
Compose Down cleanup completed in 10.587s
--- PASS: TestComposeInvenTreeEvaluation (47.76s)
PASS
ok github.com/davidvanlaatum/inventree-mcp/internal/testenv 48.316s
```

This final focused rerun used the retained harness after strengthening its assertions for the exact published-port set and Docker not-found classification after removing all containers, Compose networks, and created volumes. Earlier successful prototype runs reported 39.185s and 52.825s startup with 10.727s and 10.572s cleanup. The passing assertions are preserved in `compose_evaluation_integration_test.go.txt`; the output alone is not treated as proof of those assertions.

## Direct-stack comparison

Command:

```sh
go test ./internal/testenv -run '^TestStartInvenTreeStack$' -count=1 -v
```

Relevant output:

```text
starting InvenTree integration stack with image inventree/inventree:1.4.3, expected version 1.4.3, expected API 511
Container is ready: inventree/inventree:1.4.3
--- PASS: TestStartInvenTreeStack (55.08s)
PASS
ok github.com/davidvanlaatum/inventree-mcp/internal/testenv 55.426s
```

Container timestamps placed direct-stack application readiness at about 43 seconds. The full result includes direct-stack cleanup.

## Dependency delta

In a fresh checkout, this command:

```sh
go get github.com/testcontainers/testcontainers-go/modules/compose@v0.43.0
```

produced this repository diff summary:

```text
go.mod | 111 ++++++++++++++++++++++++++++----
go.sum | 225 +++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++
2 files changed, 324 insertions(+), 12 deletions(-)
```

`go list -m all` grew from 99 to 439 modules. The `go` directive changed from 1.25.0 to 1.25.5. The direct `go.mod` delta added the Compose module and Compose/Docker/BuildKit/containerd-related requirements, including:

```text
github.com/compose-spec/compose-go/v2 v2.11.0
github.com/containerd/containerd/api v1.10.0
github.com/containerd/containerd/v2 v2.2.4
github.com/docker/buildx v0.33.0
github.com/docker/cli v29.5.1+incompatible
github.com/docker/compose/v5 v5.1.4
github.com/docker/docker v28.5.2+incompatible
github.com/moby/buildkit v0.29.0
github.com/testcontainers/testcontainers-go/modules/compose v0.43.0
```

It also upgraded existing containerd, Docker connection/client, OpenTelemetry, OAuth2, gRPC, and protobuf requirements. The discarded prototype returned `go.mod` and `go.sum` to their exact pre-evaluation contents; no dependency change is retained by F-S01.
