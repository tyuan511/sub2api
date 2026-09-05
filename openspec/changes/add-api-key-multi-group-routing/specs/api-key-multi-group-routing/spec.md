# API Key 多分组调度规范

> 状态：Draft<br>
> 能力标识：`api-key-multi-group-routing`<br>
> 适用对象：用户创建的外部 API Key 及其网关请求<br>
> 核心原则：用户决定候选分组，系统只在候选集合内调度；所有智能优化都以成功率和可用性为前提；账单与使用记录始终归属实际提供服务的物理分组。

## 1. 背景与目标

当前一个 API Key 只绑定一个物理分组。分组内可以切换账号，但当整个分组不可用、容量不足或服务质量下降时，请求无法在用户认可的其他分组之间继续转移。

本能力允许一个 API Key 绑定一组有序物理分组，并提供两种调度模式：

- `sequential`：顺序调度。按用户排列顺序选择分组，当前分组无法服务时再进入下一个分组。
- `smart`：智能调度。只在用户选择的分组中，先以成功率、熔断状态和实时容量确定可用候选，再按价格优先、速度优先或均衡策略使用不同评分权重动态排序。

本能力同时解决以下问题：

- 分组级故障转移，而不破坏现有分组内账号调度。
- 故障发生后，在一个窗口期内让该 Key 的后续请求避开故障分组。
- 同一会话尽量固定在同一物理分组，减少上下文缓存失效。
- 分组恢复后渐进回切，不强制迁移仍在使用备用分组的活跃会话。
- 预估倍率可解释，实际计费与使用记录准确归属实际分组。
- 在多实例部署下保证粘性、熔断、半开探测和版本失效的一致性。

## 2. 固定产品决策

1. 一个外部 API Key 可以配置多个物理分组，但不会创建多个外部密钥。
2. 同一个 Key 的全部候选分组 MUST 属于同一平台；第一阶段还 MUST 使用相同的计费类型 `subscription_type`。
3. 候选分组是用户显式选择的硬白名单。调度器 MUST NOT 自动加入同平台的其他分组。
4. 用户通过“不把某分组加入候选列表”表达价格边界；第一阶段不额外提供倍率上限。
5. 智能调度中的价格优先、速度优先和均衡策略使用同一组评分维度、不同的评分权重；三者都 MUST 先满足成功率与健康准入，低价或低延迟不得补偿不可接受的成功率。
6. 使用记录、账单、成本、倍率、订阅扣减和利润控制都 MUST 以最终实际成功的物理分组为准。
7. 单分组 Key、存量 Key 和旧版客户端 MUST 保持现有行为。仅配置一个启用候选时 MUST 固定使用该组，不启用 Key 级跨组调度、成功率熔断或组级粘性；组内账号级容错及权限/额度/协议检查保持不变。
8. 分组内账号重试发生在分组间切换之前。单个账号故障 MUST NOT 直接等同于整个分组故障。
9. 已向客户端输出语义数据后，不得通过分组切换重新执行请求。
10. 回切 MUST 优先保护已有会话的缓存连续性；健康恢复不等于缓存已经预热。
11. 任一有效统计窗口内的分组访问成功率一旦低于 50%，该分组 MUST 立即熔断；价格、速度、容量或用户顺序都不得覆盖该硬阈值。
12. 价格评分 MUST 使用一定窗口内的整体缓存命中率、普通输入、缓存创建、缓存读取和输出用量，换算成以“输入 100% 命中缓存”为基准的归一化有效倍率；不得直接使用名义分组倍率排序。

## 3. 术语

- **路由集合（route set）**：一个 API Key 配置的、有顺序的物理分组列表。
- **配置分组（configured group）**：路由集合中的任一物理分组。
- **首选分组（primary group）**：优先级最小的第一个配置分组。
- **实际分组（effective group）**：本次请求最终由其账号成功提供服务并承担计费的物理分组。
- **分组访问（group visit）**：一次请求进入某个物理分组并执行该分组内部账号选择、重试和故障转移的完整过程。
- **路由版本（route version）**：路由集合或策略每次变更时递增的版本号，用于隔离旧粘性和熔断状态。
- **会话粘性（route stickiness）**：同一 Key、同一路由版本、同一会话和同一模型族尽量继续使用同一实际分组。
- **路由熔断器（route breaker）**：某个 Key 对某个配置分组、模型族和端点类型的短期状态机。
- **全局健康（global health）**：由所有有效流量聚合出的物理分组、模型族和端点类型的历史服务质量。
- **语义输出（semantic output）**：会被客户端解释为模型内容、工具调用、媒体结果或最终响应的输出；SSE 心跳或协议注释不属于语义输出。
- **冷缓存补偿（cold-cache compensation）**：因系统主动故障转移造成上游上下文缓存失效时，对用户计费量进行的有界修正，不改变供应商真实用量。

## 4. 非目标

- 不建设“每个平台自动汇总全部分组”的公共 Auto 分组。
- 不允许跨平台路由，也不在第一阶段混用标准计费与订阅计费分组。
- 不替代物理分组内部已有的账号筛选、账号粘性、并发控制和账号故障转移。
- 不保证供应商侧提示词缓存能够跨账号、租户或分组迁移。
- 不根据单次客户端错误、内容策略拒绝或用户余额问题惩罚物理分组。
- 不把预估倍率定义为报价承诺；实际账单仍按实际分组和实际模型用量结算。

## 5. 配置与接口契约

### 5.1 创建和更新请求

新增字段：

```json
{
  "group_routes": [
    { "group_id": 101, "priority": 0 },
    { "group_id": 205, "priority": 1 }
  ],
  "schedule_mode": "smart",
  "smart_preference": "balanced",
  "expected_route_version": 3
}
```

字段约束：

- `group_routes` MUST 包含 1 到 `max_groups_per_key` 个分组；默认上限为 8，管理员可配置。
- `group_id` 在同一个路由集合内 MUST 唯一。
- `priority` MUST 从 0 开始连续递增，且不得重复。
- `schedule_mode` 只能是 `sequential` 或 `smart`。
- `smart_balance_bps` 为可空整数 0..10000：0 为纯价格倾向，10000 为纯速度倾向；新 UI 默认 5000，步长 500（5%），拖动与键盘每档一致。仅用户重新调整时吸附到 5% 档位，已有非整档比例与旧预设回显/未调整保存保持原值，后端仍兼容整数 bps，不回填历史配置。该比例只分配价格/速度预算，不移除成功率和容量评分。
- `smart_preference` 保留为兼容字段。传入数值比例时服务端 MUST 派生该字段（小于 5000 为 price、大于为 speed、等于为 balanced）；未传比例的旧客户端继续使用三个预设。在 `sequential` 模式下两者为空。
- `routing_min_success_rate` MUST 为 50..95 的整数，步长 5；新建省略时默认 80，UI 重置也为 80%；顺序和智能调度都执行。已有 Key 与历史事实 MUST 保留原值（包括 50%），PATCH 省略时保留原值，非法范围或档位返回 400。已应用迁移 248 MUST 保持不变，迁移 249 只修改新记录的数据库默认值，不触发旧 Key 的配置/状态版本变化。
- 更新 MUST 原子替换整个路由集合，且 SHOULD 使用 `expected_route_version` 做乐观并发控制。
- 成功更新后 `route_version` MUST 单调递增。

兼容旧客户端：

- 只提交 `group_id` 时，服务端 MUST 把它归一为一个优先级为 0 的路由，并使用 `sequential`。
- 同时提交 `group_id` 与 `group_routes` 时，`group_id` MUST 等于 `group_routes[0].group_id`，否则返回稳定的 400 错误。
- 读取响应在兼容期 MUST 同时返回旧 `group_id` 和新 `group_routes`；旧 `group_id` 始终镜像首选分组。
- 历史 `group_id = null` 且从未显式配置 `group_routes` 的 Key MUST 保留升级前的未限定账号池语义；该迁移例外不得应用于显式多分组配置。新客户端显式提交 `group_routes` 时仍 MUST 提交 1 到 `max_groups_per_key` 项。

### 5.2 返回模型

管理端读取 API Key 时 SHOULD 返回：

```json
{
  "group_id": 101,
  "group_routes": [
    {
      "group_id": 101,
      "priority": 0,
      "current_rank": 1,
      "platform": "anthropic",
      "current_rate": 1.2,
      "normalized_effective_rate": 1.97,
      "cache_hit_rate": 0.60,
      "price_window": "1h",
      "price_confidence": "high",
      "logical_input_tokens": 10000000,
      "output_tokens": 1000000,
      "health": "healthy",
      "success_rate": 0.995,
      "latency_p95_ms": 1800,
      "score": 0.91,
      "score_breakdown": {
        "success": 0.99,
        "price": 0.83,
        "speed": 0.88,
        "capacity": 0.92
      }
    }
  ],
  "schedule_mode": "smart",
  "smart_preference": "balanced",
  "route_version": 4,
  "estimated_rate": {
    "value": 1.97,
    "low": 1.90,
    "high": 2.05,
    "reference": "full_cache_1x",
    "window": "1h",
    "confidence": "medium",
    "updated_at": "2026-09-04T10:00:00Z"
  }
}
```

`priority` 表示用户配置顺序；`current_rank` 表示智能评分在当前快照下产生的动态顺序；`current_rate` 是名义倍率；`normalized_effective_rate` 是结合窗口内缓存与输入输出结构计算的实际价格指标。健康、性能和评分拆解仅用于展示与解释，MUST NOT 暴露账号凭据、上游地址或其他用户的专属信息。

## 6. 持久化模型

### 6.1 API Key 字段

`api_keys` 新增：

| 字段 | 类型 | 约束 |
|---|---|---|
| `schedule_mode` | enum/string | `sequential` 或 `smart`，默认 `sequential` |
| `smart_preference` | nullable enum/string | `price`、`speed`、`balanced` |
| `smart_balance_bps` | nullable integer | 0..10000；空值保留旧预设行为 |
| `routing_min_success_rate` | integer | 50..95，步长 5，新建默认 80，旧值不回填 |
| `routing_state_version` | bigint | 粘性和 breaker 命名空间版本，仅候选集合/顺序改变时递增 |
| `route_version` | bigint | 非空，默认 1，路由配置变更后递增 |

兼容期保留现有 `api_keys.group_id`。它 MUST 与优先级 0 的路由同步，直到所有读取路径、回滚版本和旧客户端都不再依赖该字段。

### 6.2 路由表

新增 `api_key_group_routes`：

| 字段 | 类型 | 约束 |
|---|---|---|
| `id` | bigint | 主键 |
| `api_key_id` | bigint | 外键，删除 Key 时级联删除 |
| `group_id` | bigint | 物理分组外键 |
| `priority` | int | 从 0 开始 |
| `enabled` | bool | 默认 true |
| `created_at` | timestamp | 非空 |
| `updated_at` | timestamp | 非空 |

MUST 建立 `(api_key_id, group_id)` 唯一约束和 `(api_key_id, priority)` 唯一约束。分组被停用或删除时，路由记录 MAY 保留用于审计，但运行时 MUST 将其排除；若已无候选，系统 MUST 明确报错而不是退化为不受限的全局账号选择。

### 6.3 路由尝试事实与写放大控制

系统 SHALL 新增 `routing_attempts` 事实表或语义等价的事件存储，至少记录：

- `event_id`、`routing_decision_id`、`request_id`、`api_key_id`、`route_version`。
- `initial_group_id`、`attempted_group_id`、`effective_group_id`。
- `schedule_mode`、`smart_preference`、`attempt_index`。
- `platform`、`model_family`、`endpoint_kind`。
- 决策时的完整配置候选 ID 集合、硬过滤后的候选集合、每个候选的排除原因、特征值、置信度、各维度得分、最终分数、动态排名和选择原因。
- `strategy_version`、`score_version`、`feature_schema_version`、可空的 `model_version` 和 `experiment_id`。
- 决策所处的粘性、熔断、恢复、容量和依赖 guard 状态；不得只保存请求结束后的状态。
- 若命中实验、探索或非全量采样，记录稳定分桶、`sample_probability`、被选动作的 `action_propensity` 和分配原因。
- 价格窗口、缓存命中率、输入输出结构、归一化有效倍率和价格置信度。
- 分组访问结果、标准化失败类别、是否可重试。
- 排队时间、TTFT、完成耗时、是否输出语义数据。
- 是否发生分组切换、熔断状态变化和缓存补偿，以及最终真实成本、用户计费成本和最终成功概率评估。

候选特征 MUST 使用固定 schema 和有界列/数组表达，最多保存当前 Key 允许的 8 个候选；不得把任意模型返回、完整配置对象或无界调试 JSON 直接写入事实表。特征值必须是“决策发生时”的快照，后续价格、健康或模型版本变化不得反向改写历史事实。

系统 MUST 明确区分：

- `observed outcome`：实际被尝试分组产生的可观测结果。
- `unobserved counterfactual`：未被尝试候选的未知结果。

未选择候选可以保存当时的特征和预测，但 MUST NOT 伪造成功、价格或延迟标签。离线训练和评估必须能识别采样概率与动作概率，避免把现有策略选择偏差当成真实优劣。

正常的“第一候选一次成功”请求已经由使用记录保存最终实际分组，MUST NOT 再为每次成功同步插入一行完整 `routing_attempts`。持久化策略为：

- 发生分组切换、全部候选失败、熔断状态转换、半开探测、部分输出或供应商已产生成本的异常链路，写入完整路由尝试事实。
- 普通成功决定先进入内存聚合指标；智能模式的新会话选择 SHOULD 按稳定哈希和已知 `sample_probability` 异步保存有界候选快照，为离线回放和训练提供无时间漂移的行为基线。
- 命中策略实验、受控探索、分组切换、全部失败、熔断转换或部分计费的决定 MUST 保存完整候选上下文；实验与探索事件的保留期限不得短于对应评估窗口。
- 实验和探索只有在事件采集覆盖率、队列容量和消费者延迟处于健康状态时才能分配新会话；采集链路异常时 MUST 暂停新分配并回到 baseline，不得为了保全实验数据阻塞模型响应。
- 异常路由事件 SHOULD 先追加到有界 Redis Stream，再由后台消费者批量写入 PostgreSQL；Redis 不可用时可以进入有界进程内降级队列。
- 用户账单、供应商真实成本和最终使用记录属于关键事实，MUST 继续走现有可靠持久化路径，不能因诊断事件队列满而丢失。
- 诊断队列达到上限时 MUST 优先保留全部失败、部分计费和熔断转换事件，丢弃普通采样事件并增加 dropped 指标，MUST NOT 阻塞模型响应。

事件 MUST 有保留期限、索引和脱敏规则，不得保存 API Key 明文、账号凭据或完整请求正文。

### 6.4 使用记录扩展

现有使用记录的 `group_id` MUST 继续表示实际分组。使用记录 SHOULD 新增：

- `initial_group_id`：本次请求首先尝试的配置分组。
- `route_version`：做出本次决策的路由版本。
- `schedule_mode` 和 `smart_preference`。
- `group_switch_count`。
- `routing_decision_id`：与路由尝试链关联。
- `cache_cold_due_to_failover`。
- 供应商真实 token 字段与用户计费 token 字段，二者不得互相覆盖。
- `cache_compensation_tokens` 和补偿原因。

### 6.5 存储职责与性能架构

#### 6.5.1 总体原则

系统 SHALL 按以下原则选择存储：

- PostgreSQL 保存需要跨重启存在、需要事务一致性、需要财务核对或长期审计的事实。
- Redis 保存跨实例共享但有自然时效、允许通过后续流量重建的运行状态。
- 进程内存保存不可变只读快照、L1 缓存和可丢弃的短周期聚合数据；它不得成为粘性、熔断、权限或财务事实的唯一来源。
- 请求热路径只做有界缓存读取、Redis 原子操作和最多 `max_groups_per_key` 个候选的内存计算，MUST NOT 查询历史明细或执行在线聚合 SQL。
- 不得为每个用户或每个 API Key 预生成一份全量平台评分快照；共享指标按物理分组、模型族和端点生成，用户专属倍率只在最多 8 个候选上本地计算。

#### 6.5.2 数据归属矩阵

| 数据 | 权威存储 | Redis 用途 | 进程内用途 | 生命周期与一致性 |
|---|---|---|---|---|
| API Key、调度模式、评分策略、`route_version` | PostgreSQL | L2 鉴权/路由快照 | Ristretto L1 快照 | 长期事实；事务更新 |
| `api_key_group_routes` 有序集合 | PostgreSQL | 随鉴权快照整体缓存 | 随 API Key 快照物化 | 长期事实；不拆成每组一次查询 |
| 策略权重、成功率阈值、窗口参数 | PostgreSQL/settings | 版本化配置快照与失效通知 | 不可变配置对象 | 管理员修改后版本递增 |
| 用户分组权限、分组能力、当前价格版本 | PostgreSQL | 随鉴权/分组快照缓存 | 当前依赖版本和价格投影 | 权限变更后主动失效 |
| 缓存失效 outbox | PostgreSQL | Worker 投递删除与 Pub/Sub | 不保存权威状态 | 与配置同事务写入；投递成功后清理 |
| 最终使用记录、实际分组、真实成本、用户扣费 | PostgreSQL | 仅复用现有额度/计费缓存 | 当前请求计费上下文 | 财务关键事实，不可丢弃 |
| 原始可计费 token 结构 | PostgreSQL `usage_logs` | 不保存逐请求长期副本 | 请求结束前临时对象 | `actual_usage` 与 `billable_usage` 分离 |
| 路由 1m 健康/价格桶及监控聚合桶 | PostgreSQL；路由桶独立于监控开关 | MAY 缓存最近桶 | 构建评分时读取 | 可从使用事实重建；有保留期 |
| 全局分组评分输入与排序快照 | 非持久派生数据 | 版本化快照与当前版本指针 | 原子只读快照 | TTL 至少覆盖 3 个刷新周期；可重建 |
| Key 路由粘性 | 无需 PostgreSQL | Redis TTL 键 | MAY 做极短只读缓存 | 默认滑动 1 小时；丢失后可重建 |
| Key/分组路由熔断状态和滚动计数 | 无需 PostgreSQL | Redis Hash/Lua 权威状态 | 仅用于减少重复读取的短缓存 | TTL 覆盖最大冷却和恢复窗 |
| 半开探测租约 | 无需 PostgreSQL | Redis `SET NX`/Lua | 不得只放内存 | 秒级 TTL；自动释放 |
| 账号、用户、Key 并发槽位 | 复用现有设计 | Redis 原子脚本 | 批量读缓存 | 由请求租约和过期清理控制 |
| 异常路由尝试链 | PostgreSQL | Redis Stream 作为短期缓冲 | 有界降级队列 | 批量落库；普通成功仅采样 |
| 训练/回放用决策样本 | PostgreSQL 分区事实表或分析存储 | Redis Stream 仅作短期缓冲 | 稳定哈希采样和有界编码 | 保存候选时点快照、采样概率和结果；按期限归档 |
| 策略、特征与实验版本元数据 | PostgreSQL | 当前激活版本指针和失效通知 | 不可变版本对象 | 长期审计事实；状态转换受权限控制 |
| 未来预测模型元数据 | PostgreSQL 模型注册表 | 当前模型版本指针 | 校验后加载的推理对象 | 保存 checksum、特征契约和指标，不在 Redis 保存大制品 |
| 未来模型制品 | 版本化对象/制品存储 | 不保存完整制品 | 本地缓存已验证制品 | 内容寻址、只读、可回滚；不得作为路由配置权威源 |
| Prometheus 路由指标 | 时序监控系统 | 不保存高基数明细 | 无锁/分片计数器 | 不得以 API Key ID 作为常规标签 |

