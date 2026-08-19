## Context

The pgcqrs project provides JSON event storage with multi-tenancy and query-by-structure. Consumer position tracking and consumer locks exist as separate capabilities, but there is no reusable foundation for building materialized views from event streams.

Multiple consumers need to materialize entity state:
- **Webapps** need current entity state with version-aware consistency (staleness detection, wait-for-version)
- **Semantic indexer** needs materialized entity state for generating embeddings, with deletion handling
- **Inventory system** needs entity indexes with cascading deletes across related indexes
- **Gift exchange** needs entity mutation tracking over time

These consumers share a common event processing loop: acquire lock, resume from position, watch events, dispatch to handlers, store results, heartbeat, notify observers. The core indexer framework (`pkg/indexer/`) extracts this shared loop. Specialized indexer types (`pkg/indexer/views/`, `pkg/indexer/embedding/`) plug into it with their own handlers and storage.

Developer experience is a first-class concern — the API is designed for fast prototyping with typed handlers and minimal boilerplate, while supporting production use with consumer locks, resumable processing, and gRPC remote access.

## Goals / Non-Goals

**Goals:**
- Core indexer framework: shared Pump loop with `Indexer` interface (one method, no generics)
- Pump runs in developer's process, connects to pgcqrs via Wire (gRPC or memory)
- `query2.Watch.TickWithID` exposes event ID for heartbeating without dropping to WatchInternal
- Typed per-kind handlers: `OnKind[T]` registers a handler for a specific event kind with automatic JSON unmarshaling
- Previous-state retrieval: handlers can read current entity state via `ReduceContext.Get`
- Unified Get with version constraints: single method returning `(entity, result, error)` with `After` and `UntilVersion` options
- Version-aware consistency: `Result.Status` explicitly indicates whether version constraints were met
- `ViewProjectionStore` gRPC service: narrow KV storage service (ApplyMutations, GetEntity) used by the Pump
- `ViewProjectionConsumer` gRPC service: read API (GetEntity, Version, WatchChanges) for webapps and downstream indexers
- Storage in pgcqrs database for durability
- Resumable projections via After(position)
- Synchronous execution model for consumer lock coordination
- Client framework (`views.With`) transparently handles connection and lifecycle
- Testable with in-memory transport and store
- Composition: embedding indexer reads views store for projected entity state
- Comprehensive documentation: godoc, examples, README

**Non-Goals:**
- Not a replacement for the existing query system
- No HTTP endpoints (uses existing Transport interface)
- No web UI or dashboard
- No dynamic query changes (event kinds are static, defined at projection creation)
- No version history (event ID is version indicator, not historical record)

## Decisions

**Decision: Core indexer framework at `pkg/indexer/`**

Both view projections and embedding indexers share the same event processing loop: acquire lock, resume from position, watch events, dispatch to handlers, store results, heartbeat, notify observers. The core framework extracts this shared loop into a reusable Pump.

```
pkg/indexer/
├── pump.go       # Pump struct, Run() loop
├── indexer.go    # Indexer interface (1 method)
├── wire.go       # Wire interface, GrpcWire, MemoryWire adapters
├── lock.go       # Lock interface (Heartbeat, Release)
└── options.go    # TTL, heartbeat margin, etc.
```

Specialized indexer types live in sub-packages (`pkg/indexer/views/`, `pkg/indexer/embedding/`) and implement the `Indexer` interface.

**Decision: Indexer interface — one method, no generics**

```go
type Indexer interface {
    Query() *query2.Query
}
```

Each indexer builds a `query2.Query` with handlers registered via `OnKind` or equivalent. The Pump drives the query's Watch loop. No generics, no `Dispatch`, no proto coupling in the core.

The views indexer builds its query from `OnKind[T]` registrations. The embedding indexer builds its query from the plugin's event interests. Both plug into the same Pump.

**Decision: Pump runs in developer's process**

The Pump runs in the developer's binary, not in the pgcqrs service process. It connects to pgcqrs via the Wire interface (gRPC for production, memory transport for tests). Handlers execute locally in the developer's process.

```
  Developer's Process                        pgcqrs Service
  ┌──────────────────────────────┐          ┌──────────────────────────┐
  │                              │          │                          │
  │  Pump.Run():                 │          │  Existing Services:      │
  │  ┌────────────────────────┐  │          │  ├─ Command              │
  │  │ 1. Wire.TryAcquire ────│──│─────────►│  ├─ Query                │
  │  │ 2. Wire.GetPosition ───│──│─────────►│  ├─ ConsumerLock         │
  │  │ 3. indexer.Query()     │  │          │  └─ ConsumerPosition     │
  │  │ 4. query.Watch(ctx)    │  │          │                          │
  │  │ 5. TickWithID() ◄──────│──│──────────│  KV Services:            │
  │  │ 6. handler dispatched  │  │          │  ├─ ViewProjectionStore  │
  │  │    ├─ actx.Get() ──────│──│─────────►│  │  ├─ ApplyMutations    │
  │  │    │  ◄── prev state ──│──│──────────│  │  └─ GetEntity         │
   │  │    └─ store.Persist() ──│──│─────────►│  │                       │
  │  │ 7. Wire.Heartbeat ─────│──│─────────►│  └─ ViewProjectionConsumer│
  │  │ 8. notifier.Notify     │  │ (local)  │     ├─ GetEntity         │
  │  │ 9. goto 5              │  │          │     ├─ Version           │
  │  └────────────────────────┘  │          │     └─ WatchChanges      │
  └──────────────────────────────┘          └──────────────────────────┘
```

