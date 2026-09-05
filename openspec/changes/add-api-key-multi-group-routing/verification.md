# 验证证据

更新时间：2026-09-05

本文件区分“本地已验证”和“需要 hk1/生产流量才能验证”。未完成环境验证前，不得把该变更描述为已经完整上线。

## 1. 当前结论

- OpenSpec 严格校验通过。
- 后端默认全量测试、unit 标签全量测试、路由相关 race 测试和生产构建通过。
- 前端 lint、TypeScript、Vitest 和生产构建通过。
- 功能关闭的旧单分组路径、开启后的单分组路径以及最多 8 组的本地计划构建均已有可重复基准。
- 路由状态热路径把组级粘性与最多 8 个 breaker 快照合并为一次 Redis Pipeline；CLOSED 候选在同一请求内不再逐组访问 Redis。
- 管理端运行时指标以固定维度报告鉴权查询、计划构造、Redis 状态读取、智能排序和状态更新的 P50/P95/P99/Max；未知标签会被丢弃，不会产生基数膨胀。
- 评分 Builder、路由事实消费者、离线回放和历史清理使用独立惰性 PostgreSQL/Ent 连接池，默认最多 2 个连接；评分聚合单条查询默认 15 秒超时并报告查询次数、失败、聚合输入桶行数和耗时。
- 持续优化增强已实现为默认关闭的本地制品运行时：分层 cohort/Key 残差使用置信收缩和全局幅度/TTL/内存上限；模型只处理规则先行过滤后的最多 8 个候选，并在 schema、lineage、checksum、数据质量、drift、校准、特征、范围或耗时异常时原子回退共享规则 baseline。
- 已授权并部署 hk1，最终镜像 `v20260905.0811`，完成受控压测、粘性/熔断/恢复、旧镜像回滚对照和 PostgreSQL 范围查询/连接快照核验；完整证据见 [hk1 部署报告](evidence/hk1-20260905/README.md)。新增路由总 P95、真实 Redis 往返、部分失败统计和 shadow/canary 等发布门禁仍未完成，us02 未操作。

## 2. 可重复命令与结果

### 2026-09-05 用户滑块补充（本地验证与 hk1 发布）

- 最终 `go test ./...` 全量通过（50 个含测试的包，0 failures）。缓存升级到 v26 对应的两个旧版本断言已同步更新；新增逻辑未通过时的中间测试结果不作为最终证据。
- 新 UI 及 Key 表单相关 5 个 Vitest 文件共 51 项通过；ESLint 定向校验、`vue-tsc -b` 和 Vite 生产构建通过。未变更 pnpm lockfile。
- 新增覆盖：连续比例端点与旧 preset 精确回显、零值保存、85/95% 门槛回显、所有 50..95 档位/非法输入、真实权重改变排序、auth v26 往返、CAS 与独立运行时版本、Redis Key 隔离/窗口过期/迟到策略版本、受控恢复、无探测许可不得绕过门槛、决策时配置持久化。
- 路由相关 service/repository/handler 的 `go test -race` 通过；本地 PostgreSQL 18 的隔离 schema 事务验证迁移 248、旧版本初始化、所有合法档位及 CHECK 拒绝边界、配置版本递增而状态版本保持，最后 ROLLBACK，不触及业务表。
- 以实际 Vue 滑块组件进行 IAB 检查：1100×1000 桌面、390×844 深色；窄屏文档宽度 390px，无横向溢出。键盘 End 到 95，ArrowLeft 降至 90；价格滑块 Home 到价格 100%/速度 0%；重置分别恢复 5000 bps 和 50%。临时浏览器和服务已关闭，视口已恢复。
- 上述验证完成时尚未部署。用户随后明确授权部署 hk1，于 10:20（Asia/Shanghai）更新至 `v20260905.1016`；us02 未操作。发布证据见 [滑块扩展 hk1 发布记录](evidence/hk1-20260905/controls-release.md)。
- hk1 的迁移、资源、只读配置/路由解释和健康冒烟已通过；自定义值真实保存、门槛变化后的故障转移/恢复仍需独立端到端演练，未因容器健康而标记完成。

### 2026-09-05 新建默认门槛改为 80%（本地验证与 hk1 发布）

