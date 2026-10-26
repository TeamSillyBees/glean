# 贡献指南

本仓库是可直接运行的 Go 服务模板。修改前阅读 README、AGENTS 和相关代码；基础能力与业务源码均由当前项目维护。

保持改动聚焦，保护工作区已有修改。业务模块使用 flat package，遵循现有构造函数、错误门面和测试模式；面向开发者的文档和注释优先使用中文。

## 验证

```powershell
go test ./... -count=1
go vet ./...
go build -o ./tmp/server.exe ./cmd/server
```

修改 Ent schema 后运行 `go generate ./internal/data` 并提交生成代码；修改迁移后重新计算 atlas.sum。数据库集成验证使用显式配置的 `INITRA_TEST_DATABASE_URL`。修改 setup 时验证改名后的项目仍可生成、测试和编译，且重复初始化保留凭据。

## 提交

行为或兼容性变化记录到 CHANGELOG 的“未发布”。提交说明包含问题、变更和实际验证结果。真实凭据、运行数据、编辑器缓存和本地生成文件留在 Git 之外。

安全问题按 [SECURITY.md](SECURITY.md) 私密报告。许可证、内部授权和代码所有者由仓库所有者决定。
