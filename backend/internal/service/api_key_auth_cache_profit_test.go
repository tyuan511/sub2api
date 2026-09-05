package service

// 投影漏列回归（service 半程）：认证快照 build → L2 JSON 序列化
// → 反序列化 → 还原 apiKey.Group → 请求 ctx → 利润门解析，全链路保真。
// repository 半程（真实 GetByKeyForAuth 投影）见
// internal/repository/api_key_repo_profit_projection_integration_test.go。

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type candidateRateAuthRepoStub struct {
	UserGroupRateRepository
	rates       map[int64]float64
	requestedID []int64
}

func (s *candidateRateAuthRepoStub) GetByUserAndGroupIDs(_ context.Context, _ int64, groupIDs []int64) (map[int64]float64, error) {
	s.requestedID = append([]int64(nil), groupIDs...)
	return cloneGroupRates(s.rates), nil
}

func (s *candidateRateAuthRepoStub) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return nil, nil
}

func profitAuthTestAPIKey() *APIKey {
	groupID := int64(50)
	return &APIKey{
		ID:      82,
		UserID:  40,
		GroupID: &groupID,
		Name:    "profit-auth-roundtrip",
		Status:  StatusActive,
		User: &User{
			ID:          40,
			Email:       "profit@test.local",
			Status:      StatusActive,
			Concurrency: 5,
		},
		Group: &Group{
			ID:                   groupID,
			Name:                 "VIP-roundtrip",
			Platform:             PlatformOpenAI,
			Status:               StatusActive,
			Hydrated:             true,
			RateMultiplier:       0.06,
			SubscriptionType:     SubscriptionTypeStandard,
			PeakRateEnabled:      false,
			ProfitControlEnabled: true,
			ProfitMinMargin:      0.2,
			ProfitSafetyBuffer:   0.05,
		},
	}
}

// 快照构建 → L2 JSON 往返 → 还原 → 装门：利润字段必须全程保真，阈值与
// 计费同源（0.06 × (1−0.25) = 0.045）。
func TestAPIKeyAuthSnapshotProfitControlRoundtrip(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := profitAuthTestAPIKey()

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version, "认证快照同时携带候选集倍率、路由依赖版本和用户调度配置")

	// 模拟 L2 缓存的完整 JSON 往返（与 apiKeyCache.SetAuthCache/GetAuthCache 同构）。
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var restored APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &restored))

	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &restored)
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, materialized.Group)
	require.True(t, materialized.Group.Hydrated)
	require.True(t, materialized.Group.ProfitControlEnabled)
	require.InDelta(t, 0.2, materialized.Group.ProfitMinMargin, 1e-12)
	require.InDelta(t, 0.05, materialized.Group.ProfitSafetyBuffer, 1e-12)
	require.InDelta(t, 0.06, materialized.Group.RateMultiplier, 1e-12)

	// 中间件语义：materialized.Group 进请求 ctx → 门必须按快照配置装上。
	ctx := context.WithValue(context.Background(), ctxkey.Group, materialized.Group)
	gwSvc := &OpenAIGatewayService{}
	gate := gwSvc.resolveOpenAIProfitControlGate(ctx, materialized.GroupID)
	require.NotNil(t, gate, "还原后的认证分组必须能装门（投影漏列时本断言最先失败）")
	require.InDelta(t, 0.06*(1-0.25), gate.threshold, 1e-12)
}

// 旧版本快照（v16 及更早，无利润字段保真保证）必须被淘汰回源，不得复用。
func TestAPIKeyAuthSnapshotOldVersionEvicted(t *testing.T) {
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), profitAuthTestAPIKey())
	require.NotNil(t, snapshot)
	snapshot.Version = 16

	materialized, used, err := svc.applyAuthCacheEntry("sk-old", &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.False(t, used, "版本不匹配的缓存条目必须淘汰并回源重建")
	require.Nil(t, materialized)
}

func TestAPIKeyAuthSnapshotCarriesOnlyCandidateUserRates(t *testing.T) {
	primaryID, secondaryID := int64(50), int64(51)
	repo := &candidateRateAuthRepoStub{rates: map[int64]float64{primaryID: 0.7, secondaryID: 0.8, 999: 9.9}}
	svc := &APIKeyService{userGroupRateRepo: repo}
	apiKey := profitAuthTestAPIKey()
	apiKey.GroupRoutes = []APIKeyGroupRoute{
		{GroupID: primaryID, Priority: 0, Enabled: true, Group: apiKey.Group},
		{GroupID: secondaryID, Priority: 1, Enabled: true, Group: &Group{ID: secondaryID}},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.ElementsMatch(t, []int64{primaryID, secondaryID}, repo.requestedID)
	require.Equal(t, map[int64]float64{primaryID: 0.7, secondaryID: 0.8}, snapshot.User.GroupRates)

	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var restored APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &restored))
	materialized, used, err := svc.applyAuthCacheEntry("sk-rate-roundtrip", &restored)
	require.NoError(t, err)
	require.True(t, used)
	require.Equal(t, snapshot.User.GroupRates, materialized.User.GroupRates)
}

func TestAPIKeyAuthSnapshotEightCandidatesStaysWithinBoundedPayloadBudget(t *testing.T) {
	const maxAuthSnapshotBytes = 64 << 10
	svc := &APIKeyService{}
	apiKey := profitAuthTestAPIKey()
	apiKey.GroupRoutes = make([]APIKeyGroupRoute, DefaultMaxAPIKeyGroupRoutes)
	for index := range apiKey.GroupRoutes {
		groupID := int64(50 + index)
		group := &Group{
			ID: groupID, Name: "candidate", Platform: PlatformOpenAI,
			Status: StatusActive, SubscriptionType: SubscriptionTypeStandard,
			RateMultiplier: 1, Hydrated: true, AllowImageGeneration: true,
			AllowBatchImageGeneration: true, AllowMessagesDispatch: true, AllowLive: true,
			SupportedModelScopes: []string{"gpt-5", "gpt-5-mini"},
		}
		apiKey.GroupRoutes[index] = APIKeyGroupRoute{GroupID: groupID, Priority: index, Enabled: true, Group: group}
		if index == 0 {
			apiKey.GroupID = &apiKey.GroupRoutes[index].GroupID
			apiKey.Group = group
		}
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.Len(t, snapshot.GroupRoutes, DefaultMaxAPIKeyGroupRoutes)
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	t.Logf("eight-candidate auth snapshot bytes=%d", len(payload))
	require.LessOrEqual(t, len(payload), maxAuthSnapshotBytes)
}
