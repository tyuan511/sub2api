package handler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// apiKeyRouteRuntime owns the dependencies needed to activate a secondary
// candidate. It is intentionally small so every protocol can share identical
// subscription and billing semantics while retaining its own response format.
type apiKeyRouteRuntime struct {
	subscriptions *service.SubscriptionService
	billing       *service.BillingCacheService
}

type apiKeyRouteAdvanceError struct {
	Last    error
	Billing bool
}

type apiKeyRouteCandidateCheck func(*service.APIKey) error
type apiKeyRouteOwnerCheck func(groupID int64) (bool, error)

// activateAPIKeyRouteOwnedGroup resolves upstream state whose identity is
// namespaced by physical group (for example Responses previous_response_id).
// The owner lookup happens before sticky/smart selection and pins the request
// to that exact configured candidate; callers must subsequently use
// validateInitial and disable all cross-group replay.
func activateAPIKeyRouteOwnedGroup(c *gin.Context, ownerCheck apiKeyRouteOwnerCheck) (*service.APIKey, bool, error) {
	state, ok := middleware2.GetAPIKeyRouteState(c)
	if !ok || state.Plan == nil || ownerCheck == nil {
		return nil, false, nil
	}
	for index, candidate := range state.Plan.Candidates {
		owned, err := ownerCheck(candidate.GroupID)
		if err != nil {
			return nil, false, err
		}
		if !owned {
			continue
		}
		apiKey, ok := state.Plan.APIKeyForCandidate(index)
		if !ok {
			return nil, false, service.ErrNoEligibleAPIKeyRoute
		}
		if state.Index == index {
			return apiKey, false, nil
		}
		actual, activated := middleware2.ActivateInitialAPIKeyRouteIndex(c, index)
		if !activated {
			return nil, false, service.ErrNoEligibleAPIKeyRoute
		}
		return actual, true, nil
	}
	return nil, false, nil
}

// ensureInitial validates the currently selected candidate before the first
// billing counter, capacity lease, or upstream call. Hard-rejected candidates
// are removed from this request's frozen order without being counted as
// failovers, so they cannot reappear after a later runtime failure.
func (r apiKeyRouteRuntime) ensureInitial(c *gin.Context, candidateCheck apiKeyRouteCandidateCheck) (*service.APIKey, *service.UserSubscription, bool, error) {
	return r.ensureInitialWithFallback(c, candidateCheck, true)
}

// validateInitial is the state-owned continuation variant: it validates and
// loads the already selected group but must never migrate to another group.
func (r apiKeyRouteRuntime) validateInitial(c *gin.Context, candidateCheck apiKeyRouteCandidateCheck) (*service.APIKey, *service.UserSubscription, error) {
	apiKey, subscription, _, err := r.ensureInitialWithFallback(c, candidateCheck, false)
	return apiKey, subscription, err
}

func (r apiKeyRouteRuntime) ensureInitialWithFallback(c *gin.Context, candidateCheck apiKeyRouteCandidateCheck, allowFallback bool) (*service.APIKey, *service.UserSubscription, bool, error) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		return nil, nil, false, service.ErrNoEligibleAPIKeyRoute
	}
	// Endpoint validation and subscription checks already belong to the
	// legacy handler/auth path. Do not apply candidate-only restrictions,
	// reload subscriptions, or replace billing context for a fixed key.
	state, routed := middleware2.GetAPIKeyRouteState(c)
	if !routed || !state.Plan.RoutingEnabled {
		subscription, _ := middleware2.GetSubscriptionFromContext(c)
		return apiKey, subscription, false, nil
	}
	changed := false
	var lastErr error
	lastErrBilling := false
	for {
		lastErr = nil
		lastErrBilling = false
		if candidateCheck != nil {
			lastErr = candidateCheck(apiKey)
		}
		var subscription *service.UserSubscription
		if lastErr == nil {
			var err error
			subscription, err = r.subscriptionForCandidate(c.Request.Context(), apiKey)
			if err != nil {
				lastErr = err
				lastErrBilling = true
			}
		}
		if lastErr == nil {
			middleware2.SetSubscriptionInContext(c, subscription)
			c.Request = c.Request.WithContext(service.WithGatewayTokenRequestBillingGroup(c.Request.Context(), apiKey.Group))
			return apiKey, subscription, changed, nil
		}

		if !allowFallback {
			return nil, nil, changed, &apiKeyRouteAdvanceError{Last: lastErr, Billing: lastErrBilling}
		}
		next, advanced := middleware2.RejectInitialAPIKeyRoute(c)
		if !advanced {
			return nil, nil, changed, &apiKeyRouteAdvanceError{Last: lastErr, Billing: lastErrBilling}
		}
		apiKey = next
		changed = true
	}
}

