package apperrors

import "net/http"

// Code 定义全局统一错误码，所有平台错误和业务错误都应从这里衍生。
type Code string

// 标准错误码常量是 HTTP 层、业务层和测试断言之间的稳定契约。
const (
	// CodeOK 表示请求处理成功。
	CodeOK Code = "OK"
	// CodeBadRequest 表示请求参数或格式错误。
	CodeBadRequest Code = "BAD_REQUEST"
	// CodeUnauthorized 表示请求未通过身份认证。
	CodeUnauthorized Code = "UNAUTHORIZED"
	// CodeForbidden 表示请求方无权执行目标操作。
	CodeForbidden Code = "FORBIDDEN"
	// CodeNotFound 表示请求的资源不存在。
	CodeNotFound Code = "NOT_FOUND"
	// CodeInternalError 表示服务内部发生未分类错误。
	CodeInternalError Code = "INTERNAL_ERROR"
	// CodeDBError 表示数据库访问或事务操作失败。
	CodeDBError Code = "DB_ERROR"
	// CodeCacheError 表示缓存读取、写入或失效操作失败。
	CodeCacheError Code = "CACHE_ERROR"
)

// 标准 cause domain 常量用于 oops 内部元数据和日志归类，不作为业务错误码返回给客户端。
const (
	// DomainDB 表示数据库访问、迁移或连接错误。
	DomainDB = "db"
	// DomainRedis 表示 Redis client、锁或脚本错误。
	DomainRedis = "redis"
	// DomainCache 表示业务缓存读写错误。
	DomainCache = "cache"
	// DomainHTTPClient 表示下游 HTTP Client 调用错误。
	DomainHTTPClient = "httpclient"
	// DomainStorage 表示文件或对象存储错误。
	DomainStorage = "storage"
	// DomainAuth 表示认证、授权或 token 错误。
	DomainAuth = "auth"
	// DomainTask 表示任务队列或调度错误。
	DomainTask = "task"
	// DomainServer 表示 Web Server、中间件或框架装配错误。
	DomainServer = "server"
)

// 标准 cause hint 常量用于日志排障提示，不进入 HTTP 响应。
const (
	// HintDBConnection 提示排查数据库连接类问题。
	HintDBConnection = "检查数据库 DSN、网络连通性和账号权限"
	// HintRedisTimeout 提示排查 Redis 超时类问题。
	HintRedisTimeout = "检查 Redis 地址、连接池配置和慢命令"
	// HintJWTValidation 提示排查 JWT 校验类问题。
	HintJWTValidation = "检查 token 签名、过期时间和密钥配置"
	// HintStorageUpload 提示排查对象存储上传类问题。
	HintStorageUpload = "检查 bucket、region、AK/SK 和 STS 权限"
	// HintHTTPClientCall 提示排查 HTTP Client 调用类问题。
	HintHTTPClientCall = "检查 endpoint、timeout 和重试策略"
)

// defaultStatuses 描述错误码到 HTTP 状态码的默认映射。
var defaultStatuses = map[Code]int{
	CodeBadRequest:    http.StatusBadRequest,
	CodeUnauthorized:  http.StatusUnauthorized,
	CodeForbidden:     http.StatusForbidden,
	CodeNotFound:      http.StatusNotFound,
	CodeInternalError: http.StatusInternalServerError,
	CodeDBError:       http.StatusInternalServerError,
	CodeCacheError:    http.StatusInternalServerError,
}
