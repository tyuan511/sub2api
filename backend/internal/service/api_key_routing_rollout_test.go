package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

type routingRolloutRepoStub struct {
	SettingRepository
	mu    sync.Mutex
	value string
	err   error
	reads int
}

func (r *routingRolloutRepoStub) GetValue(context.Context, string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	if r.err != nil {
		return "", r.err
	}
	if r.value == "" {
		return "", ErrSettingNotFound
	}
	return r.value, nil
}
func (r *routingRolloutRepoStub) Set(_ context.Context, _ string, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.value = value
	return nil
}
func allowRoutingUsersForTest(t *testing.T, svc *APIKeyService, ids ...int64) *SettingService {
	t.Helper()
	if svc.cfg == nil {
		svc.cfg = &config.Config{}
	}
	svc.cfg.Gateway.APIKeyMultiGroupRoutingEnabled = true
	settings := NewSettingService(&routingRolloutRepoStub{}, svc.cfg)
	require.NoError(t, settings.SetAPIKeyRoutingRolloutSettings(context.Background(), APIKeyRoutingRolloutSettings{UserIDs: ids}))
	svc.SetRoutingRolloutSettings(settings)
	return settings
}

func TestAPIKeyRoutingRolloutSettingsValidateAndDefaultDeny(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingService(&routingRolloutRepoStub{}, nil)
	got, err := svc.GetAPIKeyRoutingRolloutSettings(ctx)
	require.NoError(t, err)
	require.Empty(t, got.UserIDs)
	require.False(t, svc.IsAPIKeyRoutingRolloutUser(ctx, 1))
	for _, ids := range [][]int64{{0}, {-1}, {9007199254740992}, make([]int64, 1001)} {
		require.Error(t, svc.SetAPIKeyRoutingRolloutSettings(ctx, APIKeyRoutingRolloutSettings{UserIDs: ids}))
	}
	require.NoError(t, svc.SetAPIKeyRoutingRolloutSettings(ctx, APIKeyRoutingRolloutSettings{UserIDs: []int64{8, 2, 8}}))
	got, err = svc.GetAPIKeyRoutingRolloutSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{2, 8}, got.UserIDs)
	got.UserIDs[0] = 99
	require.True(t, svc.IsAPIKeyRoutingRolloutUser(ctx, 2))
	require.False(t, svc.IsAPIKeyRoutingRolloutUser(ctx, 99))
}

func TestAPIKeyRoutingRolloutCacheIsBoundedAndFailClosed(t *testing.T) {
	ctx := context.Background()
	repo := &routingRolloutRepoStub{value: `{"user_ids":[5]}`}
	first, second := NewSettingService(repo, nil), NewSettingService(repo, nil)
	require.True(t, first.IsAPIKeyRoutingRolloutUser(ctx, 5))
	for i := 0; i < 100; i++ {
		require.True(t, first.IsAPIKeyRoutingRolloutUser(ctx, 5))
	}
	require.Equal(t, 1, repo.reads)
	require.True(t, second.IsAPIKeyRoutingRolloutUser(ctx, 5))
	require.NoError(t, first.SetAPIKeyRoutingRolloutSettings(ctx, APIKeyRoutingRolloutSettings{}))
	require.False(t, first.IsAPIKeyRoutingRolloutUser(ctx, 5), "local withdrawal is immediate")
	second.apiKeyRoutingRolloutCache.Store(&cachedAPIKeyRoutingRollout{expiresAt: time.Now().Add(-time.Second)})
	require.False(t, second.IsAPIKeyRoutingRolloutUser(ctx, 5), "other instances refresh after TTL")
	for _, bad := range []string{`{"user_ids":[-1]}`, `{"user_ids":["5"]}`, `invalid`} {
		repo.value = bad
		cold := NewSettingService(repo, nil)
		require.False(t, cold.IsAPIKeyRoutingRolloutUser(ctx, 5))
	}
	repo.err = errors.New("database unavailable")
	require.False(t, NewSettingService(repo, nil).IsAPIKeyRoutingRolloutUser(ctx, 5))
}

func TestAPIKeyRoutingRolloutConcurrentSaveAndReads(t *testing.T) {
	svc := NewSettingService(&routingRolloutRepoStub{}, nil)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				svc.IsAPIKeyRoutingRolloutUser(ctx, 5)
			}
		}()
	}
	for i := 0; i < 20; i++ {
		require.NoError(t, svc.SetAPIKeyRoutingRolloutSettings(ctx, APIKeyRoutingRolloutSettings{UserIDs: []int64{5}}))
	}
	require.NoError(t, svc.SetAPIKeyRoutingRolloutSettings(ctx, APIKeyRoutingRolloutSettings{}))
	wg.Wait()
	require.False(t, svc.IsAPIKeyRoutingRolloutUser(ctx, 5))
}

