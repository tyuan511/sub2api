package service

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	// ErrNoEligibleAPIKeyRoute is returned when an explicit route set exists but
	// every candidate was rejected. Callers must fail closed instead of passing a
	// nil group to the legacy global account pool.
	ErrNoEligibleAPIKeyRoute = errors.New("no eligible api key group route")
	ErrInvalidAPIKeyRouteSet = errors.New("invalid api key group route set")
)

// APIKeyRouteEligibility applies request-specific hard filters (model,
// endpoint, billing capability, and similar checks) before a group can enter
// the route plan. An empty reason is normalized for diagnostics.
type APIKeyRouteEligibility func(group *Group) (allowed bool, reason string)

// APIKeyRouteCandidate is one immutable candidate in a request route plan.
type APIKeyRouteCandidate struct {
	GroupID  int64
	Priority int
	Group    *Group
}

// APIKeyRouteExclusion records why a configured candidate was not admitted.
type APIKeyRouteExclusion struct {
	GroupID  int64
	Priority int
	Reason   string
}

// APIKeyRoutePlan is fixed for the lifetime of one request. Later score
// refreshes therefore cannot reorder an in-flight retry chain.
type APIKeyRoutePlan struct {
	APIKeyID              int64
	RouteVersion          int64
	ScheduleMode          string
	SmartPreference       *string
	SmartBalanceBPS       *int
	RoutingMinSuccessRate int
	RoutingStateVersion   int64
	Candidates            []APIKeyRouteCandidate
	Excluded              []APIKeyRouteExclusion
	LegacyUnscoped        bool
	RoutingEnabled        bool

	apiKey *APIKey
}

// APIKeyRouteCoordinator builds request-scoped route plans. The feature flag is
// intentionally captured at construction so the disabled path remains a small,
// deterministic projection of the legacy group_id.
type APIKeyRouteCoordinator struct {
	enabled bool
}

func NewAPIKeyRouteCoordinator(enabled bool) *APIKeyRouteCoordinator {
	return &APIKeyRouteCoordinator{enabled: enabled}
}

func (c *APIKeyRouteCoordinator) Enabled() bool { return c != nil && c.enabled }

