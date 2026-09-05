package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	apiKeyRouteConfigOutboxBatchSize       = 100
	apiKeyRouteConfigOutboxPollInterval    = 500 * time.Millisecond
	apiKeyRouteConfigOutboxLease           = 30 * time.Second
	apiKeyRouteConfigOutboxRedisTimeout    = 2 * time.Second
	apiKeyRouteConfigOutboxConcurrency     = 16
	apiKeyRouteVersionGuardTTL             = 24 * time.Hour
	apiKeyRouteConfigOutboxRetention       = 7 * 24 * time.Hour
	apiKeyRouteConfigOutboxCleanupInterval = time.Hour
	apiKeyRouteConfigOutboxCleanupLimit    = 1000
)

type APIKeyRouteConfigOutboxEvent struct {
	ID                   int64
	EventKey             string
	APIKeyID             int64
	OldRouteVersion      int64
	RouteVersion         int64
	OldDependencyVersion int64
	DependencyVersion    int64
	EventType            string
	AuthCacheKey         string
	Attempts             int
	CreatedAt            time.Time
}

type APIKeyRouteConfigInvalidationMessage struct {
	EventID              string `json:"event_id"`
	APIKeyID             int64  `json:"api_key_id"`
	OldRouteVersion      int64  `json:"old_route_version"`
	NewRouteVersion      int64  `json:"new_route_version"`
	OldDependencyVersion int64  `json:"old_dependency_version"`
	NewDependencyVersion int64  `json:"new_dependency_version"`
	Reason               string `json:"reason"`
}

type APIKeyRouteConfigOutboxStats struct {
	Pending           int64
	DeliveredRetained int64
	OldestCreatedAt   *time.Time
	MaxAttempts       int
	LastError         string
}

type APIKeyRouteConfigOutboxRepository interface {
	ClaimAPIKeyRouteConfigEvents(ctx context.Context, workerID string, limit int, lease time.Duration) ([]APIKeyRouteConfigOutboxEvent, error)
	MarkAPIKeyRouteConfigEventDelivered(ctx context.Context, id int64, workerID string, deliveredAt time.Time) error
	RetryAPIKeyRouteConfigEvent(ctx context.Context, id int64, workerID string, availableAt time.Time, lastError string) error
	APIKeyRouteConfigOutboxStats(ctx context.Context) (APIKeyRouteConfigOutboxStats, error)
	CleanupDeliveredAPIKeyRouteConfigEvents(ctx context.Context, before time.Time, limit int) (int64, error)
}

type APIKeyRouteConfigOutboxHealth struct {
	Running           bool          `json:"running"`
	Alerting          bool          `json:"alerting"`
	Processed         uint64        `json:"processed"`
	Failures          uint64        `json:"failures"`
	Retries           uint64        `json:"retries"`
	Cleaned           uint64        `json:"cleaned"`
	Pending           int64         `json:"pending"`
	DeliveredRetained int64         `json:"delivered_retained"`
	OldestLag         time.Duration `json:"oldest_lag"`
	MaxAttempts       int           `json:"max_attempts"`
	LastError         string        `json:"last_error,omitempty"`
	StatsError        string        `json:"stats_error,omitempty"`
	HealthySLA        time.Duration `json:"healthy_sla"`
	RecoverySLA       time.Duration `json:"recovery_sla"`
}

type APIKeyRouteConfigOutboxWorker struct {
	repo     APIKeyRouteConfigOutboxRepository
	cache    APIKeyRouteConfigCache
	local    *APIKeyService
	workerID string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	start  sync.Once
	stop   sync.Once

	running     atomic.Bool
	processed   atomic.Uint64
	failures    atomic.Uint64
	retries     atomic.Uint64
	cleaned     atomic.Uint64
	lastCleanup atomic.Int64
	lastError   atomic.Value
}

func NewAPIKeyRouteConfigOutboxWorker(repo APIKeyRouteConfigOutboxRepository, cache APIKeyCache, local ...*APIKeyService) *APIKeyRouteConfigOutboxWorker {
	ctx, cancel := context.WithCancel(context.Background())
	routeCache, _ := cache.(APIKeyRouteConfigCache)
	worker := &APIKeyRouteConfigOutboxWorker{
		repo: repo, cache: routeCache, workerID: uuid.NewString(), ctx: ctx, cancel: cancel,
	}
	if len(local) > 0 {
		worker.local = local[0]
	}
	worker.lastError.Store("")
	return worker
}

