package service

import "strings"

const (
	RoutingMetricVersionOther   = "other"
	RoutingMetricRoleHistorical = "historical"
	DefaultRoutingMetricRecent  = 4
)

// RoutingMetricVersionLabel is the only version label projection that routing
// Prometheus collectors may use. Full version history remains in PostgreSQL
// and bounded diagnostic facts; old versions collapse into a single "other"
// time-series label instead of growing cardinality forever.
type RoutingMetricVersionLabel struct {
	Version string
	Role    string
}

type RoutingMetricVersionLabels struct {
	allowed map[string]string
}

// NewRoutingMetricVersionLabels admits baseline/active/canary plus a bounded
// number of recent versions. The recent slice is expected newest-first and may
// contain the same versions as the pointers; duplicates do not consume slots.
func NewRoutingMetricVersionLabels(pointers RoutingArtifactPointers, recent []*RoutingArtifactVersion, recentLimit int) RoutingMetricVersionLabels {
	if recentLimit < 0 {
		recentLimit = 0
	}
	if recentLimit > DefaultRoutingMetricRecent {
		recentLimit = DefaultRoutingMetricRecent
	}
	labels := RoutingMetricVersionLabels{allowed: make(map[string]string, 3+recentLimit)}
	labels.add(pointers.BaselineVersion, "baseline")
	labels.add(pointers.ActiveVersion, "active")
	labels.add(pointers.CanaryVersion, "canary")
	addedRecent := 0
	for _, artifact := range recent {
		if artifact == nil || addedRecent >= recentLimit {
			continue
		}
		version := strings.TrimSpace(artifact.Version)
		if version == "" {
			continue
		}
		if _, exists := labels.allowed[version]; exists {
			continue
		}
		labels.allowed[version] = "recent"
		addedRecent++
	}
	return labels
}

func (l *RoutingMetricVersionLabels) add(version, role string) {
	version = strings.TrimSpace(version)
	if version == "" {
		return
	}
	if _, exists := l.allowed[version]; !exists {
		l.allowed[version] = role
	}
}

// Project prevents arbitrary/historical version strings from becoming a
// Prometheus label. The role is also bounded to five enum values.
func (l RoutingMetricVersionLabels) Project(version string) RoutingMetricVersionLabel {
	version = strings.TrimSpace(version)
	if role, ok := l.allowed[version]; ok {
		return RoutingMetricVersionLabel{Version: version, Role: role}
	}
	return RoutingMetricVersionLabel{Version: RoutingMetricVersionOther, Role: RoutingMetricRoleHistorical}
}

func (l RoutingMetricVersionLabels) Cardinality() int {
	return len(l.allowed) + 1 // include the collapsed historical series
}
