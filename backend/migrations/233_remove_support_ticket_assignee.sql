-- Ticket ownership is intentionally collaborative: every administrator can view
-- and reply to every ticket, so the legacy assignee field is no longer used.
DROP INDEX IF EXISTS idx_support_tickets_assignee_status;
ALTER TABLE support_tickets DROP COLUMN IF EXISTS assigned_admin_id;