#### 6.5.3 API Key 路由快照

现有 API Key 鉴权缓存的 L1/L2 结构 SHALL 扩展为一个完整且版本化的 `APIKeyRoutingSnapshot`，至少包含：

- Key 和用户鉴权所需最小字段。
- `schedule_mode`、`smart_preference`、`route_version`。
- 按 `priority` 排序的候选分组 ID。
- 每个候选的同平台校验结果、计费类型、模型/端点能力、访问限制和价格配置引用。
- 用户对各候选的专属倍率或其版本引用。
- 鉴权快照 schema version 和依赖版本。

一个 Key 的路由集合 MUST 作为单个快照整体读取，不能在请求中对每个候选执行一次 PostgreSQL 或 Redis 查询。L1 未命中时读取一次 Redis L2；L2 未命中时使用 singleflight 合并同 Key 的 PostgreSQL 回源，并在一次有界查询中预加载路由和候选分组投影，避免 N+1。

L1 和 L2 TTL MUST 带抖动以避免同时失效。无效 Key 的负缓存继续独立设置较短 TTL，不得把临时 PostgreSQL/Redis 错误写成长期 NotFound。

#### 6.5.4 全局指标与评分快照

系统 SHALL 复用现有使用事实和分钟桶统计口径，在 PostgreSQL 保存有保留期的路由健康/价格桶。路由聚合 MUST 独立于 Channel Monitor 的启停和 V1/V2 模式，不在网关请求内维护长期历史：

1. 网关完成请求后，把最终真实用量和结果写入现有使用事实。
2. 单例 `RoutingScoreBuilder` 用独立后台连接池按有界窗口和回填游标生成路由桶；每轮修复最近 10 个完整分钟并回填最多 30 分钟历史块，回填边界为 24h。重算 MUST 事务化且幂等，游标只在提交后推进；接管可重新开始回填。
3. Builder 每 60 秒读取 1h/24h 聚合桶与当前价格配置，生成包含 `score_version`、`strategy_version`、`feature_schema_version` 和可空 `model_version` 的版本化 `RoutingMetricSnapshot`。迟到事实通过滚动修复和历史循环重算纳入；冷启动尚未补齐时按样本置信度回退，不伪造完整历史。
4. Builder 先把完整快照写入新的 Redis 版本键，再原子切换 `current_version` 指针并发布更新通知。
5. 各网关实例收到通知后加载一次快照、完成校验，然后通过 atomic pointer 或等价机制整体替换进程内只读对象。
6. 请求只读取本地快照，不逐请求访问 PostgreSQL，也不逐候选读取 Redis 评分。

Builder MUST 使用数据库 advisory lock、租约或等价单例机制，避免每个实例重复扫描聚合桶。备用实例必须能在领导者失效后接管。聚合查询 MUST 使用匹配时间范围的索引和有界游标，不能周期性扫描完整 `usage_logs`。每块重算 MUST 使用 15 秒查询期限、整轮 45 秒期限；7 天保留期清理每表每轮最多 5,000 行。基础失败与容量事实消费 MUST NOT 因持续优化开关关闭而停用。

快照 MUST 拆成两层，避免用户/Key 维度爆炸：

- 共享层：成功率、缓存 token 结构、延迟、错误和基础价格特征，按物理分组、模型族、端点聚合。
- 请求层：只对当前 Key 最多 8 个候选叠加用户专属倍率、实时容量和当前熔断状态，再计算最终分数和动态排序。

价格或策略更新时可以复用窗口 token 结构，只按当前价格重新计算，不需要等待新用量或重新扫描原始明细。

#### 6.5.5 Redis 运行状态设计

Redis 键 MUST 有命名空间、版本和 TTL。需要在一个 Lua 脚本中原子读写的键 MUST 使用相同 Redis Cluster hash tag。建议语义如下：

```text
{route:<api_key_id>:v<route_version>}:sticky:<model_family>:<endpoint_kind>:<session_hash>
{route:<api_key_id>:v<route_version>}:breaker:<group_id>:<model_family>:<endpoint_kind>
{route:<api_key_id>:v<route_version>}:probe:<group_id>:<model_family>:<endpoint_kind>

route:score:<snapshot_version>:<platform>:<model_family>:<endpoint_kind>
route:score:current:<platform>:<model_family>:<endpoint_kind>
route:strategy:current:<platform>:<model_family>:<endpoint_kind>
route:model:current:<platform>:<model_family>:<endpoint_kind>
route:dep:group:<group_id>
route:dep:user:<user_id>
route:events
```

`session_hash` MUST 是不可逆摘要，键中不得出现 API Key 明文、提示词或供应商会话原文。Key 级熔断 Hash SHOULD 紧凑保存当前桶成功/失败数、状态、打开时间、冷却截止、探测 owner 和最后更新时间；不得为每个成功请求保存一个 Redis List/ZSet 成员。

每次分组访问结束后，成功率计数和状态转换 MUST 通过单个 Lua 脚本或等价原子操作完成。脚本只更新固定数量的时间桶和状态字段，并为整个键续期。当前 `route_version` 已经进入键名，因此旧请求完成时不能覆盖新版本粘性或熔断状态。

所有临时键 MUST 设置 TTL：

- 粘性键使用会话滑动 TTL。
- 熔断键 TTL 至少为最大冷却期加恢复观察期和安全余量。
- 探测租约使用短 TTL，并通过 compare-and-delete 释放。
- 评分快照 TTL 至少为刷新周期的 3 倍；新版本生效后旧版本延迟删除，允许在途请求完成。
- 策略与模型当前版本指针只引用已经完整发布并校验通过的不可变版本；指针更新必须原子，旧版本至少保留至灰度和回滚窗口结束。
- Redis Stream 使用 `MAXLEN ~` 或等价上限与消费者水位监控，不能无限增长。

#### 6.5.6 请求热路径预算

在 L1 鉴权和评分本地快照命中时，新增路由逻辑的目标预算为：

| 阶段 | PostgreSQL | 新增 Redis 往返 | CPU 复杂度 |
|---|---:|---:|---:|
| 读取 Key 路由计划 | 0 | 0 | O(1) |
| 读取粘性和最多 8 个熔断状态 | 0 | 最多 1 次 Pipeline/Lua | O(N) |
| 计算用户价格、实时容量修正和排序 | 0 | 复用现有容量批量读取 | O(N log N)，N ≤ 8 |
| 当前分组正常成功后的状态更新 | 0 | 最多 1 次 Pipeline/Lua | O(1) |
| 每多失败一个物理分组 | 0 | 最多增加 1 次原子状态更新 | O(1) |
| 最终使用与账单 | 复用现有可靠路径 | 复用现有额度缓存 | 不新增调度 SQL |

新增路由部分的目标 P95 处理开销 SHOULD 不超过 5ms，但最终阈值以测试环境和生产基线为准。实现 MUST 暴露分阶段耗时，不能只记录总请求时长。

获取粘性与熔断状态时 MUST 批量读取全部候选，不得执行 N 次串行 Redis 请求。智能排序只对当前 Key 的小集合执行，不得扫描平台所有分组。

#### 6.5.7 指标和事件写入

普通路由成功指标 SHOULD 先累加到进程内分片计数器或有界无锁队列，每 1 秒或达到批量阈值后统一刷新；禁止每请求同步写 PostgreSQL 指标表。聚合维度必须受控，模型名需要规范化为模型族，错误使用固定 taxonomy。

异常路由事件的推荐链路为：

```text
request → bounded local encoder → Redis Stream → batch consumer → PostgreSQL routing_attempts
```

- XADD/入队只在发生切组、全部失败、熔断转换、半开探测或采样命中时执行。
- 消费者按数量或时间批量 INSERT/UPSERT，建议初始值为 100 至 1000 条或最长 1 秒。
- 事件使用稳定 `event_id`，数据库建立唯一约束，使重投递保持幂等。
- 消费者成功提交 PostgreSQL 后再 ACK；pending 超时可由其他消费者认领。
- 队列积压时先关闭普通成功采样，再丢弃非关键解释字段；失败链、部分计费和财务事实优先保留。
- Prometheus 指标不得包含 `api_key_id`、`request_id`、`session_hash` 等无界标签；这些值只进入有保留期的诊断事件。

#### 6.5.8 配置事务与缓存失效

创建或更新多分组 Key MUST 在一个 PostgreSQL 事务中完成：锁定 API Key、校验 `expected_route_version`、替换路由集合、更新策略、递增 `route_version`、同步兼容 `group_id`，并写入唯一的缓存失效 outbox 事件，然后提交。任何一步失败都必须回滚。

`routing_cache_outbox` 或等价表至少包含 `event_id`、`entity_type`、`entity_id`、`old_version`、`new_version`、`action`、`attempts`、`next_attempt_at`、`delivered_at`、`created_at` 和有界 JSON payload。`event_id` MUST 唯一；payload 只保存失效所需 ID 和版本，不得复制完整 API Key 快照或密钥明文。API Key 更新、分组禁用/价格变化和用户访问权限变化 SHOULD 与各自权威数据在同一数据库事务内写出对应 outbox 事件。

事务提交后按以下顺序处理缓存：

1. 删除该 Key 的 Redis L2 鉴权快照。
2. 发布包含 API Key ID、旧/新 `route_version` 和原因的失效消息。
3. 所有实例删除对应 L1 正缓存和负缓存。
4. 新请求回源构建新版本快照；旧在途请求只允许写入旧版本 Redis 命名空间。

提交后的同步失效失败时，outbox Worker MUST 按幂等事件 ID 重试 Redis 删除和 Pub/Sub，直到成功或进入可告警的死信状态。Redis Pub/Sub 仅用于加速通知，不能作为可靠事实源。

路由快照 MUST 携带候选分组依赖版本和用户访问版本。Redis SHOULD 保存轻量的 `group_runtime_version/status` 与 `user_access_version`；请求读取粘性和熔断状态的同一个 Pipeline 中可以批量读取这些 guard。发现版本不一致、禁用或撤权时，系统 MUST 排除候选并异步刷新快照，不能继续使用旧权限。

TTL、快照 schema version、`route_version`、依赖 guard 和 outbox MUST 共同保证漏掉通知后仍能收敛。分组禁用、权限撤销等安全敏感变化 MUST 主动写 guard、失效相关 L2 快照并发布依赖版本；不得只等待普通 TTL。

#### 6.5.9 故障降级

- 评分快照过期但路由配置仍有效时，智能调度退化为用户配置顺序。
- Redis 评分键不可用但本地快照仍在允许的 stale grace 内时，可以继续使用最后已知评分；超过 grace 后必须顺序退化。
- Redis 粘性/熔断不可用时，本次请求仍可在用户候选内完成分组间失败重试，但 MUST 标记 degraded；不得把进程内状态宣称为跨实例一致。
- Redis 恢复后粘性和熔断可由新流量重建，不需要从 PostgreSQL 恢复每个临时键。
- PostgreSQL 暂时不可用时，仍在有效期内的 L1/L2 鉴权与路由快照 MAY 继续服务；配置写操作必须失败，财务用量持久化继续遵循现有 fail-safe 策略。
- 诊断事件队列不可用不得阻塞模型响应，但关键账单和供应商成本不得随诊断事件一起丢弃。

#### 6.5.10 容量与基数上限

- 每 Key 候选分组默认最多 8 个，限制单次快照大小、Redis 批量读取和排序成本。
- 粘性只为实际出现且能生成稳定 `session_hash` 的活跃会话创建，禁止为无会话请求生成长期随机键。
- 熔断状态按 Key 仅在有实际流量的候选范围创建，并由 TTL 自动回收。
- 全局评分只按受控平台、规范化模型族和有限端点枚举生成；原始任意模型字符串不得直接成为长期 Redis/Prometheus 维度。
- 路由事件、Redis Stream、PostgreSQL 尝试表和聚合桶都必须定义保留策略及容量告警。
- 管理列表必须批量加载唯一分组指标，禁止按每个 API Key、每个候选执行独立查询。

#### 6.5.11 PostgreSQL 索引、分区与资源隔离

PostgreSQL 至少需要以下访问路径：

- `api_key_group_routes(api_key_id, priority)` 唯一索引，支持一次读取有序路由。
- `api_key_group_routes(group_id, api_key_id)` 索引，支持分组禁用、删除和反向失效。
- `routing_attempts(event_id)` 唯一索引，保证异步重投幂等。
- `routing_attempts(request_id)` 索引，支持单请求诊断。
- `routing_attempts(api_key_id, created_at DESC)` 与 `(effective_group_id, created_at DESC)`，只在实际查询需要时建立。
- Channel Monitor 聚合桶使用 `(group_id, model_family, endpoint_kind, bucket_start)` 或与查询前缀匹配的等价索引。
- outbox 使用 `(delivered_at, next_attempt_at)` 部分索引，只扫描待投递记录。

`routing_attempts` 与高频聚合桶 SHOULD 按时间分区或采用等价归档机制，删除过期数据时不得对在线大表执行长事务全表删除。大 JSON 解释字段不应建立通用 GIN 索引，常用过滤条件必须拆成定长列。

评分 Builder、历史归档和事件消费者 MUST 使用独立超时、批量大小与并发上限，不能占满供鉴权、计费和用户请求使用的数据库连接池。慢聚合查询必须可以取消，并暴露查询时长、扫描行数和失败指标。

## 7. 请求路由总流程

每个请求 MUST 按以下顺序处理：

1. 鉴权 API Key，并读取同一版本的 Key、用户、路由集合和必要分组投影。
2. 校验 Key 级状态、有效期、IP 白名单、Key 级限额与用户状态。
3. 确定 `platform`、`endpoint_kind`、`model_family` 和会话标识。
4. 对配置分组执行运行时硬过滤，得到候选集合。
5. 若存在有效会话粘性且其分组仍可用，优先选择粘性分组。
6. 否则按顺序模式或智能模式选择初始分组。
7. 进入该分组既有的账号调度和账号故障转移循环。
8. 仅当本次分组访问确认失败且错误允许跨分组重试时，更新路由熔断状态并选择下一个配置分组。
9. 成功后把实际分组写入会话粘性、使用记录、计费上下文和路由尝试链。
10. 全部候选均无法服务时，返回稳定、可观测但不泄密的错误。

请求热路径 MUST 使用预计算健康快照、Redis 状态和内存投影，不得为每个请求执行 PostgreSQL 历史聚合查询。

## 8. 候选分组硬过滤

一个配置分组只有同时满足以下条件才能参与本次调度：

- 分组存在、已启用且未被软删除。
- 分组平台与路由集合平台一致。
- 分组计费类型与路由集合一致。
- 当前用户仍拥有绑定和使用该分组的权限，包含专属、受限或白名单规则。
- 分组支持请求模型、模型映射、协议和端点能力。
- 满足 Claude Code、OAuth、隐私模式、图像、视频、批处理、实时或其他入口限制。
- 当前实际分组对应的用户自定义倍率、订阅资格和利润控制允许提供服务。
- 路由熔断器不处于 `OPEN`；`HALF_OPEN` 时当前请求获得探测租约。
- 分组有可用账号或可预期的容量；容量暂满可以被排除，但不得直接标记为健康故障。

创建时的校验不能替代运行时校验。分组状态、权限、订阅、模型支持或价格配置变化后，下一请求 MUST 使用最新有效投影重新过滤。

## 9. 顺序调度

顺序调度遵循以下规则：

1. 新会话从 `priority` 数值最小、即用户排列最靠前的可用分组开始。
2. 有效粘性会话继续使用已绑定分组，即使更高优先级分组已经恢复。
3. 在一个分组内，先完成现有账号选择、账号级重试和账号级故障转移。
4. 只有当整个分组访问结果属于允许跨分组重试的类别时，才进入下一配置分组。
5. 分组只是容量暂满时，可以溢出到下一分组，但该事件不增加健康失败计数。
6. 所有后续分组都按用户配置顺序尝试，绝不重排，也绝不越过路由集合选择其他分组。
7. 首选分组恢复后，只让没有有效粘性的会话优先回到首选分组；已有备用粘性自然排水。

## 10. 智能调度

### 10.1 成功率是硬基线

智能调度 MUST 使用“先准入、后优化”的两阶段决策：

1. **可靠性准入**：根据成功率置信值、当前熔断状态、严重错误和实时容量排除不可靠候选。
2. **策略排序**：仅在通过准入的候选中，使用同一组评分维度和策略对应的不同权重计算总分，并按总分动态排序。

成功率 MUST 作为乘法可靠性因子和硬门槛，MUST NOT 只作为可被低价或低延迟抵消的普通加分项。

