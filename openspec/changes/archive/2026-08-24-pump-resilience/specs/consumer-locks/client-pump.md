# Client Pump Lifecycle

## Purpose

The client pump drives the event processing loop for indexers (view projections, embedding indexers, etc.). It acquires consumer locks, resumes from stored positions, watches events, dispatches to handlers, and heartbeats after each event. The pump lifecycle makes the pump's lifecycle explicit and observable, enabling proper error recovery, warm standby failover, and deterministic test waiting.

## Requirements

### Requirement: Pump has explicit observable states
The Pump SHALL maintain an explicit state machine with transitions that are observable via an event emitter. Six states are defined: WaitingForLock, Acquiring, Watching, LockLost, Failed, Closed.

#### Scenario: Initial state is WaitingForLock
- **WHEN** a new Pump is created and RunWithDomainStream is called
- **THEN** the Pump state SHALL be set to WaitingForLock before any lock acquisition attempt

#### Scenario: Transition to Acquiring on lock acquisition
- **GIVEN** Pump state is WaitingForLock
- **WHEN** WaitForLock returns successfully
- **THEN** Pump state SHALL transition to Acquiring

#### Scenario: Transition to Watching on watch setup
- **GIVEN** Pump state is Acquiring
- **WHEN** query.Watch returns successfully
- **THEN** Pump state SHALL transition to Watching

#### Scenario: Transition to LockLost on heartbeat failure
- **GIVEN** Pump state is Watching
- **WHEN** Heartbeat returns any error except HeartbeatConflictError
- **THEN** Pump state SHALL transition to LockLost

#### Scenario: Transition to LockLost on watch failure
- **GIVEN** Pump state is Watching
- **WHEN** TickWithID returns an error (watch stream failure)
- **THEN** Pump state SHALL transition to LockLost
- **AND** the pump SHALL release the lock and attempt re-acquisition + re-watch

#### Scenario: Transition to Failed on unrecoverable error
- **GIVEN** Pump is in any state
- **WHEN** an unrecoverable error occurs (e.g., Watch setup failure after retries)
- **THEN** Pump state SHALL transition to Failed

#### Scenario: Transition to Closed on context cancellation
- **GIVEN** Pump is in any state
- **WHEN** the pump context is canceled
- **THEN** Pump state SHALL transition to Closed

#### Scenario: LockLost transitions to WaitingForLock
- **GIVEN** Pump state is LockLost
- **WHEN** backoff completes and re-acquisition begins
- **THEN** Pump state SHALL transition to WaitingForLock

### Requirement: State changes are observable
The Client SHALL expose an event emitter for state changes, a synchronous state query, and a blocking wait-for-state method.

#### Scenario: OnStateChange receives state transitions
- **WHEN** a callback is registered via OnStateChange
- **THEN** the callback SHALL be invoked on every state transition
- **AND** the callback SHALL receive the new state, previous state, and any associated error

#### Scenario: State returns current state
- **WHEN** State() is called
- **THEN** it SHALL return the current PumpState value

#### Scenario: WaitForState blocks until target state
- **GIVEN** pump is in WaitingForLock state
- **WHEN** WaitForState(ctx, Watching) is called
- **THEN** it SHALL block until the pump transitions to Watching
- **OR** return ctx.Err() if the context is canceled

### Requirement: Pump acquires lock via notification
The Pump SHALL acquire consumer locks using a notification mechanism where available (memory transport uses emitter, gRPC transport uses server-side blocking stream). Polling is NOT used for lock acquisition.

#### Scenario: Memory transport uses emitter for lock notification
- **GIVEN** memory transport
- **WHEN** lock is released by the current holder
- **THEN** the waiting Pump SHALL be notified and attempt re-acquisition immediately

#### Scenario: gRPC transport uses separate WaitForLock RPC
- **GIVEN** gRPC transport
- **WHEN** WaitForLock is called and lock is held
- **THEN** the client SHALL call the WaitForLock RPC (server-streaming)
- **AND** the server SHALL block until the lock is available
- **AND** the server SHALL respond with LockGranted when the lock is acquired
- **AND** LockGranted SHALL include heartbeat_interval_ms (milliseconds until next heartbeat expected)
- **AND** LockGranted SHALL include deadline_ms (milliseconds until lock expires)
- **AND** the client SHALL then open a separate KeepAlive stream for heartbeat/release
- **AND** gRPC keepalive SHALL be configured at the connection level to prevent TCP timeouts

