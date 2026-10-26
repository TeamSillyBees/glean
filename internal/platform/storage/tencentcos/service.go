package tencentcos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/platform/storage"
	"github.com/tencentyun/cos-go-sdk-v5"
)

// Service 基于腾讯云官方 COS SDK 实现 storage.Service。
type Service struct {
	cfg    storage.Config
	client *cos.Client
}

// New 创建腾讯云 COS 存储服务。
func New(_ context.Context, cfg storage.Config) (*Service, error) {
	cfg = storage.ConfigWithDefaults(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Provider != storage.ProviderTencentCOS {
		return nil, fmt.Errorf("%w: tencentcos provider 需要 storage.provider=tencent_cos", storage.ErrInvalidConfig)
	}
	bucketURL, err := cosBucketURL(cfg)
	if err != nil {
		return nil, err
	}
	baseURL := &cos.BaseURL{BucketURL: bucketURL}
	client := cos.NewClient(baseURL, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:     cfg.Tencent.SecretID,
			SecretKey:    cfg.Tencent.SecretKey,
			SessionToken: cfg.Tencent.SessionToken,
		},
	})
	return &Service{cfg: cfg, client: client}, nil
}

// Upload 上传对象。
func (s *Service) Upload(ctx context.Context, input storage.UploadInput) (*storage.Object, error) {
	if input.Body == nil {
		return nil, fmt.Errorf("%w: body 不能为空", storage.ErrInvalidKey)
	}
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return nil, err
	}
	options := putOptions(input)
	if !input.Overwrite {
		header := ensureHeader(options)
		header.Set("If-None-Match", "*")
	}
	response, err := client.Object.Put(ctx, key, input.Body, options)
	if err != nil {
		return nil, normalizeCOSError(err)
	}
	return &storage.Object{
		FileName:    storage.FileNameFromKey(key),
		Key:         key,
		Bucket:      bucket,
		Size:        input.Size,
		ContentType: input.ContentType,
		ETag:        response.Header.Get("ETag"),
		URL:         s.publicURL(bucket, key),
		CreatedAt:   time.Now(),
		Metadata:    cloneMetadata(input.Metadata),
	}, nil
}

// Download 下载对象。
func (s *Service) Download(ctx context.Context, input storage.DownloadInput) (io.ReadCloser, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return nil, err
	}
	response, err := client.Object.Get(ctx, key, nil)
	if err != nil {
		return nil, normalizeCOSError(err)
	}
	return response.Body, nil
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
func (s *Service) Delete(ctx context.Context, input storage.DeleteInput) error {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return err
	}
	_, err = client.Object.Delete(ctx, key)
	return normalizeCOSError(err)
}

// DeleteBatch 批量删除对象。
func (s *Service) DeleteBatch(ctx context.Context, input storage.DeleteBatchInput) ([]string, error) {
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return nil, err
	}
	if len(input.Keys) == 0 {
		return []string{}, nil
	}
	objects := make([]cos.Object, 0, len(input.Keys))
	for _, key := range input.Keys {
		cleanKey, err := storage.NormalizeObjectKey(key)
		if err != nil {
			return nil, err
		}
		objects = append(objects, cos.Object{Key: cleanKey})
	}
	result, _, err := client.Object.DeleteMulti(ctx, &cos.ObjectDeleteMultiOptions{
		Quiet:   false,
		Objects: objects,
	})
	if err != nil {
		return nil, normalizeCOSError(err)
	}
	deleted := make([]string, 0, len(result.DeletedObjects))
	for _, item := range result.DeletedObjects {
		deleted = append(deleted, item.Key)
	}
	return deleted, nil
}

// Exists 判断对象是否存在。
func (s *Service) Exists(ctx context.Context, input storage.ObjectInput) (bool, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return false, err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return false, err
	}
	return client.Object.IsExist(ctx, key)
}

// Stat 查询对象元数据。
func (s *Service) Stat(ctx context.Context, input storage.ObjectInput) (*storage.Object, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return nil, err
	}
	response, err := client.Object.Head(ctx, key, nil)
	if err != nil {
		return nil, normalizeCOSError(err)
	}
	return objectFromHeader(bucket, key, s.publicURL(bucket, key), response.Header), nil
}

