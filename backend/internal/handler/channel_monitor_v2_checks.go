package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// groupCheckInternalKeyProvider must return a persisted, internal-only key owned
// by userID and bound exclusively to groupID. It must never return user keys.
type groupCheckInternalKeyProvider interface {
	GroupCheckInternalKey(ctx context.Context, userID, groupID int64) (*service.APIKey, error)
}

// groupCheckGateway runs the normal authenticated and billed gateway pipeline.
// Implementations must enforce the requested group, including group fallbacks.
type groupCheckGateway interface {
	RunGroupCheck(ctx context.Context, key *service.APIKey, groupID int64, model, prompt string) (*http.Response, error)
}

type groupCheckExecutor struct {
	keys    groupCheckInternalKeyProvider
	gateway groupCheckGateway
}

type groupCheckRecordStore interface {
	Complete(context.Context, int64, int64, int64, repository.ChannelGroupProbeResult) error
}

// run detaches from the HTTP request so client disconnects do not cancel a
// billable request. Completion has its own deadline even if execution times out.
func (e groupCheckExecutor) run(store groupCheckRecordStore, record *repository.ChannelGroupProbeRecord) {
	started := time.Now()
	result := repository.ChannelGroupProbeResult{Status: "failed"}
	defer func() {
		if recover() != nil {
			result.Status = "failed"
			result.SVG = nil
		}
		duration := time.Since(started).Milliseconds()
		result.DurationMS = &duration
		if result.Status == "failed" {
			message := "检测失败"
			result.ErrorMessage = &message
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := store.Complete(ctx, record.ID, record.GroupID, record.UserID, result); err != nil {
			fmt.Printf("group check %d completion failed: %v\n", record.ID, err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	requestID := uuid.NewString()
	ctx = context.WithValue(ctx, ctxkey.ClientRequestID, requestID)
	svg, err := e.executeWithIdentity(ctx, record.UserID, record.GroupID, record.Kind, record.Model, func(key *service.APIKey) error {
		identityStore, ok := store.(interface {
			SetBillingIdentity(context.Context, int64, int64, int64, int64, string) error
		})
		if !ok {
			return errors.New("group check billing identity store unavailable")
		}
		return identityStore.SetBillingIdentity(ctx, record.ID, record.GroupID, record.UserID, key.ID, "client:"+requestID)
	})
	if err != nil {
		return
	}
	result.Status = "succeeded"
	if svg != "" {
		result.SVG = &svg
	}
}

func (e groupCheckExecutor) execute(ctx context.Context, userID, groupID int64, kind, model string) (string, error) {
	return e.executeWithIdentity(ctx, userID, groupID, kind, model, nil)
}

func (e groupCheckExecutor) executeWithIdentity(ctx context.Context, userID, groupID int64, kind, model string, saveIdentity func(*service.APIKey) error) (string, error) {
	if e.keys == nil || e.gateway == nil {
		return "", errors.New("group check dependencies unavailable")
	}
	key, err := e.keys.GroupCheckInternalKey(ctx, userID, groupID)
	if err != nil {
		return "", err
	}
	if key == nil || key.ID <= 0 || key.UserID != userID || key.GroupID == nil || *key.GroupID != groupID || key.Group == nil || key.Group.ID != groupID {
		return "", errors.New("invalid internal group check key")
	}
	if saveIdentity != nil {
		if err := saveIdentity(key); err != nil {
			return "", err
		}
	}
	prompt := "Reply with the single word OK."
	if kind == "pelican" {
		prompt = "Draw a pelican riding a bicycle as an SVG image. Return only the SVG markup."
	}
	resp, err := e.gateway.RunGroupCheck(ctx, key, groupID, model, prompt)
	if err != nil {
		return "", err
	}
	if resp == nil || resp.Body == nil {
		return "", errors.New("empty gateway response")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2_000_001))
	if err != nil {
		return "", err
	}
	if len(body) > 2_000_000 {
		return "", errors.New("gateway response too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gateway returned HTTP %d", resp.StatusCode)
	}
	text, err := groupCheckResponseText(body, resp.Header.Get("Content-Type"))
	if err != nil {
		return "", err
	}
	if kind != "pelican" {
		return "", nil
	}
	start := strings.Index(text, "<svg")
	end := strings.LastIndex(text, "</svg>")
	if start < 0 || end < start {
		return "", errors.New("gateway response contains no SVG")
	}
	return text[start : end+len("</svg>")], nil
}

// groupCheckResponseText accepts OpenAI, Anthropic and Gemini response shapes,
// including their SSE variants. An SSE error event must never count as success.
func groupCheckResponseText(body []byte, contentType string) (string, error) {
	var chunks []string
	parse := func(data []byte) error {
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		var walk func(any, string)
		walk = func(value any, key string) {
			switch x := value.(type) {
			case map[string]any:
				for k, child := range x {
					walk(child, k)
				}
			case []any:
				for _, child := range x {
					walk(child, key)
				}
			case string:
				if key == "text" || key == "content" || key == "output_text" || key == "delta" {
					chunks = append(chunks, x)
				}
			}
		}
		if obj, ok := v.(map[string]any); ok {
			if obj["error"] != nil || obj["type"] == "error" || obj["type"] == "response.failed" {
				return errors.New("gateway stream failed")
			}
		}
		walk(v, "")
		return nil
	}
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		for _, line := range bytes.Split(body, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
				continue
			}
			if err := parse(data); err != nil {
				return "", err
			}
		}
	} else if err := parse(body); err != nil {
		return "", err
	}
	if len(chunks) == 0 {
		return "", errors.New("gateway response contains no text")
	}
	return strings.Join(chunks, ""), nil
}

// ProvideChannelMonitorV2Handler connects the shared probe record store to the handler.
func ProvideChannelMonitorV2Handler(svc *service.ChannelMonitorV2Service, keys *service.APIKeyService, records *repository.ChannelGroupProbeRecordRepository, db *sql.DB, users *service.UserService, subscriptions *service.SubscriptionService, gateway *GatewayHandler, openAI *OpenAIGatewayHandler) *ChannelMonitorV2Handler {
	h := NewChannelMonitorV2Handler(svc, keys)
	h.probeRecords = records
	provider := &groupCheckKeyProvider{identities: repository.NewProbeInternalKeyRepository(db), users: users, groups: keys}
	h.probeExecutor = &groupCheckExecutor{keys: provider, gateway: &groupCheckGatewayAdapter{auth: groupCheckInternalAuth(provider, subscriptions), gateway: gateway, openAI: openAI}}
	h.probeSlots = make(chan struct{}, 16)
	return h
}

type groupCheckResponse struct {
	ID           int64     `json:"id"`
	GroupID      int64     `json:"group_id"`
	Kind         string    `json:"kind"`
	Model        string    `json:"model"`
	Status       string    `json:"status"`
	IsMine       bool      `json:"is_mine"`
	CreatedAt    time.Time `json:"created_at"`
	DurationMS   *int64    `json:"duration_ms"`
	SVG          *string   `json:"svg"`
	ErrorMessage *string   `json:"error_message"`
	ActualCost   *float64  `json:"actual_cost"`
}

func (h *ChannelMonitorV2Handler) authorizeGroupCheck(c *gin.Context) (int64, int64, bool) {
	groupID, err := strconv.ParseInt(c.Param("group_id"), 10, 64)
	if err != nil || groupID <= 0 {
		response.BadRequest(c, "invalid group_id")
		return 0, 0, false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "user not found in context")
		return 0, 0, false
	}
	if h.apiKeyService == nil {
		response.Error(c, http.StatusServiceUnavailable, "group authorization unavailable")
		return 0, 0, false
	}
	groups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return 0, 0, false
	}
	for _, group := range groups {
		if group.ID == groupID {
			return groupID, subject.UserID, true
		}
	}
	response.Error(c, http.StatusForbidden, "group access denied")
	return 0, 0, false
}

