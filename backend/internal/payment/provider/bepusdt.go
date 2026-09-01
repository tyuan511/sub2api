package provider

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
)

const (
	bepusdtHTTPTimeout       = 15 * time.Second
	bepusdtMaxResponseSize   = 1 << 20
	bepusdtMaxErrorSummary   = 512
	bepusdtDefaultFiat       = "CNY"
	bepusdtDefaultCurrencies = "USDT"
	bepusdtDefaultTimeout    = 1800
	bepusdtMinTimeout        = 180
	bepusdtMaxTimeout        = 86400
)

var bepusdtCurrencyPattern = regexp.MustCompile(`^[A-Z0-9_-]+$`)

// Bepusdt implements the BEpusdt cashier API provider.
type Bepusdt struct {
	instanceID string
	config     map[string]string
	httpClient *http.Client
}

type bepusdtAPIResponse struct {
	StatusCode int                        `json:"status_code"`
	Message    string                     `json:"message"`
	Data       map[string]json.RawMessage `json:"data"`
}

func NewBepusdt(instanceID string, config map[string]string) (*Bepusdt, error) {
	if strings.TrimSpace(config["apiBase"]) == "" {
		return nil, fmt.Errorf("bepusdt config missing required key: apiBase")
	}
	if strings.TrimSpace(config["apiToken"]) == "" {
		return nil, fmt.Errorf("bepusdt config missing required key: apiToken")
	}
	base, err := normalizeBepusdtAPIBase(config["apiBase"])
	if err != nil {
		return nil, err
	}
	fiat, err := normalizeBepusdtFiat(config["fiat"])
	if err != nil {
		return nil, err
	}
	currencies, err := normalizeBepusdtCurrencies(config["currencies"])
	if err != nil {
		return nil, err
	}
	timeout, err := normalizeBepusdtTimeout(config["timeoutSeconds"])
	if err != nil {
		return nil, err
	}

	cfg := cloneStringMap(config)
	cfg["apiBase"] = base
	cfg["fiat"] = fiat
	cfg["currencies"] = currencies
	cfg["timeoutSeconds"] = strconv.Itoa(timeout)
	return &Bepusdt{
		instanceID: instanceID,
		config:     cfg,
		httpClient: &http.Client{Timeout: bepusdtHTTPTimeout},
	}, nil
}

func normalizeBepusdtAPIBase(raw string) (string, error) {
	base := strings.TrimSpace(raw)
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("bepusdt apiBase must be an HTTP or HTTPS URL")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.RawPath = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeBepusdtFiat(raw string) (string, error) {
	fiat := strings.ToUpper(strings.TrimSpace(raw))
	if fiat == "" {
		return bepusdtDefaultFiat, nil
	}
	switch fiat {
	case "CNY", "USD", "EUR", "GBP", "JPY":
		return fiat, nil
	default:
		return "", fmt.Errorf("bepusdt fiat must be one of CNY, USD, EUR, GBP, JPY")
	}
}

func normalizeBepusdtCurrencies(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return bepusdtDefaultCurrencies, nil
	}
	parts := strings.Split(raw, ",")
	clean := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.ToUpper(strings.TrimSpace(part))
		if part == "" || !bepusdtCurrencyPattern.MatchString(part) {
			return "", fmt.Errorf("bepusdt currencies contains an invalid currency: %s", part)
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return bepusdtDefaultCurrencies, nil
	}
	return strings.Join(clean, ","), nil
}

func normalizeBepusdtTimeout(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return bepusdtDefaultTimeout, nil
	}
	timeout, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || timeout < bepusdtMinTimeout || timeout > bepusdtMaxTimeout {
		return 0, fmt.Errorf("bepusdt timeoutSeconds must be between %d and %d", bepusdtMinTimeout, bepusdtMaxTimeout)
	}
	return timeout, nil
}

func (b *Bepusdt) Name() string        { return "BEpusdt" }
func (b *Bepusdt) ProviderKey() string { return payment.TypeBepusdt }
func (b *Bepusdt) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeUsdt}
}

func (b *Bepusdt) MerchantIdentityMetadata() map[string]string {
	if b == nil {
		return nil
	}
	return map[string]string{"currency": b.fiat()}
}

func (b *Bepusdt) fiat() string {
	if b == nil {
		return bepusdtDefaultFiat
	}
	fiat, err := normalizeBepusdtFiat(b.config["fiat"])
	if err != nil {
		return bepusdtDefaultFiat
	}
	return fiat
}

