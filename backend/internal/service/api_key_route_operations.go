package service

import (
	"context"
	"errors"
)

var (
	ErrAPIKeyRouteOperationInvalid = errors.New("invalid API key route operation")
	ErrAPIKeyRouteVersionStale     = errors.New("API key route version is stale")
)

type APIKeyRouteBreakerSnapshot struct {
	State              string `json:"state"`
	Successes          int64  `json:"successes"`
	Failures           int64  `json:"failures"`
	RecoverySuccesses  int64  `json:"recovery_successes"`
	RecoveryAdmissions int64  `json:"recovery_admissions"`
	OpenedAtUnixMS     int64  `json:"opened_at_unix_ms,omitempty"`
}

type APIKeyRouteBreakerOperationsCache interface {
	LoadAPIKeyRouteBreakers(ctx context.Context, keys []string) ([]APIKeyRouteBreakerSnapshot, error)
	DeleteAPIKeyRouteBreakers(ctx context.Context, keys []string) (int64, error)
}

type APIKeyRouteOperationsService struct {
	apiKeys *APIKeyService
	cache   GatewayCache
}

func NewAPIKeyRouteOperationsService(apiKeys *APIKeyService, cache GatewayCache) *APIKeyRouteOperationsService {
	return &APIKeyRouteOperationsService{apiKeys: apiKeys, cache: cache}
}

func apiKeyRouteConfiguredGroupIDs(apiKey *APIKey) []int64 {
	if apiKey == nil {
		return nil
	}
	result := make([]int64, 0, len(apiKey.GroupRoutes))
	for _, route := range apiKey.GroupRoutes {
		if route.GroupID > 0 {
			result = append(result, route.GroupID)
		}
	}
	if len(result) == 0 && apiKey.GroupID != nil && *apiKey.GroupID > 0 {
		result = append(result, *apiKey.GroupID)
	}
	return result
}

func containsAPIKeyRouteGroup(groups []int64, target int64) bool {
	for _, groupID := range groups {
		if groupID == target {
			return true
		}
	}
	return false
}
