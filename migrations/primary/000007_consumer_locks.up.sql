-- Consumer name enumeration table (deduplicates consumer names)
CREATE TABLE IF NOT EXISTS consumer_names (
    id   BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

-- Consumer locks table (exclusive access per consumer per domain/stream)
CREATE TABLE IF NOT EXISTS consumer_locks (
    stream_id      BIGINT NOT NULL REFERENCES events_stream(id) ON DELETE CASCADE,
    consumer_id    BIGINT NOT NULL REFERENCES consumer_names(id),
    holder         TEXT NOT NULL,
    acquired_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    heartbeat_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    ttl            INTERVAL NOT NULL,
    guarantee_until TIMESTAMP WITH TIME ZONE NOT NULL,
    held_until     TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (stream_id, consumer_id)
);

-- Add nullable consumer_id FK column to consumer_positions (Phase 1: dual-write)
ALTER TABLE consumer_positions
    ADD COLUMN IF NOT EXISTS consumer_id BIGINT REFERENCES consumer_names(id);

-- Backfill consumer_id from existing consumer TEXT column (idempotent)
UPDATE consumer_positions cp
SET consumer_id = cn.id
FROM consumer_names cn
WHERE cp.consumer = cn.name
  AND cp.consumer_id IS NULL;
