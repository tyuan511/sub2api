# Repository Agent Instructions

## Deployment environments

- 测试环境：`hk1`
- 正式环境：`us02`

## 镜像构建与部署

- 不要在服务器上打包或构建镜像。
- 必须在本地完成镜像构建，然后将镜像拷贝到目标服务器。
- 镜像 tag 使用 `vYYYYMMDD.HHMM` 格式，例如：`v20260831.0959`。

## 发布必须带版本号

- 任何发布、部署、回滚动作都必须显式携带版本号，禁止只发布 `latest`。
- 镜像 tag 固定为 `vYYYYMMDD.HHMM`（以本地构建时刻为准），例如 `v20260831.0959`；一个 tag 只对应一次构建，已发布或已部署的 tag 不得复用、不得覆盖重建。
- 本地构建时同时打版本 tag 与 `latest`，并且**必须传 `VERSION` 构建参数**（否则应用内版本为空/回退旧值，管理界面侧边栏角标和 `/settings/public` 的 `version` 字段不会跟着发布走）：
  `docker build --build-arg VERSION=vYYYYMMDD.HHMM -t sub2api:vYYYYMMDD.HHMM -t sub2api:latest ...`
- 注意 `deploy/build_image.sh` 目前只产出 `sub2api:latest`；用它构建后必须补打版本 tag 再发布：
  `docker tag sub2api:latest sub2api:vYYYYMMDD.HHMM`
- 拷贝镜像到服务器、修改 `deploy/docker-compose*.yml` 的 `image:` 字段、执行部署或回滚命令时，一律引用明确的版本 tag，不要写 `latest`。
- **每次发布前必须核对版本号三处一致**（历史教训：只改 override 导致基础 compose 文件停留在几个月前的老版本）：
  1. 服务器 compose **基础文件**（如 us02 `/root/sub2api-deploy/docker-compose.yml`）的 `image:` 字段；
  2. compose **override 文件**（如 `/root/sub2api-deploy/docker-compose.override.yml`）的 `image:` 字段；
  3. 实际运行容器：`docker ps --format '{{.Names}} {{.Image}}' | grep sub2api`。
  两处 compose 的版本必须同步更新为本次发布 tag，禁止只改其中一处；发布前用 `docker compose config | grep -m1 'image: sub2api'` 确认最终生效版本。
- **发布后必须核对应用内版本与镜像 tag 一致**（接口无鉴权可直接验证）：`curl -s http://127.0.0.1:<端口>/api/v1/settings/public | grep -o '"version":"[^"]*"'` 应返回本次版本号。
- 发布完成后，交付说明必须写明本次版本号，并附上核对命令（如 `docker images | grep sub2api`）。

### 版本号的坑（2026-11-11 实际踩过）

- **镜像 tag ≠ 应用内版本号，是两套东西**。应用内版本靠构建时 `--build-arg VERSION` 经 ldflags（`-X main.Version`）注入 Go 二进制，展示在管理界面侧边栏角标和 `/api/v1/settings/public` 的 `version` 字段。只把镜像 tag 打对，界面版本不会变。
- **不传 `VERSION` 参数会静默回退到错误的值**：Dockerfile 此时调用 `scripts/resolve-version.sh` 兜底，但**该脚本在仓库里并不存在**，且 `.git` 被 `.dockerignore` 排除（容器里也没有 git 元数据可用），结果应用内版本为空或回退到 `backend/cmd/server/VERSION` 里的陈旧值（曾长期是 `0.2.19`）。没有任何构建报错，只有发布后核对接口才能发现——所以上面的「发布后核对应用内版本」是必做步骤，不可省略。
- **compose 有基础文件 + override 两个文件都带 `image:`**：只改 override 时容器虽然跑的新版本，但基础文件会停留在数月前的老 tag，查配置会被误导，不带 override 启动会静默回滚。两处必须同步改。
- 时间线记录：`v20261111.1544`～`v20261111.1612` 三次发布镜像 tag 全部正确，但界面版本号始终不对/不更新；`v20261111.1618` 传了 `--build-arg VERSION` 后才恢复一致。

<!-- CODEGRAPH_START -->
## CodeGraph

In repositories indexed by CodeGraph (a `.codegraph/` directory exists at the repo root), reach for it BEFORE grep/find or reading files when you need to understand or locate code:

- **MCP tool** (when available): `codegraph_explore` answers most code questions in one call — the relevant symbols' verbatim source plus the call paths between them, including dynamic-dispatch hops grep can't follow. Name a file or symbol in the query to read its current line-numbered source. If it's listed but deferred, load it by name via tool search.
- **Shell** (always works): `codegraph explore "<symbol names or question>"` prints the same output.

If there is no `.codegraph/` directory, skip CodeGraph entirely — indexing is the user's decision.
<!-- CODEGRAPH_END -->
