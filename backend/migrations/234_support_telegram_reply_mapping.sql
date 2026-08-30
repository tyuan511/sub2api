-- Persist the Telegram notification message ID so an administrator can reply
-- to the exact notification message without selecting a conversation manually.
ALTER TABLE support_notification_outbox
    ADD COLUMN IF NOT EXISTS telegram_message_id BIGINT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_support_notification_outbox_tg_message
    ON support_notification_outbox (target_admin_id, telegram_message_id)
    WHERE telegram_message_id IS NOT NULL;
