# Consumer Locks

## Purpose

Consumer locks provide durable distributed locks for consumers, enabling exclusive access to a (domain, stream) partition. This prevents duplicate processing across instances (e.g., multiple embedding indexers running simultaneously) and provides inspectability into who is processing what. Locks are renewed via heartbeat streams that atomically carry the consumer's position, ensuring both lock validity and progress tracking in a single transaction.

## Requirements

### Requirement: Consumer can acquire an exclusive lock
The system SHALL allow a consumer to acquire an exclusive lock for a given (domain, stream, consumer) tuple. The lock has a configurable TTL and must be renewed via heartbeat to remain held.

#### Scenario: Successful lock acquisition
- **WHEN** consumer calls TryAcquire with domain "inventory-A", stream "events", consumer "search-index", holder "instance-1", ttl "30s"
- **THEN** the system creates a lock row and returns acquired=true with guarantee_until and held_until timestamps

#### Scenario: Lock already held by another instance
- **GIVEN** consumer "search-index" is locked by holder "instance-1" in domain "inventory-A", stream "events"
- **WHEN** consumer calls TryAcquire with the same domain/stream/consumer but holder "instance-2"
- **THEN** the system returns acquired=false with held_by="instance-1" and the lock's held_until time

### Requirement: Consumer can maintain lock via heartbeat stream
The system SHALL support a bidirectional gRPC stream where the consumer sends periodic heartbeats and the server responds with lock status. Heartbeats carry the consumer's current processed position; the server atomically updates both the lock heartbeat time and the consumer position in a single transaction.

#### Scenario: Heartbeat renews lock
- **GIVEN** consumer "search-index" holds the lock for domain "inventory-A", stream "events" since 2 seconds ago, TTL "30s"
- **WHEN** consumer sends Heartbeat with consumer="search-index", holder="instance-1", domain="inventory-A", stream="events", position=150
- **THEN** the system responds with locked=true, reason=RENEWED
- **AND** the consumer position is updated to 150
- **AND** the lock heartbeat_at, guarantee_until, and held_until are updated

#### Scenario: Heartbeat on expired lock
- **GIVEN** consumer "search-index" held the lock but held_until is past current time
- **WHEN** consumer sends Heartbeat
- **THEN** the system responds with locked=false, reason=EXPIRED

#### Scenario: Heartbeat on stolen lock
- **GIVEN** consumer "search-index" lock was acquired by a different holder
- **WHEN** the original holder sends Heartbeat with its holder identity
- **THEN** the system responds with locked=false, reason=STOLEN

#### Scenario: Heartbeat with stale position is rejected
- **GIVEN** consumer "search-index" holds the lock for domain "inventory-A", stream "events" with position=200
- **WHEN** consumer sends Heartbeat with position=150 (behind current position)
- **THEN** the system responds with locked=false, reason=CONFLICT
- **AND** the response includes target_version=150 and current_version=200
- **AND** the lock is NOT renewed (transaction rolled back)
- **AND** the consumer position is NOT updated

#### Scenario: First Heartbeat validates holder identity
- **GIVEN** consumer "search-index" is locked by holder "instance-1"
- **WHEN** a client opens a KeepAlive stream and sends the first Heartbeat with holder="instance-1"
- **THEN** the server validates the holder matches the lock row and associates the stream with that holder for the session
- **AND** subsequent messages are bound to that holder without re-validation

#### Scenario: First Heartbeat with mismatched holder returns STOLEN
- **GIVEN** consumer "search-index" is locked by holder "instance-1"
- **WHEN** a client opens a KeepAlive stream and sends the first Heartbeat with holder="instance-2"
- **THEN** the system responds with locked=false, reason=STOLEN
- **AND** the stream should be closed by the client

### Requirement: Lock expires after TTL without heartbeats
The system SHALL treat locks whose `held_until` is older than the current server time as expired. Expired locks are treated as non-existent on the next `TryAcquire` or `GetLock` access. `GetLock` ignores expired rows. `TryAcquire` overwrites the expired row and also cleans up to 128 other expired rows in the same (domain, stream) partition. No background reaper is used.

#### Scenario: Lock expires naturally
- **GIVEN** consumer "search-index" holds the lock with TTL "30s"
- **WHEN** 31 seconds pass without a heartbeat
- **THEN** TryAcquire from another holder succeeds (expired row is ignored, new lock is created)

