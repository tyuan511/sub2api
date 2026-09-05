package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func expectRoutingMetricRewrite(mock sqlmock.Sqlmock, start, end time.Time) {
	mock.ExpectBegin()
	for _, query := range []string{
		`DELETE FROM api_key_routing_health_metrics_1m`,
		`DELETE FROM api_key_routing_price_metrics_1m`,
		`(?s)INSERT INTO api_key_routing_health_metrics_1m.*FROM usage_logs`,
		`(?s)WITH categorized AS.*FROM routing_attempts`,
		`(?s)INSERT INTO api_key_routing_price_metrics_1m.*FROM usage_logs`,
	} {
		mock.ExpectExec(query).WithArgs(start, end).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()
}

func TestRoutingScoreRefreshIndependentBoundedWindowsAndRetention(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	source := &routingScoreObservationSource{db: db}
	now := time.Date(2026, 9, 5, 8, 20, 33, 0, time.UTC)
	end := now.Truncate(time.Minute)
	for i := 0; i < 2; i++ {
		expectRoutingMetricRewrite(mock, end.Add(-10*time.Minute), end)
		historyEnd := end.Add(-time.Duration(10+30*i) * time.Minute)
		expectRoutingMetricRewrite(mock, historyEnd.Add(-30*time.Minute), historyEnd)
		for _, table := range []string{"api_key_routing_health_metrics_1m", "api_key_routing_price_metrics_1m"} {
			mock.ExpectExec(`(?s)DELETE FROM ` + table + ` WHERE ctid IN.*LIMIT 5000`).
				WithArgs(end.Add(-7 * 24 * time.Hour)).WillReturnResult(sqlmock.NewResult(0, 0))
		}
		require.NoError(t, source.RefreshAPIKeyRoutingMetricBuckets(context.Background(), now))
		require.Equal(t, historyEnd.Add(-30*time.Minute), source.backfillEnd)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRoutingScoreRefreshRollsBackAndDoesNotAdvanceCursorOnFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	source := &routingScoreObservationSource{db: db}
	end := time.Now().UTC().Truncate(time.Minute)
	expectRoutingMetricRewrite(mock, end.Add(-10*time.Minute), end)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM api_key_routing_health_metrics_1m`).
		WithArgs(end.Add(-40*time.Minute), end.Add(-10*time.Minute)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM api_key_routing_price_metrics_1m`).
		WillReturnError(errors.New("timeout"))
	mock.ExpectRollback()
	require.ErrorContains(t, source.RefreshAPIKeyRoutingMetricBuckets(context.Background(), end), "timeout")
	require.True(t, source.backfillEnd.IsZero())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRoutingScoreBackfillClampsAndRestartsAtHistoryBoundary(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	end := time.Now().UTC().Truncate(time.Minute)
	cutoff := end.Add(-24 * time.Hour)
	source := &routingScoreObservationSource{db: db, backfillEnd: cutoff.Add(time.Minute)}
	for i := 0; i < 2; i++ {
		expectRoutingMetricRewrite(mock, end.Add(-10*time.Minute), end)
		if i == 0 {
			expectRoutingMetricRewrite(mock, cutoff, cutoff.Add(time.Minute))
		} else {
			expectRoutingMetricRewrite(mock, end.Add(-40*time.Minute), end.Add(-10*time.Minute))
		}
		for _, table := range []string{"api_key_routing_health_metrics_1m", "api_key_routing_price_metrics_1m"} {
			mock.ExpectExec(`(?s)DELETE FROM ` + table + ` WHERE ctid IN.*LIMIT 5000`).
				WithArgs(end.Add(-7 * 24 * time.Hour)).WillReturnResult(sqlmock.NewResult(0, 0))
		}
		require.NoError(t, source.RefreshAPIKeyRoutingMetricBuckets(context.Background(), end))
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRoutingScoreObservationSourceEnforcesPerQueryTimeout(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC()
	mock.ExpectQuery(`(?s)FROM api_key_routing_health_metrics_1m`).
		WithArgs(start, end).
		WillDelayFor(100 * time.Millisecond).
		WillReturnRows(sqlmock.NewRows([]string{"platform"}))

	source := &routingScoreObservationSource{db: db, queryTimeout: 10 * time.Millisecond}
	started := time.Now()
	_, err = source.LoadAPIKeyRoutingMetricAggregates(context.Background(), start, end)
	require.Error(t, err)
	require.Less(t, time.Since(started), 80*time.Millisecond)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRoutingScoreObservationSourceLoadsEndpointHealthAndMatchingActualPriceSlice(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	start := time.Now().Add(-time.Hour).UTC()
	end := time.Now().UTC()
	through := end.Add(-time.Minute)

	healthColumns := []string{
		"platform", "group_id", "model", "endpoint_kind",
		"success_requests", "failed_requests", "capacity_overflow_requests",
		"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
		"ttft_sum_ms", "ttft_count", "duration_sum_ms", "duration_count",
		"rate_multiplier", "long_context_pricing_enabled", "model_pricing", "account_pool_domain", "source_bucket_rows", "data_through",
	}
	mock.ExpectQuery(`(?s)FROM api_key_routing_health_metrics_1m`).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows(healthColumns).AddRow(
			"openai", int64(7), "gpt-5.6-sol", "responses",
			int64(80), int64(20), int64(10),
			int64(1000), int64(100), int64(20), int64(400),
			int64(8000), int64(80), int64(80000), int64(80),
			1.2, true, []byte(`[]`), "pool-digest", int64(6), through,
		))
	priceColumns := []string{
		"platform", "group_id", "model", "endpoint_kind", "service_tier", "context_bucket",
		"success_requests", "input_tokens", "image_input_tokens", "output_tokens",
		"cache_creation_tokens", "cache_creation_5m_tokens", "cache_creation_1h_tokens",
		"cache_read_tokens", "image_output_tokens", "source_bucket_rows",
	}
	mock.ExpectQuery(`(?s)FROM api_key_routing_price_metrics_1m`).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows(priceColumns).AddRow(
			"openai", int64(7), "gpt-5.6-sol", "responses", "default", 0,
			int64(80), int64(1000), int64(0), int64(100), int64(20), int64(20), int64(0), int64(400), int64(0), int64(6),
		))

	source := &routingScoreObservationSource{db: db}
	items, err := source.LoadAPIKeyRoutingMetricAggregates(context.Background(), start, end)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "responses", items[0].EndpointKind)
	require.Equal(t, int64(20), items[0].FailedRequests)
	require.Equal(t, int64(10), items[0].CapacityOverflowRequests)
	require.Equal(t, int64(1000), items[0].InputTokens)
	require.Len(t, items[0].PriceSamples, 1)
	require.Equal(t, "responses", items[0].PriceSamples[0].EndpointKind)
	require.NoError(t, mock.ExpectationsWereMet())
}
