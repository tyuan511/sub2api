//go:build unit

package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 余额退款申请表必须由迁移建出可退金额快照 + 「一人只有一笔待处理」的兜底约束，
// 否则并发提交或重复审核会留下两笔待处理申请让管理员重复打款。
func TestMigration258CreatesBalanceRefundRequests(t *testing.T) {
	content, err := FS.ReadFile("258_balance_refund_requests.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS balance_refund_requests")
	for _, column := range []string{
		"user_id", "balance", "rebate_amount", "fee_rate_percent",
		"fee_amount", "refundable_amount", "qr_storage_key", "status", "reject_reason",
	} {
		require.Contains(t, sql, column)
	}
	require.Contains(t, sql, "CHECK (status IN ('pending', 'completed', 'rejected'))")
	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS balance_refund_requests_pending_uq")
	require.Contains(t, sql, "WHERE status = 'pending'")
	// 幂等：可重复执行。
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS")
	require.NotContains(t, sql, "DROP TABLE")
}
