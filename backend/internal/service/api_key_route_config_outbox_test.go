package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/require"
)

type apiKeyRouteConfigOutboxRepoStub struct {
	mu           sync.Mutex
	events       []APIKeyRouteConfigOutboxEvent
	claimLimit   int
	delivered    []int64
	retried      []int64
	retryError   string
	cleanupCalls int
	cleaned      int64
	stats        APIKeyRouteConfigOutboxStats
	statsErr     error
}

func (r *apiKeyRouteConfigOutboxRepoStub) ClaimAPIKeyRouteConfigEvents(_ context.Context, _ string, limit int, _ time.Duration) ([]APIKeyRouteConfigOutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claimLimit = limit
	return append([]APIKeyRouteConfigOutboxEvent(nil), r.events...), nil
}

func (r *apiKeyRouteConfigOutboxRepoStub) MarkAPIKeyRouteConfigEventDelivered(_ context.Context, id int64, _ string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.delivered = append(r.delivered, id)
	return nil
}

func (r *apiKeyRouteConfigOutboxRepoStub) RetryAPIKeyRouteConfigEvent(_ context.Context, id int64, _ string, _ time.Time, lastError string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.retried = append(r.retried, id)
	r.retryError = lastError
	return nil
}

func (r *apiKeyRouteConfigOutboxRepoStub) APIKeyRouteConfigOutboxStats(context.Context) (APIKeyRouteConfigOutboxStats, error) {
	return r.stats, r.statsErr
}

func (r *apiKeyRouteConfigOutboxRepoStub) CleanupDeliveredAPIKeyRouteConfigEvents(_ context.Context, _ time.Time, _ int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupCalls++
	return r.cleaned, nil
}

type apiKeyRouteConfigCacheStub struct {
	*authInvalidationCacheStub
	mu                sync.Mutex
	guardVersion      int64
	dependencyVersion int64
	guardReads        int
	guardErr          error
	setGuardErr       error
	routePublishErr   error
	routeMessages     []APIKeyRouteConfigInvalidationMessage
}

func newAPIKeyRouteConfigCacheStub() *apiKeyRouteConfigCacheStub {
	return &apiKeyRouteConfigCacheStub{authInvalidationCacheStub: &authInvalidationCacheStub{}}
}

func (c *apiKeyRouteConfigCacheStub) GetAPIKeyRoutingGuards(context.Context, int64) (APIKeyRoutingGuards, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.guardReads++
	return APIKeyRoutingGuards{RouteVersion: c.guardVersion, DependencyVersion: c.dependencyVersion}, c.guardErr
}

func (c *apiKeyRouteConfigCacheStub) SetAPIKeyRoutingGuards(_ context.Context, _ int64, routeVersion, dependencyVersion int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.setGuardErr != nil {
		return c.setGuardErr
	}
	if routeVersion > c.guardVersion {
		c.guardVersion = routeVersion
	}
	if dependencyVersion > c.dependencyVersion {
		c.dependencyVersion = dependencyVersion
	}
	return nil
}

func (c *apiKeyRouteConfigCacheStub) PublishAPIKeyRouteConfigInvalidation(_ context.Context, message APIKeyRouteConfigInvalidationMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routeMessages = append(c.routeMessages, message)
	return c.routePublishErr
}

func validAPIKeyRouteConfigOutboxEvent(id, version int64) APIKeyRouteConfigOutboxEvent {
	return APIKeyRouteConfigOutboxEvent{
		ID: id, EventKey: "api_key_route:7:v2", APIKeyID: 7, OldRouteVersion: version - 1,
		RouteVersion: version, OldDependencyVersion: 0, DependencyVersion: 1, EventType: "api_key_route_config_changed",
		AuthCacheKey: strings.Repeat("a", 64), CreatedAt: time.Now().UTC(),
	}
}

