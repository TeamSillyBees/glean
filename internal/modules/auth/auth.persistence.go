package auth

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/teamsillybees/glean/internal/data"
	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

// Logout 原子消费当前会话的 refresh token，并由 token manager 吊销配对 access token。
func (s *Service) Logout(ctx context.Context, principal platformauth.Principal, body LogoutBody) error {
	refreshToken := strings.TrimSpace(body.RefreshToken)
	if refreshToken == "" {
		return bizerrors.BadRequest("refresh token is required")
	}
	record, err := s.tokens.ValidateRefreshToken(ctx, refreshToken)
	if err != nil {
		return mapSessionTokenError(ctx, err, "validate logout refresh token failed")
	}
	if record.UserID != principal.UserID ||
		record.SessionID != principal.SessionID ||
		record.SessionVersion != principal.SessionVersion {
		return bizerrors.Unauthorized("refresh token is invalid")
	}
	if _, err := s.tokens.ConsumeRefreshToken(ctx, refreshToken); err != nil {
		return mapSessionTokenError(ctx, err, "revoke current session failed")
	}
	s.logger.Info(ctx, "current session revoked",
		logx.String("event", "auth.session.revoked"),
		logx.String("user_id", principal.UserID.String()),
		logx.String("session_id", principal.SessionID),
	)
	return nil
}

// LogoutAll 递增用户级会话版本，使全部旧 access token 与 refresh token 失效。
func (s *Service) LogoutAll(ctx context.Context, principal platformauth.Principal) error {
	if err := s.incrementSessionVersion(ctx, principal.UserID); err != nil {
		return err
	}
	if err := s.authz.NotifyChanged(ctx, []idgen.ID{principal.UserID}, false); err != nil {
		return bizerrors.WrapInternalContext(ctx, err, "invalidate authorization identity failed")
	}
	s.logger.Info(ctx, "all sessions revoked",
		logx.String("event", "auth.sessions.revoked"),
		logx.String("user_id", principal.UserID.String()),
	)
	return nil
}

// ChangePassword 校验当前密码并在同一事务内更新密码、递增会话版本。
func (s *Service) ChangePassword(ctx context.Context, principal platformauth.Principal, body ChangePasswordBody) error {
	currentPassword := body.CurrentPassword
	newPassword := body.NewPassword
	if strings.TrimSpace(currentPassword) == "" || len([]rune(newPassword)) < 8 {
		return bizerrors.BadRequest("current password and a new password of at least 8 characters are required")
	}
	newPasswordHash, err := s.passwords.Hash(newPassword)
	if err != nil {
		return bizerrors.WrapInternalContext(ctx, err, "hash new password failed")
	}
	if err := s.changePassword(ctx, principal.UserID, currentPassword, newPassword, newPasswordHash); err != nil {
		return err
	}
	if err := s.authz.NotifyChanged(ctx, []idgen.ID{principal.UserID}, false); err != nil {
		return bizerrors.WrapInternalContext(ctx, err, "invalidate authorization identity failed")
	}
	s.logger.Info(ctx, "password changed and sessions revoked",
		logx.String("event", "auth.password.changed"),
		logx.String("user_id", principal.UserID.String()),
	)
	return nil
}

func (s *Service) incrementSessionVersion(ctx context.Context, userID idgen.ID) error {
	return data.WithinTx(ctx, s.client, func(txCtx context.Context, txClient *appent.Client) error {
		current, err := txClient.SysUser.Query().
			Where(sysuser.ID(userID), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true)).
			ForUpdate().
			Only(txCtx)
		if appent.IsNotFound(err) {
			return bizerrors.Unauthorized("authorization identity is disabled or missing")
		}
		if err != nil {
			return bizerrors.WrapDBContext(txCtx, err, "query current session version failed")
		}
		if current.SessionVersion == math.MaxInt64 {
			return bizerrors.Internal("session version exhausted")
		}
		if _, err := txClient.SysUser.UpdateOneID(userID).
			Where(sysuser.DeletedAtIsNil(), sysuser.IsEnable(true)).
			SetSessionVersion(current.SessionVersion + 1).
			Save(txCtx); err != nil {
			return mapAuthEntWriteError(txCtx, err, "revoke all sessions failed")
		}
		return nil
	})
}

func (s *Service) changePassword(ctx context.Context, userID idgen.ID, currentPassword string, newPassword string, newPasswordHash string) error {
	return data.WithinTx(ctx, s.client, func(txCtx context.Context, txClient *appent.Client) error {
		current, err := txClient.SysUser.Query().
			Where(sysuser.ID(userID), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true)).
			ForUpdate().
			Only(txCtx)
		if appent.IsNotFound(err) {
			return bizerrors.Unauthorized("authorization identity is disabled or missing")
		}
		if err != nil {
			return bizerrors.WrapDBContext(txCtx, err, "query current password failed")
		}
		if s.passwords.Compare(current.PasswordHash, currentPassword) != nil ||
			s.passwords.Compare(current.PasswordHash, newPassword) == nil {
			return bizerrors.BadRequest("password change failed")
		}
		if current.SessionVersion == math.MaxInt64 {
			return bizerrors.Internal("session version exhausted")
		}
		if _, err := txClient.SysUser.UpdateOneID(userID).
			Where(sysuser.DeletedAtIsNil(), sysuser.IsEnable(true)).
			SetPasswordHash(newPasswordHash).
			SetSessionVersion(current.SessionVersion + 1).
			Save(txCtx); err != nil {
			return mapAuthEntWriteError(txCtx, err, "change password failed")
		}
		return nil
	})
}

func mapSessionTokenError(ctx context.Context, err error, internalMessage string) error {
	if errors.Is(err, platformauth.ErrTokenStoreFailure) {
		return bizerrors.WrapInternalContext(ctx, err, internalMessage)
	}
	return bizerrors.Unauthorized("refresh token is invalid")
}

func mapAuthEntWriteError(ctx context.Context, err error, message string) error {
	if appent.IsConstraintError(err) {
		return bizerrors.WrapBadRequestContext(ctx, err, message)
	}
	return bizerrors.WrapDBContext(ctx, err, message)
}
