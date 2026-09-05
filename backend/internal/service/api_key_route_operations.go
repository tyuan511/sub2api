package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrAPIKeyRouteOperationInvalid = errors.New("invalid API key route operation")
	ErrAPIKeyRouteVersionStale     = errors.New("API key route version is stale")
)

type APIKeyRouteBreakerSnapshot struct {
	State              string `json:"state"`
	Successes          int64  `json:"successes"`
	Failures           int64  `json:"failures"`
	RecoverySuccesses  int64  `json:"recovery_successes"`
	RecoveryAdmissions int64  `json:"recovery_admissions"`
	OpenedAtUnixMS     int64  `json:"opened_at_unix_ms,omitempty"`
}

type APIKeyRouteBreakerOperationsCache interface {
	LoadAPIKeyRouteBreakers(ctx context.Context, keys []string) ([]APIKeyRouteBreakerSnapshot, error)
	DeleteAPIKeyRouteBreakers(ctx context.Context, keys []string) (int64, error)
}

type APIKeyRouteExplanationCandidate struct {
	GroupID            int64                        `json:"group_id"`
	ConfiguredPriority int                          `json:"configured_priority"`
	EffectiveRank      int                          `json:"effective_rank"`
	Admitted           bool                         `json:"admitted"`
	ExclusionReason    string                       `json:"exclusion_reason,omitempty"`
	Breaker            APIKeyRouteBreakerSnapshot   `json:"breaker"`
	Score              *APIKeyRoutingCandidateScore `json:"score,omitempty"`
}

type APIKeyRouteExplanation struct {
	APIKeyID               int64                             `json:"api_key_id"`
	RoutingEnabled         bool                              `json:"routing_enabled"`
	RouteVersion           int64                             `json:"route_version"`
	Platform               string                            `json:"platform"`
	ModelFamily            string                            `json:"model_family"`
	EndpointKind           string                            `json:"endpoint_kind"`
	ScheduleMode           string                            `json:"schedule_mode"`
	SmartPreference        *string                           `json:"smart_preference,omitempty"`
	SmartBalanceBPS        *int                              `json:"smart_balance_bps"`
	RoutingMinSuccessRate  int                               `json:"routing_min_success_rate"`
	RoutingStateVersion    int64                             `json:"routing_state_version"`
	StrategyVersion        string                            `json:"strategy_version,omitempty"`
	ScoreVersion           string                            `json:"score_version,omitempty"`
	FeatureVersion         string                            `json:"feature_version,omitempty"`
	ModelVersion           *string                           `json:"model_version,omitempty"`
	PersonalizationVersion *string                           `json:"personalization_version,omitempty"`
	LearningFallbacks      map[string]string                 `json:"learning_fallbacks,omitempty"`
	ScoreGeneratedAt       *time.Time                        `json:"score_generated_at,omitempty"`
	ScoreAgeMS             *int64                            `json:"score_age_ms,omitempty"`
	SequentialFallback     bool                              `json:"sequential_fallback"`
	StickyGroupID          *int64                            `json:"sticky_group_id,omitempty"`
	Candidates             []APIKeyRouteExplanationCandidate `json:"candidates"`
}

type APIKeyRouteClearRequest struct {
	APIKeyID     int64  `json:"api_key_id"`
	RouteVersion int64  `json:"route_version"`
	GroupID      *int64 `json:"group_id,omitempty"`
	ModelFamily  string `json:"model_family"`
	EndpointKind string `json:"endpoint_kind"`
	SessionHash  string `json:"session_hash,omitempty"`
	ClearSticky  bool   `json:"clear_sticky"`
	ClearBreaker bool   `json:"clear_breaker"`
}

type APIKeyRouteClearResult struct {
	APIKeyID        int64 `json:"api_key_id"`
	RouteVersion    int64 `json:"route_version"`
	StickyDeleted   bool  `json:"sticky_deleted"`
	BreakersDeleted int64 `json:"breakers_deleted"`
}

type APIKeyRouteOperationsService struct {
	apiKeys *APIKeyService
	cache   GatewayCache
}

func NewAPIKeyRouteOperationsService(apiKeys *APIKeyService, cache GatewayCache) *APIKeyRouteOperationsService {
	return &APIKeyRouteOperationsService{apiKeys: apiKeys, cache: cache}
}