- 新建 Key、UI 重置、服务创建（含旧客户端/未限定组入口）、Repository 及 Ent 默认值一致为 80%；50..95 的所有显式合法值继续保留，编辑省略门槛也保留原值，旧对象缺字段仍兼容为 50%。
- 新增迁移 249 只执行 `ALTER COLUMN ... SET DEFAULT 80`；已应用的 248 保持不变，历史 routing attempt 默认值不改，不重写旧 Key 或任何配置/状态版本。
- service/repository/handler/migrations 的定向 API Key 路由测试通过，包含新建默认/所有合法显式值/已有 Key 更新保持/Repository 鉴权读取/Ent 直接创建/迁移不回填契约。前端 28 项测试、TypeScript 与定向 ESLint 通过，依赖锁文件未修改。
- 本地 PostgreSQL 18 的连接级 TEMP `api_keys` 表完成真实 249 脚本检查：旧 50/95 保持不变、新建省略为 80、显式 50 仍为 50，配置版本 7/状态版本 3 均未改变；最后 ROLLBACK，不触及业务表。
- 上述本地验证后，用户明确授权部署 hk1，10:58 更新至 `v20260905.1054`。迁移 249 执行、数据库默认 80、前端新建/重置默认 80 和服务健康检查通过；发布前 25 条 Key（含软删除）的门槛/配置版本/状态版本逐条一致，活动 Key 仍为 8 条。完整发布证据见 [默认 80% hk1 发布记录](evidence/hk1-20260905/controls-release.md)。us02 未操作。

### 2026-09-05 单分组固定路由（本地验证与 hk1 发布）

- 新建/编辑时，0 或 1 个所选分组隐藏调度方式、成功率门槛、智能比例及相关解释；增加到 2 个时显示，减少到 1 个时再次隐藏。保存单分组使用 `sequential`，不提交隐藏比例/门槛；编辑省略门槛不覆盖旧值。列表只对多个启用分组显示智能标签，已绑定组不会误显示“未分组”。
- 请求计划按配置中启用的分组数量判定是否启用组间控制，不按过滤后的候选数判定。单分组以 Key ID/配置版本隔离的请求上下文标记跳过组级健康准入、状态更新与粘性读写，包括旧 `smart`/95% 门槛/OPEN 状态；旧 `group_id` 路径兼容。账号级重试、组启用状态、权限/模型/端点/订阅/计费检查保持原路径，失败事实继续采集。
- 多分组配置经请求过滤仅剩一个候选时，仍执行原门槛及 Redis 不可用时的保护。管理端解释返回 `routing_enabled=false` 与有效顺序模式，不查询单分组旧熔断/粘性状态或计算智能排名。
- `go test ./...` 全量通过；service/handler 的单组旁路、多组剩一候选、请求硬检查、失败事实、管理端解释及运行时状态相关定向 `go test -race` 通过。缓存 spy 验证单组控制零 Redis 调用，旁路不会泄漏到其他 Key/配置版本。
- `KeysView`、`RoutingPreferenceSlider`、`ApiKeyGroupRouteSelector` 的 3 个 Vitest 文件共 39 项通过；`vue-tsc -b`、定向 ESLint、Vite 生产构建与 `git diff --check` 通过。生产构建有既有大 chunk 提示，无构建错误；依赖锁文件未变更。
- OpenSpec 严格校验通过。无需数据库迁移，不批量清空旧状态或回填 Key；以上本地验证完成时尚未发布。
- 用户随后明确授权部署 hk1，11:58 更新到本地构建的 `v20260905.1153`；镜像身份、版本、健康和前端条件渲染/单组提交资源检查通过，25 条既有 Key 配置、迁移记录、环境变量与 PostgreSQL/Redis 启动状态均保持。当前没有绑定可用分组的测试 Key，未创建/修改 Key 或调用付费上游，不能把只读部署冒烟视为真实路由端到端验证。发布及回滚证据见 [单分组固定路由发布记录](evidence/hk1-20260905/controls-release.md)。us02 未操作。

### 2026-09-05 用户灰度名单（本地验证与 hk1 发布）

