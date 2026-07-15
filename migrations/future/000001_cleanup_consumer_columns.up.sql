-- Phase 2: Drop legacy consumer TEXT column from consumer_positions.
-- Run this AFTER all rolling upgrades are complete (Phase 1 dual-write period ended).
-- Requires consumer_id to be fully populated and NOT NULL.

-- Add NOT NULL constraint (requires all rows to have consumer_id populated)
ALTER TABLE consumer_positions
    ALTER COLUMN consumer_id SET NOT NULL;

-- Drop the legacy TEXT column
ALTER TABLE consumer_positions
    DROP COLUMN consumer;
