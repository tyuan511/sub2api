//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

type ProxyExpirySuite struct {
	suite.Suite
	ctx         context.Context
	tx          *dbent.Tx
	repo        *proxyRepository
	accountRepo *accountRepository
}

func (s *ProxyExpirySuite) SetupTest() {
	s.ctx = context.Background()
	s.tx = testEntTx(s.T())
	s.repo = newProxyRepositoryWithSQL(s.tx.Client(), s.tx)
	s.accountRepo = newAccountRepositoryWithSQL(s.tx.Client(), s.tx, nil)
}
func TestProxyExpirySuite(t *testing.T) { suite.Run(t, new(ProxyExpirySuite)) }

func (s *ProxyExpirySuite) mkProxy(name, mode string, expiresAt *time.Time, backupID *int64) int64 {
	p := &service.Proxy{Name: name, Protocol: "http", Host: "127.0.0.1", Port: 8080,
		Status: service.StatusActive, FallbackMode: mode, ExpiryWarnDays: 7,
		ExpiresAt: expiresAt, BackupProxyID: backupID}
	s.Require().NoError(s.repo.Create(s.ctx, p))
	return p.ID
}

func (s *ProxyExpirySuite) mkAccountWithProxy(proxyID int64) int64 {
	var id int64
	err := scanSingleRow(s.ctx, s.tx, `
		INSERT INTO accounts (name, platform, type, credentials, extra, status, proxy_id, created_at, updated_at)
		VALUES ($1,'claude','api','{}','{}','active',$2,NOW(),NOW()) RETURNING id`,
		[]any{"acc-" + time.Now().Format("150405.000000"), proxyID}, &id)
	s.Require().NoError(err)
	return id
}

func (s *ProxyExpirySuite) mkAccountWithoutProxy() int64 {
	var id int64
	err := scanSingleRow(s.ctx, s.tx, `
		INSERT INTO accounts (name, platform, type, credentials, extra, status, created_at, updated_at)
		VALUES ($1,'claude','api','{}','{}','active',NOW(),NOW()) RETURNING id`,
		[]any{"acc-no-proxy-" + time.Now().Format("150405.000000")}, &id)
	s.Require().NoError(err)
	return id
}

func (s *ProxyExpirySuite) addAccountProxy(accountID, proxyID int64, concurrency, position int) {
	_, err := s.tx.ExecContext(s.ctx, `
		INSERT INTO account_proxies (account_id, proxy_id, concurrency, position)
		VALUES ($1, $2, $3, $4)`, accountID, proxyID, concurrency, position)
	s.Require().NoError(err)
}

type proxyPoolRow struct {
	proxyID     int64
	concurrency int
	position    int
}

func (s *ProxyExpirySuite) accountProxyPool(accountID int64) []proxyPoolRow {
	rows, err := s.tx.QueryContext(s.ctx, `
		SELECT proxy_id, concurrency, position
		FROM account_proxies
		WHERE account_id=$1
		ORDER BY position, proxy_id`, accountID)
	s.Require().NoError(err)
	defer func() { _ = rows.Close() }()

	var result []proxyPoolRow
	for rows.Next() {
		var row proxyPoolRow
		s.Require().NoError(rows.Scan(&row.proxyID, &row.concurrency, &row.position))
		result = append(result, row)
	}
	s.Require().NoError(rows.Err())
	return result
}

func (s *ProxyExpirySuite) accountProxyID(id int64) *int64 {
	var pid *int64
	err := scanSingleRow(s.ctx, s.tx, `SELECT proxy_id FROM accounts WHERE id=$1`, []any{id}, &pid)
	s.Require().NoError(err)
	return pid
}

func (s *ProxyExpirySuite) TestSweep_DirectMode() {
	past := time.Now().Add(-time.Hour)
	pid := s.mkProxy("p-direct", service.FallbackModeDirect, &past, nil)
	aid := s.mkAccountWithProxy(pid)

	changed, err := s.repo.SweepExpiredProxies(s.ctx, time.Now())
	s.Require().NoError(err)
	s.Require().GreaterOrEqual(changed, int64(1))

	got, _ := s.repo.GetByID(s.ctx, pid)
	s.Require().Equal(service.StatusExpired, got.Status)
	s.Require().Nil(s.accountProxyID(aid))
	var origin *int64
	err = scanSingleRow(s.ctx, s.tx, `SELECT proxy_fallback_origin_id FROM accounts WHERE id=$1`, []any{aid}, &origin)
	s.Require().NoError(err)
	s.Require().NotNil(origin)
	s.Require().Equal(pid, *origin)
}

