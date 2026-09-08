-- Read-only production preflight for API-key routing migrations 240-251.
-- Run with psql against hk1/us02 before starting the application migration.
-- This file is intentionally outside backend/migrations: it must not be
-- recorded as an application migration or change schema state.

\echo '== required migration state =='
WITH required(filename) AS (
    VALUES
        ('240_api_key_multi_group_routing.sql'),
        ('241_api_key_routing_optimization_foundation.sql'),
        ('242_api_key_routing_price_metrics.sql'),
        ('243_api_key_routing_stability_facts.sql'),
        ('244_api_key_routing_fact_retention_index_notx.sql'),
        ('245_api_key_routing_health_metrics.sql'),
        ('246_api_key_route_dependency_invalidation.sql'),
        ('247_batch_image_actual_group.sql'),
        ('248_api_key_routing_controls.sql'),
        ('249_api_key_default_success_threshold.sql'),
        ('250_api_key_routing_usage_decision_index_notx.sql'),
        ('251_api_key_routing_activation_index_notx.sql')
)
SELECT required.filename,
       CASE WHEN applied.filename IS NULL THEN 'missing' ELSE 'applied' END AS state,
       applied.applied_at,
       applied.checksum
FROM required
LEFT JOIN schema_migrations applied USING (filename)
ORDER BY required.filename;

\echo '== usage log size and planner estimate =='
SELECT pg_size_pretty(pg_total_relation_size('usage_logs'::regclass)) AS total_size,
       pg_size_pretty(pg_relation_size('usage_logs'::regclass)) AS heap_size,
       c.reltuples::bigint AS estimated_rows,
       c.relpages AS heap_pages
FROM pg_class c
WHERE c.oid = 'usage_logs'::regclass;

\echo '== enabled multi-group keys =='
SELECT to_regclass('public.api_key_group_routes') IS NOT NULL AS present \gset routing_routes_
\if :routing_routes_present
SELECT api_key_id, COUNT(*) AS enabled_routes,
       ARRAY_AGG(group_id ORDER BY priority) AS group_ids
FROM api_key_group_routes
WHERE enabled
GROUP BY api_key_id
HAVING COUNT(*) > 1
ORDER BY api_key_id;
\else
\echo 'api_key_group_routes is not created yet (migration 240 is pending)'
\endif

\echo '== api key routing state rows requiring backfill =='
SELECT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'api_keys'
      AND column_name = 'routing_state_version'
) AS present \gset routing_state_
\if :routing_state_present
SELECT COUNT(*) AS rows_requiring_state_backfill
FROM api_keys
WHERE routing_state_version IS DISTINCT FROM route_version;
\else
\echo 'api_keys.routing_state_version is not created yet (migration 248 is pending)'
\endif

\echo '== routing constraint validation state =='
SELECT conrelid::regclass AS relation_name, conname, convalidated
FROM pg_constraint
WHERE conname IN (
    'api_keys_schedule_mode_check',
    'api_keys_smart_preference_check',
    'api_keys_route_version_check',
    'usage_logs_initial_group_fk',
    'usage_logs_route_version_check',
    'usage_logs_schedule_mode_check',
    'usage_logs_smart_preference_check',
    'usage_logs_group_switch_count_check',
    'usage_logs_cache_compensation_tokens_check',
    'api_keys_smart_balance_bps_check',
    'api_keys_routing_min_success_rate_check',
    'api_keys_routing_state_version_check'
)
ORDER BY relation_name, conname;

\echo '== routing indexes =='
SELECT indexname, indexdef
FROM pg_indexes
WHERE tablename IN ('api_key_group_routes', 'usage_logs', 'api_key_route_config_outbox')
       AND (indexname LIKE '%api_key_group_routes%'
       OR indexname LIKE '%usage_logs%routing%'
       OR indexname LIKE '%route_config_outbox%')
ORDER BY tablename, indexname;

\echo '== waiting locks on routing tables =='
SELECT blocked.pid AS blocked_pid,
       blocked.mode AS blocked_mode,
       blocked.granted AS blocked_granted,
       blocked.query_start,
       LEFT(blocked.query, 240) AS blocked_query,
       relation.relname AS relation_name
FROM pg_locks blocked
JOIN pg_class relation ON relation.oid = blocked.relation
WHERE relation.relname IN ('usage_logs', 'api_keys', 'api_key_group_routes', 'api_key_route_config_outbox')
  AND NOT blocked.granted
ORDER BY blocked.query_start;

\echo '== index builds currently in progress =='
SELECT pid, datname, relid::regclass AS relation_name, index_relid::regclass AS index_name,
       phase, lockers_total, lockers_done, blocks_total, blocks_done
FROM pg_stat_progress_create_index;
