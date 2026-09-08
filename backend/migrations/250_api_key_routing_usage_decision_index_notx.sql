-- The usage log table is populated in production. Build its routing lookup
-- index without taking the long AccessExclusive lock of a regular CREATE INDEX.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_routing_decision
    ON usage_logs (routing_decision_id) WHERE routing_decision_id IS NOT NULL;