func TestAPIKeyRouteConfigOutboxWorker_DeliversIdempotentInvalidation(t *testing.T) {
	repo := &apiKeyRouteConfigOutboxRepoStub{}
	cache := newAPIKeyRouteConfigCacheStub()
	worker := NewAPIKeyRouteConfigOutboxWorker(repo, cache)
	event := validAPIKeyRouteConfigOutboxEvent(1, 2)

	worker.processEvent(context.Background(), event)
	worker.processEvent(context.Background(), event) // crash-after-side-effect redelivery

	require.Equal(t, int64(2), cache.guardVersion)
	require.Equal(t, int64(1), cache.dependencyVersion)
	require.Equal(t, []string{event.AuthCacheKey, event.AuthCacheKey}, cache.deleted)
	require.Equal(t, []string{event.AuthCacheKey, event.AuthCacheKey}, cache.published)
	require.Len(t, cache.routeMessages, 2)
	require.Equal(t, APIKeyRouteConfigInvalidationMessage{
		EventID: event.EventKey, APIKeyID: 7, OldRouteVersion: 1, NewRouteVersion: 2,
		OldDependencyVersion: 0, NewDependencyVersion: 1,
		Reason: "api_key_route_config_changed",
	}, cache.routeMessages[0])
	require.Equal(t, []int64{1, 1}, repo.delivered)
}

func TestAPIKeyRouteConfigOutboxWorker_RetriesPartialRedisDelivery(t *testing.T) {
	repo := &apiKeyRouteConfigOutboxRepoStub{}
	cache := newAPIKeyRouteConfigCacheStub()
	cache.routePublishErr = errors.New("route pubsub unavailable")
	worker := NewAPIKeyRouteConfigOutboxWorker(repo, cache)
	event := validAPIKeyRouteConfigOutboxEvent(2, 3)

	worker.processEvent(context.Background(), event)

	require.Equal(t, []int64{2}, repo.retried)
	require.Empty(t, repo.delivered)
	require.Contains(t, repo.retryError, "pubsub")
	require.Equal(t, uint64(1), worker.Health(context.Background()).Retries)
}

func TestAPIKeyRouteConfigOutboxWorker_DeliversDependencyOnlyTransition(t *testing.T) {
	repo := &apiKeyRouteConfigOutboxRepoStub{}
	cache := newAPIKeyRouteConfigCacheStub()
	worker := NewAPIKeyRouteConfigOutboxWorker(repo, cache)
	event := validAPIKeyRouteConfigOutboxEvent(3, 4)
	event.EventKey = "api_key_dependency:7:v9"
	event.OldRouteVersion = 4
	event.OldDependencyVersion = 8
	event.DependencyVersion = 9
	event.EventType = "api_key_route_dependency_changed"

	worker.processEvent(context.Background(), event)

	require.Equal(t, int64(4), cache.guardVersion)
	require.Equal(t, int64(9), cache.dependencyVersion)
	require.Equal(t, []int64{3}, repo.delivered)
	require.Equal(t, int64(4), cache.routeMessages[0].NewRouteVersion)
	require.Equal(t, int64(9), cache.routeMessages[0].NewDependencyVersion)
}

func TestAPIKeyRouteConfigOutboxWorker_BoundedBatchCleanupAndHealth(t *testing.T) {
	oldest := time.Now().Add(-time.Minute)
	repo := &apiKeyRouteConfigOutboxRepoStub{cleaned: 4, stats: APIKeyRouteConfigOutboxStats{
		Pending: 3, DeliveredRetained: 9, OldestCreatedAt: &oldest, MaxAttempts: 5, LastError: "redis down",
	}}
	worker := NewAPIKeyRouteConfigOutboxWorker(repo, newAPIKeyRouteConfigCacheStub())
	require.NoError(t, worker.processBatch(context.Background()))
	require.Equal(t, apiKeyRouteConfigOutboxBatchSize, repo.claimLimit)
	require.Equal(t, 1, repo.cleanupCalls)
	health := worker.Health(context.Background())
	require.Equal(t, int64(3), health.Pending)
	require.Equal(t, int64(9), health.DeliveredRetained)
	require.Equal(t, uint64(4), health.Cleaned)
	require.Equal(t, 5, health.MaxAttempts)
	require.Equal(t, "redis down", health.LastError)
	require.GreaterOrEqual(t, health.OldestLag, time.Minute)
}

