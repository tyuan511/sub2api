package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RoutingArtifactManager keeps PostgreSQL authoritative while publishing only
// complete immutable objects and atomically changing Redis pointers. If Redis
// publication fails after a DB transition, gateways remain on the previous
// pointer and a retry reconciles the same version without another transition.
type RoutingArtifactManager struct {
	repo  RoutingOptimizationRepository
	cache RoutingArtifactCache
	now   func() time.Time
}

func NewRoutingArtifactManager(repo RoutingOptimizationRepository, cache RoutingArtifactCache) *RoutingArtifactManager {
	return &RoutingArtifactManager{repo: repo, cache: cache, now: time.Now}
}

func (m *RoutingArtifactManager) CreateArtifact(ctx context.Context, artifact *RoutingArtifactVersion) error {
	if m == nil || m.repo == nil {
		return ErrRoutingArtifactUnavailable
	}
	// All producers, including offline calibration, may only create immutable
	// drafts. Shadow/canary/active are separate audited governance transitions.
	if artifact == nil || artifact.Status != RoutingLifecycleDraft {
		return fmt.Errorf("%w: new routing artifacts must start as draft", ErrRoutingControlLoopBoundary)
	}
	return m.repo.CreateArtifact(ctx, artifact)
}

func (m *RoutingArtifactManager) ListArtifacts(ctx context.Context, kind, status string, limit int) ([]*RoutingArtifactVersion, error) {
	if m == nil || m.repo == nil {
		return nil, ErrRoutingArtifactUnavailable
	}
	return m.repo.ListArtifacts(ctx, kind, status, limit)
}

func (m *RoutingArtifactManager) CreateExperiment(ctx context.Context, experiment *RoutingExperiment) error {
	if m == nil || m.repo == nil {
		return ErrRoutingArtifactUnavailable
	}
	if err := ValidateRoutingExperiment(experiment); err != nil {
		return err
	}
	if len(experiment.OfflineReplay) == 0 {
		experiment.OfflineReplay = []byte(`{}`)
	}
	if len(experiment.LastEvaluation) == 0 {
		experiment.LastEvaluation = []byte(`{}`)
	}
	baseline, err := m.repo.GetArtifact(ctx, RoutingArtifactStrategy, experiment.BaselineStrategyVersion)
	if err != nil {
		return err
	}
	candidate, err := m.repo.GetArtifact(ctx, RoutingArtifactStrategy, experiment.CandidateStrategyVersion)
	if err != nil {
		return err
	}
	experimentScope := RoutingArtifactScope{
		ArtifactKind: RoutingArtifactStrategy, Platform: experiment.Platform, ModelFamily: experiment.ModelFamily,
		EndpointKind: experiment.EndpointKind, Preference: &experiment.Preference,
	}
	if experiment.Status != RoutingLifecycleDraft ||
		!routingArtifactScopeEqual(experimentScope, RoutingArtifactScopeFromVersion(baseline)) ||
		!routingArtifactScopeEqual(experimentScope, RoutingArtifactScopeFromVersion(candidate)) ||
		baseline.Status != RoutingLifecycleActive || candidate.Status != RoutingLifecycleShadow {
		return fmt.Errorf("%w: experiment versions or lifecycle do not match its scope", ErrRoutingArtifactInvalid)
	}
	return m.repo.CreateExperiment(ctx, experiment)
}

func (m *RoutingArtifactManager) ListExperiments(ctx context.Context, status string, limit int) ([]*RoutingExperiment, error) {
	if m == nil || m.repo == nil {
		return nil, ErrRoutingArtifactUnavailable
	}
	return m.repo.ListExperiments(ctx, status, limit)
}

func (m *RoutingArtifactManager) GetExperiment(ctx context.Context, experimentKey string) (*RoutingExperiment, error) {
	if m == nil || m.repo == nil {
		return nil, ErrRoutingArtifactUnavailable
	}
	return m.repo.GetExperiment(ctx, experimentKey)
}

