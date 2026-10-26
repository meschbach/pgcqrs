# Operations Guide

Operational surfaces of the pgcqrs service: health endpoints, test diagnostics, and how CI
archives them.

## Service health endpoints

The HTTP service exposes two ops endpoints (registered in `internal/service/service.go`):

| Endpoint          | Purpose                                                        |
|-------------------|----------------------------------------------------------------|
| `GET /ops/liveness`  | Liveness probe — is the process up and serving HTTP?         |
| `GET /ops/readiness` | Readiness probe — should traffic be routed to this instance? |

Both currently return a hardcoded `{"ok":true}`. Readiness therefore proves **listener-up only,
not database health**:

The ops endpoints run on their own listener configured via the `ops-listener` key (`internal/service/config.go`): address `PGCQRS_OPS_LISTENER_ADDRESS` (default `127.0.0.1:9002`) and enabled flag `PGCQRS_OPS_LISTENER_ENABLED` (default `true`, encoded as `*bool` so "unset" is distinguishable from `false`). When disabled (`PGCQRS_OPS_LISTENER_ENABLED=false`), the listener is not started and probes against its port will fail (connection refused / HTTP 404).

- `pgxpool.New` connects lazily (`internal/service/service.go`), so no Postgres round-trip
  happens at startup or during the probe.
- The suture supervisor starts child services asynchronously.
- The gRPC listener binds without touching Postgres.

In practice a 200 still transitively means "connected and serving" for the default local setup
because the service connects to Postgres *before* binding its listeners — but do not rely on
readiness as a database health check. Making readiness DB-aware is a known TODO.

### Known gaps

- **gRPC exposes no standard health service** (`grpc.health.v1.Health`). gRPC liveness can only
  be observed by dialing and issuing a request. Adding the standard health service is deferred
  future work.

## Probe wiring

- **Kubernetes/Helm** (`deploy/pgcqrs/templates/deployment.yaml`): the container's
  `livenessProbe` targets `/ops/liveness` and `readinessProbe` targets `/ops/readiness`, both on
  the `ops` port (9002).
- **Docker Compose** (`docker-compose.yaml`): the service container uses a healthcheck running
  `/service health-check ops` — a small bundled CLI that probes `/ops/readiness` on the ops
  listener (`PGCQRS_OPS_LISTENER_ADDRESS`, default `127.0.0.1:9002`).

## Integration test diagnostics

`integration-tests.sh` writes all diagnostics for a run into
`run/test-diagnostics/<run-id>/` under the repository root, where `<run-id>` is a UTC timestamp
plus mode plus PID (e.g. `20260823-141505-local-4711`), so consecutive runs never clobber each
other. Set `PGCQRS_TEST_DIAG_DIR` to override the base directory entirely.

### File inventory

Every run contains:

- `summary.txt` — mode, UTC timestamp, git SHA, Go version, per-phase result + duration, final
  verdict. Written incrementally, so partial runs still explain themselves.

Local mode adds:

- `server.log` — live-streamed service stderr (**stderr only**; stdout intentionally goes to the
  console so anomalies are visible). Survives hard kills because it streams as the service runs.
- `goroutines.txt` — goroutine dump, produced on failure only (see dump policy below).

Docked mode adds on failure:

- `docker-service.log` — full `docker logs --timestamps` capture from the service container,
  including a fresh goroutine dump when the container was running at failure time.

Each executed transport suite writes its own `test-memory.out`, `test-http.out`,
`test-grpc.out`. Startup failures legitimately precede all suites, so early-failed runs carry
fewer files.

### Cleanup

Diagnostic directories accumulate under `run/`. They are small (tens of KB per run) and
gitignored; remove them with:

```bash
rm -rf run/
```

### Readiness gating

Before any transport suite runs, the script polls `GET /ops/readiness` every 250ms for up to 15
seconds, treating only an HTTP 200 as ready:

- **local mode**: target derived from `${PGCQRS_OPS_LISTENER_ADDRESS:-127.0.0.1:9002}`,
  rewriting `0.0.0.0` to `127.0.0.1` (same convention as gRPC). Each poll iteration also
  checks the owned service process; if it dies mid-startup, the run fails immediately. The
  transport suites target the same resolved address.
- **docked mode**: fixed mapping `http://localhost:26002`.

### Migration note

The ops endpoints (`/ops/liveness`, `/ops/readiness`) were removed from the client HTTP
listener (`dataRoutes()`); requests to `/ops/*` on the client port (`9000`) now return `404`.
Probes and the `health-check ops` CLI must target the dedicated ops listener (`PGCQRS_OPS_LISTENER_ADDRESS`,
default `127.0.0.1:9002`). When the ops listener is disabled (`PGCQRS_OPS_LISTENER_ENABLED=false`),
no listener starts on that port and probes will fail (connection refused or `404`).

A readiness timeout fails the run before any suite executes and points at the diagnostics
directory.

### Goroutine dump policy (local mode)

When a run fails, teardown sends SIGQUIT to the owned service process, waits for death by
polling (2s cap, then SIGKILL), and derives `goroutines.txt` post-mortem by extracting
`server.log` from the runtime's `SIGQUIT: quit` marker onward. If no marker is present, the file
contains the annotated tail of `server.log` instead — it is never silently empty or absent. Dumps
are never printed to the console. When all transports pass, teardown is quiet: plain SIGTERM, no
dump.

### Docked-mode failure capture

On any docked failure (transport failure or readiness timeout) the script resolves the service
container via the Docker label filter `com.docker.compose.service=pgcqrs` (Compose-project
independent; includes stopped containers), preferring running containers with a newest-created
tie-break when several match (all matched IDs are listed in `summary.txt`; zero matches are noted
there too). If the chosen container is running, SIGQUIT is delivered through the Docker daemon
(`docker kill --signal=QUIT`) to append a fresh goroutine dump to the container logs, then
container output is copied into `docker-service.log`.

Because Docker ignores `restart: always` after a manual kill/stop, the script restores the
container afterwards via `docker start` (best effort; a failed restore warns on the console and
is recorded in `summary.txt` without altering the exit code).

## CI artifact behavior

Both GitHub workflows (`.github/workflows/main.yaml`, `.github/workflows/pr.yaml`) upload
`run/test-diagnostics/` as a job artifact named `integration-test-diagnostics-<go-version>` with
30-day retention whenever the job **fails or is cancelled** (`if: failure() || cancelled()`).
Cancellation is included deliberately: a hang tripping the job-level 5-minute timeout marks the
job cancelled, and plain `failure()` steps would never run — precisely the stuck-service case
this exists to debug.

CI runs the script in `local` mode (Postgres via raw `docker run`, service built natively), so
uploaded artifacts contain local-mode files (`server.log`, `goroutines.txt`, `test-*.out`,
`summary.txt`). The docked capture path is exercised only by manual verification pending
Docker-in-CI support.
