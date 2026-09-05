package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type BatchImageHandler struct {
	service  *service.BatchImagePublicService
	download *service.BatchImageDownloadService
	cleanup  *service.BatchImageCleanupService
	openAI   *OpenAIGatewayHandler
}

func NewBatchImageHandler(service *service.BatchImagePublicService, download *service.BatchImageDownloadService, cleanup *service.BatchImageCleanupService) *BatchImageHandler {
	return &BatchImageHandler{service: service, download: download, cleanup: cleanup}
}

func (h *BatchImageHandler) Submit(c *gin.Context) {
	var req service.BatchImageSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		batchImageError(c, service.ErrBatchImageInvalidItems)
		return
	}
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	if sessionID := service.ExtractClientSessionID(c); sessionID != "" {
		req.SessionID = &sessionID
	}
	apiKey, _ := middleware.GetAPIKeyFromContext(c)
	routeEndpoint := c.Request.URL.Path
	routeSessionSeed := service.HashBatchImageSubmitRequest(req)
	if idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key")); idempotencyKey != "" {
		routeSessionSeed = idempotencyKey
	}
	routeSessionHash := ""
	stickyModelFamily, stickyEndpointKind := "", ""
	if h.openAI != nil && h.openAI.gatewayService != nil && apiKey != nil {
		routeSessionHash = h.openAI.gatewayService.GenerateExplicitSessionHash(c, []byte(routeSessionSeed))
		stickyModelFamily, stickyEndpointKind = apiKeyRouteStickyScope(apiKey, req.Model, routeEndpoint)
	}
	if apiKeyMultiGroupRoutingActive(c) && h.openAI != nil && h.openAI.gatewayService != nil && apiKey != nil {
		batchCandidateCheck := func(candidate *service.APIKey) error {
			if candidate == nil || candidate.Group == nil {
				return service.ErrNoEligibleAPIKeyRoute
			}
			gid := candidate.Group.ID
			candidateOwner := service.BatchImageOwner{UserID: candidate.UserID, APIKeyID: candidate.ID, GroupID: &gid}
			if err := h.service.CheckRouteCandidate(c.Request.Context(), candidateOwner, req); err != nil {
				return err
			}
			allowed, _, healthErr := h.openAI.gatewayService.AllowAPIKeyRoute(c.Request.Context(), candidate.ID, candidate.RouteVersion, candidate.Group.ID, req.Model, routeEndpoint)
			if healthErr != nil {
				return nil
			}
			if !allowed {
				return service.ErrNoEligibleAPIKeyRoute
			}
			return nil
		}
		stickyGroupID, stickyErr := h.openAI.gatewayService.GetAPIKeyGroupSticky(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, stickyModelFamily, stickyEndpointKind, routeSessionHash)
		stickySelected := stickyErr == nil && stickyGroupID > 0 && apiKey.GroupID != nil && *apiKey.GroupID == stickyGroupID
		if stickyErr == nil && stickyGroupID > 0 && !stickySelected {
			stickyAPIKey, _, activated, activateErr := h.openAI.apiKeyRouteRuntime().activateSticky(c, stickyGroupID, batchCandidateCheck)
			if activateErr != nil {
				middleware.MarkAPIKeyRouteStickySelected(c)
				middleware.MarkAPIKeyRouteStickyBroken(c)
			} else if activated {
				stickySelected = true
				apiKey = stickyAPIKey
			}
		}
		if stickySelected {
			middleware.MarkAPIKeyRouteStickySelected(c)
		}
		if shouldActivateSmartRoute(c, stickySelected, stickyErr != nil) {
			smartAPIKey, _, ranked, _, smartErr := h.openAI.apiKeyRouteRuntime().activateSmart(c, apiKey, req.Model, routeEndpoint, routeSessionHash, batchCandidateCheck)
			if smartErr != nil {
				batchImageError(c, service.ErrBatchImageNoAccountAvailable)
				return
			}
			if len(ranked) > 0 {
				apiKey = smartAPIKey
			}
		}
		actualAPIKey, _, changed, routeErr := h.openAI.apiKeyRouteRuntime().ensureInitial(c, batchCandidateCheck)
		if routeErr != nil {
			batchImageError(c, service.ErrBatchImageNoAccountAvailable)
			return
		}
		apiKey = actualAPIKey
		if changed && stickySelected {
			middleware.MarkAPIKeyRouteStickyBroken(c)
		}
		owner, ok = batchImageOwnerFromContext(c)
		if !ok {
			batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
			return
		}
	}
	if !h.checkSecurityAuditBeforeSubmit(c, &req) {
		return
	}
	got, err := h.service.Submit(c.Request.Context(), owner, req, c.GetHeader("Idempotency-Key"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	if !got.IdempotentReplay && h.openAI != nil && h.openAI.gatewayService != nil && apiKey != nil && got.ActualGroupID != nil {
		_, _ = h.openAI.gatewayService.RecordAPIKeyRouteResult(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, *got.ActualGroupID, req.Model, routeEndpoint, true)
		_ = h.openAI.gatewayService.BindAPIKeyGroupSticky(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, stickyModelFamily, stickyEndpointKind, routeSessionHash, *got.ActualGroupID)
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) checkSecurityAuditBeforeSubmit(c *gin.Context, req *service.BatchImageSubmitRequest) bool {
	if h == nil || h.openAI == nil || req == nil {
		return true
	}
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusInternalServerError, "USER_CONTEXT_REQUIRED", "User context not found"))
		return false
	}
	items := make([]map[string]string, 0, len(req.Items))
	for _, item := range req.Items {
		if prompt := strings.TrimSpace(item.Prompt); prompt != "" {
			items = append(items, map[string]string{"prompt": prompt})
		}
	}
	if len(items) == 0 {
		return true
	}
	body, err := json.Marshal(map[string]any{"request": map[string]any{"items": items}})
	if err != nil {
		batchImageError(c, infraerrors.New(http.StatusBadRequest, "INVALID_BATCH_PROMPT", "batch prompts are invalid"))
		return false
	}
	reqLog := requestLogger(c, "handler.batch_image.security_audit",
		zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", apiKey.ID), zap.String("model", req.Model))
	decision := h.openAI.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, req.Model, body)
	if decision != nil && !decision.AllowNextStage {
		h.openAI.openAISecurityAuditError(c, decision)
		return false
	}
	return true
}

