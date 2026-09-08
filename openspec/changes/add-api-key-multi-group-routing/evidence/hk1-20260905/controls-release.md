# hk1 双滑块扩展发布

时间：2026-09-05 10:20–10:23（Asia/Shanghai）。用户明确授权部署 hk1 测试环境；us02 正式环境未操作。

## 制品与回滚

- 本地 `docker buildx build --platform linux/amd64 --load` 完成前端 pnpm 9 frozen-lockfile、Vue/TypeScript/Vite 和 Go embed 构建，服务器只加载镜像，不进行构建。
- 镜像：`sub2api:v20260905.1016`，ID `sha256:43d0449bbf002e5e8af4827aa109236a12a4b5a7325a6f915487c78eabef5da2`。
- 工作树制品基于 HEAD `2369bd2d5656787edf825a1c885b7190a1aed7d6`，包含未提交的路由改动；不是已提交正式发布版本。
- 传输归档 SHA-256：`eeda5f61a5a62090fa3fd4879808595df6d350065de8cfb319365ca1cc37d74e`。远端加载前核验，加载后架构和镜像 ID 一致。
- 旧镜像 `sub2api:v20260905.0903` 保留；远端备份目录 `/root/sub2api-test/backups/controls-release-20260905.6hSeB6` 保存原 Compose、旧镜像引用、数据库 dump、部署/验证脚本和新镜像归档。
- 数据库 dump 约 100 MiB，`pg_restore --list` 成功；SHA-256：`d69d68ed739240f2548c2e6405257d963020628103054207a2b029b46fa59c8a`。
- Compose 只替换应用 image，使用 `up -d --no-deps --no-build --pull never sub2api`。没有重启 PostgreSQL/Redis，没有清理或修改用户分组、账号和 Key。回退应用镜像时保留增量列，不执行破坏性逆迁移。

## 已核验

- `sub2api-test` 运行新镜像，`healthy`，`RestartCount=0`；`/health` 返回 `{"status":"ok"}`，根页面 HTTP 200。
- 迁移 `248_api_key_routing_controls.sql` 于 10:20:11 执行；3 个 Key 控制字段 CHECK 存在，`api_key_routing_controls_guard` 触发器启用。
- 发布前后活动数据量均为用户 2、Key 8、分组 3、账号 3。8 个旧 Key 均保持 nullable legacy 比例、默认门槛 50，且运行时状态版本等于原路由版本。
- 根页面入口 `/assets/index-BqEpWu8h.js`；实际可下载的 `/assets/KeysView-C4Ak_rex.js` 包含双滑块组件、`smart_balance_bps` 和 `routing_min_success_rate`，CSS `/assets/KeysView-DRrZLNXq.css` 包含滑块样式。
- KeysView JS SHA-256：`bf76de3c1537be2d372c819d5f9bb8b0c2de69b30ede59532f89c11ac79bd58a`；CSS：`613398d95420153d657a70a96daa7b755f89bde02cdc4323297048775458f172`。
- 管理端只读路由解释返回 `smart_balance_bps`、`routing_min_success_rate`、`routing_state_version`；运行时指标可读，后台专用数据库连接数 1（上限 2）。当前没有旧 Key 绑定到未删除分组的启用候选，抽样解释候选数为 0，因此此项只证明配置接口可读，不证明实际路由成功。凭据仅在校验进程内使用，未写入报告。
- 多分组开关保持 `true`；optimization、personalization、model prediction、exploration 均保持 `false`，未扩大实验范围。
- PostgreSQL 和 Redis 健康，启动时间均仍为 2026-08-27，重启数 0；新容器检查窗口内未发现 panic/fatal/迁移失败。

## 验证边界

此次发布采用只读冒烟，没有修改用户已有测试 Key、制造失败或调用付费上游。自定义比例/门槛的真实保存回显及新门槛下故障转移/恢复尚未在 hk1 做完整端到端演练（任务 14.8b）。本记录不替代已有 shadow/canary、压测及长期观察门禁。

## 10:33 滑条高度减半更新

- 用户再次明确授权：高度减少一半并部署 hk1。轨道及可见/原生圆形滑块均由 40px 减为 20px，刻度缩小，横向端点内缩由 20px 改为 10px；透明点击区域保持 40px。调度算法、字段、门槛和数据库迁移均无改动。
- 组件/KeysView 共 27 项测试、定向 ESLint 和 Docker 内 TypeScript/Vite/Go release 构建通过；IAB 实测两条轨道/滑块均为 20px，输入点击区域为 40px，鼠标拖至速度 100%、键盘 Home 回价格 100%、门槛 End/ArrowLeft 至 90% 均通过，并检查亮/深色截图。
- 本地构建并部署 `sub2api:v20260905.1028`，镜像 ID `sha256:7834d92112fb45a3ce0ab9ab52ec0263633cbac7c038fb1d4d06f0ffb4e06f20`；传输归档 SHA-256 `a5b4080d23c77aaaaf5271d7580ea2c36c2e6fb730783c25cbf4c6301a723cc0`。
- 发布备份目录 `/root/sub2api-test/backups/compact-slider-20260905.0xEAF8` 保存原配置/镜像引用、归档和校验结果，前版 `v20260905.1016` 保留供回滚。没有新增迁移，不修改业务数据；PostgreSQL/Redis 未重启，us02 未操作。
- 在线入口 `/assets/index-mHNFqsic.js`，KeysView JS `/assets/KeysView-BmBUYf5W.js`，CSS `/assets/KeysView-C_9HZjTd.css`。远端获取的两种浏览器 thumb CSS 均确认 `width:20px;height:20px`；JS 包含紧凑轨道和保留点击区域的类名。
- 新容器 `healthy`、重启次数 0，`/health` 返回 `{"status":"ok"}`，容器版本命令与新镜像一致。

