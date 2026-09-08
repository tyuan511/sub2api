-- Avoid write amplification when a group or user entitlement changes.
--
-- The dependency invalidation trigger used to UPDATE every dependent api_keys
-- row just to bump routing_dependency_version.  That caused row locks and
-- retriggered api-key outbox work during ordinary group administration.  The
-- auth snapshot is already invalidated through the durable outbox, so enqueue
-- the dependent cache digests directly and leave api_keys untouched.

CREATE OR REPLACE FUNCTION enqueue_route_dependent_group_auth_cache_invalidation(target_group_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    IF target_group_id IS NULL THEN
        RETURN;
    END IF;

    WITH dependent_keys AS (
        SELECT k.key AS raw_key
        FROM api_keys AS k
        WHERE k.group_id = target_group_id
          AND k.deleted_at IS NULL
          AND k.key <> ''
        UNION
        SELECT k.key AS raw_key
        FROM api_key_group_routes AS route
        JOIN api_keys AS k ON k.id = route.api_key_id
        WHERE route.group_id = target_group_id
          AND k.deleted_at IS NULL
          AND k.key <> ''
    )
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(raw_key, 'UTF8')), 'hex')
    FROM dependent_keys;
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

    WITH dependent_keys AS (
        SELECT k.key AS raw_key
        FROM api_keys AS k
        WHERE k.user_id = target_user_id
          AND k.group_id = target_group_id
          AND k.deleted_at IS NULL
          AND k.key <> ''
        UNION
        SELECT k.key AS raw_key
        FROM api_key_group_routes AS route
        JOIN api_keys AS k ON k.id = route.api_key_id
        WHERE k.user_id = target_user_id
          AND route.group_id = target_group_id
          AND k.deleted_at IS NULL
          AND k.key <> ''
    )
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(raw_key, 'UTF8')), 'hex')
    FROM dependent_keys;
END;
$$;

COMMENT ON FUNCTION enqueue_route_dependent_group_auth_cache_invalidation(BIGINT) IS
    'Queues SHA-256 auth-cache invalidations for group dependents without updating api_keys rows';

COMMENT ON FUNCTION enqueue_route_dependent_user_group_auth_cache_invalidation(BIGINT, BIGINT) IS
    'Queues SHA-256 auth-cache invalidations for user/group dependents without updating api_keys rows';
