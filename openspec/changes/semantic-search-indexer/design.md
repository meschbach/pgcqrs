## Context

The pgcqrs project provides JSON event storage with multi-tenancy and query-by-structure (kind, property equality, JSON subset). There is no semantic search capability. External tools would like to index event content for free-text similarity search.

The consumer position tracking system already exists. A separate change (`consumer-locks`) adds durable distributed locks with heartbeat+position piggyback. This framework is the primary consumer of those locks.

VectorChord is a PostgreSQL extension for vector search. It requires its own PostgreSQL instance (separate from pgcqrs) with the `vchord` extension installed. The framework targets this separate database for vector storage.

## Goals / Non-Goals

**Goals:**
- Go library at `pkg/indexer/embedding` for building embedding indexers
- Plugin interface: users implement `Plugin` with `HandleEvent(event) → []EmbedAction`
- Plugins provide pre-computed vectors (framework does not embed text during indexing)
- Consumer loop: connects via gRPC, acquires lock, watches/streams events, calls plugin
- Framework-owned database schema via embedded SQL migrations (golang-migrate pattern)
- Vector store: PostgreSQL + VectorChord, shared database across multiple indexer instances
- Normalized schema: `indexes`, `encoders`, `index_encoders` join table, dimension tables
- Auto-registration of encoders on first use (no pre-defined list)
- Multiple encoders supported concurrently per indexer
- gRPC search service: search(query, encoder, dimension, top_k, min_score, predicates) → results with event_id + metadata
- Migrator function for applying VectorChord DB schema on deploy
- Example binary showing the full flow

**Non-Goals:**
- Not a replacement for pgcqrs's built-in query system
- No HTTP endpoints (gRPC only)
- No web UI or dashboard
- No runtime DDL (all schema changes via migrations)
- No vector length validation (framework trusts plugin-provided vectors)

## Decisions

**Decision: Framework is a Go library, not a gRPC service**
The user compiles the framework into their binary with their plugin. The framework optionally serves a gRPC search endpoint. This avoids needing a plugin protocol while keeping the door open for language-agnostic search clients.

**Decision: Plugin maintains its own projection state**
The `HandleEvent` callback receives one event at a time in order. The plugin maintains its own in-memory entity state. The framework does not manage plugin state — replay from consumer position naturally rebuilds it. This is simpler and more flexible than having the framework deserialize and pass state.

**Decision: Plugin provides pre-computed vectors**
The plugin is responsible for invoking its own encoder and providing the vector in `EmbedAction`. The framework does not embed text during indexing. The plugin includes an `Encoder` name field in each action for journaling purposes. This keeps encoding logic in the plugin where it belongs, while the framework journals which encoder produced which embeddings.

**Decision: Framework does not validate vector length**
The framework does not validate that the vector length matches the encoder's expected dimension. The plugin is trusted to provide correct vectors. If the vector length does not match a supported dimension table (384, 768, 1024, 2560, 4096), the framework writes to the table matching the vector length. This keeps the framework simple and avoids false validation errors.

**Decision: EmbedAction types are separate messages (Append, Replace, Delete)**
Each action type has distinct semantics. Append adds a new embedding. Replace deletes all embeddings for the identity then inserts new ones. Delete removes all embeddings for the identity. This gives the plugin precise control over the vector lifecycle.

**Decision: Framework owns the schema via embedded SQL migrations**
The framework owns the VectorChord database schema. Migrations are embedded in the framework binary using `//go:embed` and applied via `golang-migrate` (matching the pgcqrs migrator pattern). The user does not create DDL manually. Down migrations unwind everything (drop tables, indexes, extensions).

**Decision: Separate migrator command/subcommand**
The migrator is exposed as a function (`MigrateVectorDB(ctx, dbURL)`) that the user calls from a subcommand or separate binary. This is triggered on deploy to apply schema changes before the indexer starts. Verification is implicit — if `migrator.Up()` succeeds, the schema is valid.

**Decision: Normalized schema with join tables**
The database uses a normalized schema with four tables: `indexes` (domain, stream, name), `encoders` (name only), `index_encoders` (join table linking indexes to encoders), and dimension tables (`embedded_documents_{N}`, `embedded_vectors_{N}`) referencing the join table. This provides referential integrity via foreign keys, avoids data duplication, and cleanly separates concerns.

**Decision: Encoders table is minimal (name only)**
The `encoders` table stores only `id`, `name`, and `created_at`. No dimension column — dimension is implied by the vector length at write time. No config column — the plugin manages its own encoder configuration. This keeps the table as a simple registry of "which encoder names have been seen."

**Decision: Index is implicit (created on first write)**
No validation or pre-creation of indexes. The index "exists" once data is written to the vector store. Search works once there is something to search. The normalized tables handle partitioning naturally via foreign keys.

