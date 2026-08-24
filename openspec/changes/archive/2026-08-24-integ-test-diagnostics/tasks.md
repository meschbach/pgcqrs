## 1. Diagnostics directory scaffolding

- [x] 1.1 Add `/run/` to `.gitignore` (root-anchored so only the repo-root directory matches)
- [x] 1.2 In `integration-tests.sh`: replace `set -xe` with `set -e` plus explicit phase logging; anchor paths to the
  repo root (git toplevel with script-dir fallback); add diagnostics dir setup (`PGCQRS_TEST_DIAG_DIR` override
  replacing the base path; `<run-id>` = UTC timestamp + mode + `$$`; create without `mkdir -p` so same-second collisions
  surface); restrict to bash-3.2-compatible features throughout (no `declare -A`, `mapfile`, `${var,,}`, `&>>` — macOS
  `/bin/bash` is 3.2)
- [x] 1.3 Redirect service stderr to `<diag>/server.log` (replacing `/tmp/pgcqrs-server.log`) and test outputs to
  `<diag>/test-{memory,http,grpc}.out`

## 2. Readiness gate

- [x] 2.1 Implement shared bounded readiness poll (15s deadline) against `/ops/readiness` requiring a literal HTTP 200
  per attempt (`curl -fsS --max-time 1`; connection success alone is insufficient), targeting
  `${PGCQRS_LISTENER_ADDRESS:-localhost:9000}` in `local` mode (mirrors the service's own resolution,
  internal/service/config.go:42) and `http://localhost:26000` in `docked` mode, replacing fixed startup sleeps; in local
  mode check `kill -0 $SERVER_PID` each iteration and treat a dead owned process as immediate startup failure (protects
  against a stale instance answering 200 on the same port)
- [x] 2.2 On local-mode readiness timeout: record startup failure in summary, print server-log pointer, exit non-zero
  before any transport suite runs (the EXIT trap performs the SIGQUIT dump per 4.2 — no separate signal/dump call here,
  avoiding double-signaling)
- [x] 2.3 On docked-mode readiness timeout: record startup failure in summary, exit non-zero before any transport suite
  runs
- [x] 2.4 Derive local-mode transport URLs from the service's own resolution so an override cannot desync gate and
  suites: `HTTP_URL` reuses the readiness target (`${PGCQRS_LISTENER_ADDRESS:-localhost:9000}`); `GRPC_URL` derives from
  `${PGCQRS_GRPC_LISTENER_ADDRESS:-localhost:9001}` with a wildcard host (`0.0.0.0` — valid as a bind default,
  platform-dependent as a dial target) normalized to `127.0.0.1`; docked mode keeps its fixed compose-mapping URLs

## 3. Console policy and phase runner

- [x] 3.1 Extract a shared run-transport helper (env setup, go test invocation with output capture, duration
  measurement, `[transport] PASS/FAIL <dur>` console line) to replace the three copy-pasted blocks; invoke go test
  inside a condition context so `set -e` doesn't kill the script before exit codes are captured (the current memory/HTTP
  branches have exactly this latent bug — their `TEST_EXIT=$?` is unreachable on failure — while the gRPC branch's
  `if !` form is exempt). Measure durations with `date +%s` (whole seconds; macOS bash 3.2-safe — no `$EPOCHREALTIME`,
  no BSD-hostile `%N`). The helper MUST reproduce the per-suite environment exactly — memory sets
  `PGCQRS_TEST_TRANSPORT=memory`, HTTP *unsets* it, gRPC sets `grpc` (all three systest harnesses default unset to HTTP;
  preserving unset-vs-set avoids presence-sensitive drift)
- [x] 3.2 On transport failure: print last 40 lines of that phase's output plus full file path; keep exit non-zero
- [x] 3.3 Print closing line naming the diagnostics directory on both success and failure paths

## 4. Shutdown and goroutine dump policy

- [x] 4.1 Rework `dump_server_goroutines` (local mode): after the death-poll, derive `goroutines.txt` post-mortem by
  extracting `server.log` from the `SIGQUIT: quit` marker onward (`awk '/^SIGQUIT: quit$/ {on=1} on'`); if no marker is
  present, fall back to the last ~200 lines of `server.log` prefixed with
  `# no SIGQUIT marker found; tail of server.log` so the file is never silently empty and its provenance is evident;
  write into the diag dir without cat'ing it
