package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const maxRoutingLearningScopes = 128

type routingLearningRuntimeEntry struct {
	activePersonalization   *APIKeyRoutingPersonalization
	canaryPersonalization   *APIKeyRoutingPersonalization
	shadowPersonalization   *APIKeyRoutingPersonalization
	personalizationPointers RoutingArtifactPointers
	activeModel             *APIKeyRoutingPredictionModel
	canaryModel             *APIKeyRoutingPredictionModel
	shadowModel             *APIKeyRoutingPredictionModel
	modelPointers           RoutingArtifactPointers
}

type routingLearningRuntimeCatalog struct {
	entries map[string]routingLearningRuntimeEntry
}

type RoutingLearningApplication struct {
	Snapshot           *APIKeyRoutingScoreSnapshot
	ShadowSnapshot     *APIKeyRoutingScoreSnapshot
	Personalization    APIKeyRoutingPersonalizationResult
	Prediction         APIKeyRoutingPredictionResult
	CanaryExperimentID *string
	CanaryBucket       *int
}

// RoutingLearningRuntime owns the only online representation of residual and
// model artifacts. Refreshes may read Redis in the background; Apply performs
// only bounded local lookups and arithmetic on at most eight candidates.
type RoutingLearningRuntime struct {
	cache                  RoutingArtifactCache
	personalizationEnabled bool
	modelEnabled           bool
	refreshInterval        time.Duration
	current                atomic.Pointer[routingLearningRuntimeCatalog]
	observedMu             sync.Mutex
	observed               map[string]RoutingArtifactScope
	updateMu               sync.Mutex
	refreshCh              chan RoutingArtifactScope
	stopCh                 chan struct{}
	doneCh                 chan struct{}
	startOnce              sync.Once
	stopOnce               sync.Once
	started                atomic.Bool
}

