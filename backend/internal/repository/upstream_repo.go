package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type upstreamRepository struct{ db *sql.DB }

func NewUpstreamRepository(db *sql.DB) service.UpstreamRepository { return &upstreamRepository{db: db} }

const upstreamColumns = `id, name, type, website_url, base_url, balance_api_key_encrypted,
    access_token_encrypted, user_id, balance_threshold, balance_multiplier, quota_per_unit,
    check_interval_seconds, enabled, balance, balance_checked_at, last_check_at, last_error,
    revision, created_at, updated_at`

func scanUpstream(row interface{ Scan(...any) error }) (*service.Upstream, error) {
	var u service.Upstream
	err := row.Scan(&u.ID, &u.Name, &u.Type, &u.WebsiteURL, &u.BaseURL,
		&u.BalanceAPIKeyEncrypted, &u.AccessTokenEncrypted, &u.UserID, &u.BalanceThreshold,
		&u.BalanceMultiplier, &u.QuotaPerUnit, &u.CheckIntervalSeconds, &u.Enabled,
		&u.Balance, &u.BalanceCheckedAt, &u.LastCheckAt, &u.LastError, &u.Revision, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUpstreamNotFound
	}
	if err != nil {
		return nil, err
	}
	u.HasBalanceAPIKey = u.BalanceAPIKeyEncrypted != ""
	u.HasAccessToken = u.AccessTokenEncrypted != ""
	return &u, nil
}

func (r *upstreamRepository) List(ctx context.Context) ([]service.Upstream, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+upstreamColumns+` FROM upstreams ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []service.Upstream{}
	for rows.Next() {
		u, err := scanUpstream(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *u)
	}
	return result, rows.Err()
}

func (r *upstreamRepository) Get(ctx context.Context, id int64) (*service.Upstream, error) {
	return scanUpstream(r.db.QueryRowContext(ctx, `SELECT `+upstreamColumns+` FROM upstreams WHERE id=$1`, id))
}

func (r *upstreamRepository) Save(ctx context.Context, u *service.Upstream) (*service.Upstream, error) {
	args := []any{u.Name, u.Type, u.WebsiteURL, u.BaseURL, u.BalanceAPIKeyEncrypted,
		u.AccessTokenEncrypted, u.UserID, u.BalanceThreshold, u.BalanceMultiplier,
		u.QuotaPerUnit, u.CheckIntervalSeconds, u.Enabled}
	if u.ID == 0 {
		return scanUpstream(r.db.QueryRowContext(ctx, `INSERT INTO upstreams
            (name,type,website_url,base_url,balance_api_key_encrypted,access_token_encrypted,user_id,
             balance_threshold,balance_multiplier,quota_per_unit,check_interval_seconds,enabled)
            VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+upstreamColumns, args...))
	}
	args = append(args, u.ID, u.Revision)
	// Changing the balance source or conversion invalidates the previous result.
	result, err := scanUpstream(r.db.QueryRowContext(ctx, `UPDATE upstreams SET
        name=$1,type=$2::text,website_url=$3,base_url=$4,balance_api_key_encrypted=$5,access_token_encrypted=$6,
        user_id=$7,balance_threshold=$8,balance_multiplier=$9,quota_per_unit=$10,
        check_interval_seconds=$11,enabled=$12,updated_at=NOW(),revision=revision+1,next_check_at=NOW(),
        balance=CASE WHEN type<>$2 OR base_url<>$4 OR (type='newapi' AND website_url<>$3) OR balance_api_key_encrypted<>$5 OR access_token_encrypted<>$6
            OR user_id<>$7 OR balance_multiplier<>$9 OR quota_per_unit<>$10 THEN NULL ELSE balance END,
        balance_checked_at=CASE WHEN type<>$2 OR base_url<>$4 OR (type='newapi' AND website_url<>$3) OR balance_api_key_encrypted<>$5 OR access_token_encrypted<>$6
            OR user_id<>$7 OR balance_multiplier<>$9 OR quota_per_unit<>$10 THEN NULL ELSE balance_checked_at END,
        last_error='', low_balance_notified=CASE WHEN balance_threshold<>$8 OR type<>$2 OR base_url<>$4 OR (type='newapi' AND website_url<>$3)
            OR balance_api_key_encrypted<>$5 OR access_token_encrypted<>$6 OR user_id<>$7
            OR balance_multiplier<>$9 OR quota_per_unit<>$10 THEN FALSE ELSE low_balance_notified END
        WHERE id=$13 AND revision=$14 RETURNING `+upstreamColumns, args...))
	if errors.Is(err, service.ErrUpstreamNotFound) {
		return nil, service.ErrUpstreamInvalid.WithCause(errors.New("Upstream was changed or deleted; reload and try again"))
	}
	return result, err
}

func (r *upstreamRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM upstreams WHERE id=$1`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return service.ErrUpstreamNotFound
	}
	return err
}

func (r *upstreamRepository) Claim(ctx context.Context, id int64, force bool) (*service.Upstream, error) {
	u, err := scanUpstream(r.db.QueryRowContext(ctx, `UPDATE upstreams
        SET next_check_at=NOW()+make_interval(secs => check_interval_seconds), last_check_at=NOW()
        WHERE id=$1 AND enabled AND type<>'other' AND
            (next_check_at<=NOW() OR ($2 AND (last_check_at IS NULL OR last_check_at<NOW()-INTERVAL '30 seconds')))
        RETURNING `+upstreamColumns, id, force))
	if errors.Is(err, service.ErrUpstreamNotFound) {
		return nil, nil
	}
	return u, err
}

func (r *upstreamRepository) RecordBalance(ctx context.Context, u *service.Upstream, balance *float64, lastError string, notify bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var notified bool
	if err := tx.QueryRowContext(ctx, `SELECT low_balance_notified FROM upstreams WHERE id=$1 AND revision=$2 FOR UPDATE`, u.ID, u.Revision).Scan(&notified); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if balance == nil {
		_, err = tx.ExecContext(ctx, `UPDATE upstreams SET last_error=$2 WHERE id=$1`, u.ID, lastError)
	} else {
		if *balance >= u.BalanceThreshold {
			notified = false
		}
		if notify && *balance < u.BalanceThreshold && !notified {
			payload, marshalErr := json.Marshal(service.UpstreamBalanceNotification{
				UpstreamID: u.ID, Name: u.Name, WebsiteURL: u.WebsiteURL, Balance: *balance,
				Threshold: u.BalanceThreshold, At: time.Now(),
			})
			if marshalErr != nil {
				return marshalErr
			}
			result, insertErr := tx.ExecContext(ctx, `INSERT INTO support_notification_outbox (event_type,target_admin_id,payload)
                SELECT $1,admin_id,$2 FROM admin_telegram_bindings WHERE enabled=TRUE`, service.UpstreamLowBalanceEvent, string(payload))
			if insertErr != nil {
				return fmt.Errorf("enqueue upstream alert: %w", insertErr)
			}
			count, countErr := result.RowsAffected()
			if countErr != nil {
				return countErr
			}
			notified = count > 0
		}
		_, err = tx.ExecContext(ctx, `UPDATE upstreams SET balance=$2,balance_checked_at=NOW(),last_error='',low_balance_notified=$3 WHERE id=$1`, u.ID, *balance, notified)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