func (w *APIKeyRouteConfigOutboxWorker) Start() {
	if w == nil || w.repo == nil || w.cache == nil {
		return
	}
	w.start.Do(func() {
		w.running.Store(true)
		w.wg.Add(1)
		go w.run()
	})
}

func (w *APIKeyRouteConfigOutboxWorker) Stop() {
	if w == nil {
		return
	}
	w.stop.Do(func() {
		w.cancel()
		w.wg.Wait()
		w.running.Store(false)
	})
}

func (w *APIKeyRouteConfigOutboxWorker) run() {
	defer w.wg.Done()
	defer w.running.Store(false)
	ticker := time.NewTicker(apiKeyRouteConfigOutboxPollInterval)
	defer ticker.Stop()
	for {
		if err := w.processBatch(w.ctx); err != nil && w.ctx.Err() == nil {
			w.recordFailure(err)
		}
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *APIKeyRouteConfigOutboxWorker) processBatch(ctx context.Context) error {
	w.maybeCleanup(ctx, time.Now().UTC())
	events, err := w.repo.ClaimAPIKeyRouteConfigEvents(ctx, w.workerID, apiKeyRouteConfigOutboxBatchSize, apiKeyRouteConfigOutboxLease)
	if err != nil {
		return fmt.Errorf("claim API key route config events: %w", err)
	}
	semaphore := make(chan struct{}, apiKeyRouteConfigOutboxConcurrency)
	var wg sync.WaitGroup
	for i := range events {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case semaphore <- struct{}{}:
		}
		wg.Add(1)
		go func(event APIKeyRouteConfigOutboxEvent) {
			defer wg.Done()
			defer func() { <-semaphore }()
			w.processEvent(ctx, event)
		}(events[i])
	}
	wg.Wait()
	return nil
}

func (w *APIKeyRouteConfigOutboxWorker) processEvent(parent context.Context, event APIKeyRouteConfigOutboxEvent) {
	if err := validateAPIKeyRouteConfigOutboxEvent(event); err != nil {
		w.retryEvent(event, err)
		return
	}
	if w.local != nil {
		w.local.invalidateLocalAuthCache(event.AuthCacheKey)
	}

	ctx, cancel := context.WithTimeout(parent, apiKeyRouteConfigOutboxRedisTimeout)
	err := w.cache.SetAPIKeyRoutingGuards(ctx, event.APIKeyID, event.RouteVersion, event.DependencyVersion, apiKeyRouteVersionGuardTTL)
	if err == nil {
		err = w.cache.DeleteAuthCache(ctx, event.AuthCacheKey)
	}
	if err == nil {
		err = w.cache.PublishAuthCacheInvalidation(ctx, event.AuthCacheKey)
	}
	if err == nil {
		err = w.cache.PublishAPIKeyRouteConfigInvalidation(ctx, APIKeyRouteConfigInvalidationMessage{
			EventID: event.EventKey, APIKeyID: event.APIKeyID, OldRouteVersion: event.OldRouteVersion,
			NewRouteVersion: event.RouteVersion, OldDependencyVersion: event.OldDependencyVersion,
			NewDependencyVersion: event.DependencyVersion, Reason: event.EventType,
		})
	}
	cancel()
	if err != nil {
		w.retryEvent(event, err)
		return
	}

	ackCtx, ackCancel := context.WithTimeout(context.Background(), 2*time.Second)
	err = w.repo.MarkAPIKeyRouteConfigEventDelivered(ackCtx, event.ID, w.workerID, time.Now().UTC())
	ackCancel()
	if err != nil {
		w.recordFailure(fmt.Errorf("ack API key route config event %d: %w", event.ID, err))
		return
	}
	w.processed.Add(1)
	w.lastError.Store("")
}

func validateAPIKeyRouteConfigOutboxEvent(event APIKeyRouteConfigOutboxEvent) error {
	if event.ID <= 0 || event.APIKeyID <= 0 || event.RouteVersion <= 0 || strings.TrimSpace(event.EventKey) == "" {
		return errors.New("invalid API key route config outbox identity")
	}
	if event.OldRouteVersion < 0 || event.OldRouteVersion > event.RouteVersion ||
		event.OldDependencyVersion < 0 || event.OldDependencyVersion > event.DependencyVersion ||
		(event.OldRouteVersion == event.RouteVersion && event.OldDependencyVersion == event.DependencyVersion) {
		return errors.New("invalid API key route config version transition")
	}
	if event.DependencyVersion <= 0 {
		return errors.New("invalid API key route dependency version")
	}
	if len(event.AuthCacheKey) != 64 || strings.ToLower(event.AuthCacheKey) != event.AuthCacheKey {
		return errors.New("invalid API key auth cache digest")
	}
	if _, err := hex.DecodeString(event.AuthCacheKey); err != nil {
		return errors.New("invalid API key auth cache digest")
	}
	return nil
}