func NewRoutingLearningRuntime(cache RoutingArtifactCache, personalizationEnabled, modelEnabled bool) *RoutingLearningRuntime {
	runtime := &RoutingLearningRuntime{
		cache: cache, personalizationEnabled: personalizationEnabled, modelEnabled: modelEnabled,
		refreshInterval: 30 * time.Second, observed: make(map[string]RoutingArtifactScope),
		refreshCh: make(chan RoutingArtifactScope, maxRoutingLearningScopes), stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
	runtime.current.Store(&routingLearningRuntimeCatalog{entries: make(map[string]routingLearningRuntimeEntry)})
	return runtime
}

func (r *RoutingLearningRuntime) Enabled() bool {
	return r != nil && (r.personalizationEnabled || r.modelEnabled)
}

func (r *RoutingLearningRuntime) Start() {
	if r == nil || !r.Enabled() || r.cache == nil {
		return
	}
	r.startOnce.Do(func() {
		r.started.Store(true)
		go r.run()
	})
}

func (r *RoutingLearningRuntime) Stop() {
	if r == nil || !r.started.Load() {
		return
	}
	r.stopOnce.Do(func() {
		close(r.stopCh)
		select {
		case <-r.doneCh:
		case <-time.After(2 * time.Second):
		}
	})
}

func (r *RoutingLearningRuntime) Apply(scope RoutingArtifactScope, apiKeyID, userID int64, strategyExperimentID *string, baseline *APIKeyRoutingScoreSnapshot, eligible map[int64]bool, now time.Time) RoutingLearningApplication {
	application := RoutingLearningApplication{Snapshot: baseline}
	if r == nil || !r.Enabled() || baseline == nil || scope.Validate() != nil {
		return application
	}
	r.observe(scope)
	catalog := r.current.Load()
	entry, found := routingLearningRuntimeEntry{}, false
	if catalog != nil {
		entry, found = catalog.entries[routingLearningScopeKey(scope)]
	}
	actual := baseline
	if r.personalizationEnabled {
		var personalization *APIKeyRoutingPersonalization
		if found {
			personalization, application.CanaryExperimentID, application.CanaryBucket = selectRoutingLearningCanary(
				entry.activePersonalization, entry.canaryPersonalization, entry.personalizationPointers, userID, apiKeyID, strategyExperimentID,
			)
		}
		var result APIKeyRoutingPersonalizationResult
		actual, result = ApplyAPIKeyRoutingPersonalization(actual, personalization, apiKeyID, userID, eligible, now)
		application.Personalization = result
		DefaultRoutingRuntimeMetrics().RecordPersonalization(result.Reason, result.Calibration, len(result.AppliedGroups))
	}
	if r.modelEnabled {
		var model *APIKeyRoutingPredictionModel
		if found {
			var experimentID *string
			var bucket *int
			model, experimentID, bucket = selectRoutingLearningCanary(entry.activeModel, entry.canaryModel, entry.modelPointers, userID, apiKeyID, strategyExperimentID)
			if application.CanaryExperimentID != nil && experimentID != nil && *application.CanaryExperimentID != *experimentID {
				// Never combine independently canaried learning objects in one
				// request. Keep the already selected personalization canary and
				// use the model's active version as the compatible fallback.
				model, experimentID, bucket = entry.activeModel, nil, nil
			}
			if application.CanaryExperimentID == nil {
				application.CanaryExperimentID, application.CanaryBucket = experimentID, bucket
			}
		}
		var result APIKeyRoutingPredictionResult
		actual, result = ApplyAPIKeyRoutingPredictionModel(actual, model, eligible, now)
		application.Prediction = result
		DefaultRoutingRuntimeMetrics().RecordModelPrediction(result.Reason, result.Duration, result.Calibration)
	}
	application.Snapshot = actual

	// Shadow objects are evaluated from the same frozen deterministic baseline
	// and never feed the actual candidate order, capacity, sticky, or billing path.
	shadow := baseline
	shadowApplied := false
	if found && r.personalizationEnabled && entry.shadowPersonalization != nil {
		var result APIKeyRoutingPersonalizationResult
		shadow, result = ApplyAPIKeyRoutingPersonalization(shadow, entry.shadowPersonalization, apiKeyID, userID, eligible, now)
		DefaultRoutingRuntimeMetrics().RecordPersonalization(result.Reason, result.Calibration, len(result.AppliedGroups))
		shadowApplied = result.Reason == ""
	}
	if found && r.modelEnabled && entry.shadowModel != nil {
		var result APIKeyRoutingPredictionResult
		shadow, result = ApplyAPIKeyRoutingPredictionModel(shadow, entry.shadowModel, eligible, now)
		DefaultRoutingRuntimeMetrics().RecordModelPrediction(result.Reason, result.Duration, result.Calibration)
		shadowApplied = shadowApplied || result.Reason == ""
	}
	if shadowApplied {
		application.ShadowSnapshot = shadow
	}
	return application
}

func selectRoutingLearningCanary[T any](active, canary *T, pointers RoutingArtifactPointers, userID, apiKeyID int64, strategyExperimentID *string) (*T, *string, *int) {
	if canary == nil || userID <= 0 || apiKeyID <= 0 || strategyExperimentID == nil || *strategyExperimentID == "" ||
		pointers.CanaryExperimentID != *strategyExperimentID || pointers.CanaryAllocationBPS <= 0 || pointers.CanaryBucketSaltChecksum == "" {
		return active, nil, nil
	}
	bucket := StableRoutingExperimentBucket(userID, apiKeyID, []byte(pointers.CanaryBucketSaltChecksum))
	if bucket >= pointers.CanaryAllocationBPS {
		return active, nil, &bucket
	}
	return canary, optionalStringPtr(pointers.CanaryExperimentID), &bucket
}

func (r *RoutingLearningRuntime) Refresh(ctx context.Context, scope RoutingArtifactScope) error {
	if r == nil || !r.Enabled() || r.cache == nil || scope.Validate() != nil {
		return ErrRoutingArtifactUnavailable
	}
	entry := routingLearningRuntimeEntry{}
	var refreshErr error
	if r.personalizationEnabled {
		featureScope := scope
		featureScope.ArtifactKind = RoutingArtifactFeature
		active, canary, shadow, pointers, err := r.loadPersonalization(ctx, featureScope)
		if err != nil {
			refreshErr = errors.Join(refreshErr, err)
		} else {
			entry.activePersonalization, entry.canaryPersonalization, entry.shadowPersonalization = active, canary, shadow
			entry.personalizationPointers = pointers
		}
	}
	if r.modelEnabled {
		modelScope := scope
		modelScope.ArtifactKind = RoutingArtifactModel
		active, canary, shadow, pointers, err := r.loadModel(ctx, modelScope)
		if err != nil {
			refreshErr = errors.Join(refreshErr, err)
		} else {
			entry.activeModel, entry.canaryModel, entry.shadowModel = active, canary, shadow
			entry.modelPointers = pointers
		}
	}
	r.replaceEntry(scope, entry)
	return refreshErr
}

func (r *RoutingLearningRuntime) loadPersonalization(ctx context.Context, scope RoutingArtifactScope) (active, canary, shadow *APIKeyRoutingPersonalization, pointers RoutingArtifactPointers, err error) {
	artifact, pointers, err := ResolveRoutingArtifact(ctx, r.cache, scope)
	if err != nil {
		return nil, nil, nil, pointers, err
	}
	active, err = ParseAPIKeyRoutingPersonalizationArtifact(artifact)
	if err != nil && pointers.BaselineVersion != artifact.Version {
		if baseline, loadErr := r.cache.LoadArtifact(ctx, scope, pointers.BaselineVersion); loadErr == nil {
			active, err = ParseAPIKeyRoutingPersonalizationArtifact(baseline)
		}
	}
	if err != nil {
		return nil, nil, nil, pointers, err
	}
	if pointers.CanaryVersion != "" {
		if value, loadErr := r.cache.LoadArtifact(ctx, scope, pointers.CanaryVersion); loadErr == nil {
			canary, _ = ParseAPIKeyRoutingPersonalizationArtifact(value)
		}
	}
	if pointers.ShadowVersion != "" {
		if value, loadErr := r.cache.LoadArtifact(ctx, scope, pointers.ShadowVersion); loadErr == nil {
			shadow, _ = ParseAPIKeyRoutingPersonalizationArtifact(value)
		}
	}
	return active, canary, shadow, pointers, nil
}

func (r *RoutingLearningRuntime) loadModel(ctx context.Context, scope RoutingArtifactScope) (active, canary, shadow *APIKeyRoutingPredictionModel, pointers RoutingArtifactPointers, err error) {
	artifact, pointers, err := ResolveRoutingArtifact(ctx, r.cache, scope)
	if err != nil {
		return nil, nil, nil, pointers, err
	}
	active, err = ParseAPIKeyRoutingPredictionModel(artifact)
	if err != nil && pointers.BaselineVersion != artifact.Version {
		if baseline, loadErr := r.cache.LoadArtifact(ctx, scope, pointers.BaselineVersion); loadErr == nil {
			active, err = ParseAPIKeyRoutingPredictionModel(baseline)
		}
	}
	if err != nil {
		return nil, nil, nil, pointers, err
	}
	if pointers.CanaryVersion != "" {
		if value, loadErr := r.cache.LoadArtifact(ctx, scope, pointers.CanaryVersion); loadErr == nil {
			canary, _ = ParseAPIKeyRoutingPredictionModel(value)
		}
	}
	if pointers.ShadowVersion != "" {
		if value, loadErr := r.cache.LoadArtifact(ctx, scope, pointers.ShadowVersion); loadErr == nil {
			shadow, _ = ParseAPIKeyRoutingPredictionModel(value)
		}
	}
	return active, canary, shadow, pointers, nil
}

func (r *RoutingLearningRuntime) observe(scope RoutingArtifactScope) {
	key := routingLearningScopeKey(scope)
	r.observedMu.Lock()
	if _, exists := r.observed[key]; exists {
		r.observedMu.Unlock()
		return
	}
	if len(r.observed) >= maxRoutingLearningScopes {
		r.observedMu.Unlock()
		return
	}
	r.observed[key] = scope
	r.observedMu.Unlock()
	select {
	case r.refreshCh <- scope:
	default:
	}
}

func (r *RoutingLearningRuntime) replaceEntry(scope RoutingArtifactScope, entry routingLearningRuntimeEntry) {
	r.updateMu.Lock()
	defer r.updateMu.Unlock()
	current := r.current.Load()
	next := &routingLearningRuntimeCatalog{entries: make(map[string]routingLearningRuntimeEntry)}
	if current != nil {
		for key, value := range current.entries {
			next.entries[key] = value
		}
	}
	next.entries[routingLearningScopeKey(scope)] = entry
	r.current.Store(next)
}

func (r *RoutingLearningRuntime) run() {
	defer close(r.doneCh)
	ticker := time.NewTicker(r.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case scope := <-r.refreshCh:
			r.refreshOne(scope)
		case <-ticker.C:
			r.observedMu.Lock()
			scopes := make([]RoutingArtifactScope, 0, len(r.observed))
			for _, scope := range r.observed {
				scopes = append(scopes, scope)
			}
			r.observedMu.Unlock()
			for _, scope := range scopes {
				r.refreshOne(scope)
			}
		}
	}
}