**Decision: Consumer locks prevent duplicate pumps**
The consumer name encodes the full `{domain, stream, index}` triple. The consumer lock mechanism prevents multiple pump instances from processing the same index concurrently. This ensures exactly-once processing per index.

**Decision: Search requires encoder name and dimension**
The search request includes `encoder` and `dimension` parameters. The search handler uses `encoder` to look up the `index_encoders` join, and `dimension` to select the table. The client is responsible for knowing the dimension of the encoder they used. This keeps the framework simple and avoids storing dimension redundantly.

**Decision: Plugin provides query embedding for search**
The search handler calls `plugin.EmbedQuery(ctx, text, encoderName)` to embed the search query. The plugin handles all encoding logic. The framework does not need a separate encoder registry or constructor injection for search.

**Decision: Multiple encoders per indexer instance**
An indexer may use multiple encoders concurrently (e.g., 384D for summaries, 1024D for full text). Each `EmbedAction` specifies which encoder produced the vector. The framework writes to the appropriate dimension table based on the vector length.

**Decision: Search is scoped to a specific indexer**
Search requests target a specific indexer by `{domain, stream, index}`. The search handler embeds the query text using the specified encoder, searches the corresponding dimension table filtered by `index_encoders.id`, and returns results. This keeps search fast and focused.

**Decision: Search returns event IDs only**
The search response includes event IDs and plugin-provided metadata. The client fetches full event bodies from pgcqrs separately if needed. This keeps the vector store focused on search and avoids duplicating event data.

**Decision: Deletion handling deferred**
PGCQRS core does not support event deletion. When/if it does, a `HandleDelete(identity)` callback can be added to the plugin interface. For now, the indexer only handles event creates.

**Decision: Wire interface with generic Lock for bidi heartbeating**
The indexer framework uses a `Wire[L Lock]` interface where `Lock` is a generic type parameter constrained by the `Lock` interface (matching gRPC's `KeepAlive` methods: `Heartbeat(ctx, position int64) error` and `Release(ctx) error`). This ensures bidi stream behavior from gRPC for efficiency while allowing memory transport to use a simpler implementation.

The `Wire` interface exposes both `TryAcquire` (returns `*LockResult`) and `NewKeepAlive` (returns `L` satisfying `Lock`). These are separate operations: `TryAcquire` acquires the lock, `NewKeepAlive` opens the heartbeating mechanism. This matches gRPC's current API design.

**Decision: Holder identity is a required parameter**
The `holder` parameter is required when creating a pump. The caller provides the instance identity (e.g., Kubernetes pod name, hostname). This is explicit — the caller knows best what identity to use for their environment.

**Decision: Expand memory transport to match Wire interface**
The memory transport will be expanded to implement the `Wire` interface with a `MemoryLock` type that satisfies the `Lock` interface. This allows both gRPC and memory to be used interchangeably with the indexer framework.

**Decision: Use Watch for event streaming**
The pump uses `Watch` with `afterID` for both replay and live streaming. This is simpler and more responsive than a two-phase approach (QueryBatchR2 then Watch). The Watch API handles replay of existing events and transitions to live streaming automatically.

**Decision: Position advances immediately after each event**
Position tracks the last fully processed event ID. The framework advances position immediately after each event is processed (including vector store write). This provides exactly-once semantics — the position reflects what has been fully processed, not just received.

**Decision: Plugin errors propagate to caller**
If the plugin returns an error from `HandleEvent`, the framework stops processing and returns the error to the caller. The caller decides how to handle the failure (retry, reconnect, abort). The framework does not retry or skip events.

**Decision: Heartbeat timing is configurable**
Heartbeats are sent based on the lock's `GuaranteeUntil` timestamp returned by `TryAcquire`. The client heartbeats before this deadline. The heartbeat margin is configurable via the Option pattern on the pump constructor (default: 90% of TTL, matching the server's `GuaranteeUntil` calculation).

## Schema

### Tables

