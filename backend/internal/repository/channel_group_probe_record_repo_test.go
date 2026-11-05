package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestProbeCreateOrGetRunningConflictAndRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewChannelGroupProbeRecordRepository(db)
	now := time.Now().UTC()
	mock.ExpectExec(`(?s)UPDATE channel_group_probe_records.*group_id = \$1 AND user_id = \$2.*5 minutes`).WithArgs(int64(1), int64(2)).WillReturnResult(sqlmock.NewResult(0, 0))
	insert := `(?s)INSERT INTO channel_group_probe_records.*ON CONFLICT \(group_id, user_id\) WHERE status = 'running' DO NOTHING`
	lookup := `(?s)SELECT id, kind, model, created_at.*group_id = \$1 AND user_id = \$2 AND status = 'running'`
	mock.ExpectQuery(insert).WithArgs(int64(1), int64(2), "pelican", "new").WillReturnError(sql.ErrNoRows)
	// The winner may finish after the conflict and before the lookup.
	mock.ExpectQuery(lookup).WithArgs(int64(1), int64(2)).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(insert).WithArgs(int64(1), int64(2), "pelican", "new").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(lookup).WithArgs(int64(1), int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "kind", "model", "created_at"}).AddRow(9, "connectivity", "existing", now))
	record, created, err := repo.CreateOrGetRunning(context.Background(), 1, 2, "pelican", "new")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, int64(9), record.ID)
	require.Equal(t, "existing", record.Model)
	require.Equal(t, "connectivity", record.Kind)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelGroupProbeRecordRepository(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewChannelGroupProbeRecordRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)INSERT INTO channel_group_probe_records.*'running'.*RETURNING id, created_at`).WithArgs(int64(1), int64(2), "connectivity", "model").WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(7, now))
	record, err := repo.Create(ctx, 1, 2, "connectivity", "model")
	require.NoError(t, err)
	require.Equal(t, int64(7), record.ID)
	require.Equal(t, "running", record.Status)

	mock.ExpectExec(`(?s)SET billing_api_key_id = \$4, billing_request_id = \$5.*billing_request_id IS NULL`).WithArgs(int64(7), int64(1), int64(2), int64(9), "client:abc").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.SetBillingIdentity(ctx, 7, 1, 2, 9, "client:abc"))
	mock.ExpectExec(`(?s)SET billing_api_key_id = \$4, billing_request_id = \$5.*billing_request_id IS NULL`).WithArgs(int64(7), int64(1), int64(2), int64(9), "client:abc").WillReturnResult(sqlmock.NewResult(0, 0))
	require.ErrorIs(t, repo.SetBillingIdentity(ctx, 7, 1, 2, 9, "client:abc"), sql.ErrNoRows)

	duration := int64(123)
	svg := "<svg/>"
	mock.ExpectExec(`(?s)UPDATE channel_group_probe_records.*completed_at = NOW\(\).*WHERE id = \$1 AND group_id = \$2 AND user_id = \$3 AND status = 'running'`).WithArgs(int64(7), int64(1), int64(2), "succeeded", duration, svg, nil).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.Complete(ctx, 7, 1, 2, ChannelGroupProbeResult{Status: "succeeded", DurationMS: &duration, SVG: &svg}))
	mock.ExpectExec(`(?s)UPDATE channel_group_probe_records.*WHERE id = \$1 AND group_id = \$2 AND user_id = \$3 AND status = 'running'`).WithArgs(int64(7), int64(1), int64(3), "failed", nil, nil, nil).WillReturnResult(sqlmock.NewResult(0, 0))
	require.ErrorIs(t, repo.Complete(ctx, 7, 1, 3, ChannelGroupProbeResult{Status: "failed"}), sql.ErrNoRows)
	require.Error(t, repo.Complete(ctx, 7, 1, 2, ChannelGroupProbeResult{Status: "running"}))

	columns := []string{"id", "group_id", "user_id", "kind", "model", "status", "duration_ms", "svg", "error_message", "created_at", "completed_at", "actual_cost"}
	mock.ExpectExec(`(?s)UPDATE channel_group_probe_records.*status = 'failed'.*WHERE status = 'running' AND created_at < NOW\(\) - INTERVAL '5 minutes'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`(?s)LEFT JOIN LATERAL.*COUNT\(\*\) = 1.*u.request_id = p.billing_request_id AND u.api_key_id = p.billing_api_key_id.*u.user_id = p.user_id AND u.group_id = p.group_id.*usage_billing_dedup.*WHERE p.group_id = \$1 AND \(p.user_id = \$2 OR p.status IN \('succeeded', 'failed'\)\).*ORDER BY \(p.user_id = \$2 AND p.status = 'running'\) DESC, p.created_at DESC, p.id DESC LIMIT \$3`).WithArgs(int64(1), int64(2), 50).WillReturnRows(sqlmock.NewRows(columns).AddRow(8, 1, 2, "pelican", "m", "running", nil, nil, nil, now, nil, nil).AddRow(7, 1, 3, "connectivity", "m", "succeeded", duration, svg, nil, now, now, 0.0))
	records, err := repo.List(ctx, 1, 2, 0)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, "running", records[0].Status)
	require.Nil(t, records[0].CompletedAt)
	require.Equal(t, duration, *records[1].DurationMS)
	require.Nil(t, records[0].ActualCost)
	require.NotNil(t, records[1].ActualCost)
	require.Zero(t, *records[1].ActualCost)
	require.Equal(t, svg, *records[1].SVG)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelGroupProbeListCostAppearsAfterUsageLog(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewChannelGroupProbeRecordRepository(db)
	now := time.Now().UTC()
	columns := []string{"id", "group_id", "user_id", "kind", "model", "status", "duration_ms", "svg", "error_message", "created_at", "completed_at", "actual_cost"}
	for _, cost := range []any{nil, float64(0), 0.125} {
		mock.ExpectExec(`(?s)UPDATE channel_group_probe_records.*status = 'failed'`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`(?s)LEFT JOIN LATERAL.*u.request_id = p.billing_request_id.*u.api_key_id = p.billing_api_key_id`).WithArgs(int64(1), int64(2), 50).
			WillReturnRows(sqlmock.NewRows(columns).AddRow(7, 1, 2, "pelican", "m", "failed", nil, nil, "failed", now, now, cost))
		rows, err := repo.List(context.Background(), 1, 2, 50)
		require.NoError(t, err)
		if cost == nil {
			require.Nil(t, rows[0].ActualCost)
		} else {
			require.NotNil(t, rows[0].ActualCost)
			require.Equal(t, cost, *rows[0].ActualCost)
		}
	}
	require.NoError(t, mock.ExpectationsWereMet())
}
