# 47.104.200.154 实际部署记录

更新日期：2026-11-06（Asia/Shanghai）。Glean 已部署到 **https://glean.starxy.cc**，运行已合并到 `main` 的功能提交 `c9eee61a7725db2bd105b48c991e5edc9c6caedb`。首次管理员初始化及完整历史基线导入已完成，本次新增检索路线、协议与审计迁移也已应用；采集、任务 worker/scheduler 和模型调用保持关闭。

## 当前布局与启动方式

服务器只保留一个主 Compose，项目名为 `glean`：

```text
/root/docker/glean/
├── compose.yaml
├── .env                         # 提交号和不可变镜像引用
├── config/
│   ├── app/                     # config.yaml、RBAC 模型、数据库 CA
│   ├── backend.env              # 既有数据库/Redis/JWT 凭据与运行开关
│   ├── admin-initial.txt         # 首次管理员凭据，root 0600
│   ├── postgres/                # 数据库认证与 TLS 配置
│   ├── redis/                   # Redis 配置及认证
│   ├── nginx/                   # HTTPS/API 代理配置、ACME webroot
│   └── letsencrypt/             # 证书、续期配置与成功后的 reload hook
├── data/
│   ├── app/                     # 后端 /app/var
│   ├── postgres/
│   └── redis/
└── backups/                     # 整理前归档、初始化报告、上线备份
```

`compose.yaml` 管理 `postgres`、`redis`、`backend`、`web` 四个服务。容器均使用 `restart: unless-stopped`，由 Docker 恢复运行，**没有 systemd Compose 服务**。宿主机 Nginx 保持禁用，实际入口是 Web 镜像中的非 root Nginx。

```sh
cd /root/docker/glean
docker compose ps
docker compose up -d --wait --wait-timeout 90
docker compose logs --tail 100 backend web
```

人工执行过 `stop` 的容器需要重新 `up -d`。不要使用 `down -v` 或清空持久数据目录。旧 `infra/`、源码快照、重复镜像记录、准备脚本及空目录已归档后移出运行目录；既有数据库、Redis、JWT 凭据继续沿用。

## 网络与 HTTPS

| 入口 | 实际配置 |
| --- | --- |
| 域名 | `glean.starxy.cc`，A 记录 `47.104.200.154` |
| 公网 | TCP 80、443；80 的普通请求返回 308 跳转 HTTPS，保留 ACME 与健康检查路径 |
| Nginx | 容器 UID/GID `101:101`，容器内 8080/8443 对应宿主 80/443 |
| API | Nginx 直接代理 `backend:8080`，覆盖客户端转发头；HTTPS 与前端同源 |
| 后端 | 仅 `127.0.0.1:8080` 发布到宿主；可信代理为专用 Docker 网段 `172.30.154.0/24` |
| PostgreSQL | 16.15，宿主仅 `127.0.0.1:5432`；后端通过 `glean-postgres:5432`，强制 `verify-full` |
| Redis | 7.4.12，仅 Compose 网络；认证、AOF/everysec、`noeviction` |
| CORS / 文档 | 仅允许 `https://glean.starxy.cc`；后端匿名 API 文档关闭 |

公网证书由 Let’s Encrypt 签发，当前到期日为 **2027-02-03**。证书私钥只允许 root 和容器所需组读取；Nginx 只挂载 `live/` 与 `archive/`，不挂载 ACME 账户目录。

系统自带的 `certbot.timer` 负责证书续期；`certbot.service` 的 Glean drop-in 指向 `config/letsencrypt`。续期成功后，deploy hook 修正新证书权限、检查 Nginx 配置并 reload。这个 timer 只管理证书，不管理 Compose 启动。

```sh
systemctl list-timers certbot.timer
certbot renew --dry-run --run-deploy-hooks --no-random-sleep-on-renew \
  --config-dir /root/docker/glean/config/letsencrypt
```

