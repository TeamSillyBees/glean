package file

import (
	"context"
	"mime"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// Handler 封装 file 示例模块的 HTTP 适配逻辑。
type Handler struct {
	service *Service
}

// NewHandler 创建 file 示例模块 HTTP Handler。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) upload(ctx context.Context, input *uploadLocalFileRequest) (*uploadLocalFileResponse, error) {
	if input.RawBody.Form != nil {
		defer input.RawBody.Form.RemoveAll()
	}
	data := input.RawBody.Data()
	if data == nil || !data.File.IsSet {
		return nil, bizerrors.BadRequest("file is required")
	}
	defer data.File.Close()

	vo, err := h.service.UploadLocal(ctx, data.File.Filename, data.File.ContentType, data.File.Size, data.File.File)
	if err != nil {
		return nil, err
	}
	return &uploadLocalFileResponse{
		Body: response.OK(ctx, vo),
	}, nil
}

func (h *Handler) download(ctx context.Context, input *downloadLocalFileRequest) (*downloadLocalFileResponse, error) {
	result, err := h.service.DownloadLocal(ctx, input.Key)
	if err != nil {
		return nil, err
	}
	return &downloadLocalFileResponse{
		ContentType:        result.Info.ContentType,
		ContentDisposition: attachmentDisposition(result.Info.FileName),
		Body:               result.Body,
	}, nil
}

func (h *Handler) stat(ctx context.Context, input *statLocalFileRequest) (*statLocalFileResponse, error) {
	vo, err := h.service.StatLocal(ctx, input.Key)
	if err != nil {
		return nil, err
	}
	return &statLocalFileResponse{
		Body: response.OK(ctx, vo),
	}, nil
}

func (h *Handler) delete(ctx context.Context, input *deleteLocalFileRequest) (*deleteLocalFileResponse, error) {
	if err := h.service.DeleteLocal(ctx, input.Key); err != nil {
		return nil, err
	}
	return &deleteLocalFileResponse{
		Body: response.OK(ctx, map[string]any{}),
	}, nil
}

func attachmentDisposition(fileName string) string {
	if fileName == "" {
		fileName = "download"
	}
	return mime.FormatMediaType("attachment", map[string]string{"filename": fileName})
}
