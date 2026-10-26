package file

import (
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// LocalFileVO 是 file 示例模块对外暴露的本地文件 JSON DTO。
type LocalFileVO struct {
	Key          string    `json:"key"`
	FileName     string    `json:"fileName"`
	Size         int64     `json:"size"`
	ContentType  string    `json:"contentType"`
	URL          string    `json:"url,omitempty"`
	LastModified time.Time `json:"lastModified,omitempty"`
}

type uploadLocalFileForm struct {
	File huma.FormFile `form:"file" required:"true" contentType:"application/octet-stream" doc:"待上传文件"`
}

type uploadLocalFileRequest struct {
	RawBody huma.MultipartFormFiles[uploadLocalFileForm]
}

type uploadLocalFileResponse struct {
	Body response.SuccessVO[LocalFileVO]
}

type downloadLocalFileRequest struct {
	Key string `query:"key" required:"true" example:"2026/05/07/demo.txt" doc:"对象 key"`
}

type downloadLocalFileResponse struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

type statLocalFileRequest struct {
	Key string `query:"key" required:"true" example:"2026/05/07/demo.txt" doc:"对象 key"`
}

type statLocalFileResponse struct {
	Body response.SuccessVO[LocalFileVO]
}

type deleteLocalFileRequest struct {
	Key string `query:"key" required:"true" example:"2026/05/07/demo.txt" doc:"对象 key"`
}

type deleteLocalFileResponse struct {
	Body response.SuccessVO[map[string]any]
}
