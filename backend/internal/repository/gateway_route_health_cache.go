package repository

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

var allowAPIKeyRouteScript = redis.NewScript(`
local state = redis.call('HGET', KEYS[1], 'state')
local now_ms = tonumber(ARGV[1])
local threshold = tonumber(ARGV[4] or '50')
local version = tonumber(ARGV[7] or '0')
local stored_version = tonumber(redis.call('HGET', KEYS[1], 'policy_version') or '0')
if version >= stored_version then
  if state ~= false then redis.call('HSET', KEYS[1], 'threshold_percent', threshold, 'policy_version', version) end
else
  threshold = tonumber(redis.call('HGET', KEYS[1], 'threshold_percent') or '50')
end
-- Re-evaluate an increased threshold against the rolling evidence without
-- clearing the existing breaker or session binding. Equality stays admitted.
if (state == false or state == 'CLOSED') and ARGV[8] == '1' and version >= stored_version then
  redis.call('HSET', KEYS[1], 'state', 'OPEN', 'opened_ms', now_ms, 'threshold_percent', threshold, 'policy_version', version)
  redis.call('PEXPIRE', KEYS[1], ARGV[6])
  return {0, 'OPEN'}
end
if state == 'CLOSED' and ARGV[6] then
  local bucket_ms = math.max(1000, math.floor(tonumber(ARGV[6]) / 10))
  local bucket = math.floor(now_ms / bucket_ms)
  local reset = tonumber(redis.call('HGET', KEYS[1], 'window_reset_bucket') or '-9223372036854775808')
  local successes, failures = 0, 0
  for offset = 0, 9 do
    local current = bucket - offset
    if current >= reset then
      successes = successes + tonumber(redis.call('HGET', KEYS[1], 'b:' .. current .. ':s') or '0')
      failures = failures + tonumber(redis.call('HGET', KEYS[1], 'b:' .. current .. ':f') or '0')
    end
  end
  if successes + failures >= tonumber(ARGV[5]) and successes * 100 < (successes + failures) * threshold then
    redis.call('HSET', KEYS[1], 'state', 'OPEN', 'opened_ms', now_ms)
    redis.call('PEXPIRE', KEYS[1], ARGV[6])
    return {0, 'OPEN'}
  end
end
if state == false or state == 'CLOSED' then
  return {1, 'CLOSED'}
end
if state == 'OPEN' then
  local opened_ms = tonumber(redis.call('HGET', KEYS[1], 'opened_ms') or '0')
  if now_ms - opened_ms < tonumber(ARGV[2]) then
    return {0, 'OPEN'}
  end
  local probe_key = KEYS[1] .. ':probe'
  local claimed = redis.call('SET', probe_key, now_ms, 'PX', ARGV[3], 'NX')
  if claimed then
    redis.call('HSET', KEYS[1], 'state', 'HALF_OPEN')
    return {1, 'HALF_OPEN'}
  end
  return {0, 'HALF_OPEN'}
end
if state == 'HALF_OPEN' then
  local probe_key = KEYS[1] .. ':probe'
  if redis.call('EXISTS', probe_key) == 1 then
    return {0, 'HALF_OPEN'}
  end
  -- The previous probe owner may have crashed. Once its bounded lease expires,
  -- exactly one instance can reclaim the probe instead of leaving the breaker
  -- permanently stranded in HALF_OPEN.
  local reclaimed = redis.call('SET', probe_key, now_ms, 'PX', ARGV[3], 'NX')
  if reclaimed then
    return {1, 'HALF_OPEN'}
  end
  return {0, 'HALF_OPEN'}
end
if state == 'RECOVERING' then
  local successes = tonumber(redis.call('HGET', KEYS[1], 'recovery_successes') or '0')
  local admission = redis.call('HINCRBY', KEYS[1], 'recovery_admissions', 1)
  local divisor = 4
  if successes >= 3 then divisor = 2 end
  if successes >= 7 then divisor = 1 end
  if (admission % divisor) == 0 then
    return {1, 'RECOVERING'}
  end
  return {0, 'RECOVERING'}
end
return {1, state}
`)

