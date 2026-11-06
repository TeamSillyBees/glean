# GitHub Actions：镜像发布与部署

镜像发布与服务器部署是两个独立的手动工作流，均只接受 `workflow_dispatch`，不会因 push、PR、CI 完成或定时任务自动触发：

| 工作流                                             | 职责                                                  | 权限与边界                                                                                                    |
| -------------------------------------------------- | ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `ci.yml` / **Glean CI**                            | 隔离测试、构建和容器 HTTP 验证                        | 不发布镜像，不访问生产                                                                                        |
| `publish-images.yml` / **Glean image publication** | 构建前后端、推送 GHCR、在新 runner 拉取 digest 并验证 | 仅推送 job 有 `packages: write`；验证 job 只有 `packages: read`；不使用 `production` Environment 或服务器凭据 |
| `deploy.yml` / **Glean production deployment**     | 消费已验证的镜像清单，审批后执行服务器部署            | 不构建、不推送、不申请包写入权限；必须启用 `PRODUCTION_DEPLOY_ENABLED`                                        |

真实来源验收仍是另一个独立手动工作流。`47.104.200.154` 已直接部署到 [Glean](https://glean.starxy.cc)，使用一个主 `compose.yaml`，见[实际部署记录](current-instance.md)。服务器部署开关保持 **`false`**；发布镜像不会修改它，也不会访问服务器。**本页 CD 工作流仍使用原 releases/state/ops.env 布局，尚未适配该主机的新布局，不能直接启用。**

## 先发布并验证镜像（不需要启用服务器部署）

1. 提交代码到 `main`，等待该完整 40 位小写 SHA 的 `ci.yml` **push run 最新一次执行成功**。PR merge SHA、其他分支、旧提交、跳过或未完成的 CI 均不算。
2. Actions → **Glean image publication** → Run workflow，选择 `main`，`commit` 填上述 SHA。工作流要求输入 SHA 同时等于触发工作流的 SHA 和当前 main，校验阶段不 checkout 任意输入代码。
3. 在 GitHub 托管 `ubuntu-24.04` 上构建 Linux amd64 的 `ghcr.io/teamsillybees/glean-backend` 与 `ghcr.io/teamsillybees/glean-web`，使用 `<sha>-<run_id>-<attempt>` 唯一标签；不写 `latest`。构建步骤不注入 registry token 或生产凭据。
4. 推送步骤仅使用该 job 的临时 `GITHUB_TOKEN`，不需要 PAT；token 只经 stdin 登录临时 `DOCKER_CONFIG`，退出时清理。GitHub 必须允许 `packages: write`；既有包须授权本仓库 Actions 访问。
5. 另一台全新 runner 使用只读 token，按推送后获取的 `@sha256:…` 拉取两个镜像，核对源码提交、仓库标签、Linux amd64 和非 root 用户，再用隔离 PostgreSQL / Redis 验证后端 readiness、前端静态资源与 SPA、API 代理及未登录保护。与普通 CI 复用 `scripts/ci/container-smoke.sh`；不调用外部文献源或模型，不连接生产数据库，测试资源退出时清理。
6. 只有拉取和 HTTP 验证成功才保存 `glean-images-<attempt>` artifact，其中 `images.json` 记录提交、run ID、attempt 和两个不可变 digest，保留 90 天。保留成功 run 链接和摘要；**推送完成但验证失败不等于可部署发布**。清单过期后须重新发布当前 main。

仓库 public **不表示 GHCR 包自动 public**；首次创建通常仍为 private，镜像发布工作流不会更改包可见性。独立 runner 的认证拉取验证与未来生产服务器的拉取权限是两项检查；公开包可匿名拉取，私有包需要服务器既有的独立只读 registry 身份。不要把发布 token 传给服务器。镜像成功发布不表示应用已经上线。

## 服务器部署前：管理员必须完成

1. 按[生产部署与恢复手册](release-tools.md)准备服务器、数据库 TLS、备份与恢复演练、Atlas、Docker Compose、服务器本地配置和运行权限。工作流不会自动创建数据库、账号、管理员 seed 或外部基础设施。
2. 在 GitHub `Settings → Environments` 创建 **`production`**，设置 required reviewers、禁止自己批准、禁止管理员绕过，并将允许部署的 **branch 限为 `main`**（不是同名 tag）。保护 main，要求代码审查和 `Glean CI` 成功。GitHub 的套餐 / 仓库可见性可能限制 required reviewers；若当前套餐无法提供审批保护，不要启用本工作流，应先配置支持的审批机制。仅写 `environment: production` **不会自动创建人工审批规则**。
3. 在这个 Environment（不要放到普通仓库级）设置以下值；经过上述准备后才将开关设为 `true`：

   | 类型     | 名称                        | 内容                                                            |
   | -------- | --------------------------- | --------------------------------------------------------------- |
   | Variable | `PRODUCTION_DEPLOY_ENABLED` | 默认缺省即拒绝；完成准备后显式设为 `true`                       |
   | Variable | `PRODUCTION_BASE_DIR`       | 必填，无默认；服务器应用专用绝对目录，如 `/srv/glean`（仅示例） |
   | Variable | `PRODUCTION_SSH_HOST`       | IPv4 或 DNS 主机名；此基线不接受 IPv6 字面量                    |
   | Variable | `PRODUCTION_SSH_USER`       | 已存在的专用部署账号                                            |
   | Variable | `PRODUCTION_SSH_PORT`       | SSH 端口，缺省 `22`                                             |
   | Secret   | `PRODUCTION_SSH_KEY`        | 已授权部署账号的私钥；不由工作流生成                            |

   按本项目选择，SSH / SCP **不检查服务器主机密钥**，无需配置 known_hosts Secret，也没有可选严格模式。连接仍用部署账号私钥认证且禁止交互密码；这不等于认证了服务器身份，错误 DNS、被冒充的主机或中间人可能接收发布内容并干预部署。此取舍不改变数据库 TLS、私钥保密或数据完整性要求。

   base 仅允许由 ASCII 字母、数字、`_`、`-`、`.` 组成的路径段；禁止根目录、空格、引号、控制字符、`.` / `..` 段、重复或末尾斜杠、符号链接组件。详细布局与迁移方法见生产手册。

4. 按生产手册在服务器预建 `$PRODUCTION_BASE_DIR` 及其固定子目录，允许该账号在 `$PRODUCTION_BASE_DIR/releases` 建立发布目录；保护 base 及父目录不被其他用户改写。base 本身及 `config/`、`releases/`、`backups/`、`state/`、`recovery/` 各控制目录须为该部署账号所有、`0700`，不是常见的 `0755`；具体准备示例及容器数据目录权限见生产手册。工作流不调用 sudo，也不安装软件。账号需要运行部署脚本所需的 Docker / Atlas / 备份权限；Docker socket 权限实际上接近主机 root，应用专用主机和受限部署账号控制风险。SSH 服务端需提供 POSIX shell，部署脚本需要 Bash。
5. 服务器 `$PRODUCTION_BASE_DIR/config/ops.env`、数据库 / Redis / JWT 密钥、证书、备份目录和部署状态均留在服务器；不上传 GitHub，不进入构建参数或制品。服务器必须已经能拉取这两个 GHCR 包：
   - `ghcr.io/teamsillybees/glean-backend`
   - `ghcr.io/teamsillybees/glean-web`

   私有包需要**独立授权的既有服务器只读 registry 身份**；公开包可以匿名拉取。首次创建的 GHCR 包未必公开。工作流不自动改包可见性、不创建 PAT、不将发布 token 传给服务器；尚无合适拉取权限时应停在准备阶段。GitHub Actions 必须被允许使用 `packages: write`，既有包也须允许本仓库 Actions 访问。

6. 发布工作流构建 Linux amd64 镜像，服务器架构应一致；需要 ARM / 多平台时应先扩展构建和验收。

## 再手动部署服务器

1. 先按上文完成当前 main 的镜像发布，取得成功的 **Glean image publication** run ID。部署同样要求此 SHA 的最新 push CI 成功。
2. Actions → **Glean production deployment** → Run workflow，选择 `main`，`commit` 填完整 SHA，`image_run_id` 填上述 run ID。验证阶段检查 run 属于本仓库 `publish-images.yml`、main、手动触发、相同 SHA 且成功；下载当前 attempt 的清单并严格验证两个镜像的仓库路径与 digest。不能输入任意镜像、可移动 tag 或其他工作流的 artifact。
3. 审批人检查 SHA、镜像发布记录、迁移兼容性和备份可恢复性后批准 `production`。批准后仍需 `PRODUCTION_DEPLOY_ENABLED=true`；随后重新检查当前 main、同 SHA 的最新 CI，以及镜像发布 run 的状态和 attempt。审批期间重新运行过发布工作流时拒绝沿用旧清单。
4. 部署只使用清单中的 **`@sha256:…`**，不重新构建或推送。服务器配置和 registry 只读登录继续留在服务器。
5. SSH 前再次检查 main 未变化；若变化，退出且不触碰服务器，需对新 main 重新发布和部署。此检查不是对 main 的锁，但本次发布身份始终是已审批的不可变 SHA / digest。
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

- 镜像发布的 `glean-image-publication` 与服务器部署的 `glean-production-deployment` 使用独立并发组。后者串行化部署工作流，不取消正在运行的生产部署。GitHub concurrency 不是可靠 FIFO 队列：新的等待运行可能替代较早的等待运行，不应批量排队发布。服务器脚本的锁负责额外防止直接手动部署并发。
- 禁止把 SSH 密钥 / DB 密钥放在 checkout、构建或公共 CI 中。SSH Secrets 仅注入最后的传输 / 部署步骤；不使用 `pull_request_target`、PR 代码 + Secrets 或第三方 SSH action。
- SSH 断线、job 超时 / 取消不能证明服务器操作已停止。检查服务器部署锁、进程、备份、迁移状态和 readiness 后再决定恢复；不要立即并发重试或删除数据库。失败的传输可能留下未激活的发布目录，人工核实后清理。
- 工作流不自动恢复数据库、不自动删除旧镜像 / 备份 / 发布目录、不自动回滚 schema。需要回退时使用生产手册的显式恢复流程；本工作流只接受当前 main，不是历史版本回滚入口。
- 静态安全测试和 shell 语法检查不等于真实 GHCR / SSH / 生产验收。镜像工作流成功仅证明 GHCR 推送、独立 runner 拉取及隔离 HTTP 验证通过；服务器链路仍须获得明确上线授权并完成配置后另行演练。

## 官方依据

- [GitHub Actions workflow 语法与权限](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)
- [Environment 审批、分支限制和套餐边界](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
- [Actions 安全实践：最小权限、SHA 固定和避免脚本注入](https://docs.github.com/en/actions/reference/security/secure-use)
- [使用 GITHUB_TOKEN 发布容器镜像](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)
- [按 head_sha 查询 workflow runs](https://docs.github.com/en/rest/actions/workflow-runs#list-workflow-runs-for-a-workflow)
- [本工作流固定的 checkout v6.0.2 commit](https://github.com/actions/checkout/commit/de0fac2e4500dabe0009e67214ff5f5447ce83dd)

- [GitHub Packages 包可见性](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility)
- [不可变 Actions artifact 与保留期](https://github.com/actions/upload-artifact)
