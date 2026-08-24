# ViewProjection Framework

## Purpose

The core indexer framework (`pkg/indexer/`) provides a shared Pump loop that drives event processing for any indexer type. The Pump acquires consumer locks, resumes from stored positions, watches event streams via `query2.Query`, dispatches to handlers, and heartbeats position — all running in the developer's process.

The views indexer (`pkg/indexer/views/`) builds on the core framework to provide a KV-backed materialized view system. It handles typed per-kind handlers, previous-state retrieval, atomic mutations, version-aware reads, and change notification.

The pgcqrs service provides two narrow gRPC services: `ViewProjectionStore` for KV storage operations (mutations and reads used by the Pump) and `ViewProjectionConsumer` for remote access (GetEntity, GetVersion, WatchChanges for webapps and downstream indexers).

Developer experience is a first-class concern: define a projection with typed handlers, run it, query it. Same projection works with in-memory storage for tests, PostgreSQL for production, and gRPC for remote access.

## Requirements

### Requirement: Core Indexer interface is one method
The system SHALL provide an `Indexer` interface with a single method `Query() *query2.Query`. Each indexer type builds a query2.Query with handlers registered. The Pump drives the query's Watch loop. No generics, no proto coupling in the core.

#### Scenario: Views indexer implements Indexer
- **GIVEN** a views indexer with OnKind[T] handlers registered
- **WHEN** `Query()` is called
- **THEN** it returns a `*query2.Query` with handlers registered for each OnKind kind
- **AND** the handlers dispatch to OnKind[T] functions, apply mutations to the Store, and notify observers

### Requirement: Pump drives the event processing loop
The system SHALL provide a `Pump` that manages the event processing loop: acquire consumer lock, resume from stored position, build query with AfterID, watch events via `query2.Watch.TickWithID`, and heartbeat position after each event. The Pump runs in the developer's process.

#### Scenario: Pump acquires lock and starts processing
- **GIVEN** a Pump with a Wire and Indexer
- **WHEN** `Pump.Run(ctx)` is called
- **THEN** it acquires a consumer lock via Wire.TryAcquire
- **AND** retrieves the stored position via Wire.GetPosition
- **AND** calls `indexer.Query()` to build the query
- **AND** sets AfterID on the query from the stored position
- **AND** starts the Watch loop

#### Scenario: Pump heartbeats after each event
- **GIVEN** a Pump processing events
- **WHEN** `query2.Watch.TickWithID(ctx)` returns event ID 500
- **THEN** the Pump calls `keepAlive.Heartbeat(ctx, 500)` to update position
- **AND** advances to the next event

#### Scenario: Pump resumes from last position
- **GIVEN** a Pump that previously processed events up to ID 500
- **WHEN** the Pump restarts
- **THEN** it retrieves position 500 from Wire.GetPosition
- **AND** sets AfterID=500 on the query
- **AND** resumes from event ID 501

#### Scenario: Pump stops on handler error
- **GIVEN** a Pump processing events
- **WHEN** a handler returns an error during TickWithID dispatch
- **THEN** the Pump stops processing
- **AND** returns the error to the caller
- **AND** the position reflects the last successfully processed event

#### Scenario: Pump handles lock loss
- **GIVEN** a Pump processing events
- **WHEN** the consumer lock expires (heartbeat failure)
- **THEN** the Pump stops processing and returns an error

### Requirement: query2.Watch.TickWithID exposes event ID
The system SHALL provide a `TickWithID(ctx context.Context) (int64, error)` method on `query2.Watch` that dispatches to the registered handler and returns the event ID. This enables the Pump to heartbeat with the event ID without dropping to WatchInternal.

#### Scenario: TickWithID returns event ID after dispatch
- **GIVEN** a query2.Watch with handlers registered
- **WHEN** an event with ID 42 arrives
- **THEN** TickWithID dispatches to the correct handler
- **AND** returns `(42, nil)` after the handler completes

#### Scenario: TickWithID returns error from handler
- **GIVEN** a query2.Watch with a handler that returns an error
- **WHEN** the handler returns `fmt.Errorf("processing failed")`
- **THEN** TickWithID returns `(0, error)` with the handler's error

