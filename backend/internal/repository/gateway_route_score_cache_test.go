package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestGatewayRouteScoreCachePublishesVersionBeforeCurrentPointer(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := &gatewayCache{rdb: client}
	now := time.Now().UTC().Truncate(time.Millisecond)
	snapshot := &service.APIKeyRoutingScoreSnapshot{
		Version: "score-v1", StrategyVersion: "strategy-v1", FeatureVersion: "feature-v1",
		Platform: service.PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", GeneratedAt: now,
		Groups: map[int64]service.APIKeyRoutingGroupObservation{7: {GroupID: 7, SuccessRequests: 10, NormalizedRate: 1}},
	}
	ctx := context.Background()
	if err := cache.PublishAPIKeyRoutingScoreSnapshot(ctx, snapshot, 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := cache.LoadCurrentAPIKeyRoutingScoreSnapshot(ctx, service.APIKeyRoutingScoreScope{Platform: service.PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != snapshot.Version || got.Groups[7].SuccessRequests != 10 {
		t.Fatalf("unexpected loaded snapshot: %#v", got)
	}
	all, err := cache.LoadAllCurrentAPIKeyRoutingScoreSnapshots(ctx)
	if err != nil || len(all) != 1 || all[0].Version != snapshot.Version {
		t.Fatalf("batch current load = %#v, %v", all, err)
	}
	mr.FastForward(4 * time.Minute)
	_, err = cache.LoadCurrentAPIKeyRoutingScoreSnapshot(ctx, service.APIKeyRoutingScoreScope{Platform: service.PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses"})
	if !errors.Is(err, service.ErrAPIKeyRoutingScoreSnapshotNotFound) {
		t.Fatalf("expired snapshot error = %v", err)
	}
}

func TestGatewayRouteScoreKeysShareClusterHashTag(t *testing.T) {
	scope := service.APIKeyRoutingScoreScope{Platform: "openai", ModelFamily: "gpt-5", EndpointKind: "responses"}
	versionKey := routingScoreVersionKey(scope, "v1")
	currentKey := routingScoreCurrentKey(scope)
	if routingScoreHashTag(scope) == "" || !strings.Contains(versionKey, routingScoreHashTag(scope)) || !strings.Contains(currentKey, routingScoreHashTag(scope)) {
		t.Fatalf("score keys do not share hash tag: %q %q", versionKey, currentKey)
	}
}
