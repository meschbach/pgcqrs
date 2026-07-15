## Context

Current consumer position tracking (`consumer_positions` table, `SetPosition`/`GetPosition` gRPC) allows consumers to track their progress but provides no exclusive-access guarantee. If two indexer instances both try to consume the same stream, they can both read and advance positions independently — no coordination.

The existing `consumer-position-tracking` spec defines position semantics (forward-only, per-stream per-consumer). This design extends that with distributed locks.

Multiple consumer patterns are emerging in the PGCQRS ecosystem:

- **Embedding indexer** (current need) — being extracted from another codebase, requires exclusive access to avoid duplicate GPU compute across instances
- **Ephemeral projections** (future) — per-process caches of projected state; don't need distributed locks (local mutex suffices), but follow the same consumer lifecycle
- **Persistent indexes** (future) — targeted query optimizations (e.g., entity relationship tracking) that are faster than re-running full projection sets, especially with temporal deletion

Consumer locks are the foundational mechanism that enables exclusive access for all persistent consumer patterns. A higher-level projection framework will be a separate future change.

## Goals / Non-Goals

**Goals:**
- Consumer can acquire an exclusive lease on a (consumer, domain, stream) tuple
- Lock expires after TTL unless heartbeats renew it
- Lock heartbeat carries the consumer position; server atomically updates both lock + position in one transaction
- gRPC-only API (no HTTP). In-memory transport for unit tests
- Inspectable: who holds which locks, when they expire (via ListLocks RPC)
- Transport interface extended with lock lifecycle methods (TryAcquire, Release, GetLock, ListLocks)
- Lock option on Query/Submit via typed Option parameter (not context-only)

**Non-Goals:**
- Not a general-purpose distributed lock; scoped to consumer semantics
- No lock queueing or fairness guarantees beyond AcquireWithTimeout polling
- Fake gRPC in-memory transport (deferred to separate change)

## Decisions

**Decision: Transport interface extended with lock lifecycle methods**

The `Transport` interface gains four new methods:
- `TryAcquire(ctx, domain, stream, consumer, holder, ttl) (*LockResult, error)`
- `Release(ctx, domain, stream, consumer, holder) error`
- `GetLock(ctx, domain, stream, consumer) (*LockState, error)`
- `ListLocks(ctx, domain, stream) ([]LockState, error)`

HTTP transport returns "not implemented" for these methods (same pattern as `Watch` at `pkg/v1/restful.go:279`). This enables lock operations from any transport without changing Query/Submit signatures.

Alternative considered: keep locks gRPC-only with no Transport interface change. Rejected because the in-memory transport needs to implement locks for unit testing, and the Transport interface is the abstraction boundary.

**Decision: Transport interface extended with HeartbeatWithPosition**

The `Transport` interface gains `HeartbeatWithPosition` alongside the other lock lifecycle methods:

```go
HeartbeatWithPosition(ctx context.Context, domain, stream, consumer, holder string, position int64) error
```

HTTP transport returns "not implemented" for this method (same pattern as Watch and the other lock methods). In-memory and gRPC transports implement it fully. This enables the complete lock lifecycle — including heartbeat-based renewal with piggybacked position updates — to be tested via the in-memory transport without PostgreSQL or gRPC.

The Transport interface is the abstraction boundary for all lock operations. HTTP rejects; in-memory and gRPC implement.

**Decision: Lock option on Query/Submit via typed Option parameter**

Lock assertions on Query/Submit are passed via a typed `Option` parameter:

```go
// pkg/v1/ — exported Lock type implements Option directly
type Lock struct {
    Consumer string
    Holder   string
}

type Option interface {
    apply(*config)
}

func (l *Lock) apply(c *config) {
    c.lock = l
}

// Convenience constructor
func NewLock(consumer, holder string) *Lock {
    return &Lock{Consumer: consumer, Holder: holder}
}
```

**Submit signature change:**
```go
// Before
func (s *Stream) Submit(ctx context.Context, kind string, event interface{}) (*Submitted, error)

// After
func (s *Stream) Submit(ctx context.Context, kind string, event interface{}, opts ...Option) (*Submitted, error)
```

**Query2 signature change:**
```go
// Before
func (q *Query) StreamBatch(ctx context.Context) (*v1.WireBatchR2Result, error)
func (q *Query) Watch(ctx context.Context) (v1.WatchInternal, error)

// After — unchanged, both are lock-unaware
func (q *Query) StreamBatch(ctx context.Context) (*v1.WireBatchR2Result, error)
func (q *Query) Watch(ctx context.Context) (v1.WatchInternal, error)
```