func apiKeyMultiGroupRoutingActive(c *gin.Context) bool {
	state, ok := middleware2.GetAPIKeyRouteState(c)
	return ok && !state.Locked && state.Plan.RoutingEnabled && state.Plan.Len() > 1
}

// shouldActivateSmartRoute is the session-isolation gate shared by every text
// protocol: a healthy existing group-sticky session never enters canary/shadow
// strategy selection; only a session without an accepted sticky target does.
func shouldActivateSmartRoute(c *gin.Context, stickyRouteSelected, routeStateDegraded bool) bool {
	return !stickyRouteSelected && !routeStateDegraded && apiKeyMultiGroupRoutingActive(c)
}

// apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput is the shared HTTP/SSE
// cross-group replay latch. A replay-safe failure is still forbidden once any
// semantic response bytes have reached the client. Concurrency wait heartbeats
// are transport keepalives and do not commit an upstream choice.
func apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput(c *gin.Context, err error) bool {
	if !apiKeyRouteFailureAllowsAdvance(err) || c == nil || c.Writer == nil {
		return false
	}
	if !c.Writer.Written() && c.Writer.Size() <= 0 {
		return true
	}
	return gatewayStreamHasOnlyHeartbeats(c)
}

func (e *apiKeyRouteAdvanceError) Error() string {
	if e == nil || e.Last == nil {
		return "no eligible API key candidate group remains"
	}
	return fmt.Sprintf("no eligible API key candidate group remains: %v", e.Last)
}

func (e *apiKeyRouteAdvanceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Last
}

// advance activates the next candidate that passes actual-group subscription
// and billing checks. Candidate-local failures are skipped, allowing the next
// configured group to serve the request; global/key RPM is not counted again.
func (r apiKeyRouteRuntime) advance(c *gin.Context, model, endpoint string, candidateCheck apiKeyRouteCandidateCheck) (*service.APIKey, *service.UserSubscription, bool, error) {
	var lastErr error
	lastErrBilling := false
	for {
		apiKey, ok := middleware2.AdvanceAPIKeyRoute(c)
		if !ok {
			service.RecordAPIKeyRoutingTerminalFailure(c.Request.Context(),
				service.NormalizeAPIKeyRoutingModelFamily(routingPlatformFromContext(c), model),
				service.NormalizeAPIKeyRoutingEndpointKind(endpoint))
			if lastErr != nil {
				return nil, nil, false, &apiKeyRouteAdvanceError{Last: lastErr, Billing: lastErrBilling}
			}
			return nil, nil, false, nil
		}
		if candidateCheck != nil {
			if err := candidateCheck(apiKey); err != nil {
				lastErr = err
				lastErrBilling = false
				continue
			}
		}
		subscription, err := r.subscriptionForCandidate(c.Request.Context(), apiKey)
		if err != nil {
			lastErr = err
			lastErrBilling = true
			continue
		}
		if r.billing != nil {
			platform := service.QuotaPlatform(c.Request.Context(), apiKey)
			if err := r.billing.CheckRouteSwitchBillingEligibility(c.Request.Context(), apiKey.User, apiKey.Group, subscription, platform); err != nil {
				lastErr = err
				lastErrBilling = true
				continue
			}
		}
		middleware2.SetSubscriptionInContext(c, subscription)
		c.Request = c.Request.WithContext(service.WithGatewayTokenRequestBillingGroup(c.Request.Context(), apiKey.Group))
		return apiKey, subscription, true, nil
	}
}

