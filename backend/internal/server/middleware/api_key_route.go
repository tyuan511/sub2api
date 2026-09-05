package middleware

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func selectInitialBillableAPIKeyRoute(ctx context.Context, apiKey *service.APIKey, subscriptionService *service.SubscriptionService, skipBilling bool, advance func() (*service.APIKey, bool)) (*service.APIKey, *service.UserSubscription, error) {
	for {
		if apiKey == nil || apiKey.Group == nil || !apiKey.Group.IsSubscriptionType() || subscriptionService == nil {
			return apiKey, nil, nil
		}
		// Read-only endpoints retain the active subscription even when a limit
		// is exhausted, and must not activate/reset its billing windows.
		if skipBilling {
			subscription, _ := subscriptionService.GetActiveSubscription(ctx, apiKey.User.ID, apiKey.Group.ID)
			return apiKey, subscription, nil
		}
		subscription, err := subscriptionService.GetEligibleSubscription(ctx, apiKey.User.ID, apiKey.Group)
		if err == nil {
			return apiKey, subscription, nil
		}
		if errors.Is(err, service.ErrSubscriptionMaintenance) {
			return apiKey, nil, err
		}
		if advance == nil {
			return apiKey, nil, err
		}
		next, ok := advance()
		if !ok {
			return apiKey, nil, err
		}
		apiKey = next
	}
}

// APIKeyRouteState is request-local. The plan is immutable; Index advances
// only after the current group has been exhausted and replay is still safe.
type APIKeyRouteState struct {
	Plan *service.APIKeyRoutePlan
	// Order stores indexes into the immutable Plan.Candidates slice. Sequential
	// mode uses [0..N); smart mode replaces it once, before the first upstream
	// attempt, with a request-frozen scored order.
	Order                        []int
	Cursor                       int
	Index                        int
	InitialGroupID               int64
	SwitchCount                  int
	ScoreVersion                 string
	StrategyVersion              string
	FeatureVersion               string
	ModelVersion                 *string
	ExperimentID                 *string
	ExperimentBucket             *int
	AssignmentReason             string
	StickySelected               bool
	StickyBroken                 bool
	Locked                       bool
	CacheCompensationMaxTokens   int
	CacheCompensationMaxSwitches int
	AttemptStartedAt             time.Time
	ScoreGeneratedAt             time.Time
	DecisionID                   string
	ScoreFacts                   map[int64]service.APIKeyRoutingCandidateScore
}

// MarkAPIKeyRouteStickySelected records that this request started from a
// healthy session-level group binding. If that binding is later skipped or the
// request advances, the final decision chain can account for stability loss.
func MarkAPIKeyRouteStickySelected(c *gin.Context) {
	state, ok := GetAPIKeyRouteState(c)
	if !ok {
		return
	}
	state.StickySelected = true
	bindAPIKeyRoutingUsageContext(c, state)
}

func MarkAPIKeyRouteStickyBroken(c *gin.Context) {
	state, ok := GetAPIKeyRouteState(c)
	if !ok {
		return
	}
	state.StickyBroken = true
	if state.StickySelected && c != nil && c.Request != nil {
		ctx := service.WithForceCacheBilling(c.Request.Context())
		c.Request = c.Request.WithContext(service.WithAPIKeyGroupCacheCompensation(ctx))
	}
	bindAPIKeyRoutingUsageContext(c, state)
}

func apiKeyRouteCoordinatorFromConfig(cfg *config.Config) *service.APIKeyRouteCoordinator {
	return service.NewAPIKeyRouteCoordinator(cfg != nil && cfg.Gateway.APIKeyMultiGroupRoutingEnabled)
}

func apiKeyRouteCompensationLimitsFromConfig(cfg *config.Config) []int {
	if cfg == nil {
		return nil
	}
	return []int{
		cfg.Gateway.APIKeyGroupCacheCompensationMaxTokens,
		cfg.Gateway.APIKeyGroupCacheCompensationMaxSwitches,
	}
}