#### Scenario: Server pushes LockLost on KeepAlive stream
- **GIVEN** pump is in Watching state with active KeepAlive stream
- **WHEN** the lock is released by another process or expires via TTL
- **THEN** the server SHALL send a LockLost message on the KeepAlive stream
- **AND** the pump SHALL transition to LockLost state
- **AND** the pump SHALL attempt re-acquisition after backoff

### Requirement: Pump maintains local position for dedup
The Pump SHALL maintain an explicit position field tracking the last successfully processed event ID. Position is used for watch setup and heartbeat. The pump SHALL deduplicate events by skipping any event with ID <= position.

#### Scenario: Position returned by WaitForLock
- **WHEN** a Pump starts
- **THEN** its position SHALL be the value returned by WaitForLock (server's last heartbeated position)

#### Scenario: Position updated after successful Tick
- **GIVEN** pump is in Watching state
- **WHEN** TickWithID returns eventID successfully
- **THEN** the pump SHALL update its position to eventID

#### Scenario: Duplicate event is skipped
- **GIVEN** pump is in Watching state with position = 42
- **WHEN** TickWithID returns eventID = 42 (duplicate)
- **THEN** the pump SHALL skip processing and continue
- **AND** the pump SHALL NOT call the handler

#### Scenario: Position used for watch setup
- **WHEN** a watch is created after lock acquisition
- **THEN** query.After(position) SHALL be set if position > 0

#### Scenario: Position used for heartbeat
- **WHEN** Heartbeat is called
- **THEN** the pump's current position SHALL be sent

### Requirement: Pump releases lock on heartbeat failure
The Pump SHALL release the lock immediately on any heartbeat failure except HeartbeatConflictError. No retry is attempted. The pump transitions to LockLost and attempts re-acquisition.

#### Scenario: Network error during heartbeat triggers LockLost
- **GIVEN** pump is in Watching state
- **WHEN** Heartbeat returns a transient network error
- **THEN** the pump SHALL transition to LockLost
- **AND** the pump SHALL NOT retry the heartbeat

#### Scenario: LockExpiredError triggers LockLost
- **GIVEN** pump is in Watching state
- **WHEN** Heartbeat returns LockExpiredError
- **THEN** the pump SHALL transition to LockLost

#### Scenario: HeartbeatConflictError corrects position
- **GIVEN** pump is in Watching state
- **WHEN** Heartbeat returns HeartbeatConflictError with CurrentVersion
- **THEN** the pump SHALL update its position to CurrentVersion
- **AND** the pump SHALL continue processing (not LockLost)

### Requirement: Handler errors indicate recoverability
Handlers registered with the pump MAY return a `HandlerError` interface that indicates whether the error is recoverable. By default, plain errors are treated as non-recoverable.

#### Scenario: Handler returns recoverable error
- **GIVEN** pump is in Watching state
- **WHEN** a handler returns an error implementing `HandlerError` with `Recoverable() == true`
- **THEN** the pump SHALL transition to LockLost
- **AND** the pump SHALL attempt re-acquisition + re-watch after backoff

#### Scenario: Handler returns non-recoverable error
- **GIVEN** pump is in Watching state
- **WHEN** a handler returns a plain error or `HandlerError` with `Recoverable() == false`
- **THEN** the pump SHALL transition to Failed
- **AND** the pump SHALL exit

#### Scenario: Handler returns nil
- **GIVEN** pump is in Watching state
- **WHEN** a handler returns nil
- **THEN** the pump SHALL continue processing the next event

### Requirement: Pump recovers from watch stream failure
The Pump SHALL recover from watch stream failures by releasing the lock, transitioning to LockLost, and re-acquiring + re-watching. This is consistent with heartbeat failure recovery.

#### Scenario: Watch stream error triggers recovery
- **GIVEN** pump is in Watching state
- **WHEN** TickWithID returns an error (network error, server disconnect)
- **THEN** the pump SHALL release the lock
- **AND** the pump SHALL transition to LockLost
- **AND** the pump SHALL attempt re-acquisition + re-watch after backoff

### Requirement: WaitForState enables deterministic test waiting
Tests SHALL use WaitForState instead of timeouts to synchronize with pump lifecycle.

#### Scenario: Test waits for pump readiness
- **GIVEN** two projections connected via memory transport
- **WHEN** the test calls WaitForState(ctx, Watching) on both clients
- **THEN** both pumps SHALL be in Watching state before events are submitted
- **AND** assertions SHALL pass without timeout
