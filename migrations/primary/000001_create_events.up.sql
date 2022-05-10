CREATE TABLE IF NOT EXISTS events_stream (
    id BIGSERIAL PRIMARY KEY NOT NULL,
    app text not null,
    stream text not null,
    CONSTRAINT app_stream_unique UNIQUE (app,stream)
    );

CREATE TABLE IF NOT EXISTS events (
    id BIGSERIAL PRIMARY KEY NOT NULL,
    stream_id BIGINT REFERENCES events_stream(id),
    when_occurred TIMESTAMP WITH TIME ZONE NOT NULL default now(),
    kind TEXT NOT NULL,
    event jsonb NOT NULL);