This eliminates the need for a bidi gRPC stream between pgcqrs and the developer's process. The pgcqrs service provides narrow KV storage and read services; the intelligence lives in the developer's process.

**Decision: Pump loop is minimal**

```
lock → position → indexer.Query() → query.Watch → TickWithID → heartbeat → loop
```

The Pump doesn't know about stores, notifiers, kinds, or query semantics. Each indexer type encapsulates its own store and notification internally within `Query()`. The Pump only knows: `TickWithID` returned an event ID → heartbeat with it. Error → stop.

**Decision: query2.Watch.TickWithID — expose event ID for heartbeating**

The existing `query2.Watch.Tick(ctx)` dispatches to handlers internally but swallows the event ID. The Pump needs the event ID for heartbeating after successful dispatch.

```go
// New method on query2.Watch
func (w *Watch) TickWithID(ctx context.Context) (int64, error)
```

Returns the event ID alongside the existing Tick dispatch behavior. Internally, `Tick` already has `m.Id` from `WatchInternal.Tick()` — it just doesn't return it. This is a minimal addition that lets the Pump stay at the `query2.Watch` level without dropping to `WatchInternal`.

The Pump uses `TickWithID` in its loop:

```go
func (p *Pump) Run(ctx context.Context) error {
    // ... acquire lock, get position ...
    query := p.indexer.Query()
    // ... set AfterID on query ...
    watch := query.Watch(ctx)
    for {
        eventID, err := watch.TickWithID(ctx)
        if err != nil { return err }
        if err := p.keepAlive.Heartbeat(ctx, eventID); err != nil {
            return err
        }
    }
}
```

**Decision: Wire — non-generic interface for pgcqrs connection**

The Pump connects to pgcqrs via the `Wire` interface. No generics — `NewKeepAlive` returns the `Lock` interface directly:

```go
type Wire interface {
    Watch(ctx context.Context, query *ipc.QueryIn) (v1.WatchInternal, error)
    TryAcquire(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (*v1.LockResult, error)
    NewKeepAlive(ctx context.Context, domain, stream, consumer, holder string) (Lock, error)
    GetPosition(ctx context.Context, domain, stream, consumer string) (int64, bool, error)
}

type Lock interface {
    Heartbeat(ctx context.Context, position int64) error
    Release(ctx context.Context) error
}
```

Neither `v1.GrpcAdapter` nor `v1.MemoryTransport` directly satisfy `Wire` — `GrpcAdapter` has `NewKeepAlive` but also has ~15 other public methods we don't want leaking into Pump; `MemoryTransport` lacks `NewKeepAlive` entirely. Two thin adapter types bridge the gap:

```go
type grpcWire struct{ *v1.GrpcAdapter }

func GrpcWire(conn *grpc.ClientConn) Wire {
    return &grpcWire{v1.NewGrpcAdapter(conn)}
}

// Watch, TryAcquire, GetPosition — promoted from GrpcAdapter via embedding.
// Only NewKeepAlive needs an explicit method (translates KeepAlive bidi stream → Lock):
func (g *grpcWire) NewKeepAlive(ctx context.Context, domain, stream, consumer, holder string) (Lock, error) {
    return g.GrpcAdapter.NewKeepAlive(ctx, domain, stream, consumer, holder)
}
```

Note: `v1.NewGrpcAdapter(conn)` is a new constructor that wraps an existing `*grpc.ClientConn` into a `GrpcAdapter`. The existing `NewGRPCTransport(url)` dials internally; this variant accepts a pre-dialed connection so `Connect` can share one conn for both Wire and Transport.

```go
type memoryWire struct{ v1.Transport }

func MemoryWire(t v1.Transport) Wire {
    return &memoryWire{t}
}

func (m *memoryWire) NewKeepAlive(ctx context.Context, domain, stream, consumer, holder string) (Lock, error) {
    return &memoryLock{
        transport: m.Transport,
        domain: domain, stream: stream,
        consumer: consumer, holder: holder,
    }, nil
}
```

`memoryLock` is a small type satisfying `Lock` by delegating to `Transport.HeartbeatWithPosition` and `Transport.Release`.

gRPC's `KeepAlive` bidi stream satisfies `Lock`. The `KeepAlive` struct already has `Heartbeat(ctx, position int64) error` and `Release(ctx) error`. `memoryLock` is built the same way over `MemoryTransport`.

**Decision: Store and notification are indexer-internal**

The Pump doesn't know about stores or notifiers. Each indexer type encapsulates these within its `Query()` implementation. The views indexer's handlers call `store.Persist()` and `notifier.Notify()` internally. The embedding indexer's handlers call `vectorStore.Upsert()` internally.

This keeps the core Pump minimal and lets each indexer type own its storage and notification story.

**Decision: Notifier is local, one per projection, owned by Connect**

The `Notifier` is an in-process callback registry that fires when mutations are applied. `With` creates one `Notifier` per projection, wire it to both the `ViewsIndexer` (for firing) and the `Client` (for subscribing via `OnChange` and `UntilVersion`). The Notifier lives for the lifetime of the `Client` — `Client.Close()` stops the Pump, which stops firing. Remote consumers (webapps) use `ViewProjectionConsumer.WatchChanges` gRPC instead of the local Notifier.

**Decision: OnKind[T] — typed per-kind handler registration**

Each event kind maps to a handler function with automatic JSON unmarshaling. This eliminates manual type switching and unmarshaling, and matches the pattern learned from query1→query2 migration where kind is coupled with type parsing and specific handling.

