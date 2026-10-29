package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/balancerefundrequest"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	_ "golang.org/x/image/webp"
)

// 余额退款：用户在账户余额页发起申请，系统给出「余额 − 已到账返利，再扣手续费」
// 的可退金额，用户上传收款二维码；管理员线下打款后把申请改为「已完成」（同时清空
// 该用户余额），或「已拒绝 + 原因」。
const (
	// RefundQRMaxBytes 是收款二维码图片的体积上限（1MB，远小于工单附件）。
	RefundQRMaxBytes = 1 << 20
	// RefundQRMaxPixels 防止解压炸弹：二维码本身不会有 400 万像素。
	RefundQRMaxPixels = 4_000_000
	// RefundUserNoteMaxLen 是用户备注的最大字符数。
	RefundUserNoteMaxLen = 500
	// RefundRejectReasonMaxLen 是拒绝原因的最大字符数。
	RefundRejectReasonMaxLen = 1000
	// RefundAdminNoteMaxLen 是管理员完成备注的最大字符数。
	RefundAdminNoteMaxLen = 500
	// RefundMaxRequestBodyBytes 是提交申请的请求体上限：二维码 1MB，base64
	// 编码后约 1.37MB，再加富余。
	RefundMaxRequestBodyBytes = 2 << 20
	// base64EncodedMaxLen 是允许进入解码器的 base64 长度上限（1MB 的编码结果 + 富余）。
	base64EncodedMaxLen = ((RefundQRMaxBytes + 2) / 3) * 4
	// refundAdminSearchUserLimit 限制关键词检索命中的用户数上限。
	refundAdminSearchUserLimit = 200
)

var (
	ErrRefundUserNotFound = infraerrors.NotFound("REFUND_USER_NOT_FOUND", "user not found")
	ErrRefundNoBalance    = infraerrors.BadRequest("REFUND_NO_BALANCE", "no refundable balance")
	// ErrRefundBelowMin 表示可退金额没到起退点，不值得走一次人工打款。
	ErrRefundBelowMin        = infraerrors.BadRequest("REFUND_BELOW_MINIMUM", "refundable amount is below the minimum refund threshold")
	ErrRefundRequestExists   = infraerrors.Conflict("REFUND_REQUEST_EXISTS", "a pending refund request already exists")
	ErrRefundRequestNotFound = infraerrors.NotFound("REFUND_REQUEST_NOT_FOUND", "refund request not found")
	ErrRefundRequestReviewed = infraerrors.Conflict("REFUND_REQUEST_REVIEWED", "refund request has already been reviewed")
	ErrRefundQRRequired      = infraerrors.BadRequest("REFUND_QR_REQUIRED", "payment QR code image is required")
	ErrRefundQRInvalid       = infraerrors.BadRequest("REFUND_QR_INVALID", "payment QR code must be a JPEG, PNG or WebP image under 1MB")
	ErrRefundNoteTooLong     = infraerrors.BadRequest("REFUND_NOTE_TOO_LONG", "note is too long")
	ErrRefundReasonRequired  = infraerrors.BadRequest("REFUND_REASON_REQUIRED", "rejection reason is required")
	ErrRefundReasonTooLong   = infraerrors.BadRequest("REFUND_REASON_TOO_LONG", "rejection reason is too long")
	// ErrRefundAdminNoteTooLong 是管理员完成备注超长。
	ErrRefundAdminNoteTooLong = infraerrors.BadRequest("REFUND_ADMIN_NOTE_TOO_LONG", "admin note is too long")
	// ErrRefundConcurrentUpdate 表示提交瞬间余额已被并发计费改动，冻结条件更新没命中。
	ErrRefundConcurrentUpdate   = infraerrors.Conflict("REFUND_CONCURRENT_UPDATE", "balance changed while submitting the refund request, please retry")
	ErrRefundServiceUnavailable = infraerrors.ServiceUnavailable("REFUND_SERVICE_UNAVAILABLE", "refund service unavailable")
)

// BalanceRefundQuote 是用户提交前看到的金额拆解。
type BalanceRefundQuote struct {
	UserID           int64   `json:"user_id"`
	Balance          float64 `json:"balance"`
	RebateAmount     float64 `json:"rebate_amount"`
	FeeRatePercent   float64 `json:"fee_rate_percent"`
	FeeAmount        float64 `json:"fee_amount"`
	RefundableAmount float64 `json:"refundable_amount"`
	// RefundableBase 是可退基数（余额 − 已到账返利），便于前端核对 3% 是乘在哪笔钱上。
	RefundableBase float64 `json:"refundable_base"`
	// FrozenAmount 是当前待处理申请已经冻结的金额；没有待处理申请时为 0。
	FrozenAmount float64 `json:"frozen_amount"`
	// MinRefundAmount 是起退点；BelowMinimum 表示可退金额没够到它，不给提交。
	MinRefundAmount float64                   `json:"min_refund_amount"`
	BelowMinimum    bool                      `json:"below_minimum"`
	PendingRequest  *BalanceRefundRequestView `json:"pending_request,omitempty"`
}