func (r *RoutingLearningRuntime) refreshOne(scope RoutingArtifactScope) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = r.Refresh(ctx, scope)
	cancel()
}

func routingLearningScopeKey(scope RoutingArtifactScope) string {
	return runtimeStrategyScopeKey(RoutingArtifactScope{
		ArtifactKind: RoutingArtifactStrategy, Platform: scope.Platform, ModelFamily: scope.ModelFamily,
		EndpointKind: scope.EndpointKind, Preference: scope.Preference,
	})
}

var defaultRoutingLearningRuntime atomic.Pointer[RoutingLearningRuntime]

func SetDefaultRoutingLearningRuntime(runtime *RoutingLearningRuntime) {
	defaultRoutingLearningRuntime.Store(runtime)
}

func ApplyDefaultAPIKeyRoutingLearning(scope RoutingArtifactScope, apiKeyID, userID int64, strategyExperimentID *string, baseline *APIKeyRoutingScoreSnapshot, eligible map[int64]bool, now time.Time) RoutingLearningApplication {
	runtime := defaultRoutingLearningRuntime.Load()
	if runtime == nil {
		return RoutingLearningApplication{Snapshot: baseline}
	}
	return runtime.Apply(scope, apiKeyID, userID, strategyExperimentID, baseline, eligible, now)
}

func RefreshDefaultAPIKeyRoutingLearning(ctx context.Context, scope RoutingArtifactScope) error {
	runtime := defaultRoutingLearningRuntime.Load()
	if runtime == nil || !runtime.Enabled() {
		return nil
	}
	return runtime.Refresh(ctx, scope)
}
