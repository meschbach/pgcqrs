## 1. query2.Watch Enhancement

- [x] 1.1 Add `TickWithID(ctx context.Context) (int64, error)` method to `query2.Watch` in `pkg/v1/query2/watch.go`
- [x] 1.2 Write unit tests for `TickWithID`: returns event ID alongside handler dispatch
- [x] 1.3 Verify existing `Tick` tests still pass (no behavior change)

## 2. Core Indexer Framework (`pkg/indexer/`)

- [x] 2.1 Create `pkg/indexer/` directory structure
- [x] 2.2 Define `Lock` interface with `Heartbeat(ctx, position int64) error` and `Release(ctx) error`
- [x] 2.3 Define `Wire` interface with Watch, TryAcquire, NewKeepAlive (returns Lock), GetPosition
- [x] 2.4 Define `Indexer` interface with `Query() *query2.Query`
- [x] 2.5 Implement non-generic `Pump` struct with Wire, Indexer, holder, and options
- [x] 2.6 Implement `Pump.Run(ctx)`: acquire lock → get position → build query with AfterID → Watch → TickWithID loop → heartbeat
- [x] 2.7 Implement Pump options: TTL, heartbeat margin, holder identity
- [x] 2.8 Implement `v1.NewGrpcAdapter(conn *grpc.ClientConn)` constructor in `pkg/v1/`: wraps an existing gRPC connection into a GrpcAdapter (complements existing `NewGRPCTransport(url)` which dials internally)
- [x] 2.9 Implement `GrpcWire(conn)` adapter: wraps `v1.GrpcAdapter`, embeds it for Watch/TryAcquire/GetPosition, exposes `KeepAlive` as `NewKeepAlive`
- [x] 2.10 Implement `memoryLock` satisfying `Lock` interface: delegates to `Transport.HeartbeatWithPosition` and `Transport.Release`
- [x] 2.11 Implement `MemoryWire(t)` adapter: wraps `v1.Transport`, delegates Watch/TryAcquire/GetPosition, returns `memoryLock` from `NewKeepAlive`
- [x] 2.12 Write unit tests for Pump with in-memory transport: acquire lock, process events, heartbeat
- [x] 2.13 Write unit tests for Pump lock lifecycle: acquire, heartbeat, release, loss handling
- [x] 2.14 Write unit tests for Pump position tracking: resume from last position

## 3. Views Package Structure & Types (`pkg/indexer/views/`)

- [x] 3.1 Create `pkg/indexer/views/` directory structure
- [x] 3.2 Define `Key` type with variadic constructor `NewKey(parts ...string)`, panics if > 2 parts
- [x] 3.3 Implement `Key.Parts() []string` returning 1 or 2 elements
- [x] 3.4 Define `Entity` type with `Kind`, `Key`, `Value json.RawMessage`, `Version int64`
- [x] 3.5 Define `Upsert` type with `Kind`, `Key`, `Value any` (framework marshals to JSON)
- [x] 3.6 Define `Delete` type with `Kind`, `Key`
- [x] 3.7 Define `ReduceResult` type with `Upserts []Upsert` and `Deletes []Delete`
- [x] 3.8 Define `ReduceContext` struct with unexported `get` function and public `Get(kind, key)` method
- [x] 3.9 Define `Change` type with `Upserts []Upsert`, `Deletes []Delete`, `Version int64`
- [x] 3.10 Define `Result` type with `Entity *Entity`, `Version int64`, `Status Status`
- [x] 3.11 Define `Status` type with constants: `StatusOK`, `StatusNotFound`, `StatusStale`, `StatusTimeout`
- [x] 3.12 Define `GetOption` type and options: `After(version)`, `UntilVersion(version, timeout)`
- [x] 3.13 Define `ProjectionOption` type and options: `OnKind[T]`, `ConsumerName`

## 4. Views Projection Definition

- [x] 4.1 Define `Projection` struct with domain, stream, kind handlers, consumer name
- [x] 4.2 Implement `NewProjection(domain, stream, ...ProjectionOption)` constructor
- [x] 4.3 Implement `OnKind[T]` generic option: registers typed handler for event kind, stores kind name for query building
- [x] 4.4 Implement `ConsumerName` option: overrides default projection-name-as-consumer-name
- [x] 4.5 Verify kind handlers are stored and accessible for query building

## 5. ViewsIndexer (implements `indexer.Indexer`)

- [x] 5.1 Define `ViewsIndexer` struct with projection, store, notifier, and stream
- [x] 5.2 Implement `NewViewsIndexer(proj, store, notifier, stream)` constructor
- [x] 5.3 Implement `ViewsIndexer.Query() *query2.Query`: build query from OnKind[T] registrations
- [x] 5.4 Wire up handler dispatch: query2 Op-based dispatch → OnKind[T] handler → store.Persist → notifier.Notify
- [x] 5.5 Implement ReduceContext.Get: reads from Store.Get (remote or memory)
- [x] 5.6 Verify handler receives typed event with automatic JSON unmarshaling
- [x] 5.7 Verify handler can retrieve previous state via ReduceContext.Get

## 6. Views Store

- [x] 6.1 Define `Store` interface with `Get(ctx, kind, key) (*Entity, error)` and `Persist(ctx, *ReduceResult, eventID) (*Change, error)`
- [x] 6.2 Implement `NewMemoryStore()` for unit testing
- [x] 6.3 Implement `NewRemoteStore(conn, projection)` backed by ViewProjectionStore gRPC
- [x] 6.4 Write unit tests for MemoryStore: Get, Apply, version tracking
- [x] 6.5 Write unit tests for Key routing: single-key vs composite-key

## 7. Views gRPC Proto & Generated Code

