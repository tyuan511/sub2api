-- Nullable balance preserves existing preset/optimized policies until edited.
ALTER TABLE api_keys
    ADD COLUMN smart_balance_bps integer,
    ADD COLUMN routing_min_success_rate integer NOT NULL DEFAULT 50,
    ADD COLUMN routing_state_version bigint NOT NULL DEFAULT 1;

UPDATE api_keys SET routing_state_version = route_version;

ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_smart_balance_bps_check CHECK (smart_balance_bps BETWEEN 0 AND 10000),
    ADD CONSTRAINT api_keys_routing_min_success_rate_check CHECK
        (routing_min_success_rate BETWEEN 50 AND 95 AND routing_min_success_rate % 5 = 0),
    ADD CONSTRAINT api_keys_routing_state_version_check CHECK (routing_state_version > 0);

-- Config edits still increment route_version and use the existing transactional
-- auth invalidation/outbox. This trigger also protects direct administrative SQL.
CREATE FUNCTION guard_api_key_routing_controls() RETURNS trigger AS $$
BEGIN
    IF NEW.smart_balance_bps IS DISTINCT FROM OLD.smart_balance_bps
       OR NEW.routing_min_success_rate IS DISTINCT FROM OLD.routing_min_success_rate THEN
        NEW.route_version := GREATEST(NEW.route_version, OLD.route_version + 1);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER api_key_routing_controls_guard BEFORE UPDATE ON api_keys
FOR EACH ROW EXECUTE FUNCTION guard_api_key_routing_controls();

-- Decision-time controls must survive Redis stream consumption and API-key edits.
ALTER TABLE routing_attempts
    ADD COLUMN smart_balance_bps integer CHECK (smart_balance_bps BETWEEN 0 AND 10000),
    ADD COLUMN routing_min_success_rate integer NOT NULL DEFAULT 50 CHECK
        (routing_min_success_rate BETWEEN 50 AND 95 AND routing_min_success_rate % 5 = 0),
    ADD COLUMN routing_state_version bigint CHECK (routing_state_version > 0);
