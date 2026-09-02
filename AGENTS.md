# Repository Agent Instructions

## Deployment environments

- 测试环境：`hk1`
- 正式环境：`us02`

## 镜像构建与部署

- 不要在服务器上打包或构建镜像。
- 必须在本地完成镜像构建，然后将镜像拷贝到目标服务器。
- 镜像 tag 使用 `vYYYYMMDD.HHMM` 格式，例如：`v20260831.0959`。
