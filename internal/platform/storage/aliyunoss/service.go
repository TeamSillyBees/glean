package aliyunoss

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

// Service 基于阿里云官方 OSS SDK 实现 storage.Service。
type Service struct {
	cfg    storage.Config
	client *oss.Client
}

// New 创建阿里云 OSS 存储服务。
func New(_ context.Context, cfg storage.Config) (*Service, error) {
	cfg = storage.ConfigWithDefaults(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Provider != storage.ProviderAliyunOSS {
		return nil, fmt.Errorf("%w: aliyunoss provider 需要 storage.provider=aliyun_oss", storage.ErrInvalidConfig)
	}
	var options []oss.ClientOption
	if cfg.Aliyun.SecurityToken != "" {
		options = append(options, oss.SecurityToken(cfg.Aliyun.SecurityToken))
	}
	client, err := oss.New(cfg.Aliyun.Endpoint, cfg.Aliyun.AccessKeyID, cfg.Aliyun.AccessKeySecret, options...)
	if err != nil {
		return nil, err
	}
	return &Service{cfg: cfg, client: client}, nil
}

// Close 关闭阿里云 OSS 客户端。
func (s *Service) Close() error {
	return nil
}

// Upload 上传对象。
func (s *Service) Upload(ctx context.Context, input storage.UploadInput) (*storage.Object, error) {
	if input.Body == nil {
		return nil, fmt.Errorf("%w: body 不能为空", storage.ErrInvalidKey)
	}
	bucketName, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	options := uploadOptions(input)
	if !input.Overwrite {
		options = append(options, oss.ForbidOverWrite(true))
	}
	if err := bucket.PutObject(key, input.Body, options...); err != nil {
		return nil, normalizeAliyunError(err)
	}
	object, err := s.Stat(ctx, storage.ObjectInput{Bucket: bucketName, Key: key})
	if err == nil {
		object.CreatedAt = time.Now()
		return object, nil
	}
	return &storage.Object{
		FileName:    storage.FileNameFromKey(key),
		Key:         key,
		Bucket:      bucketName,
		Size:        input.Size,
		ContentType: input.ContentType,
		URL:         s.publicURL(bucketName, key),
		CreatedAt:   time.Now(),
		Metadata:    cloneMetadata(input.Metadata),
	}, nil
}

// Download 下载对象。
func (s *Service) Download(_ context.Context, input storage.DownloadInput) (io.ReadCloser, error) {
	_, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	reader, err := bucket.GetObject(key)
	if err != nil {
		return nil, normalizeAliyunError(err)
	}
	return reader, nil
}

// DownloadBytes 读取完整对象内容。
func (s *Service) DownloadBytes(ctx context.Context, input storage.DownloadInput) ([]byte, error) {
	reader, err := s.Download(ctx, input)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// Delete 删除对象。
func (s *Service) Delete(_ context.Context, input storage.DeleteInput) error {
	_, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return err
	}
	return normalizeAliyunError(bucket.DeleteObject(key))
}

// DeleteBatch 批量删除对象。
func (s *Service) DeleteBatch(_ context.Context, input storage.DeleteBatchInput) ([]string, error) {
	bucketName := storage.DefaultBucket(s.cfg, input.Bucket)
	if strings.TrimSpace(bucketName) == "" {
		return nil, fmt.Errorf("%w: bucket 不能为空", storage.ErrInvalidConfig)
	}
	bucket, err := s.client.Bucket(bucketName)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(input.Keys))
	for _, key := range input.Keys {
		cleanKey, err := storage.NormalizeObjectKey(key)
		if err != nil {
			return nil, err
		}
		keys = append(keys, cleanKey)
	}
	if len(keys) == 0 {
		return []string{}, nil
	}
	result, err := bucket.DeleteObjects(keys)
	if err != nil {
		return nil, normalizeAliyunError(err)
	}
	return result.DeletedObjects, nil
}

// Exists 判断对象是否存在。
func (s *Service) Exists(_ context.Context, input storage.ObjectInput) (bool, error) {
	_, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return false, err
	}
	ok, err := bucket.IsObjectExist(key)
	if err != nil {
		return false, normalizeAliyunError(err)
	}
	return ok, nil
}

// Stat 查询对象元数据。
func (s *Service) Stat(_ context.Context, input storage.ObjectInput) (*storage.Object, error) {
	bucketName, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	header, err := bucket.GetObjectDetailedMeta(key)
	if err != nil {
		return nil, normalizeAliyunError(err)
	}
	return objectFromHeader(bucketName, key, s.publicURL(bucketName, key), header), nil
}

