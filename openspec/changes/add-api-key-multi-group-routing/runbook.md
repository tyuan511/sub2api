# 多分组路由运行手册

## 1. 开关与默认行为

默认配置保持关闭：

```yaml
gateway:
  api_key_multi_group_routing_enabled: false
  api_key_routing_optimization_enabled: false
  api_key_routing_personalization_enabled: false
  api_key_routing_model_prediction_enabled: false
  api_key_routing_exploration_enabled: false
database:
  routing_background_max_open_conns: 2
  routing_background_max_idle_conns: 1
  routing_background_query_timeout_seconds: 15
```

- `api_key_multi_group_routing_enabled=false`：请求继续使用兼容镜像 `api_keys.group_id`，历史 `group_id=NULL` 仍保持未限定账号池语义。
- `api_key_multi_group_routing_enabled=true`：已配置 `group_routes` 的 Key 才使用候选路由；单分组和旧 Key 保持兼容。
- `api_key_routing_optimization_enabled=false`：停止普通决策事实采样、shadow/canary 和持续优化控制环；基础失败/容量/换组事实及其有界消费者继续运行，不关闭确定性 sequential/smart baseline，也不清理健康粘性。
- 路由健康/价格聚合与 Channel Monitor V1/V2/off 无关。每分钟重算最近 10 个完整分钟，另回填最多 30 分钟历史块（最远 24h），冷启动历史补齐约需 48 轮；每块事务超时 15 秒、整轮 45 秒。不要通过启用监控 V2 来修复缺失的路由桶。
- 个性化残差与本地模型增强是满足数据门槛后的独立能力，默认关闭；只有同时开启多分组和优化总开关时配置才会接受。受控探索仍保持不可启用，直到 propensity 与流量预算控制完整交付。
- 路由后台连接池只供评分、事实消费、回放和历史清理使用；`max_open` 首发不得超过 2，放宽前必须先证明不会挤占请求池。连接使用 `application_name=sub2api-routing-background`，慢评分查询由独立 deadline 取消。

Docker Compose 通过 `.env` 暴露同名大写配置。开关是进程级启动快照，修改后必须滚动重建应用容器；不得只修改 shell 环境却复用旧容器：

| 灰度阶段 | `GATEWAY_API_KEY_MULTI_GROUP_ROUTING_ENABLED` | `GATEWAY_API_KEY_ROUTING_OPTIMIZATION_ENABLED` | personalization/model | exploration |
|---|---:|---:|---:|---:|
| 数据影子/迁移检查 | `false` | `false` | `false` | `false` |
| 单组、sequential、sticky/breaker、smart baseline | `true` | `false` | `false` | `false` |
| shadow/canary 策略实验 | `true` | `true` | `false` | `false` |
| 已批准学习制品灰度 | `true` | `true` | 按制品独立开启 | `false` |

`GATEWAY_API_KEY_ROUTING_EXPLORATION_ENABLED` 当前必须始终为 `false`；设为 `true` 会触发 fail-closed 启动校验。粘性、缓存补偿和 breaker 参数也通过 `GATEWAY_API_KEY_GROUP_*` 变量显式保存到 `.env`，避免镜像更新时丢失。

## 2. 上线前检查

1. 本地完成 `verification.md` 中全部构建和测试。
2. 在 hk1 备份数据库并确认迁移 240–247 的执行计划；这些迁移是增量式，不应通过删表回滚。
3. 检查 Redis 可用、内存余量、Stream 积压和连接池；确认所有路由 key 使用 `{api_key_id}` hash tag，并在 `pg_stat_activity` 中单独核对 `sub2api-routing-background` 不超过配置上限。
4. 确认管理员可访问 `/api/v1/admin/routing-optimization/runtime-metrics`、路由解释、制品、实验和回滚接口。
5. 准备专用测试用户、单组 Key、两组 sequential Key 和最多 8 组 smart Key；候选必须同平台、同计费类型。

### 2.1 学习制品门禁

- 个性化使用 `feature/routing-personalization-v1` 制品；单个 scope 最多 2,048 条 cohort/Key 残差、512 KiB、7 天有效期，修正按样本量和置信度收缩并受成功率/延迟/成本/容量/缓存上限约束。Key 只使用数值 ID，本地最多观察 128 个 scope，不为每个 Key 建 Redis 评分副本。
- 模型使用 `model/routing-prediction-model-v1` 的 `bounded-linear-components-v1`；最多 8 个批准特征和 8 个已通过规则硬过滤的候选，单制品最多 256 KiB、7 天有效期、在线预算 10–5,000 微秒。模型只输出成功率、TTFT、完成耗时、容量溢出、缓存命中和归一化成本的有界修正，最终排序仍由当前偏好规则完成。
- 网关只在后台从 Redis 刷新制品，在线路径只读原子本地目录。checksum、lineage、依赖、schema、TTL、数据质量、drift、校准、缺失特征、NaN/Inf、输出越界或超时任一失败时，整个模型修正原子回退同一请求的确定性 baseline。
- 新制品只能由管理 API 创建为 `draft`。学习制品必须依次经过 shadow、带稳定分桶的 canary 和人工批准才能 active；不同实验的个性化和模型 canary 不在同一请求内组合。开启开关前必须先准备已知安全的 active no-op/baseline 制品和 Redis 指针。
- `runtime-metrics` 的 `personalization` 与 `model_prediction` 只包含固定 fallback 原因、应用数、最近校准误差和模型推理 P50/P95/P99/Max；不得增加 Key、分组或版本动态标签。
6. 确认 usage、账单、ops log、routing attempt 和 Channel Monitor 聚合均可按实际组核对。
7. 确认 `/api/v1/admin/routing-optimization/runtime-metrics` 可读取，并保存 `phase_latency` 五个固定阶段和 `background_queries` 的空载基线。

