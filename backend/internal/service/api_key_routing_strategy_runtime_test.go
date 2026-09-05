package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRoutingStrategyRuntimeSelectsStableCanaryAndExposesShadow(t *testing.T) {
	baseline := routingArtifactForManagerTest(1, "baseline-v1", RoutingLifecycleActive)
	canary := routingArtifactForManagerTest(2, "canary-v2", RoutingLifecycleCanary)
	shadow := routingArtifactForManagerTest(3, "shadow-v3", RoutingLifecycleShadow)
	cache := &routingArtifactManagerCache{
		objects: map[string]*RoutingArtifactVersion{
			baseline.Version: cloneRoutingArtifactForManagerTest(baseline),
			canary.Version:   cloneRoutingArtifactForManagerTest(canary),
			shadow.Version:   cloneRoutingArtifactForManagerTest(shadow),
		},
		pointers: RoutingArtifactPointers{
			BaselineVersion: baseline.Version, ActiveVersion: baseline.Version, CanaryVersion: canary.Version,
			CanaryAllocationBPS: 5000, CanaryExperimentID: "experiment-1", CanaryBucketSaltChecksum: strings.Repeat("b", 64),
			ShadowVersion: shadow.Version, UpdatedAt: time.Now(),
		},
		pointerReady: true,
	}
	runtime := NewRoutingStrategyRuntime(cache, true)
	scope := RoutingArtifactScopeFromVersion(baseline)
	require.NoError(t, runtime.Refresh(context.Background(), scope))

	var canaryKey, activeKey int64
	for keyID := int64(1); keyID < 1000 && (canaryKey == 0 || activeKey == 0); keyID++ {
		selection := runtime.Select(scope, 11, keyID)
		if selection.AssignmentReason == RoutingAssignmentCanary {
			canaryKey = keyID
		} else {
			activeKey = keyID
		}
	}
	require.NotZero(t, canaryKey)
	require.NotZero(t, activeKey)

	first := runtime.Select(scope, 11, canaryKey)
	second := runtime.Select(scope, 11, canaryKey)
	require.Equal(t, canary.Version, first.Policy.Version)
	require.Equal(t, first.ExperimentBucket, second.ExperimentBucket)
	require.Equal(t, "experiment-1", *first.ExperimentID)
	require.NotNil(t, first.ShadowPolicy)
	require.Equal(t, shadow.Version, first.ShadowPolicy.Version)
	control := runtime.Select(scope, 11, activeKey)
	require.Equal(t, baseline.Version, control.Policy.Version)
	require.Equal(t, "experiment-1", *control.ExperimentID)
	require.NotNil(t, control.ExperimentBucket)
}

func TestRoutingStrategyRuntimeFallsBackToBuiltInWithoutLoadedScope(t *testing.T) {
	preference := APIKeySmartPreferenceSpeed
	scope := RoutingArtifactScope{
		ArtifactKind: RoutingArtifactStrategy, Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", Preference: &preference,
	}
	runtime := NewRoutingStrategyRuntime(nil, false)
	selection := runtime.Select(scope, 1, 2)
	require.Equal(t, BuiltinAPIKeyRoutingStrategyVersion, selection.Policy.Version)
	require.Equal(t, APIKeyRoutingWeights(preference), selection.Policy.Weights)
}
