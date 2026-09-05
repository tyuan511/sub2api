package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

func (h *OpenAIGatewayHandler) Live(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	if apiKey.Group == nil || (apiKey.Group.Platform != service.PlatformOpenAI && apiKey.Group.Platform != service.PlatformComposite) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Live is not supported for this platform")
		return
	}
	if !liveEnabledForAPIKey(apiKey) && !apiKeyMultiGroupRoutingActive(c) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "Live is not enabled for this group")
		return
	}
	request, err := parseLiveCallRequest(c)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	model := strings.TrimSpace(gjson.GetBytes(request.Session, "model").String())
	if !compositeTargetPlatformAllowed(c, apiKey, model, service.PlatformOpenAI) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Live only supports OpenAI models for Composite groups")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.live",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if h.billingCacheService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Billing service unavailable")
		return
	}
	routeEndpoint := c.Request.URL.Path
	liveCandidateCheck := func(candidate *service.APIKey) error {
		if candidate == nil || candidate.Group == nil || !liveEnabledForAPIKey(candidate) {
			return service.ErrNoEligibleAPIKeyRoute
		}
		if !compositeTargetPlatformAllowed(c, candidate, model, service.PlatformOpenAI) {
			return fmt.Errorf("candidate group %d does not support OpenAI Live", candidate.Group.ID)
		}
		allowed, state, healthErr := h.gatewayService.AllowAPIKeyRoute(c.Request.Context(), candidate.ID, candidate.RouteVersion, candidate.Group.ID, model, routeEndpoint)
		if healthErr != nil {
			reqLog.Warn("openai.live.api_key_group_health_read_failed", zap.Int64("candidate_group_id", candidate.Group.ID), zap.Error(healthErr))
			return nil
		}
		if !allowed {
			return fmt.Errorf("candidate group %d breaker is %s", candidate.Group.ID, state)
		}
		return nil
	}
	routeSessionHash := h.gatewayService.GenerateExplicitSessionHash(c, request.Session)
	stickyModelFamily, stickyEndpointKind := apiKeyRouteStickyScope(apiKey, model, routeEndpoint)
	stickyGroupID, stickyErr := h.gatewayService.GetAPIKeyGroupSticky(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, stickyModelFamily, stickyEndpointKind, routeSessionHash)
	routeStateDegraded := stickyErr != nil
	if stickyErr != nil {
		reqLog.Warn("openai.live.api_key_group_route_state_degraded", zap.Error(stickyErr))
	}
	stickyRouteSelected := apiKeyMultiGroupRoutingActive(c) && stickyErr == nil && stickyGroupID > 0 && apiKey.GroupID != nil && *apiKey.GroupID == stickyGroupID
	if apiKeyMultiGroupRoutingActive(c) && stickyErr == nil && stickyGroupID > 0 && (apiKey.GroupID == nil || *apiKey.GroupID != stickyGroupID) {
		stickyAPIKey, stickySubscription, activated, activateErr := h.apiKeyRouteRuntime().activateSticky(c, stickyGroupID, liveCandidateCheck)
		if activateErr != nil {
			middleware2.MarkAPIKeyRouteStickySelected(c)
			middleware2.MarkAPIKeyRouteStickyBroken(c)
			reqLog.Warn("openai.live.api_key_group_sticky_ignored", zap.Int64("sticky_group_id", stickyGroupID), zap.Error(activateErr))
		} else if activated {
			stickyRouteSelected = true
			apiKey = stickyAPIKey
			subscription = stickySubscription
		}
	}
	if stickyRouteSelected {
		middleware2.MarkAPIKeyRouteStickySelected(c)
	}
	if shouldActivateSmartRoute(c, stickyRouteSelected, routeStateDegraded) {
		smartAPIKey, smartSubscription, ranked, _, smartErr := h.apiKeyRouteRuntime().activateSmart(c, apiKey, model, routeEndpoint, routeSessionHash, liveCandidateCheck)
		if smartErr != nil {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No eligible candidate groups")
			return
		}
		if len(ranked) > 0 {
			apiKey = smartAPIKey
			subscription = smartSubscription
		}
	}
	apiKey, subscription, initialRouteChanged, err := h.apiKeyRouteRuntime().ensureInitial(c, liveCandidateCheck)
	if err != nil {
		if isAPIKeyRouteAdvanceBillingError(err) {
			status, code, message, retryAfter := billingErrorDetails(errors.Unwrap(err))
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.errorResponse(c, status, code, message)
			return
		}
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No eligible candidate groups")
		return
	}
	if initialRouteChanged && stickyRouteSelected {
		middleware2.MarkAPIKeyRouteStickyBroken(c)
	}
	if upstreamModel, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context()); ok && upstreamModel != model {
		rewrittenSession, rewriteErr := sjson.SetBytes(request.Session, "model", upstreamModel)
		if rewriteErr != nil {
			h.errorResponse(c, http.StatusInternalServerError, "api_error", "Failed to apply Composite model route")
			return
		}
		request.Session = rewrittenSession
	}
	reqLog = reqLog.With(zap.Any("actual_group_id", apiKey.GroupID), zap.Int64("route_version", apiKey.RouteVersion))
	if decision := h.checkSecurityAudit(
		c,
		reqLog,
		apiKey,
		subject,
		service.ContentModerationProtocolOpenAIResponses,
		model,
		request.Session,
	); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	if err := h.billingCacheService.CheckBillingEligibility(
		c.Request.Context(),
		apiKey.User,
		apiKey,
		apiKey.Group,
		subscription,
		service.QuotaPlatform(c.Request.Context(), apiKey),
	); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}

	userRelease, acquired, err := h.concurrencyHelper.TryAcquireUserSlot(
		c.Request.Context(),
		subject.UserID,
		subject.Concurrency,
	)
	if err != nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Live concurrency unavailable")
		return
	}
	if !acquired {
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Live concurrency limit reached")
		return
	}
	defer userRelease()

	identity := liveCallIdentity(c, apiKey, subject.UserID, subscription)
	created, err := h.gatewayService.CreateLiveCall(c.Request.Context(), request, identity, subject.Concurrency)
	if err != nil {
		if apiKeyMultiGroupRoutingActive(c) && apiKey.GroupID != nil {
			_, _ = h.gatewayService.RecordAPIKeyRouteFailure(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, *apiKey.GroupID, model, routeEndpoint, err)
		}
		h.writeLiveCreateError(c, err)
		return
	}
	if apiKeyMultiGroupRoutingActive(c) && apiKey.GroupID != nil {
		if state, observeErr := h.gatewayService.RecordAPIKeyRouteResult(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, *apiKey.GroupID, model, routeEndpoint, true); observeErr != nil {
			reqLog.Warn("openai.live.api_key_group_success_record_failed", zap.String("state", state), zap.Error(observeErr))
		}
		_ = h.gatewayService.BindAPIKeyGroupSticky(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, stickyModelFamily, stickyEndpointKind, routeSessionHash, *apiKey.GroupID)
	}
	c.Header("Location", liveSidebandLocation(c.FullPath(), created.CallID))
	c.Data(http.StatusOK, "application/sdp", created.SDP)
}