## 3. 灰度顺序

阶段之间必须保留完整观察窗口；任一阶段出现硬护栏越界即停止推进。

1. **数据影子**：保持多分组请求路由关闭，只验证迁移、outbox、事实编码、敏感字段约束和离线回放。
2. **单组兼容**：开启多分组总开关，仅允许单候选测试 Key，比较成功率、延迟、计费和日志等价性。
3. **指定 Key sequential**：只给专用 Key 配置两个候选，验证组内账号耗尽后换组、失败分类与语义输出门闩。
4. **sticky/breaker**：验证一小时组级粘性、`<50%` 硬熔断、单租约 HALF_OPEN、RECOVERING 渐进恢复和旧会话排空。
5. **smart baseline**：仍关闭优化实验，只启用确定性规则评分；price/speed/balanced 只改变权重且共用成功率硬门槛。
6. **shadow**：开启优化总开关，候选策略只计算差异，不申请容量、不写粘性、不改变计费和真实路由。
7. **canary**：使用稳定 Key/用户分桶，仅影响无健康粘性的新会话；先极小 allocation，再按证据扩大。
8. **扩面**：按平台、模型族、端点逐批扩大，不跨维度一次性全开。

压测使用 `backend/cmd/api-key-routing-bench`。先从安全来源导出 `SUB2API_BENCH_API_KEY`，再对四种 Key 运行完全相同的模型、prompt、输出上限、请求数和并发；不要把 Key 放进命令行、报告或 shell 脚本。每轮保存工具 JSON 及管理端 `runtime-metrics` 前后快照。

## 4. 告警与停止阈值

首版阈值；hk1 基线完成后可以收紧，不得在无记录的情况下放宽。

| 指标 | 告警/停止条件 | 动作 |
|---|---|---|
| 新增路由 P95 | >5ms 持续 10 分钟 | 停止扩面，回到上一阶段 |
| 最终成功率 | 相对 baseline 下降 >0.5 个百分点或置信区间确认退化 | 自动停 canary，回 baseline |
| P95/P99 | 任一关键切片相对 baseline 上升 >10% | 停 canary并检查容量/切换 |
| 每次最终成功成本 | price/balanced 超过各自 envelope | 阻止晋级 |
| 换组率 | 高于 baseline + 3 倍历史标准差或绝对值 >5% | 检查 breaker/账号池根因 |
| Redis degraded | 5 分钟比例 >0.1% | 停止 smart/canary 扩面 |
| 评分快照年龄 | >180 秒或超过 3 个构建周期 | 顺序降级并告警 |
| dropped critical fact | 任意增量 | 立即暂停 canary/探索 |
| dropped sample | 5 分钟 >1% | 关闭普通采样，保护关键事件 |
| outbox oldest pending | >60 秒 | 停止配置扩面，排查投递器 |
| Stream backlog | >50,000 或持续增长 10 分钟 | 降采样并扩消费者，不丢财务事实 |
| breaker OPEN | 单 Key/组持续 15 分钟 | 检查共享 dependency domain，避免重复惩罚 |

自动晋级不得只看总平均值；必须同时满足样本量、观察周期、置信区间和平台/模型/端点/关键 Key 切片。

## 5. 故障演练

### 5.1 功能关闭

关闭 `api_key_multi_group_routing_enabled` 并滚动重启。确认请求使用 priority=0 的 `group_id` 镜像；不要删除候选关系或 route facts。

### 5.2 优化链路关闭

关闭 `api_key_routing_optimization_enabled`。确认 deterministic baseline 仍可服务，健康 breaker、粘性和进行中请求不被清理；shadow/canary、普通决策采样与学习控制环停止，但基础失败/容量事实消费者继续提供成功率分母。

### 5.3 Redis 丢失

阻断测试实例 Redis 后确认：请求只在用户已配置候选内保守 sequential 降级；不得把空 group 传入全局账号池；日志与指标标记 degraded。恢复 Redis 后让新流量自然重建临时状态，不从 PostgreSQL 伪造旧粘性。

### 5.4 builder/outbox/事实链路停止