### Requirement: OnKind[T] registers typed per-kind handlers
The system SHALL allow developers to register typed handler functions for specific event kinds. Each handler receives the typed event struct (automatically unmarshaled from JSON) and an ReduceContext for previous-state retrieval.

#### Scenario: Handler receives typed event
- **GIVEN** a projection with `OnKind("ItemCreated", handler)` where handler expects `*ItemCreated`
- **WHEN** an "ItemCreated" event arrives with body `{"itemID": "item-42", "name": "Widget"}`
- **THEN** the handler receives a pointer to `ItemCreated{ItemID: "item-42", Name: "Widget"}`

#### Scenario: Handler retrieves previous state
- **GIVEN** a projection with `OnKind("ItemUpdated", handler)` that calls `actx.Get("items", views.Key(itemID))`
- **WHEN** an "ItemUpdated" event arrives for an existing entity
- **THEN** the handler receives the current entity state via `actx.Get`
- **AND** can merge the update with the previous state

#### Scenario: Handler ignores previous state
- **GIVEN** a projection with `OnKind("ItemCreated", handler)` that does not call `actx.Get`
- **WHEN** an "ItemCreated" event arrives
- **THEN** the handler processes the event without accessing previous state
- **AND** no unnecessary Store query is performed

### Requirement: Key type supports single and composite keys
The system SHALL provide a variadic Key type that supports 1 or 2 string parts. Simple entities use a single key; composite entities use two. Panics if more than 2 parts are provided (programmer error).

#### Scenario: Single key
- **GIVEN** an Upsert for a simple entity
- **WHEN** the handler creates `views.Key("item-42")`
- **THEN** the Key has one part

#### Scenario: Composite key
- **GIVEN** an Upsert for a location-indexed entity
- **WHEN** the handler creates `views.Key("item-42", "location-A")`
- **THEN** the Key has two parts

#### Scenario: Too many keys panics
- **GIVEN** a handler that creates `views.Key("a", "b", "c")`
- **WHEN** the Key constructor is called
- **THEN** a panic occurs with message "views.Key: expected 1 or 2 key parts, got 3"

### Requirement: Upsert.Value accepts any, framework marshals to JSON
The system SHALL accept `any` for Upsert.Value and marshal to JSON internally. This keeps handlers clean — they pass typed structs directly without manual json.Marshal.

#### Scenario: Handler passes typed struct
- **GIVEN** an Apply handler returning an Upsert
- **WHEN** the handler sets `Value: evt` where evt is `*ItemCreated`
- **THEN** the framework marshals the struct to JSON for storage

#### Scenario: Handler passes pre-marshaled JSON
- **GIVEN** an Apply handler with existing `json.RawMessage`
- **WHEN** the handler sets `Value: rawJSON`
- **THEN** the framework stores the JSON (no-op marshal)

### Requirement: Atomic mutations from single event
The system SHALL apply all mutations from a single event atomically in a single database transaction. Consumers never see partial updates from a single event.

#### Scenario: Multiple mutations applied atomically
- **GIVEN** an Apply handler that returns 2 Upserts and 1 Delete from a single event
- **WHEN** the ViewsIndexer processes the event
- **THEN** all 3 mutations are applied in a single transaction via ViewProjectionStore.ApplyMutations
- **AND** consumers see all mutations or none (atomic)

### Requirement: Storage in pgcqrs database
The system SHALL store projected entity state in pgcqrs via normalized tables: `view_projection_names`, `view_projection_kinds`, `view_projection_entries_single`, and `view_projection_entries_composite`. This enables durable, queryable materialized views with deduplicated text columns. Single-key entities use `view_projection_entries_single`; composite-key entities use `view_projection_entries_composite`. The `Key.Parts()` length determines routing. Projection names are scoped to `{domain, stream}` via `stream_id` FK to `events_stream`, ensuring separate KV storage for projections with the same name on different streams.

#### Scenario: Upsert stores entity state
- **GIVEN** an Apply handler that returns an Upsert
- **WHEN** the mutation is applied via ViewProjectionStore.ApplyMutations
- **THEN** the entity state is stored in the appropriate table (`view_projection_entries_single` or `view_projection_entries_composite`) with the correct projection_id, kind_id, key, value, and version

#### Scenario: Delete removes entity state
- **GIVEN** an entity in `view_projection_entries_single` or `view_projection_entries_composite`
- **WHEN** a Delete mutation is applied via ViewProjectionStore.ApplyMutations
- **THEN** the entity is removed from the appropriate table

