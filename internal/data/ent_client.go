package data

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/teamsillybees/glean/internal/data/ent"
	platformdatabase "github.com/teamsillybees/glean/internal/platform/database"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"

	_ "github.com/lib/pq"
)

// DatabaseConfig 描述 PostgreSQL 连接配置。
type DatabaseConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	DBName          string        `mapstructure:"dbname"`
	ApplicationName string        `mapstructure:"application_name"`
	SSLMode         string        `mapstructure:"ssl_mode"`
	SSLRootCert     string        `mapstructure:"ssl_root_cert"`
	ConnectTimeout  time.Duration `mapstructure:"connect_timeout"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	PingTimeout     time.Duration `mapstructure:"ping_timeout"`
}

// SQLDBConfig 将业务数据库配置转换为通用 SQL 连接池配置。
func SQLDBConfig(cfg DatabaseConfig) platformdatabase.Config {
	databasePath := "/" + cfg.DBName
	connectionURL := &url.URL{
		Scheme:  "postgres",
		User:    databaseUser(cfg.User, cfg.Password),
		Host:    net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:    databasePath,
		RawPath: "/" + url.PathEscape(cfg.DBName),
	}
	query := connectionURL.Query()
	query.Set("sslmode", strings.ToLower(strings.TrimSpace(cfg.SSLMode)))
	if applicationName := strings.TrimSpace(cfg.ApplicationName); applicationName != "" {
		query.Set("application_name", applicationName)
	}
	if rootCert := strings.TrimSpace(cfg.SSLRootCert); rootCert != "" {
		query.Set("sslrootcert", rootCert)
	}
	if cfg.ConnectTimeout > 0 {
		query.Set("connect_timeout", strconv.FormatInt(durationSecondsCeil(cfg.ConnectTimeout), 10))
	}
	connectionURL.RawQuery = query.Encode()
	return platformdatabase.Config{
		DriverName:      "postgres",
		DataSourceName:  connectionURL.String(),
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxIdleTime: cfg.ConnMaxIdleTime,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
		PingTimeout:     cfg.PingTimeout,
	}
}

func durationSecondsCeil(duration time.Duration) int64 {
	return int64((duration-1)/time.Second + 1)
}

func databaseUser(user string, password string) *url.Userinfo {
	if password == "" {
		return url.User(user)
	}
	return url.UserPassword(user, password)
}

// NewEntClientFromDB 基于已有 *sql.DB 创建 Ent Client 并注册 Hook，用于 sqlmock 等测试场景。
func NewEntClientFromDB(db *sql.DB, generator *idgen.Generator) *ent.Client {
	if generator != nil {
		idgen.SetDefaultGenerator(generator)
	}
	drv := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(drv))

	client.Use(entx.AuditHook(entx.AuditHookOptions{
		Operator: func(ctx context.Context) (idgen.ID, bool) {
			if id, ok := entx.OperatorIDFromContext(ctx); ok {
				return id, true
			}
			userID, ok := requestctx.UserIDFromContext(ctx)
			if !ok {
				return 0, false
			}
			id, err := idgen.Parse(userID)
			if err != nil {
				return 0, false
			}
			return id, true
		},
	}))
	client.Use(entx.RejectDeleteHook())

	return client
}
