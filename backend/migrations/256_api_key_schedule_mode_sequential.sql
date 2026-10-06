-- Smart routing has been removed. Persist every API key as sequential and
-- clear the unused smart policy columns so the control plane matches the
-- runtime. Historical usage_logs and routing_attempts rows are left unchanged:
-- they are audit records, not configuration, and nothing replays them.
--
-- The existing api_keys triggers invalidate the auth cache when schedule_mode
-- or smart_preference changes, and bump route_version when smart_balance_bps
-- changes. Both effects are intended: a key must not keep serving a cached
-- smart snapshot after this rewrite.
--
-- The smart_preference check requires NULL whenever schedule_mode is
-- sequential, so both columns are updated in the same statement.

UPDATE api_keys
SET schedule_mode = 'sequential',
    smart_preference = NULL,
    smart_balance_bps = NULL
WHERE schedule_mode IS DISTINCT FROM 'sequential'
   OR smart_preference IS NOT NULL
   OR smart_balance_bps IS NOT NULL;
