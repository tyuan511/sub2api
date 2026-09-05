package service

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// APIKeyRouteRuntimeStateCache loads the session-level group binding and every
// configured candidate's breaker snapshot in one Redis pipeline. CLOSED
// candidates can then be admitted from the request-local snapshot; states that
// require an atomic lease or admission counter still use AllowAPIKeyRoute.
type APIKeyRouteRuntimeStateCache interface {
	LoadAPIKeyRouteRuntimeState(
		ctx context.Context,
		stickyNamespaceID int64,
		stickySessionKey string,
		breakerKeys []string,
	) (stickyGroupID int64, breakers []APIKeyRouteBreakerSnapshot, err error)
}

type apiKeyRouteRuntimeScopeSnapshot struct {
	breakers map[int64]APIKeyRouteBreakerSnapshot
	err      error
}

type apiKeyRouteStickySnapshot struct {
	groupID int64
	err     error
}

type apiKeyRouteAdmission struct {
	allowed bool
	state   string
	err     error
}

// APIKeyRouteRequestRuntimeState is a mutable request-local sidecar. It is
// carried by context as a pointer so handlers may add billing/usage contexts
// without losing the one-time Redis preload.
type APIKeyRouteRequestRuntimeState struct {
	mu sync.Mutex

	APIKeyID            int64
	RouteVersion        int64
	RoutingStateVersion int64
	RoutingEnabled      bool
	MinSuccessRate      int
	Platform            string
	GroupIDs            []int64

	scopes     map[string]apiKeyRouteRuntimeScopeSnapshot
	sticky     map[string]apiKeyRouteStickySnapshot
	admissions map[string]apiKeyRouteAdmission
}

type apiKeyRouteRequestRuntimeStateContextKey struct{}

// WithAPIKeyRouteRequestRuntimeState installs the bounded candidate projection
// required for one-round-trip route-state preloading.
func WithAPIKeyRouteRequestRuntimeState(ctx context.Context, plan *APIKeyRoutePlan) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if plan == nil {
		return ctx
	}
	// Keep an explicit, version-scoped bypass marker even for fixed single-group
	// keys: protocol handlers share the same health/sticky service entry points.
	if !plan.RoutingEnabled {
		return context.WithValue(ctx, apiKeyRouteRequestRuntimeStateContextKey{}, &APIKeyRouteRequestRuntimeState{
			APIKeyID: plan.APIKeyID, RouteVersion: plan.RouteVersion,
			RoutingStateVersion: plan.RoutingStateVersion,
		})
	}
	if len(plan.Candidates) == 0 {
		return ctx
	}
	state := &APIKeyRouteRequestRuntimeState{
		APIKeyID: plan.APIKeyID, RouteVersion: plan.RouteVersion,
		RoutingEnabled:      true,
		RoutingStateVersion: plan.RoutingStateVersion, MinSuccessRate: plan.RoutingMinSuccessRate,
		GroupIDs: make([]int64, 0, len(plan.Candidates)),
		scopes:   make(map[string]apiKeyRouteRuntimeScopeSnapshot),
		sticky:   make(map[string]apiKeyRouteStickySnapshot),
	}
	for _, candidate := range plan.Candidates {
		state.GroupIDs = append(state.GroupIDs, candidate.GroupID)
		if state.Platform == "" && candidate.Group != nil {
			state.Platform = candidate.Group.Platform
		}
	}
	return context.WithValue(ctx, apiKeyRouteRequestRuntimeStateContextKey{}, state)
}

func apiKeyRouteRequestRuntimeStateFromContext(ctx context.Context) (*APIKeyRouteRequestRuntimeState, bool) {
	if ctx == nil {
		return nil, false
	}
	state, ok := ctx.Value(apiKeyRouteRequestRuntimeStateContextKey{}).(*APIKeyRouteRequestRuntimeState)
	return state, ok && state != nil && state.RoutingEnabled && state.APIKeyID > 0 && state.RouteVersion > 0 && len(state.GroupIDs) > 0
}