系统 MUST 区分两条成功率线：

- **硬熔断线**：分组访问成功率 `< 0.50` 时立即进入 `OPEN`。该阈值同时适用于顺序和智能调度，不参与权重计算，也不可被其他维度抵消。
- **智能准入线**：智能模式可以按平台和模型族配置高于或等于 0.50 的健康准入阈值；低于智能准入线但尚未低于硬熔断线的分组不会进入正常智能排序，但不一定触发硬熔断。

恰好 `0.50` 不满足“低于 50%”条件，因此不会仅由硬熔断线触发 `OPEN`，但仍受智能准入线、连续严重错误和其他熔断规则约束。

### 10.2 成功率口径

系统 MUST 同时保留账号尝试结果和分组访问结果。智能调度主要使用“分组访问成功率”：请求进入某分组后，只要该分组内部最终有一个账号按端点成功条件完成服务，本次分组访问即为成功；只有分组内所有可用账号均无法服务，才记为该分组访问失败。

成功率维度至少包含：

```text
physical_group_id × model_family × endpoint_kind × time_window
```

统计 SHOULD 同时观察 5 分钟、1 小时和 24 小时窗口，并让近期数据拥有更高权重。实现 MUST 使用 Wilson 下置信界、Beta 后验下界或等价的置信度修正，不能把小样本的 `1/1 = 100%` 当作高可信健康状态。

硬熔断计算 MUST 使用当前路由熔断作用域内、最短非空有效窗口的原始分组访问成功率。Key 级路由熔断不要求额外最小样本量：由于单账号失败已经在分组内部消化，首次完整分组访问即失败时，其成功率为 0%，可以立即开路。影响全部 Key 的全局健康熔断仍 MUST 达到管理员配置的全局最小样本量，防止单个 Key 的一次异常扩大为全局隔离。

失败分类：

| 结果 | 进入成功率分母 | 视为分组失败 | 可跨分组重试 |
|---|---:|---:|---:|
| 正常完成的 2xx/协议成功 | 是 | 否 | 否 |
| 上游连接、DNS、TLS、超时 | 是 | 是 | 是，客户端取消除外 |
| 上游账号认证失败 401/403 | 是 | 分组内账号耗尽后是 | 是 |
| 上游限流 429/529 | 是 | 是，同时标记容量压力 | 是 |
| 上游 5xx | 是 | 是 | 是 |
| 明确识别为可重试的供应商 4xx | 是 | 是 | 是 |
| 请求格式、参数或上下文过长 | 否 | 否 | 否，除非明确证明为分组差异 |
| 内容安全或版权策略拒绝 | 否 | 否 | 否 |
| 用户余额、订阅、Key、IP 限制 | 否 | 否 | 否 |
| 客户端主动取消或断开 | 否 | 否 | 否 |
| 仅容量并发已满但分组健康 | 否 | 否 | 是，作为容量溢出 |

流式请求在已输出语义数据后异常，MUST 记录服务质量失败，但 MUST NOT 跨分组重新执行。

### 10.3 小样本与冷启动

- 样本不足的分组进入 `probation`，不能因表面 100% 成功率获得主流量。
- `probation` 分组 MUST 带有小样本惩罚，不能排在有充分健康证据且综合得分相近的分组之前；系统 MAY 使用不携带用户提示词的受控探测补充健康证据。
- 完全没有可靠数据时，智能调度 MUST 暂时退化为用户配置顺序，并继续采集结果。
- 冷启动健康探测 MUST 服从熔断状态和实时容量，不得为补充样本而把用户请求随机分配给已知故障分组。

### 10.4 评分模型

每个通过准入的候选生成以下归一化评分维度：

- `S(g)`：由置信度修正后的成功率得到的可靠性因子。
- `P(g)`：模型感知、用户感知且经过缓存与输入输出结构修正的价格因子；归一化有效倍率越低，值越高。
- `L(g)`：由 TTFT、P95 完成耗时和超时率得到的速度因子；越快越高。
- `C(g)`：实时容量余量因子，需考虑并发、RPM、会话限制和共享账号去重。

全部维度 MUST 归一到 `[0, 1]`，数值越大表示越适合优先尝试。成功率、速度和容量第一阶段建议采用以下确定性口径：

```text
S(g) = confidence_adjusted_success_rate(g)

L_stream(g) = 0.60 × min_ttft_p95 / ttft_p95(g)
              + 0.40 × min_duration_p95 / duration_p95(g)

L_nonstream(g) = 0.20 × min_first_byte_p95 / first_byte_p95(g)
                 + 0.80 × min_duration_p95 / duration_p95(g)

C(g) = min(1, effective_headroom(g) / target_headroom)
```

所有分母 MUST 设置非零下限，各比值 MUST 截断到 `[0, 1]`。延迟统计 SHOULD 使用经异常值截断的 P95；容量余量 MUST 综合并发、RPM、活跃会话和共享账号去重后的最紧约束。某个性能维度缺少足够样本时，系统 MUST 使用带小样本惩罚的保守默认值，不能把“未知”当作最优；该默认值和置信度必须进入评分解释。

#### 10.4.1 缓存修正的归一化有效倍率

价格维度 MUST 使用 token 加权的窗口整体统计，不能平均每次请求的缓存率或倍率。统计作用域至少为：

```text
physical_group_id × model_family × endpoint_kind × price_window
```

默认 `price_window` 为滚动 1 小时；窗口未达到最小请求数或最小逻辑输入 token 数时，依次回退到滚动 24 小时、同平台同模型族基线，最后才回退到名义倍率。所有回退都 MUST 降低价格置信度，未知数据不得被当作最低价格。

对窗口内成功且可正常计费的请求聚合：

```text
I(g)   = ordinary_uncached_input_tokens
W5(g)  = cache_creation_5m_tokens
W1(g)  = cache_creation_1h_tokens
W(g)   = other_cache_creation_tokens
R(g)   = cache_read_tokens
O(g)   = output_tokens

LogicalInput(g) = I(g) + W5(g) + W1(g) + W(g) + R(g)
CacheHitRate(g) = R(g) / LogicalInput(g)
```

`CacheHitRate(g)` 即窗口整体缓存命中率。它 MUST 由 token 总量相除得到，而不是对请求级命中率做算术平均。客户端错误、内容策略拒绝、取消请求、跨分组故障造成的首次冷缓存以及人工补偿 token MUST NOT 污染该分组的稳态缓存命中率。

`I/W5/W1/W/R/O` MUST 来自供应商真实返回或协议解析出的 `actual_usage`，不得从经过 `ForceCacheBilling`、冷缓存补偿或其他用户计费修正后的 `billable_usage` 聚合。计费修正可以影响用户最终扣费，但不能反向伪造分组的缓存能力和价格评分输入。

系统 MUST 使用窗口内的 token 结构和**当前生效价格**重新估价，而不是直接累加可能已经过期的历史账单。设当前用户在候选分组上的有效单价为：

```text
q_in(g), q_write_5m(g), q_write_1h(g), q_write(g),
q_cache_read(g), q_output(g)
```

这些价格 MUST 经过当前模型价格解析、渠道/分组价格、service tier、长上下文规则和用户专属倍率处理。当前预估成本为：

```text
CurrentExpectedCost(g) =
    I(g)  × q_in(g)
  + W5(g) × q_write_5m(g)
  + W1(g) × q_write_1h(g)
  + W(g)  × q_write(g)
  + R(g)  × q_cache_read(g)
  + O(g)  × q_output(g)
```

然后使用同平台、同模型、同 service tier 的 1.0x 标准参考价，把同样的逻辑输入全部视为缓存读取，同时保留真实输出量：

```text
FullCacheReferenceCost(g) =
    LogicalInput(g) × q_reference_cache_read
  + O(g)            × q_reference_output

NormalizedEffectiveRate(g) =
    CurrentExpectedCost(g) / FullCacheReferenceCost(g)
```

`q_reference_*` MUST 是所有候选共享的标准 1.0x 参考价格，不包含候选分组倍率或用户专属倍率。系统 MUST NOT 用每个候选自己的有效价格同时计算分子和分母，否则分组价格差异会在相除时被抵消，无法反映用户实际价格。

因此 `NormalizedEffectiveRate(g)` 同时体现：

- 分组及用户实际价格。
- 窗口整体缓存命中率。
- 缓存创建成本及 5 分钟/1 小时缓存价格差异。
- 输入与输出 token 的真实比例；输出不受输入缓存命中影响，所以在分子和基准分母中都保留。

价格评分再在本次合格候选中归一化：

```text
P(g) = 1 - (NormalizedEffectiveRate(g) - min_normalized_rate)
           / (max_normalized_rate - min_normalized_rate)
```

当全部候选的归一化有效倍率相同时，所有候选的 `P(g)` MUST 为 1。分母为 0、参考模型没有有效缓存读取价或无法获得可比较 token 结构时，系统 MUST 使用保守回退并标记低置信度，不得产生 `NaN`、无限值或把未知候选排到第一名。

按次、图片或其他不以文本 token 和缓存计价的模型不适用上述公式。它们 MUST 使用相同业务单位下的当前预计单位成本生成价格评分，并明确标记 `cache_adjustment=not_applicable`。

三种策略 MUST 复用同一评分函数，只改变权重向量。评分 MUST 满足：

```text
Score_policy(g) = ReliabilityGate(S(g))
                  × (w_success × S(g)
                     + w_price × P(g)
                     + w_speed × L(g)
                     + w_capacity × C(g))
```

其中 `ReliabilityGate` 在成功率低于智能准入门槛时必须返回 0，通过门槛后返回 1；若原始分组访问成功率低于 0.50，则必须先进入 `OPEN`，不得进入评分。`S(g)` 仍作为所有策略共有的评分项。各权重必须非负且总和为 1。容量已满可以做硬排除，容量余量只能作为有界评分项，MUST NOT 把“账号数量更多”静默变成不受约束的容量比例权重。

连续滑块的权重为硬契约。令 `s = smart_balance_bps / 10000`，各分量先按原有方法归一化并做置信度收缩，再计算：

```text
Score(g) = 0.50 * S(g) + 0.10 * C(g) + 0.40 * ((1 - s) * P(g) + s * L(g))
```

内部评分保持 0..1；若展示百分制则乘 100。用户选择价格 70% / 速度 30% 时，最终权重 MUST 为成功率 50%、容量 10%、价格 28%、速度 12%。active/canary/shadow 与离线重放均 MUST 应用决策时的用户比例，不得被持续优化覆盖。`P(g)` 继续使用窗口缓存命中率和输入/输出结构归一化的实际成本，不退化为名义倍率。

未设置数值比例的旧 Key 保留既有预设；编辑时的兼容映射及其基线如下：

| 策略 | `w_success` | `w_price` | `w_speed` | `w_capacity` |
|---|---:|---:|---:|---:|
| `price`（1250 bps） | 0.50 | 0.35 | 0.05 | 0.10 |
| `speed`（8750 bps） | 0.50 | 0.05 | 0.35 | 0.10 |
| `balanced`（5000 bps） | 0.50 | 0.20 | 0.20 | 0.10 |

这些权重只在可靠性准入之后生效。价格优先仅提高价格维度权重，速度优先仅提高速度维度权重，均衡策略让各业务维度更接近；它们不是三套不同的调度流程。系统 MUST 保存策略版本，使一次路由决定可以复现当时采用的阈值和权重。

### 10.5 动态自动排序

- 系统 MUST 按 `Score_policy(g)` 从高到低生成当次候选顺序，不得把得分直接解释为随机流量比例。
- 请求 MUST 首先尝试排序后的第一个分组；该分组容量不足或发生允许跨分组重试的失败时，再按动态排序结果依次尝试后续分组。
- 排序 SHOULD 在健康快照更新、实时容量显著变化、熔断状态变化或新会话首次选择时重新计算，因此最高优先分组会随运行状态动态变化。
- 单个稳定会话只在首次选择或当前粘性分组失效时参与动态排序，后续请求优先服从会话粘性。
- 用户配置顺序用作冷启动顺序、同分决胜和指标不可用时的回退顺序，不参与正常智能模式的主权重计算。
- 动态排序 MUST 是确定性的：相同路由版本、策略版本、指标快照和实时容量输入应产生相同顺序。
- 若多个配置分组引用同一底层账号池，容量计算 MUST 去重，避免重复计算可用容量。

## 11. 预估倍率

### 11.1 计算方式

预估倍率 MUST 是模型感知、用户感知并经过缓存与输入输出结构修正的。对给定模型：

```text
estimated_normalized_rate =
    Σ predicted_share(g) × NormalizedEffectiveRate(g, model, price_window)
```

- 智能模式中的 `predicted_share` MUST 根据动态排序结果、各排序位置的预计可用率、容量溢出概率、故障转移概率，以及同一路由版本和评分策略下的近期真实选中占比估算，MUST NOT 直接把评分归一化成随机分流比例。
- 顺序模式中的 `predicted_share` 主要由首选分组及其预计故障转移概率构成，不能简单平均所有分组倍率。
- `NormalizedEffectiveRate` MUST 使用窗口 token 结构和当前价格重算，并包含分组模型价格、渠道或 LiteLLM 回退价格、用户专属分组倍率和当前生效的计费规则。
- 名义倍率 SHOULD 与归一化预估倍率同时返回，避免用户把缓存修正后的指标误解为固定账单倍率。

### 11.2 展示要求

前端 MUST 明确显示：

- 缓存修正后的预估倍率或区间，以及名义倍率。
- 对应模型或模型族。
- 整体缓存命中率、逻辑输入量、输出量、统计时间窗、更新时间和置信等级。
- 当前策略与候选分组。
- “最终按实际路由分组计费”的提示。

当分组倍率发生变化时，系统 MUST 重新计算预估值并展示当前价格；第一阶段不会自动移除该分组，也不会冻结创建 Key 时的旧倍率。

## 12. 两层粘性

### 12.1 分组路由粘性

Redis 键语义：

```text
route_sticky:{api_key_id}:{route_version}:{model_family}:{endpoint_kind}:{session_hash}
```

值至少包含 `effective_group_id`、绑定时间、最后成功时间和切换原因。默认滑动 TTL 为 1 小时，并与现有账号粘性 TTL 保持一致；管理员可以按平台调整。

### 12.2 分组内账号粘性

选择实际分组后，继续使用现有语义：

```text
sticky_session:{group_id}:{session_hash}
```

因此完整选择顺序为：

```text
API Key 路由集合 → 实际物理分组 → 分组内实际账号
```

### 12.3 粘性适用规则

- 路由粘性只在对应分组仍通过硬过滤且熔断器允许时生效。
- 粘性分组失败后，可以切到其他配置分组，并原子更新路由粘性。
- 模型族或端点能力不同的请求 MUST 使用不同粘性范围。
- 修改分组列表、顺序、调度模式或智能评分策略后，`route_version` 递增，旧粘性自然失效。
- 无法提取稳定 `session_hash` 的无状态请求 MAY 每次重新调度；系统不得承诺其缓存连续性。

## 13. 路由熔断与恢复

### 13.1 作用域

路由熔断器至少按以下维度隔离：

```text
api_key_id × route_version × physical_group_id × model_family × endpoint_kind
```

此外，系统 MAY 维护不含 Key 维度的全局分组健康熔断，以快速隔离已经被多 Key 共同证明不可用的分组。全局状态不得绕过用户候选白名单。

### 13.2 状态机

```text
CLOSED ──失败阈值──> OPEN ──冷却到期──> HALF_OPEN
  ▲                                      │
  │                                      ├─失败──> OPEN
  │                                      └─成功──> RECOVERING
  └──────────稳定恢复与观察窗───────────────┘
```

- `CLOSED`：正常参与调度。
- `OPEN`：窗口期内所有该 Key 的新请求跳过该分组。
- `HALF_OPEN`：只允许持有 Redis 探测租约的少量请求进入，其他请求继续使用备用分组。
- `RECOVERING`：探活已经成功，但只逐步接收新会话；已有备用会话不迁移。

### 13.3 触发规则

- 单个账号失败后 MUST 先执行分组内故障转移。
- 每次有效分组访问完成后 MUST 更新成功率统计；只有分组访问失败、分组账号耗尽、成功率跌破硬熔断线或其他明确配置的严重错误，才使路由熔断状态向降级方向转换。
- 每次有效分组访问结束后 MUST 重新计算当前 Key 熔断作用域的近期成功率；达到配置最小样本量（默认 10）且低于该 Key 的 `routing_min_success_rate / 100` 时，MUST 不等待更多连续失败而立即进入 `OPEN`。
- 门槛最低为 50%，同时适用于顺序和智能调度；恰好等于用户门槛不因本规则直接开路。提高门槛只影响该 Key，不改变其他 Key 或全局健康状态。
- Key 和共享分组健康都保留最小样本量防护；无有效样本为未知而非失败。所有已知不达标候选都被排除时不得自动降低用户门槛。严格门槛（大于 50%）下 Redis 状态不可读则拒绝准入，不能静默降为无健康检查的顺序路由。
- 明确的全组不可用、全部凭据失效或账号池耗尽 MAY 立即短期开路。
- 仅容量暂满可以触发溢出和短退避，但 MUST NOT 进入健康熔断统计。
- 错误范围是模型相关时，MUST 只熔断对应模型族或端点，不能无条件熔断整个物理分组。

### 13.4 多实例一致性

- 状态转换、失败计数和探测租约 MUST 存储在 Redis 或等价共享存储中。
- `HALF_OPEN` 探测 MUST 使用 `SET NX` 租约、Lua 或等价原子机制，防止多个实例同时放入探测洪峰。
- 熔断记录 MUST 带 `route_version`；配置更新后旧版本状态不得影响新配置。
- Redis 暂时不可用时，系统 MUST 退化为当前路由集合的顺序调度，不得扩大候选范围。

## 14. 回切与缓存连续性

### 14.1 排水式回切

分组探活成功只证明它能够处理探测请求，不证明某个用户会话的上游提示词缓存已经存在。因此系统 MUST 使用排水式回切：

