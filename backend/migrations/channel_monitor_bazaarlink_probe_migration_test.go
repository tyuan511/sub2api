package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorBazaarLinkProbeMigration(t *testing.T) {
	content, err := FS.ReadFile("244_channel_monitor_bazaarlink_probe.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS channel_monitor_bazaarlink_probes")
	require.Contains(t, sql, "result JSONB NOT NULL")
	require.Contains(t, sql, "idx_channel_monitor_bazaarlink_probes_latest")
	require.Contains(t, sql, "remote_run_id TEXT")
	require.Contains(t, sql, "task_status VARCHAR(20)")
	require.Contains(t, sql, "idx_channel_monitor_bazaarlink_tasks_due")
	require.NotContains(t, sql, "ALTER TABLE")
	require.NotContains(t, sql, "last_polled_at")
	require.NotContains(t, sql, "task_error")
}
