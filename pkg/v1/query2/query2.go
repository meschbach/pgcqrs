// Package query2 provides a fluent API for building and executing more complex queries against a `pgcqrs` stream.
// It enables building queries with multiple clauses in a builder style.
//
// # Usage
//
// To create a new query, use `query2.NewQuery(stream)`. From there, you may add `OnKind` and `OnID` clauses.
//
// # Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/meschbach/pgcqrs/pkg/v1"
//		"github.com/meschbach/pgcqrs/pkg/v1/query2"
//	)
//
//	func main() {
//		// An in memory transport for example purposes
//		transport := v1.NewMemoryTransport()
//		stream := transport.Stream("example-stream")
//
//		// Create some events
//		ctx := context.Background()
//		_, err := stream.Submit(ctx, "test-kind", map[string]string{"which": "first"})
//		if err != nil {
//			panic(err)
//		}
//		id, err := stream.Submit(ctx, "test-kind", map[string]string{"which": "second"})
//		if err != nil {
//			panic(err)
//		}
//		_, err = stream.Submit(ctx, "another-kind", nil)
//		if err != nil {
//			panic(err)
//		}
//
//		// Build our query
//		q := query2.NewQuery(stream)
//		q.OnKind("test-kind").Each(func(ctx context.Context, envelope v1.Envelope, event []byte) error {
//			fmt.Printf("Got test-kind event: %d\n", envelope.ID)
//			return nil
//		})
//		q.OnID(id).On(func(ctx context.Context, envelope v1.Envelope, event []byte) error {
//			fmt.Printf("Got specific event: %d\n", envelope.ID)
//			return nil
//		})
//
//		// Execute the query
//		if err := q.StreamBatch(ctx); err != nil {
//			panic(err)
//		}
//	}
//
// # Watching
//
// In addition to `StreamBatch`, queries may be executed with `Watch`. `Watch` will first query for all matching events, then continue to process events as they become available.
package query2