- `RECOVERING` 分组先接收少量无粘性的新会话。
- 仍粘在备用分组且持续活跃的会话继续留在备用分组。
- 备用会话在滑动 TTL 到期、主动结束或再次失败后，才按当前策略重新选择。
- 系统 MUST NOT 在首选分组恢复时批量删除备用粘性或强制搬迁活跃会话。
- 恢复观察窗内再次失败时，立即回到 `OPEN` 并延长有界冷却时间。

### 14.2 故障转移冷缓存补偿

系统无法保证供应商缓存跨分组或账号迁移。对于满足全部条件的首次冷启动请求，平台 SHOULD 提供有界补偿：

- 会话此前存在成功的路由粘性。
- 分组切换由系统故障、熔断或容量溢出触发，而不是用户修改配置或自然开启新会话。
- 请求仍属于同一 `route_version`、模型族和端点类型。
- 供应商实际用量表明原本可能命中缓存的输入被按普通输入计费。

补偿 MUST 受单次切换次数、时间窗和 token 上限约束，避免重复补偿或滥用。恢复后的排水式回切不应产生第二次强制冷启动。

供应商真实用量、真实成本和性能统计 MUST 保留原值；系统只能单独生成 `billable_input_tokens`、`cache_compensation_tokens` 或等价计费字段，MUST NOT 通过把真实 input tokens 原地改写为 cache-read tokens 来伪造实际用量。

## 15. 跨分组重试安全

### 15.1 流式响应

- 尚未输出 HTTP 响应头或语义数据时，可以按错误分类跨分组重试。
- 只输出 SSE 注释或心跳时，只有现有协议层能够安全重置流，才 MAY 重试。
- 一旦输出首个语义 token、工具调用、媒体信息或其他业务数据，MUST 禁止跨分组重试。
- 禁止重试时仍需记录实际分组、失败类别和可能的部分计费用量。

### 15.2 有副作用与异步端点

图像、视频、批处理、实时会话、WebSocket 或供应商异步任务可能在返回错误前已经被接受。系统只有满足以下任一条件时才能跨分组重试：

- 已明确确认上游未接受请求。
- 上游或系统提供端到端幂等键，并能证明重复提交不会创建重复任务或重复扣费。

否则 MUST 返回原错误并保留可关联的供应商任务标识，不能为了提高表面成功率而重复创建资源。

`previous_response_id`、供应商会话 ID 或账号绑定资源通常不能跨账号或分组使用。包含这些标识的请求 MUST 固定到原实际分组，除非协议适配器能确定性重建上下文。

## 16. 计费、配额与利润控制

- API Key 级 RPM、并发、总额度和过期校验在路由前执行一次。
- 分组级并发、RPM、订阅权益、模型倍率和利润控制在选择每个候选实际分组时执行。
- 同一次跨分组重试不得重复扣减用户 Key 级请求次数；供应商已产生的真实成本仍需审计记录。
- 标准计费请求按实际分组的模型定价和用户专属倍率结算。
- 订阅计费请求按实际分组对应的有效订阅和额度结算。创建或更新多分组订阅 Key 时，用户 MUST 对全部候选分组拥有有效资格；运行时资格失效的分组被排除。
- 若成功响应来自备用分组，使用记录、消费记录和利润控制 MUST 全部引用备用实际分组，而不是首选分组。
- 因失败尝试未产生用户可用结果时，不创建伪成功使用记录；若供应商对失败或部分输出产生可计费量，必须单独记录真实成本。

## 17. 健康快照与性能数据

系统 SHALL 周期性预计算 `group × model_family × endpoint_kind` 的调度快照，建议每 30 至 60 秒更新，并包含：

- 成功率点估计、置信下界、样本量和窗口。
- TTFT、完成耗时的 P50/P90/P95 和超时率。
- 429、529、5xx、连接错误和账号耗尽分类。
- 当前并发、会话、RPM 及归一化容量余量。
- 普通输入、缓存创建 5m/1h、其他缓存创建、缓存读取和输出 token 总量。
- token 加权的整体缓存命中率、价格窗口、样本量和价格置信度。
- 当前模型有效价格、用户倍率和标准 1.0x 参考价格的解析版本。
- 以输入 100% 缓存命中为基准的归一化有效倍率及其价格评分。
- 快照生成时间、`strategy_version`、`score_version`、`feature_schema_version`、可空 `model_version` 和数据新鲜度。
- 若启用预测模型，保存成功概率、延迟分位数、容量溢出、缓存和成本预测及各自置信度；原始聚合指标仍必须保留用于确定性回退。

请求路径可以叠加比快照更新更快的实时容量计数。快照过期或不可用时：

- 顺序模式继续按配置顺序工作。
- 智能模式退化为带实时硬过滤的配置顺序。
- 系统 MUST 输出降级指标，但不得自动加入其他分组。

## 18. 用户界面

创建和编辑 API Key 的界面 SHALL：

- 先确定平台，再只展示该平台且用户有权使用的分组。
- 支持多选、删除和拖拽排序。
- 显示每个分组的名称、名义倍率、缓存修正后的归一化有效倍率、整体缓存命中率、成功率状态、延迟和容量状态。
- 提供“顺序调度”和“智能调度”选项。
- 仅在智能调度下展示“价格优先”“速度优先”“均衡”评分策略。
- 明确解释三种策略共用相同评分维度，只是权重不同，且成功率始终是共同的前置门槛。
- 在智能模式下展示当前动态排序、总分和成功率/价格/速度/容量评分拆解；用户拖拽顺序仅作为冷启动、同分和降级回退顺序。
- 显示模型相关的缓存修正预估倍率、区间、价格窗口、输入输出样本量、置信度和更新时间。
- 明确说明最终按实际服务分组计费，且使用记录显示实际分组。

使用记录详情 SHALL 显示实际分组；发生转移时 SHOULD 同时显示首个尝试分组、切换次数、调度策略和“发生故障转移”标记，但不得暴露账号或内部凭据。

## 19. 权限与安全

- 服务端 MUST 在创建、更新和每次请求时校验用户对全部相关分组的权限。
- 用户失去某分组权限后，该分组 MUST 立即从运行时候选中排除，并触发相关缓存失效。
- 路由集合为空时，系统 MUST 返回 `API_KEY_NO_AVAILABLE_GROUP` 或等价稳定错误，不能使用无分组约束的账号。
- 日志、指标和事件 MUST 使用 API Key ID，不得记录 API Key 明文。
- 用户只能看到自己选择且有权访问的分组信息；账号、代理、上游凭据和管理员备注不得透出。
- 更新路由集合 MUST 写入管理审计，包含操作者、旧/新分组 ID 顺序、旧/新策略和路由版本，不包含密钥明文。

## 20. 可观测性

至少提供以下指标：

- `gateway_route_decisions_total{mode,preference,group,result}`。
- `gateway_route_candidate_exclusions_total{reason,group}`。
- `gateway_group_switches_total{from_group,to_group,reason}`。
- `gateway_route_breaker_transitions_total{from,to,group,scope}`。
- `gateway_route_half_open_probes_total{group,result}`。
- `gateway_route_sticky_hits_total{group,result}`。
- `gateway_route_score_snapshot_age_seconds`。
- `gateway_estimated_rate_error`，用于比较预估倍率和实际倍率。
- `gateway_route_cache_hit_rate{group,model_family,window}`。
- `gateway_route_normalized_effective_rate{group,model_family,window}`。
- `gateway_route_price_score_fallback_total{reason,group}`。
- `gateway_cold_cache_compensation_tokens_total{reason,group}`。
- `gateway_route_strategy_decisions_total{strategy_version,stage,preference,result}`。
- `gateway_route_shadow_disagreements_total{baseline_version,candidate_version,preference}`。
- `gateway_route_canary_guardrail_status{strategy_version,guardrail}`。
- `gateway_route_policy_rollbacks_total{from_version,to_version,reason}`。
- `gateway_route_rank_flips_total{platform,model_family,preference}`。
- `gateway_route_decision_events_total{kind,sampled,result}` 和 `gateway_route_decision_events_dropped_total{priority,reason}`。
- `gateway_route_model_inference_duration_seconds{model_version,result}`、`gateway_route_model_fallback_total{model_version,reason}`。
- `gateway_route_prediction_calibration_error{target,platform,model_family}` 和受控的特征缺失/drift 指标。

结构化日志 SHOULD 让管理员根据 `request_id` 还原候选过滤、评分、分组访问、账号访问、熔断和最终计费链路。管理诊断页 SHOULD 显示被排除分组及标准化原因。

告警至少覆盖：

- 某平台或模型没有任何可用候选。
- 智能快照持续过期。
- 分组切换率或开路率异常升高。
- `RECOVERING` 长时间无法回到 `CLOSED`。
- 预估倍率与实际倍率长期显著偏离。
- 冷缓存补偿量异常升高。
- shadow/canary 的最终成功率、成本、延迟、切换或冷缓存护栏恶化。
- 排名翻转率或策略回滚率异常升高。
- 决策样本覆盖率低于目标、采样概率缺失或实验事件丢失。
- 模型特征缺失、预测校准误差、数据漂移或本地推理 fallback 持续超限。

## 21. 策略生命周期与持续优化

智能调度 SHALL 被设计为可持续优化但受硬约束控制的决策系统。系统可以根据新增历史数据改进指标口径、策略权重和结果预测，但任何自动优化都不得直接修改用户候选集合、绕过可靠性准入、破坏会话粘性或改变实际分组计费归属。

### 21.1 不可学习和可学习的边界

以下规则属于不可被训练、自动调参或实验覆盖的硬边界：

- 候选只能来自用户明确选择的同平台路由集合。
- 权限、模型/端点能力、计费资格、容量硬限制和端点重试安全必须先通过硬过滤。
- 原始分组访问成功率低于 50% 时必须执行硬熔断；任何低价、低延迟或模型预测都不能重新放行。
- 已有健康粘性会话不因策略、评分或模型版本变化而迁移。
- 已输出语义流或可能已经产生副作用的请求不得被学习策略跨组重放。
- 最终使用记录、用户扣费、供应商成本和利润控制继续引用实际服务分组。

持续优化只允许改变：

- 通过硬过滤的新会话候选的动态顺序。
- 三种用户偏好内的有界评分权重、归一化参数、迟滞参数和置信度修正。
- 成功概率、TTFT、完成耗时、容量溢出、缓存命中和归一化成本等派生指标的预测精度。
- 小样本回退、窗口组合和个性化残差，但不得把未知数据解释为最优。

用户选择的 `price`、`speed` 或 `balanced` 是长期意图，不是仅供模型参考的弱标签。系统 MUST 为每种偏好定义允许的权重范围、质量下限和价格/延迟护栏；自动优化不得越过该范围把价格优先悄然变成速度优先，反之亦然。

### 21.2 用户体验目标函数

所有策略首先优化最终成功体验，而不是单次上游尝试。离线评估与未来策略版本 SHALL 至少计算：

```text
FailureRisk = 1 - P(final_success)

ExpectedSuccessfulCost =
    E(user_billable_cost_of_final_route_chain)
    / max(P(final_success), epsilon)

ExpectedTimeToSuccess =
    E(queue_time + attempt_time + retry_time + failover_time
      | final_success)

StabilityLoss =
    normalized(group_switches + sticky_breaks + cold_cache_events)
```

其中：

- `P(final_success)` 必须由分组访问成功率、候选顺序、错误相关性和置信度共同估计，不能把各分组成功率简单相加。
- `ExpectedSuccessfulCost` 面向用户体验时使用最终用户可见计费；供应商失败成本和利润风险作为独立内部护栏，不能伪装成用户倍率。
- `ExpectedTimeToSuccess` 必须包含排队、失败尝试和故障转移耗时，不能只比较成功分组自身 TTFT。
- `StabilityLoss` 用于惩罚不必要切组、粘性破坏和跨组冷缓存；正常排水式恢复不应被当作立即迁移。

策略体验损失可以表达为：

```text
ExperienceLoss_profile =
    w_failure  × FailureRisk
  + w_price    × Normalize(ExpectedSuccessfulCost)
  + w_speed    × Normalize(ExpectedTimeToSuccess)
  + w_stable   × StabilityLoss
  + w_capacity × CapacityRisk
```

在线实现可以继续使用第 10 节的等价正向 `Score_policy`，但同一 `strategy_version` MUST 固定正向评分与体验损失之间的映射，使离线回放、线上排序和验收指标使用相同语义。`w_failure` 的最低值和可靠性硬门槛由管理员设置，任何偏好均不得将其降为可忽略项。

三种策略的主目标为：

- `price`：在最终成功率和延迟护栏满足时，最小化每次最终成功的缓存修正预期成本。
- `speed`：在最终成功率和价格上限满足时，最小化到最终成功的 TTFT 与完成时间。
- `balanced`：在最终成功率满足时，最小化成本、速度、切换和容量风险的综合偏离。

### 21.3 多时间尺度控制闭环

系统 SHALL 把持续优化拆成互相隔离的控制环：

| 控制环 | 时间尺度 | 输入 | 输出 | 是否进入请求热路径 |
|---|---|---|---|---|
| 实时安全控制 | 请求级至秒级 | 熔断、权限、容量、粘性、端点状态 | 硬过滤、故障转移、探测租约 | 是；仅 Redis/本地有界操作 |
| 动态评分 | 30 至 60 秒 | 监控聚合桶、当前价格、实时容量投影 | `RoutingMetricSnapshot` 与动态排序输入 | 是；请求只读本地快照 |
| 策略校准 | 天级至周级 | 决策样本、结果、成本、延迟和实验数据 | 候选权重、阈值或预测模型版本 | 否；离线生成候选版本 |
| 产品治理 | 按需 | 用户反馈、业务边界、事故和合规要求 | 偏好合同、硬护栏、发布审批 | 否 |

慢控制环 MUST NOT 直接写入单请求的熔断或粘性状态。离线优化器只能生成不可变候选版本，必须经过数据校验、影子运行和灰度流程后才能切换为 active。

### 21.4 策略、实验和模型注册表

PostgreSQL SHALL 保存不可变、可审计的版本元数据。可以使用以下逻辑表或语义等价模型：

`routing_strategy_versions` 至少包含：

- `strategy_version`、`parent_version`、适用平台/模型族/端点和用户偏好。
- 成功率门槛、各评分权重及允许范围、窗口、迟滞、最小驻留和流量变化上限。
- `feature_schema_version`、可空 `model_version`、配置 checksum。
- `status`：`draft`、`shadow`、`canary`、`active`、`paused`、`retired`。
- 创建者、变更说明、离线评估摘要、创建/启用/停用时间。

`routing_experiments` 至少包含：

- `experiment_id`、baseline/candidate 版本、稳定分桶 salt 与目标人群。
- 流量比例、开始/结束时间、主指标、护栏、最小样本和自动停止条件。
- 状态、停止原因、评估结论和最终采用版本。

未来启用预测模型时，`routing_model_versions` 至少包含：

- `model_version`、模型类型、制品 URI、checksum 和签名/来源信息。
- `feature_schema_version`、训练数据时间窗、样本过滤规则和代码/数据 lineage。
- 离线校准、分组切片、漂移基线、推理资源预算和状态。

版本发布后 MUST 不可原地修改；任何权重、阈值、特征含义或模型制品变化都创建新版本。PostgreSQL 是版本和状态的权威源，Redis 只保存当前 active/canary 指针，网关只加载通过 schema、checksum 和兼容性校验的不可变对象。

### 21.5 当前必须预埋的决策数据

从确定性规则第一版开始，系统 MUST 能重建“当时看到什么、为什么选择、后来发生什么”：

1. 记录决策时点的配置候选、过滤后候选和每个候选的排除原因。
2. 记录每个候选的原始特征、置信度、归一化值、分项得分、总分和完整排名。
3. 记录粘性、熔断、恢复、容量、缓存价格窗口和依赖版本。
4. 记录被选分组、实际尝试链、最终结果、标准错误、实际组、切换次数和缓存影响。
5. 记录供应商真实用量、用户计费用量、预估成本、实际成本、TTFT、完成耗时和数据是否完整。
6. 记录 `route_version`、`strategy_version`、`score_version`、`feature_schema_version`、可空 `model_version` 与 `experiment_id`。
7. 记录稳定采样概率；存在探索时还必须记录每个可选动作的概率或至少被选动作的准确 `action_propensity`。

为控制写放大：

- 所有流量进入低基数聚合指标。
- 全部失败、切组、熔断转换、探测、部分计费和数据异常事件保存完整事实。
- 实验、探索和 canary 新会话决定保存完整事实。
- 普通成功按照 `routing_decision_id` 的稳定哈希采样；采样概率必须随事实保存，不能在落库后猜测。
- 同一决定的候选和尝试可以编码为有界事件后异步批量持久化，不能在请求内逐候选 INSERT。

系统不得保存提示词、响应正文、API Key 明文或供应商凭据作为训练特征。输入特征只允许使用业务必要且可枚举/分桶的信息，例如模型族、端点、输入 token 桶、预计输出桶、缓存可用性、service tier、时间桶、区域、容量和历史统计。

训练数据集必须保存查询版本、时间边界、特征 schema、采样/排除规则和 checksum。已经发生价格变更或特征口径变更时，必须使用决策时点快照或明确的 point-in-time join，禁止用未来数据覆盖历史特征造成数据泄漏。

### 21.6 分层个性化与冷启动

个性化 SHALL 采用有置信度收缩的分层结构：

```text
平台全局基线
  → 平台 × 模型族 × 端点基线
    → 物理分组表现
      → 用户群体修正
        → 用户/API Key 有界残差
```

- 新 Key 或低样本 Key 使用平台、模型和物理分组基线，并以用户配置顺序作为同分和降级回退。
- 用户/API Key 级修正只有达到最小独立样本量和置信度后才能逐步生效；样本不足时必须向上层基线收缩。
- Key 级修正只表达该 Key 常见输入长度、输出结构、缓存行为、时间段和端点组合造成的残差，不得重新定义分组的全局健康事实。
- 个性化权重只能在用户所选偏好的版本化 envelope 内变化，并受成功率、价格、延迟和切换护栏限制。
- 不得为每个用户训练或常驻一份完整模型，也不得在 Redis 复制全平台用户评分；共享预测加少量本地残差即可。

