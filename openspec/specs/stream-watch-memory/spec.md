# Stream Watch — Memory Transport

## Purpose

The memory transport implements `WatchInternal` for in-process use, primarily in tests and the `MemoryHarness` test helper. It uses a two-phase init-then-live pattern: on the first `Tick()`, it catches up on existing events (init phase), then switches to live-only mode where new events arrive via registered callbacks. Deduplication ensures exactly-once delivery across both phases.

This is the reference implementation for transport authors. Other transports should follow the same two-phase pattern: catch up on existing events, then deliver new ones, with dedup to prevent overlap.

## Requirements

### Requirement: Two-phase catch-up then live delivery
The memory watch SHALL operate in two phases. In the init phase (first `Tick()`), it delivers all existing matching events from the stream. In the live phase (subsequent `Tick()` calls), it delivers only new events as they arrive.

#### Scenario: First Tick catches up on existing events
- **GIVEN** a memory watch created on a stream with events [1, 2, 3]
- **WHEN** consumer calls `Tick(ctx)` for the first time
- **THEN** the watch returns events [1, 2, 3] in order

#### Scenario: Subsequent Ticks deliver live events only
- **GIVEN** a memory watch that has completed its init phase
- **WHEN** a new event 4 is written to the stream
- **AND** consumer calls `Tick(ctx)`
- **THEN** the watch returns event 4

#### Scenario: Init phase runs exactly once
- **GIVEN** a memory watch that has not yet completed init
- **WHEN** `Tick(ctx)` is called
- **THEN** the init phase runs
- **AND** subsequent `Tick(ctx)` calls skip the init phase

### Requirement: Exactly-once dedup across init and live phases
The memory watch SHALL deliver each matching event exactly once across both init and live phases. (Implemented via a monotonic `lastEvent` counter initialized to -1. Any event with ID <= `lastEvent` is silently dropped.)

#### Scenario: Dedup prevents overlap between phases
- **GIVEN** a memory watch with init phase delivering events [1, 2, 3]
- **WHEN** event 3 arrives again via the live callback (overlap)
- **THEN** the duplicate is dropped
- **AND** event 3 is delivered exactly once total

#### Scenario: Dedup handles out-of-order delivery
- **GIVEN** a memory watch with highest delivered ID = 5
- **WHEN** an event with ID 4 arrives
- **THEN** the event is silently dropped

#### Scenario: Dedup handles same-ID delivery
- **GIVEN** a memory watch with highest delivered ID = 5
- **WHEN** an event with ID 5 arrives
- **THEN** the event is silently dropped

#### Scenario: Dedup allows new events
- **GIVEN** a memory watch with highest delivered ID = 5
- **WHEN** an event with ID 6 arrives
- **THEN** the event is delivered
- **AND** the highest delivered ID updates to 6

#### Scenario: Initial dedup state
- **GIVEN** a memory watch created with no prior events
- **WHEN** the watch is constructed
- **THEN** the dedup counter is -1
- **AND** events with ID 0 are not dropped (0 > -1)

### Requirement: Pending queue is drained before waiting
The memory watch SHALL drain any queued events before waiting for new ones. (Implemented via a `pending` slice that buffers events between `Tick()` calls.)

#### Scenario: Tick returns from pending queue
- **GIVEN** a memory watch with events [1, 2, 3] queued
- **WHEN** consumer calls `Tick(ctx)`
- **THEN** event 1 is returned immediately without waiting

#### Scenario: Tick waits when queue is empty
- **GIVEN** a memory watch with an empty queue and init complete
- **WHEN** consumer calls `Tick(ctx)`
- **THEN** the call blocks until a new event arrives

### Requirement: Live events are captured at creation
The memory watch SHALL register a callback on the stream before reading existing events. This ensures events written during the init phase are captured and not lost. (Implemented via an `onAddPacket` callback registered before init.)

#### Scenario: Events written during init are not lost
- **GIVEN** a memory watch being created
- **WHEN** the callback is registered
- **AND** an event is written while init is reading
- **THEN** the event arrives via the callback
- **AND** is delivered during the live phase

#### Scenario: Callback uses non-blocking send
- **GIVEN** a memory watch with a full event buffer
- **WHEN** an event is written and the callback fires
- **THEN** the send is non-blocking (dropped if full)
- **AND** no panic occurs

### Requirement: Query filter applied during init
The memory watch SHALL apply the query filter (kind, afterID) during the init phase. Only events matching the filter are delivered. (Implemented via a `queryInFilter` that wraps the query.)

#### Scenario: Kind filter applied during init
- **GIVEN** a memory watch with query filtering on kind "ItemCreated"
- **WHEN** init phase reads events [1(ItemCreated), 2(ItemMoved), 3(ItemCreated)]
- **THEN** events 1 and 3 are delivered
- **AND** event 2 is skipped

#### Scenario: AfterID filter applied during init
- **GIVEN** a memory watch with query AfterID=2
- **WHEN** init phase reads events [1, 2, 3, 4]
- **THEN** events 3 and 4 are delivered
- **AND** events 1 and 2 are skipped

### Requirement: New events looked up by ID
The memory watch SHALL look up each new event by ID from the stream. (Implemented via `GetEnvelope` which accesses the stream's packet array directly — O(1) per event.)

#### Scenario: New event looked up by ID
- **GIVEN** a memory watch in live phase
- **WHEN** event ID 10 arrives via the callback
- **THEN** the watch looks up envelope with ID 10
- **AND** applies the query filter
- **AND** delivers if filter passes

#### Scenario: New event filtered out
- **GIVEN** a memory watch with kind filter "ItemCreated"
- **WHEN** event ID 10 (kind "ItemMoved") arrives
- **THEN** the query filter rejects it
- **AND** no event is delivered

### Requirement: Callback registration is serialized
The memory watch SHALL register callbacks through the transport's serialization mechanism. (Implemented via `simulateNetwork` which serializes operations through a channel.)

#### Scenario: No race condition on callback registration
- **GIVEN** the memory transport's serialization goroutine
- **WHEN** `Watch()` is called
- **THEN** the callback is appended to the stream's callback list
- **AND** this operation is serialized through the transport's input channel
- **AND** no race condition occurs with concurrent writes
