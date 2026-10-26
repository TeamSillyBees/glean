package storage

import (
	"context"
	"io"
	"time"
)

// Provider 表示底层文件或对象存储实现。
type Provider string

const (
	// ProviderLocal 表示本地文件系统存储。
	ProviderLocal Provider = "local"
	// ProviderAliyunOSS 表示阿里云 OSS。
	ProviderAliyunOSS Provider = "aliyun_oss"
	// ProviderTencentCOS 表示腾讯云 COS。
	ProviderTencentCOS Provider = "tencent_cos"
	// ProviderAWSS3 表示 AWS S3。
	ProviderAWSS3 Provider = "aws_s3"
	// ProviderS3Compatible 表示兼容 S3 协议的对象存储。
	ProviderS3Compatible Provider = "s3_compatible"
)

// ACL 表示对象访问控制策略。
type ACL string

const (
	// ACLDefault 表示使用存储桶默认 ACL。
	ACLDefault ACL = ""
	// ACLPrivate 表示私有读写。
	ACLPrivate ACL = "private"
	// ACLPublicRead 表示公开读。
	ACLPublicRead ACL = "public-read"
	// ACLPublicReadWrite 表示公开读写。
	ACLPublicReadWrite ACL = "public-read-write"
)

// Permission 表示临时凭证的权限范围。
type Permission string

const (
	// PermissionReadWrite 表示允许读写删除。
	PermissionReadWrite Permission = "read_write"
	// PermissionReadOnly 表示只读。
	PermissionReadOnly Permission = "read_only"
	// PermissionWriteOnly 表示只允许上传和删除。
	PermissionWriteOnly Permission = "write_only"
)

// Service 定义业务侧依赖的统一文件与对象存储接口。
type Service interface {
	Upload(ctx context.Context, input UploadInput) (*Object, error)
	Download(ctx context.Context, input DownloadInput) (io.ReadCloser, error)
	DownloadBytes(ctx context.Context, input DownloadInput) ([]byte, error)
	Delete(ctx context.Context, input DeleteInput) error
	DeleteBatch(ctx context.Context, input DeleteBatchInput) ([]string, error)
	Exists(ctx context.Context, input ObjectInput) (bool, error)
	Stat(ctx context.Context, input ObjectInput) (*Object, error)
	List(ctx context.Context, input ListInput) (*ListResult, error)
	Copy(ctx context.Context, input CopyInput) (*Object, error)
	Move(ctx context.Context, input CopyInput) (*Object, error)
	PresignUpload(ctx context.Context, input PresignInput) (*PresignedURL, error)
	PresignDownload(ctx context.Context, input PresignInput) (*PresignedURL, error)
	PublicURL(ctx context.Context, input ObjectInput) (string, error)
}

// MultipartService 定义大文件分片上传接口。
type MultipartService interface {
	CreateMultipartUpload(ctx context.Context, input MultipartUploadInput) (*MultipartUpload, error)
	UploadPart(ctx context.Context, input UploadPartInput) (*UploadedPart, error)
	CompleteMultipartUpload(ctx context.Context, input CompleteMultipartUploadInput) (*Object, error)
	AbortMultipartUpload(ctx context.Context, input AbortMultipartUploadInput) error
	PresignUploadPart(ctx context.Context, input PresignPartInput) (*PresignedURL, error)
}

// STSService 定义临时授权凭证接口。
type STSService interface {
	GenerateToken(ctx context.Context, input STSTokenInput) (*STSToken, error)
}

// Closer 表示可关闭底层 SDK 连接的存储实现。
type Closer interface {
	Close() error
}

// ObjectInput 定位单个对象。
type ObjectInput struct {
	Bucket string
	Key    string
}

// UploadInput 描述上传对象所需参数。
type UploadInput struct {
	Bucket               string
	Key                  string
	FileName             string
	Body                 io.Reader
	Size                 int64
	ContentType          string
	Metadata             map[string]string
	ACL                  ACL
	Overwrite            bool
	ServerSideEncryption string
	StorageClass         string
}