同一用户的不同 Key MAY 因模型结构、缓存行为和请求时段不同形成不同残差，但一个 Key 的异常不得无置信度地污染其他 Key 或全局分组健康。

### 21.7 选择偏差与受控探索

历史路由只观察被实际选择或尝试分组的结果，未选择候选的结果属于未知反事实。系统 MUST NOT 直接用“被选次数多、成功次数多”训练并证明该候选优于从未被选的分组，否则会把旧策略偏差自我强化。

第一阶段受控探索默认关闭。未来需要比较反事实时，MAY 启用有严格边界的 contextual bandit 或等价探索，但必须满足：

- 只作用于没有有效粘性的新会话，不能搬迁活跃会话。
- 探索候选已通过所有硬过滤、成功率门槛和端点安全检查，且未处于 `OPEN`/`HALF_OPEN` 的非授权探测状态。
- 候选价格、预测延迟和失败风险处于用户偏好对应的允许 envelope 内。
- 使用稳定哈希分桶、全局与 Key 级流量预算、最小驻留和每日上限，不能逐请求无界随机。
- 精确记录候选概率和 `action_propensity`，保证 IPS、doubly robust 或等价离线评估可以校正选择偏差。
- 决策事件链路必须健康；覆盖率、队列或消费者延迟不满足实验要求时停止分配新探索流量。
- 成功率、P95、成本、切换率、冷缓存或投诉护栏越界时自动停止并回到 baseline。
- 不得为探索绕过 50% 熔断、容量限制、权限、粘性或流式/副作用安全规则。

不启用探索时，离线回放只用于检查确定性排序、一致性和可观测结果，MUST NOT 宣称能够精确知道未选择候选本应产生的成功率或延迟。

### 21.8 稳定性和反馈回路控制

为了避免评分振荡、赢家锁定和容量自反馈，所有动态版本 MUST 具备：

- 成功率置信修正、指数平滑或等价窗口平滑。
- 排名切换最小分差、最小驻留时间和升降级迟滞。
- 单次权重变化、单轮排名变化和新会话流量变化上限。
- `RECOVERING` 渐进放量；恢复流量不得一次性抢回全部新会话。
- 活跃粘性不随排行榜变化，只有不可用时才重新选择。
- 容量下降引起的溢出不得反向计为健康失败；负载恢复后按迟滞收敛。
- 对共享账号、渠道、代理、区域或供应商故障域进行依赖去重或相关性标记，不能把高度相关候选的成功概率当作独立事件相乘。
- 对长期没有被选中的健康候选保留被动健康证据、受控探测或安全探索入口，避免因为缺少新样本永久失去机会。

若快照更新导致排名频繁翻转，系统 MUST 优先保持当前稳定版本并告警，不能通过缩短粘性 TTL 或批量删除缓存来强迫流量追随瞬时排名。

### 21.9 评估、影子、灰度与回滚

候选策略或模型的发布生命周期为：

```text
数据质量检查
  → 离线回放与切片评估
  → shadow 只计算不执行
  → canary 稳定分桶
  → 分阶段扩大新会话比例
  → active
  → 持续监控或一键回滚
```

- 离线评估 MUST 按平台、模型族、端点、偏好、输入长度、缓存状态和高低流量时段切片，不能只看全局平均值。
- shadow 决定必须与实际 baseline 决定通过同一 `routing_decision_id` 关联，但不得改变实际分组、粘性、账单或容量租约。
- canary 使用稳定用户/Key 分桶，避免同一 Key 在版本间逐请求跳动；只对新会话采用候选版本。
- 推广条件必须同时满足主体验指标、最终成功率、错误率、P95/P99、成本、切换、冷缓存和系统资源护栏。
- 任一硬护栏越界时自动把 Redis active/canary 指针原子切回已知安全版本，并保留事件供审计。
- 回滚不得递增用户 `route_version`，不得删除现有粘性，也不得重写历史决定。

每次决定在开始时固定使用一组兼容的 `strategy_version + score_version + feature_schema_version + model_version`，请求中途即使 active 指针变化，也必须用原组合完成解释和状态写入。

### 21.10 模型演进路线

模型不是第一阶段的前置条件。推荐演进顺序为：

1. **确定性规则基线**：使用第 10 节公式、置信区间、固定偏好 envelope 和动态快照，建立可解释基线。
2. **影子结果预测**：模型只预测成功概率、TTFT/完成耗时分位数、容量溢出概率、缓存命中/冷缓存概率和归一化成本，不参与实际选择。
3. **受约束预测排序**：规则引擎先做硬过滤，再把模型预测映射到用户偏好体验损失并确定性排序。
4. **可选上下文 bandit**：只在有准确 propensity、严格探索预算和自动停止条件后，为安全候选学习细粒度选择。

模型 SHOULD 优先采用可校准、CPU 推理开销有界且容易解释的小型模型或 learning-to-rank；不得因为“智能”而默认依赖大模型或远程推理服务。模型只能输出分项预测或受约束效用，不得直接绕过规则引擎返回任意分组 ID。

在线推理必须：

- 在网关本地内存完成，最多处理当前 Key 的 8 个候选。
- 不执行远程模型调用，不查询历史数据库，不把提示词发送给训练或推理服务。
- 有明确的 CPU、内存和耗时预算；超时、异常、NaN、schema 不兼容或制品校验失败时立即使用当前确定性 baseline。
- 暴露预测校准误差、特征缺失率、数据漂移、推理时长和 fallback 指标。
- 在模型版本切换前完成制品 checksum、特征契约和输出范围校验。

重新训练由数据量、校准误差或漂移触发，不应只按固定日期自动上线。训练完成只产生 `draft` 版本，不能直接改变生产路由。

### 21.11 用户透明度

用户界面 SHALL 说明智能调度会在用户所选分组和偏好内持续优化，但不得承诺固定分组、固定倍率或固定排名。至少展示：

- 用户当前偏好、当前动态顺序和主要分项解释。
- 成功率状态、预估倍率/区间、TTFT/耗时、缓存命中率、置信度和更新时间。
- “已有健康会话保持当前分组；新会话按最新策略选择”的说明。
- 最终仍按实际服务分组展示和计费。

管理员诊断界面还应显示决策使用的全部版本、实验分桶、shadow/canary 差异和回滚原因。不得向普通用户暴露内部账号、故障域、模型制品 URI 或其他租户数据。

### 21.12 持续优化指标

所有偏好共同的首要护栏为最终成功率，并同时监控：

- 到最终成功的 TTFT、完成耗时和 P95/P99。
- 每次最终成功的用户实际归一化成本。
- 首选一次成功率、跨组切换率、重试次数和全部候选失败率。
- 粘性保持率、异常粘性失效率、冷缓存率和缓存补偿量。
- 熔断开路率、恢复耗时、容量溢出率和评分振荡次数。
- 预测成功率/延迟/成本的校准误差、特征缺失和 drift。

策略主指标分别为：

- `price`：在成功率和延迟护栏内的每次最终成功归一化实际成本。
- `speed`：在成功率和价格护栏内的到最终成功 TTFT 与完成耗时。
- `balanced`：成功率护栏内，成本、耗时、切换和冷缓存的版本化综合体验损失。

版本比较必须报告样本量、置信区间、实验覆盖范围和观察周期。系统 MUST NOT 只凭总平均值或短窗口偶然提升自动全量发布。

Prometheus 中的具体策略和模型版本标签只允许暴露 active、canary、baseline 及有限个近期版本；长期版本明细进入有保留期的诊断事实，不能让时序标签随历史版本无限增长。

### 21.13 第一阶段必须预埋与可以后置的能力

第一阶段即使不训练模型，也 MUST 实现：

- 确定性、可解释、版本化的 baseline 策略和偏好 envelope。
- 独立的 `route_version`、`strategy_version`、`score_version`、`feature_schema_version`，以及可空的 `model_version`、`experiment_id`。
- 决策时点候选快照、稳定采样、异常全量记录、结果关联和数据保留规则。
- 稳定分桶函数和采样/动作概率字段；探索关闭时字段可以为空，但事件 schema 不再需要破坏式修改。
- 策略版本注册、shadow/canary/active 状态、active 指针、确定性回退版本和 kill switch。
- 指标平滑、迟滞、最小驻留、流量变化上限和排水式恢复。
- 按偏好拆分的体验指标、数据质量面板、版本对比和自动回滚告警。
- Builder、网关和事件消费者间稳定的特征契约；未知字段可忽略，关键字段缺失时拒绝加载。

以下能力 MAY 在数据和运行经验充足后实现：

- 模型训练流水线、模型制品存储与在线预测。
- 用户/API Key 级学习残差。
- contextual bandit、IPS/doubly robust 评估和真实流量探索。
- 自动生成权重候选和自动晋级；即使实现，也必须保留人工暂停和硬护栏。

## 22. 迁移、发布与回滚

1. 新增表和字段必须向后兼容，默认值为单分组 `sequential`。
2. 数据迁移幂等地把每个现有 `api_keys.group_id` 回填为优先级 0 的路由。
3. 过渡期对 `group_id` 与路由表实行双读校验和单事务写穿；发现不一致时优先使用可验证的路由表并告警。
4. 使用功能开关分阶段启用：内部账号、测试环境、小比例用户、全量用户。
5. 首先覆盖无副作用的同步文本端点，再覆盖流式、Responses、Claude/Gemini、媒体、批处理和实时端点。
6. 未完成安全重试适配的端点 MUST 明确禁用跨分组重试，不能静默采用不安全行为。
7. 通用可用发布前，所有对外支持端点必须有明确的“支持、受限支持或不支持”矩阵并在界面可见。
8. 回滚版本继续读取 `api_keys.group_id`，因此路由首项镜像必须保留到回滚窗口结束。
9. 第一阶段先发布确定性策略、版本注册、决策采样、体验指标、shadow/canary 控制和 kill switch；不以模型训练完成作为多分组功能上线前提。
10. 个性化残差、模型预测和受控探索分别使用独立功能开关；任一开关关闭时必须回到相同用户偏好下的确定性 baseline。

## ADDED Requirements

### Requirement: API Key 必须支持同平台多分组路由集合
系统 SHALL 允许用户为一个外部 API Key 配置一个有序物理分组集合。集合内分组 MUST 属于同一平台和同一计费类型，且 MUST 是用户有权使用的分组。

#### Scenario: 创建合法多分组 Key
- **WHEN** 用户选择多个同平台、同计费类型且有权访问的分组并提交合法顺序
- **THEN** 系统 MUST 原子保存路由集合、调度模式和路由版本
- **THEN** 返回的旧 `group_id` MUST 等于优先级 0 的分组

#### Scenario: 混入跨平台分组
- **WHEN** 用户选择的分组来自不同平台
- **THEN** 系统 MUST 拒绝保存并返回稳定的字段级错误
- **THEN** 旧路由配置 MUST 保持不变

#### Scenario: 混入不同计费类型
- **WHEN** 路由集合同时包含标准计费和订阅计费分组
- **THEN** 第一阶段实现 MUST 拒绝保存

#### Scenario: 旧客户端只提交 group_id
- **WHEN** 旧客户端创建或更新只包含一个 `group_id`
- **THEN** 系统 MUST 生成单路由顺序调度配置
- **THEN** 请求行为 MUST 与升级前一致

### Requirement: 候选集合必须是不可越界的用户白名单
调度器 MUST 只在当前 Key 的有效配置分组中选择实际分组，不得因智能优化、无可用候选、指标缺失或系统降级而使用集合外分组。

#### Scenario: 全部配置分组不可用
- **WHEN** 路由集合中的全部分组都被硬过滤、熔断或尝试失败
- **THEN** 系统 MUST 返回明确的无可用分组错误
- **THEN** 系统 MUST NOT 尝试同平台其他未选择分组

#### Scenario: 智能指标存储不可用
- **WHEN** 智能调度无法取得有效评分快照
- **THEN** 系统 MUST 在用户配置集合内按顺序退化
- **THEN** 系统 MUST 记录降级原因

### Requirement: 顺序调度必须保持用户顺序并先完成分组内故障转移
顺序调度 SHALL 按优先级由小到大选择分组。系统 MUST 先耗尽当前分组内允许的账号级故障转移，再决定是否进入下一分组。

#### Scenario: 首选分组某个账号失败但仍有可用账号
- **WHEN** 首选分组中的当前账号发生可重试错误且同组仍有合格账号
- **THEN** 系统 MUST 先在首选分组内切换账号
- **THEN** 系统 MUST NOT 因单账号错误立即切换物理分组

#### Scenario: 首选分组整体耗尽
- **WHEN** 首选分组内全部合格账号均无法服务且错误允许跨分组重试
- **THEN** 系统 MUST 尝试下一个可用配置分组
- **THEN** 成功使用记录 MUST 归属该实际分组

#### Scenario: 首选分组仅容量暂满
- **WHEN** 首选分组健康但实时容量暂满
- **THEN** 系统 MAY 溢出到下一个分组
- **THEN** 系统 MUST NOT 把该事件计为健康失败或开启健康熔断

### Requirement: 分组访问成功率门槛必须可配置且不得低于百分之五十
系统 SHALL 把 50% 作为全局安全底线，允许用户对当前 Key 选择 50%..95%、每档 5% 的门槛。当前有效窗口达到最小样本量且原始分组访问成功率低于所选门槛时 MUST 进入 `OPEN`。只有受单租约或渐进放量控制的恢复探测可以例外进入，不得将其视为普通健康候选。

#### Scenario: 首次完整分组访问失败
- **WHEN** 当前 Key、路由版本、分组、模型族和端点作用域尚无历史样本，且首次分组访问在耗尽同组可用账号后失败
- **THEN** 该作用域的分组访问成功率 MUST 计算为 0%
- **THEN** 样本不足时 MUST NOT 仅因这一个样本开启整个窗口的统计熔断
- **THEN** 当前请求在满足安全重试条件时 MUST 继续尝试下一个配置分组

#### Scenario: 滚动成功率跌破百分之五十
- **WHEN** 样本量达到最小要求，新的有效分组访问结果使当前硬熔断窗口成功率从不低于 50% 变为低于 50%
- **THEN** 系统 MUST 在记录该结果后立即进入 `OPEN`
- **THEN** 系统 MUST NOT 等待额外连续失败或下一次健康快照

#### Scenario: 成功率恰好为百分之五十
- **WHEN** 当前有效窗口内的原始分组访问成功率恰好等于 50%
- **THEN** 系统 MUST NOT 仅因 50% 硬熔断规则开路
- **THEN** 分组仍 MUST 接受智能准入线和其他熔断规则判断

#### Scenario: 单账号失败后同组恢复成功
- **WHEN** 一个账号失败但分组内另一个账号成功完成请求
- **THEN** 本次分组访问 MUST 记为成功
- **THEN** 单账号失败 MUST NOT 单独触发 50% 分组硬熔断

#### Scenario: 全局健康样本不足
- **WHEN** 聚合到全部 Key 的全局分组访问成功率低于 50% 但尚未达到全局最小样本量
- **THEN** 系统 MUST NOT 因该小样本直接全局熔断全部 Key
- **THEN** 已经满足条件的 Key 级路由熔断 MUST 继续生效

### Requirement: 智能调度必须以成功率为硬基线
智能调度 SHALL 先按成功率置信值、熔断和容量执行可靠性准入，再在合格候选中优化价格、速度或均衡目标。价格和速度优势 MUST NOT 覆盖可靠性不达标。

#### Scenario: 最便宜分组成功率不达标
- **WHEN** 价格优先模式下最低倍率分组低于可靠性门槛
- **THEN** 系统 MUST 排除该分组
- **THEN** 系统 MUST 从其余通过可靠性准入的候选中选择

#### Scenario: 最快分组成功率不达标
- **WHEN** 速度优先模式下最低延迟分组低于可靠性门槛
- **THEN** 系统 MUST 排除该分组
- **THEN** 系统 MUST NOT 用速度优势抵消失败风险

#### Scenario: 低样本分组表面成功率为百分之百
- **WHEN** 某分组只有极少样本且表面成功率为 100%
- **THEN** 系统 MUST 使用置信度修正并把它视为冷启动或观察候选
- **THEN** 系统 MUST NOT 直接给它分配主要流量

#### Scenario: 三种评分策略比较同一候选集合
- **WHEN** 价格、速度或均衡策略分别运行
- **THEN** 三种策略 MUST 使用相同的可靠性准入基线
- **THEN** 三种策略 MUST 使用相同的成功率、价格、速度和容量评分维度
- **THEN** 只有通过基线后的维度权重可以不同

#### Scenario: 智能调度生成动态顺序
- **WHEN** 候选分组通过可靠性准入且评分输入可用
- **THEN** 系统 MUST 按当前策略权重计算每个分组总分
- **THEN** 系统 MUST 按总分从高到低动态排序并首先尝试第一名
- **THEN** 系统 MUST NOT 把分数归一化为随机分流概率

#### Scenario: 健康或容量发生变化
- **WHEN** 新健康快照生效、熔断状态变化或实时容量显著变化
- **THEN** 后续无粘性请求 MUST 使用更新后的评分重新排序
- **THEN** 已有健康粘性会话 MUST NOT 仅因排名变化而被迁移

### Requirement: 价格评分必须使用缓存修正的归一化有效倍率
系统 SHALL 按物理分组、模型族、端点和价格窗口聚合普通输入、缓存创建、缓存读取与输出 token，并使用当前价格重算以输入 100% 缓存命中为基准的归一化有效倍率。价格评分 MUST 由该倍率生成，不得直接按名义分组倍率排序。

#### Scenario: 按窗口整体 token 计算缓存命中率
- **WHEN** 价格窗口内包含多次不同输入规模和缓存命中率的请求
- **THEN** 系统 MUST 用 `总 cache_read_tokens / 总 logical_input_tokens` 计算整体缓存命中率
- **THEN** 系统 MUST NOT 对请求级缓存命中率做简单算术平均

