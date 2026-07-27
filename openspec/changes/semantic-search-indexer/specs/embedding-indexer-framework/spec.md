## ADDED Requirements

### Requirement: Plugin can declare event interest
The plugin SHALL provide a specification of which events it wants to process, including which domains and which event kinds.

#### Scenario: Plugin declares interest in specific kinds
- **WHEN** a plugin specifies interest in domain "incidents", kind "IncidentCreated"
- **THEN** the framework only invokes HandleEvent for events matching those criteria

### Requirement: Plugin receives events in order
The framework SHALL deliver events to the plugin in the order they were stored in the stream, one at a time.

#### Scenario: Events delivered sequentially
- **GIVEN** a stream with events at IDs [100, 101, 102]
- **WHEN** the framework replays from position 99
- **THEN** HandleEvent is called in order for events 100, 101, 102

### Requirement: Plugin provides pre-computed vectors
The plugin SHALL invoke its own encoder and provide the resulting vector in each EmbedAction. The framework SHALL NOT embed text during indexing. The framework SHALL NOT validate vector length. The EmbedAction SHALL include an `Encoder` name field for journaling purposes.

#### Scenario: Plugin provides vector in AppendEmbedding
- **WHEN** HandleEvent returns AppendEmbedding with encoder="bge-small", identity="incident-42", vector=[0.12, -0.34, ...], text="critical failure in auth", metadata={"severity":"high"}
- **THEN** the framework stores the vector row linked to the identity and journals the encoder name

#### Scenario: Framework does not validate vector length
- **WHEN** HandleEvent returns AppendEmbedding with a vector of any length
- **THEN** the framework writes to the dimension table matching the vector length without validation

### Requirement: Plugin can append an embedding
The plugin SHALL be able to return an AppendEmbedding action, which adds a new vector for the given identity without affecting existing vectors for that identity.

#### Scenario: Plugin appends embedding for entity
- **WHEN** HandleEvent returns AppendEmbedding with encoder="bge-small", identity="incident-42", vector=[...], text="critical failure in auth", metadata={"severity":"high"}
- **THEN** the framework stores a new vector row linked to the identity

### Requirement: Plugin can replace embeddings for an identity
The plugin SHALL be able to return a ReplaceEmbeddings action, which removes all existing vectors for the given identity and inserts new ones.

#### Scenario: Plugin replaces all embeddings for entity
- **GIVEN** identity "incident-42" has 3 existing vectors
- **WHEN** HandleEvent returns ReplaceEmbeddings with identity="incident-42", vector=[...], text="updated description"
- **THEN** the framework removes all 3 previous vectors and stores 1 new vector

### Requirement: Plugin can delete embeddings for an identity
The plugin SHALL be able to return a DeleteEmbeddings action, which removes all existing vectors for the given identity.

#### Scenario: Plugin deletes all embeddings for entity
- **GIVEN** identity "incident-42" has 2 existing vectors
- **WHEN** HandleEvent returns DeleteEmbeddings with identity="incident-42"
- **THEN** the framework removes all vectors for identity "incident-42"

### Requirement: Framework owns database schema via migrations
The framework SHALL own the VectorChord database schema. Migrations SHALL be embedded in the framework binary and applied via `golang-migrate`. The user SHALL NOT create DDL manually. Down migrations SHALL unwind everything (drop tables, indexes, extensions).

#### Scenario: Migrator applies schema on deploy
- **WHEN** the user runs the migrator subcommand with a VectorChord DB URL
- **THEN** the framework applies all migrations, creating extensions, tables, and VectorChord indexes

#### Scenario: Migrator is idempotent
- **WHEN** the migrator is run and schema is already at the latest version
- **THEN** the migrator reports "no change" and exits successfully

#### Scenario: Down migration unwinds everything
- **WHEN** the user runs the down migration
- **THEN** all tables, indexes, and extensions are dropped

### Requirement: Normalized schema with join tables
The database SHALL use a normalized schema: `indexes` (domain, stream, name), `encoders` (name only), `index_encoders` (join table), and dimension tables (`embedded_documents_{N}`, `embedded_vectors_{N}`) referencing the join table.

#### Scenario: Indexes table stores index identity
- **WHEN** the framework writes an embedding for domain="prod", stream="events", index="semantic"
- **THEN** a row exists in `indexes` with domain="prod", stream="events", name="semantic"

#### Scenario: Encoders table stores encoder names
- **WHEN** the framework encounters encoder name "bge-small" for the first time
- **THEN** a row exists in `encoders` with name="bge-small"

#### Scenario: Join table links index to encoder
- **WHEN** the framework writes an embedding for index "prod.events.semantic" with encoder "bge-small"
- **THEN** a row exists in `index_encoders` linking the index to the encoder

#### Scenario: Dimension tables reference join table
- **WHEN** the framework writes a vector for a 384D encoder
- **THEN** the vector row in `embedded_vectors_384` references `index_encoders(id)` via `index_encoder_id`

### Requirement: Encoder auto-registration on first use
The framework SHALL auto-register encoders in the `encoders` table when they are first encountered in an EmbedAction. No pre-defined list of encoders is required. The `encoders` table SHALL store only `id`, `name`, and `created_at` — no dimension or config columns.

#### Scenario: First use of encoder registers it
- **WHEN** the framework receives an EmbedAction with encoder="bge-small" for the first time
- **THEN** the framework inserts a row into `encoders` with name="bge-small"

#### Scenario: Subsequent use does not re-register
- **GIVEN** encoder "bge-small" is already registered
- **WHEN** the framework receives another EmbedAction with encoder="bge-small"
- **THEN** no insert occurs (encoder already registered)

