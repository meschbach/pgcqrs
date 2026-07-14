## 1. Proto & Types

- [x] 1.1 Add `ConsumerLock` gRPC service definition to `pkg/ipc/query.proto` with `TryAcquire`, `Release`, `KeepAlive`, `ListLocks` RPCs
- [x] 1.2 Add `TryAcquireIn` (domain, stream, consumer, holder, ttl_seconds) and `TryAcquireOut` (acquired, held_by, guarantee_until, held_until) messages
- [x] 1.3 Add `ReleaseIn` (domain, stream, consumer, holder) and `ReleaseOut` (ok) messages
- [x] 1.4 Add `ListLocksIn` (domain, stream) and `ListLocksOut` (repeated LockState) messages
- [x] 1.5 Add `LockStatusReason` proto enum: `UNSPECIFIED`, `RENEWED`, `EXPIRED`, `STOLEN`, `CONFLICT`
- [x] 1.6 Add `KeepAliveClientMessage` with oneof: `Heartbeat` (domain, stream, consumer, holder, position) or `ReleaseRequest` (empty message — holder identity implicit from stream)
- [x] 1.7 Add `KeepAliveServerMessage` with oneof: `LockStatus` (locked, reason as `LockStatusReason`, guarantee_until, held_until, optional target_version, optional current_version — version fields populated only when reason=CONFLICT) or `ReleaseAck` (bool ok only — final position omitted, client knows its own position)
- [x] 1.8 Add `Lock` message (consumer, holder) and optional `Lock lock` field to `SubmitIn` (field 5). `QueryIn` does not gain a lock field — Watch is lock-unaware.
- [x] 1.9 Regenerate Go protobuf code from the updated proto
- [x] 1.10 Define Go types in `pkg/v1/`: `Lock` (Consumer, Holder) implementing `Option` interface, `LockResult` (Acquired, HeldBy, GuaranteeUntil, HeldUntil), `LockState` (Consumer, Domain, Stream, Holder, AcquiredAt, HeartbeatAt, TTL, GuaranteeUntil, HeldUntil), `Option` interface, `NewLock(consumer, holder string) *Lock` constructor, `LockNotHeldError` (Consumer, Holder, Domain, Stream) error type for lock check failures

## 2. Database Migration

- [x] 2.1 Create migration files `migrations/primary/000007_consumer_locks.up.sql` and `migrations/primary/000007_consumer_locks.down.sql`:
  - Create `consumer_names` table (id BIGSERIAL PK, name TEXT NOT NULL UNIQUE)
  - Create `consumer_locks` table with `consumer_id BIGINT REFERENCES consumer_names(id)`, `stream_id BIGINT REFERENCES events_stream(id) ON DELETE CASCADE`, `holder TEXT`, and TTL columns, PRIMARY KEY (stream_id, consumer_id)
  - Add nullable `consumer_id BIGINT REFERENCES consumer_names(id)` column to `consumer_positions`
  - Backfill `consumer_positions.consumer_id` from existing `consumer` TEXT column via `consumer_names` lookup (single idempotent UPDATE with `WHERE consumer_id IS NULL`)
  - Keep existing `consumer` TEXT column (both coexist for zero-downtime upgrade)
- [x] 2.2 Create Phase 2 migration files in `migrations/future/`:
  - `000001_cleanup_consumer_columns.up.sql` — drop `consumer_positions.consumer` TEXT column, add NOT NULL constraint to `consumer_id`
  - `000001_cleanup_consumer_columns.down.sql` — reverse (add `consumer` TEXT column back, drop NOT NULL from `consumer_id`)
  - These are ready for the next release cycle; do NOT run them as part of this change

## 3. ConsumerStore