#### Scenario: Expired lock does not interfere with new acquisition
- **GIVEN** a lock row exists for consumer "search-index" that expired 5 seconds ago
- **WHEN** a new instance calls TryAcquire for the same consumer/domain/stream
- **THEN** the system successfully acquires the lock (expired row is overwritten by new lock via INSERT ON CONFLICT)

#### Scenario: TryAcquire cleans up other expired rows
- **GIVEN** 5 expired lock rows exist in domain "inventory-A", stream "events" for various consumers
- **WHEN** a new instance calls TryAcquire for consumer "search-index" in the same domain/stream
- **THEN** the system acquires the lock and deletes up to 128 of the other expired rows in the same partition

#### Scenario: Expired lock is invisible to GetLock
- **GIVEN** a lock row exists for consumer "search-index" that expired 5 seconds ago
- **WHEN** a caller calls GetLock for the same consumer/domain/stream
- **THEN** the system returns not-found (expired row is ignored)

#### Scenario: TryAcquire rejects TTL below minimum
- **WHEN** consumer calls TryAcquire with ttl "3s" (below the 6s minimum)
- **THEN** the system returns an error indicating TTL is below the minimum

### Requirement: Consumer can release lock explicitly
The system SHALL allow a consumer to explicitly release a lock before TTL expiry, either via a unary Release RPC or via a Release message in the KeepAlive stream.

#### Scenario: Explicit release via unary RPC
- **GIVEN** consumer "search-index" holds the lock for domain "inventory-A", stream "events" with holder "instance-1"
- **WHEN** consumer calls Release with domain "inventory-A", stream "events", consumer "search-index", holder "instance-1"
- **THEN** the lock is released and other instances can acquire it immediately

#### Scenario: Release via in-stream message
- **GIVEN** consumer "search-index" holds the lock for domain "inventory-A", stream "events" with holder "instance-1"
- **WHEN** consumer sends Release message in the KeepAlive stream
- **THEN** the lock is released, the server responds with ReleaseAck, and the stream can be closed

#### Scenario: Release by non-holder is rejected
- **GIVEN** consumer "search-index" is locked by holder "instance-1"
- **WHEN** holder "instance-2" calls Release for the same consumer/domain/stream
- **THEN** the system returns an error indicating the caller is not the lock holder

#### Scenario: Release is idempotent
- **GIVEN** consumer "search-index" lock has already expired via TTL
- **WHEN** the original holder calls Release
- **THEN** the system returns success (no-op)

### Requirement: Core APIs can optionally reject requests without valid lock
The system SHALL allow consumers to opt into a lock check on Submit requests by passing a `Lock` option. The `Lock` type implements the `Option` interface and can be passed directly to the `Submit` method. The lock is verified on **every** call — not just at creation. If the lock is not held or has expired (`held_until < NOW()`), the request is rejected. When a `Lock` is provided, the server wraps the operation in a transaction to ensure atomicity. `Watch` and `StreamBatch` are lock-unaware — the client manages lock lifecycle via the heartbeat loop.

#### Scenario: Submit with valid lock succeeds
- **GIVEN** consumer "search-index" holds the lock for domain "inventory-A", stream "events" with holder "instance-1"
- **WHEN** client passes `&Lock{Consumer: "search-index", Holder: "instance-1"}` option to Submit
- **THEN** the submit proceeds atomically (lock verified + event written in single transaction) and events are written

#### Scenario: Submit with lock held by different holder is rejected
- **GIVEN** consumer "search-index" is locked by holder "instance-1"
- **WHEN** client passes `&Lock{Consumer: "search-index", Holder: "instance-2"}` option to Submit
- **THEN** the system returns an error indicating the lock is held by another holder

#### Scenario: Submit without lock option succeeds (backward compatible)
- **GIVEN** no lock is held for consumer "search-index"
- **WHEN** client calls Submit without passing a Lock option
- **THEN** the submit proceeds normally (existing behaviour preserved)

### Requirement: Operator can inspect active locks
The system SHALL support listing all active locks for a given domain/stream pair via the ListLocks RPC on gRPC and in-memory transport. HTTP transport does not support lock listing.

#### Scenario: List locks for domain/stream with active locks
- **GIVEN** consumer "search-index" holds a lock for domain "inventory-A", stream "events" with holder "instance-1"
- **AND** consumer "embedding-indexer" holds a lock for domain "inventory-A", stream "events" with holder "instance-2"
- **WHEN** operator calls ListLocks with domain "inventory-A", stream "events"
- **THEN** the system returns both locks with their holder, acquired_at, heartbeat_at, and held_until information

