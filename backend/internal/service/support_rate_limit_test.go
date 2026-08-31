package service

import (
	"context"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newSupportQuotaTestService(t *testing.T) (*SupportService, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewSupportService(nil, client, nil), server
}

func TestSupportSendQuotaMinuteLimit(t *testing.T) {
	svc, _ := newSupportQuotaTestService(t)
	ctx := context.Background()
	for i := int64(0); i < supportMessageLimitMinute; i++ {
		require.NoError(t, svc.enforceUserSendQuota(ctx, 101, 0))
	}
	err := svc.enforceUserSendQuota(ctx, 101, 0)
	require.Equal(t, "SUPPORT_MESSAGE_LIMIT_MINUTE", infraerrors.Reason(err))
	require.NotEmpty(t, infraerrors.FromError(err).Metadata["retry_after_seconds"])
}

func TestSupportSendAttemptQuotaIncludesInvalidRequests(t *testing.T) {
	svc, _ := newSupportQuotaTestService(t)
	ctx := context.Background()
	for i := int64(0); i < supportMessageLimitMinute; i++ {
		require.NoError(t, svc.EnforceUserSendAttemptQuota(ctx, 107))
	}
	err := svc.EnforceUserSendAttemptQuota(ctx, 107)
	require.Equal(t, "SUPPORT_MESSAGE_LIMIT_MINUTE", infraerrors.Reason(err))
	require.True(t, svc.redis.Exists(ctx, supportQuotaKey(107, "attempts:minute")).Val() > 0)
	require.False(t, svc.redis.Exists(ctx, supportQuotaKey(107, "messages:minute")).Val() > 0)
}

func TestSupportSendQuotaHourAndDayLimits(t *testing.T) {
	t.Run("hour", func(t *testing.T) {
		svc, server := newSupportQuotaTestService(t)
		ctx := context.Background()
		userID := int64(102)
		for batch := 0; batch < 5; batch++ {
			for i := int64(0); i < supportMessageLimitMinute; i++ {
				require.NoError(t, svc.enforceUserSendQuota(ctx, userID, 0))
			}
			server.FastForward(time.Minute + time.Second)
		}
		err := svc.enforceUserSendQuota(ctx, userID, 0)
		require.Equal(t, "SUPPORT_MESSAGE_LIMIT_HOUR", infraerrors.Reason(err))
	})

	t.Run("day", func(t *testing.T) {
		svc, server := newSupportQuotaTestService(t)
		ctx := context.Background()
		userID := int64(103)
		require.NoError(t, server.Set(supportQuotaKey(userID, "messages:day"), "200"))
		server.SetTTL(supportQuotaKey(userID, "messages:day"), 12*time.Hour)
		err := svc.enforceUserSendQuota(ctx, userID, 0)
		require.Equal(t, "SUPPORT_MESSAGE_LIMIT_DAY", infraerrors.Reason(err))
	})
}

func TestSupportImageQuotaLimitsAreAtomicWithMessageQuota(t *testing.T) {
	svc, _ := newSupportQuotaTestService(t)
	ctx := context.Background()
	userID := int64(104)
	for i := 0; i < 5; i++ {
		require.NoError(t, svc.enforceUserSendQuota(ctx, userID, 3<<20))
	}

	err := svc.enforceUserSendQuota(ctx, userID, 1<<20)
	require.Equal(t, "SUPPORT_IMAGE_LIMIT_HOUR", infraerrors.Reason(err))
	for _, suffix := range []string{"messages:minute", "messages:hour", "messages:day"} {
		count, getErr := svc.redis.Get(ctx, supportQuotaKey(userID, suffix)).Int64()
		require.NoError(t, getErr)
		require.Equal(t, int64(5), count)
	}
}

func TestSupportImageQuotaDayLimit(t *testing.T) {
	svc, server := newSupportQuotaTestService(t)
	ctx := context.Background()
	userID := int64(105)
	require.NoError(t, server.Set(supportQuotaKey(userID, "images:day"), "31457280"))
	server.SetTTL(supportQuotaKey(userID, "images:day"), 12*time.Hour)

	err := svc.enforceUserSendQuota(ctx, userID, 1)
	require.Equal(t, "SUPPORT_IMAGE_LIMIT_DAY", infraerrors.Reason(err))
}

func TestSupportSendQuotaFailsClosedWithoutRedis(t *testing.T) {
	svc := NewSupportService(nil, nil, nil)
	err := svc.enforceUserSendQuota(context.Background(), 106, 0)
	require.Equal(t, "SUPPORT_RATE_LIMIT_UNAVAILABLE", infraerrors.Reason(err))
	require.True(t, infraerrors.IsServiceUnavailable(err))
}
