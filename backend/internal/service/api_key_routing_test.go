package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func routeInputs(items ...APIKeyGroupRouteInput) *[]APIKeyGroupRouteInput { return &items }

func TestNormalizeCreateAPIKeyRoutingPreservesLegacyUnscopedKey(t *testing.T) {
	routing, err := normalizeCreateAPIKeyRouting(CreateAPIKeyRequest{})
	require.NoError(t, err)
	require.True(t, routing.LegacyUnscoped)
	require.Empty(t, routing.Routes)
	require.Equal(t, APIKeyScheduleModeSequential, routing.ScheduleMode)
}

func TestNormalizeCreateAPIKeyRoutingLegacyGroup(t *testing.T) {
	groupID := int64(42)
	routing, err := normalizeCreateAPIKeyRouting(CreateAPIKeyRequest{GroupID: &groupID})
	require.NoError(t, err)
	require.False(t, routing.LegacyUnscoped)
	require.Equal(t, []APIKeyGroupRoute{{GroupID: 42, Priority: 0, Enabled: true}}, routing.Routes)
	require.Equal(t, APIKeyScheduleModeSequential, routing.ScheduleMode)
}

func TestNormalizeCreateAPIKeyRoutingRejectsMismatchAndInvalidOrder(t *testing.T) {
	legacy := int64(7)
	_, err := normalizeCreateAPIKeyRouting(CreateAPIKeyRequest{
		GroupID: &legacy,
		GroupRoutes: routeInputs(
			APIKeyGroupRouteInput{GroupID: 8, Priority: 0},
		),
	})
	require.ErrorIs(t, err, ErrAPIKeyRouteMismatch)

	_, err = normalizeCreateAPIKeyRouting(CreateAPIKeyRequest{
		GroupRoutes: routeInputs(
			APIKeyGroupRouteInput{GroupID: 7, Priority: 0},
			APIKeyGroupRouteInput{GroupID: 8, Priority: 2},
		),
	})
	require.ErrorIs(t, err, ErrAPIKeyRoutesInvalid)
}

func TestNormalizeCreateAPIKeyRoutingSmartRequiresPreference(t *testing.T) {
	mode := APIKeyScheduleModeSmart
	_, err := normalizeCreateAPIKeyRouting(CreateAPIKeyRequest{
		GroupRoutes:  routeInputs(APIKeyGroupRouteInput{GroupID: 7, Priority: 0}),
		ScheduleMode: &mode,
	})
	require.ErrorIs(t, err, ErrAPIKeyRoutesInvalid)

	pref := APIKeySmartPreferencePrice
	routing, err := normalizeCreateAPIKeyRouting(CreateAPIKeyRequest{
		GroupRoutes:     routeInputs(APIKeyGroupRouteInput{GroupID: 7, Priority: 0}),
		ScheduleMode:    &mode,
		SmartPreference: &pref,
	})
	require.NoError(t, err)
	require.Equal(t, &pref, routing.SmartPreference)
}

type apiKeyRoutingGroupRepo struct {
	groups map[int64]*Group
	GroupRepository
}

func (r *apiKeyRoutingGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	group, ok := r.groups[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

func TestValidateAPIKeyRouteGroupsRequiresSamePlatformAndBillingType(t *testing.T) {
	user := &User{ID: 1}
	routes := []APIKeyGroupRoute{
		{GroupID: 1, Priority: 0, Enabled: true},
		{GroupID: 2, Priority: 1, Enabled: true},
	}

	svc := &APIKeyService{groupRepo: &apiKeyRoutingGroupRepo{groups: map[int64]*Group{
		1: {ID: 1, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeStandard},
		2: {ID: 2, Platform: PlatformAnthropic, SubscriptionType: SubscriptionTypeStandard},
	}}}
	_, err := svc.validateAPIKeyRouteGroups(context.Background(), user, routes)
	require.Equal(t, "API_KEY_ROUTE_PLATFORM_MISMATCH", infraerrors.Reason(err))

	svc.groupRepo = &apiKeyRoutingGroupRepo{groups: map[int64]*Group{
		1: {ID: 1, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeStandard},
		2: {ID: 2, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription},
	}}
	_, err = svc.validateAPIKeyRouteGroups(context.Background(), user, routes)
	require.Equal(t, "API_KEY_ROUTE_BILLING_MISMATCH", infraerrors.Reason(err))
}

func TestAPIKeyAuthSnapshotMultiGroupRoutingRoundTrip(t *testing.T) {
	primaryID := int64(10)
	pref := APIKeySmartPreferenceBalanced
	key := &APIKey{
		ID: 1, UserID: 2, Key: "sk-routes", GroupID: &primaryID,
		ScheduleMode: APIKeyScheduleModeSmart, SmartPreference: &pref, RouteVersion: 4,
		SmartBalanceBPS: routingControlInt(3000), RoutingMinSuccessRate: 85, RoutingStateVersion: 2,
		User:  &User{ID: 2, Status: StatusActive},
		Group: &Group{ID: 10, Name: "primary", Platform: PlatformOpenAI, Status: StatusActive},
		GroupRoutes: []APIKeyGroupRoute{
			{GroupID: 10, Priority: 0, Enabled: true, Group: &Group{ID: 10, Name: "primary", Platform: PlatformOpenAI, Status: StatusActive}},
			{GroupID: 20, Priority: 1, Enabled: true, Group: &Group{ID: 20, Name: "fallback", Platform: PlatformOpenAI, Status: StatusActive}},
		},
	}
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)

	restored := svc.snapshotToAPIKey(key.Key, snapshot)
	require.Equal(t, int64(4), restored.RouteVersion)
	require.Equal(t, int64(2), restored.RoutingStateVersion)
	require.Equal(t, 85, restored.RoutingMinSuccessRate)
	require.Equal(t, 3000, *restored.SmartBalanceBPS)
	*key.SmartBalanceBPS = 9000
	require.Equal(t, 3000, *restored.SmartBalanceBPS, "cached balance is not a shared mutable pointer")
	require.Equal(t, APIKeyScheduleModeSmart, restored.ScheduleMode)
	require.Equal(t, &pref, restored.SmartPreference)
	require.Len(t, restored.GroupRoutes, 2)
	require.Equal(t, int64(20), restored.GroupRoutes[1].GroupID)
	require.Equal(t, "fallback", restored.GroupRoutes[1].Group.Name)
}
