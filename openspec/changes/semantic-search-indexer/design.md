## Context

The pgcqrs project provides JSON event storage with multi-tenancy and query-by-structure (kind, property equality, JSON subset). There is no semantic search capability. External tools would like to index event content for free-text similarity search.

The consumer position tracking system already exists. A separate change (`consumer-locks`) adds durable distributed locks with heartbeat+position piggyback. This framework is the primary consumer of those locks.

## Goals / Non-Goals

**Goals:**
- Go library at `pkg/indexer/embedding` for building embedding indexers
- Plugin interface: users implement `Plugin` with `HandleEvent(event) → []EmbedAction`
- Consumer loop: connects via gRPC, acquires lock, watches/streams events, calls plugin
- Vector store: PostgreSQL + VectorChord, pre-created tables per dimension
- Encoder journaling: `embedded_encoders` table
- Optional gRPC search service: search(query, top_k, min_score, tolerance, predicates) → results with event_id + metadata
- Example binary showing the full flow

**Non-Goals:**
- Not a replacement for pgcqrs's built-in query system
- No HTTP endpoints (gRPC only)
- No web UI or dashboard
- No automatic migration of vector tables (user creates them with provided DDL)

## Decisions

**Decision: Framework is a Go library, not a gRPC service**
The user compiles the framework into their binary with their plugin. The framework optionally serves a gRPC search endpoint. This avoids needing a plugin protocol while keeping the door open for language-agnostic search clients.

**Decision: Plugin maintains its own projection state**
The `HandleEvent` callback receives one event at a time in order. The plugin maintains its own in-memory entity state. The framework does not manage plugin state — replay from consumer position naturally rebuilds it. This is simpler and more flexible than having the framework deserialize and pass state.

**Decision: EmbedAction types are separate messages (Append, Replace, Delete)**
Each action type has distinct semantics. Append adds a new embedding. Replace deletes all embeddings for the identity then inserts new ones. Delete removes all embeddings for the identity. This gives the plugin precise control over the vector lifecycle.

**Decision: Pre-created dimension-specific tables, not runtime DDL**
Tables for dimensions 384, 768, 1024, 2560, and 4096 are created by the user before running the indexer. The framework picks the right table based on the configured encoder. This avoids runtime `CREATE TABLE` and keeps schema changes in explicit migrations.

**Decision: Search returns event IDs only**
The search response includes event IDs and plugin-provided metadata. The client fetches full event bodies from pgcqrs separately if needed. This keeps the vector store focused on search and avoids duplicating event data.

**Decision: Deletion handling deferred**
PGCQRS core does not support event deletion. When/if it does, a `HandleDelete(identity)` callback can be added to the plugin interface. For now, the indexer only handles event creates.

## Risks / Trade-offs

- [Risk] Plugin projection state is lost on restart → Mitigation: replay from consumer position rebuilds state. This is the intended design.
- [Risk] Embedder API rate limits or failures stall the consumer loop → Mitigation: the plugin should handle embedder errors (retry, skip). The framework only fails on unhandled errors.
- [Risk] VectorChord requires PostgreSQL connection separate from pgcqrs → Mitigation: the framework accepts a separate `*pgxpool.Pool` for the vector store. This is explicit in the constructor.
- [Risk] Large embeddings (4096D) require significant storage → Mitigation: pre-created tables make the dimension choice visible. The user picks the encoder matching their cost/quality trade-off.

## Open Questions

- Should the framework support multiple indexes per stream (different encoders, different extractors)? Currently scoped to one encoder per indexer instance.
