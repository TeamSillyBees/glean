package file

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

// LocalFile 是 file 示例模块的本地文件领域模型。
type LocalFile struct {
	Key          string
	FileName     string
	Size         int64
	ContentType  string
	URL          string
	LastModified time.Time
}

// DownloadLocalResult 描述下载本地文件的结果。
type DownloadLocalResult struct {
	Info LocalFileVO
	Body []byte
}

// fileStorage 定义 file 示例模块依赖的最小存储能力。
type fileStorage interface {
	Upload(ctx context.Context, input storage.UploadInput) (*storage.Object, error)
	DownloadBytes(ctx context.Context, input storage.DownloadInput) ([]byte, error)
	Delete(ctx context.Context, input storage.DeleteInput) error
	Stat(ctx context.Context, input storage.ObjectInput) (*storage.Object, error)
}

// Service 是 file 示例模块的应用服务。
type Service struct {
	storage fileStorage
}

// NewService 构造 file 示例模块应用服务。
func NewService(storage fileStorage) *Service {
	return &Service{storage: storage}
}

// UploadLocal 上传文件到当前配置的存储 provider。
func (s *Service) UploadLocal(ctx context.Context, fileName string, contentType string, size int64, body io.Reader) (LocalFileVO, error) {
	if err := s.ensureStorage(); err != nil {
		return LocalFileVO{}, err
	}
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return LocalFileVO{}, bizerrors.BadRequest("file name is required")
	}
	if body == nil {
		return LocalFileVO{}, bizerrors.BadRequest("file body is required")
	}

	object, err := s.storage.Upload(ctx, storage.UploadInput{
		FileName:    fileName,
		Body:        body,
		Size:        size,
		ContentType: strings.TrimSpace(contentType),
	})
	if err != nil {
		return LocalFileVO{}, mapStorageError(ctx, err, "upload local file failed")
	}
	return toLocalFileVOFromObject(object), nil
}

// DownloadLocal 下载本地文件示例对象。
func (s *Service) DownloadLocal(ctx context.Context, key string) (DownloadLocalResult, error) {
	if err := s.ensureStorage(); err != nil {
		return DownloadLocalResult{}, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return DownloadLocalResult{}, bizerrors.BadRequest("key is required")
	}

	object, err := s.storage.Stat(ctx, storage.ObjectInput{Key: key})
	if err != nil {
		return DownloadLocalResult{}, mapStorageError(ctx, err, "load local file metadata failed")
	}
	body, err := s.storage.DownloadBytes(ctx, storage.DownloadInput{Key: key})
	if err != nil {
		return DownloadLocalResult{}, mapStorageError(ctx, err, "download local file failed")
	}
	return DownloadLocalResult{
		Info: toLocalFileVOFromObject(object),
		Body: body,
	}, nil
}

// StatLocal 查询本地文件示例对象元信息。
func (s *Service) StatLocal(ctx context.Context, key string) (LocalFileVO, error) {
	if err := s.ensureStorage(); err != nil {
		return LocalFileVO{}, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return LocalFileVO{}, bizerrors.BadRequest("key is required")
	}

	object, err := s.storage.Stat(ctx, storage.ObjectInput{Key: key})
	if err != nil {
		return LocalFileVO{}, mapStorageError(ctx, err, "load local file metadata failed")
	}
	return toLocalFileVOFromObject(object), nil
}

// DeleteLocal 删除本地文件示例对象。
func (s *Service) DeleteLocal(ctx context.Context, key string) error {
	if err := s.ensureStorage(); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return bizerrors.BadRequest("key is required")
	}
	if err := s.storage.Delete(ctx, storage.DeleteInput{Key: key}); err != nil {
		return mapStorageError(ctx, err, "delete local file failed")
	}
	return nil
}

func (s *Service) ensureStorage() error {
	if s == nil || s.storage == nil {
		return bizerrors.Internal("storage service is not configured")
	}
	return nil
}

func toLocalFileVOFromObject(object *storage.Object) LocalFileVO {
	if object == nil {
		return LocalFileVO{}
	}
	contentType := object.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	fileName := object.FileName
	if fileName == "" {
		fileName = storage.FileNameFromKey(object.Key)
	}
	return LocalFileVO{
		Key:          object.Key,
		FileName:     fileName,
		Size:         object.Size,
		ContentType:  contentType,
		URL:          object.URL,
		LastModified: object.LastModified,
	}
}

func mapStorageError(ctx context.Context, err error, message string) error {
	switch {
	case err == nil:
		return nil
	case storage.IsNotFound(err):
		return bizerrors.WrapNotFoundContext(ctx, err, "file not found")
	case errors.Is(err, storage.ErrInvalidKey),
		errors.Is(err, storage.ErrInvalidConfig),
		errors.Is(err, storage.ErrObjectExists):
		return bizerrors.WrapBadRequestContext(ctx, err, message)
	default:
		return bizerrors.WrapStorageContext(ctx, err, message)
	}
}
