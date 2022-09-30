CREATE TABLE events_kind (id serial primary key, kind text not null unique);
CREATE INDEX events_kind_id_kind ON events_kind(kind, id);
ALTER TABLE events ADD COLUMN kind_id int references events_kind(id);
CREATE INDEX events_kind_id_lookup ON events(kind_id);

INSERT INTO events_kind (kind) select kind from events group by kind;
UPDATE events SET kind_id = (SELECT k.id FROM events_kind as k WHERE k.kind = events.kind);