// List 列出对象。
func (s *Service) List(_ context.Context, input storage.ListInput) (*storage.ListResult, error) {
	bucketName := storage.DefaultBucket(s.cfg, input.Bucket)
	if strings.TrimSpace(bucketName) == "" {
		return nil, fmt.Errorf("%w: bucket 不能为空", storage.ErrInvalidConfig)
	}
	bucket, err := s.client.Bucket(bucketName)
	if err != nil {
		return nil, err
	}
	maxKeys := input.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	options := []oss.Option{
		oss.Prefix(input.Prefix),
		oss.MaxKeys(maxKeys),
	}
	if input.Marker != "" {
		options = append(options, oss.Marker(input.Marker))
	}
	result, err := bucket.ListObjects(options...)
	if err != nil {
		return nil, normalizeAliyunError(err)
	}
	objects := make([]storage.Object, 0, len(result.Objects))
	for _, item := range result.Objects {
		objects = append(objects, storage.Object{
			FileName:     storage.FileNameFromKey(item.Key),
			Key:          item.Key,
			Bucket:       bucketName,
			Size:         item.Size,
			ETag:         item.ETag,
			URL:          s.publicURL(bucketName, item.Key),
			StorageClass: item.StorageClass,
			LastModified: item.LastModified,
		})
	}
	return &storage.ListResult{
		Objects:     objects,
		NextMarker:  result.NextMarker,
		IsTruncated: result.IsTruncated,
	}, nil
}

// Copy 复制对象。
func (s *Service) Copy(_ context.Context, input storage.CopyInput) (*storage.Object, error) {
	sourceBucket := storage.DefaultBucket(s.cfg, input.SourceBucket)
	targetBucket := storage.DefaultBucket(s.cfg, input.TargetBucket)
	sourceKey, err := storage.NormalizeObjectKey(input.SourceKey)
	if err != nil {
		return nil, err
	}
	targetKey, err := storage.NormalizeObjectKey(input.TargetKey)
	if err != nil {
		return nil, err
	}
	bucket, err := s.client.Bucket(targetBucket)
	if err != nil {
		return nil, err
	}
	var options []oss.Option
	if !input.Overwrite {
		options = append(options, oss.ForbidOverWrite(true))
	}
	result, err := bucket.CopyObjectFrom(sourceBucket, sourceKey, targetKey, options...)
	if err != nil {
		return nil, normalizeAliyunError(err)
	}
	return &storage.Object{
		FileName:  storage.FileNameFromKey(targetKey),
		Key:       targetKey,
		Bucket:    targetBucket,
		ETag:      result.ETag,
		URL:       s.publicURL(targetBucket, targetKey),
		CreatedAt: time.Now(),
	}, nil
}

// Move 移动对象。
func (s *Service) Move(ctx context.Context, input storage.CopyInput) (*storage.Object, error) {
	copied, err := s.Copy(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := s.Delete(ctx, storage.DeleteInput{Bucket: input.SourceBucket, Key: input.SourceKey}); err != nil {
		return nil, err
	}
	return copied, nil
}

// PresignUpload 生成预签名上传 URL。
func (s *Service) PresignUpload(_ context.Context, input storage.PresignInput) (*storage.PresignedURL, error) {
	return s.presign(input, oss.HTTPPut, "PUT")
}

// PresignDownload 生成预签名下载 URL。
func (s *Service) PresignDownload(_ context.Context, input storage.PresignInput) (*storage.PresignedURL, error) {
	return s.presign(input, oss.HTTPGet, "GET")
}

// PublicURL 返回对象公开访问 URL。
func (s *Service) PublicURL(_ context.Context, input storage.ObjectInput) (string, error) {
	bucket, key, _, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return "", err
	}
	return s.publicURL(bucket, key), nil
}

// CreateMultipartUpload 初始化分片上传。
func (s *Service) CreateMultipartUpload(_ context.Context, input storage.MultipartUploadInput) (*storage.MultipartUpload, error) {
	bucketName, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	result, err := bucket.InitiateMultipartUpload(key, multipartOptions(input)...)
	if err != nil {
		return nil, normalizeAliyunError(err)
	}
	return &storage.MultipartUpload{Bucket: bucketName, Key: key, UploadID: result.UploadID}, nil
}