- [x] 3.1 Create `ConsumerStore` in `internal/service/storage/consumer.go` with `pg *pgxpool.Pool` field, absorbing `PositionStore` functionality
- [x] 3.2 Implement `resolveConsumerName(ctx, name) (int64, error)` — `INSERT INTO consumer_names (name) ON CONFLICT (name) DO NOTHING` + SELECT to get consumer_id; used by all lock and position methods
- [x] 3.3 Implement `TryAcquire(ctx, domain, stream, consumer, holder, ttl)` — reject if TTL < 6s; resolve consumer name, INSERT with ON CONFLICT check (expired rows are treated as non-existent — the INSERT overwrites them); also DELETE up to 128 other expired rows in the same (domain, stream) partition in the same transaction; returns `LockResult`
- [x] 3.4 Implement `Release(ctx, domain, stream, consumer, holder)` — resolve consumer name, DELETE with holder verification, idempotent
- [x] 3.5 Implement `GetLock(ctx, domain, stream, consumer)` — resolve consumer name, SELECT, returns `LockState`; expired rows (`held_until < NOW()`) are treated as non-existent (return not-found)
- [x] 3.6 Implement `HeartbeatWithPosition(ctx, domain, stream, consumer, holder, position)` — resolve consumer name, single `pgx.Tx` that atomically UPDATEs `consumer_locks` (heartbeat_at, held_until, guarantee_until) only if `held_until > NOW()`, and INSERT/UPDATEs `consumer_positions` with backward-position guard; if position is stale, roll back entire transaction and return conflict error with target_version and current_version
- [x] 3.7 Implement `ListLocks(ctx, domain, stream)` — SELECT all active locks (`held_until > NOW()`) joined with `consumer_names` for a domain/stream pair, returns `[]LockState`
- [x] 3.8 Migrate `SetPosition`, `GetPosition`, `ListConsumers`, `DeletePosition` from `PositionStore` to `ConsumerStore` (update to use `consumer_id` FK via `resolveConsumerName`)
- [x] 3.8a Fix `SetPosition` backward guard: distinguish "stream not found" from "position would go backwards" — return `BackwardPositionError` (existing unused type in `position.go`) when the stream exists but the position is behind the current stored position
- [x] 3.9 Ensure all `ConsumerStore` write paths (`SetPosition`, `HeartbeatWithPosition`) populate **both** `consumer` TEXT and `consumer_id` FK columns during Phase 1 (zero-downtime dual-write until Phase 2 migration drops the TEXT column)
- [x] 3.10 Remove `PositionStore` from `internal/service/storage/position.go` (or deprecate and redirect to `ConsumerStore`)

## 4. Server-Side gRPC Service

- [x] 4.1 Add `consumerStore *ConsumerStore` field to `grpcPort` struct and pass the single shared instance when constructing in `Serve()`
- [x] 4.2 Add `consumerStore *ConsumerStore` field to `grpcCommand` and `grpcQuery` structs
- [x] 4.3 Implement `grpcConsumerLock` struct with `TryAcquire` handler — delegates to `ConsumerStore.TryAcquire`
- [x] 4.4 Implement `Release` handler — delegates to `ConsumerStore.Release`
- [x] 4.5 Implement `KeepAlive` bidirectional streaming handler — processes heartbeats via `ConsumerStore.HeartbeatWithPosition`, handles in-stream Release, detects lock loss (stolen/expired), returns CONFLICT with target_version and current_version on stale position
- [x] 4.5a Add stream-to-holder association: on first `Heartbeat` message, validate holder against lock row and bind stream to that holder for the session; if holder mismatch, return STOLEN and close stream
- [x] 4.6 Add stream closure handling: detect `io.EOF` from `stream.Recv()` without prior Release message, emit `consumer_lock.stream.closed_without_release` metric but do NOT release lock (expired locks are ignored on next access)
- [x] 4.7 Add conditional lock check to `grpcCommand.Submit`: if `Lock` provided in proto, begin `pgx.Tx`, verify lock via explicit SQL SELECT within the transaction (join `consumer_locks` → `consumer_names` → `events_stream`, filter `held_until > NOW()`; no rows → `LockNotHeldError`), then perform the operation within the same transaction, then commit. If no `Lock` provided, existing behavior unchanged (no transaction). Refactor event write into `unsafeStoreWith(ctx, q queryExecer, ...)` accepting the custom `queryExecer` interface (both `*pgxpool.Pool` and `*pgx.Tx` implement this interface), eliminating the two-method split. The public `unsafeStore` wraps `unsafeStoreWith(ctx, pool, ...)`. The gRPC handler calls `unsafeStoreWith` directly with a tx when a lock is present: begin → lock check → unsafeStoreWith(ctx, tx, ...) → commit. FK violation on `events.stream_id` → `StreamNotFoundError`.
- [x] 4.8 Register `ConsumerLock` gRPC service on the gRPC server
- [x] 4.9 Update `grpcConsumerPosition` to delegate to `ConsumerStore` instead of `PositionStore`
- [x] 4.10 Implement `ListLocks` handler — delegates to `ConsumerStore.ListLocks`