#### Scenario: Version tracks last processed event
- **GIVEN** a Pump that processed events up to ID 500
- **WHEN** the version is queried via ViewProjectionConsumer.GetVersion
- **THEN** the version is 500 (from `consumer_positions` where consumer name = projection name)

#### Scenario: Projections with same name on different streams get separate storage
- **GIVEN** two projections with `ConsumerName("inventory")` on different `{domain, stream}` pairs
- **WHEN** both projections process events
- **THEN** each projection's entity state is stored separately (different `stream_id` in `view_projection_names`)
- **AND** the projections do not share KV storage

### Requirement: Unified Get with version constraints
The system SHALL provide a unified Get method returning `(entity, result, error)`. The result contains version constraint status. Developers discard what they don't need.

#### Scenario: Get entity at current version
- **GIVEN** an entity at version 500
- **WHEN** consumer calls `client.Get(ctx, "items", key)`
- **THEN** entity is returned, result has Status=StatusOK, err is nil

#### Scenario: Get entity with After constraint — satisfied
- **GIVEN** an entity at version 600
- **WHEN** consumer calls `client.Get(ctx, "items", key, views.After(500))`
- **THEN** entity is returned, result has Status=StatusOK

#### Scenario: Get entity with After constraint — stale
- **GIVEN** an entity at version 400
- **WHEN** consumer calls `client.Get(ctx, "items", key, views.After(500))`
- **THEN** entity is returned (current state), result has Status=StatusStale

#### Scenario: Get entity with UntilVersion constraint — satisfied
- **GIVEN** an entity at version 400, projection at version 450
- **WHEN** consumer calls `client.Get(ctx, "items", key, views.UntilVersion(500, 200*time.Millisecond))`
- **THEN** the Client subscribes to the local Notifier and waits for the projection to reach version 500
- **AND** on each change notification, the Client checks the projection version
- **AND** entity is returned with Status=StatusOK when version 500 is reached

#### Scenario: Get entity with UntilVersion constraint — timeout
- **GIVEN** an entity at version 400, projection at version 450
- **WHEN** consumer calls `client.Get(ctx, "items", key, views.UntilVersion(500, 200*time.Millisecond))`
- **AND** projection does not reach version 500 within 200ms
- **THEN** entity is returned (current state), result has Status=StatusTimeout

#### Scenario: Get nonexistent entity
- **GIVEN** no entity at the specified key
- **WHEN** consumer calls `client.Get(ctx, "items", key)`
- **THEN** entity is nil, result has Status=StatusNotFound, err is nil

#### Scenario: Discard result when not needed
- **GIVEN** a consumer that does not need version awareness
- **WHEN** consumer calls `entity, _, err := client.Get(ctx, "items", key)`
- **THEN** the result is discarded and the consumer uses the entity directly

### Requirement: Projection name defaults to consumer name
The system SHALL use the projection name as the consumer name by default. An optional ConsumerName override is available for migration scenarios.

#### Scenario: Default consumer name
- **GIVEN** a projection with `NewProjection("inventory", "items")`
- **WHEN** the Pump starts
- **THEN** the consumer name is "inventory"

#### Scenario: Override consumer name
- **GIVEN** a projection with `NewProjection("inventory", "items", ConsumerName("inventory-v2"))`
- **WHEN** the Pump starts
- **THEN** the consumer name is "inventory-v2"

### Requirement: Streaming API via OnChange
The system SHALL support streaming change notifications via OnChange. Consumers register for changes and receive batched upserts and deletes from each event. OnChange returns an unsubscribe function that removes the callback when called.

#### Scenario: Consumer receives batched changes
- **GIVEN** a consumer registered via `client.OnChange(fn)`
- **WHEN** an event produces 2 Upserts and 1 Delete
- **THEN** the consumer receives a Change with Upserts (2 items), Deletes (1 item), and Version (event ID)

#### Scenario: Consumer can unsubscribe
- **GIVEN** a consumer registered via `client.OnChange(fn)` that returns `unsub`
- **WHEN** `unsub()` is called
- **THEN** the callback is removed
- **AND** subsequent changes do not notify the consumer