- 系统设置的功能开关页新增独立名单卡片；用户 ID 精确查询、用户名/邮箱搜索、选择、移除和显式清空保存均已实现。默认空名单且管理员不自动开放，名单存现有 `settings.api_key_routing_rollout`，无需迁移或批量改写 Key。
- 服务层与 HTTP 测试覆盖空名单、环境总开关、指定 ID、伪造 user_id 无效、401/403、名单不泄露、非法/缺失/不存在用户 ID、保存失败不覆盖、缓存失效及并发读写。移出后只投影原主组，不修改鉴权快照；缺失/停用主组失败关闭，普通编辑保留休眠配置，重新加入恢复。
- 最终 `go test ./...` 与 `go build ./cmd/server` 通过；API Key/灰度相关 unit 标签测试、service/admin/handler 定向 race 通过。缓存命中基准在 Apple M4 上为 32.04–32.40 ns/op、0 B/op、0 allocs/op；该微基准不替代 hk1 的真实并发验证。
- 前端全量 Vitest 260 个文件、1894 项通过，其中名单卡片 6 项、KeysView 24 项、SettingsView 37 项；`vue-tsc -b`、定向 ESLint、Vite 生产构建与 `git diff --check` 通过。构建仅有大 chunk 提示，未改动依赖锁文件。
- 使用临时本地 Vue 夹具及模拟 API 在 1280×720 浏览器检查名单卡片亮色/深色、加入/移除和保存交互，无横向溢出；不涉及真实认证、数据库写入或远端请求。临时夹具、服务和浏览器页已清理，该检查不等同线上端到端验证。
- OpenSpec 严格校验通过，设计与运行手册明确：同实例保存立即生效，其他实例按 5 秒 TTL 更新；刷新失败关闭资格，撤销可能回主组造成冷缓存。必须先升级全部实例再加名单，旧镜像忽略该限制。上述本地验证完成时尚未部署。
- 用户随后明确授权部署 hk1，14:35 更新到本地构建 `v20260905.1431`，唯一应用实例已升级。线上管理员接口确认空名单、未认证接口拒绝、8 条活动 Key 解释均为单组模式，新前端资源与健康检查通过；25 条既有 Key 配置、42 条绑定、名单值、迁移与依赖状态保持，未添加名单用户、未调用付费上游，us02 未操作。数据库与旧镜像保留，见 [用户灰度名单发布记录](evidence/hk1-20260905/rollout-release.md)。真实加入/移出及多实例演练仍待完成。

### 2026-09-05 智能倾向改为 5% 步长（本地与 hk1 发布）

- KeysView 将智能比例步长改为 500 bps，即 5%；后端仍接受已有整数 bps，旧 1250/7350/8750 等比例的回显与未调整保存不改写，原权重公式和成功率门槛保持。
- 滑块与 KeysView 的 54 项定向 Vitest 测试通过，覆盖全部 21 档、非整档吸附、重置、表单提交和历史比例保留；定向 ESLint、`vue-tsc -b` 通过。
- 本地浏览器实际 Vue 组件验证：ArrowRight 50% → 55%，ArrowLeft → 50%；拖动吸附到 65%；Home/End 为 0%/100%；重置恢复均衡。加载 73.5% 时模型与显示保持原值，重新操作后转入 5% 档位。临时夹具、服务和浏览器页已清理。
- 用户随后明确授权，14:55 部署 hk1 `v20260905.1447`。本地构建与 60 项定向回归通过，在线资源确认 500 bps 步长，健康/版本/权限接口及数据保持检查通过；当前名单为 1 人，与本次发布前一致，没有名单变更或付费请求。工作区额外锁文件改动保留，本次仅在独立构建副本沿用已验证依赖锁文件，详见 [5% 步长发布证据](evidence/hk1-20260905/step5-release.md)。
- 已交付 [hk1 测试方案](test-plan-hk1.md)，明确快速验收、31 项核心用例、隔离故障注入、缓存/权重验算与真实流量门禁，环境未执行项目不标记完成。

### 2.1 OpenSpec

```bash
npx -y @fission-ai/openspec@latest validate add-api-key-multi-group-routing --type change --strict --no-interactive
```

结果：`Change 'add-api-key-multi-group-routing' is valid`。

### 2.2 后端

```bash
cd backend
go test ./... -count=1
go test -tags=unit ./... -count=1
go test -race ./internal/handler ./internal/server/middleware ./internal/service ./internal/repository \
  -run 'APIKeyRoute|Routing|BatchImage|Grok|Gemini|OpenAIResponsesWebSocket' -count=1
make build
```

结果：全部退出码为 0；生产二进制构建成功。

### 2.3 前端

```bash
cd frontend
pnpm lint:check
pnpm typecheck
pnpm test:run
pnpm build
```

结果：lint、typecheck、生产构建通过；Vitest 共 258 个文件、1862 项测试通过。

