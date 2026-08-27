-- 渠道监控分组缓存命中率快照。
-- 快照由监控周期在 RunCheck 完成后更新，用户页面只读取快照，避免每次刷新扫描 usage_logs。

CREATE TABLE IF NOT EXISTS channel_monitor_cache_hit_rate_snapshots (
    group_name           VARCHAR(200) NOT NULL,
    window_days          INT          NOT NULL,
    requests             BIGINT       NOT NULL DEFAULT 0,
    input_tokens         BIGINT       NOT NULL DEFAULT 0,
    cache_read_tokens    BIGINT       NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT      NOT NULL DEFAULT 0,
    cache_hit_rate_pct   DOUBLE PRECISION NOT NULL DEFAULT 0,
    computed_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_name, window_days),
    CONSTRAINT channel_monitor_cache_hit_rate_snapshots_window_check
        CHECK (window_days IN (7, 15, 30))
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_cache_hit_rate_snapshots_computed_at
    ON channel_monitor_cache_hit_rate_snapshots (computed_at);

COMMENT ON TABLE channel_monitor_cache_hit_rate_snapshots IS
    '按分组与时间窗口持久化的渠道监控缓存命中率快照，由监控周期刷新。';
