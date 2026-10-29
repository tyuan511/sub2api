package admin

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// RefundHandler 是余额退款申请的管理端入口：查看申请、标记完成或拒绝。
type RefundHandler struct {
	refund *service.RefundService
}

func NewRefundHandler(refund *service.RefundService) *RefundHandler {
	return &RefundHandler{refund: refund}
}

// RejectRefundRequestBody 是拒绝申请的请求体，Reason 必填。
type RejectRefundRequestBody struct {
	Reason string `json:"reason"`
}

// CompleteRefundRequestBody 是完成申请的请求体。Note 可选（打款流水号等），
// 允许整个请求体为空，兼容不带 body 的调用方。
type CompleteRefundRequestBody struct {
	Note string `json:"note"`
}

// ListRequests returns refund requests with the applicant identity attached.
// GET /api/v1/admin/refund-requests
func (h *RefundHandler) ListRequests(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	filter := service.BalanceRefundAdminFilter{
		Status:   strings.TrimSpace(c.Query("status")),
		Search:   strings.TrimSpace(c.Query("search")),
		Page:     page,
		PageSize: pageSize,
	}
	if uid, err := strconv.ParseInt(c.Query("user_id"), 10, 64); err == nil && uid > 0 {
		filter.UserID = uid
	}

	entries, total, err := h.refund.AdminList(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, entries, total, filter.Page, filter.PageSize)
}

// GetDetail returns one refund request plus the applicant's balance composition,
// so the admin can judge where the money came from before paying out.
// GET /api/v1/admin/refund-requests/:id
func (h *RefundHandler) GetDetail(c *gin.Context) {
	if _, ok := adminActor(c); !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	requestID, ok := parseRefundRequestID(c)
	if !ok {
		return
	}

	detail, err := h.refund.AdminDetail(c.Request.Context(), requestID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, detail)
}

// Complete marks a refund request as paid offline and settles the frozen balance.
// POST /api/v1/admin/refund-requests/:id/complete
func (h *RefundHandler) Complete(c *gin.Context) {
	adminID, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	requestID, ok := parseRefundRequestID(c)
	if !ok {
		return
	}

	// 备注是可选的：空 body 只算"没填"，不该因此报错。
	var req CompleteRefundRequestBody
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	entry, err := h.refund.AdminComplete(c.Request.Context(), requestID, adminID, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, entry)
}

// Reject marks a refund request as rejected with a mandatory reason.
// POST /api/v1/admin/refund-requests/:id/reject
func (h *RefundHandler) Reject(c *gin.Context) {
	adminID, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	requestID, ok := parseRefundRequestID(c)
	if !ok {
		return
	}

	var req RejectRefundRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	entry, err := h.refund.AdminReject(c.Request.Context(), requestID, adminID, req.Reason)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, entry)
}

// QR serves the applicant's payment QR code.
// GET /api/v1/admin/refund-requests/:id/qr
func (h *RefundHandler) QR(c *gin.Context) {
	adminID, ok := adminActor(c)
	if !ok {
		response.Unauthorized(c, "Admin not found in context")
		return
	}
	requestID, ok := parseRefundRequestID(c)
	if !ok {
		return
	}

	download, err := h.refund.OpenQR(c.Request.Context(), requestID, adminID, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	defer func() { _ = download.Body.Close() }()
	c.Header("Content-Type", download.ContentType)
	c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": download.Name}))
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, download.Size, download.ContentType, download.Body, nil)
}

func parseRefundRequestID(c *gin.Context) (int64, bool) {
	requestID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || requestID <= 0 {
		response.BadRequest(c, "Invalid refund request ID")
		return 0, false
	}
	return requestID, true
}
