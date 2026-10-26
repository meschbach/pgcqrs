# Spec Delta

## Purpose

Exposes operational health endpoints (liveness, readiness) on a listener dedicated to operational traffic, so the operational surface can be reached and secured independently of the client-facing HTTP and gRPC listeners.

## ADDED Requirements

### Requirement: Operational endpoints served on a dedicated listener

The system SHALL serve `/ops/liveness` and `/ops/readiness` on a dedicated operational listener that is separate from the client-facing HTTP and gRPC listeners.

#### Scenario: Ops endpoints reachable on the ops listener
- **WHEN** the service is running and a client requests `GET /ops/liveness` on the configured ops address
- **THEN** the system returns HTTP 200 with the liveness payload

#### Scenario: Ops endpoints absent from the client listener
- **WHEN** a client requests `GET /ops/liveness` or `GET /ops/readiness` on the client-facing HTTP address
- **THEN** the system does not serve those routes (HTTP 404)

### Requirement: Ops listener address is configurable

The system SHALL accept the ops listener bind address from configuration and from an environment variable, applying a default when neither is present.

#### Scenario: Ops address from configuration
- **WHEN** the ops listener address is set in service configuration
- **THEN** the system binds the ops listener to that address

#### Scenario: Ops address default
- **WHEN** no ops listener address is configured
- **THEN** the system binds the ops listener to the documented default address

### Requirement: Ops listener is enabled by default and disableable

The system SHALL start the ops listener by default and SHALL accept an explicit enablement flag (configuration and environment variable) to disable it. When disabled, the listener is not started and probes against the ops port fail.

#### Scenario: Default configuration starts the ops listener
- **WHEN** no ops listener enablement flag is configured
- **THEN** the system starts the ops listener on the configured address

#### Scenario: Ops listener disabled via configuration
- **WHEN** the ops listener is explicitly disabled via configuration (`enabled: false`) or environment variable (`PGCQRS_OPS_LISTENER_ENABLED=false`)
- **THEN** the system does not start the ops listener; probes against the ops port fail

### Requirement: Ops listener root serves an HTML index

The ops listener SHALL serve a static HTML page at its root (`/`) listing the operational endpoints with short descriptions, so operators can discover the available endpoints.

#### Scenario: Ops root returns HTML index
- **WHEN** a client requests `GET /` on the ops listener
- **THEN** the system returns an HTML page listing `/ops/liveness` and `/ops/readiness` with descriptions

### Requirement: Ops listener participates in service lifecycle

The ops listener SHALL start and stop with the service, so its availability tracks the service process rather than running independently.

#### Scenario: Ops listener follows service shutdown
- **WHEN** the service is shut down
- **THEN** the ops listener stops listening as part of that shutdown

#### Scenario: Ops listener bind failure is a startup failure
- **WHEN** the ops listener cannot bind its configured address (for example, the port is occupied)
- **THEN** the service fails to start and reports the bind error

### Requirement: Ops surface is network-private by default

The system SHALL default the ops listener bind to loopback (non-public), and SHALL NOT expose it on the public/client network unless the operator deliberately overrides the bind address. The loopback default is the primary security control for the ops surface.

#### Scenario: Ops listener binds to loopback by default
- **WHEN** no ops listener address is configured
- **THEN** the system binds the ops listener to `127.0.0.1:9002` (loopback), so it is not reachable from the public/client network

#### Scenario: Operator overrides to non-loopback bind
- **WHEN** the ops listener address is explicitly set to a non-loopback value (e.g., `0.0.0.0:9002` for Kubernetes pod-internal probes)
- **THEN** the system binds the ops listener to that address; the operator is responsible for network isolation via the orchestration platform

### Requirement: Bundled ops health-check CLI

The service binary SHALL provide a `health-check ops` subcommand that probes the ops readiness endpoint, deriving its target from the ops listener address (default `127.0.0.1:9002`, overridable via `PGCQRS_OPS_LISTENER_ADDRESS`), and SHALL exit 0 only when the probe returns HTTP 200.

#### Scenario: Ready ops listener yields success exit
- **WHEN** the ops listener returns HTTP 200 for `GET /ops/readiness` and `health-check ops` runs
- **THEN** the command exits 0

#### Scenario: Unreachable or unready ops listener yields failure exit
- **WHEN** the ops listener is unreachable or does not return HTTP 200 and `health-check ops` runs
- **THEN** the command exits non-zero
