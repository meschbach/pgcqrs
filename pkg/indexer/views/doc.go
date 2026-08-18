// Package views provides a KV-backed materialized view system built on the core indexer framework.
//
// Views enables typed per-kind event handlers, previous-state retrieval, atomic mutations,
// version-aware reads, and change notification. Define a projection with OnKind[T] handlers,
// run it, query it. The same projection works with in-memory storage for tests,
// PostgreSQL for production, and gRPC for remote access.
//
// # Quick Start
//
// Define a projection with typed handlers, create a client, and query it.
// See the Example functions for complete, tested code:
//
//   - ExampleNewProjection — defining a projection with OnKind handlers
//   - ExampleClient_Get — querying entities with version constraints
//   - ExampleClient_OnChange — registering change notifications
//   - ExampleUntilVersion — waiting for a specific version with timeout
//
// # Version-Aware Reads
//
// Views supports version constraints for consistent reads:
//
//   - After(version) — fail if projection is behind the specified version
//   - UntilVersion(version, timeout) — wait for the projection to reach a version
//
// # Change Notifications
//
// Register callbacks to observe changes as they occur. The callback signature is
// func(context.Context, Change) error, returning an unsubscribe function.
//
// # Transport Support
//
// The views framework supports multiple transports:
//
//   - Memory: In-process storage for testing
//   - gRPC: Remote storage for production
//   - PostgreSQL: Server-side storage (via gRPC services)
//   - HTTP: Not supported (returns error)
//
// The transport is automatically selected based on the system's connectivity.
//
// # Architecture
//
// The framework consists of:
//
//   - Projection: Defines event handlers and mutation logic
//   - Client: Provides query API and manages the pump lifecycle
//   - Store: KV storage interface (MemoryStore, RemoteStore, PGStore)
//   - Notifier: Broadcasts changes to observers
//   - Pump: Drives event processing (acquires lock, watches events, heartbeats)
//
// See the examples/ directory for complete working examples.
package views
