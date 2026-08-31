package goqite

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"maragu.dev/is"
)

func TestQueue_inTx(t *testing.T) {
	t.Run("runs postgresql transactions at read committed, whatever the connection defaults to", func(t *testing.T) {
		// Connects directly rather than through the test helpers, because those import this package.
		db, err := sql.Open("pgx", "postgres://test:test@localhost:5433/postgres")
		is.NotError(t, err)
		t.Cleanup(func() {
			is.NotError(t, db.Close())
		})

		// Pin the pool to one connection, so the session setting below is the one inTx gets.
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)

		// Raise the connection's default, the way a hardened server would. Transactions that ask for no
		// particular isolation level inherit this, which is the regression this test exists to catch.
		_, err = db.ExecContext(t.Context(), `set session default_transaction_isolation = 'serializable'`)
		is.NotError(t, err)

		q := New(NewOpts{DB: db, Name: "test", SQLFlavor: SQLFlavorPostgreSQL})

		var isolation, sessionDefault string
		err = q.inTx(t.Context(), func(tx *sql.Tx) error {
			if err := tx.QueryRowContext(t.Context(), `show transaction_isolation`).Scan(&isolation); err != nil {
				return err
			}
			return tx.QueryRowContext(t.Context(), `show default_transaction_isolation`).Scan(&sessionDefault)
		})
		is.NotError(t, err)

		// The raised default reached this transaction, so read committed below is the pin overriding it,
		// not the session setting having been lost along the way.
		is.Equal(t, "serializable", sessionDefault)
		is.Equal(t, "read committed", isolation)
	})
}