```go
proj := views.NewProjection("inventory", "items").
    OnKind("ItemCreated", func(ctx context.Context, e v1.Envelope, evt *ItemCreated, actx *views.ReduceContext) (*views.ReduceResult, error) {
        return &views.ReduceResult{
            Upserts: []views.Upsert{
                {Kind: "items", Key: views.Key(evt.ItemID), Value: evt},
            },
        }, nil
    }).
    OnKind("ItemDeleted", func(ctx context.Context, e v1.Envelope, evt *ItemDeleted, actx *views.ReduceContext) (*views.ReduceResult, error) {
        return &views.ReduceResult{
            Deletes: []views.Delete{
                {Kind: "items", Key: views.Key(evt.ItemID)},
            },
        }, nil
    })
```

The framework builds the query internally from the registered kinds. Developers declare what they handle; the framework handles the plumbing.

**Decision: ReduceContext — struct for handler dependencies**

Handlers receive an `ReduceContext` struct rather than a bare function. This provides previous-state retrieval and room for future expansion without breaking handler signatures.

```go
type ReduceContext struct {
    get func(kind string, key Key) (*Entity, error)  // unexported
}

func (ac *ReduceContext) Get(kind string, key Key) (*Entity, error) {
    return ac.get(kind, key)
}
```

The handler signature:

```go
func(ctx context.Context, e v1.Envelope, evt *T, actx *views.ReduceContext) (*ReduceResult, error)
```

The `ReduceContext.Get` calls through to the views Store's read path, which uses the `ViewProjectionStore.GetEntity` gRPC RPC to read from pgcqrs. This enables handlers to read previous entity state during processing.

Future expansion possibilities for `ReduceContext`: `Version()` (current projection version), `EventID()` (shortcut for `e.ID`). The unexported `get` field keeps internals hidden while the public API grows organically.

**Decision: Apply receives event by value, typed event by pointer**

The `Envelope` is passed by value (three small fields: ID, When, Kind — no need for pointer indirection). The typed event `*T` is passed as a pointer (required for JSON unmarshaling into the struct).

```go
Reduce(ctx context.Context, event Envelope, raw json.RawMessage, actx *ReduceContext) (*ReduceResult, error)
```

**Decision: Key is variadic with panic on overflow**

The `Key` type supports 1 or 2 string parts. Simple entities use a single key; composite entities use two. Panics if more than 2 parts are provided — this is a programmer error, not a runtime condition.

```go
func Key(parts ...string) Key  // panics if len(parts) > 2
func (k Key) Parts() []string  // returns 1 or 2 elements
```

Usage:
```go
views.Key(itemID)              // single key → routes to entries_single table
views.Key(itemID, locationID)  // composite key → routes to entries_composite table
```

`Key.Parts()` provides the routing information for the Store: `len(parts) == 1` targets `view_projection_entries_single`, `len(parts) == 2` targets `view_projection_entries_composite`. This eliminates sentinel values and keeps schemas clean.

**Decision: Upsert.Value is `any` — framework marshals internally**

The `Upsert` type accepts `any` for Value, and the framework marshals to JSON when storing. This keeps handlers clean — they pass typed structs directly without manual `json.Marshal`.

```go
type Upsert struct {
    Kind  string
    Key   Key
    Value any  // framework marshals to JSON internally
}
```

The handler passes the typed event:
```go
views.Upsert{Kind: "items", Key: views.Key(evt.ItemID), Value: evt}
```

There is a marshaling cost (once per event per projection), but the ergonomic win is significant. Users with pre-marshaled `json.RawMessage` can pass it directly — `json.Marshal(json.RawMessage(...))` is a no-op copy.

**Decision: ViewProjection.Reduce is a functional transformation**

The `ViewProjection` interface has a single method `Reduce` that transforms events into mutations. The `ReduceContext` provides read access to previous entity state (via a gRPC round-trip to ViewProjectionStore). The handler itself is a deterministic function — given the same event and entity state, it produces the same mutations — but `ReduceContext.Get` performs I/O to read that state.

```go
type ViewProjection interface {
    Reduce(ctx context.Context, event Envelope, raw json.RawMessage, actx *ReduceContext) (*ReduceResult, error)
}
```

This makes ViewProjections easy to test: give it an event, get back an `ReduceResult`. Assert on the upserts and deletes. The `ReduceContext` can be constructed with a mock `Get` function for testing handlers that read previous state.

**Decision: ReduceResult separates upserts and deletions**

Each event produces zero or more mutations. Upserts and deletes are separated for efficient processing:

```go
type ReduceResult struct {
    Upserts []Upsert
    Deletes []Delete
}

type Upsert struct {
    Kind  string
    Key   Key
    Value any
}

type Delete struct {
    Kind string
    Key  Key
}
```

The framework marshals `Upsert.Value` to JSON when storing. The `Key` type handles both single and composite keys transparently.

**Decision: One ViewProjection produces many entity kinds**

A single ViewProjection can produce multiple entity kinds (one-for-many pattern). This simplifies client coordination because related entity types are managed by the same projection. For example, an inventory projection might produce both "items" and "indexes" entity kinds.

**Decision: Event ID is version indicator**

The event ID serves as the version for entity state. This enables:
- Staleness awareness: client declares "I last saw version X", server responds with current version
- Consistent world views: snapshot at specific version across multiple entities
- Resumable projections: After(position) resumes from last processed event

No version history is stored — the event ID is a synchronization signal, not a historical record.

**Decision: Synchronous execution model**

`Apply` is called synchronously by the framework. This integrates cleanly with consumer locks: if `Apply` hangs, the heartbeat stops, the lock expires, and another instance can take over. The synchronous model provides clear backpressure and failure detection.

**Decision: Apply errors stop the projection**

