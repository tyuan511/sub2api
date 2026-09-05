package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type APIKeyRoutingStrategySelection struct {
	Policy           APIKeyRoutingStrategyPolicy
	AssignmentReason string
	ExperimentID     *string
	ExperimentBucket *int
	ShadowPolicy     *APIKeyRoutingStrategyPolicy
}

type routingStrategyRuntimeEntry struct {
	active   APIKeyRoutingStrategyPolicy
	canary   *APIKeyRoutingStrategyPolicy
	shadow   *APIKeyRoutingStrategyPolicy
	pointers RoutingArtifactPointers
}

type routingStrategyRuntimeCatalog struct {
	entries map[string]routingStrategyRuntimeEntry
}

// RoutingStrategyRuntime keeps strategy execution entirely in local memory.
// A new scope serves the built-in deterministic baseline immediately and is
// refreshed asynchronously; request handlers never wait on Redis or SQL.
type RoutingStrategyRuntime struct {
	cache           RoutingArtifactCache
	enabled         bool
	learning        *RoutingLearningRuntime
	refreshInterval time.Duration
	current         atomic.Pointer[routingStrategyRuntimeCatalog]
	observed        sync.Map
	updateMu        sync.Mutex
	refreshCh       chan RoutingArtifactScope
	stopCh          chan struct{}
	doneCh          chan struct{}
	startOnce       sync.Once
	stopOnce        sync.Once
	started         atomic.Bool
}

func NewRoutingStrategyRuntime(cache RoutingArtifactCache, enabled bool) *RoutingStrategyRuntime {
	runtime := &RoutingStrategyRuntime{
		cache: cache, enabled: enabled, refreshInterval: 30 * time.Second,
		refreshCh: make(chan RoutingArtifactScope, 256), stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
	runtime.current.Store(&routingStrategyRuntimeCatalog{entries: map[string]routingStrategyRuntimeEntry{}})
	return runtime
}

func (r *RoutingStrategyRuntime) Start() {
	if r == nil || !r.enabled || r.cache == nil {
		return
	}
	r.startOnce.Do(func() {
		r.started.Store(true)
		if r.learning != nil {
			r.learning.Start()
		}
		go r.run()
	})
}

func (r *RoutingStrategyRuntime) Stop() {
	if r == nil {
		return
	}
	if !r.started.Load() {
		return
	}
	r.stopOnce.Do(func() {
		close(r.stopCh)
		if r.learning != nil {
			r.learning.Stop()
		}
		select {
		case <-r.doneCh:
		case <-time.After(2 * time.Second):
		}
	})
}

func (r *RoutingStrategyRuntime) Select(scope RoutingArtifactScope, userID, apiKeyID int64) APIKeyRoutingStrategySelection {
	preference := APIKeySmartPreferenceBalanced
	if scope.Preference != nil {
		preference = *scope.Preference
	}
	fallback := APIKeyRoutingStrategySelection{
		Policy: DefaultAPIKeyRoutingStrategyPolicy(preference), AssignmentReason: RoutingAssignmentDeterministic,
	}
	if r == nil || !r.enabled || scope.Validate() != nil || scope.ArtifactKind != RoutingArtifactStrategy {
		return fallback
	}
	r.observe(scope)
	catalog := r.current.Load()
	if catalog == nil {
		return fallback
	}
	entry, ok := catalog.entries[runtimeStrategyScopeKey(scope)]
	if !ok {
		return fallback
	}
	selection := APIKeyRoutingStrategySelection{Policy: entry.active, AssignmentReason: RoutingAssignmentDeterministic}
	if entry.shadow != nil {
		shadow := *entry.shadow
		selection.ShadowPolicy = &shadow
	}
	if entry.canary == nil || userID <= 0 || apiKeyID <= 0 {
		return selection
	}
	bucket := StableRoutingExperimentBucket(userID, apiKeyID, []byte(entry.pointers.CanaryBucketSaltChecksum))
	selection.ExperimentID = optionalStringPtr(entry.pointers.CanaryExperimentID)
	selection.ExperimentBucket = &bucket
	if bucket >= entry.pointers.CanaryAllocationBPS {
		return selection
	}
	selection.Policy = *entry.canary
	selection.AssignmentReason = RoutingAssignmentCanary
	return selection
}

func (r *RoutingStrategyRuntime) Refresh(ctx context.Context, scope RoutingArtifactScope) error {
	if r == nil || !r.enabled || r.cache == nil || scope.ArtifactKind != RoutingArtifactStrategy || scope.Validate() != nil {
		return ErrRoutingArtifactUnavailable
	}
	pointers, err := r.cache.LoadPointers(ctx, scope)
	if err != nil {
		return err
	}
	activeArtifact, _, err := ResolveRoutingArtifact(ctx, r.cache, scope)
	if err != nil {
		return err
	}
	active, err := ParseAPIKeyRoutingStrategyArtifact(activeArtifact)
	if err != nil {
		if pointers.BaselineVersion == activeArtifact.Version {
			return err
		}
		baseline, loadErr := r.cache.LoadArtifact(ctx, scope, pointers.BaselineVersion)
		if loadErr != nil {
			return errors.Join(err, loadErr)
		}
		active, err = ParseAPIKeyRoutingStrategyArtifact(baseline)
		if err != nil {
			return err
		}
	}
	entry := routingStrategyRuntimeEntry{active: active, pointers: pointers}
	if pointers.CanaryVersion != "" {
		if artifact, loadErr := r.cache.LoadArtifact(ctx, scope, pointers.CanaryVersion); loadErr == nil {
			if policy, parseErr := ParseAPIKeyRoutingStrategyArtifact(artifact); parseErr == nil {
				entry.canary = &policy
			}
		}
	}
	if pointers.ShadowVersion != "" {
		if artifact, loadErr := r.cache.LoadArtifact(ctx, scope, pointers.ShadowVersion); loadErr == nil {
			if policy, parseErr := ParseAPIKeyRoutingStrategyArtifact(artifact); parseErr == nil {
				entry.shadow = &policy
			}
		}
	}
	r.replaceEntry(scope, entry)
	return nil
}

func (r *RoutingStrategyRuntime) observe(scope RoutingArtifactScope) {
	key := runtimeStrategyScopeKey(scope)
	if _, loaded := r.observed.LoadOrStore(key, scope); loaded {
		return
	}
	select {
	case r.refreshCh <- scope:
	default:
	}
}

func (r *RoutingStrategyRuntime) replaceEntry(scope RoutingArtifactScope, entry routingStrategyRuntimeEntry) {
	r.updateMu.Lock()
	defer r.updateMu.Unlock()
	current := r.current.Load()
	next := &routingStrategyRuntimeCatalog{entries: make(map[string]routingStrategyRuntimeEntry)}
	if current != nil {
		for key, value := range current.entries {
			next.entries[key] = value
		}
	}
	next.entries[runtimeStrategyScopeKey(scope)] = entry
	r.current.Store(next)
}

func (r *RoutingStrategyRuntime) run() {
	defer close(r.doneCh)
	ticker := time.NewTicker(r.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case scope := <-r.refreshCh:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = r.Refresh(ctx, scope)
			cancel()
		case <-ticker.C:
			r.observed.Range(func(_, value any) bool {
				scope, ok := value.(RoutingArtifactScope)
				if ok {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					_ = r.Refresh(ctx, scope)
					cancel()
				}
				return true
			})
		}
	}
}

