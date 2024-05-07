package sql

import (
	"database/sql"
	"fmt"

	common "github.com/maragudk/goqite/internal/common"
)

func InTx(db *sql.DB, cb func(*sql.Tx) (common.Message, error)) (response common.Message, err error) {
	tx, txErr := db.Begin()
	if txErr != nil {
		return common.Message{}, fmt.Errorf("cannot start tx: %w", txErr)
	}

	defer func() {
		if rec := recover(); rec != nil {
			err = rollback(tx, nil)
			panic(rec)
		}
	}()

	response, err = cb(tx)
	if err != nil {
		return response, rollback(tx, err)
	}

	if txErr := tx.Commit(); txErr != nil {
		return common.Message{}, fmt.Errorf("cannot commit tx: %w", txErr)
	}

	return response, nil
}

func rollback(tx *sql.Tx, err error) error {
	if txErr := tx.Rollback(); txErr != nil {
		return fmt.Errorf("cannot roll back tx after error (tx error: %v), original error: %w", txErr, err)
	}
	return err
}
