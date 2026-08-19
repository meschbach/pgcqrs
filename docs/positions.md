# Consumer Position Tracking

Consumer position tracking allows multiple independent consumers to record their progress within a stream. This is essential for building resumable projections, search indexes, or other downstream systems that need to process events in order and resume from where they left off after a restart.

## Concepts

- **Consumer**: A unique identifier for a client or process reading from a stream.
- **Position**: The ID of the last successfully processed event in a stream.
- **AfterID**: A query predicate used to retrieve events starting *after* a known position.

## API Endpoints (REST)

### Set Consumer Position
Records the last processed event ID for a consumer.

- **URL**: `/v1/domains/{domain}/streams/{stream}/positions/{consumer}`
- **Method**: `POST`
- **Body**:
  ```json
  {
    "eventID": 12345
  }
  ```
- **Constraints**: Setting a position backwards (ID less than current) will result in an error.

### Get Consumer Position
Retrieves the recorded event ID for a consumer.

- **URL**: `/v1/domains/{domain}/streams/{stream}/positions/{consumer}`
- **Method**: `GET`
- **Response**:
  ```json
  {
    "eventID": 12345,
    "found": true
  }
  ```

### List Consumers
Lists all consumers that have recorded positions for a specific stream.

- **URL**: `/v1/domains/{domain}/streams/{stream}/positions`
- **Method**: `GET`
- **Response**:
  ```json
  {
    "consumers": ["indexer-v1", "cache-warmer"]
  }
  ```

### Delete Consumer Position
Removes the recorded position for a consumer.

- **URL**: `/v1/domains/{domain}/streams/{stream}/positions/{consumer}`
- **Method**: `DELETE`

## gRPC Service

The `ConsumerPosition` service provides equivalent functionality via gRPC:

- `SetPosition(SetPositionIn) returns (SetPositionOut)`
- `GetPosition(GetPositionIn) returns (GetPositionOut)`
- `ListConsumers(ListConsumersIn) returns (ListConsumersOut)`
- `DeletePosition(DeletePositionIn) returns (DeletePositionOut)`

## Querying with AfterID

The `AfterID` predicate allows efficient retrieval of events occurring after a specific ID.

### REST API
Include `afterID` in the `WireBatchR2Request` payload for `/v1/app/{app}/{stream}/query-batch-r2`:

```json
{
  "kinds": [...],
  "afterID": 12345
}
```

### Go Client (query2)
Use the `.After(id)` method on the query builder:

```go
q := query2.NewQuery(stream)
q.After(lastPosition)
q.OnKind("UserCreated").Each(func(ctx context.Context, env v1.Envelope, data json.RawMessage) error {
    // Process event...
    return nil
})
err := q.StreamBatch(ctx)
```

## Best Practices

1. **At-Least-Once Delivery**: Consumers should be idempotent. Update the position *after* successfully processing an event (or a batch of events). If the consumer crashes before updating the position, it will re-process the last event(s) upon restart.
2. **Unique Consumer Names**: Use descriptive and unique names for consumers (e.g., include a version number if the projection schema changes).
3. **Batch Updates**: For high-throughput streams, consider updating the position every N events or every T seconds to reduce storage overhead.
