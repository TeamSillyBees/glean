package user

import (
	"strings"
	"time"

	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func userToVO(user *User) UserVO {
	return UserVO{
		ID:           user.ID,
		Username:     user.Username,
		Nickname:     user.Nickname,
		Phone:        user.Phone,
		Email:        user.Email,
		AvatarURL:    user.AvatarURL,
		RoleCodes:    append([]string(nil), user.RoleCodes...),
		IsSuperAdmin: user.IsSuperAdmin,
		IsEnable:     user.IsEnable,
		SortID:       user.SortID,
	}
}

func userFromEnt(record *appent.SysUser) *User {
	user := &User{}
	fillDomainFromEnt(user, record)
	user.RoleCodes = []string{}
	return user
}

func fillDomainFromEnt(user *User, record *appent.SysUser) {
	user.ID = record.ID
	user.Username = record.Username
	user.PasswordHash = record.PasswordHash
	user.Nickname = stringValue(record.Nickname)
	user.Phone = stringValue(record.Phone)
	user.Email = stringValue(record.Email)
	user.AvatarURL = stringValue(record.AvatarURL)
	user.IsSuperAdmin = record.IsSuperAdmin
	user.IsEnable = record.IsEnable
	user.SortID = record.SortID
	user.CreatedAt = record.CreatedAt
	user.UpdatedAt = record.UpdatedAt
	user.DeletedAt = cloneTimePtr(record.DeletedAt)
	user.CreatedBy = idValue(record.CreatedBy)
	user.UpdatedBy = idValue(record.UpdatedBy)
}

func setOptionalStrings(update *appent.SysUserUpdateOne, user *User) {
	setOrClearString(update.SetNickname, update.ClearNickname, user.Nickname)
	setOrClearString(update.SetPhone, update.ClearPhone, user.Phone)
	setOrClearString(update.SetEmail, update.ClearEmail, user.Email)
	setOrClearString(update.SetAvatarURL, update.ClearAvatarURL, user.AvatarURL)
}

func setOrClearString(set func(string) *appent.SysUserUpdateOne, clear func() *appent.SysUserUpdateOne, value string) {
	if strings.TrimSpace(value) == "" {
		clear()
		return
	}
	set(value)
}

func nullableString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func idValue(value *idgen.ID) idgen.ID {
	if value == nil {
		return 0
	}
	return *value
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
