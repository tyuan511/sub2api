//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// proxyPoolTxRepo 模拟事务语义：fn 出错时把事务内创建的账号回滚掉。
type proxyPoolTxRepo struct {
	*upstreamBillingProbeAccountRepo
	replaceErr   error
	replaceCalls int
	inTx         bool
	rolledBack   bool
}

func (r *proxyPoolTxRepo) ListShadowsByParent(context.Context, int64) ([]*Account, error) {
	return nil, nil
}

func (r *proxyPoolTxRepo) ReplaceAccountProxies(_ context.Context, _ int64, _ []AccountProxy) error {
	r.replaceCalls++
	return r.replaceErr
}

func (r *proxyPoolTxRepo) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	r.inTx = true
	before := make(map[int64]struct{}, len(r.accounts))
	r.mu.Lock()
	for id := range r.accounts {
		before[id] = struct{}{}
	}
	r.mu.Unlock()

	err := fn(ctx)
	r.inTx = false
	if err != nil {
		// 回滚：丢弃事务内新建的账号
		r.mu.Lock()
		for id := range r.accounts {
			if _, existed := before[id]; !existed {
				delete(r.accounts, id)
			}
		}
		r.mu.Unlock()
		r.rolledBack = true
	}
	return err
}

func TestCreateAccountWithProxyPoolRollsBackWhenPoolPersistFails(t *testing.T) {
	repo := &proxyPoolTxRepo{
		upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{}},
		replaceErr:                      errors.New("proxy 42 does not exist"),
	}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "pooled",
		Platform:             PlatformOpenAI,
		Type:                 AccountTypeAPIKey,
		Credentials:          map[string]any{"api_key": "sk-test"},
		SkipDefaultGroupBind: true,
		Proxies: []AccountProxy{
			{ProxyID: 41, Concurrency: 2},
			{ProxyID: 42, Concurrency: 3},
		},
	})

	require.Error(t, err)
	require.Equal(t, 1, repo.replaceCalls)
	require.True(t, repo.rolledBack, "pool persistence must run inside RunInTx so the account row is rolled back")
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Empty(t, repo.accounts, "no half-created account may survive a failed pool write")
}

func TestCreateAccountWithProxyPoolCommitsAccountAndPool(t *testing.T) {
	repo := &proxyPoolTxRepo{
		upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{}},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	created, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "pooled",
		Platform:             PlatformOpenAI,
		Type:                 AccountTypeAPIKey,
		Credentials:          map[string]any{"api_key": "sk-test"},
		Concurrency:          99, // 被代理池之和覆盖
		SkipDefaultGroupBind: true,
		Proxies: []AccountProxy{
			{ProxyID: 41, Concurrency: 2},
			{ProxyID: 42, Concurrency: 3},
		},
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.replaceCalls)
	require.False(t, repo.rolledBack)
	require.NotNil(t, created.ProxyID)
	require.Equal(t, int64(41), *created.ProxyID, "primary proxy is the first pool entry")
	require.Equal(t, 5, created.Concurrency, "account concurrency is the sum of per-proxy concurrency")
	require.Len(t, created.Proxies, 2)
}

func TestCreateAccountWithoutProxiesSkipsTransactionWrapper(t *testing.T) {
	repo := &proxyPoolTxRepo{
		upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{}},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "legacy",
		Platform:             PlatformOpenAI,
		Type:                 AccountTypeAPIKey,
		Credentials:          map[string]any{"api_key": "sk-test"},
		Concurrency:          7,
		SkipDefaultGroupBind: true,
	})

	require.NoError(t, err)
	require.Equal(t, 0, repo.replaceCalls, "legacy single-proxy/no-proxy accounts must not touch the pool table")
	require.False(t, repo.rolledBack)
}

// proxyPoolShadowRepo 在 proxyPoolTxRepo 之上模拟一个 spark 影子，记录对它的更新。
type proxyPoolShadowRepo struct {
	*proxyPoolTxRepo
	shadow *Account
}

func (r *proxyPoolShadowRepo) ListShadowsByParent(_ context.Context, parentID int64) ([]*Account, error) {
	if r.shadow != nil && r.shadow.ParentAccountID != nil && *r.shadow.ParentAccountID == parentID {
		return []*Account{r.shadow}, nil
	}
	return nil, nil
}

func (r *proxyPoolShadowRepo) Update(ctx context.Context, account *Account) error {
	if r.shadow != nil && account.ID == r.shadow.ID {
		r.shadow.Concurrency = account.Concurrency
		return nil
	}
	return r.proxyPoolTxRepo.Update(ctx, account)
}

func TestProxyPoolChangeKeepsShadowConcurrencyEqualToPoolTotal(t *testing.T) {
	parentID := int64(301)
	parent := &Account{
		ID:          parentID,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 6,
		Proxies: []AccountProxy{
			{AccountID: parentID, ProxyID: 41, Concurrency: 3},
			{AccountID: parentID, ProxyID: 42, Concurrency: 3},
		},
	}
	shadow := &Account{ID: 302, ParentAccountID: &parentID, Concurrency: 6, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive}
	repo := &proxyPoolShadowRepo{
		proxyPoolTxRepo: &proxyPoolTxRepo{
			upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{parentID: parent}},
		},
		shadow: shadow,
	}
	svc := &adminServiceImpl{accountRepo: repo}

	updated, err := svc.UpdateAccount(context.Background(), parentID, &UpdateAccountInput{
		Proxies: &[]AccountProxy{
			{ProxyID: 41, Concurrency: 5},
			{ProxyID: 42, Concurrency: 5},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 10, updated.Concurrency)
	require.Equal(t, 10, shadow.Concurrency, "shadow shares the pool, so its capacity must follow the pool total")
}

func TestUpdateAccountReassertsPoolConcurrencyWhenOnlyConcurrencyIsSent(t *testing.T) {
	accountID := int64(311)
	repo := &proxyPoolTxRepo{
		upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
			accountID: {
				ID:          accountID,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Concurrency: 8,
				Proxies: []AccountProxy{
					{AccountID: accountID, ProxyID: 41, Concurrency: 4},
					{AccountID: accountID, ProxyID: 42, Concurrency: 4},
				},
			},
		}},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	forced := 99
	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{Concurrency: &forced})
	require.NoError(t, err)
	require.Equal(t, 8, updated.Concurrency, "a pooled account's concurrency is always the pool total")
	require.Equal(t, 0, repo.replaceCalls, "no pool change must not rewrite the pool table")
}