- [x] 7.1 Create `pkg/indexer/views/grpc/views.proto` with ViewProjectionStore and ViewProjectionConsumer services
- [x] 7.2 Define ApplyMutations, GetEntity, Version, WatchChanges RPCs and messages
- [x] 7.3 Generate Go code from proto (protoc)
- [x] 7.4 Verify generated code compiles

## 8. Views gRPC Server Handlers (pgcqrs service)

- [x] 8.1 Implement `viewsStoreHandler` for ViewProjectionStore: ApplyMutations (atomic PG transaction), GetEntity
- [x] 8.2 Implement `viewsConsumerHandler` for ViewProjectionConsumer: GetEntity, Version, WatchChanges
- [x] 8.3 Implement PGStore internals: Upsert (INSERT ON CONFLICT UPDATE), Delete, Get with normalized kind/projection tables
- [x] 8.4 Implement UntilVersion: subscribe to Notifier channel, check version on each change event, return on target reached or timeout
- [x] 8.5 Implement WatchChanges: subscribe to bus, read from store, stream to client
- [x] 8.6 Register ViewProjectionStore and ViewProjectionConsumer on existing grpc.Server in `grpcPort.Serve()`
- [x] 8.7 Write unit tests for ViewProjectionStore: ApplyMutations, GetEntity with in-memory PG mock
- [x] 8.8 Write unit tests for ViewProjectionConsumer: GetEntity, Version, WatchChanges

## 9. Database Migration

- [x] 9.1 Write migration: create `view_projection_names`, `view_projection_kinds`, `view_projection_entries_single`, `view_projection_entries_composite` tables
- [x] 9.2 Write indexes: `idx_vpes_projection_kind`, `idx_vpec_projection_kind`
- [x] 9.3 Write down migration: drop all view_projection tables
- [x] 9.4 Verify migration applies cleanly

## 10. Client Framework

- [x] 10.1 Implement `Connect(ctx, address, proj)`: build GrpcWire from gRPC conn, build Transport from same conn, EnsureStream, build *v1.Stream, build RemoteStore, Notifier, ViewsIndexer (with stream), Pump; start Pump in goroutine
- [x] 10.2 Implement `ConnectMemory(ctx, transport, proj)`: build MemoryWire from transport, EnsureStream, build *v1.Stream, build MemoryStore, Notifier, ViewsIndexer (with stream), Pump; start Pump in goroutine
- [ ] 10.2a Rework entry point to `With(ctx, sys *v1.System, proj, ...ClientOption)`: extract the system's transport, dispatch via `v1.ViewFeature` connectivity to `newGRPCClient`/`newMemoryClient` sharing a `newClient` scaffold; return `ProjectionClient`. Remove `Connect`, `ConnectMemory`, `ConnectFromConfig`; rename `ConnectOption` to `ClientOption`. Connection lifecycle moves to `*v1.System` (`System.Close()`).
- [x] 10.3 Implement `Client.Get(ctx, kind, key, opts)`: call ViewProjectionConsumer gRPC GetEntity
- [x] 10.4 Implement `Client.Version()`: call ViewProjectionConsumer gRPC Version
- [x] 10.5 Implement `Client.OnChange(fn)`: subscribe to local notifier (or ViewProjectionConsumer WatchChanges)
- [x] 10.6 Implement `Client.Close()`: stop Pump, release lock, close connections
- [x] 10.7 Write unit tests for Connect/ConnectMemory: full lifecycle with in-memory transport
- [x] 10.7a Migrate unit tests to `With`: `setupClientWithEvents` and all call sites use `With(ctx, v1.NewSystem(transport), proj)`; `TestConnectMemoryCreatesClient` becomes `TestWithMemorySystem`
- [x] 10.8 Write unit tests for Client.Get with version constraints: After (satisfied/stale), UntilVersion (satisfied/timeout)

## 11. Integration Tests

- [x] 11.1 Write integration test: full Pump lifecycle with real PostgreSQL (migration, storage, gRPC services)
- [x] 11.2 Write integration test: views indexer processes events, stores mutations, client queries
- [x] 11.3 Write integration test: consumer lock contention (two Pumps, one wins)
- [x] 11.4 Write integration test: position tracking (crash and resume)
- [x] 11.5 Run full test suite and verify all pass

**Known Bug Discovered**: Integration tests revealed a bug in the gRPC transport's Watch implementation (`internal/service/grpc.go:308-340`). The Watch re-runs queries with a stale `AfterID`, causing events to be delivered multiple times. This causes data duplication in view projections when using gRPC transport. Memory transport works correctly. This is a transport-layer bug, not a view projection framework bug.

## 12. Documentation & Examples

- [x] 12.1 Write godoc for all public types: Key, Entity, Upsert, Delete, ReduceResult, ReduceContext, Result, Status, Change, Projection, Client, ViewsIndexer, Store
- [x] 12.2 Write godoc for all public methods: OnKind, Get, Version, OnChange, Close, Query
- [x] 12.2a Update godoc/README/design for `With` and `ProjectionClient`; remove `Connect`/`ConnectMemory`/`ConnectFromConfig` references
- [x] 12.3 Write godoc for core indexer types: Pump, Indexer, Wire, Lock
- [x] 12.4 Create example `examples/view-projection/`: basic inventory projection with typed handlers
- [x] 12.5 Create example `examples/view-projection-versioned/`: consumer using Get with UntilVersion
- [x] 12.6 Create example `examples/view-projection-watch/`: consumer using OnChange for streaming changes
- [x] 12.7 Update `run-examples.sh`: add view-projection examples
- [x] 12.8 Write README for pkg/indexer/views/ with overview, quick start, and API reference
