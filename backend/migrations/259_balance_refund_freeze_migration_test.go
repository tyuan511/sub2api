//go:build unit

package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 259 只允许追加 frozen_amount 列：258 已经发布到环境，迁移文件一旦落地就不能改
// （checksum 校验会拒绝），所以冻结能力必须由新迁移补齐。
func TestMigration259AddsRefundFreezeColumn(t *testing.T) {
	content, err := FS.ReadFile("259_balance_refund_freeze.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ALTER TABLE balance_refund_requests")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS frozen_amount DECIMAL(20,8) NOT NULL DEFAULT 0")
	// 幂等：可重复执行。
	require.Contains(t, sql, "IF NOT EXISTS")
	require.NotContains(t, sql, "DROP TABLE")
	require.NotContains(t, sql, "DROP COLUMN")

	// 258 必须保持原样，不能被后续需求改写。
	base, err := FS.ReadFile("258_balance_refund_requests.sql")
	require.NoError(t, err)
	require.NotContains(t, string(base), "frozen_amount")
}
