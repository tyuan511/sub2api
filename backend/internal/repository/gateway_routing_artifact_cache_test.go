package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newRoutingArtifactCacheTest(t *testing.T) (*gatewayRoutingArtifactCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return &gatewayRoutingArtifactCache{rdb: rdb}, mr
}

func routingArtifactForTest(version, status string, priceWeight float64) *service.RoutingArtifactVersion {
	payload := map[string]any{
		"weights":                map[string]any{"success": 0.5, "price": priceWeight, "speed": 0.4 - priceWeight, "capacity": 0.1},
		"success_rate_hard_gate": 0.5, "minimum_samples": 10, "max_snapshot_age_seconds": 180,
		"stability": map[string]any{"minimum_score_difference": 0.01, "minimum_residence_seconds": 300, "max_traffic_change_bps": 1000},
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	preference := service.APIKeySmartPreferenceBalanced
	return &service.RoutingArtifactVersion{
		ArtifactKind: service.RoutingArtifactStrategy, Version: version, Platform: service.PlatformOpenAI,
		ModelFamily: "gpt-5", EndpointKind: "responses", Preference: &preference, Status: status,
		SchemaVersion: "routing-strategy-v1", Checksum: hex.EncodeToString(sum[:]), Payload: encoded,
		Dependencies: json.RawMessage(`[]`), Lineage: json.RawMessage(`{}`), CreatedAt: time.Now(),
	}
}

func TestGatewayRoutingArtifactCacheAtomicPointerAndBaselineFallback(t *testing.T) {
	cache, mr := newRoutingArtifactCacheTest(t)
	ctx := context.Background()
	baseline := routingArtifactForTest("baseline-1", service.RoutingLifecycleActive, 0.2)
	active := routingArtifactForTest("active-2", service.RoutingLifecycleActive, 0.25)
	canary := routingArtifactForTest("canary-3", service.RoutingLifecycleCanary, 0.27)
	scope := service.RoutingArtifactScopeFromVersion(baseline)
	require.NoError(t, cache.PublishArtifact(ctx, baseline))
	require.NoError(t, cache.PublishArtifact(ctx, active))
	require.NoError(t, cache.PublishArtifact(ctx, canary))
	empty := ""
	pointers := service.RoutingArtifactPointers{
		BaselineVersion: baseline.Version, ActiveVersion: active.Version, CanaryVersion: canary.Version,
		CanaryAllocationBPS: 500, CanaryExperimentID: "experiment-1", CanaryBucketSaltChecksum: strings.Repeat("a", 64), UpdatedAt: time.Now(),
	}
	require.NoError(t, cache.SwapPointers(ctx, scope, pointers, &empty))

	resolved, loadedPointers, err := service.ResolveRoutingArtifact(ctx, cache, scope)
	require.NoError(t, err)
	require.Equal(t, active.Version, resolved.Version)
	require.Equal(t, pointers.ActiveVersion, loadedPointers.ActiveVersion)
	require.Equal(t, pointers.CanaryVersion, loadedPointers.CanaryVersion)
	require.Equal(t, pointers.CanaryAllocationBPS, loadedPointers.CanaryAllocationBPS)
	require.Equal(t, pointers.CanaryExperimentID, loadedPointers.CanaryExperimentID)

	// A corrupt active object must never execute; baseline remains available.
	mr.Set(routingArtifactObjectKey(scope, active.Version), `{"invalid":true}`)
	resolved, _, err = service.ResolveRoutingArtifact(ctx, cache, scope)
	require.NoError(t, err)
	require.Equal(t, baseline.Version, resolved.Version)
}

func TestGatewayRoutingArtifactCacheRejectsStaleCASAndMutation(t *testing.T) {
	cache, _ := newRoutingArtifactCacheTest(t)
	ctx := context.Background()
	baseline := routingArtifactForTest("baseline-1", service.RoutingLifecycleActive, 0.2)
	scope := service.RoutingArtifactScopeFromVersion(baseline)
	require.NoError(t, cache.PublishArtifact(ctx, baseline))
	empty := ""
	require.NoError(t, cache.SwapPointers(ctx, scope, service.RoutingArtifactPointers{
		BaselineVersion: baseline.Version, ActiveVersion: baseline.Version, UpdatedAt: time.Now(),
	}, &empty))
	stale := "someone-else"
	require.ErrorIs(t, cache.SwapPointers(ctx, scope, service.RoutingArtifactPointers{
		BaselineVersion: baseline.Version, ActiveVersion: baseline.Version, UpdatedAt: time.Now(),
	}, &stale), service.ErrRoutingArtifactPointerConflict)

	mutated := routingArtifactForTest("baseline-1", service.RoutingLifecycleActive, 0.27)
	require.ErrorIs(t, cache.PublishArtifact(ctx, mutated), service.ErrRoutingArtifactPointerConflict)
}

func TestRoutingArtifactKeysShareClusterHashTag(t *testing.T) {
	artifact := routingArtifactForTest("active-1", service.RoutingLifecycleActive, 0.2)
	scope := service.RoutingArtifactScopeFromVersion(artifact)
	pointer := routingArtifactPointerKey(scope)
	object := routingArtifactObjectKey(scope, artifact.Version)
	require.Equal(t, pointer[tagStart(pointer):tagEnd(pointer)], object[tagStart(object):tagEnd(object)])
}

func tagStart(value string) int { return strings.IndexByte(value, '{') }
func tagEnd(value string) int   { return strings.IndexByte(value, '}') + 1 }