```sql
-- Indexes: {domain, stream, name} tuple
CREATE TABLE indexes (
    id          BIGSERIAL PRIMARY KEY,
    domain      TEXT NOT NULL,
    stream      TEXT NOT NULL,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT now(),
    UNIQUE(domain, stream, name)
);

-- Encoders: minimal registry (name only)
CREATE TABLE encoders (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ DEFAULT now()
);

-- Index-Encoder join: the four-part relationship
CREATE TABLE index_encoders (
    id          BIGSERIAL PRIMARY KEY,
    index_id    BIGINT REFERENCES indexes(id) ON DELETE CASCADE,
    encoder_id  BIGINT REFERENCES encoders(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ DEFAULT now(),
    UNIQUE(index_id, encoder_id)
);

-- Dimension tables (384 example, repeat for 768, 1024, 2560, 4096)
CREATE TABLE embedded_documents_384 (
    id                  BIGSERIAL PRIMARY KEY,
    index_encoder_id    BIGINT REFERENCES index_encoders(id) ON DELETE CASCADE,
    identity            TEXT NOT NULL,
    text                TEXT NOT NULL,
    metadata            JSONB DEFAULT '{}',
    created_at          TIMESTAMPTZ DEFAULT now(),
    UNIQUE(index_encoder_id, identity)
);

CREATE TABLE embedded_vectors_384 (
    id                  BIGSERIAL PRIMARY KEY,
    index_encoder_id    BIGINT REFERENCES index_encoders(id) ON DELETE CASCADE,
    document_id         BIGINT REFERENCES embedded_documents_384(id) ON DELETE CASCADE,
    vector              vector(384) NOT NULL,
    created_at          TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX ON embedded_vectors_384 USING vchordrq (vector vector_l2_ops);
```

### Down Migrations

Down migrations drop everything: dimension tables, `index_encoders`, `encoders`, `indexes`, extensions. No data preservation on downgrade.

## Risks / Trade-offs

- [Risk] Plugin projection state is lost on restart → Mitigation: replay from Watch with afterID rebuilds state. This is the intended design.
- [Risk] Embedder API rate limits or failures stall the consumer loop → Mitigation: plugin returns error, framework stops and returns to caller. Caller decides how to handle (retry, reconnect, abort).
- [Risk] VectorChord requires PostgreSQL connection separate from pgcqrs → Mitigation: the framework accepts a separate `*pgxpool.Pool` for the vector store. This is explicit in the constructor.
- [Risk] Large embeddings (4096D) require significant storage → Mitigation: pre-dimensioned tables make the dimension choice visible. The user picks the encoder matching their cost/quality trade-off.
- [Risk] Position advances before vector store write confirms → Mitigation: position advances immediately after event processing (including vector store write). Crash between processing and position update causes re-delivery on restart (at-least-once delivery, exactly-once processing).
- [Risk] Multiple indexers sharing a database could cause contention → Mitigation: normalized schema with foreign keys provides clean partitioning. VectorChord handles concurrent writes. Consumer locks prevent duplicate pumps.
- [Risk] Search requires encoder availability for query embedding → Mitigation: plugin implements `EmbedQuery` method. Search handler calls plugin directly. No separate encoder registry needed.
- [Risk] Plugin provides incorrect vector length → Mitigation: framework does not validate. Plugin is trusted. If vector length is wrong, search results will be incorrect — this is a plugin bug, not a framework bug.

## Open Questions

- ~~Should the framework support multiple indexes per stream (different encoders, different extractors)?~~ **Decided:** Yes — multiple encoders per indexer are supported. Each `EmbedAction` specifies which encoder produced the vector. Search targets a specific encoder.

- ~~Heartbeat timing defaults?~~ **Decided:** Heartbeat interval derived from `GuaranteeUntil` timestamp in `LockResult` (server-calculated deadline). Client heartbeats before this deadline. Configurable via Option pattern.

- ~~Who owns the VectorChord database schema?~~ **Decided:** Framework owns it via embedded SQL migrations. User does not create DDL manually. Migrator applied on deploy.

- ~~How are encoders registered?~~ **Decided:** Auto-registration on first use. No pre-defined list. Encoder name included in each `EmbedAction` for journaling.

- ~~Can multiple pumps process the same index concurrently?~~ **Decided:** No. Consumer locks prevent this. Consumer name encodes the full `{domain, stream, index}` triple.

- ~~How does the search service embed queries?~~ **Decided:** Plugin implements `EmbedQuery(ctx, text, encoderName)`. Search handler calls plugin directly. No separate encoder registry.

- ~~Should the encoders table store dimension?~~ **Decided:** No. Dimension is implied by vector length at write time. Client specifies dimension at search time.

- ~~Should the encoders table store config?~~ **Decided:** No. Plugin manages its own encoder configuration. Framework only journals encoder names.

## Review Findings

### Query2: OnID + After semantics (confusion noted)

During review, confusion arose about combining `OnID` and `After` in query2:

- `OnID(42)` = "I want this specific event"
- `After(50)` = "I want events after this position"

These are fundamentally different operations. The server-side code correctly ignores `afterID` for `OnID` clauses — explicit ID lookups shouldn't be filtered by position. If you want both specific events AND events after a position, you need two separate queries.

**Intent:** The proto (`QueryIn`) is frozen/deprecated. The query2 package defines expectations. query2 currently allows combining `OnID` and `After`, which silently ignores `After` for `OnID` — this is confusing and should be addressed.

**Decision:** Document that `After` does not apply to `OnID` queries. Do not error when both are set.
