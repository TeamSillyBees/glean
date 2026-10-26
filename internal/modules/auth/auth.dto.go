package auth

import (
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// LoginBody 描述登录请求体。
type LoginBody struct {
	Username string `json:"username" example:"admin"`
	Password string `json:"password" example:"admin123"`
}

type loginRequest struct {
	Body LoginBody
}

// RefreshBody 描述刷新 token 请求体。
type RefreshBody struct {
	RefreshToken string `json:"refreshToken" example:"token"`
}

type refreshRequest struct {
	Body RefreshBody
}

// LogoutBody 描述撤销当前会话所需的 refresh token。
type LogoutBody struct {
	RefreshToken string `json:"refreshToken" example:"token"`
}

type logoutRequest struct {
	Body LogoutBody
}

type logoutAllRequest struct{}

// ChangePasswordBody 描述当前用户修改密码请求体。
type ChangePasswordBody struct {
	CurrentPassword string `json:"currentPassword" example:"old-secret-123"`
	NewPassword     string `json:"newPassword" example:"new-secret-456"`
}

type changePasswordRequest struct {
	Body ChangePasswordBody
}

type meRequest struct{}

// LoginVO 描述登录成功响应体。
type LoginVO struct {
	AccessToken  string         `json:"accessToken"`
	RefreshToken string         `json:"refreshToken"`
	User         UserIdentityVO `json:"user"`
}

// RefreshVO 描述刷新 token 响应体。
type RefreshVO struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// UserIdentityVO 描述当前登录用户的公开身份信息。
type UserIdentityVO struct {
	UserID       idgen.ID `json:"userId"`
	Username     string   `json:"username"`
	Nickname     string   `json:"nickname"`
	RoleCodes    []string `json:"roleCodes"`
	IsSuperAdmin bool     `json:"isSuperAdmin"`
	IsEnable     bool     `json:"isEnable"`
}

type loginResponse struct {
	Body response.SuccessVO[LoginVO]
}

type refreshResponse struct {
	Body response.SuccessVO[RefreshVO]
}

type meResponse struct {
	Body response.SuccessVO[UserIdentityVO]
}

type logoutResponse struct {
	Body response.SuccessVO[map[string]any]
}

type logoutAllResponse struct {
	Body response.SuccessVO[map[string]any]
}

type changePasswordResponse struct {
	Body response.SuccessVO[map[string]any]
}
