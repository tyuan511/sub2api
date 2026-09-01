-- Support workspace lists every ticket by its most recent message.
-- Keep the tie-breaker in the index so pagination can use one ordered scan.
CREATE INDEX IF NOT EXISTS idx_support_tickets_last_message
    ON support_tickets (last_message_at DESC, id DESC);
