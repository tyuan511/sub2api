package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type UpstreamHandler struct{ service *service.UpstreamService }

func NewUpstreamHandler(s *service.UpstreamService) *UpstreamHandler {
	return &UpstreamHandler{service: s}
}

func upstreamID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid upstream ID")
		return 0, false
	}
	return id, true
}
func (h *UpstreamHandler) List(c *gin.Context) {
	list, err := h.service.List(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, list)
}
func (h *UpstreamHandler) Create(c *gin.Context) { h.save(c, 0) }
func (h *UpstreamHandler) Update(c *gin.Context) {
	if id, ok := upstreamID(c); ok {
		h.save(c, id)
	}
}
func (h *UpstreamHandler) save(c *gin.Context, id int64) {
	var input service.UpstreamInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid upstream configuration")
		return
	}
	u, err := h.service.Save(c.Request.Context(), id, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, u)
}
func (h *UpstreamHandler) Delete(c *gin.Context) {
	id, ok := upstreamID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
func (h *UpstreamHandler) Refresh(c *gin.Context) {
	id, ok := upstreamID(c)
	if !ok {
		return
	}
	u, err := h.service.Refresh(c.Request.Context(), id, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, u)
}
