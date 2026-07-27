## Why

The current query system supports structural filtering (kind, property equality, JSON subset match) but has no semantic search. An embedding-based indexer enables free-text similarity search over stream events, unlocking use cases like "find incidents similar to this description" or "search all events mentioning authentication failures."

Multiple consumers need to independently track and coordinate their position in streams. The embedding indexer is the first consumer of the new consumer locks system, requiring exclusive access to a (domain, stream) partition to avoid duplicate processing across instances.

The indexer framework targets a separate PostgreSQL instance with the VectorChord extension for vector search. This reduces resource contention with the primary pgcqrs database and proves out the pattern of strong consistent vector storage.

## What Changes

- New Go library at `pkg/indexer/embedding/` providing an indexer framework
- Plugin interface: users implement `Plugin` with `HandleEvent` (returns pre-computed vectors) and `EmbedQuery` (embeds search queries)
- Consumer loop: connects to pgcqrs via Wire interface, acquires lock, opens heartbeater, processes events through plugin
- Framework-owned database schema via embedded SQL migrations (golang-migrate pattern)
- Normalized schema: `indexes`, `encoders`, `index_encoders` join table, and dimension tables (`embedded_documents_{N}`, `embedded_vectors_{N}`)
- Multiple indexer instances share a single VectorChord database; data partitioned by `{domain}.{stream}.{index}.{encoder}` tuple
- Multiple encoders supported concurrently per indexer
- gRPC search service embedded in the framework binary for clients to query
- Migrator subcommand for applying VectorChord DB schema on deploy
- Example indexer binary demonstrating the framework

## Capabilities

### New Capabilities

- `embedding-indexer-framework`: Go library providing consumer loop, plugin interface, lock management, vector store abstraction, and schema migrations for building semantic search indexers over pgcqrs streams

### Modified Capabilities

- `memory-transport`: Expanded to match gRPC's Wire interface for consumer lock and heartbeating support

## Impact

- New package: `pkg/indexer/embedding/` with plugin types, consumer loop, vector store, search service, migrations
- New package: `pkg/indexer/embedding/migrations/` with embedded SQL for VectorChord schema
- New database schema in separate VectorChord PostgreSQL: `indexes`, `encoders`, `index_encoders`, and dimension tables (384, 768, 1024, 2560, 4096) with VectorChord `vchordrq` indexes
- New gRPC proto for search service within the framework
- Migrator function (`MigrateVectorDB`) for applying schema on deploy
- Example binary under `examples/` (or `cmd/`)
- Expanded memory transport: add `NewKeepAlive` method and `MemoryLock` type to match gRPC's Wire interface
