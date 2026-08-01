# Docker Compose Testcontainers Evaluation

F-S01 evaluated `github.com/testcontainers/testcontainers-go/modules/compose` v0.43.0 against the direct-container `internal/testenv` stack. The evaluation used the official InvenTree 1.4.3 [`contrib/container/docker-compose.yml`](https://github.com/inventree/InvenTree/blob/6b237de54e4cbfd7f51daff8403c17869898d965/contrib/container/docker-compose.yml) as its base and a test-only override.

## Prototype topology and overrides

The official file defines PostgreSQL, Redis, the InvenTree server, an InvenTree background worker, and Caddy. Current MCP integration tests exercise synchronous InvenTree API behavior and need only PostgreSQL, Redis, and the server. The worker and proxy do not add coverage to those paths.

The prototype override:

- pinned `INVENTREE_TAG=1.4.3`;
- removed fixed `container_name` values, restart policies, persistent bind mounts, and production `env_file` settings;
- supplied the same disposable database and bootstrap-admin settings as `internal/testenv`;
- ran the same migration and Gunicorn command as the direct stack;
- published only the InvenTree web port, using an ephemeral loopback-only binding;
- placed the worker and proxy behind an opt-in Compose profile so the core comparison started only database, cache, and server services.

The exact discarded override, Go harness, and reproduction command are retained in [`docs/f-s01-compose-prototype`](f-s01-compose-prototype/README.md).

The worker and Caddy could be enabled for a deployment-compatibility test, but they are not backend prerequisites for the current MCP integration suite. Enabling them would add two containers, proxy configuration and mounts, another InvenTree process, and additional readiness and cleanup work without exercising a current client contract.

## Results

The comparison ran on 2026-08-01 using Docker Desktop 29.6.2 on macOS, warm local images, Testcontainers for Go v0.43.0, and `inventree/inventree:1.4.3`. Timings are single-run observations, not benchmarks.

| Criterion | Compose prototype | Current `internal/testenv` stack |
| --- | --- | --- |
| Startup | Core topology ready in 36.768s, 39.185s, and 52.825s across three focused runs. | Ready in about 43s during a 55.08s full test. |
| Logs | `ServiceContainer(...).Logs(...)` returned logs for all three services after startup. Live forwarding would require separately attaching log consumers. | Streams filtered container logs to `testing.T` during startup and after `Start` returns. |
| Inspection | `ServiceContainer` exposed all three containers for inspection. | The environment owns direct `testcontainers.Container` handles and inspects them without a service-name lookup. |
| Endpoint discovery | `ServiceContainer("inventree-server").PortEndpoint(...)` returned the ephemeral loopback URL. | The server container returns the same endpoint directly. |
| Published ports | Only server port 8000 was published, on `127.0.0.1`; database and cache remained network-internal. | Database, cache, and server use explicit ephemeral `127.0.0.1` bindings. |
| Readiness | Per-service Testcontainers waits worked after adding the current 200/401/403 matcher for `/api/version/`. Compose `depends_on` alone was not sufficient for application readiness. | Uses explicit database, cache, and server wait strategies, including the same version-endpoint matcher. |
| Token setup | Two calls for the named bootstrap token returned the same token, and the token authenticated successfully. | Creates and proves the same deterministic named bootstrap token. |
| Cleanup | `Down(RemoveOrphans(true), RemoveVolumes(true))` completed in 10.727s, 10.572s, and 10.587s. The final strengthened rerun required Docker not-found errors for all three containers, the Compose network, and created volumes. `Close()` was also required to release Compose and Docker client transports. | Terminates owned containers in reverse order and removes the dedicated network in about 12s. |
| Dependency cost | Substantially expands and upgrades the Compose/Docker/BuildKit/containerd/OpenTelemetry transitive graph; the evaluated version also required a higher Go patch directive. | Uses the already-required Testcontainers core and PostgreSQL module. |
| Configuration drift | Requires carrying or fetching an upstream Compose file plus a merge-sensitive override using Compose-specific reset/override tags. | Declares the three required services directly in Go next to their waits, security settings, and cleanup behavior. |

The observed startup and cleanup differences are too small and cache-sensitive to establish a performance advantage. Compose starts database and cache concurrently, while the direct stack starts them sequentially, but InvenTree migrations dominate both paths.

## Decision

Reject Compose for the current integration environment. It should neither replace the direct stack nor be retained as an optional canary now.

The prototype proved that the module can supply the required inspection, endpoint, readiness, token, port-binding, and cleanup capabilities. It did not provide a material coverage or timing benefit, and its additional per-service live-log setup, large dependency increase, duplicate upstream configuration, override drift risk, and extra `Down` plus `Close` lifecycle make it less maintainable for the present three-service test topology.

Reconsider a Compose compatibility path only if a future MCP contract depends on worker behavior, proxy/static/media behavior, or another official service that would make deployment-topology parity materially valuable. A future evaluation must still pin the InvenTree image, keep host ports loopback-only, use explicit service waits, prove deterministic credential setup, and measure CI cost before becoming blocking.
