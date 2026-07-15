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

### Requirement: Plugin can append an embedding
The plugin SHALL be able to return an AppendEmbedding action, which adds a new vector for the given identity without affecting existing vectors for that identity.

#### Scenario: Plugin appends embedding for entity
- **WHEN** HandleEvent returns AppendEmbedding with identity="incident-42", text="critical failure in auth", metadata={"severity":"high"}
- **THEN** the framework stores a new vector row linked to the identity

### Requirement: Plugin can replace embeddings for an identity
The plugin SHALL be able to return a ReplaceEmbeddings action, which removes all existing vectors for the given identity and inserts new ones.

#### Scenario: Plugin replaces all embeddings for entity
- **GIVEN** identity "incident-42" has 3 existing vectors
- **WHEN** HandleEvent returns ReplaceEmbeddings with identity="incident-42", text="updated description"
- **THEN** the framework removes all 3 previous vectors and stores 1 new vector

### Requirement: Plugin can delete embeddings for an identity
The plugin SHALL be able to return a DeleteEmbeddings action, which removes all existing vectors for the given identity.

#### Scenario: Plugin deletes all embeddings for entity
- **GIVEN** identity "incident-42" has 2 existing vectors
- **WHEN** HandleEvent returns DeleteEmbeddings with identity="incident-42"
- **THEN** the framework removes all vectors for identity "incident-42"

### Requirement: Indexer acquires lock before consuming events
The framework SHALL acquire a consumer lock (via pgcqrs TryAcquire or KeepAlive) before beginning event consumption and SHALL maintain the lock throughout processing.

#### Scenario: Indexer acquires lock on startup
- **WHEN** the framework starts for consumer "search-index", domain "prod", stream "events"
- **THEN** it calls TryAcquire on pgcqrs and only begins processing if the lock is acquired

#### Scenario: Indexer stops processing on lock loss
- **GIVEN** the framework holds a lock via KeepAlive stream
- **WHEN** the server responds with locked=false, reason="stolen"
- **THEN** the framework stops event processing and shuts down the consumer loop

### Requirement: Indexer advances position on heartbeat
The framework SHALL include the latest processed event ID in each heartbeat sent to pgcqrs, enabling atomic lock renewal and position advancement.

#### Scenario: Position advances with heartbeat
- **GIVEN** the framework has processed events up to ID 250
- **WHEN** the framework sends a Heartbeat on the KeepAlive stream
- **THEN** the heartbeat includes position=250

### Requirement: Search returns matching events
The search service SHALL accept a free-text query, embed it using the configured encoder, perform a vector similarity search, and return results including event IDs and plugin-provided metadata.

#### Scenario: Search returns top matches
- **WHEN** a client searches with query="authentication failure", top_k=5
- **THEN** the service returns up to 5 results, each with event_id, score, and metadata

#### Scenario: Search with metadata predicates
- **WHEN** a client searches with query="failure" and predicates=[{"severity": "critical"}]
- **THEN** results are filtered to only include vectors whose metadata matches the predicates

### Requirement: Search lag is reported
The search response SHALL include information about how current the index is relative to the stream, specifically the last processed event ID and the last known stream event ID.

#### Scenario: Search reports current lag
- **WHEN** a client calls Search
- **THEN** the response includes lastIndexedEventID and optional hint about staleness
