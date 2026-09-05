package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"
)

var ErrAPIKeyRoutingScoreSnapshotNotFound = errors.New("api key routing score snapshot not found")

// APIKeyRoutingScoreScope is deliberately bounded to shared dimensions. API
// key and user IDs never enter the global snapshot namespace.
type APIKeyRoutingScoreScope struct {
	Platform     string
	ModelFamily  string
	EndpointKind string
}

func (s APIKeyRoutingScoreScope) Key() string {
	return strings.ToLower(strings.TrimSpace(s.Platform)) + "\x00" +
		strings.ToLower(strings.TrimSpace(s.ModelFamily)) + "\x00" +
		strings.ToLower(strings.TrimSpace(s.EndpointKind))
}

func (s APIKeyRoutingScoreScope) Valid() bool {
	return boundedRoutingDimension(s.Platform) && boundedRoutingDimension(s.ModelFamily) && boundedRoutingDimension(s.EndpointKind)
}

func boundedRoutingDimension(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 200 && !strings.ContainsRune(value, '\x00')
}

type apiKeyRoutingScoreCatalog struct {
	snapshots map[string]*APIKeyRoutingScoreSnapshot
}

// AtomicAPIKeyRoutingScoreStore gives the request path a single atomic read.
// Replace clones all maps before publication, so a builder cannot mutate an
// in-flight request's score version.
type AtomicAPIKeyRoutingScoreStore struct {
	current atomic.Pointer[apiKeyRoutingScoreCatalog]
}

func NewAtomicAPIKeyRoutingScoreStore() *AtomicAPIKeyRoutingScoreStore {
	store := &AtomicAPIKeyRoutingScoreStore{}
	store.current.Store(&apiKeyRoutingScoreCatalog{snapshots: map[string]*APIKeyRoutingScoreSnapshot{}})
	return store
}

func (s *AtomicAPIKeyRoutingScoreStore) Replace(snapshots []*APIKeyRoutingScoreSnapshot) error {
	next := &apiKeyRoutingScoreCatalog{snapshots: make(map[string]*APIKeyRoutingScoreSnapshot, len(snapshots))}
	for _, snapshot := range snapshots {
		if err := ValidateAPIKeyRoutingScoreSnapshot(snapshot); err != nil {
			return err
		}
		copy := cloneAPIKeyRoutingScoreSnapshot(snapshot)
		scope := APIKeyRoutingScoreScope{Platform: copy.Platform, ModelFamily: copy.ModelFamily, EndpointKind: copy.EndpointKind}
		if _, duplicate := next.snapshots[scope.Key()]; duplicate {
			return fmt.Errorf("duplicate routing score scope %q", scope.Key())
		}
		next.snapshots[scope.Key()] = copy
	}
	s.current.Store(next)
	return nil
}

func (s *AtomicAPIKeyRoutingScoreStore) Lookup(scope APIKeyRoutingScoreScope, maxAge time.Duration, now time.Time) (*APIKeyRoutingScoreSnapshot, bool) {
	if s == nil || !scope.Valid() {
		return nil, false
	}
	catalog := s.current.Load()
	if catalog == nil {
		return nil, false
	}
	snapshot := catalog.snapshots[scope.Key()]
	if snapshot == nil && scope.EndpointKind != "other" {
		fallback := scope
		fallback.EndpointKind = "other"
		snapshot = catalog.snapshots[fallback.Key()]
	}
	if snapshot == nil {
		fallback := scope
		fallback.ModelFamily = strings.ToLower(strings.TrimSpace(scope.Platform)) + "-other"
		fallback.EndpointKind = "other"
		snapshot = catalog.snapshots[fallback.Key()]
	}
	if snapshot == nil {
		return nil, false
	}
	if maxAge > 0 && (snapshot.GeneratedAt.IsZero() || now.Sub(snapshot.GeneratedAt) > maxAge) {
		return nil, false
	}
	return snapshot, true
}

