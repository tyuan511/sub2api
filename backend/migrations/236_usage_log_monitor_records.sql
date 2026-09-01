-- Store active channel-monitor probes in the usage log ledger without
-- attributing them to a real downstream API key or upstream account.
-- Monitor rows are informational only and must never participate in billing.

ALTER TABLE usage_logs
    ALTER COLUMN api_key_id DROP NOT NULL,
    ALTER COLUMN account_id DROP NOT NULL;

-- Keep monitor audit rows when a key/account is removed. Existing installs
-- created these foreign keys with ON DELETE CASCADE in the initial schema.
ALTER TABLE usage_logs
    -- Inline REFERENCES in the initial schema use PostgreSQL's generated
    -- names (`usage_logs_api_key_id_fkey` / `usage_logs_account_id_fkey`).
    -- Keep the custom names too for installations that renamed them.
    DROP CONSTRAINT IF EXISTS usage_logs_api_key_id_fkey,
    DROP CONSTRAINT IF EXISTS usage_logs_account_id_fkey,
    DROP CONSTRAINT IF EXISTS usage_logs_api_keys_usage_logs,
    DROP CONSTRAINT IF EXISTS usage_logs_accounts_usage_logs;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_api_keys_usage_logs
        FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE SET NULL,
    ADD CONSTRAINT usage_logs_accounts_usage_logs
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS is_monitor BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS channel_monitor_id BIGINT REFERENCES channel_monitors(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_usage_logs_monitor_created_at
    ON usage_logs (is_monitor, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_usage_logs_channel_monitor_created_at
    ON usage_logs (channel_monitor_id, created_at DESC)
    WHERE channel_monitor_id IS NOT NULL;

COMMENT ON COLUMN usage_logs.is_monitor IS
    'Whether this row was produced by an active channel monitor probe; monitor rows are not billable.';
COMMENT ON COLUMN usage_logs.channel_monitor_id IS
    'The channel monitor that produced this usage log, when is_monitor is true.';