// activateSticky moves directly to a configured sticky group after validating
// the same hard request, subscription, and billing constraints as sequential
// failover. Stale or removed sticky targets are ignored safely.
func (r apiKeyRouteRuntime) activateSticky(c *gin.Context, groupID int64, candidateCheck apiKeyRouteCandidateCheck) (*service.APIKey, *service.UserSubscription, bool, error) {
	candidate, index, ok := middleware2.FindAPIKeyRouteGroup(c, groupID)
	if !ok {
		return nil, nil, false, nil
	}
	state, _ := middleware2.GetAPIKeyRouteState(c)
	if state != nil && index == state.Index {
		subscription, _ := middleware2.GetSubscriptionFromContext(c)
		return candidate, subscription, false, nil
	}
	if candidateCheck != nil {
		if err := candidateCheck(candidate); err != nil {
			return nil, nil, false, err
		}
	}
	subscription, err := r.subscriptionForCandidate(c.Request.Context(), candidate)
	if err != nil {
		return nil, nil, false, err
	}
	actual, ok := middleware2.ActivateInitialAPIKeyRouteIndex(c, index)
	if !ok {
		return nil, nil, false, service.ErrNoEligibleAPIKeyRoute
	}
	middleware2.SetSubscriptionInContext(c, subscription)
	c.Request = c.Request.WithContext(service.WithGatewayTokenRequestBillingGroup(c.Request.Context(), actual.Group))
	return actual, subscription, true, nil
}

