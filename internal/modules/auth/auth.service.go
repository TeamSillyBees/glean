package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/sysrole"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/data/ent/sysuserrole"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// Identity 描述 auth 模块关心的最小用户身份聚合。
type Identity struct {
	UserID         idgen.ID
	Username       string
	Nickname       string
	PasswordHash   string
	SessionVersion int64
	RoleCodes      []string
	IsSuperAdmin   bool
	IsEnable       bool
}

// passwordVerifier 抽象密码校验行为。
type passwordManager interface {
	Hash(password string) (string, error)
	Compare(hash string, password string) error
}

// tokenManager 定义 auth 模块依赖的令牌能力。
type tokenManager interface {
	IssueTokenPair(ctx context.Context, principal platformauth.Principal) (platformauth.TokenPair, error)
	ValidateRefreshToken(ctx context.Context, token string) (*platformauth.RefreshTokenRecord, error)
	RotateRefreshToken(ctx context.Context, token string, expected platformauth.RefreshTokenRecord, principal platformauth.Principal) (platformauth.TokenPair, error)
	ConsumeRefreshToken(ctx context.Context, token string) (*platformauth.RefreshTokenRecord, error)
}

type authorizationInvalidator interface {
	NotifyChanged(ctx context.Context, userIDs []idgen.ID, reloadPolicy bool) error
}

// Service 是 auth 模块的应用服务，直接通过 Ent Client 操作数据库。
type Service struct {
	client            *appent.Client
	passwords         passwordManager
	tokens            tokenManager
	loginGuard        platformauth.LoginGuard
	authz             authorizationInvalidator
	logger            *logx.Logger
	dummyPasswordHash string
}

// NewService 构造 auth 模块应用服务。
func NewService(
	client *appent.Client,
	passwords passwordManager,
	tokens tokenManager,
	loginGuard platformauth.LoginGuard,
	invalidator authorizationInvalidator,
	logger *logx.Logger,
) (*Service, error) {
	if client == nil || passwords == nil || tokens == nil || loginGuard == nil || invalidator == nil {
		return nil, fmt.Errorf("auth service 依赖不能为空")
	}
	dummyPasswordHash, err := passwords.Hash("initra-login-timing-placeholder")
	if err != nil {
		return nil, fmt.Errorf("初始化登录等时密码哈希失败: %w", err)
	}
	if logger == nil {
		logger = logx.NewNop()
	}
	return &Service{
		client:            client,
		passwords:         passwords,
		tokens:            tokens,
		loginGuard:        loginGuard,
		authz:             invalidator,
		logger:            logger.Named("security.audit"),
		dummyPasswordHash: dummyPasswordHash,
	}, nil
}

// Login 校验账号密码并签发 access JWT 与 opaque refresh token。
func (s *Service) Login(ctx context.Context, body LoginBody) (LoginVO, error) {
	account := strings.TrimSpace(body.Username)
	sourceIP, _ := requestctx.ClientIPFromContext(ctx)
	if account == "" || strings.TrimSpace(body.Password) == "" {
		s.auditLoginRejected(ctx, account, sourceIP, "credentials_invalid")
		return LoginVO{}, bizerrors.LoginFailed()
	}
	if err := s.loginGuard.Check(ctx, account, sourceIP); err != nil {
		if errors.Is(err, platformauth.ErrLoginProtectionStoreFailure) {
			return LoginVO{}, bizerrors.WrapInternalContext(ctx, err, "check login protection failed")
		}
		s.auditLoginRejected(ctx, account, sourceIP, loginGuardReason(err))
		return LoginVO{}, bizerrors.LoginFailed()
	}

	identity, err := s.findByUsername(ctx, account)
	if err != nil {
		return LoginVO{}, err
	}
	passwordHash := s.dummyPasswordHash
	if identity != nil {
		passwordHash = identity.PasswordHash
	}
	passwordMatches := s.passwords.Compare(passwordHash, body.Password) == nil
	if identity == nil || !identity.IsEnable || !passwordMatches {
		guardErr := s.loginGuard.RecordFailure(ctx, account)
		if errors.Is(guardErr, platformauth.ErrLoginProtectionStoreFailure) {
			return LoginVO{}, bizerrors.WrapInternalContext(ctx, guardErr, "record login failure failed")
		}
		reason := "credentials_invalid"
		if errors.Is(guardErr, platformauth.ErrLoginLocked) {
			reason = "account_locked"
		}
		s.auditLoginRejected(ctx, account, sourceIP, reason)
		return LoginVO{}, bizerrors.LoginFailed()
	}
	if err := s.loginGuard.Reset(ctx, account); err != nil {
		return LoginVO{}, bizerrors.WrapInternalContext(ctx, err, "reset login protection failed")
	}

	tokenPair, err := s.tokens.IssueTokenPair(ctx, platformauth.Principal{
		UserID:         identity.UserID,
		SessionVersion: identity.SessionVersion,
		Roles:          append([]string(nil), identity.RoleCodes...),
	})
	if err != nil {
		return LoginVO{}, bizerrors.WrapInternalContext(ctx, err, "issue token failed")
	}

	s.logger.Info(ctx, "login succeeded",
		logx.String("event", "auth.login.succeeded"),
		logx.String("account", account),
		logx.String("source_ip", sourceIP),
		logx.String("user_id", identity.UserID.String()),
	)
	return LoginVO{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		User:         toUserIdentityVO(identity),
	}, nil
}

