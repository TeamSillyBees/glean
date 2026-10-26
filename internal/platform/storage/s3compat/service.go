package s3compat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

const defaultMaxKeys int32 = 1000

// Service 基于 AWS SDK for Go v2 实现 S3 与 S3 兼容对象存储。
type Service struct {
	cfg       storage.Config
	client    *s3.Client
	presigner *s3.PresignClient
}

// New 创建 S3 或 S3 兼容存储服务。
func New(ctx context.Context, cfg storage.Config) (*Service, error) {
	cfg = storage.ConfigWithDefaults(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Provider != storage.ProviderAWSS3 && cfg.Provider != storage.ProviderS3Compatible {
		return nil, fmt.Errorf("%w: s3compat provider 不匹配", storage.ErrInvalidConfig)
	}
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		if cfg.S3.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.S3.Endpoint)
		}
		options.UsePathStyle = cfg.S3.UsePathStyle
	})
	return &Service{
		cfg:       cfg,
		client:    client,
		presigner: s3.NewPresignClient(client),
	}, nil
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
	request := &s3.PutObjectInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		Body:     input.Body,
		Metadata: input.Metadata,
	}
	if input.Size > 0 {
		request.ContentLength = aws.Int64(input.Size)
	}
	if input.ContentType != "" {
		request.ContentType = aws.String(input.ContentType)
	}
	if input.ACL != storage.ACLDefault {
		request.ACL = s3ACL(input.ACL)
	}
	if input.StorageClass != "" {
		request.StorageClass = s3types.StorageClass(input.StorageClass)
	}
	if input.ServerSideEncryption != "" {
		request.ServerSideEncryption = s3types.ServerSideEncryption(input.ServerSideEncryption)
	}
	if !input.Overwrite {
		request.IfNoneMatch = aws.String("*")
	}
	response, err := s.client.PutObject(ctx, request)
	if err != nil {
		return nil, normalizeS3Error(err)
	}
	return &storage.Object{
		FileName:    storage.FileNameFromKey(key),
		Key:         key,
		Bucket:      bucket,
		Size:        input.Size,
		ContentType: input.ContentType,
		ETag:        aws.ToString(response.ETag),
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
	response, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, normalizeS3Error(err)
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
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return normalizeS3Error(err)
}

// DeleteBatch 批量删除对象。
func (s *Service) DeleteBatch(ctx context.Context, input storage.DeleteBatchInput) ([]string, error) {
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("%w: bucket 不能为空", storage.ErrInvalidConfig)
	}
	if len(input.Keys) == 0 {
		return []string{}, nil
	}
	objects := make([]s3types.ObjectIdentifier, 0, len(input.Keys))
	for _, key := range input.Keys {
		cleanKey, err := storage.NormalizeObjectKey(key)
		if err != nil {
			return nil, err
		}
		objects = append(objects, s3types.ObjectIdentifier{Key: aws.String(cleanKey)})
	}
	response, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(false)},
	})
	if err != nil {
		return nil, normalizeS3Error(err)
	}
	deleted := make([]string, 0, len(response.Deleted))
	for _, item := range response.Deleted {
		deleted = append(deleted, aws.ToString(item.Key))
	}
	return deleted, nil
}

// Exists 判断对象是否存在。
func (s *Service) Exists(ctx context.Context, input storage.ObjectInput) (bool, error) {
	_, err := s.Stat(ctx, input)
	if err == nil {
		return true, nil
	}
	if storage.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

// Stat 查询对象元数据。
func (s *Service) Stat(ctx context.Context, input storage.ObjectInput) (*storage.Object, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	response, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, normalizeS3Error(err)
	}
	return &storage.Object{
		FileName:     storage.FileNameFromKey(key),
		Key:          key,
		Bucket:       bucket,
		Size:         aws.ToInt64(response.ContentLength),
		ContentType:  aws.ToString(response.ContentType),
		ETag:         aws.ToString(response.ETag),
		URL:          s.publicURL(bucket, key),
		StorageClass: string(response.StorageClass),
		LastModified: aws.ToTime(response.LastModified),
		Metadata:     cloneMetadata(response.Metadata),
	}, nil
}

