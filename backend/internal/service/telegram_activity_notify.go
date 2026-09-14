package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Telegram activity notification event types. They share the support
// notification outbox so delivery inherits the same retry/backoff behaviour.
const (
	ActivityEventRechargeBalance      = "recharge_balance"
	ActivityEventRechargeSubscription = "recharge_subscription"
	ActivityEventRedeemBalance        = "redeem_balance"
	ActivityEventRedeemConcurrency    = "redeem_concurrency"
	ActivityEventRedeemSubscription   = "redeem_subscription"
)

// ActivityNotification describes a user-initiated recharge or redeem event
// that administrators may want to be told about.
type ActivityNotification struct {
	EventType string    `json:"-"`
	UserID    int64     `json:"user_id"`
	UserEmail string    `json:"user_email"`
	Amount    float64   `json:"amount,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	OrderID   int64     `json:"order_id,omitempty"`
	At        time.Time `json:"at"`
}

// ActivityNotifier delivers activity notifications to bound administrators.
type ActivityNotifier interface {
	NotifyActivity(ctx context.Context, notification ActivityNotification)
}

var defaultActivityNotifier ActivityNotifier

// SetDefaultActivityNotifier wires the process-wide notifier. The
// SupportTelegramService provider registers itself here so redeem and payment
// flows stay decoupled from the support module.
func SetDefaultActivityNotifier(notifier ActivityNotifier) {
	defaultActivityNotifier = notifier
}

// EnqueueActivityNotification best-effort enqueues a notification. It never
// blocks or fails the caller's business transaction.
func EnqueueActivityNotification(ctx context.Context, notification ActivityNotification) {
	notifier := defaultActivityNotifier
	if notifier == nil {
		return
	}
	if notification.At.IsZero() {
		notification.At = time.Now()
	}
	notifier.NotifyActivity(ctx, notification)
}

func isActivityEventType(eventType string) bool {
	switch eventType {
	case ActivityEventRechargeBalance, ActivityEventRechargeSubscription,
		ActivityEventRedeemBalance, ActivityEventRedeemConcurrency, ActivityEventRedeemSubscription:
		return true
	default:
		return false
	}
}

func isRechargeActivityEvent(eventType string) bool {
	return eventType == ActivityEventRechargeBalance || eventType == ActivityEventRechargeSubscription
}

func formatActivityTelegramNotification(notification ActivityNotification) string {
	if notification.At.IsZero() {
		notification.At = time.Now()
	}
	title := "通知"
	switch notification.EventType {
	case ActivityEventRechargeBalance:
		title = "充值到账"
	case ActivityEventRechargeSubscription:
		title = "订阅购买"
	case ActivityEventRedeemBalance:
		title = "兑换码 · 余额"
	case ActivityEventRedeemConcurrency:
		title = "兑换码 · 并发"
	case ActivityEventRedeemSubscription:
		title = "兑换码 · 订阅"
	}
	email := strings.TrimSpace(notification.UserEmail)
	identity := email
	if email == "" {
		identity = fmt.Sprintf("#%d", notification.UserID)
	} else if notification.UserID > 0 {
		identity = fmt.Sprintf("%s (#%d)", email, notification.UserID)
	}
	lines := []string{
		"[" + title + "]",
		"时间：" + notification.At.Format("2006-01-02 15:04:05"),
		"用户：" + identity,
	}
	switch notification.EventType {
	case ActivityEventRechargeBalance:
		lines = append(lines, fmt.Sprintf("金额：%.2f", notification.Amount))
		if strings.TrimSpace(notification.Detail) != "" {
			lines = append(lines, "到账："+strings.TrimSpace(notification.Detail))
		}
		if notification.OrderID > 0 {
			lines = append(lines, fmt.Sprintf("订单：#%d", notification.OrderID))
		}
	case ActivityEventRechargeSubscription:
		lines = append(lines, fmt.Sprintf("金额：%.2f", notification.Amount))
		if notification.OrderID > 0 {
			lines = append(lines, fmt.Sprintf("订单：#%d", notification.OrderID))
		}
	case ActivityEventRedeemBalance:
		lines = append(lines, fmt.Sprintf("金额：%.2f", notification.Amount))
	case ActivityEventRedeemConcurrency:
		if strings.TrimSpace(notification.Detail) != "" {
			lines = append(lines, "数量："+strings.TrimSpace(notification.Detail))
		}
	case ActivityEventRedeemSubscription:
		if strings.TrimSpace(notification.Detail) != "" {
			lines = append(lines, "时长："+strings.TrimSpace(notification.Detail))
		}
	}
	return strings.Join(lines, "\n")
}
