# Stream Watch

## Purpose

Stream watch provides a continuous query mechanism that delivers events from a stream to a consumer. The consumer creates a watch with a query (domain, stream, filters) and receives events one at a time via `Tick()`. Each transport implements its own strategy for catching up on existing events and receiving new ones, but all transports guarantee exactly-once delivery with monotonically increasing event IDs.

## Requirements

### Requirement: WatchInternal interface provides single Tick method
The system SHALL provide a `WatchInternal` interface with a single method `Tick(ctx context.Context) (*ipc.QueryOut, error)`. Each call returns the next matching event or blocks until one is available.

#### Scenario: Tick returns next event
- **GIVEN** a WatchInternal created with a query for domain "inventory", stream "events"
- **WHEN** the stream contains matching events [1, 2, 3]
- **WHEN** consumer calls `Tick(ctx)`
- **THEN** the first call returns event ID 1
- **AND** the second call returns event ID 2
- **AND** the third call returns event ID 3

#### Scenario: Tick blocks waiting for new events
- **GIVEN** a WatchInternal with no pending events
- **WHEN** consumer calls `Tick(ctx)` with a valid context
- **THEN** the call blocks until a new event arrives or context is canceled

#### Scenario: Tick returns error on context cancellation
- **GIVEN** a WatchInternal with no pending events
- **WHEN** consumer calls `Tick(ctx)` and the context is canceled
- **THEN** the call returns `ctx.Err()`

### Requirement: Transport creates watch from query
The system SHALL provide a `Transport.Watch(ctx, *ipc.QueryIn) (WatchInternal, error)` method that creates a watch from a query. The query specifies the domain, stream, and optional filters (kind, afterID). The returned WatchInternal delivers events matching the query.

#### Scenario: Create watch with kind filter
- **GIVEN** a transport with events in domain "inventory", stream "events"
- **WHEN** consumer calls `Transport.Watch(ctx, query)` where query filters on kind "ItemCreated"
- **THEN** the returned WatchInternal only delivers events with kind "ItemCreated"

#### Scenario: Create watch with AfterID filter
- **GIVEN** a transport with events [1, 2, 3, 4, 5] in domain "inventory", stream "events"
- **WHEN** consumer calls `Transport.Watch(ctx, query)` where query has AfterID=2
- **THEN** the returned WatchInternal only delivers events with ID > 2 (events 3, 4, 5)

#### Scenario: Create watch on empty stream
- **GIVEN** a transport with no events in domain "inventory", stream "events"
- **WHEN** consumer calls `Transport.Watch(ctx, query)`
- **THEN** the returned WatchInternal delivers no events until new events are written
- **AND** subsequent events written to the stream are delivered as they arrive

### Requirement: Exactly-once delivery with monotonic IDs
The system SHALL deliver each matching event exactly once per watch. Event IDs returned by `Tick()` are strictly monotonically increasing — each event ID is greater than all previously delivered IDs.

#### Scenario: No duplicate delivery
- **GIVEN** a WatchInternal watching a stream with events [1, 2, 3]
- **WHEN** consumer calls `Tick()` repeatedly until no more events are available
- **THEN** each event ID appears exactly once in the returned results

#### Scenario: Monotonically increasing IDs
- **GIVEN** a WatchInternal watching a stream
- **WHEN** consumer calls `Tick()` and receives events [10, 11, 12]
- **THEN** each subsequent ID is greater than the previous
- **AND** no ID is repeated

#### Scenario: New events delivered in order
- **GIVEN** a WatchInternal with init phase complete
- **WHEN** events 10, 11, 12 are written to the stream sequentially
- **AND** consumer calls `Tick()` for each
- **THEN** events are returned in order [10, 11, 12]

### Requirement: Watch query is immutable after creation
The system SHALL NOT modify the query after watch creation. Filters (kind, afterID) are captured at creation time and do not change.

#### Scenario: Query filters are captured at creation
- **GIVEN** a WatchInternal created with AfterID=5
- **WHEN** consumer calls `Tick()` and receives events
- **THEN** all returned events have ID > 5
- **AND** the AfterID filter does not change as events are delivered

