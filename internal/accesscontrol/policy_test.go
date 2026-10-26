package accesscontrol

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

// TestLoadPolicyRulesReadsEnabledDatabaseRelations 验证 Casbin 策略确实来自三张 RBAC 业务表。
func TestLoadPolicyRulesReadsEnabledDatabaseRelations(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	generator, err := idgen.NewGenerator(1)
	require.NoError(t, err)
	control, err := New(data.NewEntClientFromDB(db, generator), nil, logx.NewNop(), Options{
		AppName: "initra", Env: "test", InstanceID: "instance-1", CacheTTL: time.Minute,
	})
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .* FROM "sys_role"`).WillReturnRows(sqlmock.NewRows([]string{
		"id", "code", "name", "remark", "is_builtin", "is_enable", "sort_id",
		"deleted_at", "created_at", "updated_at", "created_by", "updated_by",
	}).AddRow(int64(101), "auditor", "审计员", nil, false, true, 1, nil, now, now, nil, nil))
	mock.ExpectQuery(`SELECT .* FROM "sys_role_menu"`).WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{
		"id", "role_id", "menu_id", "deleted_at", "created_at", "updated_at", "created_by", "updated_by",
	}).AddRow(int64(201), int64(101), int64(301), nil, now, now, nil, nil))
	mock.ExpectQuery(`SELECT .* FROM "sys_menu"`).WithArgs(int64(301)).WillReturnRows(sqlmock.NewRows([]string{
		"id", "parent_id", "app_id", "title", "menu_type", "route_path", "component_path",
		"permission_code", "icon", "is_visible", "is_cached", "sort_id", "deleted_at",
		"created_at", "updated_at", "created_by", "updated_by",
	}).AddRow(int64(301), nil, nil, "审计查看", int16(1), nil, nil, "system:audit:read", nil, false, true, 1, nil, now, now, nil, nil))

	rules, err := control.LoadPolicyRules(context.Background())
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, "auditor", rules[0].RoleCode)
	require.Equal(t, "system:audit:read", rules[0].PermissionCode)
	require.NoError(t, mock.ExpectationsWereMet())
}