func (w *APIKeyRouteConfigOutboxWorker) retryEvent(event APIKeyRouteConfigOutboxEvent, cause error) {
	w.recordFailure(cause)
	w.retries.Add(1)
	retryAt := time.Now().UTC().Add(apiKeyRouteConfigRetryDelay(event.Attempts + 1))
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 2*time.Second)
	err := w.repo.RetryAPIKeyRouteConfigEvent(retryCtx, event.ID, w.workerID, retryAt, boundedAPIKeyRouteConfigError(cause))
	retryCancel()
	if err != nil {
		w.recordFailure(fmt.Errorf("release failed API key route config event %d: %w", event.ID, err))
	}
}

func apiKeyRouteConfigRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := min(attempt-1, 8)
	base := time.Second * time.Duration(1<<shift)
	if base > 5*time.Minute {
		base = 5 * time.Minute
	}
	return time.Duration(float64(base) * (0.8 + rand.Float64()*0.4))
}

func boundedAPIKeyRouteConfigError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 512 {
		return message[:512]
	}
	return message
}

func (w *APIKeyRouteConfigOutboxWorker) recordFailure(err error) {
	if err == nil {
		return
	}
	w.failures.Add(1)
	w.lastError.Store(boundedAPIKeyRouteConfigError(err))
	slog.Warn("API key route config outbox processing failed", "error", err)
}

func (w *APIKeyRouteConfigOutboxWorker) maybeCleanup(ctx context.Context, now time.Time) {
	previous := w.lastCleanup.Load()
	if previous > 0 && now.Sub(time.Unix(0, previous)) < apiKeyRouteConfigOutboxCleanupInterval {
		return
	}
	if !w.lastCleanup.CompareAndSwap(previous, now.UnixNano()) {
		return
	}
	count, err := w.repo.CleanupDeliveredAPIKeyRouteConfigEvents(ctx, now.Add(-apiKeyRouteConfigOutboxRetention), apiKeyRouteConfigOutboxCleanupLimit)
	if err != nil {
		w.recordFailure(fmt.Errorf("cleanup delivered API key route config events: %w", err))
		return
	}
	if count > 0 {
		w.cleaned.Add(uint64(count))
	}
}

func (w *APIKeyRouteConfigOutboxWorker) Health(ctx context.Context) APIKeyRouteConfigOutboxHealth {
	health := APIKeyRouteConfigOutboxHealth{
		HealthySLA: 5 * time.Second, RecoverySLA: 6 * time.Minute,
	}
	if w == nil {
		return health
	}
	health.Running = w.running.Load()
	health.Processed = w.processed.Load()
	health.Failures = w.failures.Load()
	health.Retries = w.retries.Load()
	health.Cleaned = w.cleaned.Load()
	if value := w.lastError.Load(); value != nil {
		health.LastError, _ = value.(string)
	}
	if w.repo == nil {
		return health
	}
	stats, err := w.repo.APIKeyRouteConfigOutboxStats(ctx)
	if err != nil {
		health.StatsError = boundedAPIKeyRouteConfigError(err)
		health.Alerting = true
		return health
	}
	health.Pending = stats.Pending
	health.DeliveredRetained = stats.DeliveredRetained
	health.MaxAttempts = stats.MaxAttempts
	if health.LastError == "" {
		health.LastError = stats.LastError
	}
	if stats.OldestCreatedAt != nil {
		health.OldestLag = time.Since(*stats.OldestCreatedAt)
		if health.OldestLag < 0 {
			health.OldestLag = 0
		}
	}
	health.Alerting = !health.Running || health.OldestLag > health.HealthySLA || health.MaxAttempts >= 5
	return health
}

func ProvideAPIKeyRouteConfigOutboxWorker(repo APIKeyRouteConfigOutboxRepository, cache APIKeyCache, apiKeyService *APIKeyService) *APIKeyRouteConfigOutboxWorker {
	worker := NewAPIKeyRouteConfigOutboxWorker(repo, cache, apiKeyService)
	worker.Start()
	return worker
}
