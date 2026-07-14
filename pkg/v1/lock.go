package v1

import (
	"fmt"
	"time"
)

// Option is an interface for passing optional parameters to Submit.
type Option interface {
	apply(*submitConfig)
}

type submitConfig struct {
	lock *Lock
}

// Lock implements Option and represents an exclusive consumer lock assertion.
type Lock struct {
	Consumer string
	Holder   string
}

func (l *Lock) apply(c *submitConfig) {
	c.lock = l
}

// NewLock creates a new Lock with the given consumer and holder.
func NewLock(consumer, holder string) *Lock {
	return &Lock{Consumer: consumer, Holder: holder}
}

// LockResult contains the result of a TryAcquire operation.
type LockResult struct {
	Acquired       bool
	HeldBy         string
	GuaranteeUntil time.Time
	HeldUntil      time.Time
}

// LockState represents the current state of a consumer lock.
type LockState struct {
	Consumer       string
	Domain         string
	Stream         string
	Holder         string
	AcquiredAt     time.Time
	HeartbeatAt    time.Time
	TTL            time.Duration
	GuaranteeUntil time.Time
	HeldUntil      time.Time
}

// LockNotHeldError is returned when a lock assertion fails.
type LockNotHeldError struct {
	Consumer string
	Holder   string
	Domain   string
	Stream   string
}

func (e *LockNotHeldError) Error() string {
	return fmt.Sprintf("lock not held: consumer=%s holder=%s domain=%s stream=%s", e.Consumer, e.Holder, e.Domain, e.Stream)
}

// LockExpiredError is returned when a lock does not exist or has expired.
type LockExpiredError struct {
	Consumer string
	Domain   string
	Stream   string
}

func (e *LockExpiredError) Error() string {
	return fmt.Sprintf("lock expired or not found: consumer=%s domain=%s stream=%s", e.Consumer, e.Domain, e.Stream)
}

// HeartbeatConflictError is returned when a heartbeat carries a stale position.
type HeartbeatConflictError struct {
	TargetVersion  int64
	CurrentVersion int64
}

func (e *HeartbeatConflictError) Error() string {
	return fmt.Sprintf("heartbeat conflict: target_version=%d current_version=%d", e.TargetVersion, e.CurrentVersion)
}

// TTLTooLowError is returned when a lock TTL is below the minimum allowed value.
type TTLTooLowError struct {
	Provided time.Duration
	Minimum  time.Duration
}

func (e *TTLTooLowError) Error() string {
	return fmt.Sprintf("ttl %v is below minimum %v", e.Provided, e.Minimum)
}

// LockMinimumTTL is the minimum allowed TTL for a consumer lock.
const LockMinimumTTL = 6 * time.Second

// DefaultLockTTL is the default TTL for a consumer lock.
const DefaultLockTTL = 30 * time.Second

// DefaultGuaranteeFraction is the fraction of TTL that is the guarantee period (90%).
const DefaultGuaranteeFraction = 0.9
