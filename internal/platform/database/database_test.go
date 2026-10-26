package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/do"
	"github.com/stretchr/testify/require"
)

var testDriverID atomic.Int64

func TestOpenPingsDatabase(t *testing.T) {
	name := registerTestDriver(nil)

	db, err := Open(context.Background(), Config{
		DriverName:     name,
		DataSourceName: "test",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
}

func TestConfigValidateRejectsInvalidPoolLimits(t *testing.T) {
	base := Config{DriverName: "postgres", DataSourceName: "postgres://example"}
	tests := []struct {
		name    string
		mutate  func(*Config)
		message string
	}{
		{
			name: "idle exceeds open",
			mutate: func(cfg *Config) {
				cfg.MaxOpenConns = 5
				cfg.MaxIdleConns = 6
			},
			message: "max idle conns 不能大于 max open conns",
		},
		{
			name: "negative idle lifetime",
			mutate: func(cfg *Config) {
				cfg.ConnMaxIdleTime = -time.Second
			},
			message: "conn max idle time 不能小于 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.mutate(&cfg)

			require.ErrorContains(t, cfg.Validate(), tt.message)
		})
	}
}

func TestOpenReturnsPingError(t *testing.T) {
	name := registerTestDriver(errors.New("database unavailable"))

	db, err := Open(context.Background(), Config{
		DriverName:     name,
		DataSourceName: "test",
	})

	require.Nil(t, db)
	require.ErrorContains(t, err, "database ping failed")
}

func TestOpenAppliesPingTimeout(t *testing.T) {
	name := registerBlockingPingDriver()
	started := time.Now()

	db, err := Open(context.Background(), Config{
		DriverName:     name,
		DataSourceName: "test",
		PingTimeout:    10 * time.Millisecond,
	})

	require.Nil(t, db)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), time.Second)
}

func TestRegisterProvidesPingingDatabase(t *testing.T) {
	name := registerTestDriver(nil)
	injector := do.New()

	Register(injector, Config{
		DriverName:     name,
		DataSourceName: "test",
	})

	db := do.MustInvoke[*sql.DB](injector)
	require.NotNil(t, db)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
}

func registerTestDriver(pingErr error) string {
	name := fmt.Sprintf("initra_database_test_%d", testDriverID.Add(1))
	sql.Register(name, &pingDriver{pingErr: pingErr})
	return name
}

func registerBlockingPingDriver() string {
	name := fmt.Sprintf("initra_database_test_%d", testDriverID.Add(1))
	sql.Register(name, &pingDriver{waitForContext: true})
	return name
}

type pingDriver struct {
	pingErr        error
	waitForContext bool
}

func (d *pingDriver) Open(_ string) (driver.Conn, error) {
	return &pingConn{pingErr: d.pingErr, waitForContext: d.waitForContext}, nil
}

type pingConn struct {
	pingErr        error
	waitForContext bool
}

func (c *pingConn) Ping(ctx context.Context) error {
	if c.waitForContext {
		<-ctx.Done()
		return ctx.Err()
	}
	return c.pingErr
}

func (c *pingConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("prepare is unused")
}

func (c *pingConn) Close() error {
	return nil
}

func (c *pingConn) Begin() (driver.Tx, error) {
	return nil, errors.New("begin is unused")
}
