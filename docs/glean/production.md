# 生产部署、备份与恢复

这是**待运维配置并在目标环境演练的单机部署工具**，不是已部署声明。仓库不附带生产凭据，不会自动开通服务器、创建账号、改密码、初始化管理员、导入基线或启用采集。GitHub Actions 入口见 [手动部署工作流](github-actions-deployment.md)。

## 架构与前置条件

- Linux amd64，Docker Engine + Compose v2、Bash、Python 3、`flock`、`curl`、GNU tar/coreutils、PostgreSQL 16 客户端以及已安装并固定版本的 Atlas CLI。服务端不现场下载/执行安装脚本。使用与数据库匹配的 `pg_dump`/`pg_restore` 主版本。
- 单个 `glean-production` Compose 项目：非 root、只读根文件系统的后端和 Web；镜像必须为不可变 `@sha256:` 引用。后端的 `var` 是持久 bind mount，不是容器临时层。
- **外部 PostgreSQL 与 Redis** 由运维独立提供。数据库必须 `verify-full`，CA 在宿主机和容器中均可读；Redis 必须处于受限私网、启用认证和持久化，不能暴露公网。当前 Redis 客户端配置没有在本方案中增加 TLS 传输，跨不可信网络应先补足受保护传输，不能靠本 Compose 获得该保障。
- 宿主机 TLS 反向代理终止 HTTPS：`/api/` **直接**转发到 `127.0.0.1:8080`，其他路径转发到 `127.0.0.1:3000`。外层代理应覆盖 `X-Forwarded-For` 为真实客户端地址并设置 `X-Forwarded-Proto=https`，不要沿用来自公网的不可信转发头。后端 `server.trusted_proxies` 仅配置实际可信入口的源地址（Docker 场景需核对后端看到的网关地址），不能设为信任所有代理。现有 Web Nginx `/api/` 转发用于本地一跳链路，会重新设置转发头，因此生产入口不能把 API 再经过 Web 容器，否则会丢失真实 IP/HTTPS 信息并影响登录限流。只开放 TLS 入口；8080/3000 仅回环绑定。TLS 证书/代理由运维配置，本方案不自动签发证书。
- 现有镜像仓库拉取权限须由服务器原有登录或公开镜像提供。脚本不创建新持久凭据，也不把 GitHub token 传到服务器。
- 发布前备份需要短暂停写。停止所有同库后端、独立 monitor/import/enrichment 命令及其他写入者；脚本只能停止本 Compose 的 `backend`，不能发现其他主机的写进程。正常运行只能有约定的一套写入者。前端此时可显示 API 不可用；应由入口代理展示维护页。

## 服务器私有配置（由运维准备，不在 Actions 中填写）

1. 参考 `deploy/ops.env.example` 创建 `/etc/glean/ops.env`，权限 `0600`。该文件是受信任 Bash 配置，仅部署操作用户可写。`DATABASE_URL` 是唯一数据库连接来源：备份、迁移、后端运行时均从它派生数据库主机、端口、库名、用户和密码，避免备份 A 库却升级 B 库。只接受 postgres URI 和 `sslmode=verify-full`；禁止 query 参数覆盖数据库身份/搜索路径。
2. 参考 `deploy/backend.env.example` 创建 `/etc/glean/backend.env`（`0600`），填写已有 Redis 认证、已有 JWT secret、唯一 Snowflake node 和已有启用操作员 ID。不要在 `.env`/镜像/Git/Actions artifact 中放入这些值，也不要重新运行 setup 覆盖既有账号。
3. `/etc/glean/configs` 包含仓库的 `config.yaml`、`config.prod.yaml`、`rbac_model.conf` 及 `db-ca.crt`。**不复制开发 `config.local.yaml`**。在 `config.prod.yaml` 中设置实际 HTTPS CORS 白名单、实例 ID；后端容器 UID/GID `65532:65532` 必须有只读权限。`backend.env` 由 Docker 读取，不需要放宽为全员可读。数据库 CA 在容器固定为 `/app/configs/db-ca.crt`，URI 的 `sslrootcert` 指向宿主机对应 CA 文件。
4. 提前准备彼此不嵌套的 `GLEAN_DATA_DIR`、`GLEAN_ORIGINAL_ARCHIVE_DIR`、`GLEAN_BACKUP_ROOT`、`GLEAN_STATE_DIR`。原始私有档案目录保存**用户原始文献档案**，不能用仓库内经过整理的历史基线代替；原始档案可包含敏感信息，只允许备份操作用户读取。应用数据目录供 UID 65532 写入，备份操作用户须可读取；不要以 `chmod 777` 修权限。备份/状态目录仅操作用户可读写。档案只允许常规文件/目录，符号链接、硬链接和设备文件会使备份失败。
5. 私有配置、数据库角色权限、TLS/Redis 配置需要运维另行加密备份。它们**不**进入下述应用备份，也绝不上传 GitHub。保留现有凭据，不因演练创建可登录的持久生产账号。

