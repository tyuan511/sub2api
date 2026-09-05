package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

const defaultAPIKeyGroupStickyTTL = time.Hour

func apiKeyGroupStickyCacheKey(apiKeyID, routeVersion int64, modelFamily, endpointKind, sessionHash string) string {
	digest := sha256.Sum256([]byte(modelFamily + "\x00" + endpointKind + "\x00" + sessionHash))
	// The hash tag makes the logical API-key routing state cluster-safe even
	// though it is stored through the existing sticky cache abstraction.
	return fmt.Sprintf("api_key_group:{%d}:v%d:%x", apiKeyID, routeVersion, digest[:16])
}

func apiKeyGroupStickyTTL(cfgSeconds int) time.Duration {
	if cfgSeconds <= 0 {
		return defaultAPIKeyGroupStickyTTL
	}
	return time.Duration(cfgSeconds) * time.Second
}

func getAPIKeyGroupSticky(ctx context.Context, cache GatewayCache, apiKeyID, routeVersion int64, modelFamily, endpointKind, sessionHash string) (int64, error) {
	if apiKeyRouteControlsDisabled(ctx, apiKeyID, routeVersion) {
		return 0, nil
	}
	if cache == nil || apiKeyID <= 0 || routeVersion <= 0 || modelFamily == "" || endpointKind == "" {
		return 0, nil
	}
	if scope, sticky, used := preloadAPIKeyRouteRuntimeState(ctx, cache, apiKeyID, routeVersion, modelFamily, endpointKind, sessionHash); used {
		if sessionHash == "" {
			return 0, scope.err
		}
		if sticky.err != nil {
			DefaultRoutingRuntimeMetrics().RecordSticky("error")
			return 0, sticky.err
		}
		if sticky.groupID > 0 {
			DefaultRoutingRuntimeMetrics().RecordSticky("hit")
		} else {
			DefaultRoutingRuntimeMetrics().RecordSticky("miss")
		}
		return sticky.groupID, nil
	}
	if sessionHash == "" {
		return 0, nil
	}
	started := time.Now()
	groupID, err := cache.GetSessionAccountID(ctx, apiKeyID, apiKeyGroupStickyCacheKey(apiKeyID, apiKeyRoutingRuntimeVersion(ctx, apiKeyID, routeVersion), modelFamily, endpointKind, sessionHash))
	DefaultRoutingRuntimeMetrics().RecordPhaseLatency(RoutingLatencyPhaseStateRead, time.Since(started))
	if errors.Is(err, ErrStickySessionNotFound) {
		DefaultRoutingRuntimeMetrics().RecordSticky("miss")
		return 0, nil
	}
	if err != nil {
		DefaultRoutingRuntimeMetrics().RecordSticky("error")
	} else if groupID > 0 {
		DefaultRoutingRuntimeMetrics().RecordSticky("hit")
	}
	return groupID, err
}

func bindAPIKeyGroupSticky(ctx context.Context, cache GatewayCache, apiKeyID, routeVersion int64, modelFamily, endpointKind, sessionHash string, groupID int64, ttl time.Duration) error {
	if apiKeyRouteControlsDisabled(ctx, apiKeyID, routeVersion) {
		return nil
	}
	if cache == nil || apiKeyID <= 0 || routeVersion <= 0 || modelFamily == "" || endpointKind == "" || sessionHash == "" || groupID <= 0 {
		return nil
	}
	started := time.Now()
	err := cache.SetSessionAccountID(ctx, apiKeyID, apiKeyGroupStickyCacheKey(apiKeyID, apiKeyRoutingRuntimeVersion(ctx, apiKeyID, routeVersion), modelFamily, endpointKind, sessionHash), groupID, ttl)
	DefaultRoutingRuntimeMetrics().RecordPhaseLatency(RoutingLatencyPhaseStateWrite, time.Since(started))
	if err != nil {
		DefaultRoutingRuntimeMetrics().RecordSticky("error")
	} else {
		DefaultRoutingRuntimeMetrics().RecordSticky("bind")
	}
	return err
}

func (s *GatewayService) GetAPIKeyGroupSticky(ctx context.Context, apiKeyID, routeVersion int64, modelFamily, endpointKind, sessionHash string) (int64, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled {
		return 0, nil
	}
	return getAPIKeyGroupSticky(ctx, s.cache, apiKeyID, routeVersion, modelFamily, endpointKind, sessionHash)
}

func (s *GatewayService) BindAPIKeyGroupSticky(ctx context.Context, apiKeyID, routeVersion int64, modelFamily, endpointKind, sessionHash string, groupID int64) error {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled {
		return nil
	}
	seconds := 0
	if s.cfg != nil {
		seconds = s.cfg.Gateway.APIKeyGroupStickyTTLSeconds
	}
	return bindAPIKeyGroupSticky(ctx, s.cache, apiKeyID, routeVersion, modelFamily, endpointKind, sessionHash, groupID, apiKeyGroupStickyTTL(seconds))
}

func (s *OpenAIGatewayService) GetAPIKeyGroupSticky(ctx context.Context, apiKeyID, routeVersion int64, modelFamily, endpointKind, sessionHash string) (int64, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled {
		return 0, nil
	}
	return getAPIKeyGroupSticky(ctx, s.cache, apiKeyID, routeVersion, modelFamily, endpointKind, sessionHash)
}

func (s *OpenAIGatewayService) BindAPIKeyGroupSticky(ctx context.Context, apiKeyID, routeVersion int64, modelFamily, endpointKind, sessionHash string, groupID int64) error {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled {
		return nil
	}
	seconds := 0
	if s.cfg != nil {
		seconds = s.cfg.Gateway.APIKeyGroupStickyTTLSeconds
	}
	return bindAPIKeyGroupSticky(ctx, s.cache, apiKeyID, routeVersion, modelFamily, endpointKind, sessionHash, groupID, apiKeyGroupStickyTTL(seconds))
}