func (h *BatchImageHandler) Get(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.service.Get(c.Request.Context(), owner, c.Param("id"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) List(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	got, err := h.service.List(c.Request.Context(), owner, service.BatchImageJobsQuery{
		Status:     c.Query("status"),
		TaskName:   c.Query("task_name"),
		Downloaded: c.Query("downloaded"),
		From:       c.Query("from"),
		To:         c.Query("to"),
		Limit:      limit,
		Cursor:     c.Query("cursor"),
	})
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) Models(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.service.ListModels(c.Request.Context(), owner)
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) Items(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	got, err := h.service.ListItems(c.Request.Context(), owner, c.Param("id"), service.BatchImageItemsQuery{
		Status: c.Query("status"),
		Limit:  limit,
		Cursor: c.Query("cursor"),
	})
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) Cancel(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.service.Cancel(c.Request.Context(), owner, c.Param("id"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) ItemContent(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	imageIndex := 0
	if raw := c.Query("image_index"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			batchImageError(c, service.ErrBatchImageItemImageIndexOutOfRange)
			return
		}
		imageIndex = parsed
	}
	stream, err := h.download.OpenItemContent(c.Request.Context(), owner, c.Param("id"), c.Param("custom_id"), imageIndex)
	if err != nil {
		batchImageError(c, err)
		return
	}
	defer func() { _ = stream.Reader.Close() }()

	c.Header("Content-Type", stream.ContentType)
	c.Header("Content-Disposition", service.BatchImageContentDispositionAttachment(stream.Filename))
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")
	if stream.ContentLength != nil && *stream.ContentLength >= 0 {
		c.Header("Content-Length", strconv.FormatInt(*stream.ContentLength, 10))
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, stream.Reader); err != nil {
		return
	}
	h.markDownloadedBestEffort(c, owner)
}

// markDownloadedBestEffort 在响应体已写出后标记下载状态；
// 此时无法再向客户端返回错误，失败只能记日志（不能静默丢弃）。
func (h *BatchImageHandler) markDownloadedBestEffort(c *gin.Context, owner service.BatchImageOwner) {
	if err := h.service.MarkDownloaded(c.Request.Context(), owner, c.Param("id")); err != nil {
		logger.L().Warn("batch_image.mark_downloaded_failed",
			zap.String("batch_id", c.Param("id")),
			zap.Error(err),
		)
	}
}

func (h *BatchImageHandler) Download(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	maxItems, _ := strconv.Atoi(c.Query("max_items"))

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", service.BatchImageContentDispositionAttachment(c.Param("id")+".zip"))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	result, err := h.download.StreamZip(c.Request.Context(), owner, c.Param("id"), service.BatchImageZipOptions{
		Status:          c.Query("status"),
		MaxItems:        maxItems,
		IncludeManifest: true,
	}, c.Writer)
	if err != nil {
		if result == nil || !c.Writer.Written() {
			batchImageError(c, err)
		}
		return
	}
	h.markDownloadedBestEffort(c, owner)
}

func (h *BatchImageHandler) DeleteRecord(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	if err := h.service.DeleteRecord(c.Request.Context(), owner, c.Param("id")); err != nil {
		batchImageError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *BatchImageHandler) DeleteOutputs(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.cleanup.DeleteOutputsForOwner(c.Request.Context(), owner, c.Param("id"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func batchImageOwnerFromContext(c *gin.Context) (service.BatchImageOwner, bool) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.ID <= 0 || apiKey.UserID <= 0 {
		return service.BatchImageOwner{}, false
	}
	return service.BatchImageOwner{
		UserID:   apiKey.UserID,
		APIKeyID: apiKey.ID,
		GroupID:  apiKey.GroupID,
	}, true
}

func batchImageError(c *gin.Context, err error) {
	status := infraerrors.Code(err)
	code := infraerrors.Reason(err)
	message := infraerrors.Message(err)
	if err == nil {
		status = http.StatusInternalServerError
		code = "INTERNAL_ERROR"
		message = "internal error"
	}
	if status == 0 || (status == http.StatusInternalServerError && strings.TrimSpace(code) == "") {
		status = http.StatusInternalServerError
		code = "INTERNAL_ERROR"
		message = "internal error"
	}
	if errors.Is(err, service.ErrBatchImageJobNotFound) {
		status = http.StatusNotFound
		code = "BATCH_IMAGE_NOT_FOUND"
		message = "batch image job not found"
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    "invalid_request_error",
			"code":    code,
			"message": message,
		},
	})
}
