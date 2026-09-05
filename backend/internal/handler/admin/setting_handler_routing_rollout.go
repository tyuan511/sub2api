package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"log/slog"
)

func (h *SettingHandler) GetAPIKeyRoutingRollout(c *gin.Context) {
	settings, err := h.settingService.GetAPIKeyRoutingRolloutSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateAPIKeyRoutingRollout(c *gin.Context) {
	var req struct {
		UserIDs *[]int64 `json:"user_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "user_ids must be an array of integer IDs (use [] to disable rollout)")
		return
	}
	settings, err := service.NormalizeAPIKeyRoutingRollout(service.APIKeyRoutingRolloutSettings{UserIDs: *req.UserIDs})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if len(settings.UserIDs) > 0 && h.userService == nil {
		response.Error(c, 503, "User validation unavailable")
		return
	}
	for _, id := range settings.UserIDs {
		if _, err := h.userService.GetProfile(c.Request.Context(), id); err != nil {
			response.BadRequest(c, "Rollout users must exist and must not be deleted")
			return
		}
	}
	if err := h.settingService.SetAPIKeyRoutingRolloutSettings(c.Request.Context(), settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	slog.Info("API key routing rollout updated", "audit", true, "user_id", subject.UserID, "rollout_user_ids", settings.UserIDs)
	response.Success(c, settings)
}
