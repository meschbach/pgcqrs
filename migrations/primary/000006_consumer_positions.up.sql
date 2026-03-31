CREATE TABLE IF NOT EXISTS consumer_positions (
    stream_id BIGINT NOT NULL REFERENCES events_stream(id) ON DELETE CASCADE,
    consumer TEXT NOT NULL,
    event_id BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (stream_id, consumer)
);

CREATE INDEX IF NOT EXISTS idx_consumer_prefix ON consumer_positions (stream_id, substr(consumer, 1, 16));
