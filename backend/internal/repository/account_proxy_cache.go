package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/redis/go-redis/v9"
)

const (
	// 账号内每个代理的并发槽位（有序集合）
	// 格式: concurrency:account_proxy:{accountID}:{proxyID}，成员为 requestID，分数为时间戳
	accountProxySlotKeyPrefix = "concurrency:account_proxy:"
	// 会话粘性代理绑定
	// 格式: sticky_proxy:{accountID}:{sessionHash}
	stickyProxyKeyPrefix = "sticky_proxy:"
)

func accountProxySlotKey(accountID, proxyID int64) string {
	return fmt.Sprintf("%s%d:%d", accountProxySlotKeyPrefix, accountID, proxyID)
}

func stickyProxyKey(accountID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", stickyProxyKeyPrefix, accountID, sessionHash)
}

// acquireProxySlotScript 与账号槽位同构：先按 TTL 清理过期成员，再判满、占位。
var acquireProxySlotScript = redis.NewScript(`
	redis.replicate_commands()
	local key = KEYS[1]
	local maxConcurrency = tonumber(ARGV[1])
	local ttl = tonumber(ARGV[2])
	local requestID = ARGV[3]

	local timeResult = redis.call('TIME')
	local now = tonumber(timeResult[1])
	redis.call('ZREMRANGEBYSCORE', key, '-inf', now - ttl)

	local exists = redis.call('ZSCORE', key, requestID)
	if exists ~= false then
		redis.call('ZADD', key, now, requestID)
		redis.call('EXPIRE', key, ttl)
		return 1
	end

	local current = redis.call('ZCARD', key)
	if maxConcurrency > 0 and current >= maxConcurrency then
		return 0
	end

	redis.call('ZADD', key, now, requestID)
	redis.call('EXPIRE', key, ttl)
	return 1
`)

// AcquireAccountProxySlot 占用「账号 × 代理」维度的一个并发槽位。
func (c *concurrencyCache) AcquireAccountProxySlot(ctx context.Context, accountID, proxyID int64, maxConcurrency int, requestID string) (bool, error) {
	if accountID <= 0 || proxyID <= 0 || requestID == "" {
		return false, nil
	}
	result, err := acquireProxySlotScript.Run(
		ctx,
		c.rdb,
		[]string{accountProxySlotKey(accountID, proxyID)},
		maxConcurrency,
		c.slotTTLSeconds,
		requestID,
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

// ReleaseAccountProxySlot 释放「账号 × 代理」维度的并发槽位。
func (c *concurrencyCache) ReleaseAccountProxySlot(ctx context.Context, accountID, proxyID int64, requestID string) error {
	if accountID <= 0 || proxyID <= 0 || requestID == "" {
		return nil
	}
	return c.rdb.ZRem(ctx, accountProxySlotKey(accountID, proxyID), requestID).Err()
}

// GetAccountProxyConcurrencyBatch 批量读取某账号下各代理当前的在途请求数。
func (c *concurrencyCache) GetAccountProxyConcurrencyBatch(ctx context.Context, accountID int64, proxyIDs []int64) (map[int64]int, error) {
	result := make(map[int64]int, len(proxyIDs))
	if accountID <= 0 || len(proxyIDs) == 0 {
		return result, nil
	}

	now, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("redis TIME: %w", err)
	}
	cutoff := strconv.FormatInt(now.Unix()-int64(c.slotTTLSeconds), 10)

	pipe := c.rdb.Pipeline()
	cmds := make(map[int64]*redis.IntCmd, len(proxyIDs))
	for _, proxyID := range proxyIDs {
		if proxyID <= 0 {
			continue
		}
		key := accountProxySlotKey(accountID, proxyID)
		pipe.ZRemRangeByScore(ctx, key, "-inf", cutoff)
		cmds[proxyID] = pipe.ZCard(ctx, key)
	}
	if len(cmds) == 0 {
		return result, nil
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("pipeline exec: %w", err)
	}
	for proxyID, cmd := range cmds {
		result[proxyID] = int(cmd.Val())
	}
	return result, nil
}

// GetStickyProxyID 读取会话在该账号上已绑定的代理；无绑定返回 ErrStickyProxyNotFound。
func (c *concurrencyCache) GetStickyProxyID(ctx context.Context, accountID int64, sessionHash string) (int64, error) {
	if accountID <= 0 || sessionHash == "" {
		return 0, service.ErrStickyProxyNotFound
	}
	proxyID, err := c.rdb.Get(ctx, stickyProxyKey(accountID, sessionHash)).Int64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, service.ErrStickyProxyNotFound
		}
		return 0, err
	}
	return proxyID, nil
}

// SetStickyProxyID 写入/续期会话在该账号上的代理绑定。
func (c *concurrencyCache) SetStickyProxyID(ctx context.Context, accountID, proxyID int64, sessionHash string, ttl time.Duration) error {
	if accountID <= 0 || proxyID <= 0 || sessionHash == "" || ttl <= 0 {
		return nil
	}
	return c.rdb.Set(ctx, stickyProxyKey(accountID, sessionHash), proxyID, ttl).Err()
}
