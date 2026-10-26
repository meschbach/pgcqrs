# Tasks

## 1. Ops listener configuration

- [x] 1.1 Add `OpsListenerConfig` (address, enabled flag) to `internal/service/config.go`, defaulting to `127.0.0.1:9002` / `true` and overridable via `PGCQRS_OPS_LISTENER_ADDRESS` / `PGCQRS_OPS_LISTENER_ENABLED`; encode enabled as `*bool` so "unset" (true) is distinguishable from "false"; wire it into `Config.LoadDefaults`. Verify with a unit test asserting the default when unset and the env override when set for both fields.
- [x] 1.2 Document the `ops-listener` config key, its env vars (`PGCQRS_OPS_LISTENER_ADDRESS`, `PGCQRS_OPS_LISTENER_ENABLED`), and their defaults in `docs/operations.md`. Verify the doc names the keys, the variables, the defaults, and the consequence of disabling the listener.

## 2. Listener and route split

- [x] 2.1 Split `service.routes()` into `dataRoutes()` (`/`, `/v1/*`) and `opsRoutes()` (`/` HTML index, `/ops/liveness`, `/ops/readiness`). The client `/` stays on the data router serving the JSON service info (unchanged); the ops `/` serves a static HTML page listing the ops endpoints with short descriptions. Verify with a unit test asserting `dataRoutes()` returns HTTP 404 for `/ops/liveness` while `opsRoutes()` returns HTTP 200 for `/ops/liveness` and an HTML page at `/`.
- [x] 2.2 Implement a shared listener lifecycle used by all three listeners: each listener (client HTTP, gRPC, ops) runs in its own goroutine returning an error, and one coordinator selects on listener completion, listener error, and context cancellation; on the first failure it performs a controlled shutdown of the remaining listeners and returns the error to the caller. Remove the suture supervisor from service startup (the gRPC listener was its only member; `grpcPort.Serve` already returns an error and slots into the coordinator directly), and replace the client `serve()` panic on bind failure with a returned error; start the ops listener only when `OpsListenerConfig.Enabled` is true. Set the ops `http.Server` `ReadTimeout: 5s, WriteTimeout: 5s` (not mirroring the client's 30s; loopback is fast, a slow probe indicates a sick listener). Verify that occupying any listener's port (client, gRPC, or ops) yields a controlled startup failure with the reported bind error (no panic), that a free port yields HTTP 200 from the ops address, and that disabling the ops listener prevents it from starting.
- [x] 2.3 Add test coverage that the client listener no longer serves `/ops/*` and the ops listener does not serve `/v1/*`. Verify the tests pass.

## 3. Consumer migration

- [x] 3.1 Update `integration-tests.sh` readiness gating to poll `/ops/readiness` on the ops endpoint, independently of the client HTTP endpoint: in `local` mode derive it from `PGCQRS_OPS_LISTENER_ADDRESS` (default `127.0.0.1:9002`), rewriting a `0.0.0.0` host to `127.0.0.1` for the dial target (same convention as the gRPC URL rewrite); in `docked` mode target `http://localhost:26002`, matching the new compose publish of the ops port. Verify the readiness phase passes in a `local` run and in a `docked` run.
- [x] 3.2 Replace `health-check http` CLI subcommand with `health-check ops` in `cmd/service/healthcheck.go`: dial the ops address from `PGCQRS_OPS_LISTENER_ADDRESS` (default `127.0.0.1:9002`), rewriting a `0.0.0.0` host to `127.0.0.1` before dialing, `GET /ops/readiness`, exit 0 on HTTP 200 else non-zero. Update `docker-compose.yaml`: change healthcheck from `["CMD","/service","health-check","http"]` to `["CMD","/service","health-check","ops"]`, publish ops port `26002:9002`, and set `PGCQRS_OPS_LISTENER_ADDRESS` env var to `0.0.0.0:9002`. Update `deploy/pgcqrs/templates/deployment.yaml`: add a named container port `ops` (9002), set `PGCQRS_OPS_LISTENER_ADDRESS: 0.0.0.0:9002` in the template env (kubelet probes reach the pod IP, not loopback), and retarget `livenessProbe`/`readinessProbe` to the `/ops/*` paths on the `ops` port. Verify `docker compose up` reports healthy and `helm template` renders the probes against the ops port with the env set.
- [x] 3.3 Update `docs/operations.md` endpoint/probe documentation and the `AGENTS.md` health-endpoint references to the ops listener, including the migration note that `/ops/*` on the client port now returns 404 and the consequence of disabling the ops listener. Verify no remaining in-repo claim places the ops endpoints on the client port.

## 4. Integration verification

- [x] 4.1 Run `./dev.sh up` and confirm all transport suites pass with the readiness gate targeting the ops listener. Verify the run completes green and `summary.txt` shows a successful readiness phase.

## Workflow follow-up

- Archive the change after the project's review requirements are satisfied.
- Verify the archived result.
