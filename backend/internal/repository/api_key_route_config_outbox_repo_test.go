package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRouteConfigOutboxRepository_ClaimUsesLeaseAndSkipLocked(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	created := time.Now().UTC()
	mock.ExpectQuery("(?s)delivered_at IS NULL.*claimed_at < NOW\\(\\) - .*LIMIT \\$2.*FOR UPDATE SKIP LOCKED.*RETURNING").
		WithArgs("worker-a", 100, int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "event_key", "api_key_id", "old_route_version", "route_version", "old_dependency_version", "dependency_version", "event_type", "auth_cache_key", "attempts", "created_at",
		}).AddRow(int64(4), "api_key_route:7:v2", int64(7), int64(1), int64(2), int64(0), int64(1), "api_key_route_config_changed", strings.Repeat("a", 64), 3, created))

	repo := NewAPIKeyRouteConfigOutboxRepository(db)
	events, err := repo.ClaimAPIKeyRouteConfigEvents(context.Background(), "worker-a", 100, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(1), events[0].OldRouteVersion)
	require.Equal(t, int64(2), events[0].RouteVersion)
	require.Equal(t, int64(1), events[0].DependencyVersion)
	require.Equal(t, strings.Repeat("a", 64), events[0].AuthCacheKey)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyRouteConfigOutboxRepository_ClaimIsBoundedByDefault(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("(?s)FROM api_key_route_config_outbox.*LIMIT \\$2.*SKIP LOCKED").
		WithArgs("worker", 100, int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "event_key", "api_key_id", "old_route_version", "route_version", "old_dependency_version", "dependency_version", "event_type", "auth_cache_key", "attempts", "created_at",
		}))
	repo := NewAPIKeyRouteConfigOutboxRepository(db)
	_, err = repo.ClaimAPIKeyRouteConfigEvents(context.Background(), "worker", 0, 0)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyRouteConfigOutboxRepository_DeliveryRetryCleanupAndStats(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := NewAPIKeyRouteConfigOutboxRepository(db)
	now := time.Now().UTC()

	mock.ExpectExec("(?s)UPDATE api_key_route_config_outbox.*delivered_at = \\$3").
		WithArgs(int64(1), "worker", now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.MarkAPIKeyRouteConfigEventDelivered(context.Background(), 1, "worker", now))

	retryAt := now.Add(time.Minute)
	mock.ExpectExec("(?s)UPDATE api_key_route_config_outbox.*attempts = attempts \\+ 1").
		WithArgs(int64(2), "worker", retryAt, "redis down").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.RetryAPIKeyRouteConfigEvent(context.Background(), 2, "worker", retryAt, "redis down"))

	oldest := now.Add(-time.Minute)
	mock.ExpectQuery("(?s)COUNT\\(\\*\\) FILTER.*MAX\\(attempts\\) FILTER").
		WillReturnRows(sqlmock.NewRows([]string{"pending", "delivered", "oldest", "max_attempts", "last_error"}).
			AddRow(5, 8, oldest, 7, "redis down"))
	stats, err := repo.APIKeyRouteConfigOutboxStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(5), stats.Pending)
	require.Equal(t, int64(8), stats.DeliveredRetained)
	require.Equal(t, 7, stats.MaxAttempts)

	before := now.Add(-7 * 24 * time.Hour)
	mock.ExpectExec("(?s)WITH expired AS.*delivered_at < \\$1.*LIMIT \\$2.*DELETE FROM api_key_route_config_outbox").
		WithArgs(before, 1000).
		WillReturnResult(sqlmock.NewResult(0, 17))
	cleaned, err := repo.CleanupDeliveredAPIKeyRouteConfigEvents(context.Background(), before, 0)
	require.NoError(t, err)
	require.Equal(t, int64(17), cleaned)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyRouteConfigOutboxRepository_RejectsLostClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	now := time.Now().UTC()
	mock.ExpectExec("UPDATE api_key_route_config_outbox").
		WithArgs(int64(3), "old-worker", now).
		WillReturnResult(sqlmock.NewResult(0, 0))
	repo := NewAPIKeyRouteConfigOutboxRepository(db)
	err = repo.MarkAPIKeyRouteConfigEventDelivered(context.Background(), 3, "old-worker", now)
	require.ErrorContains(t, err, "no longer owned")
}
