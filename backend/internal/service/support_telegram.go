package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/admintelegrambinding"
	"github.com/Wei-Shaw/sub2api/ent/supportnotificationoutbox"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	supportTelegramSettingKey = "support_telegram_config"
	telegramBindingTTL        = 10 * time.Minute
)

type supportTelegramStoredConfig struct {
	Enabled                bool   `json:"enabled"`
	BotUsername            string `json:"bot_username"`
	BotTokenEncrypted      string `json:"bot_token_encrypted"`
	WebhookSecretEncrypted string `json:"webhook_secret_encrypted"`
	WebhookBaseURL         string `json:"webhook_base_url"`
	WebhookSet             bool   `json:"webhook_set"`
}

type supportTelegramRuntimeConfig struct {
	Enabled        bool
	BotUsername    string
	BotToken       string
	WebhookSecret  string
	WebhookBaseURL string
	WebhookSet     bool
}

type SupportTelegramService struct {
	client     *ent.Client
	redis      *redis.Client
	support    *SupportService
	settings   SettingRepository
	encryptor  SecretEncryptor
	httpClient *http.Client
	stop       chan struct{}
}

func NewSupportTelegramService(client *ent.Client, redisClient *redis.Client, support *SupportService, settings SettingRepository, encryptor SecretEncryptor) *SupportTelegramService {
	return &SupportTelegramService{client: client, redis: redisClient, support: support, settings: settings,
		encryptor: encryptor, httpClient: &http.Client{Timeout: 15 * time.Second}, stop: make(chan struct{})}
}

func ProvideSupportTelegramService(client *ent.Client, redisClient *redis.Client, support *SupportService, settings SettingRepository, encryptor SecretEncryptor) *SupportTelegramService {
	svc := NewSupportTelegramService(client, redisClient, support, settings, encryptor)
	svc.Start()
	return svc
}

func (s *SupportTelegramService) loadStored(ctx context.Context) (*supportTelegramStoredConfig, error) {
	raw, err := s.settings.GetValue(ctx, supportTelegramSettingKey)
	if err != nil {
		if err == ErrSettingNotFound {
			return &supportTelegramStoredConfig{}, nil
		}
		return nil, err
	}
	var cfg supportTelegramStoredConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("decode support Telegram config: %w", err)
	}
	return &cfg, nil
}

func (s *SupportTelegramService) loadRuntime(ctx context.Context) (*supportTelegramRuntimeConfig, error) {
	stored, err := s.loadStored(ctx)
	if err != nil {
		return nil, err
	}
	runtime := &supportTelegramRuntimeConfig{Enabled: stored.Enabled, BotUsername: stored.BotUsername,
		WebhookBaseURL: stored.WebhookBaseURL, WebhookSet: stored.WebhookSet}
	if stored.BotTokenEncrypted != "" {
		runtime.BotToken, err = s.encryptor.Decrypt(stored.BotTokenEncrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt Telegram bot token: %w", err)
		}
	}
	if stored.WebhookSecretEncrypted != "" {
		runtime.WebhookSecret, err = s.encryptor.Decrypt(stored.WebhookSecretEncrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt Telegram webhook secret: %w", err)
		}
	}
	return runtime, nil
}

func (s *SupportTelegramService) GetConfig(ctx context.Context) (*TelegramConfigView, error) {
	cfg, err := s.loadStored(ctx)
	if err != nil {
		return nil, err
	}
	return &TelegramConfigView{Enabled: cfg.Enabled, BotUsername: cfg.BotUsername, TokenSet: cfg.BotTokenEncrypted != "",
		WebhookSet: cfg.WebhookSet, WebhookBaseURL: cfg.WebhookBaseURL}, nil
}

type telegramAPIResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

type telegramMessageResult struct {
	MessageID int64 `json:"message_id"`
}

