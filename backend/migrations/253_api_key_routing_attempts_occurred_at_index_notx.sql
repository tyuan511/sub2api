-- The monitor aggregates routing_attempts by occurred_at.  Keep this index
-- separate and concurrent so the first deployment does not block fact writes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_routing_attempts_occurred_at_id
    ON routing_attempts (occurred_at, id);
