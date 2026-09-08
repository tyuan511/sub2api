ALTER TABLE batch_image_jobs
    ADD COLUMN IF NOT EXISTS group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS batch_image_jobs_group_created_at_idx
    ON batch_image_jobs (group_id, created_at DESC)
    WHERE group_id IS NOT NULL;

COMMENT ON COLUMN batch_image_jobs.group_id IS
    'Physical group selected before batch submission; settlement usage must retain this actual routing attribution.';