### Requirement: Watch is closed by context cancellation
The system SHALL support closing a watch by canceling the context passed to `Tick()`. The watch releases any held resources (connections, goroutines, channels) after context cancellation.

#### Scenario: Context cancellation stops watch
- **GIVEN** a WatchInternal with pending events
- **WHEN** the context is canceled
- **AND** consumer calls `Tick(ctx)`
- **THEN** the call returns `ctx.Err()`
- **AND** the watch releases all held resources

#### Scenario: Watch cleanup on error
- **GIVEN** a WatchInternal that encounters a transport error
- **WHEN** `Tick()` returns an error
- **THEN** the watch releases all held resources
- **AND** subsequent `Tick()` calls return an error

### Requirement: Signal coordination ensures no lost notifications
The system SHALL provide a signal coordination mechanism that ensures notifications arriving during query execution are queued, not lost. The first signal shall be available immediately (seeded at creation). Subsequent signals shall be queued while the consumer is busy.

#### Scenario: First signal available immediately
- **GIVEN** a signal coordination mechanism created with an emitter
- **WHEN** the consumer calls `Wait()` for the first time
- **THEN** the call returns immediately without requiring a prior `Signal()` call

#### Scenario: Signal during busy period is queued
- **GIVEN** a signal coordination mechanism with a consumer processing a query
- **WHEN** a `Signal()` call arrives while the consumer is busy
- **THEN** the signal is queued
- **AND** the next `Wait()` call returns immediately

#### Scenario: Multiple signals coalesce
- **GIVEN** a signal coordination mechanism
- **WHEN** multiple `Signal()` calls arrive before the next `Wait()`
- **THEN** only one signal is queued
- **AND** the next `Wait()` returns once

#### Scenario: Context cancellation unblocks Wait
- **GIVEN** a signal coordination mechanism with no pending signal
- **WHEN** the consumer calls `Wait(ctx)` and the context is canceled
- **THEN** the call returns `ctx.Err()`
- **AND** the signal is not consumed

### Requirement: query2.Watch wraps WatchInternal with handler dispatch
The system SHALL provide a `query2.Watch` struct that wraps a `WatchInternal` and dispatches events to registered handlers based on the operation code.

#### Scenario: Handler dispatches by operation code
- **GIVEN** a query2.Watch with handler registered for a specific operation
- **WHEN** an event with that operation arrives via `Tick()`
- **THEN** the handler is called with the event envelope and body

#### Scenario: TickWithID returns event ID
- **GIVEN** a query2.Watch with handlers registered
- **WHEN** an event with ID 42 arrives and handler completes successfully
- **THEN** `TickWithID()` returns `(42, nil)`

#### Scenario: TickWithID returns handler error
- **GIVEN** a query2.Watch with a handler that returns an error
- **WHEN** the handler is called and returns an error
- **THEN** `TickWithID()` returns `(0, error)` with the handler's error

#### Scenario: Pump loops until error
- **GIVEN** a query2.Watch with handlers
- **WHEN** `Pump(ctx)` is called
- **THEN** it calls `Tick(ctx)` in a loop until `Tick()` returns an error
- **AND** the error is returned to the caller

### Requirement: HTTP transport does not support Watch
The HTTP transport SHALL return an error when `Watch()` is called. The watch subsystem relies on server-streaming or long-lived connections to push events to consumers. HTTP's request-response model cannot support this pattern. Consumers using the HTTP transport SHALL use `QueryBatchR2()` for polling instead.

#### Scenario: HTTP Watch returns error
- **GIVEN** an HTTP transport layer
- **WHEN** consumer calls `Transport.Watch(ctx, query)`
- **THEN** the call returns an error indicating Watch is not supported
- **AND** the error message directs the consumer to use QueryBatchR2 or gRPC transport

#### Scenario: HTTP Watch does not open connections
- **GIVEN** an HTTP transport layer
- **WHEN** consumer calls `Transport.Watch(ctx, query)`
- **THEN** no HTTP connection is opened
- **AND** no resources are allocated