func prepareInitialAPIKeyRoute(apiKey *service.APIKey, coordinator *service.APIKeyRouteCoordinator, compensationLimits ...int) (*service.APIKey, *APIKeyRouteState, error) {
	if apiKey != nil && len(apiKey.GroupRoutes) > 0 && (apiKey.GroupID == nil ||
		(len(apiKey.GroupRoutes) == 1 && apiKey.GroupRoutes[0].GroupID <= 0)) {
		return nil, nil, service.ErrInvalidAPIKeyRouteSet
	}
	if apiKey == nil || coordinator == nil || !coordinator.Enabled() {
		return apiKey, nil, nil
	}
	if !apiKey.HasMultipleEnabledGroupRoutes() {
		// A withdrawn multi-group key with a missing primary must never turn
		// into an unscoped key. Valid fixed keys otherwise keep their exact
		// legacy projection and GROUP_DISABLED/GROUP_DELETED/permission checks.
		return apiKey, &APIKeyRouteState{Plan: &service.APIKeyRoutePlan{
			APIKeyID: apiKey.ID, RouteVersion: apiKey.RouteVersion,
			RoutingStateVersion: apiKey.EffectiveRoutingStateVersion(),
		}}, nil
	}
	plan, err := coordinator.BuildPlan(apiKey, func(group *service.Group) (bool, string) {
		if group == nil || apiKey.User == nil {
			return false, "group_or_user_missing"
		}
		if group.IsSubscriptionType() || apiKey.User.CanBindGroup(group.ID, group.IsExclusive) {
			return true, ""
		}
		return false, "group_not_allowed"
	})
	if err != nil {
		return nil, nil, err
	}
	maxTokens := service.DefaultAPIKeyGroupCacheCompensationMaxTokens
	maxSwitches := service.DefaultAPIKeyGroupCacheCompensationMaxSwitches
	if len(compensationLimits) > 0 && compensationLimits[0] > 0 {
		maxTokens = compensationLimits[0]
	}
	if len(compensationLimits) > 1 && compensationLimits[1] > 0 {
		maxSwitches = compensationLimits[1]
	}
	state := &APIKeyRouteState{
		Plan: plan, Order: make([]int, plan.Len()), AttemptStartedAt: time.Now(),
		CacheCompensationMaxTokens: maxTokens, CacheCompensationMaxSwitches: maxSwitches,
	}
	for i := range state.Order {
		state.Order[i] = i
	}
	if plan.LegacyUnscoped {
		return apiKey, state, nil
	}
	routed, ok := plan.APIKeyForCandidate(0)
	if !ok {
		return nil, nil, fmt.Errorf("%w: initial candidate missing", service.ErrNoEligibleAPIKeyRoute)
	}
	if routed.GroupID != nil {
		state.InitialGroupID = *routed.GroupID
	}
	return routed, state, nil
}

func SetAPIKeyRouteState(c *gin.Context, state *APIKeyRouteState) {
	if c == nil || state == nil || state.Plan == nil {
		return
	}
	c.Set(string(ContextKeyAPIKeyRouteState), state)
	if c.Request != nil {
		c.Request = c.Request.WithContext(service.WithAPIKeyRouteRequestRuntimeState(c.Request.Context(), state.Plan))
	}
	bindAPIKeyRoutingUsageContext(c, state)
}

func GetAPIKeyRouteState(c *gin.Context) (*APIKeyRouteState, bool) {
	if c == nil {
		return nil, false
	}
	value, exists := c.Get(string(ContextKeyAPIKeyRouteState))
	if !exists {
		return nil, false
	}
	state, ok := value.(*APIKeyRouteState)
	return state, ok && state != nil && state.Plan != nil
}

// LockAPIKeyRoute freezes the already selected physical group for detached or
// state-owned work. The route facts remain available, but no later handler may
// reorder or advance the request-local plan.
func LockAPIKeyRoute(c *gin.Context) {
	state, ok := GetAPIKeyRouteState(c)
	if !ok {
		return
	}
	state.Locked = true
	bindAPIKeyRoutingUsageContext(c, state)
}

// AdvanceAPIKeyRoute moves to the next pre-filtered group and updates the
// request-local API key/group context without mutating the shared auth cache
// object. Subscription context is deliberately cleared; the handler must load
// and validate the new group's subscription before attempting upstream work.
func AdvanceAPIKeyRoute(c *gin.Context) (*service.APIKey, bool) {
	state, ok := GetAPIKeyRouteState(c)
	ensureAPIKeyRouteOrder(state)
	if !ok || state.Locked || state.Cursor+1 >= len(state.Order) {
		return nil, false
	}
	return activateAPIKeyRouteIndex(c, state.Order[state.Cursor+1], false)
}