- [x] 4.2 EXIT trap (sole owner of service signaling and dump extraction — no other code path signals the service):
  capture `$?` at entry, disable `set -e`, guard every command, re-exit with the captured status (a failing trap command
  under `set -e` replaces the verdict — e.g., `wait` returning 143 after signal-death flips a green run red). Non-zero
  entry status ⇒ SIGQUIT dump + poll-for-death (≤2s cap, escalate to SIGKILL); zero ⇒ plain TERM; BOTH paths use the
  same bounded wait (an unbounded success-path `wait` hangs the script and voids the self-bounding guarantee). Chain
  `trap 'exit 130' INT` and `trap 'exit 143' TERM` so bash actually runs the EXIT trap on Ctrl-C/CI cancellation (bash
  skips EXIT traps on untrapped fatal signals); their non-zero statuses feed the dump rule, so no separate abnormality
  flag is needed
- [x] 4.3 Docked failure path: resolve the service container via
  `docker ps -aq --filter label=com.docker.compose.service=pgcqrs` (label filter avoids Compose CLI-variant and
  project-name coupling; includes stopped containers) and read `.State.Running` via `docker inspect`; if running,
  `docker kill --signal=QUIT <id>` (best effort, never an error) and wait for dump flush; unconditionally copy
  `docker logs --timestamps` to `<diag>/docker-service.log` on any docked failure including readiness timeouts; then
  best-effort restore via `docker start <id>` (Docker ignores `restart: always` after manual kills) — a restore failure
  prints a console warning and records `container restore: FAILED` in summary.txt without altering the exit code.
  Resolution MUST handle zero matches (skip capture, record `no service container found` in summary) and multiple
  matches (prefer running containers, newest-created tie-break, warn on console and list matched IDs in summary;
  signal/capture only the chosen container)

## 5. Run summary

- [x] 5.1 Write `summary.txt` incrementally: mode, timestamp, git SHA, Go version header at start; per-phase result +
  duration as each completes; final verdict at end (including readiness-timeout case)

## 6. CI wiring

- [x] 6.1 In `.github/workflows/main.yaml`: replace the `/tmp/pgcqrs-goroutines-*.txt` glob upload step with
  `actions/upload-artifact@v7` of `run/test-diagnostics/`, `if: failure() || cancelled()`, `if-no-files-found: warn`,
  name suffixed by matrix Go version, retention 30 days
- [x] 6.2 In `.github/workflows/pr.yaml`: add the same artifact upload step (same action version and inputs, including
  `if: failure() || cancelled()`) for PR-build parity

## 7. Documentation

- [x] 7.1 Create `docs/operations.md` covering: `/ops/liveness` + `/ops/readiness` endpoints (documented honestly:
  readiness currently proves listener-up only — lazy pgxpool, async suture start, gRPC binds without DB contact — not
  database health), Helm probe wiring in `deploy/pgcqrs/templates/deployment.yaml` and compose healthcheck
  (`/service health-check http`), diagnostics directory contract and file inventory (including `docker-service.log`),
  cleanup guidance (`rm -rf run/`), readiness gating used in both modes, CI artifact behavior (workflows run `local`
  mode, so artifacts contain local-mode files; the docked path is manual-verification-only pending Docker-in-CI), the
  docked capture flow and its restore-via-`docker start` caveat, and the gRPC health-service gap
- [x] 7.2 Update `AGENTS.md`: fix the stale `docker-up.sh` port claim (actual host mappings are 26000/26001), and add
  light feature-pointer notes directing agents/humans to `docs/operations.md` for service health endpoints and test
  diagnostics

## 8. Verification

- [x] 8.1 Local passing run: verify console shows only phase lines + closing pointer; verify diag dir contents complete;
  confirm no `goroutines.txt`
- [x] 8.2 Induced failure run (e.g., stop postgres or point gRPC at a dead port): verify tail-and-pointer output,
  `goroutines.txt` extracted from `server.log` starting at the `SIGQUIT: quit` marker (or tail-fallback), non-zero exit
- [x] 8.3 Readiness-timeout run (e.g., break service startup): verify fast fail with server-log pointer before any
  transport suite
- [x] 8.4 Induced `docked` failure run: verify `docker-service.log` captures container output — including a fresh
  goroutine dump when the container was running — the service container is running again afterward (restored via
  `docker start`), and console output matches local-mode failure parity
- [x] 8.5 `docked` mode smoke: `./dev.sh integration` still works unchanged from caller's perspective
- [x] 8.6 Stale-port race: occupy port 9000 with a dummy listener, run local mode, verify immediate startup failure with
  server-log pointer (owned-process-death detection), no transport suites executed, non-zero exit
- [x] 8.7 Docked resolution edges: with an extra stopped container carrying the pgcqrs service label present (e.g.,
  `docker run --label com.docker.compose.service=pgcqrs busybox true`), induce a docked failure and verify the
  chosen-target warning fires, `summary.txt` lists all matched IDs, only the chosen container is signaled/restored; then
  with no labeled container present, verify the absence note in `summary.txt`, no `docker-service.log`, and unchanged
  non-zero verdict
