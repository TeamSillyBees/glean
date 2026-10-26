package catalog

import (
	"context"
	"encoding/json"

	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaper"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaperstate"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpreference"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// Preferences 仅返回当前身份保存的筛选配置。
func (s *Service) Preferences(ctx context.Context) (PreferenceVO, error) {
	uid, err := userID(ctx)
	if err != nil {
		return PreferenceVO{}, err
	}
	p, err := s.client.GleanPreference.Query().Where(gleanpreference.UserID(uid)).Only(ctx)
	if ent.IsNotFound(err) {
		return PreferenceVO{Filters: map[string]any{}}, nil
	}
	if err != nil {
		return PreferenceVO{}, bizerrors.WrapDBContext(ctx, err, "load preferences failed")
	}
	return PreferenceVO{Filters: p.Filters}, nil
}

// SavePreferences 持久保存当前用户的筛选状态，不创建或修改科学分类。
func (s *Service) SavePreferences(ctx context.Context, input PreferenceVO) (PreferenceVO, error) {
	uid, err := userID(ctx)
	if err != nil {
		return PreferenceVO{}, err
	}
	if input.Filters == nil {
		input.Filters = map[string]any{}
	}
	encoded, err := json.Marshal(input.Filters)
	if err != nil || len(encoded) > 32*1024 {
		return PreferenceVO{}, bizerrors.BadRequest("preferences must be JSON smaller than 32 KiB")
	}
	err = data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
		// 父用户行锁同时保证软删除检查与该用户偏好 upsert 的并发一致性。
		if _, err := c.SysUser.Query().Where(sysuser.ID(uid), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true)).ForUpdate().Only(ctx); err != nil {
			return err
		}
		record, err := c.GleanPreference.Query().Where(gleanpreference.UserID(uid)).Only(ctx)
		if ent.IsNotFound(err) {
			_, err = c.GleanPreference.Create().SetUserID(uid).SetFilters(input.Filters).Save(ctx)
			return err
		}
		if err != nil {
			return err
		}
		return c.GleanPreference.UpdateOneID(record.ID).SetFilters(input.Filters).Exec(ctx)
	})
	if err != nil {
		return PreferenceVO{}, bizerrors.WrapDBContext(ctx, err, "save preferences failed")
	}
	return input, nil
}

// SavePaperState 仅更新个人阅读和隐藏状态，历史来源和主题成员关系保持不变。
func (s *Service) SavePaperState(ctx context.Context, paperID idgen.ID, input PaperStateVO) (PaperStateVO, error) {
	uid, err := userID(ctx)
	if err != nil {
		return PaperStateVO{}, err
	}
	if input.ReadStatus != "unread" && input.ReadStatus != "reading" && input.ReadStatus != "read" {
		return PaperStateVO{}, bizerrors.BadRequest("invalid personal readStatus")
	}
	err = data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
		if _, err := c.SysUser.Query().Where(sysuser.ID(uid), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true)).ForUpdate().Only(ctx); err != nil {
			return err
		}
		if _, err := c.GleanPaper.Query().Where(gleanpaper.ID(paperID)).ForShare().Only(ctx); err != nil {
			return err
		}
		record, err := c.GleanPaperState.Query().Where(gleanpaperstate.UserID(uid), gleanpaperstate.PaperID(paperID)).Only(ctx)
		if ent.IsNotFound(err) {
			_, err = c.GleanPaperState.Create().SetUserID(uid).SetPaperID(paperID).SetHidden(input.Hidden).SetReadStatus(input.ReadStatus).Save(ctx)
			return err
		}
		if err != nil {
			return err
		}
		return c.GleanPaperState.UpdateOneID(record.ID).SetHidden(input.Hidden).SetReadStatus(input.ReadStatus).Exec(ctx)
	})
	if ent.IsNotFound(err) {
		return PaperStateVO{}, bizerrors.WrapNotFoundContext(ctx, err, "paper or active user not found")
	}
	if err != nil {
		return PaperStateVO{}, bizerrors.WrapDBContext(ctx, err, "save personal paper state failed")
	}
	return input, nil
}
