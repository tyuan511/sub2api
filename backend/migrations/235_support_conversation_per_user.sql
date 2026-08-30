-- Customer support is one continuous conversation per user. Consolidate any
-- duplicate tickets before enforcing the invariant in the database.

CREATE TEMP TABLE support_ticket_merge_map ON COMMIT DROP AS
SELECT
    id AS source_id,
    FIRST_VALUE(id) OVER (
        PARTITION BY user_id
        ORDER BY last_message_at DESC, id DESC
    ) AS target_id
FROM support_tickets;

CREATE TEMP TABLE support_ticket_read_merge ON COMMIT DROP AS
SELECT
    merge_map.target_id AS ticket_id,
    ticket_read.reader_id,
    MAX(ticket_read.last_read_message_id) AS last_read_message_id,
    MAX(ticket_read.read_at) AS read_at,
    MIN(ticket_read.created_at) AS created_at
FROM support_ticket_reads AS ticket_read
JOIN support_ticket_merge_map AS merge_map ON merge_map.source_id = ticket_read.ticket_id
GROUP BY merge_map.target_id, ticket_read.reader_id;

UPDATE support_ticket_messages AS message
SET ticket_id = merge_map.target_id
FROM support_ticket_merge_map AS merge_map
WHERE message.ticket_id = merge_map.source_id
  AND merge_map.source_id <> merge_map.target_id;

UPDATE support_ticket_attachments AS attachment
SET ticket_id = merge_map.target_id
FROM support_ticket_merge_map AS merge_map
WHERE attachment.ticket_id = merge_map.source_id
  AND merge_map.source_id <> merge_map.target_id;

UPDATE support_notification_outbox AS notification
SET ticket_id = merge_map.target_id
FROM support_ticket_merge_map AS merge_map
WHERE notification.ticket_id = merge_map.source_id
  AND merge_map.source_id <> merge_map.target_id;

DELETE FROM support_ticket_reads;

INSERT INTO support_ticket_reads (
    ticket_id,
    reader_id,
    last_read_message_id,
    read_at,
    created_at
)
SELECT ticket_id, reader_id, last_read_message_id, read_at, created_at
FROM support_ticket_read_merge;

WITH merged_ticket_times AS (
    SELECT
        merge_map.target_id,
        MIN(ticket.created_at) AS created_at,
        MAX(ticket.updated_at) AS updated_at
    FROM support_ticket_merge_map AS merge_map
    JOIN support_tickets AS ticket ON ticket.id = merge_map.source_id
    GROUP BY merge_map.target_id
), latest_messages AS (
    SELECT DISTINCT ON (message.ticket_id)
        message.ticket_id,
        message.id AS message_id,
        message.created_at
    FROM support_ticket_messages AS message
    ORDER BY message.ticket_id, message.created_at DESC, message.id DESC
)
UPDATE support_tickets AS ticket
SET
    last_message_id = latest_messages.message_id,
    last_message_at = latest_messages.created_at,
    created_at = merged_ticket_times.created_at,
    updated_at = GREATEST(merged_ticket_times.updated_at, latest_messages.created_at)
FROM merged_ticket_times
JOIN latest_messages ON latest_messages.ticket_id = merged_ticket_times.target_id
WHERE ticket.id = merged_ticket_times.target_id;

DELETE FROM support_tickets AS ticket
USING support_ticket_merge_map AS merge_map
WHERE ticket.id = merge_map.source_id
  AND merge_map.source_id <> merge_map.target_id;

DROP INDEX IF EXISTS idx_support_tickets_user_status;
CREATE UNIQUE INDEX IF NOT EXISTS supportticket_user_id ON support_tickets (user_id);
