# 开发指南

## 环境与代码组织

需要 Go 1.26、Node 24 / npm 11、Docker Compose、Atlas CLI 和 PostgreSQL 客户端；具体依赖以 [go.mod](../go.mod) 和 [package-lock.json](../web/package-lock.json) 为准。

| 目录                                    | 职责                                      |
| --------------------------------------- | ----------------------------------------- |
| `cmd/server`                            | HTTP API 入口，Gin + Huma                 |
| `internal/boot`                         | 配置、依赖装配和生命周期                  |
| `internal/platform`                     | 认证、数据库、HTTP Client、任务等基础能力 |
| `internal/modules/catalog`              | 论文、主题、来源证据、审核与个人状态      |
| `internal/modules/monitor`              | 来源适配、增量编排、模型阶段和持久进度    |
| `internal/retrieval`                    | 检索路线、研究协议和证据门槛              |
| `internal/data/schema`、`db/migrations` | Ent 定义与版本化迁移                      |
| `tools`                                 | 本地配置、基线导入、摘要补全及登记表工具  |
| `web`                                   | React / TypeScript 阅读与管理工作台       |

项目为单一 Go module，基础能力随源码维护。`platform` 不依赖业务或 boot；boot 和各模块 providers 负责构造注入，业务对象不持有 injector。业务模块使用 flat package，错误经 `bizerrors` 门面创建。详细约定见 [AGENTS.md](../AGENTS.md)。

Ent 生成代码随仓库提交。修改 schema 后运行 `go generate ./internal/data` 并同步迁移及 `atlas.sum`。迁移禁用物理外键；逻辑关联在事务内校验和加锁。既有 Glean 数据库按原迁移历史升级，不重新建立模板基线。

## 本地启动

以下通用命令在仓库根目录执行。环境变量分别给出 Bash 与 PowerShell 写法；含密码的数据库 URL 在私有终端中设置，不写入仓库。

### 1. 准备配置和依赖

```sh
go run ./tools/setup -name Glean -slug glean
docker compose up -d postgres redis
```

setup 仅准备本地配置和管理员 seed，重复运行保留既有凭据。它不会连接数据库、执行迁移或初始化账号。默认开发数据库名为 `glean`，仅绑定回环地址。

在私有终端中设置 `DATABASE_URL` 后，显式执行迁移与首次 seed：

```sh
atlas -c file://db/atlas.hcl migrate apply --env local
psql "$DATABASE_URL" --set ON_ERROR_STOP=1 --single-transaction -f db/seeds/001_seed_admin.local.sql
```

PowerShell 的第二条命令使用 `"$env:DATABASE_URL"`。既有数据库只执行待应用迁移，不重新 seed 或清库。本地回环开发可用 `sslmode=disable`，共享环境必须使用 `verify-full`。

### 2. 启动应用

Bash：

```bash
APP_ENV=local INITRA_IDGEN_NODE=1 go run ./cmd/server
```

PowerShell：

```powershell
$env:APP_ENV = "local"
$env:INITRA_IDGEN_NODE = "1"
go run ./cmd/server
```

另开终端启动前端：

```sh
cd web
npm ci
npm run dev
```

打开 `http://127.0.0.1:5173` 并使用本地初始化账号登录。Vite 将 `/api` 转发到 `127.0.0.1:8080`。应用配置使用 `APP_ENV`，其余环境覆盖保留 `INITRA_` 前缀。前端 token 仅保存在内存，刷新后重新登录。

### 3. 导入历史数据

```sh
go run ./tools/importbaseline -mode verify
```

从已启用账号取得操作员 ID，私密设置 `GLEAN_BASELINE_DATABASE_URL` 后导入：

```sh
go run ./tools/importbaseline -mode import -node 17 -operator '<existing-user-id>' -allow-local-insecure
```

每个同时运行的进程使用不同 Snowflake node。`-allow-local-insecure` 仅用于回环开发库；生产导入不能使用它。导入报告、幂等与隔离处理见[历史数据](guides/baseline.md)。

### 4. 运行采集与模型处理

私密设置 `INITRA_MONITOR_DATABASE_URL` 与 `INITRA_MONITOR_OPERATOR_ID`，先检查配置，再执行有限预算的采集：

```sh
go run ./cmd/glean-monitor --node 11 --doctor --allow-local-insecure
go run ./cmd/glean-monitor --node 11 --sources crossref,pubmed,europepmc --max-pages 6 --allow-local-insecure
```