### 2.4 本地路由计划基准

```bash
cd backend
go test ./internal/service -run '^$' \
  -bench '^BenchmarkAPIKeyRouteCoordinatorBuildPlan$' \
  -benchmem -benchtime=1s -count=5
```

测试主机：Apple M4，darwin/arm64。

| 场景 | ns/op 范围 | B/op | allocs/op |
|---|---:|---:|---:|
| 功能关闭、单分组 | 84.48–87.72 | 136 | 2 |
| 功能开启、单分组 | 144.7–147.0 | 256 | 4 |
| 功能开启、8 分组 | 457.0–458.7 | 1536 | 9 |

该基准包含固定桶阶段计时本身的开销。这组数据只衡量本地路由计划构建，不能替代 hk1 上包含鉴权、Redis、账号调度和真实并发的端到端 P50/P95/P99。

### 2.5 快照与 Redis 往返预算

- `TestAPIKeyAuthSnapshotEightCandidatesStaysWithinBoundedPayloadBudget`：代表性 8 候选鉴权快照为 8,748 bytes，防回归上限为 64 KiB。
- `TestAPIKeyRouteRuntimeStateLoadsStickyAndEightBreakersInOnePipeline`：确认 1 个粘性 GET 与 8 个 breaker HGETALL 共用 1 次 Pipeline。
- `TestAPIKeyRouteRuntimeStatePreloadsStickyAndClosedBreakersOnce`：确认后续 CLOSED 候选准入不增加逐组 Redis 读取。
- `TestAPIKeyRouteRuntimeStateKeepsAtomicAdmissionForNonClosedBreaker`：确认 OPEN/HALF_OPEN/RECOVERING 仍经过原子租约/渐进放量准入，不被批量快照旁路。
- `TestAPIKeyService_GetByKey_UsesL1Cache`：确认 L1 命中不再次回源 API Key Repository；路由版本 guard 按检查周期合并，不逐请求访问 PostgreSQL。
- migration 测试覆盖候选正反索引、outbox 待投递索引、routing attempt 诊断/保留索引以及健康/价格聚合主键。

### 2.6 后台数据库隔离与分阶段指标

- `TestProvideRoutingBackgroundDBUsesDedicatedBoundedLazyPool`：确认路由后台池与业务池使用不同 DI 类型，配置上限生效，且 `sql.Open` 在未启用后台工作时不会建立连接。
- `TestProvideRoutingBackgroundDBKeepsPoolBoundedWithZeroValueConfig`：确认绕过配置加载器的零值构造仍回退到 2 连接硬上限。
- `TestRoutingScoreObservationSourceEnforcesPerQueryTimeout`：确认慢聚合查询会被独立 deadline 取消。
- `TestRoutingRuntimeMetricsSnapshotUsesBoundedDimensions`：确认只接受固定阶段并输出 P50/P95/P99/Max，同时暴露后台查询次数、失败数、聚合输入桶行数和平均/最大耗时。
- `/api/v1/admin/routing-optimization/runtime-metrics` 的 `phase_latency` 固定包含 `auth_lookup`、`plan_build`、`state_read`、`smart_ranking`、`state_write`；`background_queries` 提供后台聚合观测。
- PostgreSQL 连接设置 `application_name=sub2api-routing-background`，便于 hk1 通过 `pg_stat_activity` 单独核对连接占用。默认配置为 `routing_background_max_open_conns=2`、`routing_background_max_idle_conns=1`、`routing_background_query_timeout_seconds=15`。

### 2.7 hk1 压测工具

仓库内 `backend/cmd/api-key-routing-bench` 使用固定 OpenAI-compatible streaming Chat 请求执行有界并发，API Key 只从 `SUB2API_BENCH_API_KEY` 环境变量读取，不接受命令行 Key，也不输出响应正文。结果为 JSON，包含成功率、完成/成功 RPS、总延迟与语义 TTFT 的 P50/P95/P99/Max、usage 覆盖数，以及输入/输出/总 token 吞吐；错误只按 timeout、HTTP 状态或有界 taxonomy 汇总。

```bash
cd backend
export SUB2API_BENCH_API_KEY="从安全来源读取，不要写入仓库"
go run ./cmd/api-key-routing-bench \
  -base-url http://127.0.0.1:18080 \
  -model <固定模型> \
  -scenario eight-groups-smart \
  -requests 1000 \
  -concurrency 32
```