// UploadPart 上传分片。
func (s *Service) UploadPart(_ context.Context, input storage.UploadPartInput) (*storage.UploadedPart, error) {
	if input.PartNumber <= 0 {
		return nil, fmt.Errorf("%w: part_number 必须大于 0", storage.ErrInvalidConfig)
	}
	if input.Body == nil {
		return nil, fmt.Errorf("%w: part body 不能为空", storage.ErrInvalidKey)
	}
	bucketName, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	result, err := bucket.UploadPart(oss.InitiateMultipartUploadResult{
		Bucket:   bucketName,
		Key:      key,
		UploadID: input.UploadID,
	}, input.Body, input.Size, input.PartNumber)
	if err != nil {
		return nil, normalizeAliyunError(err)
	}
	return &storage.UploadedPart{PartNumber: result.PartNumber, ETag: result.ETag, Size: input.Size}, nil
}

// CompleteMultipartUpload 完成分片上传。
func (s *Service) CompleteMultipartUpload(_ context.Context, input storage.CompleteMultipartUploadInput) (*storage.Object, error) {
	bucketName, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	parts := storage.SortUploadedParts(input.Parts)
	ossParts := make([]oss.UploadPart, 0, len(parts))
	for _, part := range parts {
		ossParts = append(ossParts, oss.UploadPart{PartNumber: part.PartNumber, ETag: part.ETag})
	}
	result, err := bucket.CompleteMultipartUpload(oss.InitiateMultipartUploadResult{
		Bucket:   bucketName,
		Key:      key,
		UploadID: input.UploadID,
	}, ossParts)
	if err != nil {
		return nil, normalizeAliyunError(err)
	}
	return &storage.Object{
		FileName:  storage.FileNameFromKey(key),
		Key:       key,
		Bucket:    bucketName,
		ETag:      result.ETag,
		URL:       result.Location,
		CreatedAt: time.Now(),
	}, nil
}

// AbortMultipartUpload 取消分片上传。
func (s *Service) AbortMultipartUpload(_ context.Context, input storage.AbortMultipartUploadInput) error {
	bucketName, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return err
	}
	return normalizeAliyunError(bucket.AbortMultipartUpload(oss.InitiateMultipartUploadResult{
		Bucket:   bucketName,
		Key:      key,
		UploadID: input.UploadID,
	}))
}

// PresignUploadPart 生成分片上传预签名 URL。
func (s *Service) PresignUploadPart(_ context.Context, input storage.PresignPartInput) (*storage.PresignedURL, error) {
	if input.PartNumber <= 0 {
		return nil, fmt.Errorf("%w: part_number 必须大于 0", storage.ErrInvalidConfig)
	}
	bucketName, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	expires := input.Expires
	if expires == 0 {
		expires = s.cfg.PresignDefaultTTL
	}
	url, err := bucket.SignURL(key, oss.HTTPPut, int64(expires.Seconds()),
		oss.AddParam("partNumber", input.PartNumber),
		oss.AddParam("uploadId", input.UploadID),
	)
	if err != nil {
		return nil, err
	}
	return &storage.PresignedURL{
		URL:       url,
		Key:       key,
		Method:    "PUT",
		ExpiresAt: time.Now().Add(expires),
		Headers:   map[string]string{},
		FormData:  map[string]string{"bucket": bucketName},
	}, nil
}

func (s *Service) bucketAndKey(bucketName string, key string) (string, string, *oss.Bucket, error) {
	actualBucket := storage.DefaultBucket(s.cfg, bucketName)
	if strings.TrimSpace(actualBucket) == "" {
		return "", "", nil, fmt.Errorf("%w: bucket 不能为空", storage.ErrInvalidConfig)
	}
	actualKey, err := storage.NormalizeObjectKey(key)
	if err != nil {
		return "", "", nil, err
	}
	bucket, err := s.client.Bucket(actualBucket)
	if err != nil {
		return "", "", nil, err
	}
	return actualBucket, actualKey, bucket, nil
}

func (s *Service) presign(input storage.PresignInput, method oss.HTTPMethod, methodName string) (*storage.PresignedURL, error) {
	_, key, bucket, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	expires := input.Expires
	if expires == 0 {
		expires = s.cfg.PresignDefaultTTL
	}
	var options []oss.Option
	if input.ContentType != "" {
		options = append(options, oss.ContentType(input.ContentType))
	}
	for key, value := range input.Metadata {
		options = append(options, oss.Meta(key, value))
	}
	url, err := bucket.SignURL(key, method, int64(expires.Seconds()), options...)
	if err != nil {
		return nil, err
	}
	return &storage.PresignedURL{
		URL:       url,
		Key:       key,
		Method:    methodName,
		ExpiresAt: time.Now().Add(expires),
		Headers:   map[string]string{},
	}, nil
}