func TestAPIKeyAuthCacheRouteVersionGuardRejectsMissedPubSubSnapshot(t *testing.T) {
	cache := newAPIKeyRouteConfigCacheStub()
	cache.guardVersion = 3
	cache.dependencyVersion = 2
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, cache, nil)
	allowRoutingUsersForTest(t, svc, 77)
	local, err := ristretto.NewCache(&ristretto.Config{NumCounters: 100, MaxCost: 10, BufferItems: 64})
	require.NoError(t, err)
	defer local.Close()
	svc.authCacheL1 = local
	svc.authCfg.l1TTL = time.Minute
	entry := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{
		Version: apiKeyAuthSnapshotVersion, APIKeyID: 7, RouteVersion: 2, RoutingDependencyVersion: 2,
	}}
	entry.Snapshot.UserID = 77
	entry.Snapshot.GroupRoutes = []APIKeyAuthGroupRouteSnapshot{{GroupID: 11, Enabled: true}, {GroupID: 12, Priority: 1, Enabled: true}}
	_ = local.SetWithTTL("digest", entry, 1, time.Minute)
	local.Wait()
	require.False(t, svc.authCacheRouteVersionCurrent(context.Background(), "digest", entry))
	require.Equal(t, []string{"digest"}, cache.deleted)
}

func TestAPIKeyAuthCacheRouteVersionGuardFailsOpenDuringRedisLoss(t *testing.T) {
	cache := newAPIKeyRouteConfigCacheStub()
	cache.guardErr = errors.New("redis unavailable")
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, cache, nil)
	allowRoutingUsersForTest(t, svc, 77)
	entry := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{APIKeyID: 7, RouteVersion: 2, RoutingDependencyVersion: 1}}
	entry.Snapshot.UserID = 77
	entry.Snapshot.GroupRoutes = []APIKeyAuthGroupRouteSnapshot{{GroupID: 11, Enabled: true}, {GroupID: 12, Priority: 1, Enabled: true}}
	require.True(t, svc.authCacheRouteVersionCurrent(context.Background(), "digest", entry))
	require.Empty(t, cache.deleted)
}

func TestAPIKeyAuthCacheDependencyGuardRejectsStaleCandidatePermissions(t *testing.T) {
	cache := newAPIKeyRouteConfigCacheStub()
	cache.guardVersion = 4
	cache.dependencyVersion = 9
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, cache, nil)
	allowRoutingUsersForTest(t, svc, 77)
	entry := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{
		APIKeyID: 7, RouteVersion: 4, RoutingDependencyVersion: 8,
	}}
	entry.Snapshot.UserID = 77
	entry.Snapshot.GroupRoutes = []APIKeyAuthGroupRouteSnapshot{{GroupID: 11, Enabled: true}, {GroupID: 12, Priority: 1, Enabled: true}}
	require.False(t, svc.authCacheRouteVersionCurrent(context.Background(), "digest", entry))
	require.Equal(t, []string{"digest"}, cache.deleted)
}

func TestValidateAPIKeyRouteConfigOutboxEventRejectsSensitiveOrMalformedPayload(t *testing.T) {
	event := validAPIKeyRouteConfigOutboxEvent(1, 2)
	event.AuthCacheKey = "sk-plaintext"
	require.ErrorContains(t, validateAPIKeyRouteConfigOutboxEvent(event), "digest")
	event = validAPIKeyRouteConfigOutboxEvent(1, 2)
	event.OldRouteVersion = event.RouteVersion
	event.OldDependencyVersion = event.DependencyVersion
	require.ErrorContains(t, validateAPIKeyRouteConfigOutboxEvent(event), "transition")
}
