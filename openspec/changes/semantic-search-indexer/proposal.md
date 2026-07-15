## Why

The current query system supports structural filtering (kind, property equality, JSON subset match) but has no semantic search. An embedding-based indexer enables free-text similarity search over stream events, unlocking use cases like "find incidents similar to this description" or "search all events mentioning authentication failures."

## What Changes

- New Go library at `pkg/indexer/embedding` providing an indexer framework
- Plugin interface: users implement `Plugin` with `HandleEvent` returning slice of typed `EmbedAction` (append, replace, delete)
- Consumer loop: connects to pgcqrs via gRPC, replays from consumer position, processes events through plugin
- Lock integration: uses the new consumer lock service (from the `consumer-locks` change) with heartbeat piggybacking position
- Vector store abstraction with PostgreSQL + VectorChord backend
- Pre-created dimension tables (384, 768, 1024, 2560, 4096) with normalized schema: `embedded_documents_N` + `embedded_vectors_N`
- `embedded_encoders` table to journal which encoder produced which embeddings
- Optional gRPC search service embedded in the framework binary for clients to query
- Search returns event ID + metadata (no full event body)
- Example indexer binary demonstrating the framework

No changes to pgcqrs core beyond what's in the `consumer-locks` change.

## Capabilities

### New Capabilities
- `embedding-indexer-framework`: Go library providing consumer loop, plugin interface, lock management, and vector store abstraction for building semantic search indexers over pgcqrs streams

### Modified Capabilities

None — this is purely additive. No existing specs are changed.

## Impact

- New package: `pkg/indexer/embedding/` with plugin types, consumer loop, vector store, search service
- New database schema: `embedded_documents_N` and `embedded_vectors_N` tables (per dimension) plus `embedded_encoders` table — these live in the indexer's own PostgreSQL (not pgcqrs's)
- New gRPC proto for search service within the framework
- Example binary under `examples/` (or `cmd/`)
- No changes to existing pgcqrs core code beyond using the lock service
