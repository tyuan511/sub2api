package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoutingControlLoopContractsKeepSlowLoopsOffRequestState(t *testing.T) {
	contracts := RoutingControlLoopContracts()
	require.Len(t, contracts, 4)
	for _, contract := range contracts {
		require.NoError(t, ValidateRoutingControlLoopContract(contract))
	}
	require.True(t, contracts[RoutingControlLoopRequestSafety].MayWriteRequestState)
	require.False(t, contracts[RoutingControlLoopCalibration].MayWriteRequestState)
	require.True(t, contracts[RoutingControlLoopCalibration].MayCreateDraft)
	require.False(t, contracts[RoutingControlLoopCalibration].MayPublishScore)
	require.True(t, contracts[RoutingControlLoopGovernance].RequiresPromotionGate)
}

func TestRoutingArtifactManagerOnlyCreatesDrafts(t *testing.T) {
	repo := &routingArtifactManagerRepo{}
	manager := NewRoutingArtifactManager(repo, nil)
	artifact := routingArtifactForManagerTest(1, "unsafe-active", RoutingLifecycleActive)

	err := manager.CreateArtifact(context.Background(), artifact)
	require.ErrorIs(t, err, ErrRoutingControlLoopBoundary)
	require.Empty(t, repo.artifacts)
}
