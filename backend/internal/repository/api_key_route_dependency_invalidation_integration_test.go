//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRouteDependencyInvalidation_CoversCandidateAndAccessMutations(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	primary := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name: fmt.Sprintf("route-dependency-primary-%d", suffix), Platform: service.PlatformOpenAI,
		SubscriptionType: service.SubscriptionTypeStandard, Status: service.StatusActive, RateMultiplier: 1,
	})
	fallback := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name: fmt.Sprintf("route-dependency-fallback-%d", suffix), Platform: service.PlatformOpenAI,
		SubscriptionType: service.SubscriptionTypeStandard, Status: service.StatusActive, RateMultiplier: 1,
	})
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("route-dependency-%d@example.com", suffix), Concurrency: 5,
	})
	primaryID := primary.ID
	rawKey := fmt.Sprintf("sk-route-dependency-%d", suffix)
	key := &service.APIKey{
		UserID: user.ID, GroupID: &primaryID, Key: rawKey, Name: "route-dependency", Status: service.StatusActive,
		ScheduleMode: service.APIKeyScheduleModeSequential, RouteVersion: 1,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: primary.ID, Priority: 0, Enabled: true},
			{GroupID: fallback.ID, Priority: 1, Enabled: true},
		},
	}
	repo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	require.NoError(t, repo.Create(ctx, key))

	digestBytes := sha256.Sum256([]byte(rawKey))
	digest := hex.EncodeToString(digestBytes[:])
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM user_allowed_groups WHERE user_id = $1", user.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM api_key_route_config_outbox WHERE api_key_id = $1", key.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = $1", digest)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id = $1", key.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id IN ($1, $2)", primary.ID, fallback.ID)
	})

	readVersions := func() (routeVersion, dependencyVersion int64) {
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT route_version, routing_dependency_version FROM api_keys WHERE id = $1", key.ID).
			Scan(&routeVersion, &dependencyVersion))
		return routeVersion, dependencyVersion
	}
	assertDependencyEvent := func(routeVersion, oldDependencyVersion, dependencyVersion int64) {
		var (
			eventType       string
			payloadRoute    int64
			payloadOldDep   int64
			payloadNewDep   int64
			payloadCacheKey string
		)
		eventKey := fmt.Sprintf("api_key_dependency:%d:v%d", key.ID, dependencyVersion)
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT event_type,
			       (payload->>'route_version')::bigint,
			       (payload->>'old_dependency_version')::bigint,
			       (payload->>'dependency_version')::bigint,
			       payload->>'auth_cache_key'
			FROM api_key_route_config_outbox
			WHERE event_key = $1`, eventKey).
			Scan(&eventType, &payloadRoute, &payloadOldDep, &payloadNewDep, &payloadCacheKey))
		require.Equal(t, "api_key_route_dependency_changed", eventType)
		require.Equal(t, routeVersion, payloadRoute)
		require.Equal(t, oldDependencyVersion, payloadOldDep)
		require.Equal(t, dependencyVersion, payloadNewDep)
		require.Equal(t, digest, payloadCacheKey)
		require.NotContains(t, payloadCacheKey, rawKey)
	}

	routeVersion, dependencyVersion := readVersions()
	require.Equal(t, int64(1), routeVersion)
	require.Equal(t, int64(1), dependencyVersion)

	_, err := integrationDB.ExecContext(ctx, "UPDATE groups SET rate_multiplier = 1.25 WHERE id = $1", fallback.ID)
	require.NoError(t, err)
	unchangedRoute, afterCandidateChange := readVersions()
	require.Equal(t, routeVersion, unchangedRoute, "dependency changes must not alter user route order")
	require.Equal(t, dependencyVersion+1, afterCandidateChange, "non-primary candidate changes must invalidate the key")
	assertDependencyEvent(routeVersion, dependencyVersion, afterCandidateChange)

	_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET name = name || '-cosmetic' WHERE id = $1", fallback.ID)
	require.NoError(t, err)
	_, afterCosmeticChange := readVersions()
	require.Equal(t, afterCandidateChange, afterCosmeticChange, "cosmetic group updates must not churn route snapshots")

	_, err = integrationDB.ExecContext(ctx, "UPDATE users SET status = 'disabled' WHERE id = $1", user.ID)
	require.NoError(t, err)
	_, afterUserChange := readVersions()
	require.Equal(t, afterCosmeticChange+1, afterUserChange)
	assertDependencyEvent(routeVersion, afterCosmeticChange, afterUserChange)

	_, err = integrationDB.ExecContext(ctx,
		"INSERT INTO user_allowed_groups (user_id, group_id) VALUES ($1, $2)", user.ID, fallback.ID)
	require.NoError(t, err)
	_, afterAccessChange := readVersions()
	require.Equal(t, afterUserChange+1, afterAccessChange, "one access mutation must produce one dependency bump")
	assertDependencyEvent(routeVersion, afterUserChange, afterAccessChange)
}
