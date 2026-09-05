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
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ChatCompletions handles OpenAI Chat Completions API requests.
// POST /v1/chat/completions
func (h *OpenAIGatewayHandler) ChatCompletions(c *gin.Context) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)

	requestStart := time.Now()

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
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.chat_completions",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		logRequestBodyReadFailure(reqLog, c.Request, err)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !openAICompatibleTextTargetAllowed(c, apiKey, reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	bindRequestedReasoningEffort(c, body, reqModel)
	routePolicyBody := body
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	if _, err := service.ValidateOpenAIServiceTierField(body); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if service.IsGPTImageGenerationModel(reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "This model is not supported on the Chat Completions endpoint")
		return
	}

	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	if h.rejectIfCyberSessionBlocked(c, apiKey, body, reqModel, cyberBlockFormatChat) {
		return
	}

	// Group-specific mapping is resolved after the actual initial route.
	var channelMapping service.ChannelMappingResult

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	routeSessionHash := sessionHash
	promptCacheKey := h.gatewayService.ExtractSessionID(c, body)
	routeEndpoint := c.Request.URL.Path
	chatCandidateCheck := func(candidate *service.APIKey) error {
		if candidate == nil || candidate.Group == nil {
			return service.ErrNoEligibleAPIKeyRoute
		}
		if !openAICompatibleTextTargetAllowed(c, candidate, reqModel) {
			return fmt.Errorf("candidate group %d does not support this endpoint target", candidate.Group.ID)
		}
		if _, _, policyErr := applyOpenAIReasoningEffortPolicyForRequest(c, candidate, routePolicyBody); policyErr != nil {
			return fmt.Errorf("candidate group %d reasoning policy rejected request: %w", candidate.Group.ID, policyErr)
		}
		allowed, state, healthErr := h.gatewayService.AllowAPIKeyRoute(c.Request.Context(), candidate.ID, candidate.RouteVersion, candidate.Group.ID, reqModel, routeEndpoint)
		if healthErr != nil {
			reqLog.Warn("openai_chat_completions.api_key_group_health_read_failed", zap.Int64("candidate_group_id", candidate.Group.ID), zap.Error(healthErr))
			return nil
		}
		if !allowed {
			return fmt.Errorf("candidate group %d breaker is %s", candidate.Group.ID, state)
		}
		return nil
	}
	stickyModelFamily, stickyEndpointKind := apiKeyRouteStickyScope(apiKey, reqModel, routeEndpoint)
	stickyGroupID, stickyErr := h.gatewayService.GetAPIKeyGroupSticky(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, stickyModelFamily, stickyEndpointKind, routeSessionHash)
	routeStateDegraded := stickyErr != nil
	if stickyErr != nil {
		reqLog.Warn("openai_chat_completions.api_key_group_route_state_degraded", zap.Error(stickyErr))
	}
	stickyRouteSelected := apiKeyMultiGroupRoutingActive(c) && stickyErr == nil && stickyGroupID > 0 && apiKey.GroupID != nil && *apiKey.GroupID == stickyGroupID
	if apiKeyMultiGroupRoutingActive(c) && stickyErr == nil && stickyGroupID > 0 && (apiKey.GroupID == nil || *apiKey.GroupID != stickyGroupID) {
		stickyAPIKey, stickySubscription, activated, activateErr := h.apiKeyRouteRuntime().activateSticky(c, stickyGroupID, chatCandidateCheck)
		if activateErr != nil {
			middleware2.MarkAPIKeyRouteStickySelected(c)
			middleware2.MarkAPIKeyRouteStickyBroken(c)
			reqLog.Warn("openai_chat_completions.api_key_group_sticky_ignored", zap.Int64("sticky_group_id", stickyGroupID), zap.Error(activateErr))
		} else if activated {
			stickyRouteSelected = true
			apiKey = stickyAPIKey
			subscription = stickySubscription
			reqLog.Info("openai_chat_completions.api_key_group_sticky_hit", zap.Int64("sticky_group_id", stickyGroupID))
		}
	}
	if stickyRouteSelected {
		middleware2.MarkAPIKeyRouteStickySelected(c)
	}
	if shouldActivateSmartRoute(c, stickyRouteSelected, routeStateDegraded) {
		smartAPIKey, smartSubscription, ranked, activated, smartErr := h.apiKeyRouteRuntime().activateSmart(c, apiKey, reqModel, routeEndpoint, routeSessionHash, chatCandidateCheck)
		if smartErr != nil {
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "server_error", "No eligible candidate groups", streamStarted)
			return
		}
		if len(ranked) > 0 {
			apiKey = smartAPIKey
			subscription = smartSubscription
			state, _ := middleware2.GetAPIKeyRouteState(c)
			reqLog.Info("openai_chat_completions.api_key_group_smart_order_applied", zap.Bool("initial_group_changed", activated), zap.String("score_version", state.ScoreVersion), zap.Int("candidate_count", len(ranked)))
		}
	}
	apiKey, subscription, initialRouteChanged, err := h.apiKeyRouteRuntime().ensureInitial(c, chatCandidateCheck)
	if err != nil {
		if isAPIKeyRouteAdvanceBillingError(err) {
			status, code, message, retryAfter := billingErrorDetails(errors.Unwrap(err))
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.handleStreamingAwareError(c, status, code, message, streamStarted)
			return
		}
		var policyErr *service.ReasoningEffortOverLimitError
		if errors.As(err, &policyErr) {
			respondOpenAIReasoningEffortPolicyError(c, policyErr, h.errorResponse)
			return
		}
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "server_error", "No eligible candidate groups", streamStarted)
		return
	}
	if initialRouteChanged && stickyRouteSelected {
		middleware2.MarkAPIKeyRouteStickyBroken(c)
	}
	requestPlatform = openAICompatibleRequestPlatform(c.Request.Context(), apiKey)
	reqLog = reqLog.With(zap.Any("actual_group_id", apiKey.GroupID), zap.Int64("route_version", apiKey.RouteVersion))
	if cappedBody, changed, policyErr := applyOpenAIReasoningEffortPolicyForRequest(c, apiKey, routePolicyBody); policyErr != nil {
		respondOpenAIReasoningEffortPolicyError(c, policyErr, h.errorResponse)
		return
	} else if changed {
		body = cappedBody
	} else {
		body = routePolicyBody
	}
	channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai_chat_completions.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	var oauth429FailoverState service.OpenAIOAuth429FailoverState

	// 分组利润控制：chat completions 文本入口请求级装门并固定 pricingAt。
	ccPricingCtx, pricingAt := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), apiKey.GroupID)
	c.Request = c.Request.WithContext(ccPricingCtx)
	advanceChatRoute := func(routeErr error) (bool, error) {
		if !apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput(c, routeErr) {
			return false, nil
		}
		if apiKeyMultiGroupRoutingActive(c) && routeErr != nil {
			middleware2.MarkAPIKeyRouteStickyBroken(c)
		}
		if apiKeyMultiGroupRoutingActive(c) && routeErr != nil && apiKey.GroupID != nil {
			if state, observeErr := h.gatewayService.RecordAPIKeyRouteFailure(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, *apiKey.GroupID, reqModel, routeEndpoint, routeErr); observeErr != nil {
				reqLog.Warn("openai_chat_completions.api_key_group_health_record_failed", zap.String("state", state), zap.Error(observeErr))
			}
		}
		nextAPIKey, nextSubscription, advanced, advanceErr := h.apiKeyRouteRuntime().advance(c, reqModel, routeEndpoint, chatCandidateCheck)
		if !advanced {
			return false, advanceErr
		}
		apiKey = nextAPIKey
		subscription = nextSubscription
		requestPlatform = openAICompatibleRequestPlatform(c.Request.Context(), apiKey)
		body, _, _ = applyOpenAIReasoningEffortPolicyForRequest(c, apiKey, routePolicyBody)
		channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
		newPricingCtx, newPricingAt := h.gatewayService.RebindOpenAIRequestPricingContext(c.Request.Context(), apiKey.GroupID)
		c.Request = c.Request.WithContext(newPricingCtx)
		pricingAt = newPricingAt
		switchCount = 0
		profitVetoCount = 0
		failedAccountIDs = make(map[int64]struct{})
		sameAccountRetryCount = make(map[int64]int)
		lastFailoverErr = nil
		oauth429FailoverState = service.OpenAIOAuth429FailoverState{}
		reqLog.Info("openai_chat_completions.api_key_group_route_switched", zap.Int64p("actual_group_id", apiKey.GroupID))
		return true, nil
	}

	for {
		if failoverClientGone(c) {
			return
		}
		reqLog.Debug("openai_chat_completions.account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
		selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			"",
			sessionHash,
			reqModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportAny,
			service.OpenAIEndpointCapabilityChatCompletions,
			false,
			false,
			true,
			requestPlatform,
		)
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("openai_chat_completions.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			reqLog.Warn("openai_chat_completions.account_select_failed",
				zap.Error(openAICompatibleSelectionErrorForLog(err, requestPlatform)),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
			)
			if len(failedAccountIDs) == 0 {
				if advanced, advanceErr := advanceChatRoute(err); advanced {
					continue
				} else if advanceErr != nil && isAPIKeyRouteAdvanceBillingError(advanceErr) {
					status, code, message, retryAfter := billingErrorDetails(errors.Unwrap(advanceErr))
					if retryAfter > 0 {
						c.Header("Retry-After", strconv.Itoa(retryAfter))
					}
					h.handleStreamingAwareError(c, status, code, message, streamStarted)
					return
				}
				cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel)
				cls = classifySelectionFailureError(err, cls)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
				return
			} else {
				if advanced, advanceErr := advanceChatRoute(apiKeyRouteFailureCause(lastFailoverErr, err)); advanced {
					continue
				} else if advanceErr != nil && isAPIKeyRouteAdvanceBillingError(advanceErr) {
					status, code, message, retryAfter := billingErrorDetails(errors.Unwrap(advanceErr))
					if retryAfter > 0 {
						c.Header("Retry-After", strconv.Itoa(retryAfter))
					}
					h.handleStreamingAwareError(c, status, code, message, streamStarted)
					return
				}
				if lastFailoverErr != nil {
					h.handleFailoverExhausted(c, lastFailoverErr, streamStarted)
				} else {
					h.handleStreamingAwareError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
				}
				return
			}
		}
		if selection == nil || selection.Account == nil {
			if advanced, advanceErr := advanceChatRoute(service.ErrNoAvailableAccounts); advanced {
				continue
			} else if advanceErr != nil && isAPIKeyRouteAdvanceBillingError(advanceErr) {
				status, code, message, retryAfter := billingErrorDetails(errors.Unwrap(advanceErr))
				if retryAfter > 0 {
					c.Header("Retry-After", strconv.Itoa(retryAfter))
				}
				h.handleStreamingAwareError(c, status, code, message, streamStarted)
				return
			}
			cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
			return
		}
		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai_chat_completions.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
		_ = scheduleDecision
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc, slotResult := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, reqStream, &streamStarted, reqLog)
		if slotResult == openAISlotAcquireProfitVetoed {
			// 利润终检否决：排除该账号重新选号；否决次数达上限则按无可用账号终止。
			if !recordOpenAIProfitVeto(failedAccountIDs, account.ID, &profitVetoCount) {
				h.handleOpenAIProfitVetoExhausted(c, streamStarted, reqLog, profitVetoCount)
				return
			}
			continue
		}
		if slotResult != openAISlotAcquireOK {
			return
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()

		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		writerSizeBeforeForward := c.Writer.Size()
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
			}()
			return h.gatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, promptCacheKey, "")
		}()
		var cyberBlockBodyChat []byte
		if service.GetOpsCyberPolicy(c) != nil {
			cyberBlockBodyChat = body
		}
		h.recordCyberPolicyIfMarked(c, apiKey, account, subscription, reqModel, err != nil, cyberBlockBodyChat, clientRequestedUsageFields(c, channelMapping, reqModel, ""), service.HashUsageRequestPayload(body))

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if err == nil && result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		// #5148 对齐：错误返回携带的部分 result（流中断前上游已计量的 usage）照常
		// 入账；failover 错误恒定 result=nil，不会重复计费。
		submitChatUsage := func(res *service.OpenAIForwardResult) {
			if res == nil {
				return
			}
			stampOpenAIRequestedReasoningEffort(res, c)
			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetClientIP(c)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := resolveOpenAIUpstreamEndpoint(c, account, res)
			quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
			sessionID := service.ExtractClientSessionID(c)
			cyberBlocked := service.GetOpsCyberPolicy(c) != nil
			h.submitOpenAIUsageRecordTask(c.Request.Context(), res, func(ctx context.Context) {
				if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
					Result:             res,
					APIKey:             apiKey,
					User:               apiKey.User,
					Account:            account,
					Subscription:       subscription,
					InboundEndpoint:    inboundEndpoint,
					UpstreamEndpoint:   upstreamEndpoint,
					UserAgent:          userAgent,
					IPAddress:          clientIP,
					APIKeyService:      h.apiKeyService,
					QuotaPlatform:      quotaPlatform,
					SessionID:          sessionID,
					ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, res.UpstreamModel),
					PricingAt:          pricingAt,
					CyberBlocked:       cyberBlocked,
				}); err != nil {
					logger.L().With(
						zap.String("component", "handler.openai_gateway.chat_completions"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", apiKey.ID),
						zap.Any("group_id", apiKey.GroupID),
						zap.String("model", reqModel),
						zap.Int64("account_id", account.ID),
					).Error("openai_chat_completions.record_usage_failed", zap.Error(err))
				}
			})
		}
		if err != nil {
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai_chat_completions.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
			} else {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					if failoverClientGone(c) {
						reqLog.Info("openai_chat_completions.failover_aborted_client_disconnected",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						return
					}
					if c.Writer.Size() != writerSizeBeforeForward {
						h.gatewayService.ObserveOpenAIAccountHealthFailure(c.Request.Context(), account, err)
						h.handleFailoverExhausted(c, failoverErr, true)
						return
					}
					if failoverErr.ShouldReportAccountScheduleFailure() {
						h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, nil), false, nil, err)
					}
					if !failoverErr.ShouldRetryNextAccount() {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					// Pool mode: retry on the same account
					if failoverErr.RetryableOnSameAccount {
						retryLimit := effectiveSameAccountRetryLimit(failoverErr, account)
						if sameAccountRetryAllowed(failoverErr, sameAccountRetryCount[account.ID], retryLimit) {
							sameAccountRetryCount[account.ID]++
							retryDelay := sameAccountRetryDelayFor(failoverErr, sameAccountRetryCount[account.ID])
							reqLog.Warn("openai_chat_completions.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
								zap.Duration("retry_delay", retryDelay),
							)
							select {
							case <-c.Request.Context().Done():
								return
							case <-time.After(retryDelay):
							}
							continue
						}
					}
					h.gatewayService.RecordOpenAIAccountSwitch()
					failedAccountIDs[account.ID] = struct{}{}
					lastFailoverErr = failoverErr
					if switchCount >= maxAccountSwitches {
						if advanced, advanceErr := advanceChatRoute(failoverErr); advanced {
							continue
						} else if advanceErr != nil && isAPIKeyRouteAdvanceBillingError(advanceErr) {
							status, code, message, retryAfter := billingErrorDetails(errors.Unwrap(advanceErr))
							if retryAfter > 0 {
								c.Header("Retry-After", strconv.Itoa(retryAfter))
							}
							h.handleStreamingAwareError(c, status, code, message, streamStarted)
							return
						}
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					switchCount++
					if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					reqLog.Warn("openai_chat_completions.upstream_failover_switching",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
						zap.Int("switch_count", switchCount),
						zap.Int("max_switches", maxAccountSwitches),
					)
					continue
				}
				h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, nil), false, nil, err)
				upstreamErrorAlreadyCommunicated := openAIForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
				wroteFallback := false
				if !upstreamErrorAlreadyCommunicated {
					wroteFallback = h.ensureOpenAIStreamReadErrorResponse(c, err, streamStarted)
					if !wroteFallback {
						wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
					}
				}
				reqLog.Warn("openai_chat_completions.forward_failed",
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
					zap.Error(err),
				)
				submitChatUsage(result)
				return
			}
		}
		if result != nil {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, result), true, result.FirstTokenMs)
		} else {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, result), true, nil)
		}

		submitChatUsage(result)
		if apiKeyMultiGroupRoutingActive(c) && apiKey.GroupID != nil {
			if state, observeErr := h.gatewayService.RecordAPIKeyRouteResult(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, *apiKey.GroupID, reqModel, routeEndpoint, true); observeErr != nil {
				reqLog.Warn("openai_chat_completions.api_key_group_success_record_failed", zap.String("state", state), zap.Error(observeErr))
			}
			_ = h.gatewayService.BindAPIKeyGroupSticky(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, stickyModelFamily, stickyEndpointKind, routeSessionHash, *apiKey.GroupID)
		}
		reqLog.Debug("openai_chat_completions.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

// resolveOpenAIUpstreamEndpoint returns the actual upstream endpoint for an
// OpenAI-compatible account. A forwarding result is authoritative because a
// single inbound route may choose raw Chat or a Responses bridge at runtime.
// The account-based derivation remains as a fallback for existing callers and
// forwarding paths that do not report their endpoint yet.
func resolveOpenAIUpstreamEndpoint(c *gin.Context, account *service.Account, result *service.OpenAIForwardResult) string {
	if result != nil {
		if endpoint := strings.TrimSpace(result.UpstreamEndpoint); endpoint != "" {
			return endpoint
		}
	}
	if endpoint := service.GetActualOpenAIUpstreamEndpoint(c); endpoint != "" {
		return endpoint
	}
	if account != nil && account.Type == service.AccountTypeAPIKey &&
		!openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return EndpointChatCompletions
	}
	return GetUpstreamEndpoint(c, account.Platform)
}