// FindAPIKeyRouteGroup returns a request-local projection without changing the
// active route. It is used to validate a sticky target before committing it.
func FindAPIKeyRouteGroup(c *gin.Context, groupID int64) (*service.APIKey, int, bool) {
	state, ok := GetAPIKeyRouteState(c)
	if !ok || groupID <= 0 {
		return nil, 0, false
	}
	for index, candidate := range state.Plan.Candidates {
		if candidate.GroupID != groupID {
			continue
		}
		apiKey, ok := state.Plan.APIKeyForCandidate(index)
		return apiKey, index, ok
	}
	return nil, 0, false
}

// ActivateAPIKeyRouteIndex commits an already validated candidate and updates
// every request-local context projection that downstream handlers consume.
func ActivateAPIKeyRouteIndex(c *gin.Context, nextIndex int) (*service.APIKey, bool) {
	return activateAPIKeyRouteIndex(c, nextIndex, false)
}

// ActivateInitialAPIKeyRouteIndex changes the initial candidate before any
// upstream attempt. It does not count as a failover and rewrites the remaining
// visit order so a sticky candidate is tried first, followed by every other
// admitted candidate exactly once.
func ActivateInitialAPIKeyRouteIndex(c *gin.Context, nextIndex int) (*service.APIKey, bool) {
	state, ok := GetAPIKeyRouteState(c)
	ensureAPIKeyRouteOrder(state)
	if !ok || state.Locked || nextIndex < 0 || nextIndex >= state.Plan.Len() {
		return nil, false
	}
	order := make([]int, 0, state.Plan.Len())
	order = append(order, nextIndex)
	for _, index := range state.Order {
		if index != nextIndex {
			order = append(order, index)
		}
	}
	state.Order = order
	state.Cursor = 0
	state.SwitchCount = 0
	return activateAPIKeyRouteIndex(c, nextIndex, true)
}

// RejectInitialAPIKeyRoute removes the current candidate from this request's
// frozen visit order and activates the next candidate as the initial route.
// It is only for pre-upstream hard filtering: no request has been attempted,
// so the move neither increments SwitchCount nor retains the rejected group as
// a later fallback. Runtime failures must use AdvanceAPIKeyRoute instead.
func RejectInitialAPIKeyRoute(c *gin.Context) (*service.APIKey, bool) {
	state, ok := GetAPIKeyRouteState(c)
	ensureAPIKeyRouteOrder(state)
	if !ok || state.Locked || state.Cursor < 0 || state.Cursor >= len(state.Order) || len(state.Order) <= 1 {
		return nil, false
	}

	cursor := state.Cursor
	order := make([]int, 0, len(state.Order)-1)
	order = append(order, state.Order[:cursor]...)
	order = append(order, state.Order[cursor+1:]...)
	if cursor >= len(order) {
		return nil, false
	}
	state.Order = order
	state.Cursor = cursor
	state.SwitchCount = 0
	return activateAPIKeyRouteIndex(c, order[cursor], true)
}

// ApplyAPIKeyRouteOrder freezes a scored subset/order before the first
// upstream attempt. groupIDs may omit hard-rejected candidates, but may not
// introduce a group outside the configured request plan.
func ApplyAPIKeyRouteOrder(c *gin.Context, groupIDs []int64, scoreVersion, strategyVersion, featureVersion string, generatedAt time.Time) (*service.APIKey, bool) {
	state, ok := GetAPIKeyRouteState(c)
	if !ok || state.Locked || len(groupIDs) == 0 {
		return nil, false
	}
	indexes := make([]int, 0, len(groupIDs))
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if _, duplicate := seen[groupID]; duplicate {
			return nil, false
		}
		seen[groupID] = struct{}{}
		_, index, found := FindAPIKeyRouteGroup(c, groupID)
		if !found {
			return nil, false
		}
		indexes = append(indexes, index)
	}
	state.Order = indexes
	state.Cursor = 0
	state.SwitchCount = 0
	state.ScoreVersion = scoreVersion
	state.StrategyVersion = strategyVersion
	state.FeatureVersion = featureVersion
	state.ScoreGeneratedAt = generatedAt
	return activateAPIKeyRouteIndex(c, indexes[0], true)
}

