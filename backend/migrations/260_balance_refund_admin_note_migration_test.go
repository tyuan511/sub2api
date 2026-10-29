//go:build unit

package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 完成备注必须由新迁移追加，且默认空串：老数据（没有备注）不能因为加列而变成 NULL，
// 否则服务层读出来要到处判空。
func TestMigration260AddsRefundAdminNote(t *testing.T) {
	content, err := FS.ReadFile("260_balance_refund_admin_note.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ALTER TABLE balance_refund_requests")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS admin_note TEXT NOT NULL DEFAULT ''")
	require.Contains(t, sql, "IF NOT EXISTS")
	require.NotContains(t, sql, "DROP")

	// 已发布的迁移保持原样。
	base, err := FS.ReadFile("258_balance_refund_requests.sql")
	require.NoError(t, err)
	require.NotContains(t, string(base), "admin_note")
	freeze, err := FS.ReadFile("259_balance_refund_freeze.sql")
	require.NoError(t, err)
	require.NotContains(t, string(freeze), "admin_note")
}
