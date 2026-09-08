-- Speed up bounded, priority-tiered routing fact retention cleanup without
-- locking writes on a populated routing_attempts table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_routing_attempts_priority_occurred
    ON routing_attempts (event_priority, occurred_at, id);
