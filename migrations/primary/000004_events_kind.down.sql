DROP INDEX events_kind_id_lookup;
ALTER TABLE events DROP COLUMN kind_id;
DROP INDEX events_kind_id_kind;
DROP TABLE events_kind;