#### Scenario: List locks for domain/stream with no locks
- **WHEN** operator calls ListLocks with domain "inventory-A", stream "events"
- **THEN** the system returns an empty list

## Client Pump Lifecycle

### Purpose

The client pump drives the event processing loop for indexers (view projections, embedding indexers, etc.). It acquires consumer locks, resumes from stored positions, watches events, dispatches to handlers, and heartbeats after each event. The pump lifecycle makes the pump's lifecycle explicit and observable, enabling proper error recovery, warm standby failover, and deterministic test waiting.

### Requirements

#### Requirement: Pump has explicit observable states
The Pump SHALL maintain an explicit state machine with transitions that are observable via an event emitter. Six states are defined: WaitingForLock, Acquiring, Watching, LockLost, Failed, Closed.

##### Scenario: Initial state is WaitingForLock
- **WHEN** a new Pump is created and RunWithDomainStream is called
- **THEN** the Pump state SHALL be set to WaitingForLock before any lock acquisition attempt

##### Scenario: Transition to Acquiring on lock acquisition
- **GIVEN** Pump state is WaitingForLock
- **WHEN** WaitForLock returns successfully
- **THEN** Pump state SHALL transition to Acquiring

##### Scenario: Transition to Watching on watch setup
- **GIVEN** Pump state is Acquiring
- **WHEN** query.Watch returns successfully
- **THEN** Pump state SHALL transition to Watching

##### Scenario: Transition to LockLost on heartbeat failure
- **GIVEN** Pump state is Watching
- **WHEN** Heartbeat returns any error except HeartbeatConflictError
- **THEN** Pump state SHALL transition to LockLost

##### Scenario: Transition to LockLost on watch failure
- **GIVEN** Pump state is Watching
- **WHEN** TickWithID returns an error (watch stream failure)
- **THEN** Pump state SHALL transition to LockLost
- **AND** the pump SHALL release the lock and attempt re-acquisition + re-watch

##### Scenario: Transition to Failed on unrecoverable error
- **GIVEN** Pump is in any state
- **WHEN** an unrecoverable error occurs (e.g., Watch setup failure after retries)
- **THEN** Pump state SHALL transition to Failed

##### Scenario: Transition to Closed on context cancellation
- **GIVEN** Pump is in any state
- **WHEN** the pump context is canceled
- **THEN** Pump state SHALL transition to Closed

##### Scenario: LockLost transitions to WaitingForLock
- **GIVEN** Pump state is LockLost
- **WHEN** backoff completes and re-acquisition begins
- **THEN** Pump state SHALL transition to WaitingForLock

#### Requirement: State changes are observable
The Client SHALL expose an event emitter for state changes, a synchronous state query, and a blocking wait-for-state method.

##### Scenario: OnStateChange receives state transitions
- **WHEN** a callback is registered via OnStateChange
- **THEN** the callback SHALL be invoked on every state transition
- **AND** the callback SHALL receive the new state, previous state, and any associated error

##### Scenario: State returns current state
- **WHEN** State() is called
- **THEN** it SHALL return the current PumpState value

##### Scenario: WaitForState blocks until target state
- **GIVEN** pump is in WaitingForLock state
- **WHEN** WaitForState(ctx, Watching) is called
- **THEN** it SHALL block until the pump transitions to Watching
- **OR** return ctx.Err() if the context is canceled

#### Requirement: Pump acquires lock via notification
The Pump SHALL acquire consumer locks using a notification mechanism where available (memory transport uses emitter, gRPC transport uses server-side blocking stream). Polling is NOT used for lock acquisition.

##### Scenario: Memory transport uses emitter for lock notification
- **GIVEN** memory transport
- **WHEN** lock is released by the current holder
- **THEN** the waiting Pump SHALL be notified and attempt re-acquisition immediately

##### Scenario: gRPC transport uses separate WaitForLock RPC
- **GIVEN** gRPC transport
- **WHEN** WaitForLock is called and lock is held
- **THEN** the client SHALL call the WaitForLock RPC (server-streaming)
- **AND** the server SHALL block until the lock is available
- **AND** the server SHALL respond with LockGranted when the lock is acquired
- **AND** LockGranted SHALL include heartbeat_interval_ms (milliseconds until next heartbeat expected)
- **AND** LockGranted SHALL include deadline_ms (milliseconds until lock expires)
- **AND** the client SHALL then open a separate KeepAlive stream for heartbeat/release
- **AND** gRPC keepalive SHALL be configured at the connection level to prevent TCP timeouts

