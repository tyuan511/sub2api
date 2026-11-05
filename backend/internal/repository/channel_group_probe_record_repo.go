package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ChannelGroupProbeRecord is a shared group probe and its optional result.
type ChannelGroupProbeRecord struct {
	ID           int64
	GroupID      int64
	UserID       int64
	Kind         string
	Model        string
	Status       string
	DurationMS   *int64
	SVG          *string
	ErrorMessage *string
	CreatedAt    time.Time
	CompletedAt  *time.Time
	ActualCost   *float64
}

type ChannelGroupProbeResult struct {
	Status       string
	DurationMS   *int64
	SVG          *string
	ErrorMessage *string
}

type ChannelGroupProbeRecordRepository struct{ db *sql.DB }

func NewChannelGroupProbeRecordRepository(db *sql.DB) *ChannelGroupProbeRecordRepository {
	return &ChannelGroupProbeRecordRepository{db: db}
}

// Create inserts a running record.
func (r *ChannelGroupProbeRecordRepository) Create(ctx context.Context, groupID, userID int64, kind, model string) (*ChannelGroupProbeRecord, error) {
	row := r.db.QueryRowContext(ctx, `INSERT INTO channel_group_probe_records (group_id, user_id, kind, model, status)
		VALUES ($1, $2, $3, $4, 'running') RETURNING id, created_at`, groupID, userID, kind, model)
	record := &ChannelGroupProbeRecord{GroupID: groupID, UserID: userID, Kind: kind, Model: model, Status: "running"}
	if err := row.Scan(&record.ID, &record.CreatedAt); err != nil {
		return nil, err
	}
	return record, nil
}

// SetBillingIdentity persists correlation before any billable request is sent.
func (r *ChannelGroupProbeRecordRepository) SetBillingIdentity(ctx context.Context, id, groupID, userID, keyID int64, requestID string) error {
	if keyID <= 0 || requestID == "" {
		return fmt.Errorf("invalid probe billing identity")
	}
	res, err := r.db.ExecContext(ctx, `UPDATE channel_group_probe_records
		SET billing_api_key_id = $4, billing_request_id = $5
		WHERE id = $1 AND group_id = $2 AND user_id = $3 AND status = 'running' AND billing_request_id IS NULL`, id, groupID, userID, keyID, requestID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// Complete transitions a matching running record to a terminal state. A missing,
// mismatched, or already completed record returns sql.ErrNoRows.
func (r *ChannelGroupProbeRecordRepository) Complete(ctx context.Context, id, groupID, userID int64, result ChannelGroupProbeResult) error {
	if result.Status != "succeeded" && result.Status != "failed" {
		return fmt.Errorf("invalid terminal probe status %q", result.Status)
	}
	res, err := r.db.ExecContext(ctx, `UPDATE channel_group_probe_records
		SET status = $4, duration_ms = $5, svg = $6, error_message = $7, completed_at = NOW()
		WHERE id = $1 AND group_id = $2 AND user_id = $3 AND status = 'running'`,
		id, groupID, userID, result.Status, result.DurationMS, result.SVG, result.ErrorMessage)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RecoverStaleRunning marks records left behind by a crashed or interrupted worker as failed.
// It is deliberately conservative: the executor timeout is two minutes, so five minutes
// gives a healthy request enough room to finish before recovery becomes visible.
func (r *ChannelGroupProbeRecordRepository) RecoverStaleRunning(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `UPDATE channel_group_probe_records
		SET status = 'failed', error_message = '检测任务已中断', completed_at = NOW()
		WHERE status = 'running' AND created_at < NOW() - INTERVAL '5 minutes'`)
	return err
}

// List returns at most limit records; a non-positive limit defaults to 50.
func (r *ChannelGroupProbeRecordRepository) List(ctx context.Context, groupID, viewerUserID int64, limit int) ([]ChannelGroupProbeRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	if err := r.RecoverStaleRunning(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.id, p.group_id, p.user_id, p.kind, p.model, p.status, p.duration_ms, p.svg, p.error_message, p.created_at, p.completed_at, cost.actual_cost
		FROM channel_group_probe_records p
		LEFT JOIN LATERAL (
			SELECT CASE WHEN COUNT(*) = 1 THEN MAX(u.actual_cost) END AS actual_cost
			FROM usage_logs u
			WHERE p.user_id = $2 AND u.request_id = p.billing_request_id AND u.api_key_id = p.billing_api_key_id
				AND u.user_id = p.user_id AND u.group_id = p.group_id
				AND (EXISTS (SELECT 1 FROM usage_billing_dedup d WHERE d.request_id = u.request_id AND d.api_key_id = u.api_key_id)
					OR EXISTS (SELECT 1 FROM usage_billing_dedup_archive d WHERE d.request_id = u.request_id AND d.api_key_id = u.api_key_id))
		) cost ON true
		WHERE p.group_id = $1 AND (p.user_id = $2 OR p.status IN ('succeeded', 'failed'))
		ORDER BY p.created_at DESC, p.id DESC LIMIT $3`, groupID, viewerUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]ChannelGroupProbeRecord, 0)
	for rows.Next() {
		var record ChannelGroupProbeRecord
		if err := rows.Scan(&record.ID, &record.GroupID, &record.UserID, &record.Kind, &record.Model, &record.Status, &record.DurationMS, &record.SVG, &record.ErrorMessage, &record.CreatedAt, &record.CompletedAt, &record.ActualCost); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}