func (s *Service) publicURL(bucket string, key string) string {
	if s.cfg.PublicBaseURL != "" {
		return storage.JoinURL(s.cfg.PublicBaseURL, key)
	}
	if s.cfg.Aliyun.CustomDomain != "" {
		return storage.JoinURL(withProtocol(s.cfg.Aliyun.CustomDomain, s.cfg.Aliyun.UseHTTPS), key)
	}
	return storage.JoinURL(withProtocol(bucket+"."+endpointHost(s.cfg.Aliyun.Endpoint), s.cfg.Aliyun.UseHTTPS), key)
}

func uploadOptions(input storage.UploadInput) []oss.Option {
	var options []oss.Option
	if input.ContentType != "" {
		options = append(options, oss.ContentType(input.ContentType))
	}
	if input.ACL != storage.ACLDefault {
		options = append(options, oss.ObjectACL(aliyunACL(input.ACL)))
	}
	if input.StorageClass != "" {
		options = append(options, oss.StorageClass(oss.StorageClassType(input.StorageClass)))
	}
	if input.ServerSideEncryption != "" {
		options = append(options, oss.ServerSideEncryption(input.ServerSideEncryption))
	}
	for key, value := range input.Metadata {
		options = append(options, oss.Meta(key, value))
	}
	return options
}

func multipartOptions(input storage.MultipartUploadInput) []oss.Option {
	var options []oss.Option
	if input.ContentType != "" {
		options = append(options, oss.ContentType(input.ContentType))
	}
	if input.ACL != storage.ACLDefault {
		options = append(options, oss.ObjectACL(aliyunACL(input.ACL)))
	}
	if input.StorageClass != "" {
		options = append(options, oss.StorageClass(oss.StorageClassType(input.StorageClass)))
	}
	if input.ServerSideEncryption != "" {
		options = append(options, oss.ServerSideEncryption(input.ServerSideEncryption))
	}
	for key, value := range input.Metadata {
		options = append(options, oss.Meta(key, value))
	}
	return options
}

func aliyunACL(acl storage.ACL) oss.ACLType {
	switch acl {
	case storage.ACLPrivate:
		return oss.ACLPrivate
	case storage.ACLPublicRead:
		return oss.ACLPublicRead
	case storage.ACLPublicReadWrite:
		return oss.ACLPublicReadWrite
	default:
		return oss.ACLDefault
	}
}

func objectFromHeader(bucket string, key string, url string, header http.Header) *storage.Object {
	size, _ := strconv.ParseInt(header.Get("Content-Length"), 10, 64)
	lastModified, _ := http.ParseTime(header.Get("Last-Modified"))
	return &storage.Object{
		FileName:     storage.FileNameFromKey(key),
		Key:          key,
		Bucket:       bucket,
		Size:         size,
		ContentType:  header.Get("Content-Type"),
		ETag:         strings.Trim(header.Get("ETag"), "\""),
		URL:          url,
		LastModified: lastModified,
		Metadata:     aliyunMetadata(header),
	}
}

func aliyunMetadata(header http.Header) map[string]string {
	metadata := map[string]string{}
	for key, values := range header {
		normalized := strings.ToLower(key)
		if !strings.HasPrefix(normalized, "x-oss-meta-") || len(values) == 0 {
			continue
		}
		metadata[strings.TrimPrefix(normalized, "x-oss-meta-")] = values[0]
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func normalizeAliyunError(err error) error {
	if err == nil {
		return nil
	}
	var serviceErr oss.ServiceError
	if ok := errorAs(err, &serviceErr); ok {
		switch serviceErr.Code {
		case "NoSuchKey", "NoSuchBucket":
			return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
		case "FileAlreadyExists", "ObjectAlreadyExists":
			return fmt.Errorf("%w: %w", storage.ErrObjectExists, err)
		}
	}
	if strings.Contains(err.Error(), "StatusCode=404") || strings.Contains(err.Error(), "NoSuchKey") {
		return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	}
	return err
}

func errorAs(err error, target any) bool {
	switch typed := target.(type) {
	case *oss.ServiceError:
		if value, ok := errors.AsType[oss.ServiceError](err); ok {
			*typed = value
			return true
		}
	}
	return false
}

func withProtocol(host string, useHTTPS bool) string {
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	if useHTTPS {
		return "https://" + host
	}
	return "http://" + host
}

func endpointHost(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	return strings.TrimRight(endpoint, "/")
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	copied := make(map[string]string, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	return copied
}