// activateSmart freezes one score version and chooses the first request-valid
// group before billing/account selection. A missing or stale snapshot is a
// deterministic sequential fallback, not a request failure.
func (r apiKeyRouteRuntime) activateSmart(c *gin.Context, apiKey *service.APIKey, model, endpoint, sessionHash string, candidateCheck apiKeyRouteCandidateCheck) (*service.APIKey, *service.UserSubscription, []service.APIKeyRoutingCandidateScore, bool, error) {
	state, ok := middleware2.GetAPIKeyRouteState(c)
	if !ok || !state.Plan.RoutingEnabled || apiKey == nil || apiKey.Group == nil || state.Plan.ScheduleMode != service.APIKeyScheduleModeSmart || state.Plan.SmartPreference == nil {
		return apiKey, nil, nil, false, nil
	}
	scope := service.APIKeyRoutingScoreScope{
		Platform:     apiKey.Group.Platform,
		ModelFamily:  service.NormalizeAPIKeyRoutingModelFamily(apiKey.Group.Platform, model),
		EndpointKind: service.NormalizeAPIKeyRoutingEndpointKind(endpoint),
	}
	strategyScope := service.RoutingArtifactScope{
		ArtifactKind: service.RoutingArtifactStrategy, Platform: scope.Platform, ModelFamily: scope.ModelFamily,
		EndpointKind: scope.EndpointKind, Preference: state.Plan.SmartPreference,
	}
	userID := int64(0)
	if apiKey.User != nil {
		userID = apiKey.User.ID
	}
	selection := service.SelectDefaultAPIKeyRoutingStrategy(strategyScope, userID, apiKey.ID)
	selection.Policy = service.ApplyAPIKeyRoutingControls(selection.Policy, apiKey)
	if selection.ShadowPolicy != nil {
		shadow := service.ApplyAPIKeyRoutingControls(*selection.ShadowPolicy, apiKey)
		selection.ShadowPolicy = &shadow
	}
	snapshot, found := service.DefaultAPIKeyRoutingScoreStore().Lookup(scope, time.Duration(selection.Policy.MaxSnapshotAgeSeconds)*time.Second, time.Now())
	if found {
		service.DefaultRoutingRuntimeMetrics().RecordScoreSnapshot(true, time.Since(snapshot.GeneratedAt))
	} else {
		service.DefaultRoutingRuntimeMetrics().RecordScoreSnapshot(false, 0)
	}
	if !found {
		return apiKey, nil, nil, false, nil
	}
	rankingStarted := time.Now()
	var userRates map[int64]float64
	if apiKey.User != nil {
		userRates = apiKey.User.GroupRates
	}
	projectedSnapshot := service.ProjectAPIKeyRoutingScoreSnapshot(state.Plan.Candidates, snapshot, userRates)
	baselineRanked := service.RankAPIKeyRoutingCandidatesWithPolicy(state.Plan.Candidates, projectedSnapshot, selection.Policy)
	statisticallyExcluded := make(map[int64]string)
	// A breaker-owned recovery attempt is distinct from ordinary admission.
	// Only the subsequent atomic health check can grant its bounded probe.
	for index := range baselineRanked {
		if !strings.HasPrefix(baselineRanked[index].Exclusion, "success_rate_below_") {
			continue
		}
		statisticallyExcluded[baselineRanked[index].GroupID] = baselineRanked[index].Exclusion
		if candidateCheck != nil {
			baselineRanked[index].Eligible = true
			baselineRanked[index].Exclusion = ""
		}
	}
	baselineRanked = applyAPIKeyRoutingCandidateCheck(c, baselineRanked, candidateCheck)
	for index := range baselineRanked {
		if reason, excluded := statisticallyExcluded[baselineRanked[index].GroupID]; excluded && !service.APIKeyRouteRecoveryAdmitted(c.Request.Context(), scope.ModelFamily, scope.EndpointKind, baselineRanked[index].GroupID) {
			baselineRanked[index].Eligible = false
			baselineRanked[index].Exclusion = reason
		}
	}
	baselineRanked = applyAPIKeyRoutingBreakerEligibility(c, scope.ModelFamily, scope.EndpointKind, baselineRanked)
	eligible := make(map[int64]bool, len(baselineRanked))
	for _, score := range baselineRanked {
		if score.Eligible {
			if breaker, found := service.APIKeyRoutePreloadedBreaker(c.Request.Context(), scope.ModelFamily, scope.EndpointKind, score.GroupID); found && breaker.State == service.APIKeyRouteBreakerHalfOpen {
				// HALF_OPEN remains a rule candidate so the later atomic admission
				// can grant one probe, but an ungranted probe is not model input.
				continue
			}
			eligible[score.GroupID] = true
		}
	}
	learning := service.ApplyDefaultAPIKeyRoutingLearning(strategyScope, apiKey.ID, userID, selection.ExperimentID, projectedSnapshot, eligible, time.Now())
	ranked := service.RankAPIKeyRoutingCandidatesWithPolicy(state.Plan.Candidates, learning.Snapshot, selection.Policy)
	ranked = applyAPIKeyRoutingBaselineEligibility(ranked, baselineRanked)
	for index := range ranked {
		if !strings.HasPrefix(ranked[index].Exclusion, "success_rate_below_") {
			continue
		}
		for _, base := range baselineRanked {
			if base.GroupID == ranked[index].GroupID && base.Eligible {
				ranked[index] = base
				break
			}
		}
	}
	ranked = annotateAPIKeyRoutingLearning(ranked, baselineRanked, learning.Personalization.AppliedGroups)
	ranked = service.DefaultAPIKeyRoutingOrderStabilizer().Stabilize(
		apiKey.ID, apiKey.RouteVersion, scope, *state.Plan.SmartPreference, sessionHash,
		ranked, selection.Policy.Stability, time.Now(),
	)
	// Consume a granted recovery admission rather than leaving an unused probe
	// lease behind. activateSmart only runs for new/non-sticky sessions.
	isRecovery := func(score service.APIKeyRoutingCandidateScore) bool {
		return score.Eligible && service.APIKeyRouteRecoveryAdmitted(c.Request.Context(), scope.ModelFamily, scope.EndpointKind, score.GroupID)
	}
	sort.SliceStable(ranked, func(i, j int) bool { return isRecovery(ranked[i]) && !isRecovery(ranked[j]) })
	middleware2.SetAPIKeyRouteScoreFacts(c, ranked)
	var shadowRanked []service.APIKeyRoutingCandidateScore
	shadowPolicy := selection.Policy
	if selection.ShadowPolicy != nil {
		shadowPolicy = *selection.ShadowPolicy
	}
	shadowSnapshot := learning.ShadowSnapshot
	if shadowSnapshot == nil && selection.ShadowPolicy != nil && selection.ShadowPolicy.Version != selection.Policy.Version {
		shadowSnapshot = projectedSnapshot
	}
	if shadowSnapshot != nil {
		shadowRanked = service.RankAPIKeyRoutingCandidatesWithPolicy(state.Plan.Candidates, shadowSnapshot, shadowPolicy)
		shadowRanked = applyAPIKeyRoutingBaselineEligibility(shadowRanked, baselineRanked)
	}
	service.DefaultRoutingRuntimeMetrics().RecordPhaseLatency(service.RoutingLatencyPhaseSmartRanking, time.Since(rankingStarted))
	ordered := make([]int64, 0, len(ranked))
	for _, score := range ranked {
		if !score.Eligible {
			continue
		}
		_, _, exists := middleware2.FindAPIKeyRouteGroup(c, score.GroupID)
		if !exists {
			continue
		}
		ordered = append(ordered, score.GroupID)
	}
	if len(ordered) == 0 {
		return nil, nil, ranked, false, service.ErrNoEligibleAPIKeyRoute
	}

	for len(ordered) > 0 {
		actual, applied := middleware2.ApplyAPIKeyRouteOrder(c, ordered, snapshot.Version, selection.Policy.Version, snapshot.FeatureVersion, snapshot.GeneratedAt)
		if !applied {
			return nil, nil, ranked, false, service.ErrNoEligibleAPIKeyRoute
		}
		subscription, err := r.subscriptionForCandidate(c.Request.Context(), actual)
		if err != nil {
			ordered = ordered[1:]
			continue
		}
		middleware2.SetSubscriptionInContext(c, subscription)
		c.Request = c.Request.WithContext(service.WithGatewayTokenRequestBillingGroup(c.Request.Context(), actual.Group))
		middleware2.SetAPIKeyRouteStrategyAssignment(c, selection, learning.Snapshot.ModelVersion)
		recovery := service.APIKeyRouteRecoveryAdmitted(c.Request.Context(), scope.ModelFamily, scope.EndpointKind, actual.Group.ID)
		// Probes are health interventions, not normal scoring-policy decisions.
		// Keep their outcome/usage facts, but exclude them from policy replay data.
		if !recovery {
			service.RecordAPIKeyRoutingDecision(c.Request.Context(), snapshot.ModelFamily, snapshot.EndpointKind)
		}
		if !recovery && len(shadowRanked) > 0 {
			service.RecordAPIKeyRoutingShadowDecision(c.Request.Context(), shadowPolicy, shadowSnapshot, shadowRanked)
		}
		changed := apiKey.GroupID == nil || actual.GroupID == nil || *apiKey.GroupID != *actual.GroupID
		return actual, subscription, ranked, changed, nil
	}
	return nil, nil, ranked, false, service.ErrNoEligibleAPIKeyRoute
}

