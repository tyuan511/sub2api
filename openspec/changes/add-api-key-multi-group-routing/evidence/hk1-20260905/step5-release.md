# hk1 智能倾向 5% 步长发布

时间：2026-09-05 14:55–14:56（Asia/Shanghai）。用户明确授权部署 hk1 并要求测试方案；us02 未操作。

## 构建与制品

- 本地构建 `linux/amd64`，镜像 `sub2api:v20260905.1447`，ID `sha256:f926272dbb7ea67d36c6b3fac01e0f22ce0fc0a5c6136ef01e21cb1cbf5c80bd`，47,096,588 bytes；服务器仅校验、加载和运行，无服务器构建。
- 工作树基于 `2369bd2d5656787edf825a1c885b7190a1aed7d6`，版本标记 `2369bd2d5656-dirty`；前端 pnpm 9 frozen-lockfile、TypeScript/Vite 与 Go embed 发布编译通过，60 项滑块/Key/名单卡片定向测试通过。
- 首次构建因工作区 `frontend/pnpm-lock.yaml` 有额外改动，缺失 package.json 的 overrides 契约而被 frozen-lockfile 拒绝，没有部署失败制品。保留工作区锁文件，在独立快照 `/tmp/sub2api-step5-source.oZcGTD` 中使用 HEAD/上一版本已验证的锁文件后重建，不移除安全 overrides、不做依赖升级、不使用 no-frozen-lockfile。
- 工作区锁文件 SHA-256 前后均为 `7e72911e51a324a20d9f0034b90c4b3e6793f90d0d8273872157f463f8a421cc`；构建锁文件为 `8dbd1876020e41b644d971414d29100c9f428f39ede953c03d0442b834f6f3af`。构建与工作区 KeysView 源码 SHA-256 一致：`2fc17e31289240ed64842f66f52e490f2ef1e36150db96960d756508f90d2b64`，均包含 500 bps 步长。
- 传输归档 SHA-256：`4fc8475a5669e346caf4ef89a52fc6d69b6ce1e82c1f9c025c42ffa675d694c5`，远端加载前校验、加载后架构/镜像 ID 校验通过。

## 备份与变更范围

- 备份目录 `/root/sub2api-test/backups/step5-release-20260905.G8RQjS`，权限 700，保留原 Compose/环境、旧镜像引用、数据库 dump、Key/候选/名单/迁移/依赖快照、发布与校验脚本、`verification.json`。
- 数据库 dump 经 `pg_restore --list` 校验，SHA-256：`ae5e59cafe9acd2ca4e9d594d64b66ba783c6aa7ceedb66f1031692ed2bed65e`。
- 旧镜像 `sub2api:v20260905.1431` 保留。展开 Compose 确认只有应用 image 变化，使用 `up -d --no-deps --no-build --pull never sub2api` 更新；健康失败回退路径已保留，本次未触发回滚。
- 未改动名单、用户 Key、分组或账号；没有新增迁移、环境变量变更、数据库/Redis 重启或付费上游调用。

## 上线后核验

- 新镜像运行、版本一致，`healthy`、重启次数 0；`/health` 返回 `{"status":"ok"}`，启动窗口 panic/fatal/迁移失败匹配为 0。
- 根页面入口 `/assets/index-C7RmM01H.js`。线上 `/assets/KeysView-BDTEoNwp.js` 明确包含 `min:0,max:1e4,step:500`，成功率门槛仍为 `min:50,max:95,step:5`；SHA-256：`7f6f79bb0fa8cc3afeda599bc1367ae241cb7ca4815150d4b134cf40eb90760d`。
- `/assets/SettingsView-DHgRgb_g.js` 灰度名单卡片资源可获取，SHA-256：`f76b81339a17bf705e788c7b50369eeda57a6abbee904144ec0ae57e76dbdf1d`。
- 管理员名单 GET 只读核验成功，当前 1 位用户，名单值与本次发布前完全一致（不是上一版发布时的空名单）；没有替用户添加/移除 ID。用户能力/管理员名单接口无认证请求均返回 401。
- 全部 25 条 Key（含软删除）配置和 42 条候选绑定逐条不变；活动用户/Key/分组/账号数保持 2/8/4/4；迁移记录、运行环境哈希保持。
- PostgreSQL/Redis 仍 healthy、重启 0，启动时间为 2026-08-27；多分组总开关仍为 true，灰度资格由现有名单控制。

## 验证边界与测试交付

本次是部署与只读冒烟，没有执行名单保存/撤销、修改测试 Key、故障注入或真实上游计费请求。上一轮本地浏览器已验证 50→55→50 的键盘步进、拖动吸附 65%、0/100 端点、重置与历史比例保持；这不等同 hk1 真实保存与路由端到端。

详细测试方案见 [hk1 测试方案](../../test-plan-hk1.md)，包含 31 项核心用例、快速验收顺序、权重/缓存验算、性能与安全停止门禁。需要指定测试用户/Key、隔离故障夹具和真实上游预算的项目保持待执行。
