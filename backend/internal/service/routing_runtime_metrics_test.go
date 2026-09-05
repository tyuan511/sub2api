package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRoutingRuntimeMetricsSnapshotUsesBoundedDimensions(t *testing.T) {
	metrics := &RoutingRuntimeMetrics{}
	metrics.RecordPlan(3, 2)
	metrics.RecordPlan(1000, 0)
	metrics.RecordSwitch(true)
	metrics.RecordSticky("hit")
	metrics.RecordSticky("miss")
	metrics.RecordSticky("bind")
	metrics.RecordSticky("error")
	metrics.RecordBreaker(APIKeyRouteBreakerOpen, false, false)
	metrics.RecordBreaker(APIKeyRouteBreakerHalfOpen, true, false)
	metrics.RecordBreaker(APIKeyRouteBreakerClosed, false, true)
	metrics.RecordScoreSnapshot(true, 125*time.Millisecond)
	metrics.RecordScoreSnapshot(true, 375*time.Millisecond)
	metrics.RecordScoreSnapshot(false, 0)
	for index := 0; index < 50; index++ {
		metrics.RecordPhaseLatency(RoutingLatencyPhasePlanBuild, time.Millisecond)
	}
	for index := 0; index < 45; index++ {
		metrics.RecordPhaseLatency(RoutingLatencyPhasePlanBuild, 5*time.Millisecond)
	}
	for index := 0; index < 4; index++ {
		metrics.RecordPhaseLatency(RoutingLatencyPhasePlanBuild, 10*time.Millisecond)
	}
	metrics.RecordPhaseLatency(RoutingLatencyPhasePlanBuild, 25*time.Millisecond)
	metrics.RecordPhaseLatency("unbounded-user-label", time.Hour)
	metrics.RecordBackgroundQuery(10*time.Millisecond, 12, false)
	metrics.RecordBackgroundQuery(30*time.Millisecond, 8, true)
	metrics.RecordPersonalization("", .02, 2)
	metrics.RecordPersonalization(RoutingLearningFallbackDrift, .15, 0)
	metrics.RecordModelPrediction("", 8*time.Microsecond, .03)
	metrics.RecordModelPrediction(RoutingLearningFallbackTimeout, 20*time.Microsecond, .04)

	snapshot := metrics.Snapshot()
	require.Equal(t, uint64(2), snapshot.Plans)
	require.Len(t, snapshot.CandidateCountBuckets, DefaultMaxAPIKeyGroupRoutes+1)
	require.Equal(t, uint64(1), snapshot.CandidateCountBuckets[3])
	require.Equal(t, uint64(1), snapshot.CandidateCountBuckets[DefaultMaxAPIKeyGroupRoutes])
	require.Equal(t, uint64(2), snapshot.ExcludedCandidates)
	require.Equal(t, uint64(1), snapshot.GroupSwitches)
	require.Equal(t, uint64(1), snapshot.StickyBreaks)
	require.Equal(t, uint64(1), snapshot.HalfOpenProbes)
	require.Equal(t, uint64(1), snapshot.RedisDegraded)
	require.Equal(t, uint64(2), snapshot.ScoreSnapshotHits)
	require.Equal(t, uint64(1), snapshot.ScoreSnapshotMisses)
	require.Equal(t, float64(250), snapshot.ScoreAgeAverageMS)
	require.Equal(t, uint64(375), snapshot.ScoreAgeMaxMS)
	require.Len(t, snapshot.PhaseLatency, 5)
	planLatency := snapshot.PhaseLatency[RoutingLatencyPhasePlanBuild]
	require.Equal(t, uint64(100), planLatency.Samples)
	require.Equal(t, float64(1), planLatency.P50MS)
	require.Equal(t, float64(5), planLatency.P95MS)
	require.Equal(t, float64(10), planLatency.P99MS)
	require.Equal(t, float64(25), planLatency.MaxMS)
	require.Equal(t, uint64(2), snapshot.BackgroundQueries.Queries)
	require.Equal(t, uint64(1), snapshot.BackgroundQueries.Failures)
	require.Equal(t, uint64(20), snapshot.BackgroundQueries.ScannedRows)
	require.Equal(t, float64(20), snapshot.BackgroundQueries.AverageDuration)
	require.Equal(t, float64(30), snapshot.BackgroundQueries.MaxDuration)
	require.Equal(t, uint64(2), snapshot.Personalization.Attempts)
	require.Equal(t, uint64(1), snapshot.Personalization.Applications)
	require.Equal(t, uint64(2), snapshot.Personalization.AppliedGroups)
	require.Equal(t, uint64(1), snapshot.Personalization.Fallbacks[RoutingLearningFallbackDrift])
	require.Len(t, snapshot.Personalization.Fallbacks, len(routingLearningFallbackReasons))
	require.Equal(t, .15, snapshot.Personalization.LastCalibrationError)
	require.Equal(t, uint64(2), snapshot.ModelPrediction.Attempts)
	require.Equal(t, uint64(1), snapshot.ModelPrediction.Applications)
	require.Equal(t, uint64(1), snapshot.ModelPrediction.Fallbacks[RoutingLearningFallbackTimeout])
	require.Equal(t, uint64(2), snapshot.ModelPrediction.InferenceLatency.Samples)
	require.Equal(t, .04, snapshot.ModelPrediction.LastCalibrationError)
}