2026-11-06 已实际通过上述续期演练与 reload。HTTP-01 续期需要公网 80 持续可达，见 [Certbot 官方说明](https://eff-certbot.readthedocs.io/en/stable/using.html#webroot)。数据库使用独立的私有 CA/叶证书，数据库证书到期日为 2027-12-05，不由此 Certbot timer 续期。

## 版本、数据与账号

- [功能版本 c9eee61 的 Glean CI](https://github.com/TeamSillyBees/glean/actions/runs/42224678350) 成功。
- [镜像发布与独立 runner 拉取验收](https://github.com/TeamSillyBees/glean/actions/runs/42225487107) 成功，run attempt 为 1。
- 本次前后端镜像均由生产主机匿名按 digest 拉取，并核对 Linux/amd64、镜像 digest 和提交标签。未向服务器保存 GitHub token。

```text
ghcr.io/teamsillybees/glean-backend@sha256:3d81a2c64670fb86968439c15111bc335e789bf242d0c3a17dec42dcf8074fa5
ghcr.io/teamsillybees/glean-web@sha256:e2691332e4d744a96a99583fee4e8bc486a510890254ca621875ca53e7bbb316
```

完整基线导入结果：**38,989 篇去重论文、39,495 条主题成员关系、0 条隔离失败**。其中生物医学 AI 31,399 条、LLM/Agent 4,059 条、生物医学世界模型 4,037 条。报告位于 `backups/initialization-1060363/import-report.json`。

首次管理员为 `admin`（ID `1000000000001`），随机初始密码保存在服务器 `config/admin-initial.txt`，未写入 Git、镜像或本记录。用 root 在私有终端读取，登录后通过“个人研究空间 → 修改密码”设置自己的密码。

后端 Snowflake node 为 154；首次导入使用 155，配置 doctor 使用 156。数据库应用角色为 `glean_app`，没有 superuser/CREATEDB/CREATEROLE 权限。PostgreSQL/Redis/后端/Web 的容器内存上限分别为 640/320/512/128 MiB；数据库连接池 10/5。

## 2026-11-06 检索功能升级

19 个功能提交已从 `codex/general-literature-20261106` 快进合并并推送到 `main`。功能版本为 `c9eee61`；后续仅记录部署结果的文档提交不改变运行镜像。

升级前停止后端写入并完成 PostgreSQL、角色、Redis、应用数据和配置备份，然后通过 Atlas 应用：

- `20261105000000_topic_search_routes.sql`
- `20261106000000_retrieval_protocol_audit.sql`

数据库迁移版本为 `20261106000000`。新增四张检索审计表及协议/证据字段，物理外键数量仍为 0。迁移前后论文、成员关系、用户、主题配置、审核、修改历史和观察记录数量完全一致。保持原有 Compose、数据库/Redis/JWT 凭据、管理员密码和运行开关，仅更新 `.env` 的版本与镜像引用。

版本切换于 2026-11-06 21:33（Asia/Shanghai）完成。升级前备份位于 `backups/before-update-c9eee61-20261106T133309Z/`，其中保留旧镜像引用、迁移日志、数据数量对比、目标镜像清单和生产验收报告。旧版镜像也保留在本机；此次没有执行回滚。

## 验收与备份

已通过：

- Compose 配置、Nginx 语法、PostgreSQL/Redis 健康检查和后端 `/ready`。
- 公网 HTTPS 证书校验、首页/静态资源、安全响应头、HTTP→HTTPS 跳转。
- 未登录 API 返回 401、CORS 白名单、管理员登录/身份、三个主题精确数量、论文列表和真实来源证据。
- 新增长期运行列表、共享模型预算、检索模板与协议计划预览接口；当前模型累计调用为 0。
- 退出当前验收会话后原 access token 失效；密码与 token 未输出到验收日志。
- Let’s Encrypt 模拟续期及 Nginx reload。

功能提交的完整 CI 已通过 Go/PostgreSQL、前端、真实浏览器、容器以及备份恢复验证，镜像发布后另在全新 runner 按 digest 拉取验收。本次生产 HTTP 验收与升级记录在 `backups/before-update-c9eee61-20261106T133309Z/acceptance.json`、`deployment.json`；首次上线验收仍保留在 `backups/initialization-1060363/acceptance.json`。

备份目录：

- `before-layout-20261105T142305Z/`：整理前完整布局、空库 dump、角色及 SHA256 校验，保留原凭据和调整前状态。
- `initialization-1060363/`：私有迁移/seed/import/doctor 日志、导入报告及 HTTP 验收结果。
- `before-update-c9eee61-20261106T133309Z/`：此次升级前一致性备份、SHA256 清单及迁移/切换/验收记录；`COMPLETE` 表示备份完成，`DEPLOYED` 表示已切换，`VERIFIED` 表示最终数据和服务核对通过。
- `deployed-*/`：上线后短暂停写生成的 PostgreSQL dump、角色、Redis RDB、应用数据、私有配置/证书归档、版本和 SHA256 清单；`COMPLETE` 表示各备份步骤及结构校验完成。

生产备份包含秘密，只留在服务器私有目录。已核验 dump 目录、tar 结构、Redis RDB 和文件校验和；尚未对这些完整生产备份做异机恢复，也未配置周期备份或异机副本。先前环境准备中的隔离迁移/恢复演练不等于这份备份的恢复验收。

## GitHub CD 与后续变更

`production` Environment 的 SSH 账号、专用密钥及 base 变量继续保留，`PRODUCTION_DEPLOY_ENABLED` 仍为 **`false`**，符合此前要求。镜像发布工作流可以独立使用。

**当前 `deploy.yml` / `scripts/production` 仍面向原 releases/state/ops.env 布局，不适用于本机整理后的单 Compose 布局。** 本次是在服务器直接部署已验证的 main 镜像，没有运行该 CD 工作流。后续启用 CD 前，先适配并测试备份、迁移、镜像更新及状态管理，再完成 GitHub Environment 审批配置；不能直接打开旧开关覆盖当前实例。

当前采集与模型调用保持关闭。启用前须另做有界真实来源验收，并配置相应模型端点/密钥。没有通过生成测试数据或 mock 代替真实历史数据。