四个对比场景必须分别使用功能关闭单组、功能开启单组、8 组 sequential、8 组 smart 的专用 Key，保持模型、prompt、`max-tokens`、请求数和并发一致；每轮同时保存运行时指标前后快照。`go test ./cmd/api-key-routing-bench -count=1` 覆盖 nearest-rank 分位数、语义 TTFT 判定、usage 覆盖和失败汇总。

### 2.8 持续优化本地安全门禁

- `TestAPIKeyRoutingPersonalizationColdStartShrinkageAndKeyIsolation`：验证新 Key 共享 baseline、低样本不生效、cohort/Key 分层收缩，以及一个 Key 不会改变其他 Key 或全局快照。
- `TestAPIKeyRoutingPersonalizationDisablesDriftedArtifact`：验证 drift 自动停用整个残差制品。
- `TestAPIKeyRoutingPredictionIsBoundedAndOnlyTouchesRuleEligibleCandidates` 与 handler 硬过滤测试：验证模型只处理规则已准入候选，无法复活低于 50% 成功率或请求能力不匹配的分组。
- `TestAPIKeyRoutingPredictionTimeoutFallsBackAtomically`、`TestRoutingPredictionArtifactRejectsCorruptionAndUnknownFeatures`：验证超时不留下部分修正，checksum/未批准特征制品拒绝加载。
- `TestRoutingLearningRuntimeRefreshesOffPathAndAppliesFromLocalMemory`：验证刷新完成后在线 Apply 不再读取 Redis/SQL，版本随请求事实固定。
- `TestRoutingLearningArtifactLifecycleRequiresShadowCanaryAndApproval`：验证学习制品不能从 draft 直接 active，必须经过 shadow、canary 和人工批准。
- `TestRoutingRuntimeMetricsSnapshotUsesBoundedDimensions`：覆盖固定 fallback 枚举、校准值与推理耗时分位数，不引入 Key/组/版本标签。

Apple M4、darwin/arm64、8 候选、`-benchtime=500ms -count=3` 的本地微基准：个性化为 1.296–1.311 µs/op、模型为 3.688–3.874 µs/op、个性化后模型为 5.017–5.035 µs/op。该结果证明本地计算本身有界，但不替代 hk1 端到端 P95 门禁。

### 2.9 本地 linux/amd64 发布制品

2026-09-05 按“只在本地构建、服务器不构建镜像”的发布约束执行：

```bash
docker buildx build --platform linux/amd64 --load \
  --tag sub2api:v20260905.0556 \
  --build-arg VERSION=v20260905.0556 \
  --build-arg COMMIT=2369bd2d5656-dirty \
  --file Dockerfile .
```

- 前端 pnpm 9 frozen-lockfile 安装、Vue/TypeScript 生产构建和 Go `embed` release 编译全部成功。
- 本地镜像为 `linux/amd64`、47,024,334 bytes，manifest list digest 为 `sha256:d028f79d49323285f338717b4f82092d34e74dc0728d63292f5356d68adf0789`。
- 容器内 `--version` 输出 `v20260905.0556`、commit `2369bd2d5656-dirty`；`dirty` 明确表示这是待验证工作树制品，而非可追溯正式发布提交。
- 当时导出 `/tmp/sub2api-v20260905.0556-linux-amd64.tar.gz`（44 MiB），SHA-256 为 `62ead8b6933c4bb1c05f8f475fcce309b9eb87740e2ebf6c599de848d18ec0f9`。该阶段仅是本地制品证据；之后获授权部署并迭代到 0811，详见 hk1 报告。
- 使用该 amd64 镜像与一次性 PostgreSQL 18、Redis 8 容器完成本地启动冒烟，`/health` 返回 `{"status":"ok"}`，容器健康状态为 `healthy`。
- `schema_migrations` 确认 240–247 八个迁移全部执行，五个新增控制面表均存在；启动日志确认路由后台数据库池按 `max_open=2`、`max_idle=1`、`query_timeout=15s` 配置并成功启动服务。本地空库冒烟不替代 hk1 的真实数据 `EXPLAIN`、连接占用和灰度验证。

### 2.10 部署配置可达性

