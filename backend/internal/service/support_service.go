package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/admintelegrambinding"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/supportticket"
	"github.com/Wei-Shaw/sub2api/ent/supportticketattachment"
	"github.com/Wei-Shaw/sub2api/ent/supportticketmessage"
	"github.com/Wei-Shaw/sub2api/ent/supportticketread"
	"github.com/Wei-Shaw/sub2api/ent/user"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/redis/go-redis/v9"
	_ "golang.org/x/image/webp"
)

const (
	SupportMaxAttachmentBytes  = 3 << 20
	SupportMaxAttachments      = 2
	SupportMaxRequestBodyBytes = 7 << 20
	supportMaxPixels           = 20_000_000
	SupportAdminChannel        = "support:realtime:admins"
)

type SupportService struct {
	client  *ent.Client
	redis   *redis.Client
	storage SupportAttachmentStore
}

func NewSupportService(client *ent.Client, redisClient *redis.Client, storage SupportAttachmentStore) *SupportService {
	return &SupportService{client: client, redis: redisClient, storage: storage}
}

type preparedSupportAttachment struct {
	SupportUpload
	key    string
	size   int64
	width  int
	height int
	sha256 string
}

func validateSupportCreate(input *SupportCreateInput) error {
	input.Content = strings.TrimSpace(input.Content)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.Content == "" || len([]rune(input.Content)) > 10000 {
		return infraerrors.BadRequest("SUPPORT_CONTENT_INVALID", "content is required and must not exceed 10000 characters")
	}
	if len(input.ClientRequestID) > 64 {
		return infraerrors.BadRequest("SUPPORT_REQUEST_ID_INVALID", "client_request_id is too long")
	}
	return nil
}

func validateSupportReply(input *SupportReplyInput) error {
	input.Content = strings.TrimSpace(input.Content)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.Content == "" && len(input.Attachments) == 0 {
		return infraerrors.BadRequest("SUPPORT_REPLY_EMPTY", "reply content or an attachment is required")
	}
	if len([]rune(input.Content)) > 10000 {
		return infraerrors.BadRequest("SUPPORT_CONTENT_INVALID", "content must not exceed 10000 characters")
	}
	if len(input.ClientRequestID) > 64 {
		return infraerrors.BadRequest("SUPPORT_REQUEST_ID_INVALID", "client_request_id is too long")
	}
	return nil
}

