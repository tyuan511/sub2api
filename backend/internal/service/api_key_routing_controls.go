package service

import (
	"context"
	"fmt"
	"time"
)

// Creation default only; legacy persisted keys and facts retain their 50% fallback.
const DefaultNewAPIKeyRoutingMinSuccessRate = 80

// Count configured selections before request/health filtering. A multi-group
// key with only one currently healthy candidate must not bypass its hard gate.
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
func ValidateAPIKeyRoutingControls(balance, minimum *int) error {
	if balance != nil && (*balance < 0 || *balance > 10000) {
		return fmt.Errorf("%w: smart_balance_bps must be between 0 and 10000", ErrAPIKeyRoutesInvalid)
	}
	if minimum != nil && (*minimum < 50 || *minimum > 95 || *minimum%5 != 0) {
		return fmt.Errorf("%w: routing_min_success_rate must be 50 to 95 in steps of 5", ErrAPIKeyRoutesInvalid)
	}
	return nil
}

func APIKeyRoutingBalancePreference(balance int) string {
	if balance < 5000 {
		return APIKeySmartPreferencePrice
	}
	if balance > 5000 {
		return APIKeySmartPreferenceSpeed
	}
	return APIKeySmartPreferenceBalanced
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

func APIKeyRoutingBalanceWeights(balance int) APIKeyRoutingScoreWeights {
	speed := float64(max(0, min(10000, balance))) / 10000
	return APIKeyRoutingScoreWeights{Success: .50, Capacity: .10, Price: .40 * (1 - speed), Speed: .40 * speed}
}

// Apply after selecting active/canary/shadow artifacts. Optimization may improve
// component estimates and stability, but cannot override the user's exact ratio.
func ApplyAPIKeyRoutingControls(policy APIKeyRoutingStrategyPolicy, key *APIKey) APIKeyRoutingStrategyPolicy {
	if key == nil {
		return policy
	}
	policy.SuccessRateHardGate = float64(key.EffectiveRoutingMinSuccessRate()) / 100
	if key.SmartBalanceBPS != nil {
		policy.Weights = APIKeyRoutingBalanceWeights(*key.SmartBalanceBPS)
		policy.Preference = APIKeyRoutingBalancePreference(*key.SmartBalanceBPS)
	}
	return policy
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

func apiKeyRoutingRuntimeVersion(ctx context.Context, id, version int64) int64 {
	if state, ok := apiKeyRouteRequestRuntimeStateFromContext(ctx); ok && state.APIKeyID == id && state.RouteVersion == version && state.RoutingStateVersion > 0 {
		return state.RoutingStateVersion
	}
	if meta, ok := APIKeyRoutingUsageContextFromContext(ctx); ok && meta.APIKeyID == id && meta.RouteVersion == version && meta.RoutingStateVersion > 0 {
		return meta.RoutingStateVersion
	}
	return version
}

func apiKeyRoutingMinimumFromContext(ctx context.Context, id, version int64) int {
	minimum := 50
	if state, ok := apiKeyRouteRequestRuntimeStateFromContext(ctx); ok && state.APIKeyID == id && state.RouteVersion == version {
		minimum = state.MinSuccessRate
	} else if meta, ok := APIKeyRoutingUsageContextFromContext(ctx); ok && meta.APIKeyID == id && meta.RouteVersion == version {
		minimum = meta.RoutingMinSuccessRate
	}
	if minimum < 50 || minimum > 95 || minimum%5 != 0 {
		return 50
	}
	return minimum
}

// Shared observations are read from local memory, including on sticky and
// sequential paths. Missing samples are unknown, never fabricated as failures.
func apiKeyRoutingBelowSharedGate(ctx context.Context, groupID int64, model, endpoint string, minimum, samples int) bool {
	state, ok := apiKeyRouteRequestRuntimeStateFromContext(ctx)
	if !ok {
		return false
	}
	scope := APIKeyRoutingScoreScope{Platform: state.Platform, ModelFamily: model, EndpointKind: endpoint}
	snapshot, ok := DefaultAPIKeyRoutingScoreStore().Lookup(scope, 180*time.Second, time.Now())
	if !ok {
		return false
	}
	observation, ok := snapshot.Groups[groupID]
	if !ok {
		return false
	}
	total := observation.SuccessRequests + observation.FailedRequests
	return total >= int64(samples) && observation.SuccessRequests*100 < total*int64(minimum)
}
