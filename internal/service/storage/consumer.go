package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// StreamNotFoundError is returned when a consumer attempts to set position on a non-existent stream.
type StreamNotFoundError struct {
	Domain string
	Stream string
}

func (e *StreamNotFoundError) Error() string {
	return "stream does not exist: " + e.Domain + "/" + e.Stream
}

// SetPositionResult contains the result of a SetPosition operation.
type SetPositionResult struct {
	PreviousEventID *int64
	CurrentEventID  int64
}

// BackwardPositionError is returned when a consumer attempts to set position backwards.
type BackwardPositionError struct {
	Consumer  string
	Current   int64
	Requested int64
}

func (e *BackwardPositionError) Error() string {
	return "cannot set position backwards"
}

// OperationError wraps database errors with context about the operation.
type OperationError struct {
	Operation  string
	Underlying error
}

func (e *OperationError) Error() string {
	return e.Operation + ": " + e.Underlying.Error()
}

func (e *OperationError) Unwrap() error {
	return e.Underlying
}

// ConsumerStore provides storage operations for consumer locks and positions.
// It consolidates PositionStore functionality with lock lifecycle operations.
type ConsumerStore struct {
	pg *pgxpool.Pool
}

// NewConsumerStore creates a new ConsumerStore instance.
func NewConsumerStore(pg *pgxpool.Pool) *ConsumerStore {
	return &ConsumerStore{pg: pg}
}

// resolveConsumerName resolves a consumer name to its consumer_id via the consumer_names
// enumeration table. Inserts the name if it doesn't exist (idempotent).
func (s *ConsumerStore) resolveConsumerName(ctx context.Context, name string) (int64, error) {
	_, err := s.pg.Exec(ctx,
		`INSERT INTO consumer_names (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`, name)
	if err != nil {
		return 0, &OperationError{Operation: "insert consumer name", Underlying: err}
	}

	var consumerID int64
	err = s.pg.QueryRow(ctx,
		`SELECT id FROM consumer_names WHERE name = $1`, name).Scan(&consumerID)
	if err != nil {
		return 0, &OperationError{Operation: "select consumer id", Underlying: err}
	}
	return consumerID, nil
}

// resolveConsumerNameInTx resolves a consumer name within a transaction.
func (s *ConsumerStore) resolveConsumerNameInTx(ctx context.Context, tx pgx.Tx, name string) (int64, error) {
	_, err := tx.Exec(ctx,
		`INSERT INTO consumer_names (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`, name)
	if err != nil {
		return 0, &OperationError{Operation: "insert consumer name", Underlying: err}
	}

	var consumerID int64
	err = tx.QueryRow(ctx,
		`SELECT id FROM consumer_names WHERE name = $1`, name).Scan(&consumerID)
	if err != nil {
		return 0, &OperationError{Operation: "select consumer id", Underlying: err}
	}
	return consumerID, nil
}

