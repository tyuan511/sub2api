package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRoutingCanaryMonitorAutomaticallyRollsBackViolatingExperiment(t *testing.T) {
	baseline := routingArtifactForManagerTest(1, "baseline-v1", RoutingLifecycleActive)
	candidate := routingArtifactForManagerTest(2, "canary-v2", RoutingLifecycleCanary)
	experiment := routingExperimentForManagerTest(baseline, candidate)
	experiment.Status = RoutingLifecycleCanary
	now := time.Now().Add(-2 * time.Hour)
	experiment.StartedAt = &now
	healthy := RoutingCanaryMetrics{
		Decisions: 2000, ObservationDuration: 2 * time.Hour, EventCoverage: 1, FinalSuccessRate: 0.99,
		P95LatencyMS: 1000, CostPerSuccess: 1, CriticalSlicesHealthy: true,
	}
	unhealthy := healthy
	unhealthy.FinalSuccessRate = 0.4
	repo := &routingArtifactManagerRepo{
		artifacts:   map[string]*RoutingArtifactVersion{baseline.Version: baseline, candidate.Version: candidate},
		experiments: map[string]*RoutingExperiment{experiment.ExperimentKey: experiment},
		metrics:     map[string]RoutingCanaryMetrics{baseline.Version: healthy, candidate.Version: unhealthy},
	}
	cache := &routingArtifactManagerCache{
		objects: map[string]*RoutingArtifactVersion{
			baseline.Version: cloneRoutingArtifactForManagerTest(baseline), candidate.Version: cloneRoutingArtifactForManagerTest(candidate),
		},
		pointers: RoutingArtifactPointers{
			BaselineVersion: baseline.Version, ActiveVersion: baseline.Version, CanaryVersion: candidate.Version,
			CanaryAllocationBPS: experiment.AllocationBPS, CanaryExperimentID: experiment.ExperimentKey,
			CanaryBucketSaltChecksum: experiment.BucketSaltChecksum, UpdatedAt: time.Now(),
		},
		pointerReady: true,
	}
	monitor := NewRoutingCanaryMonitor(repo, NewRoutingArtifactManager(repo, cache))

	require.NoError(t, monitor.EvaluateOnce(context.Background()))
	require.Equal(t, baseline.Version, cache.pointers.ActiveVersion)
	require.Empty(t, cache.pointers.CanaryVersion)
	require.Equal(t, RoutingLifecyclePaused, repo.experiments[experiment.ExperimentKey].Status)
	require.EqualValues(t, 1, monitor.Stats().RolledBack)
}
