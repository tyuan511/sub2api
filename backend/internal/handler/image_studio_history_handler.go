package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ImageStudioHandler struct {
	studio *service.ImageStudioService
	async  *AsyncImageHandler
}

func NewImageStudioHandler(studio *service.ImageStudioService, async *AsyncImageHandler) *ImageStudioHandler {
	return &ImageStudioHandler{studio: studio, async: async}
}
func studioUser(c *gin.Context) int64 {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0
	}
	return subject.UserID
}
func (h *ImageStudioHandler) Status(c *gin.Context) {
	if studioUser(c) == 0 {
		return
	}
	response.Success(c, gin.H{"available": h != nil && h.studio != nil && h.studio.Available(c.Request.Context())})
}
func (h *ImageStudioHandler) List(c *gin.Context) {
	userID := studioUser(c)
	if userID == 0 {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 || page > 100000 {
		response.BadRequest(c, "Invalid page")
		return
	}
	items, more, err := h.studio.List(c.Request.Context(), userID, page)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"items": items, "has_more": more, "page": page})
}
func (h *ImageStudioHandler) Get(c *gin.Context) {
	userID := studioUser(c)
	if userID == 0 {
		return
	}
	item, err := h.studio.Get(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, item)
}
func (h *ImageStudioHandler) Delete(c *gin.Context) {
	userID := studioUser(c)
	if userID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	if err := h.studio.Delete(ctx, c.Param("id"), userID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
func (h *ImageStudioHandler) File(c *gin.Context) {
	userID := studioUser(c)
	if userID == 0 {
		return
	}
	file, err := h.studio.File(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, file)
}

func (h *ImageStudioHandler) Legacy(c *gin.Context) {
	userID := studioUser(c)
	if userID == 0 {
		return
	}
	result, err := h.studio.Legacy(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, result)
}

func (h *ImageStudioHandler) Thumbnail(c *gin.Context) {
	userID := studioUser(c)
	if userID == 0 {
		return
	}
	file, err := h.studio.Thumbnail(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, file)
}

// Uses the gateway's existing API-key authentication, group/IP/quota checks,
// request validation, moderation and billed generation execution.
func (h *ImageStudioHandler) Submit(c *gin.Context) {
	if h == nil || h.studio == nil || h.async == nil || h.async.openAI == nil {
		imageTaskError(c, service.ErrImageTaskUnavailable)
		return
	}
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.UserID <= 0 || key.ID <= 0 {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	if key.Group == nil || key.Group.Platform != service.PlatformOpenAI || !service.GroupAllowsImageGeneration(key.Group) {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		imageTaskJSONError(c, 400, "invalid_request_error", "无法读取生图请求")
		return
	}
	c.Request = c.Request.Clone(c.Request.Context())
	if strings.HasSuffix(c.Request.URL.Path, "/edits") {
		c.Request.URL.Path = "/v1/images/edits/async"
	} else {
		c.Request.URL.Path = "/v1/images/generations/async"
	}
	parsed, err := h.async.openAI.gatewayService.ParseOpenAIImagesRequest(c, body)
	if err != nil {
		imageTaskJSONError(c, 400, "invalid_request_error", err.Error())
		return
	}
	if parsed.Stream || parsed.N < 1 || parsed.N > 4 || len(parsed.Uploads) > 4 || len(parsed.InputImageURLs) > 0 || parsed.HasMask || !strings.HasPrefix(parsed.Model, "gpt-image-") {
		imageTaskJSONError(c, 400, "invalid_request_error", "图片创作支持 1–4 张 GPT Image 输出和最多 4 张上传参考图")
		return
	}
	for _, file := range parsed.Uploads {
		if len(file.Data) > 10*1024*1024 {
			imageTaskJSONError(c, 400, "invalid_request_error", "参考图不能超过 10 MB")
			return
		}
	}
	if !h.async.checkSecurityAuditBeforeSubmit(c, key, service.PlatformOpenAI, body) {
		return
	}
	ratio, resolution := studioDimensions(parsed.Size)
	meta := service.StudioMetadata{Prompt: parsed.Prompt, Model: parsed.Model, Count: parsed.N, Ratio: ratio, Resolution: resolution, Size: parsed.Size, KeyName: key.Name}
	task, tasks, err := h.studio.Start(c.Request.Context(), service.ImageTaskOwner{UserID: key.UserID, APIKeyID: key.ID}, meta, parsed.Uploads)
	if err != nil {
		imageTaskError(c, err)
		return
	}
	taskCtx, recorder, cancel := newAsyncImageContext(c, body, tasks.ExecutionTimeout())
	runner := *h.async
	runner.tasks = tasks
	c.Header("Cache-Control", "no-store")
	c.Header("Retry-After", "3")
	c.JSON(http.StatusAccepted, gin.H{"id": task.ID, "task_id": task.ID, "status": task.Status})
	go runner.run(task.ID, service.PlatformOpenAI, taskCtx, recorder, cancel)
}
func studioDimensions(size string) (string, string) {
	if strings.TrimSpace(size) == "" || strings.EqualFold(size, "auto") {
		return "auto", "1K"
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "1:1", "1K"
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	if w <= 0 || h <= 0 {
		return "1:1", "1K"
	}
	a, b := w, h
	for b != 0 {
		a, b = b, a%b
	}
	tier := "1K"
	if w > 2048 || h > 2048 {
		tier = "4K"
	} else if w > 1024 || h > 1024 {
		tier = "2K"
	}
	// Studio display presets are separate from gateway billing tiers.
	if size == "1792x1008" || size == "1008x1792" || size == "1792x768" {
		tier = "1K"
	}
	ratio := strconv.Itoa(w/a) + ":" + strconv.Itoa(h/a)
	if ratio == "7:3" {
		ratio = "21:9"
	}
	return ratio, tier
}
func (h *ImageStudioHandler) StorageSettings(c *gin.Context) {
	state, profiles, err := h.studio.StorageSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"active_id": state.ActiveID, "enabled": state.Enabled, "profiles": profiles})
}
func (h *ImageStudioHandler) MigrateStorage(c *gin.Context) {
	var in struct {
		From int64 `json:"from_id"`
		To   int64 `json:"to_id"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "Invalid migration")
		return
	}
	moved, remaining, err := h.studio.MigrateStorage(c.Request.Context(), in.From, in.To)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"moved": moved, "remaining": remaining})
}
func (h *ImageStudioHandler) Import(c *gin.Context) {
	userID := studioUser(c)
	if userID == 0 {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 180*1024*1024)
	if err := c.Request.ParseMultipartForm(8 * 1024 * 1024); err != nil {
		response.BadRequest(c, "Invalid history upload")
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	var in struct {
		service.StudioMetadata
		Status    string `json:"status"`
		Error     string `json:"error"`
		CreatedAt int64  `json:"created_at"`
	}
	if err := json.Unmarshal([]byte(c.Request.FormValue("metadata")), &in); err != nil || len(in.Prompt) > 128000 || len(in.Error) > 600 || len(in.KeyName) > 100 || in.Count < 1 || in.Count > 4 {
		response.BadRequest(c, "Invalid history metadata")
		return
	}
	files := map[string][]service.OpenAIImagesUpload{}
	for _, kind := range []string{"reference", "output"} {
		headers := c.Request.MultipartForm.File[kind]
		if len(headers) > 4 {
			response.BadRequest(c, "Too many images")
			return
		}
		for _, header := range headers {
			f, err := header.Open()
			if err != nil {
				response.BadRequest(c, "Invalid image")
				return
			}
			data, err := io.ReadAll(io.LimitReader(f, 32*1024*1024+1))
			f.Close()
			if err != nil || len(data) > 32*1024*1024 {
				response.BadRequest(c, "Image exceeds 32 MB")
				return
			}
			files[kind] = append(files[kind], service.OpenAIImagesUpload{FileName: header.Filename, ContentType: header.Header.Get("Content-Type"), Data: data})
		}
	}
	item, err := h.studio.Import(c.Request.Context(), userID, in.StudioMetadata, in.Status, in.Error, in.CreatedAt, files["reference"], files["output"])
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}
