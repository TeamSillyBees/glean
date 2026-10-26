package local

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/platform/storage"
)

const defaultListMaxKeys = 1000

// Service 使用本地文件系统实现 storage.Service。
type Service struct {
	cfg storage.Config
}

// New 创建本地文件系统存储服务。
func New(cfg storage.Config) (*Service, error) {
	cfg = storage.ConfigWithDefaults(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Provider != storage.ProviderLocal {
		return nil, fmt.Errorf("%w: local provider 需要 storage.provider=local", storage.ErrInvalidConfig)
	}
	root, err := filepath.Abs(cfg.Local.RootDir)
	if err != nil {
		return nil, fmt.Errorf("%w: 解析本地存储目录失败: %w", storage.ErrInvalidConfig, err)
	}
	cfg.Local.RootDir = root
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("创建本地存储目录失败: %w", err)
	}
	service := &Service{cfg: cfg}
	tempRoot, err := service.multipartRoot()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		return nil, fmt.Errorf("创建本地分片目录失败: %w", err)
	}
	return service, nil
}

// Upload 保存对象。
func (s *Service) Upload(_ context.Context, input storage.UploadInput) (*storage.Object, error) {
	if input.Body == nil {
		return nil, fmt.Errorf("%w: body 不能为空", storage.ErrInvalidKey)
	}
	if input.Size > 0 && s.cfg.Local.MaxSize > 0 && input.Size > s.cfg.Local.MaxSize {
		return nil, fmt.Errorf("%w: 文件大小超过限制", storage.ErrInvalidConfig)
	}
	key, err := s.objectKeyForUpload(input)
	if err != nil {
		return nil, err
	}
	if err := s.validateExtension(key); err != nil {
		return nil, err
	}
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	fullPath, err := s.fullPath(bucket, key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return nil, err
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !input.Overwrite {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	file, err := os.OpenFile(fullPath, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w: %s", storage.ErrObjectExists, key)
		}
		return nil, err
	}
	defer file.Close()

	hash := md5.New() //nolint:gosec // ETag 只用于对象标识，不用于安全校验。
	written, err := copyWithLimit(io.MultiWriter(file, hash), input.Body, s.cfg.Local.MaxSize)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(fullPath)
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(fullPath)
		return nil, err
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	contentType := input.ContentType
	if contentType == "" {
		name := input.FileName
		if name == "" {
			name = key
		}
		contentType = storage.ContentTypeFromName(name)
	}
	return &storage.Object{
		FileName:     storage.FileNameFromKey(key),
		Key:          key,
		Bucket:       bucket,
		Size:         written,
		ContentType:  contentType,
		ETag:         hex.EncodeToString(hash.Sum(nil)),
		URL:          s.publicURL(bucket, key, false),
		LastModified: info.ModTime(),
		CreatedAt:    time.Now(),
		Metadata:     cloneMetadata(input.Metadata),
	}, nil
}

// Download 打开对象读取流。
func (s *Service) Download(_ context.Context, input storage.DownloadInput) (io.ReadCloser, error) {
	key, err := storage.NormalizeObjectKey(input.Key)
	if err != nil {
		return nil, err
	}
	fullPath, err := s.fullPath(storage.DefaultBucket(s.cfg, input.Bucket), key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	return file, err
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
	key, err := storage.NormalizeObjectKey(input.Key)
	if err != nil {
		return err
	}
	fullPath, err := s.fullPath(storage.DefaultBucket(s.cfg, input.Bucket), key)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return err
	}
	return nil
}

// DeleteBatch 批量删除对象，返回成功删除的 key。
func (s *Service) DeleteBatch(ctx context.Context, input storage.DeleteBatchInput) ([]string, error) {
	deleted := make([]string, 0, len(input.Keys))
	for _, key := range input.Keys {
		err := s.Delete(ctx, storage.DeleteInput{Bucket: input.Bucket, Key: key})
		if err != nil {
			if storage.IsNotFound(err) {
				continue
			}
			return deleted, err
		}
		deleted = append(deleted, key)
	}
	return deleted, nil
}

// Exists 判断对象是否存在。
func (s *Service) Exists(_ context.Context, input storage.ObjectInput) (bool, error) {
	key, err := storage.NormalizeObjectKey(input.Key)
	if err != nil {
		return false, err
	}
	fullPath, err := s.fullPath(storage.DefaultBucket(s.cfg, input.Bucket), key)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}

// Stat 返回对象元数据。
func (s *Service) Stat(_ context.Context, input storage.ObjectInput) (*storage.Object, error) {
	key, err := storage.NormalizeObjectKey(input.Key)
	if err != nil {
		return nil, err
	}
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	fullPath, err := s.fullPath(bucket, key)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	return &storage.Object{
		FileName:     storage.FileNameFromKey(key),
		Key:          key,
		Bucket:       bucket,
		Size:         info.Size(),
		ContentType:  storage.ContentTypeFromName(key),
		URL:          s.publicURL(bucket, key, false),
		LastModified: info.ModTime(),
	}, nil
}

// List 按前缀列出对象。
func (s *Service) List(_ context.Context, input storage.ListInput) (*storage.ListResult, error) {
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	prefix := strings.Trim(strings.ReplaceAll(input.Prefix, "\\", "/"), "/")
	root, err := s.fullPath(bucket, ".")
	if err != nil {
		return nil, err
	}
	objects := make([]storage.Object, 0)
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return &storage.ListResult{Objects: objects}, nil
	}
	err = filepath.WalkDir(root, func(itemPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && bucket == "" && entry.Name() == s.cfg.Local.TempDir {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, itemPath)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return nil
		}
		if input.Marker != "" && key <= input.Marker {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		objects = append(objects, storage.Object{
			FileName:     storage.FileNameFromKey(key),
			Key:          key,
			Bucket:       bucket,
			Size:         info.Size(),
			ContentType:  storage.ContentTypeFromName(key),
			URL:          s.publicURL(bucket, key, false),
			LastModified: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(objects, func(i int, j int) bool {
		return objects[i].Key < objects[j].Key
	})
	maxKeys := input.MaxKeys
	if maxKeys <= 0 {
		maxKeys = defaultListMaxKeys
	}
	result := &storage.ListResult{Objects: objects}
	if len(objects) > maxKeys {
		result.Objects = objects[:maxKeys]
		result.IsTruncated = true
		result.NextMarker = result.Objects[len(result.Objects)-1].Key
	}
	return result, nil
}

// Copy 复制对象。
func (s *Service) Copy(ctx context.Context, input storage.CopyInput) (*storage.Object, error) {
	reader, err := s.Download(ctx, storage.DownloadInput{Bucket: input.SourceBucket, Key: input.SourceKey})
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return s.Upload(ctx, storage.UploadInput{
		Bucket:    input.TargetBucket,
		Key:       input.TargetKey,
		Body:      reader,
		Overwrite: input.Overwrite,
	})
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

// PresignUpload 明确拒绝本地存储不具备的签名上传能力。
func (s *Service) PresignUpload(_ context.Context, _ storage.PresignInput) (*storage.PresignedURL, error) {
	return nil, storage.ErrUnsupported
}

// PresignDownload 明确拒绝本地存储不具备的签名下载能力。
func (s *Service) PresignDownload(_ context.Context, _ storage.PresignInput) (*storage.PresignedURL, error) {
	return nil, storage.ErrUnsupported
}

// PublicURL 返回对象公开访问 URL。
func (s *Service) PublicURL(_ context.Context, input storage.ObjectInput) (string, error) {
	key, err := storage.NormalizeObjectKey(input.Key)
	if err != nil {
		return "", err
	}
	return s.publicURL(storage.DefaultBucket(s.cfg, input.Bucket), key, true), nil
}

// CreateMultipartUpload 初始化本地分片上传会话。
func (s *Service) CreateMultipartUpload(_ context.Context, input storage.MultipartUploadInput) (*storage.MultipartUpload, error) {
	key, err := storage.NormalizeObjectKey(input.Key)
	if err != nil {
		return nil, err
	}
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	uploadID, err := storage.NewObjectKey("upload.tmp", time.Now())
	if err != nil {
		return nil, err
	}
	uploadID = strings.ReplaceAll(uploadID, "/", "-")
	dir, err := s.multipartDir(bucket, uploadID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &storage.MultipartUpload{Bucket: bucket, Key: key, UploadID: uploadID}, nil
}

// UploadPart 保存本地分片。
func (s *Service) UploadPart(_ context.Context, input storage.UploadPartInput) (*storage.UploadedPart, error) {
	if input.Body == nil {
		return nil, fmt.Errorf("%w: part body 不能为空", storage.ErrInvalidKey)
	}
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	if _, err := storage.NormalizeObjectKey(input.Key); err != nil {
		return nil, err
	}
	dir, err := s.multipartDir(bucket, input.UploadID)
	if err != nil {
		return nil, err
	}
	partPath, err := multipartPartPath(dir, input.PartNumber)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	file, err := os.Create(partPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	hash := md5.New() //nolint:gosec // ETag 只用于对象标识，不用于安全校验。
	written, err := copyWithLimit(io.MultiWriter(file, hash), input.Body, s.cfg.Local.MaxSize)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(partPath)
		return nil, err
	}
	return &storage.UploadedPart{
		PartNumber: input.PartNumber,
		ETag:       hex.EncodeToString(hash.Sum(nil)),
		Size:       written,
	}, nil
}

// CompleteMultipartUpload 合并本地分片。
func (s *Service) CompleteMultipartUpload(_ context.Context, input storage.CompleteMultipartUploadInput) (*storage.Object, error) {
	key, err := storage.NormalizeObjectKey(input.Key)
	if err != nil {
		return nil, err
	}
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	parts := storage.SortUploadedParts(input.Parts)
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: parts 不能为空", storage.ErrInvalidConfig)
	}
	dir, err := s.multipartDir(bucket, input.UploadID)
	if err != nil {
		return nil, err
	}
	partPaths := make([]string, len(parts))
	for index, part := range parts {
		partPaths[index], err = multipartPartPath(dir, part.PartNumber)
		if err != nil {
			return nil, err
		}
	}
	fullPath, err := s.fullPath(bucket, key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return nil, err
	}
	out, err := os.Create(fullPath)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	hash := md5.New() //nolint:gosec // ETag 只用于对象标识，不用于安全校验。
	total := int64(0)
	for index := range parts {
		if s.cfg.Local.MaxSize > 0 && total >= s.cfg.Local.MaxSize {
			_ = os.Remove(fullPath)
			return nil, fmt.Errorf("%w: 文件大小超过限制", storage.ErrInvalidConfig)
		}
		in, err := os.Open(partPaths[index])
		if err != nil {
			_ = os.Remove(fullPath)
			return nil, err
		}
		remaining := int64(0)
		if s.cfg.Local.MaxSize > 0 {
			remaining = s.cfg.Local.MaxSize - total
		}
		written, copyErr := copyWithLimit(io.MultiWriter(out, hash), in, remaining)
		closeErr := in.Close()
		if copyErr != nil {
			_ = os.Remove(fullPath)
			return nil, copyErr
		}
		if closeErr != nil {
			_ = os.Remove(fullPath)
			return nil, closeErr
		}
		total += written
	}
	if err := out.Sync(); err != nil {
		_ = os.Remove(fullPath)
		return nil, err
	}
	cleanupDir, err := s.multipartDir(bucket, input.UploadID)
	if err != nil {
		_ = os.Remove(fullPath)
		return nil, err
	}
	_ = os.RemoveAll(cleanupDir)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	return &storage.Object{
		FileName:     storage.FileNameFromKey(key),
		Key:          key,
		Bucket:       bucket,
		Size:         total,
		ContentType:  storage.ContentTypeFromName(key),
		ETag:         hex.EncodeToString(hash.Sum(nil)),
		URL:          s.publicURL(bucket, key, false),
		LastModified: info.ModTime(),
		CreatedAt:    time.Now(),
	}, nil
}

// AbortMultipartUpload 删除本地分片临时目录。
func (s *Service) AbortMultipartUpload(_ context.Context, input storage.AbortMultipartUploadInput) error {
	bucket := storage.DefaultBucket(s.cfg, input.Bucket)
	dir, err := s.multipartDir(bucket, input.UploadID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// PresignUploadPart 当前本地实现不提供分片直传端点。
func (s *Service) PresignUploadPart(_ context.Context, _ storage.PresignPartInput) (*storage.PresignedURL, error) {
	return nil, storage.ErrUnsupported
}

func (s *Service) objectKeyForUpload(input storage.UploadInput) (string, error) {
	if strings.TrimSpace(input.Key) != "" {
		return storage.NormalizeObjectKey(input.Key)
	}
	if s.cfg.Local.GenerateDatePath {
		return storage.NewObjectKey(input.FileName, time.Now())
	}
	return storage.NormalizeObjectKey(input.FileName)
}

func (s *Service) fullPath(bucket string, key string) (string, error) {
	parts := []string{s.cfg.Local.RootDir}
	if strings.TrimSpace(bucket) != "" {
		cleanBucket, err := storage.NormalizeObjectKey(bucket)
		if err != nil {
			return "", err
		}
		parts = append(parts, filepath.FromSlash(cleanBucket))
	}
	if key != "." {
		cleanKey, err := storage.NormalizeObjectKey(key)
		if err != nil {
			return "", err
		}
		parts = append(parts, filepath.FromSlash(cleanKey))
	}
	fullPath := filepath.Join(parts...)
	rel, err := filepath.Rel(s.cfg.Local.RootDir, fullPath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", storage.ErrInvalidKey, key)
	}
	return fullPath, nil
}

func (s *Service) multipartRoot() (string, error) {
	tempDir := strings.TrimSpace(strings.ReplaceAll(s.cfg.Local.TempDir, "\\", "/"))
	if tempDir == "" || strings.HasPrefix(tempDir, "/") {
		return "", fmt.Errorf("%w: storage.local.temp_dir 必须是非空相对路径", storage.ErrInvalidConfig)
	}
	for _, segment := range strings.Split(tempDir, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, ":") {
			return "", fmt.Errorf("%w: storage.local.temp_dir 包含非法路径片段", storage.ErrInvalidConfig)
		}
	}
	tempRoot := filepath.Join(s.cfg.Local.RootDir, filepath.FromSlash(tempDir))
	contained, err := isStrictSubdirectory(s.cfg.Local.RootDir, tempRoot)
	if err != nil {
		return "", fmt.Errorf("%w: 校验本地分片目录失败: %v", storage.ErrInvalidConfig, err)
	}
	if !contained {
		return "", fmt.Errorf("%w: storage.local.temp_dir 必须位于存储根目录内", storage.ErrInvalidConfig)
	}
	return tempRoot, nil
}

func (s *Service) multipartDir(bucket string, uploadID string) (string, error) {
	tempRoot, err := s.multipartRoot()
	if err != nil {
		return "", err
	}
	cleanBucket, err := validateMultipartSegment("bucket", bucket, true)
	if err != nil {
		return "", err
	}
	cleanUploadID, err := validateMultipartSegment("upload_id", uploadID, false)
	if err != nil {
		return "", err
	}
	parts := []string{tempRoot}
	if cleanBucket != "" {
		parts = append(parts, cleanBucket)
	}
	parts = append(parts, cleanUploadID)
	dir := filepath.Join(parts...)
	contained, err := isStrictSubdirectory(tempRoot, dir)
	if err != nil {
		return "", fmt.Errorf("校验本地分片会话目录失败: %w", err)
	}
	if !contained {
		return "", fmt.Errorf("%w: 分片会话目录必须位于临时根目录内", storage.ErrInvalidKey)
	}
	return dir, nil
}

func multipartPartPath(dir string, partNumber int) (string, error) {
	if partNumber <= 0 {
		return "", fmt.Errorf("%w: part_number 必须大于 0", storage.ErrInvalidConfig)
	}
	partPath := filepath.Join(dir, strconv.Itoa(partNumber))
	contained, err := isStrictSubdirectory(dir, partPath)
	if err != nil {
		return "", fmt.Errorf("校验本地分片路径失败: %w", err)
	}
	if !contained {
		return "", fmt.Errorf("%w: 分片路径必须位于会话目录内", storage.ErrInvalidKey)
	}
	return partPath, nil
}

func validateMultipartSegment(name string, value string, allowEmpty bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if allowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("%w: %s 不能为空", storage.ErrInvalidKey, name)
	}
	if value == "." || value == ".." {
		return "", fmt.Errorf("%w: %s 包含非法路径片段", storage.ErrInvalidKey, name)
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.' {
			continue
		}
		return "", fmt.Errorf("%w: %s 只能包含字母、数字、点、下划线和连字符", storage.ErrInvalidKey, name)
	}
	return value, nil
}

func isStrictSubdirectory(root string, target string) (bool, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false, err
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

func (s *Service) validateExtension(key string) error {
	if len(s.cfg.Local.AllowedExtensions) == 0 {
		return nil
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(key)), ".")
	for _, allowed := range s.cfg.Local.AllowedExtensions {
		if strings.TrimPrefix(strings.ToLower(strings.TrimSpace(allowed)), ".") == ext {
			return nil
		}
	}
	return fmt.Errorf("%w: 不支持的文件类型 %q", storage.ErrInvalidConfig, ext)
}

func (s *Service) publicURL(bucket string, key string, requireBase bool) string {
	baseURL := s.cfg.Local.PublicBaseURL
	if baseURL == "" {
		baseURL = s.cfg.PublicBaseURL
	}
	if baseURL == "" {
		if requireBase {
			return ""
		}
		return ""
	}
	return storage.JoinURL(baseURL, bucket, key)
}

func copyWithLimit(dst io.Writer, src io.Reader, maxSize int64) (int64, error) {
	if maxSize <= 0 {
		return io.Copy(dst, src)
	}
	limited := &io.LimitedReader{R: src, N: maxSize + 1}
	written, err := io.Copy(dst, limited)
	if err != nil {
		return written, err
	}
	if written > maxSize {
		return written, fmt.Errorf("%w: 文件大小超过限制", storage.ErrInvalidConfig)
	}
	return written, nil
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