// List 列出对象。
func (s *Service) List(ctx context.Context, input storage.ListInput) (*storage.ListResult, error) {
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("%w: bucket 不能为空", storage.ErrInvalidConfig)
	}
	maxKeys := int32(input.MaxKeys)
	if maxKeys <= 0 {
		maxKeys = defaultMaxKeys
	}
	request := &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		Prefix:  aws.String(input.Prefix),
		MaxKeys: aws.Int32(maxKeys),
	}
	if input.Marker != "" {
		request.ContinuationToken = aws.String(input.Marker)
	}
	response, err := s.client.ListObjectsV2(ctx, request)
	if err != nil {
		return nil, normalizeS3Error(err)
	}
	objects := make([]storage.Object, 0, len(response.Contents))
	for _, item := range response.Contents {
		key := aws.ToString(item.Key)
		objects = append(objects, storage.Object{
			FileName:     storage.FileNameFromKey(key),
			Key:          key,
			Bucket:       bucket,
			Size:         aws.ToInt64(item.Size),
			ETag:         aws.ToString(item.ETag),
			URL:          s.publicURL(bucket, key),
			StorageClass: string(item.StorageClass),
			LastModified: aws.ToTime(item.LastModified),
		})
	}
	return &storage.ListResult{
		Objects:     objects,
		NextMarker:  aws.ToString(response.NextContinuationToken),
		IsTruncated: aws.ToBool(response.IsTruncated),
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
	request := &s3.CopyObjectInput{
		Bucket:     aws.String(targetBucket),
		Key:        aws.String(targetKey),
		CopySource: aws.String(sourceBucket + "/" + escapeS3CopyKey(sourceKey)),
	}
	if !input.Overwrite {
		request.IfNoneMatch = aws.String("*")
	}
	response, err := s.client.CopyObject(ctx, request)
	if err != nil {
		return nil, normalizeS3Error(err)
	}
	etag := ""
	if response.CopyObjectResult != nil {
		etag = aws.ToString(response.CopyObjectResult.ETag)
	}
	return &storage.Object{
		FileName:  storage.FileNameFromKey(targetKey),
		Key:       targetKey,
		Bucket:    targetBucket,
		ETag:      etag,
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
func (s *Service) PresignUpload(ctx context.Context, input storage.PresignInput) (*storage.PresignedURL, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	request := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: aws.String(input.ContentType),
		Metadata:    input.Metadata,
	}
	expires := s.expires(input.Expires)
	response, err := s.presigner.PresignPutObject(ctx, request, func(options *s3.PresignOptions) {
		options.Expires = expires
	})
	if err != nil {
		return nil, err
	}
	return presigned(response.URL, key, "PUT", expires, response.SignedHeader), nil
}

// PresignDownload 生成预签名下载 URL。
func (s *Service) PresignDownload(ctx context.Context, input storage.PresignInput) (*storage.PresignedURL, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	expires := s.expires(input.Expires)
	response, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, func(options *s3.PresignOptions) {
		options.Expires = expires
	})
	if err != nil {
		return nil, err
	}
	return presigned(response.URL, key, "GET", expires, response.SignedHeader), nil
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
	request := &s3.CreateMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		Metadata: input.Metadata,
	}
	if input.ContentType != "" {
		request.ContentType = aws.String(input.ContentType)
	}
	if input.ACL != storage.ACLDefault {
		request.ACL = s3ACL(input.ACL)
	}
	if input.StorageClass != "" {
		request.StorageClass = s3types.StorageClass(input.StorageClass)
	}
	if input.ServerSideEncryption != "" {
		request.ServerSideEncryption = s3types.ServerSideEncryption(input.ServerSideEncryption)
	}
	response, err := s.client.CreateMultipartUpload(ctx, request)
	if err != nil {
		return nil, normalizeS3Error(err)
	}
	return &storage.MultipartUpload{Bucket: bucket, Key: key, UploadID: aws.ToString(response.UploadId)}, nil
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
	request := &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(input.UploadID),
		PartNumber: aws.Int32(int32(input.PartNumber)),
		Body:       input.Body,
	}
	if input.Size > 0 {
		request.ContentLength = aws.Int64(input.Size)
	}
	response, err := s.client.UploadPart(ctx, request)
	if err != nil {
		return nil, normalizeS3Error(err)
	}
	return &storage.UploadedPart{PartNumber: input.PartNumber, ETag: aws.ToString(response.ETag), Size: input.Size}, nil
}