### Requirement: Multiple encoders per indexer
An indexer instance SHALL support multiple encoders concurrently. Each EmbedAction SHALL specify which encoder produced the vector. The framework SHALL write to the appropriate dimension table based on the vector length.

#### Scenario: Indexer uses multiple encoders
- **WHEN** a plugin returns AppendEmbedding with encoder="bge-small" (384D vector) and AppendEmbedding with encoder="bge-large" (1024D vector)
- **THEN** the framework writes to `embedded_vectors_384` for the first action and `embedded_vectors_1024` for the second

### Requirement: Index is implicit (created on first write)
No validation or pre-creation of indexes is required. The index "exists" once data is written to the vector store. Search works once there is something to search.

#### Scenario: First write creates the index implicitly
- **WHEN** the framework writes the first embedding for a new index
- **THEN** rows appear in `indexes`, `encoders`, `index_encoders`, and the appropriate dimension tables

### Requirement: Consumer locks prevent duplicate pumps
The consumer name SHALL encode the full `{domain, stream, index}` triple. The consumer lock mechanism SHALL prevent multiple pump instances from processing the same index concurrently.

#### Scenario: Lock prevents duplicate processing
- **GIVEN** Pump A holds the lock for consumer "idx-prod.events.semantic"
- **WHEN** Pump B tries to acquire the same consumer
- **THEN** the lock acquisition is denied

### Requirement: Indexer acquires lock before consuming events
The framework SHALL acquire a consumer lock (via pgcqrs TryAcquire) before beginning event consumption and SHALL maintain the lock throughout processing via heartbeats (via NewKeepAlive).

#### Scenario: Indexer acquires lock on startup
- **WHEN** the framework starts for consumer "idx-prod.events.semantic", domain "prod", stream "events"
- **THEN** it calls TryAcquire on pgcqrs and only begins processing if the lock is acquired

#### Scenario: Indexer opens heartbeater after lock acquisition
- **GIVEN** the framework has acquired the lock via TryAcquire
- **WHEN** the framework calls NewKeepAlive
- **THEN** it receives a Lock handle for heartbeating and releases the lock on shutdown

#### Scenario: Indexer stops processing on lock loss
- **GIVEN** the framework holds a lock via KeepAlive stream
- **WHEN** the server responds with locked=false, reason="stolen"
- **THEN** the framework stops event processing and returns error to caller

### Requirement: Indexer advances position after each event
The framework SHALL advance the consumer position immediately after each event is processed, including the vector store write.

#### Scenario: Position advances after event processing
- **GIVEN** the framework has processed event ID 250
- **WHEN** the event processing completes (including vector store write)
- **THEN** the internal position tracker records 250 as the last processed event ID

#### Scenario: Position advances with heartbeat
- **GIVEN** the framework has processed events up to ID 250
- **WHEN** the framework sends a Heartbeat on the KeepAlive stream
- **THEN** the heartbeat includes position=250

### Requirement: Plugin errors stop processing
The framework SHALL stop processing and return the error to the caller when the plugin returns an error from HandleEvent.

#### Scenario: Plugin error stops processing
- **GIVEN** the framework is processing event ID 100
- **WHEN** HandleEvent returns an error
- **THEN** the framework stops event processing and returns the error to the caller

### Requirement: Plugin provides query embedding for search
The plugin SHALL implement an `EmbedQuery(ctx, text, encoderName)` method that embeds search query text using the specified encoder. The search handler SHALL call this method to embed queries. The framework SHALL NOT have a separate encoder registry for search.

#### Scenario: Search handler calls plugin for query embedding
- **WHEN** a search request arrives with query="authentication failure", encoder="bge-small"
- **THEN** the search handler calls `plugin.EmbedQuery(ctx, "authentication failure", "bge-small")` and receives a vector

### Requirement: Search is scoped to a specific indexer
The search service SHALL accept requests targeting a specific indexer by `{domain, stream, index}`. The search handler SHALL embed the query text using the plugin, search the corresponding dimension table filtered by `index_encoders.id`, and return results.

#### Scenario: Search targets specific indexer
- **WHEN** a client searches with domain="prod", stream="events", index="semantic", encoder="bge-small", dimension=384, query="authentication failure", top_k=5
- **THEN** the service searches `embedded_vectors_384` via the `index_encoders` join and returns up to 5 results

### Requirement: Search requires encoder name and dimension
The search request SHALL include `encoder` and `dimension` parameters. The search handler SHALL use `encoder` to look up the `index_encoders` join, and `dimension` to select the table. The client is responsible for knowing the dimension of the encoder they used.

#### Scenario: Search uses specified encoder and dimension
- **WHEN** a client searches with encoder="bge-small", dimension=384
- **THEN** the service looks up `index_encoders` for the encoder and searches `embedded_vectors_384`

#### Scenario: Search fails for unknown encoder
- **WHEN** a client searches with encoder="unknown-encoder"
- **THEN** the service returns an error indicating the encoder is not registered

### Requirement: Search returns matching events
The search service SHALL accept a free-text query, embed it using the plugin, perform a vector similarity search, and return results including event IDs and plugin-provided metadata.

#### Scenario: Search returns top matches
- **WHEN** a client searches with query="authentication failure", encoder="bge-small", dimension=384, top_k=5
- **THEN** the service returns up to 5 results, each with event_id, score, and metadata

#### Scenario: Search with metadata predicates
- **WHEN** a client searches with query="failure", encoder="bge-small", dimension=384, and predicates=[{"severity": "critical"}]
- **THEN** results are filtered to only include vectors whose metadata matches the predicates

### Requirement: Search lag is reported
The search response SHALL include information about how current the index is relative to the stream, specifically the last processed event ID and the last known stream event ID.

#### Scenario: Search reports current lag
- **WHEN** a client calls Search
- **THEN** the response includes lastIndexedEventID and optional hint about staleness
