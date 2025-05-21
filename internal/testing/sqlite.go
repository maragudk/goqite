package testing

import (
	"database/sql"
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

//go:embed schema_sqlite.sql
var sqliteSchema string

func NewSQLiteDB(t testing.TB) *sql.DB {
	t.Helper()

	t.Cleanup(func() {
		cleanupSQLite(t)
	})

	db, err := sql.Open("sqlite3", "test.db?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(t.Context(), `select * from goqite`); err != nil && !errors.Is(err, sql.ErrNoRows) {
		if _, err = db.Exec(sqliteSchema); err != nil {
			t.Fatal(err)
		}
	}

	return db
}

var mutex sync.Mutex

func cleanupSQLite(t testing.TB) {
	t.Helper()

	mutex.Lock()
	defer mutex.Unlock()

	files, err := filepath.Glob("test.db*")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
}
