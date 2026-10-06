package service

import (
	"context"
	"fmt"
	"sort"
)

type normalizedAPIKeyRouting struct {
	Routes         []APIKeyGroupRoute
	ScheduleMode   string
	MinSuccessRate int
	LegacyUnscoped bool
}

// API key failover is always a fixed sequential order. Smart routing has been
// removed, so every write path normalizes to sequential and drops the legacy
// smart policy fields.
func normalizeCreateAPIKeyRouting(req CreateAPIKeyRequest) (normalizedAPIKeyRouting, error) {
	if err := ValidateAPIKeyRoutingControls(req.RoutingMinSuccessRate); err != nil {
		return normalizedAPIKeyRouting{}, err
	}

	// Preserve the pre-existing null group_id contract only when the caller did
	// not opt into the new route-set fields.
	if req.GroupRoutes == nil && req.GroupID == nil && req.RoutingMinSuccessRate == nil {
		return normalizedAPIKeyRouting{
			ScheduleMode:   APIKeyScheduleModeSequential,
			LegacyUnscoped: true,
			MinSuccessRate: DefaultNewAPIKeyRoutingMinSuccessRate,
		}, nil
	}

	routes, err := normalizeAPIKeyRouteInputs(req.GroupRoutes, req.GroupID)
	if err != nil {
		return normalizedAPIKeyRouting{}, err
	}
	minimum := DefaultNewAPIKeyRoutingMinSuccessRate
	if req.RoutingMinSuccessRate != nil {
		minimum = *req.RoutingMinSuccessRate
	}
	return normalizedAPIKeyRouting{Routes: routes, ScheduleMode: APIKeyScheduleModeSequential, MinSuccessRate: minimum}, nil
}

func normalizeUpdateAPIKeyRouting(current *APIKey, req UpdateAPIKeyRequest) (normalizedAPIKeyRouting, bool, error) {
	if err := ValidateAPIKeyRoutingControls(req.RoutingMinSuccessRate); err != nil {
		return normalizedAPIKeyRouting{}, false, err
	}
	changed := req.GroupRoutes != nil || req.GroupID != nil || req.RoutingMinSuccessRate != nil
	if !changed {
		return normalizedAPIKeyRouting{}, false, nil
	}
	if current == nil {
		return normalizedAPIKeyRouting{}, false, ErrAPIKeyNotFound
	}

	minimum := current.EffectiveRoutingMinSuccessRate()
	if req.RoutingMinSuccessRate != nil {
		minimum = *req.RoutingMinSuccessRate
	}

	var routes []APIKeyGroupRoute
	if req.GroupRoutes != nil || req.GroupID != nil {
		var err error
		routes, err = normalizeAPIKeyRouteInputs(req.GroupRoutes, req.GroupID)
		if err != nil {
			return normalizedAPIKeyRouting{}, false, err
		}
	} else {
		routes = append([]APIKeyGroupRoute(nil), current.GroupRoutes...)
	}

	if len(routes) == 0 {
		return normalizedAPIKeyRouting{}, false, fmt.Errorf("%w: an explicit route configuration requires at least one group", ErrAPIKeyRoutesInvalid)
	}
	normalized := normalizedAPIKeyRouting{Routes: routes, ScheduleMode: APIKeyScheduleModeSequential, MinSuccessRate: minimum}
	if apiKeyRoutingConfigurationEquivalent(current, normalized) {
		return normalized, false, nil
	}
	return normalized, true, nil
}

func normalizeAPIKeyRouteInputs(inputs *[]APIKeyGroupRouteInput, legacyGroupID *int64) ([]APIKeyGroupRoute, error) {
	if inputs == nil {
		if legacyGroupID == nil || *legacyGroupID <= 0 {
			return nil, fmt.Errorf("%w: group_id must be positive", ErrAPIKeyRoutesInvalid)
		}
		return []APIKeyGroupRoute{{GroupID: *legacyGroupID, Priority: 0, Enabled: true}}, nil
	}
	if len(*inputs) == 0 || len(*inputs) > DefaultMaxAPIKeyGroupRoutes {
		return nil, fmt.Errorf("%w: group_routes must contain 1 to %d groups", ErrAPIKeyRoutesInvalid, DefaultMaxAPIKeyGroupRoutes)
	}

	sorted := append([]APIKeyGroupRouteInput(nil), (*inputs)...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Priority < sorted[j].Priority })
	routes := make([]APIKeyGroupRoute, 0, len(sorted))
	seenGroups := make(map[int64]struct{}, len(sorted))
	for i, input := range sorted {
		if input.GroupID <= 0 || input.Priority != i {
			return nil, fmt.Errorf("%w: priorities must be unique and contiguous from zero", ErrAPIKeyRoutesInvalid)
		}
		if _, exists := seenGroups[input.GroupID]; exists {
			return nil, fmt.Errorf("%w: duplicate group_id %d", ErrAPIKeyRoutesInvalid, input.GroupID)
		}
		seenGroups[input.GroupID] = struct{}{}
		routes = append(routes, APIKeyGroupRoute{GroupID: input.GroupID, Priority: input.Priority, Enabled: true})
	}
	if legacyGroupID != nil && (*legacyGroupID <= 0 || routes[0].GroupID != *legacyGroupID) {
		return nil, ErrAPIKeyRouteMismatch
	}
	return routes, nil
}

func (s *APIKeyService) validateAPIKeyRouteGroups(ctx context.Context, user *User, routes []APIKeyGroupRoute) ([]APIKeyGroupRoute, error) {
	if len(routes) == 0 {
		return nil, nil
	}
	validated := make([]APIKeyGroupRoute, len(routes))
	copy(validated, routes)
	var subscriptionType string
	for i := range validated {
		group, err := s.groupRepo.GetByID(ctx, validated[i].GroupID)
		if err != nil {
			return nil, fmt.Errorf("get group %d: %w", validated[i].GroupID, err)
		}
		if i == 0 {
			subscriptionType = group.SubscriptionType
		} else if group.SubscriptionType != subscriptionType {
			return nil, ErrAPIKeyRouteBilling
		}
		validated[i].Group = group
	}
	for i := range validated {
		if !s.canUserBindGroup(ctx, user, validated[i].Group) {
			return nil, ErrGroupNotAllowed
		}
	}
	return validated, nil
}
