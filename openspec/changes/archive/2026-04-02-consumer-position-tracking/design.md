## Context

Multiple consumers need to independently track their position within a stream to enable resumable materialized view projections. Currently, PGCQRS provides event storage and retrieval, but no native way for consumers to persist their read position. Clients building search indexes, aggregations, or other downstream systems must manage this state externally.

This design adds consumer position tracking as a first-class capability within PGCQRS, along with a query predicate to efficiently backfill from a known position.

## Goals / Non-Goals

**Goals:**
- Allow multiple independent consumers to track their position in a stream
- Support setting, getting, listing, and deleting consumer positions
- Enable efficient backfill queries using an `AfterID` predicate
- Provide a lightweight key-value store for position metadata (no locks, coordination, or consensus)

**Non-Goals:**
- Consumer coordination or leader election
- Shared work distribution among consumers
- Atomic transaction across event write + position update
- Streaming position updates (clients poll)

## Decisions

### 1. Position as Separate Storage

Consumer positions will be stored in a separate table from events. This maintains separation of concerns and allows positions to be queried/updated without impacting event storage performance.

**Alternative considered:** Store positions as events in the stream. Rejected - adds noise to event log and complicates queries.

### 2. Transport Interface Extension

The Transport interface will gain new methods:
```go
SetPosition(ctx context.Context, domain, stream, consumer string, eventID int64) error
GetPosition(ctx context.Context, domain, stream, consumer string) (int64, error)
ListConsumers(ctx context.Context, domain, stream string) ([]string, error)
DeletePosition(ctx context.Context, domain, stream, consumer string) error
```

The implementation handles normalization:
- Stream references use `events_stream.id` (via domain+stream lookup)
- Consumer names stored directly (no separate lookup)

**Alternative considered:** Separate PositionTransport interface. Rejected - keeping it on Transport maintains consistency with existing patterns.

### 3. Query Predicate: AfterID

The QueryIn message will gain a new optional field:
```protobuf
optional int64 afterID = 6;
```

This enables: `SELECT * FROM events WHERE id > afterID`

**Alternative considered:** Use Consistency model. Rejected - Consistency is for ensuring all events up to a point are observed, not for filtering.

### 4. query2 Builder Support

The query2 package will gain an `After(id int64)` method:
```go
func (q *Query) After(id int64) *Query
```

This chains naturally with existing `OnKind()` and `OnID()` methods.

### 5. Storage Schema (Normalized)

Consumer positions reference the stream by ID (normalized), while storing consumer names directly. This avoids orphaning consumer names when streams are deleted.

```sql
-- Consumer positions: stream normalized, consumer name embedded
CREATE TABLE consumer_positions (
    stream_id BIGINT NOT NULL REFERENCES events_stream(id) ON DELETE CASCADE,
    consumer TEXT NOT NULL,
    event_id BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (stream_id, consumer)
);

CREATE INDEX idx_consumer_prefix ON consumer_positions (stream_id, substr(consumer, 1, 16));
```

Note:
- `ON DELETE CASCADE` - deleting a stream cascades to delete all associated positions
- Consumer names are embedded to prevent orphans when streams are deleted
- UpdatedAt uses wall-clock time, not event time - it tracks when the position was recorded

### 6. Backward Position Updates

SetPosition will error if the new event ID is less than the current stored position. This prevents a slow consumer from accidentally overwriting a faster consumer's progress.

**Rationale:** Consumers are responsible for not starting the same consumer identifier twice. If that occurs and the second instance tries to go backwards, it's treated as an error.

### 7. Single Atomic Query Implementation

The SetPosition operation uses a single PostgreSQL query with a CTE to ensure atomicity and prevent race conditions:

```sql
WITH es AS (
    SELECT id FROM events_stream WHERE app = $1 AND stream = $2 FOR SHARE
),
previous AS (
    SELECT cp.event_id as previous_event_id
    FROM consumer_positions cp
    JOIN es ON cp.stream_id = es.id
    WHERE cp.consumer = $3
)
INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
SELECT es.id, $3, $4, NOW()
FROM es
CROSS JOIN (SELECT 1) AS stream_required
WHERE NOT EXISTS (SELECT 1 FROM previous WHERE COALESCE(previous_event_id, 0) > $4)
ON CONFLICT (stream_id, consumer) DO UPDATE 
SET event_id = EXCLUDED.event_id, updated_at = EXCLUDED.updated_at
WHERE COALESCE(consumer_positions.event_id, 0) <= EXCLUDED.event_id
RETURNING 
    (SELECT previous_event_id FROM previous) as previous_event_id,
    $4 as current_event_id
```

**How it works:**
- The `es` CTE resolves domain+stream to stream_id using SELECT with FOR SHARE. If the stream doesn't exist, this returns 0 rows.
- The `CROSS JOIN (SELECT 1) AS stream_required` **asserts** the stream exists - if the stream doesn't exist, the entire query produces zero rows and fails with "no rows in result set"
- The Go implementation wraps this error to explicitly state: "stream does not exist: {domain}/{stream}"
- **IMPORTANT**: SetPosition does NOT create streams. If the stream doesn't exist, the operation fails.
- The `previous` CTE fetches the existing position (empty if first-time consumer)
- The `WHERE NOT EXISTS` gate rejects backward movement atomically (same position is allowed)
- `ON CONFLICT` handles the upsert, with WHERE clause ensuring only forward updates succeed
- `RETURNING` provides the previous event ID and current event ID for diagnostic purposes

**Return semantics:**

| Scenario | `previous_event_id` returned | Error |
|----------|-------------------------------|-------|
| First time consumer | `nil` (no previous) | none |
| Forward moved | The old event ID | none |
| Backward rejected | N/A | BackwardPositionError |
| Stream not found | N/A | StreamNotFoundError |

### 8. Return Type

SetPosition returns a result struct containing:
```go
type SetPositionResult struct {
    PreviousEventID *int64  // nil if first-time consumer
    CurrentEventID int64   // the position that was set
}
```

This provides diagnostic information to clients about what happened, enabling better debugging and logging.

## Risks / Trade-offs

- **[Risk]** Position drift if client crashes between processing event and updating position.
  - **Mitigation:** Clients should update position after successful processing, but accept at-least-once delivery. Idempotent event processing handles duplicates.

- **[Risk]** Large number of consumers per stream.
  - **Mitigation:** Primary key is (stream_id, consumer) - indexed for lookups. For very high consumer counts, consider partitioning or external tracking.

## Migration Plan

1. Add migration: create `consumer_positions` table (stream_id FK, consumer, event_id, updated_at) with CASCADE delete
2. Add proto messages for position operations
3. Implement storage layer for positions (handle stream→id lookup)
4. Add methods to Transport interface (memory, gRPC, HTTP implementations)
5. Add `afterID` field to QueryIn proto
6. Implement storage-level filtering by ID range
7. Add query2.After() builder method
8. Add gRPC and REST endpoints for position CRUD

Rollback: Migration can be reversed with down migration. Existing code continues to work (new methods are additive).

## Open Questions

(none - see Decisions above for resolved questions)
