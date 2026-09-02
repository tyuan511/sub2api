-- 账号多代理绑定表。
--
-- 一个账号可绑定多个代理，每个代理各自带并发上限；账号总并发（accounts.concurrency）
-- 由服务层维护为各代理并发之和。
--
-- 兼容性：该表对某账号没有任何行时，账号完全退回旧行为——只使用
-- accounts.proxy_id 与 accounts.concurrency，调度路径不受影响。
CREATE TABLE IF NOT EXISTS account_proxies (
    account_id      BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    proxy_id        BIGINT NOT NULL REFERENCES proxies(id) ON DELETE CASCADE,
    concurrency     INT NOT NULL DEFAULT 3,               -- 该代理单独的并发上限
    sort_order      INT NOT NULL DEFAULT 0,               -- 展示与轮询顺序
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, proxy_id)
);

CREATE INDEX IF NOT EXISTS idx_account_proxies_proxy_id ON account_proxies(proxy_id);
CREATE INDEX IF NOT EXISTS idx_account_proxies_account_sort ON account_proxies(account_id, sort_order);
