-- Telegram notifications for user balance recharge and redeem-code redemption.
-- Adds per-binding toggles and makes the outbox ticket reference optional so
-- non-support events can reuse the same retryable delivery worker.

ALTER TABLE admin_telegram_bindings
    ADD COLUMN IF NOT EXISTS notify_balance_recharge BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE admin_telegram_bindings
    ADD COLUMN IF NOT EXISTS notify_redeem BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE support_notification_outbox
    ALTER COLUMN ticket_id DROP NOT NULL;