func (m *RoutingArtifactManager) PauseExperiment(ctx context.Context, experimentKey, reason string) (*RoutingExperiment, error) {
	if m == nil || m.repo == nil || m.cache == nil {
		return nil, ErrRoutingArtifactUnavailable
	}
	experiment, err := m.repo.GetExperiment(ctx, experimentKey)
	if err != nil {
		return nil, err
	}
	if reason == "" {
		reason = "manual_experiment_pause"
	}
	scope := RoutingArtifactScope{ArtifactKind: RoutingArtifactStrategy, Platform: experiment.Platform,
		ModelFamily: experiment.ModelFamily, EndpointKind: experiment.EndpointKind, Preference: &experiment.Preference}
	if experiment.Status == RoutingLifecycleCanary || experiment.Status == RoutingLifecycleActive {
		if _, err := m.RollbackToBaselineWithReason(ctx, scope, reason); err != nil {
			return nil, err
		}
		return m.repo.GetExperiment(ctx, experimentKey)
	}
	if experiment.Status != RoutingLifecycleDraft && experiment.Status != RoutingLifecycleShadow && experiment.Status != RoutingLifecyclePaused {
		return nil, ErrRoutingLifecycleConflict
	}
	candidate, err := m.repo.GetArtifact(ctx, RoutingArtifactStrategy, experiment.CandidateStrategyVersion)
	if err != nil {
		return nil, err
	}
	if candidate.Status == RoutingLifecycleShadow {
		if err := m.repo.TransitionArtifact(ctx, candidate.ID, candidate.Status, RoutingLifecyclePaused, m.now()); err != nil {
			return nil, err
		}
	}
	if experiment.Status != RoutingLifecyclePaused {
		stopReason := reason
		if err := m.repo.TransitionExperiment(ctx, experiment.ID, experiment.Status, RoutingLifecyclePaused, nil, &stopReason, m.now()); err != nil {
			return nil, err
		}
	}
	pointers, pointerErr := m.cache.LoadPointers(ctx, scope)
	if pointerErr == nil && pointers.ShadowVersion == candidate.Version {
		next := pointers
		next.ShadowVersion = ""
		next.UpdatedAt = m.now().UTC()
		if err := m.cache.SwapPointers(ctx, scope, next, &pointers.ActiveVersion); err != nil {
			return nil, err
		}
	}
	return m.repo.GetExperiment(ctx, experimentKey)
}

func (m *RoutingArtifactManager) ResumeExperimentShadow(ctx context.Context, experimentKey string) (*RoutingExperiment, error) {
	if m == nil || m.repo == nil || m.cache == nil {
		return nil, ErrRoutingArtifactUnavailable
	}
	experiment, err := m.repo.GetExperiment(ctx, experimentKey)
	if err != nil {
		return nil, err
	}
	if experiment.Status != RoutingLifecyclePaused {
		return nil, ErrRoutingLifecycleConflict
	}
	var replay RoutingOfflineReplayReport
	if len(experiment.OfflineReplay) == 0 || json.Unmarshal(experiment.OfflineReplay, &replay) != nil || !replay.Passed ||
		replay.CandidateStrategyVersion != experiment.CandidateStrategyVersion {
		return nil, fmt.Errorf("%w: passing offline replay required before resume", ErrRoutingPromotionEvidence)
	}
	candidate, err := m.repo.GetArtifact(ctx, RoutingArtifactStrategy, experiment.CandidateStrategyVersion)
	if err != nil {
		return nil, err
	}
	if candidate.Status != RoutingLifecyclePaused && candidate.Status != RoutingLifecycleShadow {
		return nil, ErrRoutingLifecycleConflict
	}
	if _, err := m.Promote(ctx, RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactStrategy, Version: candidate.Version, TargetStatus: RoutingLifecycleShadow,
		BaselineVersion: experiment.BaselineStrategyVersion,
	}); err != nil {
		return nil, err
	}
	if err := m.repo.TransitionExperiment(ctx, experiment.ID, RoutingLifecyclePaused, RoutingLifecycleShadow, nil, nil, m.now()); err != nil {
		return nil, err
	}
	return m.repo.GetExperiment(ctx, experimentKey)
}

func (m *RoutingArtifactManager) LoadPointers(ctx context.Context, scope RoutingArtifactScope) (RoutingArtifactPointers, error) {
	if m == nil || m.cache == nil {
		return RoutingArtifactPointers{}, ErrRoutingArtifactUnavailable
	}
	return m.cache.LoadPointers(ctx, scope)
}

type RoutingArtifactPromotion struct {
	ArtifactKind             string
	Version                  string
	TargetStatus             string
	BaselineVersion          string
	CanaryAllocationBPS      int
	CanaryExperimentID       string
	CanaryBucketSaltChecksum string
	ApprovedBy               *int64
}

