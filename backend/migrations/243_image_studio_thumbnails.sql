-- Thumbnail keys are derived from each retained original object location.
-- Existing files are backfilled on demand without rewriting their originals.
ALTER TABLE image_studio_files ADD COLUMN IF NOT EXISTS thumbnail_ready BOOLEAN NOT NULL DEFAULT FALSE;
