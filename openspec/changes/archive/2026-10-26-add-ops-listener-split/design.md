# Design

## Context

See `proposal.md` — Why. Current state that shapes the approach:

- `service.routes()` (`internal/service/service.go:33`) builds a single `mux.Router` holding the service-info route `/`, the `/ops/*` subrouter, and the `/v1/*` subrouter.
- `service.serve()` (`internal/service/service.go:208`) runs one `http.Server` on `cfg.Listener.Address` with optional TLS, and it blocks. gRPC runs as a separate supervised service (`grpcPort`) added to a `suture` supervisor before `serve()` is called.
- `ListenerConfig.Address` defaults to `localhost:9000` (`PGCQRS_LISTENER_ADDRESS`); gRPC defaults to `0.0.0.0:9001`. Deployments override the HTTP address to `0.0.0.0:9000`.
- `todo.md` already records the intent: bind liveness/readiness on an operational port separate from HTTP/gRPC.

## Goals / Non-Goals

**Goals:**
- Serve `/ops/liveness` and `/ops/readiness` on a listener dedicated to operational traffic.
- Make that listener's bind address configurable, with a default and an environment variable.
- Tie the ops listener to the service lifecycle (starts and stops with the process; bind failure is a startup failure).
- Update every in-repo consumer of the ops endpoints in the same change.

**Non-Goals:**
- Making readiness meaningful (it currently always returns `true`; database-aware readiness is a separate concern from `todo.md`).
- Wiring a gRPC health service (`grpc.health.v1.Health`).
- Adding new operational endpoints such as metrics scrape or pprof (the HTML index at ops `/` lists existing endpoints; future endpoints are a separate concern).
- Terminating TLS on the ops listener. It is assumed to be reached over loopback or a cluster-internal network; TLS can be added later without changing the split.
- Running the ops listener as a separate process or container.

## Decisions

1. **Separate listener, not an exemption list.** Remove `/ops/*` from the client router entirely and serve it from a second `http.Server`. Alternative considered: keep one listener and teach each client-plane middleware (auth, CORS, headers) to skip `/ops`. Rejected because it couples client policy to operational policy forever and re-opens the question every time client middleware is added. A separate listener makes the boundary structural.

2. **Config key `ops-listener` with enablement flag.** Add `OpsListenerConfig` alongside `ListenerConfig`/`GRPCListenerConfig`, with env vars `PGCQRS_OPS_LISTENER_ADDRESS` (bind address) and `PGCQRS_OPS_LISTENER_ENABLED` (default `true`), and defaults `127.0.0.1:9002` / `true`. The loopback default is the **primary security control**: the ops surface is not exposed on the public/client network unless the operator deliberately overrides the bind address. The explicit enable flag (encoded as `*bool` so "unset" is distinguishable from "false") allows disabling the listener entirely; when disabled, the listener is not started and probes against the ops port fail. Deployments that need the kubelet to reach the probe set the address to a pod-reachable value (e.g. `0.0.0.0:9002`), mirroring how they already override the HTTP address. Anything that *dials* the ops listener (the `health-check ops` CLI, the integration-script readiness gate) rewrites a `0.0.0.0` host to `127.0.0.1` before dialing — `0.0.0.0` is a bind default, not a portable dial target, matching the existing gRPC convention in `integration-tests.sh`. Port `9002` follows the HTTP `9000` / gRPC `9001` sequence. Alternative considered: defaulting to `0.0.0.0:9002` so probes work unconfigured. Rejected to keep the default from widening the operational surface beyond loopback, consistent with the existing HTTP default.

3. **One shared listener lifecycle for all three listeners; no supervision tree for ops.** The ops listener does not join the suture supervisor. Instead, client HTTP, gRPC, and ops converge on a single listener lifecycle: each listener runs in its own goroutine and returns an error (`grpcPort.Serve` already has this shape; the client `serve()` is refactored from panic-on-error to returning the error), and one coordinator selects on listener completion, listener error, and context cancellation. The first failure — bind or mid-run, on any listener — triggers a controlled shutdown: the remaining listeners are stopped gracefully (reusing the existing telemetry graceful-shutdown pattern) and the error is returned to the caller, not crashed. `Serve` propagates the error so `cmd/service` reports it and exits non-zero; a signal-driven shutdown remains a clean exit. With the gRPC listener joining the same lifecycle, suture drops out of service startup entirely (it was only supervising `grpcPort`), and in-process restart-with-backoff is removed: orchestration (Compose `restart: always`, Kubernetes Deployments) owns restarts, and in-process restarts only mask listener failures from liveness probes. The ops listener is started only when `OpsListenerConfig.Enabled` is true (default). Alternative considered: a supervised service that wraps its bind error in `suture.ErrTerminateSupervisorTree` to fail the process through the supervisor. Rejected: it fails through the supervision machinery rather than a controlled shutdown, and leaves the three listeners on three different failure paths.

4. **Split the routers with different `/` routes.** `routes()` becomes `dataRoutes()` (service info `/` + `/v1/*`) and `opsRoutes()` (`/` HTML index + `/ops/liveness`, `/ops/readiness`). The data router continues serving the JSON service-info endpoint at `/`, unchanged. The ops router serves a static HTML index at `/` listing the ops endpoints with short descriptions (e.g., "returns `{\"ok\":true}` when the process is up"). The ops router carries no client middleware; the data router keeps its current middleware. This is the concrete mechanism that lets the auth change (see `add-service-authentication`) apply authentication unconditionally to the client listener.

5. **Ops server timeouts (5s/5s, not mirroring client's 30s).** The ops listener is a second `http.Server` with its own timeouts. Set `ReadTimeout: 5s, WriteTimeout: 5s` rather than mirroring the client server's `30s`. Loopback is fast; a probe taking >5s indicates a sick listener, not a slow one, and a shorter timeout makes a stuck ops listener die faster rather than linger. The `grpcPort` analogue (`runService`) uses short waits, supporting this philosophy.

## Risks / Trade-offs

- **Breaking probe location** → Any external consumer probing `/ops/*` on the client port gets a 404 after this change. Mitigation: update all in-repo consumers (`integration-tests.sh`, `docker-compose.yaml`, `deploy/pgcqrs/templates/deployment.yaml`) in this change and document the migration in `docs/operations.md`.
- **Kubernetes probe needs a non-loopback bind** → If a deployment relies on the loopback default, kubelet probes against the pod IP fail. Mitigation: the Helm deployment template sets the ops address explicitly, exactly as it already does for the HTTP listener.
- **Config drift across the three listeners** → Three address keys can diverge. Mitigation: document the three keys and their defaults in `docs/operations.md`.
- **Disabling the ops listener breaks probes** → If an operator sets `enabled: false` or `PGCQRS_OPS_LISTENER_ENABLED=false`, the listener is not started; probes against the ops port fail (HTTP 404 / connection refused), and orchestration may restart the container. Mitigation: document the consequence in `docs/operations.md` and the Helm chart values.
- **Readiness remains a stub** → Splitting the port does not make readiness meaningful. Mitigation: explicitly a non-goal here; tracked by the existing `todo.md` item.
- **In-process listener restarts are removed** → gRPC no longer restarts on failure (bind or mid-run) with backoff; any listener failure now ends the process in a controlled shutdown with the error reported. Mitigation: orchestration already owns restarts (`restart: always` in Compose, Deployment replicas in Kubernetes), and in-process restarts masked failures from probes, leaving half-alive processes; the behavior change is called out in the proposal.
