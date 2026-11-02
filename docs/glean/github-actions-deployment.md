# GitHub Actions 手动生产部署

`.github/workflows/ci.yml` 仍只进行隔离测试和不发布镜像的构建；真实来源验收仍是独立手动工作流。新增 `deploy.yml` **只接受 `workflow_dispatch`**，不会因 push、PR、CI 完成或定时任务自动发布 / 上线。

此处交付的是配置，不表示生产环境已经存在、Secrets 已配置、镜像已发布或部署成功。本次开发不创建持久凭据、不修改仓库 Secrets，也不运行该部署工作流。

## 首次使用前：管理员必须完成

1. 按[生产部署与恢复手册](production.md)准备服务器、数据库 TLS、备份与恢复演练、Atlas、Docker Compose、服务器本地配置和运行权限。工作流不会自动创建数据库、账号、管理员 seed 或外部基础设施。
2. 在 GitHub `Settings → Environments` 创建 **`production`**，设置 required reviewers、禁止自己批准、禁止管理员绕过，并将允许部署的 **branch 限为 `main`**（不是同名 tag）。保护 main，要求代码审查和 `Glean CI` 成功。GitHub 的套餐 / 仓库可见性可能限制 required reviewers；若当前套餐无法提供审批保护，不要启用本工作流，应先配置支持的审批机制。仅写 `environment: production` **不会自动创建人工审批规则**。
3. 在这个 Environment（不要放到普通仓库级）设置以下值；经过上述准备后才将开关设为 `true`：

   | 类型 | 名称 | 内容 |
   | --- | --- | --- |
   | Variable | `PRODUCTION_DEPLOY_ENABLED` | 默认缺省即拒绝；完成准备后显式设为 `true` |
   | Variable | `PRODUCTION_BASE_DIR` | 必填，无默认；服务器应用专用绝对目录，如 `/srv/glean`（仅示例） |
   | Variable | `PRODUCTION_SSH_HOST` | IPv4 或 DNS 主机名；此基线不接受 IPv6 字面量 |
   | Variable | `PRODUCTION_SSH_USER` | 已存在的专用部署账号 |
   | Variable | `PRODUCTION_SSH_PORT` | SSH 端口，缺省 `22` |
   | Secret | `PRODUCTION_SSH_KEY` | 已授权部署账号的私钥；不由工作流生成 |

   按本项目选择，SSH / SCP **不检查服务器主机密钥**，无需配置 known_hosts Secret，也没有可选严格模式。连接仍用部署账号私钥认证且禁止交互密码；这不等于认证了服务器身份，错误 DNS、被冒充的主机或中间人可能接收发布内容并干预部署。此取舍不改变数据库 TLS、私钥保密或数据完整性要求。

   base 仅允许由 ASCII 字母、数字、`_`、`-`、`.` 组成的路径段；禁止根目录、空格、引号、控制字符、`.` / `..` 段、重复或末尾斜杠、符号链接组件。详细布局与迁移方法见生产手册。
4. 按生产手册在服务器预建 `$PRODUCTION_BASE_DIR` 及其固定子目录，允许该账号在 `$PRODUCTION_BASE_DIR/releases` 建立发布目录；保护 base 及父目录不被其他用户改写。base 本身及 `config/`、`releases/`、`backups/`、`state/`、`recovery/` 各控制目录须为该部署账号所有、`0700`，不是常见的 `0755`；具体准备示例及容器数据目录权限见生产手册。工作流不调用 sudo，也不安装软件。账号需要运行部署脚本所需的 Docker / Atlas / 备份权限；Docker socket 权限实际上接近主机 root，应用专用主机和受限部署账号控制风险。SSH 服务端需提供 POSIX shell，部署脚本需要 Bash。
5. 服务器 `$PRODUCTION_BASE_DIR/config/ops.env`、数据库 / Redis / JWT 密钥、证书、备份目录和部署状态均留在服务器；不上传 GitHub，不进入构建参数或制品。服务器必须已经能拉取这两个 GHCR 包：
   - `ghcr.io/teamsillybees/glean-backend`
   - `ghcr.io/teamsillybees/glean-web`

   私有包需要**独立授权的既有服务器只读 registry 身份**；公开包可以匿名拉取。首次创建的 GHCR 包未必公开。工作流不自动改包可见性、不创建 PAT、不将发布 token 传给服务器；尚无合适拉取权限时应停在准备阶段。GitHub Actions 必须被允许使用 `packages: write`，既有包也须允许本仓库 Actions 访问。
6. 该基线使用 GitHub 托管 `ubuntu-24.04` 构建 Linux amd64 镜像，服务器架构应一致；需要 ARM / 多平台时应先扩展构建和验收，而不是直接上线。

