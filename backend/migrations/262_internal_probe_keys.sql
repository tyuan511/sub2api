-- A database-only billing identity; its reserved prefix is rejected by external
-- authentication even before a key lookup or authentication-cache access.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS is_internal_probe BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_internal_probe_marker_check
    CHECK (is_internal_probe = (key LIKE 'sub2api-internal-probe-%')) NOT VALID;

CREATE UNIQUE INDEX IF NOT EXISTS api_keys_internal_probe_user_group
    ON api_keys (user_id, group_id)
    WHERE deleted_at IS NULL AND group_id IS NOT NULL AND is_internal_probe;
