package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type normalizedAPIKeyRouting struct {
	Routes          []APIKeyGroupRoute
	ScheduleMode    string
	SmartPreference *string
	SmartBalanceBPS *int
	MinSuccessRate  int
	LegacyUnscoped  bool
}

func normalizeCreateAPIKeyRouting(req CreateAPIKeyRequest) (normalizedAPIKeyRouting, error) {
	if err := ValidateAPIKeyRoutingControls(req.SmartBalanceBPS, req.RoutingMinSuccessRate); err != nil {
		return normalizedAPIKeyRouting{}, err
	}
	mode := APIKeyScheduleModeSequential
	if req.ScheduleMode != nil {
		mode = strings.TrimSpace(*req.ScheduleMode)
	}

	// Preserve the pre-existing null group_id contract only when the caller did
	// not opt into the new route-set fields.
	if req.GroupRoutes == nil && req.GroupID == nil && req.ScheduleMode == nil && req.SmartPreference == nil && req.SmartBalanceBPS == nil && req.RoutingMinSuccessRate == nil {
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
	preference := req.SmartPreference
	if req.SmartBalanceBPS != nil && mode == APIKeyScheduleModeSmart {
		value := APIKeyRoutingBalancePreference(*req.SmartBalanceBPS)
		preference = &value
	}
	pref, err := normalizeAPIKeyRoutingPolicy(mode, preference)
	if err != nil {
		return normalizedAPIKeyRouting{}, err
	}
	balance := cloneIntPtr(req.SmartBalanceBPS)
	if mode != APIKeyScheduleModeSmart {
		balance = nil
	}
	minimum := DefaultNewAPIKeyRoutingMinSuccessRate
	if req.RoutingMinSuccessRate != nil {
		minimum = *req.RoutingMinSuccessRate
	}
	return normalizedAPIKeyRouting{Routes: routes, ScheduleMode: mode, SmartPreference: pref, SmartBalanceBPS: balance, MinSuccessRate: minimum}, nil
}

func normalizeUpdateAPIKeyRouting(current *APIKey, req UpdateAPIKeyRequest) (normalizedAPIKeyRouting, bool, error) {
	if err := ValidateAPIKeyRoutingControls(req.SmartBalanceBPS, req.RoutingMinSuccessRate); err != nil {
		return normalizedAPIKeyRouting{}, false, err
	}
	changed := req.GroupRoutes != nil || req.GroupID != nil || req.ScheduleMode != nil || req.SmartPreference != nil || req.SmartBalanceBPS != nil || req.RoutingMinSuccessRate != nil
	if !changed {
		return normalizedAPIKeyRouting{}, false, nil
	}
	if current == nil {
		return normalizedAPIKeyRouting{}, false, ErrAPIKeyNotFound
	}

	mode := current.ScheduleMode
	if mode == "" {
		mode = APIKeyScheduleModeSequential
	}
	pref := current.SmartPreference
	balance := cloneIntPtr(current.SmartBalanceBPS)
	minimum := current.EffectiveRoutingMinSuccessRate()
	if req.RoutingMinSuccessRate != nil {
		minimum = *req.RoutingMinSuccessRate
	}
	if req.ScheduleMode != nil {
		mode = strings.TrimSpace(*req.ScheduleMode)
		if mode == APIKeyScheduleModeSequential && req.SmartPreference == nil {
			pref = nil
		}
	}
	if req.SmartPreference != nil {
		pref = req.SmartPreference
		// An old client explicitly selecting a preset restores that preset.
		balance = nil
	}
	if req.SmartBalanceBPS != nil {
		balance = cloneIntPtr(req.SmartBalanceBPS)
	}

	var routes []APIKeyGroupRoute
	if req.GroupRoutes != nil || req.GroupID != nil {
		var err error
		routes, err = normalizeAPIKeyRouteInputs(req.GroupRoutes, req.GroupID)
		if err != nil {
			return normalizedAPIKeyRouting{}, false, err
		}
		// A legacy group_id-only update has always meant a single explicit group.
		// Normalize its policy as sequential even if the previous route was smart.
		if req.GroupRoutes == nil && req.GroupID != nil && req.ScheduleMode == nil {
			mode = APIKeyScheduleModeSequential
			pref = nil
		}
	} else {
		routes = append([]APIKeyGroupRoute(nil), current.GroupRoutes...)
	}

	if mode == APIKeyScheduleModeSmart && balance != nil {
		value := APIKeyRoutingBalancePreference(*balance)
		pref = &value
	}
	if mode != APIKeyScheduleModeSmart {
		balance = nil
	}
	pref, err := normalizeAPIKeyRoutingPolicy(mode, pref)
	if err != nil {
		return normalizedAPIKeyRouting{}, false, err
	}
	if len(routes) == 0 {
		return normalizedAPIKeyRouting{}, false, fmt.Errorf("%w: an explicit route configuration requires at least one group", ErrAPIKeyRoutesInvalid)
	}
	return normalizedAPIKeyRouting{Routes: routes, ScheduleMode: mode, SmartPreference: pref, SmartBalanceBPS: balance, MinSuccessRate: minimum}, true, nil
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

func normalizeAPIKeyRoutingPolicy(mode string, preference *string) (*string, error) {
	switch mode {
	case APIKeyScheduleModeSequential:
		if preference != nil && strings.TrimSpace(*preference) != "" {
			return nil, fmt.Errorf("%w: smart_preference must be empty for sequential mode", ErrAPIKeyRoutesInvalid)
		}
		return nil, nil
	case APIKeyScheduleModeSmart:
		if preference == nil {
			return nil, fmt.Errorf("%w: smart_preference is required for smart mode", ErrAPIKeyRoutesInvalid)
		}
		value := strings.TrimSpace(*preference)
		switch value {
		case APIKeySmartPreferencePrice, APIKeySmartPreferenceSpeed, APIKeySmartPreferenceBalanced:
			return &value, nil
		default:
			return nil, fmt.Errorf("%w: unsupported smart_preference %q", ErrAPIKeyRoutesInvalid, value)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported schedule_mode %q", ErrAPIKeyRoutesInvalid, mode)
	}
}

func (s *APIKeyService) validateAPIKeyRouteGroups(ctx context.Context, user *User, routes []APIKeyGroupRoute) ([]APIKeyGroupRoute, error) {
	if len(routes) == 0 {
		return nil, nil
	}
	validated := make([]APIKeyGroupRoute, len(routes))
	copy(validated, routes)
	var platform, subscriptionType string
	for i := range validated {
		group, err := s.groupRepo.GetByID(ctx, validated[i].GroupID)
		if err != nil {
			return nil, fmt.Errorf("get group %d: %w", validated[i].GroupID, err)
		}
		if i == 0 {
			platform = group.Platform
			subscriptionType = group.SubscriptionType
		} else if group.Platform != platform {
			return nil, ErrAPIKeyRoutePlatform
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
