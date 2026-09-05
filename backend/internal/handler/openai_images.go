package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Images handles OpenAI Images API requests.
// POST /v1/images/generations
// POST /v1/images/edits
func (h *OpenAIGatewayHandler) Images(c *gin.Context) {
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
		"handler.openai_gateway.images",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	if isMultipartImagesContentType(c.GetHeader("Content-Type")) {
		setOpsRequestContext(c, "", false)
	} else {
		setOpsRequestContext(c, "", false)
	}

	parsed, err := h.gatewayService.ParseOpenAIImagesRequest(c, body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	requestModel := parsed.Model
	ensureCompositeTargetPlatform(c, apiKey, requestModel)
	clientRequestModel := clientRequestedModel(c, requestModel)
	routingModel := requestModel
	if resolvedModel, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context()); ok {
		routingModel = resolvedModel
	}
	if !compositeTargetPlatformAllowed(c, apiKey, requestModel, service.PlatformOpenAI) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}

	reqLog = reqLog.With(
		zap.String("model", clientRequestModel),
		zap.String("routing_model", routingModel),
		zap.Bool("stream", parsed.Stream),
		zap.Bool("multipart", parsed.Multipart),
		zap.String("capability", string(parsed.RequiredCapability)),
		zap.String("img_quality", parsed.Quality),
		zap.String("img_size", parsed.Size),
	)

	if !service.GroupAllowsImageGeneration(apiKey.Group) && !apiKeyMultiGroupRoutingActive(c) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, requestModel, parsed.ModerationBody()); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	imageReleaseFunc, acquired := h.acquireImageGenerationSlot(c, streamStarted)
	if !acquired {
		return
	}
	if imageReleaseFunc != nil {
		defer imageReleaseFunc()
	}

	setOpsRequestContext(c, clientRequestModel, parsed.Stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(parsed.Stream, false)))

	var channelMapping service.ChannelMappingResult

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, parsed.Stream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, body)
	routeSessionHash := sessionHash
	routeEndpoint := c.Request.URL.Path
	imagesCandidateCheck := func(candidate *service.APIKey) error {
		if candidate == nil || candidate.Group == nil {
			return service.ErrNoEligibleAPIKeyRoute
		}
		if !compositeTargetPlatformAllowed(c, candidate, requestModel, service.PlatformOpenAI) {
			return fmt.Errorf("candidate group %d does not support image target", candidate.Group.ID)
		}
		if !service.GroupAllowsImageGeneration(candidate.Group) {
			return fmt.Errorf("candidate group %d does not allow image generation", candidate.Group.ID)
		}
		allowed, state, healthErr := h.gatewayService.AllowAPIKeyRoute(c.Request.Context(), candidate.ID, candidate.RouteVersion, candidate.Group.ID, requestModel, routeEndpoint)
		if healthErr != nil {
			reqLog.Warn("openai.images.api_key_group_health_read_failed", zap.Int64("candidate_group_id", candidate.Group.ID), zap.Error(healthErr))
			return nil
		}
		if !allowed {
			return fmt.Errorf("candidate group %d breaker is %s", candidate.Group.ID, state)
		}
		return nil
	}
	stickyModelFamily, stickyEndpointKind := apiKeyRouteStickyScope(apiKey, requestModel, routeEndpoint)
	stickyGroupID, stickyErr := h.gatewayService.GetAPIKeyGroupSticky(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, stickyModelFamily, stickyEndpointKind, routeSessionHash)
	routeStateDegraded := stickyErr != nil
	if stickyErr != nil {
		reqLog.Warn("openai.images.api_key_group_route_state_degraded", zap.Error(stickyErr))
	}
	stickyRouteSelected := apiKeyMultiGroupRoutingActive(c) && stickyErr == nil && stickyGroupID > 0 && apiKey.GroupID != nil && *apiKey.GroupID == stickyGroupID
	if apiKeyMultiGroupRoutingActive(c) && stickyErr == nil && stickyGroupID > 0 && (apiKey.GroupID == nil || *apiKey.GroupID != stickyGroupID) {
		stickyAPIKey, stickySubscription, activated, activateErr := h.apiKeyRouteRuntime().activateSticky(c, stickyGroupID, imagesCandidateCheck)
		if activateErr != nil {
			middleware2.MarkAPIKeyRouteStickySelected(c)
			middleware2.MarkAPIKeyRouteStickyBroken(c)
			reqLog.Warn("openai.images.api_key_group_sticky_ignored", zap.Int64("sticky_group_id", stickyGroupID), zap.Error(activateErr))
		} else if activated {
			stickyRouteSelected = true
			apiKey = stickyAPIKey
			subscription = stickySubscription
			reqLog.Info("openai.images.api_key_group_sticky_hit", zap.Int64("sticky_group_id", stickyGroupID))
		}
	}
	if stickyRouteSelected {
		middleware2.MarkAPIKeyRouteStickySelected(c)
	}
	if shouldActivateSmartRoute(c, stickyRouteSelected, routeStateDegraded) {
		smartAPIKey, smartSubscription, ranked, activated, smartErr := h.apiKeyRouteRuntime().activateSmart(c, apiKey, requestModel, routeEndpoint, routeSessionHash, imagesCandidateCheck)
		if smartErr != nil {
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "server_error", "No eligible candidate groups", streamStarted)
			return
		}
		if len(ranked) > 0 {
			apiKey = smartAPIKey
			subscription = smartSubscription
			state, _ := middleware2.GetAPIKeyRouteState(c)
			reqLog.Info("openai.images.api_key_group_smart_order_applied", zap.Bool("initial_group_changed", activated), zap.String("score_version", state.ScoreVersion), zap.Int("candidate_count", len(ranked)))
		}
	}
	apiKey, subscription, initialRouteChanged, err := h.apiKeyRouteRuntime().ensureInitial(c, imagesCandidateCheck)
	if err != nil {
		if isAPIKeyRouteAdvanceBillingError(err) {
			status, code, message, retryAfter := billingErrorDetails(errors.Unwrap(err))
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.handleStreamingAwareError(c, status, code, message, streamStarted)
			return
		}
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "server_error", "No eligible candidate groups", streamStarted)
		return
	}
	if initialRouteChanged && stickyRouteSelected {
		middleware2.MarkAPIKeyRouteStickyBroken(c)
	}
	reqLog = reqLog.With(zap.Any("actual_group_id", apiKey.GroupID), zap.Int64("route_version", apiKey.RouteVersion))
	channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, routingModel)

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai.images.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	requestCtx := service.WithOpenAIImagesEndpoint(service.WithOpenAIImageGenerationIntent(c.Request.Context()))

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	stopJSONKeepalive := func() {}
	jsonKeepaliveStarted := false
	defer func() { stopJSONKeepalive() }()
	var oauth429FailoverState service.OpenAIOAuth429FailoverState
	advanceImagesRoute := func(routeErr error) (bool, error) {
		// Image generation/edit may already have been accepted upstream even
		// when no response bytes arrived. Cross-group replay is therefore only
		// allowed for pre-upstream capacity/selection failures.
		if routeErr != nil && !errors.Is(routeErr, service.ErrNoAvailableAccounts) {
			return false, nil
		}
		if !apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput(c, routeErr) {
			return false, nil
		}
		if apiKeyMultiGroupRoutingActive(c) && routeErr != nil && apiKey.GroupID != nil {
			if state, observeErr := h.gatewayService.RecordAPIKeyRouteFailure(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, *apiKey.GroupID, requestModel, routeEndpoint, routeErr); observeErr != nil {
				reqLog.Warn("openai.images.api_key_group_health_record_failed", zap.String("state", state), zap.Error(observeErr))
			}
		}
		nextAPIKey, nextSubscription, advanced, advanceErr := h.apiKeyRouteRuntime().advance(c, requestModel, routeEndpoint, imagesCandidateCheck)
		if !advanced {
			return false, advanceErr
		}
		apiKey = nextAPIKey
		subscription = nextSubscription
		channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, routingModel)
		requestCtx = service.WithOpenAIImagesEndpoint(service.WithOpenAIImageGenerationIntent(c.Request.Context()))
		profitVetoCount = 0
		failedAccountIDs = make(map[int64]struct{})
		sameAccountRetryCount = make(map[int64]int)
		lastFailoverErr = nil
		switchCount = 0
		oauth429FailoverState = service.OpenAIOAuth429FailoverState{}
		reqLog.Info("openai.images.api_key_group_route_switched", zap.Int64p("actual_group_id", apiKey.GroupID))
		return true, nil
	}

	for {
		reqLog.Debug("openai.images.account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
		selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForImages(
			requestCtx,
			apiKey.GroupID,
			sessionHash,
			routingModel,
			failedAccountIDs,
			parsed.RequiredCapability,
		)
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("openai.images.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			reqLog.Warn("openai.images.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
			)
			if len(failedAccountIDs) == 0 {
				if advanced, advanceErr := advanceImagesRoute(err); advanced {
					continue
				} else if advanceErr != nil && isAPIKeyRouteAdvanceBillingError(advanceErr) {
					status, code, message, retryAfter := billingErrorDetails(errors.Unwrap(advanceErr))
					if retryAfter > 0 {
						c.Header("Retry-After", strconv.Itoa(retryAfter))
					}
					h.handleStreamingAwareError(c, status, code, message, streamStarted)
					return
				}
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, clientRequestModel, routingModel, service.PlatformOpenAI)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				message := cls.Message
				if !cls.ModelNotFound {
					message = "No available compatible accounts"
				}
				h.handleStreamingAwareError(c, cls.Status, cls.ErrType, message, streamStarted)
				return
			}
			if lastFailoverErr != nil {
				h.handleFailoverExhausted(c, lastFailoverErr, streamStarted)
			} else {
				h.handleFailoverExhaustedSimple(c, 502, streamStarted)
			}
			return
		}
		if selection == nil || selection.Account == nil {
			if advanced, advanceErr := advanceImagesRoute(service.ErrNoAvailableAccounts); advanced {
				continue
			} else if advanceErr != nil && isAPIKeyRouteAdvanceBillingError(advanceErr) {
				status, code, message, retryAfter := billingErrorDetails(errors.Unwrap(advanceErr))
				if retryAfter > 0 {
					c.Header("Retry-After", strconv.Itoa(retryAfter))
				}
				h.handleStreamingAwareError(c, status, code, message, streamStarted)
				return
			}
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, clientRequestModel, routingModel, service.PlatformOpenAI)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			message := cls.Message
			if !cls.ModelNotFound {
				message = "No available compatible accounts"
			}
			h.handleStreamingAwareError(c, cls.Status, cls.ErrType, message, streamStarted)
			return
		}

		reqLog.Debug("openai.images.account_schedule_decision",
			zap.String("layer", scheduleDecision.Layer),
			zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
			zap.Int("candidate_count", scheduleDecision.CandidateCount),
			zap.Int("top_k", scheduleDecision.TopK),
			zap.Int64("latency_ms", scheduleDecision.LatencyMs),
			zap.Float64("load_skew", scheduleDecision.LoadSkew),
		)

		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai.images.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc, slotResult := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, parsed.Stream, &streamStarted, reqLog)
		if slotResult == openAISlotAcquireProfitVetoed {
			// Images 调度不装利润门，此分支实际不可达；防御性排除重选并受同一否决上限约束。
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
		if !parsed.Stream && !jsonKeepaliveStarted {
			stopJSONKeepalive = service.StartOpenAIImagesJSONKeepalive(c, h.openAIImagesJSONKeepaliveInterval())
			jsonKeepaliveStarted = true
		}
		forwardStart := time.Now()
		writerSizeBeforeForward := service.OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c)
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
			}()
			return h.gatewayService.ForwardImages(requestCtx, c, account, body, parsed, channelMapping.MappedModel)
		}()
		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		if err != nil {
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai.images.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
			} else {
				var imageUpstreamErr *service.OpenAIImagesUpstreamError
				if errors.As(err, &imageUpstreamErr) {
					retryableServerError := service.IsOpenAIImagesRetryableUpstreamError(imageUpstreamErr)
					if retryableServerError {
						h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, requestModel, false, result), false, nil, err)
					} else {
						h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, requestModel, false, result), true, nil)
					}
					logEvent := "openai.images.upstream_user_error"
					if retryableServerError {
						logEvent = "openai.images.upstream_server_error_after_flush"
					}
					reqLog.Warn(logEvent,
						zap.Int64("account_id", account.ID),
						zap.Int("status_code", imageUpstreamErr.StatusCode),
						zap.String("error_type", imageUpstreamErr.ErrorType),
						zap.String("error_code", imageUpstreamErr.Code),
						zap.Error(err),
					)
					return
				}
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, requestModel, false, result), false, nil, err)
					if service.OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c) != writerSizeBeforeForward {
						reqLog.Warn("openai.images.upstream_failover_skipped_after_flush",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						h.handleFailoverExhausted(c, failoverErr, true)
						return
					}
					if failoverClientGone(c) {
						reqLog.Info("openai.images.failover_aborted_client_disconnected",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						return
					}
					if failoverErr.RetryableOnSameAccount {
						retryLimit := effectiveSameAccountRetryLimit(failoverErr, account)
						if sameAccountRetryAllowed(failoverErr, sameAccountRetryCount[account.ID], retryLimit) {
							sameAccountRetryCount[account.ID]++
							retryDelay := sameAccountRetryDelayFor(failoverErr, sameAccountRetryCount[account.ID])
							reqLog.Warn("openai.images.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
								zap.Duration("retry_delay", retryDelay),
							)
							select {
							case <-requestCtx.Done():
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
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					switchCount++
					if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					reqLog.Warn("openai.images.upstream_failover_switching",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
						zap.Int("switch_count", switchCount),
						zap.Int("max_switches", maxAccountSwitches),
					)
					continue
				}
				h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, requestModel, false, result), false, nil, err)
				upstreamErrorAlreadyCommunicated := openAIForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
				wroteFallback := false
				if !upstreamErrorAlreadyCommunicated {
					wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
				}
				fields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
					zap.Error(err),
				}
				if shouldLogOpenAIForwardFailureAsWarn(c, wroteFallback) {
					reqLog.Warn("openai.images.forward_failed", fields...)
					return
				}
				reqLog.Error("openai.images.forward_failed", fields...)
				return
			}
		}
		if result != nil {
			// 排除 spark 影子:其 codex_* 仅由 QueryUsage(/wham/usage bengalfox)更新(外审第7轮 P1)。
			if account.Type == service.AccountTypeOAuth && !account.IsShadow() {
				h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(c.Request.Context(), account.ID, result.ResponseHeaders)
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, requestModel, false, result), true, result.FirstTokenMs)
		} else {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, requestModel, false, result), true, nil)
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		if parsed.Multipart {
			requestPayloadHash = service.HashUsageRequestPayload([]byte(parsed.StickySessionSeed()))
		}
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)

		upstreamModel := ""
		if result != nil {
			upstreamModel = result.UpstreamModel
		}
		sessionID := service.ExtractClientSessionID(c)
		h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
				Result:             result,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.apiKeyService,
				QuotaPlatform:      quotaPlatform,
				SessionID:          sessionID,
				ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, requestModel, upstreamModel),
			}); err != nil {
				logger.L().With(
					zap.String("component", "handler.openai_gateway.images"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", clientRequestModel),
					zap.Int64("account_id", account.ID),
				).Error("openai.images.record_usage_failed", zap.Error(err))
			}
		})
		if apiKeyMultiGroupRoutingActive(c) && apiKey.GroupID != nil {
			if state, observeErr := h.gatewayService.RecordAPIKeyRouteResult(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, *apiKey.GroupID, requestModel, routeEndpoint, true); observeErr != nil {
				reqLog.Warn("openai.images.api_key_group_success_record_failed", zap.String("state", state), zap.Error(observeErr))
			}
			_ = h.gatewayService.BindAPIKeyGroupSticky(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, stickyModelFamily, stickyEndpointKind, routeSessionHash, *apiKey.GroupID)
		}

		reqLog.Debug("openai.images.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

func (h *OpenAIGatewayHandler) openAIImagesJSONKeepaliveInterval() time.Duration {
	if h.cfg == nil || h.cfg.Gateway.ImageNonstreamKeepaliveInterval <= 0 {
		return 0
	}
	return time.Duration(h.cfg.Gateway.ImageNonstreamKeepaliveInterval) * time.Second
}

func isMultipartImagesContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/form-data")
}
