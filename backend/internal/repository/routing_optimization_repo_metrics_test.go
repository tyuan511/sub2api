package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestRoutingOptimizationRepositoryLoadsDecisionLevelCanaryMetrics(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	since := time.Now().Add(-2 * time.Hour).UTC()
	mock.ExpectQuery(`(?s)WITH attempt_features AS.*decision_rollup AS.*FROM decision_rollup`).
		WithArgs("experiment-1", "candidate-v2", since).
		WillReturnRows(sqlmock.NewRows([]string{
			"decisions", "finals", "errors", "billing", "latency", "orphan_finals",
			"coverage", "billing_coverage", "latency_coverage", "success", "failure_risk", "p95", "p95_ttft", "p99", "p99_ttft", "cost",
			"expected_time", "expected_ttft", "supplier_cost", "switch", "average_switches", "sticky_break", "cold", "stability_loss",
			"calibration_error", "missing_feature_rate", "feature_schema_versions", "feature_schema_version",
		}).AddRow(int64(2000), int64(1998), int64(20), int64(1960), int64(1970), int64(0),
			0.999, 1.0, 1.0, 0.98, 0.021, 1200.0, 220.0, 2100.0, 400.0, 1.25, 850.0, 180.0, .7,
			0.03, .04, .005, 0.01, .018, .042, .01, int64(1), "routing-features-v1"))
	mock.ExpectQuery(`(?s)WITH per_decision AS.*FROM per_key`).
		WithArgs("experiment-1", "candidate-v2", since).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_id", "decisions", "finals", "successes"}).
			AddRow(int64(42), int64(400), int64(400), int64(396)))
	repo := &routingOptimizationRepository{db: db}

	metrics, err := repo.LoadCanaryMetrics(context.Background(), "experiment-1", "candidate-v2", since)
	require.NoError(t, err)
	require.EqualValues(t, 2000, metrics.Decisions)
	require.Equal(t, 0.999, metrics.EventCoverage)
	require.Equal(t, 0.98, metrics.FinalSuccessRate)
	require.Equal(t, 1200.0, metrics.P95LatencyMS)
	require.Equal(t, 220.0, metrics.P95TTFTMS)
	require.Equal(t, 2100.0, metrics.P99LatencyMS)
	require.Equal(t, 400.0, metrics.P99TTFTMS)
	require.Equal(t, 1.25, metrics.CostPerSuccess)
	require.Equal(t, 1.25, metrics.ExpectedSuccessfulCost)
	require.Equal(t, 850.0, metrics.ExpectedTimeToSuccessMS)
	require.Equal(t, 180.0, metrics.ExpectedTTFTToSuccessMS)
	require.Equal(t, .021, metrics.FailureRisk)
	require.Equal(t, .005, metrics.StickyBreakRate)
	require.Equal(t, .018, metrics.StabilityLoss)
	require.Equal(t, .042, metrics.PredictionCalibrationError)
	require.Equal(t, .01, metrics.MissingFeatureRate)
	require.EqualValues(t, 1, metrics.FeatureSchemaVersionCount)
	require.Equal(t, "routing-features-v1", metrics.FeatureSchemaVersion)
	require.False(t, metrics.FeatureDriftDetected)
	require.Equal(t, "candidate-v2", metrics.StrategyVersion)
	require.Equal(t, "routing-score-loss-map-v1", metrics.ScoreLossMappingVersion)
	require.True(t, metrics.CriticalSlicesHealthy)
	require.Len(t, metrics.CriticalSlices, 1)
	require.EqualValues(t, 42, metrics.CriticalSlices[0].APIKeyID)
	require.Greater(t, metrics.SuccessRateLowerBound, 0.97)
	require.Greater(t, metrics.ObservationDuration, time.Hour)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRoutingOptimizationRepositoryPrunesFactsInBoundedBatches(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	sampleBefore := time.Now().Add(-30 * 24 * time.Hour).UTC()
	diagnosticBefore := time.Now().Add(-90 * 24 * time.Hour).UTC()
	criticalBefore := time.Now().Add(-180 * 24 * time.Hour).UTC()
	mock.ExpectExec(`(?s)WITH expired AS.*DELETE FROM routing_attempts`).
		WithArgs(sampleBefore, diagnosticBefore, criticalBefore, 5000).
		WillReturnResult(sqlmock.NewResult(0, 321))
	repo := &routingOptimizationRepository{db: db}

	deleted, err := repo.PruneRoutingAttempts(context.Background(), sampleBefore, diagnosticBefore, criticalBefore, 5000)
	require.NoError(t, err)
	require.EqualValues(t, 321, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}