// List 列出对象。
func (s *Service) List(ctx context.Context, input storage.ListInput) (*storage.ListResult, error) {
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("%w: bucket 不能为空", storage.ErrInvalidConfig)
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return nil, err
	}
	maxKeys := input.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	result, _, err := client.Bucket.Get(ctx, &cos.BucketGetOptions{
		Prefix:  input.Prefix,
		Marker:  input.Marker,
		MaxKeys: maxKeys,
	})
	if err != nil {
		return nil, normalizeCOSError(err)
	}
	objects := make([]storage.Object, 0, len(result.Contents))
	for _, item := range result.Contents {
		lastModified, _ := time.Parse(time.RFC3339, item.LastModified)
		objects = append(objects, storage.Object{
			FileName:     storage.FileNameFromKey(item.Key),
			Key:          item.Key,
			Bucket:       bucket,
			Size:         item.Size,
			ETag:         item.ETag,
			URL:          s.publicURL(bucket, item.Key),
			StorageClass: item.StorageClass,
			LastModified: lastModified,
		})
	}
	return &storage.ListResult{
		Objects:     objects,
		NextMarker:  result.NextMarker,
		IsTruncated: result.IsTruncated,
	}, nil
}

// Copy 复制对象。
func (s *Service) Copy(ctx context.Context, input storage.CopyInput) (*storage.Object, error) {
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
	client, err := s.clientForBucket(targetBucket)
	if err != nil {
		return nil, err
	}
	sourceURL := s.publicURL(sourceBucket, sourceKey)
	options := &cos.ObjectCopyOptions{}
	if !input.Overwrite {
		options.ObjectCopyHeaderOptions = &cos.ObjectCopyHeaderOptions{
			XCosCopySourceIfNoneMatch: "*",
		}
	}
	result, _, err := client.Object.Copy(ctx, targetKey, sourceURL, options)
	if err != nil {
		return nil, normalizeCOSError(err)
	}
	lastModified, _ := time.Parse(time.RFC3339, result.LastModified)
	return &storage.Object{
		FileName:     storage.FileNameFromKey(targetKey),
		Key:          targetKey,
		Bucket:       targetBucket,
		ETag:         result.ETag,
		URL:          s.publicURL(targetBucket, targetKey),
		CreatedAt:    time.Now(),
		LastModified: lastModified,
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
func (s *Service) PresignUpload(ctx context.Context, input storage.PresignInput) (*storage.PresignedURL, error) {
	return s.presign(ctx, input, http.MethodPut)
}

// PresignDownload 生成预签名下载 URL。
func (s *Service) PresignDownload(ctx context.Context, input storage.PresignInput) (*storage.PresignedURL, error) {
	return s.presign(ctx, input, http.MethodGet)
}

// PublicURL 返回对象公开访问 URL。
func (s *Service) PublicURL(_ context.Context, input storage.ObjectInput) (string, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return "", err
	}
	return s.publicURL(bucket, key), nil
}

// CreateMultipartUpload 初始化分片上传。
func (s *Service) CreateMultipartUpload(ctx context.Context, input storage.MultipartUploadInput) (*storage.MultipartUpload, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return nil, err
	}
	result, _, err := client.Object.InitiateMultipartUpload(ctx, key, multipartOptions(input))
	if err != nil {
		return nil, normalizeCOSError(err)
	}
	return &storage.MultipartUpload{Bucket: bucket, Key: key, UploadID: result.UploadID}, nil
}

// UploadPart 上传分片。
func (s *Service) UploadPart(ctx context.Context, input storage.UploadPartInput) (*storage.UploadedPart, error) {
	if input.PartNumber <= 0 {
		return nil, fmt.Errorf("%w: part_number 必须大于 0", storage.ErrInvalidConfig)
	}
	if input.Body == nil {
		return nil, fmt.Errorf("%w: part body 不能为空", storage.ErrInvalidKey)
	}
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return nil, err
	}
	response, err := client.Object.UploadPart(ctx, key, input.UploadID, input.PartNumber, input.Body, nil)
	if err != nil {
		return nil, normalizeCOSError(err)
	}
	return &storage.UploadedPart{PartNumber: input.PartNumber, ETag: response.Header.Get("ETag"), Size: input.Size}, nil
}

