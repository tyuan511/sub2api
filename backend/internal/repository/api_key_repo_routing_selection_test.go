package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestLoadRecentAPIKeyRoutingSelectionObservationsUsesWeightedSuccessfulDecisions(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newAPIKeyRepositoryWithSQL(nil, db)
	since := time.Unix(1_800_000_000, 0).UTC()
	through := since.Add(time.Hour)
	rows := sqlmock.NewRows([]string{
		"api_key_id", "route_version", "platform", "model_family", "endpoint_kind", "strategy_version", "smart_preference",
		"selected_group_id", "sampled_selections", "weighted_selections", "weight_squares", "data_through",
	}).AddRow(int64(7), int64(3), "openai", "gpt-5", "responses", "routing-rules-v1", "price", int64(11), int64(5), 500.0, 50_000.0, through)
	mock.ExpectQuery("WITH successful_decisions AS").
		WithArgs(pq.Array([]int64{7, 8}), since).
		WillReturnRows(rows)

	observations, err := repo.LoadRecentAPIKeyRoutingSelectionObservations(context.Background(), []int64{7, 8}, since)
	require.NoError(t, err)
	require.Len(t, observations, 1)
	require.EqualValues(t, 7, observations[0].APIKeyID)
	require.EqualValues(t, 3, observations[0].RouteVersion)
	require.Equal(t, "price", observations[0].SmartPreference)
	require.EqualValues(t, 5, observations[0].SampledSelections)
	require.InDelta(t, 500, observations[0].WeightedSelections, 1e-12)
	require.InDelta(t, 50_000, observations[0].WeightSquares, 1e-12)
	require.Equal(t, through, observations[0].DataThrough)
	require.NoError(t, mock.ExpectationsWereMet())
}