// ResolveAndCheckLock resolves a consumer name and verifies the lock is held within a transaction.
func (s *ConsumerStore) ResolveAndCheckLock(ctx context.Context, tx pgx.Tx, domain, stream string, lock *v1.Lock) error {
	consumerID, err := s.resolveConsumerNameInTx(ctx, tx, lock.Consumer)
	if err != nil {
		return err
	}

	var held bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM consumer_locks cl
			JOIN events_stream es ON cl.stream_id = es.id
			WHERE es.app = $1 AND es.stream = $2 AND cl.consumer_id = $3
			  AND cl.holder = $4
			  AND cl.held_until > NOW()
		)`, domain, stream, consumerID, lock.Holder).Scan(&held)
	if err != nil {
		return err
	}
	if !held {
		return &v1.LockNotHeldError{
			Consumer: lock.Consumer,
			Holder:   lock.Holder,
			Domain:   domain,
			Stream:   stream,
		}
	}
	return nil
}

func lockAttrSet(domain, stream, consumer string) attribute.Set {
	return attribute.NewSet(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
		attribute.String("consumer-lock.consumer", consumer),
	)
}

func isConflict(conflictHolder *string, holder string) bool {
	return conflictHolder != nil && *conflictHolder != holder
}

// TryAcquire attempts to acquire an exclusive lock for a consumer on a stream.
// Returns a LockResult indicating whether the lock was acquired and, if not,
// who currently holds it.
func (s *ConsumerStore) TryAcquire(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (out *v1.LockResult, retErr error) {
	ctx, span := tracer.Start(ctx, "consumerStore.TryAcquire", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
		attribute.String("consumer-lock.consumer", consumer),
		attribute.String("consumer-lock.holder", holder),
		attribute.Float64("consumer-lock.ttl", ttl.Seconds()),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		if out != nil {
			span.SetAttributes(
				attribute.Bool("consumer-lock.acquired", out.Acquired),
			)
		}
		span.End()
	}()

	attrs := metric.WithAttributeSet(lockAttrSet(domain, stream, consumer))
	AcquireAttempts.Add(ctx, 1, attrs)

	if ttl < v1.LockMinimumTTL {
		AcquireFailures.Add(ctx, 1, attrs)
		return nil, &v1.TTLTooLowError{Provided: ttl, Minimum: v1.LockMinimumTTL}
	}

	consumerID, err := s.resolveConsumerName(ctx, consumer)
	if err != nil {
		return nil, err
	}

	guaranteeUntil := time.Now().Add(time.Duration(float64(ttl) * v1.DefaultGuaranteeFraction))
	heldUntil := time.Now().Add(ttl)

	row := s.pg.QueryRow(ctx, `
		INSERT INTO consumer_locks (stream_id, consumer_id, holder, ttl, guarantee_until, held_until)
		SELECT es.id, $2, $3, $4, $5, $6
		FROM events_stream es
		WHERE es.app = $1 AND es.stream = $7
		ON CONFLICT (stream_id, consumer_id) DO UPDATE
		SET holder = EXCLUDED.holder,
		    acquired_at = NOW(),
		    heartbeat_at = NOW(),
		    ttl = EXCLUDED.ttl,
		    guarantee_until = EXCLUDED.guarantee_until,
		    held_until = EXCLUDED.held_until
		WHERE consumer_locks.held_until < NOW()
		RETURNING (
			SELECT cl.holder
			FROM consumer_locks cl
			WHERE cl.stream_id = consumer_locks.stream_id
			  AND cl.consumer_id = consumer_locks.consumer_id
			  AND cl.held_until > NOW()
		)`, domain, consumerID, holder, ttl, guaranteeUntil, heldUntil)

	// cleanExpiredLocks is called before row.Scan() because QueryRow returns a Row object
	// immediately but doesn't execute the query until Scan() is called. This allows the
	// cleanup to proceed while the database is preparing the INSERT query result.
	s.cleanExpiredLocks(ctx, domain, stream, consumerID)

	var conflictHolder *string
	err = row.Scan(&conflictHolder)
	if err != nil {
		if err == pgx.ErrNoRows {
			AcquireFailures.Add(ctx, 1, attrs)
			return nil, &StreamNotFoundError{Domain: domain, Stream: stream}
		}
		return nil, &OperationError{Operation: "try acquire lock", Underlying: err}
	}

	if isConflict(conflictHolder, holder) {
		AcquireFailures.Add(ctx, 1, attrs)
		out = &v1.LockResult{
			Acquired:       false,
			HeldBy:         *conflictHolder,
			GuaranteeUntil: guaranteeUntil,
			HeldUntil:      heldUntil,
		}
		return out, nil
	}

	AcquireSuccesses.Add(ctx, 1, attrs)
	activeLocks.Add(ctx, 1, attrs)
	out = &v1.LockResult{
		Acquired:       true,
		HeldBy:         holder,
		GuaranteeUntil: guaranteeUntil,
		HeldUntil:      heldUntil,
	}
	return out, nil
}

// cleanExpiredLocks removes up to 128 expired lock rows in the same (domain, stream) partition.
// Errors are logged as span events but do not fail the caller.
func (s *ConsumerStore) cleanExpiredLocks(ctx context.Context, domain, stream string, excludeConsumerID int64) {
	tag, cleanupErr := s.pg.Exec(ctx, `
		DELETE FROM consumer_locks cl
		WHERE cl.stream_id IN (
			SELECT es.id FROM events_stream es WHERE es.app = $1 AND es.stream = $2
		) AND cl.held_until < NOW()
		AND cl.consumer_id != $3
		LIMIT 128`, domain, stream, excludeConsumerID)
	if cleanupErr != nil {
		trace.SpanFromContext(ctx).AddEvent("cleanup.expired_locks_failed", trace.WithAttributes(
			attribute.String("consumer-lock.error", cleanupErr.Error()),
		))
		return
	}
	if tag.RowsAffected() > 0 {
		CleanupDeleted.Add(ctx, tag.RowsAffected(), metric.WithAttributeSet(lockAttrSet(domain, stream, "")))
	}
}

// Release releases a consumer lock. Only the current holder can release.
// The operation is idempotent — releasing an already-expired or already-released
// lock returns success.
func (s *ConsumerStore) Release(ctx context.Context, domain, stream, consumer, holder string) (retErr error) {
	ctx, span := tracer.Start(ctx, "consumerStore.Release", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
		attribute.String("consumer-lock.consumer", consumer),
		attribute.String("consumer-lock.holder", holder),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()
	}()

	consumerID, err := s.resolveConsumerName(ctx, consumer)
	if err != nil {
		return err
	}

	var acquiredAt time.Time
	err = s.pg.QueryRow(ctx, `
		SELECT cl.acquired_at
		FROM consumer_locks cl
		JOIN events_stream es ON cl.stream_id = es.id
		WHERE es.app = $1 AND es.stream = $2
		  AND cl.consumer_id = $3
		  AND cl.holder = $4`, domain, stream, consumerID, holder).Scan(&acquiredAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return &OperationError{Operation: "query lock for release", Underlying: err}
	}
	lockFound := err == nil

	tag, err := s.pg.Exec(ctx, `
		DELETE FROM consumer_locks cl
		USING events_stream es
		WHERE cl.stream_id = es.id
		  AND es.app = $1 AND es.stream = $2
		  AND cl.consumer_id = $3
		  AND cl.holder = $4`, domain, stream, consumerID, holder)
	if err != nil {
		return &OperationError{Operation: "release lock", Underlying: err}
	}

	if tag.RowsAffected() > 0 {
		ReleaseExplicit.Add(ctx, 1, metric.WithAttributeSet(lockAttrSet(domain, stream, consumer)))
		activeLocks.Add(ctx, -1, metric.WithAttributeSet(lockAttrSet(domain, stream, consumer)))
		if lockFound {
			duration := time.Since(acquiredAt).Seconds()
			holdDuration.Record(ctx, duration, metric.WithAttributeSet(lockAttrSet(domain, stream, consumer)))
		}
	}
	return nil
}

// GetLock returns the current state of a consumer lock.
// Expired locks (held_until < NOW()) are treated as non-existent.
func (s *ConsumerStore) GetLock(ctx context.Context, domain, stream, consumer string) (out *v1.LockState, retErr error) {
	ctx, span := tracer.Start(ctx, "consumerStore.GetLock", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
		attribute.String("consumer-lock.consumer", consumer),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		if out == nil {
			span.SetAttributes(attribute.Bool("consumer-lock.found", false))
		} else {
			span.SetAttributes(attribute.Bool("consumer-lock.found", true))
		}
		span.End()
	}()

	consumerID, err := s.resolveConsumerName(ctx, consumer)
	if err != nil {
		return nil, err
	}

	var state v1.LockState
	var heldUntil time.Time
	err = s.pg.QueryRow(ctx, `
		SELECT cn.name, es.app, es.stream, cl.holder,
		       cl.acquired_at, cl.heartbeat_at, cl.ttl,
		       cl.guarantee_until, cl.held_until
		FROM consumer_locks cl
		JOIN events_stream es ON cl.stream_id = es.id
		JOIN consumer_names cn ON cl.consumer_id = cn.id
		WHERE es.app = $1 AND es.stream = $2 AND cl.consumer_id = $3`, domain, stream, consumerID).Scan(
		&state.Consumer, &state.Domain, &state.Stream, &state.Holder,
		&state.AcquiredAt, &state.HeartbeatAt, &state.TTL,
		&state.GuaranteeUntil, &heldUntil)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, &OperationError{Operation: "get lock", Underlying: err}
	}
	if heldUntil.Before(time.Now()) {
		ExpiryIgnored.Add(ctx, 1, metric.WithAttributeSet(lockAttrSet(domain, stream, consumer)))
		return nil, nil
	}
	state.HeldUntil = heldUntil
	return &state, nil
}

// ListLocks returns all active locks (held_until > NOW()) for a domain/stream pair.
func (s *ConsumerStore) ListLocks(ctx context.Context, domain, stream string) (out []v1.LockState, retErr error) {
	ctx, span := tracer.Start(ctx, "consumerStore.ListLocks", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.SetAttributes(attribute.Int("consumer-lock.lock_count", len(out)))
		span.End()
	}()

	rows, err := s.pg.Query(ctx, `
		SELECT cn.name, es.app, es.stream, cl.holder,
		       cl.acquired_at, cl.heartbeat_at, cl.ttl,
		       cl.guarantee_until, cl.held_until
		FROM consumer_locks cl
		JOIN events_stream es ON cl.stream_id = es.id
		JOIN consumer_names cn ON cl.consumer_id = cn.id
		WHERE es.app = $1 AND es.stream = $2
		  AND cl.held_until > NOW()
		ORDER BY cl.acquired_at`, domain, stream)
	if err != nil {
		return nil, &OperationError{Operation: "list locks", Underlying: err}
	}
	defer rows.Close()

	var locks []v1.LockState
	for rows.Next() {
		var state v1.LockState
		if err := rows.Scan(
			&state.Consumer, &state.Domain, &state.Stream, &state.Holder,
			&state.AcquiredAt, &state.HeartbeatAt, &state.TTL,
			&state.GuaranteeUntil, &state.HeldUntil); err != nil {
			return nil, &OperationError{Operation: "list locks scan", Underlying: err}
		}
		locks = append(locks, state)
	}
	if locks == nil {
		locks = []v1.LockState{}
	}
	return locks, rows.Err()
}

// lockState tracks whether a lock exists and who holds it.
type lockState struct {
	exists      bool
	holder      string
	acquiredAt  time.Time
	heartbeatAt time.Time
	heldUntil   time.Time
}

// checkLockState queries the current lock holder and timestamps within a transaction.
func (s *ConsumerStore) checkLockState(ctx context.Context, tx pgx.Tx, domain, stream string, consumerID int64) (lockState, error) {
	var state lockState
	err := tx.QueryRow(ctx, `
		SELECT cl.holder, cl.acquired_at, cl.heartbeat_at, cl.held_until
		FROM consumer_locks cl
		JOIN events_stream es ON cl.stream_id = es.id
		WHERE es.app = $1 AND es.stream = $2 AND cl.consumer_id = $3`,
		domain, stream, consumerID).Scan(&state.holder, &state.acquiredAt, &state.heartbeatAt, &state.heldUntil)
	switch {
	case err == nil: //nolint:staticcheck
		state.exists = true
	case errors.Is(err, pgx.ErrNoRows):
		state.exists = false
	default:
		return lockState{}, &OperationError{Operation: "check lock state", Underlying: err}
	}
	return state, nil
}

// updateLockHeartbeat updates the lock heartbeat timestamps within a transaction.
// Returns true if the lock was updated, false if no matching lock was found.
func (s *ConsumerStore) updateLockHeartbeat(ctx context.Context, tx pgx.Tx, domain, stream string, consumerID int64, holder string) (bool, error) {
	result, err := tx.Exec(ctx, `
		UPDATE consumer_locks
		SET heartbeat_at = NOW(),
		    guarantee_until = NOW() + (ttl * $4),
		    held_until = NOW() + ttl
		WHERE stream_id IN (
			SELECT es.id FROM events_stream es WHERE es.app = $1 AND es.stream = $2
		) AND consumer_id = $3 AND holder = $5 AND held_until > NOW()`,
		domain, stream, consumerID,
		float64(v1.DefaultGuaranteeFraction), holder)
	if err != nil {
		return false, &OperationError{Operation: "heartbeat lock update", Underlying: err}
	}
	return result.RowsAffected() > 0, nil
}

// updatePosition atomically updates the consumer position with a backward-position guard.
// Returns true if the position was updated, false if the position was stale.
func (s *ConsumerStore) updatePosition(ctx context.Context, tx pgx.Tx, domain, stream, consumer string, consumerID, position int64) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO consumer_positions (stream_id, consumer, consumer_id, event_id, updated_at)
		SELECT es.id, $3, $4, $5, NOW()
		FROM events_stream es
		WHERE es.app = $1 AND es.stream = $2
		ON CONFLICT (stream_id, consumer) DO UPDATE
		SET event_id = EXCLUDED.event_id,
		    consumer_id = EXCLUDED.consumer_id,
		    updated_at = EXCLUDED.updated_at
		WHERE COALESCE(consumer_positions.event_id, 0) <= EXCLUDED.event_id`,
		domain, stream, consumer, consumerID, position)
	if err != nil {
		return false, &OperationError{Operation: "heartbeat position update", Underlying: err}
	}
	return tag.RowsAffected() > 0, nil
}

