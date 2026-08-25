## Context

The consumer-locks capability shipped with a minimal pump implementation. The pump does one-shot lock acquisition, no retry, no state visibility. Practical experience with view-projection-framework exposed reliability gaps:

- Flaky tests with 500ms timeouts masking deeper timing issues
- Hard-to-debug failures when the pump exits on any transient error
- Duplicate event processing in gRPC Watch causing incorrect projection state
- No way to know if the pump is ready before submitting events

The pump needs a resilient lifecycle with explicit state management, error recovery, and observability.

## Goals / Non-Goals

**Goals:**
- Explicit state machine with observable transitions
- Notification-based lock acquisition (no polling)
- Local position tracking for dedup
- Unreliable heartbeat (fail fast, hand off)
- Generic `Wire[L Lock]` interface
- Server-side `WaitForLock` RPC (server-streaming, separate from KeepAlive)
- Adapter code in `pkg/v1` (no transport code in `pkg/indexer`)

**Non-Goals:**
- Persistent pump state across restarts (server tracks position)
- Changing the Transport interface
- HTTP transport support (views require gRPC)

## Decisions

### Decision: State Machine with 6 States

```
WaitingForLock    - Blocked waiting to acquire lock (normal standby state)
Acquiring         - Lock acquired, setting up watch
Watching          - Actively processing events
LockLost          - Detected lock loss, will re-acquire
Failed            - Unrecoverable error
Closed            - Context done, exiting cleanly
```

**Transitions:**
```
[Initial] → WaitingForLock
WaitingForLock → Acquiring (lock acquired)
Acquiring → Watching (watch established)
Watching → LockLost (heartbeat failure, watch failure, server LockLost push)
Watching → Failed (unrecoverable error)
LockLost → WaitingForLock (re-acquire)
Any → Closed (context canceled)
```

**Key property**: `WaitingForLock` is the *steady state* for a standby pod. It's not a failure — it's the expected state for a service waiting to take over.

**LockLost is a distinct state**: Lock loss is observable as a separate event, enabling explicit tracking of lock loss occurrences. The pump transitions from LockLost back to WaitingForLock after backoff, then re-acquires the lock.

**Watch failure is recoverable**: If the watch stream fails (network error, server disconnect), the pump releases the lock, transitions to LockLost, and re-acquires + re-watches. This is consistent with heartbeat failure recovery.

### Decision: Notification-Based Lock Acquisition

**Polling is wrong for lock acquisition.** A standby pod checking every 1-30s wastes requests. Instead:

- **Memory transport**: The memory transport gains a lock-release emitter. When `Release()` is called or a lock expires via TTL, the emitter fires. The pump subscribes and wakes immediately when the lock becomes available.
- **gRPC transport**: A new `WaitForLock` server-streaming RPC. The client calls it, and the server blocks until the lock is available. The server uses an event-driven approach (not polling) to minimize latency.

**Backoff is still used** between `LockLost` → next `WaitForLock` attempt, to avoid tight loops on transient failures. The pump uses `cenkalti/backoff/v5` (already an indirect dependency via testcontainers) for exponential backoff with jitter.

### Decision: Local Position Tracking + Dedup

The Pump maintains an explicit `position` field:

```go
type Pump[L Lock] struct {
    wire     Wire[L]
    position int64  // last successfully processed event ID
    // ...
}
```