#### Scenario: 以百分之百缓存命中为共同基准
- **WHEN** 系统为某候选分组计算价格指标
- **THEN** 分子 MUST 使用该分组当前有效价格和窗口内真实的普通输入、缓存创建、缓存读取及输出结构
- **THEN** 分母 MUST 使用同模型同档位的标准 1.0x 参考价格，把全部逻辑输入视为缓存读取并保留真实输出量
- **THEN** 两者之比 MUST 成为该分组的归一化有效倍率

#### Scenario: 标准一倍分组达到百分之百输入缓存命中
- **WHEN** 某分组所有价格项均等于标准 1.0x 参考价格，窗口内全部逻辑输入都是缓存读取，且没有缓存创建
- **THEN** 该分组归一化有效倍率 MUST 等于 1.0

#### Scenario: 低名义倍率分组缓存表现更差
- **WHEN** 分组 A 的名义倍率低于分组 B，但 A 较低的缓存命中率和缓存创建成本使其归一化有效倍率高于 B
- **THEN** 价格维度 MUST 认为分组 B 更便宜
- **THEN** 价格优先策略在其他硬门槛通过后 MUST 给 B 更高的价格得分

#### Scenario: 输出量占成本主要部分
- **WHEN** 某模型窗口内输出 token 成本明显高于输入成本
- **THEN** 100% 缓存基准 MUST 在分子和分母中都保留输出 token 及对应价格
- **THEN** 系统 MUST NOT 假设缓存命中可以消除输出成本

#### Scenario: 分组当前价格发生变化
- **WHEN** 窗口 token 结构不变但分组价格、渠道价格、service tier 或用户专属倍率变化
- **THEN** 系统 MUST 使用当前价格重新估算窗口成本并刷新归一化有效倍率
- **THEN** 系统 MUST NOT 等待旧价格下的历史账单自然退出窗口

#### Scenario: 缓存写入存在五分钟和一小时档位
- **WHEN** 上游用量分别报告 5 分钟和 1 小时缓存创建 token
- **THEN** 当前预估成本 MUST 使用各自的当前缓存创建价格
- **THEN** 系统 MUST NOT 把两类 token 无条件合并为同一价格

#### Scenario: 价格样本不足
- **WHEN** 一小时窗口未达到最小请求数或最小逻辑输入 token 数
- **THEN** 系统 MUST 按 24 小时、平台模型基线、名义倍率的顺序保守回退
- **THEN** 系统 MUST 降低价格置信度并展示回退来源
- **THEN** 未知价格表现 MUST NOT 被当作最优价格

#### Scenario: 故障转移制造冷缓存
- **WHEN** 一个已有缓存的会话因系统跨分组故障转移而产生首次冷缓存或计费补偿
- **THEN** 该事件 MUST NOT 降低目标分组的稳态缓存命中率或重复进入价格训练样本
- **THEN** 真实用量和补偿事实仍 MUST 独立保留用于成本与审计

#### Scenario: 非文本 token 计费模型
- **WHEN** 模型按次、按图片或使用不具备可比较缓存价格的计费方式
- **THEN** 系统 MUST 使用相同业务单位下的当前预计单位成本计算价格得分
- **THEN** 系统 MUST 标记缓存修正不适用，而不是伪造缓存命中率

### Requirement: 智能调度必须使用物理分组历史和实时容量
系统 SHALL 按物理分组、模型族和端点类型汇总分组访问成功率、延迟和错误，并结合实时容量生成版本化调度快照。

#### Scenario: 同一分组不同模型表现不同
- **WHEN** 某分组对模型 A 健康但对模型 B 持续失败
- **THEN** 模型 A 和模型 B MUST 使用不同的健康与熔断结论
- **THEN** 系统 MUST NOT 因模型 B 故障无条件排除该分组的全部模型

#### Scenario: 多个分组共享底层账号
- **WHEN** 候选分组映射到重叠的账号池
- **THEN** 容量估计 MUST 对共享账号去重
- **THEN** 系统 MUST NOT 把重复引用计算为额外容量

#### Scenario: 热路径执行智能选择
- **WHEN** 网关处理普通模型请求
- **THEN** 系统 MUST 从内存或 Redis 快照读取历史评分
- **THEN** 系统 MUST NOT 在请求热路径聚合 PostgreSQL 历史记录

### Requirement: 路由必须同时具备 Key 级避障和会话级粘性
系统 SHALL 在分组故障后让同一 Key 的新请求在熔断窗口内避开故障分组，并让已有会话尽量维持在当前实际分组。

#### Scenario: 一个会话触发首选分组熔断
- **WHEN** 某会话确认首选分组整体发生合格故障并切换到备用分组
- **THEN** 该 Key、路由版本、模型族和端点范围的首选分组 MUST 进入共享避障状态
- **THEN** 窗口期内其他新请求 MUST 跳过该首选分组

#### Scenario: 同一会话继续请求
- **WHEN** 会话已经在备用分组成功且粘性仍有效
- **THEN** 后续请求 MUST 优先继续使用该备用分组
- **THEN** 系统 MUST 避免无故在多个分组之间抖动

#### Scenario: 路由配置发生变化
- **WHEN** 用户修改分组、顺序、模式或智能评分策略
- **THEN** 系统 MUST 递增 `route_version`
- **THEN** 仅候选分组、顺序或启用状态变化时 MUST 递增 `routing_state_version` 隔离旧粘性和 breaker
- **THEN** 仅修改比例、门槛或模式时 MUST 保留健康会话和已有健康样本，并按新门槛重新判断；调低门槛不得跳过既有冷却和恢复流程

### Requirement: 用户调度滑块必须可访问并与后端权重一致
系统 SHALL 使用两条独立滑块配置价格/速度预算和成功率门槛，并在 PostgreSQL、鉴权快照、请求计划及决策事实中保持精确值。滑块 MUST 支持键盘、重置、深色模式和窄屏，不得为每种比例创建共享评分缓存或监控标签。

#### Scenario: 设置非预设比例
- **WHEN** 用户选择价格 70% / 速度 30% 并保存
- **THEN** API MUST 保存 `smart_balance_bps = 3000`，后端采用 50/28/12/10 的成功率/价格/速度/容量权重
- **THEN** 重新编辑 MUST 回显相同比例，不能舍入为三个旧预设之一

#### Scenario: 用户设置较严格门槛
- **WHEN** 某 Key 将门槛从 50% 调至 85%，当前候选已有足量样本且成功率为 80%
- **THEN** 该 Key 的候选 MUST 熔断，其他 Key 继续依照自己的门槛准入
- **THEN** 所有候选均不达标时 MUST 返回无可用候选，不得扩展白名单或降低门槛

#### Scenario: 历史请求重放
- **WHEN** 用户修改比例或门槛后重放之前的决策
- **THEN** 重放 MUST 使用决策时持久化的比例、门槛和版本，而不是当前 API Key 配置

### Requirement: 熔断恢复必须使用单探测和排水式回切
系统 SHALL 使用 `CLOSED`、`OPEN`、`HALF_OPEN` 和 `RECOVERING` 状态控制故障分组恢复。多实例下同一范围只能有受限的半开探测，恢复后不得强制迁移活跃备用会话。

#### Scenario: 开路冷却到期
- **WHEN** `OPEN` 冷却时间到期
- **THEN** 只有获得共享原子租约的请求可以进入 `HALF_OPEN` 探测
- **THEN** 其他请求 MUST 继续使用备用分组

#### Scenario: 半开探测成功
- **WHEN** 半开探测成功
- **THEN** 分组 MUST 进入 `RECOVERING` 而不是立即承接全部会话
- **THEN** 系统 MUST 逐步放入无粘性的新会话

#### Scenario: 首选分组恢复但备用会话仍活跃
- **WHEN** 首选分组进入恢复状态且某会话仍在备用分组持续成功
- **THEN** 系统 MUST 保留该会话的备用分组粘性直到自然过期或失败
- **THEN** 系统 MUST NOT 批量删除粘性强制回切

#### Scenario: 恢复观察期再次失败
- **WHEN** `RECOVERING` 分组再次发生达到阈值的合格失败
- **THEN** 系统 MUST 将其重新置为 `OPEN`
- **THEN** 系统 MUST 使用有界退避延长冷却时间

### Requirement: 缓存失效必须被减轻且真实用量不可被改写
系统 SHALL 通过会话粘性和排水式回切减少跨分组冷缓存。对系统故障转移导致的合格冷启动 MAY 提供有界计费补偿，但必须同时保存供应商真实用量和独立的用户计费用量。

#### Scenario: 故障转移导致首次冷缓存
- **WHEN** 一个已有成功粘性的会话因系统故障被迫切换分组且产生可识别的冷缓存差额
- **THEN** 系统 SHOULD 在配置上限内记录冷缓存补偿
- **THEN** 用户计费字段 MAY 应用补偿
- **THEN** 供应商真实 token 和真实成本 MUST 保持不变

#### Scenario: 用户主动修改路由导致冷缓存
- **WHEN** 用户修改路由配置、模型或主动开始新会话
- **THEN** 系统 MUST NOT 将该冷缓存自动认定为故障补偿

#### Scenario: 分组恢复
- **WHEN** 原分组恢复但活跃会话仍粘在备用分组
- **THEN** 系统 MUST 通过自然排水避免再次制造冷缓存

### Requirement: 使用记录和计费必须归属实际分组
每次成功请求的使用记录、价格解析、用户倍率、订阅扣减和利润控制 MUST 使用实际提供服务的物理分组。首选分组和策略只作为路由上下文记录。

#### Scenario: 请求在第二分组成功
- **WHEN** 请求先访问首选分组失败并在第二分组成功
- **THEN** `usage_logs.group_id` MUST 为第二分组 ID
- **THEN** 计费 MUST 使用第二分组的有效模型价格和用户倍率
- **THEN** 路由尝试事实 MUST 保留首选分组失败和切换链路

#### Scenario: 失败分组没有产生用户结果
- **WHEN** 某分组访问失败且没有产生用户可用结果或用户计费用量
- **THEN** 系统 MUST NOT 为该分组创建伪成功使用记录
- **THEN** 系统 MUST 在内部路由尝试中记录失败

#### Scenario: 失败尝试产生供应商成本
- **WHEN** 失败或部分流式请求已经产生供应商可计费量
- **THEN** 系统 MUST 记录真实成本和实际分组
- **THEN** 用户计费处理 MUST 遵守部分结果与补偿策略，且不得丢失成本事实

### Requirement: 预估倍率必须可解释但不得替代实际结算
系统 SHALL 根据动态排序下各分组的预计实际命中占比和缓存修正后的归一化有效倍率，计算模型相关的整体预估倍率，并同时展示名义倍率、区间、价格窗口和置信度。

#### Scenario: 用户查看智能调度预估倍率
- **WHEN** 用户选择模型和智能评分策略
- **THEN** 页面 MUST 展示基于当前合格候选、预测命中占比和 100% 缓存基准的归一化预估倍率
- **THEN** 页面 MUST 同时展示整体缓存命中率、输入输出样本量和名义倍率
- **THEN** 页面 MUST 说明最终按实际分组结算

#### Scenario: 配置分组价格变化
- **WHEN** 某候选分组倍率更新
- **THEN** 系统 MUST 使用当前价格重放窗口 token 结构并重新计算归一化预估倍率
- **THEN** 系统 MUST NOT 自动加入新分组或静默恢复旧价格

#### Scenario: 统计置信度不足
- **WHEN** 成功率、缓存输入输出结构或排序命中占比样本不足
- **THEN** 页面 MUST 显示低置信度或更宽区间
- **THEN** 系统 MUST NOT 把单点预估表达为保证价格

### Requirement: 跨分组重试必须避免重复输出和重复副作用
系统 MUST 只在尚未输出语义数据且请求可以安全重放时执行跨分组重试。可能已经被供应商接受的有副作用请求必须有可证明的幂等保护。

#### Scenario: 流式响应尚未输出语义数据
- **WHEN** 上游在首个语义数据之前发生可重试错误
- **THEN** 系统 MAY 切换到下一个配置分组重试

#### Scenario: 流式响应已经输出 token
- **WHEN** 客户端已经收到至少一个语义 token 或工具调用
- **THEN** 系统 MUST NOT 跨分组重新执行请求
- **THEN** 系统 MUST 记录实际分组和中断结果

#### Scenario: 异步媒体任务接收状态不明确
- **WHEN** 图像、视频或批处理上游可能已经接受请求但响应失败
- **THEN** 系统 MUST NOT 在没有端到端幂等保证时提交到另一个分组
- **THEN** 系统 MUST 返回可诊断错误并保留已有任务标识

### Requirement: 权限变化和分组状态变化必须实时收敛
系统 SHALL 在运行时重新验证分组权限、状态、模型能力、计费资格和策略限制，并在相关数据变化时使认证和路由缓存失效。

#### Scenario: 用户失去备用分组权限
- **WHEN** 管理员撤销用户对某备用分组的权限
- **THEN** 后续请求 MUST 排除该分组
- **THEN** 已有指向该分组的粘性 MUST 不再生效

#### Scenario: 最后一个分组被停用
- **WHEN** Key 的最后一个配置分组被停用或删除
- **THEN** 请求 MUST 返回明确的无可用分组错误
- **THEN** 系统 MUST NOT 把空集合解释为可以访问任意分组

### Requirement: 持久事实、跨实例状态和本地快照必须分层存储
系统 SHALL 使用 PostgreSQL 保存配置、财务和长期审计事实，使用 Redis 保存有 TTL 的跨实例运行状态，并使用进程内存保存可重建的只读快照与聚合计数。任何正确性关键状态都不得只存在于单个实例内存。

#### Scenario: 服务进程重启
- **WHEN** 任一网关实例重启
- **THEN** API Key 路由配置、最终使用记录、账单和异常路由事实 MUST 可从 PostgreSQL 恢复
- **THEN** 评分本地快照 MUST 可从 Redis 或 PostgreSQL 聚合数据重建
- **THEN** 进程重启 MUST NOT 改写用户路由配置

#### Scenario: Redis 数据被清空
- **WHEN** Redis 故障恢复后粘性、熔断或评分快照已经丢失
- **THEN** PostgreSQL 中的路由配置、使用和财务事实 MUST 保持完整
- **THEN** 临时路由状态 MUST 由新流量和后台 Builder 重建
- **THEN** 系统 MUST NOT 为恢复临时状态而逐会话写入 PostgreSQL

#### Scenario: 关键财务记录与诊断队列同时失败
- **WHEN** Redis Stream 或诊断事件队列不可用但请求已经产生用户账单或供应商成本
- **THEN** 关键使用和成本事实 MUST 继续走现有可靠持久化路径
- **THEN** 系统 MUST NOT 把诊断队列成功当作财务提交成功的前提

#### Scenario: 原子更新 Key 路由配置
- **WHEN** 用户提交带正确 `expected_route_version` 的路由更新
- **THEN** 路由集合、调度模式、评分策略、兼容 `group_id`、递增后的 `route_version` 和缓存失效 outbox 事件 MUST 在同一 PostgreSQL 事务中提交
- **THEN** Redis L2 删除和 Pub/Sub 失效 MUST 只在事务提交后执行
- **THEN** 任一步数据库写入失败 MUST 保留旧配置

#### Scenario: 提交后 Redis 失效失败
- **WHEN** PostgreSQL 配置事务已经提交但即时 Redis 删除或 Pub/Sub 失败
- **THEN** outbox 事件 MUST 保持待投递并由后台 Worker 幂等重试
- **THEN** TTL、版本校验和依赖 guard MUST 作为漏通知的收敛保障
- **THEN** 系统 MUST 输出积压与死信告警

#### Scenario: 安全敏感依赖发生变化
- **WHEN** 分组被禁用或用户分组权限被撤销
- **THEN** 系统 MUST 更新依赖版本并主动失效相关 L2/L1 路由快照
- **THEN** 系统 MUST NOT 只依靠较长的普通 TTL 等待权限自然收敛

### Requirement: 路由热路径必须使用有界缓存和批量读取
在 L1 鉴权缓存和本地评分快照命中时，新增路由逻辑 MUST 不查询 PostgreSQL。候选状态必须批量读取，评分只能遍历当前 Key 的有界候选集合。

#### Scenario: L1 和本地评分快照命中
- **WHEN** 一个普通请求命中 API Key L1 快照和本地评分版本
- **THEN** 路由选择 MUST 执行 0 次 PostgreSQL 查询
- **THEN** 粘性和全部候选熔断状态 MUST 在最多 1 次新增 Redis 批量往返中取得
- **THEN** 系统 MUST 只对当前 Key 最多 8 个候选做内存评分与排序

#### Scenario: L1 未命中但 Redis L2 命中
- **WHEN** API Key 的进程内快照未命中而 Redis L2 快照有效
- **THEN** 系统 MUST 使用一次 L2 读取恢复完整路由计划
- **THEN** 系统 MUST NOT 对候选分组逐个查询 PostgreSQL

#### Scenario: L1 和 L2 同时未命中
- **WHEN** 多个并发请求同时访问同一个冷 Key
- **THEN** 系统 MUST 使用 singleflight 或等价机制合并数据库回源
- **THEN** 回源查询 MUST 一次性预加载有序路由与必要分组投影
- **THEN** 系统 MUST NOT 产生按候选数量增长的 N+1 查询

#### Scenario: 正常首选分组一次成功
- **WHEN** 请求在第一候选正常成功
- **THEN** 路由状态更新 MUST 在最多 1 次新增 Redis Pipeline/Lua 往返内完成
- **THEN** 系统 MUST NOT 为该普通成功同步插入完整路由尝试记录

#### Scenario: 性能基准验收
- **WHEN** 在测试环境以单分组和 8 分组 Key 分别执行相同并发压测
- **THEN** 系统 MUST 分别报告鉴权、Redis 状态读取、评分排序和状态更新的 P50/P95/P99
- **THEN** 新增路由逻辑的目标 P95 开销 SHOULD 不超过 5ms
- **THEN** 最终发布门槛 MUST 以部署环境基线确定并记录

### Requirement: 评分快照必须异步生成并原子发布
系统 SHALL 从有索引的 PostgreSQL 聚合桶异步生成共享评分快照，经 Redis 版本键分发后原子加载到各网关实例。请求不得在线聚合历史用量。

