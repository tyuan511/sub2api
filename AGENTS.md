# Repository Agent Instructions

## Deployment environments

- 测试环境：`hk1`
- 正式环境：`us02`

## 镜像构建与部署

- 不要在服务器上打包或构建镜像。
- 必须在本地完成镜像构建，然后将镜像拷贝到目标服务器。
- 镜像 tag 使用 `vYYYYMMDD.HHMM` 格式，例如：`v20260831.0959`。

<!-- CODEGRAPH_START -->
## CodeGraph

In repositories indexed by CodeGraph (a `.codegraph/` directory exists at the repo root), reach for it BEFORE grep/find or reading files when you need to understand or locate code:

- **MCP tool** (when available): `codegraph_explore` answers most code questions in one call — the relevant symbols' verbatim source plus the call paths between them, including dynamic-dispatch hops grep can't follow. Name a file or symbol in the query to read its current line-numbered source. If it's listed but deferred, load it by name via tool search.
- **Shell** (always works): `codegraph explore "<symbol names or question>"` prints the same output.

If there is no `.codegraph/` directory, skip CodeGraph entirely — indexing is the user's decision.
<!-- CODEGRAPH_END -->
