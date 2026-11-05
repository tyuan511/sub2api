CREATE TABLE channel_group_probe_records (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL REFERENCES groups(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    kind TEXT NOT NULL CHECK (kind IN ('connectivity', 'pelican')),
    model TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'succeeded', 'failed')),
    duration_ms BIGINT,
    svg TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_channel_group_probe_records_group_created
    ON channel_group_probe_records (group_id, created_at DESC, id DESC);