##### Scenario: Server pushes LockLost on KeepAlive stream
- **GIVEN** pump is in Watching state with active KeepAlive stream
- **WHEN** the lock is released by another process or expires via TTL
- **THEN** the server SHALL send a LockLost message on the KeepAlive stream
- **AND** the pump SHALL transition to LockLost state
- **AND** the pump SHALL attempt re-acquisition after backoff

#### Requirement: Pump maintains local position for dedup
The Pump SHALL maintain an explicit position field tracking the last successfully processed event ID. Position is used for watch setup and heartbeat. The pump SHALL deduplicate events by skipping any event with ID <= position.

##### Scenario: Position returned by WaitForLock
- **WHEN** a Pump starts
- **THEN** its position SHALL be the value returned by WaitForLock (server's last heartbeated position)

##### Scenario: Position updated after successful Tick
- **GIVEN** pump is in Watching state
- **WHEN** TickWithID returns eventID successfully
- **THEN** the pump SHALL update its position to eventID

##### Scenario: Duplicate event is skipped
- **GIVEN** pump is in Watching state with position = 42
- **WHEN** TickWithID returns eventID = 42 (duplicate)
- **THEN** the pump SHALL skip processing and continue
- **AND** the pump SHALL NOT call the handler

##### Scenario: Position used for watch setup
- **WHEN** a watch is created after lock acquisition
- **THEN** query.After(position) SHALL be set if position > 0

##### Scenario: Position used for heartbeat
- **WHEN** Heartbeat is called
- **THEN** the pump's current position SHALL be sent

#### Requirement: Pump releases lock on heartbeat failure
The Pump SHALL release the lock immediately on any heartbeat failure except HeartbeatConflictError. No retry is attempted. The pump transitions to LockLost and attempts re-acquisition.

##### Scenario: Network error during heartbeat triggers LockLost
- **GIVEN** pump is in Watching state
- **WHEN** Heartbeat returns a transient network error
- **THEN** the pump SHALL transition to LockLost
- **AND** the pump SHALL NOT retry the heartbeat

##### Scenario: LockExpiredError triggers LockLost
- **GIVEN** pump is in Watching state
- **WHEN** Heartbeat returns LockExpiredError
- **THEN** the pump SHALL transition to LockLost

##### Scenario: HeartbeatConflictError corrects position
- **GIVEN** pump is in Watching state
- **WHEN** Heartbeat returns HeartbeatConflictError with CurrentVersion
- **THEN** the pump SHALL update its position to CurrentVersion
- **AND** the pump SHALL continue processing (not LockLost)

#### Requirement: Handler errors indicate recoverability
Handlers registered with the pump MAY return a `HandlerError` interface that indicates whether the error is recoverable. By default, plain errors are treated as non-recoverable.

##### Scenario: Handler returns recoverable error
- **GIVEN** pump is in Watching state
- **WHEN** a handler returns an error implementing `HandlerError` with `Recoverable() == true`
- **THEN** the pump SHALL transition to LockLost
- **AND** the pump SHALL attempt re-acquisition + re-watch after backoff

##### Scenario: Handler returns non-recoverable error
- **GIVEN** pump is in Watching state
- **WHEN** a handler returns a plain error or `HandlerError` with `Recoverable() == false`
- **THEN** the pump SHALL transition to Failed
- **AND** the pump SHALL exit

##### Scenario: Handler returns nil
- **GIVEN** pump is in Watching state
- **WHEN** a handler returns nil
- **THEN** the pump SHALL continue processing the next event

#### Requirement: Pump recovers from watch stream failure
The Pump SHALL recover from watch stream failures by releasing the lock, transitioning to LockLost, and re-acquiring + re-watching. This is consistent with heartbeat failure recovery.

##### Scenario: Watch stream error triggers recovery
- **GIVEN** pump is in Watching state
- **WHEN** TickWithID returns an error (network error, server disconnect)
- **THEN** the pump SHALL release the lock
- **AND** the pump SHALL transition to LockLost
- **AND** the pump SHALL attempt re-acquisition + re-watch after backoff

#### Requirement: WaitForState enables deterministic test waiting
Tests SHALL use WaitForState instead of timeouts to synchronize with pump lifecycle.

##### Scenario: Test waits for pump readiness
- **GIVEN** two projections connected via memory transport
- **WHEN** the test calls WaitForState(ctx, Watching) on both clients
- **THEN** both pumps SHALL be in Watching state before events are submitted
- **AND** assertions SHALL pass without timeout
