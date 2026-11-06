# 贡献指南

本仓库维护 Glean 文献工作台及其基础能力。修改前阅读 [README](README.md)、[AGENTS.md](AGENTS.md) 和相关代码，保护工作区已有改动。

业务模块遵循现有 flat package、构造注入、业务错误门面和事务模式。面向开发者的文档和注释优先使用中文。

## 验证

完整命令、数据库测试与 CI 边界见[开发指南](docs/development.md#验证)。修改 Go 文件后运行 gofmt 和相关测试；基础能力、装配、数据库与项目工具变更执行完整 Go 验证。前端改动执行对应测试、类型检查、lint 和构建。

修改 Ent schema 后运行 `go generate ./internal/data` 并提交生成代码；迁移同步更新 `atlas.sum`。数据库集成验证使用专用 `INITRA_TEST_DATABASE_URL`。修改 setup 时验证改名后的项目可生成、测试、编译，且重复初始化保留凭据。

## 文档与提交

- 当前能力和用法更新到对应文档；未完成工作放在 [Roadmap](docs/roadmap.md)，用户可感知变化写入 [Changelog](CHANGELOG.md) 的“未发布”。
- 提交说明包含问题、变更和实际验证结果。测试跳过、外部验收和生产验证分别说明。
- 真实凭据、运行数据、编辑器缓存和本地生成文件留在 Git 之外。

安全问题按 [SECURITY.md](SECURITY.md) 私密报告。模板来源和许可边界见 [TEMPLATE_ORIGIN.md](TEMPLATE_ORIGIN.md)。
