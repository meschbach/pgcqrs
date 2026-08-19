# View Projection Framework

The view projection framework provides a KV-backed materialized view system built on the core indexer framework. It enables typed per-kind event handlers, previous-state retrieval, atomic mutations, version-aware reads, and change notification.

## Architecture

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

### Components

- **Projection**: Defines event handlers and mutation logic
- **Client**: Provides query API and manages the pump lifecycle
- **Store**: KV storage interface with `Get` and `Persist` methods
  - `MemoryStore`: In-memory for testing
  - `RemoteStore`: gRPC-backed for production
  - `PGStore`: PostgreSQL-backed (server-side)
- **Notifier**: Broadcasts changes to observers
- **Pump**: Drives event processing — acquires lock, resumes from position, watches events, heartbeats

## Transport Support

`views.With` dispatches based on the `*v1.System`'s transport (via its view connectivity):

- **gRPC**: Full support with remote storage and version tracking
- **Memory**: Full support with in-process storage and version tracking
- **HTTP**: Not supported - the HTTP transport does not expose view projection services; `views.With` returns an error

## Database Schema

```sql
view_projection_names       -- projection name enumeration
view_projection_kinds       -- entity kind enumeration
view_projection_entries_single     -- single-key entities
view_projection_entries_composite  -- composite-key entities
```

## Examples

- `examples/view-projection/` — Basic inventory projection with typed handlers
- `examples/view-projection-versioned/` — Version-aware reads with `UntilVersion` and `After`
- `examples/view-projection-watch/` — Change notifications with `OnChange`

## API Reference

For detailed API documentation, see `go doc github.com/meschbach/pgcqrs/pkg/indexer/views`.