var recordAPIKeyRouteResultScript = redis.NewScript(`
local success = tonumber(ARGV[1])
local now_ms = tonumber(ARGV[2])
local window_ms = tonumber(ARGV[3])
local min_samples = tonumber(ARGV[4])
local recovery_target = tonumber(ARGV[5])
local bucket_ms = tonumber(ARGV[6])
local bucket_count = tonumber(ARGV[7])
local threshold = tonumber(ARGV[8] or '50')
local version = tonumber(ARGV[9] or '0')
local stored_version = tonumber(redis.call('HGET', KEYS[1], 'policy_version') or '0')
if version >= stored_version then
  redis.call('HSET', KEYS[1], 'threshold_percent', threshold, 'policy_version', version)
else
  threshold = tonumber(redis.call('HGET', KEYS[1], 'threshold_percent') or '50')
end
local bucket = math.floor(now_ms / bucket_ms)
local state = redis.call('HGET', KEYS[1], 'state') or 'CLOSED'

if state == 'HALF_OPEN' then
  redis.call('DEL', KEYS[1] .. ':probe')
  if success == 1 then
    redis.call('HSET', KEYS[1], 'state', 'RECOVERING', 'successes', 1, 'failures', 0, 'recovery_successes', 1, 'recovery_admissions', 0)
    redis.call('PEXPIRE', KEYS[1], window_ms)
    return 'RECOVERING'
  end
  redis.call('HSET', KEYS[1], 'state', 'OPEN', 'opened_ms', now_ms, 'successes', 0, 'failures', 1, 'recovery_successes', 0, 'recovery_admissions', 0)
  redis.call('PEXPIRE', KEYS[1], window_ms)
  return 'OPEN'
end

if state == 'RECOVERING' then
  if success == 0 then
    redis.call('HSET', KEYS[1], 'state', 'OPEN', 'opened_ms', now_ms, 'successes', 0, 'failures', 1, 'recovery_successes', 0, 'recovery_admissions', 0)
    redis.call('PEXPIRE', KEYS[1], window_ms)
    return 'OPEN'
  end
  local recovered = redis.call('HINCRBY', KEYS[1], 'recovery_successes', 1)
  redis.call('HINCRBY', KEYS[1], 'successes', 1)
  if recovered >= recovery_target then
    redis.call('HDEL', KEYS[1], 'b:' .. bucket .. ':s', 'b:' .. bucket .. ':f')
    redis.call('HSET', KEYS[1], 'state', 'CLOSED', 'successes', 0, 'failures', 0, 'recovery_successes', 0, 'recovery_admissions', 0, 'window_reset_bucket', bucket)
    redis.call('PEXPIRE', KEYS[1], window_ms)
    return 'CLOSED'
  end
  redis.call('PEXPIRE', KEYS[1], window_ms)
  return 'RECOVERING'
end

local success_field = 'b:' .. bucket .. ':s'
local failure_field = 'b:' .. bucket .. ':f'
if success == 1 then redis.call('HINCRBY', KEYS[1], success_field, 1)
else redis.call('HINCRBY', KEYS[1], failure_field, 1) end

-- Continuously active keys retain only the rolling ring. Sparse keys expire as
-- a whole, so the hash cannot grow with the lifetime of an API key.
local expired_bucket = bucket - bucket_count
redis.call('HDEL', KEYS[1], 'b:' .. expired_bucket .. ':s', 'b:' .. expired_bucket .. ':f')

local successes = 0
local failures = 0
local reset_bucket = tonumber(redis.call('HGET', KEYS[1], 'window_reset_bucket') or '-9223372036854775808')
for offset = 0, bucket_count - 1 do
  local current = bucket - offset
  if current >= reset_bucket then
    successes = successes + tonumber(redis.call('HGET', KEYS[1], 'b:' .. current .. ':s') or '0')
    failures = failures + tonumber(redis.call('HGET', KEYS[1], 'b:' .. current .. ':f') or '0')
  end
end
redis.call('HSET', KEYS[1], 'successes', successes, 'failures', failures)
local total = successes + failures
if state == 'OPEN' then
  redis.call('PEXPIRE', KEYS[1], window_ms)
  return 'OPEN'
end
if total >= min_samples and successes * 100 < total * threshold then
  redis.call('HSET', KEYS[1], 'state', 'OPEN', 'opened_ms', now_ms)
  redis.call('PEXPIRE', KEYS[1], window_ms)
  return 'OPEN'
end
redis.call('HSET', KEYS[1], 'state', 'CLOSED')
redis.call('PEXPIRE', KEYS[1], window_ms)
return 'CLOSED'
`)

func (c *gatewayCache) AllowAPIKeyRoute(ctx context.Context, key string, now time.Time, cooldown, probeLease time.Duration) (bool, string, error) {
	return c.AllowAPIKeyRouteWithThreshold(ctx, key, now, cooldown, probeLease, 5*time.Minute, 10, 50, 0, false)
}

func (c *gatewayCache) AllowAPIKeyRouteWithThreshold(ctx context.Context, key string, now time.Time, cooldown, probeLease, window time.Duration, minSamples, minimum int, version int64, belowSharedGate bool) (bool, string, error) {
	forceOpen := 0
	if belowSharedGate {
		forceOpen = 1
	}
	result, err := allowAPIKeyRouteScript.Run(ctx, c.rdb, []string{key}, now.UnixMilli(), cooldown.Milliseconds(), probeLease.Milliseconds(), minimum, minSamples, window.Milliseconds(), version, forceOpen).Slice()
	if err != nil {
		return false, "", err
	}
	allowed, _ := result[0].(int64)
	state, _ := result[1].(string)
	return allowed == 1, state, nil
}

