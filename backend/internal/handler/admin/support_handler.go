package admin

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

func adminActor(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	return subject.UserID, ok
}

func (h *SupportHandler) List(c *gin.Context) {
	adminID, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	var targetUserID int64
	if raw := strings.TrimSpace(c.Query("user_id")); raw != "" {
		var err error
		targetUserID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || targetUserID <= 0 {
			response.BadRequest(c, "Invalid user ID")
			return
		}
	}
	page, size := response.ParsePagination(c)
	result, err := h.support.List(c.Request.Context(), adminID, true, service.SupportListFilter{
		Search: c.Query("search"), UserID: targetUserID, Page: page, PageSize: size,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *SupportHandler) SearchUsers(c *gin.Context) {
	if _, ok := adminActor(c); !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	items, err := h.support.SearchUsers(c.Request.Context(), c.Query("search"), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *SupportHandler) StartConversation(c *gin.Context) {
	adminID, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	uploads, err := readAdminSupportUploads(c)
	if err != nil {
		writeAdminSupportUploadError(c, err)
		return
	}
	targetUserID, err := strconv.ParseInt(c.PostForm("user_id"), 10, 64)
	if err != nil || targetUserID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	item, err := h.support.StartAdminConversation(c.Request.Context(), adminID, targetUserID, service.SupportReplyInput{
		Content: c.PostForm("content"), ClientRequestID: c.PostForm("client_request_id"), Attachments: uploads,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *SupportHandler) Get(c *gin.Context) {
	adminID, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid ticket ID")
		return
	}
	item, err := h.support.Get(c.Request.Context(), adminID, id, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *SupportHandler) Reply(c *gin.Context) {
	adminID, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid ticket ID")
		return
	}
	uploads, err := readAdminSupportUploads(c)
	if err != nil {
		writeAdminSupportUploadError(c, err)
		return
	}
	item, err := h.support.Reply(c.Request.Context(), adminID, id, true, service.SupportReplyInput{
		Content: c.PostForm("content"), ClientRequestID: c.PostForm("client_request_id"), Attachments: uploads,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func readAdminSupportUploads(c *gin.Context) ([]service.SupportUpload, error) {
	// Keep multipart parsing in the public handler implementation through the
	// same request contract; admin uploads use the same validation in service.
	if err := c.Request.ParseMultipartForm(service.SupportMaxRequestBodyBytes); err != nil {
		return nil, err
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

func writeAdminSupportUploadError(c *gin.Context, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		response.Error(c, http.StatusRequestEntityTooLarge, "单次发送内容和图片总大小不能超过 7 MB")
		return
	}
	response.BadRequest(c, err.Error())
}

func (h *SupportHandler) UnreadCount(c *gin.Context) {
	adminID, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	count, err := h.support.UnreadCount(c.Request.Context(), adminID, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"count": count})
}

func (h *SupportHandler) Attachment(c *gin.Context) {
	adminID, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid attachment ID")
		return
	}
	download, err := h.support.OpenAttachment(c.Request.Context(), adminID, id, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	defer func() { _ = download.Body.Close() }()
	c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": download.Name}))
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, download.Size, download.ContentType, download.Body, nil)
}

type hideAttachmentRequest struct {
	Hidden bool `json:"hidden"`
}

func (h *SupportHandler) HideAttachment(c *gin.Context) {
	adminID, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid attachment ID")
		return
	}
	var req hideAttachmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	if err := h.support.HideAttachment(c.Request.Context(), adminID, id, req.Hidden); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"ok": true})
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
	pubsub := h.support.Subscribe(c.Request.Context(), service.SupportAdminChannel)
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
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
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

func (h *SupportHandler) GetTelegramConfig(c *gin.Context) {
	item, err := h.telegram.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}
func (h *SupportHandler) SaveTelegramConfig(c *gin.Context) {
	var req service.TelegramConfigInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	item, err := h.telegram.SaveConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}
func (h *SupportHandler) GetTelegramBinding(c *gin.Context) {
	id, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found")
		return
	}
	item, err := h.telegram.GetBinding(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}
func (h *SupportHandler) CreateTelegramBindLink(c *gin.Context) {
	id, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found")
		return
	}
	item, err := h.telegram.CreateBindLink(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}
func (h *SupportHandler) UpdateTelegramBinding(c *gin.Context) {
	id, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found")
		return
	}
	var req service.TelegramBindingInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	item, err := h.telegram.UpdateBinding(c.Request.Context(), id, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}
func (h *SupportHandler) DeleteTelegramBinding(c *gin.Context) {
	id, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found")
		return
	}
	if err := h.telegram.DeleteBinding(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"ok": true})
}
func (h *SupportHandler) TestTelegramBinding(c *gin.Context) {
	id, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found")
		return
	}
	if err := h.telegram.TestBinding(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"ok": true})
}
