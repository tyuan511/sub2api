-- 退款申请提交后冻结整笔余额：users.balance 转入 users.frozen_balance，
-- 审核期间不可消费；拒绝解冻退回，完成则结清冻结额。
-- 258 已经发布到环境，这里只追加列，不修改已应用的迁移文件。

ALTER TABLE balance_refund_requests
    ADD COLUMN IF NOT EXISTS frozen_amount DECIMAL(20,8) NOT NULL DEFAULT 0;

COMMENT ON COLUMN balance_refund_requests.frozen_amount IS '提交时冻结的余额（转入 users.frozen_balance 的金额）';
