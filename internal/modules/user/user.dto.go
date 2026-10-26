package user

import (
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/pagination"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// PageUsersQuery 描述用户分页 HTTP 查询参数。
type PageUsersQuery struct {
	pagination.PageQuery
	Keyword string `query:"keyword" example:"alice" doc:"用户名、昵称、手机号或邮箱关键字"`
}

// CreateUserBody 描述创建用户请求体。
type CreateUserBody struct {
	Username     string   `json:"username" example:"alice"`
	Password     string   `json:"password" example:"secret-123"`
	Nickname     string   `json:"nickname" example:"Alice"`
	Phone        string   `json:"phone" example:"13800000000"`
	Email        string   `json:"email" example:"alice@example.com"`
	AvatarURL    string   `json:"avatarUrl" example:"https://example.com/avatar.png"`
	RoleCodes    []string `json:"roleCodes" example:"admin"`
	IsSuperAdmin bool     `json:"isSuperAdmin" example:"false"`
	IsEnable     *bool    `json:"isEnable,omitempty" example:"true"`
	SortID       int32    `json:"sortId" example:"0"`
}

// UpdateUserBody 描述更新用户请求体。
type UpdateUserBody struct {
	Nickname     *string   `json:"nickname,omitempty" example:"Alice Updated"`
	Phone        *string   `json:"phone,omitempty" example:"13800000001"`
	Email        *string   `json:"email,omitempty" example:"alice.updated@example.com"`
	AvatarURL    *string   `json:"avatarUrl,omitempty" example:"https://example.com/avatar-updated.png"`
	RoleCodes    *[]string `json:"roleCodes,omitempty" example:"viewer"`
	IsSuperAdmin *bool     `json:"isSuperAdmin,omitempty" example:"false"`
	IsEnable     *bool     `json:"isEnable,omitempty" example:"true"`
	SortID       *int32    `json:"sortId,omitempty" example:"10"`
}

// UserVO 是 user 模块对外暴露的用户 JSON DTO。
type UserVO struct {
	ID           idgen.ID `json:"id"`
	Username     string   `json:"username"`
	Nickname     string   `json:"nickname"`
	Phone        string   `json:"phone"`
	Email        string   `json:"email"`
	AvatarURL    string   `json:"avatarUrl"`
	RoleCodes    []string `json:"roleCodes"`
	IsSuperAdmin bool     `json:"isSuperAdmin"`
	IsEnable     bool     `json:"isEnable"`
	SortID       int32    `json:"sortId"`
}

type getUserRequest struct {
	ID idgen.ID `path:"id" example:"1771234567890123456" doc:"用户 ID"`
}

type getUserResponse struct {
	Body response.SuccessVO[UserVO]
}

type pageUsersRequest struct {
	PageUsersQuery
}

type pageUsersResponse struct {
	Body response.SuccessVO[pagination.PageVO[UserVO]]
}

type createUserRequest struct {
	Body CreateUserBody
}

type createUserResponse struct {
	Body response.SuccessVO[UserVO]
}

type updateUserRequest struct {
	ID   idgen.ID `path:"id" example:"1771234567890123456" doc:"用户 ID"`
	Body UpdateUserBody
}

type updateUserResponse struct {
	Body response.SuccessVO[UserVO]
}

type deleteUserRequest struct {
	ID idgen.ID `path:"id" example:"1771234567890123456" doc:"用户 ID"`
}

type deleteUserResponse struct {
	Body response.SuccessVO[map[string]any]
}