## 5. Transport Interface Extension

- [x] 5.1 Add `TryAcquire(ctx, domain, stream, consumer, holder string, ttl time.Duration) (*LockResult, error)` to Transport interface
- [x] 5.2 Add `Release(ctx, domain, stream, consumer, holder string) error` to Transport interface
- [x] 5.3 Add `GetLock(ctx, domain, stream, consumer string) (*LockState, error)` to Transport interface
- [x] 5.4 Add `ListLocks(ctx, domain, stream string) ([]LockState, error)` to Transport interface
- [x] 5.4a Add `HeartbeatWithPosition(ctx, domain, stream, consumer, holder string, position int64) error` to Transport interface
- [x] 5.5 Implement `TryAcquire` on `GrpcAdapter` — delegates to gRPC `ConsumerLock.TryAcquire`
- [x] 5.6 Implement `Release` on `GrpcAdapter` — delegates to gRPC `ConsumerLock.Release`
- [x] 5.7 Implement `GetLock` on `GrpcAdapter` — delegates to gRPC (or returns error if not available)
- [x] 5.8 Implement `ListLocks` on `GrpcAdapter` — delegates to gRPC `ConsumerLock.ListLocks`
- [x] 5.8a Implement `HeartbeatWithPosition` on `GrpcAdapter` — delegates to gRPC `ConsumerLock.KeepAlive` (unary wrapper)
- [x] 5.9 Implement `TryAcquire` on `HTTPTransportLayer` — returns `errors.New("not implemented")`
- [x] 5.10 Implement `Release` on `HTTPTransportLayer` — returns `errors.New("not implemented")`
- [x] 5.11 Implement `GetLock` on `HTTPTransportLayer` — returns `errors.New("not implemented")`
- [x] 5.12 Implement `ListLocks` on `HTTPTransportLayer` — returns `errors.New("not implemented")`
- [x] 5.13 Implement `HeartbeatWithPosition` on `HTTPTransportLayer` — returns `errors.New("not implemented")`

## 6. Lock Option Enforcement

Lock options are verified on every Submit call (not just at creation). Watch is lock-unaware — the client manages lock lifecycle via the heartbeat loop. `QueryBatchR2` / `StreamBatch` are lock-unaware — they are not part of the consumer workflow. All transport implementations extract and enforce the lock on Submit.

- [x] 6.1 Update `Stream.Submit` signature to accept `...Option` variadic parameter
- [x] 6.2 Update `Transport.Submit` interface to accept `...Option` variadic parameter
- [x] 6.3 In `memory.Submit`: extract `Lock` from options, if present verify lock via atomic transaction (check lock + append packet in single simulateNetwork op), reject if not held (`held_until < NOW()`)
- [x] 6.4 In `GrpcAdapter.Submit`: extract `Lock` from options, populate `SubmitIn.lock` field

## 6a. Watch API Evolution

