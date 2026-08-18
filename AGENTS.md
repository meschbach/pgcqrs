# AGENTS.md - PGCQRS Development Guide

This file provides guidance for agentic coding agents operating in the pgcqrs repository.

## Project Overview

PGCQRS is a Go project that provides a JSON event store with multi-tenancy and observability support, backed by PostgreSQL. The project requires Go 1.26+.

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

# Run storage integration tests (testcontainers with PostgreSQL)
./dev.sh storage-integ
```

**Typical workflow**: Make code changes → `./dev.sh services` → `./dev.sh examples` or `./dev.sh integration` as needed.

Alternatively, use `docker-up.sh` for quicker setup on ports 9000/9001.

### Local Development Experience (DevXP)

This project provides comprehensive local testing capabilities that mirror CI/CD pipelines. All tests can be run locally without external dependencies beyond Docker.

#### Testing Layers

**1. Unit Tests** (no external dependencies)
```bash
go test -count=1 ./pkg/... ./internal/...
```
- Tests pure logic, data structures, and algorithms
- Uses in-memory transports and mocks
- Fast execution (< 5 seconds)

**2. Storage Integration Tests** (testcontainers)
```bash
./dev.sh storage-integ
# or directly:
go test -tags testcontainers_pg -timeout 30s -count 1 -v ./internal/service/storage/...
```
- Tests PostgreSQL-specific storage logic
- Uses testcontainers-go to spin up ephemeral PostgreSQL instances
- Tests consumer locks, positions, queries, and migrations
- Requires Docker but no manual setup

**3. System Tests** (all transports)
```bash
./dev.sh integration
# or directly:
./integration-tests.sh
```
- Tests the full system across all three transports:
  - **Memory transport**: In-process, no network
  - **HTTP transport**: REST API over HTTP
  - **gRPC transport**: gRPC with bidirectional streaming
- Requires running service (via `./dev.sh services` or `docker-up.sh`)
- Validates transport-agnostic behavior

**4. Example Drift Detection**
```bash
./dev.sh examples
# or directly:
./run-examples.sh
```
- Runs all example applications against all transports
- Ensures examples stay in sync with API changes
- Catches breaking changes early

**5. Full Pipeline**
```bash
./dev.sh up
```
- Runs all tests in sequence:
  1. Unit tests
  2. Storage integration tests (testcontainers)
  3. Integration tests (requires running service)
  4. Example drift detection
- Mirrors CI/CD pipeline exactly
- Use this before committing to ensure nothing breaks

#### Test Tags and Build Constraints

Some tests require specific build tags:
- `testcontainers_pg`: Tests that need PostgreSQL via testcontainers
- Tests are automatically skipped if tag is not provided

Example:
```bash
# Run testcontainers tests
go test -tags testcontainers_pg ./internal/service/storage/...

# Run without testcontainers (tests will be skipped)
go test ./internal/service/storage/...
```

#### Environment Variables

System tests use environment variables to configure transport:
- `PGCQRS_TEST_TRANSPORT`: `memory`, `http`, or `grpc`
- `PGCQRS_TEST_URL`: Service URL (e.g., `http://localhost:9000` or `localhost:9001`)
- `PGCQRS_TEST_APP_BASE`: App name prefix for test isolation

The `integration-tests.sh` script automatically sets these for each transport.

#### Docker Compose Services

The local development environment includes:
- **PostgreSQL 18**: Primary database (port 26113)
- **pgcqrs service**: HTTP (port 26000) + gRPC (port 26001)
- **OpenTelemetry Collector**: For tracing (port 16001)

Services are defined in `docker-compose.yaml` and can be managed via:
```bash
# Start services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down
```

#### Testing gRPC-Specific Features

gRPC-specific features (consumer locks, bidirectional streaming, WaitForLock) can be tested locally:

1. **Unit tests**: Test gRPC handlers directly with testcontainers
   ```bash
   go test -tags testcontainers_pg -v ./internal/service/...
   ```

2. **System tests**: Test gRPC client against running service
   ```bash
   export PGCQRS_TEST_TRANSPORT=grpc
   export PGCQRS_TEST_URL=localhost:26001
   go test -v ./systest/...
   ```

3. **Examples**: Run gRPC examples
   ```bash
   ./dev.sh examples  # Runs all examples including gRPC
   ```

#### Common Testing Patterns

**Testing with Memory Transport** (fastest, no setup):
```bash
go test -v ./pkg/v1/...
```

**Testing with Real Database** (testcontainers):
```bash
go test -tags testcontainers_pg -v ./internal/service/storage/...
```

**Testing Full System** (requires running service):
```bash
./dev.sh up  # Full pipeline
# or
./dev.sh integration  # Just system tests
```

**Testing Specific Transport**:
```bash
# Memory
PGCQRS_TEST_TRANSPORT=memory go test ./systest/...

# HTTP
PGCQRS_TEST_TRANSPORT=http PGCQRS_TEST_URL=http://localhost:26000 go test ./systest/...

# gRPC
PGCQRS_TEST_TRANSPORT=grpc PGCQRS_TEST_URL=localhost:26001 go test ./systest/...
```

#### Troubleshooting

**Tests fail with "connection refused"**:
- Ensure services are running: `docker-compose ps`
- Start services: `./dev.sh services` or `docker-compose up -d`

**Testcontainers tests fail**:
- Ensure Docker is running
- Check Docker socket permissions
- Increase Docker memory limit if needed

**Port conflicts**:
- Check if ports 26000, 26001, 26113 are in use
- Stop conflicting services or change ports in `docker-compose.yaml`

**gRPC tests skip unexpectedly**:
- Ensure `PGCQRS_TEST_TRANSPORT=grpc` is set
- Ensure gRPC service is running on port 26001

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
- **Protocol Design**: Use relative times (e.g., `ttl_seconds`, `timeout_ms`) instead of absolute timestamps in gRPC messages to avoid clock skew issues between client and server. Absolute timestamps are acceptable for logging and metrics.

### Go Formatting

- Use `gofmt` and `goimports` for code formatting
- Run `gofmt -w -s .` or `goimports -w .` before committing
- No line length limit enforced by gofmt, but keep lines reasonable
- **Generated files**: Do NOT manually edit or run formatters/linters on protobuf-generated files (`*.pb.go`, `*_grpc.pb.go`). These are auto-generated from `.proto` files and excluded via `.golangci.yml`. To regenerate, use `./gen-grpc.sh` (preferred) or `protoc` with the appropriate plugins.

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

<!-- OCR:START -->
## Open Code Review Instructions

These instructions are for AI assistants handling code review in this project.

Always open `.ocr/skills/SKILL.md` when the request:
- Asks for code review, PR review, or feedback on changes
- Mentions "review my code" or similar phrases
- Wants multi-perspective analysis of code quality
- Asks to map, organize, or navigate a large changeset

Use `.ocr/skills/SKILL.md` to learn:
- How to run the 8-phase review workflow
- How to generate a Code Review Map for large changesets
- Available reviewer personas and their focus areas
- Session management and output format

Keep this managed block so `ocr init` can refresh the instructions.
<!-- OCR:END -->
