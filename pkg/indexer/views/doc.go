// Package views provides a KV-backed materialized view system built on the core indexer framework.
//
// Views enables typed per-kind event handlers, previous-state retrieval, atomic mutations,
// version-aware reads, and change notification. Define a projection with OnKind[T] handlers,
// run it, query it. The same projection works with in-memory storage for tests,
// PostgreSQL for production, and gRPC for remote access.
package views