- Builder 停止：smart 在快照 stale grace 后降为用户顺序。
- Outbox 停止：禁止继续修改路由配置；恢复后验证 claim/lease/retry 和 route/dependency version 收敛。
- 决策事实链路异常：暂停 canary 与探索，但不阻断模型响应和可靠 usage/账单路径。

### 5.5 制品损坏与自动回滚

发布 checksum/schema 不兼容的测试制品，确认实例拒绝加载并回 baseline。触发 canary 硬护栏，确认 current 指针原子回到 baseline、实验进入 paused、原因可审计，健康粘性不被删除。

## 6. 回滚

1. 首先关闭优化总开关，停止 shadow/canary 影响。
2. 如确定性多分组仍异常，再关闭多分组总开关并滚动重启，恢复 `group_id` 兼容路径。
3. 保留 PostgreSQL 增量列、候选关系、outbox、制品与 routing facts；不要在事故期间执行破坏性逆迁移。
4. 核对已开始请求仍按其冻结的 route/strategy/score version 完成；不得批量清除粘性逼迫切换。
5. 抽样核对实际组计费、异步媒体任务归属和补偿上限后再恢复流量。

## 7. 数据治理

- 决策事实只允许 ID、枚举、布尔、分桶和有界数值；禁止 prompt、response、API Key 明文、上游凭据、账号地址和可反推正文的数据。
- 候选数组最多 8 项；普通首选成功按稳定概率采样，失败、切换和财务关键事实走高优先级。
- `sample_probability` 记录观察采样；只有真实随机探索才允许非空 `action_propensity`。确定性历史不得宣称因果收益。
- sample/diagnostic/critical 默认分别保留 30/90/180 天；Redis Stream 使用近似 `MAXLEN=100000`，过期清理由有界批次执行。
- 数据集必须保存查询版本、时间边界、feature schema、采样/排除规则和 checksum；训练与回放使用决策时点快照或 point-in-time join。
- 未选择候选的结果必须标记 unobserved；不得把缺失结果当失败或成功。

## 8. 用户说明

- 多分组新特性仅向“系统设置 → 功能开关 → 多分组与智能调度灰度名单”中的用户开放；名单外保持旧单组界面和行为，管理员也需显式加入。默认空名单，不是全量开放。

- 一个 Key 最多选择 8 个同平台、同计费类型分组，并按用户顺序配置。
- sequential 和 smart 都只在用户选中的候选内工作；不能接受的倍率不加入候选即可。
- 仅选一个分组时固定使用该组，页面隐藏调度/熔断/智能倾向选项，后端跳过 Key 级组间 sticky/breaker；账号级容错、分组启用状态、权限和额度检查仍生效。只有配置了至少两个启用候选才启用这些控制；多组故障后只剩一个可用组不属于此例外。
- smart 的 price/speed/balanced 是同一套动态评分的不同权重，成功率和健康硬门槛始终优先；有效窗口成功率低于该 Key 的门槛会熔断。新建默认 80%，可在 50%–95% 之间每档 5% 调整；旧 Key 保持原值，只有主动重置才改为 80%。
- price 使用窗口整体缓存命中率以及实际输入/输出 token 结构，以 full-cache 参考成本归一化；页面预估倍率是解释值，不是价格承诺。
- 故障转移后会在窗口内保持组级粘性，保护会话和上游缓存；主组探活成功后只先接新会话，旧会话自然排空。
- 使用记录、账单和运维日志显示最终实际路由分组，同时保留初始组、切换次数和路由版本用于解释。
- 持续优化不会扩大候选白名单，也不能绕过熔断、重放安全、健康粘性和实际组计费。

## 9. 用户名单灰度发布步骤

1. 先部署所有实例；旧版本不识别灰度名单，混合版本期间不能承诺完整限制。确认多分组总开关状态，空名单下用户能力接口为 false，单组仍可请求，高级配置写入被拒绝。
2. 在系统设置的功能开关页搜索用户名/邮箱，或输入完整用户 ID，选择少量专用测试用户并保存。最多 1000 位；空数组表示撤销所有用户资格，配置变更写审计日志。
3. 本实例保存后立即生效，其他实例正常情况下最多约 5 秒加一次有界查询收敛。刷新失败关闭高级资格；不要将权限资格缓存进长期 JWT/鉴权对象。刷新用户 Key 页面后核对界面，再验证名单内多组请求、名单外旧路径及 API 拒绝。
4. 按专用测试用户、小批真实用户、分批扩名单推进。分别观察成功率、实际组计费、缓存命中率及用户投诉；名单灰度不自动启动 shadow/canary，也不替代已有发布门禁。
5. 回退时移出指定用户或清空名单。原多组配置与 Redis 状态不批量删除，新请求固定回原主组；已开始的流保持原计划。原主组不健康/停用仍可能报错，切组也可能产生冷缓存，建议先核对主组并小批量操作。
6. 若需要恢复，重新加入名单；未被用户主动改组的原配置会重新生效。注意回滚到无名单门禁的旧镜像将重新忽略名单，应配合关闭多分组总开关，不能只回滚镜像就声称灰度隔离仍成立。
