// browserseed 仅在隔离 CI 数据库创建普通浏览器测试账号，不生成或修改用户配置。
package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if err := seed(); err != nil {
		fmt.Fprintln(os.Stderr, "browser seed:", err)
		os.Exit(1)
	}
}

// validateDestination 拒绝非 CI、非回环或非专用数据库，避免测试账号进入用户数据库。
func validateDestination(ci, dsn, password string) error {
	u, err := url.Parse(dsn)
	if ci != "true" || err != nil || u == nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Path != "/glean_browser_ci" || u.Query().Get("sslmode") != "disable" {
		return fmt.Errorf("requires CI=true and the dedicated loopback glean_browser_ci database")
	}
	// lib/pq also accepts host/dbname as URL query options; do not let them override
	// the loopback destination validated above.
	if len(u.Query()) != 1 || len(u.Query()["sslmode"]) != 1 || u.Fragment != "" {
		return fmt.Errorf("test database URL may only contain a single sslmode option")
	}
	if len(password) < 24 || len(password) > 72 {
		return fmt.Errorf("requires a generated CI-only password between 24 and 72 bytes")
	}
	return nil
}

func seed() error {
	dsn, password := os.Getenv("GLEAN_E2E_DATABASE_URL"), os.Getenv("GLEAN_E2E_PASSWORD")
	if err := validateDestination(os.Getenv("CI"), dsn, password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("password hashing failed")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("database open failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("database transaction failed")
	}
	defer tx.Rollback()
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_user`).Scan(&existing); err != nil || existing != 0 {
		return fmt.Errorf("refusing to seed an uninitialized or nonempty account database")
	}
	for i, username := range []string{"glean-browser", "glean-browser-other"} {
		_, err := tx.ExecContext(ctx, `INSERT INTO sys_user
			(id, username, password_hash, nickname, is_super_admin, is_enable, session_version, sort_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, false, true, 1, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			7+i, username, string(hash), "浏览器验收用户")
		if err != nil {
			return fmt.Errorf("test account insert failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("test account transaction commit failed")
	}
	fmt.Println("Created two ordinary CI-only accounts; baseline operator ID is 7.")
	return nil
}
