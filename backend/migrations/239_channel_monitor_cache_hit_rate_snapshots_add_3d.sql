-- 渠道状态新增 3 天缓存命中率窗口。
-- 231 号迁移的约束只允许 7/15/30 天，先扩展约束再由监控周期写入 3 天快照。

ALTER TABLE channel_monitor_cache_hit_rate_snapshots
    DROP CONSTRAINT IF EXISTS channel_monitor_cache_hit_rate_snapshots_window_check;

ALTER TABLE channel_monitor_cache_hit_rate_snapshots
    ADD CONSTRAINT channel_monitor_cache_hit_rate_snapshots_window_check
    CHECK (window_days IN (3, 7, 15, 30));
