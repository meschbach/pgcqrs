## 1. Proto Changes

- [x] 1.1 Add `WaitForLockRequest`, `LockGranted` messages to `pkg/ipc/query.proto`
- [x] 1.2 Add `WaitForLock` RPC to `ConsumerLock` service (server-streaming: returns stream of `LockGranted`)
- [x] 1.3 Add `lock_lost` variant to `KeepAliveServerMessage` (server-initiated push)
- [x] 1.4 Regenerate protobuf code

## 2. Memory Transport Emitter

- [x] 2.1 Add `LockReleasedEvent` type to `pkg/v1/`
- [x] 2.2 Add `onLockRelease` emitter to memory transport struct (use `emitter.MutexDispatcher[LockReleasedEvent]`)
- [x] 2.3 Emit `LockReleasedEvent` on `Release()` call
- [x] 2.4 Emit `LockReleasedEvent` on TTL expiry cleanup in `TryAcquire()`
- [x] 2.5 Add `OnLockRelease()` method to memory transport
- [x] 2.6 Ensure listeners use buffered channel (size 1) with non-blocking send (matching `RequerySignal` pattern) to avoid deadlocking the actor goroutine
- [x] 2.7 Write unit tests for emitter firing on Release + TTL expiry

## 3. Wire[L Lock] Interface

- [x] 3.1 Refactor `Wire` interface to generic `Wire[L Lock]` with `Watch` + `WaitForLock` and move adapter implementations (`memoryLock`, `memoryWire`, `GrpcWire`) from `pkg/indexer/adapter.go` to `pkg/v1/`
- [x] 3.2 Remove `TryAcquire`, `NewKeepAlive`, `GetPosition` from Wire interface
- [x] 3.3 Implement `WaitForLock` on `MemoryWire` (emitter loop + TryAcquire + GetPosition)
- [x] 3.4 Implement `WaitForLock` on `GrpcWire` (call WaitForLock RPC, receive LockGranted, return position + heartbeat info)
- [x] 3.5 Write unit tests for MemoryWire.WaitForLock (notification-based acquisition)
- [x] 3.6 Write unit tests for GrpcWire.WaitForLock (server-side blocking)

## 4. Server-Side WaitForLock and LockLost Push

- [x] 4.1 Add `LockReleasedEvent` type and `onLockRelease` emitter to service bus (similar to `EventStorageEvent`)
- [x] 4.2 Add `dispatchOnLockReleased` method to bus
- [x] 4.3 Update `ConsumerStore.TryAcquire` to return `[]LockReleasedInfo` for expired locks overwritten during acquisition
- [x] 4.4 Update `ConsumerStore.Release` to return `LockReleasedInfo` for the released lock
- [x] 4.5 Update gRPC `TryAcquire` handler to emit `LockReleasedEvent` for each expired lock returned by store
- [x] 4.6 Update gRPC `Release` handler to emit `LockReleasedEvent` after successful release
- [x] 4.7 Implement `WaitForLock` handler in gRPC service (TryAcquire, subscribe to emitter, retry on event, handle TTL expiry via timer)
- [x] 4.8 Implement LockLost server-push: KeepAlive handler subscribes to `LockReleasedEvent` and sends `LockLost` message when lock is lost
- [x] 4.9 Write unit tests for server-side WaitForLock (blocking, acquisition, position return)
- [x] 4.10 Write unit tests for LockLost server-push (lock released, client receives LockLost)
- [x] 4.11 Write unit tests for LockReleasedEvent emission (explicit release, TTL expiry)

## 5. State Machine

- [x] 5.1 Define `PumpState` type with six constants (WaitingForLock, Acquiring, Watching, LockLost, Failed, Closed) in `pkg/indexer/`
- [x] 5.2 Define `PumpStateEvent` struct with State, Previous, Error fields
- [x] 5.3 Add event emitter (`emitter.MutexDispatcher[PumpStateEvent]`) and atomic state to Pump struct
- [x] 5.4 Implement `p.setState()` helper on Pump: stores state, emits event
- [x] 5.5 Thread state through Client: expose `State()`, `OnStateChange()`, `WaitForState()` on Client
- [x] 5.6 Write unit tests for state machine type definitions and transitions
- [x] 5.7 Write unit tests for Client state observability APIs (State, OnStateChange, WaitForState)

## 5A. Backoff Utility