后端强制 `APP_ENV=prod`、数据库 verify-full、禁用内存 token store、关闭匿名文档。首次发布前先在隔离环境用实际配置验证启动、Redis 与 DB 连接、登录权限和 HTTPS。`/ready` 是后端依赖就绪检查，Web `/healthz` 只是静态文件健康；二者都通过仍不等于真实外部来源持续采集通过。

## 发布流程

Actions 将精确提交归档到新的 release 目录并写入 `deploy/REVISION`。人工也必须准备相同不可变归档，不修改已部署目录：

```sh
cd /opt/glean/releases/<immutable-release>
GLEAN_OPS_ENV=/etc/glean/ops.env bash scripts/production/deploy.sh \
  '<full-40-character-commit>' \
  'ghcr.io/<owner>/glean-backend@sha256:<64-hex-digest>' \
  'ghcr.io/<owner>/glean-web@sha256:<64-hex-digest>'
```

顺序：校验 revision/digest/私有配置 → 获取操作锁 → 拉取镜像 → 停止后端写入 → 完整备份并校验归档/校验和 → Atlas 显式迁移 → 启动应用 → `/ready` 和 Web 健康检查 → 原子记录成功 release。镜像构建不在生产主机执行。迁移不会重跑 seed，不会基线重导入。

`current.json` / `previous.json` 只记录提交、镜像摘要、release 路径和非秘密的 DB 身份/数据路径。已有状态下更换 DB 身份或数据路径会拒绝常规部署；不能通过删除记录来绕过。首次接入也应人工确认现存运行容器与配置一致。DB 切换必须是单独审核的维护/恢复操作，不能在常规发布前悄悄修改 ops 文件。

备份、迁移、启动或就绪失败时停止后端，不自动回滚数据库，也不把一次 HTTP 成功当成全面验收。迁移输出写入私有备份目录 `migration.log`，不要直接贴到公共日志。故障可能发生在迁移已提交后，所以不能盲目启动旧代码。

## 手动应用备份

明确停止**所有**写入者后：

```sh
GLEAN_OPS_ENV=/etc/glean/ops.env bash scripts/production/backup.sh --writers-stopped
```

每个私有备份目录包含：

- `database.dump`：完整 custom-format PostgreSQL dump，包括用户密码哈希与会话版本、角色权限、个人阅读/隐藏、主题、人工审核、纠错、审计历史、原始 raw JSON/来源证据、monitor JSONB 状态，以及 Atlas 非 public schema 的迁移记录。**基线导入不是这些数据的备份。**
- `data.tar.gz`：持久 `var` 内容，包括上传文件；`originals.tar.gz`：原始用户私有档案；`REVISION`、`SHA256SUMS` 和最后写入的 `COMPLETE`。
- 私有诊断日志、dump 目录清单。缺少 COMPLETE、校验不符或不安全归档的包不得恢复。

备份含密码哈希和私有文献，应视同秘密：加密、异机/异地域复制、访问审计、保留期与删除策略由运维明确配置。脚本不自动清理旧备份；必须监控磁盘空间并验证异机副本可恢复。建议每日至少一次应用备份、每次升级前备份，并根据实际 RPO/RTO 调整；本仓库未测得生产恢复耗时或实际容灾目标。

