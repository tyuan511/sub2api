package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type imageGenerationGroup struct {
	dto.Group
	ImageModels []string `json:"image_models"`
}

// ImageStudioStatus exposes availability without revealing storage credentials.
func (h *AsyncImageHandler) ImageStudioStatus(c *gin.Context) {
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	response.Success(c, gin.H{"available": h.enabled()})
}

// ImageGenerationGroups serves the same capability catalog to the Key picker
// and creation dialog, including users who have not created any keys yet.
// GET /api/v1/groups/image-generation
func (h *GatewayHandler) ImageGenerationGroups(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	groups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]imageGenerationGroup, 0, len(groups))
	for i := range groups {
		group := &groups[i]
		if group.Platform != service.PlatformOpenAI || group.Status != service.StatusActive || !group.AllowImageGeneration {
			continue
		}
		models, err := h.gatewayService.GetAvailableImageModels(c.Request.Context(), group.ID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if group.CustomModelsListEnabled() {
			models = filterModelsByCustomList(models, nil, group.ModelsListConfig.Models)
		}
		if len(models) > 0 {
			out = append(out, imageGenerationGroup{Group: *dto.GroupFromService(group), ImageModels: models})
		}
	}
	response.Success(c, out)
}
