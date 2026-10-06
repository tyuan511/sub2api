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
	require.Len(t, snapshot.PhaseLatency, 4)
	planLatency := snapshot.PhaseLatency[RoutingLatencyPhasePlanBuild]
	require.Equal(t, uint64(100), planLatency.Samples)
	require.Equal(t, float64(1), planLatency.P50MS)
	require.Equal(t, float64(5), planLatency.P95MS)
	require.Equal(t, float64(10), planLatency.P99MS)
	require.Equal(t, float64(25), planLatency.MaxMS)
}
