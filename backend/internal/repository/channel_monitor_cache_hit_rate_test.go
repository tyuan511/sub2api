package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRepositoryComputeCacheHitRatesForGroups(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &channelMonitorRepository{db: db}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT g.name")).
		WillReturnRows(sqlmock.NewRows([]string{
			"name", "requests", "input_tokens", "cache_read_tokens", "cache_creation_tokens",
		}).AddRow("gpt-pro-20x", int64(12), int64(20), int64(30), int64(10)))

	got, err := repo.ComputeCacheHitRatesForGroups(context.Background(), []string{"gpt-pro-20x"}, 7)
	require.NoError(t, err)
	require.Contains(t, got, "gpt-pro-20x")
	require.Equal(t, int64(12), got["gpt-pro-20x"].Requests)
	require.InDelta(t, 50.0, got["gpt-pro-20x"].CacheHitRatePct, 0.0001)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorRepositoryComputeCacheHitRatesForGroupsEmpty(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &channelMonitorRepository{db: db}

	got, err := repo.ComputeCacheHitRatesForGroups(context.Background(), nil, 7)
	require.NoError(t, err)
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorRepositoryRefreshGroupCacheHitRateSnapshots(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &channelMonitorRepository{db: db}

	mock.ExpectExec(regexp.QuoteMeta("WITH windows AS")).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 3))

	err := repo.RefreshGroupCacheHitRateSnapshots(context.Background(), []string{"gpt-pro-20x"})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorRepositoryRefreshGroupCacheHitRateSnapshotsEmpty(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &channelMonitorRepository{db: db}

	require.NoError(t, repo.RefreshGroupCacheHitRateSnapshots(context.Background(), nil))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorRepositoryListGroupCacheHitRateSnapshots(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &channelMonitorRepository{db: db}
	computedAt := time.Date(2026, 8, 27, 1, 2, 3, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT group_name, window_days")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"group_name", "window_days", "requests", "input_tokens", "cache_read_tokens",
			"cache_creation_tokens", "cache_hit_rate_pct", "computed_at",
		}).AddRow("gpt-pro-20x", 7, int64(12), int64(20), int64(30), int64(10), 50.0, computedAt))

	got, err := repo.ListGroupCacheHitRateSnapshots(context.Background(), []string{"gpt-pro-20x"})
	require.NoError(t, err)
	require.NotNil(t, got["gpt-pro-20x"][7])
	require.Equal(t, int64(12), got["gpt-pro-20x"][7].Requests)
	require.InDelta(t, 50.0, got["gpt-pro-20x"][7].CacheHitRatePct, 0.0001)
	require.Equal(t, computedAt, got["gpt-pro-20x"][7].ComputedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorRepositoryListGroupCacheHitRateSnapshotsEmpty(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &channelMonitorRepository{db: db}

	got, err := repo.ListGroupCacheHitRateSnapshots(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

var _ service.ChannelMonitorRepository = (*channelMonitorRepository)(nil)
