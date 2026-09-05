-- Endpoint-scoped health and actual supplier-usage buckets for smart routing.
-- Failure categories are a bounded JSON object sourced only from the routing
-- fact outcome enum; no request content or credentials enter this table.
CREATE TABLE IF NOT EXISTS api_key_routing_health_metrics_1m (
    bucket_start TIMESTAMPTZ NOT NULL,
    platform TEXT NOT NULL,
    group_id BIGINT NOT NULL,
    model TEXT NOT NULL,
    endpoint_kind TEXT NOT NULL DEFAULT 'other'
        CHECK (endpoint_kind IN (
            'messages', 'chat_completions', 'responses', 'embeddings', 'images',
            'video', 'audio', 'live', 'generate_content', 'count_tokens', 'other'
        )),
    success_requests BIGINT NOT NULL DEFAULT 0 CHECK (success_requests >= 0),
    failed_requests BIGINT NOT NULL DEFAULT 0 CHECK (failed_requests >= 0),
    capacity_overflow_requests BIGINT NOT NULL DEFAULT 0 CHECK (capacity_overflow_requests >= 0),
    failure_categories JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(failure_categories) = 'object'),
    input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_creation_tokens >= 0),
    cache_read_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    ttft_sum_ms BIGINT NOT NULL DEFAULT 0 CHECK (ttft_sum_ms >= 0),
    ttft_count BIGINT NOT NULL DEFAULT 0 CHECK (ttft_count >= 0),
    duration_sum_ms BIGINT NOT NULL DEFAULT 0 CHECK (duration_sum_ms >= 0),
    duration_count BIGINT NOT NULL DEFAULT 0 CHECK (duration_count >= 0),
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket_start, platform, group_id, model, endpoint_kind)
);

CREATE INDEX IF NOT EXISTS idx_api_key_routing_health_scope_time
    ON api_key_routing_health_metrics_1m
    (platform, group_id, model, endpoint_kind, bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_api_key_routing_health_time
    ON api_key_routing_health_metrics_1m (bucket_start DESC);

COMMENT ON TABLE api_key_routing_health_metrics_1m IS
    'One-minute endpoint health, capacity overflow, latency, and actual supplier token facts for API-key smart routing.';
