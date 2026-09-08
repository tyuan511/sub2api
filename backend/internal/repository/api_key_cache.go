package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	apiKeyRateLimitKeyPrefix      = "apikey:ratelimit:"
	apiKeyRateLimitDuration       = 24 * time.Hour
	apiKeyAuthCachePrefix         = "apikey:auth:"
	apiKeyRouteVersionPrefix      = "apikey:route_version:"
	apiKeyDependencyVersionPrefix = "apikey:route_dependency_version:"
	authCacheInvalidateChannel    = "auth:cache:invalidate"
	routeConfigInvalidateChannel  = "route:config:invalidate"
)

// apiKeyRateLimitKey generates the Redis key for API key creation rate limiting.
func apiKeyRateLimitKey(userID int64) string {
	return fmt.Sprintf("%s%d", apiKeyRateLimitKeyPrefix, userID)
}

func apiKeyAuthCacheKey(key string) string {
	return fmt.Sprintf("%s%s", apiKeyAuthCachePrefix, key)
}

func apiKeyRouteVersionKey(apiKeyID int64) string {
	return fmt.Sprintf("%s{%d}", apiKeyRouteVersionPrefix, apiKeyID)
}

func apiKeyDependencyVersionKey(apiKeyID int64) string {
	return fmt.Sprintf("%s{%d}", apiKeyDependencyVersionPrefix, apiKeyID)
}

type apiKeyCache struct {
	rdb *redis.Client
}

func NewAPIKeyCache(rdb *redis.Client) service.APIKeyCache {
	return &apiKeyCache{rdb: rdb}
}

func (c *apiKeyCache) GetCreateAttemptCount(ctx context.Context, userID int64) (int, error) {
	key := apiKeyRateLimitKey(userID)
	count, err := c.rdb.Get(ctx, key).Int()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return count, err
}

func (c *apiKeyCache) IncrementCreateAttemptCount(ctx context.Context, userID int64) error {
	key := apiKeyRateLimitKey(userID)
	pipe := c.rdb.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, apiKeyRateLimitDuration)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *apiKeyCache) DeleteCreateAttemptCount(ctx context.Context, userID int64) error {
	key := apiKeyRateLimitKey(userID)
	return c.rdb.Del(ctx, key).Err()
}

func (c *apiKeyCache) IncrementDailyUsage(ctx context.Context, apiKey string) error {
	return c.rdb.Incr(ctx, apiKey).Err()
}

func (c *apiKeyCache) SetDailyUsageExpiry(ctx context.Context, apiKey string, ttl time.Duration) error {
	return c.rdb.Expire(ctx, apiKey, ttl).Err()
}

func (c *apiKeyCache) GetAuthCache(ctx context.Context, key string) (*service.APIKeyAuthCacheEntry, error) {
	val, err := c.rdb.Get(ctx, apiKeyAuthCacheKey(key)).Bytes()
	if err != nil {
		return nil, err
	}
	var entry service.APIKeyAuthCacheEntry
	if err := json.Unmarshal(val, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (c *apiKeyCache) SetAuthCache(ctx context.Context, key string, entry *service.APIKeyAuthCacheEntry, ttl time.Duration) error {
	if entry == nil {
		return nil
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, apiKeyAuthCacheKey(key), payload, ttl).Err()
}

func (c *apiKeyCache) DeleteAuthCache(ctx context.Context, key string) error {
	return c.rdb.Del(ctx, apiKeyAuthCacheKey(key)).Err()
}

var setAPIKeyRoutingGuardsScript = redis.NewScript(`
local current_route = tonumber(redis.call('GET', KEYS[1]) or '0')
local requested_route = tonumber(ARGV[1])
local current_dependency = tonumber(redis.call('GET', KEYS[2]) or '0')
local requested_dependency = tonumber(ARGV[2])
if requested_route > current_route then
  redis.call('SET', KEYS[1], requested_route, 'PX', ARGV[3])
else
  redis.call('PEXPIRE', KEYS[1], ARGV[3])
end
if requested_dependency > current_dependency then
  redis.call('SET', KEYS[2], requested_dependency, 'PX', ARGV[3])
else
  redis.call('PEXPIRE', KEYS[2], ARGV[3])
end
return {math.max(current_route, requested_route), math.max(current_dependency, requested_dependency)}
`)

func (c *apiKeyCache) GetAPIKeyRoutingGuards(ctx context.Context, apiKeyID int64) (service.APIKeyRoutingGuards, error) {
	values, err := c.rdb.MGet(ctx, apiKeyRouteVersionKey(apiKeyID), apiKeyDependencyVersionKey(apiKeyID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return service.APIKeyRoutingGuards{}, err
	}
	guards := service.APIKeyRoutingGuards{}
	if len(values) > 0 && values[0] != nil {
		guards.RouteVersion, _ = strconv.ParseInt(fmt.Sprint(values[0]), 10, 64)
	}
	if len(values) > 1 && values[1] != nil {
		guards.DependencyVersion, _ = strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)
	}
	return guards, nil
}

func (c *apiKeyCache) SetAPIKeyRoutingGuards(ctx context.Context, apiKeyID, routeVersion, dependencyVersion int64, ttl time.Duration) error {
	if apiKeyID <= 0 || routeVersion <= 0 || dependencyVersion <= 0 || ttl <= 0 {
		return errors.New("invalid API key routing guards")
	}
	return setAPIKeyRoutingGuardsScript.Run(ctx, c.rdb,
		[]string{apiKeyRouteVersionKey(apiKeyID), apiKeyDependencyVersionKey(apiKeyID)},
		routeVersion, dependencyVersion, ttl.Milliseconds()).Err()
}

// PublishAuthCacheInvalidation publishes a cache invalidation message to all instances
func (c *apiKeyCache) PublishAuthCacheInvalidation(ctx context.Context, cacheKey string) error {
	return c.rdb.Publish(ctx, authCacheInvalidateChannel, cacheKey).Err()
}

func (c *apiKeyCache) PublishAPIKeyRouteConfigInvalidation(ctx context.Context, message service.APIKeyRouteConfigInvalidationMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return c.rdb.Publish(ctx, routeConfigInvalidateChannel, payload).Err()
}

// SubscribeAuthCacheInvalidation subscribes to cache invalidation messages
func (c *apiKeyCache) SubscribeAuthCacheInvalidation(ctx context.Context, handler func(cacheKey string)) error {
	pubsub := c.rdb.Subscribe(ctx, authCacheInvalidateChannel)

	// Verify subscription is working
	_, err := pubsub.Receive(ctx)
	if err != nil {
		_ = pubsub.Close()
		return fmt.Errorf("subscribe to auth cache invalidation: %w", err)
	}

	defer func() {
		if err := pubsub.Close(); err != nil {
			log.Printf("Warning: failed to close auth cache invalidation pubsub: %v", err)
		}
	}()
	service.NotifyAuthCacheSubscriptionReady(ctx)

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return errors.New("auth cache invalidation pubsub channel closed")
			}
			if msg != nil {
				handler(msg.Payload)
			}
		}
	}
}