#### Scenario: Observer notification is best-effort
- **GIVEN** a consumer registered via `client.OnChange(fn)`
- **WHEN** the ViewsIndexer processes an event
- **THEN** the position is heartbeated before observers are notified
- **AND** observer delivery is best-effort (no guarantee)

### Requirement: gRPC ViewProjectionStore in pgcqrs service
The system SHALL provide a gRPC ViewProjectionStore service in the existing pgcqrs service process for KV storage operations. The Pump calls ApplyMutations to write mutations and GetEntity to read previous state.

#### Scenario: Pump applies mutations via gRPC
- **GIVEN** a Pump processing events in the developer's process
- **WHEN** a handler returns an ReduceResult with upserts and deletes
- **THEN** the ViewsIndexer calls ViewProjectionStore.ApplyMutations via gRPC
- **AND** mutations are applied atomically in the pgcqrs database

#### Scenario: Handler reads previous state via gRPC
- **GIVEN** a handler that calls `actx.Get("items", key)`
- **WHEN** the handler executes during event processing
- **THEN** the ViewsIndexer calls ViewProjectionStore.GetEntity via gRPC
- **AND** returns the current entity state to the handler

### Requirement: gRPC ViewProjectionConsumer in pgcqrs service
The system SHALL provide a gRPC ViewProjectionConsumer in the existing pgcqrs service process for remote access to projections. The ViewProjectionStore writes state to the database; the Consumer reads from the same storage.

#### Scenario: Remote client gets entity
- **GIVEN** a webapp client connected to the gRPC ViewProjectionConsumer
- **WHEN** client calls `GetEntity(projection="inventory", kind="items", key=["item-42"])`
- **THEN** the entity state is returned with kind, key, value, and version

#### Scenario: Remote client gets entity with UntilVersion constraint
- **GIVEN** a webapp client connected to the gRPC ViewProjectionConsumer
- **WHEN** client calls `GetEntity` with `UntilVersionConstraint(version=500, timeout_ms=200)`
- **THEN** the service subscribes to the projection change bus and waits for the projection to reach version 500
- **AND** on each change notification, the service checks the projection version
- **AND** returns the entity with Status=StatusOK when version 500 is reached, or Status=StatusTimeout after 200ms

#### Scenario: Remote client watches changes
- **GIVEN** a webapp client connected to the gRPC ViewProjectionConsumer
- **WHEN** client calls `WatchChanges(projection="inventory")`
- **THEN** the client receives a stream of WatchChangesResponse with batched Upserts and Deletes

#### Scenario: Remote client watches changes with after_version
- **GIVEN** a webapp client connected to the gRPC ViewProjectionConsumer
- **WHEN** client calls `WatchChanges(projection="inventory", after_version=500)`
- **THEN** the client only receives changes with Version > 500
- **AND** changes with Version <= 500 are filtered out

#### Scenario: Remote client gets version
- **GIVEN** a webapp client connected to the gRPC ViewProjectionConsumer
- **WHEN** client calls `GetVersion(projection="inventory")`
- **THEN** the current version (last processed event ID) is returned

### Requirement: Single {domain, stream} per ViewProjection
The system SHALL allow each ViewProjection to consume from exactly one {domain, stream} pair. This simplifies the Pump and matches the consumer lock granularity.

#### Scenario: ViewProjection consumes from single stream
- **GIVEN** a projection with domain="inventory" and stream="items"
- **WHEN** the Pump starts processing
- **THEN** it watches the "inventory.items" stream

#### Scenario: Multiple projections consume from same stream
- **GIVEN** two projections registered with the same {domain, stream}
- **WHEN** both Pumps start processing
- **THEN** each acquires its own consumer lock (different consumer names) and processes events independently

### Requirement: Documentation of public APIs
The system SHALL provide comprehensive documentation for all public APIs, including godoc comments, examples, and README.

#### Scenario: All public types have godoc
- **GIVEN** the views package
- **WHEN** a developer runs `go doc github.com/meschbach/pgcqrs/pkg/indexer/views`
- **THEN** all public types and methods have clear documentation

#### Scenario: Examples demonstrate common use cases
- **GIVEN** the views package
- **WHEN** a developer looks at examples
- **THEN** they find examples for: inventory projection with typed handlers, webapp consumer with UntilVersion, streaming consumer with OnChange