// getConflictVersion retrieves the current stored position for conflict details.
func (s *ConsumerStore) getConflictVersion(ctx context.Context, domain, stream, consumer string) int64 {
	var currentVersion int64
	err := s.pg.QueryRow(ctx, `
		SELECT cp.event_id
		FROM consumer_positions cp
		JOIN events_stream es ON cp.stream_id = es.id
		WHERE es.app = $1 AND es.stream = $2 AND cp.consumer = $3`,
		domain, stream, consumer).Scan(&currentVersion)
	if err != nil {
		return 0
	}
	return currentVersion
}

// resolveLockError determines the appropriate error when a lock heartbeat update fails.
// Called when updateLockHeartbeat returns false (no rows updated).
// Returns LockExpiredError if the lock exists but is expired, LockNotHeldError if held by another,
// or LockNotFoundError if the lock doesn't exist.
func resolveLockError(state *lockState, domain, stream, consumer, holder string) error {
	if !state.exists {
		return &LockNotFoundError{Domain: domain, Stream: stream, Consumer: consumer, Holder: holder}
	}
	if state.holder != holder {
		return &v1.LockNotHeldError{Consumer: consumer, Holder: holder, Domain: domain, Stream: stream}
	}
	if state.heldUntil.Before(time.Now()) {
		return &v1.LockExpiredError{Consumer: consumer, Domain: domain, Stream: stream}
	}
	return &LockNotFoundError{Domain: domain, Stream: stream, Consumer: consumer, Holder: holder}
}