func (s *ProxyExpirySuite) TestSweep_EnqueuesChangedAccountIDsWithoutFullRebuild() {
	past := time.Now().Add(-time.Hour)
	firstProxyID := s.mkProxy("p-bulk-first", service.FallbackModeDirect, &past, nil)
	secondProxyID := s.mkProxy("p-bulk-second", service.FallbackModeDirect, &past, nil)
	firstAccountID := s.mkAccountWithProxy(firstProxyID)
	secondAccountID := s.mkAccountWithProxy(secondProxyID)

	changed, err := s.repo.SweepExpiredProxies(s.ctx, time.Now())
	s.Require().NoError(err)
	s.Require().EqualValues(2, changed)

	var payloadRaw []byte
	err = scanSingleRow(s.ctx, s.tx, `
		SELECT payload
		FROM scheduler_outbox
		WHERE event_type=$1
		ORDER BY id DESC
		LIMIT 1`, []any{service.SchedulerOutboxEventAccountBulkChanged}, &payloadRaw)
	s.Require().NoError(err)

	var payload struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	s.Require().NoError(json.Unmarshal(payloadRaw, &payload))
	s.Require().Equal([]int64{firstAccountID, secondAccountID}, payload.AccountIDs)

	var fullRebuildCount int
	err = scanSingleRow(s.ctx, s.tx, `
		SELECT COUNT(*)
		FROM scheduler_outbox
		WHERE event_type=$1`, []any{service.SchedulerOutboxEventFullRebuild}, &fullRebuildCount)
	s.Require().NoError(err)
	s.Require().Zero(fullRebuildCount)
}

func (s *ProxyExpirySuite) TestSweep_ProxyMode_Healthy() {
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-time.Hour)
	backup := s.mkProxy("p-backup", service.FallbackModeNone, &future, nil)
	pid := s.mkProxy("p-main", service.FallbackModeProxy, &past, &backup)
	aid := s.mkAccountWithProxy(pid)

	_, err := s.repo.SweepExpiredProxies(s.ctx, time.Now())
	s.Require().NoError(err)
	s.Require().Equal(backup, *s.accountProxyID(aid))
	var origin *int64
	err = scanSingleRow(s.ctx, s.tx, `SELECT proxy_fallback_origin_id FROM accounts WHERE id=$1`, []any{aid}, &origin)
	s.Require().NoError(err)
	s.Require().NotNil(origin)
	s.Require().Equal(pid, *origin)
}

func (s *ProxyExpirySuite) TestSweep_NoneMode_KeepsAccount() {
	past := time.Now().Add(-time.Hour)
	pid := s.mkProxy("p-none", service.FallbackModeNone, &past, nil)
	aid := s.mkAccountWithProxy(pid)

	_, err := s.repo.SweepExpiredProxies(s.ctx, time.Now())
	s.Require().NoError(err)
	got, _ := s.repo.GetByID(s.ctx, pid)
	s.Require().Equal(service.StatusExpired, got.Status)
	s.Require().Equal(pid, *s.accountProxyID(aid))
	var origin *int64
	err = scanSingleRow(s.ctx, s.tx, `SELECT proxy_fallback_origin_id FROM accounts WHERE id=$1`, []any{aid}, &origin)
	s.Require().NoError(err)
	s.Require().Nil(origin)
}

func (s *ProxyExpirySuite) TestSweep_ProxyMode_ReplacesPoolOnlyBindingAndPreservesOtherBindings() {
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-time.Hour)
	backup := s.mkProxy("p-pool-backup", service.FallbackModeNone, &future, nil)
	other := s.mkProxy("p-pool-other", service.FallbackModeNone, &future, nil)
	main := s.mkProxy("p-pool-main", service.FallbackModeProxy, &past, &backup)
	aid := s.mkAccountWithoutProxy()
	s.addAccountProxy(aid, main, 2, 7)
	s.addAccountProxy(aid, other, 11, 3)

	_, err := s.repo.SweepExpiredProxies(s.ctx, time.Now())
	s.Require().NoError(err)
	s.Require().Nil(s.accountProxyID(aid), "pool-only expiry must not invent a legacy proxy_id")
	s.Require().Equal([]proxyPoolRow{
		{proxyID: other, concurrency: 11, position: 3},
		{proxyID: backup, concurrency: 2, position: 7},
	}, s.accountProxyPool(aid))
}

