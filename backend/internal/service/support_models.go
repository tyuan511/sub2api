package service

import (
	"io"
	"time"
)

const (
	SupportStatusOpen        = "open"
	SupportStatusWaitingUser = "waiting_user"
	SupportStatusResolved    = "resolved"
	SupportStatusClosed      = "closed"

	SupportPriorityNormal = "normal"

	SupportMessagePublic = "public"
)

type SupportUpload struct {
	Name        string
	ContentType string
	Data        []byte
}

type SupportCreateInput struct {
	Content         string
	ClientRequestID string
	Attachments     []SupportUpload
}

type SupportReplyInput struct {
	Content         string
	ClientRequestID string
	Attachments     []SupportUpload
}

type SupportListFilter struct {
	Search   string
	UserID   int64
	Page     int
	PageSize int
}

type SupportAttachment struct {
	ID           int64      `json:"id"`
	MessageID    int64      `json:"message_id"`
	OriginalName string     `json:"original_name"`
	ContentType  string     `json:"content_type"`
	Size         int64      `json:"size"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	HiddenAt     *time.Time `json:"hidden_at,omitempty"`
	DownloadURL  string     `json:"download_url"`
}

type SupportMessage struct {
	ID          int64               `json:"id"`
	SenderID    int64               `json:"sender_id"`
	SenderRole  string              `json:"sender_role"`
	Content     string              `json:"content"`
	Attachments []SupportAttachment `json:"attachments"`
	CreatedAt   time.Time           `json:"created_at"`
}

type SupportTicket struct {
	ID                 int64            `json:"id"`
	UserID             int64            `json:"user_id"`
	UserEmail          string           `json:"user_email,omitempty"`
	UserName           string           `json:"user_name,omitempty"`
	LastMessageAt      time.Time        `json:"last_message_at"`
	LastMessagePreview string           `json:"last_message_preview,omitempty"`
	UnreadCount        int              `json:"unread_count"`
	Messages           []SupportMessage `json:"messages,omitempty"`
}

type SupportListResult struct {
	Items    []SupportTicket `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	Pages    int             `json:"pages"`
}

type SupportUserSearchItem struct {
	UserID             int64      `json:"user_id"`
	UserEmail          string     `json:"user_email"`
	UserName           string     `json:"user_name"`
	TicketID           *int64     `json:"ticket_id,omitempty"`
	LastMessageAt      *time.Time `json:"last_message_at,omitempty"`
	LastMessagePreview string     `json:"last_message_preview,omitempty"`
}

type SupportAttachmentDownload struct {
	Body        io.ReadCloser
	Name        string
	ContentType string
	Size        int64
}

type SupportRealtimeEvent struct {
	Type      string    `json:"type"`
	TicketID  int64     `json:"ticket_id"`
	UserID    int64     `json:"user_id"`
	MessageID *int64    `json:"message_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type TelegramConfigView struct {
	Enabled        bool   `json:"enabled"`
	BotUsername    string `json:"bot_username"`
	TokenSet       bool   `json:"token_set"`
	WebhookSet     bool   `json:"webhook_set"`
	WebhookBaseURL string `json:"webhook_base_url"`
}

type TelegramConfigInput struct {
	Enabled        bool   `json:"enabled"`
	BotToken       string `json:"bot_token"`
	WebhookBaseURL string `json:"webhook_base_url"`
}

type TelegramBindingView struct {
	Bound            bool       `json:"bound"`
	Enabled          bool       `json:"enabled"`
	TelegramUsername string     `json:"telegram_username,omitempty"`
	NotifyNewTicket  bool       `json:"notify_new_ticket"`
	NotifyUserReply  bool       `json:"notify_user_reply"`
	BoundAt          *time.Time `json:"bound_at,omitempty"`
	LastSuccessAt    *time.Time `json:"last_success_at,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
}

type TelegramBindingInput struct {
	Enabled         bool `json:"enabled"`
	NotifyNewTicket bool `json:"notify_new_ticket"`
	NotifyUserReply bool `json:"notify_user_reply"`
}

type TelegramBindLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
