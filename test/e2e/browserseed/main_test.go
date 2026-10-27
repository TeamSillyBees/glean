package main

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestDestinationRejectsNonIsolatedDatabase(t *testing.T) {
	for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(key, "")
	}
	password := strings.Repeat("x", 32)
	valid := "postgres://postgres:test@127.0.0.1:5432/glean_browser_ci?sslmode=disable"
	if err := validateDestination("true", valid, password); err != nil {
		t.Fatal(err)
	}
	for _, dsn := range []string{
		"", "postgres://postgres:test@example.org/glean_browser_ci?sslmode=disable",
		"postgres://postgres:test@127.0.0.1/glean?sslmode=disable",
		"postgres://postgres:test@127.0.0.1/glean_browser_ci",
		"mysql://postgres:test@127.0.0.1/glean_browser_ci?sslmode=disable",
		valid + "&host=example.org",
		valid + "&dbname=glean",
		valid + "&sslmode=disable",
		valid + "&host=%ZZ",
		valid + "&host=example.org;dbname=glean",
	} {
		if err := validateDestination("true", dsn, password); err == nil {
			t.Fatal("unsafe test destination was accepted")
		}
	}
	if validateDestination("false", valid, password) == nil || validateDestination("true", valid, "short") == nil {
		t.Fatal("non-CI or weak test password was accepted")
	}
}

func TestDestinationRejectsEnvironmentIndirection(t *testing.T) {
	keys := []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "unexpected-destination")
			err := validateDestination("true", "postgres://postgres:test@127.0.0.1:5432/glean_browser_ci?sslmode=disable", strings.Repeat("x", 32))
			if err == nil {
				t.Fatal("environment-indirected destination was accepted")
			}
		})
	}
}

// TestDefaultRoleCreatedBeforeOrdinaryAccounts 固定普通账号创建所需的 viewer 初始化。
func TestDefaultRoleCreatedBeforeOrdinaryAccounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	mock.ExpectExec(`INSERT INTO sys_role .*VALUES \(1000000000102, 'viewer', .* true, true, 10, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP\)`).WillReturnResult(sqlmock.NewResult(1000000000102, 1))
	require.NoError(t, seedDefaultRole(context.Background(), tx))
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