## 发布步骤

1. 确认当前 `main` 的完整 40 位小写 SHA。等待这个 **完全相同的 SHA** 的 `ci.yml` **push run 最新一次执行成功**；PR 的 merge SHA、其他分支的成功、旧提交成功、跳过 / 中止 / 尚在执行的 CI 均不算。
2. Actions → **Glean production deployment** → Run workflow，选择 `main`，`commit` 填该 SHA。提交号不是 branch、tag 或短 SHA。工作流只在原仓库的 main 上运行；验证阶段不 checkout 任意输入代码。
3. 审批人检查 SHA、对应 CI、迁移兼容性、备份可恢复性后批准 `production`。批准后重新验证 main 与同 SHA 的最新 CI，避免审批期间版本改变。
4. 工作流构建后使用当前 job 的临时 `GITHUB_TOKEN` 推送 GHCR；不需要 PAT。登录只存在于临时 `DOCKER_CONFIG`，退出时 logout 并清理。镜像使用 SHA / run ID / attempt 唯一标签，**部署参数始终使用 `@sha256:…` digest，不使用 `latest` 或其他可移动 tag**。
5. SSH 前再次检查 main 未变化；若此时已变化，会失败退出，可能已发布镜像但**未触碰生产服务器**，需对新的 main 重新运行。此检查不是对 main 的锁，后续 main 可以前进，但本次发布身份始终为审批过的不可变 SHA / digest。
6. 从验证过的 Git commit 创建归档，补入 `deploy/REVISION`。将归档传入全新的 `$PRODUCTION_BASE_DIR/releases/<sha>-<run_id>-<attempt>`；服务器拒绝符号链接路径，重复运行不会覆盖既有 release。工作流显式传入同一个 base 后运行以下命令（手动运行先 `export PRODUCTION_BASE_DIR=...`）：

   ```sh
   bash scripts/production/deploy.sh \
     '<40位提交SHA>' \
     'ghcr.io/teamsillybees/glean-backend@sha256:<64位digest>' \
     'ghcr.io/teamsillybees/glean-web@sha256:<64位digest>'
   ```

   发布、配置、数据、原始档案、备份、状态与隔离恢复文件均在 base 中；容器内部路径、Docker 全局缓存/日志及外置数据库/Redis 存储不因此改变。不要仅修改 base 来迁移已有实例，必须先按生产手册执行人工维护迁移。

7. 以工作流成功、服务器发布状态和实际 readiness 为证据；摘要仅在服务器脚本成功后记录 SHA / digests。备份、迁移、停写与失败恢复语义见生产手册。

## 失败、并发与边界

- `glean-production-deployment` 并发组串行化工作流，不取消正在运行的生产部署。GitHub concurrency 不是可靠 FIFO 队列：新的等待运行可能替代较早的等待运行，不应批量排队发布。服务器脚本的锁负责额外防止直接手动部署并发。
- 禁止把 SSH 密钥 / DB 密钥放在 checkout、构建或公共 CI 中。SSH Secrets 仅注入最后的传输 / 部署步骤；不使用 `pull_request_target`、PR 代码 + Secrets 或第三方 SSH action。
- SSH 断线、job 超时 / 取消不能证明服务器操作已停止。检查服务器部署锁、进程、备份、迁移状态和 readiness 后再决定恢复；不要立即并发重试或删除数据库。失败的传输可能留下未激活的发布目录，人工核实后清理。
- 工作流不自动恢复数据库、不自动删除旧镜像 / 备份 / 发布目录、不自动回滚 schema。需要回退时使用生产手册的显式恢复流程；本工作流只接受当前 main，不是历史版本回滚入口。
- 静态安全测试和 shell 语法检查不等于真实 GHCR / SSH / 生产验收。必须在获得明确上线授权及完成配置后，执行一次受保护的演练；未执行前不能宣称部署链路已经实测。

## 官方依据

- [GitHub Actions workflow 语法与权限](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)
- [Environment 审批、分支限制和套餐边界](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
- [Actions 安全实践：最小权限、SHA 固定和避免脚本注入](https://docs.github.com/en/actions/reference/security/secure-use)
- [使用 GITHUB_TOKEN 发布容器镜像](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)
- [按 head_sha 查询 workflow runs](https://docs.github.com/en/rest/actions/workflow-runs#list-workflow-runs-for-a-workflow)
- [本工作流固定的 checkout v6.0.2 commit](https://github.com/actions/checkout/commit/de0fac2e4500dabe0009e67214ff5f5447ce83dd)
