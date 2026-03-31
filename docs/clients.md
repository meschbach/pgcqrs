## PGCQRS Client API Summary

### Overview
PGCQRS provides a Postgres-backed event store, enabling event submission and retrieval via gRPC and REST. It supports multi-tenancy through domains and streams, with native OpenTelemetry instrumentation for observability.

### Key Features
- **Event Streaming**: Ordered, in-order sequence of events.
- **Transport Protocols**: gRPC and REST (HTTP/JSON).
- **Multi-Tenancy**: Separate streams for each application or domain.
- **Consumer Tracking**: Record and resume progress with consumer positions.
- **Observability**: Built-in OpenTelemetry tracing and metrics.

### gRPC API Services

#### `Command` Service
Used for managing streams and submitting events.
- `CreateStream(CreateStreamIn) returns (CreateStreamOut)`
- `Submit(SubmitIn) returns (SubmitOut)`: Submits a new event to a stream.

#### `Query` Service
Used for retrieving events and querying streams.
- `ListStreams(ListStreamsIn) returns (ListStreamsOut)`
- `Get(GetIn) returns (GetOut)`: Retrieves a specific event by ID.
- `Query(QueryIn) returns (stream QueryOut)`: Streams events matching the query.
- `Watch(QueryIn) returns (stream QueryOut)`: Real-time event subscription.

#### `ConsumerPosition` Service
Used for tracking consumer progress.
- `SetPosition(SetPositionIn) returns (SetPositionOut)`
- `GetPosition(GetPositionIn) returns (GetPositionOut)`
- `ListConsumers(ListConsumersIn) returns (ListConsumersOut)`
- `DeletePosition(DeletePositionIn) returns (DeletePositionOut)`

See [Consumer Position Tracking](./positions.md) for more details.

### Go Client Usage (query2)

The `query2` package provides a high-level, idiomatic Go API for building and executing queries.

```go
// Setup system and stream
transport, _ := v1.NewGRPCTransport("localhost:9001")
system := v1.NewSystem(transport)
stream := system.MustStream(ctx, "my-app", "user-events")

// Build and execute a query
q := query2.NewQuery(stream)
q.OnKind("UserCreated").Each(func(ctx context.Context, env v1.Envelope, data json.RawMessage) error {
    fmt.Printf("Received user created: %v\n", env.ID)
    return nil
})
err := q.StreamBatch(ctx)
```

### REST API

PGCQRS also exposes a REST API on port 9000 (default).

- `PUT /v1/app/{domain}/{stream}`: Ensure stream exists.
- `POST /v1/app/{domain}/{stream}/submit/{kind}`: Submit an event.
- `GET /v1/app/{domain}/{stream}/payload/{id}`: Get event payload by ID.
- `POST /v1/app/{domain}/{stream}/query-batch-r2`: Execute an R2 batch query.
- `GET /v1/domains/{domain}/streams/{stream}/positions/{consumer}`: Get consumer position.

### Implementation Notes
- Use `goimports` for import management.
- Follow structured logging patterns (avoid `fmt.Print`).
- Always use `context.Context` for cancellation and timeouts.
- All gRPC methods are defined in `pkg/ipc/query.proto`.
