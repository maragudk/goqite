package internal

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/maragudk/goqite"
)

//go:embed schema.sql
var schema string

func NewDB(t testing.TB, path string) *sql.DB {
	t.Helper()

	// Check if file exists already
	exists := false
	if _, err := os.Stat(path); err == nil {
		exists = true
	}

	if path != ":memory:" && !exists {
		t.Cleanup(func() {
			for _, p := range []string{path, path + "-shm", path + "-wal"} {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			}
		})
	}

	db, err := sql.Open("sqlite3", path+"?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if !exists {
		_, err = db.Exec(schema)
		if err != nil {
			t.Fatal(err)
		}
	}

	return db
}

func NewQ(t testing.TB, opts goqite.NewOpts, path string) *goqite.Queue {
	t.Helper()

	opts.DB = NewDB(t, path)

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
