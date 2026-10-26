package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

func catalogMock(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = db.Close() })
	g, err := idgen.NewGenerator(20)
	require.NoError(t, err)
	return NewService(data.NewEntClientFromDB(db, g)), mock
}

// TestImportReplayDoesNotOverwriteHistory 验证导入重放不写入元数据或新主题关系。
func TestImportReplayDoesNotOverwriteHistory(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "glean_membership" WHERE .*`).WithArgs("llm-agent", "legacy-42").WillReturnRows(sqlmock.NewRows([]string{"id", "paper_id"}).AddRow(201, 101))
	mock.ExpectCommit()
	result, err := service.Import(context.Background(), ImportRecord{Topic: "llm-agent", OriginalID: "legacy-42", Title: "different title must not overwrite"})
	require.NoError(t, err)
	require.Equal(t, idgen.ID(101), result.PaperID)
	require.Equal(t, idgen.ID(201), result.MembershipID)
	require.False(t, result.Created)
}

// TestImportSharesExistingIdentity 验证 DOI 归一化后复用父论文，成员原始标识仍独立。
func TestImportSharesExistingIdentity(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "glean_membership" WHERE .*`).WithArgs("biomed-ai", "bio-1").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "glean_paper" WHERE .* FOR UPDATE`).WithArgs("doi:10.1234/example", "10.1234/example").WillReturnRows(sqlmock.NewRows([]string{"id", "doi"}).AddRow(101, "10.1234/example"))
	mock.ExpectExec(`INSERT INTO "glean_membership" .*`).WillReturnResult(sqlmock.NewResult(202, 1))
	mock.ExpectCommit()
	result, err := service.Import(context.Background(), ImportRecord{Topic: "biomed-ai", OriginalID: "bio-1", Title: "Paper", DOI: "https://doi.org/10.1234/EXAMPLE", Raw: map[string]any{"original": "preserved"}})
	require.NoError(t, err)
	require.Equal(t, idgen.ID(101), result.PaperID)
	require.True(t, result.Created)
}

// TestImportConflictingIdentifiersRollsBack 验证冲突标识不得静默合并两篇历史论文。
func TestImportConflictingIdentifiersRollsBack(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "glean_membership" WHERE .*`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "glean_paper" WHERE .* FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "doi", "arxiv_id"}).AddRow(101, "10.1234/a", nil).AddRow(102, nil, "2401.12345"))
	mock.ExpectRollback()
	_, err := service.Import(context.Background(), ImportRecord{Topic: "llm-agent", OriginalID: "a", Title: "Paper", DOI: "10.1234/a", ArxivID: "2401.12345v2"})
	require.Error(t, err)
}

// TestImportParentFailureRollsBack 验证父记录写入失败时不留孤立成员关系。
func TestImportParentFailureRollsBack(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "glean_membership" WHERE .*`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "glean_paper" WHERE .* FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`INSERT INTO "glean_paper" .*`).WillReturnError(errors.New("database unavailable"))
	mock.ExpectRollback()
	_, err := service.Import(context.Background(), ImportRecord{Topic: "llm-agent", OriginalID: "new", Title: "Paper"})
	require.Error(t, err)
}

// TestPaperPaginationIsDeterministic 验证稳定排序、数量与分页参数独立。
func TestPaperPaginationIsDeterministic(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectQuery(`SELECT COUNT\("glean_paper"\."id"\).*`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT .* FROM "glean_paper" .* ORDER BY "glean_paper"\."year" DESC, "glean_paper"\."formal_online_date" DESC NULLS LAST, "glean_paper"\."id" DESC LIMIT 20 OFFSET 20`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	page, err := service.Papers(requestctx.WithUserID(context.Background(), "7"), PaperQuery{Topic: "biomed-ai", Page: 2, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, 50, page.Total)
	require.Equal(t, 2, page.Page)
	require.Empty(t, page.Items)
}

// TestPersonalStateLocksLiveParents 验证状态更新先锁定有效用户和论文，只写个人数据。
func TestPersonalStateLocksLiveParents(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "sys_user" .* FOR UPDATE`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery(`SELECT .* FROM "glean_paper" .* FOR SHARE`).WithArgs(int64(101)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(101))
	mock.ExpectQuery(`SELECT .* FROM "glean_paper_state" .*`).WithArgs(int64(7), int64(101)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`INSERT INTO "glean_paper_state" .*`).WillReturnResult(sqlmock.NewResult(301, 1))
	mock.ExpectCommit()
	result, err := service.SavePaperState(requestctx.WithUserID(context.Background(), "7"), 101, PaperStateVO{Hidden: true, ReadStatus: "reading"})
	require.NoError(t, err)
	require.True(t, result.Hidden)
	require.Equal(t, "reading", result.ReadStatus)
}

// TestImportDatesAndIdentityNormalization 验证没有出处的正式日期被拒绝，标题相同不构成去重证据。
func TestImportDatesAndIdentityNormalization(t *testing.T) {
	now := time.Now()
	r := ImportRecord{Topic: "biomed-ai", OriginalID: "1", Title: "Identical title", FormalOnlineDate: &now}
	require.Error(t, normalizeImport(&r))
	r.FormalOnlineDate = nil
	require.NoError(t, normalizeImport(&r))
	r2 := r
	r2.Topic = "llm-agent"
	require.NotEqual(t, identityKey(r), identityKey(r2))
	require.Equal(t, "10.1234/ab.c", normalizeDOI("https://doi.org/10.1234/AB.C"))
	require.Equal(t, "2401.12345", normalizeArxiv("https://arxiv.org/pdf/2401.12345v3.pdf"))
	require.Empty(t, normalizeDOI("not a DOI"))
}

// TestUserInputsRejectedBeforeDatabase 验证未认证、非法状态和巨大偏好不能进入数据库。
func TestUserInputsRejectedBeforeDatabase(t *testing.T) {
	service := NewService(nil)
	_, err := service.Papers(context.Background(), PaperQuery{})
	require.Error(t, err)
	ctx := requestctx.WithUserID(context.Background(), "7")
	_, err = service.SavePaperState(ctx, 101, PaperStateVO{ReadStatus: "scientifically-reviewed"})
	require.Error(t, err)
	_, err = service.SavePreferences(ctx, PreferenceVO{Filters: map[string]any{"notJSON": make(chan int)}})
	require.Error(t, err)
}
