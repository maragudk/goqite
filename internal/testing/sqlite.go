package testing

import (
	"database/sql"
	_ "embed"
	"os"
	"path/filepath"
	"sync"
	"testing"

	internalsql "maragu.dev/goqite/internal/sql"
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

	err = internalsql.InTx(t.Context(), db, func(tx *sql.Tx) error {
		var exists bool
		query := `select exists (select 1 from sqlite_master where type = 'table' and name = 'goqite')`
		if err := tx.QueryRowContext(t.Context(), query).Scan(&exists); err != nil {
			return err
		}

		if exists {
			return nil
		}

		if _, err := tx.ExecContext(t.Context(), sqliteSchema); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
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
