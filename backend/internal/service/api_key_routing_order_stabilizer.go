package service

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultRoutingOrderStateLimit = 20000
	defaultRoutingOrderStateTTL   = 2 * time.Hour
	routingTrafficRampInterval    = time.Minute
)

type routingOrderState struct {
	stableOrder     []int64
	stableSince     time.Time
	transitionOrder []int64
	transitionSince time.Time
	lastSeen        time.Time
}

// APIKeyRoutingOrderStabilizer is a bounded, process-local hot-path guard for
// new-session ranking. It never overrides sticky sessions (callers consult
// sticky state first) and route_version is part of the key, so configuration
// changes cannot inherit old ranking state.
type APIKeyRoutingOrderStabilizer struct {
	mu         sync.Mutex
	states     map[string]*routingOrderState
	maxEntries int
	ttl        time.Duration
}

func NewAPIKeyRoutingOrderStabilizer(maxEntries int, ttl time.Duration) *APIKeyRoutingOrderStabilizer {
	if maxEntries <= 0 {
		maxEntries = defaultRoutingOrderStateLimit
	}
	if ttl <= 0 {
		ttl = defaultRoutingOrderStateTTL
	}
	return &APIKeyRoutingOrderStabilizer{states: make(map[string]*routingOrderState), maxEntries: maxEntries, ttl: ttl}
}

func (s *APIKeyRoutingOrderStabilizer) Stabilize(
	apiKeyID, routeVersion int64,
	scope APIKeyRoutingScoreScope,
	preference, sessionHash string,
	ranked []APIKeyRoutingCandidateScore,
	policy APIKeyRoutingStabilityPolicy,
	now time.Time,
) []APIKeyRoutingCandidateScore {
	if s == nil || apiKeyID <= 0 || routeVersion <= 0 || len(ranked) < 2 {
		return ranked
	}
	proposed := eligibleRoutingOrder(ranked)
	if len(proposed) < 2 {
		return ranked
	}
	key := fmt.Sprintf("%d:%d:%s:%s", apiKeyID, routeVersion, scope.Key(), strings.ToLower(strings.TrimSpace(preference)))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpired(now)
	state := s.states[key]
	if state == nil {
		s.ensureCapacity()
		s.states[key] = &routingOrderState{stableOrder: proposed, stableSince: now, lastSeen: now}
		return ranked
	}
	state.lastSeen = now
	stable := retainRoutingOrder(state.stableOrder, proposed)
	if len(stable) == 0 {
		state.stableOrder, state.stableSince = proposed, now
		state.transitionOrder = nil
		return ranked
	}
	if stable[0] == proposed[0] {
		state.stableOrder = mergeRoutingOrder(stable, proposed)
		state.transitionOrder = nil
		return reorderRoutingScores(ranked, state.stableOrder)
	}
	if now.Sub(state.stableSince) < time.Duration(policy.MinimumResidenceSeconds)*time.Second ||
		routingTopScoreAdvantage(ranked, proposed[0], stable[0]) < policy.MinimumScoreDifference {
		state.transitionOrder = nil
		return reorderRoutingScores(ranked, stable)
	}

	// Only one adjacent promotion is admitted per completed transition. This
	// bounds a single snapshot's rank movement even when raw metrics jump.
	target := promoteOnePosition(stable, proposed[0])
	if !sameRoutingOrder(state.transitionOrder, target) {
		state.transitionOrder = target
		state.transitionSince = now
	}
	rampBPS := policy.MaxTrafficChangeBPS
	if rampBPS <= 0 {
		rampBPS = 1
	}
	elapsedIntervals := int(now.Sub(state.transitionSince)/routingTrafficRampInterval) + 1
	admissionBPS := elapsedIntervals * rampBPS
	if admissionBPS >= 10000 {
		state.stableOrder = append([]int64(nil), target...)
		state.stableSince = now
		state.transitionOrder = nil
		return reorderRoutingScores(ranked, target)
	}
	if routingTransitionBucket(key, target[0], sessionHash) < admissionBPS {
		return reorderRoutingScores(ranked, target)
	}
	return reorderRoutingScores(ranked, stable)
}

func (s *APIKeyRoutingOrderStabilizer) evictExpired(now time.Time) {
	for key, state := range s.states {
		if now.Sub(state.lastSeen) > s.ttl {
			delete(s.states, key)
		}
	}
}

func (s *APIKeyRoutingOrderStabilizer) ensureCapacity() {
	if len(s.states) < s.maxEntries {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, state := range s.states {
		if oldestKey == "" || state.lastSeen.Before(oldest) {
			oldestKey, oldest = key, state.lastSeen
		}
	}
	delete(s.states, oldestKey)
}

func eligibleRoutingOrder(ranked []APIKeyRoutingCandidateScore) []int64 {
	order := make([]int64, 0, len(ranked))
	for _, score := range ranked {
		if score.Eligible {
			order = append(order, score.GroupID)
		}
	}
	return order
}

func retainRoutingOrder(stable, proposed []int64) []int64 {
	available := make(map[int64]struct{}, len(proposed))
	for _, id := range proposed {
		available[id] = struct{}{}
	}
	result := make([]int64, 0, len(proposed))
	for _, id := range stable {
		if _, ok := available[id]; ok {
			result = append(result, id)
			delete(available, id)
		}
	}
	for _, id := range proposed {
		if _, ok := available[id]; ok {
			result = append(result, id)
			delete(available, id)
		}
	}
	return result
}

func mergeRoutingOrder(stable, proposed []int64) []int64 { return retainRoutingOrder(stable, proposed) }

func promoteOnePosition(order []int64, groupID int64) []int64 {
	result := append([]int64(nil), order...)
	for index := 1; index < len(result); index++ {
		if result[index] == groupID {
			result[index-1], result[index] = result[index], result[index-1]
			break
		}
	}
	return result
}

func routingTopScoreAdvantage(ranked []APIKeyRoutingCandidateScore, proposed, stable int64) float64 {
	scores := make(map[int64]float64, len(ranked))
	for _, score := range ranked {
		scores[score.GroupID] = score.Score
	}
	return scores[proposed] - scores[stable]
}

func reorderRoutingScores(ranked []APIKeyRoutingCandidateScore, order []int64) []APIKeyRoutingCandidateScore {
	positions := make(map[int64]int, len(order))
	for index, id := range order {
		positions[id] = index
	}
	result := append([]APIKeyRoutingCandidateScore(nil), ranked...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Eligible != result[j].Eligible {
			return result[i].Eligible
		}
		left, leftOK := positions[result[i].GroupID]
		right, rightOK := positions[result[j].GroupID]
		if leftOK && rightOK {
			return left < right
		}
		return leftOK
	})
	return result
}

func sameRoutingOrder(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func routingTransitionBucket(key string, targetGroupID int64, sessionHash string) int {
	digest := sha256.Sum256([]byte(key + "\x00" + fmt.Sprint(targetGroupID) + "\x00" + sessionHash))
	return int(binary.BigEndian.Uint64(digest[:8]) % 10000)
}

var defaultAPIKeyRoutingOrderStabilizer = NewAPIKeyRoutingOrderStabilizer(0, 0)

func DefaultAPIKeyRoutingOrderStabilizer() *APIKeyRoutingOrderStabilizer {
	return defaultAPIKeyRoutingOrderStabilizer
}
