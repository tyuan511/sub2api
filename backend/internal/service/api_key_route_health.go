package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	APIKeyRouteBreakerClosed     = "CLOSED"
	APIKeyRouteBreakerOpen       = "OPEN"
	APIKeyRouteBreakerHalfOpen   = "HALF_OPEN"
	APIKeyRouteBreakerRecovering = "RECOVERING"

	APIKeyRouteOutcomeHealthFailure    = "health_failure"
	APIKeyRouteOutcomeCapacityOverflow = "capacity_overflow"
)

type APIKeyRouteHealthCache interface {
	AllowAPIKeyRoute(ctx context.Context, key string, now time.Time, cooldown, probeLease time.Duration) (allowed bool, state string, err error)
	RecordAPIKeyRouteResult(ctx context.Context, key string, success bool, now time.Time, window time.Duration, minSamples, recoverySuccesses int) (state string, err error)
}

// Optional extension keeps legacy caches/tests compatible with the 50% gate.
type APIKeyRouteThresholdHealthCache interface {
	AllowAPIKeyRouteWithThreshold(context.Context, string, time.Time, time.Duration, time.Duration, time.Duration, int, int, int64, bool) (bool, string, error)
	RecordAPIKeyRouteResultWithThreshold(context.Context, string, bool, time.Time, time.Duration, int, int, int, int64) (string, error)
}

type APIKeyRouteHealthPolicy struct {
	Window            time.Duration
	Cooldown          time.Duration
	ProbeLease        time.Duration
	MinimumSamples    int
	RecoverySuccesses int
}

func DefaultAPIKeyRouteHealthPolicy(cfg *config.Config) APIKeyRouteHealthPolicy {
	window, cooldown, minSamples := 5*time.Minute, 30*time.Second, 10
	if cfg != nil {
		if cfg.Gateway.APIKeyGroupBreakerWindowSeconds > 0 {
			window = time.Duration(cfg.Gateway.APIKeyGroupBreakerWindowSeconds) * time.Second
		}
		if cfg.Gateway.APIKeyGroupBreakerCooldownSeconds > 0 {
			cooldown = time.Duration(cfg.Gateway.APIKeyGroupBreakerCooldownSeconds) * time.Second
		}
		if cfg.Gateway.APIKeyGroupBreakerMinSamples > 0 {
			minSamples = cfg.Gateway.APIKeyGroupBreakerMinSamples
		}
	}
	return APIKeyRouteHealthPolicy{Window: window, Cooldown: cooldown, ProbeLease: 10 * time.Second, MinimumSamples: minSamples, RecoverySuccesses: 10}
}

func APIKeyRouteHealthKey(apiKeyID, routeVersion, groupID int64, model, endpoint string) string {
	scope := sha256.Sum256([]byte(model + "\x00" + endpoint))
	return fmt.Sprintf("api_key_route_health:{%d}:v%d:g%d:%x", apiKeyID, routeVersion, groupID, scope[:8])
}

func allowAPIKeyRoute(ctx context.Context, cache GatewayCache, policy APIKeyRouteHealthPolicy, apiKeyID, routeVersion, groupID int64, model, endpoint string) (bool, string, error) {
	if apiKeyRouteControlsDisabled(ctx, apiKeyID, routeVersion) {
		return true, APIKeyRouteBreakerClosed, nil
	}
	model, endpoint = normalizeAPIKeyRouteRuntimeScope(ctx, model, endpoint)
	request, cached := apiKeyRouteRequestRuntimeStateFromContext(ctx)
	cached = cached && request.APIKeyID == apiKeyID && request.RouteVersion == routeVersion
	admissionKey := fmt.Sprintf("%s\x00%d", apiKeyRouteRuntimeScopeKey(model, endpoint), groupID)
	if cached {
		request.mu.Lock()
		previous, found := request.admissions[admissionKey]
		request.mu.Unlock()
		if found {
			return previous.allowed, previous.state, previous.err
		}
	}
	allowed, state, err := allowAPIKeyRouteOnce(ctx, cache, policy, apiKeyID, routeVersion, groupID, model, endpoint)
	if cached {
		request.mu.Lock()
		if request.admissions == nil {
			request.admissions = make(map[string]apiKeyRouteAdmission)
		}
		request.admissions[admissionKey] = apiKeyRouteAdmission{allowed, state, err}
		if scope, found := request.scopes[apiKeyRouteRuntimeScopeKey(model, endpoint)]; found && scope.err == nil {
			breaker := scope.breakers[groupID]
			breaker.State = state
			scope.breakers[groupID] = breaker
		}
		request.mu.Unlock()
	}
	return allowed, state, err
}

