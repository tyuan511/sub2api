-- 退款「已完成」支持可选备注（打款流水号/渠道单号等），与拒绝原因对称、对用户可见。
-- 258/259 已发布到环境，这里只追加列。

ALTER TABLE balance_refund_requests
    ADD COLUMN IF NOT EXISTS admin_note TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN balance_refund_requests.admin_note IS '管理员完成退款时的可选备注，对用户可见';
