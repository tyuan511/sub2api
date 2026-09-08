package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryNewKeySuccessThresholdDefault(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "new-key-default@test.com")
	for _, test := range []struct{ stored, expected int }{{0, 80}, {50, 50}, {95, 95}} {
		key := &service.APIKey{UserID: user.ID, Key: fmt.Sprintf("sk-default-%d", test.stored),
			Name: "default", Status: service.StatusActive, RoutingMinSuccessRate: test.stored}
		require.NoError(t, repo.Create(ctx, key))
		require.Equal(t, test.expected, key.RoutingMinSuccessRate)
		stored, err := repo.GetByKeyForAuth(ctx, key.Key)
		require.NoError(t, err)
		require.Equal(t, test.expected, stored.RoutingMinSuccessRate)
	}
	// Direct Ent creates must agree with the API and repository defaults.
	key, err := client.APIKey.Create().SetUserID(user.ID).SetKey("sk-ent-default").SetName("ent-default").Save(ctx)
	require.NoError(t, err)
	require.Equal(t, 80, key.RoutingMinSuccessRate)
}

func TestAPIKeyRepositoryRoutingControlsRoundTripAndStateVersion(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "key-controls@test.com")
	group, err := client.Group.Create().SetName("controls").SetPlatform(service.PlatformOpenAI).Save(ctx)
	require.NoError(t, err)
	pref, balance := service.APIKeySmartPreferencePrice, 3000
	key := &service.APIKey{UserID: user.ID, Key: "sk-controls", Name: "controls", Status: service.StatusActive,
		GroupID: &group.ID, ScheduleMode: service.APIKeyScheduleModeSmart, SmartPreference: &pref,
		SmartBalanceBPS: &balance, RoutingMinSuccessRate: 85, RouteVersion: 4, RoutingStateVersion: 2,
		GroupRoutes: []service.APIKeyGroupRoute{{GroupID: group.ID, Priority: 0, Enabled: true}}}
	require.NoError(t, repo.Create(ctx, key))
	rawKey := key.Key
	key, err = repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	// Auth projections intentionally do not include the credential. Normal
	// control-plane edits use GetByID, which includes it for invalidation.
	key.Key = rawKey
	require.Equal(t, 3000, *key.SmartBalanceBPS)
	require.Equal(t, 85, key.RoutingMinSuccessRate)
	require.Equal(t, int64(2), key.RoutingStateVersion)
	version := key.RouteVersion
	updatedBalance := 0
	key.SmartBalanceBPS, key.RoutingMinSuccessRate = &updatedBalance, 95
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{Routing: &service.APIKeyRoutingMutation{
		ExpectedRouteVersion: &version, Routes: key.GroupRoutes, PreserveRuntimeState: true}}))
	require.Equal(t, int64(5), key.RouteVersion)
	require.Equal(t, int64(2), key.RoutingStateVersion)
	read, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, 0, *read.SmartBalanceBPS)
	require.Equal(t, 95, read.RoutingMinSuccessRate)
	version = key.RouteVersion
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{Routing: &service.APIKeyRoutingMutation{
		ExpectedRouteVersion: &version, Routes: key.GroupRoutes}}))
	require.Equal(t, int64(6), key.RouteVersion)
	require.Equal(t, int64(3), key.RoutingStateVersion)
}

func TestRoutingFactPersistsDecisionTimeControls(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	repo := &routingOptimizationRepository{client: client}
	ctx := context.Background()
	pref, balance := service.APIKeySmartPreferencePrice, 3000
	fact := &service.RoutingAttemptFact{
		EventID: "controls-event", RoutingDecisionID: "controls-decision", RouteVersion: 9,
		ScheduleMode: service.APIKeyScheduleModeSmart, SmartPreference: &pref, SmartBalanceBPS: &balance,
		RoutingMinSuccessRate: 85, RoutingStateVersion: 3,
		Platform: service.PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses",
		StrategyVersion: "strategy-v1", ScoreVersion: "score-v1", FeatureSchemaVersion: "feature-v1",
		SampleProbability: 1, AssignmentReason: service.RoutingAssignmentDeterministic,
		OutcomeVisibility: service.RoutingOutcomeObserved, EventPriority: service.RoutingEventPriorityDiagnostic, OccurredAt: time.Now(),
	}
	require.NoError(t, repo.CreateRoutingAttempts(ctx, []*service.RoutingAttemptFact{fact}))
	stored, err := client.RoutingAttempt.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, 3000, *stored.SmartBalanceBps)
	require.Equal(t, 85, stored.RoutingMinSuccessRate)
	require.Equal(t, int64(3), *stored.RoutingStateVersion)
}