func apiKeyRouteControlsDisabled(ctx context.Context, apiKeyID, routeVersion int64) bool {
	if ctx == nil {
		return false
	}
	state, ok := ctx.Value(apiKeyRouteRequestRuntimeStateContextKey{}).(*APIKeyRouteRequestRuntimeState)
	return ok && state != nil && state.APIKeyID == apiKeyID && state.RouteVersion == routeVersion && !state.RoutingEnabled
}

func apiKeyRouteRuntimeScopeKey(modelFamily, endpointKind string) string {
	return modelFamily + "\x00" + endpointKind
}

// APIKeyRoutePreloadedBreaker returns the request-local breaker snapshot loaded
// by the one Redis Pipeline used for sticky and route health. It never performs
// I/O and lets smart ranking finish hard admission before optional learning.
func APIKeyRoutePreloadedBreaker(ctx context.Context, modelFamily, endpointKind string, groupID int64) (APIKeyRouteBreakerSnapshot, bool) {
	requestState, ok := apiKeyRouteRequestRuntimeStateFromContext(ctx)
	if !ok || groupID <= 0 {
		return APIKeyRouteBreakerSnapshot{}, false
	}
	modelFamily, endpointKind = normalizeAPIKeyRouteRuntimeScope(ctx, modelFamily, endpointKind)
	requestState.mu.Lock()
	defer requestState.mu.Unlock()
	scope, loaded := requestState.scopes[apiKeyRouteRuntimeScopeKey(modelFamily, endpointKind)]
	if !loaded || scope.err != nil {
		return APIKeyRouteBreakerSnapshot{}, false
	}
	breaker, exists := scope.breakers[groupID]
	return breaker, exists
}

// Recovery is an explicit atomic admission, not merely an observed HALF_OPEN
// state (whose probe may belong to another request/instance).
func APIKeyRouteRecoveryAdmitted(ctx context.Context, modelFamily, endpointKind string, groupID int64) bool {
	state, ok := apiKeyRouteRequestRuntimeStateFromContext(ctx)
	if !ok {
		return false
	}
	modelFamily, endpointKind = normalizeAPIKeyRouteRuntimeScope(ctx, modelFamily, endpointKind)
	key := fmt.Sprintf("%s\x00%d", apiKeyRouteRuntimeScopeKey(modelFamily, endpointKind), groupID)
	state.mu.Lock()
	defer state.mu.Unlock()
	admission, found := state.admissions[key]
	return found && admission.err == nil && admission.allowed && (admission.state == APIKeyRouteBreakerHalfOpen || admission.state == APIKeyRouteBreakerRecovering)
}

func apiKeyRouteRuntimeStickyKey(modelFamily, endpointKind, sessionHash string) string {
	return apiKeyRouteRuntimeScopeKey(modelFamily, endpointKind) + "\x00" + sessionHash
}

func normalizeAPIKeyRouteRuntimeScope(ctx context.Context, modelFamily, endpointKind string) (string, string) {
	if requestState, ok := apiKeyRouteRequestRuntimeStateFromContext(ctx); ok {
		modelFamily = NormalizeAPIKeyRoutingModelFamily(requestState.Platform, modelFamily)
		endpointKind = NormalizeAPIKeyRoutingEndpointKind(endpointKind)
	}
	return modelFamily, endpointKind
}

