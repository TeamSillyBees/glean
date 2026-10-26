# 认证会话与 HTTP 暴露安全

## 会话模型

access JWT 只携带 `userId`、随机 `sessionId`、`sessionVersion` 和必要的标准 JWT 声明。角色、权限和超级管理员状态不进入 token，请求时由 `auth.IdentityResolver` 从共享缓存或数据库解析。`sys_user.session_version` 初始为 1；全部退出、修改密码或禁用账号时递增，旧 access token 会在中间件版本比对阶段被拒绝，旧 refresh token 也不能继续轮转。

refresh token 是 opaque 随机值，Redis 只保存其指纹以及对应的用户、会话、版本和 access token JTI。`POST /api/v1/auth/logout` 校验 refresh token 属于当前 access 会话后原子消费它，并把配对 access token 加入剩余寿命黑名单。`POST /api/v1/auth/logout-all` 与 `PUT /api/v1/auth/password` 通过会话版本撤销该用户的全部旧会话；改密时密码哈希与版本递增位于同一 Ent 事务。

## 登录防护

`auth.login_protection` 同时配置账号和来源 IP 固定窗口限流，以及账号连续失败锁定。Redis Lua 脚本保证多实例计数、锁定切换和成功清零原子执行；账号和 IP 在 Redis key 中只使用 SHA-256 指纹。未知账号、禁用账号、错误密码、限流和锁定均返回统一 `LOGIN_FAILED` 响应，内部安全审计日志才记录拒绝原因、账号、可信来源 IP 和链路标识，且不记录密码或 token。

只有 dev/local/test 在显式允许内存 token store 时可使用进程内登录防护；共享环境必须启用 Redis 和登录防护。可信代理通过 `server.trusted_proxies` 配置，只有直连来源命中该白名单时才信任转发 IP 头。

## OpenAPI、CORS 与文档

Web 层注册名为 `bearerAuth` 的 HTTP Bearer/JWT security scheme。模块在 Huma operation 之后登记 `RouteSecurity` 时，注册表自动为公开接口写入空 security，为认证/权限接口写入 Bearer 要求，并补充 401；权限接口额外补充 403。

`server.cors` 使用 origin、method、header 明确白名单并验证预检请求。共享环境禁止任意来源、方法或请求头通配符。`server.docs.enabled` 控制 `/docs`、`/openapi*` 和 `/schemas/*`；这些匿名文档路由只允许在 dev/local/test 开启，其他环境必须关闭。