func (s *SupportTelegramService) call(ctx context.Context, token, method string, payload any) (*telegramAPIResponse, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/"+method, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var result telegramAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode Telegram response: %w", err)
	}
	if !result.OK {
		return &result, fmt.Errorf("Telegram %s failed: %s", method, result.Description)
	}
	return &result, nil
}

func (s *SupportTelegramService) SaveConfig(ctx context.Context, input TelegramConfigInput) (*TelegramConfigView, error) {
	stored, err := s.loadStored(ctx)
	if err != nil {
		return nil, err
	}
	input.BotToken = strings.TrimSpace(input.BotToken)
	input.WebhookBaseURL = strings.TrimRight(strings.TrimSpace(input.WebhookBaseURL), "/")
	tokenProvided := input.BotToken != ""
	if input.WebhookBaseURL != "" {
		u, err := url.Parse(input.WebhookBaseURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, infraerrors.BadRequest("TELEGRAM_WEBHOOK_URL_INVALID", "webhook base URL must be an absolute HTTPS URL without query or fragment")
		}
	}
	token := input.BotToken
	if token == "" && stored.BotTokenEncrypted != "" {
		token, err = s.encryptor.Decrypt(stored.BotTokenEncrypted)
		if err != nil {
			return nil, infraerrors.BadRequest("TELEGRAM_TOKEN_UNREADABLE", "stored Telegram credentials cannot be decrypted; re-enter the bot token to restore the configuration")
		}
	}
	if input.Enabled && token == "" {
		return nil, infraerrors.BadRequest("TELEGRAM_TOKEN_REQUIRED", "bot token is required")
	}
	if token != "" {
		result, err := s.call(ctx, token, "getMe", map[string]any{})
		if err != nil {
			return nil, infraerrors.BadRequest("TELEGRAM_TOKEN_INVALID", err.Error())
		}
		var bot struct {
			Username string `json:"username"`
		}
		if err := json.Unmarshal(result.Result, &bot); err != nil || strings.TrimSpace(bot.Username) == "" {
			return nil, infraerrors.BadRequest("TELEGRAM_BOT_INVALID", "Telegram bot has no username")
		}
		stored.BotUsername = bot.Username
		stored.BotTokenEncrypted, err = s.encryptor.Encrypt(token)
		if err != nil {
			return nil, err
		}
	}
	secret := ""
	if !tokenProvided && stored.WebhookSecretEncrypted != "" {
		secret, err = s.encryptor.Decrypt(stored.WebhookSecretEncrypted)
		if err != nil {
			return nil, infraerrors.BadRequest("TELEGRAM_SECRET_UNREADABLE", "stored Telegram credentials cannot be decrypted; re-enter the bot token to restore the configuration")
		}
	}
	if secret == "" {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		secret = hex.EncodeToString(b)
		stored.WebhookSecretEncrypted, err = s.encryptor.Encrypt(secret)
		if err != nil {
			return nil, err
		}
	}
	stored.Enabled = input.Enabled
	stored.WebhookBaseURL = input.WebhookBaseURL
	stored.WebhookSet = false
	if input.Enabled && input.WebhookBaseURL != "" {
		_, err := s.call(ctx, token, "setWebhook", map[string]any{
			"url":          input.WebhookBaseURL + "/api/v1/integrations/telegram/webhook",
			"secret_token": secret, "allowed_updates": []string{"message"}, "drop_pending_updates": false,
		})
		if err != nil {
			return nil, infraerrors.BadRequest("TELEGRAM_WEBHOOK_FAILED", err.Error())
		}
		stored.WebhookSet = true
	} else if token != "" {
		_, _ = s.call(ctx, token, "deleteWebhook", map[string]any{"drop_pending_updates": false})
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	if err := s.settings.Set(ctx, supportTelegramSettingKey, string(raw)); err != nil {
		return nil, err
	}
	return s.GetConfig(ctx)
}

func (s *SupportTelegramService) GetBinding(ctx context.Context, adminID int64) (*TelegramBindingView, error) {
	binding, err := s.client.AdminTelegramBinding.Query().Where(admintelegrambinding.AdminIDEQ(adminID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return &TelegramBindingView{}, nil
		}
		return nil, err
	}
	username := ""
	if binding.TelegramUsername != nil {
		username = *binding.TelegramUsername
	}
	lastError := ""
	if binding.LastError != nil {
		lastError = *binding.LastError
	}
	return &TelegramBindingView{Bound: true, Enabled: binding.Enabled, TelegramUsername: username,
		NotifyNewTicket: binding.NotifyNewTicket, NotifyUserReply: binding.NotifyUserReply,
		BoundAt:       &binding.BoundAt,
		LastSuccessAt: binding.LastSuccessAt, LastError: lastError}, nil
}

func (s *SupportTelegramService) CreateBindLink(ctx context.Context, adminID int64) (*TelegramBindLink, error) {
	if s.redis == nil {
		return nil, infraerrors.New(503, "TELEGRAM_BINDING_UNAVAILABLE", "Redis is required for Telegram binding")
	}
	cfg, err := s.loadRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || cfg.BotToken == "" || cfg.BotUsername == "" {
		return nil, infraerrors.BadRequest("TELEGRAM_NOT_CONFIGURED", "configure and enable the Telegram bot first")
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	code := base64.RawURLEncoding.EncodeToString(b)
	if err := s.redis.Set(ctx, "support:telegram:bind:"+code, strconv.FormatInt(adminID, 10), telegramBindingTTL).Err(); err != nil {
		return nil, err
	}
	return &TelegramBindLink{URL: "https://t.me/" + cfg.BotUsername + "?start=" + code, ExpiresAt: time.Now().Add(telegramBindingTTL)}, nil
}

func (s *SupportTelegramService) UpdateBinding(ctx context.Context, adminID int64, input TelegramBindingInput) (*TelegramBindingView, error) {
	updated, err := s.client.AdminTelegramBinding.Update().Where(admintelegrambinding.AdminIDEQ(adminID)).
		SetEnabled(input.Enabled).SetNotifyNewTicket(input.NotifyNewTicket).SetNotifyUserReply(input.NotifyUserReply).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if updated == 0 {
		return nil, infraerrors.NotFound("TELEGRAM_BINDING_NOT_FOUND", "Telegram binding not found")
	}
	return s.GetBinding(ctx, adminID)
}

func (s *SupportTelegramService) DeleteBinding(ctx context.Context, adminID int64) error {
	_, err := s.client.AdminTelegramBinding.Delete().Where(admintelegrambinding.AdminIDEQ(adminID)).Exec(ctx)
	return err
}

func (s *SupportTelegramService) TestBinding(ctx context.Context, adminID int64) error {
	cfg, err := s.loadRuntime(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled || cfg.BotToken == "" {
		return infraerrors.BadRequest("TELEGRAM_NOT_CONFIGURED", "Telegram bot is not enabled")
	}
	binding, err := s.client.AdminTelegramBinding.Query().Where(admintelegrambinding.AdminIDEQ(adminID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return infraerrors.NotFound("TELEGRAM_BINDING_NOT_FOUND", "Telegram binding not found")
		}
		return err
	}
	_, err = s.call(ctx, cfg.BotToken, "sendMessage", map[string]any{"chat_id": binding.ChatID, "text": "FastVibe 客服消息通知测试成功"})
	if err != nil {
		_, _ = s.client.AdminTelegramBinding.UpdateOneID(binding.ID).SetLastError(err.Error()).Save(ctx)
		return err
	}
	_, _ = s.client.AdminTelegramBinding.UpdateOneID(binding.ID).SetLastSuccessAt(time.Now()).ClearLastError().Save(ctx)
	return nil
}

type telegramReplyReference struct {
	MessageID int64 `json:"message_id"`
}

type telegramPhotoSize struct {
	FileID string `json:"file_id"`
}

type telegramMessage struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Caption   string `json:"caption"`
	Chat      struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From *struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
	ReplyToMessage *telegramReplyReference `json:"reply_to_message"`
	Photo          []telegramPhotoSize     `json:"photo"`
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type supportTelegramNotificationPayload struct {
	Content   string `json:"content"`
	UserID    int64  `json:"user_id"`
	UserEmail string `json:"user_email"`
}

func formatSupportTelegramNotification(eventType string, payload supportTelegramNotificationPayload) string {
	action := "发起会话"
	if eventType == "user_reply" {
		action = "回复"
	}
	identity := strings.TrimSpace(payload.UserEmail)
	if identity == "" {
		identity = fmt.Sprintf("#%d", payload.UserID)
	}
	content := []rune(strings.TrimSpace(payload.Content))
	if len(content) > 240 {
		content = append(content[:240], []rune("...")...)
	}
	return fmt.Sprintf("[用户 %s %s]\n%s", identity, action, string(content))
}

func (s *SupportTelegramService) HandleWebhook(ctx context.Context, secretHeader string, body []byte) error {
	cfg, err := s.loadRuntime(ctx)
	if err != nil {
		return err
	}
	if cfg.WebhookSecret == "" || subtle.ConstantTimeCompare([]byte(cfg.WebhookSecret), []byte(secretHeader)) != 1 {
		return infraerrors.Unauthorized("TELEGRAM_WEBHOOK_UNAUTHORIZED", "invalid Telegram webhook secret")
	}
	var update telegramUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		return infraerrors.BadRequest("TELEGRAM_UPDATE_INVALID", "invalid Telegram update")
	}
	if update.Message == nil || update.Message.From == nil || update.Message.Chat.Type != "private" {
		return nil
	}
	message := update.Message
	parts := strings.Fields(strings.TrimSpace(message.Text))
	if len(parts) != 2 || parts[0] != "/start" {
		return s.handleAdminReply(ctx, cfg, message, update.UpdateID)
	}
	code := parts[1]
	if s.redis == nil {
		return nil
	}
	adminRaw, err := s.redis.GetDel(ctx, "support:telegram:bind:"+code).Result()
	if err != nil {
		if err == redis.Nil {
			_, _ = s.call(ctx, cfg.BotToken, "sendMessage", map[string]any{"chat_id": message.Chat.ID, "text": "绑定链接已过期，请回到管理后台重新生成。"})
			return nil
		}
		return err
	}
	adminID, err := strconv.ParseInt(adminRaw, 10, 64)
	if err != nil {
		return err
	}
	other, err := s.client.AdminTelegramBinding.Query().Where(admintelegrambinding.TelegramUserIDEQ(message.From.ID)).Only(ctx)
	if err == nil && other.AdminID != adminID {
		_, _ = s.call(ctx, cfg.BotToken, "sendMessage", map[string]any{"chat_id": message.Chat.ID, "text": "该 Telegram 账号已绑定其他管理员。"})
		return nil
	}
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	existing, err := s.client.AdminTelegramBinding.Query().Where(admintelegrambinding.AdminIDEQ(adminID)).Only(ctx)
	if err == nil {
		_, err = s.client.AdminTelegramBinding.UpdateOneID(existing.ID).SetTelegramUserID(message.From.ID).
			SetChatID(message.Chat.ID).SetTelegramUsername(message.From.Username).SetEnabled(true).SetBoundAt(time.Now()).Save(ctx)
	} else if ent.IsNotFound(err) {
		_, err = s.client.AdminTelegramBinding.Create().SetAdminID(adminID).SetTelegramUserID(message.From.ID).
			SetChatID(message.Chat.ID).SetTelegramUsername(message.From.Username).SetEnabled(true).SetBoundAt(time.Now()).Save(ctx)
	}
	if err != nil {
		return err
	}
	_, _ = s.call(ctx, cfg.BotToken, "sendMessage", map[string]any{"chat_id": message.Chat.ID, "text": "FastVibe 客服通知已绑定"})
	return nil
}

func (s *SupportTelegramService) handleAdminReply(ctx context.Context, cfg *supportTelegramRuntimeConfig, message *telegramMessage, updateID int64) error {
	if s.support == nil || message.From == nil {
		return nil
	}
	binding, err := s.client.AdminTelegramBinding.Query().Where(
		admintelegrambinding.TelegramUserIDEQ(message.From.ID),
		admintelegrambinding.ChatIDEQ(message.Chat.ID),
		admintelegrambinding.EnabledEQ(true),
	).Only(ctx)
	if err != nil {
		return nil
	}
	if message.ReplyToMessage == nil {
		_, _ = s.call(ctx, cfg.BotToken, "sendMessage", map[string]any{"chat_id": message.Chat.ID, "text": "请使用 Telegram 的“回复”功能引用一条客服通知后再发送，避免回复到错误会话。"})
		return nil
	}
	mapping, err := s.client.SupportNotificationOutbox.Query().Where(
		supportnotificationoutbox.TargetAdminIDEQ(binding.AdminID),
		supportnotificationoutbox.TelegramMessageIDEQ(message.ReplyToMessage.MessageID),
		supportnotificationoutbox.StatusEQ("sent"),
	).Only(ctx)
	if err != nil || mapping.MessageID == nil {
		_, _ = s.call(ctx, cfg.BotToken, "sendMessage", map[string]any{"chat_id": message.Chat.ID, "text": "这条消息不是有效的客服通知，请引用最新的客服通知回复。"})
		return nil
	}
	content := strings.TrimSpace(message.Text)
	if content == "" {
		content = strings.TrimSpace(message.Caption)
	}
	uploads, err := s.telegramPhotoUploads(ctx, cfg.BotToken, message)
	if err != nil {
		_, _ = s.call(ctx, cfg.BotToken, "sendMessage", map[string]any{"chat_id": message.Chat.ID, "text": "图片下载失败，请重试或改为发送文字。"})
		return nil
	}
	requestID := fmt.Sprintf("telegram-%d-%d", message.From.ID, updateID)
	_, err = s.support.Reply(ctx, binding.AdminID, mapping.TicketID, true, SupportReplyInput{
		Content: content, ClientRequestID: requestID, Attachments: uploads,
	})
	if err != nil {
		_, _ = s.call(ctx, cfg.BotToken, "sendMessage", map[string]any{"chat_id": message.Chat.ID, "text": "回复发送失败：" + err.Error()})
		return nil
	}
	_, _ = s.call(ctx, cfg.BotToken, "sendMessage", map[string]any{"chat_id": message.Chat.ID, "text": "已回复用户。"})
	return nil
}

func (s *SupportTelegramService) telegramPhotoUploads(ctx context.Context, token string, message *telegramMessage) ([]SupportUpload, error) {
	if len(message.Photo) == 0 {
		return nil, nil
	}
	photo := message.Photo[len(message.Photo)-1]
	result, err := s.call(ctx, token, "getFile", map[string]any{"file_id": photo.FileID})
	if err != nil {
		return nil, err
	}
	var file struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(result.Result, &file); err != nil || strings.TrimSpace(file.FilePath) == "" {
		return nil, fmt.Errorf("Telegram file path missing")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.telegram.org/file/bot"+token+"/"+file.FilePath, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Telegram file download returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, SupportMaxAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	return []SupportUpload{{Name: fmt.Sprintf("telegram-%d.jpg", message.MessageID), ContentType: "image/jpeg", Data: data}}, nil
}

func (s *SupportTelegramService) Start() {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.processOutbox(context.Background())
			case <-s.stop:
				return
			}
		}
	}()
}

