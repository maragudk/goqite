package goqite_test

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"maragu.dev/goqite"
)

//go:embed schema_postgres.sql
var localReproPostgresSchema string

func TestLocalPostgreSQLBenchmarkStyleCanPrint40001(t *testing.T) {
	if os.Getenv("GOQITE_TEST_REPRODUCE_40001") == "" {
		t.Skip("set GOQITE_TEST_REPRODUCE_40001=1 and GOQITE_TEST_POSTGRES_DSN=postgres://user:pass@host:5432/dbname")
	}

	dsn := os.Getenv("GOQITE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set GOQITE_TEST_POSTGRES_DSN=postgres://user:pass@host:5432/dbname")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	})

	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(100)

	if _, err := db.ExecContext(t.Context(), localReproPostgresSchema); err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "42P07" && pgErr.Code != "42710" && pgErr.Code != "42723") {
			t.Fatal(err)
		}
	}

	name := fmt.Sprintf("repro_40001_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.WithoutCancel(t.Context()), `delete from goqite where queue = $1`, name); err != nil {
			t.Fatal(err)
		}
	})

	q := goqite.New(goqite.NewOpts{
		DB:         db,
		MaxReceive: 1000,
		Name:       name,
		SQLFlavor:  goqite.SQLFlavorPostgreSQL,
		Timeout:    time.Second,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	const workers = 50

	var seen atomic.Bool
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for ctx.Err() == nil {
				if err := q.Send(ctx, goqite.Message{Body: []byte("yo")}); err != nil {
					if isSerializationError(err) {
						t.Log("send:", err)
						seen.Store(true)
						continue
					}
					continue
				}

				m, err := q.Receive(ctx)
				if err != nil {
					if isSerializationError(err) {
						t.Log("receive:", err)
						seen.Store(true)
						continue
					}
					continue
				}

				if m == nil {
					continue
				}

				err = q.Delete(ctx, m.ID)
				if err != nil {
					if isSerializationError(err) {
						t.Log("delete:", err)
						seen.Store(true)
						continue
					}
				}
			}
		}()
	}

	wg.Wait()

	if seen.Load() {
		t.Fatal("reproduced 40001; expected this run to succeed without serialization errors")
	}
}

func isSerializationError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
}
