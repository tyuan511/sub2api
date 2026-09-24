package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeSafeSystemOnePlatformMigration(t *testing.T) {
	content, err := FS.ReadFile("255_typesafe_systemone_platform.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check")
	require.Contains(t, sql, "'opencode_go', 'typesafe'))")
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check")
}
