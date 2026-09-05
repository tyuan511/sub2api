-- Reverse dependency invalidation for multi-group API-key auth/routing snapshots.
-- All events contain only SHA-256 digests; API-key plaintext never enters an outbox payload.

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS routing_dependency_version BIGINT NOT NULL DEFAULT 1;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'api_keys_routing_dependency_version_check') THEN
        ALTER TABLE api_keys ADD CONSTRAINT api_keys_routing_dependency_version_check
            CHECK (routing_dependency_version >= 1);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_api_key_group_routes_group_api_key
    ON api_key_group_routes (group_id, api_key_id);

CREATE INDEX IF NOT EXISTS idx_api_key_route_config_outbox_delivered
    ON api_key_route_config_outbox (delivered_at, id)
    WHERE delivered_at IS NOT NULL;

CREATE OR REPLACE FUNCTION enqueue_route_dependent_group_auth_cache_invalidation(target_group_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    IF target_group_id IS NULL THEN
        RETURN;
    END IF;
    UPDATE api_keys AS target
    SET routing_dependency_version = target.routing_dependency_version + 1,
        updated_at = NOW()
    WHERE target.id IN (
        SELECT k.id
        FROM api_keys AS k
        WHERE k.group_id = target_group_id
          AND k.deleted_at IS NULL
          AND k.key <> ''
        UNION
        SELECT k.id
        FROM api_key_group_routes AS route
        JOIN api_keys AS k ON k.id = route.api_key_id
        WHERE route.group_id = target_group_id
          AND k.deleted_at IS NULL
          AND k.key <> ''
    );
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_route_dependent_user_group_auth_cache_invalidation(target_user_id BIGINT, target_group_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    IF target_user_id IS NULL OR target_group_id IS NULL THEN
        RETURN;
    END IF;
    UPDATE api_keys AS target
    SET routing_dependency_version = target.routing_dependency_version + 1,
        updated_at = NOW()
    WHERE target.id IN (
        SELECT k.id
        FROM api_keys AS k
        WHERE k.user_id = target_user_id
          AND k.group_id = target_group_id
          AND k.deleted_at IS NULL
          AND k.key <> ''
        UNION
        SELECT k.id
        FROM api_key_group_routes AS route
        JOIN api_keys AS k ON k.id = route.api_key_id
        WHERE k.user_id = target_user_id
          AND route.group_id = target_group_id
          AND k.deleted_at IS NULL
          AND k.key <> ''
    );
END;
$$;

-- Route-only changes can retain the same compatibility group_id. Include the
-- explicit route fields so the older generic trigger cannot miss that case.
CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        RETURN OLD;
    END IF;

    IF OLD.key IS DISTINCT FROM NEW.key
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
       OR OLD.user_id IS DISTINCT FROM NEW.user_id
       OR OLD.group_id IS DISTINCT FROM NEW.group_id
       OR OLD.schedule_mode IS DISTINCT FROM NEW.schedule_mode
       OR OLD.smart_preference IS DISTINCT FROM NEW.smart_preference
       OR OLD.route_version IS DISTINCT FROM NEW.route_version
       OR OLD.routing_dependency_version IS DISTINCT FROM NEW.routing_dependency_version
       OR OLD.ip_whitelist IS DISTINCT FROM NEW.ip_whitelist
       OR OLD.ip_blacklist IS DISTINCT FROM NEW.ip_blacklist
       OR OLD.expires_at IS DISTINCT FROM NEW.expires_at THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        IF NEW.deleted_at IS NULL AND NEW.key IS DISTINCT FROM OLD.key THEN
            PERFORM enqueue_auth_cache_invalidation(NEW.key);
        END IF;
    END IF;
    IF OLD.routing_dependency_version IS DISTINCT FROM NEW.routing_dependency_version THEN
        INSERT INTO api_key_route_config_outbox (
            event_key, api_key_id, route_version, event_type, payload
        ) VALUES (
            format('api_key_dependency:%s:v%s', NEW.id, NEW.routing_dependency_version),
            NEW.id,
            NEW.route_version,
            'api_key_route_dependency_changed',
            jsonb_build_object(
                'api_key_id', NEW.id,
                'old_route_version', NEW.route_version,
                'route_version', NEW.route_version,
                'old_dependency_version', OLD.routing_dependency_version,
                'dependency_version', NEW.routing_dependency_version,
                'auth_cache_key', encode(sha256(convert_to(NEW.key, 'UTF8')), 'hex')
            )
        ) ON CONFLICT (event_key) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    -- Group writes are administrative and cold. Invalidate on every material
    -- row update so newly added price/capability fields are covered by default.
    IF TG_OP = 'UPDATE'
       AND to_jsonb(OLD) - ARRAY['updated_at', 'created_at', 'name', 'description', 'sort_order', 'duplicate_operation_id']::text[]
           IS NOT DISTINCT FROM
           to_jsonb(NEW) - ARRAY['updated_at', 'created_at', 'name', 'description', 'sort_order', 'duplicate_operation_id']::text[] THEN
        RETURN NEW;
    END IF;
    PERFORM enqueue_route_dependent_group_auth_cache_invalidation(OLD.id);
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_groups_auth_cache_invalidation ON groups;
CREATE TRIGGER trg_groups_auth_cache_invalidation
BEFORE UPDATE OR DELETE ON groups
FOR EACH ROW EXECUTE FUNCTION enqueue_group_auth_cache_invalidation();

CREATE OR REPLACE FUNCTION enqueue_allowed_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.user_id IS NOT DISTINCT FROM NEW.user_id
       AND OLD.group_id IS NOT DISTINCT FROM NEW.group_id THEN
        PERFORM enqueue_route_dependent_user_group_auth_cache_invalidation(NEW.user_id, NEW.group_id);
        RETURN NEW;
    END IF;
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        PERFORM enqueue_route_dependent_user_group_auth_cache_invalidation(OLD.user_id, OLD.group_id);
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        PERFORM enqueue_route_dependent_user_group_auth_cache_invalidation(NEW.user_id, NEW.group_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_user_allowed_groups_auth_cache_invalidation ON user_allowed_groups;
CREATE TRIGGER trg_user_allowed_groups_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON user_allowed_groups
FOR EACH ROW EXECUTE FUNCTION enqueue_allowed_group_auth_cache_invalidation();

CREATE OR REPLACE FUNCTION enqueue_user_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.role IS NOT DISTINCT FROM NEW.role
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at
       AND OLD.restrict_public_groups IS NOT DISTINCT FROM NEW.restrict_public_groups THEN
        RETURN NEW;
    END IF;
    UPDATE api_keys
    SET routing_dependency_version = routing_dependency_version + 1,
        updated_at = NOW()
    WHERE user_id = OLD.id
      AND deleted_at IS NULL
      AND key <> '';
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_users_auth_cache_invalidation ON users;
CREATE TRIGGER trg_users_auth_cache_invalidation
BEFORE UPDATE OR DELETE ON users
FOR EACH ROW EXECUTE FUNCTION enqueue_user_auth_cache_invalidation();

CREATE OR REPLACE FUNCTION enqueue_user_subscription_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    -- Usage counters are hot and deliberately excluded. Only subscription
    -- admission/identity changes invalidate dependent route snapshots.
    IF TG_OP = 'UPDATE'
       AND OLD.user_id IS NOT DISTINCT FROM NEW.user_id
       AND OLD.group_id IS NOT DISTINCT FROM NEW.group_id
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.starts_at IS NOT DISTINCT FROM NEW.starts_at
       AND OLD.expires_at IS NOT DISTINCT FROM NEW.expires_at
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND OLD.user_id IS NOT DISTINCT FROM NEW.user_id
       AND OLD.group_id IS NOT DISTINCT FROM NEW.group_id THEN
        PERFORM enqueue_route_dependent_user_group_auth_cache_invalidation(NEW.user_id, NEW.group_id);
        RETURN NEW;
    END IF;
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        PERFORM enqueue_route_dependent_user_group_auth_cache_invalidation(OLD.user_id, OLD.group_id);
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        PERFORM enqueue_route_dependent_user_group_auth_cache_invalidation(NEW.user_id, NEW.group_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_user_subscriptions_auth_cache_invalidation ON user_subscriptions;
CREATE TRIGGER trg_user_subscriptions_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON user_subscriptions
FOR EACH ROW EXECUTE FUNCTION enqueue_user_subscription_auth_cache_invalidation();

CREATE OR REPLACE FUNCTION enqueue_user_group_rate_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.user_id IS NOT DISTINCT FROM NEW.user_id
       AND OLD.group_id IS NOT DISTINCT FROM NEW.group_id
       AND OLD.rate_multiplier IS NOT DISTINCT FROM NEW.rate_multiplier
       AND OLD.rpm_override IS NOT DISTINCT FROM NEW.rpm_override THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND OLD.user_id IS NOT DISTINCT FROM NEW.user_id
       AND OLD.group_id IS NOT DISTINCT FROM NEW.group_id THEN
        PERFORM enqueue_route_dependent_user_group_auth_cache_invalidation(NEW.user_id, NEW.group_id);
        RETURN NEW;
    END IF;
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        PERFORM enqueue_route_dependent_user_group_auth_cache_invalidation(OLD.user_id, OLD.group_id);
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        PERFORM enqueue_route_dependent_user_group_auth_cache_invalidation(NEW.user_id, NEW.group_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_user_group_rates_auth_cache_invalidation ON user_group_rate_multipliers;
CREATE TRIGGER trg_user_group_rates_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON user_group_rate_multipliers
FOR EACH ROW EXECUTE FUNCTION enqueue_user_group_rate_auth_cache_invalidation();

COMMENT ON FUNCTION enqueue_route_dependent_group_auth_cache_invalidation(BIGINT) IS
    'Bumps a monotonic dependency guard for legacy primary and multi-group reverse dependencies';
