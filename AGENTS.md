# AGENTS.md - PGCQRS Development Guide

This file provides guidance for agentic coding agents operating in the pgcqrs repository.

## Project Overview

PGCQRS is a Go project that provides a JSON event store with multi-tenancy and observability support, backed by PostgreSQL. The project requires Go 1.24+.

## Build, Test, and Development Commands

### Running Tests

```bash
# Run all unit tests (pkg and internal)
go test -count=1 ./pkg/... ./internal/...

# Run a single test by name
go test -run TestName ./path/to/package

# Run tests with verbose output
go test -v ./pkg/...

# Run linter (golangci-lint must be installed)
golangci-lint run ./...

# Run system/integration tests (requires Docker and running service)
./integration-tests.sh
```

**Note**: To verify fixes work systemically (as CI does), run `./dev.sh up`. This runs the full test suite including unit tests, integration tests with a real PostgreSQL database, and system tests across all transports (memory, HTTP, gRPC).

### Building

```bash
# Build all binaries (runs tests + builds migrator, service, pgcqrs CLI)
./build.sh

# Build individual binaries
go build ./cmd/migrator
go build ./cmd/service
go build ./cmd/pgcqrs
```

### Development Environment

The `dev.sh` script provides fine-grained control over the development workflow:

```bash
# Full pipeline: build, test, deploy, run examples + integration tests
./dev.sh up

# Rebuild and restart containers only (after code changes)
./dev.sh services

# Run example drift detection only
./dev.sh examples

# Run transport verification tests only (memory, HTTP, gRPC)
./dev.sh integration

# Run both examples + integration tests
./dev.sh system_tests
```

**Typical workflow**: Make code changes → `./dev.sh services` → `./dev.sh examples` or `./dev.sh integration` as needed.

Alternatively, use `docker-up.sh` for quicker setup on ports 9000/9001.

### Quality Gates

This project has two quality gates that should be run locally to match CI:

1. **Example drift detection** - verifies examples compile and work with the current codebase
   ```bash
   ./dev.sh examples
   # or directly: ./run-examples.sh
   ```

2. **Transport verification** - runs systest suite with memory, HTTP, and gRPC transports
   ```bash
   ./dev.sh integration
   # or directly: ./integration-tests.sh
   ```

Both are automatically run via `./dev.sh up` when the system tests stage executes.

### Database Migrations

```bash
# Run migrations (requires CFG_PRIMARY env var pointing to config file)
./migrator primary
```

## Code Style Guidelines

### General Principles

- **Documentation**: Limit lines to 120 characters for readability (matching book formatting)
- **Testing**: Always test code changes before presenting them
- **Git**: Do NOT use the `git` command (per GEMINI.md)

### Go Formatting

- Use `gofmt` and `goimports` for code formatting
- Run `gofmt -w -s .` or `goimports -w .` before committing
- No line length limit enforced by gofmt, but keep lines reasonable
- **Generated files**: Do NOT manually edit or run formatters/linters on protobuf-generated files (`*.pb.go`, `*_grpc.pb.go`). These are auto-generated from `.proto` files and excluded via `.golangci.yml`. To regenerate, use `protoc` with the appropriate plugins.

### Linting

