# Stream Watch — gRPC Transport

## Purpose

The gRPC transport implements `WatchInternal` on the client side as a thin wrapper around server streaming, and implements the full watch logic on the server side. The server uses a bus-triggered re-query pattern: it subscribes to an in-process event bus and re-queries PostgreSQL whenever new events are stored. Deduplication ensures exactly-once delivery across re-query cycles.

## Requirements

### Requirement: gRPC Watch client is a thin receive wrapper
The gRPC client-side watch SHALL wrap a server streaming connection and call `Recv()` on each `Tick()`. All watch logic (query building, dedup, re-query) lives server-side.

#### Scenario: Client Tick receives server-streamed message
- **GIVEN** a gRPC client watch connected to a server
- **WHEN** client calls `Tick(ctx)`
- **THEN** the client receives the next `QueryOut` message from the server stream

#### Scenario: Client Tick returns error on stream failure
- **GIVEN** a gRPC client watch with a broken server stream
- **WHEN** client calls `Tick(ctx)`
- **THEN** the call returns an error

### Requirement: Server Watch builds query from request
The gRPC server watch SHALL build query operations from the incoming request's event filters (domain, stream, kind, afterID).

#### Scenario: Server builds operations from query
- **GIVEN** a Watch request with domain "inventory", stream "events", kind "ItemCreated"
- **WHEN** server processes the Watch request
- **THEN** query operations are built from the request clauses
- **AND** the operations filter on kind "ItemCreated"

#### Scenario: Server returns nil for empty operations
- **GIVEN** a Watch request with no event clauses
- **WHEN** server processes the Watch request
- **THEN** the Watch handler returns nil (no error)

### Requirement: Server Watch re-queries on new events
The gRPC server watch SHALL re-run the query whenever new events are stored. (Implemented via a bus subscription that signals a re-query loop.)

#### Scenario: New event triggers re-query
- **GIVEN** a gRPC server watch running the re-query loop
- **WHEN** an event is stored and the bus fires a notification
- **THEN** the loop re-runs the query with updated `AfterID`

#### Scenario: Bus signal is non-blocking
- **GIVEN** a gRPC server watch with the re-query signal at capacity
- **WHEN** the bus fires a notification
- **THEN** the signal is dropped (no-op)
- **AND** the loop continues with the current query cycle

#### Scenario: Initial query runs immediately
- **GIVEN** a gRPC server watch just created
- **WHEN** the Watch handler starts the re-query loop
- **THEN** the first query runs immediately without waiting for a bus signal

#### Scenario: Signal during initial query processing is queued
- **GIVEN** a gRPC server watch just created
- **WHEN** the Watch handler starts the re-query loop
- **AND** an event is stored while the initial query is executing
- **THEN** the bus signal is queued
- **AND** the next re-query cycle picks up the new event

### Requirement: Server Watch advances read position after each query
The gRPC server watch SHALL advance its read position to the highest delivered event ID after each query. The next query uses this position as the `AfterID` filter.

#### Scenario: AfterID advances after query
- **GIVEN** a gRPC server watch with read position at event 100
- **WHEN** the re-query loop runs and delivers events [101, 102, 103]
- **THEN** the read position advances to 103
- **AND** the next query only returns events with ID > 103

#### Scenario: AfterID initialized from request
- **GIVEN** a Watch request with `AfterID = 50`
- **WHEN** the Watch handler starts
- **THEN** the read position is initialized to 50
- **AND** the first query uses `AfterID = 50`

### Requirement: Server Watch deduplicates events
The gRPC server watch SHALL deduplicate events to ensure exactly-once delivery. Events with ID less than or equal to the highest already-sent ID are silently dropped. This is defense-in-depth: the SQL `AfterID` filter should prevent duplicates, but the `lastSentID` guard in `grpcResultStream` catches any that slip through. A `watch.dedup.drops` OTel counter is incremented on each drop for observability.

#### Scenario: Dedup drops already-delivered events
- **GIVEN** a gRPC server watch with highest delivered ID = 10
- **WHEN** a query returns event ID 10
- **THEN** the event is dropped (not sent to client)

#### Scenario: Dedup allows new events
- **GIVEN** a gRPC server watch with highest delivered ID = 10
- **WHEN** a query returns event ID 11
- **THEN** the event is sent to the client
- **AND** the highest delivered ID updates to 11

#### Scenario: Initial dedup state
- **GIVEN** a new gRPC server watch
- **WHEN** the watch is created
- **THEN** the dedup counter (`lastSentID`) is initialized to -1
- **AND** events with ID 0 are not dropped (0 > -1)

### Requirement: Server Watch processes results synchronously
The gRPC server watch SHALL process query results synchronously in the main loop, translating each result to a gRPC message before processing the next. This ensures the dedup counter is always up-to-date before the next re-query.

#### Scenario: Results processed in order
- **GIVEN** a gRPC server watch re-querying
- **WHEN** a query returns events [101, 102, 103]
- **THEN** each event is translated and sent to the client in order
- **AND** the dedup counter updates after each send

#### Scenario: Send failure stops the watch
- **GIVEN** a gRPC server watch with a broken client connection
- **WHEN** translating an event fails
- **THEN** the watch returns the error

### Requirement: Server Watch stops on context cancellation
The gRPC server watch SHALL stop the re-query loop when the client's context is canceled.

#### Scenario: Context cancellation stops loop
- **GIVEN** a gRPC server watch running the re-query loop
- **WHEN** the client's context is canceled
- **THEN** the loop exits
- **AND** the context error is returned

### Requirement: Server Watch unsubscribes from bus on exit
The gRPC server watch SHALL unsubscribe from the event bus when the watch exits.

#### Scenario: Bus watcher cleaned up on exit
- **GIVEN** a gRPC server watch with an active bus subscription
- **WHEN** the Watch handler returns (context canceled or error)
- **THEN** the bus subscription is removed
- **AND** no further bus notifications are delivered to the watch

### Requirement: Server Watch handles rapid events without duplication
The gRPC server watch SHALL NOT deliver duplicate events when multiple events are stored rapidly and bus signals are dropped. The dedup mechanism ensures each event is delivered exactly once regardless of signal delivery.

#### Scenario: Rapid event writes are deduplicated
- **GIVEN** a gRPC server watch processing event 100
- **WHEN** events 101, 102, 103 are stored rapidly
- **AND** only one bus signal is queued (channel capacity 1)
- **THEN** the next re-query returns all three events
- **AND** each is delivered exactly once (dedup prevents duplicates)
