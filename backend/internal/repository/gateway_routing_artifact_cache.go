package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const routingArtifactObjectTTL = 7 * 24 * time.Hour

var publishRoutingArtifactScript = redis.NewScript(`
local existing = redis.call('GET', KEYS[1])
if existing and existing ~= ARGV[1] then
  return 0
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return 1
`)

var swapRoutingArtifactPointersScript = redis.NewScript(`
if ARGV[1] == '1' then
  local current = redis.call('HGET', KEYS[1], 'active') or ''
  if current ~= ARGV[2] then
    return -1
  end
end
for i = 2, 5 do
  if ARGV[i + 1] ~= '' and redis.call('EXISTS', KEYS[i]) == 0 then
    return -2
  end
end
redis.call('HSET', KEYS[1],
  'baseline', ARGV[3],
  'active', ARGV[4],
  'canary', ARGV[5],
  'shadow', ARGV[6],
  'canary_allocation_bps', ARGV[7],
  'canary_experiment_id', ARGV[8],
  'canary_bucket_salt_checksum', ARGV[9],
  'updated_at', ARGV[10])
return 1
`)

type gatewayRoutingArtifactCache struct{ rdb *redis.Client }

type routingArtifactCacheObject struct {
	ArtifactKind  string          `json:"artifact_kind"`
	Version       string          `json:"version"`
	ParentVersion *string         `json:"parent_version,omitempty"`
	Platform      string          `json:"platform"`
	ModelFamily   string          `json:"model_family"`
	EndpointKind  string          `json:"endpoint_kind"`
	Preference    *string         `json:"preference,omitempty"`
	SchemaVersion string          `json:"schema_version"`
	Checksum      string          `json:"checksum"`
	Payload       json.RawMessage `json:"payload"`
	Dependencies  json.RawMessage `json:"dependencies"`
	Lineage       json.RawMessage `json:"lineage"`
}

func NewGatewayRoutingArtifactCache(rdb *redis.Client) service.RoutingArtifactCache {
	return &gatewayRoutingArtifactCache{rdb: rdb}
}

func (c *gatewayRoutingArtifactCache) PublishArtifact(ctx context.Context, artifact *service.RoutingArtifactVersion) error {
	if c == nil || c.rdb == nil {
		return service.ErrRoutingArtifactUnavailable
	}
	if err := service.ValidateRoutingArtifact(artifact); err != nil {
		return err
	}
	object := routingArtifactCacheObject{
		ArtifactKind: artifact.ArtifactKind, Version: artifact.Version, ParentVersion: artifact.ParentVersion,
		Platform: artifact.Platform, ModelFamily: artifact.ModelFamily, EndpointKind: artifact.EndpointKind,
		Preference: artifact.Preference, SchemaVersion: artifact.SchemaVersion, Checksum: artifact.Checksum,
		Payload: artifact.Payload, Dependencies: artifact.Dependencies, Lineage: artifact.Lineage,
	}
	payload, err := json.Marshal(object)
	if err != nil {
		return err
	}
	scope := service.RoutingArtifactScopeFromVersion(artifact)
	result, err := publishRoutingArtifactScript.Run(ctx, c.rdb,
		[]string{routingArtifactObjectKey(scope, artifact.Version)}, payload, routingArtifactObjectTTL.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return service.ErrRoutingArtifactPointerConflict
	}
	return nil
}

