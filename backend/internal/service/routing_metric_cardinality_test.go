package service

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoutingMetricVersionLabelsBoundHistoricalCardinality(t *testing.T) {
	recent := make([]*RoutingArtifactVersion, 0, 1000)
	for index := 999; index >= 0; index-- {
		recent = append(recent, &RoutingArtifactVersion{Version: fmt.Sprintf("strategy-v%d", index)})
	}
	labels := NewRoutingMetricVersionLabels(RoutingArtifactPointers{
		BaselineVersion: "strategy-v1",
		ActiveVersion:   "strategy-v900",
		CanaryVersion:   "strategy-v999",
	}, recent, 1000)

	require.LessOrEqual(t, labels.Cardinality(), 3+DefaultRoutingMetricRecent+1)
	require.Equal(t, RoutingMetricVersionLabel{Version: "strategy-v1", Role: "baseline"}, labels.Project("strategy-v1"))
	require.Equal(t, RoutingMetricVersionLabel{Version: "strategy-v900", Role: "active"}, labels.Project("strategy-v900"))
	require.Equal(t, RoutingMetricVersionLabel{Version: "strategy-v999", Role: "canary"}, labels.Project("strategy-v999"))
	require.Equal(t, RoutingMetricVersionLabel{Version: RoutingMetricVersionOther, Role: RoutingMetricRoleHistorical}, labels.Project("strategy-v42"))
	require.Equal(t, RoutingMetricVersionLabel{Version: RoutingMetricVersionOther, Role: RoutingMetricRoleHistorical}, labels.Project("attacker-controlled-version"))
}

func TestRoutingMetricVersionLabelsDoNotChargePointerDuplicatesToRecentLimit(t *testing.T) {
	labels := NewRoutingMetricVersionLabels(RoutingArtifactPointers{
		BaselineVersion: "v1", ActiveVersion: "v1", CanaryVersion: "v2",
	}, []*RoutingArtifactVersion{{Version: "v2"}, {Version: "v3"}, {Version: "v4"}}, 2)

	require.Equal(t, RoutingMetricVersionLabel{Version: "v3", Role: "recent"}, labels.Project("v3"))
	require.Equal(t, RoutingMetricVersionLabel{Version: "v4", Role: "recent"}, labels.Project("v4"))
	require.Equal(t, 5, labels.Cardinality()) // v1/v2/v3/v4 + historical
}
