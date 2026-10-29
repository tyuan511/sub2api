-- 余额退款申请：用户提交收款二维码发起退款，管理员审核为「已完成」或「拒绝」。
-- 金额字段是提交时刻的快照，费率或返利之后变化都不会改写历史。

CREATE TABLE IF NOT EXISTS balance_refund_requests (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT NOT NULL,
    balance           DECIMAL(20,8) NOT NULL DEFAULT 0,
    rebate_amount     DECIMAL(20,8) NOT NULL DEFAULT 0,
    fee_rate_percent  DECIMAL(5,2) NOT NULL DEFAULT 0,
    fee_amount        DECIMAL(20,2) NOT NULL DEFAULT 0,
    refundable_amount DECIMAL(20,2) NOT NULL DEFAULT 0,
    qr_storage_key    VARCHAR(500) NOT NULL,
    qr_content_type   VARCHAR(64) NOT NULL DEFAULT 'image/png',
    qr_size           BIGINT NOT NULL DEFAULT 0,
    status            VARCHAR(20) NOT NULL DEFAULT 'pending',
    user_note         TEXT NOT NULL DEFAULT '',
    reject_reason     TEXT,
    reviewed_by       BIGINT,
    reviewed_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT balance_refund_requests_status_check
        CHECK (status IN ('pending', 'completed', 'rejected')),
    CONSTRAINT balance_refund_requests_fee_rate_check
        CHECK (fee_rate_percent >= 0 AND fee_rate_percent <= 100)
);

-- 一个用户最多只能有一笔待处理申请（服务层校验之外的兜底约束）。
CREATE UNIQUE INDEX IF NOT EXISTS balance_refund_requests_pending_uq
    ON balance_refund_requests (user_id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_balance_refund_requests_status_created
    ON balance_refund_requests (status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_balance_refund_requests_user_created
    ON balance_refund_requests (user_id, created_at DESC);
