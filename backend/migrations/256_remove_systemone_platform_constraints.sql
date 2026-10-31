-- Platform CHECK constraints are application-managed after migration 242.
-- Remove the constraints restored by the legacy TypeSafe migration without
-- modifying the already-applied 255_typesafe_systemone_platform.sql file.
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
