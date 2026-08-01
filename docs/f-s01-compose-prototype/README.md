# F-S01 Compose Prototype

These files preserve the discarded F-S01 Compose prototype so the evaluation can be reproduced without retaining the Compose module or its dependency graph in the product module.

Run the prototype only in a disposable clean clone or worktree with Docker available:

```sh
mkdir -p internal/testenv/testdata/compose
curl --fail --location \
  https://raw.githubusercontent.com/inventree/InvenTree/6b237de54e4cbfd7f51daff8403c17869898d965/contrib/container/docker-compose.yml \
  --output internal/testenv/testdata/compose/inventree-1.4.3.yml
cp docs/f-s01-compose-prototype/test.override.yml internal/testenv/testdata/compose/test.override.yml
cp docs/f-s01-compose-prototype/compose_evaluation_integration_test.go.txt internal/testenv/compose_evaluation_integration_test.go
go get github.com/testcontainers/testcontainers-go/modules/compose@v0.43.0
INVENTREE_TEST_COMPOSE=1 go test ./internal/testenv -run '^TestComposeInvenTreeEvaluation$' -count=1 -v
```

The temporary test asserts the active service set, `ServiceContainer` access, post-start logs, the exact server-only loopback publication, endpoint discovery, runtime/API versions, deterministic named-token reuse, authenticated token use, `Down` success, and not-found responses for all service containers, Compose networks, and created volumes after cleanup. It logs startup, per-service log sizes, and cleanup timing without logging the token or test credentials. Sanitized output and the measured module delta are retained in [results.md](results.md).

Do not commit the generated Go/module changes from the disposable run. The authoritative evaluation and decision are in [Docker Compose Testcontainers Evaluation](../testcontainers-compose-evaluation.md).
