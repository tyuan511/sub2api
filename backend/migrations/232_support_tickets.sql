-- Native customer support tickets, private image metadata, read state and
-- retryable Telegram notifications for administrators.

CREATE TABLE IF NOT EXISTS support_tickets (
    id                  BIGSERIAL PRIMARY KEY,
    ticket_no           VARCHAR(32) NOT NULL UNIQUE,
    user_id             BIGINT NOT NULL,
    title               VARCHAR(200) NOT NULL,
    category            VARCHAR(32) NOT NULL,
    status              VARCHAR(24) NOT NULL DEFAULT 'open',
    priority            VARCHAR(16) NOT NULL DEFAULT 'normal',
    assigned_admin_id   BIGINT,
    last_message_id     BIGINT,
    last_message_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at         TIMESTAMPTZ,
    closed_at           TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT support_tickets_status_check CHECK (status IN ('open', 'in_progress', 'waiting_user', 'resolved', 'closed')),
    CONSTRAINT support_tickets_priority_check CHECK (priority IN ('low', 'normal', 'high', 'urgent'))
);

CREATE INDEX IF NOT EXISTS idx_support_tickets_user_status ON support_tickets (user_id, status);
CREATE INDEX IF NOT EXISTS idx_support_tickets_status_last_message ON support_tickets (status, last_message_at DESC);
CREATE INDEX IF NOT EXISTS idx_support_tickets_assignee_status ON support_tickets (assigned_admin_id, status);
CREATE INDEX IF NOT EXISTS idx_support_tickets_category_created ON support_tickets (category, created_at DESC);

CREATE TABLE IF NOT EXISTS support_ticket_messages (
    id                  BIGSERIAL PRIMARY KEY,
    ticket_id           BIGINT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    sender_id           BIGINT NOT NULL,
    sender_role         VARCHAR(16) NOT NULL,
    kind                VARCHAR(16) NOT NULL DEFAULT 'public',
    content             TEXT NOT NULL DEFAULT '',
    client_request_id   VARCHAR(64),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT support_ticket_messages_role_check CHECK (sender_role IN ('user', 'admin', 'system')),
    CONSTRAINT support_ticket_messages_kind_check CHECK (kind IN ('public', 'note', 'system')),
    CONSTRAINT support_ticket_messages_sender_request_unique UNIQUE (sender_id, client_request_id)
);

CREATE INDEX IF NOT EXISTS idx_support_ticket_messages_ticket_id ON support_ticket_messages (ticket_id, id);

CREATE TABLE IF NOT EXISTS support_ticket_attachments (
    id                  BIGSERIAL PRIMARY KEY,
    ticket_id           BIGINT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    message_id          BIGINT NOT NULL REFERENCES support_ticket_messages(id) ON DELETE CASCADE,
    uploader_id         BIGINT NOT NULL,
    storage_key         VARCHAR(500) NOT NULL UNIQUE,
    original_name       VARCHAR(255) NOT NULL,
    content_type        VARCHAR(64) NOT NULL,
    size                BIGINT NOT NULL,
    width               INT NOT NULL,
    height              INT NOT NULL,
    sha256              VARCHAR(64) NOT NULL,
    hidden_at           TIMESTAMPTZ,
    hidden_by           BIGINT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_support_ticket_attachments_message ON support_ticket_attachments (ticket_id, message_id);
CREATE INDEX IF NOT EXISTS idx_support_ticket_attachments_uploader ON support_ticket_attachments (uploader_id, created_at DESC);

CREATE TABLE IF NOT EXISTS support_ticket_reads (
    id                      BIGSERIAL PRIMARY KEY,
    ticket_id               BIGINT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    reader_id               BIGINT NOT NULL,
    last_read_message_id    BIGINT NOT NULL,
    read_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT support_ticket_reads_unique UNIQUE (ticket_id, reader_id)
);

CREATE INDEX IF NOT EXISTS idx_support_ticket_reads_reader ON support_ticket_reads (reader_id, read_at DESC);

CREATE TABLE IF NOT EXISTS admin_telegram_bindings (
    id                      BIGSERIAL PRIMARY KEY,
    admin_id                BIGINT NOT NULL UNIQUE,
    telegram_user_id        BIGINT NOT NULL UNIQUE,
    chat_id                 BIGINT NOT NULL,
    telegram_username       VARCHAR(64),
    enabled                 BOOLEAN NOT NULL DEFAULT TRUE,
    notify_new_ticket       BOOLEAN NOT NULL DEFAULT TRUE,
    notify_user_reply       BOOLEAN NOT NULL DEFAULT TRUE,
    notify_high_priority    BOOLEAN NOT NULL DEFAULT TRUE,
    bound_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_success_at         TIMESTAMPTZ,
    last_error              TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_admin_telegram_bindings_enabled ON admin_telegram_bindings (enabled);
CREATE INDEX IF NOT EXISTS idx_admin_telegram_bindings_chat ON admin_telegram_bindings (chat_id);

CREATE TABLE IF NOT EXISTS support_notification_outbox (
    id                  BIGSERIAL PRIMARY KEY,
    event_type          VARCHAR(32) NOT NULL,
    ticket_id           BIGINT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    message_id          BIGINT,
    target_admin_id     BIGINT NOT NULL,
    payload             TEXT NOT NULL,
    status              VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempt_count       INT NOT NULL DEFAULT 0,
    next_attempt_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at           TIMESTAMPTZ,
    last_error          TEXT,
    sent_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT support_notification_outbox_status_check CHECK (status IN ('pending', 'processing', 'sent', 'failed', 'dead'))
);

CREATE INDEX IF NOT EXISTS idx_support_notification_outbox_ready ON support_notification_outbox (status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_support_notification_outbox_admin ON support_notification_outbox (target_admin_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_support_notification_outbox_ticket ON support_notification_outbox (ticket_id, created_at DESC);