// HeartbeatWithPosition atomically renews a lock and updates the consumer position
// in a single transaction. If the position is stale (behind the current stored position),
// the entire transaction is rolled back and a HeartbeatConflictError is returned.
func (s *ConsumerStore) HeartbeatWithPosition(ctx context.Context, domain, stream, consumer, holder string, position int64) (retErr error) {
	ctx, span := tracer.Start(ctx, "consumerStore.HeartbeatWithPosition", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
		attribute.String("consumer-lock.consumer", consumer),
		attribute.String("consumer-lock.holder", holder),
		attribute.Int64("consumer-lock.position", position),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()
	}()

	consumerID, err := s.resolveConsumerName(ctx, consumer)
	if err != nil {
		return err
	}

	tx, err := s.pg.Begin(ctx)
	if err != nil {
		return &OperationError{Operation: "begin heartbeat transaction", Underlying: err}
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, tx.Rollback(ctx))
		}
	}()

	return s.heartbeatWithinTx(ctx, tx, domain, stream, consumer, holder, consumerID, position)
}

func (s *ConsumerStore) heartbeatWithinTx(ctx context.Context, tx pgx.Tx, domain, stream, consumer, holder string, consumerID, position int64) error {
	state, err := s.checkLockState(ctx, tx, domain, stream, consumerID)
	if err != nil {
		return err
	}

	updated, err := s.updateLockHeartbeat(ctx, tx, domain, stream, consumerID, holder)
	if err != nil {
		return err
	}
	if !updated {
		return resolveLockError(&state, domain, stream, consumer, holder)
	}

	if state.exists {
		interval := time.Since(state.heartbeatAt).Seconds()
		heartbeatInterval.Record(ctx, interval, metric.WithAttributeSet(lockAttrSet(domain, stream, consumer)))
	}

	positionUpdated, err := s.updatePosition(ctx, tx, domain, stream, consumer, consumerID, position)
	if err != nil {
		return err
	}
	if !positionUpdated {
		HeartbeatConflict.Add(ctx, 1, metric.WithAttributeSet(lockAttrSet(domain, stream, consumer)))
		currentVersion := s.getConflictVersion(ctx, domain, stream, consumer)
		trace.SpanFromContext(ctx).AddEvent("heartbeat.conflict", trace.WithAttributes(
			attribute.Int64("consumer-lock.target_version", position),
			attribute.Int64("consumer-lock.current_version", currentVersion),
		))
		return &v1.HeartbeatConflictError{
			TargetVersion:  position,
			CurrentVersion: currentVersion,
		}
	}

	HeartbeatProcessed.Add(ctx, 1, metric.WithAttributeSet(lockAttrSet(domain, stream, consumer)))
	trace.SpanFromContext(ctx).AddEvent("heartbeat.renewed")
	return tx.Commit(ctx)
}

