package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const maxRetries = 3

const retryBackoff = 10 * time.Millisecond

func InTx(ctx context.Context, db *sql.DB, cb func(*sql.Tx) error) (err error) {
	return InTxWithIsolation(ctx, db, sql.LevelSerializable, cb)
}

func InTxWithIsolation(ctx context.Context, db *sql.DB, isolation sql.IsolationLevel, cb func(*sql.Tx) error) (err error) {
	for i := range maxRetries {
		err = inTx(ctx, db, isolation, cb)
		if !isSerializationError(err) {
			return err
		}

		if i == maxRetries-1 {
			return err
		}

		backoff := retryBackoff * time.Duration(1<<i)
		jitter := time.Duration(rand.Int64N(int64(backoff)))
		timer := time.NewTimer(backoff + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	return err
}

func inTx(ctx context.Context, db *sql.DB, isolation sql.IsolationLevel, cb func(*sql.Tx) error) (err error) {
	tx, txErr := db.BeginTx(ctx, &sql.TxOptions{Isolation: isolation})
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

func isSerializationError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
}