// Refresh 校验 opaque refresh token 并轮转新的 token pair。
func (s *Service) Refresh(ctx context.Context, body RefreshBody) (RefreshVO, error) {
	if strings.TrimSpace(body.RefreshToken) == "" {
		return RefreshVO{}, bizerrors.BadRequest("refresh token is required")
	}

	record, err := s.tokens.ValidateRefreshToken(ctx, body.RefreshToken)
	if err != nil {
		if errors.Is(err, platformauth.ErrTokenStoreFailure) {
			return RefreshVO{}, bizerrors.WrapInternalContext(ctx, err, "validate refresh token failed")
		}
		return RefreshVO{}, bizerrors.Unauthorized("refresh token is invalid")
	}

	identity, err := s.findByID(ctx, record.UserID)
	if err != nil {
		return RefreshVO{}, err
	}
	if identity == nil || !identity.IsEnable {
		return RefreshVO{}, bizerrors.Unauthorized("refresh token is invalid")
	}
	if record.SessionVersion != identity.SessionVersion {
		return RefreshVO{}, bizerrors.Unauthorized("refresh token is invalid")
	}

	tokenPair, err := s.tokens.RotateRefreshToken(ctx, body.RefreshToken, *record, platformauth.Principal{
		UserID:         identity.UserID,
		SessionID:      record.SessionID,
		SessionVersion: identity.SessionVersion,
		Roles:          append([]string(nil), identity.RoleCodes...),
	})
	if err != nil {
		if errors.Is(err, platformauth.ErrTokenStoreFailure) {
			return RefreshVO{}, bizerrors.WrapInternalContext(ctx, err, "rotate refresh token failed")
		}
		return RefreshVO{}, bizerrors.Unauthorized("refresh token is invalid")
	}

	return RefreshVO{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
	}, nil
}

// Me 查询当前登录用户信息。
func (s *Service) Me(ctx context.Context, userID idgen.ID) (UserIdentityVO, error) {
	identity, err := s.findByID(ctx, userID)
	if err != nil {
		return UserIdentityVO{}, err
	}
	if identity == nil {
		return UserIdentityVO{}, bizerrors.UserNotFound(userID)
	}
	return toUserIdentityVO(identity), nil
}

// ---- Ent 数据库操作 ----

func (s *Service) findByUsername(ctx context.Context, username string) (*Identity, error) {
	record, err := s.client.SysUser.Query().
		Where(
			sysuser.Username(strings.TrimSpace(username)),
			sysuser.DeletedAtIsNil(),
		).
		Only(ctx)
	if appent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query identity failed")
	}
	return s.toIdentity(ctx, record)
}

func (s *Service) findByID(ctx context.Context, id idgen.ID) (*Identity, error) {
	record, err := s.client.SysUser.Query().
		Where(
			sysuser.ID(id),
			sysuser.DeletedAtIsNil(),
		).
		Only(ctx)
	if appent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query identity failed")
	}
	return s.toIdentity(ctx, record)
}

func (s *Service) toIdentity(ctx context.Context, record *appent.SysUser) (*Identity, error) {
	roleCodes, err := s.loadRoleCodes(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	return &Identity{
		UserID:         record.ID,
		Username:       record.Username,
		Nickname:       stringValue(record.Nickname),
		PasswordHash:   record.PasswordHash,
		SessionVersion: record.SessionVersion,
		RoleCodes:      roleCodes,
		IsSuperAdmin:   record.IsSuperAdmin,
		IsEnable:       record.IsEnable,
	}, nil
}

func (s *Service) auditLoginRejected(ctx context.Context, account string, sourceIP string, reason string) {
	s.logger.Warn(ctx, "login rejected",
		logx.String("event", "auth.login.rejected"),
		logx.String("account", account),
		logx.String("source_ip", sourceIP),
		logx.String("reason", reason),
	)
}

func loginGuardReason(err error) string {
	switch {
	case errors.Is(err, platformauth.ErrLoginLocked):
		return "account_locked"
	case errors.Is(err, platformauth.ErrLoginRateLimited):
		return "rate_limited"
	default:
		return "rejected"
	}
}

func (s *Service) loadRoleCodes(ctx context.Context, userID idgen.ID) ([]string, error) {
	var rows []struct {
		Code string `json:"code"`
	}
	err := s.client.SysRole.Query().
		Where(
			sysrole.DeletedAtIsNil(),
			sysrole.IsEnable(true),
			sysrole.HasUserRolesWith(
				sysuserrole.UserID(userID),
				sysuserrole.DeletedAtIsNil(),
			),
		).
		Order(appent.Asc(sysrole.FieldSortID), appent.Asc(sysrole.FieldID)).
		Select(sysrole.FieldCode).
		Scan(ctx, &rows)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query identity roles failed")
	}

	roleCodes := make([]string, 0, len(rows))
	for _, row := range rows {
		roleCodes = append(roleCodes, row.Code)
	}
	return roleCodes, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func toUserIdentityVO(identity *Identity) UserIdentityVO {
	return UserIdentityVO{
		UserID:       identity.UserID,
		Username:     identity.Username,
		Nickname:     identity.Nickname,
		RoleCodes:    append([]string(nil), identity.RoleCodes...),
		IsSuperAdmin: identity.IsSuperAdmin,
		IsEnable:     identity.IsEnable,
	}
}