// BalanceRefundRequestView 是一条退款申请的用户侧视图。
type BalanceRefundRequestView struct {
	ID      int64   `json:"id"`
	Balance float64 `json:"balance"`
	// FrozenAmount 是提交时从余额转入冻结的那笔钱，拒绝时原路解冻退回。
	FrozenAmount     float64 `json:"frozen_amount"`
	RebateAmount     float64 `json:"rebate_amount"`
	FeeRatePercent   float64 `json:"fee_rate_percent"`
	FeeAmount        float64 `json:"fee_amount"`
	RefundableAmount float64 `json:"refundable_amount"`
	Status           string  `json:"status"`
	UserNote         string  `json:"user_note"`
	RejectReason     string  `json:"reject_reason,omitempty"`
	// AdminNote 是管理员完成退款时填写的可选备注，对用户可见。
	AdminNote  string     `json:"admin_note,omitempty"`
	HasQRCode  bool       `json:"has_qr_code"`
	CreatedAt  time.Time  `json:"created_at"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
}

// AdminBalanceRefundDetail 是管理端单条申请的详情：列表字段 + 余额来源构成。
type AdminBalanceRefundDetail struct {
	AdminBalanceRefundEntry
	// Composition 是申请人的余额入账构成；查询失败时为 null，不影响申请本身展示。
	Composition *RefundBalanceComposition `json:"composition,omitempty"`
}

// AdminBalanceRefundEntry 在用户侧视图之上补齐管理员需要的身份信息与二维码元数据。
type AdminBalanceRefundEntry struct {
	BalanceRefundRequestView
	UserID        int64  `json:"user_id"`
	UserEmail     string `json:"user_email"`
	UserName      string `json:"user_name"`
	QRContentType string `json:"qr_content_type"`
	QRSize        int64  `json:"qr_size"`
	ReviewedBy    *int64 `json:"reviewed_by,omitempty"`
}

// BalanceRefundCreateInput 是提交申请的入参；QR 图片允许 data URL 或裸 base64。
type BalanceRefundCreateInput struct {
	QRData        []byte
	QRContentType string
	Note          string
}

// BalanceRefundAdminFilter 是管理端列表的筛选条件。
type BalanceRefundAdminFilter struct {
	Status   string
	Search   string
	UserID   int64
	Page     int
	PageSize int
}

// refundDB 抽象出 ent 客户端与事务客户端共用的原语 SQL 能力，
// 让冻结/解冻可以在同一个事务里与申请行的写入一起提交。
type refundDB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// RefundService 承载余额退款的报价、申请、冻结与审核链路。
//
// 资金口径：提交申请时把用户当时的整笔余额从 users.balance 转入 users.frozen_balance
// （审核期间不可消费）；拒绝则解冻原路退回余额；完成则结清冻结额（线下已打款），
// 申请之后新到账的钱不受影响。余额的每一次增减都会写一笔余额历史。
type RefundService struct {
	client               *dbent.Client
	userRepo             UserRepository
	settingService       *SettingService
	storage              SupportAttachmentStore
	redeemCodes          RedeemCodeRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCache         *BillingCacheService
}

func NewRefundService(
	client *dbent.Client,
	userRepo UserRepository,
	settingService *SettingService,
	storage SupportAttachmentStore,
	redeemCodes RedeemCodeRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	billingCache *BillingCacheService,
) *RefundService {
	return &RefundService{
		client:               client,
		userRepo:             userRepo,
		settingService:       settingService,
		storage:              storage,
		redeemCodes:          redeemCodes,
		authCacheInvalidator: authCacheInvalidator,
		billingCache:         billingCache,
	}
}

// GetQuote 计算当前用户的可退款金额，并带上他的待处理申请（如果有）。
func (s *RefundService) GetQuote(ctx context.Context, userID int64) (*BalanceRefundQuote, error) {
	if userID <= 0 {
		return nil, ErrRefundUserNotFound
	}
	userEntity, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrRefundUserNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}

	quote := s.buildQuote(ctx, userEntity.Balance, userID)
	pending, err := s.pendingRequest(ctx, userID)
	if err != nil {
		return nil, err
	}
	if pending != nil {
		view := toBalanceRefundRequestView(pending)
		quote.PendingRequest = &view
		// 待处理期间余额已冻结，报价按剩余可用余额给出，冻结额单独回显。
		quote.FrozenAmount = view.FrozenAmount
	}
	return quote, nil
}

// buildQuote 把「余额、已到账返利、费率」折算成金额四件套。
func (s *RefundService) buildQuote(ctx context.Context, balance float64, userID int64) *BalanceRefundQuote {
	return newBalanceRefundQuote(
		userID,
		balance,
		s.transferredRebateAmount(ctx, userID),
		s.settingService.GetRefundFeeRatePercent(ctx),
		s.settingService.GetRefundMinAmount(ctx),
	)
}

// newBalanceRefundQuote 是纯函数口径：先剔掉推广返利，再对本金收手续费，最后按起退点判定。
// 返利取到 2 位精度（与退款精度一致），避免 8 位零头把手续费基数顶出分叉。
func newBalanceRefundQuote(userID int64, balance, rebateAmount, feeRatePercent, minRefundAmount float64) *BalanceRefundQuote {
	rebate := roundTo(rebateAmount, 2)
	amount := payment.CalculateRefundAmount(balance, rebate, feeRatePercent)
	return &BalanceRefundQuote{
		UserID:           userID,
		Balance:          roundTo(balance, 8),
		RebateAmount:     rebate,
		FeeRatePercent:   feeRatePercent,
		FeeAmount:        amount.Fee,
		RefundableAmount: amount.Refundable,
		RefundableBase:   amount.Base,
		MinRefundAmount:  roundTo(minRefundAmount, 2),
		BelowMinimum:     amount.Refundable < minRefundAmount,
	}
}

// transferredRebateAmount 统计累计「返利转余额」进入余额的金额（8 位精度求和后
// 收敛到 2 位）。这部分是推广得来的钱，不参与退款。
func (s *RefundService) transferredRebateAmount(ctx context.Context, userID int64) float64 {
	if s.client == nil {
		return 0
	}
	return queryTransferredRebate(ctx, s.client, userID)
}

// queryTransferredRebate 在给定连接（普通客户端或事务客户端）上统计已到账返利，
// 让提交链路能在同一个事务里读到与冻结一致的数字。
func queryTransferredRebate(ctx context.Context, db refundDB, userID int64) float64 {
	rows, err := db.QueryContext(ctx, `
SELECT COALESCE(SUM(amount), 0)::double precision
FROM user_affiliate_ledger
WHERE user_id = $1
  AND action = 'transfer'`, userID)
	if err != nil {
		return 0
	}
	defer func() { _ = rows.Close() }()

	var total float64
	if rows.Next() {
		if err := rows.Scan(&total); err != nil {
			return 0
		}
	}
	if err := rows.Err(); err != nil {
		return 0
	}
	return roundTo(total, 2)
}

// CreateRequest 落一条待处理申请，并把用户当时的整笔余额转入冻结。
//
// 金额是提交时刻的快照：之后的费率、返利或新到账余额都不会改写这条记录，管理员
// 审核时看到的就是用户当时确认的那笔钱。冻结与落申请在同一个事务里完成，
// 不会出现「钱冻了但没有申请」或「申请了但钱还能花」。
func (s *RefundService) CreateRequest(ctx context.Context, userID int64, input BalanceRefundCreateInput) (*BalanceRefundRequestView, error) {
	if len(input.QRData) == 0 {
		return nil, ErrRefundQRRequired
	}
	note := strings.TrimSpace(input.Note)
	if len([]rune(note)) > RefundUserNoteMaxLen {
		return nil, ErrRefundNoteTooLong
	}

	// 先校验图片字节再开事务：不合法的载荷不该去抢行锁。
	prepared, err := prepareRefundQR(input.QRData)
	if err != nil {
		return nil, err
	}
	if err := s.storage.Put(ctx, prepared.key, prepared.contentType, prepared.data); err != nil {
		return nil, fmt.Errorf("store refund qr code: %w", err)
	}

	var created *dbent.BalanceRefundRequest
	err = s.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		balance, err := lockUserBalance(txCtx, txClient, userID)
		if err != nil {
			return err
		}
		if _, err := txClient.BalanceRefundRequest.Query().
			Where(
				balancerefundrequest.UserIDEQ(userID),
				balancerefundrequest.StatusEQ(BalanceRefundStatusPending),
			).
			First(txCtx); err == nil {
			return ErrRefundRequestExists
		} else if !dbent.IsNotFound(err) {
			return fmt.Errorf("check pending refund request: %w", err)
		}

		quote := newBalanceRefundQuote(
			userID,
			balance,
			queryTransferredRebate(txCtx, txClient, userID),
			s.settingService.GetRefundFeeRatePercent(txCtx),
			s.settingService.GetRefundMinAmount(txCtx),
		)
		if quote.RefundableAmount <= 0 {
			return ErrRefundNoBalance
		}
		// 起退点在服务端再判一次：前端隐藏入口只是体验，不能当成约束。
		if quote.BelowMinimum {
			return ErrRefundBelowMin
		}
		if err := freezeUserBalance(txCtx, txClient, userID, balance); err != nil {
			return err
		}

		record, err := txClient.BalanceRefundRequest.Create().
			SetUserID(userID).
			SetBalance(balance).
			SetFrozenAmount(balance).
			SetRebateAmount(quote.RebateAmount).
			SetFeeRatePercent(quote.FeeRatePercent).
			SetFeeAmount(quote.FeeAmount).
			SetRefundableAmount(quote.RefundableAmount).
			SetQrStorageKey(prepared.key).
			SetQrContentType(prepared.contentType).
			SetQrSize(int64(len(prepared.data))).
			SetStatus(BalanceRefundStatusPending).
			SetUserNote(note).
			Save(txCtx)
		if err != nil {
			// 唯一索引兜底：并发提交时同一用户只会留下一笔待处理申请。
			if isRefundPendingUniqueViolation(err) {
				return ErrRefundRequestExists
			}
			return fmt.Errorf("create refund request: %w", err)
		}
		created = record
		return nil
	})
	if err != nil {
		// 事务回滚时冻结一并撤销，这里只需要回收已上传的图片对象。
		_ = s.storage.Delete(ctx, prepared.key)
		if errors.Is(err, ErrRefundRequestExists) ||
			errors.Is(err, ErrRefundNoBalance) ||
			errors.Is(err, ErrRefundBelowMin) ||
			errors.Is(err, ErrRefundConcurrentUpdate) {
			return nil, err
		}
		return nil, err
	}

	// 余额减少：鉴权缓存与计费缓存必须失效，否则网关还会按旧余额放行请求。
	s.invalidateBalanceCaches(ctx, userID)
	s.recordBalanceHistory(ctx, userID, -created.FrozenAmount,
		fmt.Sprintf("余额退款申请 #%d 提交，冻结余额", created.ID))

	view := toBalanceRefundRequestView(created)
	return &view, nil
}

// ListMyRequests 返回当前用户的退款申请（新的在前）。
func (s *RefundService) ListMyRequests(ctx context.Context, userID int64, page, pageSize int) ([]BalanceRefundRequestView, int64, error) {
	ps, pg := applyPagination(pageSize, page)
	total, err := s.client.BalanceRefundRequest.Query().
		Where(balancerefundrequest.UserIDEQ(userID)).
		Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count refund requests: %w", err)
	}
	records, err := s.client.BalanceRefundRequest.Query().
		Where(balancerefundrequest.UserIDEQ(userID)).
		Order(dbent.Desc(balancerefundrequest.FieldCreatedAt)).
		Limit(ps).Offset((pg - 1) * ps).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list refund requests: %w", err)
	}
	views := make([]BalanceRefundRequestView, 0, len(records))
	for _, record := range records {
		views = append(views, toBalanceRefundRequestView(record))
	}
	return views, int64(total), nil
}

// OpenQR 打开申请里的收款二维码。用户只能读自己的，管理员可读全部。
func (s *RefundService) OpenQR(ctx context.Context, requestID, actorID int64, isAdmin bool) (*SupportAttachmentDownload, error) {
	record, err := s.client.BalanceRefundRequest.Get(ctx, requestID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrRefundRequestNotFound
		}
		return nil, fmt.Errorf("get refund request: %w", err)
	}
	if !isAdmin && record.UserID != actorID {
		return nil, ErrRefundRequestNotFound
	}
	body, err := s.storage.Open(ctx, record.QrStorageKey)
	if err != nil {
		return nil, fmt.Errorf("open refund qr code: %w", err)
	}
	return &SupportAttachmentDownload{
		Body:        body,
		Name:        fmt.Sprintf("refund-qr-%d", record.ID),
		ContentType: record.QrContentType,
		Size:        record.QrSize,
	}, nil
}

// AdminList 返回管理端的退款申请列表，附带用户邮箱/用户名。
func (s *RefundService) AdminList(ctx context.Context, filter BalanceRefundAdminFilter) ([]AdminBalanceRefundEntry, int64, error) {
	ps, pg := applyPagination(filter.PageSize, filter.Page)

	query := s.client.BalanceRefundRequest.Query()
	if status := strings.TrimSpace(filter.Status); status != "" {
		if !isValidRefundStatus(status) {
			return nil, 0, infraerrors.BadRequest("REFUND_STATUS_INVALID", "invalid refund request status")
		}
		query = query.Where(balancerefundrequest.StatusEQ(status))
	}
	if filter.UserID > 0 {
		query = query.Where(balancerefundrequest.UserIDEQ(filter.UserID))
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		userIDs, err := s.searchUserIDs(ctx, search)
		if err != nil {
			return nil, 0, err
		}
		if len(userIDs) == 0 {
			return []AdminBalanceRefundEntry{}, 0, nil
		}
		query = query.Where(balancerefundrequest.UserIDIn(userIDs...))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count admin refund requests: %w", err)
	}
	records, err := query.
		Order(dbent.Desc(balancerefundrequest.FieldCreatedAt)).
		Limit(ps).Offset((pg - 1) * ps).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list admin refund requests: %w", err)
	}

	profiles := s.userProfiles(ctx, records)
	entries := make([]AdminBalanceRefundEntry, 0, len(records))
	for _, record := range records {
		entry := AdminBalanceRefundEntry{
			BalanceRefundRequestView: toBalanceRefundRequestView(record),
			UserID:                   record.UserID,
			QRContentType:            record.QrContentType,
			QRSize:                   record.QrSize,
			ReviewedBy:               record.ReviewedBy,
		}
		if profile, ok := profiles[record.UserID]; ok {
			entry.UserEmail = profile.email
			entry.UserName = profile.name
		}
		entries = append(entries, entry)
	}
	return entries, int64(total), nil
}

// AdminComplete 把申请标记为「已完成」：管理员已按收款二维码在站外打款，
// 因此结清提交时冻结的那笔钱（随退款离开系统）。申请之后新到账的余额不受影响。
//
// note 可选（打款流水号、渠道单号等），与拒绝原因一样对用户可见。
//
// 状态翻转与冻结结清在同一个事务里完成：重复调用只会有一次生效，另一次返回 409，
// 不会出现「打了两次款」或「结了冻但状态没变」。
func (s *RefundService) AdminComplete(ctx context.Context, requestID, adminID int64, note string) (*AdminBalanceRefundEntry, error) {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > RefundAdminNoteMaxLen {
		return nil, ErrRefundAdminNoteTooLong
	}
	var userID int64
	var frozen float64
	if err := s.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		updated, err := txClient.BalanceRefundRequest.Update().
			Where(
				balancerefundrequest.IDEQ(requestID),
				balancerefundrequest.StatusEQ(BalanceRefundStatusPending),
			).
			SetStatus(BalanceRefundStatusCompleted).
			SetReviewedBy(adminID).
			SetReviewedAt(time.Now()).
			SetAdminNote(note).
			ClearRejectReason().
			Save(txCtx)
		if err != nil {
			return fmt.Errorf("complete refund request: %w", err)
		}
		if updated == 0 {
			return ErrRefundRequestReviewed
		}
		record, err := txClient.BalanceRefundRequest.Get(txCtx, requestID)
		if err != nil {
			return fmt.Errorf("load completed refund request: %w", err)
		}
		if err := settleFrozenBalance(txCtx, txClient, record.UserID, record.FrozenAmount); err != nil {
			return err
		}
		userID, frozen = record.UserID, record.FrozenAmount
		return nil
	}); err != nil {
		return nil, err
	}

	// 冻结额已随退款结清；余额本身没变，但缓存里可能还带着旧的冻结口径。
	s.invalidateBalanceCaches(ctx, userID)
	_ = frozen
	return s.adminEntry(ctx, requestID)
}

// AdminReject 拒绝申请：把提交时冻结的余额解冻退回，用户可以继续消费或改好
// 二维码后重新提交。状态翻转与解冻同事务。
func (s *RefundService) AdminReject(ctx context.Context, requestID, adminID int64, reason string) (*AdminBalanceRefundEntry, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, ErrRefundReasonRequired
	}
	if len([]rune(reason)) > RefundRejectReasonMaxLen {
		return nil, ErrRefundReasonTooLong
	}

	var userID int64
	var frozen float64
	if err := s.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		updated, err := txClient.BalanceRefundRequest.Update().
			Where(
				balancerefundrequest.IDEQ(requestID),
				balancerefundrequest.StatusEQ(BalanceRefundStatusPending),
			).
			SetStatus(BalanceRefundStatusRejected).
			SetRejectReason(reason).
			SetReviewedBy(adminID).
			SetReviewedAt(time.Now()).
			Save(txCtx)
		if err != nil {
			return fmt.Errorf("reject refund request: %w", err)
		}
		if updated == 0 {
			return ErrRefundRequestReviewed
		}
		record, err := txClient.BalanceRefundRequest.Get(txCtx, requestID)
		if err != nil {
			return fmt.Errorf("load rejected refund request: %w", err)
		}
		if err := unfreezeUserBalance(txCtx, txClient, record.UserID, record.FrozenAmount); err != nil {
			return err
		}
		userID, frozen = record.UserID, record.FrozenAmount
		return nil
	}); err != nil {
		return nil, err
	}

	s.invalidateBalanceCaches(ctx, userID)
	s.recordBalanceHistory(ctx, userID, frozen,
		fmt.Sprintf("余额退款申请 #%d 已拒绝，解冻退回余额", requestID))
	return s.adminEntry(ctx, requestID)
}

// withTx 在一个 ent 事务里执行 fn；外层已有事务时直接复用，避免嵌套提交。
func (s *RefundService) withTx(ctx context.Context, fn func(txCtx context.Context, txClient *dbent.Client) error) error {
	if s.client == nil {
		return ErrRefundServiceUnavailable
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin refund transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := fn(txCtx, tx.Client()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit refund transaction: %w", err)
	}
	return nil
}

// lockUserBalance 在事务内读取并锁住用户余额，作为冻结金额的唯一可信来源。
func lockUserBalance(ctx context.Context, db refundDB, userID int64) (float64, error) {
	rows, err := db.QueryContext(ctx, `
SELECT balance::double precision
FROM users
WHERE id = $1
  AND deleted_at IS NULL
FOR UPDATE`, userID)
	if err != nil {
		return 0, fmt.Errorf("lock user balance: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, ErrRefundUserNotFound
	}
	var balance float64
	if err := rows.Scan(&balance); err != nil {
		return 0, fmt.Errorf("scan user balance: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read user balance: %w", err)
	}
	return balance, nil
}

// freezeUserBalance 把整笔余额从 balance 移入 frozen_balance。
// 条件更新带上刚读到的余额值：并发计费改过数字时这里就会失配，宁可让用户重试，
// 也不能按旧快照多冻一笔。
func freezeUserBalance(ctx context.Context, db refundDB, userID int64, amount float64) error {
	res, err := db.ExecContext(ctx, `
UPDATE users
SET balance = 0,
    frozen_balance = COALESCE(frozen_balance, 0) + $2,
    updated_at = NOW()
WHERE id = $1
  AND balance = $2
  AND deleted_at IS NULL`, userID, amount)
	if err != nil {
		return fmt.Errorf("freeze refund balance: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("freeze refund balance result: %w", err)
	}
	if affected != 1 {
		return ErrRefundConcurrentUpdate
	}
	return nil
}

// unfreezeUserBalance 把冻结额解冻退回余额。只退回实际还冻结着的部分，
// 避免有人手工改过 frozen_balance 时凭空造钱。
func unfreezeUserBalance(ctx context.Context, db refundDB, userID int64, amount float64) error {
	if amount <= 0 {
		return nil
	}
	res, err := db.ExecContext(ctx, `
UPDATE users
SET balance = balance + LEAST($2, COALESCE(frozen_balance, 0)),
    frozen_balance = GREATEST(COALESCE(frozen_balance, 0) - $2, 0),
    updated_at = NOW()
WHERE id = $1
  AND deleted_at IS NULL`, userID, amount)
	if err != nil {
		return fmt.Errorf("unfreeze refund balance: %w", err)
	}
	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("unfreeze refund balance result: %w", err)
	}
	return nil
}

// settleFrozenBalance 结清冻结额：钱已在站外退给用户，冻结部分随之离开账户。
func settleFrozenBalance(ctx context.Context, db refundDB, userID int64, amount float64) error {
	if amount <= 0 {
		return nil
	}
	res, err := db.ExecContext(ctx, `
UPDATE users
SET frozen_balance = GREATEST(COALESCE(frozen_balance, 0) - $2, 0),
    updated_at = NOW()
WHERE id = $1
  AND deleted_at IS NULL`, userID, amount)
	if err != nil {
		return fmt.Errorf("settle refund freeze: %w", err)
	}
	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("settle refund freeze result: %w", err)
	}
	return nil
}

// recordBalanceHistory 写一笔余额流水，让冻结与解冻出现在用户的余额历史里，
// 和退款申请单对得上账。类型用 balance_refund 而不是 admin_balance，避免
// 退款流水混进"管理员调整"里，把余额来源分析带偏。
func (s *RefundService) recordBalanceHistory(ctx context.Context, userID int64, amount float64, notes string) {

	if s.redeemCodes == nil || amount == 0 {
		return
	}
	code, err := GenerateRedeemCode()
	if err != nil {
		logger.LegacyPrintf("service.refund", "failed to generate refund history code: %v", err)
		return
	}
	owner := userID
	now := time.Now()
	record := &RedeemCode{
		Code:   code,
		Type:   AdjustmentTypeBalanceRefund,
		Value:  roundTo(amount, 8),
		Status: StatusUsed,
		UsedBy: &owner,
		Notes:  notes,
		UsedAt: &now,
	}
	if err := s.redeemCodes.Create(ctx, record); err != nil {
		logger.LegacyPrintf("service.refund", "failed to record refund balance history: user_id=%d err=%v", userID, err)
	}
}

// invalidateBalanceCaches 让网关与计费缓存立即看到新的可用余额。
func (s *RefundService) invalidateBalanceCaches(ctx context.Context, userID int64) {
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCache == nil {
		return
	}
	cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.billingCache.InvalidateUserBalance(cacheCtx, userID); err != nil {
		logger.LegacyPrintf("service.refund", "invalidate user balance cache failed: user_id=%d err=%v", userID, err)
	}
}

func (s *RefundService) adminEntry(ctx context.Context, requestID int64) (*AdminBalanceRefundEntry, error) {
	record, err := s.client.BalanceRefundRequest.Get(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("reload refund request: %w", err)
	}
	entry := AdminBalanceRefundEntry{
		BalanceRefundRequestView: toBalanceRefundRequestView(record),
		UserID:                   record.UserID,
		QRContentType:            record.QrContentType,
		QRSize:                   record.QrSize,
		ReviewedBy:               record.ReviewedBy,
	}
	if profile, err := s.userRepo.GetByIDIncludeDeleted(ctx, record.UserID); err == nil && profile != nil {
		entry.UserEmail = profile.Email
		entry.UserName = profile.Username
	}
	return &entry, nil
}

func (s *RefundService) pendingRequest(ctx context.Context, userID int64) (*dbent.BalanceRefundRequest, error) {
	record, err := s.client.BalanceRefundRequest.Query().
		Where(
			balancerefundrequest.UserIDEQ(userID),
			balancerefundrequest.StatusEQ(BalanceRefundStatusPending),
		).
		Order(dbent.Desc(balancerefundrequest.FieldCreatedAt)).
		First(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("query pending refund request: %w", err)
	}
	return record, nil
}

func (s *RefundService) searchUserIDs(ctx context.Context, search string) ([]int64, error) {
	records, err := s.client.User.Query().
		Where(user.Or(
			user.EmailContainsFold(search),
			user.UsernameContainsFold(search),
		)).
		Limit(refundAdminSearchUserLimit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("search users for refund list: %w", err)
	}
	ids := make([]int64, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids, nil
}

func (s *RefundService) userProfiles(ctx context.Context, records []*dbent.BalanceRefundRequest) map[int64]refundUserProfile {
	ids := make([]int64, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.UserID)
	}
	profiles := make(map[int64]refundUserProfile, len(ids))
	if len(ids) == 0 {
		return profiles
	}
	users, err := s.client.User.Query().
		Where(user.IDIn(ids...)).
		All(ctx)
	if err != nil {
		return profiles
	}
	for _, record := range users {
		profiles[record.ID] = refundUserProfile{email: record.Email, name: record.Username}
	}
	return profiles
}

type refundUserProfile struct {
	email string
	name  string
}

func toBalanceRefundRequestView(record *dbent.BalanceRefundRequest) BalanceRefundRequestView {
	view := BalanceRefundRequestView{
		ID:               record.ID,
		Balance:          roundTo(record.Balance, 8),
		FrozenAmount:     roundTo(record.FrozenAmount, 2),
		RebateAmount:     roundTo(record.RebateAmount, 2),
		FeeRatePercent:   record.FeeRatePercent,
		FeeAmount:        roundTo(record.FeeAmount, 2),
		RefundableAmount: roundTo(record.RefundableAmount, 2),
		Status:           record.Status,
		UserNote:         record.UserNote,
		AdminNote:        record.AdminNote,
		HasQRCode:        record.QrStorageKey != "",
		CreatedAt:        record.CreatedAt,
	}
	if record.RejectReason != nil {
		view.RejectReason = *record.RejectReason
	}
	if record.ReviewedAt != nil {
		reviewedAt := *record.ReviewedAt
		view.ReviewedAt = &reviewedAt
	}
	return view
}

func isValidRefundStatus(status string) bool {
	switch status {
	case BalanceRefundStatusPending, BalanceRefundStatusCompleted, BalanceRefundStatusRejected:
		return true
	default:
		return false
	}
}

func isRefundPendingUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "balance_refund_requests_pending_uq")
}

// DecodeRefundQRImage 接受 "data:image/png;base64,..." 或裸 base64（含 URL-safe
// 变体），统一还原成图片字节。类型与体积判定只看字节内容，不信任客户端声明的 MIME。
func DecodeRefundQRImage(raw string) ([]byte, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, ErrRefundQRRequired
	}
	if strings.HasPrefix(value, "data:") {
		idx := strings.Index(value, ",")
		if idx < 0 {
			return nil, ErrRefundQRInvalid
		}
		value = value[idx+1:]
	}
	if len(value) > base64EncodedMaxLen {
		return nil, ErrRefundQRInvalid
	}
	normalized := strings.NewReplacer("\n", "", "\r", "", " ", "", "-", "+", "_", "/").Replace(value)
	data, err := base64.StdEncoding.DecodeString(normalized)
	if err != nil {
		return nil, ErrRefundQRInvalid
	}
	return data, nil
}

// preparedRefundQR 是校验通过、等待落盘的二维码。
type preparedRefundQR struct {
	key         string
	contentType string
	data        []byte
}

// prepareRefundQR 校验图片内容与体积，并生成隔离的存储 key。
// 只接受 JPEG / PNG / WebP（http.DetectContentType 的判定，不信任客户端声明）。
func prepareRefundQR(data []byte) (*preparedRefundQR, error) {
	if len(data) == 0 || len(data) > RefundQRMaxBytes {
		return nil, ErrRefundQRInvalid
	}
	detected := http.DetectContentType(data)
	switch detected {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, ErrRefundQRInvalid
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > RefundQRMaxPixels {
		return nil, ErrRefundQRInvalid
	}

	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return nil, fmt.Errorf("generate refund qr key: %w", err)
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[detected]
	return &preparedRefundQR{
		key:         fmt.Sprintf("refund/%s/%s%s", time.Now().UTC().Format("2006/01/02"), hex.EncodeToString(random), ext),
		contentType: detected,
		data:        data,
	}, nil
}

// RefundBalanceComposition 是一个用户的余额来源构成。
//
// 用途是管理员审核退款时的决策依据：钱在余额里是流动的，线下代付、对公转账
// 这类入账在系统里没有支付订单，所以不能拿"线上实付"硬封顶可退金额（会误伤
// 真付过钱的人）。这里把能归因的入账分门别类摊开，让管理员自己判断该退不该退。
type RefundBalanceComposition struct {
	// PaidOnline 是线上支付净额：订单到账额剔除赠送额度、再扣掉已退金额。
	PaidOnline float64 `json:"paid_online"`
	// RedeemCodes 是卡密兑换入账累计（只算已使用的）。
	RedeemCodes float64 `json:"redeem_codes"`
	// AdminAdjusted 是管理员手动加减的净额（不含退款冻结/解冻，那部分单独类型）。
	AdminAdjusted float64 `json:"admin_adjusted"`
	// PromoBonus 是优惠码赠送累计。
	PromoBonus float64 `json:"promo_bonus"`
	// RebateTransferred 是推广返利转入余额的累计（也就是退款时剔除的那笔）。
	RebateTransferred float64 `json:"rebate_transferred"`
	// AttributedTotal 是上面几项的合计。
	AttributedTotal float64 `json:"attributed_total"`
	// CurrentBalance / FrozenBalance 是此刻的账户余额与冻结额。
	CurrentBalance float64 `json:"current_balance"`
	FrozenBalance  float64 `json:"frozen_balance"`
	// ReferenceBalance 是判定"无法归因"所用的账户规模：审核某条申请时取该申请的
	// 余额快照，其余场景取实时余额+冻结额。
	ReferenceBalance float64 `json:"reference_balance"`
	// Unattributed 是账户规模超出可归因入账的部分：第三方首登赠送、直接改库等
	// 没有流水可查的钱都会落到这里，非零本身就是一条信号。
	Unattributed float64 `json:"unattributed"`
}

// balanceCompositionSQL 一次取回全部来源，避免管理员打开一条申请就打五六次库。
const balanceCompositionSQL = `
SELECT
  COALESCE((SELECT sum(po.amount - po.bonus_amount - po.refund_amount)
            FROM payment_orders po
            WHERE po.user_id = $1
              AND po.order_type = 'balance'
              AND po.status IN ('COMPLETED', 'PARTIALLY_REFUNDED', 'REFUNDED',
                                'REFUND_REQUESTED', 'REFUND_PENDING', 'REFUNDING', 'REFUND_FAILED')), 0),
  COALESCE((SELECT sum(rc.value) FROM redeem_codes rc
            WHERE rc.used_by = $1 AND rc.type = 'balance' AND rc.status = 'used'), 0),
  COALESCE((SELECT sum(rc.value) FROM redeem_codes rc
            WHERE rc.used_by = $1 AND rc.type = 'admin_balance'), 0),
  COALESCE((SELECT sum(pu.bonus_amount) FROM promo_code_usages pu
            WHERE pu.user_id = $1), 0),
  COALESCE((SELECT sum(ual.amount) FROM user_affiliate_ledger ual
            WHERE ual.user_id = $1 AND ual.action = 'transfer'), 0),
  COALESCE((SELECT u.balance FROM users u WHERE u.id = $1), 0),
  COALESCE((SELECT u.frozen_balance FROM users u WHERE u.id = $1), 0)`

// buildBalanceComposition 把原始入账数字折算成构成视图。
//
// 纯函数，便于单测。referenceBalance 是判定"无法归因"的账户规模：
// 无法归因 = max(0, referenceBalance − 可归因合计)，一律保留 2 位小数。
func buildBalanceComposition(paidOnline, redeemCodes, adminAdjusted, promoBonus, rebateTransferred, balance, frozen float64, referenceBalance ...float64) *RefundBalanceComposition {
	attributed := roundTo(paidOnline, 2) + roundTo(redeemCodes, 2) + roundTo(adminAdjusted, 2) +
		roundTo(promoBonus, 2) + roundTo(rebateTransferred, 2)
	reference := roundTo(balance, 2) + roundTo(frozen, 2)
	if len(referenceBalance) > 0 {
		reference = roundTo(referenceBalance[0], 2)
	}
	unattributed := reference - roundTo(attributed, 2)
	if unattributed < 0 {
		unattributed = 0
	}
	return &RefundBalanceComposition{
		PaidOnline:        roundTo(paidOnline, 2),
		RedeemCodes:       roundTo(redeemCodes, 2),
		AdminAdjusted:     roundTo(adminAdjusted, 2),
		PromoBonus:        roundTo(promoBonus, 2),
		RebateTransferred: roundTo(rebateTransferred, 2),
		AttributedTotal:   roundTo(attributed, 2),
		CurrentBalance:    roundTo(balance, 2),
		FrozenBalance:     roundTo(frozen, 2),
		ReferenceBalance:  reference,
		Unattributed:      roundTo(unattributed, 2),
	}
}

// BalanceComposition 查询某个用户的余额来源构成（账户规模取实时余额+冻结额）。
func (s *RefundService) BalanceComposition(ctx context.Context, userID int64) (*RefundBalanceComposition, error) {
	return s.balanceComposition(ctx, userID, nil)
}

// balanceComposition 的 referenceBalance 非空时，用它判定"无法归因"。
func (s *RefundService) balanceComposition(ctx context.Context, userID int64, referenceBalance *float64) (*RefundBalanceComposition, error) {
	if s.client == nil || userID <= 0 {
		return nil, ErrRefundUserNotFound
	}
	rows, err := s.client.QueryContext(ctx, balanceCompositionSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("query balance composition: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil, ErrRefundUserNotFound
	}
	var paidOnline, redeemCodes, adminAdjusted, promoBonus, rebate, balance, frozen float64
	if err := rows.Scan(&paidOnline, &redeemCodes, &adminAdjusted, &promoBonus, &rebate, &balance, &frozen); err != nil {
		return nil, fmt.Errorf("scan balance composition: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read balance composition: %w", err)
	}
	if referenceBalance != nil {
		return buildBalanceComposition(paidOnline, redeemCodes, adminAdjusted, promoBonus, rebate, balance, frozen, *referenceBalance), nil
	}
	return buildBalanceComposition(paidOnline, redeemCodes, adminAdjusted, promoBonus, rebate, balance, frozen), nil
}

// AdminDetail 返回单条申请的完整信息 + 申请人的余额来源构成。
func (s *RefundService) AdminDetail(ctx context.Context, requestID int64) (*AdminBalanceRefundDetail, error) {
	entry, err := s.adminEntry(ctx, requestID)
	if err != nil {
		return nil, err
	}
	// 用申请单的余额快照判定"无法归因"：冻结之后实时余额是 0，
	// 拿实时余额算会让这条信号在管理员最需要看的时候恰好消失。
	reference := entry.Balance
	composition, err := s.balanceComposition(ctx, entry.UserID, &reference)
	if err != nil {
		// 构成只是辅助信息，查不到也不该让管理员看不到申请本身。
		composition = nil
	}
	return &AdminBalanceRefundDetail{
		AdminBalanceRefundEntry: *entry,
		Composition:             composition,
	}, nil
}