When `Apply` returns an error, the projection stops and the Pump returns the error. Missing an event is a problem — the ViewProjection builds a comprehension over time, and skipping events would produce incorrect state. The caller decides how to handle the failure (retry, log, crash). Position tracking ensures resume from the last successfully processed event on restart.

**Decision: Consumer lock for exclusive access**

Each ViewProjection acquires a consumer lock on its {domain, stream} partition. This ensures exclusive access and prevents duplicate processing across instances. The lock lifecycle is managed by the Pump using the Wire interface (TryAcquire, NewKeepAlive, Heartbeat, Release).

**Decision: Position tracking via HeartbeatWithPosition**

The Pump tracks the last processed event ID via `HeartbeatWithPosition` through the Wire's KeepAlive mechanism, keeping position updates in the existing signaling machinery. On startup, it resumes from the stored position using `After(position)`. This provides at-least-once processing semantics — if the process crashes between applying mutations and updating the position, the event is reprocessed on restart. Mutations are idempotent (upserts replace, deletes are idempotent), so reprocessing is safe.

**Decision: Heartbeat before observer notification**

The event processing order within the views indexer's handlers:

1. Apply event → mutations (OnKind[T] handler)
2. Store mutations (atomic commit via Store.Persist → ViewProjectionStore gRPC)
3. Return from handler → Pump heartbeats position
4. Notifier notifies observers (best-effort, local)

Observer notification is best-effort: observers are typically other processes (webapps via gRPC WatchChanges, semantic indexers, etc.), and we have no delivery guarantee regardless of ordering. By heartbeating first, we avoid reprocessing events that observers may or may not have received.

**Decision: Projection name defaults to consumer name**

The projection name doubles as the consumer name for position tracking and lock coordination. This simplifies setup — one name serves both purposes. An optional `ConsumerName` override is available for migration scenarios where the projection name and consumer name need to differ.

```go
proj := views.NewProjection("inventory", "items")  // consumer name = "inventory"
proj := views.NewProjection("inventory", "items",
    views.ConsumerName("inventory-v2"),  // explicit override
)
```

**Decision: ViewProjectionStore — narrow KV storage gRPC service**

The `ViewProjectionStore` gRPC service provides the write and internal-read path for view projections. It is a narrow KV storage service — not a pump orchestrator. The Pump runs in the developer's process and calls this service for mutations and previous-state reads.

```protobuf
service ViewProjectionStore {
    rpc ApplyMutations(ApplyMutationsRequest) returns (ApplyMutationsResponse);
    rpc GetEntity(GetEntityRequest) returns (GetEntityResponse);
}
```

- `ApplyMutations`: receives an `ReduceResult` (upserts + deletes), applies atomically in a single transaction, returns a `Change` describing what was written
- `GetEntity`: reads a single entity by kind + key, used by `ReduceContext.Get` during handler execution

Both RPCs operate against the pgcqrs database tables (`view_projection_entries_single`, `view_projection_entries_composite`).

**Decision: ViewProjectionConsumer — read API for external clients**

The `ViewProjectionConsumer` gRPC service provides the external read path for webapps, CLIs, and downstream indexers. It reads from the same pgcqrs database tables.

```protobuf
service ViewProjectionConsumer {
    rpc GetEntity(GetEntityRequest) returns (GetEntityResponse);
    rpc Version(VersionRequest) returns (VersionResponse);
    rpc WatchChanges(WatchChangesRequest) returns (stream WatchChangesResponse);
}
```

- `GetEntity`: reads entity state with optional version constraints (After, UntilVersion)
- `Version`: returns the projection version (from `consumer_positions`)
- `WatchChanges`: streams change notifications (subscribes to in-process bus)

**Decision: Both gRPC services register on existing server, no new config**

The `ViewProjectionStore` and `ViewProjectionConsumer` services register on the existing `grpc.Server` in `grpcPort.Serve()`, alongside Command, Query, ConsumerLock, and ConsumerPosition. They share the same `pgxpool`, `bus`, and `consumerStore`. No new configuration fields are needed.

```go
// In grpcPort.Serve():
views.RegisterViewProjectionStoreServer(service, &viewsStoreHandler{
    store: views.NewPGStore(db),
})
views.RegisterViewProjectionConsumerServer(service, &viewsConsumerHandler{
    store:     views.NewPGStore(db),
    bus:       bus,
    positions: consumerStore,
})
```

**Decision: Get returns three values — entity, result, error**

The unified `Get` method returns `(entity, result, error)`. The `result` contains version constraint status. Developers discard what they don't need:

```go
// Simple case — discard result
entity, _, err := proj.Get(ctx, "items", views.Key("item-42"))

// Version-aware case — check result
entity, result, err := proj.Get(ctx, "items", views.Key("item-42"), views.UntilVersion(500, 200*time.Millisecond))
if result.Status != views.StatusOK {
    // version constraint not met
}
```

The `err` is for transport/store errors. The `result.Status` is for version constraint status. This separation is explicit and testable.

**Decision: Version constraint options — After and UntilVersion**

Two options control version-aware behavior:

```go
func After(version int64) GetOption           // fail immediately if stale
func UntilVersion(version int64, timeout time.Duration) GetOption  // wait up to timeout for version
```

`After(v)` is consistent with query2's `After(id)` terminology. `UntilVersion(v, timeout)` waits for the projection to reach version v, bailing after the timeout. The timeout is practical — ~200ms for webapps.

**Decision: UntilVersion uses notification, not polling**

`UntilVersion` waits for the projection to reach a target version. Rather than polling `consumer_positions` every 10ms (up to 20 database queries per call), the Client subscribes to the local Notifier and wakes on each change event. The `Change` type already carries `Version int64` (the event ID), so the Client checks the version on each notification and returns when the target is reached or the timeout expires.

