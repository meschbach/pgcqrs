## ADDED Requirements

### Requirement: Diagnostics directory per integration run
The integration test script (`integration-tests.sh`) SHALL write all diagnostics for a run into `run/test-diagnostics/<run-id>/` under the repository root (resolved from the script location, independent of the caller's working directory), where `<run-id>` is a UTC timestamp plus mode plus process ID (e.g., `20260823-141505-local-4711`) so consecutive invocations cannot collide even within the same second. Each transport suite that executes SHALL produce its output file (`test-memory.out`, `test-http.out`, `test-grpc.out`); a run executing all three suites produces all three. Every run SHALL contain `summary.txt`. Local runs SHALL additionally stream service stderr — and only stderr; stdout is intentionally uncaptured — live into `server.log` and, on failure, produce `goroutines.txt` per the dump policy below. Docked runs produce no server-owned files; on failure they produce `docker-service.log` per the docked diagnostics requirement below. The base path SHALL be overridable via `PGCQRS_TEST_DIAG_DIR`.

#### Scenario: Passing local run produces complete diagnostics
- **WHEN** the script runs in `local` mode and all transports pass
- **THEN** a new `run/test-diagnostics/<run-id>/` exists containing `server.log`, the three `test-*.out` files, and `summary.txt` recording mode, timestamp, git SHA, Go version, per-phase durations, and verdict

#### Scenario: Consecutive runs do not clobber each other
- **WHEN** the script runs twice without cleaning
- **THEN** each run writes to its own distinct `<run-id>` directory

#### Scenario: Hard-killed run still leaves evidence
- **WHEN** a run is terminated externally mid-execution (e.g., CI cancellation)
- **THEN** `server.log` contains service output streamed up to termination because it is written live, not buffered until exit

### Requirement: Console output is phases, verdicts, and pointers
The script console output SHALL consist of phase banners with per-phase result and duration (e.g., `[memory] PASS 1s`) plus a closing line naming the diagnostics directory. It SHALL NOT trace commands (`set -x`) or print goroutine dumps. On a phase failure it SHALL print at most the last 40 lines of that phase's test output followed by the full file path.

#### Scenario: All transports pass quietly
- **WHEN** memory, HTTP, and gRPC transports pass in `local` mode
- **THEN** the console shows one status line per transport with duration and a final line naming the diagnostics directory, with no stack traces or command tracing

#### Scenario: Transport failure shows tail and pointer
- **WHEN** the gRPC transport tests fail
- **THEN** the console prints `[grpc] FAIL <duration>`, the last 40 lines of `test-grpc.out`, the path to the full file, and exits non-zero after writing `goroutines.txt`

### Requirement: Readiness gating before transports
In both `local` and `docked` modes the script SHALL poll `GET /ops/readiness` on the mode's HTTP endpoint for up to 15 seconds before running any transport suite, treating only an HTTP 200 response as ready (a successful connection alone SHALL NOT satisfy the gate), replacing fixed startup sleeps. In `local` mode the poll loop SHALL additionally check the owned service process each iteration and treat its death before a successful probe as immediate startup failure, so a stale instance answering 200 on the same port can never gate the run.

#### Scenario: Service becomes ready promptly
- **WHEN** `/ops/readiness` returns HTTP 200 within the deadline
- **THEN** the script proceeds immediately to run the transport suites

#### Scenario: Local service never becomes ready
- **WHEN** `/ops/readiness` has not returned HTTP 200 within 15 seconds in `local` mode
- **THEN** the script delivers a SIGQUIT dump signal to the stuck-mid-startup service during teardown and produces `goroutines.txt` per the dump policy below (a stuck-mid-startup process is prime dump material), records the startup failure in `summary.txt`, points at `server.log` on the console, and exits non-zero without running any transport suite

#### Scenario: Docked service never becomes ready
- **WHEN** `/ops/readiness` has not returned HTTP 200 within 15 seconds in `docked` mode
- **THEN** the script records the startup failure in `summary.txt`, exits non-zero without running any transport suite, and requires no privileged access to the containers

#### Scenario: Local server process dies during polling
- **WHEN** the owned service process exits while `/ops/readiness` has not yet returned HTTP 200 (for example, a bind failure caused by an occupied port)
- **THEN** the script aborts polling immediately, records the startup failure in `summary.txt`, points at `server.log` on the console, and exits non-zero without running any transport suite

### Requirement: Self-bounded total runtime
The script SHALL terminate on its own via internal deadlines (startup-readiness deadline and per-suite go test timeouts) without relying on external timeouts, so that external job timeouts act only as last-resort safety nets and CI failure-path artifact steps reliably execute.

#### Scenario: Hung transport cannot hang the script
- **WHEN** a go test invocation exceeds its `--timeout 5s`
- **THEN** go test self-terminates with failure, the script captures the output, dumps goroutines to file, and exits non-zero on its own

### Requirement: Goroutine dump policy
In `local` mode (where the script owns the service process), the script SHALL send SIGQUIT to capture a goroutine dump into `goroutines.txt` only when the run failed (transport failure, readiness timeout) or when the service is being torn down abnormally; it SHALL use a plain kill when all transports passed. The dump SHALL never be printed to the console. After signaling, the script SHALL wait for process death by polling up to 2 seconds rather than a fixed sleep; if the process survives the wait, it SHALL escalate to SIGKILL so no orphan survives any path. Because the dump arrives on the service's stderr (streamed into `server.log`), `goroutines.txt` SHALL be derived after process death by extracting `server.log` from the runtime's `SIGQUIT: quit` marker onward; when no marker is present, the script SHALL fall back to writing the tail of `server.log` prefixed with a comment identifying it as a fallback tail rather than a runtime dump, so the file is never silently empty and always self-describing. In `docked` mode no process dump is produced; docked failure diagnostics are covered by their own requirement below.

#### Scenario: Success teardown is quiet
- **WHEN** all transports passed and cleanup runs in `local` mode
- **THEN** the server is killed plainly, no `goroutines.txt` is produced, and no stack traces appear anywhere on the console

#### Scenario: Failure teardown captures dump to file
- **WHEN** any phase failed and cleanup runs in `local` mode
- **THEN** `goroutines.txt` exists in the diagnostics directory containing the runtime stack dump and the console is free of stack traces

#### Scenario: Missing dump marker still yields evidence
- **WHEN** the script delivers SIGQUIT but `server.log` contains no `SIGQUIT: quit` marker afterward
- **THEN** `goroutines.txt` still exists, containing the annotated tail of `server.log`, and is never silently empty or absent

### Requirement: Docked mode service diagnostics
In `docked` mode, when the run fails (transport failure or readiness timeout) the script SHALL copy the service container's log output into `docker-service.log` in the diagnostics directory and then restore the container to running state. Container resolution SHALL NOT depend on the Compose project name or CLI variant: the script SHALL locate the service container via a Docker label filter (`com.docker.compose.service=pgcqrs`) across running and stopped containers. When the filter matches several containers, the script SHALL prefer running containers with a newest-created tie-break, signal and capture only the chosen container, warn on the console, and list all matched IDs in `summary.txt`. When it matches no container, the script SHALL record the absence in `summary.txt`, produce no `docker-service.log`, and still exit non-zero from the original failure. Restoring the container SHALL be best-effort: a restore failure SHALL be surfaced on the console and noted in `summary.txt` without altering the run's exit code. When the container is running, the script SHALL first deliver SIGQUIT through the Docker daemon (`docker kill --signal=QUIT`) as a best effort to append a goroutine dump to the container logs. When the container has exited (crash, OOM, restart give-up) no goroutine dump is obtainable; the captured logs SHALL still be recorded, preserving whatever crash output exists. The script SHALL NOT print the captured output to the console, SHALL NOT depend on any tooling inside the container, and SHALL NOT treat signal-delivery failure as an error.

#### Scenario: Failed docked run captures container stacks
- **WHEN** the transport tests fail in `docked` mode and the service container is running
- **THEN** `docker-service.log` exists containing the container's output including a fresh goroutine dump and the container is running again afterward

#### Scenario: Crashed container still yields evidence
- **WHEN** the run fails while the service container is not running
- **THEN** `docker-service.log` exists containing the container's prior output (e.g., panic or OOM messages), no goroutine dump is expected, and the container is restored to running state afterward

#### Scenario: Ambiguous container match still produces diagnostics
- **WHEN** the label filter matches more than one service container when a `docked` run fails
- **THEN** the script selects the newest running container, signals and captures only that one, restores it, warns on the console, and lists all matched IDs in `summary.txt`

#### Scenario: Missing container noted without altering the verdict
- **WHEN** the label filter matches no service container when a `docked` run fails
- **THEN** `summary.txt` records the absence, no `docker-service.log` is produced, and the run still exits non-zero from the original failure

#### Scenario: Console parity across modes
- **WHEN** a docked run fails
- **THEN** the console shows the same phase status lines, failure tail, and diagnostics-directory pointer as a failing local run

#### Scenario: Restore failure preserves the original verdict
- **WHEN** diagnostics capture succeeds but restoring the container afterwards fails (for example, its database dependency is down)
- **THEN** the script warns on the console, records the failed restore in `summary.txt`, and still exits with the original non-zero code

### Requirement: CI archives diagnostics as artifacts
Both GitHub workflows that invoke `integration-tests.sh` SHALL upload `run/test-diagnostics/` as a job artifact on failure or job cancellation, named with the Go matrix version suffix. PR workflow uploads SHALL exist on parity with main workflow uploads. The prior glob-based upload of `/tmp/pgcqrs-goroutines-*.txt` SHALL be removed.

#### Scenario: Failed PR build yields downloadable diagnostics
- **WHEN** the integration test step fails in the PR workflow
- **THEN** an artifact containing `run/test-diagnostics/` is attached to the run

#### Scenario: Failed main build keeps deterministic artifact path
- **WHEN** the integration test step fails in the main workflow
- **THEN** the artifact is uploaded from `run/test-diagnostics/` directly, with no filesystem globbing or PID-dependent paths

#### Scenario: Cancelled run archives partial evidence
- **WHEN** the job is cancelled (user action or external timeout) after the integration script has begun writing diagnostics
- **THEN** an artifact containing whatever diagnostics were written — including the live-streamed `server.log` — is attached to the run

### Requirement: Operations documentation
A `docs/operations.md` SHALL document: the `/ops/liveness` and `/ops/readiness` endpoints and their use by the Helm chart probes, the diagnostics directory contract and cleanup guidance, the CI artifact behavior, the readiness gating used by the integration script, and the known gap that gRPC exposes no standard health service.

#### Scenario: Operator can diagnose a stuck service from the doc alone
- **WHEN** an operator reads `docs/operations.md` after an integration run failure
- **THEN** the doc tells them where diagnostics live, what each file contains, how readiness gating works, and which health endpoints exist across HTTP and gRPC
