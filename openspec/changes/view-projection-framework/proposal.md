## Why

Multiple consumers need to materialize entity state from event streams, but there is no reusable foundation for
building resumable, lock-coordinated projections. Today, each consumer (embedding indexer, webapp caches, search
indexes) implements its own event processing, position tracking, and state management. This leads to:

- Duplicated infrastructure: every consumer re-implements Watch loops, heartbeat management, and position tracking
- Inconsistent patterns: some consumers store state externally, some recompute on every query
- No coordination: consumers cannot share projected state or notify each other of changes
- No version awareness: consumers cannot express "give me state as of version X" or "wait until the projection
  catches up to version Y"

The core indexer framework (`pkg/indexer/`) extracts the shared event processing loop into a reusable Pump.
Specialized indexer types (`pkg/indexer/views/`, `pkg/indexer/embedding/`) plug into it with their own handlers
and storage. The views indexer provides a KV-backed materialized view system with typed per-kind handlers,
consumer lock coordination, resumable processing, version-aware reads, and change notification.

Developer experience is a first-class concern. The API is designed for fast prototyping: define a projection with
typed handlers, run it, query it. The same projection works with in-memory storage for tests, PostgreSQL for
production, and gRPC for remote access — transparently.

## What Changes

- New core indexer framework at `pkg/indexer/` providing Pump, Indexer interface (one method, no generics), Wire interface, and Lock interface
- `GrpcWire` / `MemoryWire` adapter types: thin wrappers bridging Wire to existing `GrpcAdapter` and `MemoryTransport` types
- `query2.Watch.TickWithID` — new method exposing event ID for heartbeating without dropping to WatchInternal
- New views indexer at `pkg/indexer/views/` providing ViewProjection types, client framework, and projection lifecycle management
- `OnKind[T]` typed handler registration: each event kind maps to a typed handler function, eliminating manual
  JSON unmarshaling and type switching
- `ReduceContext` struct passed to handlers, providing previous-state retrieval (`Get`) and room for future expansion
- `Key` variadic type for entity keys: single-key for the common case, two-level composite keys when needed
- `Notifier` type: local in-process callback registry for change notifications, used by `OnChange` and `UntilVersion`
- `With` client framework: runs a projection against a `*v1.System`, building Wire + Transport from the system's transport,
  creating Notifier, builds Pump, handles lifecycle, exposes Get/Version/OnChange API
- `UntilVersion` uses notification (subscribes to Notifier), not polling — zero extra database queries
- `ViewProjectionStore` gRPC service: narrow KV storage service (ApplyMutations, GetEntity) in pgcqrs, used by the Pump for mutations and previous-state reads
- `ViewProjectionConsumer` gRPC service: read API (GetEntity, Version, WatchChanges) in pgcqrs for webapps and downstream indexers
- Storage in pgcqrs database for projected entity state (two-table schema: single-key and composite-key)
- Integration with existing consumer locks for exclusive access
- Integration with existing consumer position tracking for resumable projections
- Integration with existing Watch for event streaming
- In-process event bus connecting ViewProjectionStore → ViewProjectionConsumer (same pattern as existing Watch)
- Composition: embedding indexer reads views store for projected entity state
- Comprehensive documentation: godoc, examples, README

## Capabilities

### New Capabilities

- `indexer-core`: Core indexer framework at `pkg/indexer/` providing Pump loop, Indexer interface (`Query() *query2.Query`),
  Wire interface for pgcqrs connection, and Lock interface for heartbeating
- `view-projection-framework`: Views indexer at `pkg/indexer/views/` providing `OnKind[T]` typed handler registration,
  `ReduceContext` for previous-state retrieval, `With` client framework,
  and pgcqrs-backed KV storage for projected entity state
- `view-projection-store`: gRPC ViewProjectionStore in the pgcqrs service providing KV storage operations
  (ApplyMutations, GetEntity) used by the Pump
- `view-projection-consumer`: gRPC ViewProjectionConsumer in the pgcqrs service providing remote access
  to projections (GetEntity, Version, WatchChanges) for webapps and external services

### Modified Capabilities

- `query2-watch`: Add `TickWithID(ctx) (int64, error)` method to `query2.Watch` for exposing event ID
- `transport-interface`: No changes to Transport interface (uses existing Watch, TryAcquire, GetPosition,
  HeartbeatWithPosition methods). New `GrpcAdapter(conn)` constructor added to `pkg/v1/` for wrapping
  pre-dialed connections (used by `GrpcWire` to share one gRPC conn for both Wire and Transport).

## Impact

- New package: `pkg/indexer/` with Pump, Indexer interface, Wire, Lock, and options
- New package: `pkg/indexer/views/` with projection types, client framework, Store, and client API
- New package: `pkg/indexer/views/grpc/` with proto definition and generated gRPC code (ViewProjectionStore + ViewProjectionConsumer)
- New database schema in pgcqrs: `view_projection_names`, `view_projection_kinds`, `view_projection_entries_single`, and `view_projection_entries_composite` tables
- New gRPC ViewProjectionStore and ViewProjectionConsumer registered on existing pgcqrs gRPC server (no new config)
- Modified `pkg/v1/query2/watch.go`: add `TickWithID` method
- No changes to existing Transport interface
- Pump runs in developer's process, connects to pgcqrs via Wire (gRPC or memory)
- Developer writes Go code with typed handlers; framework handles gRPC communication transparently
- In-process event bus connects ViewProjectionStore → ViewProjectionConsumer (same pattern as existing Watch)
- Semantic indexer can read from views Store for projected entity state (composition)
- Webapps can query projections via ViewProjectionConsumer gRPC for current entity state with version-aware consistency
- All public APIs documented with godoc, examples, and README
