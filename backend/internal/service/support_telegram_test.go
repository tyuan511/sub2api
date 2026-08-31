package service

import (
	"strings"
	"testing"

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
