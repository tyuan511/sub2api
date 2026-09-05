package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type routingScoreObservationSource struct {
	db           *sql.DB
	queryTimeout time.Duration
	refreshMu    sync.Mutex
	backfillEnd  time.Time
}

const (
	routingMetricRecentWindow = 10 * time.Minute
	routingMetricBackfillStep = 30 * time.Minute
	routingMetricHistory      = 24 * time.Hour
	routingMetricRetention    = 7 * 24 * time.Hour
)

// RefreshAPIKeyRoutingMetricBuckets runs under the score builder's singleton
// lease, using only its dedicated background pool. Channel Monitor V1/V2/off
// must not control routing reliability. Rewrite complete minutes atomically;
// every tick repairs late writes in the recent window and one bounded history
// chunk. The in-memory backfill cursor intentionally restarts after failover:
// rewrites are idempotent, and historical/late arrivals are revisited each cycle.
func (s *routingScoreObservationSource) RefreshAPIKeyRoutingMetricBuckets(ctx context.Context, now time.Time) error {
	if s == nil || s.db == nil {
		return errors.New("routing score observation database unavailable")
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	end := now.UTC().Truncate(time.Minute)
	recentStart := end.Add(-routingMetricRecentWindow)
	if err := s.rewriteRoutingMetricWindow(ctx, recentStart, end); err != nil {
		return err
	}
	cutoff := end.Add(-routingMetricHistory)
	historyEnd := s.backfillEnd
	if historyEnd.IsZero() || !historyEnd.After(cutoff) || historyEnd.After(recentStart) {
		historyEnd = recentStart
	}
	historyStart := historyEnd.Add(-routingMetricBackfillStep)
	if historyStart.Before(cutoff) {
		historyStart = cutoff
	}
	if err := s.rewriteRoutingMetricWindow(ctx, historyStart, historyEnd); err != nil {
		return err
	}
	s.backfillEnd = historyStart
	// Cleanup is also independent of monitor mode and bounded per table/tick.
	for _, table := range []string{"api_key_routing_health_metrics_1m", "api_key_routing_price_metrics_1m"} {
		queryCtx, cancel := s.withQueryTimeout(ctx)
		_, err := s.db.ExecContext(queryCtx, fmt.Sprintf(`DELETE FROM %s WHERE ctid IN (
			SELECT ctid FROM %s WHERE bucket_start < $1 ORDER BY bucket_start LIMIT 5000
		)`, table, table), end.Add(-routingMetricRetention))
		cancel()
		if err != nil {
			return fmt.Errorf("prune routing metric buckets: %w", err)
		}
	}
	return nil
}

func (s *routingScoreObservationSource) rewriteRoutingMetricWindow(ctx context.Context, start, end time.Time) (err error) {
	started := time.Now()
	defer func() {
		// RowsAffected counts rewritten buckets, not scanned source facts. Do not
		// misreport it as scanned_rows; source I/O is verified via EXPLAIN.
		service.DefaultRoutingRuntimeMetrics().RecordBackgroundQuery(time.Since(started), 0, err != nil)
	}()
	queryCtx, cancel := s.withQueryTimeout(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(queryCtx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	queries := []string{
		`DELETE FROM api_key_routing_health_metrics_1m WHERE bucket_start >= $1 AND bucket_start < $2`,
		`DELETE FROM api_key_routing_price_metrics_1m WHERE bucket_start >= $1 AND bucket_start < $2`,
		fmt.Sprintf(apiKeyRoutingHealthUsageMetricsSQL, channelMonitorV2PlatformSQL, channelMonitorV2ModelSQL, channelMonitorV2EndpointKindSQL),
		apiKeyRoutingHealthFailureMetricsSQL,
		fmt.Sprintf(apiKeyRoutingPriceMetricsSQL, channelMonitorV2PlatformSQL, channelMonitorV2ModelSQL),
	}
	for _, query := range queries {
		_, execErr := tx.ExecContext(queryCtx, query, start, end)
		if execErr != nil {
			return fmt.Errorf("rewrite routing metrics [%s,%s): %w", start, end, execErr)
		}
	}
	return tx.Commit()
}

func NewRoutingScoreObservationSource(pool *RoutingBackgroundDB, cfg *config.Config) service.APIKeyRoutingScoreObservationSource {
	if pool == nil {
		return &routingScoreObservationSource{}
	}
	timeout := 15 * time.Second
	if cfg != nil && cfg.Database.RoutingBackgroundQueryTimeoutSeconds > 0 {
		timeout = time.Duration(cfg.Database.RoutingBackgroundQueryTimeoutSeconds) * time.Second
	}
	return &routingScoreObservationSource{db: pool.DB, queryTimeout: timeout}
}

func (s *routingScoreObservationSource) LoadAPIKeyRoutingMetricAggregates(ctx context.Context, start, end time.Time) ([]service.APIKeyRoutingMetricAggregate, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("routing score observation database unavailable")
	}
	healthStarted := time.Now()
	var healthScannedRows int64
	healthRecorded := false
	recordHealth := func(failed bool) {
		if healthRecorded {
			return
		}
		healthRecorded = true
		service.DefaultRoutingRuntimeMetrics().RecordBackgroundQuery(time.Since(healthStarted), healthScannedRows, failed)
	}
	defer func() {
		if !healthRecorded {
			recordHealth(true)
		}
	}()
	queryCtx, cancel := s.withQueryTimeout(ctx)
	rows, err := s.db.QueryContext(queryCtx, `
		SELECT m.platform, m.group_id, m.model, m.endpoint_kind,
		       SUM(m.success_requests), SUM(m.failed_requests), SUM(m.capacity_overflow_requests),
		       SUM(m.input_tokens), SUM(m.output_tokens),
		       SUM(m.cache_creation_tokens), SUM(m.cache_read_tokens),
		       SUM(m.ttft_sum_ms), SUM(m.ttft_count),
		       SUM(m.duration_sum_ms), SUM(m.duration_count),
		       COALESCE(g.rate_multiplier, 1),
		       COALESCE(g.long_context_pricing_enabled, TRUE),
		       COALESCE(g.model_pricing, '[]'::jsonb),
		       COALESCE((
		         SELECT md5(string_agg(ag.account_id::text, ',' ORDER BY ag.account_id))
		         FROM account_groups ag
		         WHERE ag.group_id = m.group_id
		       ), ''),
		       COUNT(*), MAX(m.bucket_start)
		FROM api_key_routing_health_metrics_1m m
		LEFT JOIN groups g ON g.id = m.group_id
		WHERE m.bucket_start >= $1 AND m.bucket_start < $2 AND m.group_id > 0
		GROUP BY m.platform, m.group_id, m.model, m.endpoint_kind, g.rate_multiplier,
		         g.long_context_pricing_enabled, g.model_pricing
		ORDER BY m.platform, m.group_id, m.model, m.endpoint_kind
	`, start, end)
	if err != nil {
		cancel()
		recordHealth(true)
		return nil, err
	}
	defer cancel()
	defer func() { _ = rows.Close() }()
	result := make([]service.APIKeyRoutingMetricAggregate, 0)
	index := make(map[string]int)
	for rows.Next() {
		var item service.APIKeyRoutingMetricAggregate
		var longContextPricingEnabled bool
		var modelPricingJSON []byte
		var sourceBucketRows int64
		if err := rows.Scan(
			&item.Platform, &item.GroupID, &item.Model, &item.EndpointKind,
			&item.SuccessRequests, &item.FailedRequests, &item.CapacityOverflowRequests,
			&item.InputTokens, &item.OutputTokens, &item.CacheCreationTokens, &item.CacheReadTokens,
			&item.TTFTSumMs, &item.TTFTCount, &item.DurationSumMs, &item.DurationCount,
			&item.RateMultiplier, &longContextPricingEnabled, &modelPricingJSON,
			&item.AccountPoolDomain, &sourceBucketRows, &item.DataThrough,
		); err != nil {
			return nil, err
		}
		healthScannedRows += sourceBucketRows
		var modelPricing []service.ChannelModelPricing
		if len(modelPricingJSON) > 0 {
			if err := json.Unmarshal(modelPricingJSON, &modelPricing); err != nil {
				return nil, fmt.Errorf("decode routing price config for group %d: %w", item.GroupID, err)
			}
		}
		item.Group = &service.Group{
			ID: item.GroupID, Platform: item.Platform, RateMultiplier: item.RateMultiplier,
			LongContextPricingEnabled: longContextPricingEnabled, ModelPricing: modelPricing,
		}
		index[routingHealthAggregateKey(item.Platform, item.GroupID, item.Model, item.EndpointKind)] = len(result)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	cancel()
	recordHealth(false)

	priceStarted := time.Now()
	var priceScannedRows int64
	priceRecorded := false
	recordPrice := func(failed bool) {
		if priceRecorded {
			return
		}
		priceRecorded = true
		service.DefaultRoutingRuntimeMetrics().RecordBackgroundQuery(time.Since(priceStarted), priceScannedRows, failed)
	}
	defer func() {
		if !priceRecorded {
			recordPrice(true)
		}
	}()
	priceQueryCtx, priceCancel := s.withQueryTimeout(ctx)
	priceRows, err := s.db.QueryContext(priceQueryCtx, `
		SELECT platform, group_id, model, endpoint_kind, service_tier, context_bucket,
		       SUM(success_requests), SUM(input_tokens), SUM(image_input_tokens), SUM(output_tokens),
		       SUM(cache_creation_tokens), SUM(cache_creation_5m_tokens),
		       SUM(cache_creation_1h_tokens), SUM(cache_read_tokens), SUM(image_output_tokens), COUNT(*)
		FROM api_key_routing_price_metrics_1m
		WHERE bucket_start >= $1 AND bucket_start < $2 AND group_id > 0
		GROUP BY platform, group_id, model, endpoint_kind, service_tier, context_bucket
		ORDER BY platform, group_id, model, endpoint_kind, service_tier, context_bucket
	`, start, end)
	if err != nil {
		priceCancel()
		recordPrice(true)
		return nil, err
	}
	defer priceCancel()
	defer func() { _ = priceRows.Close() }()
	for priceRows.Next() {
		var platform string
		var groupID int64
		var sample service.APIKeyRoutingPriceSample
		var sourceBucketRows int64
		if err := priceRows.Scan(
			&platform, &groupID, &sample.Model, &sample.EndpointKind, &sample.ServiceTier, &sample.ContextBucket,
			&sample.SuccessRequests, &sample.InputTokens, &sample.ImageInputTokens, &sample.OutputTokens,
			&sample.CacheCreationTokens, &sample.CacheCreation5mTokens,
			&sample.CacheCreation1hTokens, &sample.CacheReadTokens, &sample.ImageOutputTokens, &sourceBucketRows,
		); err != nil {
			return nil, err
		}
		priceScannedRows += sourceBucketRows
		if position, ok := index[routingHealthAggregateKey(platform, groupID, sample.Model, sample.EndpointKind)]; ok {
			result[position].PriceSamples = append(result[position].PriceSamples, sample)
		}
	}
	err = priceRows.Err()
	recordPrice(err != nil)
	return result, err
}

func (s *routingScoreObservationSource) withQueryTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := s.queryTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}

func routingPriceAggregateKey(platform string, groupID int64, model string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", platform, groupID, model)
}

func routingHealthAggregateKey(platform string, groupID int64, model, endpoint string) string {
	endpoint = service.NormalizeAPIKeyRoutingEndpointKind(endpoint)
	return routingPriceAggregateKey(platform, groupID, model) + "\x00" + endpoint
}
