# View Projection Framework - Exploration Summary

This document summarizes the architectural decisions made during the exploration phase before implementation.

## Key Architectural Changes (Revised)

### 1. Core Indexer Framework (`pkg/indexer/`)
Both view projections and embedding indexers share the same event processing loop. The core framework extracts this into a reusable Pump:
- `Pump` struct drives the loop: lock → position → query.Watch → TickWithID → heartbeat
- `Indexer` interface: one method (`Query() *query2.Query`), no generics
- `Wire` interface: connects to pgcqrs (gRPC or memory), with `GrpcWire` and `MemoryWire` adapter types
- Pump runs in developer's process, not server-side

### 2. query2.Watch.TickWithID
New method on `query2.Watch` exposing the event ID for heartbeating. Eliminates the need to drop to `WatchInternal`. One-line change — `Tick` already has `m.Id` internally.

### 3. Split gRPC Services (Revised Roles)
- **ViewProjectionStore**: Narrow KV storage service (ApplyMutations, GetEntity). Used by the Pump for mutations and previous-state reads. Replaces the old ViewProjectionIndexer (which was a pump orchestrator).
- **ViewProjectionConsumer**: Read API (GetEntity, GetVersion, WatchChanges). Used by webapps and downstream indexers. Unchanged from original design.

Both register on the existing gRPC server — no new config, no new deployment.

### 4. Pump in Developer's Process
The Pump runs in the developer's binary. It connects to pgcqrs via Wire (gRPC for production, memory for tests). Handlers execute locally. No bidi gRPC stream needed — the Pump uses existing Wire primitives (Watch, locks, positions) plus the narrow ViewProjectionStore gRPC for KV operations. The `Connect`/`ConnectMemory` functions bridge Wire and Transport from the same underlying connection.

### 5. Store and Notification are Indexer-Internal
The Pump doesn't know about stores or notifiers. Each indexer type encapsulates these within its `Query()` implementation. The views indexer's handlers call `store.Persist()` and `notifier.Notify()` internally.

### 6. Composition: Indexers Read Each Other's Stores
The views Store's `Get` method is available to any indexer. The embedding indexer holds a `views.Store` reader to access projected entity state for generating embeddings. The core Pump has no knowledge of cross-indexer dependencies.

### 7. Two-Table Schema (Unchanged)
- `view_projection_entries_single`: Single-key entities (key TEXT)
- `view_projection_entries_composite`: Composite-key entities (key1 TEXT, key2 TEXT)
- `Key.Parts()` routes to correct table
- No sentinel values, no wasted storage

### 8. In-Process Event Bus (Unchanged)
- ViewProjectionStore handler emits changes to bus after storing mutations
- ViewProjectionConsumer subscribes to bus for WatchChanges
- Same pattern as existing Watch mechanism

## Design Decisions Recorded

### High Confidence
1. **Core Indexer interface is one method, no generics** — `Query() *query2.Query`
2. **Pump runs in developer's process** — not server-side
3. **query2.Watch.TickWithID** — expose event ID without dropping to WatchInternal
4. **ViewProjectionStore replaces ViewProjectionIndexer** — narrow KV service, not pump orchestrator
5. **Store and notification are indexer-internal** — Pump doesn't know about them
6. **Composition via Store.Get** — embedding reads views state
7. **Two-table schema** — Clean separation, no waste
8. **Wire interface** — Non-generic, with thin GrpcWire/MemoryWire adapter types
9. **EnsureStream before Pump starts** — Prevents errors on non-existent streams
10. **Two version concepts** — Entity version (per-entity) vs Projection version (global)

### Resolved
1. **Where does the Pump run?** — Developer's process, not pgcqrs service
2. **How does the Pump heartbeat?** — Via query2.Watch.TickWithID, not WatchInternal
3. **What is the Indexer interface?** — One method (`Query()`), no generics, no Dispatch
4. **How do indexers compose?** — Via Store.Get, not Pump awareness
5. **What gRPC services are needed?** — ViewProjectionStore (KV) + ViewProjectionConsumer (read), both on existing server

## Document Updates

All OpenSpec artifacts updated:
- ✅ proposal.md - Updated capabilities (indexer-core, view-projection-store), impact
- ✅ design.md - Major rewrite: core framework, Pump architecture, TickWithID, composition, revised gRPC services
- ✅ tasks.md - Reorganized: query2 enhancement, core indexer, views indexer, gRPC handlers, client framework
- ✅ specs/view-projection-framework/spec.md - New requirements (Indexer interface, Pump, TickWithID, composition, ViewProjectionStore)

## Next Steps

Ready to proceed with implementation. The architecture is clear:
1. Add `TickWithID` to `query2.Watch`
2. Implement core indexer framework (`pkg/indexer/`)
3. Implement views types and projection definition (`pkg/indexer/views/`)
4. Implement ViewsIndexer (implements Indexer)
5. Implement Store (memory + remote)
6. Implement gRPC proto and server handlers (ViewProjectionStore + Consumer)
7. Implement database migration
8. Implement client framework (Connect, ConnectMemory)
9. Write tests
10. Write documentation and examples

## Open Questions

None identified. All major architectural decisions have been made and recorded.
