package testing

import (
	"database/sql"
	_ "embed"
	"fmt"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"

	"maragu.dev/goqite"
)

func Run(t *testing.T, name string, f func(t *testing.T, db *sql.DB)) {
	t.Run(name, func(t *testing.T) {
		t.Run("sqlite", func(t *testing.T) {
			db := NewSQLiteDB(t)
			f(t, db)
		})

		t.Run("postgresql", func(t *testing.T) {
			t.SkipNow()
			db := NewPostgreSQLDB(t)
			f(t, db)
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