func (c *gatewayRoutingArtifactCache) SwapPointers(ctx context.Context, scope service.RoutingArtifactScope, pointers service.RoutingArtifactPointers, expectedActive *string) error {
	if c == nil || c.rdb == nil {
		return service.ErrRoutingArtifactUnavailable
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if err := service.ValidateRoutingArtifactPointers(pointers); err != nil {
		return err
	}
	hasCAS, expected := "0", ""
	if expectedActive != nil {
		hasCAS, expected = "1", *expectedActive
	}
	keys := []string{
		routingArtifactPointerKey(scope),
		routingArtifactObjectKey(scope, pointers.BaselineVersion),
		routingArtifactObjectKey(scope, pointers.ActiveVersion),
		routingArtifactObjectKey(scope, pointers.CanaryVersion),
		routingArtifactObjectKey(scope, pointers.ShadowVersion),
	}
	result, err := swapRoutingArtifactPointersScript.Run(ctx, c.rdb, keys,
		hasCAS, expected, pointers.BaselineVersion, pointers.ActiveVersion, pointers.CanaryVersion, pointers.ShadowVersion,
		pointers.CanaryAllocationBPS, pointers.CanaryExperimentID, pointers.CanaryBucketSaltChecksum,
		pointers.UpdatedAt.UTC().Format(time.RFC3339Nano)).Int()
	if err != nil {
		return err
	}
	switch result {
	case 1:
		return nil
	case -1:
		return service.ErrRoutingArtifactPointerConflict
	default:
		return service.ErrRoutingArtifactUnavailable
	}
}

func (c *gatewayRoutingArtifactCache) LoadPointers(ctx context.Context, scope service.RoutingArtifactScope) (service.RoutingArtifactPointers, error) {
	if c == nil || c.rdb == nil {
		return service.RoutingArtifactPointers{}, service.ErrRoutingArtifactUnavailable
	}
	if err := scope.Validate(); err != nil {
		return service.RoutingArtifactPointers{}, err
	}
	values, err := c.rdb.HMGet(ctx, routingArtifactPointerKey(scope),
		"baseline", "active", "canary", "shadow", "canary_allocation_bps", "canary_experiment_id", "canary_bucket_salt_checksum", "updated_at").Result()
	if err != nil {
		return service.RoutingArtifactPointers{}, err
	}
	if len(values) != 8 || values[0] == nil || values[1] == nil || values[7] == nil {
		return service.RoutingArtifactPointers{}, service.ErrRoutingArtifactUnavailable
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, fmt.Sprint(values[7]))
	if err != nil {
		return service.RoutingArtifactPointers{}, service.ErrRoutingArtifactUnavailable
	}
	pointers := service.RoutingArtifactPointers{
		BaselineVersion: fmt.Sprint(values[0]), ActiveVersion: fmt.Sprint(values[1]), UpdatedAt: updatedAt,
	}
	if values[2] != nil {
		pointers.CanaryVersion = fmt.Sprint(values[2])
	}
	if values[3] != nil {
		pointers.ShadowVersion = fmt.Sprint(values[3])
	}
	if pointers.CanaryVersion != "" {
		allocation, parseErr := strconv.Atoi(fmt.Sprint(values[4]))
		if parseErr != nil {
			return service.RoutingArtifactPointers{}, service.ErrRoutingArtifactUnavailable
		}
		pointers.CanaryAllocationBPS = allocation
		if values[5] != nil {
			pointers.CanaryExperimentID = fmt.Sprint(values[5])
		}
		if values[6] != nil {
			pointers.CanaryBucketSaltChecksum = fmt.Sprint(values[6])
		}
	}
	if err := service.ValidateRoutingArtifactPointers(pointers); err != nil {
		return service.RoutingArtifactPointers{}, err
	}
	return pointers, nil
}

func (c *gatewayRoutingArtifactCache) LoadArtifact(ctx context.Context, scope service.RoutingArtifactScope, version string) (*service.RoutingArtifactVersion, error) {
	if c == nil || c.rdb == nil || strings.TrimSpace(version) == "" {
		return nil, service.ErrRoutingArtifactUnavailable
	}
	payload, err := c.rdb.Get(ctx, routingArtifactObjectKey(scope, version)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, service.ErrRoutingArtifactUnavailable
	}
	if err != nil {
		return nil, err
	}
	var object routingArtifactCacheObject
	if json.Unmarshal(payload, &object) != nil {
		return nil, service.ErrRoutingArtifactUnavailable
	}
	artifact := service.RoutingArtifactVersion{
		ArtifactKind: object.ArtifactKind, Version: object.Version, ParentVersion: object.ParentVersion,
		Platform: object.Platform, ModelFamily: object.ModelFamily, EndpointKind: object.EndpointKind,
		Preference: object.Preference, Status: service.RoutingLifecycleActive, SchemaVersion: object.SchemaVersion,
		Checksum: object.Checksum, Payload: object.Payload, Dependencies: object.Dependencies, Lineage: object.Lineage,
	}
	if service.ValidateRoutingArtifact(&artifact) != nil ||
		artifact.Version != version || !routingArtifactScopesEqual(scope, service.RoutingArtifactScopeFromVersion(&artifact)) {
		return nil, service.ErrRoutingArtifactUnavailable
	}
	return &artifact, nil
}

func routingArtifactPointerKey(scope service.RoutingArtifactScope) string {
	return fmt.Sprintf("gateway_route_artifact:{%s}:pointers", routingArtifactScopeTag(scope))
}

func routingArtifactObjectKey(scope service.RoutingArtifactScope, version string) string {
	versionHash := sha256.Sum256([]byte(version))
	return fmt.Sprintf("gateway_route_artifact:{%s}:object:%s", routingArtifactScopeTag(scope), hex.EncodeToString(versionHash[:12]))
}

func routingArtifactScopeTag(scope service.RoutingArtifactScope) string {
	preference := ""
	if scope.Preference != nil {
		preference = *scope.Preference
	}
	value := strings.Join([]string{scope.ArtifactKind, scope.Platform, scope.ModelFamily, scope.EndpointKind, preference}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}

func routingArtifactScopesEqual(a, b service.RoutingArtifactScope) bool {
	preferenceA, preferenceB := "", ""
	if a.Preference != nil {
		preferenceA = *a.Preference
	}
	if b.Preference != nil {
		preferenceB = *b.Preference
	}
	return a.ArtifactKind == b.ArtifactKind && a.Platform == b.Platform && a.ModelFamily == b.ModelFamily &&
		a.EndpointKind == b.EndpointKind && preferenceA == preferenceB
}

var _ service.RoutingArtifactCache = (*gatewayRoutingArtifactCache)(nil)