func (m *RoutingArtifactManager) Promote(ctx context.Context, input RoutingArtifactPromotion) (RoutingArtifactPointers, error) {
	if m == nil || m.repo == nil || m.cache == nil ||
		!oneOf(input.TargetStatus, RoutingLifecycleShadow, RoutingLifecycleCanary, RoutingLifecycleActive) {
		return RoutingArtifactPointers{}, ErrRoutingArtifactInvalid
	}
	candidate, err := m.repo.GetArtifact(ctx, input.ArtifactKind, input.Version)
	if err != nil {
		return RoutingArtifactPointers{}, err
	}
	scope := RoutingArtifactScopeFromVersion(candidate)
	pointers, pointerErr := m.cache.LoadPointers(ctx, scope)
	if pointerErr != nil && !errors.Is(pointerErr, ErrRoutingArtifactUnavailable) {
		return RoutingArtifactPointers{}, pointerErr
	}
	pointersMissing := pointerErr != nil
	redisExpectedActive := pointers.ActiveVersion
	if pointerErr != nil {
		pointers = RoutingArtifactPointers{BaselineVersion: input.BaselineVersion}
		if pointers.BaselineVersion == "" && candidate.Status == RoutingLifecycleActive {
			pointers.BaselineVersion = candidate.Version
		}
	}
	if pointers.BaselineVersion == "" {
		return RoutingArtifactPointers{}, fmt.Errorf("%w: baseline version required", ErrRoutingArtifactInvalid)
	}
	baseline, err := m.repo.GetArtifact(ctx, input.ArtifactKind, pointers.BaselineVersion)
	if err != nil {
		return RoutingArtifactPointers{}, err
	}
	if !routingArtifactScopeEqual(scope, RoutingArtifactScopeFromVersion(baseline)) {
		return RoutingArtifactPointers{}, ErrRoutingArtifactInvalid
	}
	var experiment *RoutingExperiment
	isStrategy := scope.ArtifactKind == RoutingArtifactStrategy
	if isStrategy && input.TargetStatus == RoutingLifecycleActive && pointers.CanaryExperimentID != "" {
		experiment, err = m.repo.GetExperiment(ctx, pointers.CanaryExperimentID)
		if err != nil {
			return RoutingArtifactPointers{}, err
		}
	}
	if input.TargetStatus == RoutingLifecycleActive && candidate.Status != RoutingLifecycleActive {
		if input.ApprovedBy == nil || *input.ApprovedBy <= 0 {
			return RoutingArtifactPointers{}, fmt.Errorf("%w: active promotion requires an approved canary experiment", ErrRoutingPromotionEvidence)
		}
		if isStrategy {
			if experiment == nil || experiment.Status != RoutingLifecycleCanary {
				return RoutingArtifactPointers{}, fmt.Errorf("%w: active promotion requires a canary experiment", ErrRoutingPromotionEvidence)
			}
			evaluation, evidenceErr := m.loadCanaryPromotionEvaluation(ctx, experiment)
			if evidenceErr != nil {
				return RoutingArtifactPointers{}, evidenceErr
			}
			if !evaluation.Ready || !evaluation.PromotionEligible {
				return RoutingArtifactPointers{}, fmt.Errorf("%w: ready=%t eligible=%t violations=%v", ErrRoutingPromotionEvidence, evaluation.Ready, evaluation.PromotionEligible, evaluation.Violations)
			}
			evidence, marshalErr := json.Marshal(evaluation)
			if marshalErr != nil {
				return RoutingArtifactPointers{}, fmt.Errorf("marshal routing promotion evidence: %w", marshalErr)
			}
			if err := m.repo.UpdateExperimentEvidence(ctx, experiment.ID, experiment.Status, nil, evidence, m.now()); err != nil {
				return RoutingArtifactPointers{}, err
			}
		} else if candidate.Status != RoutingLifecycleCanary || pointers.CanaryVersion != candidate.Version || pointers.CanaryExperimentID == "" {
			return RoutingArtifactPointers{}, fmt.Errorf("%w: learning artifact must complete an audited canary before active", ErrRoutingPromotionEvidence)
		}
	}
	if input.TargetStatus == RoutingLifecycleCanary {
		if isStrategy {
			if input.CanaryExperimentID == "" {
				return RoutingArtifactPointers{}, fmt.Errorf("%w: canary experiment is required", ErrRoutingArtifactInvalid)
			}
			experiment, err = m.repo.GetExperiment(ctx, input.CanaryExperimentID)
			if err != nil {
				return RoutingArtifactPointers{}, err
			}
			experimentScope := RoutingArtifactScope{
				ArtifactKind: RoutingArtifactStrategy, Platform: experiment.Platform, ModelFamily: experiment.ModelFamily,
				EndpointKind: experiment.EndpointKind, Preference: &experiment.Preference,
			}
			if experiment.CandidateStrategyVersion != candidate.Version || experiment.BaselineStrategyVersion != baseline.Version ||
				!routingArtifactScopeEqual(scope, experimentScope) ||
				experiment.Status != RoutingLifecycleShadow ||
				(input.CanaryAllocationBPS > 0 && input.CanaryAllocationBPS != experiment.AllocationBPS) ||
				(input.CanaryBucketSaltChecksum != "" && input.CanaryBucketSaltChecksum != experiment.BucketSaltChecksum) {
				return RoutingArtifactPointers{}, fmt.Errorf("%w: canary experiment does not match promotion", ErrRoutingArtifactInvalid)
			}
			input.CanaryAllocationBPS = experiment.AllocationBPS
			input.CanaryBucketSaltChecksum = experiment.BucketSaltChecksum
		} else {
			governance, lookupErr := m.repo.GetExperiment(ctx, input.CanaryExperimentID)
			if lookupErr != nil {
				return RoutingArtifactPointers{}, lookupErr
			}
			governanceScope := RoutingArtifactScope{
				ArtifactKind: RoutingArtifactStrategy, Platform: governance.Platform, ModelFamily: governance.ModelFamily,
				EndpointKind: governance.EndpointKind, Preference: &governance.Preference,
			}
			checksum, checksumErr := hex.DecodeString(governance.BucketSaltChecksum)
			if candidate.Status != RoutingLifecycleShadow ||
				(governance.Status != RoutingLifecycleShadow && governance.Status != RoutingLifecycleCanary) ||
				!routingArtifactScopeEqual(RoutingArtifactScope{
					ArtifactKind: RoutingArtifactStrategy, Platform: scope.Platform, ModelFamily: scope.ModelFamily,
					EndpointKind: scope.EndpointKind, Preference: scope.Preference,
				}, governanceScope) ||
				(input.CanaryAllocationBPS > 0 && input.CanaryAllocationBPS != governance.AllocationBPS) ||
				(input.CanaryBucketSaltChecksum != "" && input.CanaryBucketSaltChecksum != governance.BucketSaltChecksum) ||
				governance.AllocationBPS < 1 || governance.AllocationBPS > 10000 || checksumErr != nil || len(checksum) != 32 {
				return RoutingArtifactPointers{}, fmt.Errorf("%w: learning canary must bind an existing compatible strategy experiment", ErrRoutingArtifactInvalid)
			}
			input.CanaryAllocationBPS = governance.AllocationBPS
			input.CanaryBucketSaltChecksum = governance.BucketSaltChecksum
		}
	}
	if pointers.ActiveVersion == "" {
		switch {
		case candidate.Status == RoutingLifecycleActive:
			pointers.ActiveVersion = candidate.Version
		case baseline.Status == RoutingLifecycleActive:
			pointers.ActiveVersion = baseline.Version
		default:
			return RoutingArtifactPointers{}, fmt.Errorf("%w: active version unavailable", ErrRoutingArtifactInvalid)
		}
	}
	if err := m.cache.PublishArtifact(ctx, baseline); err != nil {
		return RoutingArtifactPointers{}, err
	}

	databaseExpectedActive := pointers.ActiveVersion
	if candidate.Status != input.TargetStatus {
		if err := m.repo.PromoteArtifact(ctx, candidate.ID, candidate.Status, input.TargetStatus, &databaseExpectedActive, m.now()); err != nil {
			return RoutingArtifactPointers{}, err
		}
		candidate, err = m.repo.GetArtifact(ctx, input.ArtifactKind, input.Version)
		if err != nil {
			return RoutingArtifactPointers{}, err
		}
	}
	if experiment != nil && experiment.Status != input.TargetStatus {
		if err := m.repo.TransitionExperiment(ctx, experiment.ID, experiment.Status, input.TargetStatus, input.ApprovedBy, nil, m.now()); err != nil {
			return RoutingArtifactPointers{}, err
		}
		experiment.Status = input.TargetStatus
	}
	if err := m.cache.PublishArtifact(ctx, candidate); err != nil {
		return RoutingArtifactPointers{}, err
	}

	next := pointers
	next.BaselineVersion = baseline.Version
	switch input.TargetStatus {
	case RoutingLifecycleCanary:
		if next.ActiveVersion == "" {
			return RoutingArtifactPointers{}, fmt.Errorf("%w: canary requires an active baseline", ErrRoutingArtifactInvalid)
		}
		next.CanaryVersion = candidate.Version
		if input.CanaryAllocationBPS > 0 || input.CanaryExperimentID != "" || input.CanaryBucketSaltChecksum != "" {
			next.CanaryAllocationBPS = input.CanaryAllocationBPS
			next.CanaryExperimentID = input.CanaryExperimentID
			next.CanaryBucketSaltChecksum = input.CanaryBucketSaltChecksum
		}
	case RoutingLifecycleActive:
		next.ActiveVersion = candidate.Version
		next.CanaryVersion = ""
		next.CanaryAllocationBPS = 0
		next.CanaryExperimentID = ""
		next.CanaryBucketSaltChecksum = ""
		if next.ShadowVersion == candidate.Version {
			next.ShadowVersion = ""
		}
	case RoutingLifecycleShadow:
		// Shadow is an observable comparison pointer, never an execution
		// pointer. Publishing it lets every gateway compute the same
		// side-effect-free alternate ordering.
		next.ShadowVersion = candidate.Version
		if !pointersMissing && next == pointers {
			return next, nil
		}
	}
	next.UpdatedAt = m.now().UTC()
	if err := m.cache.SwapPointers(ctx, scope, next, &redisExpectedActive); err != nil {
		return RoutingArtifactPointers{}, err
	}
	if scope.ArtifactKind == RoutingArtifactStrategy {
		_ = RefreshDefaultAPIKeyRoutingStrategy(ctx, scope)
	} else if scope.ArtifactKind == RoutingArtifactFeature || scope.ArtifactKind == RoutingArtifactModel {
		_ = RefreshDefaultAPIKeyRoutingLearning(ctx, scope)
	}
	return next, nil
}