func (s *SupportTelegramService) processOutbox(ctx context.Context) {
	now := time.Now()
	_, _ = s.client.SupportNotificationOutbox.Update().Where(
		supportnotificationoutbox.StatusEQ("processing"), supportnotificationoutbox.LockedAtLT(now.Add(-5*time.Minute)),
	).SetStatus("failed").SetNextAttemptAt(now).ClearLockedAt().Save(ctx)
	items, err := s.client.SupportNotificationOutbox.Query().Where(
		supportnotificationoutbox.StatusIn("pending", "failed"), supportnotificationoutbox.NextAttemptAtLTE(now),
	).Order(ent.Asc(supportnotificationoutbox.FieldID)).Limit(20).All(ctx)
	if err != nil {
		logger.L().Warn("support.telegram_outbox_query_failed", zap.Error(err))
		return
	}
	for _, item := range items {
		claimed, err := s.client.SupportNotificationOutbox.Update().Where(
			supportnotificationoutbox.IDEQ(item.ID), supportnotificationoutbox.StatusIn("pending", "failed"),
		).SetStatus("processing").SetLockedAt(now).Save(ctx)
		if err != nil || claimed == 0 {
			continue
		}
		s.deliverOutbox(ctx, item)
	}
}

func (s *SupportTelegramService) deliverOutbox(ctx context.Context, item *ent.SupportNotificationOutbox) {
	cfg, err := s.loadRuntime(ctx)
	if err != nil || !cfg.Enabled || cfg.BotToken == "" {
		s.failOutbox(ctx, item, fmt.Errorf("Telegram bot is not enabled"), 0)
		return
	}
	binding, err := s.client.AdminTelegramBinding.Query().Where(admintelegrambinding.AdminIDEQ(item.TargetAdminID), admintelegrambinding.EnabledEQ(true)).Only(ctx)
	if err != nil {
		s.failOutbox(ctx, item, err, 0)
		return
	}
	var payload supportTelegramNotificationPayload
	if err := json.Unmarshal([]byte(item.Payload), &payload); err != nil {
		s.failOutbox(ctx, item, err, 0)
		return
	}
	text := formatSupportTelegramNotification(item.EventType, payload)
	result, err := s.call(ctx, cfg.BotToken, "sendMessage", map[string]any{"chat_id": binding.ChatID, "text": text})
	if err != nil {
		retry := 0
		if result != nil {
			retry = result.Parameters.RetryAfter
		}
		s.failOutbox(ctx, item, err, retry)
		_, _ = s.client.AdminTelegramBinding.UpdateOneID(binding.ID).SetLastError(err.Error()).Save(ctx)
		return
	}
	var sent telegramMessageResult
	if err := json.Unmarshal(result.Result, &sent); err != nil || sent.MessageID <= 0 {
		s.failOutbox(ctx, item, fmt.Errorf("Telegram sendMessage returned no message ID"), 0)
		return
	}
	if _, err := s.client.SupportNotificationOutbox.UpdateOneID(item.ID).SetStatus("sent").SetSentAt(time.Now()).SetTelegramMessageID(sent.MessageID).ClearLockedAt().ClearLastError().Save(ctx); err != nil {
		s.failOutbox(ctx, item, err, 0)
		return
	}
	_, _ = s.client.AdminTelegramBinding.UpdateOneID(binding.ID).SetLastSuccessAt(time.Now()).ClearLastError().Save(ctx)
}

func (s *SupportTelegramService) failOutbox(ctx context.Context, item *ent.SupportNotificationOutbox, cause error, retryAfter int) {
	attempt := item.AttemptCount + 1
	status := "failed"
	if attempt >= 8 {
		status = "dead"
	}
	delay := time.Duration(1<<min(attempt, 8)) * time.Minute
	if retryAfter > 0 {
		delay = time.Duration(retryAfter) * time.Second
	}
	message := cause.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	_, _ = s.client.SupportNotificationOutbox.UpdateOneID(item.ID).SetStatus(status).SetAttemptCount(attempt).
		SetNextAttemptAt(time.Now().Add(delay)).SetLastError(message).ClearLockedAt().Save(ctx)
}
