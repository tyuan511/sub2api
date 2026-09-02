-- Persist the configured order of each account's proxy pool. Round-robin
-- selection must not depend on created_at because replacing a pool would
-- otherwise silently change its rotation order.
ALTER TABLE account_proxies
    ADD COLUMN IF NOT EXISTS position INTEGER;

WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY account_id ORDER BY created_at, proxy_id) - 1 AS position
    FROM account_proxies
)
UPDATE account_proxies ap
SET position = ranked.position
FROM ranked
WHERE ap.id = ranked.id
  AND (ap.position IS NULL OR ap.position <> ranked.position);

ALTER TABLE account_proxies
    ALTER COLUMN position SET DEFAULT 0,
    ALTER COLUMN position SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_account_proxies_account_position
    ON account_proxies (account_id, position, proxy_id);