// SetAPIKeyRouteScoreFacts freezes the numeric explanation used by this
// request. A later background score refresh cannot change the replay record.
func SetAPIKeyRouteScoreFacts(c *gin.Context, ranked []service.APIKeyRoutingCandidateScore) {
	state, ok := GetAPIKeyRouteState(c)
	if !ok {
		return
	}
	state.ScoreFacts = make(map[int64]service.APIKeyRoutingCandidateScore, len(ranked))
	for _, score := range ranked {
		state.ScoreFacts[score.GroupID] = score
	}
	bindAPIKeyRoutingUsageContext(c, state)
}

func SetAPIKeyRouteStrategyAssignment(c *gin.Context, selection service.APIKeyRoutingStrategySelection, modelVersion *string) {
	state, ok := GetAPIKeyRouteState(c)
	if !ok {
		return
	}
	state.StrategyVersion = selection.Policy.Version
	state.AssignmentReason = selection.AssignmentReason
	state.ExperimentID = cloneRouteStringPtr(selection.ExperimentID)
	state.ExperimentBucket = cloneRouteIntPtr(selection.ExperimentBucket)
	state.ModelVersion = cloneRouteStringPtr(modelVersion)
	bindAPIKeyRoutingUsageContext(c, state)
}

func activateAPIKeyRouteIndex(c *gin.Context, nextIndex int, initial bool) (*service.APIKey, bool) {
	state, ok := GetAPIKeyRouteState(c)
	if !ok || state.Locked || nextIndex < 0 || nextIndex >= state.Plan.Len() {
		return nil, false
	}
	apiKey, ok := state.Plan.APIKeyForCandidate(nextIndex)
	if !ok {
		return nil, false
	}
	if !initial && nextIndex != state.Index {
		state.SwitchCount++
		if state.StickySelected {
			state.StickyBroken = true
			if c.Request != nil {
				ctx := service.WithForceCacheBilling(c.Request.Context())
				c.Request = c.Request.WithContext(service.WithAPIKeyGroupCacheCompensation(ctx))
			}
		}
		service.DefaultRoutingRuntimeMetrics().RecordSwitch(state.StickyBroken)
	}
	state.Index = nextIndex
	for position, index := range state.Order {
		if index == nextIndex {
			state.Cursor = position
			break
		}
	}
	if initial && apiKey.GroupID != nil {
		state.InitialGroupID = *apiKey.GroupID
	}
	state.AttemptStartedAt = time.Now()
	c.Set(string(ContextKeyAPIKey), apiKey)
	c.Set(string(ContextKeyOpsFallbackAPIKey), apiKey)
	c.Set(string(ContextKeySubscription), nil)
	setGroupContext(c, apiKey.Group)
	bindAPIKeyRoutingUsageContext(c, state)
	return apiKey, true
}

func bindAPIKeyRoutingUsageContext(c *gin.Context, state *APIKeyRouteState) {
	if c == nil || c.Request == nil || state == nil || state.Plan == nil || !state.Plan.RoutingEnabled || state.Plan.RouteVersion < 1 {
		return
	}
	if strings.TrimSpace(state.DecisionID) == "" {
		state.DecisionID, _ = c.Request.Context().Value(ctxkey.ClientRequestID).(string)
		state.DecisionID = strings.TrimSpace(state.DecisionID)
		if state.DecisionID == "" {
			state.DecisionID = uuid.NewString()
		}
	}
	var effectiveGroupID int64
	var platform string
	if state.Index >= 0 && state.Index < state.Plan.Len() {
		effectiveGroupID = state.Plan.Candidates[state.Index].GroupID
		if group := state.Plan.Candidates[state.Index].Group; group != nil {
			platform = group.Platform
		}
	}
	ctx := service.WithAPIKeyRoutingUsageContext(c.Request.Context(), service.APIKeyRoutingUsageContext{
		DecisionID:                   state.DecisionID,
		APIKeyID:                     state.Plan.APIKeyID,
		RouteVersion:                 state.Plan.RouteVersion,
		InitialGroupID:               state.InitialGroupID,
		EffectiveGroupID:             effectiveGroupID,
		Platform:                     platform,
		ScheduleMode:                 state.Plan.ScheduleMode,
		SmartPreference:              state.Plan.SmartPreference,
		SmartBalanceBPS:              state.Plan.SmartBalanceBPS,
		RoutingMinSuccessRate:        state.Plan.RoutingMinSuccessRate,
		RoutingStateVersion:          state.Plan.RoutingStateVersion,
		SwitchCount:                  state.SwitchCount,
		StrategyVersion:              state.StrategyVersion,
		ScoreVersion:                 state.ScoreVersion,
		FeatureVersion:               state.FeatureVersion,
		ModelVersion:                 state.ModelVersion,
		ExperimentID:                 state.ExperimentID,
		ExperimentBucket:             state.ExperimentBucket,
		AssignmentReason:             state.AssignmentReason,
		StickyBroken:                 state.StickyBroken,
		CacheCompensationMaxTokens:   state.CacheCompensationMaxTokens,
		CacheCompensationMaxSwitches: state.CacheCompensationMaxSwitches,
		AttemptStartedAt:             state.AttemptStartedAt,
		Candidates:                   apiKeyRoutingDecisionCandidates(state, effectiveGroupID),
	})
	c.Request = c.Request.WithContext(ctx)
}

func cloneRouteStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneRouteIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneRouteFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func apiKeyRoutingDecisionCandidates(state *APIKeyRouteState, effectiveGroupID int64) []service.APIKeyRoutingDecisionCandidate {
	if state == nil || state.Plan == nil {
		return nil
	}
	ranks := make(map[int]int, len(state.Order))
	for rank, index := range state.Order {
		ranks[index] = rank
	}
	result := make([]service.APIKeyRoutingDecisionCandidate, 0, len(state.Plan.Candidates)+len(state.Plan.Excluded))
	for index, candidate := range state.Plan.Candidates {
		item := service.APIKeyRoutingDecisionCandidate{
			GroupID: candidate.GroupID, ConfiguredPriority: candidate.Priority, Admitted: true,
			OutcomeVisibility: "unobserved",
		}
		if rank, ok := ranks[index]; ok {
			rankCopy := rank
			item.Rank = &rankCopy
		}
		if score, ok := state.ScoreFacts[candidate.GroupID]; ok {
			success, smoothedSuccess, confidence, total, breakdown := score.SuccessRate, score.SmoothedSuccessRate, score.Confidence, score.Score, score.Breakdown
			item.SuccessRate, item.SmoothedSuccessRate, item.Confidence, item.Score, item.ScoreBreakdown = &success, &smoothedSuccess, &confidence, &total, &breakdown
			normalizedRate, ttft, duration := score.NormalizedRate, score.TTFTMS, score.DurationMS
			capacity, cacheHit := score.CapacityScore, score.CacheHitRate
			item.NormalizedRate, item.TTFTMS, item.DurationMS = &normalizedRate, &ttft, &duration
			item.CapacityScore, item.CacheHitRate, item.ObservationWindow = &capacity, &cacheHit, score.ObservationWindow
			item.DependencyDomains = append([]string(nil), score.DependencyDomains...)
			item.SharedBaselineScore = cloneRouteFloat64Ptr(score.SharedBaselineScore)
			item.LearningAdjustment = cloneRouteFloat64Ptr(score.LearningAdjustment)
			item.PersonalizationWeight = cloneRouteFloat64Ptr(score.PersonalizationWeight)
			if !score.Eligible {
				item.Admitted = false
				item.ExclusionReason = score.Exclusion
			}
		}
		if candidate.GroupID == effectiveGroupID {
			item.OutcomeVisibility = "observed"
		}
		result = append(result, item)
	}
	for _, excluded := range state.Plan.Excluded {
		result = append(result, service.APIKeyRoutingDecisionCandidate{
			GroupID: excluded.GroupID, ConfiguredPriority: excluded.Priority, Admitted: false,
			ExclusionReason: excluded.Reason, OutcomeVisibility: "unobserved",
		})
	}
	return result
}

func ensureAPIKeyRouteOrder(state *APIKeyRouteState) {
	if state == nil || state.Plan == nil || len(state.Order) > 0 {
		return
	}
	state.Order = make([]int, state.Plan.Len())
	for i := range state.Order {
		state.Order[i] = i
	}
	state.Cursor = state.Index
}

// SetSubscriptionInContext updates the actual group's subscription after a
// request-local route switch. A nil value intentionally clears any previous
// group's subscription.
func SetSubscriptionInContext(c *gin.Context, subscription *service.UserSubscription) {
	if c == nil {
		return
	}
	c.Set(string(ContextKeySubscription), subscription)
}