func parseLiveCallRequest(c *gin.Context) (*service.LiveCallRequest, error) {
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		sdp := c.PostForm("sdp")
		session := json.RawMessage(c.PostForm("session"))
		request := &service.LiveCallRequest{SDP: sdp, Session: session}
		if err := service.ValidateLiveCallRequest(request); err != nil {
			return nil, err
		}
		return request, nil
	}
	var request service.LiveCallRequest
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&request); err != nil {
		return nil, errors.New("request body must be valid JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("request body must contain one JSON object")
	}
	if err := service.ValidateLiveCallRequest(&request); err != nil {
		return nil, err
	}
	return &request, nil
}

func liveSidebandLocation(fullPath, callID string) string {
	prefix := "/v1/live/"
	if strings.HasPrefix(fullPath, "/backend-api/codex/") {
		prefix = "/backend-api/codex/"
	}
	return prefix + url.PathEscape(callID)
}

func liveCallIdentity(
	c *gin.Context,
	apiKey *service.APIKey,
	userID int64,
	subscription *service.UserSubscription,
) service.LiveCallIdentity {
	var subscriptionID *int64
	if subscription != nil {
		value := subscription.ID
		subscriptionID = &value
	}
	return service.LiveCallIdentity{
		APIKeyID:        apiKey.ID,
		UserID:          userID,
		GroupID:         apiKey.GroupID,
		SubscriptionID:  subscriptionID,
		UserAgent:       c.GetHeader("User-Agent"),
		IPAddress:       ip.GetClientIP(c),
		InboundEndpoint: GetInboundEndpoint(c),
	}
}

