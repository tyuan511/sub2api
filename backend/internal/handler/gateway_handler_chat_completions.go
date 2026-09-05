package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ChatCompletions handles OpenAI Chat Completions API endpoint for Anthropic platform groups.
// POST /v1/chat/completions
// This converts Chat Completions requests to Anthropic format (via Responses format chain),
// forwards to Anthropic upstream, and converts responses back to Chat Completions format.
func (h *GatewayHandler) ChatCompletions(c *gin.Context) {
	streamStarted := false

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.chat_completions",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	// Read request body
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.chatCompletionsErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false)

	// Validate JSON
	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	// Extract model and stream
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	bindRequestedReasoningEffort(c, body, reqModel)
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !compositeTargetPlatformResolved(c, apiKey, reqModel) {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by composite groups")
		return
	}
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	if service.IsGPTImageGenerationModel(reqModel) {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "This model is not supported on the Chat Completions endpoint")
		return
	}
	if apiKey.Group != nil && apiKey.Group.ClaudeCodeOnly && !apiKeyMultiGroupRoutingActive(c) {
		h.chatCompletionsErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group is restricted to Claude Code clients (/v1/messages only)")
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))
	var channelMapping service.ChannelMappingResult
	var pricingAt time.Time

	// Error passthrough binding
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	// Parse before route selection so group-level sticky/smart ordering uses the
	// same stable session identity as the eventual in-group account scheduler.
	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, _ := service.ParseGatewayRequest(bodyRef, "chat_completions")
	if parsedReq == nil {
		parsedReq = &service.ParsedRequest{Model: reqModel, Stream: reqStream, Body: bodyRef}
	}
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)
	routeEndpoint := c.Request.URL.Path
	gatewayCCCandidateCheck := func(candidate *service.APIKey) error {
		if candidate == nil || candidate.Group == nil {
			return service.ErrNoEligibleAPIKeyRoute
		}
		if candidate.Group.ClaudeCodeOnly {
			return fmt.Errorf("candidate group %d is restricted to Claude Code", candidate.Group.ID)
		}
		if !compositeTargetPlatformResolved(c, candidate, reqModel) {
			return fmt.Errorf("candidate group %d does not support this model target", candidate.Group.ID)
		}
		allowed, state, healthErr := h.gatewayService.AllowAPIKeyRoute(c.Request.Context(), candidate.ID, candidate.RouteVersion, candidate.Group.ID, reqModel, routeEndpoint)
		if healthErr != nil {
			reqLog.Warn("gateway.cc.api_key_group_health_read_failed", zap.Int64("candidate_group_id", candidate.Group.ID), zap.Error(healthErr))
			return nil
		}
		if !allowed {
			return fmt.Errorf("candidate group %d breaker is %s", candidate.Group.ID, state)
		}
		return nil
	}
	stickyModelFamily, stickyEndpointKind := apiKeyRouteStickyScope(apiKey, reqModel, routeEndpoint)
	stickyGroupID, stickyErr := h.gatewayService.GetAPIKeyGroupSticky(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, stickyModelFamily, stickyEndpointKind, sessionHash)
	routeStateDegraded := stickyErr != nil
	if stickyErr != nil {
		reqLog.Warn("gateway.cc.api_key_group_route_state_degraded", zap.String("reason", "sticky_read_failed"), zap.Error(stickyErr))
	}
	stickyRouteSelected := apiKeyMultiGroupRoutingActive(c) && stickyErr == nil && stickyGroupID > 0 && apiKey.GroupID != nil && *apiKey.GroupID == stickyGroupID
	if apiKeyMultiGroupRoutingActive(c) && stickyErr == nil && stickyGroupID > 0 && (apiKey.GroupID == nil || *apiKey.GroupID != stickyGroupID) {
		stickyAPIKey, stickySubscription, activated, activateErr := h.apiKeyRouteRuntime().activateSticky(c, stickyGroupID, gatewayCCCandidateCheck)
		if activateErr != nil {
			middleware2.MarkAPIKeyRouteStickySelected(c)
			middleware2.MarkAPIKeyRouteStickyBroken(c)
			reqLog.Warn("gateway.cc.api_key_group_sticky_ignored", zap.Int64("sticky_group_id", stickyGroupID), zap.Error(activateErr))
		} else if activated {
			stickyRouteSelected = true
			apiKey = stickyAPIKey
			subscription = stickySubscription
			reqLog.Info("gateway.cc.api_key_group_sticky_hit", zap.Int64("sticky_group_id", stickyGroupID))
		}
	}
	if stickyRouteSelected {
		middleware2.MarkAPIKeyRouteStickySelected(c)
	}
	if shouldActivateSmartRoute(c, stickyRouteSelected, routeStateDegraded) {
		smartAPIKey, smartSubscription, ranked, activated, smartErr := h.apiKeyRouteRuntime().activateSmart(c, apiKey, reqModel, routeEndpoint, sessionHash, gatewayCCCandidateCheck)
		if smartErr != nil {
			h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "server_error", "No eligible candidate groups")
			return
		}
		if len(ranked) > 0 {
			apiKey = smartAPIKey
			subscription = smartSubscription
			state, _ := middleware2.GetAPIKeyRouteState(c)
			reqLog.Info("gateway.cc.api_key_group_smart_order_applied", zap.Bool("initial_group_changed", activated), zap.String("score_version", state.ScoreVersion), zap.Int("candidate_count", len(ranked)))
		}
	}
	apiKey, subscription, initialRouteChanged, initialRouteErr := h.apiKeyRouteRuntime().ensureInitial(c, gatewayCCCandidateCheck)
	if initialRouteErr != nil {
		if isAPIKeyRouteAdvanceBillingError(initialRouteErr) {
			status, code, message, retryAfter := billingErrorDetails(errors.Unwrap(initialRouteErr))
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.chatCompletionsErrorResponse(c, status, code, message)
		} else {
			h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "server_error", "No eligible candidate groups")
		}
		return
	}
	if initialRouteChanged && stickyRouteSelected {
		middleware2.MarkAPIKeyRouteStickyBroken(c)
	}
	reqLog = reqLog.With(zap.Any("actual_group_id", apiKey.GroupID), zap.Int64("route_version", apiKey.RouteVersion))
	channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	pricingCtx, frozenPricingAt := service.WithGatewayTokenRequestPricing(c.Request.Context())
	pricingAt = frozenPricingAt
	c.Request = c.Request.WithContext(pricingCtx)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)
	if err != nil {
		reqLog.Warn("gateway.cc.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. Re-check billing
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("gateway.cc.billing_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.chatCompletionsErrorResponse(c, status, code, message)
		return
	}

	groupPlatform := effectiveAPIKeyPlatform(c, apiKey)
	selectionSessionHash := sessionHash
	if groupPlatform == service.PlatformGemini && selectionSessionHash != "" {
		selectionSessionHash = "gemini:" + selectionSessionHash
	}
	// 3. Account selection + failover loop
	fs := NewFailoverState(h.maxAccountSwitches, false)
	if groupPlatform == service.PlatformGemini {
		fs = NewFailoverState(h.maxAccountSwitchesGemini, false)
	}
	advanceGatewayCCRoute := func(routeErr error) (bool, error) {
		if !apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput(c, routeErr) {
			return false, nil
		}
		if apiKeyMultiGroupRoutingActive(c) && routeErr != nil {
			middleware2.MarkAPIKeyRouteStickyBroken(c)
		}
		if apiKeyMultiGroupRoutingActive(c) && routeErr != nil && apiKey.GroupID != nil {
			if state, observeErr := h.gatewayService.RecordAPIKeyRouteFailure(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, *apiKey.GroupID, reqModel, routeEndpoint, routeErr); observeErr != nil {
				reqLog.Warn("gateway.cc.api_key_group_health_record_failed", zap.String("state", state), zap.Error(observeErr))
			}
		}
		nextAPIKey, nextSubscription, advanced, advanceErr := h.apiKeyRouteRuntime().advance(c, reqModel, routeEndpoint, gatewayCCCandidateCheck)
		if !advanced {
			return false, advanceErr
		}
		apiKey = nextAPIKey
		subscription = nextSubscription
		channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
		newPricingCtx, newPricingAt := service.WithGatewayTokenRequestPricing(c.Request.Context())
		c.Request = c.Request.WithContext(newPricingCtx)
		pricingAt = newPricingAt
		groupPlatform = effectiveAPIKeyPlatform(c, apiKey)
		selectionSessionHash = sessionHash
		if groupPlatform == service.PlatformGemini && selectionSessionHash != "" {
			selectionSessionHash = "gemini:" + selectionSessionHash
		}
		fs = NewFailoverState(h.maxAccountSwitches, false)
		if groupPlatform == service.PlatformGemini {
			fs = NewFailoverState(h.maxAccountSwitchesGemini, false)
		}
		reqLog.Info("gateway.cc.api_key_group_route_switched", zap.Int64p("actual_group_id", apiKey.GroupID))
		return true, nil
	}

	for {
		if c.Request.Context().Err() != nil {
			return
		}
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, selectionSessionHash, reqModel, fs.FailedAccountIDs, "", int64(0))
		if err != nil {
			if advanced, advanceErr := advanceGatewayCCRoute(err); advanced {
				continue
			} else if advanceErr != nil {
				reqLog.Warn("gateway.cc.api_key_group_route_advance_failed", zap.Error(advanceErr))
			}
			if len(fs.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel, groupPlatform)
				cls = classifySelectionFailureError(err, cls)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				message := cls.Message
				if !cls.ModelNotFound {
					message = "No available accounts: " + err.Error()
				}
				h.chatCompletionsErrorResponse(c, cls.Status, cls.ErrType, message)
				return
			}
			action := fs.HandleSelectionExhausted(c.Request.Context())
			switch action {
			case FailoverContinue:
				continue
			case FailoverCanceled:
				failoverClientGone(c)
				return
			default:
				if fs.LastFailoverErr != nil {
					h.handleCCFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
				} else {
					h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "server_error", "All available accounts exhausted")
				}
				return
			}
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)

		// 4. Acquire account concurrency slot
		accountReleaseFunc := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				markOpsRoutingCapacityLimited(c)
				h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
				return
			}
			accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
				c,
				account.ID,
				selection.WaitPlan.MaxConcurrency,
				selection.WaitPlan.Timeout,
				reqStream,
				&streamStarted,
			)
			if err != nil {
				reqLog.Warn("gateway.cc.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				h.handleConcurrencyError(c, err, "account", streamStarted)
				return
			}
			// 多代理池：账号槽位到手后再绑定出口代理（等待期间不占代理槽位）。
			bound, boundRelease, bindErr := h.gatewayService.BindAccountProxyAfterSlot(c.Request.Context(), account, accountReleaseFunc)
			if bindErr != nil {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				reqLog.Warn("gateway.cc.account_proxy_bind_failed", zap.Int64("account_id", account.ID), zap.Error(bindErr))
				markOpsRoutingCapacityLimited(c)
				h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
				return
			}
			account = bound
			accountReleaseFunc = boundRelease
			selection.Account = bound
		}
		// 终检与准入后绑定使用选号结果携带的门（见 responses 同名注释）。
		admissionCtx := service.ContextWithSelectionProfitGate(c.Request.Context(), selection)
		latest, vetoed, reason := h.gatewayService.GatewayProfitControlVetoLatest(admissionCtx, account)
		if vetoed {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			reqLog.Debug("gateway.cc.account_slot_profit_vetoed", zap.Int64("account_id", account.ID), zap.String("reason", reason))
			if fs.RecordProfitVeto(account.ID) == FailoverExhausted {
				reqLog.Warn("gateway.cc.profit_veto_attempts_exhausted", zap.Int("profit_veto_count", fs.ProfitVetoCount()))
				h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "api_error", profitVetoExhaustedMessage)
				return
			}
			continue
		}
		account = latest
		selection.Account = latest
		if selection.ProfitGateActive() {
			if err := h.gatewayService.BindStickySessionAfterProfitAdmission(admissionCtx, apiKey.GroupID, selectionSessionHash, account.ID); err != nil {
				reqLog.Warn("gateway.cc.bind_sticky_session_after_profit_admission_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			}
		}
		accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

		if groupPlatform == service.PlatformGemini && account.Platform != service.PlatformGemini {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			fs.FailedAccountIDs[account.ID] = struct{}{}
			continue
		}

		// 5. Forward request
		writerSizeBeforeForward := c.Writer.Size()
		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		var result *service.ForwardResult
		setActualUpstreamEndpoint(c, "")
		if account.Platform == service.PlatformGemini {
			if h.geminiCompatService == nil {
				h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", "Gemini compatibility service is not configured")
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				return
			}
			result, err = h.geminiCompatService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody)
		} else if shouldUseAntigravityCompat(account) {
			if h.antigravityGatewayService == nil {
				h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", "Antigravity compatibility service is not configured")
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				return
			}
			setActualUpstreamEndpoint(c, EndpointAntigravityGenerateContent)
			result, err = h.antigravityGatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, parsedReq)
		} else {
			result, err = h.gatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, parsedReq)
		}

		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleCCFailoverExhausted(c, failoverErr, true)
					return
				}
				action := fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr)
				switch action {
				case FailoverContinue:
					continue
				case FailoverExhausted:
					if c.Writer.Size() == writerSizeBeforeForward {
						if advanced, advanceErr := advanceGatewayCCRoute(fs.LastFailoverErr); advanced {
							continue
						} else if advanceErr != nil {
							reqLog.Warn("gateway.cc.api_key_group_route_advance_failed", zap.Error(advanceErr))
						}
					}
					h.handleCCFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
					return
				case FailoverCanceled:
					failoverClientGone(c)
					return
				}
			}
			upstreamErrorAlreadyCommunicated := gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
			wroteFallback := false
			if !upstreamErrorAlreadyCommunicated {
				wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
			}
			reqLog.Error("gateway.cc.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
				zap.Error(err),
			)
			return
		}

		// 6. Record usage
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
		sessionID := service.ExtractClientSessionID(c)
		stampForwardRequestedReasoningEffort(result, service.RequestedReasoningEffortFromContext(c.Request.Context()))
		if apiKey.GroupID != nil {
			if state, observeErr := h.gatewayService.RecordAPIKeyRouteResult(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, *apiKey.GroupID, reqModel, routeEndpoint, true); observeErr != nil {
				reqLog.Warn("gateway.cc.api_key_group_health_record_failed", zap.String("state", state), zap.Error(observeErr))
			}
			_ = h.gatewayService.BindAPIKeyGroupSticky(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, stickyModelFamily, stickyEndpointKind, sessionHash, *apiKey.GroupID)
		}
		h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
				Result:             result,
				QuotaPlatform:      quotaPlatform,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				PricingAt:          pricingAt,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.apiKeyService,
				SessionID:          sessionID,
				ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel),
			}); err != nil {
				reqLog.Error("gateway.cc.record_usage_failed",
					zap.Int64("account_id", account.ID),
					zap.Error(err),
				)
			}
		})
		return
	}
}