func TestAPIKeyRoutingRolloutWithdrawalPreservesConfigAndPrimaryChecks(t *testing.T) {
	ctx := context.Background()
	key := testRouteOperationsAPIKey()
	key.ScheduleMode = APIKeyScheduleModeSmart
	pref := APIKeySmartPreferencePrice
	key.SmartPreference = &pref
	key.SmartBalanceBPS = routingControlInt(3000)
	key.RoutingMinSuccessRate = 95
	svc := &APIKeyService{}
	settings := allowRoutingUsersForTest(t, svc, key.UserID)
	require.Same(t, key, svc.ProjectAPIKeyRoutingForUser(ctx, key))
	require.NoError(t, settings.SetAPIKeyRoutingRolloutSettings(ctx, APIKeyRoutingRolloutSettings{}))
	fixed := svc.ProjectAPIKeyRoutingForUser(ctx, key)
	plan, err := NewAPIKeyRouteCoordinator(true).BuildPlan(fixed, nil)
	require.NoError(t, err)
	require.False(t, plan.RoutingEnabled)
	require.Len(t, plan.Candidates, 1)
	require.Equal(t, *key.GroupID, plan.Candidates[0].GroupID)
	require.Len(t, key.GroupRoutes, 2)
	require.Equal(t, APIKeyScheduleModeSmart, key.ScheduleMode)
	require.Equal(t, 95, key.RoutingMinSuccessRate)
	key.Group.Status = "inactive"
	_, err = NewAPIKeyRouteCoordinator(true).BuildPlan(svc.ProjectAPIKeyRoutingForUser(ctx, key), nil)
	require.ErrorIs(t, err, ErrNoEligibleAPIKeyRoute, "inactive primary cannot fall through to another group")
	key.Group.Status = StatusActive
	key.GroupID = nil
	_, err = NewAPIKeyRouteCoordinator(true).BuildPlan(svc.ProjectAPIKeyRoutingForUser(ctx, key), nil)
	require.ErrorIs(t, err, ErrInvalidAPIKeyRouteSet, "missing primary must not become unscoped")
	primary := int64(11)
	key.GroupID = &primary
	require.NoError(t, settings.SetAPIKeyRoutingRolloutSettings(ctx, APIKeyRoutingRolloutSettings{UserIDs: []int64{key.UserID}}))
	require.Same(t, key, svc.ProjectAPIKeyRoutingForUser(ctx, key), "re-adding restores the saved configuration")
	svc.cfg.Gateway.APIKeyMultiGroupRoutingEnabled = false
	require.False(t, svc.IsRoutingEnabledForUser(ctx, key.UserID), "process kill switch remains authoritative")
}

type rolloutWriteKeyRepo struct {
	APIKeyRepository
	key     *APIKey
	updates int
}

func (r *rolloutWriteKeyRepo) GetByID(context.Context, int64) (*APIKey, error) {
	clone := *r.key
	return &clone, nil
}
func (r *rolloutWriteKeyRepo) Create(_ context.Context, key *APIKey) error { r.key = key; return nil }
func (r *rolloutWriteKeyRepo) Update(_ context.Context, key *APIKey, _ APIKeyUpdateFields) error {
	r.key = key
	r.updates++
	return nil
}

type rolloutUserRepo struct{ UserRepository }

func (r *rolloutUserRepo) GetByID(_ context.Context, id int64) (*User, error) {
	return &User{ID: id, Status: StatusActive}, nil
}

func TestAPIKeyRoutingRolloutRejectsDirectAPIBypassButAllowsLegacyEdits(t *testing.T) {
	ctx := context.Background()
	key := testRouteOperationsAPIKey()
	repo := &rolloutWriteKeyRepo{key: key}
	svc := &APIKeyService{apiKeyRepo: repo, userRepo: &rolloutUserRepo{}, cfg: &config.Config{}, groupRepo: &apiKeyRoutingGroupRepo{groups: map[int64]*Group{11: key.GroupRoutes[0].Group, 12: key.GroupRoutes[1].Group}}}
	mode, pref := APIKeyScheduleModeSmart, APIKeySmartPreferencePrice
	advanced := []CreateAPIKeyRequest{
		{GroupRoutes: routeInputs(APIKeyGroupRouteInput{GroupID: 11}, APIKeyGroupRouteInput{GroupID: 12, Priority: 1})},
		{ScheduleMode: &mode, SmartPreference: &pref}, {SmartBalanceBPS: routingControlInt(5000)}, {RoutingMinSuccessRate: routingControlInt(80)},
	}
	for _, request := range advanced {
		_, err := svc.Create(ctx, key.UserID, request)
		require.ErrorIs(t, err, ErrAPIKeyRoutingNotEnabled)
		_, err = svc.Update(ctx, key.ID, key.UserID, UpdateAPIKeyRequest{GroupRoutes: request.GroupRoutes, ScheduleMode: request.ScheduleMode, SmartPreference: request.SmartPreference, SmartBalanceBPS: request.SmartBalanceBPS, RoutingMinSuccessRate: request.RoutingMinSuccessRate})
		require.ErrorIs(t, err, ErrAPIKeyRoutingNotEnabled)
	}
	name := "renamed"
	updated, err := svc.Update(ctx, key.ID, key.UserID, UpdateAPIKeyRequest{Name: &name})
	require.NoError(t, err)
	require.Len(t, updated.GroupRoutes, 2)
	require.Equal(t, name, updated.Name)
	_, err = svc.Update(ctx, key.ID, 99, UpdateAPIKeyRequest{Name: &name})
	require.ErrorIs(t, err, ErrInsufficientPerms)
	settings := allowRoutingUsersForTest(t, svc, key.UserID)
	created, err := svc.Create(ctx, key.UserID, advanced[0])
	require.NoError(t, err)
	require.Len(t, created.GroupRoutes, 2)
	require.NoError(t, settings.SetAPIKeyRoutingRolloutSettings(ctx, APIKeyRoutingRolloutSettings{}))
	legacy, err := svc.Create(ctx, key.UserID, CreateAPIKeyRequest{GroupID: key.GroupID})
	require.NoError(t, err)
	require.Len(t, legacy.GroupRoutes, 1)
}

func BenchmarkAPIKeyRoutingRolloutCachedMembership(b *testing.B) {
	svc := NewSettingService(&routingRolloutRepoStub{}, nil)
	svc.storeAPIKeyRoutingRollout(APIKeyRoutingRolloutSettings{UserIDs: []int64{5}}, time.Hour)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !svc.IsAPIKeyRoutingRolloutUser(ctx, 5) {
			b.Fatal("missing membership")
		}
	}
}