This project uses [golangci-lint](https://golangci-lint.run/) for code quality. Run with:

```bash
golangci-lint run ./...
```

The linter is configured in `.golangci.yml`. Key rules:

- **Complexity**: Keep cyclomatic complexity under 8
- **Duplication**: No more than 80 lines of duplicate code (dupl)
- **Print statements**: Avoid `fmt.Print*` in non-CLI code - use structured logging instead
- **Parallel tests**: All tests must call `t.Parallel()`
- **Testify**: Use testify correctly (testifylint)

Note: The `cmd/` directory is excluded from most linters (CLI tools have different standards).

### Import Organization

Standard Go import grouping:

```go
import (
    "context"
    "fmt"
    
    "github.com/example/package"
    "github.com/meschbach/pgcqrs/pkg/v1"
    
    "go.opentelemetry.io/otel/trace"
    "golang.org/x/exp/slices"
)
```

Order: stdlib, external dependencies, internal packages.

### Naming Conventions

- **Packages**: Short, lowercase, e.g., `v1`, `query2`, `memory`
- **Types**: PascalCase, e.g., `Stream`, `QueryBuilder`, `TransportError`
- **Functions/Methods**: PascalCase, e.g., `MustSubmit`, `Perform`
- **Variables**: CamelCase, e.g., `ctx`, `err`, `harness`
- **Constants**: PascalCase or SCREAMING_SNAKE_CASE
- **Interfaces**: Often end with `-er`, e.g., `Transport`, `QueryResults`

### Code Quality

- **Complexity**: Keep cyclomatic complexity under 8 per function (gocyclo)
- **Duplication**: Avoid more than 80 lines of duplicate code (dupl)
- **Logging**: Use structured logging instead of `fmt.Print*` (forbidigo)
- **Exhaustiveness**: Use exhaustive enums where applicable

### Error Handling

Two patterns are used in this codebase:

1. **Panic-style (Must functions)**: For fatal errors that should crash
   ```go
   func (s *Stream) MustSubmit(ctx context.Context, kind string, event interface{}) *Submitted {
       out, err := s.Submit(ctx, kind, event)
       junk.Must(err)  // panics on error
       return out
   }
   ```

2. **Regular error returns**: For recoverable errors
   ```go
   func (s *Stream) Submit(ctx context.Context, kind string, event interface{}) (*Submitted, error) {
       return s.system.Transport.Submit(ctx, s.domain, s.stream, kind, event)
   }
   ```

Use `junk.Must(err)` from `internal/junk` for panic-style error handling. Use standard error returns for public APIs.

### Custom Error Types

Implement the error interface with wrapped errors:
```go
type TransportError struct {
    Underlying error
}

func (t *TransportError) Error() string {
    return fmt.Sprintf("transport error: %s", t.Underlying.Error())
}

func (t *TransportError) Unwrap() error {
    return t.Underlying
}
```

### Handling Deferred Close Errors

When using `defer` to close resources (like `resp.Body.Close()`), don't ignore the error. Use `errors.Join`:

```go
// Pattern 1: Using defer with closure
resp, err := c.wire.Do(req)
if err != nil {
    return err
}
defer func() { err = errors.Join(err, resp.Body.Close()) }()

// Pattern 2: Direct close with multiple return paths
resp, err := c.wire.Do(req)
if err != nil {
    return err
}
closeErr := resp.Body.Close()

if resp.StatusCode != 200 {
    return errors.Join(&BadResponseCode{URL: url, Code: resp.StatusCode}, closeErr)
}
return result, closeErr
```

Use `errors.Join` to combine the close error with any operation errors.

### Testing Conventions

This project uses:

- **testify**: `github.com/stretchr/testify/assert` and `require`
- **faker**: `github.com/go-faker/faker/v4` for generating test data
- **paralleltest**: All tests must call `t.Parallel()` (enforced by linter)
- **testifylint**: Use testify correctly (see linter rules)

#### Test File Structure

```go
package v1

import (
    "context"
    "testing"
    "time"
    
    "github.com/go-faker/faker/v4"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestExample(t *testing.T) {
    // Use MemoryHarness for unit tests
    MemoryHarness(t, func(ctx context.Context, h Harness) {
        // test code here
        require.NoError(t, err)
        assert.Equal(t, expected, actual)
    })
}
```

#### MemoryHarness Pattern

Use the `MemoryHarness` helper for unit tests (found in `pkg/v1/query_test.go`):
```go
func MemoryHarness(t *testing.T, perform func(ctx context.Context, h Harness)) {
    t.Parallel()
    
    ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
    defer done()
    
    harness := Harness{
        appName:    faker.Name(),
        streamName: faker.Name(),
    }
    mem := NewMemoryTransport()
    harness.system = NewSystem(mem)
    harness.stream = harness.system.MustStream(ctx, harness.appName, harness.streamName)
    
    perform(ctx, harness)
}
```

#### System/Integration Tests

For tests requiring a running service:
```bash
export PGCQRS_TEST_TRANSPORT="memory"  # or "grpc"
export PGCQRS_TEST_URL="http://localhost:9000"
export PGCQRS_TEST_APP_BASE="systest-"
go test -count=1 --timeout 5s ./systest/...
```

### Context Usage

- Always accept `context.Context` as the first parameter
- Use `context.WithTimeout` for operations with deadlines
- Pass context to all transport and query operations

### Observability (OpenTelemetry)

The project uses OpenTelemetry for tracing. Use the package-level tracer:
```go
import "github.com/meschbach/pgcqrs/pkg/v1"

func ExampleFunction(ctx context.Context) {
    ctx, span := tracer.Start(ctx, "pgcqrs.ExampleFunction")
    defer span.End()
    // ... operation
}
```

### Project Structure

- `pkg/` - Public API packages (v1, query2, service)
- `internal/` - Internal implementation packages
- `cmd/` - CLI entrypoints (migrator, service, pgcqrs)
- `systest/` - System/integration tests
- `examples/` - Example applications
- `migrations/` - Database migrations
- `deploy/` - Deployment configurations

### Deprecation Notices

When deprecating functions, add a doc comment:
```go
// Deprecated: This method is superseded by the `pgcqrs/pkg/v1/query2` package...
func (s *Stream) Query() *QueryBuilder { ... }
```

### Configuration

Configuration is typically JSON-based. See `deploy/integration-tests/primary.json` for examples.

### Important Environment Variables

- `CFG_PRIMARY` - Path to primary configuration file
- `PGCQRS_SERVICE_TRANSPORT` - Transport type for examples (memory, http, grpc)
- `PGCQRS_SERVICE_URL` - URL for examples (e.g., http://localhost:9000 or localhost:9001)
- `PGCQRS_TEST_TRANSPORT` - Transport type for tests (memory, http, grpc)
- `PGCQRS_TEST_URL` - URL for integration tests
- `PGCQRS_TEST_APP_BASE` - App base name for tests
