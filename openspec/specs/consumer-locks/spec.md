# Consumer Locks

## Purpose

TBD

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