// SetPosition records a consumer's position in a stream.
// Populates both consumer TEXT and consumer_id FK columns (Phase 1 dual-write).
// Returns BackwardPositionError when the stream exists but the position would go backwards.
// Returns StreamNotFoundError when the stream doesn't exist.
func (s *ConsumerStore) SetPosition(ctx context.Context, domain, stream, consumer string, eventID int64) (*SetPositionResult, error) {
	consumerID, err := s.resolveConsumerName(ctx, consumer)
	if err != nil {
		return nil, err
	}

	var streamID int64
	err = s.pg.QueryRow(ctx,
		`SELECT id FROM events_stream WHERE app = $1 AND stream = $2`,
		domain, stream).Scan(&streamID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, &StreamNotFoundError{Domain: domain, Stream: stream}
		}
		return nil, err
	}

	var previousEventID *int64
	err = s.pg.QueryRow(ctx, `
		INSERT INTO consumer_positions (stream_id, consumer, consumer_id, event_id, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (stream_id, consumer) DO UPDATE
		SET event_id = EXCLUDED.event_id,
		    consumer_id = EXCLUDED.consumer_id,
		    updated_at = EXCLUDED.updated_at
		WHERE COALESCE(consumer_positions.event_id, 0) <= EXCLUDED.event_id
		RETURNING event_id`,
		streamID, consumer, consumerID, eventID).Scan(&previousEventID)
	if err != nil {
		if err == pgx.ErrNoRows {
			var current int64
			err = s.pg.QueryRow(ctx, `
				SELECT event_id FROM consumer_positions
				WHERE stream_id = $1 AND consumer = $2`,
				streamID, consumer).Scan(&current)
			if err != nil {
				current = 0
			}
			return nil, &BackwardPositionError{
				Consumer:  consumer,
				Current:   current,
				Requested: eventID,
			}
		}
		return nil, err
	}

	return &SetPositionResult{
		PreviousEventID: previousEventID,
		CurrentEventID:  eventID,
	}, nil
}