func runtimeStrategyScopeKey(scope RoutingArtifactScope) string {
	preference := ""
	if scope.Preference != nil {
		preference = *scope.Preference
	}
	return APIKeyRoutingScoreScope{Platform: scope.Platform, ModelFamily: scope.ModelFamily, EndpointKind: scope.EndpointKind}.Key() + "\x00" + preference
}

var defaultRoutingStrategyRuntime atomic.Pointer[RoutingStrategyRuntime]

func SetDefaultRoutingStrategyRuntime(runtime *RoutingStrategyRuntime) {
	defaultRoutingStrategyRuntime.Store(runtime)
	if runtime == nil {
		SetDefaultRoutingLearningRuntime(nil)
	}
}

func SelectDefaultAPIKeyRoutingStrategy(scope RoutingArtifactScope, userID, apiKeyID int64) APIKeyRoutingStrategySelection {
	runtime := defaultRoutingStrategyRuntime.Load()
	if runtime == nil {
		preference := APIKeySmartPreferenceBalanced
		if scope.Preference != nil {
			preference = *scope.Preference
		}
		return APIKeyRoutingStrategySelection{
			Policy: DefaultAPIKeyRoutingStrategyPolicy(preference), AssignmentReason: RoutingAssignmentDeterministic,
		}
	}
	return runtime.Select(scope, userID, apiKeyID)
}

func RefreshDefaultAPIKeyRoutingStrategy(ctx context.Context, scope RoutingArtifactScope) error {
	runtime := defaultRoutingStrategyRuntime.Load()
	if runtime == nil || !runtime.enabled {
		return nil
	}
	return runtime.Refresh(ctx, scope)
}