// LatestForPlatform supports a compact management-plane explanation when no
// request model/endpoint is available. It is never used to route traffic.
func (s *AtomicAPIKeyRoutingScoreStore) LatestForPlatform(platform string) (*APIKeyRoutingScoreSnapshot, bool) {
	if s == nil {
		return nil, false
	}
	catalog := s.current.Load()
	if catalog == nil {
		return nil, false
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	var latest *APIKeyRoutingScoreSnapshot
	for _, snapshot := range catalog.snapshots {
		if strings.ToLower(snapshot.Platform) != platform {
			continue
		}
		if latest == nil || snapshot.GeneratedAt.After(latest.GeneratedAt) {
			latest = snapshot
		}
	}
	return latest, latest != nil
}

func ValidateAPIKeyRoutingScoreSnapshot(snapshot *APIKeyRoutingScoreSnapshot) error {
	if snapshot == nil {
		return errors.New("routing score snapshot is nil")
	}
	if strings.TrimSpace(snapshot.Version) == "" || strings.TrimSpace(snapshot.StrategyVersion) == "" || strings.TrimSpace(snapshot.FeatureVersion) == "" {
		return errors.New("routing score snapshot versions are required")
	}
	scope := APIKeyRoutingScoreScope{Platform: snapshot.Platform, ModelFamily: snapshot.ModelFamily, EndpointKind: snapshot.EndpointKind}
	if !scope.Valid() {
		return errors.New("routing score snapshot scope is invalid")
	}
	if snapshot.GeneratedAt.IsZero() {
		return errors.New("routing score snapshot generated_at is required")
	}
	if len(snapshot.Groups) == 0 {
		return errors.New("routing score snapshot groups are required")
	}
	for groupID, observation := range snapshot.Groups {
		if groupID <= 0 || observation.GroupID != groupID {
			return fmt.Errorf("routing score snapshot group identity mismatch for %d", groupID)
		}
		if observation.SuccessRequests < 0 || observation.FailedRequests < 0 || observation.NormalizedRate < 0 || observation.PriceNormalizationFactor < 0 {
			return fmt.Errorf("routing score snapshot group %d contains negative metrics", groupID)
		}
		finite := []float64{
			observation.TTFTP50Ms, observation.DurationP50Ms, observation.CapacityScore,
			observation.NormalizedRate, observation.PriceNormalizationFactor, observation.Confidence, observation.PriceConfidence,
			observation.SmoothedSuccessRate, observation.CacheHitRate,
		}
		for _, value := range finite {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("routing score snapshot group %d contains non-finite metrics", groupID)
			}
		}
		if observation.CapacityScore > 1 || observation.Confidence > 1 || observation.PriceConfidence > 1 || observation.SmoothedSuccessRate > 1 || observation.CacheHitRate > 1 {
			return fmt.Errorf("routing score snapshot group %d contains out-of-range ratios", groupID)
		}
		if len(observation.DependencyDomains) > 4 {
			return fmt.Errorf("routing score snapshot group %d has too many dependency domains", groupID)
		}
		for _, domain := range observation.DependencyDomains {
			if !boundedRoutingDimension(domain) {
				return fmt.Errorf("routing score snapshot group %d has invalid dependency domain", groupID)
			}
		}
	}
	return nil
}

func cloneAPIKeyRoutingScoreSnapshot(snapshot *APIKeyRoutingScoreSnapshot) *APIKeyRoutingScoreSnapshot {
	if snapshot == nil {
		return nil
	}
	copy := *snapshot
	copy.ModelVersion = cloneStringPtr(snapshot.ModelVersion)
	copy.ExperimentID = cloneStringPtr(snapshot.ExperimentID)
	copy.Groups = make(map[int64]APIKeyRoutingGroupObservation, len(snapshot.Groups))
	for id, observation := range snapshot.Groups {
		observation.DependencyDomains = append([]string(nil), observation.DependencyDomains...)
		copy.Groups[id] = observation
	}
	return &copy
}

// APIKeyRoutingScoreCache publishes a complete immutable version before
// moving its scope's current pointer. Implementations must never expose a
// pointer to a missing or partially written version.
type APIKeyRoutingScoreCache interface {
	PublishAPIKeyRoutingScoreSnapshot(ctx context.Context, snapshot *APIKeyRoutingScoreSnapshot, ttl time.Duration) error
	LoadCurrentAPIKeyRoutingScoreSnapshot(ctx context.Context, scope APIKeyRoutingScoreScope) (*APIKeyRoutingScoreSnapshot, error)
	LoadAllCurrentAPIKeyRoutingScoreSnapshots(ctx context.Context) ([]*APIKeyRoutingScoreSnapshot, error)
}

var defaultAPIKeyRoutingScoreStore = NewAtomicAPIKeyRoutingScoreStore()

func DefaultAPIKeyRoutingScoreStore() *AtomicAPIKeyRoutingScoreStore {
	return defaultAPIKeyRoutingScoreStore
}