func (m *RoutingArtifactManager) loadCanaryPromotionEvaluation(ctx context.Context, experiment *RoutingExperiment) (RoutingCanaryEvaluation, error) {
	if experiment == nil || experiment.Status != RoutingLifecycleCanary {
		return RoutingCanaryEvaluation{}, ErrRoutingLifecycleConflict
	}
	since := experiment.CreatedAt
	if experiment.StartedAt != nil {
		since = *experiment.StartedAt
	}
	baseline, err := m.repo.LoadCanaryMetrics(ctx, experiment.ExperimentKey, experiment.BaselineStrategyVersion, since)
	if err != nil {
		return RoutingCanaryEvaluation{}, err
	}
	candidate, err := m.repo.LoadCanaryMetrics(ctx, experiment.ExperimentKey, experiment.CandidateStrategyVersion, since)
	if err != nil {
		return RoutingCanaryEvaluation{}, err
	}
	controlBPS := 10000 - experiment.AllocationBPS
	if controlBPS > 0 {
		candidate.ExpectedDecisions = baseline.Decisions * int64(experiment.AllocationBPS) / int64(controlBPS)
	}
	guardrails, err := ParseRoutingCanaryGuardrails(experiment.Guardrails)
	if err != nil {
		return RoutingCanaryEvaluation{}, err
	}
	return EvaluateRoutingCanaryForPreference(guardrails, experiment.Preference, baseline, candidate), nil
}