- [x] 6a.1 Add `Channel(ctx context.Context, backlog int) <-chan Envelope` method to `query2.Watch` — runs `Tick()` in a hidden goroutine, feeds events to a buffered channel with the specified backlog size (default 16 if 0). The `ctx` parameter controls the hidden goroutine's lifecycle. The channel is closed when `Tick()` returns an error. The hidden goroutine uses the provided context for cancellation.
- [x] 6a.2 Add `Err() error` method to `query2.Watch` — returns the error that caused the channel to close (nil if closed cleanly via context cancellation). Must be called after the channel is drained/closed.
- [x] 6a.3 Add `Events(ctx context.Context) iter.Seq2[Envelope, error]` method to `query2.Watch` — pull-based range-over-function iterator wrapping `Tick()`. No internal goroutine beyond the existing `Tick()` pump. Yields `Envelope` values until `Tick()` returns an error.
- [x] 6a.4 Write tests for `Channel()` — verify events flow through, verify error closes channel, verify context cancellation stops the pump
- [x] 6a.5 Write tests for `Events()` iterator — verify range-over-function yields events, verify error terminates iteration, verify context cancellation

## 7. In-Memory Transport

- [x] 7.0 Add `now func() time.Time` field to `memory` struct (defaults to `time.Now`); all lock operations use `m.now()` instead of `time.Now()` for expiry checks; tests inject a controllable clock to trigger TTL expiry without real time delays
- [x] 7.1 Add `locks map[string]*memoryLock` to `memory` struct with key = domain + "/" + stream + "/" + consumer
- [x] 7.2 Define `memoryLock` struct: holder, acquiredAt, heartbeatAt, ttl, guaranteeUntil, heldUntil
- [x] 7.3 Implement `TryAcquire` via `simulateNetwork` — reject if TTL < 6s; mutex-protected map insert with conflict check; expired rows are treated as non-existent (overwrite on conflict); also remove up to 128 other expired rows in the same domain/stream
- [x] 7.4 Implement `Release` via `simulateNetwork` — holder verification, idempotent delete
- [x] 7.5 Implement `GetLock` via `simulateNetwork` — map lookup, check held_until > now; expired rows are treated as non-existent (return not-found, do not delete)
- [x] 7.6 Implement `ListLocks` via `simulateNetwork` — return all active locks (held_until > now) for a domain/stream pair
- [x] 7.7 Implement `HeartbeatWithPosition` via `simulateNetwork` — atomic lock heartbeat + position update (single operation); if position is stale (backward guard fails), return conflict with target_version and current_version; if lock expired or held by different holder, return appropriate error
- [x] 7.8 Add position tracking to `memory` struct (already has `positions` map); `HeartbeatWithPosition` updates both lock heartbeat and position atomically via `simulateNetwork`

## 8. Client-Side gRPC Adapter

- [x] 8.1 Add lock client methods to `GrpcAdapter` (TryAcquire, Release, ListLocks) delegating to `ConsumerLock` gRPC client
- [x] 8.2 Implement `KeepAlive` client-side struct with `Heartbeat(ctx, position int64) error` and `Release(ctx) error` methods. No goroutine, no ticker, no Run/Stop. The client owns the goroutine and drives heartbeat timing via `context.WithTimeout`. The `KeepAlive` struct wraps the bidirectional gRPC `ConsumerLock.KeepAliveClient` stream and exposes synchronous send+recv.
- [x] 8.3 Implement `KeepAlive.Heartbeat`: send `KeepAliveClientMessage.Heartbeat` with domain/stream/consumer/holder/position over the bidirectional stream, receive `KeepAliveServerMessage.LockStatus` response, return error if reason is CONFLICT/EXPIRED/STOLEN
- [x] 8.4 Implement `KeepAlive.Release`: send `KeepAliveClientMessage.ReleaseRequest` over the bidirectional stream, receive `KeepAliveServerMessage.ReleaseAck`, return error if ack.ok is false

## 9. Service Wiring