// chatCompletionsErrorResponse writes an error in OpenAI Chat Completions format.
func (h *GatewayHandler) chatCompletionsErrorResponse(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// handleCCFailoverExhausted writes a failover-exhausted error in CC format.
func (h *GatewayHandler) handleCCFailoverExhausted(c *gin.Context, lastErr *service.UpstreamFailoverError, streamStarted bool) {
	if streamStarted {
		return
	}
	if lastErr != nil {
		copyFailoverRetryAfter(c, lastErr.ResponseHeaders)
	}
	if lastErr != nil && lastErr.IsCredentialFailure() {
		status, message := credentialFailoverClientResponse(lastErr)
		h.chatCompletionsErrorResponse(c, status, "server_error", message)
		return
	}
	if lastErr != nil && lastErr.IsOpenAICapacityShed() && strings.TrimSpace(lastErr.ClientMessage) != "" {
		status := lastErr.ClientStatusCode
		if status <= 0 {
			status = http.StatusServiceUnavailable
		}
		h.chatCompletionsErrorResponse(c, status, "server_error", lastErr.ClientMessage)
		return
	}
	statusCode := http.StatusBadGateway
	if lastErr != nil && lastErr.StatusCode > 0 {
		statusCode = lastErr.StatusCode
	}
	if lastErr != nil && service.IsOpenAISilentRefusalErrorBody(lastErr.ResponseBody) {
		service.SetOpsUpstreamError(c, statusCode, service.OpenAISilentRefusalClientMessage(), "")
		h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", service.OpenAISilentRefusalClientMessage())
		return
	}
	h.chatCompletionsErrorResponse(c, statusCode, "server_error", "All available accounts exhausted")
}
