package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestReplaceAPIKeyRouteGroupPreservesOrderAndRenumbers(t *testing.T) {
	routes := []service.APIKeyGroupRoute{
		{APIKeyID: 99, GroupID: 11, Priority: 0, Enabled: true},
		{APIKeyID: 99, GroupID: 12, Priority: 4, Enabled: false},
	}

	got := replaceAPIKeyRouteGroup(routes, 11, 13, 99)
	require.Equal(t, []service.APIKeyGroupRoute{
		{APIKeyID: 99, GroupID: 13, Priority: 0, Enabled: true},
		{APIKeyID: 99, GroupID: 12, Priority: 1, Enabled: false},
	}, got)
}

func TestReplaceAPIKeyRouteGroupMergesExistingTarget(t *testing.T) {
	routes := []service.APIKeyGroupRoute{
		{APIKeyID: 99, GroupID: 11, Priority: 0, Enabled: false},
		{APIKeyID: 99, GroupID: 13, Priority: 3, Enabled: true},
		{APIKeyID: 99, GroupID: 12, Priority: 5, Enabled: true},
	}

	got := replaceAPIKeyRouteGroup(routes, 11, 13, 99)
	require.Equal(t, []service.APIKeyGroupRoute{
		{APIKeyID: 99, GroupID: 13, Priority: 0, Enabled: true},
		{APIKeyID: 99, GroupID: 12, Priority: 1, Enabled: true},
	}, got)
}

func TestReplaceAPIKeyRouteGroupDoesNotMutateInput(t *testing.T) {
	routes := []service.APIKeyGroupRoute{{APIKeyID: 7, GroupID: 11, Priority: 2, Enabled: true}}
	_ = replaceAPIKeyRouteGroup(routes, 11, 13, 7)
	require.Equal(t, int64(11), routes[0].GroupID)
	require.Equal(t, 2, routes[0].Priority)
}
