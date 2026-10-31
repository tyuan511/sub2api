-- Databases that applied the original 255_typesafe_systemone_platform.sql
-- restored the platform CHECK constraints that 242 removed. Drop them again so
-- every database converges on application-level platform validation
-- (domain.IsConcretePlatform). Idempotent.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
