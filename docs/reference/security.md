# 认证、权限与 HTTP 安全

## 会话模型

access JWT 只携带 `userId`、随机 `sessionId`、`sessionVersion` 和必要的标准 JWT 声明。角色、权限和超级管理员状态不进入 token，请求时由 `auth.IdentityResolver` 从共享缓存或数据库解析。`sys_user.session_version` 初始为 1；全部退出、修改密码或禁用账号时递增，旧 access token 会在中间件版本比对阶段被拒绝，旧 refresh token 也不能继续轮转。

refresh token 是 opaque 随机值，Redis 只保存其指纹以及对应的用户、会话、版本和 access token JTI。`POST /api/v1/auth/logout` 校验 refresh token 属于当前 access 会话后原子消费它，并把配对 access token 加入剩余寿命黑名单。`POST /api/v1/auth/logout-all` 与 `PUT /api/v1/auth/password` 通过会话版本撤销该用户的全部旧会话；改密时密码哈希与版本递增位于同一 Ent 事务。

## 登录防护

`auth.login_protection` 同时配置账号和来源 IP 固定窗口限流，以及账号连续失败锁定。Redis Lua 脚本保证多实例计数、锁定切换和成功清零原子执行；账号和 IP 在 Redis key 中只使用 SHA-256 指纹。未知账号、禁用账号、错误密码、限流和锁定均返回统一 `LOGIN_FAILED` 响应，内部安全审计日志才记录拒绝原因、账号、可信来源 IP 和链路标识，且不记录密码或 token。

只有 dev/local/test 在显式允许内存 token store 时可使用进程内登录防护；共享环境必须启用 Redis 和登录防护。可信代理通过 `server.trusted_proxies` 配置，只有直连来源命中该白名单时才信任转发 IP 头。

## OpenAPI、CORS 与文档

Web 层注册名为 `bearerAuth` 的 HTTP Bearer/JWT security scheme。模块在 Huma operation 之后登记 `RouteSecurity` 时，注册表自动为公开接口写入空 security，为认证/权限接口写入 Bearer 要求，并补充 401；权限接口额外补充 403。

`server.cors` 使用 origin、method、header 明确白名单并验证预检请求。共享环境禁止任意来源、方法或请求头通配符。`server.docs.enabled` 控制 `/docs`、`/openapi*` 和 `/schemas/*`；这些匿名文档路由只允许在 dev/local/test 开启，其他环境必须关闭。

## 数据库 RBAC

权限策略以数据库角色、权限资源和授权关系为唯一事实源。

### 稳定标识与请求链路

- 权限使用 `domain:resource:action` 风格的稳定字符串，例如 `system:user:read`。
- 权限路由通过 `RouteSecurity.Permission` 直接登记权限标识。
- Casbin 模型为 `p(role_code, permission_code)`；adapter 只读取有效的 `sys_role`、`sys_menu`、`sys_role_menu`，不允许 Casbin API 反向写库。
- access JWT 只保存 `userId`、`sessionId`、`sessionVersion` 和必要的标准 JWT 声明，不保存角色、权限或租户快照。中间件按请求从 Redis 缓存或数据库解析当前启用用户、会话版本、有效角色和超级管理员状态，因此会话撤销、角色撤销、角色禁用和用户禁用不受旧 token 过期时间影响。

### 管理与审计

`rbac` 模块提供角色、权限资源、用户角色和角色权限的查询、创建、更新、删除或完整集合替换接口。关系替换、软删除和审计字段写入在同一 Ent 事务中完成；事务提交后才执行缓存失效与策略通知。

保护规则如下：

- 角色编码和权限编码创建后不可修改，避免路由与策略引用漂移。
- 内置角色不可禁用或删除；普通角色仍被用户引用时不可删除。
- 只有启用且未删除的用户能以超级管理员身份绕过普通策略；超级管理员不依赖 `admin` 角色。
- 禁止禁用、降级或删除最后一个启用且未删除的超级管理员。
- 禁用角色后，它不再进入用户请求身份和 Casbin 策略；删除权限资源会同时软删除其角色授权关系。

### 缓存与多实例

用户当前角色和超级管理员状态按用户缓存在 Redis。用户、角色或用户角色变化后主动删除受影响用户缓存；角色权限、角色启用状态或权限资源变化后，本实例立即调用 `LoadPolicy`，并通过 Redis Pub/Sub 通知其他实例重载。Redis 不可用时鉴权保持 fail-closed；只在 dev/local/test 明确关闭 Redis 时退化为直接查库和本实例重载。

安全报告渠道见 [SECURITY.md](../../SECURITY.md)，生产配置入口见[部署说明](../deployment/README.md)。
