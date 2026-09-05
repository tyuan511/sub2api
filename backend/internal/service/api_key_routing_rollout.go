package service

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrAPIKeyRoutingNotEnabled = infraerrors.Forbidden("API_KEY_ROUTING_NOT_ENABLED", "Multi-group routing is not enabled for this user")

func (s *APIKeyService) SetRoutingRolloutSettings(settings *SettingService) {
	s.routingRolloutSettings = settings
}

func (s *APIKeyService) IsRoutingEnabledForUser(ctx context.Context, userID int64) bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.APIKeyMultiGroupRoutingEnabled && s.routingRolloutSettings.IsAPIKeyRoutingRolloutUser(ctx, userID)
}

func requestsAdvancedAPIKeyRouting(routes *[]APIKeyGroupRouteInput, mode, preference *string, balance, threshold *int) bool {
	return (routes != nil && len(*routes) > 1) || (mode != nil && strings.TrimSpace(*mode) == APIKeyScheduleModeSmart) ||
		(preference != nil && strings.TrimSpace(*preference) != "") || balance != nil || threshold != nil
}

// Project only this request; never mutate the shared auth snapshot or rewrite
// saved fallbacks. Rollout withdrawal pins subsequent requests to group_id,
// not the first currently healthy fallback. Existing streams keep their plan.
func (s *APIKeyService) ProjectAPIKeyRoutingForUser(ctx context.Context, key *APIKey) *APIKey {
	if key == nil || len(key.GroupRoutes) <= 1 || s.IsRoutingEnabledForUser(ctx, key.UserID) {
		return key
	}
	projected := *key
	projected.ScheduleMode = APIKeyScheduleModeSequential
	projected.SmartPreference, projected.SmartBalanceBPS = nil, nil
	if len(key.GroupRoutes) > 0 {
		// A missing/inconsistent primary must fail closed, not become unscoped.
		projected.GroupRoutes = []APIKeyGroupRoute{{Enabled: true}}
		for _, route := range key.GroupRoutes {
			if key.GroupID != nil && route.GroupID == *key.GroupID {
				route.Priority = 0
				projected.GroupRoutes[0] = route
				break
			}
		}
	}
	return &projected
}
