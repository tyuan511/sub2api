package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/apikeyrouteconfigoutbox"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryRoutingCreateAndCASUpdateSQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "api-key-routing@test.com")
	primary, err := client.Group.Create().
		SetName("routing-primary").
		SetPlatform(service.PlatformOpenAI).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	fallback, err := client.Group.Create().
		SetName("routing-fallback").
		SetPlatform(service.PlatformOpenAI).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	pref := service.APIKeySmartPreferenceBalanced
	primaryID := primary.ID
	key := &service.APIKey{
		UserID:          user.ID,
		Key:             "sk-routing-cas",
		Name:            "routing",
		GroupID:         &primaryID,
		ScheduleMode:    service.APIKeyScheduleModeSmart,
		SmartPreference: &pref,
		RouteVersion:    1,
		Status:          service.StatusActive,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: primary.ID, Priority: 0, Enabled: true},
			{GroupID: fallback.ID, Priority: 1, Enabled: true},
		},
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, &primaryID, got.GroupID)
	require.Equal(t, service.APIKeyScheduleModeSmart, got.ScheduleMode)
	require.Equal(t, &pref, got.SmartPreference)
	require.Equal(t, int64(1), got.RouteVersion)
	require.Equal(t, int64(1), got.RoutingDependencyVersion)
	require.Len(t, got.GroupRoutes, 2)
	require.Equal(t, fallback.ID, got.GroupRoutes[1].GroupID)
	require.Equal(t, "routing-fallback", got.GroupRoutes[1].Group.Name)

	outboxCount, err := client.APIKeyRouteConfigOutbox.Query().
		Where(apikeyrouteconfigoutbox.APIKeyIDEQ(key.ID)).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, outboxCount)
	createdOutbox, err := client.APIKeyRouteConfigOutbox.Query().
		Where(apikeyrouteconfigoutbox.APIKeyIDEQ(key.ID)).
		Only(ctx)
	require.NoError(t, err)
	var createdPayload map[string]any
	require.NoError(t, json.Unmarshal(createdOutbox.Payload, &createdPayload))
	digest := sha256.Sum256([]byte(key.Key))
	require.Equal(t, fmt.Sprintf("%x", digest[:]), createdPayload["auth_cache_key"])
	require.Equal(t, float64(0), createdPayload["old_route_version"])
	require.Equal(t, float64(0), createdPayload["old_dependency_version"])
	require.Equal(t, float64(1), createdPayload["dependency_version"])
	require.NotContains(t, string(createdOutbox.Payload), key.Key)

	expected := int64(1)
	fallbackID := fallback.ID
	got.GroupID = &fallbackID
	got.ScheduleMode = service.APIKeyScheduleModeSequential
	got.SmartPreference = nil
	routes := []service.APIKeyGroupRoute{
		{GroupID: fallback.ID, Priority: 0, Enabled: true},
		{GroupID: primary.ID, Priority: 1, Enabled: true},
	}
	require.NoError(t, repo.Update(ctx, got, service.APIKeyUpdateFields{
		GroupID: true,
		Routing: &service.APIKeyRoutingMutation{
			ExpectedRouteVersion: &expected,
			Routes:               routes,
		},
	}))
	require.Equal(t, int64(2), got.RouteVersion)

	updated, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, &fallbackID, updated.GroupID)
	require.Equal(t, int64(2), updated.RouteVersion)
	require.Equal(t, fallback.ID, updated.GroupRoutes[0].GroupID)
	require.Equal(t, primary.ID, updated.GroupRoutes[1].GroupID)

	stale := int64(1)
	err = repo.Update(ctx, updated, service.APIKeyUpdateFields{
		GroupID: true,
		Routing: &service.APIKeyRoutingMutation{
			ExpectedRouteVersion: &stale,
			Routes:               routes,
		},
	})
	require.ErrorIs(t, err, service.ErrAPIKeyRouteConflict)

	outboxCount, err = client.APIKeyRouteConfigOutbox.Query().
		Where(apikeyrouteconfigoutbox.APIKeyIDEQ(key.ID)).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, outboxCount)
	updatedOutboxes, err := client.APIKeyRouteConfigOutbox.Query().
		Where(apikeyrouteconfigoutbox.APIKeyIDEQ(key.ID)).
		All(ctx)
	require.NoError(t, err)
	var updatedPayload map[string]any
	for _, event := range updatedOutboxes {
		if event.RouteVersion == 2 {
			require.NoError(t, json.Unmarshal(event.Payload, &updatedPayload))
		}
	}
	require.Equal(t, float64(1), updatedPayload["old_route_version"])
	require.Equal(t, float64(1), updatedPayload["old_dependency_version"])
	require.Equal(t, float64(1), updatedPayload["dependency_version"])
}