func (s *APIKeyRouteOperationsService) Explain(ctx context.Context, apiKeyID int64, modelFamily, endpointKind, sessionHash string) (*APIKeyRouteExplanation, error) {
	if s == nil || s.apiKeys == nil || s.cache == nil || apiKeyID <= 0 {
		return nil, ErrAPIKeyRouteOperationInvalid
	}
	modelFamily = strings.ToLower(strings.TrimSpace(modelFamily))
	endpointKind = NormalizeAPIKeyRoutingEndpointKind(endpointKind)
	if !boundedRoutingDimension(modelFamily) || !boundedRoutingDimension(endpointKind) {
		return nil, ErrAPIKeyRouteOperationInvalid
	}
	apiKey, err := s.apiKeys.GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}
	apiKey = s.apiKeys.ProjectAPIKeyRoutingForUser(ctx, apiKey)
	plan, err := NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	if err != nil && !errors.Is(err, ErrNoEligibleAPIKeyRoute) {
		return nil, err
	}
	platform := PlatformFromAPIKey(apiKey)
	explanation := &APIKeyRouteExplanation{
		APIKeyID: apiKey.ID, RouteVersion: apiKey.RouteVersion, Platform: platform,
		ModelFamily: modelFamily, EndpointKind: endpointKind, ScheduleMode: apiKey.ScheduleMode,
		SmartPreference:       cloneStringPtr(apiKey.SmartPreference),
		SmartBalanceBPS:       cloneIntPtr(apiKey.SmartBalanceBPS),
		RoutingMinSuccessRate: apiKey.EffectiveRoutingMinSuccessRate(),
		RoutingStateVersion:   apiKey.EffectiveRoutingStateVersion(),
	}
	if plan == nil {
		return explanation, nil
	}
	explanation.RoutingEnabled = plan.RoutingEnabled
	explanation.ScheduleMode = plan.ScheduleMode

	breakerKeys := make([]string, len(plan.Candidates))
	for index, candidate := range plan.Candidates {
		breakerKeys[index] = APIKeyRouteHealthKey(apiKey.ID, apiKey.EffectiveRoutingStateVersion(), candidate.GroupID, modelFamily, endpointKind)
	}
	breakerStates := make([]APIKeyRouteBreakerSnapshot, len(breakerKeys))
	if breakerCache, ok := s.cache.(APIKeyRouteBreakerOperationsCache); ok && plan.RoutingEnabled && len(breakerKeys) > 0 {
		if loaded, loadErr := breakerCache.LoadAPIKeyRouteBreakers(ctx, breakerKeys); loadErr == nil && len(loaded) == len(breakerKeys) {
			breakerStates = loaded
		}
	}
	for index := range breakerStates {
		if breakerStates[index].State == "" {
			breakerStates[index].State = APIKeyRouteBreakerClosed
		}
	}

	ranked := make([]APIKeyRoutingCandidateScore, 0, len(plan.Candidates))
	if plan.RoutingEnabled && apiKey.ScheduleMode == APIKeyScheduleModeSmart && apiKey.SmartPreference != nil {
		scope := APIKeyRoutingScoreScope{Platform: platform, ModelFamily: modelFamily, EndpointKind: endpointKind}
		userID := int64(0)
		if apiKey.User != nil {
			userID = apiKey.User.ID
		}
		strategyScope := RoutingArtifactScope{ArtifactKind: RoutingArtifactStrategy, Platform: platform, ModelFamily: modelFamily, EndpointKind: endpointKind, Preference: apiKey.SmartPreference}
		selection := SelectDefaultAPIKeyRoutingStrategy(strategyScope, userID, apiKey.ID)
		selection.Policy = ApplyAPIKeyRoutingControls(selection.Policy, apiKey)
		if snapshot, found := DefaultAPIKeyRoutingScoreStore().Lookup(scope, time.Duration(selection.Policy.MaxSnapshotAgeSeconds)*time.Second, time.Now()); found {
			var userRates map[int64]float64
			if apiKey.User != nil {
				userRates = apiKey.User.GroupRates
			}
			projected := ProjectAPIKeyRoutingScoreSnapshot(plan.Candidates, snapshot, userRates)
			baselineRanked := RankAPIKeyRoutingCandidatesWithPolicy(plan.Candidates, projected, selection.Policy)
			breakerByGroup := make(map[int64]string, len(plan.Candidates))
			for index, candidate := range plan.Candidates {
				if index < len(breakerStates) {
					breakerByGroup[candidate.GroupID] = breakerStates[index].State
				}
			}
			eligible := make(map[int64]bool, len(baselineRanked))
			for _, score := range baselineRanked {
				if score.Eligible && breakerByGroup[score.GroupID] != APIKeyRouteBreakerOpen {
					eligible[score.GroupID] = true
				}
			}
			learning := ApplyDefaultAPIKeyRoutingLearning(strategyScope, apiKey.ID, userID, selection.ExperimentID, projected, eligible, time.Now())
			ranked = RankAPIKeyRoutingCandidatesWithPolicy(plan.Candidates, learning.Snapshot, selection.Policy)
			ranked = preserveRoutingBaselineEligibility(ranked, baselineRanked)
			ranked = annotateRoutingLearningScores(ranked, baselineRanked, learning.Personalization.AppliedGroups)
			explanation.StrategyVersion = selection.Policy.Version
			explanation.ScoreVersion = snapshot.Version
			explanation.FeatureVersion = snapshot.FeatureVersion
			explanation.ModelVersion = cloneStringPtr(learning.Snapshot.ModelVersion)
			if learning.Personalization.Reason == "" && learning.Personalization.Version != "" {
				explanation.PersonalizationVersion = optionalStringPtr(learning.Personalization.Version)
			}
			explanation.LearningFallbacks = make(map[string]string, 2)
			if learning.Personalization.Reason != "" {
				explanation.LearningFallbacks["personalization"] = learning.Personalization.Reason
			}
			if learning.Prediction.Reason != "" {
				explanation.LearningFallbacks["model"] = learning.Prediction.Reason
			}
			if len(explanation.LearningFallbacks) == 0 {
				explanation.LearningFallbacks = nil
			}
			generated := snapshot.GeneratedAt
			explanation.ScoreGeneratedAt = &generated
			age := time.Since(generated).Milliseconds()
			if age < 0 {
				age = 0
			}
			explanation.ScoreAgeMS = &age
		} else {
			explanation.SequentialFallback = true
		}
	}

	rankByGroup := make(map[int64]int, len(plan.Candidates))
	scoreByGroup := make(map[int64]APIKeyRoutingCandidateScore, len(ranked))
	if len(ranked) > 0 {
		for rank, score := range ranked {
			rankByGroup[score.GroupID] = rank
			scoreByGroup[score.GroupID] = score
		}
	} else {
		for rank, candidate := range plan.Candidates {
			rankByGroup[candidate.GroupID] = rank
		}
	}
	for index, candidate := range plan.Candidates {
		item := APIKeyRouteExplanationCandidate{
			GroupID: candidate.GroupID, ConfiguredPriority: candidate.Priority, EffectiveRank: rankByGroup[candidate.GroupID],
			Admitted: true, Breaker: breakerStates[index],
		}
		if item.Breaker.State == APIKeyRouteBreakerOpen {
			item.Admitted = false
			item.ExclusionReason = "breaker_open"
		}
		total := item.Breaker.Successes + item.Breaker.Failures
		if item.Breaker.State == APIKeyRouteBreakerClosed && total >= int64(DefaultAPIKeyRouteHealthPolicy(s.apiKeys.cfg).MinimumSamples) && item.Breaker.Successes*100 < total*int64(apiKey.EffectiveRoutingMinSuccessRate()) {
			item.Admitted = false
			item.ExclusionReason = fmt.Sprintf("success_rate_below_%d_percent", apiKey.EffectiveRoutingMinSuccessRate())
		}
		if score, ok := scoreByGroup[candidate.GroupID]; ok {
			scoreCopy := score
			item.Score = &scoreCopy
			if !score.Eligible {
				item.Admitted = false
				item.ExclusionReason = score.Exclusion
			}
		}
		explanation.Candidates = append(explanation.Candidates, item)
	}
	for _, excluded := range plan.Excluded {
		explanation.Candidates = append(explanation.Candidates, APIKeyRouteExplanationCandidate{
			GroupID: excluded.GroupID, ConfiguredPriority: excluded.Priority, EffectiveRank: -1,
			Admitted: false, ExclusionReason: excluded.Reason, Breaker: APIKeyRouteBreakerSnapshot{State: APIKeyRouteBreakerClosed},
		})
	}
	sort.SliceStable(explanation.Candidates, func(i, j int) bool {
		left, right := explanation.Candidates[i], explanation.Candidates[j]
		if left.EffectiveRank < 0 {
			return false
		}
		if right.EffectiveRank < 0 {
			return true
		}
		return left.EffectiveRank < right.EffectiveRank
	})
	if plan.RoutingEnabled && strings.TrimSpace(sessionHash) != "" {
		groupID, stickyErr := getAPIKeyGroupSticky(ctx, s.cache, apiKey.ID, apiKey.EffectiveRoutingStateVersion(), modelFamily, endpointKind, sessionHash)
		if stickyErr != nil {
			return nil, stickyErr
		}
		explanation.StickyGroupID = positiveInt64Ptr(groupID)
	}
	return explanation, nil
}

