package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type userViewCacheSnapshotRepoStub struct {
	ChannelMonitorRepository
	listSnapshotsCalls int
	computeCalls       int
}

type runCheckCacheSnapshotRepoStub struct {
	ChannelMonitorRepository
	monitor       *ChannelMonitor
	refreshGroups [][]string
}

type snapshotRuntimeStub struct{ rt ChannelMonitorRuntime }

func (s snapshotRuntimeStub) GetChannelMonitorRuntime(context.Context) ChannelMonitorRuntime {
	return s.rt
}

func (r *runCheckCacheSnapshotRepoStub) GetByID(context.Context, int64) (*ChannelMonitor, error) {
	return r.monitor, nil
}

func (r *runCheckCacheSnapshotRepoStub) InsertHistoryBatch(context.Context, []*ChannelMonitorHistoryRow) error {
	return nil
}

func (r *runCheckCacheSnapshotRepoStub) MarkChecked(context.Context, int64, time.Time) error {
	return nil
}

func (r *runCheckCacheSnapshotRepoStub) RefreshGroupCacheHitRateSnapshots(_ context.Context, groups []string) error {
	r.refreshGroups = append(r.refreshGroups, append([]string(nil), groups...))
	return nil
}

func (r *userViewCacheSnapshotRepoStub) ListEnabled(context.Context) ([]*ChannelMonitor, error) {
	return []*ChannelMonitor{{
		ID:           1,
		Name:         "pro",
		Provider:     MonitorProviderOpenAI,
		PrimaryModel: "gpt-5.6-sol",
		GroupName:    "gpt-pro-20x",
		Enabled:      true,
	}}, nil
}

func (r *userViewCacheSnapshotRepoStub) ListLatestForMonitorIDs(context.Context, []int64) (map[int64][]*ChannelMonitorLatest, error) {
	return map[int64][]*ChannelMonitorLatest{
		1: {{Model: "gpt-5.6-sol", Status: MonitorStatusOperational}},
	}, nil
}

func (r *userViewCacheSnapshotRepoStub) ComputeAvailabilityForMonitors(context.Context, []int64, int) (map[int64][]*ChannelMonitorAvailability, error) {
	return map[int64][]*ChannelMonitorAvailability{
		1: {{Model: "gpt-5.6-sol", AvailabilityPct: 99}},
	}, nil
}

func (r *userViewCacheSnapshotRepoStub) ListRecentHistoryForMonitors(context.Context, []int64, map[int64]string, int) (map[int64][]*ChannelMonitorHistoryEntry, error) {
	return map[int64][]*ChannelMonitorHistoryEntry{}, nil
}

func (r *userViewCacheSnapshotRepoStub) ListGroupCacheHitRateSnapshots(context.Context, []string) (map[string]map[int]*GroupCacheHitRateSnapshot, error) {
	r.listSnapshotsCalls++
	return map[string]map[int]*GroupCacheHitRateSnapshot{
		"gpt-pro-20x": {
			7: {GroupName: "gpt-pro-20x", WindowDays: 7, InputTokens: 10, CacheReadTokens: 90, CacheHitRatePct: 90, ComputedAt: time.Now()},
		},
	}, nil
}

func (r *userViewCacheSnapshotRepoStub) ComputeCacheHitRatesForGroups(context.Context, []string, int) (map[string]*GroupCacheHitRate, error) {
	r.computeCalls++
	return nil, nil
}

func TestListUserViewReadsPersistedCacheHitRateSnapshots(t *testing.T) {
	repo := &userViewCacheSnapshotRepoStub{}
	svc := NewChannelMonitorService(repo, nil)

	views, err := svc.ListUserView(context.Background())
	require.NoError(t, err)
	require.Len(t, views, 1)
	require.NotNil(t, views[0].CacheHitRate7d)
	require.InDelta(t, 90.0, *views[0].CacheHitRate7d, 0.0001)
	require.Nil(t, views[0].CacheHitRate15d)
	require.Equal(t, 1, repo.listSnapshotsCalls)
	require.Zero(t, repo.computeCalls)
}

func TestRunCheckRefreshesGroupCacheHitRateSnapshots(t *testing.T) {
	repo := &runCheckCacheSnapshotRepoStub{monitor: &ChannelMonitor{
		ID:           1,
		Provider:     MonitorProviderDeepseek,
		PrimaryModel: "quota",
		GroupName:    "gpt-pro-20x",
		Enabled:      true,
		CheckMode:    MonitorCheckModeQuota,
	}}
	svc := NewChannelMonitorService(repo, nil)
	svc.SetRuntimeReader(snapshotRuntimeStub{rt: ChannelMonitorRuntime{
		Enabled: true,
		Mode:    ChannelMonitorModeV1,
	}})

	_, err := svc.RunCheck(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, [][]string{{"gpt-pro-20x"}}, repo.refreshGroups)
}