#### Scenario: Builder 周期刷新评分
- **WHEN** 到达 30 至 60 秒刷新周期
- **THEN** 单例 Builder MUST 以有界窗口/游标刷新原始事实，再只读取所需窗口内的聚合桶
- **THEN** Builder MUST NOT 全表扫描 `usage_logs`
- **THEN** Builder MUST 生成带 schema、数据和策略版本的完整快照

#### Scenario: 监控 V1 或关闭时仍可执行 smart baseline
- **WHEN** 多分组路由开启，Channel Monitor 使用 V1 或关闭，持续优化关闭
- **THEN** Builder MUST 独立维护路由健康/价格桶并发布评分快照
- **THEN** 失败、容量和换组事实 MUST 继续被有界消费，普通优化决策采样关闭
- **THEN** 任一重算块失败 MUST 回滚该块且不得推进其回填游标或发布本轮不完整快照

#### Scenario: 新评分版本发布
- **WHEN** Builder 成功生成新快照
- **THEN** 系统 MUST 先完整写入版本键，再原子切换当前版本指针
- **THEN** 网关实例 MUST 在完整校验后整体替换本地只读快照
- **THEN** 请求 MUST 不会观察到半新半旧的评分数据

#### Scenario: 多个 Builder 实例同时运行
- **WHEN** 多个服务实例同时具备构建能力
- **THEN** advisory lock、租约或等价单例机制 MUST 只允许一个实例执行同一轮聚合
- **THEN** 领导者失效后其他实例 MUST 能在有界时间内接管

#### Scenario: 用户专属倍率不同
- **WHEN** 两个用户选择相同物理分组但具有不同用户分组倍率
- **THEN** 系统 MUST 复用同一份物理分组指标快照
- **THEN** 各请求 MUST 只在本地候选集合上叠加自己的倍率
- **THEN** 系统 MUST NOT 为每个用户或 API Key 保存一份全量评分副本

#### Scenario: 评分版本过期
- **WHEN** 当前 Redis 和本地评分快照超过允许的新鲜度与 stale grace
- **THEN** 智能模式 MUST 在用户候选集合内退化为配置顺序
- **THEN** 请求 MUST NOT 为恢复评分而同步查询历史数据库

### Requirement: Redis 路由状态必须原子、可过期且兼容集群
系统 SHALL 使用带 `route_version` 的 Redis 键保存路由粘性、Key 级熔断、滚动计数和半开租约。状态更新必须原子执行，所有键必须有界并设置 TTL。

#### Scenario: 多实例并发更新成功率
- **WHEN** 多个实例同时完成同一 Key 路由作用域的分组访问
- **THEN** 成功/失败计数和 50% 熔断转换 MUST 通过 Lua 或等价原子操作更新
- **THEN** 系统 MUST NOT 因最后写入覆盖而丢失计数

#### Scenario: Redis Cluster 执行原子脚本
- **WHEN** 一个脚本同时读写粘性、熔断或探测相关键
- **THEN** 这些键 MUST 使用相同 hash tag 落在一个 slot
- **THEN** 实现 MUST 不产生 `CROSSSLOT` 错误

#### Scenario: 旧版本请求晚于配置更新完成
- **WHEN** 旧 `route_version` 的在途请求在新配置提交后结束
- **THEN** 它只能更新旧版本 Redis 命名空间和自己的历史使用事实
- **THEN** 它 MUST NOT 覆盖新版本粘性、熔断或评分快照

#### Scenario: 临时状态长期无流量
- **WHEN** 某会话、熔断作用域、探测租约或旧评分快照不再被访问
- **THEN** Redis MUST 按各自 TTL 自动回收
- **THEN** Redis Stream 和诊断缓存 MUST 受最大长度或容量限制

#### Scenario: Redis 路由状态不可用
- **WHEN** Redis 无法读取粘性或熔断状态
- **THEN** 本次请求 MAY 在用户候选内继续执行安全的当前请求故障转移
- **THEN** 系统 MUST 标记 degraded 并按现有容量控制的 fail-safe 语义处理
- **THEN** 系统 MUST NOT 把单实例内存状态描述为跨实例一致

### Requirement: 路由诊断写入必须异步批量化并具备背压
系统 SHALL 对普通成功使用聚合指标，对异常路由尝试使用有界异步事件管道和幂等批量落库。诊断写入不得成为模型响应的同步依赖。

#### Scenario: 大量普通首选成功
- **WHEN** 网关持续产生第一候选一次成功的请求
- **THEN** 路由指标 MUST 先在进程内按受控维度聚合并批量刷新
- **THEN** 系统 MUST NOT 每请求同步写入 PostgreSQL 路由指标行

#### Scenario: 请求发生分组切换
- **WHEN** 请求发生分组切换、熔断转换、半开探测、全部失败或部分计费
- **THEN** 系统 SHOULD 把带稳定 `event_id` 的异常事件写入 Redis Stream
- **THEN** 消费者 MUST 批量持久化并在提交成功后 ACK
- **THEN** 重投递 MUST 由数据库唯一约束保持幂等

#### Scenario: 诊断队列出现积压
- **WHEN** Redis Stream 或进程内队列达到背压阈值
- **THEN** 系统 MUST 首先停止普通成功采样
- **THEN** 系统 MUST 优先保留失败、熔断、部分计费和财务相关事件
- **THEN** 路由主请求 MUST NOT 因非关键诊断事件阻塞
- **THEN** 系统 MUST 输出队列深度、消费延迟和 dropped 指标

#### Scenario: 指标基数控制
- **WHEN** 系统导出 Prometheus 路由指标
- **THEN** 标签 MUST 使用受控平台、模型族、端点、策略、分组和错误枚举
- **THEN** `api_key_id`、`request_id` 和 `session_hash` MUST NOT 成为常规指标标签

### Requirement: 路由行为必须可观测和可审计
系统 SHALL 为路由决定、候选排除、分组切换、熔断变化、恢复探测、倍率估算误差和缓存补偿提供指标与脱敏事件。

#### Scenario: 管理员诊断一次故障转移
- **WHEN** 管理员使用 request ID 查询请求
- **THEN** 系统 SHOULD 展示路由版本、配置候选、排除原因、尝试顺序、标准化错误和最终实际分组
- **THEN** 结果 MUST NOT 包含 API Key 明文、账号凭据或完整请求正文

#### Scenario: 智能评分版本变化
- **WHEN** 管理员修改可靠性门槛或策略权重
- **THEN** 新决策 MUST 引用新的策略版本
- **THEN** 历史路由尝试 MUST 保留当时评分快照或可解释字段

### Requirement: 持续优化必须服从用户偏好和不可学习硬边界
系统 SHALL 只在用户选择的偏好 envelope 和通过硬过滤的候选中优化新会话排序。策略、评分、实验或模型版本不得修改候选白名单、绕过 50% 熔断、破坏健康粘性、改变重试安全或重写实际分组计费。

#### Scenario: 预测模型把集合外分组评为最优
- **WHEN** 当前模型或候选策略对一个不在 Key 路由集合中的分组给出最高效用
- **THEN** 规则引擎 MUST 在评分前排除该分组
- **THEN** 路由决定 MUST NOT 包含或访问该分组

#### Scenario: 模型预测硬熔断分组已经恢复
- **WHEN** 分组的实际路由熔断状态为 `OPEN` 且原始有效窗口成功率低于 50%，但预测成功率高于门槛
- **THEN** 普通请求 MUST NOT 进入该分组
- **THEN** 只有第 13 节定义的受限半开探测可以验证恢复

#### Scenario: 自动调参越过用户偏好范围
- **WHEN** 为价格优先生成的候选版本把速度权重提高到该偏好 envelope 之外
- **THEN** 策略注册和发布校验 MUST 拒绝该版本
- **THEN** 当前 active 版本 MUST 保持不变

#### Scenario: 评分或模型版本在活跃会话期间更新
- **WHEN** active `score_version` 或 `model_version` 已更新而会话仍有健康粘性
- **THEN** 后续请求 MUST 继续使用粘性分组
- **THEN** 版本更新 MUST NOT 递增该 Key 的 `route_version` 或批量删除粘性
- **THEN** 没有有效粘性的新会话才使用新版本排序

### Requirement: 决策事实必须支持时点回放和无偏差分析
系统 SHALL 以有界异步事件记录决策时点候选、特征、排名、选择、版本和实际结果，并明确保存采样概率与可观测范围。历史特征不可被后续状态反向改写，未选择候选不可伪造结果标签。

#### Scenario: 普通智能新会话命中稳定采样
- **WHEN** 一个第一候选成功的智能新会话按 `routing_decision_id` 稳定哈希命中采样
- **THEN** 事件 MUST 保存当时全部有界候选的特征、置信度、分项得分、排名、选择和版本
- **THEN** 事件 MUST 保存准确 `sample_probability`
- **THEN** 请求热路径 MUST NOT 为每个候选同步插入数据库

#### Scenario: 实验或探索决定产生结果
- **WHEN** 新会话命中 shadow 关联、canary 或受控探索
- **THEN** 系统 MUST 保存实验分桶、baseline/candidate 版本和完整候选上下文
- **THEN** 实际探索必须保存被选动作的准确 `action_propensity`
- **THEN** 事件保留期 MUST 覆盖实验评估窗口

#### Scenario: 候选没有被实际尝试
- **WHEN** 一个候选只参与评分但请求在更高排名分组成功
- **THEN** 系统 MAY 保存该候选的时点特征和预测
- **THEN** 系统 MUST 将其结果标记为未观测
- **THEN** 训练管道 MUST NOT 把它自动标为成功或失败

#### Scenario: 历史决定后价格和特征口径变化
- **WHEN** 决策完成后分组价格、缓存统计或特征算法发生变化
- **THEN** 历史事实 MUST 保留原 `feature_schema_version` 和决策时点数值
- **THEN** 离线数据集 MUST 使用 point-in-time join 或原快照，不能引入未来数据

#### Scenario: 决策输入包含用户内容
- **WHEN** 系统构建训练或回放事件
- **THEN** 事件 MUST NOT 包含提示词、响应正文、API Key 明文或上游凭据
- **THEN** 请求特征 MUST 使用经过批准的枚举、数值或分桶字段

### Requirement: 策略版本必须具备不可变生命周期和原子回滚
系统 SHALL 在 PostgreSQL 注册不可变策略、特征、实验和模型版本，通过 Redis 原子指针发布，通过网关本地不可变对象执行。候选版本必须经过数据检查、离线评估、shadow 和 canary，任一硬护栏越界时可以无状态切回安全 baseline。

#### Scenario: 离线优化器生成新权重
- **WHEN** 离线任务根据新增历史数据生成一组权重或阈值
- **THEN** 系统 MUST 创建新的 `draft strategy_version`
- **THEN** 该任务 MUST NOT 直接修改 active 版本或生产路由

#### Scenario: shadow 版本参与比较
- **WHEN** 一个 `shadow` 策略为真实请求计算候选顺序
- **THEN** shadow 决定 MUST 与实际决定通过同一 `routing_decision_id` 关联
- **THEN** shadow MUST NOT 获取容量租约、写粘性、改变实际分组或参与计费

#### Scenario: 发布完整的新策略版本
- **WHEN** 候选版本通过校验并被批准进入 canary 或 active
- **THEN** 系统 MUST 先写完不可变版本和依赖快照，再原子切换 Redis 指针
- **THEN** 网关 MUST 在 schema、checksum 和依赖校验通过后整体替换本地对象
- **THEN** 请求 MUST 不会组合半新半旧的策略、特征和模型版本

#### Scenario: canary 硬护栏恶化
- **WHEN** canary 的最终成功率、P95/P99、成本、切换率或冷缓存率越过配置停止条件
- **THEN** 系统 MUST 自动停止扩量并原子回到安全 baseline
- **THEN** 回滚 MUST NOT 递增用户 `route_version`、删除健康粘性或改写历史决定
- **THEN** 停止原因和版本差异 MUST 可审计

#### Scenario: 网关无法加载候选版本
- **WHEN** 特征 schema 不兼容、模型 checksum 失败、推理对象异常或加载超时
- **THEN** 网关 MUST 继续使用已知安全的确定性版本
- **THEN** 请求 MUST NOT 为等待版本加载而查询历史数据库或远程推理服务
- **THEN** 系统 MUST 输出版本加载和 fallback 指标

### Requirement: 个性化优化必须分层收缩并受样本置信度约束
系统 SHALL 从平台/模型/端点基线、物理分组表现逐步叠加用户群体和 Key 有界残差。低样本个体必须向共享基线收缩，个性化不得污染全局健康事实或越过用户偏好 envelope。

#### Scenario: 新建 Key 没有历史数据
- **WHEN** 新 Key 首次使用智能调度且没有个体历史
- **THEN** 系统 MUST 使用平台、模型、端点和物理分组共享基线
- **THEN** 同分和指标不可用时 MUST 使用用户配置顺序
- **THEN** 未知个体表现 MUST NOT 被解释为最优

#### Scenario: Key 积累充分且稳定的数据
- **WHEN** 一个 Key 在特定模型族、端点和输入结构上达到最小样本量与置信度
- **THEN** 系统 MAY 在用户偏好 envelope 内逐步启用 Key 级残差
- **THEN** 决策解释 MUST 能区分共享基线和个性化修正
- **THEN** 修正幅度 MUST 受版本化上限控制

#### Scenario: 个体数据变稀疏或发生漂移
- **WHEN** Key 近期样本不足、特征缺失或残差校准误差超过门槛
- **THEN** 个性化权重 MUST 向共享基线收缩或停用
- **THEN** 系统 MUST NOT 继续长期使用低置信度旧残差

#### Scenario: 单个 Key 发生异常流量
- **WHEN** 一个 Key 出现短期异常失败、超长输入或非典型缓存行为
- **THEN** 该数据 MAY 影响该 Key 的有界残差和 Key 级熔断
- **THEN** 在未满足全局样本和异常过滤条件前 MUST NOT 污染其他 Key 的个性化或全局分组结论

### Requirement: 动态策略必须抑制振荡、赢家锁定和相关故障误判
系统 SHALL 使用置信修正、平滑、最小分差、最小驻留、流量变化上限和依赖相关性控制动态排序。瞬时排名变化不得搬迁活跃会话或造成新会话流量无界震荡。

#### Scenario: 两个分组在相邻快照中小幅互换排名
- **WHEN** 新旧得分差低于配置的排名切换迟滞或尚未满足最小驻留时间
- **THEN** 系统 MUST 保持当前稳定的新会话首选顺序
- **THEN** 已有粘性会话 MUST 不受影响

#### Scenario: 当前赢家因新增流量出现容量下降
- **WHEN** 首选分组由于获得更多新会话而容量余量降低
- **THEN** 系统 MUST 以有界速度把新会话溢出到后续候选
- **THEN** 容量溢出 MUST NOT 计为健康失败
- **THEN** 容量恢复后 MUST 按迟滞逐步收敛而非立即抢回全部流量

#### Scenario: 多个候选共享同一故障域
- **WHEN** 候选分组共享账号、渠道、代理、区域或供应商依赖
- **THEN** 容量和最终成功概率估算 MUST 对共享依赖去重或加入相关性修正
- **THEN** 系统 MUST NOT 将这些候选错误视为完全独立的冗余容量

#### Scenario: 健康候选长期没有被选择
- **WHEN** 现有策略使一个仍有资格的候选长期缺少新结果
- **THEN** 系统 SHOULD 使用被动健康证据、无用户内容探测或受控探索更新置信度
- **THEN** 系统 MUST NOT 仅因历史选中次数少永久判定其质量差

### Requirement: 受控探索必须默认关闭且只作用于安全新会话
第一阶段系统 SHALL 默认使用确定性排序。未来启用探索时，探索只能在通过全部硬约束且处于用户偏好 envelope 内的候选中，对没有有效粘性的新会话使用稳定分桶和有界预算，并具备准确 propensity、自动停止和 baseline 回退。

#### Scenario: 活跃会话命中实验分桶
- **WHEN** 一个已有健康粘性的会话哈希落入探索或 canary 分桶
- **THEN** 会话 MUST 继续使用粘性分组
- **THEN** 系统 MUST NOT 为获得训练样本主动切组

#### Scenario: 探索候选价格超出用户策略护栏
- **WHEN** 一个候选通过基本健康检查但预估倍率超过当前偏好的探索上限
- **THEN** 探索策略 MUST 排除该候选
- **THEN** 探索预算 MUST NOT 覆盖用户偏好护栏

#### Scenario: 探索运行时质量恶化
- **WHEN** 探索桶的成功率、成本、延迟、切换或冷缓存触发停止条件
- **THEN** 系统 MUST 在有界时间内停止探索并回到 baseline
- **THEN** 已分配但尚未开始的新会话 MUST 使用停止后的安全版本

#### Scenario: 探索事件采集链路不健康
- **WHEN** 决策事件覆盖率不足、队列接近上限或消费者延迟超过实验阈值
- **THEN** 系统 MUST 暂停 canary/探索的新会话分配并使用 baseline
- **THEN** 路由主请求 MUST NOT 因实验事件写入而阻塞
- **THEN** 受影响时间窗的数据 MUST 标记为不完整且不得用于自动晋级

#### Scenario: 没有探索数据的离线回放
- **WHEN** 历史数据完全来自确定性旧策略且未记录有效动作概率
- **THEN** 离线评估 MAY 验证候选过滤、公式和已观测结果
- **THEN** 系统 MUST NOT 宣称准确估计未选择分组的反事实效果

### Requirement: 预测模型只能增强分项预测且必须有本地确定性兜底
未来模型 SHALL 预测成功概率、延迟分位数、容量溢出、缓存行为和归一化成本等分项结果；规则引擎继续负责硬过滤、偏好映射和最终排序。推理必须本地、有界、版本化，失败时立即使用确定性 baseline。

