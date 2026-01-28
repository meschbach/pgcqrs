## PGCQRS Client API Summary

### Overview
The PGCQRS client provides an interface to interact with a Postgres-backed event stream, enabling record creation and retrieval via HTTP/JSON (v1), gRPC (v1), and native Go client integration. It abstracts the underlying PostgreSQL sequence management and ensures ordered, reliable event delivery.

### Key Features
- **Event Streaming**: Ordered, in-order sequence of events (streams)
- **Transport Protocols**: HTTP/JSON (v1), gRPC (v1), and Go client integration
- **Record Management**: Create, read, and query records with versioned identifiers
- **Consistency**: Ensures atomic writes with optimistic concurrency control
- **Scalability**: Designed for high-throughput, low-latency operations
- **Native Go Client**: Zero-copy serialization, async support, and idiomatic integration

### API Methods
#### gRPC API
1. **Create Record**
   - **Service**: `pgcqrs.RecordService`
   - **Method**: `CreateRecord(context.Context, *CreateRequest) (*CreateResponse, error)`
   - **Request**: `bytes` (binary-encoded content) + `metadata` (map[string]string)
   - **Response**: `recordId` (string), `version` (int32), `timestamp` (timestamp)

2. **Recall Record**
   - **Service**: `pgcqrs.RecordService`
   - **Method**: `GetRecord(context.Context, *GetRequest) (*GetResponse, error)`
   - **Request**: `recordId` (string)
   - **Response**: `data` (bytes), `metadata` (map[string]string), `version` (int32), `timestamp` (timestamp)

3. **List Records**
   - **Service**: `pgcqrs.StreamService`
   - **Method**: `ListRecords(context.Context, *ListRequest) (*ListResponse, error)`
   - **Request**: `streamName` (string), `fromVersion` (int32), `limit` (int32)
   - **Response**: Array of records with pagination support

#### Go Client API
```go
// Example: Create record
client, _ := NewClient("localhost:5432")
data := []byte("{\"event\": "user.signup"}")
record, err := client.CreateRecord(ctx, "user_events", data, map[string]string{\"user.id\": "123"})

// Example: Recall record
record, err := client.GetRecord(ctx, "user_events", "record-789")
```

### Security
- **Authentication**: Requires bearer token in `Authorization` header
- **Validation**: Schema validation for all inbound payloads
- **Rate Limiting**: 100 requests/second per stream

### Error Handling
- **400 Bad Request**: Invalid payload format
- **401 Unauthorized**: Missing/invalid authentication token
- **404 Not Found**: Stream or record does not exist
- **429 Too Many Requests**: Rate limit exceeded

### Implementation Notes
- Use `goimports` for formatting
- Follow the `fmt` package for logging
- Include comprehensive tests for all API endpoints
- Ensure thread safety for concurrent record operations
- For gRPC, use `google.golang.org/grpc` and `google.golang.org/protobuf` packages

For detailed usage examples, refer to the integration tests in `internal/clients` and `internal/grpc`.