func preserveRoutingBaselineEligibility(ranked, baseline []APIKeyRoutingCandidateScore) []APIKeyRoutingCandidateScore {
	baselineByGroup := make(map[int64]APIKeyRoutingCandidateScore, len(baseline))
	for _, score := range baseline {
		baselineByGroup[score.GroupID] = score
	}
	for index := range ranked {
		base, exists := baselineByGroup[ranked[index].GroupID]
		if !exists || !base.Eligible {
			ranked[index].Eligible = false
			if exists {
				ranked[index].Exclusion = base.Exclusion
			}
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Eligible && !ranked[j].Eligible })
	return ranked
}

func annotateRoutingLearningScores(ranked, baseline []APIKeyRoutingCandidateScore, weights map[int64]float64) []APIKeyRoutingCandidateScore {
	baselineByGroup := make(map[int64]float64, len(baseline))
	for _, score := range baseline {
		baselineByGroup[score.GroupID] = score.Score
	}
	for index := range ranked {
		base, exists := baselineByGroup[ranked[index].GroupID]
		if !exists {
			continue
		}
		baseCopy, adjustment := base, ranked[index].Score-base
		ranked[index].SharedBaselineScore, ranked[index].LearningAdjustment = &baseCopy, &adjustment
		if weight, ok := weights[ranked[index].GroupID]; ok {
			weightCopy := weight
			ranked[index].PersonalizationWeight = &weightCopy
		}
	}
	return ranked
}

