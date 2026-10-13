package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func registerUpstreamRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	upstreams := admin.Group("/upstreams")
	upstreams.GET("", h.Admin.Upstream.List)
	upstreams.POST("", h.Admin.Upstream.Create)
	upstreams.PUT("/:id", h.Admin.Upstream.Update)
	upstreams.DELETE("/:id", h.Admin.Upstream.Delete)
	upstreams.POST("/:id/refresh", h.Admin.Upstream.Refresh)
}
