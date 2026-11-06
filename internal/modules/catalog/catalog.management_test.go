package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	sql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/server"
)

func managerContext() context.Context {
	return platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: 7, IsSuperAdmin: true})
}

// TestManagementRejectsNonManagersBeforeDatabase 防止隐藏管理按钮被直接 API 绕过。
func TestManagementRejectsNonManagersBeforeDatabase(t *testing.T) {
	s := NewService(nil)
	for _, tc := range []struct {
		ctx  context.Context
		want int
	}{{context.Background(), 401}, {platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: 7}), 403}} {
		_, err := s.SaveReview(tc.ctx, 1, "biomed-ai", ReviewBody{Status: "included"})
		require.Equal(t, tc.want, bizerrors.StatusOf(err))
		_, err = s.SaveCorrection(tc.ctx, 1, CorrectionBody{})
		require.Equal(t, tc.want, bizerrors.StatusOf(err))
		_, err = s.SaveTopic(tc.ctx, "", TopicBody{Name: "Topic"}, true)
		require.Equal(t, tc.want, bizerrors.StatusOf(err))
	}
}

// TestManagementRoutesFailClosed 固定新端点的显式权限与匿名拒绝契约。
func TestManagementRoutesFailClosed(t *testing.T) {
	app, err := server.NewApp(server.Options{Title: "management-test", Version: "test", Env: "test"}, nil, nil, nil, nil)
	require.NoError(t, err)
	NewModule(NewHandler(nil)).Register(app.API, app.Registry)
	for _, r := range []struct{ method, path string }{{"PUT", "/api/v1/catalog/papers/{id}/topics/{slug}/review"}, {"PUT", "/api/v1/catalog/papers/{id}/correction"}, {"POST", "/api/v1/catalog/topics"}, {"PUT", "/api/v1/catalog/topics/{slug}"}} {
		policy, ok := app.Registry.Lookup(r.method, r.path)
		require.True(t, ok)
		require.Equal(t, platformauth.AccessModePermission, policy.AccessMode)
		require.Equal(t, "catalog:manage", policy.Permission)
		path := strings.NewReplacer("{id}", "1", "{slug}", "biomed-ai").Replace(r.path)
		rec := httptest.NewRecorder()
		app.Engine.ServeHTTP(rec, httptest.NewRequest(r.method, path, strings.NewReader(`{}`)))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
}

// TestEffectiveMetadataSQLBindsUserInput 验证覆盖层表达式仍使用 PostgreSQL 参数而不是问号或拼接输入。
func TestEffectiveMetadataSQLBindsUserInput(t *testing.T) {
	attack := `100%_' OR TRUE --`
	q, args := sql.Dialect(dialect.Postgres).Select("id").From(sql.Table("glean_paper")).Where(effectiveContains(effectiveColumn(`"glean_paper"`, "title"), attack)).Query()
	require.Contains(t, q, "COALESCE")
	require.Contains(t, q, "$1")
	require.NotContains(t, q, "?")
	require.NotContains(t, q, attack)
	require.Equal(t, []any{`%100\%\_' or true --%`}, args)
	for _, scope := range []string{"curated", "pending", "excluded"} {
		q, args := membershipSelector(PaperQuery{Topic: "biomed-ai", CollectionScope: scope}).Query()
		require.Contains(t, q, "manual_review")
		require.Contains(t, q, "paper_id")
		require.NotEmpty(t, args)
	}
}

// TestReviewVersionConflictRollsBack 验证旧版本不能覆盖别人的决定。
func TestReviewVersionConflictRollsBack(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "sys_user" .* FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery(`SELECT .* FROM "glean_paper" .* FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(101))
	mock.ExpectQuery(`SELECT .* FROM "glean_membership"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(201))
	mock.ExpectQuery(`SELECT .* FROM "glean_paper_review"`).WillReturnRows(sqlmock.NewRows([]string{"id", "version", "status"}).AddRow(301, 2, "pending"))
	mock.ExpectRollback()
	_, err := service.SaveReview(managerContext(), 101, "biomed-ai", ReviewBody{Status: "pending", ExpectedVersion: 1})
	require.Equal(t, 409, bizerrors.StatusOf(err))
}

// TestReviewAuditFailureRollsBack 验证审核与历史一起提交，不能出现无审计的共享决定。
func TestReviewAuditFailureRollsBack(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "sys_user" .* FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery(`SELECT .* FROM "glean_paper" .* FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(101))
	mock.ExpectQuery(`SELECT .* FROM "glean_membership"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(201))
	mock.ExpectQuery(`SELECT .* FROM "glean_paper_review"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`INSERT INTO "glean_paper_review"`).WillReturnResult(sqlmock.NewResult(301, 1))
	mock.ExpectExec(`INSERT INTO "glean_catalog_change"`).WillReturnError(errors.New("audit unavailable"))
	mock.ExpectRollback()
	_, err := service.SaveReview(managerContext(), 101, "biomed-ai", ReviewBody{Status: "pending"})
	require.ErrorContains(t, err, "audit unavailable")
}

// TestManagementInputBounds 验证简单表单不能写入错误状态、身份字段或无限数据。
func TestManagementInputBounds(t *testing.T) {
	empty := ""
	year := -1
	require.Error(t, validateCorrection(CorrectionBody{Fields: CorrectionFields{Title: &empty}}))
	require.Error(t, validateCorrection(CorrectionBody{Fields: CorrectionFields{Year: &year}}))
	require.Error(t, normalizeTopic(&TopicBody{Slug: "unsafe / slug", Name: "Topic"}))
	require.Error(t, normalizeTopic(&TopicBody{Slug: "custom", Name: "Topic", SearchEnabled: true}))
	builtin := TopicBody{Slug: "biomed-ai", Name: "Biomed", SearchEnabled: true}
	require.NoError(t, normalizeTopic(&builtin))
	require.Empty(t, builtin.TermGroups)
	longName := TopicBody{Name: strings.Repeat("a", 100)}
	require.NoError(t, normalizeTopic(&longName))
	require.Len(t, longName.Slug, 80)
	custom := TopicBody{Slug: "custom", Name: "Topic", SearchEnabled: true, TermGroups: [][]string{{"  keyword ", "keyword"}}}
	require.NoError(t, normalizeTopic(&custom))
	require.Equal(t, [][]string{{"keyword"}}, custom.TermGroups)
}