func (c *gatewayCache) RecordAPIKeyRouteResult(ctx context.Context, key string, success bool, now time.Time, window time.Duration, minSamples, recoverySuccesses int) (string, error) {
	return c.RecordAPIKeyRouteResultWithThreshold(ctx, key, success, now, window, minSamples, recoverySuccesses, 50, 0)
}

func (c *gatewayCache) RecordAPIKeyRouteResultWithThreshold(ctx context.Context, key string, success bool, now time.Time, window time.Duration, minSamples, recoverySuccesses, minimum int, version int64) (string, error) {
	value := 0
	if success {
		value = 1
	}
	const bucketCount = 10
	bucketWidth := window / bucketCount
	if bucketWidth < time.Second {
		bucketWidth = time.Second
	}
	state, err := recordAPIKeyRouteResultScript.Run(ctx, c.rdb, []string{key}, value, now.UnixMilli(), window.Milliseconds(), minSamples, recoverySuccesses, bucketWidth.Milliseconds(), bucketCount, minimum, version).Text()
	return state, err
}

func (c *gatewayCache) LoadAPIKeyRouteBreakers(ctx context.Context, keys []string) ([]service.APIKeyRouteBreakerSnapshot, error) {
	result := make([]service.APIKeyRouteBreakerSnapshot, len(keys))
	if len(keys) == 0 {
		return result, nil
	}
	commands := make([]*redis.MapStringStringCmd, len(keys))
	_, err := c.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for index, key := range keys {
			commands[index] = pipe.HGetAll(ctx, key)
		}
		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	for index, command := range commands {
		values, commandErr := command.Result()
		if commandErr != nil && !errors.Is(commandErr, redis.Nil) {
			return nil, commandErr
		}
		result[index] = service.APIKeyRouteBreakerSnapshot{
			State: values["state"], Successes: parseRouteBreakerInt64(values["successes"]),
			Failures: parseRouteBreakerInt64(values["failures"]), RecoverySuccesses: parseRouteBreakerInt64(values["recovery_successes"]),
			RecoveryAdmissions: parseRouteBreakerInt64(values["recovery_admissions"]), OpenedAtUnixMS: parseRouteBreakerInt64(values["opened_ms"]),
		}
	}
	return result, nil
}

func (c *gatewayCache) LoadAPIKeyRouteRuntimeState(ctx context.Context, stickyNamespaceID int64, stickySessionKey string, breakerKeys []string) (int64, []service.APIKeyRouteBreakerSnapshot, error) {
	breakers := make([]service.APIKeyRouteBreakerSnapshot, len(breakerKeys))
	if stickySessionKey == "" && len(breakerKeys) == 0 {
		return 0, breakers, nil
	}
	var stickyCommand *redis.StringCmd
	commands := make([]*redis.MapStringStringCmd, len(breakerKeys))
	_, err := c.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		if stickySessionKey != "" {
			stickyCommand = pipe.Get(ctx, buildSessionKey(stickyNamespaceID, stickySessionKey))
		}
		for index, key := range breakerKeys {
			commands[index] = pipe.HGetAll(ctx, key)
		}
		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, nil, err
	}
	stickyGroupID := int64(0)
	if stickyCommand != nil {
		stickyGroupID, err = stickyCommand.Int64()
		if err != nil && !errors.Is(err, redis.Nil) {
			return 0, nil, err
		}
	}
	for index, command := range commands {
		values, commandErr := command.Result()
		if commandErr != nil && !errors.Is(commandErr, redis.Nil) {
			return 0, nil, commandErr
		}
		breakers[index] = service.APIKeyRouteBreakerSnapshot{
			State: values["state"], Successes: parseRouteBreakerInt64(values["successes"]),
			Failures: parseRouteBreakerInt64(values["failures"]), RecoverySuccesses: parseRouteBreakerInt64(values["recovery_successes"]),
			RecoveryAdmissions: parseRouteBreakerInt64(values["recovery_admissions"]), OpenedAtUnixMS: parseRouteBreakerInt64(values["opened_ms"]),
		}
	}
	return stickyGroupID, breakers, nil
}

func (c *gatewayCache) DeleteAPIKeyRouteBreakers(ctx context.Context, keys []string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	allKeys := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		allKeys = append(allKeys, key, key+":probe")
	}
	return c.rdb.Del(ctx, allKeys...).Result()
}

func parseRouteBreakerInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

var _ service.APIKeyRouteBreakerOperationsCache = (*gatewayCache)(nil)
var _ service.APIKeyRouteRuntimeStateCache = (*gatewayCache)(nil)