func allowAPIKeyRouteOnce(ctx context.Context, cache GatewayCache, policy APIKeyRouteHealthPolicy, apiKeyID, routeVersion, groupID int64, model, endpoint string) (bool, string, error) {
	model, endpoint = normalizeAPIKeyRouteRuntimeScope(ctx, model, endpoint)
	minimum := apiKeyRoutingMinimumFromContext(ctx, apiKeyID, routeVersion)
	belowSharedGate := apiKeyRoutingBelowSharedGate(ctx, groupID, model, endpoint, minimum, policy.MinimumSamples)
	if snapshot, used, err := prefetchedAPIKeyRouteBreaker(ctx, cache, apiKeyID, routeVersion, groupID, model, endpoint); used {
		if err != nil {
			DefaultRoutingRuntimeMetrics().RecordBreaker(APIKeyRouteBreakerClosed, false, true)
			if minimum > 50 {
				return false, "STATE_UNAVAILABLE", nil
			}
			return true, APIKeyRouteBreakerClosed, err
		}
		// OPEN/HALF_OPEN/RECOVERING require an atomic cooldown lease or gradual
		// admission counter. CLOSED is the overwhelmingly common path and is safe
		// to serve directly from the request-frozen batch snapshot.
		total := snapshot.Successes + snapshot.Failures
		belowGate := total >= int64(policy.MinimumSamples) && snapshot.Successes*100 < total*int64(minimum)
		if snapshot.State == APIKeyRouteBreakerClosed && !belowGate && !belowSharedGate {
			DefaultRoutingRuntimeMetrics().RecordBreaker(snapshot.State, false, false)
			return true, snapshot.State, nil
		}
	}
	health, ok := cache.(APIKeyRouteHealthCache)
	if !ok || apiKeyID <= 0 || routeVersion <= 0 || groupID <= 0 {
		if belowSharedGate {
			return false, APIKeyRouteBreakerOpen, nil
		}
		if minimum > 50 {
			return false, "STATE_UNAVAILABLE", nil
		}
		return true, APIKeyRouteBreakerClosed, nil
	}
	started := time.Now()
	key := APIKeyRouteHealthKey(apiKeyID, apiKeyRoutingRuntimeVersion(ctx, apiKeyID, routeVersion), groupID, model, endpoint)
	var allowed bool
	var state string
	var err error
	if thresholds, supported := cache.(APIKeyRouteThresholdHealthCache); supported {
		allowed, state, err = thresholds.AllowAPIKeyRouteWithThreshold(ctx, key, time.Now(), policy.Cooldown, policy.ProbeLease, policy.Window, policy.MinimumSamples, minimum, routeVersion, belowSharedGate)
	} else if minimum > 50 {
		return false, "STATE_UNAVAILABLE", nil
	} else {
		allowed, state, err = health.AllowAPIKeyRoute(ctx, key, time.Now(), policy.Cooldown, policy.ProbeLease)
	}
	DefaultRoutingRuntimeMetrics().RecordPhaseLatency(RoutingLatencyPhaseStateRead, time.Since(started))
	if err != nil {
		// Redis health state is a safety accelerator, not an authority for
		// expanding the candidate set. On an outage, keep this request inside
		// its frozen configured route plan and let the handler use sequential
		// failover while surfacing the error for logs/metrics.
		DefaultRoutingRuntimeMetrics().RecordBreaker(APIKeyRouteBreakerClosed, false, true)
		if minimum > 50 {
			return false, "STATE_UNAVAILABLE", nil
		}
		return true, APIKeyRouteBreakerClosed, err
	}
	DefaultRoutingRuntimeMetrics().RecordBreaker(state, allowed && state == APIKeyRouteBreakerHalfOpen, false)
	if belowSharedGate && state != APIKeyRouteBreakerHalfOpen && state != APIKeyRouteBreakerRecovering {
		return false, APIKeyRouteBreakerOpen, nil
	}
	return allowed, state, nil
}

func recordAPIKeyRouteResult(ctx context.Context, cache GatewayCache, policy APIKeyRouteHealthPolicy, apiKeyID, routeVersion, groupID int64, model, endpoint string, success bool) (string, error) {
	model, endpoint = normalizeAPIKeyRouteRuntimeScope(ctx, model, endpoint)
	state := APIKeyRouteBreakerClosed
	if apiKeyRouteControlsDisabled(ctx, apiKeyID, routeVersion) {
		// Retain failure observations for shared group statistics, without
		// creating or consulting a per-key breaker for a fixed group.
		if !success {
			emitAPIKeyRoutingFailureFact(ctx, apiKeyID, routeVersion, groupID, model, endpoint, state, RoutingFactOutcomeRouteAttemptFailed)
		}
		return state, nil
	}
	health, ok := cache.(APIKeyRouteHealthCache)
	if !ok || apiKeyID <= 0 || routeVersion <= 0 || groupID <= 0 {
		if !success {
			emitAPIKeyRoutingFailureFact(ctx, apiKeyID, routeVersion, groupID, model, endpoint, state, RoutingFactOutcomeRouteAttemptFailed)
		}
		return state, nil
	}
	started := time.Now()
	key := APIKeyRouteHealthKey(apiKeyID, apiKeyRoutingRuntimeVersion(ctx, apiKeyID, routeVersion), groupID, model, endpoint)
	var err error
	if thresholds, supported := cache.(APIKeyRouteThresholdHealthCache); supported {
		state, err = thresholds.RecordAPIKeyRouteResultWithThreshold(ctx, key, success, time.Now(), policy.Window, policy.MinimumSamples, policy.RecoverySuccesses, apiKeyRoutingMinimumFromContext(ctx, apiKeyID, routeVersion), routeVersion)
	} else {
		state, err = health.RecordAPIKeyRouteResult(ctx, key, success, time.Now(), policy.Window, policy.MinimumSamples, policy.RecoverySuccesses)
	}
	DefaultRoutingRuntimeMetrics().RecordPhaseLatency(RoutingLatencyPhaseStateWrite, time.Since(started))
	DefaultRoutingRuntimeMetrics().RecordBreaker(state, false, err != nil)
	if !success {
		emitAPIKeyRoutingFailureFact(ctx, apiKeyID, routeVersion, groupID, model, endpoint, state, RoutingFactOutcomeRouteAttemptFailed)
	}
	return state, err
}

