package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// ProbeInternalKeyPrefix marks a database-only billing identity. It must never
// be accepted as an external API credential.
const ProbeInternalKeyPrefix = "sub2api-internal-probe-"

// ProbeInternalKeyRepository persists one billing identity per user and group.
type ProbeInternalKeyRepository struct{ db *sql.DB }

func NewProbeInternalKeyRepository(db *sql.DB) *ProbeInternalKeyRepository {
	return &ProbeInternalKeyRepository{db: db}
}

// GetOrCreate returns only the identity ID, never the stored key. Concurrent
// callers use the partial unique index to converge on the same row.
func (r *ProbeInternalKeyRepository) GetOrCreate(ctx context.Context, userID, groupID int64) (int64, error) {
	if userID <= 0 || groupID <= 0 {
		return 0, fmt.Errorf("invalid probe user or group")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return 0, fmt.Errorf("generate probe identity: %w", err)
	}
	key := ProbeInternalKeyPrefix + hex.EncodeToString(random[:])
	var id int64
	err := r.db.QueryRowContext(ctx, `INSERT INTO api_keys (user_id, group_id, key, name, status, is_internal_probe)
		VALUES ($1, $2, $3, '分组检测', 'disabled', TRUE)
		ON CONFLICT (user_id, group_id) WHERE deleted_at IS NULL AND group_id IS NOT NULL AND is_internal_probe
		DO UPDATE SET user_id = EXCLUDED.user_id
		RETURNING id`, userID, groupID, key).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("get probe identity: %w", err)
	}
	return id, nil
}
