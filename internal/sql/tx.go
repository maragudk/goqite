package sql

import (
	"context"
	"database/sql"
	"fmt"
)

// InTx runs the callback in a transaction started with the given options, committing it if the callback returns
// a nil error, and rolling it back otherwise.
// The options are passed on to [database/sql.DB.BeginTx], so nil leaves the isolation level to the connection.
func InTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, cb func(*sql.Tx) error) (err error) {
	tx, txErr := db.BeginTx(ctx, opts)
	if txErr != nil {
		return fmt.Errorf("cannot start tx: %w", txErr)
	}

	defer func() {
		if rec := recover(); rec != nil {
			err = rollback(tx, nil)
			panic(rec)
		}
	}()

	if err := cb(tx); err != nil {
		return rollback(tx, err)
	}

	if txErr := tx.Commit(); txErr != nil {
		return fmt.Errorf("cannot commit tx: %w", txErr)
	}

	return nil
}

func rollback(tx *sql.Tx, err error) error {
	if txErr := tx.Rollback(); txErr != nil {
		return fmt.Errorf("cannot roll back tx after error (tx error: %v), original error: %w", txErr, err)
	}
	return err
}