// DownloadInput 描述下载对象所需参数。
type DownloadInput struct {
	Bucket string
	Key    string
}

// DeleteInput 描述删除对象所需参数。
type DeleteInput struct {
	Bucket string
	Key    string
}

// DeleteBatchInput 描述批量删除对象所需参数。
type DeleteBatchInput struct {
	Bucket string
	Keys   []string
}

// ListInput 描述对象列表查询参数。
type ListInput struct {
	Bucket  string
	Prefix  string
	MaxKeys int
	Marker  string
}

// ListResult 表示对象列表结果。
type ListResult struct {
	Objects     []Object
	NextMarker  string
	IsTruncated bool
}

// CopyInput 描述复制或移动对象所需参数。
type CopyInput struct {
	SourceBucket string
	SourceKey    string
	TargetBucket string
	TargetKey    string
	Overwrite    bool
}

// PresignInput 描述预签名 URL 参数。
type PresignInput struct {
	Bucket      string
	Key         string
	Expires     time.Duration
	ContentType string
	Metadata    map[string]string
}

// Object 表示统一对象元数据。
type Object struct {
	FileName     string
	Key          string
	Bucket       string
	Size         int64
	ContentType  string
	ETag         string
	URL          string
	StorageClass string
	LastModified time.Time
	CreatedAt    time.Time
	Metadata     map[string]string
}

// PresignedURL 表示预签名 URL 及调用约束。
type PresignedURL struct {
	URL       string
	Key       string
	Method    string
	ExpiresAt time.Time
	Headers   map[string]string
	FormData  map[string]string
}

// IsExpired 判断预签名 URL 是否已过期。
func (u PresignedURL) IsExpired(now time.Time) bool {
	return !u.ExpiresAt.IsZero() && now.After(u.ExpiresAt)
}

// MultipartUploadInput 描述初始化分片上传所需参数。
type MultipartUploadInput struct {
	Bucket               string
	Key                  string
	ContentType          string
	Metadata             map[string]string
	ACL                  ACL
	ServerSideEncryption string
	StorageClass         string
}

// MultipartUpload 表示分片上传会话。
type MultipartUpload struct {
	Bucket   string
	Key      string
	UploadID string
}

// UploadPartInput 描述上传单个分片所需参数。
type UploadPartInput struct {
	Bucket     string
	Key        string
	UploadID   string
	PartNumber int
	Body       io.Reader
	Size       int64
}

// UploadedPart 表示已上传分片。
type UploadedPart struct {
	PartNumber int
	ETag       string
	Size       int64
}

// CompleteMultipartUploadInput 描述完成分片上传所需参数。
type CompleteMultipartUploadInput struct {
	Bucket   string
	Key      string
	UploadID string
	Parts    []UploadedPart
}

// AbortMultipartUploadInput 描述取消分片上传所需参数。
type AbortMultipartUploadInput struct {
	Bucket   string
	Key      string
	UploadID string
}

// PresignPartInput 描述生成分片上传预签名 URL 所需参数。
type PresignPartInput struct {
	Bucket     string
	Key        string
	UploadID   string
	PartNumber int
	Expires    time.Duration
}

// STSTokenInput 描述临时授权凭证申请参数。
type STSTokenInput struct {
	Bucket          string
	Duration        time.Duration
	Permission      Permission
	Policy          string
	Prefixes        []string
	RoleARN         string
	RoleSessionName string
	ExternalID      string
}

// STSToken 表示统一临时授权凭证。
type STSToken struct {
	Provider        Provider
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	Expiration      time.Time
	Bucket          string
	Region          string
	Endpoint        string
}

// IsExpired 判断临时凭证是否已过期。
func (t STSToken) IsExpired(now time.Time) bool {
	return !t.Expiration.IsZero() && now.After(t.Expiration)
}