**Lifecycle:**
1. **Startup**: `position` returned by `WaitForLock` (server's last heartbeated position)
2. **Watch setup**: `query.After(position)` if position > 0
3. **Event processed**: `position = eventID` (updated after successful Tick)
4. **Heartbeat**: sends `position`
5. **Lock re-acquisition**: position returned by `WaitForLock`
6. **On restart**: position returned by `WaitForLock`

**Dedup guard**: `if eventID <= position { continue }` in the watch loop. This catches duplicates from any source — gRPC Watch bugs, Watch reconnection, memory race, etc.

**Three-layer dedup strategy:**
1. Server SQL `AfterID` filter (existing)
2. Server `grpcResultStream.lastSentID` guard (existing)
3. Client pump position check (new)

### Decision: Unreliable Heartbeat

**Any heartbeat error except HeartbeatConflictError → LockLost → release → re-acquire.** No retry.

Rationale: if the pump can't heartbeat, it can't make forward progress. The lock TTL is ticking. Better to release immediately and let someone else with better connectivity take over.

HeartbeatConflictError is a special case: it indicates the client sent a stale position. The pump updates its position to CurrentVersion and continues processing. This is not a failure — it's a correction.

This means at-least-once processing for events between the last successful heartbeat and the failed one. The dedup guard handles re-processing the same event in a single session, but a crash → handoff could reprocess 1-2 events. That's the safe direction.

### Decision: Generic `Wire[L Lock]` Interface

```go
type Wire[L Lock] interface {
    Watch(ctx context.Context, query *ipc.QueryIn) (v1.WatchInternal, error)
    WaitForLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (L, int64, error)
}
```

The generic type parameter `L` is necessary because Go return types are not covariant. Without generics, `WaitForLock` would return the `Lock` interface, but the concrete implementations (`*v1.KeepAlive` for gRPC, `*memoryLock` for memory) need to return their specific types. Go's type system doesn't allow a function returning `*v1.KeepAlive` to satisfy an interface requiring `Lock` return type.

**Circular imports are real, not theoretical**: The adapter implementations live in `pkg/v1` (gRPC adapter, memory transport), but the `Lock` interface is in `pkg/indexer`. If `Wire` were non-generic and returned `Lock`, `pkg/v1` would need to import `pkg/indexer`, creating a circular dependency. The generic approach avoids this by keeping the interface in `pkg/indexer` while allowing `pkg/v1` to return concrete types that structurally satisfy `Lock`.

**Future improvement**: The wire transports could be pulled into a separate package to eliminate the circular import concern entirely, but that's a separate design issue.

**Adapter code lives in `pkg/v1`**, not `pkg/indexer`. The framework is pure — no transport code.

### Decision: WaitForLock Proto

**Two separate RPCs** instead of dual-mode KeepAlive stream. This simplifies the handler logic and separates concerns cleanly.

```protobuf
// ConsumerLock service gains a new RPC
service ConsumerLock {
  rpc TryAcquire(TryAcquireIn) returns (TryAcquireOut);
  rpc Release(ReleaseIn) returns (ReleaseOut);
  rpc KeepAlive(stream KeepAliveClientMessage) returns (stream KeepAliveServerMessage);
  rpc ListLocks(ListLocksIn) returns (ListLocksOut);
  rpc WaitForLock(WaitForLockRequest) returns (stream LockGranted);  // NEW: server-streaming
}

// --- WaitForLock RPC (separate from KeepAlive) ---

message WaitForLockRequest {
    string domain = 1;
    string stream = 2;
    string consumer = 3;
    string holder = 4;
    int32 ttl_seconds = 5;
}

message LockGranted {
    int64 position = 1;
    int64 heartbeat_interval_ms = 2;  // milliseconds until next heartbeat expected (90% of TTL)
    int64 deadline_ms = 3;            // milliseconds until lock expires (full TTL)
}

// --- KeepAlive stream (unchanged + LockLost push) ---

message KeepAliveClientMessage {
    oneof message {
        KeepAliveHeartbeat heartbeat = 1;
        KeepAliveReleaseRequest release_request = 2;
    }
}

message KeepAliveServerMessage {
    oneof message {
        KeepAliveLockStatus lock_status = 1;
        KeepAliveReleaseAck release_ack = 2;
        LockLost lock_lost = 3;        // NEW: server-initiated push
    }
}

message LockLost {
    string reason = 1;  // "expired", "stolen", etc.
}
```

**Note:** Use relative times (e.g., `ttl_seconds`, `heartbeat_interval_ms`, `deadline_ms`) instead of absolute timestamps to avoid clock skew issues between client and server.

**WaitForLock lifecycle** (separate RPC):
1. Client calls `WaitForLock` RPC (server-streaming)
2. Server responds with `LockGranted` when lock acquired (single message, then stream ends)
3. Client then opens separate `KeepAlive` stream for heartbeat/release

**KeepAlive lifecycle** (existing, enhanced):
1. Client opens KeepAlive bidi stream
2. Client sends heartbeat → server responds with lock_status
3. Client sends release_request → server responds with release_ack
4. Server can push `LockLost` when lock is lost (stolen, expired)

**Rationale:** Two RPCs are simpler than dual-mode KeepAlive handler. WaitForLock is a blocking acquisition concern. KeepAlive handles ongoing heartbeat/release/lock-loss notification.

### Decision: LockLost Server-Push

The server can proactively notify clients when their lock is lost (stolen, expired). This avoids waiting for the next heartbeat to fail.

**Implementation**: The server-side KeepAlive handler uses a `LockReleasedEvent` emitter (similar to the existing `EventStorageEvent` bus pattern). When a lock is released (explicit Release or TTL expiry detected during TryAcquire), the event is emitted. The KeepAlive handler subscribes to this emitter for the specific (domain, stream, consumer) tuple and sends a `LockLost` message to the client when the event fires.

**No separate monitoring goroutine needed**: The event bus handles notification. The KeepAlive handler subscribes on behalf of each connected client and sends `LockLost` when the emitter fires.

### Decision: Server-Side WaitForLock

The server implements `WaitForLock` using an event-driven approach (not polling) to minimize lock acquisition latency. Web use cases require sub-200ms response times, and polling with even a 1s interval would waste up to 1s on lock transitions.

**Implementation:**
1. Attempt `TryAcquire` immediately
2. If acquired → return `LockGranted` with position
3. If not acquired → subscribe to `LockReleasedEvent` emitter and wait
4. Set a timer for the current holder's `held_until` (TTL expiry fallback)
5. On event or timer → retry `TryAcquire`
6. Repeat until acquired or client disconnects

**TTL expiry handling:** Since there's no background reaper, TTL expiry is only detected lazily. The waiter sets a timer for the current holder's `held_until` to ensure the waiter doesn't block indefinitely if the holder crashes without releasing.

**Client crash detection:** Rely on gRPC keepalive (~30s timeout) to detect crashed clients. When a client crashes, the gRPC connection is eventually closed by keepalive, causing `stream.Recv()` to return an error and the WaitForLock handler to exit. This is simpler than adding a server-side timeout and sufficient for our use case.

**Testing:** The event-driven complexity is driven out via unit tests covering: immediate acquisition, blocked acquisition with release notification, TTL expiry timer firing, client disconnect during wait.

### Decision: Concurrent WaitForLock Handling

When multiple clients call WaitForLock simultaneously and the lock is released, all waiters wake up and attempt TryAcquire. **First-to-acquire-wins** — the failed waiters go back to waiting on the emitter.

**Rationale:** We don't expect many concurrent waiters (typically 1-2 standby pods). FIFO queuing adds complexity without significant benefit. Thundering herd is not a concern at this scale.

### Decision: Pump State Machine Metrics

The pump emits OTel metrics for observability:

- **State transitions counter** (by state): Tracks how often each state is entered
- **Lock loss counter**: Tracks explicit lock loss events
- **Watch failure counter**: Tracks watch stream failures

**No histograms** for lock hold times or wait times. These are long-held (minutes/hours), and the distribution is less actionable than simple counts. If duration distributions become valuable later, they can be added.

### Decision: Testing Strategy

Three layers of testing:

1. **Unit tests**: Mock emitter for WaitForLock, mock Wire for pump state machine
2. **Integration tests**: Memory transport with real emitter, test lock acquisition/release/notification
3. **gRPC system tests**: Full stack with real gRPC service, test WaitForLock over network, LockLost server-push, concurrent waiters

### Decision: Adapter Code in pkg/v1

```go
// pkg/v1/grpcAdapter.go
type GrpcWire struct{ *GrpcAdapter }
func (g *GrpcWire) WaitForLock(ctx, domain, stream, consumer, holder string, ttl time.Duration) (*KeepAlive, int64, error) {
    // call WaitForLock RPC (server-streaming), receive LockGranted
    // LockGranted includes position, heartbeat_interval_ms, deadline_ms
    return keepAlive, granted.Position, nil
}
// *KeepAlive already has Heartbeat/Release → structurally satisfies Lock

// pkg/v1/memory.go (or separate file)
type MemoryWire struct{ Transport }
func (w *MemoryWire) WaitForLock(ctx, domain, stream, consumer, holder string, ttl time.Duration) (*memoryLock, int64, error) {
    // emitter loop + TryAcquire
    return &memoryLock{...}, position, nil
}
```

Both in `pkg/v1`. Nothing in `pkg/indexer` except the generic interface.

### Decision: LockReleasedEvent Emission Architecture

The `LockReleasedEvent` must fire on both explicit `Release()` and TTL expiry detected during `TryAcquire()`. The emission follows the existing pattern used for `EventStorageEvent`:

**Service layer emits, storage layer returns domain events:**

```go
// Storage layer: ConsumerStore.TryAcquire returns info about expired locks
func (cs *ConsumerStore) TryAcquire(...) (*LockResult, []LockReleasedInfo, error) {
    // ... acquire logic ...
    // If overwriting expired lock, return info about the expired lock
    return result, expiredLocks, nil
}

// Service layer: gRPC handler emits after storage succeeds
func (g *grpcConsumerLock) TryAcquire(ctx context.Context, in *ipc.TryAcquireIn) (*ipc.TryAcquireOut, error) {
    result, expiredLocks, err := g.consumerStore.TryAcquire(...)
    if err != nil { return nil, err }

    // Emit events for expired locks
    for _, expired := range expiredLocks {
        g.bus.dispatchOnLockReleased(ctx, expired.Domain, expired.Stream, expired.Consumer, expired.Holder)
    }
    return translateResult(result), nil
}

// Explicit release also emits
func (g *grpcConsumerLock) Release(ctx context.Context, in *ipc.ReleaseIn) (*ipc.ReleaseOut, error) {
    err := g.consumerStore.Release(...)
    if err != nil { return nil, err }
    g.bus.dispatchOnLockReleased(ctx, domain, stream, consumer, holder)
    return &ipc.ReleaseOut{Ok: true}, nil
}
```

**Rationale:** This matches the existing pattern where the service layer (not storage layer) is responsible for emitting domain events. The storage layer remains pure and isolated from the bus concept.

### Decision: Handler Error Interface

Handlers registered with the pump can return errors. Not all errors should be treated the same — some are recoverable (transient failures), some are not (permanent failures).

**Handlers return a special error interface that indicates recoverability:**

```go
// HandlerError is returned by handlers to indicate error semantics
type HandlerError interface {
    error
    Recoverable() bool  // true if pump should retry, false if pump should exit
}

// Default behavior: plain errors are NOT recoverable
// Handlers can wrap errors to indicate recoverability:
type RecoverableError struct {
    Err error
}

func (e *RecoverableError) Error() string { return e.Err.Error() }
func (e *RecoverableError) Unwrap() error { return e.Err }
func (e *RecoverableError) Recoverable() bool { return true }

// Usage in handler:
func MyHandler(ctx context.Context, e Envelope, body json.RawMessage) error {
    if transientFailure {
        return &RecoverableError{Err: errors.New("temporary network issue")}
    }
    if permanentFailure {
        return errors.New("invalid data")  // plain error = not recoverable
    }
    return nil
}
```

**Pump behavior:**
- Handler returns `nil` → continue processing
- Handler returns `HandlerError` with `Recoverable() == true` → transition to LockLost, backoff, retry
- Handler returns plain `error` or `HandlerError` with `Recoverable() == false` → transition to Failed, exit

**Rationale:** Default to non-recoverable (safe direction). Handlers that encounter transient failures can explicitly opt into recovery by returning `RecoverableError`.

## Failure Mode Analysis

| Failure | Detection | Recovery |
|---------|-----------|----------|
| **Lock held by another** | `WaitForLock` blocks, server notifies when available | Normal standby, wait for notification |
| **Lock stolen during watching** | `Heartbeat` returns error | LockLost → re-acquire |
| **Lock TTL exceeded** | `Heartbeat` returns error | LockLost → re-acquire |
| **Stale heartbeat position** | `Heartbeat` returns `HeartbeatConflictError` | Use `CurrentVersion`, retry |
| **Watch stream failure** | `TickWithID` returns error | Release lock, backoff, re-acquire + re-watch |
| **Heartbeat network failure** | `Heartbeat` returns error (not ConflictError) | LockLost → re-acquire (no retry) |
| **Setup Watch() failure** | `query.Watch()` returns error | Release lock, backoff, retry |
| **Pump goroutine panics** | Runtime panic | Process restarts via orchestrator |
| **Process killed (SIGKILL)** | No cleanup | Lock TTL expires (~30s), re-acquired by another |
| **Graceful shutdown** | Context canceled | Clean exit via `Closed` state, lock released |

## Risks / Trade-offs

- [Risk] Unreliable heartbeat means at-least-once processing → Mitigation: dedup guard handles re-processing in a single session. Handlers should be idempotent where possible.
- [Risk] Server-side `WaitForLock` adds complexity to the gRPC service → Mitigation: same stream shape as existing `KeepAlive`, just new message variants.
- [Risk] Generic `Wire[L Lock]` makes the Pump generic → Mitigation: type inference handles most cases, user code is clean.
- [Risk] Moving adapter code to `pkg/v1` is a breaking change → Mitigation: we haven't shipped yet, breaking changes are OK.
