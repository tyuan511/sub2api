-- API key routing continuous-optimization foundation.
-- Long-lived configuration/audit facts live in PostgreSQL; Redis only points
-- at complete immutable versions and buffers bounded diagnostic events.

CREATE TABLE IF NOT EXISTS routing_artifact_versions (
    id BIGSERIAL PRIMARY KEY,
    artifact_kind VARCHAR(24) NOT NULL,
    version VARCHAR(96) NOT NULL,
    parent_version VARCHAR(96),
    platform VARCHAR(32) NOT NULL,
    model_family VARCHAR(96) NOT NULL,
    endpoint_kind VARCHAR(32) NOT NULL,
    preference VARCHAR(16),
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    schema_version VARCHAR(64) NOT NULL,
    checksum VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    dependencies JSONB NOT NULL DEFAULT '[]'::jsonb,
    lineage JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    activated_at TIMESTAMPTZ,
    retired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT routing_artifact_versions_kind_check
        CHECK (artifact_kind IN ('strategy', 'score', 'feature', 'model')),
    CONSTRAINT routing_artifact_versions_status_check
        CHECK (status IN ('draft', 'shadow', 'canary', 'active', 'paused', 'retired')),
    CONSTRAINT routing_artifact_versions_preference_check
        CHECK (preference IS NULL OR preference IN ('price', 'speed', 'balanced')),
    CONSTRAINT routing_artifact_versions_payload_object_check
        CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT routing_artifact_versions_dependencies_array_check
        CHECK (jsonb_typeof(dependencies) = 'array'),
    CONSTRAINT routing_artifact_versions_lineage_object_check
        CHECK (jsonb_typeof(lineage) = 'object'),
    CONSTRAINT routing_artifact_versions_kind_version_unique UNIQUE (artifact_kind, version)
);

CREATE INDEX IF NOT EXISTS idx_routing_artifact_versions_scope_status
    ON routing_artifact_versions (platform, model_family, endpoint_kind, artifact_kind, status);