func routingPlatformFromContext(c *gin.Context) string {
	if c == nil {
		return "unknown"
	}
	if apiKey, ok := middleware2.GetAPIKeyFromContext(c); ok && apiKey != nil && apiKey.Group != nil {
		return apiKey.Group.Platform
	}
	return "unknown"
}

func apiKeyRouteStickyScope(apiKey *service.APIKey, model, endpoint string) (string, string) {
	platform := ""
	if apiKey != nil && apiKey.Group != nil {
		platform = apiKey.Group.Platform
	}
	return service.NormalizeAPIKeyRoutingModelFamily(platform, model), service.NormalizeAPIKeyRoutingEndpointKind(endpoint)
}

func applyAPIKeyRoutingCandidateCheck(c *gin.Context, ranked []service.APIKeyRoutingCandidateScore, candidateCheck apiKeyRouteCandidateCheck) []service.APIKeyRoutingCandidateScore {
	if candidateCheck == nil {
		return ranked
	}
	for index := range ranked {
		if !ranked[index].Eligible {
			continue
		}
		candidate, _, exists := middleware2.FindAPIKeyRouteGroup(c, ranked[index].GroupID)
		if !exists {
			ranked[index].Eligible = false
			ranked[index].Exclusion = "candidate_not_configured"
			continue
		}
		if err := candidateCheck(candidate); err != nil {
			ranked[index].Eligible = false
			ranked[index].Exclusion = "request_capability_rejected"
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Eligible && !ranked[j].Eligible
	})
	return ranked
}

