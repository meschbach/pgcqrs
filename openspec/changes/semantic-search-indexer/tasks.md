## 1. Package Structure & Types

- [ ] 1.1 Create `pkg/indexer/embedding/` directory structure with Go module references
- [ ] 1.2 Define `Plugin` interface (`HandleEvent`, `EmbedQuery`, `Specification`), `Event` type (wrapping `v1.Envelope + json.RawMessage`), and `EmbedAction` types (AppendEmbedding, ReplaceEmbeddings, DeleteEmbeddings)
- [ ] 1.3 Define `EmbedAction` fields: `Encoder` (string, for journaling), `Identity`, `Vector` ([]float32, plugin-provided), `Text` (string, stored for context), `Metadata` (JSONB)
- [ ] 1.4 Define `Specification` type for declaring event interest (domains, stream kinds)

## 2. Consumer Loop

- [ ] 2.1 Implement `Pump` struct with consumer loop: connect via Wire, acquire lock via TryAcquire, open heartbeater via NewKeepAlive, start streaming
- [ ] 2.2 Implement Watch-based event streaming with afterID for replay and live processing
- [ ] 2.3 Implement heartbeat loop: send heartbeats based on GuaranteeUntil timestamp, configurable via Option pattern
- [ ] 2.4 Implement position tracking: advance position immediately after each event is processed
- [ ] 2.5 Handle lock loss: stop processing, return error to caller
- [ ] 2.6 Handle plugin errors: stop processing, return error to caller
- [ ] 2.7 Define NewPump constructor with required parameters: wire, domain, stream, index (user-chosen name), consumer, holder, plugin, store

## 3. Schema Migrations

- [ ] 3.1 Create `pkg/indexer/embedding/migrations/vector.go` with `//go:embed "vector/*.sql"` for embedded SQL
- [ ] 3.2 Write migration 000001: create `vector` and `vchord` extensions (`CREATE EXTENSION IF NOT EXISTS vchord CASCADE`)
- [ ] 3.3 Write migration 000002: create `indexes` table (domain, stream, name with UNIQUE constraint)
- [ ] 3.4 Write migration 000003: create `encoders` table (id, name with UNIQUE, created_at — no dimension, no config)
- [ ] 3.5 Write migration 000004: create `index_encoders` join table (index_id → indexes, encoder_id → encoders, UNIQUE(index_id, encoder_id))
- [ ] 3.6 Write migration 000005: create dimension tables (`embedded_documents_{N}` and `embedded_vectors_{N}` for N in 384, 768, 1024, 2560, 4096) referencing `index_encoders(id)`, with VectorChord `vchordrq` indexes
- [ ] 3.7 Write down migrations for all (drop dimension tables, join table, encoders, indexes, extensions — unwind everything)
- [ ] 3.8 Implement `MigrateVectorDB(ctx context.Context, dbURL string) error` using `golang-migrate` (matching pgcqrs migrator pattern)
- [ ] 3.9 Verify `golang-migrate` pgx driver works with VectorChord PostgreSQL

## 4. Vector Store

- [ ] 4.1 Define `Store` interface (Upsert, Search, DeleteByIdentity) — all operations scoped by `index_encoder_id`
- [ ] 4.2 Implement auto-registration: ensure index exists in `indexes` table (INSERT ON CONFLICT DO NOTHING), ensure encoder exists in `encoders` table (INSERT ON CONFLICT DO NOTHING), ensure join exists in `index_encoders` table (INSERT ON CONFLICT DO NOTHING)
- [ ] 4.3 Implement write path: select dimension table based on vector length, insert into `embedded_documents_{N}` and `embedded_vectors_{N}` with `index_encoder_id`
- [ ] 4.4 Implement search: vector similarity on `embedded_vectors_{N}` filtered by `index_encoder_id`, join with `embedded_documents_{N}` for metadata, support metadata predicate filtering

## 5. gRPC Search Service

- [ ] 5.1 Define proto for search service: `SearchRequest` with `domain`, `stream`, `index`, `encoder`, `dimension`, `query`, `top_k`, `min_score`, `predicates`; `SearchResponse` with `results`, `encoder`, `dimension`, `last_indexed_event_id`
- [ ] 5.2 Implement search RPC handler: look up `index_encoders` join by domain, stream, index, encoder → get `index_encoder_id` → select dimension table → call `plugin.EmbedQuery(ctx, query, encoder)` → search filtered by `index_encoder_id` → return results
- [ ] 5.3 Include lag information in search response (last indexed event ID)
- [ ] 5.4 Handle encoder not found: return clear error if requested encoder is not registered for the target index

## 6. Example Binary

- [ ] 6.1 Create example plugin that projects entity state over a stream (e.g., "incident" events) with plugin-provided vectors and `EmbedQuery` implementation
- [ ] 6.2 Create example `main.go` that wires plugin, vector store, migrator subcommand, and gRPC search service
- [ ] 6.3 Demonstrate multiple encoders (e.g., 384D + 1024D) in the example plugin

## 7. Tests

- [ ] 7.1 Write unit tests for EmbedAction processing (append, replace, delete) with normalized schema
- [ ] 7.2 Write unit tests for consumer loop with in-memory pgcqrs transport + lock
- [ ] 7.3 Write unit tests for vector store abstraction with in-memory backend
- [ ] 7.4 Write unit tests for auto-registration of indexes, encoders, and join table entries
- [ ] 7.5 Write test proving Watch + afterID works correctly (code path exists but no test coverage)
- [ ] 7.6 Write tests for migrator (apply up, apply down, verify unwind)
- [ ] 7.7 Run full test suite and verify all pass

## 8. Query2 Hygiene (discovered during review)

- [ ] 8.1 Document in query2 that `After` does not apply to `OnID` queries
- [ ] 8.2 Add tests verifying the documented behavior

## 9. Wire Interface with Generic Lock (discovered during review)

- [ ] 9.1 Define `Lock` interface in `pkg/indexer` matching gRPC's `KeepAlive` methods: `Heartbeat(ctx, position int64) error` and `Release(ctx) error`
- [ ] 9.2 Define `Wire[L Lock]` interface in `pkg/indexer` with `TryAcquire` (returns `*LockResult`), `NewKeepAlive` (returns `L`), `GetPosition`, `Watch`, `QueryBatchR2`
- [ ] 9.3 Expand memory transport to implement `Wire` interface with `MemoryLock` type satisfying `Lock`
- [ ] 9.4 Verify gRPC adapter already satisfies `Wire[*KeepAlive]` (no changes needed)
- [ ] 9.5 Write tests for both transports using the `Wire` interface

## 10. Naming Confusion Resolution (discovered during review)

- [ ] 10.1 Document the distinction: `TryAcquire` acquires the lock (returns `*LockResult`), `NewKeepAlive` opens the heartbeating mechanism (returns `Lock`)
- [ ] 10.2 Ensure consistent terminology in codebase: "lock acquisition" vs "heartbeating"
