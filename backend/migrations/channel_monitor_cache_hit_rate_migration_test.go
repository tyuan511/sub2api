package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration239AllowsThreeDayCacheHitRateSnapshots(t *testing.T) {
	content, err := FS.ReadFile("239_channel_monitor_cache_hit_rate_snapshots_add_3d.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS channel_monitor_cache_hit_rate_snapshots_window_check")
	require.Contains(t, sql, "ADD CONSTRAINT channel_monitor_cache_hit_rate_snapshots_window_check")
	require.Contains(t, sql, "CHECK (window_days IN (3, 7, 15, 30))")
}