// preloadAPIKeyRouteRuntimeState returns used=false when the request is not a
// routed-key request or the cache does not support the batch contract.
func preloadAPIKeyRouteRuntimeState(
	ctx context.Context,
	cache GatewayCache,
	apiKeyID, routeVersion int64,
	modelFamily, endpointKind, sessionHash string,
) (apiKeyRouteRuntimeScopeSnapshot, apiKeyRouteStickySnapshot, bool) {
	requestState, ok := apiKeyRouteRequestRuntimeStateFromContext(ctx)
	if !ok || requestState.APIKeyID != apiKeyID || requestState.RouteVersion != routeVersion {
		return apiKeyRouteRuntimeScopeSnapshot{}, apiKeyRouteStickySnapshot{}, false
	}
	batchCache, ok := cache.(APIKeyRouteRuntimeStateCache)
	if !ok {
		return apiKeyRouteRuntimeScopeSnapshot{}, apiKeyRouteStickySnapshot{}, false
	}

	modelFamily, endpointKind = normalizeAPIKeyRouteRuntimeScope(ctx, modelFamily, endpointKind)
	scopeKey := apiKeyRouteRuntimeScopeKey(modelFamily, endpointKind)
	stickyKey := apiKeyRouteRuntimeStickyKey(modelFamily, endpointKind, sessionHash)

	requestState.mu.Lock()
	defer requestState.mu.Unlock()
	scope, scopeLoaded := requestState.scopes[scopeKey]
	sticky, stickyLoaded := requestState.sticky[stickyKey]
	if scopeLoaded && (sessionHash == "" || stickyLoaded) {
		return scope, sticky, true
	}

	breakerKeys := make([]string, 0, len(requestState.GroupIDs))
	if !scopeLoaded {
		for _, groupID := range requestState.GroupIDs {
			breakerKeys = append(breakerKeys, APIKeyRouteHealthKey(apiKeyID, apiKeyRoutingRuntimeVersion(ctx, apiKeyID, routeVersion), groupID, modelFamily, endpointKind))
		}
	}
	logicalStickyKey := ""
	if sessionHash != "" && !stickyLoaded {
		logicalStickyKey = apiKeyGroupStickyCacheKey(apiKeyID, apiKeyRoutingRuntimeVersion(ctx, apiKeyID, routeVersion), modelFamily, endpointKind, sessionHash)
	}
	started := time.Now()
	stickyGroupID, breakers, err := batchCache.LoadAPIKeyRouteRuntimeState(ctx, apiKeyID, logicalStickyKey, breakerKeys)
	DefaultRoutingRuntimeMetrics().RecordPhaseLatency(RoutingLatencyPhaseStateRead, time.Since(started))
	if !scopeLoaded {
		scope = apiKeyRouteRuntimeScopeSnapshot{breakers: make(map[int64]APIKeyRouteBreakerSnapshot, len(requestState.GroupIDs)), err: err}
		if err == nil && len(breakers) == len(requestState.GroupIDs) {
			for index, groupID := range requestState.GroupIDs {
				scope.breakers[groupID] = breakers[index]
			}
		}
		requestState.scopes[scopeKey] = scope
	}
	if sessionHash != "" && !stickyLoaded {
		sticky = apiKeyRouteStickySnapshot{groupID: stickyGroupID, err: err}
		requestState.sticky[stickyKey] = sticky
	}
	return scope, sticky, true
}

func prefetchedAPIKeyRouteBreaker(
	ctx context.Context,
	cache GatewayCache,
	apiKeyID, routeVersion, groupID int64,
	modelFamily, endpointKind string,
) (APIKeyRouteBreakerSnapshot, bool, error) {
	scope, _, used := preloadAPIKeyRouteRuntimeState(ctx, cache, apiKeyID, routeVersion, modelFamily, endpointKind, "")
	if !used {
		return APIKeyRouteBreakerSnapshot{}, false, nil
	}
	if scope.err != nil {
		return APIKeyRouteBreakerSnapshot{}, true, scope.err
	}
	snapshot, ok := scope.breakers[groupID]
	if !ok {
		return APIKeyRouteBreakerSnapshot{}, true, ErrNoEligibleAPIKeyRoute
	}
	if snapshot.State == "" {
		snapshot.State = APIKeyRouteBreakerClosed
	}
	return snapshot, true, nil
}
