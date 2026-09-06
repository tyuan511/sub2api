-- Keep the score-builder activation set cheap when the route table grows.
-- This index is partial because disabled candidates must not wake the builder.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_api_key_group_routes_enabled_api_key
    ON api_key_group_routes (api_key_id)
    WHERE enabled;
