ALTER TABLE events ADD COLUMN kind text NOT NULL;
CREATE INDEX events_stream_kind ON events(stream_id, kind);

UPDATE events e SET kind = (SELECT k.kind FROM events_kind k WHERE e.kind_id = k.id)
