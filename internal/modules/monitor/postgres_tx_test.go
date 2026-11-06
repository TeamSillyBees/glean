package monitor

import (
	"context"
	"database/sql"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"fmt"
)

// monitorTestTxDriver 将业务事务映射到保存点，测试结束时仍整体回滚隔离 schema。
type monitorTestTxDriver struct {
	*entsql.Driver
	tx       *sql.Tx
	sequence int
}

func (d *monitorTestTxDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	d.sequence++
	name := fmt.Sprintf("monitor_nested_%d", d.sequence)
	if _, err := d.tx.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return nil, err
	}
	return &monitorTestSavepoint{ExecQuerier: d.Driver, tx: d.tx, name: name}, nil
}
func (d *monitorTestTxDriver) Close() error { return nil }

type monitorTestSavepoint struct {
	dialect.ExecQuerier
	tx   *sql.Tx
	name string
}

func (s *monitorTestSavepoint) Commit() error {
	_, e := s.tx.Exec("RELEASE SAVEPOINT " + s.name)
	return e
}
func (s *monitorTestSavepoint) Rollback() error {
	if _, e := s.tx.Exec("ROLLBACK TO SAVEPOINT " + s.name); e != nil {
		return e
	}
	_, e := s.tx.Exec("RELEASE SAVEPOINT " + s.name)
	return e
}
