package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

const SettingKeyAPIKeyRoutingRollout = "api_key_routing_rollout"
const MaxAPIKeyRoutingRolloutUsers = 1000
const apiKeyRoutingRolloutTTL = 5 * time.Second

type APIKeyRoutingRolloutSettings struct {
	UserIDs []int64 `json:"user_ids"`
}

type cachedAPIKeyRoutingRollout struct {
	users     map[int64]struct{}
	expiresAt time.Time
}

func NormalizeAPIKeyRoutingRollout(settings APIKeyRoutingRolloutSettings) (APIKeyRoutingRolloutSettings, error) {
	if len(settings.UserIDs) > MaxAPIKeyRoutingRolloutUsers {
		return APIKeyRoutingRolloutSettings{}, fmt.Errorf("at most %d rollout users are allowed", MaxAPIKeyRoutingRolloutUsers)
	}
	ids := make([]int64, 0, len(settings.UserIDs))
	seen := make(map[int64]struct{}, len(settings.UserIDs))
	for _, id := range settings.UserIDs {
		if id <= 0 || id > 9007199254740991 {
			return APIKeyRoutingRolloutSettings{}, fmt.Errorf("user IDs must be positive safe integers")
		}
		if _, ok := seen[id]; !ok {
			ids = append(ids, id)
			seen[id] = struct{}{}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return APIKeyRoutingRolloutSettings{UserIDs: ids}, nil
}

func (s *SettingService) readAPIKeyRoutingRollout(ctx context.Context) (APIKeyRoutingRolloutSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyAPIKeyRoutingRollout)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && value == "") {
		return APIKeyRoutingRolloutSettings{UserIDs: []int64{}}, nil
	}
	if err != nil {
		return APIKeyRoutingRolloutSettings{}, err
	}
	var settings APIKeyRoutingRolloutSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return settings, err
	}
	return NormalizeAPIKeyRoutingRollout(settings)
}

func (s *SettingService) storeAPIKeyRoutingRollout(settings APIKeyRoutingRolloutSettings, ttl time.Duration) {
	entry := &cachedAPIKeyRoutingRollout{users: make(map[int64]struct{}, len(settings.UserIDs)), expiresAt: time.Now().Add(ttl)}
	for _, id := range settings.UserIDs {
		entry.users[id] = struct{}{}
	}
	s.apiKeyRoutingRolloutCache.Store(entry)
}

// Admin reads are fresh. The same mutex serializes refresh and save so an old
// in-flight database read cannot restore a user after a successful removal.
func (s *SettingService) GetAPIKeyRoutingRolloutSettings(ctx context.Context) (APIKeyRoutingRolloutSettings, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	s.apiKeyRoutingRolloutMu.Lock()
	defer s.apiKeyRoutingRolloutMu.Unlock()
	settings, err := s.readAPIKeyRoutingRollout(ctx)
	if err == nil {
		s.storeAPIKeyRoutingRollout(settings, apiKeyRoutingRolloutTTL)
	}
	return settings, err
}

func (s *SettingService) SetAPIKeyRoutingRolloutSettings(ctx context.Context, settings APIKeyRoutingRolloutSettings) error {
	normalized, err := NormalizeAPIKeyRoutingRollout(settings)
	if err != nil {
		return err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	s.apiKeyRoutingRolloutMu.Lock()
	defer s.apiKeyRoutingRolloutMu.Unlock()
	if err := s.settingRepo.Set(ctx, SettingKeyAPIKeyRoutingRollout, string(data)); err != nil {
		return err
	}
	s.storeAPIKeyRoutingRollout(normalized, apiKeyRoutingRolloutTTL)
	return nil
}

// One bounded refresh per process per TTL, not per user/key. The steady-state
// request path is one atomic load and an O(1) lookup; no Redis or SQL call.
// Missing/corrupt settings and refresh errors fail closed to the legacy route.
func (s *SettingService) IsAPIKeyRoutingRolloutUser(ctx context.Context, userID int64) bool {
	if s == nil || s.settingRepo == nil || userID <= 0 {
		return false
	}
	if cached := s.apiKeyRoutingRolloutCache.Load(); cached != nil && time.Now().Before(cached.expiresAt) {
		_, allowed := cached.users[userID]
		return allowed
	}
	s.apiKeyRoutingRolloutMu.Lock()
	defer s.apiKeyRoutingRolloutMu.Unlock()
	if cached := s.apiKeyRoutingRolloutCache.Load(); cached != nil && time.Now().Before(cached.expiresAt) {
		_, allowed := cached.users[userID]
		return allowed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	settings, err := s.readAPIKeyRoutingRollout(dbCtx)
	if err != nil {
		s.storeAPIKeyRoutingRollout(APIKeyRoutingRolloutSettings{}, time.Second)
		return false
	}
	s.storeAPIKeyRoutingRollout(settings, apiKeyRoutingRolloutTTL)
	_, allowed := s.apiKeyRoutingRolloutCache.Load().users[userID]
	return allowed
}
