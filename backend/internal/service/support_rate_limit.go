package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/redis/go-redis/v9"
)

const (
	supportMessageLimitMinute = int64(10)
	supportMessageLimitHour   = int64(50)
	supportMessageLimitDay    = int64(200)
	supportImageLimitHour     = int64(15 << 20)
	supportImageLimitDay      = int64(30 << 20)
)

type supportQuotaWindow struct {
	keySuffix string
	reason    string
	message   string
	increment int64
	limit     int64
	window    time.Duration
}

var supportQuotaScript = redis.NewScript(`
for i = 1, #KEYS do
  local offset = (i - 1) * 3
  local increment = tonumber(ARGV[offset + 1])
  local limit = tonumber(ARGV[offset + 2])
  local window_ms = tonumber(ARGV[offset + 3])
  local current = tonumber(redis.call('GET', KEYS[i]) or '0')
  if current + increment > limit then
    local ttl = redis.call('PTTL', KEYS[i])
    if ttl < 1 then
      ttl = window_ms
    end
    return {0, i, ttl}
  end
end

for i = 1, #KEYS do
  local offset = (i - 1) * 3
  local increment = tonumber(ARGV[offset + 1])
  local window_ms = tonumber(ARGV[offset + 3])
  local current = redis.call('INCRBY', KEYS[i], increment)
  local ttl = redis.call('PTTL', KEYS[i])
  if current == increment or ttl < 0 then
    redis.call('PEXPIRE', KEYS[i], window_ms)
  end
end

return {1, 0, 0}
`)

func supportQuotaKey(userID int64, suffix string) string {
	return fmt.Sprintf("support:quota:{%d}:%s", userID, suffix)
}

func supportQuotaWindows(imageBytes int64) []supportQuotaWindow {
	windows := []supportQuotaWindow{
		{keySuffix: "messages:minute", reason: "SUPPORT_MESSAGE_LIMIT_MINUTE", message: "发送过于频繁，每分钟最多发送 10 条消息，请稍后再试", increment: 1, limit: supportMessageLimitMinute, window: time.Minute},
		{keySuffix: "messages:hour", reason: "SUPPORT_MESSAGE_LIMIT_HOUR", message: "发送消息已达到每小时 50 条上限，请稍后再试", increment: 1, limit: supportMessageLimitHour, window: time.Hour},
		{keySuffix: "messages:day", reason: "SUPPORT_MESSAGE_LIMIT_DAY", message: "发送消息已达到 24 小时内 200 条上限，请稍后再试", increment: 1, limit: supportMessageLimitDay, window: 24 * time.Hour},
	}
	if imageBytes > 0 {
		windows = append(windows,
			supportQuotaWindow{keySuffix: "images:hour", reason: "SUPPORT_IMAGE_LIMIT_HOUR", message: "图片上传已达到每小时 15 MB 上限，请稍后再试", increment: imageBytes, limit: supportImageLimitHour, window: time.Hour},
			supportQuotaWindow{keySuffix: "images:day", reason: "SUPPORT_IMAGE_LIMIT_DAY", message: "图片上传已达到 24 小时内 30 MB 上限，请稍后再试", increment: imageBytes, limit: supportImageLimitDay, window: 24 * time.Hour},
		)
	}
	return windows
}

func supportAttemptQuotaWindows() []supportQuotaWindow {
	return []supportQuotaWindow{
		{keySuffix: "attempts:minute", reason: "SUPPORT_MESSAGE_LIMIT_MINUTE", message: "发送过于频繁，每分钟最多发送 10 条消息，请稍后再试", increment: 1, limit: supportMessageLimitMinute, window: time.Minute},
		{keySuffix: "attempts:hour", reason: "SUPPORT_MESSAGE_LIMIT_HOUR", message: "发送消息已达到每小时 50 条上限，请稍后再试", increment: 1, limit: supportMessageLimitHour, window: time.Hour},
		{keySuffix: "attempts:day", reason: "SUPPORT_MESSAGE_LIMIT_DAY", message: "发送消息已达到 24 小时内 200 条上限，请稍后再试", increment: 1, limit: supportMessageLimitDay, window: 24 * time.Hour},
	}
}

// EnforceUserSendAttemptQuota runs before multipart parsing so malformed or
// oversized requests cannot bypass the support-specific request limits.
func (s *SupportService) EnforceUserSendAttemptQuota(ctx context.Context, userID int64) error {
	return s.enforceSupportQuota(ctx, userID, supportAttemptQuotaWindows())
}

func (s *SupportService) enforceUserSendQuota(ctx context.Context, userID, imageBytes int64) error {
	return s.enforceSupportQuota(ctx, userID, supportQuotaWindows(imageBytes))
}

func (s *SupportService) enforceSupportQuota(ctx context.Context, userID int64, windows []supportQuotaWindow) error {
	if s.redis == nil {
		return infraerrors.ServiceUnavailable("SUPPORT_RATE_LIMIT_UNAVAILABLE", "客服消息暂时无法发送，请稍后重试")
	}
	keys := make([]string, 0, len(windows))
	args := make([]any, 0, len(windows)*3)
	for _, quota := range windows {
		keys = append(keys, supportQuotaKey(userID, quota.keySuffix))
		args = append(args, quota.increment, quota.limit, quota.window.Milliseconds())
	}
	values, err := supportQuotaScript.Run(ctx, s.redis, keys, args...).Slice()
	if err != nil {
		return infraerrors.ServiceUnavailable("SUPPORT_RATE_LIMIT_UNAVAILABLE", "客服消息暂时无法发送，请稍后重试").WithCause(err)
	}
	if len(values) != 3 {
		return infraerrors.ServiceUnavailable("SUPPORT_RATE_LIMIT_UNAVAILABLE", "客服消息暂时无法发送，请稍后重试")
	}
	allowed, err := supportQuotaValue(values[0])
	if err != nil {
		return infraerrors.ServiceUnavailable("SUPPORT_RATE_LIMIT_UNAVAILABLE", "客服消息暂时无法发送，请稍后重试").WithCause(err)
	}
	if allowed == 1 {
		return nil
	}
	index, err := supportQuotaValue(values[1])
	if err != nil || index < 1 || index > int64(len(windows)) {
		return infraerrors.ServiceUnavailable("SUPPORT_RATE_LIMIT_UNAVAILABLE", "客服消息暂时无法发送，请稍后重试")
	}
	retryMillis, err := supportQuotaValue(values[2])
	if err != nil {
		retryMillis = windows[index-1].window.Milliseconds()
	}
	retrySeconds := (retryMillis + 999) / 1000
	if retrySeconds < 1 {
		retrySeconds = 1
	}
	quota := windows[index-1]
	return infraerrors.TooManyRequests(quota.reason, quota.message).WithMetadata(map[string]string{
		"retry_after_seconds": strconv.FormatInt(retrySeconds, 10),
	})
}

func supportQuotaValue(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, fmt.Errorf("unexpected support quota value type %T", value)
	}
}
