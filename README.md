# initra

initra 是面向企业内部 Go 服务的源码模板。复制仓库后即可在同一项目中查看和修改业务与基础能力；Gin、Huma、Ent、Redis 等第三方库继续通过 Go Modules 管理。

项目采用单一 Go module，包含现有认证、权限、文件、HTTP Client、异步任务示例及其测试。派生项目由业务团队独立维护。

## 目录

```text
cmd/server           服务入口
internal/boot        配置、装配和生命周期
internal/platform    项目内基础能力源码
internal/modules     业务模块
internal/data        Ent schema、生成代码和迁移工具
configs              分环境配置与 Casbin model
db                   迁移与种子数据
test                 架构、集成和 HTTP 测试
tools/setup          本地改名与凭据初始化
```

## 开始使用

需要 Go 1.26、Docker Compose、Atlas 和 PostgreSQL 客户端。使用仓库模板或复制源码后，在项目根目录执行：

```powershell
go run ./tools/setup -module example.com/my-api -name "My API" -slug my-api

docker compose up -d postgres redis
$env:DATABASE_URL = "postgres://postgres:postgres@127.0.0.1:5432/my-api?sslmode=disable"
atlas -c file://db/atlas.hcl migrate apply --env local
psql $env:DATABASE_URL --set ON_ERROR_STOP=1 --single-transaction -f db/seeds/001_seed_admin.local.sql

$env:APP_ENV = "local"
go run ./cmd/server
```

直接试用本仓库时，执行 `go run ./tools/setup` 保留现有名称，连接 URL 中的数据库名使用 `initra`。

setup 会改写 module 和内部引用、应用名称及默认命名空间，生成未纳入 Git 的 `configs/config.local.yaml` 和 `db/seeds/001_seed_admin.local.sql`。JWT 密钥随机生成；管理员用户名为 `admin`，初始密码仅在首次生成 seed 时显示，文件中只保存 bcrypt 哈希。重复执行保留已有凭据。迁移和 seed 由上述命令显式执行。

## 配置

配置按默认值、`configs/config.yaml`、`configs/config.<env>.yaml`、环境变量的顺序覆盖，并在启动时校验。运行环境使用 `APP_ENV`，其他配置环境变量默认使用 `INITRA_` 前缀，例如 `INITRA_AUTH_JWT_SECRET` 和 `INITRA_IDGEN_NODE`。

setup 准备本地环境；dev、test 和共享环境需要自行提供 JWT 密钥。每个运行实例必须显式配置唯一的 0–1023 Snowflake node。共享环境要求 PostgreSQL `verify-full`、Redis 认证状态、明确的 CORS 白名单，并关闭匿名 API 文档。

本地依赖默认连接回环地址；Docker 镜像通过环境变量接收运行配置。生产配置和本地凭据由各业务项目自行管理。

## 数据库

Ent schema 位于 `internal/data/schema`，生成代码随仓库提交。修改 schema 后执行：

```powershell
go generate ./internal/data
go run ./internal/data/migratediff/main.go <name> --env local --config-dir configs --dev-url <独立的迁移开发库URL>
atlas migrate hash --dir file://db/migrations
atlas -c file://db/atlas.hcl migrate apply --env local
```

diff 使用独立的临时开发库重放迁移；apply 使用 `DATABASE_URL` 指定目标库。当前迁移从全新基线开始，整个历史不创建物理外键；关联一致性由应用事务和行锁维护。

## 开发验证

```powershell
go test ./... -count=1
go vet ./...
go build -o ./tmp/server.exe ./cmd/server
```

默认测试使用 fake、sqlmock 和内存依赖。设置 `INITRA_TEST_DATABASE_URL` 后，集成测试会在该 PostgreSQL 测试库的独立事务 schema 内验证迁移和 seed，并在结束时回滚；CI 已启用此验证。

业务模块按 flat package 组织。开发约定见 [AGENTS.md](AGENTS.md)，贡献流程见 [CONTRIBUTING.md](CONTRIBUTING.md)。
