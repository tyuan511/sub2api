package handler

import (
	"mime"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// RefundHandler 处理用户侧的余额退款申请：报价、提交、查看自己的申请。
type RefundHandler struct {
	refund *service.RefundService
}

func NewRefundHandler(refund *service.RefundService) *RefundHandler {
	return &RefundHandler{refund: refund}
}

// RefundQuoteResponse 是报价接口的响应：金额拆解 + 是否可提交 + 待处理申请。
type RefundQuoteResponse struct {
	*service.BalanceRefundQuote
	CanSubmit bool `json:"can_submit"`
}

// CreateRefundRequestBody 是提交申请的请求体。QRImage 允许
// "data:image/png;base64,..." 或裸 base64。
type CreateRefundRequestBody struct {
	QRImage string `json:"qr_image"`
	Note    string `json:"note"`
}

// GetQuote returns the refundable amount breakdown for the current user.
// GET /api/v1/user/refunds/quote
func (h *RefundHandler) GetQuote(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}

	quote, err := h.refund.GetQuote(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, RefundQuoteResponse{
		BalanceRefundQuote: quote,
		CanSubmit:          quote.PendingRequest == nil && !quote.BelowMinimum,
	})
}

// CreateRequest submits a refund request with the payment QR code.
// POST /api/v1/user/refund-requests
func (h *RefundHandler) CreateRequest(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}

	var req CreateRefundRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	data, err := service.DecodeRefundQRImage(req.QRImage)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	created, err := h.refund.CreateRequest(c.Request.Context(), subject.UserID, service.BalanceRefundCreateInput{
		QRData: data,
		Note:   req.Note,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, created)
}

// ListMyRequests returns the current user's refund requests, newest first.
// GET /api/v1/user/refund-requests
func (h *RefundHandler) ListMyRequests(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}

	page, pageSize := response.ParsePagination(c)
	requests, total, err := h.refund.ListMyRequests(c.Request.Context(), subject.UserID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, requests, total, page, pageSize)
}

// QR serves the uploaded payment QR code, owner-only.
// GET /api/v1/user/refund-requests/:id/qr
func (h *RefundHandler) QR(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	requestID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || requestID <= 0 {
		response.BadRequest(c, "Invalid request ID")
		return
	}

	download, err := h.refund.OpenQR(c.Request.Context(), requestID, subject.UserID, false)
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