**Transport interface signature change:**
```go
Submit(ctx context.Context, domain, stream, kind string, event interface{}, opts ...Option) (*Submitted, error)
QueryBatchR2(ctx context.Context, domain, stream string, batch *WireBatchR2Request, out *WireBatchR2Result) error  // unchanged — not part of consumer workflow
Watch(ctx context.Context, query *ipc.QueryIn) (WatchInternal, error)  // unchanged — Watch is lock-unaware
```

The `StreamTransport` interface (used by query2) is unchanged:
```go
type StreamTransport interface {
    QueryBatchR2(ctx context.Context, batch *WireBatchR2Request) (*WireBatchR2Result, error)  // unchanged
    Watch(ctx context.Context, query *ipc.QueryIn) (WatchInternal, error)  // unchanged
}
```

Only `Submit` gains `...Option` on the Transport interface. Memory and gRPC extract and enforce the lock on `Submit`. HTTP ignores the option. `QueryBatchR2` and `Watch` are unchanged across all transports.

`QueryBatchR2` does not gain lock support because `StreamBatch` is not part of the consumer workflow — the consumer uses Watch (event processing) and Submit (event writing) with the heartbeat loop managing lock lifecycle. `StreamBatch` is used for ad-hoc one-shot queries where lock enforcement is unnecessary.

**Usage:**
```go
// Struct literal
stream.Submit(ctx, "kind", event, &v1.Lock{Consumer: "idx", Holder: "pod-1"})

// Constructor
stream.Submit(ctx, "kind", event, v1.NewLock("idx", "pod-1"))

// Query2 Watch — lock-unaware, managed via heartbeat loop
watch := query2.NewQuery(stream).
    OnKind("Foo").
    Watch(ctx)
```

