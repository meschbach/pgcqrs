# Spec Delta

## MODIFIED Requirements

### Requirement: Readiness gating before transports
In both `local` and `docked` modes the script SHALL poll `GET /ops/readiness` on the mode's operational (ops) endpoint for up to 15 seconds before running any transport suite, treating only an HTTP 200 response as ready (a successful connection alone SHALL NOT satisfy the gate), replacing fixed startup sleeps. The ops endpoint SHALL be resolved independently of the client HTTP endpoint, from the ops listener address. In `local` mode the poll loop SHALL additionally check the owned service process each iteration and treat its death before a successful probe as immediate startup failure, so a stale instance answering 200 on the same port can never gate the run.

#### Scenario: Service becomes ready promptly
- **WHEN** `/ops/readiness` returns HTTP 200 on the ops endpoint within the deadline
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

#### Scenario: Readiness probe targets the ops endpoint, not the client endpoint
- **WHEN** the ops listener address differs from the client HTTP address
- **THEN** the readiness gate polls the ops endpoint and succeeds even though `/ops/readiness` is not served on the client endpoint

### Requirement: Operations documentation
A `docs/operations.md` SHALL document: the `/ops/liveness` and `/ops/readiness` endpoints, the dedicated ops listener they are served on (including its configuration keys and environment variables), and their use by the Helm chart probes; the diagnostics directory contract and cleanup guidance; the CI artifact behavior; the readiness gating used by the integration script; and the known gap that gRPC exposes no standard health service.

#### Scenario: Operator can diagnose a stuck service from the doc alone
- **WHEN** an operator reads `docs/operations.md` after an integration run failure
- **THEN** the doc tells them where diagnostics live, what each file contains, how readiness gating works, which health endpoints exist across HTTP and gRPC, and which listener and address serve the ops endpoints
