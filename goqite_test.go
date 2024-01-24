package goqite_test

import (
	"context"
	"database/sql"
	_ "embed"
	"os"
	"testing"
	"time"

	"github.com/maragudk/is"
	_ "github.com/mattn/go-sqlite3"

	"github.com/maragudk/goqite"
)

//go:embed schema.sql
var schema string

func TestQueue(t *testing.T) {
	t.Run("can send and receive and delete a message", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{Timeout: time.Millisecond}, ":memory:")

		m, err := q.Receive(context.Background())
		is.NotError(t, err)
		is.Nil(t, m)

		m = &goqite.Message{
			Body: []byte("yo"),
		}

		err = q.Send(context.Background(), *m)
		is.NotError(t, err)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))

		err = q.Delete(context.Background(), m.ID)
		is.NotError(t, err)

		time.Sleep(time.Millisecond)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.Nil(t, m)
	})

	t.Run("does not receive a delayed message immediately", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{}, ":memory:")

		m := &goqite.Message{
			Body:  []byte("yo"),
			Delay: time.Millisecond,
		}

		err := q.Send(context.Background(), *m)
		is.NotError(t, err)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.Nil(t, m)

		time.Sleep(time.Millisecond)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))
	})

	t.Run("does not receive a message twice in a row", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{Timeout: time.Second}, ":memory:")

		m := &goqite.Message{
			Body: []byte("yo"),
		}

		err := q.Send(context.Background(), *m)
		is.NotError(t, err)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.Nil(t, m)
	})

	t.Run("does receive a message up to two times if set and timeout has passed", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{Timeout: time.Millisecond, MaxReceive: 2}, ":memory:")

		m := &goqite.Message{
			Body: []byte("yo"),
		}

		err := q.Send(context.Background(), *m)
		is.NotError(t, err)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))

		time.Sleep(time.Millisecond)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))

		time.Sleep(time.Millisecond)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.Nil(t, m)
	})
}

func BenchmarkQueue(b *testing.B) {
	b.Run("send and receive messages", func(b *testing.B) {
		q := newQ(b, goqite.NewOpts{}, "bench.db")

		for i := 0; i < b.N; i++ {
			m := &goqite.Message{
				Body: []byte("yo"),
			}

			err := q.Send(context.Background(), *m)
			is.NotError(b, err)

			m, err = q.Receive(context.Background())
			is.NotError(b, err)
			is.NotNil(b, m)

			err = q.Delete(context.Background(), m.ID)
			is.NotError(b, err)
		}
	})
}

func newQ(t testing.TB, opts goqite.NewOpts, path string) *goqite.Queue {
	t.Helper()

	if path != ":memory:" {
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
	_, err = db.Exec(schema)
	if err != nil {
		t.Fatal(err)
	}

	opts.DB = db

	return goqite.New(opts)
}