// CompleteMultipartUpload 完成分片上传。
func (s *Service) CompleteMultipartUpload(ctx context.Context, input storage.CompleteMultipartUploadInput) (*storage.Object, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return nil, err
	}
	parts := storage.SortUploadedParts(input.Parts)
	cosParts := make([]cos.Object, 0, len(parts))
	for _, part := range parts {
		cosParts = append(cosParts, cos.Object{PartNumber: part.PartNumber, ETag: part.ETag})
	}
	result, _, err := client.Object.CompleteMultipartUpload(ctx, key, input.UploadID, &cos.CompleteMultipartUploadOptions{
		Parts: cosParts,
	})
	if err != nil {
		return nil, normalizeCOSError(err)
	}
	return &storage.Object{
		FileName:  storage.FileNameFromKey(key),
		Key:       key,
		Bucket:    bucket,
		ETag:      result.ETag,
		URL:       result.Location,
		CreatedAt: time.Now(),
	}, nil
}

// AbortMultipartUpload 取消分片上传。
func (s *Service) AbortMultipartUpload(ctx context.Context, input storage.AbortMultipartUploadInput) error {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return err
	}
	_, err = client.Object.AbortMultipartUpload(ctx, key, input.UploadID)
	return normalizeCOSError(err)
}

// PresignUploadPart 生成分片上传预签名 URL。
func (s *Service) PresignUploadPart(ctx context.Context, input storage.PresignPartInput) (*storage.PresignedURL, error) {
	if input.PartNumber <= 0 {
		return nil, fmt.Errorf("%w: part_number 必须大于 0", storage.ErrInvalidConfig)
	}
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return nil, err
	}
	expires := input.Expires
	if expires == 0 {
		expires = s.cfg.PresignDefaultTTL
	}
	opt := &cos.PresignedURLOptions{
		Query: &url.Values{
			"partNumber": []string{strconv.Itoa(input.PartNumber)},
			"uploadId":   []string{input.UploadID},
		},
	}
	u, err := client.Object.GetPresignedURL2(ctx, http.MethodPut, key, expires, opt)
	if err != nil {
		return nil, err
	}
	return &storage.PresignedURL{URL: u.String(), Key: key, Method: "PUT", ExpiresAt: time.Now().Add(expires)}, nil
}

func (s *Service) bucketAndKey(bucket string, key string) (string, string, error) {
	actualBucket := storage.DefaultBucket(s.cfg, bucket)
	if strings.TrimSpace(actualBucket) == "" {
		return "", "", fmt.Errorf("%w: bucket 不能为空", storage.ErrInvalidConfig)
	}
	actualKey, err := storage.NormalizeObjectKey(key)
	if err != nil {
		return "", "", err
	}
	return actualBucket, actualKey, nil
}

func (s *Service) clientForBucket(bucket string) (*cos.Client, error) {
	if bucket == "" || bucket == s.cfg.Bucket {
		return s.client, nil
	}
	cfg := s.cfg
	cfg.Bucket = bucket
	bucketURL, err := cosBucketURL(cfg)
	if err != nil {
		return nil, err
	}
	return cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:     cfg.Tencent.SecretID,
			SecretKey:    cfg.Tencent.SecretKey,
			SessionToken: cfg.Tencent.SessionToken,
		},
	}), nil
}

func (s *Service) presign(ctx context.Context, input storage.PresignInput, method string) (*storage.PresignedURL, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	client, err := s.clientForBucket(bucket)
	if err != nil {
		return nil, err
	}
	expires := input.Expires
	if expires == 0 {
		expires = s.cfg.PresignDefaultTTL
	}
	u, err := client.Object.GetPresignedURL2(ctx, method, key, expires, nil)
	if err != nil {
		return nil, err
	}
	return &storage.PresignedURL{
		URL:       u.String(),
		Key:       key,
		Method:    method,
		ExpiresAt: time.Now().Add(expires),
		Headers:   map[string]string{},
	}, nil
}

