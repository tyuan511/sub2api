//go:build integration

package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpstreamManagementPostgres(t *testing.T) {
	ctx := context.Background()
	repo := NewUpstreamRepository(integrationDB)
	u, err := repo.Save(ctx, &service.Upstream{Name: "integration upstream", Type: "sub2api", BaseURL: "https://example.com/v1",
		BalanceAPIKeyEncrypted: "fixture-encrypted-key", BalanceThreshold: 10, BalanceMultiplier: 1, QuotaPerUnit: 500000, CheckIntervalSeconds: 300, Enabled: true})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM support_notification_outbox WHERE event_type=$1 AND payload::jsonb->>'upstream_id'=$2`, service.UpstreamLowBalanceEvent, strconv.FormatInt(u.ID, 10))
		_ = repo.Delete(ctx, u.ID)
	})
	require.True(t, u.HasBalanceAPIKey)
	require.Nil(t, u.Balance)
	lease, err := repo.Claim(ctx, u.ID, false)
	require.NoError(t, err)
	require.NotNil(t, lease)
	duplicate, err := repo.Claim(ctx, u.ID, false)
	require.NoError(t, err)
	require.Nil(t, duplicate)
	duplicate, err = repo.Claim(ctx, u.ID, true)
	require.NoError(t, err)
	require.Nil(t, duplicate)

	adminID := time.Now().UnixNano()
	_, err = integrationDB.Exec(`INSERT INTO admin_telegram_bindings(admin_id,telegram_user_id,chat_id) VALUES ($1,$1,$1)`, adminID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = integrationDB.Exec(`DELETE FROM admin_telegram_bindings WHERE admin_id=$1`, adminID) })
	countAlerts := func() int {
		t.Helper()
		var count int
		require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM support_notification_outbox WHERE target_admin_id=$1 AND event_type=$2 AND payload::jsonb->>'upstream_id'=$3`, adminID, service.UpstreamLowBalanceEvent, strconv.FormatInt(u.ID, 10)).Scan(&count))
		return count
	}

	require.NoError(t, repo.RecordBalance(ctx, lease, floatPointer(8), "", true))
	require.Equal(t, 1, countAlerts())
	require.NoError(t, repo.RecordBalance(ctx, lease, floatPointer(7), "", true))
	require.Equal(t, 1, countAlerts())
	require.NoError(t, repo.RecordBalance(ctx, lease, floatPointer(10), "", true))
	require.Equal(t, 1, countAlerts())
	require.NoError(t, repo.RecordBalance(ctx, lease, floatPointer(9), "", true))
	require.Equal(t, 2, countAlerts())
	require.NoError(t, repo.RecordBalance(ctx, lease, nil, "Upstream returned HTTP 401", true))
	current, err := repo.Get(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, 9.0, *current.Balance)
	require.NotNil(t, current.BalanceCheckedAt)
	require.Contains(t, current.LastError, "401")

	// Amount conversion invalidates the old balance, and a query started before the edit cannot overwrite it.
	current.BalanceMultiplier = 2
	edited, err := repo.Save(ctx, current)
	require.NoError(t, err)
	require.Nil(t, edited.Balance)
	require.Nil(t, edited.BalanceCheckedAt)
	require.NoError(t, repo.RecordBalance(ctx, lease, floatPointer(1), "", true))
	edited, err = repo.Get(ctx, u.ID)
	require.NoError(t, err)
	require.Nil(t, edited.Balance)
	require.Equal(t, 2, countAlerts())
	edited.Name = "renamed"
	edited, err = repo.Save(ctx, edited)
	require.NoError(t, err)
	require.Equal(t, "renamed", edited.Name)
	require.NoError(t, repo.Delete(ctx, u.ID))
	_, err = repo.Get(ctx, u.ID)
	require.ErrorIs(t, err, service.ErrUpstreamNotFound)
}