func (h *ChannelMonitorV2Handler) ListGroupChecks(c *gin.Context) {
	groupID, userID, ok := h.authorizeGroupCheck(c)
	if !ok {
		return
	}
	if h.probeRecords == nil {
		response.Error(c, http.StatusServiceUnavailable, "group checks unavailable")
		return
	}
	records, err := h.probeRecords.List(c.Request.Context(), groupID, userID, 50)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list group checks")
		return
	}
	items := make([]groupCheckResponse, 0, len(records))
	for _, record := range records {
		item := groupCheckResponse{ID: record.ID, GroupID: record.GroupID, Kind: record.Kind, Model: record.Model, Status: record.Status, IsMine: record.UserID == userID, CreatedAt: record.CreatedAt, DurationMS: record.DurationMS, SVG: record.SVG}
		if item.IsMine {
			item.ActualCost = record.ActualCost
		}
		// Never expose upstream errors: they can contain credentials or private data.
		if item.IsMine && record.ErrorMessage != nil && *record.ErrorMessage != "" {
			message := "检测失败"
			item.ErrorMessage = &message
		}
		items = append(items, item)
	}
	response.Success(c, gin.H{"items": items})
}

func (h *ChannelMonitorV2Handler) StartGroupCheck(c *gin.Context) {
	groupID, userID, ok := h.authorizeGroupCheck(c)
	if !ok {
		return
	}
	var input struct {
		Kind  string `json:"kind"`
		Model string `json:"model"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || (input.Kind != "connectivity" && input.Kind != "pelican") || strings.TrimSpace(input.Model) == "" || len(input.Model) > 200 {
		response.BadRequest(c, "invalid group check request")
		return
	}
	if h.probeRecords == nil || h.probeExecutor == nil || h.probeSlots == nil {
		response.Error(c, http.StatusServiceUnavailable, "group check execution unavailable")
		return
	}
	select {
	case h.probeSlots <- struct{}{}:
	default:
		response.Error(c, http.StatusTooManyRequests, "too many group checks")
		return
	}
	record, err := h.probeRecords.Create(c.Request.Context(), groupID, userID, input.Kind, strings.TrimSpace(input.Model))
	if err != nil {
		<-h.probeSlots
		response.Error(c, http.StatusInternalServerError, "failed to create group check")
		return
	}
	go func() {
		defer func() { <-h.probeSlots }()
		h.probeExecutor.run(h.probeRecords, record)
	}()
	c.JSON(http.StatusAccepted, gin.H{"code": 0, "message": "success", "data": groupCheckResponse{ID: record.ID, GroupID: record.GroupID, Kind: record.Kind, Model: record.Model, Status: record.Status, IsMine: true, CreatedAt: record.CreatedAt}})
}