func inspectSupportAttachments(uploads []SupportUpload) ([]preparedSupportAttachment, error) {
	if len(uploads) > SupportMaxAttachments {
		return nil, infraerrors.BadRequest("SUPPORT_TOO_MANY_ATTACHMENTS", "at most 2 images can be attached")
	}
	prepared := make([]preparedSupportAttachment, 0, len(uploads))
	for _, upload := range uploads {
		if len(upload.Data) == 0 || len(upload.Data) > SupportMaxAttachmentBytes {
			return nil, infraerrors.BadRequest("SUPPORT_ATTACHMENT_SIZE", "each image must be no larger than 3 MB")
		}
		detected := http.DetectContentType(upload.Data)
		if detected != "image/jpeg" && detected != "image/png" && detected != "image/webp" {
			return nil, infraerrors.BadRequest("SUPPORT_ATTACHMENT_TYPE", "only JPEG, PNG and WebP images are supported")
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(upload.Data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > supportMaxPixels {
			return nil, infraerrors.BadRequest("SUPPORT_ATTACHMENT_INVALID", "invalid image or image dimensions are too large")
		}
		sum := sha256.Sum256(upload.Data)
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return nil, err
		}
		ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[detected]
		key := fmt.Sprintf("support/%s/%s%s", time.Now().UTC().Format("2006/01/02"), hex.EncodeToString(random), ext)
		name := strings.TrimSpace(filepath.Base(upload.Name))
		if name == "" || name == "." {
			name = "image" + ext
		}
		if len(name) > 255 {
			name = name[:255]
		}
		prepared = append(prepared, preparedSupportAttachment{SupportUpload: SupportUpload{
			Name: name, ContentType: detected, Data: upload.Data,
		}, key: key, size: int64(len(upload.Data)), width: cfg.Width, height: cfg.Height, sha256: hex.EncodeToString(sum[:])})
	}
	return prepared, nil
}

func (s *SupportService) storePreparedAttachments(ctx context.Context, items []preparedSupportAttachment) error {
	stored := make([]preparedSupportAttachment, 0, len(items))
	for i := range items {
		item := &items[i]
		if err := s.storage.Put(ctx, item.key, item.ContentType, item.Data); err != nil {
			s.cleanupPrepared(ctx, stored)
			return fmt.Errorf("store support attachment: %w", err)
		}
		stored = append(stored, *item)
		item.Data = nil
	}
	return nil
}

func supportAttachmentBytes(items []preparedSupportAttachment) int64 {
	var total int64
	for _, item := range items {
		total += item.size
	}
	return total
}

func (s *SupportService) cleanupPrepared(ctx context.Context, items []preparedSupportAttachment) {
	for _, item := range items {
		_ = s.storage.Delete(ctx, item.key)
	}
}

func supportTicketNo() (string, error) {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "T" + time.Now().UTC().Format("20060102") + "-" + strings.ToUpper(hex.EncodeToString(b)), nil
}

func (s *SupportService) Create(ctx context.Context, userID int64, input SupportCreateInput) (*SupportTicket, error) {
	if err := validateSupportCreate(&input); err != nil {
		return nil, err
	}
	if input.ClientRequestID != "" {
		existing, err := s.client.SupportTicketMessage.Query().Where(
			supportticketmessage.SenderIDEQ(userID), supportticketmessage.ClientRequestIDEQ(input.ClientRequestID),
		).Only(ctx)
		if err == nil {
			return s.Get(ctx, userID, existing.TicketID, false)
		}
		if err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
	}
	existingTicket, err := s.client.SupportTicket.Query().Where(supportticket.UserIDEQ(userID)).Only(ctx)
	if err == nil {
		return s.Reply(ctx, userID, existingTicket.ID, false, SupportReplyInput{
			Content: input.Content, ClientRequestID: input.ClientRequestID, Attachments: input.Attachments,
		})
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	prepared, err := inspectSupportAttachments(input.Attachments)
	if err != nil {
		return nil, err
	}
	if err := s.enforceUserSendQuota(ctx, userID, supportAttachmentBytes(prepared)); err != nil {
		return nil, err
	}
	if err := s.storePreparedAttachments(ctx, prepared); err != nil {
		return nil, err
	}
	keepObjects := false
	defer func() {
		if !keepObjects {
			s.cleanupPrepared(context.Background(), prepared)
		}
	}()

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	rollback := func(cause error) (*SupportTicket, error) { _ = tx.Rollback(); return nil, cause }
	number, err := supportTicketNo()
	if err != nil {
		return rollback(err)
	}
	now := time.Now()
	ticketEntity, err := tx.SupportTicket.Create().
		SetTicketNo(number).SetUserID(userID).SetTitle("联系客服").SetCategory("other").
		SetStatus(SupportStatusOpen).SetPriority(SupportPriorityNormal).SetLastMessageAt(now).Save(ctx)
	if err != nil {
		return rollback(err)
	}
	messageCreate := tx.SupportTicketMessage.Create().SetTicketID(ticketEntity.ID).SetSenderID(userID).
		SetSenderRole("user").SetKind(SupportMessagePublic).SetContent(input.Content).SetCreatedAt(now)
	if input.ClientRequestID != "" {
		messageCreate.SetClientRequestID(input.ClientRequestID)
	}
	messageEntity, err := messageCreate.Save(ctx)
	if err != nil {
		return rollback(err)
	}
	if err := s.createAttachmentRows(ctx, tx, ticketEntity.ID, messageEntity.ID, userID, prepared); err != nil {
		return rollback(err)
	}
	if _, err := tx.SupportTicket.UpdateOneID(ticketEntity.ID).SetLastMessageID(messageEntity.ID).SetLastMessageAt(now).Save(ctx); err != nil {
		return rollback(err)
	}
	if err := upsertSupportRead(ctx, tx, ticketEntity.ID, userID, messageEntity.ID); err != nil {
		return rollback(err)
	}
	if err := s.enqueueAdminNotifications(ctx, tx, "new_ticket", ticketEntity, messageEntity); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	keepObjects = true
	s.publish(ctx, SupportRealtimeEvent{Type: "ticket.created", TicketID: ticketEntity.ID, UserID: userID, MessageID: &messageEntity.ID, CreatedAt: now})
	return s.Get(ctx, userID, ticketEntity.ID, false)
}

func (s *SupportService) StartAdminConversation(ctx context.Context, adminID, targetUserID int64, input SupportReplyInput) (*SupportTicket, error) {
	if err := validateSupportReply(&input); err != nil {
		return nil, err
	}
	if _, err := s.client.User.Query().Where(user.IDEQ(targetUserID), user.RoleEQ("user")).Only(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil, infraerrors.NotFound("SUPPORT_USER_NOT_FOUND", "user not found")
		}
		return nil, err
	}
	if input.ClientRequestID != "" {
		existingMessage, err := s.client.SupportTicketMessage.Query().Where(
			supportticketmessage.SenderIDEQ(adminID), supportticketmessage.ClientRequestIDEQ(input.ClientRequestID),
		).Only(ctx)
		if err == nil {
			return s.Get(ctx, adminID, existingMessage.TicketID, true)
		}
		if err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
	}
	existingTicket, err := s.client.SupportTicket.Query().Where(supportticket.UserIDEQ(targetUserID)).Only(ctx)
	if err == nil {
		return s.Reply(ctx, adminID, existingTicket.ID, true, input)
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}

	prepared, err := inspectSupportAttachments(input.Attachments)
	if err != nil {
		return nil, err
	}
	if err := s.storePreparedAttachments(ctx, prepared); err != nil {
		return nil, err
	}
	keepObjects := false
	defer func() {
		if !keepObjects {
			s.cleanupPrepared(context.Background(), prepared)
		}
	}()

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	rollback := func(cause error) (*SupportTicket, error) { _ = tx.Rollback(); return nil, cause }
	number, err := supportTicketNo()
	if err != nil {
		return rollback(err)
	}
	now := time.Now()
	ticketEntity, err := tx.SupportTicket.Create().
		SetTicketNo(number).SetUserID(targetUserID).SetTitle("联系客服").SetCategory("other").
		SetStatus(SupportStatusOpen).SetPriority(SupportPriorityNormal).SetLastMessageAt(now).Save(ctx)
	if err != nil {
		return rollback(err)
	}
	messageCreate := tx.SupportTicketMessage.Create().SetTicketID(ticketEntity.ID).SetSenderID(adminID).
		SetSenderRole("admin").SetKind(SupportMessagePublic).SetContent(input.Content).SetCreatedAt(now)
	if input.ClientRequestID != "" {
		messageCreate.SetClientRequestID(input.ClientRequestID)
	}
	messageEntity, err := messageCreate.Save(ctx)
	if err != nil {
		return rollback(err)
	}
	if err := s.createAttachmentRows(ctx, tx, ticketEntity.ID, messageEntity.ID, adminID, prepared); err != nil {
		return rollback(err)
	}
	if _, err := tx.SupportTicket.UpdateOneID(ticketEntity.ID).SetLastMessageID(messageEntity.ID).SetLastMessageAt(now).Save(ctx); err != nil {
		return rollback(err)
	}
	if err := upsertSupportRead(ctx, tx, ticketEntity.ID, adminID, messageEntity.ID); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	keepObjects = true
	s.publish(ctx, SupportRealtimeEvent{Type: "ticket.created", TicketID: ticketEntity.ID, UserID: targetUserID, MessageID: &messageEntity.ID, CreatedAt: now})
	return s.Get(ctx, adminID, ticketEntity.ID, true)
}

func (s *SupportService) createAttachmentRows(ctx context.Context, tx *ent.Tx, ticketID, messageID, uploaderID int64, items []preparedSupportAttachment) error {
	for _, item := range items {
		if _, err := tx.SupportTicketAttachment.Create().SetTicketID(ticketID).SetMessageID(messageID).
			SetUploaderID(uploaderID).SetStorageKey(item.key).SetOriginalName(item.Name).
			SetContentType(item.ContentType).SetSize(item.size).SetWidth(item.width).SetHeight(item.height).
			SetSha256(item.sha256).Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func upsertSupportRead(ctx context.Context, tx *ent.Tx, ticketID, readerID, messageID int64) error {
	return tx.SupportTicketRead.Create().SetTicketID(ticketID).SetReaderID(readerID).SetLastReadMessageID(messageID).
		SetReadAt(time.Now()).OnConflictColumns(supportticketread.FieldTicketID, supportticketread.FieldReaderID).
		UpdateLastReadMessageID().UpdateReadAt().Exec(ctx)
}

func (s *SupportService) enqueueAdminNotifications(ctx context.Context, tx *ent.Tx, event string, ticketEntity *ent.SupportTicket, message *ent.SupportTicketMessage) error {
	owner, err := tx.User.Get(ctx, ticketEntity.UserID)
	if err != nil {
		return err
	}
	query := tx.AdminTelegramBinding.Query().Where(admintelegrambinding.EnabledEQ(true))
	if event == "new_ticket" {
		query.Where(admintelegrambinding.NotifyNewTicketEQ(true))
	} else {
		query.Where(admintelegrambinding.NotifyUserReplyEQ(true))
	}
	bindings, err := query.All(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"content": message.Content, "user_id": ticketEntity.UserID, "user_email": owner.Email,
	})
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if _, err := tx.SupportNotificationOutbox.Create().SetEventType(event).SetTicketID(ticketEntity.ID).
			SetMessageID(message.ID).SetTargetAdminID(binding.AdminID).SetPayload(string(payload)).Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *SupportService) Reply(ctx context.Context, actorID, ticketID int64, isAdmin bool, input SupportReplyInput) (*SupportTicket, error) {
	if err := validateSupportReply(&input); err != nil {
		return nil, err
	}
	if input.ClientRequestID != "" {
		existing, err := s.client.SupportTicketMessage.Query().Where(
			supportticketmessage.SenderIDEQ(actorID), supportticketmessage.ClientRequestIDEQ(input.ClientRequestID),
		).Only(ctx)
		if err == nil {
			return s.Get(ctx, actorID, existing.TicketID, isAdmin)
		}
		if err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
	}
	ticketEntity, err := s.client.SupportTicket.Get(ctx, ticketID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, infraerrors.NotFound("SUPPORT_NOT_FOUND", "ticket not found")
		}
		return nil, err
	}
	if !isAdmin && ticketEntity.UserID != actorID {
		return nil, infraerrors.NotFound("SUPPORT_NOT_FOUND", "ticket not found")
	}
	prepared, err := inspectSupportAttachments(input.Attachments)
	if err != nil {
		return nil, err
	}
	if !isAdmin {
		if err := s.enforceUserSendQuota(ctx, actorID, supportAttachmentBytes(prepared)); err != nil {
			return nil, err
		}
	}
	if err := s.storePreparedAttachments(ctx, prepared); err != nil {
		return nil, err
	}
	keepObjects := false
	defer func() {
		if !keepObjects {
			s.cleanupPrepared(context.Background(), prepared)
		}
	}()

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	rollback := func(cause error) (*SupportTicket, error) { _ = tx.Rollback(); return nil, cause }
	now := time.Now()
	role := "user"
	if isAdmin {
		role = "admin"
	}
	create := tx.SupportTicketMessage.Create().SetTicketID(ticketID).SetSenderID(actorID).SetSenderRole(role).
		SetKind(SupportMessagePublic).SetContent(input.Content).SetCreatedAt(now)
	if input.ClientRequestID != "" {
		create.SetClientRequestID(input.ClientRequestID)
	}
	messageEntity, err := create.Save(ctx)
	if err != nil {
		return rollback(err)
	}
	if err := s.createAttachmentRows(ctx, tx, ticketID, messageEntity.ID, actorID, prepared); err != nil {
		return rollback(err)
	}

	nextStatus := ticketEntity.Status
	if isAdmin {
		nextStatus = SupportStatusWaitingUser
	} else {
		nextStatus = SupportStatusOpen
	}
	update := tx.SupportTicket.UpdateOneID(ticketID).SetLastMessageID(messageEntity.ID).SetLastMessageAt(now).SetStatus(nextStatus)
	if nextStatus != SupportStatusResolved {
		update.ClearResolvedAt()
	}
	if nextStatus != SupportStatusClosed {
		update.ClearClosedAt()
	}
	if _, err := update.Save(ctx); err != nil {
		return rollback(err)
	}
	if err := upsertSupportRead(ctx, tx, ticketID, actorID, messageEntity.ID); err != nil {
		return rollback(err)
	}
	if !isAdmin {
		fresh, err := tx.SupportTicket.Get(ctx, ticketID)
		if err != nil {
			return rollback(err)
		}
		if err := s.enqueueAdminNotifications(ctx, tx, "user_reply", fresh, messageEntity); err != nil {
			return rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	keepObjects = true
	s.publish(ctx, SupportRealtimeEvent{Type: "message.created", TicketID: ticketID, UserID: ticketEntity.UserID, MessageID: &messageEntity.ID, CreatedAt: now})
	return s.Get(ctx, actorID, ticketID, isAdmin)
}

func (s *SupportService) List(ctx context.Context, userID int64, isAdmin bool, filter SupportListFilter) (*SupportListResult, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	query := s.client.SupportTicket.Query()
	if !isAdmin {
		query.Where(supportticket.UserIDEQ(userID))
	} else if filter.UserID > 0 {
		query.Where(supportticket.UserIDEQ(filter.UserID))
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		userIDs, err := s.client.User.Query().Where(user.Or(user.UsernameContainsFold(search), user.EmailContainsFold(search))).IDs(ctx)
		if err != nil {
			return nil, err
		}
		if len(userIDs) == 0 {
			query.Where(supportticket.UserIDEQ(-1))
		} else {
			query.Where(supportticket.UserIDIn(userIDs...))
		}
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, err
	}
	entities, err := query.Order(ent.Desc(supportticket.FieldLastMessageAt)).Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).All(ctx)
	if err != nil {
		return nil, err
	}
	previews, err := s.supportLastMessagePreviews(ctx, entities)
	if err != nil {
		return nil, err
	}
	items := make([]SupportTicket, 0, len(entities))
	for _, entity := range entities {
		item, err := s.ticketFromEntity(ctx, entity, userID, isAdmin, false)
		if err != nil {
			return nil, err
		}
		item.LastMessagePreview = previews[entity.ID]
		items = append(items, *item)
	}
	pages := int(math.Ceil(float64(total) / float64(filter.PageSize)))
	if pages < 1 {
		pages = 1
	}
	return &SupportListResult{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize, Pages: pages}, nil
}

func (s *SupportService) SearchUsers(ctx context.Context, search string, limit int) ([]SupportUserSearchItem, error) {
	search = strings.TrimSpace(search)
	if search == "" {
		return []SupportUserSearchItem{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	users, err := s.client.User.Query().Where(
		user.RoleEQ("user"),
		user.Or(user.UsernameContainsFold(search), user.EmailContainsFold(search)),
	).Order(ent.Asc(user.FieldUsername), ent.Asc(user.FieldEmail)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return []SupportUserSearchItem{}, nil
	}
	userIDs := make([]int64, 0, len(users))
	for _, entity := range users {
		userIDs = append(userIDs, entity.ID)
	}
	tickets, err := s.client.SupportTicket.Query().Where(supportticket.UserIDIn(userIDs...)).All(ctx)
	if err != nil {
		return nil, err
	}
	previews, err := s.supportLastMessagePreviews(ctx, tickets)
	if err != nil {
		return nil, err
	}
	ticketByUser := make(map[int64]*ent.SupportTicket, len(tickets))
	for _, ticketEntity := range tickets {
		ticketByUser[ticketEntity.UserID] = ticketEntity
	}
	items := make([]SupportUserSearchItem, 0, len(users))
	for _, entity := range users {
		item := SupportUserSearchItem{UserID: entity.ID, UserEmail: entity.Email, UserName: entity.Username}
		if ticketEntity := ticketByUser[entity.ID]; ticketEntity != nil {
			item.TicketID = &ticketEntity.ID
			item.LastMessageAt = &ticketEntity.LastMessageAt
			item.LastMessagePreview = previews[ticketEntity.ID]
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].LastMessageAt, items[j].LastMessageAt
		if left != nil && right != nil {
			return left.After(*right)
		}
		if left != nil || right != nil {
			return left != nil
		}
		leftLabel := strings.ToLower(items[i].UserName + "\x00" + items[i].UserEmail)
		rightLabel := strings.ToLower(items[j].UserName + "\x00" + items[j].UserEmail)
		return leftLabel < rightLabel
	})
	return items, nil
}

func (s *SupportService) supportLastMessagePreviews(ctx context.Context, tickets []*ent.SupportTicket) (map[int64]string, error) {
	messageToTicket := make(map[int64]int64, len(tickets))
	messageIDs := make([]int64, 0, len(tickets))
	for _, ticketEntity := range tickets {
		if ticketEntity.LastMessageID == nil {
			continue
		}
		messageIDs = append(messageIDs, *ticketEntity.LastMessageID)
		messageToTicket[*ticketEntity.LastMessageID] = ticketEntity.ID
	}
	previews := make(map[int64]string, len(messageIDs))
	if len(messageIDs) == 0 {
		return previews, nil
	}
	messages, err := s.client.SupportTicketMessage.Query().Where(supportticketmessage.IDIn(messageIDs...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, message := range messages {
		previews[messageToTicket[message.ID]] = supportMessagePreview(message.Content)
	}
	return previews, nil
}

func supportMessagePreview(content string) string {
	preview := strings.Join(strings.Fields(content), " ")
	if preview == "" {
		return "[图片]"
	}
	runes := []rune(preview)
	if len(runes) > 120 {
		return string(runes[:120]) + "..."
	}
	return preview
}

func (s *SupportService) Get(ctx context.Context, actorID, ticketID int64, isAdmin bool) (*SupportTicket, error) {
	entity, err := s.client.SupportTicket.Get(ctx, ticketID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, infraerrors.NotFound("SUPPORT_NOT_FOUND", "ticket not found")
		}
		return nil, err
	}
	if !isAdmin && entity.UserID != actorID {
		return nil, infraerrors.NotFound("SUPPORT_NOT_FOUND", "ticket not found")
	}
	item, err := s.ticketFromEntity(ctx, entity, actorID, isAdmin, true)
	if err != nil {
		return nil, err
	}
	if entity.LastMessageID != nil {
		_ = s.client.SupportTicketRead.Create().SetTicketID(ticketID).SetReaderID(actorID).SetLastReadMessageID(*entity.LastMessageID).
			SetReadAt(time.Now()).OnConflictColumns(supportticketread.FieldTicketID, supportticketread.FieldReaderID).
			UpdateLastReadMessageID().UpdateReadAt().Exec(ctx)
	}
	item.UnreadCount = 0
	return item, nil
}

func (s *SupportService) ticketFromEntity(ctx context.Context, entity *ent.SupportTicket, readerID int64, isAdmin, includeMessages bool) (*SupportTicket, error) {
	item := &SupportTicket{ID: entity.ID, UserID: entity.UserID, LastMessageAt: entity.LastMessageAt}
	owner, err := s.client.User.Query().Where(user.IDEQ(entity.UserID)).Only(ctx)
	if err == nil {
		item.UserEmail, item.UserName = owner.Email, owner.Username
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	readID := int64(0)
	read, err := s.client.SupportTicketRead.Query().Where(supportticketread.TicketIDEQ(entity.ID), supportticketread.ReaderIDEQ(readerID)).Only(ctx)
	if err == nil {
		readID = read.LastReadMessageID
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	unreadQuery := s.client.SupportTicketMessage.Query().Where(supportticketmessage.TicketIDEQ(entity.ID), supportticketmessage.IDGT(readID), supportticketmessage.SenderIDNEQ(readerID))
	if !isAdmin {
		unreadQuery.Where(supportticketmessage.KindEQ(SupportMessagePublic))
	}
	item.UnreadCount, err = unreadQuery.Count(ctx)
	if err != nil {
		return nil, err
	}
	if !includeMessages {
		return item, nil
	}
	messageQuery := s.client.SupportTicketMessage.Query().Where(supportticketmessage.TicketIDEQ(entity.ID))
	if !isAdmin {
		messageQuery.Where(supportticketmessage.KindEQ(SupportMessagePublic))
	}
	messages, err := messageQuery.Order(ent.Asc(supportticketmessage.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	messageIDs := make([]int64, 0, len(messages))
	for _, message := range messages {
		messageIDs = append(messageIDs, message.ID)
	}
	attachmentsByMessage := map[int64][]SupportAttachment{}
	if len(messageIDs) > 0 {
		attachments, err := s.client.SupportTicketAttachment.Query().Where(supportticketattachment.MessageIDIn(messageIDs...)).Order(ent.Asc(supportticketattachment.FieldID)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, attachment := range attachments {
			if attachment.HiddenAt != nil && !isAdmin {
				continue
			}
			base := "/api/v1/support/attachments/"
			if isAdmin {
				base = "/api/v1/admin/support/attachments/"
			}
			attachmentsByMessage[attachment.MessageID] = append(attachmentsByMessage[attachment.MessageID], SupportAttachment{
				ID: attachment.ID, MessageID: attachment.MessageID, OriginalName: attachment.OriginalName,
				ContentType: attachment.ContentType, Size: attachment.Size, Width: attachment.Width, Height: attachment.Height,
				HiddenAt: attachment.HiddenAt, DownloadURL: fmt.Sprintf("%s%d", base, attachment.ID),
			})
		}
	}
	item.Messages = make([]SupportMessage, 0, len(messages))
	for _, message := range messages {
		item.Messages = append(item.Messages, SupportMessage{ID: message.ID, SenderID: message.SenderID,
			SenderRole: message.SenderRole, Content: message.Content,
			Attachments: attachmentsByMessage[message.ID], CreatedAt: message.CreatedAt})
	}
	return item, nil
}

func (s *SupportService) OpenAttachment(ctx context.Context, actorID, attachmentID int64, isAdmin bool) (*SupportAttachmentDownload, error) {
	attachment, err := s.client.SupportTicketAttachment.Get(ctx, attachmentID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, infraerrors.NotFound("SUPPORT_ATTACHMENT_NOT_FOUND", "attachment not found")
		}
		return nil, err
	}
	if attachment.HiddenAt != nil && !isAdmin {
		return nil, infraerrors.NotFound("SUPPORT_ATTACHMENT_NOT_FOUND", "attachment not found")
	}
	if !isAdmin {
		ticketEntity, err := s.client.SupportTicket.Get(ctx, attachment.TicketID)
		if err != nil || ticketEntity.UserID != actorID {
			return nil, infraerrors.NotFound("SUPPORT_ATTACHMENT_NOT_FOUND", "attachment not found")
		}
	}
	body, err := s.storage.Open(ctx, attachment.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("open support attachment: %w", err)
	}
	return &SupportAttachmentDownload{Body: body, Name: attachment.OriginalName, ContentType: attachment.ContentType, Size: attachment.Size}, nil
}

func (s *SupportService) HideAttachment(ctx context.Context, adminID, attachmentID int64, hidden bool) error {
	update := s.client.SupportTicketAttachment.UpdateOneID(attachmentID)
	if hidden {
		update.SetHiddenAt(time.Now()).SetHiddenBy(adminID)
	} else {
		update.ClearHiddenAt().ClearHiddenBy()
	}
	if _, err := update.Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return infraerrors.NotFound("SUPPORT_ATTACHMENT_NOT_FOUND", "attachment not found")
		}
		return err
	}
	return nil
}

func (s *SupportService) UnreadCount(ctx context.Context, readerID int64, isAdmin bool) (int, error) {
	query := s.client.SupportTicketMessage.Query().Where(
		supportticketmessage.SenderIDNEQ(readerID),
		supportMessageAfterReadCursor(readerID),
	)
	if !isAdmin {
		query.Where(
			supportticketmessage.KindEQ(SupportMessagePublic),
			supportMessageOwnedBy(readerID),
		)
	}
	return query.Count(ctx)
}

func supportMessageAfterReadCursor(readerID int64) predicate.SupportTicketMessage {
	return func(messages *entsql.Selector) {
		reads := entsql.Table(supportticketread.Table)
		readAtOrAfterMessage := messages.New().
			Select(reads.C(supportticketread.FieldID)).
			From(reads).
			Where(entsql.And(
				entsql.ColumnsEQ(reads.C(supportticketread.FieldTicketID), messages.C(supportticketmessage.FieldTicketID)),
				entsql.EQ(reads.C(supportticketread.FieldReaderID), readerID),
				entsql.ColumnsGTE(reads.C(supportticketread.FieldLastReadMessageID), messages.C(supportticketmessage.FieldID)),
			))
		messages.Where(entsql.NotExists(readAtOrAfterMessage))
	}
}

func supportMessageOwnedBy(userID int64) predicate.SupportTicketMessage {
	return func(messages *entsql.Selector) {
		tickets := entsql.Table(supportticket.Table)
		ownedTicket := messages.New().
			Select(tickets.C(supportticket.FieldID)).
			From(tickets).
			Where(entsql.And(
				entsql.ColumnsEQ(tickets.C(supportticket.FieldID), messages.C(supportticketmessage.FieldTicketID)),
				entsql.EQ(tickets.C(supportticket.FieldUserID), userID),
			))
		messages.Where(entsql.Exists(ownedTicket))
	}
}

func SupportUserChannel(userID int64) string { return fmt.Sprintf("support:realtime:user:%d", userID) }

func (s *SupportService) publish(ctx context.Context, event SupportRealtimeEvent) {
	if s.redis == nil {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	_ = s.redis.Publish(ctx, SupportAdminChannel, payload).Err()
	_ = s.redis.Publish(ctx, SupportUserChannel(event.UserID), payload).Err()
}

func (s *SupportService) Subscribe(ctx context.Context, channel string) *redis.PubSub {
	if s.redis == nil {
		return nil
	}
	return s.redis.Subscribe(ctx, channel)
}
