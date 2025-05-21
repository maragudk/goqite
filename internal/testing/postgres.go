package testing

import (
	"context"
	"database/sql"
	_ "embed"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	internalsql "maragu.dev/goqite/internal/sql"
)

//go:embed schema_postgres.sql
var postgresSchema string

var once sync.Once

// NewPostgreSQLDB for testing.
func NewPostgreSQLDB(t *testing.T) *sql.DB {
	t.Helper()

	once.Do(func() {
		migrateTemplate1(t)
	})

	adminDB, adminClose := connect(t, "postgres")

	name := createName(t)
	if _, err := adminDB.ExecContext(t.Context(), `create database `+name); err != nil {
		t.Fatal(err)
	}
	db, close := connect(t, name)

	t.Cleanup(func() {
		close(t)
		if _, err := adminDB.ExecContext(context.WithoutCancel(t.Context()), `drop database if exists `+name); err != nil {
			t.Fatal(err)
		}
		adminClose(t)
	})

	return db
}

func migrateTemplate1(t *testing.T) {
	t.Helper()

	db, close := connect(t, "template1")
	defer close(t)

	for err := db.PingContext(t.Context()); err != nil; {
		time.Sleep(100 * time.Millisecond)
	}

	err := internalsql.InTx(t.Context(), db, func(tx *sql.Tx) error {
		var exists bool
		query := `select exists (select from information_schema.tables where table_name = 'goqite')`
		if err := tx.QueryRowContext(t.Context(), query).Scan(&exists); err != nil {
			return err
		}

		if exists {
			return nil
		}

		if _, err := tx.ExecContext(t.Context(), postgresSchema); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func connect(t *testing.T, name string) (*sql.DB, func(t *testing.T)) {
	t.Helper()

	db, err := sql.Open("pgx", "postgres://test:test@localhost:5433/"+name)
	if err != nil {
		t.Fatal(err)
	}

	return db, func(t *testing.T) {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func createName(t *testing.T) string {
	t.Helper()

	const letters = "abcdefghijklmnopqrstuvwxyz"
	var b strings.Builder
	for range 16 {
		i := rand.IntN(len(letters))
		b.WriteByte(letters[i])
	}

	return b.String()
}