func (s *Service) publicURL(bucket string, key string) string {
	if s.cfg.PublicBaseURL != "" {
		return storage.JoinURL(s.cfg.PublicBaseURL, key)
	}
	if s.cfg.Tencent.CustomDomain != "" {
		return storage.JoinURL(withProtocol(s.cfg.Tencent.CustomDomain, s.cfg.Tencent.UseHTTPS), key)
	}
	if s.cfg.Tencent.Endpoint != "" {
		return storage.JoinURL(s.cfg.Tencent.Endpoint, key)
	}
	return storage.JoinURL(withProtocol(bucket+".cos."+s.cfg.Tencent.Region+".myqcloud.com", s.cfg.Tencent.UseHTTPS), key)
}

func cosBucketURL(cfg storage.Config) (*url.URL, error) {
	if cfg.Tencent.Endpoint != "" {
		return url.Parse(cfg.Tencent.Endpoint)
	}
	return url.Parse(withProtocol(cfg.Bucket+".cos."+cfg.Tencent.Region+".myqcloud.com", cfg.Tencent.UseHTTPS))
}

func putOptions(input storage.UploadInput) *cos.ObjectPutOptions {
	options := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{},
	}
	if input.ContentType != "" {
		options.ContentType = input.ContentType
	}
	if input.Size > 0 {
		options.ContentLength = input.Size
	}
	if input.StorageClass != "" {
		options.XCosStorageClass = input.StorageClass
	}
	if input.ServerSideEncryption != "" {
		options.XCosServerSideEncryption = input.ServerSideEncryption
	}
	if len(input.Metadata) > 0 {
		header := http.Header{}
		for key, value := range input.Metadata {
			header.Set(key, value)
		}
		options.XCosMetaXXX = &header
	}
	if input.ACL != storage.ACLDefault {
		options.ACLHeaderOptions = &cos.ACLHeaderOptions{XCosACL: string(input.ACL)}
	}
	return options
}

func multipartOptions(input storage.MultipartUploadInput) *cos.InitiateMultipartUploadOptions {
	options := &cos.InitiateMultipartUploadOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{},
	}
	if input.ContentType != "" {
		options.ContentType = input.ContentType
	}
	if input.StorageClass != "" {
		options.XCosStorageClass = input.StorageClass
	}
	if input.ServerSideEncryption != "" {
		options.XCosServerSideEncryption = input.ServerSideEncryption
	}
	if len(input.Metadata) > 0 {
		header := http.Header{}
		for key, value := range input.Metadata {
			header.Set(key, value)
		}
		options.XCosMetaXXX = &header
	}
	if input.ACL != storage.ACLDefault {
		options.ACLHeaderOptions = &cos.ACLHeaderOptions{XCosACL: string(input.ACL)}
	}
	return options
}

func ensureHeader(options *cos.ObjectPutOptions) *http.Header {
	if options.ObjectPutHeaderOptions == nil {
		options.ObjectPutHeaderOptions = &cos.ObjectPutHeaderOptions{}
	}
	if options.XOptionHeader == nil {
		header := http.Header{}
		options.XOptionHeader = &header
	}
	return options.XOptionHeader
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
		Metadata:     cosMetadata(header),
	}
}

func cosMetadata(header http.Header) map[string]string {
	metadata := map[string]string{}
	for key, values := range header {
		normalized := strings.ToLower(key)
		if !strings.HasPrefix(normalized, "x-cos-meta-") || len(values) == 0 {
			continue
		}
		metadata[strings.TrimPrefix(normalized, "x-cos-meta-")] = values[0]
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func normalizeCOSError(err error) error {
	if err == nil {
		return nil
	}
	if cosErr, ok := cos.IsCOSError(err); ok {
		switch cosErr.Code {
		case "NoSuchKey", "NoSuchBucket", "NoSuchResource":
			return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
		case "PreconditionFailed":
			return fmt.Errorf("%w: %w", storage.ErrObjectExists, err)
		}
		if cosErr.Response != nil && cosErr.Response.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
		}
	}
	return err
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
