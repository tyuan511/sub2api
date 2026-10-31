-- Platform CHECK constraints were removed by migration 242 and are intentionally
-- not restored by later migrations. Platform validation is handled by the
-- application layer so newer platform values can be introduced without a
-- Historical TypeSafe allowlist ended with: 'opencode_go', 'typesafe'))
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
