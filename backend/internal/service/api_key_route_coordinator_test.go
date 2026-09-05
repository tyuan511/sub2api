package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func routeTestGroup(id int64, platform, billing, status string) *Group {
	return &Group{ID: id, Name: "group", Platform: platform, SubscriptionType: billing, Status: status}
}

func TestAPIKeyRouteCoordinator_DisabledPreservesLegacyProjection(t *testing.T) {
	group := routeTestGroup(10, PlatformOpenAI, SubscriptionTypeStandard, StatusActive)
	groupID := group.ID
	key := &APIKey{
		ID: 3, GroupID: &groupID, Group: group, RouteVersion: 9,
		GroupRoutes: []APIKeyGroupRoute{
			{GroupID: 10, Priority: 0, Enabled: true, Group: group},
			{GroupID: 20, Priority: 1, Enabled: true, Group: routeTestGroup(20, PlatformOpenAI, SubscriptionTypeStandard, StatusActive)},
		},
	}

	plan, err := NewAPIKeyRouteCoordinator(false).BuildPlan(key, nil)
	require.NoError(t, err)
	require.False(t, plan.RoutingEnabled)
	require.Len(t, plan.Candidates, 1)
	require.Equal(t, int64(10), plan.Candidates[0].GroupID)
}

func TestAPIKeyRouteCoordinator_PreservesLegacyUnscopedKey(t *testing.T) {
	plan, err := NewAPIKeyRouteCoordinator(true).BuildPlan(&APIKey{ID: 4}, nil)
	require.NoError(t, err)
	require.True(t, plan.LegacyUnscoped)
	require.Empty(t, plan.Candidates)
}

func TestAPIKeyRouteCoordinator_SequentialHardFiltersAndClonesActualGroup(t *testing.T) {
	first := routeTestGroup(10, PlatformOpenAI, SubscriptionTypeStandard, StatusActive)
	second := routeTestGroup(20, PlatformOpenAI, SubscriptionTypeStandard, StatusActive)
	inactive := routeTestGroup(30, PlatformOpenAI, SubscriptionTypeStandard, "disabled")
	wrongPlatform := routeTestGroup(40, PlatformAnthropic, SubscriptionTypeStandard, StatusActive)
	firstID := first.ID
	key := &APIKey{
		ID: 5, GroupID: &firstID, Group: first, RouteVersion: 11, ScheduleMode: APIKeyScheduleModeSequential,
		GroupRoutes: []APIKeyGroupRoute{
			{GroupID: 20, Priority: 1, Enabled: true, Group: second},
			{GroupID: 10, Priority: 0, Enabled: true, Group: first},
			{GroupID: 30, Priority: 2, Enabled: true, Group: inactive},
			{GroupID: 40, Priority: 3, Enabled: true, Group: wrongPlatform},
		},
	}

	plan, err := NewAPIKeyRouteCoordinator(true).BuildPlan(key, func(group *Group) (bool, string) {
		if group.ID == second.ID {
			return false, "model_unsupported"
		}
		return true, ""
	})
	require.NoError(t, err)
	require.Equal(t, int64(11), plan.RouteVersion)
	require.Equal(t, []APIKeyRouteCandidate{{GroupID: 10, Priority: 0, Group: first}}, plan.Candidates)
	require.Equal(t, []APIKeyRouteExclusion{
		{GroupID: 20, Priority: 1, Reason: "model_unsupported"},
		{GroupID: 30, Priority: 2, Reason: "group_inactive"},
		{GroupID: 40, Priority: 3, Reason: "platform_mismatch"},
	}, plan.Excluded)

	actual, ok := plan.APIKeyForCandidate(0)
	require.True(t, ok)
	require.NotSame(t, key, actual)
	require.Equal(t, first.ID, *actual.GroupID)
	require.Same(t, first, actual.Group)
	require.Equal(t, second.ID, key.GroupRoutes[0].GroupID, "the auth snapshot must not be mutated")
}

func TestAPIKeyRouteCoordinator_ExplicitEmptyResultFailsClosed(t *testing.T) {
	group := routeTestGroup(10, PlatformOpenAI, SubscriptionTypeStandard, "disabled")
	groupID := group.ID
	key := &APIKey{ID: 6, GroupID: &groupID, Group: group, GroupRoutes: []APIKeyGroupRoute{{GroupID: groupID, Priority: 0, Enabled: true, Group: group}}}

	_, err := NewAPIKeyRouteCoordinator(true).BuildPlan(key, nil)
	require.ErrorIs(t, err, ErrNoEligibleAPIKeyRoute)
}

func TestAPIKeyRouteCoordinator_RejectsMalformedRouteSet(t *testing.T) {
	group := routeTestGroup(10, PlatformOpenAI, SubscriptionTypeStandard, StatusActive)
	groupID := group.ID
	key := &APIKey{ID: 7, GroupID: &groupID, Group: group, GroupRoutes: []APIKeyGroupRoute{{GroupID: groupID, Priority: 1, Enabled: true, Group: group}}}

	_, err := NewAPIKeyRouteCoordinator(true).BuildPlan(key, nil)
	require.True(t, errors.Is(err, ErrInvalidAPIKeyRouteSet))
}

var apiKeyRouteBenchmarkPlan *APIKeyRoutePlan

func BenchmarkAPIKeyRouteCoordinatorBuildPlan(b *testing.B) {
	benchmarkKey := func(candidateCount int) *APIKey {
		groups := make([]*Group, candidateCount)
		routes := make([]APIKeyGroupRoute, candidateCount)
		for i := range candidateCount {
			groups[i] = &Group{
				ID:               int64(i + 1),
				Name:             fmt.Sprintf("group-%d", i+1),
				Platform:         PlatformOpenAI,
				SubscriptionType: SubscriptionTypeStandard,
				Status:           StatusActive,
			}
			routes[i] = APIKeyGroupRoute{
				GroupID:  groups[i].ID,
				Priority: i,
				Enabled:  true,
				Group:    groups[i],
			}
		}
		firstID := groups[0].ID
		return &APIKey{
			ID:           99,
			GroupID:      &firstID,
			Group:        groups[0],
			RouteVersion: 7,
			ScheduleMode: APIKeyScheduleModeSequential,
			GroupRoutes:  routes,
		}
	}

	for _, tc := range []struct {
		name           string
		enabled        bool
		candidateCount int
	}{
		{name: "feature_disabled_single_group", enabled: false, candidateCount: 1},
		{name: "feature_enabled_single_group", enabled: true, candidateCount: 1},
		{name: "feature_enabled_eight_groups", enabled: true, candidateCount: DefaultMaxAPIKeyGroupRoutes},
	} {
		b.Run(tc.name, func(b *testing.B) {
			coordinator := NewAPIKeyRouteCoordinator(tc.enabled)
			key := benchmarkKey(tc.candidateCount)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				plan, err := coordinator.BuildPlan(key, nil)
				if err != nil {
					b.Fatal(err)
				}
				apiKeyRouteBenchmarkPlan = plan
			}
		})
	}
}
