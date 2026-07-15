## MODIFIED Requirements

### Requirement: Consumer can set position in stream
The system SHALL allow a consumer to record their current read position within a stream by specifying the domain, stream, consumer identifier, and the event ID they have processed. Position may be set via the existing `SetPosition` RPC or piggybacked on the lock heartbeat stream. Position operations are managed by `ConsumerStore` which handles both lock and position state atomically. Consumer names are normalized via a `consumer_names` enumeration table (two-phase migration: phase 1 adds FK column alongside existing TEXT, phase 2 in a future release drops the TEXT column).

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

#### Scenario: Heartbeat with stale position returns conflict
- **GIVEN** consumer "search-index" holds the lock for domain "inventory-A", stream "events" with position=200
- **WHEN** consumer sends Heartbeat via KeepAlive stream with position=150
- **THEN** the system returns a conflict error with target_version=150 and current_version=200
- **AND** the lock is NOT renewed and the position is NOT updated (entire transaction rolled back)

#### Scenario: Set position to current position (idempotent)
- **GIVEN** consumer "search-index" has position 150 in stream "events" of domain "inventory-A"
- **WHEN** consumer calls SetPosition with domain "inventory-A", stream "events", consumer "search-index", eventID 150
- **THEN** the system returns success with previousEventID 150

#### Scenario: Set position via lock heartbeat
- **GIVEN** consumer "search-index" holds the lock for domain "inventory-A", stream "events" via KeepAlive stream
- **WHEN** consumer sends Heartbeat with position=200 (at or ahead of current position)
- **THEN** the system updates the consumer position to 200 atomically with the lock heartbeat (single transaction via ConsumerStore.HeartbeatWithPosition)

### Requirement: Position operations are backward compatible with pre-migration data
Position read operations (GetPosition, ListConsumers, DeletePosition) SHALL correctly handle rows created before the consumer_names migration. Rows with NULL consumer_id are read via the legacy TEXT column. Rows with a populated consumer_id are read via the consumer_names enumeration table. SetPosition promotes legacy rows by dual-writing both columns.

#### Scenario: GetPosition reads a pre-migration row
- **GIVEN** a consumer_positions row exists with consumer="legacy-index" and consumer_id=NULL (pre-migration)
- **WHEN** GetPosition is called for "legacy-index"
- **THEN** the system returns the position from the TEXT column with no error

#### Scenario: ListConsumers merges both migration paths
- **GIVEN** a pre-migration row exists with consumer="legacy-index" and consumer_id=NULL
- **AND** a post-migration row exists with consumer="modern-index" and consumer_id populated
- **WHEN** ListConsumers is called
- **THEN** the system returns both "legacy-index" and "modern-index"

#### Scenario: DeletePosition removes a pre-migration row
- **GIVEN** a consumer_positions row exists with consumer="legacy-index" and consumer_id=NULL
- **WHEN** DeletePosition is called for "legacy-index"
- **THEN** the row is deleted and GetPosition returns not-found
