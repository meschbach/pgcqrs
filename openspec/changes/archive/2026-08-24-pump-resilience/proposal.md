## Why

The consumer-locks capability shipped with a minimal pump implementation. The pump does one-shot lock acquisition, no
retry, no state visibility. Practical experience with view-projection-framework exposed reliability gaps:

- Flaky tests with 500ms timeouts masking deeper timing issues
- Hard-to-debug failures when the pump exits on any transient error
- Duplicate event processing in gRPC Watch causing incorrect projection state
- No way to know if the pump is ready before submitting events

Before GA, the pump needs a resilient lifecycle with explicit state management, error recovery, and observability.

## What Changes

- Pump gains an explicit state machine with observable transitions
- Lock acquisition becomes notification-based (emitter on memory, blocking stream on gRPC) instead of polling
- Pump maintains local position for dedup — suppresses duplicates from any transport source
- Heartbeat is unreliable — any error releases the lock immediately rather than retrying into TTL expiry
- Wire interface refactored to generic `Wire[L Lock]` with only `Watch` + `WaitForLock`
- Server-side `WaitForLock` RPC implemented (server-streaming, separate from KeepAlive)
- Adapter code moved out of `pkg/indexer` to `pkg/v1` (no transport code in framework)

## Capabilities

### Modified Capabilities

- `consumer-locks`: Pump lifecycle hardened with state machine, notification-based acquisition, position dedup,
  unreliable heartbeat, and handler error recoverability

## Impact

- `pkg/indexer/pump.go`: Major rewrite — outer loop becomes resilient with explicit state machine
- `pkg/indexer/wire.go`: Refactor to generic `Wire[L Lock]` with `Watch` + `WaitForLock`
- `pkg/indexer/lock.go`: No changes (interface already clean)
- `pkg/v1/grpcAdapter.go`: Add `WaitForLock` RPC call, return position
- `pkg/v1/memory.go`: Add lock-release emitter, emit on `Release()` + TTL expiry
- `pkg/ipc/query.proto`: Add `WaitForLock` RPC (server-streaming), `LockGranted` message, `LockLost` message on
  KeepAlive stream
- `internal/service/storage/consumer.go`: Update `TryAcquire` and `Release` to return expired lock info
- `internal/service/grpcConsumerLocks.go`: Emit `LockReleasedEvent` after lock operations, implement `WaitForLock`
  handler
- `internal/service/bus.go`: Add `LockReleasedEvent` emitter
- Tests: Unit (memory), integration (gRPC), systest (dedup, state transitions)
