package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFormatSupportTelegramNotification(t *testing.T) {
	payload := supportTelegramNotificationPayload{
		Content:   "  你好哦  ",
		UserID:    2,
		UserEmail: "support-user@local.test",
	}

	require.Equal(t,
		"[用户 support-user@local.test 回复]\n你好哦",
		formatSupportTelegramNotification("user_reply", payload),
	)
	text := formatSupportTelegramNotification("new_ticket", payload)
	require.Equal(t,
		"[用户 support-user@local.test 发起会话]\n你好哦",
		text,
	)
	require.NotContains(t, text, "/admin/support")
}

func TestFormatSupportTelegramNotificationTruncatesAndFallsBackToUserID(t *testing.T) {
	text := formatSupportTelegramNotification("user_reply", supportTelegramNotificationPayload{
		Content: strings.Repeat("长", 241), UserID: 9,
	})

	require.Equal(t, "[用户 #9 回复]\n"+strings.Repeat("长", 240)+"...", text)
	require.NotContains(t, text, "请直接引用")
}

func TestFormatActivityTelegramNotification(t *testing.T) {
	at := time.Date(2026, 9, 14, 15, 4, 5, 0, time.Local)

	require.Equal(t,
		"[充值到账]\n时间：2026-09-14 15:04:05\n用户：buyer@example.test (#7)\n金额：99.00\n到账：108.90\n订单：#42",
		formatActivityTelegramNotification(ActivityNotification{
			EventType: ActivityEventRechargeBalance, UserID: 7, UserEmail: "buyer@example.test",
			Amount: 99, Detail: "108.90", OrderID: 42, At: at,
		}),
	)
	require.Equal(t,
		"[兑换码 · 余额]\n时间：2026-09-14 15:04:05\n用户：redeemer@example.test (#8)\n金额：50.00",
		formatActivityTelegramNotification(ActivityNotification{
			EventType: ActivityEventRedeemBalance, UserID: 8, UserEmail: "redeemer@example.test",
			Amount: 50, At: at,
		}),
	)
	require.Equal(t,
		"[兑换码 · 订阅]\n时间：2026-09-14 15:04:05\n用户：#9\n时长：30 天",
		formatActivityTelegramNotification(ActivityNotification{
			EventType: ActivityEventRedeemSubscription, UserID: 9, Detail: "30 天", At: at,
		}),
	)
}

func TestFormatTelegramOutboxTextDispatchesByEventType(t *testing.T) {
	require.Equal(t,
		"[用户 support-user@local.test 回复]\n你好",
		formatTelegramOutboxText("user_reply", `{"content":"你好","user_email":"support-user@local.test"}`, supportTelegramNotificationPayload{Content: "你好", UserEmail: "support-user@local.test"}),
	)

	raw := `{"user_id":3,"user_email":"r@example.test","amount":5,"at":"2026-09-14T15:04:05Z"}`
	require.Contains(t, formatTelegramOutboxText(ActivityEventRedeemBalance, raw, supportTelegramNotificationPayload{}), "[兑换码 · 余额]")
}

func TestIsRechargeActivityEvent(t *testing.T) {
	require.True(t, isRechargeActivityEvent(ActivityEventRechargeBalance))
	require.True(t, isRechargeActivityEvent(ActivityEventRechargeSubscription))
	require.False(t, isRechargeActivityEvent(ActivityEventRedeemBalance))
	require.False(t, isActivityEventType("user_reply"))
}