CREATE INDEX IF NOT EXISTS idx_routing_artifact_versions_created
    ON routing_artifact_versions (created_at DESC, id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_routing_artifact_versions_one_active
    ON routing_artifact_versions (
        artifact_kind, platform, model_family, endpoint_kind, COALESCE(preference, '')
    ) WHERE status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS idx_routing_artifact_versions_one_canary
    ON routing_artifact_versions (
        artifact_kind, platform, model_family, endpoint_kind, COALESCE(preference, '')
    ) WHERE status = 'canary';

CREATE TABLE IF NOT EXISTS routing_experiments (
    id BIGSERIAL PRIMARY KEY,
    experiment_key VARCHAR(96) NOT NULL UNIQUE,
    platform VARCHAR(32) NOT NULL,
    model_family VARCHAR(96) NOT NULL,
    endpoint_kind VARCHAR(32) NOT NULL,
    preference VARCHAR(16) NOT NULL,
    baseline_strategy_version VARCHAR(96) NOT NULL,
    candidate_strategy_version VARCHAR(96) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    allocation_bps INTEGER NOT NULL DEFAULT 0,
    bucket_salt_checksum VARCHAR(128) NOT NULL,
    guardrails JSONB NOT NULL DEFAULT '{}'::jsonb,
    offline_replay JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_evaluation JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_evaluated_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    stopped_at TIMESTAMPTZ,
    stop_reason VARCHAR(512),
    approved_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT routing_experiments_preference_check
        CHECK (preference IN ('price', 'speed', 'balanced')),
    CONSTRAINT routing_experiments_status_check
        CHECK (status IN ('draft', 'shadow', 'canary', 'active', 'paused', 'retired')),
    CONSTRAINT routing_experiments_allocation_check
        CHECK (allocation_bps BETWEEN 0 AND 10000),
    CONSTRAINT routing_experiments_guardrails_object_check
        CHECK (jsonb_typeof(guardrails) = 'object'),
    CONSTRAINT routing_experiments_offline_replay_object_check
        CHECK (jsonb_typeof(offline_replay) = 'object'),
    CONSTRAINT routing_experiments_last_evaluation_object_check
        CHECK (jsonb_typeof(last_evaluation) = 'object'),
    CONSTRAINT routing_experiments_versions_differ_check
        CHECK (baseline_strategy_version <> candidate_strategy_version)
);

CREATE INDEX IF NOT EXISTS idx_routing_experiments_scope_status
    ON routing_experiments (platform, model_family, endpoint_kind, preference, status);

CREATE TABLE IF NOT EXISTS routing_attempts (
    id BIGSERIAL PRIMARY KEY,
    event_id VARCHAR(64) NOT NULL UNIQUE,
    routing_decision_id VARCHAR(64) NOT NULL,
    request_id VARCHAR(64),
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    route_version BIGINT NOT NULL,
    initial_group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    attempted_group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    effective_group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    selected_group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    schedule_mode VARCHAR(16) NOT NULL,
    smart_preference VARCHAR(16),
    attempt_index INTEGER NOT NULL DEFAULT 0,
    platform VARCHAR(32) NOT NULL,
    model_family VARCHAR(96) NOT NULL,
    endpoint_kind VARCHAR(32) NOT NULL,
    strategy_version VARCHAR(96) NOT NULL,
    score_version VARCHAR(96) NOT NULL,
    feature_schema_version VARCHAR(96) NOT NULL,
    model_version VARCHAR(96),
    experiment_id VARCHAR(96),
    experiment_bucket INTEGER,
    sample_probability DOUBLE PRECISION NOT NULL DEFAULT 1,
    action_propensity DOUBLE PRECISION,
    assignment_reason VARCHAR(32) NOT NULL DEFAULT 'deterministic',
    candidates JSONB NOT NULL DEFAULT '[]'::jsonb,
    selected_reason VARCHAR(64),
    outcome_visibility VARCHAR(16) NOT NULL DEFAULT 'observed',
    outcome_category VARCHAR(64),
    retryable BOOLEAN NOT NULL DEFAULT FALSE,
    semantic_output BOOLEAN NOT NULL DEFAULT FALSE,
    switched_group BOOLEAN NOT NULL DEFAULT FALSE,
    breaker_transition VARCHAR(32),
    queue_ms INTEGER,
    ttft_ms INTEGER,
    duration_ms INTEGER,
    actual_usage JSONB,
    billable_usage JSONB,
    actual_cost DECIMAL(20,10),
    billed_cost DECIMAL(20,10),
    cache_cold_due_to_failover BOOLEAN NOT NULL DEFAULT FALSE,
    event_priority VARCHAR(16) NOT NULL DEFAULT 'diagnostic',
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT routing_attempts_route_version_check CHECK (route_version >= 1),
    CONSTRAINT routing_attempts_schedule_mode_check CHECK (schedule_mode IN ('sequential', 'smart')),
    CONSTRAINT routing_attempts_preference_check CHECK (
        (schedule_mode = 'sequential' AND smart_preference IS NULL)
        OR (schedule_mode = 'smart' AND smart_preference IN ('price', 'speed', 'balanced'))
    ),
    CONSTRAINT routing_attempts_attempt_index_check CHECK (attempt_index >= 0 AND attempt_index < 8),
    CONSTRAINT routing_attempts_sample_probability_check CHECK (sample_probability > 0 AND sample_probability <= 1),
    CONSTRAINT routing_attempts_action_propensity_check CHECK (action_propensity IS NULL OR (action_propensity > 0 AND action_propensity <= 1)),
    CONSTRAINT routing_attempts_deterministic_propensity_check CHECK (assignment_reason <> 'deterministic' OR action_propensity IS NULL),
    CONSTRAINT routing_attempts_candidates_array_check CHECK (jsonb_typeof(candidates) = 'array' AND jsonb_array_length(candidates) <= 8),
    CONSTRAINT routing_attempts_actual_usage_object_check CHECK (actual_usage IS NULL OR jsonb_typeof(actual_usage) = 'object'),
    CONSTRAINT routing_attempts_billable_usage_object_check CHECK (billable_usage IS NULL OR jsonb_typeof(billable_usage) = 'object'),
    CONSTRAINT routing_attempts_visibility_check CHECK (outcome_visibility IN ('observed', 'unobserved')),
    CONSTRAINT routing_attempts_priority_check CHECK (event_priority IN ('sample', 'diagnostic', 'critical'))
);

CREATE INDEX IF NOT EXISTS idx_routing_attempts_request
    ON routing_attempts (request_id) WHERE request_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_routing_attempts_decision
    ON routing_attempts (routing_decision_id, attempt_index);

CREATE INDEX IF NOT EXISTS idx_routing_attempts_api_key_created
    ON routing_attempts (api_key_id, created_at DESC) WHERE api_key_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_routing_attempts_effective_group_created
    ON routing_attempts (effective_group_id, created_at DESC) WHERE effective_group_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_routing_attempts_experiment_created
    ON routing_attempts (experiment_id, created_at DESC) WHERE experiment_id IS NOT NULL;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS initial_group_id BIGINT,
    ADD COLUMN IF NOT EXISTS route_version BIGINT,
    ADD COLUMN IF NOT EXISTS schedule_mode VARCHAR(16),
    ADD COLUMN IF NOT EXISTS smart_preference VARCHAR(16),
    ADD COLUMN IF NOT EXISTS group_switch_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS routing_decision_id VARCHAR(64),
    ADD COLUMN IF NOT EXISTS cache_cold_due_to_failover BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS actual_usage JSONB,
    ADD COLUMN IF NOT EXISTS billable_usage JSONB,
    ADD COLUMN IF NOT EXISTS cache_compensation_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_compensation_reason VARCHAR(64);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'usage_logs_initial_group_fk') THEN
        ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_initial_group_fk
            FOREIGN KEY (initial_group_id) REFERENCES groups(id) ON DELETE SET NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'usage_logs_route_version_check') THEN
        ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_route_version_check
            CHECK (route_version IS NULL OR route_version >= 1);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'usage_logs_schedule_mode_check') THEN
        ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_schedule_mode_check
            CHECK (schedule_mode IS NULL OR schedule_mode IN ('sequential', 'smart'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'usage_logs_smart_preference_check') THEN
        ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_smart_preference_check
            CHECK (smart_preference IS NULL OR smart_preference IN ('price', 'speed', 'balanced'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'usage_logs_group_switch_count_check') THEN
        ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_group_switch_count_check
            CHECK (group_switch_count >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'usage_logs_cache_compensation_tokens_check') THEN
        ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_cache_compensation_tokens_check
            CHECK (cache_compensation_tokens >= 0);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_usage_logs_routing_decision
    ON usage_logs (routing_decision_id) WHERE routing_decision_id IS NOT NULL;
