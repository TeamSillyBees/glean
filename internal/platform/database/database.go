package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/samber/do"
)

const defaultPingTimeout = 5 * time.Second

// Config 描述标准 SQL 数据库连接池配置。
type Config struct {
	DriverName      string
	DataSourceName  string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration
	PingTimeout     time.Duration
}

// Validate 校验数据库连接池配置。
func (c Config) Validate() error {
	switch {
	case strings.TrimSpace(c.DriverName) == "":
		return fmt.Errorf("database driver name 不能为空")
	case strings.TrimSpace(c.DataSourceName) == "":
		return fmt.Errorf("database data source name 不能为空")
	case c.MaxOpenConns < 0:
		return fmt.Errorf("database max open conns 不能小于 0")
	case c.MaxIdleConns < 0:
		return fmt.Errorf("database max idle conns 不能小于 0")
	case c.MaxOpenConns > 0 && c.MaxIdleConns > c.MaxOpenConns:
		return fmt.Errorf("database max idle conns 不能大于 max open conns")
	case c.ConnMaxIdleTime < 0:
		return fmt.Errorf("database conn max idle time 不能小于 0")
	case c.ConnMaxLifetime < 0:
		return fmt.Errorf("database conn max lifetime 不能小于 0")
	case c.PingTimeout < 0:
		return fmt.Errorf("database ping timeout 不能小于 0")
	}
	return nil
}

// Open 创建 SQL 连接池并执行 Ping 检查。
func Open(ctx context.Context, cfg Config) (*sql.DB, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	db, err := sql.Open(cfg.DriverName, cfg.DataSourceName)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.pingTimeout())
	defer cancel()
	if err := Ping(pingCtx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (c Config) pingTimeout() time.Duration {
	if c.PingTimeout <= 0 {
		return defaultPingTimeout
	}
	return c.PingTimeout
}

// Ping 执行数据库 Ping 健康检查。
func Ping(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database client 不能为空")
	}
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}
	return nil
}

// Register 将 SQL 连接池注册到 DI 容器，并在解析 provider 时执行 Ping 检查。
func Register(injector *do.Injector, cfg Config) {
	do.Provide(injector, func(i *do.Injector) (*sql.DB, error) {
		return Open(context.Background(), cfg)
	})
}