func (h *OpenAIGatewayHandler) writeLiveCreateError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrLiveConcurrencyFull):
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Live concurrency limit reached")
	case errors.Is(err, service.ErrLiveUnavailable):
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Live is unavailable")
	default:
		var attestationErr *service.LiveAttestationUnavailableError
		if errors.As(err, &attestationErr) {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", attestationErr.Error())
			return
		}
		var upstreamErr *service.UpstreamFailoverError
		if errors.As(err, &upstreamErr) && upstreamErr.StatusCode >= 400 && upstreamErr.StatusCode < 500 {
			h.errorResponse(c, upstreamErr.StatusCode, "invalid_request_error", "Live upstream rejected the request")
			return
		}
		h.errorResponse(c, http.StatusBadGateway, "api_error", "Live upstream request failed")
	}
}

func (h *OpenAIGatewayHandler) LiveSideband(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	if !liveEnabledForAPIKey(apiKey) && !apiKeyMultiGroupRoutingActive(c) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "Live is not enabled for this group")
		return
	}
	var record *service.LiveCallRecord
	var err error
	if apiKeyMultiGroupRoutingActive(c) {
		ownedAPIKey, _, ownerErr := activateAPIKeyRouteOwnedGroup(c, func(groupID int64) (bool, error) {
			candidateGroupID := groupID
			candidateRecord, lookupErr := h.gatewayService.GetLiveCallForIdentity(c.Request.Context(), c.Param("call_id"), service.LiveCallIdentity{
				APIKeyID: apiKey.ID, UserID: subject.UserID, GroupID: &candidateGroupID,
			})
			if errors.Is(lookupErr, service.ErrLiveIdentityMismatch) {
				return false, nil
			}
			if lookupErr != nil {
				return false, lookupErr
			}
			record = candidateRecord
			return true, nil
		})
		if ownerErr == nil && ownedAPIKey != nil {
			apiKey = ownedAPIKey
		}
		if ownerErr != nil {
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Live call not found")
			return
		}
	}
	identity := service.LiveCallIdentity{APIKeyID: apiKey.ID, UserID: subject.UserID, GroupID: apiKey.GroupID}
	if !liveEnabledForAPIKey(apiKey) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "Live is not enabled for this group")
		return
	}
	if record == nil {
		record, err = h.gatewayService.GetLiveCallForIdentity(c.Request.Context(), c.Param("call_id"), identity)
	}
	if err != nil {
		if errors.Is(err, service.ErrLiveIdentityMismatch) {
			h.errorResponse(c, http.StatusForbidden, "permission_error", "Live call belongs to another identity")
			return
		}
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Live call not found")
		return
	}
	downstream, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer func() { _ = downstream.CloseNow() }()
	if err := h.gatewayService.ProxyLiveSideband(c.Request.Context(), record, downstream); err != nil {
		_ = downstream.Close(coderws.StatusInternalError, "live sideband closed")
		return
	}
	_ = downstream.Close(coderws.StatusNormalClosure, "")
}

func liveEnabledForAPIKey(apiKey *service.APIKey) bool {
	return apiKey != nil &&
		apiKey.Group != nil &&
		(apiKey.Group.Platform == service.PlatformOpenAI || apiKey.Group.Platform == service.PlatformComposite) &&
		apiKey.Group.AllowLive
}