// CompleteMultipartUpload 完成分片上传。
func (s *Service) CompleteMultipartUpload(ctx context.Context, input storage.CompleteMultipartUploadInput) (*storage.Object, error) {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return nil, err
	}
	parts := storage.SortUploadedParts(input.Parts)
	completedParts := make([]s3types.CompletedPart, 0, len(parts))
	for _, part := range parts {
		completedParts = append(completedParts, s3types.CompletedPart{
			ETag:       aws.String(part.ETag),
			PartNumber: aws.Int32(int32(part.PartNumber)),
		})
	}
	response, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(input.UploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return nil, normalizeS3Error(err)
	}
	return &storage.Object{
		FileName:  storage.FileNameFromKey(key),
		Key:       key,
		Bucket:    bucket,
		ETag:      aws.ToString(response.ETag),
		URL:       s.publicURL(bucket, key),
		CreatedAt: time.Now(),
	}, nil
}

// AbortMultipartUpload 取消分片上传。
func (s *Service) AbortMultipartUpload(ctx context.Context, input storage.AbortMultipartUploadInput) error {
	bucket, key, err := s.bucketAndKey(input.Bucket, input.Key)
	if err != nil {
		return err
	}
	_, err = s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(input.UploadID),
	})
	return normalizeS3Error(err)
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
	expires := s.expires(input.Expires)
	response, err := s.presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(input.UploadID),
		PartNumber: aws.Int32(int32(input.PartNumber)),
	}, func(options *s3.PresignOptions) {
		options.Expires = expires
	})
	if err != nil {
		return nil, err
	}
	return presigned(response.URL, key, "PUT", expires, response.SignedHeader), nil
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

func (s *Service) expires(expires time.Duration) time.Duration {
	if expires > 0 {
		return expires
	}
	return s.cfg.PresignDefaultTTL
}

func (s *Service) publicURL(bucket string, key string) string {
	if s.cfg.PublicBaseURL != "" {
		return storage.JoinURL(s.cfg.PublicBaseURL, key)
	}
	if s.cfg.S3.CustomDomain != "" {
		return storage.JoinURL(withProtocol(s.cfg.S3.CustomDomain, s.cfg.S3.UseHTTPS), key)
	}
	if s.cfg.S3.Endpoint != "" {
		endpoint := strings.TrimRight(s.cfg.S3.Endpoint, "/")
		if s.cfg.S3.UsePathStyle {
			return storage.JoinURL(endpoint, bucket, key)
		}
		parsed, err := url.Parse(endpoint)
		if err == nil && parsed.Host != "" {
			parsed.Host = bucket + "." + parsed.Host
			parsed.Path = joinURLPath(parsed.Path, key)
			return parsed.String()
		}
		return storage.JoinURL(endpoint, bucket, key)
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, s.cfg.S3.Region, key)
}

func loadAWSConfig(ctx context.Context, cfg storage.Config) (aws.Config, error) {
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.S3.Region),
	}
	if cfg.S3.AccessKeyID != "" || cfg.S3.SecretAccessKey != "" || cfg.S3.SessionToken != "" {
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.S3.AccessKeyID,
			cfg.S3.SecretAccessKey,
			cfg.S3.SessionToken,
		)))
	}
	return awsconfig.LoadDefaultConfig(ctx, options...)
}

func s3ACL(acl storage.ACL) s3types.ObjectCannedACL {
	switch acl {
	case storage.ACLPrivate:
		return s3types.ObjectCannedACLPrivate
	case storage.ACLPublicRead:
		return s3types.ObjectCannedACLPublicRead
	case storage.ACLPublicReadWrite:
		return s3types.ObjectCannedACLPublicReadWrite
	default:
		return ""
	}
}

func normalizeS3Error(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*s3types.NoSuchKey](err); ok {
		return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	}
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "NoSuchBucket":
			return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
		case "PreconditionFailed", "ConditionalRequestConflict":
			return fmt.Errorf("%w: %w", storage.ErrObjectExists, err)
		}
	}
	return err
}

func presigned(url string, key string, method string, expires time.Duration, headers map[string][]string) *storage.PresignedURL {
	result := &storage.PresignedURL{
		URL:       url,
		Key:       key,
		Method:    method,
		ExpiresAt: time.Now().Add(expires),
		Headers:   map[string]string{},
	}
	for key, values := range headers {
		if len(values) > 0 {
			result.Headers[key] = values[0]
		}
	}
	return result
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

func joinURLPath(basePath string, key string) string {
	parts := []string{}
	if trimmed := strings.Trim(basePath, "/"); trimmed != "" {
		parts = append(parts, trimmed)
	}
	parts = append(parts, strings.Trim(key, "/"))
	return "/" + strings.Join(parts, "/")
}

func escapeS3CopyKey(key string) string {
	return strings.ReplaceAll(url.PathEscape(key), "%2F", "/")
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