## 10:59 新建默认门槛 80% 发布

- 用户明确授权后，本地构建 `linux/amd64` 镜像 `sub2api:v20260905.1054`，前端 pnpm 9 / TypeScript / Vite 和 Go embed 发布构建通过；服务器仅接收并加载镜像。
- 镜像 ID：`sha256:3d44f28a018efd33dc3804dcba05870647695ec21cb4e1478eec89d48906b00f`；传输归档 SHA-256：`43ed088d78395e4844ef13783f05622beb252faf9a8278f69fcb86b5046ad098`，远端加载前后核验一致。
- 发布备份目录：`/root/sub2api-test/backups/default80-release-20260905.4v6ghr`。保留旧 Compose、`v20260905.1028` 镜像引用、Key 控制字段快照、约 100 MiB 数据库 dump、发布/校验脚本与校验 JSON。dump 的 `pg_restore --list` 通过，SHA-256 为 `0df13b85bb4f62b77412b55392f575b760db5df0b39238459446697e1260119c`。
- 只替换应用镜像，未重启 PostgreSQL/Redis，未修改其他开关或业务数据，us02 未操作。
- `249_api_key_default_success_threshold.sql` 于 10:58:20 执行；`api_keys.routing_min_success_rate` 数据库默认值为 80，历史 `routing_attempts` 默认值仍为 50。
- 发布前全部 25 条 Key（含软删除）的门槛、配置版本、运行时状态版本与发布后逐条一致；活动用户/Key/分组/账号数为 2/8/3/3。没有为冒烟新建或修改业务 Key，没有调用付费上游。
- 新根页面入口 `/assets/index-Cq8xfWve.js`；线上 `/assets/KeysView-DOJ1WEC6.js` 中新建和重置共用 80 常量、旧字段缺失回显仍为 50。JS SHA-256：`eeff92f4e16a5aefaf80574401438a29211b14e1dcb3c4e444a7c322f249808b`。
- 精简滑块 CSS `/assets/KeysView-C_9HZjTd.css` 保持 20px；SHA-256：`49721a013bb5825607eec900dfdd44d6101a168e533d8a3d37ed4026472eadea`。
- 应用健康、重启次数 0、`/health` 返回 `{"status":"ok"}`，容器 `--version` 为 `v20260905.1054`；检查窗口内没有 panic/fatal/迁移失败。PostgreSQL/Redis 健康，启动时间仍为 2026-08-27。

## 11:58 单分组固定路由发布

- 用户明确授权部署 hk1。本地 `docker buildx build --platform linux/amd64 --load` 完成 pnpm 9 frozen-lockfile、TypeScript/Vite 与 Go embed 发布构建，服务器只加载制品。镜像为 `sub2api:v20260905.1153`，基于 HEAD `2369bd2d5656787edf825a1c885b7190a1aed7d6` 加当前未提交改动，版本明确标记 `dirty`。
- 镜像 ID：`sha256:2c478906b99e1e665f7707848efecf06bb0ded17a383e44a68cbb6e304ef9a11`，架构 `linux/amd64`。传输归档 SHA-256：`ee6a04d2d2510200ac86cb5e0ed622748097f8636e014c23a3d0afb441d27c33`；远端加载前后校验一致。
- 发布备份目录：`/root/sub2api-test/backups/single-group-release-20260905.cE96oW`，权限 700。保存原 Compose/环境文件、旧镜像引用、Key 配置/迁移/依赖快照、部署/核验脚本与 `verification.json`；旧镜像 `v20260905.1054` 保留。约 100 MiB 数据库 dump 经 `pg_restore --list` 校验通过，SHA-256：`9c1f564c6bd3c305220a754e134ad5324cda7555cf231fa86a3a69cd3ee7e5d0`。
- 展开 Compose 后逐字段比较，唯一变更为应用 image；以 `up -d --no-deps --no-build --pull never sub2api` 更新应用，并设置启动健康失败时恢复旧 Compose/镜像的回退路径。没有数据库迁移或业务数据写入，不重启 PostgreSQL/Redis，us02 未操作。
- `sub2api-test` 运行新镜像且 `--version` 一致，`healthy`、重启次数 0；`/health` 返回 `{"status":"ok"}`，根页面与前端资源 HTTP 200。启动检查窗口内 panic/fatal/迁移失败匹配数均为 0。
- 在线入口 `/assets/index-BWGt2QQB.js`；KeysView JS `/assets/KeysView-NBMDTuMK.js`，SHA-256：`e0f942d8416d8a1aaf21fefdadf498ec19bc08ba6487687ea63e3f20787e8866`。编译产物验证多分组条件控制调度/门槛/智能面板，单组提交强制顺序模式且省略隐藏门槛；新建/重置默认仍为 80%。CSS `/assets/KeysView-C_9HZjTd.css` 的 20px 滑块保持，SHA-256：`49721a013bb5825607eec900dfdd44d6101a168e533d8a3d37ed4026472eadea`。
- 全部 25 条 Key（含软删除）的分组镜像、模式、偏好、比例、门槛、配置版本及运行时版本逐条一致；活动用户/Key/分组/账号数仍为 2/8/3/3，数据库默认门槛 80、迁移文件记录、运行环境变量哈希保持一致。PostgreSQL/Redis 均健康且重启次数 0，启动时间仍为 2026-08-27。
- 本地单组/多组边界、零 Redis 读写及失败事实测试、后端全量与定向 race、前端 39 项测试已通过；本次线上只做只读冒烟。当前 0 条 Key 绑定到启用且未删除的可用分组，因此没有执行真实单组/多组请求端到端测试；未为验证创建/修改用户 Key 或调用付费上游。