#### Scenario: 模型为候选生成结果预测
- **WHEN** 一个智能新会话使用启用模型的策略版本
- **THEN** 规则引擎 MUST 先执行候选硬过滤
- **THEN** 模型 MUST 只处理剩余最多 8 个候选的批准特征
- **THEN** 最终排序 MUST 由用户偏好目标和硬护栏确定

#### Scenario: 在线推理超过时间预算
- **WHEN** 本地模型推理超时、返回 NaN、缺少关键特征或发生异常
- **THEN** 请求 MUST 立即使用同策略的确定性评分版本
- **THEN** 系统 MUST NOT 调用远程模型或在线聚合 PostgreSQL 历史数据
- **THEN** fallback 原因和推理时长 MUST 被计量

#### Scenario: 新模型训练完成
- **WHEN** 训练流水线输出新的模型制品和离线指标
- **THEN** 系统 MUST 只注册一个带 lineage、schema 和 checksum 的 `draft model_version`
- **THEN** 新模型 MUST 经过 shadow 和 canary 后才能成为 active
- **THEN** 固定日期重训完成 MUST NOT 自动等同于生产发布

### Requirement: 策略晋级必须按偏好体验指标和共同成功护栏评估
系统 SHALL 分别按价格、速度和均衡偏好的目标评估候选版本，同时把最终成功率作为共同首要护栏。晋级结果必须包含样本量、置信区间、切片范围和观察周期，不能只依据总平均值。

#### Scenario: 价格版本降低平均倍率但成功率下降
- **WHEN** price canary 的每次成功成本改善但最终成功率跌破共同护栏
- **THEN** 候选版本 MUST NOT 晋级
- **THEN** 系统 MUST 停止扩量或回滚

#### Scenario: 速度版本缩短 TTFT 但价格越界
- **WHEN** speed canary 的 TTFT 改善但每次成功成本超过速度偏好的价格上限
- **THEN** 候选版本 MUST NOT 全量发布

#### Scenario: 全局平均改善但关键切片恶化
- **WHEN** 候选版本全局体验指标改善，但某个主要平台、模型族、端点或高流量输入桶显著恶化
- **THEN** 系统 MUST 按预设切片护栏阻止全量晋级或缩小适用范围
- **THEN** 评估报告 MUST 同时显示总体和关键切片结果

### Requirement: 只有用户选择多个分组时才启用调度控制
系统 SHALL 在表单选择两个及以上分组时显示调度方式、成功率熔断门槛及智能倾向控制；未选或只选一个分组时隐藏这些配置。后端 SHALL 根据请求硬过滤前配置中 `enabled=true` 的候选数量判断是否启用跨组控制，不能只依据当前可用候选数。

#### Scenario: 新建或编辑单分组 Key
- **WHEN** 表单只选择一个分组，包括历史保存过 smart/高成功率门槛的单分组 Key
- **THEN** 调度方式、熔断滑块、智能倾向及其评分解释 MUST 不显示
- **THEN** 保存 MUST 使用单组 sequential 配置，不提交隐藏的智能比例或成功率门槛，已有门槛不得因隐藏而被重置
- **THEN** 请求 MUST 使用选中组，不查询或更新 Key 的组级 sticky/breaker 状态，不因旧 OPEN 状态或此状态 Redis 故障而拒绝该组
- **THEN** 模型/端点兼容、分组启用状态、权限、订阅、额度检查和组内账号重试 MUST 继续执行；该组无法服务时返回相应错误，不扩展到未选分组
- **THEN** 实际组 usage/计费和失败统计 MUST 保留

#### Scenario: 表单从一个分组增加到两个或删回一个
- **WHEN** 用户加入第二个分组
- **THEN** 多分组控制 MUST 显示，新建默认成功率门槛为 80%，智能模式才显示价格/速度滑块
- **WHEN** 用户随后删回一个分组
- **THEN** 多分组控制 MUST 立即隐藏，保存不得悄悄提交隐藏的比例和门槛

#### Scenario: 多分组 Key 只剩一个候选通过请求过滤
- **WHEN** Key 配置中至少两个候选启用，但其中部分因权限、协议能力、分组停用或健康问题被排除
- **THEN** 剩余候选 MUST 继续遵守该 Key 的熔断和成功率门槛，不得误判为固定单分组并绕过保护

### Requirement: 多分组新特性必须通过用户 ID 灰度名单开放
系统 SHALL 在“系统设置 → 功能开关”提供多分组与智能调度灰度名单；名单仅对管理员可见，可搜索用户名/邮箱或按完整用户 ID 添加、移除并保存。该名单控制整个新特性集合：多分组、顺序/智能调度、成功率门槛与智能比例。系统 SHALL 同时要求服务器多分组总开关开启，不因管理员角色自动豁免名单限制。

#### Scenario: 空名单与指定用户开放
- **WHEN** 灰度设置尚不存在或管理员保存空名单
- **THEN** 所有用户 MUST 保持旧版单分组体验，不自动纳入现有用户或管理员
- **WHEN** 管理员保存用户 ID 名单
- **THEN** 系统 MUST 校验正整数、用户存在且未删除，去重排序并限制最多 1000 个 ID；省略字段或传 null 不得被解释为清空名单
- **THEN** 仅名单内用户且总开关开启时 MUST 获得新功能能力；名单外不显示候选组编辑器、调度、熔断、智能倾向或列表智能标签

#### Scenario: 直接调用 API 不得绕过灰度
- **WHEN** 名单外用户调用 Key 创建/更新 API 提交多个候选组、smart 模式、智能偏好/比例或成功率门槛
- **THEN** 服务端 MUST 返回 `403 API_KEY_ROUTING_NOT_ENABLED`，以认证用户身份判断，不接受请求体或查询参数中的伪造用户 ID
- **THEN** 原来的单分组创建/改组及名称、状态、额度等普通编辑 MUST 继续可用
- **THEN** 认证后的 `GET /api/v1/keys/routing-capabilities` MUST 只返回当前用户的布尔能力，不返回名单；未认证请求 MUST 被拒绝

#### Scenario: 移出名单时保留配置并回退原主分组
- **WHEN** 存量多分组 Key 的拥有者不再属于灰度名单
- **THEN** 后续请求 MUST 以请求局部副本固定使用原 `group_id`，不按健康状态选择备用组，不运行组间智能排序、熔断或粘性读写
- **THEN** 原主组不合法、停用、无权限或不支持请求时 MUST 保持硬检查失败，不得转成无限定 Key 或扩展到其他组
- **THEN** 系统 MUST 保留原候选组、模式、比例、门槛与版本；名单外仅改名等普通编辑且未主动换组时不得覆盖这些配置
- **THEN** 已开始的流式请求 MUST 沿用已冻结的计划；重新加入名单可恢复尚未被用户主动修改的配置
- **THEN** 管理员界面 MUST 提示回退主组可能造成冷缓存，建议小批量操作；撤销灰度属于管理性回退，不视为普通健康排序迁移

#### Scenario: 缓存、跨实例生效与失败关闭
- **WHEN** 系统判断用户灰度资格
- **THEN** 持久化 MUST 复用 PostgreSQL settings 中的独立 JSON 设置，进程内使用不可变 ID 集合与原子缓存，命中为 O(1)，不访问 Redis/PostgreSQL
- **THEN** 普通单分组网关请求 MUST 不额外查询灰度设置；多分组资格冷读/过期刷新 MUST 按实例合并，TTL 为 5 秒，数据库查询超时 1 秒
- **THEN** 保存完成后本实例 MUST 立即使用新缓存，其他实例在旧 TTL 过期后刷新；设置缺失、损坏或刷新失败 MUST 回退旧版单组路径，不无限使用陈旧的允许资格
- **THEN** 刷新与保存 MUST 防止迟到的旧读覆盖新名单；不得把资格固化进长寿命鉴权缓存，也不得因改名单批量删除路由粘性或重写 Key

#### Scenario: 多实例首次发布灰度控制
- **WHEN** 部署此灰度门禁
- **THEN** 操作手册 MUST 明确空名单默认关闭和旧版本不识别名单的风险；所有服务实例升级并验证名单外路径之后才能添加试用用户
- **THEN** 用户名单灰度 MUST 独立于评分策略的 shadow/canary 实验，不自动启动持续优化或扩大实验流量

### Requirement: 发布必须保持单分组兼容和可回滚
系统 SHALL 通过幂等回填、兼容字段镜像和功能开关发布多分组能力。关闭功能或回滚旧版本时，存量单分组 Key 必须继续工作。

#### Scenario: 数据迁移存量 Key
- **WHEN** 迁移处理一个仅有旧 `group_id` 的 Key
- **THEN** 系统 MUST 创建一个对应优先级 0 的路由
- **THEN** 调度模式 MUST 为 `sequential`
- **THEN** 重复执行迁移 MUST 不产生重复路由

#### Scenario: 功能开关关闭
- **WHEN** 多分组调度功能未对某用户启用
- **THEN** 单分组 Key MUST 继续走现有调度链路
- **THEN** 服务端 MUST 拒绝创建新的多分组配置，但不得破坏已保存数据

#### Scenario: 回滚到只认识 group_id 的版本
- **WHEN** 部署回滚发生在兼容窗口内
- **THEN** 旧版本 MUST 能使用镜像的首选 `group_id` 继续提供服务
- **THEN** 多分组明细 MUST 保留，以便重新升级后恢复

## 23. 验收清单

- [ ] 存量 Key 迁移后行为、账单和使用记录不变。
- [ ] 单分组新 Key 与当前逻辑完全等价。
- [ ] 跨平台、跨计费类型、重复分组、非法顺序和越权分组均被拒绝。
- [ ] 路由集合原子更新，版本冲突不会覆盖其他编辑。
- [ ] 顺序模式先执行分组内账号切换，再按配置顺序切换物理分组。
- [ ] Key 级分组访问成功率低于 50% 时立即熔断，恰好 50% 时不因该规则开路。
- [ ] 首次完整分组访问失败可直接触发 Key 级熔断，但单账号失败后同组成功不会触发。
- [ ] 全局 50% 熔断在达到全局最小样本量后生效，不会被单 Key 小样本放大。
- [ ] 智能模式三种评分策略共用相同维度和成功率硬门槛，只改变权重。
- [ ] 智能模式按总分动态排序并优先尝试第一名，不做按分数随机分流。
- [ ] 价格维度按窗口总 token 计算缓存命中率，不平均请求级百分比。
- [ ] 价格维度用当前价格重放普通输入、5m/1h 缓存创建、缓存读取和输出结构。
- [ ] 归一化有效倍率以全部逻辑输入 100% 缓存命中为基准，并保留输出成本。
- [ ] 名义倍率更低但缓存实际成本更高的分组，不会在价格评分中被错误判定为更便宜。
- [ ] 故障转移造成的首次冷缓存和人工补偿不会污染稳态价格评分。
- [ ] 价格样本不足时按规定保守回退并显示低置信度。
- [ ] 小样本 100% 成功率不会获得主流量。
- [ ] PostgreSQL 是路由配置、最终使用、账单和长期审计事实的唯一权威来源。
- [ ] 路由配置和失效 outbox 同事务提交，Redis/PubSub 失败可幂等重试并最终收敛。
- [ ] 分组禁用和用户撤权通过依赖 guard 在同一 Redis 批量读取中阻止陈旧快照继续授权。
- [ ] Redis 清空或进程重启不会丢失路由配置和财务事实，临时状态可自动重建。
- [ ] L1 与本地评分命中时新增路由逻辑执行 0 次 PostgreSQL 查询。
- [ ] L1/L2 冷启动使用 singleflight 和一次有界预加载，不产生候选分组 N+1 查询。
- [ ] 最多 8 个候选的粘性和熔断状态通过单次 Redis 批量读取取得。
- [ ] 正常一次成功的新增路由状态写入不超过一次 Redis 往返，且不同步插入路由尝试明细。
- [ ] Builder 只读取增量聚合桶，评分快照先写版本键再原子切换指针。
- [ ] 路由表反向索引、尝试表幂等/诊断索引、聚合桶时间索引和 outbox 待投递索引覆盖实际查询计划。
- [ ] 大型路由尝试表和聚合桶按时间分区或等价归档，清理不会形成在线长事务。
- [ ] 共享评分不按用户或 Key 复制，用户倍率只在本地候选集合计算。
- [ ] Redis Lua 键通过 hash tag 兼容 Cluster，旧 `route_version` 请求无法污染新状态。
- [ ] 所有粘性、熔断、探测、快照和 Stream 都有 TTL 或容量上限。
- [ ] 诊断事件积压时普通采样先降级，模型响应和关键财务记录不被阻塞或丢弃。
- [ ] 8 分组相对单分组的新增路由 P50/P95/P99 开销经过压测并满足发布门槛。
- [ ] 历史快照缺失或过期时只在用户配置集合内顺序退化。
- [ ] 实时容量溢出不会污染健康失败率。
- [ ] 共享底层账号不会被重复计算容量。
- [ ] 一个会话触发的合格分组故障能让同 Key 新请求在窗口期内避开该分组。
- [ ] 多实例同时半开时只有受限探测请求进入恢复分组。
- [ ] 恢复后新会话渐进回切，活跃备用会话保持粘性直至自然排水。
- [ ] 更改路由配置后旧粘性和熔断状态不影响新版本。
- [ ] 已输出语义流后不会跨分组重放。
- [ ] 媒体和异步任务在接收状态不明时不会重复创建。
- [ ] `previous_response_id` 等账号绑定上下文不会被错误迁移。
- [ ] 成功使用记录的 `group_id` 始终等于实际成功分组。
- [ ] 供应商真实用量与补偿后的用户计费用量分别保存。
- [ ] 预估倍率按模型、用户倍率和预测分配计算，并显示置信度。
- [ ] 权限或分组状态变化后缓存及时失效。
- [ ] 全部候选不可用时返回稳定错误，绝不访问集合外分组。
- [ ] 可通过 request ID 还原完整脱敏路由链路。
- [ ] 智能新会话采样事实能重建决策时点的完整有界候选、特征、得分、排名、选择、版本和实际结果。
- [ ] 普通成功、异常、实验和探索使用不同保留优先级；积压时关键结果和实验事实优先保留。
- [ ] 每条采样事件保存准确 `sample_probability`，探索事件保存准确 `action_propensity`，未选择候选保持未观测状态。
- [ ] 历史训练数据使用 point-in-time 特征，不会被后续价格、健康或特征口径污染。
- [ ] 路由训练和回放数据不包含提示词、响应正文、API Key 明文或供应商凭据。
- [ ] `route_version` 与策略、评分、特征、模型和实验版本独立；策略升级和回滚不会使粘性或缓存批量失效。
- [ ] 策略版本不可变，并支持 draft、shadow、canary、active、paused、retired 生命周期和原子 active 指针。
- [ ] shadow 只计算不执行，不获取容量、不写粘性且不影响账单。
- [ ] canary 使用稳定分桶且只作用于新会话，硬护栏越界可自动回到确定性 baseline。
- [ ] 动态排序具备置信修正、迟滞、最小驻留和流量变化上限，不会因相邻快照小幅波动频繁翻转。
- [ ] 共享故障域的候选在容量和最终成功概率中被去重或相关性修正。
- [ ] 新 Key 使用共享基线，个体数据充分后才启用有界残差，数据稀疏或漂移时自动收缩。
- [ ] 三种偏好的版本评估都以最终成功率为共同护栏，并分别验收每次成功成本、到成功耗时或综合体验损失。
- [ ] 模型只增强分项预测，推理超时、异常或 schema 不兼容时在本地无损回到确定性评分。
- [ ] 受控探索默认关闭；未来启用时不作用于活跃粘性会话、不越过硬过滤和偏好 envelope，并具备预算、propensity 和自动停止。
- [ ] 功能关闭和版本回滚时单分组 Key 仍可正常使用。

## 24. 待实现前确认的参数

以下是运维参数，不改变本规范的产品语义，实施前由压测和生产基线确定：

- 各平台/模型族高于或等于固定 50% 硬熔断线的智能准入阈值，以及全局健康熔断的最小样本量。
- 5 分钟、1 小时、24 小时窗口的组合方式和置信度算法。
- 连续失败开路阈值、初始冷却时间、最大退避时间和恢复观察窗。
- `RECOVERING` 新会话放量阶梯。
- 三种智能评分策略的最终权重、各维度归一化方式和容量变化重排阈值。
- 三种偏好的权重 envelope、共同成功率护栏、价格/延迟上限和体验损失归一化方式。
- 排名切换最小分差、最小驻留时间、单轮排名变化和新会话流量变化上限。
- 价格窗口最小请求数、最小逻辑输入 token 数、异常值处理和从 1 小时回退到 24 小时的阈值。
- API Key 路由 L1/L2 TTL、抖动比例、负缓存 TTL 和 singleflight 超时。
- 评分快照刷新周期、Redis TTL、stale grace、Builder 接管时间和最大快照尺寸。
- Redis 路由状态单次读写超时、熔断键 TTL、Stream 最大长度和积压阈值。
- 诊断事件批量条数、最长等待时间、普通成功采样率和 PostgreSQL 保留周期。
- 智能新会话决策样本率、异常/实验/探索事件保留期、最低样本覆盖率和事件丢失告警阈值。
- 缓存失效 outbox 的重试退避、最大积压时间、死信阈值和清理周期。
- Builder/事件消费者的独立数据库连接预算、查询超时、分区周期和归档批量。
- 测试与生产环境允许的新增路由 P95/P99 延迟、Redis 错误率和降级率门槛。
- 路由粘性 TTL；默认建议与现有账号粘性保持 1 小时。
- 冷缓存补偿的时间、次数和 token 上限。
- 路由尝试事实和高基数指标的保留周期。
- 第一阶段各协议与端点的支持矩阵。
- 策略 draft/shadow/canary/active 的晋级条件、最小观察周期、稳定分桶比例和自动回滚阈值。
- 个性化残差的最小样本量、收缩强度、最大修正幅度、过期时间和漂移停用条件。
- 模型制品大小、checksum/签名策略、本地加载超时、单请求推理 CPU/P95 预算和 fallback 门槛。
- 若未来启用探索：全局与 Key 级预算、每日上限、偏好 envelope、最小 propensity 和自动停止条件。
