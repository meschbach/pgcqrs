-- Reverse Phase 2: restore consumer TEXT column and drop NOT NULL from consumer_id.

-- Add back the legacy TEXT column
ALTER TABLE consumer_positions
    ADD COLUMN consumer TEXT;

-- Backfill from consumer_id FK (via consumer_names lookup)
UPDATE consumer_positions cp
SET consumer = cn.name
FROM consumer_names cn
WHERE cp.consumer_id = cn.id
  AND cp.consumer IS NULL;

-- Drop NOT NULL constraint from consumer_id
ALTER TABLE consumer_positions
    ALTER COLUMN consumer_id DROP NOT NULL;
