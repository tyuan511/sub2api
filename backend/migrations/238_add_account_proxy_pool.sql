-- Account-scoped proxy pools. The same proxy may be reused by many accounts,
-- while each account/proxy binding has its own concurrency cap.
CREATE TABLE IF NOT EXISTS account_proxies (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    proxy_id BIGINT NOT NULL REFERENCES proxies(id) ON DELETE RESTRICT,
    concurrency INTEGER NOT NULL DEFAULT 3 CHECK (concurrency >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_proxies_account_proxy_key UNIQUE (account_id, proxy_id)
);

CREATE INDEX IF NOT EXISTS idx_account_proxies_proxy_id
    ON account_proxies (proxy_id);

CREATE INDEX IF NOT EXISTS idx_account_proxies_account_id
    ON account_proxies (account_id);

-- Preserve the legacy one-proxy configuration for existing accounts.
INSERT INTO account_proxies (account_id, proxy_id, concurrency)
SELECT id, proxy_id, GREATEST(concurrency, 0)
FROM accounts
WHERE proxy_id IS NOT NULL
ON CONFLICT (account_id, proxy_id) DO NOTHING;