func (b *Bepusdt) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	amount, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("bepusdt create payment: invalid amount %s", req.Amount)
	}
	notifyURL := strings.TrimSpace(req.NotifyURL)
	if notifyURL == "" {
		notifyURL = strings.TrimSpace(b.config["notifyUrl"])
	}
	returnURL := strings.TrimSpace(req.ReturnURL)
	if returnURL == "" {
		returnURL = strings.TrimSpace(b.config["returnUrl"])
	}
	payload := map[string]any{
		// BEpusdt decodes amount as float64 before verifying the signature;
		// sign the same normalized representation it will see (e.g. 50, not 50.00).
		"amount":       amount.InexactFloat64(),
		"currencies":   b.config["currencies"],
		"fiat":         b.fiat(),
		"name":         strings.TrimSpace(req.Subject),
		"notify_url":   notifyURL,
		"order_id":     strings.TrimSpace(req.OrderID),
		"redirect_url": returnURL,
		"timeout":      json.Number(b.config["timeoutSeconds"]),
	}
	payload["signature"] = bepusdtSign(payload, b.config["apiToken"])

	var response bepusdtAPIResponse
	if err := b.doJSON(ctx, http.MethodPost, "/api/v1/order/create-order", payload, &response); err != nil {
		return nil, fmt.Errorf("bepusdt create payment: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bepusdt create payment: upstream status_code=%d message=%s", response.StatusCode, response.Message)
	}
	tradeID := bepusdtString(response.Data, "trade_id")
	payURL := bepusdtString(response.Data, "payment_url")
	if tradeID == "" || payURL == "" {
		return nil, fmt.Errorf("bepusdt create payment: response missing trade_id or payment_url")
	}
	return &payment.CreatePaymentResponse{TradeNo: tradeID, PayURL: payURL, Currency: b.fiat()}, nil
}

func (b *Bepusdt) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return nil, fmt.Errorf("bepusdt query order: missing trade id")
	}
	var response bepusdtAPIResponse
	if err := b.doJSON(ctx, http.MethodPost, "/api/v1/pay/info", map[string]string{"trade_id": tradeNo}, &response); err != nil {
		return nil, fmt.Errorf("bepusdt query order: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bepusdt query order: upstream status_code=%d message=%s", response.StatusCode, response.Message)
	}
	status, err := bepusdtInt(response.Data, "status")
	if err != nil {
		return nil, fmt.Errorf("bepusdt query order: invalid status: %w", err)
	}
	amount, err := bepusdtFloat(response.Data, "money", "amount")
	if err != nil {
		return nil, fmt.Errorf("bepusdt query order: invalid amount: %w", err)
	}
	return &payment.QueryOrderResponse{
		TradeNo:  firstNonEmpty(bepusdtString(response.Data, "trade_id"), tradeNo),
		Status:   bepusdtProviderStatus(status),
		Amount:   amount,
		Metadata: map[string]string{"currency": b.fiat()},
	}, nil
}

func (b *Bepusdt) CancelPayment(ctx context.Context, tradeNo string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return fmt.Errorf("bepusdt cancel payment: missing trade id")
	}
	payload := map[string]any{"trade_id": tradeNo}
	payload["signature"] = bepusdtSign(payload, b.config["apiToken"])
	var response bepusdtAPIResponse
	if err := b.doJSON(ctx, http.MethodPost, "/api/v1/order/cancel-transaction", payload, &response); err != nil {
		return fmt.Errorf("bepusdt cancel payment: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("bepusdt cancel payment: upstream status_code=%d message=%s", response.StatusCode, response.Message)
	}
	return nil
}