func (s *APIKeyRouteOperationsService) ClearState(ctx context.Context, request APIKeyRouteClearRequest) (*APIKeyRouteClearResult, error) {
	if s == nil || s.apiKeys == nil || s.cache == nil || request.APIKeyID <= 0 || request.RouteVersion <= 0 || (!request.ClearSticky && !request.ClearBreaker) {
		return nil, ErrAPIKeyRouteOperationInvalid
	}
	request.ModelFamily = strings.ToLower(strings.TrimSpace(request.ModelFamily))
	request.EndpointKind = NormalizeAPIKeyRoutingEndpointKind(request.EndpointKind)
	if !boundedRoutingDimension(request.ModelFamily) || !boundedRoutingDimension(request.EndpointKind) || (request.ClearSticky && strings.TrimSpace(request.SessionHash) == "") {
		return nil, ErrAPIKeyRouteOperationInvalid
	}
	apiKey, err := s.apiKeys.GetByID(ctx, request.APIKeyID)
	if err != nil {
		return nil, err
	}
	if apiKey.RouteVersion != request.RouteVersion {
		return nil, fmt.Errorf("%w: current=%d requested=%d", ErrAPIKeyRouteVersionStale, apiKey.RouteVersion, request.RouteVersion)
	}
	groupIDs := apiKeyRouteConfiguredGroupIDs(apiKey)
	if request.GroupID != nil {
		if !containsAPIKeyRouteGroup(groupIDs, *request.GroupID) {
			return nil, ErrAPIKeyRouteOperationInvalid
		}
		groupIDs = []int64{*request.GroupID}
	}
	result := &APIKeyRouteClearResult{APIKeyID: apiKey.ID, RouteVersion: apiKey.RouteVersion}
	if request.ClearSticky {
		if err := s.cache.DeleteSessionAccountID(ctx, apiKey.ID, apiKeyGroupStickyCacheKey(apiKey.ID, apiKey.EffectiveRoutingStateVersion(), request.ModelFamily, request.EndpointKind, request.SessionHash)); err != nil {
			return nil, err
		}
		result.StickyDeleted = true
	}
	if request.ClearBreaker {
		breakerCache, ok := s.cache.(APIKeyRouteBreakerOperationsCache)
		if !ok {
			return nil, errors.New("API key route breaker operations unavailable")
		}
		keys := make([]string, 0, len(groupIDs))
		for _, groupID := range groupIDs {
			keys = append(keys, APIKeyRouteHealthKey(apiKey.ID, apiKey.EffectiveRoutingStateVersion(), groupID, request.ModelFamily, request.EndpointKind))
		}
		result.BreakersDeleted, err = breakerCache.DeleteAPIKeyRouteBreakers(ctx, keys)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func apiKeyRouteConfiguredGroupIDs(apiKey *APIKey) []int64 {
	if apiKey == nil {
		return nil
	}
	result := make([]int64, 0, len(apiKey.GroupRoutes))
	for _, route := range apiKey.GroupRoutes {
		if route.GroupID > 0 {
			result = append(result, route.GroupID)
		}
	}
	if len(result) == 0 && apiKey.GroupID != nil && *apiKey.GroupID > 0 {
		result = append(result, *apiKey.GroupID)
	}
	return result
}

func containsAPIKeyRouteGroup(groups []int64, target int64) bool {
	for _, groupID := range groups {
		if groupID == target {
			return true
		}
	}
	return false
}
