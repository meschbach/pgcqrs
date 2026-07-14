## 1. Package Structure & Types

- [ ] 1.1 Create `pkg/indexer/embedding/` directory structure with Go module references
- [ ] 1.2 Define `Plugin` interface (`HandleEvent`, `Specification`), `Event` type (wrapping `v1.Envelope + json.RawMessage`), and `EmbedAction` types (AppendEmbedding, ReplaceEmbeddings, DeleteEmbeddings)
- [ ] 1.3 Define `Specification` type for declaring event interest (domains, stream kinds)

## 2. Consumer Loop

- [ ] 2.1 Implement `Indexer` struct with consumer loop: connect via gRPC, acquire lock via TryAcquire/KeepAlive, start streaming
- [ ] 2.2 Implement replay from consumer position using QueryBatchR2 with AfterID
- [ ] 2.3 Implement Watch-based continuous event processing
- [ ] 2.4 Integrate lock heartbeat with position piggyback (uses pgcqrs lock service from consumer-locks change)
- [ ] 2.5 Handle lock loss: stop processing, shut down cleanly

## 3. Vector Store

- [ ] 3.1 Define `Store` interface (Upsert, Search, DeleteByIdentity)
- [ ] 3.2 Implement PostgreSQL + VectorChord store with pre-dimensioned table selection based on encoder config
- [ ] 3.3 Implement `embedded_encoders` table CRUD
- [ ] 3.4 Implement search with vector similarity + metadata predicate filtering

## 4. gRPC Search Service

- [ ] 4.1 Define proto for search service within the framework
- [ ] 4.2 Implement search RPC handler (embed query text, vector search, return results)
- [ ] 4.3 Include lag information in search response (last indexed event ID)

## 5. Example Binary

- [ ] 5.1 Create example plugin that projects entity state over a stream (e.g., "incident" events)
- [ ] 5.2 Create example `main.go` that wires plugin, vector store, and serves gRPC search
- [ ] 5.3 Provide DDL SQL for creating dimension tables and encoders table

## 6. Tests

- [ ] 6.1 Write unit tests for EmbedAction processing (append, replace, delete)
- [ ] 6.2 Write unit tests for consumer loop with in-memory pgcqrs transport + lock
- [ ] 6.3 Write unit tests for vector store abstraction with in-memory backend
- [ ] 6.4 Run full test suite and verify all pass