```
Client.Get(ctx, "items", key, UntilVersion(500, 200ms))
  1. Read current entity from ViewProjectionConsumer gRPC
  2. Check: current projection version ≥ 500? → return OK
  3. Subscribe to Notifier channel
  4. Wait: Notifier fires → check version ≥ 500? → return OK
  5. Timeout → return entity, StatusTimeout
```

Zero extra database queries. The Notifier is local to the developer's process, so delivery is a channel receive — not a network call. The Notifier fires after `Store.Persist` commits mutations and the Pump heartbeats, so the version in the `Change` event reflects the latest processed event.

**Decision: Result struct for version constraint feedback**

```go
type Result struct {
    Entity  *Entity
    Version int64   // version retrieved
    Status  Status  // what happened
}

type Status int

const (
    StatusOK       Status = iota  // constraint satisfied
    StatusNotFound                // entity doesn't exist
    StatusStale                   // projection behind requested version (After failed)
    StatusTimeout                 // waited for version, timed out (UntilVersion failed)
)
```

This is explicit — the developer always knows whether the version constraint was met, what version was retrieved, and why it might have failed. No hidden failure modes behind error strings.

**Decision: Atomic mutations from single event**

All mutations from a single event are applied atomically in a single database transaction. This ensures consistency — consumers never see partial updates from a single event.

**Decision: Storage in pgcqrs database with normalized text columns**

Projected entity state is stored in pgcqrs. Projection names are scoped to a specific stream via `stream_id` FK to `events_stream`. Entity kinds are normalized into an enumeration table. Projection version is derived from `consumer_positions` — the projection name is the consumer name, and `consumer_positions.event_id` is the version.

```sql
CREATE TABLE view_projection_names (
    id        BIGSERIAL PRIMARY KEY,
    stream_id BIGINT NOT NULL REFERENCES events_stream(id),
    name      TEXT NOT NULL,
    CONSTRAINT view_projection_names_stream_id_name_unique UNIQUE (stream_id, name)
);

CREATE TABLE view_projection_kinds (
    id   BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL UNIQUE
);

CREATE TABLE view_projection_entries_single (
    id            BIGSERIAL PRIMARY KEY,
    projection_id BIGINT NOT NULL REFERENCES view_projection_names(id),
    kind_id       BIGINT NOT NULL REFERENCES view_projection_kinds(id),
    key           TEXT NOT NULL,
    value         JSONB NOT NULL,
    version       BIGINT NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(projection_id, kind_id, key)
);

CREATE TABLE view_projection_entries_composite (
    id            BIGSERIAL PRIMARY KEY,
    projection_id BIGINT NOT NULL REFERENCES view_projection_names(id),
    kind_id       BIGINT NOT NULL REFERENCES view_projection_kinds(id),
    key1          TEXT NOT NULL,
    key2          TEXT NOT NULL,
    value         JSONB NOT NULL,
    version       BIGINT NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(projection_id, kind_id, key1, key2)
);
```

Two levels of version exist:
- **Entity version** (`view_projection_entries_single.version` / `view_projection_entries_composite.version`): when this specific entity was last mutated. Used by `After(v)` constraint on `Get()`.
- **Projection version** (`consumer_positions.event_id`): where the projection is in the stream. Used by `Version()`, `UntilVersion()`, and resume on restart.

`view_projection_names` is scoped to `{domain, stream}` via the `stream_id` FK. This ensures that two projections with the same name on different streams get separate KV storage. The `ProjectionIdentity` type carries the `{domain, stream, projection}` triple through the API. `view_projection_names` is a separate table from `consumer_names`, not a reference. Although the projection name defaults to the consumer name, they are not always identical — the `ConsumerName` option allows them to diverge for migration scenarios. More importantly, `consumer_names` will grow beyond view projections as other consumer types use the same infrastructure. Keeping the tables separate means `view_projection_entries_*` references only projection-relevant names, yielding tighter indexes and less data to scan on entity queries.

**Decision: Store interface in views package**

The Store interface handles entity storage and retrieval. It lives in the `views` package (Go-idiomatic). Implementations:
- `views.NewMemoryStore()` — in-memory, for unit testing
- `views.NewRemoteStore(grpcConn)` — backed by ViewProjectionStore gRPC, for production

The Store is bound to a single projection at construction time. Version queries go through the `Client` type, which derives version from `consumer_positions` via the ViewProjectionConsumer gRPC service. The Store routes to `view_projection_entries_single` or `view_projection_entries_composite` based on `Key.Parts()` length.

```go
type Store interface {
    Get(ctx context.Context, kind string, key Key) (*Entity, error)
    Persist(ctx context.Context, result *ReduceResult, eventID int64) (*Change, error)
}
```

