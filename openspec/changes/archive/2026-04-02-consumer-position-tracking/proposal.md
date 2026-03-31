## Why

Multiple consumers need to independently track their position in a stream to enable resumable materialized view projections. Without a native way to store "where am I" per consumer, clients must manage this state externally (in Elasticsearch, Redis, etc.), adding complexity. Providing built-in consumer position tracking enables reliable projector patterns for search indexes, aggregations, and other downstream systems.

## What Changes

- Add `ConsumerPosition` storage table: `(domain, stream, consumer, event_id, updated_at)`
- Add position operations to Transport interface: `SetPosition`, `GetPosition`, `ListConsumers`, `DeletePosition`
- Add gRPC and HTTP endpoints for position management
- Add query2 builder support for `After(id)` predicate to enable backfill from position

## Capabilities

### New Capabilities
- `consumer-position-tracking`: Allows consumers to persist and retrieve their read position within a stream, enabling resumable projectors and multi-consumer architectures

### Modified Capabilities
- (none)

## Impact

- New interface methods on Transport
- New database migration for consumer_positions table
- New gRPC service methods
- New REST endpoints
- New query2 method: `After(id int64)`
