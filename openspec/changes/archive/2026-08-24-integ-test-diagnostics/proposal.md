## Why

Integration test diagnostics currently land in the wrong places: passing runs dump ~66% stack-trace noise into the
console (the exit trap unconditionally cats a goroutine dump), diagnostic files pile up in `/tmp` under PID-suffixed
names with no lifecycle, and CI uploads artifacts by globbing `/tmp/pgcqrs-goroutines-*` — a guess that misses entirely
on PR builds (`pr.yaml` has no upload step) and can never fire when a hang trips the 5-minute job timeout (cancelled
jobs skip `failure()` steps). Debugging a stuck service means archaeology across three inconsistent surfaces.

## What Changes

- Establish `run/test-diagnostics/<run-id>/` as the single diagnostics directory for integration runs, with mode-scoped
  contents: per-transport test outputs and a run summary (mode, git SHA, Go version, timings, verdict) in every run;
  live-streamed server log and goroutine dump in local runs; container log capture in docked failures.
- Rework `integration-tests.sh` console policy: phase banners with timing and verdicts replace `set -x` tracing;
  diagnostics write to files always and print to console never (short tail of failing output plus file pointer only on
  failure).
- Replace fixed startup sleeps with a bounded readiness poll against the existing `/ops/readiness` endpoint (same signal
  the Helm chart probes in `deploy/pgcqrs/templates/deployment.yaml`) in **both modes** — local (port 9000) and docked
  (port 26000) — failing fast with a diagnostics pointer when the service never becomes ready.
- Self-bound total runtime (~30s worst case) so the script always exits on its own, keeping CI's 5-minute job timeout as
  pure safety net.
- Capture stack dumps to file without printing; stop dumping entirely when all transports pass (quiet kill instead of
  SIGQUIT dump) in local mode.
- Add docked-mode failure diagnostics with console parity: signal the service container through the Docker daemon when
  it is running (`docker kill --signal=QUIT` — no in-container tooling required for the scratch-based image),
  unconditionally capture container logs into the run's diagnostics directory (crash output survives even when no dump
  is obtainable), and restore the container afterward (Docker ignores `restart: always` after manual kills).
- Update both GitHub workflows: upload `run/test-diagnostics/` as a job artifact with deterministic path on failure *or*
  cancellation (`if: failure() || cancelled()` — cancellation skips plain `failure()` steps, which previously forfeited
  streamed evidence), add the missing upload step to `pr.yaml`.
- Add `run/` to `.gitignore`.
- Document the operations story — ops endpoints, k8s probe wiring, diagnostics directory contract, CI artifact behavior,
  gRPC health gap — in a new `docs/operations.md`.

## Capabilities

### New Capabilities

- `integration-diagnostics`: The diagnostics contract for integration test runs — where diagnostic files live, what the
  console shows, how readiness gating works, and how CI archives diagnostics.

### Modified Capabilities

## Impact

- **Scripts**: `integration-tests.sh` (rework), `dev.sh` untouched (calls it unchanged); docked-mode failure path
  requires the `docker` CLI (already a prerequisite of docked mode)
- **CI**: `.github/workflows/pr.yaml`, `.github/workflows/main.yaml` (artifact upload replaces `/tmp` glob hack)
- **Repo**: `.gitignore` (+`run/`), new `docs/operations.md` (shared human/agent documentation), `AGENTS.md` (light
  feature pointers + stale port correction)
- **No product code changes**: reuses existing `/ops/readiness` endpoint; no API, schema, or transport changes
- **Local dev**: stale `/tmp/pgcqrs-*` files no longer produced; old files remain until manually removed