- [x] 5A.1 Add `cenkalti/backoff/v5` as direct dependency in `go.mod`
- [x] 5A.2 Create backoff configuration helper in `pkg/indexer/` (exponential backoff with jitter, start 100ms, max 30s)
- [x] 5A.3 Write unit tests for backoff configuration

## 5B. Handler Error Interface

- [x] 5B.1 Define `HandlerError` interface in `pkg/indexer/` with `Recoverable() bool` method
- [x] 5B.2 Define `RecoverableError` struct that wraps errors and implements `HandlerError` with `Recoverable() == true`
- [x] 5B.3 Update `query2.Watch.TickWithID` to check if handler error implements `HandlerError` and propagate recoverability
- [x] 5B.4 Write unit tests for handler error interface and recoverability detection

## 6. Pump Outer Loop

- [x] 6.1 Refactor `RunWithDomainStream` to use generic `Wire[L Lock]`
- [x] 6.2 Implement outer loop: WaitingForLock → Acquiring → Watching → LockLost → Failed → Closed
- [x] 6.3 Call `wire.WaitForLock()` to acquire lock + position
- [x] 6.4 Set `p.position` from WaitForLock return value
- [x] 6.5 Setup watch from position (query.After(position))
- [x] 6.6 Handle watch setup failure: release lock, transition to LockLost, backoff (using cenkalti/backoff/v5), retry
- [x] 6.7 Handle watch stream failure (TickWithID error): release lock, transition to LockLost, backoff, re-acquire + re-watch
- [x] 6.8 Handle LockLost: backoff, re-acquire
- [x] 6.9 Handle context cancellation: transition to Closed, clean exit
- [x] 6.10 Reset backoff on successful lock acquisition
- [x] 6.11 Write unit tests for outer loop state transitions
- [x] 6.12 Write unit tests for backoff behavior (reset on success, exponential growth)

## 7. Pump Inner Loop

- [x] 7.1 Implement `runWatchLoop` with dedup guard: `if eventID <= position { continue }`
- [x] 7.2 Update `p.position` after successful Tick
- [x] 7.3 Implement unreliable heartbeat: any error except HeartbeatConflictError → LockLost (no retry)
- [x] 7.4 Handle `HeartbeatConflictError`: update position to CurrentVersion, continue (not LockLost)
- [x] 7.5 Handle watch stream failure (TickWithID error): return error to outer loop for recovery
- [x] 7.6 Handle handler errors: check if error implements `HandlerError`, if `Recoverable() == true` → LockLost, else → Failed
- [x] 7.7 Write unit tests for dedup guard (skip duplicate events)
- [x] 7.8 Write unit tests for unreliable heartbeat (any error except ConflictError → LockLost)
- [x] 7.9 Write unit tests for HeartbeatConflictError recovery (update position, continue)
- [x] 7.10 Write unit tests for handler error recoverability (recoverable → LockLost, non-recoverable → Failed)

## 8. Integration Tests

- [x] 8.1 Update `TestMultipleProjectionsOnSameStream` to use `WaitForState(PumpStateWatching)` instead of 500ms timeout
- [x] 8.2 Update `TestConcurrentReadersDuringUpdates` to use `WaitForState`
- [x] 8.3 Update `TestGRPCWatchDuplicateEvents` to use `WaitForState`
- [x] 8.4 Add test for dedup: submit duplicate events, verify handler called once
- [x] 8.5 Add test for unreliable heartbeat: simulate heartbeat failure, verify LockLost transition
- [x] 8.6 Add test for watch failure recovery: simulate watch stream error, verify LockLost + re-acquire + re-watch
- [x] 8.7 Run full test suite: `go test -count=1 ./pkg/... ./internal/...` and verify all pass
- [x] 8.8 Run `./dev.sh up` and verify system tests pass (memory, HTTP, gRPC)

## 9. Metrics and Observability

- [x] 9.1 Add OTel counter for pump state transitions (by state)
- [x] 9.2 Add OTel counter for lock loss events
- [x] 9.3 Add OTel counter for watch stream failures
- [x] 9.4 Write unit tests for metrics emission

## 10. gRPC System Tests

- [x] 10.1 Add systest for WaitForLock over gRPC (client blocks, server notifies on release)
- [x] 10.2 Add systest for LockLost server-push (client receives LockLost when lock released)
- [x] 10.3 Add systest for concurrent WaitForLock (multiple waiters, first-to-acquire-wins)
- [x] 10.4 Add systest for pump state machine transitions over gRPC
