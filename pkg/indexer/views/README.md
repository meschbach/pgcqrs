# View Projection Framework

The view projection framework provides a KV-backed materialized view system built on the core indexer framework. It enables typed per-kind event handlers, previous-state retrieval, atomic mutations, version-aware reads, and change notification.

## Overview

Define a projection with `OnKind[T]` typed handlers, run it, query it. The same projection works with in-memory storage for tests, PostgreSQL for production, and gRPC for remote access.

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

## Quick Start

### Define a Projection

```go
import (
    "context"
    v1 "github.com/meschbach/pgcqrs/pkg/v1"
    "github.com/meschbach/pgcqrs/pkg/indexer/views"
)

type ItemCreated struct {
    ItemID string `json:"itemID"`
    Name   string `json:"name"`
}

proj := views.NewProjection("inventory", "items",
    views.OnKind("ItemCreated", func(ctx context.Context, e v1.Envelope, evt *ItemCreated, actx *views.ReduceContext) (*views.ReduceResult, error) {
        return &views.ReduceResult{
            Upserts: []views.Upsert{
                {Kind: "items", Key: views.NewKey(evt.ItemID), Value: evt},
            },
        }, nil
    }),
)
```

### Connect and Query

```go
// In-memory (for testing)
sys := v1.NewSystem(v1.NewMemoryTransport())
defer sys.Close()
client, err := views.With(ctx, sys, proj)
defer client.Close()

// From config (memory, HTTP, or gRPC transports)
cfg := v1.NewConfig().LoadEnv()
sys, err := cfg.SystemFromConfig()
defer sys.Close()
client, err := views.With(ctx, sys, proj)
defer client.Close()

// Query entity state
entity, result, err := client.Get(ctx, "items", views.NewKey("item-42"))
if result.Status == views.StatusOK {
    fmt.Printf("Item: %s (version %d)\n", entity.Value, entity.Version)
}
```

### Version-Aware Reads

```go
// Fail if stale
entity, result, err := client.Get(ctx, "items", views.NewKey("item-42"), views.After(500))
if result.Status == views.StatusStale {
    // Entity version is behind 500
}

// Wait for version
entity, result, err := client.Get(ctx, "items", views.NewKey("item-42"), views.UntilVersion(500, 200*time.Millisecond))
if result.Status == views.StatusTimeout {
    // Projection didn't reach version 500 in time
}
```

### Change Notifications

```go
client.OnChange(func(c views.Change) {
    fmt.Printf("Version %d: %d upserts, %d deletes\n", c.Version, len(c.Upserts), len(c.Deletes))
})
```

## API Reference

### Core Types

| Type | Description |
|------|-------------|
| `Key` | Entity key with 1 or 2 string parts. `NewKey("id")` or `NewKey("id", "sub-id")` |
| `Entity` | Stored entity with `Kind`, `Key`, `Value` (json.RawMessage), `Version` |
| `Upsert` | Mutation: `Kind`, `Key`, `Value` (any — framework marshals to JSON) |
| `Delete` | Mutation: `Kind`, `Key` |
| `ReduceResult` | Handler output: `Upserts []Upsert`, `Deletes []Delete` |
| `ReduceContext` | Handler context: `Get(kind, key)` reads previous entity state |
| `Change` | Notification: `Upserts`, `Deletes`, `Version` |
| `Result` | Get response: `Entity`, `Status` |
| `Status` | `StatusOK`, `StatusNotFound`, `StatusStale`, `StatusTimeout` |

### Projection

```go
proj := views.NewProjection(domain, stream, ...opts)
views.OnKind[T](kind string, handler func(ctx, envelope, *T, *ReduceContext) (*ReduceResult, error))
views.ConsumerName(name string)  // override default consumer name
```

### Client

```go
sys, err := cfg.SystemFromConfig()
defer sys.Close()
client, err := views.With(ctx, sys, proj, ...opts) // returns views.ProjectionClient
entity, result, err := client.Get(ctx, kind, key, ...opts)
version, err := client.Version(ctx)
client.OnChange(fn func(Change))                   // returns unsubscribe func
client.WaitForState(ctx, state)
client.Close()
```

### Get Options

```go
views.After(version int64)                              // fail if stale
views.UntilVersion(version int64, timeout time.Duration) // wait for version
```

## Architecture

- **Pump** (`pkg/indexer/`): Drives the event processing loop — acquires lock, resumes from position, watches events, heartbeats
- **ViewsIndexer** (`pkg/indexer/views/`): Implements `Indexer` interface, builds query2.Query with typed handlers
- **Store**: KV storage interface with `Get` and `Persist` methods
  - `MemoryStore`: In-memory for testing
  - `RemoteStore`: gRPC-backed for production
  - `PGStore`: PostgreSQL-backed (server-side)
- **gRPC Services**: `ViewProjectionStore` (mutations/reads for Pump), `ViewProjectionConsumer` (reads for webapps)

## Database Schema

```sql
view_projection_names       -- projection name enumeration
view_projection_kinds       -- entity kind enumeration
view_projection_entries_single     -- single-key entities
view_projection_entries_composite  -- composite-key entities
```

## Transport Requirements

`views.With` dispatches based on the `*v1.System`'s transport (via its view connectivity):

- **gRPC**: Full support with remote storage and version tracking
- **Memory**: Full support with in-process storage and version tracking
- **HTTP**: Not supported - the HTTP transport does not expose view projection services; `views.With` returns an error

## Examples

- `examples/view-projection/` — Basic inventory projection with typed handlers
- `examples/view-projection-versioned/` — Version-aware reads with `UntilVersion` and `After`
- `examples/view-projection-watch/` — Change notifications with `OnChange`
