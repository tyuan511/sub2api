# hk1 用户灰度名单发布

时间：2026-09-05 14:35–14:38（Asia/Shanghai）。用户明确授权部署 hk1 测试环境；us02 正式环境未操作。

## 制品与回滚

- 本地执行 `docker buildx build --platform linux/amd64 --load`，pnpm 9 frozen-lockfile、TypeScript/Vite 与 Go embed 发布构建通过；服务器只接收、校验并加载镜像，不构建。
- 镜像：`sub2api:v20260905.1431`，ID `sha256:b80e048f9e78544f106893b3ab06fb7283cbfb7b295c131e6e7ef3bdf6287ec9`，架构 `linux/amd64`，48,097,231 bytes。
- 基于 HEAD `2369bd2d5656787edf825a1c885b7190a1aed7d6` 与未提交工作树；版本标记 `2369bd2d5656-dirty`，不冒充已提交版本。
- 镜像归档 SHA-256：`e098c1c25262707c22d3d668367b4364120405f61a25d1d71f59bcd6cd6169d3`，传输后校验一致；加载后的镜像 ID 和架构匹配。
- 备份目录 `/root/sub2api-test/backups/routing-rollout-20260905.9HBwiY`（权限 700）保存原 Compose/环境文件、旧镜像引用、Key/绑定/名单/迁移/依赖快照、数据库 dump、发布与核验脚本以及 `verification.json`。
- 约 100 MiB 的数据库 dump 经 `pg_restore --list` 校验通过，SHA-256：`b33e38c1e91341f3fd5d56da35cffff54395f25b3606f8b5c2cdc45012532c39`。
- 旧镜像 `sub2api:v20260905.1153` 保留。展开 Compose 逐字段比较，仅改变应用 image；使用 `up -d --no-deps --no-build --pull never sub2api` 更新，启动健康失败会恢复旧 Compose/镜像。本次未触发回滚。
- 旧镜像不识别用户名单，不能依赖名单限制旧版本访问；如未来需回滚且仍需关闭新功能，应同时按运行手册关闭多分组总开关。禁止回滚时覆盖业务数据库或删除新增列。

## 已核验

- hk1 当前仅有一个本应用实例，已更新为新镜像；启动时间 `2026-09-05T06:35:48.810428709Z`，`healthy`，`RestartCount=0`，`/health` 返回 `{"status":"ok"}`，容器版本命令与新镜像一致。
- 管理端 GET `/api/v1/admin/settings/api-key-routing-rollout` 使用既有管理员凭据只读验证，返回空名单（0 人）；名单持久化值与发布前一致，没有自动加入管理员或任何用户，凭据没有输出或写入报告。
- 管理端名单和用户能力接口在无认证请求下均返回 401。只读解释抽查全部 8 条活动 Key，均为 `routing_enabled=false`、`schedule_mode=sequential`、最多一个候选；不调用上游。
- 首次解释检查遗漏必填模型/端点参数而得到 400，补齐 `model_family=gpt&endpoint_kind=responses` 后通过；该无效检查不作为路由故障或通过证据。
- 全部 25 条 Key（含软删除）的分组镜像、模式、比例、门槛、路由/运行时版本与全部 42 条分组绑定逐条保持。发布前后活动用户/Key/分组/账号数均为 2/8/4/4。
- 没有新增迁移、名单写入或环境变量变更；多分组总开关仍为 `true`，但默认名单为空，因此本次没有开放任何灰度用户。
- PostgreSQL/Redis 健康且重启次数 0，启动时间仍为 2026-08-27；启动检查窗口内没有 panic/fatal/迁移失败匹配。
- 根页面与新前端资源均为 HTTP 200，线上入口 `/assets/index-CDpgxH9N.js`。`/assets/KeysView-UXfJu7Uq.js` 包含用户能力开关，SHA-256：`37428eb8432b54778486d5d7910ccde2fb9d7dcb669fc14ce678d81b0b35b02a`；`/assets/SettingsView-C1_xYEyR.js` 包含名单卡片和 user_ids 保存字段，SHA-256：`3e9288f63393ad4a3843cf63e1c9fd317b2b51ccedbaf8c27f36c21ba091964a`。

## 验证边界

本次是部署与只读冒烟，没有添加灰度用户、修改 Key、制造故障或调用付费上游。名单加入/移出的真实保存回显与请求端到端、多实例 TTL 收敛仍待专用测试用户与独立演练；本地已有自动化回归不替代上述环境验证。未扩大策略 shadow/canary 或学习优化范围。
