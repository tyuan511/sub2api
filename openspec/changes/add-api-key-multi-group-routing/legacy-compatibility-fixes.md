# 单分组兼容性修复

本轮修复保留在 `codex/smart-routing`，工作目录为 `/Users/yuantang/code/sub2api-smart-routing`。此前一部分修复已包含在暂存开发进度的提交 `bb3b9a7e1` 中，本轮继续补齐隔离边界和回归测试。没有合并主分支或执行部署。

## 已修复

| 问题 | 修复后的行为 | 回归证据 |
| --- | --- | --- |
| 旧 Messages 异步计费只携带 `ForceCacheBilling` 入参，丢失 context 标记后按普通输入收费 | 入参继续决定旧账号切换的缓存计费；明确的跨组补偿仍受 token 和切换次数上限限制 | `TestGatewayLegacyForceCacheBillingBooleanRemainsAuthoritative` |
| 允许未分组 Key 调度时，被新增候选检查拒绝 | 未启用多组路由的请求沿用原 Key 和订阅，进入旧处理路径 | `TestGatewayLegacyRoutingPreservesUngroupedAndStickySelection`、`TestAPIKeyRouteSingleGroupPreservesLegacyRequestContext` |
| `/v1/usage` 丢弃额度耗尽的订阅，或触发订阅窗口维护 | 查询继续保留当前订阅及用量，不激活或重置计费窗口 | `TestAPIKeyLegacyRoutingUsageRetainsExhaustedSubscriptionWithoutMaintenance` |
| 固定分组的删除、停用、权限撤销响应变成候选不可用 503 | 恢复原鉴权顺序、HTTP 状态及响应体；Anthropic/OpenAI 和 Google 鉴权均覆盖 | `TestAPIKeyLegacyRoutingKeepsAuthResponses` |
| 普通 Key 的认证缓存额外访问路由版本 Redis，固定分组额外查询候选费率 | 单分组跳过路由版本守卫读取和候选费率/观测加载；多分组 Key 才进入路由控制 | `TestAPIKeyRoutingLegacyAuthAvoidsRoutingIO`、`TestAPIKeyRoutingSingleGroupSnapshotSkipsCandidateRateQuery` |
| 固定分组粘性预取被标记为分组 0，导致重复读取 Redis、丢失绑定 | 保存实际分组 ID；预取后即使 Redis 不可用，负载感知调度仍选择原绑定账号 | `TestGatewayLegacyRoutingPreservesUngroupedAndStickySelection` |
| 新补偿逻辑改变旧账号成本统计、OpenAI 旧请求费用和用量记录 | 额外成本重算限定于明确的跨组补偿；OpenAI 原账号级标记不触发新增折扣；无路由上下文时不写新增 actual/billable usage JSON | `TestGatewayLegacyForceCacheBillingBooleanRemainsAuthoritative`、`TestOpenAILegacyRoutingForceFlagDoesNotChangeBilling` |

同时验证了多分组订阅故障转移后 Key 与订阅指向同一实际分组、共享认证对象不被修改，以及缺失主分组的 Key 不会进入无分组账号池。

本轮新增恢复回流：最近 15 分钟有足够成功请求时，长期低成功率候选进入受控恢复状态；价格偏好下仅按稳定会话 hash 放行恢复子集，低价候选可以实际拿到新会话探测流量。恢复信号仍需达到用户 Key 自己的成功率门槛，并接入 Redis 的 `RECOVERING` 熔断阶段；近期查询失败不会阻断原有 1h/24h 快照发布。

## 验证

以下命令在该工作目录的 `backend` 下通过：

```sh
go test -tags unit ./internal/service ./internal/server/middleware ./internal/handler ./internal/repository ./internal/server/routes ./internal/config ./cmd/server ./migrations
```

八个包全部通过，其中 service 172.581 秒、handler 41.417 秒。最终针对性测试也通过，包含普通请求费用兼容性和原有 `TestOpenAIGatewayServiceRecordUsage_GroupFailoverCompensationKeepsActualUsageAndAuditsAmount`，验证隔离修复保留跨组补偿。

使用 Go 的只读 overlay，将七个相关生产源码文件映射到修复前快照后运行同一批回归测试，成功复现计费、额外缓存访问、订阅查询、错误响应、固定分组上下文和粘性预取问题。该对照没有回滚或改写工作目录文件。

测试日志：`/tmp/sub2api-routing-fix-full.log`、`/tmp/sub2api-routing-fix-final-targeted.log`。预期失败的修复前对照：`/tmp/sub2api-routing-fix-negative-control.log`。

## 验证范围

本轮确认的是上述请求行为及依赖访问边界。单元测试不等价于生产部署、真实负载测试或迁移执行验证；本分支尚未与暂停期间更新的 `main` 合并，最终合并版本需要重新验证。多组功能仍有共享数据库迁移、认证缓存结构和后台服务变更，不能把本轮通过解释为整个功能上线后的资源开销绝对为零。
