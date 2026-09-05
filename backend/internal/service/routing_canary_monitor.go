package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type RoutingCanaryMonitorStats struct {
	Evaluated  uint64
	RolledBack uint64
	Errors     uint64
}

// RoutingCanaryMonitor is a slow control loop. It reads only persisted,
// point-in-time decision facts and can only ask the artifact manager to move
// the version pointer back to baseline; it cannot alter Key routes or sticky
// state.
type RoutingCanaryMonitor struct {
	repo       RoutingOptimizationRepository
	manager    *RoutingArtifactManager
	interval   time.Duration
	stopCh     chan struct{}
	doneCh     chan struct{}
	startOnce  sync.Once
	stopOnce   sync.Once
	started    atomic.Bool
	evaluated  atomic.Uint64
	rolledBack atomic.Uint64
	errors     atomic.Uint64
}

func NewRoutingCanaryMonitor(repo RoutingOptimizationRepository, manager *RoutingArtifactManager) *RoutingCanaryMonitor {
	return &RoutingCanaryMonitor{
		repo: repo, manager: manager, interval: 30 * time.Second,
		stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
}

func (m *RoutingCanaryMonitor) Start() {
	if m == nil || m.repo == nil || m.manager == nil {
		return
	}
	m.startOnce.Do(func() {
		m.started.Store(true)
		go m.run()
	})
}

func (m *RoutingCanaryMonitor) Stop() {
	if m == nil || !m.started.Load() {
		return
	}
	m.stopOnce.Do(func() {
		close(m.stopCh)
		select {
		case <-m.doneCh:
		case <-time.After(2 * time.Second):
		}
	})
}

func (m *RoutingCanaryMonitor) Stats() RoutingCanaryMonitorStats {
	if m == nil {
		return RoutingCanaryMonitorStats{}
	}
	return RoutingCanaryMonitorStats{Evaluated: m.evaluated.Load(), RolledBack: m.rolledBack.Load(), Errors: m.errors.Load()}
}

func (m *RoutingCanaryMonitor) EvaluateOnce(ctx context.Context) error {
	experiments, err := m.repo.ListExperiments(ctx, RoutingLifecycleCanary, 100)
	if err != nil {
		m.errors.Add(1)
		return err
	}
	for _, experiment := range experiments {
		if experiment == nil {
			continue
		}
		since := experiment.CreatedAt
		if experiment.StartedAt != nil {
			since = *experiment.StartedAt
		}
		baseline, loadErr := m.repo.LoadCanaryMetrics(ctx, experiment.ExperimentKey, experiment.BaselineStrategyVersion, since)
		if loadErr != nil {
			m.errors.Add(1)
			continue
		}
		candidate, loadErr := m.repo.LoadCanaryMetrics(ctx, experiment.ExperimentKey, experiment.CandidateStrategyVersion, since)
		if loadErr != nil {
			m.errors.Add(1)
			continue
		}
		controlBPS := 10000 - experiment.AllocationBPS
		if controlBPS > 0 {
			candidate.ExpectedDecisions = baseline.Decisions * int64(experiment.AllocationBPS) / int64(controlBPS)
		}
		evaluation, evaluateErr := m.manager.EvaluateCanaryAndRollback(ctx, experiment, baseline, candidate)
		m.evaluated.Add(1)
		if evaluateErr != nil {
			m.errors.Add(1)
			continue
		}
		if evaluation.Rollback {
			m.rolledBack.Add(1)
		}
	}
	return nil
}

func (m *RoutingCanaryMonitor) run() {
	defer close(m.doneCh)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = m.EvaluateOnce(ctx)
			cancel()
		}
	}
}
