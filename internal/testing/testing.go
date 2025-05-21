package testing

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"

	"maragu.dev/goqite"
)

func Run(t *testing.T, name string, timeout time.Duration, f func(t *testing.T, db *sql.DB, q *goqite.Queue)) {
	t.Run(name, func(t *testing.T) {
		t.Run("sqlite", func(t *testing.T) {
			db := NewSQLiteDB(t)
			q := NewQ(t, goqite.NewOpts{DB: db, Timeout: timeout, SQLFlavor: goqite.SQLFlavorSQLite})
			f(t, db, q)
		})

		t.Run("postgresql", func(t *testing.T) {
			db := NewPostgreSQLDB(t)
			q := NewQ(t, goqite.NewOpts{DB: db, Timeout: timeout, SQLFlavor: goqite.SQLFlavorPostgreSQL})
			f(t, db, q)
		})
	})
}

func NewQ(t testing.TB, opts goqite.NewOpts) *goqite.Queue {
	t.Helper()

	if opts.Name == "" {
		opts.Name = "test"
	}

	return goqite.New(opts)
}

type Logger func(msg string, args ...any)

func (f Logger) Info(msg string, args ...any) {
	f(msg, args...)
}

func NewLogger(t *testing.T) Logger {
	t.Helper()

	return Logger(func(msg string, args ...any) {
		logArgs := []any{msg}
		for i := 0; i < len(args); i += 2 {
			logArgs = append(logArgs, fmt.Sprintf("%v=%v", args[i], args[i+1]))
		}
		t.Log(logArgs...)
	})
}