func applyAPIKeyRoutingBreakerEligibility(c *gin.Context, modelFamily, endpointKind string, ranked []service.APIKeyRoutingCandidateScore) []service.APIKeyRoutingCandidateScore {
	if c == nil || c.Request == nil {
		return ranked
	}
	for index := range ranked {
		if !ranked[index].Eligible {
			continue
		}
		breaker, found := service.APIKeyRoutePreloadedBreaker(c.Request.Context(), modelFamily, endpointKind, ranked[index].GroupID)
		if !found {
			continue
		}
		if breaker.State == service.APIKeyRouteBreakerOpen {
			ranked[index].Eligible = false
			ranked[index].Exclusion = "breaker_" + breaker.State
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Eligible && !ranked[j].Eligible })
	return ranked
}

// applyAPIKeyRoutingBaselineEligibility preserves every rule/capability hard
// rejection computed before local learning. A model can change component values,
// but it can never put an excluded group back into the candidate set.
func applyAPIKeyRoutingBaselineEligibility(ranked, baseline []service.APIKeyRoutingCandidateScore) []service.APIKeyRoutingCandidateScore {
	baselineByGroup := make(map[int64]service.APIKeyRoutingCandidateScore, len(baseline))
	for _, score := range baseline {
		baselineByGroup[score.GroupID] = score
	}
	for index := range ranked {
		base, exists := baselineByGroup[ranked[index].GroupID]
		if !exists || !base.Eligible {
			ranked[index].Eligible = false
			if exists {
				ranked[index].Exclusion = base.Exclusion
			} else {
				ranked[index].Exclusion = "candidate_not_in_baseline"
			}
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Eligible && !ranked[j].Eligible
	})
	return ranked
}

func annotateAPIKeyRoutingLearning(ranked, baseline []service.APIKeyRoutingCandidateScore, personalizationWeights map[int64]float64) []service.APIKeyRoutingCandidateScore {
	baselineByGroup := make(map[int64]float64, len(baseline))
	for _, score := range baseline {
		baselineByGroup[score.GroupID] = score.Score
	}
	for index := range ranked {
		base, exists := baselineByGroup[ranked[index].GroupID]
		if !exists {
			continue
		}
		baseCopy := base
		adjustment := ranked[index].Score - base
		ranked[index].SharedBaselineScore = &baseCopy
		ranked[index].LearningAdjustment = &adjustment
		if weight, applied := personalizationWeights[ranked[index].GroupID]; applied {
			weightCopy := weight
			ranked[index].PersonalizationWeight = &weightCopy
		}
	}
	return ranked
}

func (h *GatewayHandler) apiKeyRouteRuntime() apiKeyRouteRuntime {
	return apiKeyRouteRuntime{subscriptions: h.subscriptionService, billing: h.billingCacheService}
}

