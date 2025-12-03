# PGCQRS
Provides a JSON event store with support for multi-tenancy and observability.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

## Features

*   **Event Storage**: Persists all events directly into Postgres.
*   **Multi-Tenancy**: separate streams for each client application or domain.
*   **Security**: Support for TLS.
*   **Observability**: Native OpenTelemetry (OTEL) instrumentation.

## Usage
```bash
pg_url="user:password@postgres:5432/pgcqrs"
docker run -e "PGCQRS_STORAGE_POSTGRES_URL=$pg_url" ghcr.io/meschbach/pgcqrs-migrator:latest
docker run -d -p 9000:9000 -p 9001:9001 -e "PGCQRS_STORAGE_POSTGRES_URL=$pg_url" ghcr.io/meschbach/pgcqrs:latest 
```

### Example operations in Go
```go
    //Creates the stream
    exampleKind := "example" //used for the kind of event
    stream := sys.MustStream(ctx, "readme", "test")
    //submit events to be queried
    stream.MustSubmit(ctx, exampleKind, &Event{First: true})
    stream.MustSubmit(ctx, exampleKind, &Event{First: false})
    
    // prepare a query to find all events with First == true
    q := query2.NewQuery(stream)
    q.OnKind(exampleKind).Subset(Event{First: true}).On(v1.EntityFunc(func(ctx context.Context, e v1.Envelope, entity Event) {
        //will be called for each found event as the query is executing
        fmt.Printf("%#v\n", e)
    }))
    if err = q.StreamBatch(ctx); err != nil {
        panic(err)
    }
```
See the full example in [`examples/readme/main.go`](examples/readme/main.go) .

## Development: Getting started quickly
To get started quickly, you'll need:
*   Go 1.25+
*   Docker & Docker Compose (for local development)

Just run `./docker-up.sh` to get it moving on port `9000` and `9001` .  The tool `pgcqrs` will be available in the root
of the repository.

### Examples and Testing

The project contains several examples demonstrating different usage patterns in the `examples/` directory:

*   **Simple**: Basic usage (`examples/simple`)
*   **Querying**: How to query events (`examples/query`, `examples/query2`)
*   **Watching**: Subscribing to streams (`examples/watch`)
*   **Batching**: Batch query operations (`examples/queryBatch`)

A whole battery of examples are available via `./run-examples.sh`.

# Contributing
Pull requests are welcome.

For major changes, please open an issue first to discuss what you would like to change.

Contributions will be accepted under the Apache 2.0 license.
