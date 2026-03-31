## 1. Database Migration

- [x] 1.1 Create migration: add `consumer_positions` table with (domain, stream, consumer, event_id, updated_at)
- [x] 1.2 Add down migration to drop the table

## 2. Proto Definitions

- [x] 2.1 Add `ConsumerPosition` messages to query.proto (SetPositionIn, SetPositionOut, GetPositionIn, GetPositionOut, etc.)
- [x] 2.2 Add `after_id` optional field to QueryIn message
- [x] 2.3 Regenerate protobuf Go code

## 3. Storage Layer

- [x] 3.1 Create storage package for consumer positions (internal/service/storage/position.go)
- [x] 3.2 Implement SetPosition storage operation
- [x] 3.3 Implement GetPosition storage operation
- [x] 3.4 Implement ListConsumers storage operation
- [x] 3.5 Implement DeletePosition storage operation
- [x] 3.6 Add storage operation for AfterID filtering in queries

## 4. Transport Interface

- [x] 4.1 Add position methods to Transport interface (SetPosition, GetPosition, ListConsumers, DeletePosition)
- [x] 4.2 Add AfterID to WireQuery struct

## 5. Memory Transport Implementation

- [x] 5.1 Implement SetPosition in memory transport (fixed backward check)
- [x] 5.2 Implement GetPosition in memory transport
- [x] 5.3 Implement ListConsumers in memory transport
- [x] 5.4 Implement DeletePosition in memory transport

## 6. gRPC Service

- [x] 6.1 Add ConsumerPosition service to query.proto
- [x] 6.2 Implement gRPC handlers for position CRUD
- [x] 6.3 Register new service in gRPC server
- [x] 6.4 Generate gRPC Go code

## 7. REST API

- [x] 7.1 Add REST endpoints for position CRUD (/v1/domains/{domain}/streams/{stream}/positions/{consumer})
- [x] 7.2 Add REST support for AfterID query parameter

## 8. query2 Package

- [x] 8.1 Add After() method to Query builder
- [x] 8.2 Wire After() through to proto request
- [x] 8.3 Add tests for After() functionality

## 9. Tests and Integration

- [x] 9.1 Add unit tests for storage layer
- [x] 9.2 Add unit tests for memory transport position methods
- [x] 9.3 Add integration tests for gRPC position operations
- [x] 9.4 Add integration tests for AfterID query filter

## 10. Documentation

- [x] 10.1 Update API documentation for new endpoints
- [x] 10.2 Add usage examples for consumer position tracking

## 11. SetPosition Refactor (Single Query + New Return Type)

- [x] 11.1 Refactor storage.SetPosition to single atomic query with CTE + RETURNING
- [x] 11.2 Update storage.SetPosition return type to SetPositionResult{PreviousEventID, CurrentEventID}
- [x] 11.3 Update Transport interface SetPosition signature to return SetPositionResult
- [x] 11.4 Update memory transport SetPosition to return previous position
- [x] 11.5 Update gRPC adapter SetPosition to handle new return type
- [x] 11.6 Update REST adapter SetPosition to handle new return type
- [x] 11.7 Update service layer to handle new return type
- [x] 11.8 Update tests to expect new behavior:
  - [x] 11.8.1 Add test for stream-not-found returning error
  - [x] 11.8.2 Add test for idempotent same-position success
  - [x] 11.8.3 Add test verifying previousEventID is returned
  - [x] 11.8.4 Update existing tests to match new return type

## 12. Verification Fixes

- [x] 12.1 Fix storage.SetPosition to return StreamNotFoundError for non-existent stream (remove auto-create behavior)
- [x] 12.2 Add test for stream-not-found scenario returning StreamNotFoundError
- [x] 12.3 Fix context.Background/TODO usage in position_test.go to use t.Context()
- [x] 12.4 Fix context.TODO usage in after_test.go to use t.Context()
- [x] 12.5 Fix unnamed return values in grpcAdapter.go GetPosition method
- [x] 12.6 Update InsertCreatesStream test to expect error for non-existent stream
