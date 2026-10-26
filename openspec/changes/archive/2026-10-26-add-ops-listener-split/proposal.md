# Proposal

## Why

Operational endpoints (`/ops/liveness`, `/ops/readiness`) currently share the client-facing HTTP listener (`internal/service/service.go:37`). Because operators and clients are distinct audiences, every client-facing middleware concern (authentication, CORS, security headers, rate limits) must special-case the ops routes, and the operational surface is exposed to whatever network can reach client traffic. A dedicated listener removes that tension and gives the operational surface room to grow (metrics, pprof) without re-litigating client-plane policy.

## What Changes

- **BREAKING**: Remove `/ops/*` routes from the client HTTP listener; requests to `/ops/*` on the client port will no longer be served.
- Add a dedicated ops listener serving `/ops/liveness`, `/ops/readiness`, and a static HTML index at `/`, with its own configurable bind address (`ops-listener` config key, `PGCQRS_OPS_LISTENER_ADDRESS` env var, default loopback address), enabled by default with an explicit disable flag (`PGCQRS_OPS_LISTENER_ENABLED`).
- Unify listener lifecycle: the ops listener joins the client HTTP and gRPC listeners on one shared lifecycle — each listener runs in its own goroutine, and the first failure (bind or mid-run, any listener) triggers a controlled shutdown that reports the error to the caller instead of crashing. This removes the client listener's panic-on-bind-failure and the gRPC listener's suture-supervised restart-with-backoff (the supervision tree drops out of service startup).
- Split the single router into two routers with different `/` routes: the client (data) router continues serving service-info JSON at `/`; the ops router serves an HTML index at `/` listing the ops endpoints with descriptions.
- Replace the `health-check http` CLI subcommand with `health-check ops`, which probes the ops listener's `/ops/readiness` endpoint.
- Update all in-repo consumers of the ops endpoints to target the ops listener: the integration-test readiness gate, the Compose healthcheck (now `health-check ops`), the Helm chart probes, and the operations documentation.

## Capabilities

### New Capabilities
- `service-ops-endpoints`: Exposes operational endpoints (liveness, readiness) on a dedicated listener, separate from client-facing HTTP and gRPC traffic, so operational policy and client policy can diverge.

### Modified Capabilities
- `integration-diagnostics`: The readiness-gating and operations-documentation requirements currently pin the probes to the client HTTP endpoint; they change to target the dedicated ops listener.

## Impact

- `internal/service/service.go`: split `routes()` into `dataRoutes()` (service info `/` + `/v1/*`) and `opsRoutes()` (`/` HTML index + `/ops/liveness` + `/ops/readiness`); replace the suture supervisor with a shared listener lifecycle coordinating client HTTP, gRPC, and ops (controlled shutdown, error returned to the caller).
- `cmd/service/main.go`: report the error returned by `Serve` and exit non-zero on the controlled shutdown.
- `internal/service/config.go`: add `OpsListenerConfig` (address, enabled flag, defaults, env vars).
- `internal/service/ops.go`: served by the new listener; add HTML index handler.
- `cmd/service/healthcheck.go`: replace `http` subcommand with `ops` (probe ops listener's `/ops/readiness`).
- `integration-tests.sh`: readiness gate targets the ops address.
- `docker-compose.yaml`: healthcheck uses `health-check ops`; publish ops port `26002:9002` and set `PGCQRS_OPS_LISTENER_ADDRESS=0.0.0.0:9002`.
- `deploy/pgcqrs/templates/deployment.yaml`: add the ops container port, set the ops listener address env var, and retarget liveness/readiness probes to the ops port.
- `docs/operations.md`, `AGENTS.md`: document the ops listener and its endpoints.
