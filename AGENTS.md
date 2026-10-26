# AGENTS.md

本文件适用于整个模板项目。用户在对话中的具体要求优先。

## 开发流程

1. 修改前阅读 README、相关源码、配置和测试，检查工作区状态，保护已有改动。
2. 使用 rg 定位真实入口，按已有模式做聚焦修改；文档与代码不一致时同步修正文档。
3. 修改 Go 文件后运行 gofmt，运行相关测试；影响基础能力、装配、数据库或项目工具时执行完整验证。
4. 完成后简要说明变更、验证结果和未验证事项。

```powershell
go test ./... -count=1
go vet ./...
go build -o ./tmp/server.exe ./cmd/server
```

## 代码组织

- 项目是单一 Go module，业务与基础能力均在当前仓库维护。
- `internal/platform` 承载基础能力，不反向依赖 boot、业务模块或项目生成的 Ent。
- `internal/boot` 负责配置、装配和生命周期；依赖通过构造函数传入。依赖解析集中在 boot 和各模块 providers.go。
- `internal/modules/<module>` 使用 flat package：handler、dto、service、routes 和 providers 按职责分文件。Service 需要数据库时直接使用 Ent Client；跨模块协作由调用方定义小接口。
- Handler 负责 HTTP 适配，DTO 位于 dto 文件；Huma 包装类型使用私有 request/response，JSON 输出使用 VO。按用例复用已有 Body/Query，避免机械增加转换层。
- 导出声明使用以标识符开头的中文注释；复杂逻辑、测试 fake 和关键断言解释意图。
- 基础能力在启动时通过 `Register(injector, cfg)` 显式装配；service/handler 不持有 injector。
- 业务错误集中在 `internal/modules/bizerrors`，其他业务模块通过该门面创建错误。

## 数据和运行约束

- 业务 ID 统一使用 `internal/platform/idgen.ID`，JSON/OpenAPI 表示为字符串；每个运行实例显式配置唯一的 Snowflake node。
- Ent schema 使用本地 entx 字段和索引助手，Ent Client 注册审计和物理删除保护 Hook。版本字段助手只提供字段，不代表自动乐观锁。
- 生成代码随仓库提交；修改 schema 或生成入口后运行 `go generate ./internal/data`，检查生成结果并同步迁移。
- 迁移禁用物理外键和数据库级联，保留 `migrate.WithForeignKeys(false)`。逻辑关联写入在同一事务内检查有效父记录并加 FOR SHARE，父记录删除或相关状态更新加 FOR UPDATE 并显式处理子关系。
- 逻辑外键列有索引覆盖，非唯一索引避免被其他索引的最左前缀完全覆盖。
- Redis 操作复用本地 redisx；批量清理使用 SCAN/UNLINK。任务业务通过 task 接口，Asynq 只出现在 asynqadapter；外部副作用由业务保证幂等。
- 文件业务使用 storage 接口；云 SDK 留在 provider。HTTP Client 由启动层统一装配，业务依赖 Executor。
- Worker/Scheduler 按配置接入生命周期，禁用时不解析；应用关闭预算不小于 Worker 关闭预算。

## 安全和配置

- 所有 /api/ 路由登记 RouteSecurity：公开、登录可用、权限访问分别显式声明，缺失登记时 fail-closed。权限唯一来源为数据库角色、权限资源和授权关系。
- access JWT 只包含用户、会话与会话版本等声明，请求时解析当前有效身份；全部退出、改密和禁用账号递增会话版本。
- 登录使用 LoginGuard。共享环境必须启用 Redis 认证状态和登录防护；内存实现仅允许 dev/local/test 显式启用。
- 运行环境使用 APP_ENV，其他环境变量默认使用 INITRA_ 前缀。配置具备默认值、环境覆盖、严格校验和脱敏输出。
- 共享环境使用 PostgreSQL verify-full、明确 CORS 白名单，并关闭匿名 API 文档。
- 密码、token、验证码、session value、Authorization、访问密钥和带密码 DSN 不进入日志。
- setup 只准备本地文件，迁移和 seed 显式执行；现有本地凭据应保留。

## 测试

修改业务模块应补充与风险匹配的覆盖；数据库操作优先用 sqlmock 验证 SQL、事务和参数。保留架构边界与迁移约束测试。需要真实 PostgreSQL 验证时设置 INITRA_TEST_DATABASE_URL，测试在独立事务 schema 中运行并回滚。