func (m *RoutingArtifactManager) RollbackToBaseline(ctx context.Context, scope RoutingArtifactScope) (RoutingArtifactPointers, error) {
	return m.RollbackToBaselineWithReason(ctx, scope, "manual_baseline_rollback")
}

func (m *RoutingArtifactManager) RollbackToBaselineWithReason(ctx context.Context, scope RoutingArtifactScope, reason string) (RoutingArtifactPointers, error) {
	if m == nil || m.repo == nil || m.cache == nil {
		return RoutingArtifactPointers{}, ErrRoutingArtifactUnavailable
	}
	pointers, err := m.cache.LoadPointers(ctx, scope)
	if err != nil {
		return RoutingArtifactPointers{}, err
	}
	var experiment *RoutingExperiment
	if pointers.CanaryExperimentID != "" {
		experiment, err = m.repo.GetExperiment(ctx, pointers.CanaryExperimentID)
		if err != nil && !errors.Is(err, ErrRoutingArtifactNotFound) {
			return RoutingArtifactPointers{}, err
		}
	}
	if experiment == nil {
		experiments, listErr := m.repo.ListExperiments(ctx, RoutingLifecycleActive, 500)
		if listErr != nil {
			return RoutingArtifactPointers{}, listErr
		}
		for _, item := range experiments {
			if item != nil && item.CandidateStrategyVersion == pointers.ActiveVersion &&
				routingArtifactScopeEqual(scope, RoutingArtifactScope{
					ArtifactKind: RoutingArtifactStrategy, Platform: item.Platform, ModelFamily: item.ModelFamily,
					EndpointKind: item.EndpointKind, Preference: &item.Preference,
				}) {
				experiment = item
				break
			}
		}
	}
	baseline, err := m.repo.GetArtifact(ctx, scope.ArtifactKind, pointers.BaselineVersion)
	if err != nil {
		return RoutingArtifactPointers{}, err
	}
	if baseline.Status != RoutingLifecycleActive {
		if baseline.Status != RoutingLifecyclePaused {
			return RoutingArtifactPointers{}, ErrRoutingLifecycleConflict
		}
		if err := m.repo.PromoteArtifact(ctx, baseline.ID, baseline.Status, RoutingLifecycleActive, &pointers.ActiveVersion, m.now()); err != nil {
			return RoutingArtifactPointers{}, err
		}
		baseline, err = m.repo.GetArtifact(ctx, scope.ArtifactKind, pointers.BaselineVersion)
		if err != nil {
			return RoutingArtifactPointers{}, err
		}
	}
	if pointers.CanaryVersion != "" {
		canary, canaryErr := m.repo.GetArtifact(ctx, scope.ArtifactKind, pointers.CanaryVersion)
		if canaryErr != nil {
			return RoutingArtifactPointers{}, canaryErr
		}
		if canary.Status == RoutingLifecycleCanary {
			if err := m.repo.TransitionArtifact(ctx, canary.ID, RoutingLifecycleCanary, RoutingLifecyclePaused, m.now()); err != nil {
				return RoutingArtifactPointers{}, err
			}
		}
	}
	if err := m.cache.PublishArtifact(ctx, baseline); err != nil {
		return RoutingArtifactPointers{}, err
	}
	if experiment != nil && experiment.Status != RoutingLifecyclePaused && experiment.Status != RoutingLifecycleRetired {
		stopReason := reason
		if err := m.repo.TransitionExperiment(ctx, experiment.ID, experiment.Status, RoutingLifecyclePaused, nil, &stopReason, m.now()); err != nil {
			return RoutingArtifactPointers{}, err
		}
	}
	next := RoutingArtifactPointers{
		BaselineVersion: pointers.BaselineVersion, ActiveVersion: pointers.BaselineVersion, UpdatedAt: m.now().UTC(),
	}
	if err := m.cache.SwapPointers(ctx, scope, next, &pointers.ActiveVersion); err != nil {
		return RoutingArtifactPointers{}, err
	}
	if scope.ArtifactKind == RoutingArtifactStrategy {
		_ = RefreshDefaultAPIKeyRoutingStrategy(ctx, scope)
	} else if scope.ArtifactKind == RoutingArtifactFeature || scope.ArtifactKind == RoutingArtifactModel {
		_ = RefreshDefaultAPIKeyRoutingLearning(ctx, scope)
	}
	return next, nil
}

func routingArtifactScopeEqual(a, b RoutingArtifactScope) bool {
	preferenceA, preferenceB := "", ""
	if a.Preference != nil {
		preferenceA = *a.Preference
	}
	if b.Preference != nil {
		preferenceB = *b.Preference
	}
	return a.ArtifactKind == b.ArtifactKind && a.Platform == b.Platform && a.ModelFamily == b.ModelFamily &&
		a.EndpointKind == b.EndpointKind && preferenceA == preferenceB
}
