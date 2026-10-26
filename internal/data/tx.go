package data

import (
	"context"
	"fmt"

	"github.com/teamsillybees/glean/internal/data/ent"
)

// WithinTx 在 Ent 事务中执行 fn，并避免向 service 层暴露 Ent Tx 对象。
func WithinTx(ctx context.Context, client *ent.Client, fn func(context.Context, *ent.Client) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	if err := fn(ctx, tx.Client()); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			return fmt.Errorf("%w: rollback failed: %v", err, rerr)
		}
		return err
	}

	return tx.Commit()
}
