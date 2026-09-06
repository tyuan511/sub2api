-- Store BazaarLink's compact, redacted verdict separately from ordinary V1
-- availability history. Identity checks must not change uptime/availability
-- calculations or the regular monitor timeline.
CREATE TABLE IF NOT EXISTS channel_monitor_bazaarlink_probes (
    id             BIGSERIAL PRIMARY KEY,
    monitor_id     BIGINT NOT NULL REFERENCES channel_monitors(id) ON DELETE CASCADE,
    result         JSONB NOT NULL,
    checked_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    remote_run_id  TEXT,
    task_status    VARCHAR(20) NOT NULL DEFAULT 'completed',
    submitted_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ,
    next_poll_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_bazaarlink_probes_latest
    ON channel_monitor_bazaarlink_probes (monitor_id, checked_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_bazaarlink_tasks_due
    ON channel_monitor_bazaarlink_probes (task_status, next_poll_at, expires_at, id)
    WHERE task_status IN ('queued', 'running');

CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_monitor_bazaarlink_remote_run
    ON channel_monitor_bazaarlink_probes (remote_run_id)
    WHERE remote_run_id IS NOT NULL AND remote_run_id <> '';