func (b *Bepusdt) VerifyNotification(_ context.Context, rawBody string, _ map[string]string) (*payment.PaymentNotification, error) {
	decoder := json.NewDecoder(strings.NewReader(rawBody))
	decoder.UseNumber()
	var data map[string]any
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("bepusdt parse notification: %w", err)
	}
	signature, ok := data["signature"].(string)
	if !ok || strings.TrimSpace(signature) == "" {
		return nil, fmt.Errorf("bepusdt notification missing signature")
	}
	expected := bepusdtSign(data, b.config["apiToken"])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(signature))), []byte(expected)) != 1 {
		return nil, fmt.Errorf("bepusdt notification signature mismatch")
	}
	status, err := bepusdtAnyInt(data["status"])
	if err != nil {
		return nil, fmt.Errorf("bepusdt notification invalid status: %w", err)
	}
	if status == 1 {
		return nil, nil
	}
	tradeID, _ := data["trade_id"].(string)
	orderID, _ := data["order_id"].(string)
	tradeID, orderID = strings.TrimSpace(tradeID), strings.TrimSpace(orderID)
	if tradeID == "" || orderID == "" {
		return nil, fmt.Errorf("bepusdt notification missing trade_id or order_id")
	}
	amount, err := bepusdtAnyFloat(data["amount"])
	if err != nil {
		return nil, fmt.Errorf("bepusdt notification invalid amount: %w", err)
	}
	notificationStatus := payment.NotificationStatusSuccess
	if status != 2 {
		notificationStatus = payment.ProviderStatusFailed
	}
	return &payment.PaymentNotification{
		TradeNo:  tradeID,
		OrderID:  orderID,
		Amount:   amount,
		Status:   notificationStatus,
		RawData:  rawBody,
		Metadata: map[string]string{"currency": b.fiat()},
	}, nil
}

func (b *Bepusdt) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, fmt.Errorf("bepusdt refunds are not supported")
}

func (b *Bepusdt) doJSON(ctx context.Context, method, endpoint string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(b.config["apiBase"], "/")+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Sub2API/BEpusdt")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, bepusdtMaxResponseSize))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBepusdtError(string(responseBody)))
	}
	if err := json.Unmarshal(responseBody, result); err != nil {
		return fmt.Errorf("invalid JSON response: %w", err)
	}
	return nil
}

func bepusdtSign(data map[string]any, token string) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		if key != "signature" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := data[key]
		if value == nil {
			continue
		}
		if stringValue, ok := value.(string); ok && stringValue == "" {
			continue
		}
		parts = append(parts, key+"="+fmt.Sprintf("%v", value))
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + token))
	return hex.EncodeToString(sum[:])
}

func bepusdtString(data map[string]json.RawMessage, key string) string {
	var value string
	if raw, ok := data[key]; ok && json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

func bepusdtInt(data map[string]json.RawMessage, key string) (int, error) {
	if raw, ok := data[key]; ok {
		var value int
		if json.Unmarshal(raw, &value) == nil {
			return value, nil
		}
		var stringValue string
		if json.Unmarshal(raw, &stringValue) == nil {
			return bepusdtAnyInt(stringValue)
		}
	}
	return 0, fmt.Errorf("missing %s", key)
}

func bepusdtFloat(data map[string]json.RawMessage, keys ...string) (float64, error) {
	for _, key := range keys {
		if raw, ok := data[key]; ok {
			var value json.Number
			if json.Unmarshal(raw, &value) == nil {
				return strconv.ParseFloat(string(value), 64)
			}
			var stringValue string
			if json.Unmarshal(raw, &stringValue) == nil {
				return strconv.ParseFloat(strings.TrimSpace(stringValue), 64)
			}
		}
	}
	return 0, fmt.Errorf("missing amount")
}

func bepusdtAnyInt(value any) (int, error) {
	switch typed := value.(type) {
	case json.Number:
		return strconv.Atoi(string(typed))
	case float64:
		return int(typed), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(typed))
	default:
		return 0, fmt.Errorf("unsupported status type %T", value)
	}
}

func bepusdtAnyFloat(value any) (float64, error) {
	switch typed := value.(type) {
	case json.Number:
		return strconv.ParseFloat(string(typed), 64)
	case float64:
		return typed, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(typed), 64)
	default:
		return 0, fmt.Errorf("unsupported amount type %T", value)
	}
}

func bepusdtProviderStatus(status int) string {
	switch status {
	case 2:
		return payment.ProviderStatusPaid
	case 3, 4, 6:
		return payment.ProviderStatusFailed
	default:
		return payment.ProviderStatusPending
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncateBepusdtError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > bepusdtMaxErrorSummary {
		return value[:bepusdtMaxErrorSummary] + "..."
	}
	return value
}

var _ payment.Provider = (*Bepusdt)(nil)
var _ payment.CancelableProvider = (*Bepusdt)(nil)
var _ payment.MerchantIdentityProvider = (*Bepusdt)(nil)
