# Glean

**持续生长的文献库。只有 LLM 调用暂用 mock，其他运行链路均为真实实现。**

Glean 将历史论文、增量发现与证据核验分开管理：一个 Go 后端，三个主题视图，一个中文优先的 React 阅读与查询工作台。基于 [initra](https://github.com/TeamSillyBees/initra) 源码模板构建，沿用认证、权限、Ent/PostgreSQL、Redis 和任务抽象，而不是另起平行基础框架。

## 当前能力与边界

- 历史快照：生物医学 AI **31,399**、LLM / Agent 知识工程 **4,059**、生物医学世界模型 **4,037**，共 **39,495 条主题成员关系**。包含已纳入、范围外、预印本及待核记录，不假装全部已核实。
- 文献检索、主题筛选、精确历史分类、来源证据、正式在线/卷期日期、版本化载体证据；个人阅读/隐藏与原科学结论相互独立。
- 72 小时扫描窗口、重叠补漏、分页断点、重试预算、并发租约与采集/模型处理/入库分离；Crossref、PubMed、arXiv OAI、bioRxiv、Europe PMC、medRxiv 均为真实公开 API，已通过有界真实 Go 请求验证和离线回归。
- 模型阅读与总结目前为**明确标识的 mock**。Mock 不构成科学核验，不将模拟论文插入历史文献库。真实模型密钥未配置。
- 前端只访问本服务 API，不自行调用外部文献库、运行模型或修改科学分类。
- 只有 LLM API 暂用 mock。正常服务和监控命令使用真实数据源、真实 PostgreSQL 与真实目录入库；合成来源仅存在于测试或显式 `--demo`。自动调度默认关闭，配置运行账号后可用一个开关启用真实采集。尚无外部部署。

完整历史数据已打包，不等于已经导入你自己的数据库；实际导入数量以导入报告为准。源码测试、CI 数据库测试和真实浏览器视觉验收是不同的验证层。

## 技术与目录

| 位置 | 用途 |
|---|---|
| `cmd/server` | 独立 Go HTTP API，Gin + Huma，initra 安全边界 |
| `internal/modules/catalog` | 文献、主题、证据、载体与个人状态 |
| `internal/modules/monitor` | 增量编排、来源适配器、mock 审核、持久进度 |
| `internal/baseline`、`tools/importbaseline` | 历史数据提取、校验、幂等导入、失败隔离 |
| `internal/data/schema`、`db/migrations` | Ent 定义、生成代码、显式迁移 |
| `web` | React、TypeScript、shadcn/ui-style 开放源码组件、Radix、Tailwind |
| `data/baselines` | 42 个压缩历史分片、显示分类、校验清单 |

## 本地启动

需要 Go 1.26、Node 24/npm 11、Docker Compose、Atlas CLI 和 PostgreSQL 客户端。下面是本机开发流程，不是生产部署脚本。

### 1. 初始化本地应用配置与依赖

```sh
go run ./tools/setup -name Glean -slug glean
docker compose up -d postgres redis
```

setup 只生成 Git 忽略的本地配置与管理员 seed；不会执行数据库迁移。首次生成时显示本地管理员密码，请自行妥善保存，后续执行不会覆盖既有凭据。不要将本地配置、密码或带密码的数据库 URL 提交到仓库。

本地数据库由 Compose 创建为 `glean`，账号为 `postgres`，仅绑定回环地址。为后续命令在自己的终端私密设置 `DATABASE_URL` 指向此本地数据库；本地允许 `sslmode=disable`，共享环境必须使用 `verify-full`。

```sh
atlas -c file://db/atlas.hcl migrate apply --env local
psql "$DATABASE_URL" --set ON_ERROR_STOP=1 --single-transaction -f db/seeds/001_seed_admin.local.sql
```

### 2. 校验并导入真实历史基线

```sh
go run ./tools/importbaseline -mode verify
```

从已创建的管理员账号获得用户 ID（登录响应或 `/api/v1/auth/me`），设置 `GLEAN_BASELINE_DATABASE_URL` 为同一数据库 URL。也可在启动服务前通过本地数据库查询已创建的启用用户 ID。

```sh
go run ./tools/importbaseline -mode import -node 17 -operator '<existing-user-id>' -allow-local-insecure
```

`17` 是示例节点，必须与同时运行的服务/导入器不同。`-allow-local-insecure` 只适用于明确的本地回环开发数据库，不能用于远程数据库。

所有分片先校验后导入。重跑不会重复新增相同主题原始编号；失败条目隔离并非零退出，不静默忽略。报告写入忽略的 `.baseline/`。详见 [基线说明](docs/glean/baseline.md)。

### 3. 启动后端和前端

```sh
APP_ENV=local INITRA_IDGEN_NODE=1 go run ./cmd/server
```

另一个终端：

```sh
cd web
npm ci
npm run dev
```

浏览器访问 `http://127.0.0.1:5173`，用本地初始化账号登录。Vite 将 `/api` 转发到 `127.0.0.1:8080`。后端配置环境变量保留 initra 的 **`INITRA_` 前缀**以保持模板兼容；应用标识、数据库与项目名为 Glean。

可先启动服务获取用户 ID，再完成步骤 2；在导入前，页面显示真实空状态，不会偷偷显示假数据。

### 4. 运行真实增量采集

在同一私有终端配置 `INITRA_MONITOR_DATABASE_URL`（与服务相同的数据库）和 `INITRA_MONITOR_OPERATOR_ID`（已有启用账号的 ID）。无需文献来源 API key 或 LLM key；不自动发送用户邮箱。

```sh
# 仅检查数据库、运行身份与来源配置，不请求外部源。
go run ./cmd/glean-monitor --node 11 --doctor --allow-local-insecure

# 有界真实请求；窗口可按实际需要调整，不应晚于当前时间。
go run ./cmd/glean-monitor --node 11 \
  --sources crossref,pubmed,europepmc \
  --since 2026-10-23T00:00:00Z --until 2026-10-26T00:00:00Z \
  --max-pages 6 --allow-local-insecure
```

真实来源记录随已获取页面写入真实目录，位于“新发现待审”，不是演示论文。只有模型阅读/总结结果明确标识为占位；不会因此获得科学核验或正式纳入资格。有限命令可能因预算、退避或尚有分页返回待继续状态，不冒充完整扫描。

省略时间窗可恢复未完成任务或启动到期任务。需要持续每 72 小时更新时，为后端配置 `INITRA_MONITOR_OPERATOR_ID`，并开启 `INITRA_MONITOR_SCHEDULER_ENABLED=true`；这一个主开关统一装配所需任务 worker/scheduler。每分钟只是检查持久化进度的唤醒，不是每分钟全量抓取。默认不开启，是为了在账号、数据库和来源配置确认前不静默启动网络任务。

[真实采集运行说明](cmd/glean-monitor/README.md) 包含恢复、请求预算、限流、隔离记录、真实端到端 canary 和单独的显式离线测试模式。不要用 `--demo` 或模拟数据库作为正常部署方案。

## 可选容器前后端

完成本地配置、迁移和基线导入后：

Linux 开发机先使用当前非 root 用户创建私有运行目录，并显式传递文件所有者 ID。不要将本地配置改成全员可读，也不要以 root 启动应用容器：

```sh
export LOCAL_UID="$(id -u)" LOCAL_GID="$(id -g)"
test "$LOCAL_UID" != 0 || { echo "请使用普通开发用户"; exit 1; }
install -d -m 700 var
docker compose --profile app up --build -d
```

本地 `config.local.yaml` 保持 setup 创建的私有权限，后端容器以对应 UID/GID 读取；`var` 挂载供日志/上传写入。macOS/Windows Docker Desktop 的挂载 UID 行为需要在目标机器验证。

前端 `http://127.0.0.1:3000`，`/api` 由 Nginx 反向代理至 Compose 内部的 `backend:8080`。仅供本地验证，不能直接作为公网生产配置。前端静态健康检查不代表数据库或后端就绪。生产部署需要自行提供 TLS、受控账号/密钥、备份及明确来源访问额度。

## 验证

```sh
go test ./... -count=1
go vet ./...
go build -o ./tmp/glean-server ./cmd/server
go build -o ./tmp/glean-monitor ./cmd/glean-monitor
go build -o ./tmp/glean-importbaseline ./tools/importbaseline
cd web
npm test
npm run typecheck
npm run lint
npm run build
npm run format:check
```

CI 启动隔离 PostgreSQL 16，强制检查真实迁移、文献导入/查询、监控持久化和完整历史快照测试，不能用跳过数据库测试冒充通过。本地未设置 `INITRA_TEST_DATABASE_URL` 时，相关测试会明确跳过。常规 CI 的浏览器任务仅使用真实本地 Go API、PostgreSQL 和已提交的完整历史数据，分别验证桌面/手机，不请求外部文献服务。真实 Europe PMC 采集→入库→页面验收已移到 Actions 的 **Manual live source acceptance**，只有手动触发才运行，不参与 push/PR 构建测试；外部源不可用时该手动验收明确失败，不回退到模拟记录或吞掉失败。Go 外部来源 canary 同样受 `live` build tag 与显式环境变量保护，默认测试不编译它。报告在上传前清理临时认证信息；截图用于实际布局检查。真实 LLM 接入仍待后续配置。

## 来源与覆盖

[真实来源说明](docs/glean/real-sources.md) 区分 API 增量日期、正式出版日期、分页完整性及真实请求验证；[期刊登记表](data/journals/README.md) 保存历史指标证据、ISSN 与覆盖缺口。核心期刊抓取完整题录再等待语义审核，不用狭窄关键词把潜在相关论文提前丢弃。扩展主题检索不受核心名单限制。

同一论文的来源更新、科学版本、正式出版事件与模型处理分别记录。后来的题名、摘要、DOI 来源信息也可搜索；它们不会静默覆盖原始历史核验。永久的单条身份冲突/坏记录隔离，其他记录继续；来源失败、长 Retry-After、隔离数和覆盖水位都可观察。

默认阅读范围是经过核验的历史纳入集合，另有新发现待审与全部记录。个人隐藏目前明确作用于所有主题，和科学结论无关；隐藏可恢复。

## 设计原则

- 发现时间、索引时间、预印本时间、正式在线/卷期时间分别保存；投稿/录用日期不冒充正式发表。
- 三个主题独立资格与科学判断；JIF/CCF 证据版本化，不混用目录规则。
- 旧科学核验不被 mock 或低置信度自动结果覆盖；获取内容不等于读过内容。
- 不把数千条逐篇描述直接展开成侧栏选项；原始证据保留，显示分组复用最新历史目录。
- 无硬编码模型密钥；认证 token 仅在前端内存中，页面刷新后重新登录。

参阅 [API 契约](docs/glean/api-contract.md)、[历史基线](docs/glean/baseline.md)、[前端说明](web/README.md)、[模板来源](TEMPLATE_ORIGIN.md) 以及 [开发约定](AGENTS.md)。
