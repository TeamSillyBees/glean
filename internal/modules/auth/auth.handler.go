package auth

import (
	"context"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// Handler 封装 auth 模块的 HTTP 适配逻辑。
type Handler struct {
	service *Service
}

// NewHandler 创建 auth 模块 Handler。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) login(ctx context.Context, input *loginRequest) (*loginResponse, error) {
	vo, err := h.service.Login(ctx, input.Body)
	if err != nil {
		return nil, err
	}
	return &loginResponse{
		Body: response.OK(ctx, vo),
	}, nil
}

func (h *Handler) refresh(ctx context.Context, input *refreshRequest) (*refreshResponse, error) {
	vo, err := h.service.Refresh(ctx, input.Body)
	if err != nil {
		return nil, err
	}
	return &refreshResponse{
		Body: response.OK(ctx, vo),
	}, nil
}

func (h *Handler) logout(ctx context.Context, input *logoutRequest) (*logoutResponse, error) {
	principal, ok := currentPrincipal(ctx)
	if !ok {
		return nil, bizerrors.Unauthorized("authorization identity is missing")
	}
	if err := h.service.Logout(ctx, principal, input.Body); err != nil {
		return nil, err
	}
	return &logoutResponse{Body: response.OK(ctx, map[string]any{})}, nil
}

func (h *Handler) logoutAll(ctx context.Context, _ *logoutAllRequest) (*logoutAllResponse, error) {
	principal, ok := currentPrincipal(ctx)
	if !ok {
		return nil, bizerrors.Unauthorized("authorization identity is missing")
	}
	if err := h.service.LogoutAll(ctx, principal); err != nil {
		return nil, err
	}
	return &logoutAllResponse{Body: response.OK(ctx, map[string]any{})}, nil
}

func (h *Handler) changePassword(ctx context.Context, input *changePasswordRequest) (*changePasswordResponse, error) {
	principal, ok := currentPrincipal(ctx)
	if !ok {
		return nil, bizerrors.Unauthorized("authorization identity is missing")
	}
	if err := h.service.ChangePassword(ctx, principal, input.Body); err != nil {
		return nil, err
	}
	return &changePasswordResponse{Body: response.OK(ctx, map[string]any{})}, nil
}

func (h *Handler) me(ctx context.Context, _ *meRequest) (*meResponse, error) {
	userIDText, ok := requestctx.UserIDFromContext(ctx)
	if !ok {
		return nil, bizerrors.Unauthorized("user_id is missing")
	}
	userID, err := idgen.Parse(userIDText)
	if err != nil {
		return nil, bizerrors.Unauthorized("user_id is invalid")
	}

	vo, err := h.service.Me(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &meResponse{
		Body: response.OK(ctx, vo),
	}, nil
}

func currentPrincipal(ctx context.Context) (platformauth.Principal, bool) {
	return platformauth.PrincipalFromContext(ctx)
}