func classifyAPIKeyRouteFailure(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrNoAvailableAccounts) || errors.Is(err, ErrNoAvailableCompactAccounts) {
		return APIKeyRouteOutcomeCapacityOverflow
	}
	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) && (failoverErr.StatusCode == http.StatusTooManyRequests || failoverErr.StatusCode == http.StatusServiceUnavailable) {
		return APIKeyRouteOutcomeCapacityOverflow
	}
	return APIKeyRouteOutcomeHealthFailure
}

// recordAPIKeyRouteFailure keeps capacity/load shedding out of the health
// breaker. Capacity overflows remain observable and may trigger this request's
// fallback, but they cannot push the group below the 50% health threshold.
func recordAPIKeyRouteFailure(ctx context.Context, cache GatewayCache, policy APIKeyRouteHealthPolicy, apiKeyID, routeVersion, groupID int64, model, endpoint string, cause error) (string, error) {
	outcome := classifyAPIKeyRouteFailure(cause)
	if outcome == "" {
		return APIKeyRouteBreakerClosed, nil
	}
	if outcome == APIKeyRouteOutcomeCapacityOverflow {
		emitAPIKeyRoutingFailureFact(ctx, apiKeyID, routeVersion, groupID, model, endpoint, "", RoutingFactOutcomeCapacityOverflow)
		return APIKeyRouteBreakerClosed, nil
	}
	return recordAPIKeyRouteResult(ctx, cache, policy, apiKeyID, routeVersion, groupID, model, endpoint, false)
}

func (s *GatewayService) AllowAPIKeyRoute(ctx context.Context, apiKeyID, routeVersion, groupID int64, model, endpoint string) (bool, string, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled {
		return true, APIKeyRouteBreakerClosed, nil
	}
	return allowAPIKeyRoute(ctx, s.cache, DefaultAPIKeyRouteHealthPolicy(s.cfg), apiKeyID, routeVersion, groupID, model, endpoint)
}

func (s *GatewayService) RecordAPIKeyRouteResult(ctx context.Context, apiKeyID, routeVersion, groupID int64, model, endpoint string, success bool) (string, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled {
		return APIKeyRouteBreakerClosed, nil
	}
	return recordAPIKeyRouteResult(ctx, s.cache, DefaultAPIKeyRouteHealthPolicy(s.cfg), apiKeyID, routeVersion, groupID, model, endpoint, success)
}

func (s *GatewayService) RecordAPIKeyRouteFailure(ctx context.Context, apiKeyID, routeVersion, groupID int64, model, endpoint string, cause error) (string, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled {
		return APIKeyRouteBreakerClosed, nil
	}
	return recordAPIKeyRouteFailure(ctx, s.cache, DefaultAPIKeyRouteHealthPolicy(s.cfg), apiKeyID, routeVersion, groupID, model, endpoint, cause)
}

func (s *OpenAIGatewayService) AllowAPIKeyRoute(ctx context.Context, apiKeyID, routeVersion, groupID int64, model, endpoint string) (bool, string, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled {
		return true, APIKeyRouteBreakerClosed, nil
	}
	return allowAPIKeyRoute(ctx, s.cache, DefaultAPIKeyRouteHealthPolicy(s.cfg), apiKeyID, routeVersion, groupID, model, endpoint)
}

func (s *OpenAIGatewayService) RecordAPIKeyRouteResult(ctx context.Context, apiKeyID, routeVersion, groupID int64, model, endpoint string, success bool) (string, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled {
		return APIKeyRouteBreakerClosed, nil
	}
	return recordAPIKeyRouteResult(ctx, s.cache, DefaultAPIKeyRouteHealthPolicy(s.cfg), apiKeyID, routeVersion, groupID, model, endpoint, success)
}

func (s *OpenAIGatewayService) RecordAPIKeyRouteFailure(ctx context.Context, apiKeyID, routeVersion, groupID int64, model, endpoint string, cause error) (string, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled {
		return APIKeyRouteBreakerClosed, nil
	}
	return recordAPIKeyRouteFailure(ctx, s.cache, DefaultAPIKeyRouteHealthPolicy(s.cfg), apiKeyID, routeVersion, groupID, model, endpoint, cause)
}
