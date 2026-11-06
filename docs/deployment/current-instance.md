# 当前线上实例

最近部署记录：2026-11-07 00:57（Asia/Shanghai）。实例为 [Glean](https://glean.starxy.cc)，主机 `47.104.200.154`，base 为 `/root/docker/glean`。本页维护最近记录的运行状态与操作入口；功能历史见 [Changelog](../../CHANGELOG.md)。

## 版本与开关

| 项目                          | 最近已核对状态                                     |
| ----------------------------- | -------------------------------------------------- |
| 功能提交                      | `3189c048554a87aebd199b7c8709123354965a14`         |
| 数据库迁移                    | `20261106000000`，物理外键为 0                     |
| 历史导入                      | 38,989 篇论文、39,495 条主题成员关系、0 条导入隔离 |
| 自动采集 / worker / scheduler | 关闭                                               |
| 模型处理                      | 关闭                                               |
| GitHub 服务器部署开关         | `PRODUCTION_DEPLOY_ENABLED=false`                  |
| 运行节点                      | 后端 154；首次导入 155、配置 doctor 156            |

该提交的 [CI](https://github.com/TeamSillyBees/glean/actions/runs/42255439217) 和[镜像发布/独立拉取验证](https://github.com/TeamSillyBees/glean/actions/runs/42256355039)通过。运行镜像为：

```text
ghcr.io/teamsillybees/glean-backend@sha256:d98653405addb8707b714b5cfe4a547369f3225e11f33ea5bec0089c13e5e4a4
ghcr.io/teamsillybees/glean-web@sha256:1fb54aeae985f3abcb017c0e980b35ab29d8defa497acf3ffb80766cd088331a
```

## 布局与管理

```text
/root/docker/glean/
├── compose.yaml
├── .env                      # 提交号与不可变镜像引用
├── config/
│   ├── app/                  # 应用 YAML、RBAC、数据库 CA
│   ├── backend.env           # 数据库/Redis/JWT 配置与运行开关
│   ├── admin-initial.txt     # 首次管理员凭据，root 0600
│   ├── postgres/             # 认证与 TLS 配置
│   ├── redis/
│   ├── nginx/                # HTTPS、API 代理与 ACME webroot
│   └── letsencrypt/          # 证书、续期配置与 reload hook
├── data/
│   ├── app/
│   ├── postgres/
│   └── redis/
└── backups/
```

一个名为 `glean` 的 Compose 项目管理 `postgres`、`redis`、`backend`、`web`。容器使用 `restart: unless-stopped`，由 Docker 恢复运行，没有 systemd Compose 服务；宿主 Nginx 禁用，入口由 Web 容器提供。

```sh
cd /root/docker/glean
docker compose ps
docker compose up -d --wait --wait-timeout 90
docker compose logs --tail 100 backend web
```

人工 stop 过的容器需重新 `up -d`。不要用 `down -v` 或清空持久目录处理故障。修改 `config/backend.env` 后，用对应 Compose 配置重建后端；仅 restart 不会重新读取 env_file。

## 网络与证书

| 入口        | 配置                                                                                     |
| ----------- | ---------------------------------------------------------------------------------------- |
| 公网        | `glean.starxy.cc` → `47.104.200.154`，TCP 80 / 443                                       |
| HTTP        | 普通请求 308 跳转 HTTPS，保留 ACME 和健康路径                                            |
| Web         | 非 root Nginx，UID/GID `101:101`；容器 8080/8443 映射宿主 80/443                         |
| API         | Web 直接代理 `backend:8080` 并覆盖转发头                                                 |
| 后端        | 宿主仅 `127.0.0.1:8080`；可信代理为专用 Docker 网段 `172.30.154.0/24`                    |
| PostgreSQL  | 已记录版本 16.15，宿主仅 `127.0.0.1:5432`；应用连接 `glean-postgres:5432`，`verify-full` |
| Redis       | 已记录版本 7.4.12，仅 Compose 网络，认证、AOF/everysec、`noeviction`                     |
| CORS / 文档 | 仅允许 `https://glean.starxy.cc`，匿名 API 文档关闭                                      |

Let’s Encrypt 公网证书已记录到期日为 **2027-02-03**。系统 `certbot.timer` 负责续期；Glean drop-in 指向 `config/letsencrypt`，deploy hook 调整权限、检查 Nginx 并 reload。它不负责 Compose 启动。

```sh
systemctl list-timers certbot.timer
certbot renew --dry-run --run-deploy-hooks --no-random-sleep-on-renew \
  --config-dir /root/docker/glean/config/letsencrypt
```

上述续期演练已于 2026-11-06 通过。HTTP-01 需要公网 80 可达；数据库使用独立私有 CA，已记录证书到期日 **2027-12-05**，不由 Certbot 续期。

## 账号与资源

首次管理员为 `admin`（ID `1000000000001`），初始随机密码仅保存在服务器 `config/admin-initial.txt`。登录后可通过「个人研究空间 → 修改密码」设置自己的密码；旧会话随改密失效。更新部署保留现有凭据，不重新 seed。

数据库应用角色 `glean_app` 不具备 superuser / CREATEDB / CREATEROLE。PostgreSQL、Redis、后端、Web 的容器内存上限分别为 640、320、512、128 MiB；数据库连接池为 10/5。

## 备份与验证记录

所有下列路径相对于 `backups/`，包含秘密的备份保持私有：

| 路径                                      | 用途                                                          |
| ----------------------------------------- | ------------------------------------------------------------- |
| `before-layout-20261105T142305Z/`         | 整理前布局、数据库、角色及校验清单                            |
| `initialization-1060363/`                 | 首次迁移/seed/import/doctor、导入报告和上线验收               |
| `before-update-c9eee61-20261106T133309Z/` | 检索迁移升级前备份与迁移/切换记录                             |
| `before-update-7fa0b91-20261106T150459Z/` | 页面易用性升级前备份                                          |
| `before-update-ab256b4-20261106T162836Z/` | 连通性功能升级前备份及逐源结果                                |
| `before-update-3189c04-20261106T165715Z/` | 最近导航修复前备份、旧镜像、HTTP 验收和部署记录               |
| `deployed-*/`                             | 上线后停写备份，包括数据库、角色、Redis、应用数据、配置与证书 |

最近版本的 `acceptance.json`、`deployment.json` 位于对应 `before-update-3189c04-…` 目录；`COMPLETE` 表示备份步骤完成，`DEPLOYED` 表示切换完成，`VERIFIED` 表示最终核对通过。

已验证服务健康、HTTPS、登录/退出、数据数量、目录与监控 API、部署按钮代码、证书模拟续期，以及 dump/tar/RDB 和 SHA256。2026-11-07 00:29 的六源轻量连通性测试全部成功，前后采集状态一致；它不证明完整采集或长期可用性。

仍待完成：完整生产备份的异机恢复、周期备份与异机副本、六源持续采集验收、1Password 扩展弹窗直接复测。待办统一见 [Roadmap](../roadmap.md)。

## 后续升级

镜像发布工作流可独立使用。服务器 `production` Environment 的专用 SSH 配置与 base 变量已准备，但 **`deploy.yml` / `scripts/production` 仍面向另一套 releases/state/ops.env 布局**。适配和恢复演练完成前保持服务器部署开关关闭，不直接运行旧脚本覆盖此实例。

新版本更新须记录目标提交和 digest、检查迁移兼容性、停写备份、显式迁移、切换并验证；保存旧镜像和恢复证据。采集与模型启用条件见[部署说明](README.md#后台开关)。
