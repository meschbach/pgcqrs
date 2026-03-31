## ADDED Requirements

### Requirement: Consumer can set position in stream
The system SHALL allow a consumer to record their current read position within a stream by specifying the domain, stream, consumer identifier, and the event ID they have processed.

#### Scenario: Set position for new consumer
- **WHEN** consumer calls SetPosition with domain "inventory-A", stream "events", consumer "search-index", eventID 150
- **THEN** the system stores the position and returns no error

#### Scenario: Set position when stream does not exist
- **WHEN** consumer calls SetPosition with domain "inventory-A", stream "non-existent-stream", consumer "search-index", eventID 150
- **THEN** the system returns an error indicating the stream was not found

#### Scenario: Update position for existing consumer
- **WHEN** consumer calls SetPosition with domain "inventory-A", stream "events", consumer "search-index", eventID 200
- **THEN** the system updates the stored position from 150 to 200 and returns no error

#### Scenario: Attempt to set position backwards
- **GIVEN** consumer "search-index" has position 150 in stream "events" of domain "inventory-A"
- **WHEN** consumer calls SetPosition with domain "inventory-A", stream "events", consumer "search-index", eventID 100
- **THEN** the system returns an error indicating the position would go backwards

#### Scenario: Set position to current position (idempotent)
- **GIVEN** consumer "search-index" has position 150 in stream "events" of domain "inventory-A"
- **WHEN** consumer calls SetPosition with domain "inventory-A", stream "events", consumer "search-index", eventID 150
- **THEN** the system returns success with previousEventID 150

### Requirement: Consumer can get position in stream
The system SHALL allow a consumer to retrieve their current read position within a stream by specifying the domain, stream, and consumer identifier.

#### Scenario: Get position for existing consumer
- **GIVEN** consumer "search-index" has position 150 in stream "events" of domain "inventory-A"
- **WHEN** consumer calls GetPosition with domain "inventory-A", stream "events", consumer "search-index"
- **THEN** the system returns eventID 150

#### Scenario: Get position for unknown consumer
- **WHEN** consumer calls GetPosition with domain "inventory-A", stream "events", consumer "unknown"
- **THEN** the system returns an error indicating the consumer does not exist

### Requirement: Consumer can list all consumers for a stream
The system SHALL allow listing all consumers that have recorded a position for a given domain and stream.

#### Scenario: List consumers for stream with positions
- **GIVEN** consumers "search-index" and "stats-aggregator" have positions in stream "events" of domain "inventory-A"
- **WHEN** client calls ListConsumers with domain "inventory-A", stream "events"
- **THEN** the system returns ["search-index", "stats-aggregator"]

#### Scenario: List consumers for stream with no positions
- **WHEN** client calls ListConsumers with domain "inventory-A", stream "empty-stream"
- **THEN** the system returns an empty list

### Requirement: Consumer can delete position in stream
The system SHALL allow a consumer to remove their recorded position from a stream.

#### Scenario: Delete existing position
- **GIVEN** consumer "search-index" has position 150 in stream "events" of domain "inventory-A"
- **WHEN** consumer calls DeletePosition with domain "inventory-A", stream "events", consumer "search-index"
- **THEN** the position is removed and subsequent GetPosition calls return error

#### Scenario: Delete non-existent position
- **WHEN** consumer calls DeletePosition with domain "inventory-A", stream "events", consumer "unknown"
- **THEN** the system returns no error (idempotent delete)

### Requirement: Query can filter events after specified ID
The system SHALL allow queries to return only events with IDs strictly greater than a specified value.

#### Scenario: Query events after ID with matching events
- **GIVEN** stream "events" contains events with IDs [1, 2, 3, 4, 5]
- **WHEN** client queries stream with AfterID set to 2
- **THEN** the system returns events with IDs [3, 4, 5]

#### Scenario: Query events after ID with no matching events
- **GIVEN** stream "events" contains events with IDs [1, 2, 3]
- **WHEN** client queries stream with AfterID set to 3
- **THEN** the system returns an empty result set

#### Scenario: Query events after ID with AfterID of zero
- **GIVEN** stream "events" contains events with IDs [1, 2, 3]
- **WHEN** client queries stream with AfterID set to 0
- **THEN** the system returns all events [1, 2, 3]

### Requirement: Query supports combining AfterID with kind filters
The system SHALL allow combining the AfterID filter with existing kind-based filtering.

#### Scenario: Query with AfterID and kind filter
- **GIVEN** stream "events" contains events: (1, ItemAdded), (2, ItemMoved), (3, ItemAdded), (4, ItemMoved)
- **WHEN** client queries stream with AfterID=1 and OnKind("ItemAdded")
- **THEN** the system returns events with IDs [3] (ItemAdded after ID 1)
