-- Price-composition rollup for multi-group smart routing. The table stores
-- supplier-reported token quantities, never billable/compensated quantities or
-- historical prices, so RoutingScoreBuilder can replay the recent workload
-- against the current group/channel/model price card.

CREATE TABLE IF NOT EXISTS api_key_routing_price_metrics_1m (
    bucket_start TIMESTAMPTZ NOT NULL,
    platform TEXT NOT NULL,
    group_id BIGINT NOT NULL,
    model TEXT NOT NULL,
    endpoint_kind TEXT NOT NULL DEFAULT 'other'
        CHECK (endpoint_kind IN (
            'messages', 'chat_completions', 'responses', 'embeddings', 'images',
            'video', 'audio', 'live', 'generate_content', 'count_tokens', 'other'
        )),
    service_tier TEXT NOT NULL DEFAULT 'default'
        CHECK (service_tier IN ('default', 'priority', 'flex', 'fast', 'other')),
    context_bucket SMALLINT NOT NULL DEFAULT 0
        CHECK (context_bucket BETWEEN 0 AND 7),
    success_requests BIGINT NOT NULL DEFAULT 0 CHECK (success_requests >= 0),
    input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    image_input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (image_input_tokens >= 0),
    output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_creation_tokens >= 0),
    cache_creation_5m_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_creation_5m_tokens >= 0),
    cache_creation_1h_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_creation_1h_tokens >= 0),
    cache_read_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    image_output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (image_output_tokens >= 0),
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (
        bucket_start, platform, group_id, model,
        endpoint_kind, service_tier, context_bucket
    )
);

CREATE INDEX IF NOT EXISTS idx_api_key_routing_price_metrics_scope_time
    ON api_key_routing_price_metrics_1m
    (platform, group_id, model, bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_api_key_routing_price_metrics_time
    ON api_key_routing_price_metrics_1m (bucket_start DESC);

COMMENT ON TABLE api_key_routing_price_metrics_1m IS
    'One-minute actual supplier token composition for current-price routing replay; failover cold-cache compensation rows are excluded.';
COMMENT ON COLUMN api_key_routing_price_metrics_1m.context_bucket IS
    'Bounded per-request logical input bucket used to avoid applying long-context or interval prices to a cross-request token sum.';
