package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestProvideRoutingBackgroundDBUsesDedicatedBoundedLazyPool(t *testing.T) {
	cfg := &config.Config{
		Timezone: "Asia/Shanghai",
		Database: config.DatabaseConfig{
			Host:                          "127.0.0.1",
			Port:                          5432,
			User:                          "postgres",
			DBName:                        "sub2api",
			SSLMode:                       "disable",
			MaxOpenConns:                  256,
			MaxIdleConns:                  128,
			ConnMaxLifetimeMinutes:        30,
			ConnMaxIdleTimeMinutes:        5,
			RoutingBackgroundMaxOpenConns: 3,
			RoutingBackgroundMaxIdleConns: 1,
		},
	}

	pool, err := ProvideRoutingBackgroundDB(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	require.NotNil(t, pool.Client)
	require.NotNil(t, pool.DB)

	stats := pool.DB.Stats()
	require.Equal(t, 3, stats.MaxOpenConnections)
	require.Zero(t, stats.OpenConnections, "sql.Open must stay lazy until a routing background job actually queries")
	require.NoError(t, pool.Close())
	require.NoError(t, pool.Close(), "cleanup must be idempotent")
}

func TestProvideRoutingBackgroundDBKeepsPoolBoundedWithZeroValueConfig(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Host:         "127.0.0.1",
			Port:         5432,
			User:         "postgres",
			DBName:       "sub2api",
			SSLMode:      "disable",
			MaxOpenConns: 1,
		},
	}

	pool, err := ProvideRoutingBackgroundDB(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	require.Equal(t, 2, pool.DB.Stats().MaxOpenConnections)
}
