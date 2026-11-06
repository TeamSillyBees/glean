package catalog

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/response"
	"github.com/teamsillybees/glean/internal/retrieval"
)

type protocolPreviewRequest struct {
	Body struct {
		Document json.RawMessage `json:"document"`
	}
}
type protocolPreviewResponse struct {
	Body response.SuccessVO[retrieval.ProtocolPreview]
}

func (h *Handler) previewProtocol(ctx context.Context, input *protocolPreviewRequest) (*protocolPreviewResponse, error) {
	if _, err := requireManager(ctx); err != nil {
		return nil, err
	}
	p, err := retrieval.ImportProtocol(input.Body.Document, time.Now().UTC())
	if err != nil {
		return nil, bizerrors.BadRequest(err.Error())
	}
	return &protocolPreviewResponse{Body: response.OK(ctx, p)}, nil
}

func prepareTopicProtocol(input *TopicBody, previous *retrieval.Protocol) error {
	if input.SearchProtocol == nil {
		input.SearchProtocol = previous
	}
	if input.SearchProtocol == nil {
		return nil
	}
	p, err := retrieval.PreviewProtocol(*input.SearchProtocol, time.Now().UTC())
	if err != nil {
		return bizerrors.BadRequest(err.Error())
	}
	raw, _ := json.Marshal(input.SearchProtocol)
	old, _ := json.Marshal(previous)
	if string(raw) != string(old) {
		if input.ProtocolAcknowledgement != p.Acknowledgement {
			return bizerrors.BadRequest("请先预览并确认协议的来源、日期和执行语义差异")
		}
		if previous != nil && (input.SearchProtocol.ProfileID != previous.ProfileID || input.SearchProtocol.ProfileVersion == previous.ProfileVersion) {
			return bizerrors.BadRequest("协议 ID 不可改变；更改协议必须使用新的 profile_version")
		}
	}
	if input.SearchEnabled && !p.Executable {
		return bizerrors.BadRequest("计划模式或未明确历史起点的协议不能启用采集")
	}
	input.SearchRoutes = &p.SearchRoutes
	input.TermGroups = [][]string{}
	return nil
}
