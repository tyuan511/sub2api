package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestBepusdtSignMatchesDocumentation(t *testing.T) {
	data := map[string]any{
		"order_id":     "20220201030210321",
		"amount":       float64(42),
		"notify_url":   "http://example.com/notify",
		"redirect_url": "http://example.com/redirect",
	}
	require.Equal(t, "1cd4b52df5587cfb1968b0c0c6e156cd", bepusdtSign(data, "epusdt_password_xasddawqe"))
}

func TestNewBepusdtDefaultsAndValidation(t *testing.T) {
	provider, err := NewBepusdt("1", map[string]string{
		"apiBase":  "http://localhost:8080/",
		"apiToken": "token",
	})
	require.NoError(t, err)
	require.Equal(t, []payment.PaymentType{payment.TypeUsdt}, provider.SupportedTypes())
	require.Equal(t, "http://localhost:8080", provider.config["apiBase"])
	require.Equal(t, "CNY", provider.config["fiat"])
	require.Equal(t, "USDT", provider.config["currencies"])
	require.Equal(t, "1800", provider.config["timeoutSeconds"])
	_, err = NewBepusdt("1", map[string]string{"apiBase": "ftp://localhost", "apiToken": "token"})
	require.Error(t, err)
}

func TestBepusdtCreatePaymentSignsNormalizedAmount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/order/create-order", r.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, float64(50), body["amount"])
		require.Equal(t, bepusdtSign(body, "token"), body["signature"])
		_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"trade-1","payment_url":"https://pay.test/trade-1"}}`))
	}))
	defer server.Close()

	provider, err := NewBepusdt("1", map[string]string{"apiBase": server.URL, "apiToken": "token"})
	require.NoError(t, err)
	result, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID: "order-1", Amount: "50.00", Subject: "Balance", NotifyURL: "https://site.test/notify", ReturnURL: "https://site.test/return",
	})
	require.NoError(t, err)
	require.Equal(t, "trade-1", result.TradeNo)
	require.Equal(t, "https://pay.test/trade-1", result.PayURL)
}

func TestBepusdtCreatePaymentUsesConfiguredURLsWhenRequestOmitsThem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "https://site.test/notify", body["notify_url"])
		require.Equal(t, "https://site.test/return", body["redirect_url"])
		_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"trade-2","payment_url":"https://pay.test/trade-2"}}`))
	}))
	defer server.Close()

	provider, err := NewBepusdt("1", map[string]string{
		"apiBase": server.URL, "apiToken": "token",
		"notifyUrl": "https://site.test/notify", "returnUrl": "https://site.test/return",
	})
	require.NoError(t, err)
	result, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID: "order-2", Amount: "50.00", Subject: "Balance",
	})
	require.NoError(t, err)
	require.Equal(t, "trade-2", result.TradeNo)
}

func TestBepusdtQueryAndNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/pay/info", r.URL.Path)
		_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"trade-1","status":2,"money":"50.00"}}`))
	}))
	defer server.Close()
	provider, err := NewBepusdt("1", map[string]string{"apiBase": server.URL, "apiToken": "token"})
	require.NoError(t, err)
	result, err := provider.QueryOrder(context.Background(), "trade-1")
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusPaid, result.Status)
	require.Equal(t, 50.0, result.Amount)

	notificationData := map[string]any{
		"trade_id": "trade-1", "order_id": "order-1", "amount": float64(50),
		"actual_amount": "7.1", "token": "T-address", "block_transaction_id": "tx-1", "status": float64(2),
	}
	notificationData["signature"] = bepusdtSign(notificationData, "token")
	raw, err := json.Marshal(notificationData)
	require.NoError(t, err)
	notification, err := provider.VerifyNotification(context.Background(), string(raw), nil)
	require.NoError(t, err)
	require.Equal(t, payment.NotificationStatusSuccess, notification.Status)
	require.Equal(t, "order-1", notification.OrderID)

	notificationData["signature"] = "bad"
	raw, _ = json.Marshal(notificationData)
	_, err = provider.VerifyNotification(context.Background(), string(raw), nil)
	require.Error(t, err)
}