- [x] 9.1 Create a single `ConsumerStore` instance in `Serve()` from pgxpool, shared by HTTP and gRPC paths
- [x] 9.2 Pass `consumerStore` to `grpcPort` when constructing (same instance used by HTTP routes)
- [x] 9.3 Update HTTP position routes to delegate to `ConsumerStore` instead of `PositionStore`

## 10. Observability

- [x] 10.1 Add OTel metrics to `internal/service/storage/otel.go`: counters (acquire attempts/successes/failures, release explicit, heartbeat processed, heartbeat conflict, expiry ignored, stream closed without release, assertion checks/rejections), gauge (active locks), histograms (hold duration, heartbeat interval)
- [x] 10.2 Add tracing spans to `ConsumerStore` methods (TryAcquire, Release, HeartbeatWithPosition, GetLock, ListLocks) with attributes: domain, stream, consumer, holder, status, ttl, guarantee_until, held_until, position, target_version, current_version
- [x] 10.3 Add tracing spans to `grpcConsumerLock` handlers (TryAcquire, Release, ListLocks) and stream events (heartbeat, release, stream closed)
- [x] 10.4 Add metrics recording to `ConsumerStore` methods: increment counters on acquire/release/heartbeat/expiry/assertion, update gauge on lock acquire/release
- [x] 10.5 Add span events for significant moments: heartbeat.renewed, heartbeat.expired, heartbeat.conflict, stream.closed_without_release (also emit counter metric)
- [x] 10.6 Verify in-memory transport creates spans but does not emit metrics

## 11. Tests

- [x] 11.1 Write unit tests for `ConsumerStore` (try acquire ignoring expired locks, try acquire cleans up other expired rows up to 128, try acquire rejects TTL < 6s, heartbeat with position, heartbeat with stale position returns conflict error, release, list locks, consumer name resolution/deduplication)
- [x] 11.2 Write unit tests for Release (explicit, idempotent, non-holder rejection)
- [x] 11.3 Write unit tests for lock expiry (TryAcquire ignores expired lock and creates new one, GetLock returns not-found for expired lock)
- [x] 11.3a Write unit tests for SetPosition backward guard (returns BackwardPositionError when stream exists but position is behind, returns StreamNotFoundError when stream doesn't exist)
- [x] 11.4 Write unit tests for lock option enforcement (valid lock succeeds on Submit, expired lock rejected with LockNotHeldError, no lock backward compatible with no transaction opened, atomic transaction on Submit — verify no transaction opened when no Lock option provided)
- [x] 11.5 Write unit tests for in-memory lock transport (TryAcquire, Release, GetLock, ListLocks, HeartbeatWithPosition with atomic lock+position update, stale-position conflict detection, TTL expiry ignore semantics, lock option on Submit)
- [x] 11.5a Write unit tests for in-memory clock injection (inject controllable clock, advance past TTL, verify TryAcquire succeeds on expired lock, verify GetLock returns not-found for expired lock, verify HeartbeatWithPosition fails on expired lock)
- [x] 11.6 Write gRPC integration test for lock acquire, heartbeat, heartbeat conflict (stale position returns target_version + current_version), release, list locks, and lock option
- [x] 11.7 Write gRPC integration test for KeepAlive bidirectional stream (heartbeat renewal, in-stream release, stream closure without release emits metric)
- [x] 11.7a Write unit tests for client-side `KeepAlive` struct — `Heartbeat` sends message and returns nil on RENUMED, returns error on CONFLICT/EXPIRED/STOLEN; `Release` sends message and returns nil on ack.ok, returns error on ack.not-ok
- [x] 11.7b Write unit tests for `query2.Watch.Channel()` — events flow through channel, error closes channel, context cancellation stops pump, `Err()` returns correct error
- [x] 11.7c Write unit tests for `query2.Watch.Events()` iterator — range-over-function yields events, error terminates iteration, context cancellation
- [x] 11.8 Run full test suite (`go test -count=1 ./pkg/... ./internal/...`) and verify all pass
- [x] 11.9 Run linter (`golangci-lint run ./...`) and fix any issues
