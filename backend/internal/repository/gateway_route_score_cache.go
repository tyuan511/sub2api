package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const routingScoreScopesKey = "route:score:scopes"

func routingScoreHashTag(scope service.APIKeyRoutingScoreScope) string {
	parts := []string{scope.Platform, scope.ModelFamily, scope.EndpointKind}
	for i := range parts {
		parts[i] = url.PathEscape(strings.ToLower(strings.TrimSpace(parts[i])))
	}
	return "{route-score:" + strings.Join(parts, ":") + "}"
}

func routingScoreVersionKey(scope service.APIKeyRoutingScoreScope, version string) string {
	return routingScoreHashTag(scope) + ":snapshot:" + url.PathEscape(strings.TrimSpace(version))
}

func routingScoreCurrentKey(scope service.APIKeyRoutingScoreScope) string {
	return routingScoreHashTag(scope) + ":current"
}

var publishRoutingScoreSnapshotScript = redis.NewScript(`
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
redis.call('SET', KEYS[2], ARGV[1], 'PX', ARGV[3])
return 1
`)

func (c *gatewayCache) PublishAPIKeyRoutingScoreSnapshot(ctx context.Context, snapshot *service.APIKeyRoutingScoreSnapshot, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	if err := service.ValidateAPIKeyRoutingScoreSnapshot(snapshot); err != nil {
		return err
	}
	if ttl <= 0 {
		return errors.New("routing score snapshot ttl must be positive")
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal routing score snapshot: %w", err)
	}
	scope := service.APIKeyRoutingScoreScope{Platform: snapshot.Platform, ModelFamily: snapshot.ModelFamily, EndpointKind: snapshot.EndpointKind}
	if err := publishRoutingScoreSnapshotScript.Run(ctx, c.rdb, []string{
		routingScoreVersionKey(scope, snapshot.Version),
		routingScoreCurrentKey(scope),
	}, snapshot.Version, body, ttl.Milliseconds()).Err(); err != nil {
		return err
	}
	scopeBody, _ := json.Marshal(scope)
	return c.rdb.SAdd(ctx, routingScoreScopesKey, scopeBody).Err()
}

func (c *gatewayCache) LoadAllCurrentAPIKeyRoutingScoreSnapshots(ctx context.Context) ([]*service.APIKeyRoutingScoreSnapshot, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	encodedScopes, err := c.rdb.SMembers(ctx, routingScoreScopesKey).Result()
	if err != nil {
		return nil, err
	}
	scopes := make([]service.APIKeyRoutingScoreScope, 0, len(encodedScopes))
	for _, encoded := range encodedScopes {
		var scope service.APIKeyRoutingScoreScope
		if json.Unmarshal([]byte(encoded), &scope) == nil && scope.Valid() {
			scopes = append(scopes, scope)
		}
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].Key() < scopes[j].Key() })
	if len(scopes) == 0 {
		return nil, nil
	}

	pointerCommands := make([]*redis.StringCmd, len(scopes))
	_, err = c.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for i, scope := range scopes {
			pointerCommands[i] = pipe.Get(ctx, routingScoreCurrentKey(scope))
		}
		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	type versionedScope struct {
		scope   service.APIKeyRoutingScoreScope
		version string
	}
	versions := make([]versionedScope, 0, len(scopes))
	for i, command := range pointerCommands {
		version, commandErr := command.Result()
		if commandErr == nil && strings.TrimSpace(version) != "" {
			versions = append(versions, versionedScope{scope: scopes[i], version: version})
		}
	}
	if len(versions) == 0 {
		return nil, nil
	}
	bodyCommands := make([]*redis.StringCmd, len(versions))
	_, err = c.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for i, item := range versions {
			bodyCommands[i] = pipe.Get(ctx, routingScoreVersionKey(item.scope, item.version))
		}
		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	result := make([]*service.APIKeyRoutingScoreSnapshot, 0, len(versions))
	for i, command := range bodyCommands {
		body, commandErr := command.Bytes()
		if commandErr != nil {
			continue
		}
		var snapshot service.APIKeyRoutingScoreSnapshot
		if json.Unmarshal(body, &snapshot) != nil || snapshot.Version != versions[i].version || service.ValidateAPIKeyRoutingScoreSnapshot(&snapshot) != nil {
			continue
		}
		result = append(result, &snapshot)
	}
	return result, nil
}

func (c *gatewayCache) LoadCurrentAPIKeyRoutingScoreSnapshot(ctx context.Context, scope service.APIKeyRoutingScoreScope) (*service.APIKeyRoutingScoreSnapshot, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	if !scope.Valid() {
		return nil, errors.New("routing score scope is invalid")
	}
	version, err := c.rdb.Get(ctx, routingScoreCurrentKey(scope)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, service.ErrAPIKeyRoutingScoreSnapshotNotFound
	}
	if err != nil {
		return nil, err
	}
	body, err := c.rdb.Get(ctx, routingScoreVersionKey(scope, version)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, service.ErrAPIKeyRoutingScoreSnapshotNotFound
	}
	if err != nil {
		return nil, err
	}
	var snapshot service.APIKeyRoutingScoreSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return nil, fmt.Errorf("decode routing score snapshot: %w", err)
	}
	if snapshot.Version != version {
		return nil, errors.New("routing score current pointer version mismatch")
	}
	if err := service.ValidateAPIKeyRoutingScoreSnapshot(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

var _ service.APIKeyRoutingScoreCache = (*gatewayCache)(nil)
