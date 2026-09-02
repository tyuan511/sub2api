package repository

import (
	"context"
	"errors"
	"sync"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccountproxy "github.com/Wei-Shaw/sub2api/ent/accountproxy"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// loadAccountProxies 批量加载账号的多代理池绑定（含代理实体）。
// 返回的每个切片按 sort_order、proxy_id 排序。
func (r *accountRepository) loadAccountProxies(ctx context.Context, accountIDs []int64) (map[int64][]service.AccountProxy, error) {
	out := make(map[int64][]service.AccountProxy)

	accountIDs = uniquePositiveInt64s(accountIDs)
	if len(accountIDs) == 0 {
		return out, nil
	}

	allEntries := make([]*dbent.AccountProxy, 0, len(accountIDs))
	proxyIDs := make([]int64, 0, len(accountIDs))
	for start := 0; start < len(accountIDs); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(accountIDs) {
			end = len(accountIDs)
		}
		entries, err := r.client.AccountProxy.Query().
			Where(dbaccountproxy.AccountIDIn(accountIDs[start:end]...)).
			Order(
				dbaccountproxy.ByAccountID(),
				dbaccountproxy.BySortOrder(),
				dbaccountproxy.ByProxyID(),
			).
			All(ctx)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			proxyIDs = append(proxyIDs, e.ProxyID)
		}
		allEntries = append(allEntries, entries...)
	}
	if len(allEntries) == 0 {
		return out, nil
	}

	proxyMap, err := r.loadProxies(ctx, proxyIDs)
	if err != nil {
		return nil, err
	}

	for _, e := range allEntries {
		out[e.AccountID] = append(out[e.AccountID], service.AccountProxy{
			AccountID:   e.AccountID,
			ProxyID:     e.ProxyID,
			Concurrency: e.Concurrency,
			SortOrder:   e.SortOrder,
			Proxy:       proxyMap[e.ProxyID],
		})
	}
	return out, nil
}

// ReplaceAccountProxies 用给定绑定整体替换账号的多代理池。
// 传入空切片表示清空绑定，账号退回只使用 proxy_id 的旧行为。
func (r *accountRepository) ReplaceAccountProxies(ctx context.Context, accountID int64, bindings []service.AccountProxy) error {
	if accountID <= 0 {
		return service.ErrAccountNotFound
	}
	normalized := service.NormalizeAccountProxies(accountID, bindings)

	// 调用方通过 RunInTx 携带事务时复用它（账号 + 代理池原子落库）；
	// 否则自行开启事务保证删除旧绑定与创建新绑定的原子性（与 BindGroups 同一模式）。
	contextTx := dbent.TxFromContext(ctx)
	var (
		tx       *dbent.Tx
		txClient *dbent.Client
	)
	if contextTx != nil {
		txClient = contextTx.Client()
	} else {
		var err error
		tx, err = r.client.Tx(ctx)
		if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
			return err
		}
		if err == nil {
			defer func() { _ = tx.Rollback() }()
			txClient = tx.Client()
		} else {
			// 已处于外部事务中（ErrTxStarted），复用当前 client
			tx = nil
			txClient = r.client
		}
	}

	if _, err := txClient.AccountProxy.Delete().
		Where(dbaccountproxy.AccountIDEQ(accountID)).
		Exec(ctx); err != nil {
		return translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}

	if len(normalized) > 0 {
		builders := make([]*dbent.AccountProxyCreate, 0, len(normalized))
		for _, b := range normalized {
			builders = append(builders, txClient.AccountProxy.Create().
				SetAccountID(accountID).
				SetProxyID(b.ProxyID).
				SetConcurrency(b.Concurrency).
				SetSortOrder(b.SortOrder))
		}
		if _, err := txClient.AccountProxy.CreateBulk(builders...).Save(ctx); err != nil {
			return translatePersistenceError(err, service.ErrAccountNotFound, nil)
		}
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	// 外部事务尚未提交时快照读不到新数据，推迟到 RunInTx 提交后统一同步。
	if contextTx != nil {
		registerAccountSnapshotSyncAfterCommit(ctx, accountID)
		return nil
	}
	r.syncSchedulerAccountSnapshot(ctx, accountID)
	return nil
}

// accountTxAfterCommit 记录事务提交后需要刷新调度快照的账号。
type accountTxAfterCommit struct {
	mu      sync.Mutex
	syncIDs map[int64]struct{}
}

type accountTxAfterCommitKeyType struct{}

var accountTxAfterCommitKey = accountTxAfterCommitKeyType{}

func registerAccountSnapshotSyncAfterCommit(ctx context.Context, accountID int64) {
	hooks, _ := ctx.Value(accountTxAfterCommitKey).(*accountTxAfterCommit)
	if hooks == nil || accountID <= 0 {
		return
	}
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	if hooks.syncIDs == nil {
		hooks.syncIDs = make(map[int64]struct{})
	}
	hooks.syncIDs[accountID] = struct{}{}
}

// RunInTx 在一个数据库事务里执行 fn：fn 收到的 ctx 携带事务，
// 本仓库的 Create / Update / UpdateExtra / ReplaceAccountProxies 会自动复用它，
// 从而让「账号行 + 多代理池绑定」要么全部落库要么全部回滚。
// 提交成功后统一刷新事务内触碰过的账号调度快照。ctx 已在事务中时直接执行 fn。
func (r *accountRepository) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if fn == nil {
		return nil
	}
	if dbent.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		if errors.Is(err, dbent.ErrTxStarted) {
			return fn(ctx)
		}
		return err
	}
	hooks := &accountTxAfterCommit{}
	txCtx := context.WithValue(dbent.NewTxContext(ctx, tx), accountTxAfterCommitKey, hooks)
	if err := fn(txCtx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	hooks.mu.Lock()
	ids := make([]int64, 0, len(hooks.syncIDs))
	for id := range hooks.syncIDs {
		ids = append(ids, id)
	}
	hooks.mu.Unlock()
	for _, id := range ids {
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

// GetAccountProxies 读取单个账号的多代理池绑定。
func (r *accountRepository) GetAccountProxies(ctx context.Context, accountID int64) ([]service.AccountProxy, error) {
	byAccount, err := r.loadAccountProxies(ctx, []int64{accountID})
	if err != nil {
		return nil, err
	}
	return byAccount[accountID], nil
}