func (h *OpenAIGatewayHandler) apiKeyRouteRuntime() apiKeyRouteRuntime {
	return apiKeyRouteRuntime{subscriptions: h.subscriptionService, billing: h.billingCacheService}
}

func (r apiKeyRouteRuntime) subscriptionForCandidate(ctx context.Context, apiKey *service.APIKey) (*service.UserSubscription, error) {
	if apiKey == nil || apiKey.Group == nil || !apiKey.Group.IsSubscriptionType() {
		return nil, nil
	}
	if r.subscriptions == nil || apiKey.User == nil {
		return nil, service.ErrSubscriptionNotFound
	}
	return r.subscriptions.GetEligibleSubscription(ctx, apiKey.User.ID, apiKey.Group)
}

func isAPIKeyRouteAdvanceBillingError(err error) bool {
	var routeErr *apiKeyRouteAdvanceError
	return errors.As(err, &routeErr) && routeErr.Last != nil && routeErr.Billing
}

func apiKeyRouteFailureCause(last *service.UpstreamFailoverError, fallback error) error {
	if last != nil {
		return last
	}
	return fallback
}

type apiKeyRouteReplayDecision struct {
	Allow  bool
	Reason string
}

func classifyAPIKeyRouteReplay(err error) apiKeyRouteReplayDecision {
	if err == nil {
		return apiKeyRouteReplayDecision{Allow: true, Reason: "pre_upstream_candidate_skip"}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return apiKeyRouteReplayDecision{Reason: "request_canceled"}
	}
	var failoverErr *service.UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		if !failoverErr.ShouldRetryNextAccount() {
			return apiKeyRouteReplayDecision{Reason: "upstream_retry_stopped"}
		}
		if failoverErr.RequestScopedTransient || failoverErr.Scope == service.GatewayFailureScopeRequest {
			return apiKeyRouteReplayDecision{Reason: "request_scoped_failure"}
		}
		if failoverErr.Scope == service.GatewayFailureScopeProvider {
			return apiKeyRouteReplayDecision{Reason: "provider_scoped_failure"}
		}
		if failoverErr.IsCredentialFailure() {
			if failoverErr.Scope == "" || failoverErr.Scope == service.GatewayFailureScopeAccount {
				return apiKeyRouteReplayDecision{Allow: true, Reason: "account_credential_failure"}
			}
			return apiKeyRouteReplayDecision{Reason: "non_account_credential_failure"}
		}
		if failoverErr.StatusCode >= 400 && failoverErr.StatusCode < 500 && failoverErr.StatusCode != 408 && failoverErr.StatusCode != 429 {
			if failoverErr.Scope == service.GatewayFailureScopeAccount && failoverErr.NextAccountAction == service.NextAccountRetry {
				return apiKeyRouteReplayDecision{Allow: true, Reason: "explicit_account_local_client_status"}
			}
			return apiKeyRouteReplayDecision{Reason: "client_or_policy_failure"}
		}
		return apiKeyRouteReplayDecision{Allow: true, Reason: "retryable_upstream_failure"}
	}
	if errors.Is(err, service.ErrNoAvailableAccounts) || errors.Is(err, service.ErrNoAvailableCompactAccounts) {
		return apiKeyRouteReplayDecision{Allow: true, Reason: "group_capacity_exhausted"}
	}
	return apiKeyRouteReplayDecision{Reason: "unclassified_failure"}
}

// apiKeyRouteFailureAllowsAdvance is the shared cross-group replay boundary.
// A nil cause means the current candidate was skipped before an upstream call
// (for example an OPEN breaker). Once an upstream call exists, only explicit
// retryable failover errors or account-capacity exhaustion may cross groups.
// Client/request failures, authorization/billing failures and cancellation
// therefore stay on the current group and preserve at-most-once semantics.
func apiKeyRouteFailureAllowsAdvance(err error) bool {
	return classifyAPIKeyRouteReplay(err).Allow
}