// GetPosition retrieves a consumer's current position in a stream.
func (s *ConsumerStore) GetPosition(ctx context.Context, domain, stream, consumer string) (eventID int64, found bool, err error) {
	err = s.pg.QueryRow(ctx, `
		SELECT cp.event_id
		FROM consumer_positions cp
		JOIN events_stream es ON cp.stream_id = es.id
		WHERE es.app = $1 AND es.stream = $2 AND cp.consumer = $3`,
		domain, stream, consumer).Scan(&eventID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, err
	}
	return eventID, true, nil
}

// ListConsumers returns all consumers that have a position in the specified stream.
func (s *ConsumerStore) ListConsumers(ctx context.Context, domain, stream string) ([]string, error) {
	rows, err := s.pg.Query(ctx, `
		SELECT cp.consumer
		FROM consumer_positions cp
		JOIN events_stream es ON cp.stream_id = es.id
		WHERE es.app = $1 AND es.stream = $2`, domain, stream)
	if err != nil {
		if err == pgx.ErrNoRows {
			return []string{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	var consumers []string
	for rows.Next() {
		var consumer string
		if err := rows.Scan(&consumer); err != nil {
			return nil, err
		}
		consumers = append(consumers, consumer)
	}
	if consumers == nil {
		consumers = []string{}
	}
	return consumers, rows.Err()
}

// DeletePosition removes a consumer's position from a stream.
func (s *ConsumerStore) DeletePosition(ctx context.Context, domain, stream, consumer string) error {
	_, err := s.pg.Exec(ctx, `
		DELETE FROM consumer_positions
		WHERE stream_id IN (
			SELECT es.id FROM events_stream es
			WHERE es.app = $1 AND es.stream = $2
		) AND consumer = $3`, domain, stream, consumer)
	return err
}

// LockNotFoundError is returned when a lock does not exist.
type LockNotFoundError struct {
	Domain   string
	Stream   string
	Consumer string
	Holder   string
}

func (e *LockNotFoundError) Error() string {
	return "lock not found: " + e.Domain + "/" + e.Stream + "/" + e.Consumer + "/" + e.Holder
}