The transport implementations extract the lock from options on `Submit`:
- Memory transport: extracts from opts, validates lock via atomic transaction before Submit
- gRPC adapter: extracts from opts, populates `SubmitIn.lock` proto field
- HTTP transport: ignores lock option (HTTP doesn't support locks; lock methods return "not implemented" separately)

`Watch` is lock-unaware across all transports. The client manages lock lifecycle via the heartbeat loop; when the lock expires, the heartbeat fails and the client closes the Watch.

The proto message `SubmitIn` gains an optional `Lock lock` field. The server enforces the lock atomically before processing the submit.

**Atomic guarantee:** When a `Lock` is provided, the server wraps the operation in a transaction: verify lock is held, then perform the write. This ensures the lock is valid at write time, not just at check time. The client accepts this additional cost by opting into the lock.

**Submit transaction architecture (conditional on Lock option):**

The Submit path splits into two flows based on whether a `Lock` is provided:

```
No lock (fast path — unchanged):
  unsafeStore(ctx, app, stream, kind, body)
    → pool.Query(INSERT INTO events_kind ...)    ← round trip 1
    → pool.Query(INSERT INTO events ...)         ← round trip 2

With lock (new path — atomic via pgx.Tx):
  grpcCommand.Submit(ctx, in, lock)
    → pg.BeginTx(ctx)                                     ← begin transaction
    → tx.QueryRow(lock_check_sql, consumer, domain, stream)
        → no rows → tx.Rollback(), return LockNotHeldError
    → tx.Query(INSERT INTO events_kind ...)                ← round trip 3 (on tx)
    → tx.Query(INSERT INTO events ...)                     ← round trip 4 (on tx)
        → FK violation on stream_id → tx.Rollback(), return StreamNotFoundError
    → tx.Commit()                                         ← round trip 5
    → bus.dispatchOnEventStored(...)
```

The lock check is a separate, explicit SQL query within the transaction — not a CTE or subquery. This keeps error handling clean: each step has a single, unambiguous failure mode. The lock check SQL joins `consumer_locks` → `consumer_names` → `events_stream` and filters by `held_until > NOW()`.

**Decision: Unify store path via `queryExecer` interface**

Both `*pgxpool.Pool` and `*pgx.Tx` implement a common interface with `Query` and `QueryRow` methods. This means the event write logic does not need two separate methods — a single internal method accepting this interface works with both a bare pool connection and a transaction:

```go
// internal/service/storage.go — custom interface since pgx v5 does not export QueryExecer
type queryExecer interface {
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Single internal method — works with pool OR tx
func unsafeStoreWith(ctx context.Context, q queryExecer, app, stream, kind string, body []byte) error {
    // kind upsert + event insert — identical SQL regardless of q type
}
```

**Note:** pgx v5 does not export a `QueryExecer` interface (despite some documentation suggesting otherwise). The custom `queryExecer` interface above is defined locally in `internal/service/storage.go` with only the methods actually needed (`Query` and `QueryRow`; `Exec` is not required for the store path). Both `*pgxpool.Pool` and `*pgx.Tx` satisfy this interface via their existing method signatures.

The caller owns the transaction lifecycle:
- **No lock (fast path):** `unsafeStoreWith(ctx, pool, ...)` — no transaction overhead, 2 round-trips
- **With lock:** `tx, _ := pool.Begin(ctx)` → lock check on tx → `unsafeStoreWith(ctx, tx, ...)` → `tx.Commit()` — 5 round-trips (begin, lock check, kind upsert, event insert, commit)

The public `unsafeStore` wrapper remains for backward compatibility — it calls `unsafeStoreWith(ctx, pool, ...)`. The gRPC handler calls `unsafeStoreWith` directly when orchestrating a lock-checked transaction.

This eliminates the two-method split (`unsafeStore` + `unsafeStoreInTx`), avoids type assertions, and keeps the SQL logic in exactly one place. The transaction boundary is explicit at the call site, not hidden inside the store method.

**Query count:** The lock-checked Submit path executes 5 queries (begin, lock check, kind upsert, event insert, commit) versus 2 for the non-lock path. This is an accepted cost for the atomicity guarantee. The no-lock path remains on the fast path (pool direct, no transaction) — the `queryExecer` unification means both paths share identical SQL with zero overhead on the non-lock path. Future optimization potential: combine the lock check and event insert into a single CTE statement, reducing to 3 queries (begin, combined statement, commit). This is deferred — the current approach prioritizes clear error handling and maintainability over query minimization.

**No context-based lock helpers.** Lock assertions are always explicit via the `...Option` parameter. No `WithLockAssertion`/`LockAssertionFrom` context helpers. The explicit option is the API — if a lock is needed, it is passed directly. This makes lock requirements visible at the call site.

Alternative considered: context-only approach. Rejected because it hides the lock requirement from the API surface and makes the atomic guarantee implicit.

**Decision: Deprecated query APIs excluded from Option support**

The old `QueryBuilder` API (`Query.Perform`, `QueryBuilder.Stream`) and the `Transport.QueryBatch` (R1) method are deprecated and do not gain `...Option` support. Only `Stream.Submit` and the corresponding transport interface (`Transport.Submit`) accept lock options. `QueryBatchR2`, `StreamBatch`, and `Watch` are lock-unaware — they do not accept lock options. The deprecated paths are exclusively used by the old `QueryBuilder` in `pkg/v1/queryStream.go` and `pkg/v1/query.go` — no query2 code touches them.

**Decision: ConsumerStore consolidates lock + position operations**

A single `ConsumerStore` replaces `PositionStore` and absorbs lock operations:
- Lock methods: `TryAcquire`, `Release`, `GetLock`, `HeartbeatWithPosition`, `ListLocks`
- Position methods: `SetPosition`, `GetPosition`, `ListConsumers`, `DeletePosition`
- Consumer name resolution: `resolveConsumerName(name) (int64, error)` — resolves consumer name to `consumer_id` via `consumer_names` table

`HeartbeatWithPosition` uses a single `pgx.Tx` to atomically update both `consumer_locks` and `consumer_positions` tables. Both tables now reference `consumer_id` (FK to `consumer_names`), making the atomic transaction straightforward. If the heartbeat carries a stale position (backward guard fails), the transaction is rolled back and a conflict error is returned.

A single `ConsumerStore` instance is created in `Serve()` and shared by both HTTP and gRPC paths. This fixes the current duplicate `PositionStore` wiring where `service.positions` and `grpcPort.positions` are separate instances wrapping the same pool.

The `grpcConsumerPosition` handler delegates to `ConsumerStore`. The `grpcCommand` handler gets a `consumerStore` reference for lock assertion checks on Submit.

**Decision: Fix BackwardPositionError defect in SetPosition**

The existing `SetPosition` implementation uses a SQL-level backward guard (`WHERE NOT EXISTS ... WHERE COALESCE(previous_event_id, 0) > $4`). When the guard fails, the INSERT returns no rows, and the code returns `StreamNotFoundError` — which is incorrect. A backward position attempt on an existing stream should return a `BackwardPositionError`, not "stream not found."

The `BackwardPositionError` type already exists in `internal/service/storage/position.go` but is unused. When migrating `SetPosition` to `ConsumerStore`, fix this by:
1. Querying the stream existence separately (or using a CTE that distinguishes "stream not found" from "backward rejected")
2. Returning `BackwardPositionError` when the stream exists but the position would go backwards
3. Returning `StreamNotFoundError` only when the stream genuinely doesn't exist

**Decision: Consumer names normalized via enumeration table**

Consumer names (e.g., "search-index", "embedding-indexer") are deduplicated via a global `consumer_names` table. Both `consumer_locks` and `consumer_positions` reference this table via `consumer_id` FK instead of storing the name as TEXT.

```sql
CREATE TABLE consumer_names (
    id   BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);
```

The `holder` column in `consumer_locks` remains as TEXT — holders are transient (pod names), high-cardinality, and not worth normalizing.

Consumer names are resolved on write via `INSERT INTO consumer_names (name) ON CONFLICT (name) DO NOTHING` followed by a SELECT. This is a simple enumeration/dedup table — no in-process caching in `ConsumerStore`. Consumer names are stable (typically a small set like "search-index", "embedding-indexer") and the two round-trips are acceptable for the lock and position write paths.

**Decision: Two-phase migration for consumer_positions**

Migrating `consumer_positions` to use `consumer_id` FK is done in two phases to avoid downtime:

**Phase 1 (this change — `000007`):**
- Create `consumer_names` table
- Create `consumer_locks` with `consumer_id FK` directly
- Add nullable `consumer_id BIGINT REFERENCES consumer_names(id)` column to `consumer_positions`
- Seed `consumer_names` from existing data (`INSERT INTO consumer_names(name) SELECT DISTINCT consumer FROM consumer_positions ON CONFLICT DO NOTHING`) before backfilling
- Backfill `consumer_positions.consumer_id` from existing `consumer` TEXT column via a single idempotent UPDATE with `WHERE consumer_id IS NULL` (safe for typical table sizes — one row per consumer per domain/stream)
- Keep old `consumer` TEXT column (both coexist during rolling upgrade)
- All writes populate **both** `consumer` TEXT and `consumer_id` FK columns; reads use `consumer_id` (via JOIN)
- GetPosition, ListConsumers, and DeletePosition use LEFT JOIN on `consumer_names` to prefer the FK path and fall back to the TEXT column for pre-migration rows where `consumer_id IS NULL`

**Phase 2 (future change — `migrations/future/`):**
- Drop `consumer_positions.consumer` TEXT column
- Add NOT NULL constraint to `consumer_positions.consumer_id`

The Phase 2 migration files will be created in `migrations/future/` as part of this change so they are ready for the next release cycle.

**Migrator safety:** The `migrations/future/` directory is safe because the migrator embeds only `migrations/primary/*.sql` via the `//go:embed "primary/*.sql"` directive in `migrations/primary.go`. There is no runtime directory scanning — the migration source is compile-time. The `future/` directory will not be included in the binary until a second embed directive and corresponding `MigrateFuture()` function are explicitly added.

This ensures zero-downtime upgrades: during rolling deploy, old code reads `consumer` TEXT, new code reads `consumer_id` FK. Both columns are populated by all writes until Phase 2 is executed.

**Decision: Lock key = (consumer, domain, stream)**

The lock is scoped to the same granularity as consumer positions. A consumer is a logical name like `"embedding-indexer-v2"`; when combined with domain+stream, it identifies a specific work partition.

Alternative considered: lock by consumer name alone. Rejected because the same logical consumer (e.g., "search-index") might legitimately process different domains in parallel.

**Decision: Atomic lock + position update via heartbeat; backward position is a conflict error**

The `KeepAlive` stream carries the consumer's latest processed event ID. The server runs both updates in a single `pgx.Tx`:
1. `UPDATE consumer_locks SET heartbeat_at=NOW(), held_until=NOW()+ttl, ... WHERE stream_id=$1 AND consumer_id=$2`
2. `INSERT INTO consumer_positions ... ON CONFLICT ... DO UPDATE ... WHERE event_id >= $new_event_id` (backward-position guard)

If the backward guard fails (heartbeat carries a position behind the current stored position), the entire transaction is rolled back — the lock is NOT renewed. The server returns a conflict error to the client containing:
- `target_version`: the event ID the client attempted to write
- `current_version`: the event ID currently stored in the database

This surfaces an operational problem (another consumer processing, or internal defect) to the client/operator for investigation. The consumer's lock will expire and another instance can acquire it.

Alternative considered: renew the lock even if the position is stale. Rejected because it would silently mask a conflict — the stale consumer continues holding the lock while another consumer may have advanced the position.

**Decision: TTL-based expiry — expired locks ignored, bounded cleanup on acquire**

Expired locks (where `held_until < NOW()`) are treated as non-existent on the next `TryAcquire` or `GetLock` access. The expired row is **ignored, not deleted** by `GetLock`. On `TryAcquire`, the expired row is overwritten by the new lock via `INSERT ... ON CONFLICT`.

To bound accumulated expired rows from abandoned consumers, `TryAcquire` also cleans up **up to 128** other expired rows in the same (domain, stream) partition in the same transaction. This keeps the table bounded without a background reaper. The limit of 128 is chosen to keep the cleanup query fast (single DELETE with `LIMIT 128`) while preventing unbounded work per acquisition.

This eliminates the need for a background reaper goroutine, its lifecycle management, and its interaction with the `simulateNetwork` pattern in the in-memory transport. Postgres handles the storage; the application handles the logic on access.

Expired rows are bounded because:
- The target row is overwritten on next acquisition (no per-tuple accumulation)
- Other expired rows in the same partition are cleaned up on next `TryAcquire` (up to 128 per call)
- The `ListLocks` endpoint filters by `held_until > NOW()`, so expired locks are invisible to operators
- The table is naturally small (one row per active consumer per domain/stream)

**Decision: gRPC only, no HTTP**

The indexer framework uses gRPC exclusively. HTTP watch is already unimplemented. Adding HTTP lock endpoints adds surface area with no consumers. HTTP transport returns "not implemented" for all lock methods: `TryAcquire`, `Release`, `GetLock`, `ListLocks`, and `HeartbeatWithPosition`.

**Decision: ListLocks for gRPC and in-memory transport only**

A `ListLocks(domain, stream)` RPC returns all active locks for a given domain/stream pair. This provides inspectability into who holds which locks and when they expire. The in-memory transport also implements `ListLocks` for unit testing. HTTP transport does not support lock listing.

**Decision: In-memory transport implements full lock lifecycle (including HeartbeatWithPosition)**

The `memory` transport in `pkg/v1` needs full lock semantics for unit testing the consumer lifecycle without PostgreSQL or gRPC. The memory implementation supports:
- `TryAcquire` / `Release` / `GetLock` with mutex-protected map
- `HeartbeatWithPosition` — atomic lock heartbeat + position update (single operation, matching the `pgx.Tx` semantics of the Postgres store)
- Stale-position conflict detection (backward guard, returns conflict with target_version and current_version)
- TTL expiry handling (check `held_until > now` on every operation; expired rows ignored by GetLock, overwritten on TryAcquire; TryAcquire also cleans up to 128 other expired rows in the same domain/stream)
- Lock option on queries/submits (via typed Option parameter, atomic transaction)
- `ListLocks` for inspectability in tests

All lock operations go through `simulateNetwork` to maintain the single-goroutine serialization pattern. `HeartbeatWithPosition` is on the Transport interface — HTTP returns "not implemented", in-memory and gRPC implement it.

**Clock injection for TTL testing:** The `memory` struct gains a `now func() time.Time` field (defaulting to `time.Now`). All lock operations use `m.now()` instead of `time.Now()` for expiry checks. Tests inject a controllable clock and advance it to trigger TTL expiry, then verify `TryAcquire` succeeds and `GetLock` returns not-found. This enables testing the full expiry lifecycle without background goroutines or real time delays.

This allows the entire consumer lock lifecycle to be tested in-process, matching the gRPC transport behaviour. The in-memory transport covers: lock acquire/release, heartbeat with position update, stale-position conflict detection, lock assertion enforcement, and TTL expiry handling — all without PostgreSQL or gRPC.

**Decision: Explicit release via unary RPC and in-stream message**

Lock holders can release via a unary `Release` RPC (for cases without an active stream) or via a `Release` message in the `KeepAlive` bidirectional stream (for clean shutdown while streaming). Both are idempotent — releasing an already-expired or already-released lock returns success. Only the current holder can release; attempts by other holders are rejected.

Alternative considered: release only via stream closure. Rejected because it doesn't distinguish expected shutdown from crash, and requires the client to maintain an active stream just to release.

**Decision: Stream closure is observation-only, not auto-release**

When the `KeepAlive` stream closes without the client sending a `Release` message, the server emits a metric (`consumer_lock.stream.closed_without_release`) but does NOT release the lock. Expired locks are treated as non-existent on the next access. This avoids the race condition where the stream fails (network partition) but the client is still processing — auto-releasing would allow another client to acquire the lock while the original is still working.

Stream closure is detected via `io.EOF` from `stream.Recv()` when the client closes its send side. The handler tracks a `releaseSent` boolean; on `io.EOF` without release, the metric is emitted.

Alternative considered: auto-release on stream closure. Rejected because stream closure is not a reliable crash signal — it could indicate network partition, gRPC timeout, or load balancer drop, none of which mean the client has stopped processing.

**Decision: KeepAlive server-side session tracking via local variable**

The `KeepAlive` bidirectional stream handler tracks which holder is bound to the stream using a local variable (`boundHolder string`) within the handler function. On the first `Heartbeat` message, the server validates the holder against the lock row and stores it in `boundHolder`. Subsequent messages are validated against `boundHolder` (not re-fetched from the database). If the first `Heartbeat` carries a holder that doesn't match the lock row, the server returns `STOLEN` and the stream should be closed. No struct-level state is needed — the binding lives and dies with the stream handler invocation.

**Decision: guarantee_until and held_until timing model**

The lock has two periods within its TTL:
- **Guarantee period** (90% of TTL, e.g., 27s for 30s TTL): Client-side advisory. The client should target renewal before `guarantee_until`. The server does not distinguish between guarantee and advisory periods — it only checks `held_until`.
- **Advisory period** (10% of TTL, e.g., 3s for 30s TTL): Client-side advisory. Absorbs clock drift between client and server. The server does not treat this period specially.

Server-side lock validity: `held_until > NOW()`. If `held_until` has passed, the lock is expired and must be reacquired via normal `TryAcquire` semantics. There is no "late renewal" path on the server — the server does not distinguish between on-time and late heartbeats.

Both `guarantee_until` and `held_until` are stored in the `consumer_locks` table and updated on every heartbeat. On the next `TryAcquire` or `GetLock`, locks with `held_until < NOW()` are treated as non-existent (ignored).

Default TTL: 30s. Guarantee: 27s. Advisory: 3s.
Default heartbeat interval: 30s (client-side). Hard minimum: 6s.
Server-enforced minimum TTL: 6s (rejects `TryAcquire` with TTL < 6s to prevent excessive write load).

Operational requirement: all nodes must be within the advisory period duration (3s) of each other, achievable with NTP or equivalent clock sync.

**Decision: Client-managed heartbeat via context.WithTimeout — no library-side goroutine**

The client drives heartbeat timing entirely. No library-side goroutine, ticker, or pump is involved. The client creates a `context.WithTimeout` for the heartbeat interval (e.g., 30s), and when that context expires, it calls `KeepAlive.Heartbeat(ctx, position)` synchronously. The server responds with a new `guarantee_until`; the client creates a fresh timeout context.

```go
// Client loop — single goroutine, no hidden pump
for {
    hbCtx, hbCancel := context.WithTimeout(ctx, 30*time.Second)
    select {
    case <-hbCtx.Done():
        hbCancel()
        if err := ka.Heartbeat(ctx, position); err != nil {
            return err  // CONFLICT, network error, etc.
        }
    case event := <-watchCh:
        hbCancel()
        process(event)
        position = event.ID
    case <-ctx.Done():
        hbCancel()
        ka.Release(ctx)
        return ctx.Err()
    }
}
```

The zombie scenario resolves naturally: if the processing goroutine deadlocks, nobody selects on `hbCtx.Done()`, no heartbeat is sent, and the server-side `held_until` timeout expires. No staleness detection is needed on the server — just a timeout.

The `KeepAlive` type on the client side is a thin wrapper over the bidirectional gRPC stream:
- `Heartbeat(ctx, position int64) error` — send heartbeat, receive response, return error on CONFLICT
- `Release(ctx) error` — send release, receive ack, clean shutdown

No `Run()`, no `Stop()`, no `Events()` channel on the keepalive. The client owns the goroutine and the select loop.

The `guarantee_until` field is purely a client-side advisory — the server does not use it for any enforcement. Server-side lock validity is determined solely by `held_until > NOW()`.

**Decision: Watch API gains Channel() and Events() iterator — existing Tick() frozen**

The existing `WatchInternal` interface (`Tick(ctx) (*QueryOut, error)`) is frozen. Two new accessors are added on the query2 `Watch` type to support different client integration patterns:

**`Events(ctx) iter.Seq2[Envelope, error]`** — Pull-based iterator for the common case (no heartbeat needed). Wraps `Tick()` in a range-over-function loop. The caller drives the loop; no internal goroutine beyond the hidden `Tick()` pump that feeds the iterator.

```go
// Simple case: just process events
for event, err := range watch.Events(ctx) {
    if err != nil { return err }
    process(event)
}
```

**`Channel(ctx context.Context, backlog int) <-chan Envelope`** — Push-based channel for select-loop integration (heartbeat path). The `ctx` parameter controls the hidden goroutine's lifecycle; cancelling it stops the `Tick()` pump. The `backlog` parameter sets the buffered channel size (default 16 if 0). Internally runs `Tick()` in a goroutine that feeds the buffered channel. The single internal goroutine is the library's responsibility; the client's goroutine is the select loop.

```go
// Heartbeat case: interleave heartbeat with event processing
events := watch.Channel(16)
for {
    hbCtx, hbCancel := context.WithTimeout(ctx, 30*time.Second)
    select {
    case <-hbCtx.Done():
        hbCancel()
        ka.Heartbeat(ctx, position)
    case event, ok := <-events:
        hbCancel()
        if !ok { return watch.Err() }
        process(event)
        position = event.ID
    }
}
```

The three APIs — `Tick()`, `Events()`, `Channel()` — share the same underlying `WatchInternal` pump. `Tick()` is frozen for backward compatibility. `Events()` is a convenience wrapper. `Channel()` is for select-loop integration. Only one should be used per Watch instance.

Alternative considered: make the iterator heartbeat-aware (yield heartbeat ticks alongside events). Rejected because an iterator cannot enforce timing — if the consumer is slow, the heartbeat deadline passes while the iterator is blocked on `Tick()`. The goroutine model is required for scheduling.

**Decision: Lock assertion on Submit via explicit Option**

Consumers opt into a lock check on Submit requests by passing a `Lock` option directly to each call. The lock is verified **on every call** — not just at creation. Each verification is a single `GetLock` DB round-trip (`held_until > NOW()` check), which is cheap.

If the lock is not held or has expired (`held_until < NOW()`), the request is rejected.

This prevents duplicate processing: the consumer only reads/writes when it holds the lock. Without the assertion, existing behaviour is preserved (backward compatible).

**Watch is lock-unaware.** The client manages lock lifecycle entirely via the heartbeat loop (`KeepAlive.Heartbeat`). When the lock expires, the heartbeat returns an error, and the client closes the Watch. The server does not enforce a lock assertion on Watch re-queries. This simplifies the Watch implementation (no lock plumbing in the event production path) and gives the client full control over lock lifecycle. If the heartbeat loop is broken (client deadlock), the Watch continues processing events — this is a client bug, and the operator is expected to detect it via monitoring (the heartbeat metric stops incrementing).

Alternative considered: lock assertion on Watch re-queries. Rejected because it adds complexity (lock option on Watch, re-query transaction, error handling) for a scenario that is already a client bug. The heartbeat loop is the primary lock lifecycle manager; the Watch is a pure event producer.

In the proto, `SubmitIn` gains an optional `Lock lock` field. The gRPC adapter populates this from the `...Option` parameter. The server enforces it.

**Decision: Lock carries consumer + holder only**

The `Lock` struct (and proto `Lock` message) contains `consumer` and `holder`. Domain/stream come from the parent `QueryIn`/`SubmitIn` message. This avoids redundancy and matches the existing message structure.

**Decision: KeepAlive proto uses enum for lock status reason**

The `LockStatus.reason` field uses a proto enum (`LockStatusReason`) rather than a string. Values: `UNSPECIFIED`, `RENEWED`, `EXPIRED`, `STOLEN`, `CONFLICT`. This provides type safety and efficiency over string comparison. The server does not distinguish between on-time and late renewals — it only checks `held_until > NOW()`. The client tracks its own timing relative to `guarantee_until`.

When `reason = CONFLICT`, the `LockStatus` message includes `target_version` (the event ID the client attempted to write) and `current_version` (the event ID currently in the database). This enables the client/operator to diagnose the conflict.

**Decision: ReleaseAck is minimal, ReleaseRequest is empty**

The `KeepAlive` bidirectional stream uses two oneofs:
- Client→server: `Heartbeat` | `ReleaseRequest` (empty message — holder identity is implicit from the stream context; additional fields deferred to future versions if needed)
- Server→client: `LockStatus` | `ReleaseAck` (contains only `bool ok` — final position is omitted; the client knows its own position from the last heartbeat, and can call `GetPosition` if it needs the server's view)

**Stream-to-holder association:** The first `Heartbeat` message in the stream carries the holder identity. The server validates it against the lock row (`holder` column) and associates the stream with that holder for the session. Subsequent messages are bound to that holder — the server does not re-validate holder on each message (the stream is already authenticated). If the first Heartbeat's holder does not match the lock holder, the server returns `STOLEN` and the stream should be closed by the client.

Alternative considered: include final position in `ReleaseAck`. Rejected because the client already knows its position and the server processed the last heartbeat before the release (messages are ordered in the stream).

## Observability

This is the first use of OTel metrics in the PGCQRS codebase. Tracing follows existing patterns (package-level tracer, manual spans).

### Tracing

Spans are created using the package-level tracer in `internal/service/storage/otel.go`. Each significant operation gets its own span:

```
otelgrpc (automatic, one span per RPC)
  └─ grpcConsumerLock.TryAcquire
       └─ consumerStore.TryAcquire

  └─ grpcConsumerLock.KeepAlive (stream span)
       ├─ Heartbeat (per-message span)
       │    └─ consumerStore.HeartbeatWithPosition
       │
       ├─ Release (per-message span)
       │    └─ consumerStore.Release
       │
       └─ StreamClosed (event, no child span)

   └─ grpcConsumerLock.Release
        └─ consumerStore.Release

   └─ grpcConsumerLock.ListLocks
        └─ consumerStore.ListLocks
```

**Span attributes** (on all lock-related spans):
- `consumer-lock.domain`, `consumer-lock.stream`, `consumer-lock.consumer`, `consumer-lock.holder` — the lock key
- `consumer-lock.status` — `RENEWED`, `EXPIRED`, `STOLEN`, `CONFLICT` (LockStatusReason enum)
- `consumer-lock.ttl`, `consumer-lock.guarantee_until`, `consumer-lock.held_until` — timing
- `consumer-lock.position` — on heartbeat spans
- `consumer-lock.target_version`, `consumer-lock.current_version` — on conflict spans

**Span events** (for significant moments):
- `heartbeat.renewed`, `heartbeat.expired`, `heartbeat.conflict`
- `stream.closed_without_release` (also emits `consumer_lock.stream.closed_without_release` counter), `stream.release_received`

### Metrics

This is the first use of `go.opentelemetry.io/otel/metric` in the codebase. All metrics are in `internal/service/storage/otel.go` alongside the existing tracer.

**Label hierarchy** (reflects PGCQRS consistency model — order guaranteed within a stream):
```
Domain (top-level partition)
  └─ Stream (belongs to domain)
       └─ Consumer (processes a stream)
```

Labels on all metrics: `consumer-lock.domain`, `consumer-lock.stream`, `consumer-lock.consumer`

**Counters:**

| Metric | Description |
|--------|-------------|
| `consumer_lock.acquire.attempts` | TryAcquire calls |
| `consumer_lock.acquire.successes` | Locks successfully acquired |
| `consumer_lock.acquire.failures` | Locks not acquired (held by another) |
| `consumer_lock.release.explicit` | Explicit Release calls (unary + in-stream) |
| `consumer_lock.heartbeat.processed` | Heartbeats processed |
| `consumer_lock.heartbeat.conflict` | Heartbeats rejected due to stale position (backward guard) |
| `consumer_lock.expiry.ignored` | Locks treated as expired on access (ignored by GetLock, overwritten on TryAcquire) |
| `consumer_lock.stream.closed_without_release` | KeepAlive streams closed without Release message |
| `consumer_lock.assertion.checks` | Lock assertion checks on queries/submits |
| `consumer_lock.assertion.rejections` | Requests rejected (lock not held) |
| `consumer_lock.cleanup.deleted` | Expired lock rows removed during TryAcquire cleanup |

**Gauges:**

| Metric | Description |
|--------|-------------|
| `consumer_lock.active` | Currently held locks (increment on acquire, decrement on release/expiry) |

**Histograms:**

| Metric | Description |
|--------|-------------|
| `consumer_lock.hold_duration.seconds` | Time between acquire and release/expiry |
| `consumer_lock.heartbeat.interval.seconds` | Time between heartbeats |

### In-memory transport and observability

- **Traces**: Yes — the in-memory transport creates spans using the package-level tracer. OTel tracer is no-op if not configured, so zero cost in tests.
- **Metrics**: No — the in-memory transport does not emit metrics. Metrics are an operational concern, not a testing concern. Unit tests verify lock semantics, not performance.

## Risks / Trade-offs

- [Risk] Stale lock holder continues processing after lock expires → Mitigation: client-side context deadline stops processing before guarantee period ends; lock option on queries rejects requests if lock expired (`held_until < NOW()`)
- [Risk] Clock skew between clients and server → Mitigation: advisory period absorbs up to 3s of drift client-side; operational requirement mandates NTP or equivalent sync
- [Risk] Very aggressive heartbeat intervals (< 6s) cause write contention on `consumer_locks` → Mitigation: server rejects `TryAcquire` with TTL < 6s; default heartbeat is 30s; each consumer has its own row (primary key = consumer+domain+stream), no row-level contention between different consumers
- [Risk] Stream closure without auto-release means crash failover is slow (full TTL) → Mitigation: acceptable trade-off to avoid double-processing; clients that want faster failover should implement application-level health checks
- [Risk] ConsumerStore consolidation increases surface area of single struct → Mitigation: both lock and position operations are consumer-related; the consolidation makes atomic heartbeat+position updates natural via single transaction
- [Risk] `unsafeStore` does not accept `pgx.Tx`, making atomic lock+submit transactions require refactoring → Mitigation: Event write extracted into `unsafeStoreWith(ctx, q queryExecer, ...)` — both `*pgxpool.Pool` and `*pgx.Tx` implement the custom `queryExecer` interface, so a single method works for both the fast path (pool direct, no transaction) and the lock-checked path (tx with begin/commit). No type assertions, no two-method split, no transaction overhead on the non-lock path.
- [Risk] Lock-checked Submit executes 5 queries (begin, lock check, kind upsert, event insert, commit) versus 2 without lock → Mitigation: accepted cost for atomicity guarantee. Future optimization: combine lock check and event insert into a single CTE statement (reduces to 3 queries). Deferred — current approach prioritizes clear error handling.
