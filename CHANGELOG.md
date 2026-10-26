# 变更记录

本文件记录面向使用者和维护者的重要变化。新增条目先写入“未发布”，正式发布时再由维护者确定版本号、发布日期和迁移说明；本文件不自行承诺语义化版本兼容规则。

## 未发布

- 改为单一 Go module 的源码服务模板：根目录直接提供服务和测试，基础能力内置到 internal/platform。
- 移除独立脚手架 CLI、模板镜像、同步工具及旧开发技能分发和使用说明。
- 增加项目本地 setup，支持改名、随机本地 JWT 密钥与管理员 seed，重复执行保留已有凭据。
- 以当前 Ent schema 建立全新的无物理外键迁移基线，不兼容旧项目和数据库迁移历史。
- CI 直接验证模板、改名后的项目、Ent 再生成及真实 PostgreSQL 迁移和 seed。
- 修复项目改名误改配置值、空 YAML 分组无法写入密钥，以及改名后旧外部依赖检查失效的问题；CI 同时检查未跟踪的生成文件。
- 清理旧 examples 工作目录、过期事务待办、技能目录忽略项及 CLI 二进制发布规划。

## v0.1.20 - 2026-09-02

### 新增

- 增加安全报告、贡献流程和变更记录基础文档。
- 增加 golangci-lint v2 基础配置。
- 补齐存量导出声明注释，并增加基于 Go AST 的架构测试，阻止新增不规范注释。
- CLI 增加 `snippet add`、可加载的事务化 `config add` 和 `doctor --json`。
- API 模板增加随机 JWT secret、一次性管理员密码、安全 `app.slug`、事务化逻辑外键检查和 nonroot 容器基线。
- API 模板增加数据库 RBAC adapter、角色/权限/分配管理接口、Redis 身份缓存失效和 Casbin 多实例策略重载。
- API 模板增加当前会话退出、全部会话退出、修改密码后的全会话失效，以及 Redis 账号/IP 登录限流、连续失败锁定和安全审计。
- OpenAPI 增加 Bearer/JWT scheme，并从 `RouteSecurity` 自动生成 operation security 与 401/403 响应；CORS 和文档暴露改为环境化配置。

### 变更

- 移除语义失真的 `crud add` 和未生效的 `allow_multiple_devices` 配置。
- 配置加载改为拒绝未知字段；JWT 要求至少 32 字节密钥并拒绝无效用户 ID。
- refresh token 改为查询用户成功后原子轮转；Redis token store 初始化或访问异常时 fail-closed。
- HTTP Client 默认不记录错误响应正文，5xx 全量重试改为显式 opt-in。
- local storage 不再返回不会过期的伪 presign URL，改为明确返回不支持。
- `logx.Logger.Sync` 只负责刷新，资源关闭统一使用共享且幂等的 `Close`/`Shutdown`。
- access JWT 不再保存角色；路由改为直接登记稳定权限标识，`sys_role`、`sys_menu`、`sys_role_menu` 成为唯一权限事实源。
- access JWT 改为只保存用户 ID、会话 ID 和会话版本；`sys_user.session_version` 递增可批量使旧 access/refresh token 失效。

### 修复

- 修复本地上传 overwrite 竞态、代理头无条件信任、Redis SCAN glob 扩散、数据库 Ping 无超时和结构化日志脱敏不完整等问题。
- 修复 Windows 空格 replace、模块/配置半写入、模板换行假漂移和 checker 漏报根模块 `internal` import 等脚手架问题。

## 历史版本

仓库已经存在 `v0.1.0` 至 `v0.1.19` tag，但这些版本早于本变更记录建立。为避免根据提交标题猜测用户影响，此处不回填未经核实的逐版本条目；需要追溯时以对应 tag、提交历史和当时的可执行代码为准。

## 待所有者决定

- 版本兼容承诺和长期支持范围；
- 正式 release note 的审批人和发布渠道；
- SBOM、签名及 provenance 的实现方案。
