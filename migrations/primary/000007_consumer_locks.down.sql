DROP TABLE IF EXISTS consumer_locks;
DROP TABLE IF EXISTS consumer_names;

-- Remove the consumer_id FK column added in the up migration
ALTER TABLE consumer_positions DROP COLUMN IF EXISTS consumer_id;
