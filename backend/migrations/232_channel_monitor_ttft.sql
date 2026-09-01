-- 流式 Responses 监控指标：首字延迟（TTFT，毫秒）与生成速度（Token/s）。
-- 旧历史记录保持 NULL，避免伪造无法从非流式记录推导的指标。

ALTER TABLE channel_monitor_histories
    ADD COLUMN IF NOT EXISTS first_token_ms INT;

COMMENT ON COLUMN channel_monitor_histories.first_token_ms IS
    '流式 Responses 首个可见输出事件相对请求开始的耗时（毫秒）；非流式检测或旧记录为 NULL。';

ALTER TABLE channel_monitor_histories
    ADD COLUMN IF NOT EXISTS tokens_per_second DOUBLE PRECISION;

COMMENT ON COLUMN channel_monitor_histories.tokens_per_second IS
    '流式 Responses 首个可见输出之后的生成速度（Token/s）；上游未返回 output token usage 或旧记录为 NULL。';
