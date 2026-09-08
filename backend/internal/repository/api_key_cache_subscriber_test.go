package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyCacheSubscriber_BlocksUntilContextCancellation(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	cache := NewAPIKeyCache(client)
	ctx, cancel := context.WithCancel(context.Background())
	received := make(chan string, 1)
	returned := make(chan error, 1)
	go func() {
		returned <- cache.SubscribeAuthCacheInvalidation(ctx, func(value string) { received <- value })
	}()

	var value string
	require.Eventually(t, func() bool {
		require.NoError(t, client.Publish(context.Background(), authCacheInvalidateChannel, "hash").Err())
		select {
		case value = <-received:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, "hash", value)
	select {
	case err := <-returned:
		t.Fatalf("subscriber returned while connection was active: %v", err)
	default:
	}
	cancel()
	select {
	case err := <-returned:
		require.True(t, errors.Is(err, context.Canceled))
	case <-time.After(time.Second):
		t.Fatal("subscriber did not stop after context cancellation")
	}
}

func TestAPIKeyCacheRouteVersionGuardIsMonotonicAndExpiring(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	cache, ok := NewAPIKeyCache(client).(service.APIKeyRouteConfigCache)
	require.True(t, ok)

	require.NoError(t, cache.SetAPIKeyRoutingGuards(context.Background(), 17, 4, 6, time.Hour))
	require.NoError(t, cache.SetAPIKeyRoutingGuards(context.Background(), 17, 3, 5, time.Hour))
	guards, err := cache.GetAPIKeyRoutingGuards(context.Background(), 17)
	require.NoError(t, err)
	require.Equal(t, service.APIKeyRoutingGuards{RouteVersion: 4, DependencyVersion: 6}, guards)
	require.Equal(t, time.Hour, server.TTL(apiKeyRouteVersionKey(17)))
	require.Equal(t, time.Hour, server.TTL(apiKeyDependencyVersionKey(17)))
}

func TestAPIKeyCachePublishesBoundedRouteConfigMessage(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	cache, ok := NewAPIKeyCache(client).(service.APIKeyRouteConfigCache)
	require.True(t, ok)
	pubsub := client.Subscribe(context.Background(), routeConfigInvalidateChannel)
	defer func() { _ = pubsub.Close() }()
	_, err := pubsub.Receive(context.Background())
	require.NoError(t, err)

	want := service.APIKeyRouteConfigInvalidationMessage{
		EventID: "api_key_route:17:v4", APIKeyID: 17, OldRouteVersion: 3,
		NewRouteVersion: 4, OldDependencyVersion: 5, NewDependencyVersion: 6,
		Reason: "api_key_route_config_changed",
	}
	require.NoError(t, cache.PublishAPIKeyRouteConfigInvalidation(context.Background(), want))
	message, err := pubsub.ReceiveMessage(context.Background())
	require.NoError(t, err)
	var got service.APIKeyRouteConfigInvalidationMessage
	require.NoError(t, json.Unmarshal([]byte(message.Payload), &got))
	require.Equal(t, want, got)
	require.NotContains(t, message.Payload, "sk-")
}
