package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

var errAPIKeyRouteModelUnsupported = errors.New("candidate group does not support requested model")

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

func rejectAPIKeyRouteUnsupportedModel(c *gin.Context, diag service.ModelAvailabilityDiagnoser, candidate *service.APIKey, model string) error {
	if !apiKeyMultiGroupRoutingActive(c) || candidate == nil || candidate.Group == nil {
		return nil
	}
	model = strings.TrimSpace(model)
	if model == "" || diag == nil {
		return nil
	}
	platform := effectiveAPIKeyPlatform(c, candidate)
	if platform == "" {
		platform = candidate.Group.Platform
	}
	groupID := candidate.Group.ID
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	diagnosis := diag.DiagnoseModelAvailabilityForPlatform(ctx, &groupID, model, platform)
	if diagnosis.HasModelSupport {
		return nil
	}
	return fmt.Errorf("%w: group %d model %s", errAPIKeyRouteModelUnsupported, groupID, model)
}

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

type apiKeyRouteFailureObserver func(ctx context.Context, apiKeyID, routeVersion, groupID int64, model, endpoint string, cause error) (string, error)

// observeAPIKeyRouteFailure records a group-health failure even when semantic
// output has already closed cross-group replay. Failover remains gated; the
// denominator must still include the completed group visit.
func observeAPIKeyRouteFailure(c *gin.Context, apiKey *service.APIKey, model, endpoint string, routeErr error, observe apiKeyRouteFailureObserver) (string, error) {
	if c == nil || routeErr == nil || apiKey == nil || apiKey.GroupID == nil || observe == nil || !apiKeyMultiGroupRoutingActive(c) {
		return "", nil
	}
	middleware2.MarkAPIKeyRouteStickyBroken(c)
	return observe(c.Request.Context(), apiKey.ID, apiKey.RouteVersion, *apiKey.GroupID, model, endpoint, routeErr)
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
		reserved := false
		if state, stateOK := middleware2.GetAPIKeyRouteState(c); stateOK && state != nil && state.Plan != nil && !state.Locked {
			if c != nil {
				if _, exists := c.Get(failoverTransitionBudgetContextKey); !exists {
					maxGroupTransitions := state.Plan.Len() - 1
					if maxGroupTransitions < 1 {
						maxGroupTransitions = 1
					}
					failoverTransitionBudgetFor(c, maxGroupTransitions)
				}
			}
			hasNext := state.Cursor+1 < len(state.Order)
			if len(state.Order) == 0 {
				hasNext = state.Index+1 < state.Plan.Len()
			}
			if hasNext {
				if !reserveFailoverTransition(c) {
					return nil, nil, false, &apiKeyRouteAdvanceError{Last: lastErr, Billing: lastErrBilling}
				}
				reserved = true
			}
		}
		apiKey, ok := middleware2.AdvanceAPIKeyRoute(c)
		if !ok {
			if reserved {
				releaseFailoverTransition(c)
			}
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
				if reserved {
					releaseFailoverTransition(c)
				}
				lastErr = err
				lastErrBilling = false
				continue
			}
		}
		subscription, err := r.subscriptionForCandidate(c.Request.Context(), apiKey)
		if err != nil {
			if reserved {
				releaseFailoverTransition(c)
			}
			lastErr = err
			lastErrBilling = true
			continue
		}
		if r.billing != nil {
			platform := service.QuotaPlatform(c.Request.Context(), apiKey)
			if err := r.billing.CheckRouteSwitchBillingEligibility(c.Request.Context(), apiKey.User, apiKey.Group, subscription, platform); err != nil {
				if reserved {
					releaseFailoverTransition(c)
				}
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