func (s *ProxyExpirySuite) TestSweep_ProxyMode_ExistingFallbackBindingWinsAndLegacyPoolSurvives() {
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-time.Hour)
	backup := s.mkProxy("p-existing-backup", service.FallbackModeNone, &future, nil)
	other := s.mkProxy("p-existing-other", service.FallbackModeNone, &future, nil)
	main := s.mkProxy("p-existing-main", service.FallbackModeProxy, &past, &backup)
	aid := s.mkAccountWithProxy(main)
	s.addAccountProxy(aid, main, 2, 5)
	s.addAccountProxy(aid, backup, 13, 1)
	s.addAccountProxy(aid, other, 17, 9)

	_, err := s.repo.SweepExpiredProxies(s.ctx, time.Now())
	s.Require().NoError(err)
	s.Require().Equal(backup, *s.accountProxyID(aid))
	s.Require().Equal([]proxyPoolRow{
		{proxyID: backup, concurrency: 13, position: 1},
		{proxyID: main, concurrency: 2, position: 5},
		{proxyID: other, concurrency: 17, position: 9},
	}, s.accountProxyPool(aid))
}

func (s *ProxyExpirySuite) TestRevert_ExistingFallbackBindingPreservesTargetAndRestoresOriginBinding() {
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-time.Hour)
	backup := s.mkProxy("p-revert-existing-backup", service.FallbackModeNone, &future, nil)
	other := s.mkProxy("p-revert-existing-other", service.FallbackModeNone, &future, nil)
	main := s.mkProxy("p-revert-existing-main", service.FallbackModeProxy, &past, &backup)
	aid := s.mkAccountWithProxy(main)
	s.addAccountProxy(aid, main, 2, 5)
	s.addAccountProxy(aid, backup, 13, 1)
	s.addAccountProxy(aid, other, 17, 9)

	_, err := s.repo.SweepExpiredProxies(s.ctx, time.Now())
	s.Require().NoError(err)
	s.Require().NoError(s.accountRepo.RevertProxyFallback(s.ctx, aid))

	s.Require().Equal(main, *s.accountProxyID(aid))
	s.Require().Equal([]proxyPoolRow{
		{proxyID: backup, concurrency: 13, position: 1},
		{proxyID: main, concurrency: 2, position: 5},
		{proxyID: other, concurrency: 17, position: 9},
	}, s.accountProxyPool(aid))
}

func (s *ProxyExpirySuite) TestSweep_DirectMode_PreservesLegacyBindingForRevert() {
	past := time.Now().Add(-time.Hour)
	main := s.mkProxy("p-direct-revert-main", service.FallbackModeDirect, &past, nil)
	aid := s.mkAccountWithProxy(main)
	s.addAccountProxy(aid, main, 4, 6)

	_, err := s.repo.SweepExpiredProxies(s.ctx, time.Now())
	s.Require().NoError(err)
	s.Require().Nil(s.accountProxyID(aid))
	s.Require().NoError(s.accountRepo.RevertProxyFallback(s.ctx, aid))

	s.Require().Equal(main, *s.accountProxyID(aid))
	s.Require().Equal([]proxyPoolRow{{proxyID: main, concurrency: 4, position: 6}}, s.accountProxyPool(aid))
}

func (s *ProxyExpirySuite) TestSweep_DirectMode_RemovesOnlyExpiredPoolBinding() {
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-time.Hour)
	main := s.mkProxy("p-pool-direct-main", service.FallbackModeDirect, &past, nil)
	other := s.mkProxy("p-pool-direct-other", service.FallbackModeNone, &future, nil)
	aid := s.mkAccountWithoutProxy()
	s.addAccountProxy(aid, main, 2, 2)
	s.addAccountProxy(aid, other, 5, 4)

	_, err := s.repo.SweepExpiredProxies(s.ctx, time.Now())
	s.Require().NoError(err)
	s.Require().Equal([]proxyPoolRow{
		{proxyID: other, concurrency: 5, position: 4},
	}, s.accountProxyPool(aid))
}
