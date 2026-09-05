package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRoutingOrderStabilizerSuppressesSmallFlipAndHonorsResidence(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	stabilizer := NewAPIKeyRoutingOrderStabilizer(10, time.Hour)
	policy := APIKeyRoutingStabilityPolicy{MinimumScoreDifference: 0.05, MinimumResidenceSeconds: 300, MaxTrafficChangeBPS: 10000}
	scope := APIKeyRoutingScoreScope{Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses"}
	initial := routingScoresForStability(1, 0.80, 2, 0.70)
	require.Equal(t, int64(1), stabilizer.Stabilize(7, 1, scope, APIKeySmartPreferenceBalanced, "a", initial, policy, now)[0].GroupID)

	smallFlip := routingScoresForStability(2, 0.82, 1, 0.79)
	require.Equal(t, int64(1), stabilizer.Stabilize(7, 1, scope, APIKeySmartPreferenceBalanced, "b", smallFlip, policy, now.Add(10*time.Minute))[0].GroupID)

	bigFlip := routingScoresForStability(2, 0.95, 1, 0.70)
	require.Equal(t, int64(1), stabilizer.Stabilize(8, 1, scope, APIKeySmartPreferenceBalanced, "a", initial, policy, now)[0].GroupID)
	require.Equal(t, int64(1), stabilizer.Stabilize(8, 1, scope, APIKeySmartPreferenceBalanced, "b", bigFlip, policy, now.Add(time.Minute))[0].GroupID)
	require.Equal(t, int64(2), stabilizer.Stabilize(8, 1, scope, APIKeySmartPreferenceBalanced, "c", bigFlip, policy, now.Add(6*time.Minute))[0].GroupID)
}

func TestRoutingOrderStabilizerBoundsNewSessionTrafficAndRankMovement(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	stabilizer := NewAPIKeyRoutingOrderStabilizer(10, time.Hour)
	policy := APIKeyRoutingStabilityPolicy{MinimumScoreDifference: 0.01, MinimumResidenceSeconds: 0, MaxTrafficChangeBPS: 1000}
	scope := APIKeyRoutingScoreScope{Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses"}
	initial := []APIKeyRoutingCandidateScore{{GroupID: 1, Eligible: true, Score: .8}, {GroupID: 2, Eligible: true, Score: .7}, {GroupID: 3, Eligible: true, Score: .6}}
	proposed := []APIKeyRoutingCandidateScore{{GroupID: 3, Eligible: true, Score: .95}, {GroupID: 1, Eligible: true, Score: .7}, {GroupID: 2, Eligible: true, Score: .6}}
	_ = stabilizer.Stabilize(9, 1, scope, APIKeySmartPreferenceBalanced, "initial", initial, policy, now)

	newTop := 0
	for index := 0; index < 1000; index++ {
		result := stabilizer.Stabilize(9, 1, scope, APIKeySmartPreferenceBalanced, fmt.Sprintf("session-%d", index), proposed, policy, now.Add(time.Second))
		if result[0].GroupID == 3 {
			newTop++
		}
	}
	// Group 3 can move only one adjacent position in this round, so it cannot
	// become top even for the admitted 10% transition cohort.
	require.Zero(t, newTop)

	// Complete the first 3->2 transition, then after another transition the
	// candidate may become top. The second transition is again capped at 10%.
	firstComplete := stabilizer.Stabilize(9, 1, scope, APIKeySmartPreferenceBalanced, "finish-1", proposed, policy, now.Add(10*time.Minute))
	require.Equal(t, []int64{1, 3, 2}, eligibleRoutingOrder(firstComplete))
	newTop = 0
	for index := 0; index < 1000; index++ {
		result := stabilizer.Stabilize(9, 1, scope, APIKeySmartPreferenceBalanced, fmt.Sprintf("second-%d", index), proposed, policy, now.Add(10*time.Minute+time.Second))
		if result[0].GroupID == 3 {
			newTop++
		}
	}
	require.Greater(t, newTop, 50)
	require.Less(t, newTop, 150)
}

func TestRoutingOrderStabilizerIsRouteVersionIsolatedAndBounded(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	stabilizer := NewAPIKeyRoutingOrderStabilizer(2, time.Hour)
	policy := APIKeyRoutingStabilityPolicy{MinimumScoreDifference: 0.5, MinimumResidenceSeconds: 300, MaxTrafficChangeBPS: 1000}
	scope := APIKeyRoutingScoreScope{Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses"}
	old := routingScoresForStability(1, .8, 2, .7)
	flipped := routingScoresForStability(2, .8, 1, .7)
	_ = stabilizer.Stabilize(1, 1, scope, APIKeySmartPreferenceBalanced, "a", old, policy, now)

	result := stabilizer.Stabilize(1, 2, scope, APIKeySmartPreferenceBalanced, "b", flipped, policy, now.Add(time.Second))
	require.Equal(t, int64(2), result[0].GroupID, "new route_version must not inherit old ranking residence")
	_ = stabilizer.Stabilize(2, 1, scope, APIKeySmartPreferenceBalanced, "c", old, policy, now)
	require.LessOrEqual(t, len(stabilizer.states), 2)
}

func routingScoresForStability(firstID int64, firstScore float64, secondID int64, secondScore float64) []APIKeyRoutingCandidateScore {
	return []APIKeyRoutingCandidateScore{
		{GroupID: firstID, Eligible: true, Score: firstScore},
		{GroupID: secondID, Eligible: true, Score: secondScore},
	}
}
