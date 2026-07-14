## Why

The embedding indexer is being extracted from another codebase into PGCQRS as a first-class consumer. It requires exclusive access to a (domain, stream) partition to avoid duplicate processing across instances.

This isn't just about the indexer. Multiple consumer patterns are emerging:

- **Embedding indexer** — produces vectors from events, needs exclusive access to avoid duplicate GPU compute
- **Ephemeral projections** — per-process caches of projected state; don't need distributed locks (local mutex suffices), but follow the same consumer lifecycle
- **Persistent indexes** — targeted query optimizations (e.g., entity relationship tracking) that are much faster than re-running full projection sets, especially when dealing with temporal deletion

The current system has no coordination between consumer instances. Without locks:

- Two indexer instances processing the same stream both read and advance positions independently
- This leads to duplicate embedding computation, wasted GPU cycles, and possible inconsistent index state
- There is no inspectability into who is processing what

Advisory locks were considered but rejected: they're in-memory, non-durable, disappear on restart, offer no inspectability, and don't integrate with consumer position tracking.

Consumer locks provide a durable, inspectable foundation that serves the embedding indexer today and future consumers (persistent indexes, projections) tomorrow. A projection framework will be a separate future change.

## What Changes

- New `consumer_names` and `consumer_locks` database tables in PGCQRS core migrations (`000007_consumer_locks.up.sql` / `000007_consumer_locks.down.sql`). A second migration in a future directory will drop the legacy `consumer` TEXT column from `consumer_positions`.
- **Transport interface extended** with five new methods: `TryAcquire`, `Release`, `GetLock`, `ListLocks`, `HeartbeatWithPosition` — enabling lock lifecycle operations from any transport (gRPC, memory). HTTP transport returns "not implemented" for these methods (same pattern as `Watch`).
- **Lock option via typed Option parameter** — `Lock` type implements `Option` interface directly; `NewLock(consumer, holder string)` convenience constructor. Consumers pass `&v1.Lock{...}` or `v1.NewLock(...)` to Submit; transport extracts and enforces atomically. `Watch` is lock-unaware — the client manages lock lifecycle via the heartbeat loop and closes the Watch on expiry.
- New gRPC service `ConsumerLock` with four RPCs:
  - `TryAcquire` — non-blocking, returns immediately with lock status (+ who holds it if not acquired)
  - `Release` — explicit release by the current holder; idempotent
  - `KeepAlive` — bidirectional stream: client sends heartbeats (carrying position), server responds with lock status; supports in-stream `Release` for clean shutdown
  - `ListLocks` — returns all active locks for a domain/stream (inspectability; also implemented on in-memory transport, not on HTTP)
- **Proto `SubmitIn` gains optional `Lock lock` field** — the gRPC adapter extracts lock from options and populates this field; server-side enforces atomically before processing
- Stream closure without auto-release — detected via `io.EOF` on `stream.Recv()` when the client closes without sending a Release message; a metric is emitted but the lock is NOT released (expired locks are ignored on next access; no background reaper)
- Heartbeat stream doubles as a position update mechanism — the server atomically updates both the lock heartbeat and the consumer position in one transaction; if the heartbeat carries a stale position (backward guard fails), the transaction is rolled back and a conflict error is returned with the target version and current version
- Lock rows expire via TTL (default 30s, minimum 6s); expired locks are treated as non-existent on next access; `TryAcquire` cleans up to 128 other expired rows in the same partition (no background reaper goroutine)
- **`ConsumerStore` consolidates lock + position operations** — replaces `PositionStore`; `HeartbeatWithPosition` uses a single `pgx.Tx` for atomicity; a single `ConsumerStore` instance is shared by both HTTP and gRPC paths (fixing the current duplicate `PositionStore` wiring)
- gRPC-only (no HTTP lock endpoints). HTTP transport returns "not implemented" for lock methods.
- **In-memory transport** (`pkg/v1/memory.go`) implements full lock semantics for unit testing: TryAcquire, Release, HeartbeatWithPosition, GetLock, ListLocks, TTL expiry handling (expired locks ignored), and lock option on Submit — enabling the entire consumer lock lifecycle to be tested without PostgreSQL or gRPC

## Capabilities

### New Capabilities
- `consumer-locks`: Durable distributed locks for consumers, with try acquire, heartbeat-based renewal with piggybacked position updates, and TTL-based expiry

### Modified Capabilities
- `consumer-position-tracking`: Existing consumer positions will now be updatable via the lock heartbeat stream (piggybacked), not just via the existing SetPosition RPC. The existing SetPosition RPC remains available for consumers that don't need locks. `PositionStore` is absorbed into `ConsumerStore`. Consumer names are normalized via a `consumer_names` enumeration table (two-phase migration: phase 1 adds FK column alongside existing TEXT, phase 2 in a future release drops the TEXT column).
- `transport-interface`: Transport interface gains `TryAcquire`, `Release`, `GetLock`, `ListLocks`, `HeartbeatWithPosition` methods. `Submit` gains `...Option` variadic parameter for atomic lock enforcement. `QueryBatchR2` and `Watch` are lock-unaware.

## Impact

- New migration for `consumer_names` and `consumer_locks` tables in `migrations/primary/`; `consumer_positions` gains a nullable `consumer_id` FK column (old `consumer` TEXT column retained for zero-downtime upgrade)
- Transport interface (`pkg/v1/transport.go`) extended with 5 new methods
- New `Lock` (implements `Option`), `LockResult`, `LockState`, `Option` types in `pkg/v1/`; `NewLock(consumer, holder string)` constructor
- New `ConsumerStore` in `internal/service/storage/consumer.go` (consolidates `PositionStore`; single instance shared by HTTP and gRPC)
- New gRPC service `ConsumerLock` in `internal/service/grpc.go` and proto definition in `pkg/ipc/query.proto`
- Proto `SubmitIn` gains optional `Lock lock` field. `QueryIn` does not gain a lock field — Watch is lock-unaware.
- gRPC adapter (`pkg/v1/grpcAdapter.go`) extended with lock client methods and option-based extraction
- HTTP transport (`pkg/v1/restful.go`) returns "not implemented" for lock methods
- In-memory transport (`pkg/v1/memory.go`) gets a full lock implementation
- New spec file: `openspec/specs/consumer-locks/spec.md`
- `PositionStore` removed; `grpcConsumerPosition` and HTTP position routes delegate to `ConsumerStore`