- `deploy/docker-compose.yml` 与 `deploy/.env.example` 已显式暴露多分组、优化、事实采样、个性化、模型、探索、粘性、缓存补偿和 breaker 的全部 `GATEWAY_API_KEY_*` 启动参数，默认值全部保持 fail-closed。
- `TestLoadAPIKeyRoutingEnvironmentOverrides` 证明 Compose 使用的大写环境变量会被 Viper 加载为对应字段；`go test -tags=unit ./internal/config -count=1` 通过。
- 使用无敏感信息的临时 PostgreSQL 占位密码运行 `docker compose config` 成功，展开结果包含全部 12 个路由环境变量；探索开关固定默认 `false`，错误开启仍由配置校验拒绝启动。
- 运行手册给出数据影子、确定性路由、策略实验和学习制品四阶段开关矩阵，并明确开关是进程级快照，修改后必须重建应用容器。

## 3. 协议与安全重放证据

`TestAPIKeyRoutePhysicalGroupSelectionPrecedesProtocolSideEffects` 对下列入口执行源码级顺序回归检查，要求实际组选择早于分组能力、计费、账号调度或任务提交副作用：

- Claude Messages；
- Gateway/OpenAI Responses 与 Chat Completions；
- OpenAI Embeddings、Images、Live、Live Sideband、Responses WebSocket；
- Gemini native generate/stream；
- Grok 媒体、Realtime、Voice；
- Batch Image 与异步图片任务。

流式、SSE 和 WebSocket 使用统一语义输出门闩：仅在尚未输出语义内容且错误分类允许重放时跨组；传输心跳不关闭门闩，任一语义字节会永久关闭。媒体任务在提交上游前锁定实际组；Batch Image 的幂等重放沿用原任务实际组，不重复记录本次候选成功。

## 4. hk1 待补证据

下段 06:02 为部署前历史快照，不代表最终运行状态。授权后已完成的环境验证见 [2026-09-05 hk1 报告及原始 JSON](evidence/hk1-20260905/README.md)；清单中仍未完整覆盖的部分继续作为发布门禁，不能因为容器 healthy 就勾选全部。

2026-09-05 06:02（Asia/Shanghai）只读复检：hk1 仍运行 `sub2api:v20260903.1435`，应用、PostgreSQL 18 和 Redis 8 均健康，新实现尚未部署；健康接口返回 `{"status":"ok"}`，根盘剩余约 12 GiB。业务库约 1,393 MiB，当前 4 个 idle、1 个 active 连接；尚无 240+ 迁移。Redis 为 18 个客户端、0 blocked、0 rejected、0 evicted，检查时约 2 ops/s；最近 30 分钟应用日志没有 error/panic/fatal/deadline。远程持久化配置尚无新路由键，Compose 仍引用旧 tag，因此首次加载新镜像时必须保持新开关默认关闭并先完成数据影子检查。更早的旧镜像日志曾存在 Channel Monitor `insert history`/`mark checked` 的 `context deadline exceeded`，压测时仍需把它作为既有错误基线区分。这组只读信息不构成 13.6b 完成证据。

以下项目必须在测试环境执行并把原始结果或仪表盘快照附到本文件后，才能勾选 13.5–13.9：

1. 相同请求体、模型、账号池和并发下，对功能关闭单组、功能开启单组、8 组 sequential、8 组 smart 分别压测。
2. 报告成功率、请求数、req/s、P50/P95/P99、TTFT、输入/输出/总 token 吞吐，以及路由分阶段耗时。
3. 验证新增路由 P95 不超过 5ms；若不满足，只能使用书面批准后的新预算。
4. 用 Redis 命令/客户端指标确认正常 CLOSED 路径为一次状态读取 Pipeline、一次成功状态 Lua 更新；不得有 N 次串行读取。
5. 用 PostgreSQL `EXPLAIN (ANALYZE, BUFFERS)` 和连接池指标确认所需索引生效、Builder/消费者未挤占请求连接。
6. 执行功能关闭、Redis 丢失、builder/outbox 停止、事实链路异常、制品损坏、canary 自动回退和旧版本回滚演练。
7. 抽样核对 usage、账单、ops log 和 routing facts 的最终实际组、initial group、route version 与 switch count 一致。

## 5. 完整上线判定

只有同时满足以下条件才允许把 13.10 标为完成：

- 本文件第 4 节全部有 hk1 证据；
- 灰度阶段无未解释的硬护栏越界；
- 回滚演练成功，关闭多分组后旧 `group_id` 镜像仍可服务；
- 告警、运行手册和用户说明已随发布交付；
- 生产扩大流量另行获得明确授权。