**Redis 限制：**此包不包含 Redis AOF/RDB、活跃会话、限流状态或 Asynq 队列。它不是完整基础设施灾备。Redis 应单独持久化和备份；同一事件恢复时必须选择审核过的匹配恢复点，避免旧队列继续执行。数据库保存 monitor 检查点，但 Redis 丢失后的队列重建/去重及运行恢复必须另行验证，不能声称零任务丢失。

## 恢复到全新隔离数据库（不替换生产）

1. 运维创建一个**新的空数据库**，不提前迁移/seed；使用不同数据库名与隔离网络/Redis，禁止恢复环境自动采集。为当前终端私密设置 `GLEAN_RESTORE_DATABASE_URL`，同样 verify-full；host/port/database 与源库相同（即使换了账号/密码/query）会拒绝，其他 DNS 别名指向同一库仍须运维排除。不要把带密码 URL 写到命令参数、聊天或日志。
2. 选择不存在的恢复目录，必须在现有生产数据/档案/备份/状态目录之外：

```sh
GLEAN_OPS_ENV=/etc/glean/ops.env bash scripts/production/restore.sh \
  /var/lib/glean/backups/<backup-id> /var/lib/glean-restored/<new-id> \
  --confirm-empty-target
```

脚本验证校验和、拒绝非空数据库、预先拒绝归档路径穿越/链接/设备，解出 `data/` 与 `originals/`，再用单事务恢复数据库。没有 DROP、`--clean`、自动流量切换或生产覆盖。PG restore 失败回滚 DB 事务；已解压文件保留在新隔离目录供检查，不自动删除。用新的空目录重新演练；不能将残留目录直接作为服务数据。

3. 在隔离环境校验：用户/角色/会话版本、已知论文原始内容、审核结论、纠错显示与原始证据仍分离、审计历史、个人阅读状态、私有档案与上传文件字节/权限、monitor checkpoint/watermark、Atlas revision。先修复新文件夹所有权以供非 root 应用读取；不要复制应用可写权限到备份目录。保持采集主开关关闭，用隔离 Redis，验证登录/页面/后端依赖后再执行 doctor 和经过批准的来源恢复检查。
4. 生产切换需要独立确认恢复点、停写窗口、Redis 配套策略、配置/密钥、目标权限与回退路径。由运维在维护窗口执行，不由本脚本自动操作。旧库和旧文件保留到恢复验收完成。

## 应用代码回退

仅当运维已确认**当前 DB schema 与旧版本兼容**，才在 `previous.json` 指向的旧不可变 release 下运行：

```sh
GLEAN_OPS_ENV=/etc/glean/ops.env bash scripts/production/rollback.sh \
  '<previous-full-commit>' '<previous-backend@sha256:digest>' '<previous-web@sha256:digest>' \
  --confirm-schema-compatible
```

仍先停写并备份、启动指定旧镜像并检查就绪；**不**执行 down migration 或恢复 DB。如果 schema 不兼容，保持维护状态，优先修复前滚；若确需数据库恢复，走上面的新库恢复/人工切换流程。不要在故障脚本中自动删除数据卷或覆盖生产库。

## 可复现验证与边界

```sh
python3 -m unittest discover -s scripts/production/tests -v
# 以下只接受显式隔离的 loopback CI 数据库（可 CREATE DATABASE）；不读取生产 URL。
# 在私有环境变量中设置 GLEAN_RECOVERY_CI_DATABASE_URL 后：
bash scripts/production/tests/recovery-drill.sh
```

离线测试验证停写→备份→迁移→启动顺序、失败停机、摘要/revision 约束、秘密不进入参数/标准日志、数据库身份一致性及恶意归档拒绝。真实 PostgreSQL 演练使用全部迁移与实际业务表，比较恢复前后所有表全部列，并逐字比较用户私有原始档案和上传文件；重复恢复拒绝非空库且不改变内容。它使用临时测试数据库，结束仅清理本次创建的唯一测试库。CI 演练证明备份格式/数据完整性，不替代实际生产权限、TLS、Redis 恢复及外部采集连续性验收。
