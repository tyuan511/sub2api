package service

import (
	"context"
	"fmt"
)

// Creation default only; legacy persisted keys and facts retain their 50% fallback.
const DefaultNewAPIKeyRoutingMinSuccessRate = 80

// Count configured selections before request/health filtering. A multi-group
// key with only one currently healthy candidate still keeps routing controls.
func (k *APIKey) HasMultipleEnabledGroupRoutes() bool {
	if k == nil {
		return false
	}
	count := 0
	for _, route := range k.GroupRoutes {
		if route.Enabled {
			count++
			if count > 1 {
				return true
			}
		}
	}
	return false
}

// Basis points retain exact legacy preset weights (12.5/87.5), while new keys
// can select any integer basis-point balance without multiplying shared caches.
func ValidateAPIKeyRoutingControls(minimum *int) error {
	if minimum != nil && (*minimum < 50 || *minimum > 95 || *minimum%5 != 0) {
		return fmt.Errorf("%w: routing_min_success_rate must be 50 to 95 in steps of 5", ErrAPIKeyRoutesInvalid)
	}
	return nil
}

func (k *APIKey) EffectiveRoutingMinSuccessRate() int {
	if k == nil || k.RoutingMinSuccessRate < 50 || k.RoutingMinSuccessRate > 95 || k.RoutingMinSuccessRate%5 != 0 {
		return 50
	}
	return k.RoutingMinSuccessRate
}

func (k *APIKey) EffectiveRoutingStateVersion() int64 {
	if k.RoutingStateVersion > 0 {
		return k.RoutingStateVersion
	}
	return k.RouteVersion
}

func sameAPIKeyRouteSet(a, b []APIKeyGroupRoute) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].GroupID != b[i].GroupID || a[i].Priority != b[i].Priority || a[i].Enabled != b[i].Enabled {
			return false
		}
	}
	return true
}

// apiKeyRoutingConfigurationEquivalent compares the effective persisted
// routing configuration with the normalized update. The API accepts both the
// legacy group_id projection and the explicit route list, so a legacy single
// group key must compare equal to the equivalent one-item route list emitted
// by the current UI.
func apiKeyRoutingConfigurationEquivalent(current *APIKey, next normalizedAPIKeyRouting) bool {
	if current == nil {
		return false
	}
	currentMode := current.ScheduleMode
	if currentMode == "" {
		currentMode = APIKeyScheduleModeSequential
	}
	if currentMode != next.ScheduleMode || current.EffectiveRoutingMinSuccessRate() != next.MinSuccessRate {
		return false
	}
	currentRoutes := current.GroupRoutes
	if len(currentRoutes) == 0 && current.GroupID != nil && *current.GroupID > 0 {
		currentRoutes = []APIKeyGroupRoute{{GroupID: *current.GroupID, Priority: 0, Enabled: true}}
	}
	return sameAPIKeyRouteSet(currentRoutes, next.Routes)
}

func apiKeyRoutingRuntimeVersion(ctx context.Context, id, version int64) int64 {
	if state, ok := apiKeyRouteRequestRuntimeStateFromContext(ctx); ok && state.APIKeyID == id && state.RouteVersion == version && state.RoutingStateVersion > 0 {
		return state.RoutingStateVersion
	}
	if meta, ok := APIKeyRoutingUsageContextFromContext(ctx); ok && meta.APIKeyID == id && meta.RouteVersion == version && meta.RoutingStateVersion > 0 {
		return meta.RoutingStateVersion
	}
	return version
}

func apiKeyRoutingMinimumFromContext(context.Context, int64, int64) int {
	// Success rate is a ranking signal, not a user-configurable intercept.
	// The breaker still uses the original 50% floor as an internal safety net.
	return 50
}

// Shared observations are read from local memory, including on sticky and
// sequential paths. Missing samples are unknown, never fabricated as failures.
// Success rate no longer force-opens a candidate; low success only lowers score.
func apiKeyRoutingBelowSharedGate(context.Context, int64, string, string, int, int) bool {
	return false
}