// BuildPlan validates and freezes the candidates visible at request start.
// Smart mode currently uses the deterministic user order as its safe baseline;
// a versioned score snapshot can replace that ordering without changing this
// plan contract.
func (c *APIKeyRouteCoordinator) BuildPlan(apiKey *APIKey, eligible APIKeyRouteEligibility) (*APIKeyRoutePlan, error) {
	started := time.Now()
	defer func() {
		DefaultRoutingRuntimeMetrics().RecordPhaseLatency(RoutingLatencyPhasePlanBuild, time.Since(started))
	}()
	if apiKey == nil {
		return nil, fmt.Errorf("%w: api key is nil", ErrInvalidAPIKeyRouteSet)
	}
	plan := &APIKeyRoutePlan{
		APIKeyID:              apiKey.ID,
		RouteVersion:          apiKey.RouteVersion,
		ScheduleMode:          apiKey.ScheduleMode,
		SmartPreference:       cloneStringPtr(apiKey.SmartPreference),
		SmartBalanceBPS:       cloneIntPtr(apiKey.SmartBalanceBPS),
		RoutingMinSuccessRate: apiKey.EffectiveRoutingMinSuccessRate(),
		RoutingStateVersion:   apiKey.EffectiveRoutingStateVersion(),
		RoutingEnabled:        c.Enabled() && apiKey.HasMultipleEnabledGroupRoutes(),
		apiKey:                apiKey,
	}
	defer func() {
		DefaultRoutingRuntimeMetrics().RecordPlan(len(plan.Candidates), len(plan.Excluded))
	}()
	if plan.ScheduleMode == "" {
		plan.ScheduleMode = APIKeyScheduleModeSequential
	}
	if !plan.RoutingEnabled {
		plan.ScheduleMode = APIKeyScheduleModeSequential
		plan.SmartPreference = nil
		plan.SmartBalanceBPS = nil
	}

	// Disabled and pre-migration paths are deliberately identical to the old
	// behavior, including the legacy nil group_id meaning "unscoped".
	if !c.Enabled() || len(apiKey.GroupRoutes) == 0 {
		if apiKey.GroupID == nil {
			plan.LegacyUnscoped = true
			return plan, nil
		}
		if *apiKey.GroupID <= 0 {
			return nil, fmt.Errorf("%w: group_id must be positive", ErrInvalidAPIKeyRouteSet)
		}
		plan.Candidates = []APIKeyRouteCandidate{{GroupID: *apiKey.GroupID, Priority: 0, Group: apiKey.Group}}
		return plan, nil
	}

	routes := append([]APIKeyGroupRoute(nil), apiKey.GroupRoutes...)
	if len(routes) > DefaultMaxAPIKeyGroupRoutes {
		return nil, fmt.Errorf("%w: candidate count exceeds %d", ErrInvalidAPIKeyRouteSet, DefaultMaxAPIKeyGroupRoutes)
	}
	sort.SliceStable(routes, func(i, j int) bool { return routes[i].Priority < routes[j].Priority })
	seen := make(map[int64]struct{}, len(routes))
	var routePlatform, subscriptionType string
	for i, route := range routes {
		if route.GroupID <= 0 || route.Priority != i {
			return nil, fmt.Errorf("%w: priorities must be contiguous from zero", ErrInvalidAPIKeyRouteSet)
		}
		if _, exists := seen[route.GroupID]; exists {
			return nil, fmt.Errorf("%w: duplicate group %d", ErrInvalidAPIKeyRouteSet, route.GroupID)
		}
		seen[route.GroupID] = struct{}{}
		if !route.Enabled {
			plan.Excluded = append(plan.Excluded, APIKeyRouteExclusion{GroupID: route.GroupID, Priority: route.Priority, Reason: "route_disabled"})
			continue
		}

		group := route.Group
		if group == nil && apiKey.GroupID != nil && *apiKey.GroupID == route.GroupID {
			group = apiKey.Group
		}
		if group == nil || group.ID != route.GroupID {
			plan.Excluded = append(plan.Excluded, APIKeyRouteExclusion{GroupID: route.GroupID, Priority: route.Priority, Reason: "group_snapshot_missing"})
			continue
		}
		if !group.IsActive() {
			plan.Excluded = append(plan.Excluded, APIKeyRouteExclusion{GroupID: route.GroupID, Priority: route.Priority, Reason: "group_inactive"})
			continue
		}
		if routePlatform == "" {
			routePlatform = group.Platform
			subscriptionType = group.SubscriptionType
		} else if group.Platform != routePlatform {
			plan.Excluded = append(plan.Excluded, APIKeyRouteExclusion{GroupID: route.GroupID, Priority: route.Priority, Reason: "platform_mismatch"})
			continue
		} else if group.SubscriptionType != subscriptionType {
			plan.Excluded = append(plan.Excluded, APIKeyRouteExclusion{GroupID: route.GroupID, Priority: route.Priority, Reason: "billing_type_mismatch"})
			continue
		}
		if eligible != nil {
			allowed, reason := eligible(group)
			if !allowed {
				if reason == "" {
					reason = "request_ineligible"
				}
				plan.Excluded = append(plan.Excluded, APIKeyRouteExclusion{GroupID: route.GroupID, Priority: route.Priority, Reason: reason})
				continue
			}
		}
		plan.Candidates = append(plan.Candidates, APIKeyRouteCandidate{GroupID: route.GroupID, Priority: route.Priority, Group: group})
	}
	if len(plan.Candidates) == 0 {
		return nil, ErrNoEligibleAPIKeyRoute
	}
	return plan, nil
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (p *APIKeyRoutePlan) Len() int {
	if p == nil {
		return 0
	}
	return len(p.Candidates)
}

// APIKeyForCandidate returns a shallow request-local API key projection whose
// GroupID and Group point at the actual route candidate. The cached auth object
// is never mutated.
func (p *APIKeyRoutePlan) APIKeyForCandidate(index int) (*APIKey, bool) {
	if p == nil || p.apiKey == nil || index < 0 || index >= len(p.Candidates) {
		return nil, false
	}
	candidate := p.Candidates[index]
	copy := *p.apiKey
	groupID := candidate.GroupID
	copy.GroupID = &groupID
	copy.Group = candidate.Group
	// The auth snapshot's group-specific RPM override belongs to the legacy
	// mirrored (first) group. A secondary candidate must resolve its own
	// override by (user, group) instead of reusing that pointer.
	if copy.User != nil && (p.apiKey.GroupID == nil || *p.apiKey.GroupID != groupID) {
		userCopy := *copy.User
		userCopy.UserGroupRPMOverride = nil
		copy.User = &userCopy
	}
	return &copy, true
}
