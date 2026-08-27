package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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
