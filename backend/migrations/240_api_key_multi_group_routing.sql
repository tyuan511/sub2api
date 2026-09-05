-- API Key multi-group routing control-plane foundation.
-- The legacy api_keys.group_id column remains the compatibility mirror of priority 0.

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS schedule_mode VARCHAR(16) NOT NULL DEFAULT 'sequential',
    ADD COLUMN IF NOT EXISTS smart_preference VARCHAR(16),
    ADD COLUMN IF NOT EXISTS route_version BIGINT NOT NULL DEFAULT 1;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'api_keys_schedule_mode_check'
    ) THEN
        ALTER TABLE api_keys
            ADD CONSTRAINT api_keys_schedule_mode_check
            CHECK (schedule_mode IN ('sequential', 'smart'));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'api_keys_smart_preference_check'
    ) THEN
        ALTER TABLE api_keys
            ADD CONSTRAINT api_keys_smart_preference_check
            CHECK (
                (schedule_mode = 'sequential' AND smart_preference IS NULL)
                OR
                (schedule_mode = 'smart' AND smart_preference IN ('price', 'speed', 'balanced'))
            );
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'api_keys_route_version_check'
    ) THEN
        ALTER TABLE api_keys
            ADD CONSTRAINT api_keys_route_version_check CHECK (route_version >= 1);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS api_key_group_routes (
    id BIGSERIAL PRIMARY KEY,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    priority INTEGER NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT api_key_group_routes_priority_check CHECK (priority >= 0),
    CONSTRAINT api_key_group_routes_api_key_group_unique UNIQUE (api_key_id, group_id),
    CONSTRAINT api_key_group_routes_api_key_priority_unique UNIQUE (api_key_id, priority)
);

CREATE INDEX IF NOT EXISTS idx_api_key_group_routes_group_api_key
    ON api_key_group_routes (group_id, api_key_id);

CREATE INDEX IF NOT EXISTS idx_api_key_group_routes_api_key_enabled_priority
    ON api_key_group_routes (api_key_id, enabled, priority);

-- Backfill only legacy keys with an explicit group. Historical NULL group_id means
-- an unrestricted legacy key and must not be converted to an empty configured route set.
INSERT INTO api_key_group_routes (api_key_id, group_id, priority, enabled, created_at, updated_at)
SELECT id, group_id, 0, TRUE, created_at, updated_at
FROM api_keys
WHERE group_id IS NOT NULL
ON CONFLICT (api_key_id, group_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS api_key_route_config_outbox (
    id BIGSERIAL PRIMARY KEY,
    event_key VARCHAR(160) NOT NULL,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    route_version BIGINT NOT NULL,
    event_type VARCHAR(64) NOT NULL DEFAULT 'api_key_route_config_changed',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ,
    claimed_by VARCHAR(128),
    delivered_at TIMESTAMPTZ,
    last_error VARCHAR(512),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT api_key_route_config_outbox_event_key_unique UNIQUE (event_key),
    CONSTRAINT api_key_route_config_outbox_route_version_check CHECK (route_version >= 1),
    CONSTRAINT api_key_route_config_outbox_attempts_check CHECK (attempts >= 0)
);

CREATE INDEX IF NOT EXISTS idx_api_key_route_config_outbox_pending
    ON api_key_route_config_outbox (available_at, id)
    WHERE delivered_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_api_key_route_config_outbox_api_key_created
    ON api_key_route_config_outbox (api_key_id, created_at DESC);