The `Get` method provides the read path (used by `ReduceContext.Get` and by other indexers for composition). The `Persist` method provides the write path (used by the views indexer's handlers during event processing).

**Decision: Persist always advances the projection version**

Every processed event advances the projection version, even when the handler returns no mutations (`nil` result). `Persist` treats a `nil` result as an empty result: it applies zero mutations but still records the position. `PGStore.Persist` writes the `consumer_positions` row in the same transaction as the mutations (guarded so a stale event ID never regresses the position), keeping entity mutations and the projection version atomic. The in-process `Notifier` also fires for empty changes, so observers wake up with the new version even when no entity changed. This guarantees `Version()`/`UntilVersion()` reflect how far the projection has consumed the stream, independent of whether the events mutated state.

**Decision: Composition — indexers read each other's stores**

The views Store's `Get` method is available to any indexer that needs projected entity state. The embedding indexer holds a `views.Store` (or just the `Get` reader) to access projected state for generating embeddings:

```go
type EmbeddingIndexer struct {
    plugin    Plugin
    store     VectorStore
    reader    views.Store  // reads projected state from views
}

// In the embedding handler:
func (e *EmbeddingIndexer) handleEvent(ctx context.Context, event v1.Envelope) ([]EmbedAction, error) {
    entity, err := e.reader.Get(ctx, "items", views.Key(itemID))
    // use entity state to generate embedding
}
```

This enables indexer composition without the core Pump knowing about cross-indexer dependencies. Each indexer type manages its own read dependencies internally.

**Decision: Streaming API uses Change type with batched upserts/deletes**

The streaming API yields `Change` objects, which contain batched upserts and deletes from a single event:

```go
type Change struct {
    Upserts []Upsert
    Deletes []Delete
    Version int64
}
```

This matches the gRPC `WatchChangesResponse` proto and enables efficient batch processing. Consumers can process all upserts, then all deletes, without checking a boolean flag per mutation.

**Decision: Single {domain, stream} per ViewProjection**

Each ViewProjection consumes from exactly one {domain, stream} pair. This simplifies the Indexer and matches the consumer lock granularity. If a projection needs events from multiple streams, create multiple projections.

**Decision: Documentation as first-class concern**

All public APIs must be documented with:
- Godoc comments on all public types and methods
- Example programs showing common use cases
- README for the views package with overview, quick start, and API reference

The product is both (a) a set of services and (b) a set of APIs for interacting with those services. Developer experience is a first-class concern.

**Decision: EnsureStream before Pump starts**

`With` calls `EnsureStream` during setup, before building the `*v1.Stream` for the ViewsIndexer. This guarantees the domain and stream exist before Watch is called, preventing confusing errors. The Pump itself does not call `EnsureStream` — it operates through Wire, which has no EnsureStream method.

**Decision: Indexer unmarshals raw JSON into typed events for OnKind[T]**

The query2 Watch mechanism delivers `v1.Envelope` plus `json.RawMessage`. The views indexer's internal dispatch function unmarshals `rawJSON` into `*T` before calling the `OnKind[T]` handler. This follows the existing `EntityFuncE` pattern at `pkg/v1/queryStream.go:74-82`. Handlers receive typed events; the framework handles the plumbing.

**Decision: Entity.Version is per-entity, Projection version is global**

Two distinct version concepts exist:
- **Entity version** (`view_projection_entries_single.version` / `view_projection_entries_composite.version`): when this specific entity was last mutated. Used by `After(v)` constraint on `Get()`.
- **Projection version** (`consumer_positions.event_id`): where the projection is in the stream. Used by `Version()`, `UntilVersion()`, and resume on restart.

These are kept separate in documentation and code. `Store.Get` returns `Entity` with the per-entity version. `Client.Version()` reads from `consumer_positions` via the ViewProjectionConsumer gRPC service.

(See also: "Storage in pgcqrs database" above for the schema-level view of these two versions.)

**Decision: Package layout**

```
pkg/indexer/
├── pump.go              # Pump struct, Run() loop
├── indexer.go           # Indexer interface: Query() *query2.Query
├── wire.go              # Wire interface, GrpcWire, MemoryWire adapters
├── lock.go              # Lock interface, memoryLock
├── options.go           # Pump options
│
├── views/
│   ├── indexer.go       # ViewsIndexer (implements Indexer)
│   ├── projection.go    # Projection, OnKind[T], NewProjection
│   ├── handler.go       # ReduceContext, ReduceResult, Upsert, Delete
│   ├── key.go           # Key type (variadic, 1-2 parts)
│   ├── entity.go        # Entity type
│   ├── store.go         # Store interface (reader + writer)
│   ├── remote.go        # RemoteStore (gRPC → pgcqrs ViewProjectionStore)
│   ├── memory.go        # MemoryStore (for tests)
│   ├── client.go        # With, Get, Version, OnChange
│   ├── result.go        # Result, Status types
│   ├── change.go        # Change type
│   └── grpc/
│       ├── views.proto
│       └── *.pb.go
│
└── embedding/
    ├── indexer.go       # EmbeddingIndexer (implements Indexer)
    ├── plugin.go        # Plugin interface (HandleEvent, EmbedQuery)
    ├── actions.go       # EmbedAction types
    ├── store.go         # VectorStore
    ├── search.go        # gRPC search service
    └── migrations/
        └── *.sql

internal/service/
├── grpc.go              # registers ViewProjectionStore + Consumer
├── viewsStore.go        # ViewProjectionStore handler (ApplyMutations, GetEntity)
└── viewsConsumer.go     # ViewProjectionConsumer handler

migrations/
└── XXXX_view_projections.sql
```

## Schema

### Tables

```sql
-- Projection name enumeration (deduplicates projection names, scoped to stream)
CREATE TABLE view_projection_names (
    id        BIGSERIAL PRIMARY KEY,
    stream_id BIGINT NOT NULL REFERENCES events_stream(id),
    name      TEXT NOT NULL,
    CONSTRAINT view_projection_names_stream_id_name_unique UNIQUE (stream_id, name)
);

-- Entity kind enumeration (deduplicates kind names)
CREATE TABLE view_projection_kinds (
    id   BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL UNIQUE
);

-- Single-key projected entity state (the common case)
CREATE TABLE view_projection_entries_single (
    id            BIGSERIAL PRIMARY KEY,
    projection_id BIGINT NOT NULL REFERENCES view_projection_names(id),
    kind_id       BIGINT NOT NULL REFERENCES view_projection_kinds(id),
    key           TEXT NOT NULL,
    value         JSONB NOT NULL,
    version       BIGINT NOT NULL,     -- event ID that produced this state
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(projection_id, kind_id, key)
);

-- Composite-key projected entity state (two-part keys)
CREATE TABLE view_projection_entries_composite (
    id            BIGSERIAL PRIMARY KEY,
    projection_id BIGINT NOT NULL REFERENCES view_projection_names(id),
    kind_id       BIGINT NOT NULL REFERENCES view_projection_kinds(id),
    key1          TEXT NOT NULL,
    key2          TEXT NOT NULL,
    value         JSONB NOT NULL,
    version       BIGINT NOT NULL,     -- event ID that produced this state
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(projection_id, kind_id, key1, key2)
);
```

### Indexes

```sql
-- Single-key: fast lookup by projection + kind
CREATE INDEX idx_vpes_projection_kind ON view_projection_entries_single(projection_id, kind_id);

-- Composite: fast lookup by projection + kind
CREATE INDEX idx_vpec_projection_kind ON view_projection_entries_composite(projection_id, kind_id);
```

### Key Routing

The `Key` type carries routing information. At every call site, `Key.Parts()` determines which table to target:

- `len(parts) == 1` → `view_projection_entries_single`
- `len(parts) == 2` → `view_projection_entries_composite`

This eliminates sentinel values (empty string for unused key2) and keeps schemas clean. The Store implementation routes to the correct table based on key length.

### Down Migrations

Down migrations drop everything: `view_projection_entries_single`, `view_projection_entries_composite`, `view_projection_kinds`, `view_projection_names`. No data preservation on downgrade.

## Proto Definition

```protobuf
syntax = "proto3";
package views;
option go_package = "github.com/meschbach/pgcqrs/pkg/indexer/views/grpc";

// ViewProjectionStore provides KV storage for view projections.
// Used by the Pump (in the developer's process) for mutations and previous-state reads.
service ViewProjectionStore {
    rpc ApplyMutations(ApplyMutationsRequest) returns (ApplyMutationsResponse);
    rpc GetEntity(GetEntityRequest) returns (GetEntityResponse);
}

// ViewProjectionConsumer serves entity state to remote clients (webapps, downstream indexers).
service ViewProjectionConsumer {
    rpc GetEntity(GetEntityRequest) returns (GetEntityResponse);
    rpc Version(VersionRequest) returns (VersionResponse);
    rpc WatchChanges(WatchChangesRequest) returns (stream WatchChangesResponse);
}

// --- ViewProjectionStore messages ---

message ApplyMutationsRequest {
    string projection = 1;
    repeated Upsert upserts = 2;
    repeated Delete deletes = 3;
    int64 event_id = 4;
}

message ApplyMutationsResponse {
    repeated Upsert applied_upserts = 1;
    repeated Delete applied_deletes = 2;
    int64 version = 3;
}

// --- Shared messages ---

message Entity {
    string kind = 1;
    repeated string key = 2;
    bytes value = 3;
    int64 version = 4;
}

message Upsert {
    string kind = 1;
    repeated string key = 2;
    bytes value = 3;
    int64 version = 4;
}

message Delete {
    string kind = 1;
    repeated string key = 2;
    int64 version = 3;
}

message AfterConstraint {
    int64 version = 1;
}

message UntilVersionConstraint {
    int64 version = 1;
    int64 timeout_ms = 2;
}

message GetEntityRequest {
    string projection = 1;
    string kind = 2;
    repeated string key = 3;
    oneof version_constraint {
        AfterConstraint after = 4;
        UntilVersionConstraint until_version = 5;
    }
}

message GetEntityResponse {
    Entity entity = 1;
    int32 status = 2;  // Status enum: 0=OK, 1=NotFound, 2=Stale, 3=Timeout
}

// --- ViewProjectionConsumer messages ---

message VersionRequest {
    string projection = 1;
    string domain = 2;
    string stream = 3;
}

message VersionResponse {
    int64 version = 1;
}

message WatchChangesRequest {
    string projection = 1;
}

message WatchChangesResponse {
    repeated Upsert upserts = 1;
    repeated Delete deletes = 2;
    int64 version = 3;
}
```

## API Summary

### Core Indexer Framework (`pkg/indexer/`)

```go
// Indexer defines the interface for event processing.
// Each indexer type builds a query2.Query with handlers registered.
type Indexer interface {
    Query() *query2.Query
}

// Wire connects the Pump to pgcqrs for events, locks, and positions.
type Wire interface {
    Watch(ctx context.Context, query *ipc.QueryIn) (v1.WatchInternal, error)
    TryAcquire(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (*v1.LockResult, error)
    NewKeepAlive(ctx context.Context, domain, stream, consumer, holder string) (Lock, error)
    GetPosition(ctx context.Context, domain, stream, consumer string) (int64, bool, error)
}

// GrpcWire wraps a gRPC connection into a Wire.
func GrpcWire(conn *grpc.ClientConn) Wire

// MemoryWire wraps a Transport into a Wire (for testing).
func MemoryWire(t v1.Transport) Wire

// Lock provides heartbeating and release for consumer locks.
type Lock interface {
    Heartbeat(ctx context.Context, position int64) error
    Release(ctx context.Context) error
}

// Pump drives the event processing loop for any Indexer.
type Pump struct { /* ... */ }
func NewPump(wire Wire, indexer Indexer, holder string, opts ...Option) *Pump
func (p *Pump) Run(ctx context.Context) error
```

### Views Types (`pkg/indexer/views/`)

```go
type Key struct{ /* variadic, 1-2 string parts */ }
func Key(parts ...string) Key    // panics if > 2
func (k Key) Parts() []string    // returns the key parts (1 or 2 elements)

type Entity struct {
    Kind    string
    Key     Key
    Value   json.RawMessage
    Version int64
}

type Upsert struct {
    Kind  string
    Key   Key
    Value any  // framework marshals to JSON
}

type Delete struct {
    Kind string
    Key  Key
}

type ReduceResult struct {
    Upserts []Upsert
    Deletes []Delete
}

type Result struct {
    Entity  *Entity
    Version int64
    Status  Status
}

type Status int
const (
    StatusOK       Status = iota
    StatusNotFound
    StatusStale
    StatusTimeout
)

type Change struct {
    Upserts []Upsert
    Deletes []Delete
    Version int64
}
```

### ReduceContext (passed to handlers)

```go
type ReduceContext struct{ /* unexported get function */ }
func (ac *ReduceContext) Get(kind string, key Key) (*Entity, error)
```

### Projection Definition

```go
func NewProjection(domain, stream string, opts ...ProjectionOption) *Projection
func OnKind[T any](kind string, handler func(ctx context.Context, e v1.Envelope, evt *T, actx *ReduceContext) (*ReduceResult, error)) ProjectionOption
func ConsumerName(name string) ProjectionOption
```

### ViewsIndexer (implements `indexer.Indexer`)

```go
type ViewsIndexer struct { /* projection, store, notifier, stream */ }
func NewViewsIndexer(proj *Projection, store Store, notifier *Notifier, stream v1.StreamTransport) *ViewsIndexer
func (v *ViewsIndexer) Query() *query2.Query
```

### Store (views KV storage)

```go
type Store interface {
    Get(ctx context.Context, kind string, key Key) (*Entity, error)
    Persist(ctx context.Context, result *ReduceResult, eventID int64) (*Change, error)
}

func NewMemoryStore() Store
func NewRemoteStore(conn *grpc.ClientConn, projection string) Store
```

### Notifier (in-process change notifications)

```go
type Notifier struct { /* ... */ }
func NewNotifier() *Notifier
func (n *Notifier) Notify(c Change)
func (n *Notifier) OnChange(fn func(Change))
```

One `Notifier` per projection, created by `With`. `ViewsIndexer` calls `Notify` after each apply. `Client.OnChange` and `UntilVersion` subscribe via `OnChange`.

### Client Framework (developer's process)

```go
// With runs a projection against the given system. The system's transport
// determines whether the projection is served in-process (memory) or remotely
// (gRPC); other transports return an error.
func With(ctx context.Context, sys *v1.System, proj *Projection, opts ...ClientOption) (ProjectionClient, error)

// ProjectionClient handles lifecycle and exposes the query API
type ProjectionClient interface { /* Get, Version, OnChange, WaitForState, Close */ }
func (c *Client[L]) Get(ctx context.Context, kind string, key Key, opts ...GetOption) (*Entity, *Result, error)
func (c *Client[L]) Version(ctx context.Context) (int64, error)
func (c *Client[L]) OnChange(fn func(Change)) func()
func (c *Client[L]) Close() error
```

`With` extracts the `*v1.System`'s transport and probes it for `v1.ViewFeature` connectivity. A gRPC connection selects `newGRPCClient` (builds a `GrpcWire` for the Pump, a `RemoteStore`/`RemoteReader` for storage and reads — all from the same gRPC connection). In-process connectivity selects `newMemoryClient` (builds a `MemoryWire` for the Pump, a `MemoryStore`/`MemoryReader` backed by the transport). Both paths share a `newClient` scaffold that calls `EnsureStream` and constructs the `*v1.Stream` before building the ViewsIndexer. The connection lifecycle stays on the caller's `*v1.System` — `System.Close()` releases the transport, so callers should `defer sys.Close()`.

### Options

```go
func After(version int64) GetOption
func UntilVersion(version int64, timeout time.Duration) GetOption
```

### query2.Watch Enhancement

```go
// New method on query2.Watch — returns event ID alongside dispatch
func (w *Watch) TickWithID(ctx context.Context) (int64, error)
```

## Risks / Trade-offs

- [Risk] Synchronous Persist blocks the event loop → Mitigation: Persist should be fast (functional transformation, minimal I/O via ReduceContext.Get). Long-running operations should be async or deferred.
- [Risk] High entity cardinality causes memory pressure → Mitigation: Storage is in pgcqrs database, not in-memory. Projection queries the database on demand via Store.Get.
- [Risk] Consumer lock contention if many projections target the same stream → Mitigation: Each projection has its own consumer name, so locks don't conflict. Multiple projections can consume from the same stream concurrently.
- [Risk] Apply errors halt the projection → Mitigation: This is intentional — skipping events would produce incorrect state. The caller handles retry/crash. Position tracking enables resume from last good position.
- [Risk] Observer pattern creates notification storms under high event volume → Mitigation: Initial implementation broadcasts all changes. Optimization (batching, debouncing) deferred to future change.
- [Risk] Round-trip cost for remote Store operations → Mitigation: Each event may require 2-3 gRPC calls (Get for previous state, ApplyMutations for writes). Acceptable for initial implementation. Future optimizations: batch window, local cache, or in-process topology.
- [Trade-off] At-least-once instead of exactly-once → Mutations are idempotent (upserts replace, deletes are idempotent), so reprocessing is safe. Consistent with how all other consumers in the system work.
- [Trade-off] Framework marshals Upsert.Value → Minor overhead from marshaling `any` to JSON. Significant ergonomic win — handlers pass typed structs directly.
- [Trade-off] Pump runs in developer's process → Developer's binary must stay running for projections to process. This is the expected deployment model — indexers are long-running services.
