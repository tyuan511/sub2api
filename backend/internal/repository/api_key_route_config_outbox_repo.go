package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type apiKeyRouteConfigOutboxRepository struct {
	db *sql.DB
}

func NewAPIKeyRouteConfigOutboxRepository(db *sql.DB) service.APIKeyRouteConfigOutboxRepository {
	return &apiKeyRouteConfigOutboxRepository{db: db}
}

func (r *apiKeyRouteConfigOutboxRepository) ClaimAPIKeyRouteConfigEvents(ctx context.Context, workerID string, limit int, lease time.Duration) ([]service.APIKeyRouteConfigOutboxEvent, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nil API key route config outbox database")
	}
	if limit <= 0 {
		limit = 100
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 30
	}
	rows, err := r.db.QueryContext(ctx, `
WITH candidates AS (
    SELECT id
    FROM api_key_route_config_outbox
    WHERE delivered_at IS NULL
      AND available_at <= NOW()
      AND (claimed_at IS NULL OR claimed_at < NOW() - ($3 * INTERVAL '1 second'))
    ORDER BY id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE api_key_route_config_outbox AS o
SET claimed_at = NOW(), claimed_by = $1, updated_at = NOW()
FROM candidates AS c
WHERE o.id = c.id
RETURNING o.id, o.event_key, o.api_key_id,
          COALESCE((o.payload->>'old_route_version')::bigint, GREATEST(o.route_version - 1, 0)),
          o.route_version,
          COALESCE((o.payload->>'old_dependency_version')::bigint, 0),
          COALESCE((o.payload->>'dependency_version')::bigint, 1),
          o.event_type, COALESCE(o.payload->>'auth_cache_key', ''),
          o.attempts, o.created_at
`, workerID, limit, leaseSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	events := make([]service.APIKeyRouteConfigOutboxEvent, 0, limit)
	for rows.Next() {
		var event service.APIKeyRouteConfigOutboxEvent
		if err := rows.Scan(&event.ID, &event.EventKey, &event.APIKeyID, &event.OldRouteVersion, &event.RouteVersion,
			&event.OldDependencyVersion, &event.DependencyVersion,
			&event.EventType, &event.AuthCacheKey, &event.Attempts, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *apiKeyRouteConfigOutboxRepository) MarkAPIKeyRouteConfigEventDelivered(ctx context.Context, id int64, workerID string, deliveredAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE api_key_route_config_outbox
SET delivered_at = $3, last_error = NULL, claimed_at = NULL, claimed_by = NULL, updated_at = NOW()
WHERE id = $1 AND claimed_by = $2 AND delivered_at IS NULL
`, id, workerID, deliveredAt)
	if err != nil {
		return err
	}
	return requireAPIKeyRouteConfigClaim(result, id, workerID)
}

func (r *apiKeyRouteConfigOutboxRepository) RetryAPIKeyRouteConfigEvent(ctx context.Context, id int64, workerID string, availableAt time.Time, lastError string) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE api_key_route_config_outbox
SET attempts = attempts + 1, available_at = $3, last_error = $4,
    claimed_at = NULL, claimed_by = NULL, updated_at = NOW()
WHERE id = $1 AND claimed_by = $2 AND delivered_at IS NULL
`, id, workerID, availableAt, lastError)
	if err != nil {
		return err
	}
	return requireAPIKeyRouteConfigClaim(result, id, workerID)
}

func requireAPIKeyRouteConfigClaim(result sql.Result, id int64, workerID string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("API key route config claim %d is no longer owned by %s", id, workerID)
	}
	return nil
}

func (r *apiKeyRouteConfigOutboxRepository) APIKeyRouteConfigOutboxStats(ctx context.Context) (service.APIKeyRouteConfigOutboxStats, error) {
	var (
		stats     service.APIKeyRouteConfigOutboxStats
		oldest    sql.NullTime
		lastError sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE delivered_at IS NULL),
       COUNT(*) FILTER (WHERE delivered_at IS NOT NULL),
       MIN(created_at) FILTER (WHERE delivered_at IS NULL),
       COALESCE(MAX(attempts) FILTER (WHERE delivered_at IS NULL), 0),
       (SELECT last_error
        FROM api_key_route_config_outbox
        WHERE delivered_at IS NULL AND last_error IS NOT NULL
        ORDER BY available_at DESC, id DESC
        LIMIT 1)
FROM api_key_route_config_outbox
`).Scan(&stats.Pending, &stats.DeliveredRetained, &oldest, &stats.MaxAttempts, &lastError)
	if err != nil {
		return stats, err
	}
	if oldest.Valid {
		value := oldest.Time
		stats.OldestCreatedAt = &value
	}
	if lastError.Valid {
		stats.LastError = lastError.String
	}
	return stats, nil
}

func (r *apiKeyRouteConfigOutboxRepository) CleanupDeliveredAPIKeyRouteConfigEvents(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	result, err := r.db.ExecContext(ctx, `
WITH expired AS (
    SELECT id
    FROM api_key_route_config_outbox
    WHERE delivered_at IS NOT NULL AND delivered_at < $1
    ORDER BY delivered_at ASC, id ASC
    LIMIT $2
)
DELETE FROM api_key_route_config_outbox AS o
USING expired AS e
WHERE o.id = e.id
`, before, limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
