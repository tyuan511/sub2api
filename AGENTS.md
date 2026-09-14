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
- 本地构建时同时打版本 tag 与 `latest`：
  `docker build -t sub2api:vYYYYMMDD.HHMM -t sub2api:latest ...`
- 注意 `deploy/build_image.sh` 目前只产出 `sub2api:latest`；用它构建后必须补打版本 tag 再发布：
  `docker tag sub2api:latest sub2api:vYYYYMMDD.HHMM`
- 拷贝镜像到服务器、修改 `deploy/docker-compose*.yml` 的 `image:` 字段、执行部署或回滚命令时，一律引用明确的版本 tag，不要写 `latest`。
- 发布完成后，交付说明必须写明本次版本号，并附上核对命令（如 `docker images | grep sub2api`）。

<!-- CODEGRAPH_START -->
## CodeGraph

In repositories indexed by CodeGraph (a `.codegraph/` directory exists at the repo root), reach for it BEFORE grep/find or reading files when you need to understand or locate code:

- **MCP tool** (when available): `codegraph_explore` answers most code questions in one call — the relevant symbols' verbatim source plus the call paths between them, including dynamic-dispatch hops grep can't follow. Name a file or symbol in the query to read its current line-numbered source. If it's listed but deferred, load it by name via tool search.
- **Shell** (always works): `codegraph explore "<symbol names or question>"` prints the same output.

If there is no `.codegraph/` directory, skip CodeGraph entirely — indexing is the user's decision.
<!-- CODEGRAPH_END -->
