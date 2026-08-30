package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type SupportHandler struct {
	support  *service.SupportService
	telegram *service.SupportTelegramService
}

func NewSupportHandler(support *service.SupportService, telegram *service.SupportTelegramService) *SupportHandler {
	return &SupportHandler{support: support, telegram: telegram}
}

func supportActor(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	return subject.UserID, ok
}

func readSupportUploads(c *gin.Context) ([]service.SupportUpload, error) {
	if err := c.Request.ParseMultipartForm(service.SupportMaxRequestBodyBytes); err != nil {
		return nil, fmt.Errorf("parse multipart form: %w", err)
	}
	files := c.Request.MultipartForm.File["attachments"]
	if len(files) > service.SupportMaxAttachments {
		return nil, fmt.Errorf("一次最多上传 %d 张图片", service.SupportMaxAttachments)
	}
	uploads := make([]service.SupportUpload, 0, len(files))
	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, service.SupportMaxAttachmentBytes+1))
		_ = file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(data) > service.SupportMaxAttachmentBytes {
			return nil, fmt.Errorf("单张图片不能超过 3 MB")
		}
		uploads = append(uploads, service.SupportUpload{Name: header.Filename, ContentType: header.Header.Get("Content-Type"), Data: data})
	}
	return uploads, nil
}

func writeSupportUploadError(c *gin.Context, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		response.Error(c, http.StatusRequestEntityTooLarge, "单次发送内容和图片总大小不能超过 7 MB")
		return
	}
	response.BadRequest(c, err.Error())
}

func writeSupportServiceError(c *gin.Context, err error) {
	appError := infraerrors.FromError(err)
	if retryAfter := appError.Metadata["retry_after_seconds"]; retryAfter != "" {
		c.Header("Retry-After", retryAfter)
	}
	response.ErrorFrom(c, err)
}

func (h *SupportHandler) List(c *gin.Context) {
	userID, ok := supportActor(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	page, size := response.ParsePagination(c)
	result, err := h.support.List(c.Request.Context(), userID, false, service.SupportListFilter{
		Search: c.Query("search"), Page: page, PageSize: size,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *SupportHandler) Create(c *gin.Context) {
	userID, ok := supportActor(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	if err := h.support.EnforceUserSendAttemptQuota(c.Request.Context(), userID); err != nil {
		writeSupportServiceError(c, err)
		return
	}
	uploads, err := readSupportUploads(c)
	if err != nil {
		writeSupportUploadError(c, err)
		return
	}
	item, err := h.support.Create(c.Request.Context(), userID, service.SupportCreateInput{
		Content:         c.PostForm("content"),
		ClientRequestID: c.PostForm("client_request_id"), Attachments: uploads,
	})
	if err != nil {
		writeSupportServiceError(c, err)
		return
	}
	response.Created(c, item)
}

func (h *SupportHandler) Get(c *gin.Context) {
	userID, ok := supportActor(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid ticket ID")
		return
	}
	item, err := h.support.Get(c.Request.Context(), userID, id, false)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *SupportHandler) Reply(c *gin.Context) {
	userID, ok := supportActor(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid ticket ID")
		return
	}
	if err := h.support.EnforceUserSendAttemptQuota(c.Request.Context(), userID); err != nil {
		writeSupportServiceError(c, err)
		return
	}
	uploads, err := readSupportUploads(c)
	if err != nil {
		writeSupportUploadError(c, err)
		return
	}
	item, err := h.support.Reply(c.Request.Context(), userID, id, false, service.SupportReplyInput{
		Content: c.PostForm("content"), ClientRequestID: c.PostForm("client_request_id"), Attachments: uploads,
	})
	if err != nil {
		writeSupportServiceError(c, err)
		return
	}
	response.Success(c, item)
}

func (h *SupportHandler) UnreadCount(c *gin.Context) {
	userID, ok := supportActor(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	count, err := h.support.UnreadCount(c.Request.Context(), userID, false)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"count": count})
}

func serveSupportAttachment(c *gin.Context, download *service.SupportAttachmentDownload) {
	defer func() { _ = download.Body.Close() }()
	disposition := mime.FormatMediaType("inline", map[string]string{"filename": download.Name})
	c.Header("Content-Type", download.ContentType)
	c.Header("Content-Disposition", disposition)
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, download.Size, download.ContentType, download.Body, nil)
}

func (h *SupportHandler) Attachment(c *gin.Context) {
	userID, ok := supportActor(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid attachment ID")
		return
	}
	download, err := h.support.OpenAttachment(c.Request.Context(), userID, id, false)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	serveSupportAttachment(c, download)
}

func sameOriginSupportWS(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

var supportWSUpgrader = websocket.Upgrader{CheckOrigin: sameOriginSupportWS, Subprotocols: []string{"sub2api-support"}}

func (h *SupportHandler) WebSocket(c *gin.Context) {
	userID, ok := supportActor(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	h.serveWebSocket(c, service.SupportUserChannel(userID))
}

func (h *SupportHandler) serveWebSocket(c *gin.Context, channel string) {
	pubsub := h.support.Subscribe(c.Request.Context(), channel)
	if pubsub == nil {
		response.Error(c, http.StatusServiceUnavailable, "Realtime support is unavailable")
		return
	}
	defer func() { _ = pubsub.Close() }()
	conn, err := supportWSUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	ch := pubsub.Channel()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-ch:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(message.Payload)); err != nil {
				return
			}
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *SupportHandler) TelegramWebhook(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		response.BadRequest(c, "Invalid body")
		return
	}
	err = h.telegram.HandleWebhook(c.Request.Context(), c.GetHeader("X-Telegram-Bot-Api-Secret-Token"), body)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"ok": true})
}