doctor 不请求外部源。第二条会恢复未完成运行或启动到期扫描；首次无历史起点时默认回看 30 天，有限命令可能需要续跑。采集不依赖 LLM key，模型缺配置时保留真实待处理状态。

持续调度由 `INITRA_MONITOR_SCHEDULER_ENABLED=true` 显式启用，并要求已有操作员；模型另有独立开关和累计预算。运行与恢复见[采集 CLI](../cmd/glean-monitor/README.md)，范围配置见[检索方案](guides/search-plans.md)，模型配置见[论文处理](guides/paper-pipeline.md)。

### 可选本地容器

完成配置、迁移和导入后，Linux 普通用户可以运行前后端容器：

```bash
export LOCAL_UID="$(id -u)" LOCAL_GID="$(id -g)"
test "$LOCAL_UID" != 0 || { echo "请使用普通开发用户"; exit 1; }
install -d -m 700 var
docker compose --profile app up --build -d
```

前端位于 `http://127.0.0.1:3000`。本地配置保持私有权限，容器使用对应 UID/GID。Windows/macOS Docker Desktop 的挂载权限需在目标环境验证；公网部署使用[部署说明](deployment/README.md)。

## 验证

```sh
go test ./... -count=1
go vet ./...
go build -o ./tmp/server ./cmd/server
go build -o ./tmp/glean-monitor ./cmd/glean-monitor
go build -o ./tmp/importbaseline ./tools/importbaseline
```

```sh
cd web
npm ci
npm test
npm run typecheck
npm run lint
npm run build
npm run format:check
```

数据库测试使用专用的 `INITRA_TEST_DATABASE_URL`，按独立 schema/事务隔离。未提供时会明确 skip，不能记作数据库测试通过。完整历史查询诊断见[性能参考](reference/query-performance.md)；浏览器测试的依赖和命令见 [E2E 说明](../web/e2e/README.md)。

## CI 与外部验收

| 验证层                     | 用途                                               | 外部依赖                             |
| -------------------------- | -------------------------------------------------- | ------------------------------------ |
| Go / PostgreSQL / 历史基线 | 业务语义、迁移、生成代码、导入、持久恢复与完整快照 | 隔离 PostgreSQL                      |
| 前端检查                   | DOM 测试、类型、lint、格式与构建                   | 测试中可使用明确的网络 mock          |
| 容器 HTTP                  | 镜像启动、反向代理、readiness 与认证边界           | 隔离 PostgreSQL / Redis              |
| 真实浏览器                 | 桌面/手机通过 Go API 操作完整历史库                | 隔离后端与数据库，不请求公开文献 API |
| 备份恢复                   | 真实表与文件的备份、空库恢复及完整性               | 独立测试库；不使用生产备份           |
| 手动来源/模型验收          | 官方 API 兼容性、有限真实采集、模型协议            | 显式启用；模型需实际配置和预算       |

`main` push 与手动 CI 执行完整验证。功能分支通过 PR 运行，同一 PR 的新提交取消旧运行；main/手动运行不互相取消。只有完整 diff 被证明全部为允许的 `README.md` 或 `docs/**/*.md` 时，PR 才可显式跳过运行时任务；`query-performance.md`、测试、配置和未知路径不在该允许范围。判定异常时回退完整验证，`CI validation` 汇总结果。

Go module/编译缓存与 Docker 层缓存用于减少构建开销，不代替测试。完整历史性能诊断另有保守选择器；修改其文档或相关代码会触发诊断。普通 CI 不发布镜像、不部署生产。

公开来源 canary 使用 `live` build tag 和显式开关，例如 Bash 中：

```bash
GLEAN_LIVE_SOURCE_CANARY=1 GLEAN_LIVE_SOURCE=pubmed \
  go test -tags=live ./internal/modules/monitor -run '^TestLiveSourceCanary$' -v -count=1
```

每来源最多两次请求，不追完整分页。Actions 的 **Manual live source acceptance** 分开执行六源有界探针和 Europe PMC 单条采集→临时数据库→浏览器验证。普通 CI、连通性探测、有限页验收和持续运行分别记录，不能互相替代。模型实服检查不进入默认测试，也不启用生产队列。

提交要求见[贡献指南](../CONTRIBUTING.md)。
