package goqite_test

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"math/rand"
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
}

func TestQueue_New(t *testing.T) {
	t.Run("panics if db is nil", func(t *testing.T) {
		defer func() {
			r := recover()
			is.Equal(t, "db cannot be nil", r)
		}()

		goqite.New(goqite.NewOpts{Name: "test"})
	})

	t.Run("panics if name is empty", func(t *testing.T) {
		defer func() {
			r := recover()
			is.Equal(t, "name cannot be empty", r)
		}()

		goqite.New(goqite.NewOpts{DB: &sql.DB{}})
	})

	t.Run("panics if max receive is negative", func(t *testing.T) {
		defer func() {
			r := recover()
			is.Equal(t, "max receive cannot be negative", r)
		}()

		goqite.New(goqite.NewOpts{DB: &sql.DB{}, Name: "test", MaxReceive: -1})
	})

	t.Run("panics if timeout is negative", func(t *testing.T) {
		defer func() {
			r := recover()
			is.Equal(t, "timeout cannot be negative", r)
		}()

		goqite.New(goqite.NewOpts{DB: &sql.DB{}, Name: "test", Timeout: -1})
	})
}

func TestQueue_Send(t *testing.T) {
	t.Run("panics if delay is negative", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{}, ":memory:")

		var err error
		defer func() {
			is.NotError(t, err)
			r := recover()
			is.Equal(t, "delay cannot be negative", r)
		}()

		err = q.Send(context.Background(), goqite.Message{Delay: -1})
	})
}

func TestQueue_Receive(t *testing.T) {
	t.Run("does not receive a delayed message immediately", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{}, ":memory:")

		m := &goqite.Message{
			Body:  []byte("yo"),
			Delay: 2 * time.Millisecond,
		}

		err := q.Send(context.Background(), *m)
		is.NotError(t, err)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.Nil(t, m)

		time.Sleep(2 * time.Millisecond)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))
	})

	t.Run("does not receive a message twice in a row", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{}, ":memory:")

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

	t.Run("does not receive a message from a different queue", func(t *testing.T) {
		q1 := newQ(t, goqite.NewOpts{}, "test.db")
		q2 := newQ(t, goqite.NewOpts{Name: "q2"}, "test.db")

		err := q1.Send(context.Background(), goqite.Message{Body: []byte("yo")})
		is.NotError(t, err)

		m, err := q2.Receive(context.Background())
		is.NotError(t, err)
		is.Nil(t, m)
	})
}

func TestQueue_Extend(t *testing.T) {
	t.Run("does not receive a message that has had the timeout extended", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{Timeout: time.Millisecond}, ":memory:")

		m := &goqite.Message{
			Body: []byte("yo"),
		}

		err := q.Send(context.Background(), *m)
		is.NotError(t, err)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.NotNil(t, m)

		err = q.Extend(context.Background(), m.ID, time.Second)
		is.NotError(t, err)

		time.Sleep(time.Millisecond)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.Nil(t, m)
	})

	t.Run("panics if delay is negative", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{}, ":memory:")

		var err error
		defer func() {
			is.NotError(t, err)
			r := recover()
			is.Equal(t, "delay cannot be negative", r)
		}()

		m := &goqite.Message{
			Body: []byte("yo"),
		}

		err = q.Send(context.Background(), *m)
		is.NotError(t, err)

		m, err = q.Receive(context.Background())
		is.NotError(t, err)
		is.NotNil(t, m)

		err = q.Extend(context.Background(), m.ID, -1)
	})
}

func TestQueue_ReceiveAndWait(t *testing.T) {
	t.Run("waits for a message until the context is cancelled", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{Timeout: time.Millisecond}, ":memory:")

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()

		m, err := q.ReceiveAndWait(ctx, time.Millisecond)
		is.Error(t, context.DeadlineExceeded, err)
		is.Nil(t, m)
	})

	t.Run("gets a message immediately if there is one", func(t *testing.T) {
		q := newQ(t, goqite.NewOpts{Timeout: time.Millisecond}, ":memory:")

		err := q.Send(context.Background(), goqite.Message{Body: []byte("yo")})
		is.NotError(t, err)

		m, err := q.ReceiveAndWait(context.Background(), time.Millisecond)
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))
	})
}

func TestSetup(t *testing.T) {
	t.Run("creates the database table", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ":memory:?_journal=WAL&_timeout=5000&_fk=true")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)

		_, err = db.Exec(`select * from goqite`)
		is.Equal(t, "no such table: goqite", err.Error())
		err = goqite.Setup(context.Background(), db)
		is.NotError(t, err)
		_, err = db.Exec(`select * from goqite`)
		is.NotError(t, err)
	})
}

func BenchmarkQueue(b *testing.B) {
	b.Run("send, receive, delete", func(b *testing.B) {
		q := newQ(b, goqite.NewOpts{}, "bench.db")

		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				err := q.Send(context.Background(), goqite.Message{
					Body: []byte("yo"),
				})
				is.NotError(b, err)

				m, err := q.Receive(context.Background())
				is.NotError(b, err)
				is.NotNil(b, m)

				err = q.Delete(context.Background(), m.ID)
				is.NotError(b, err)
			}
		})
	})

	b.Run("receive and delete message on a big table with multiple queues", func(b *testing.B) {
		indexes := []struct {
			query string
			skip  bool
		}{
			{"-- no index", false},
			{"create index goqite_created_idx on goqite (created);", true},
			{"create index goqite_created_idx on goqite (created);create index goqite_queue_idx on goqite (queue);", true},
			{"create index goqite_queue_created_idx on goqite (queue, created);", true},
			{"create index goqite_queue_timeout_idx on goqite (queue, timeout);", true},
			{"create index goqite_queue_created_timeout_idx on goqite (queue, created, timeout);", true},
			{"create index goqite_queue_timeout_created_idx on goqite (queue, timeout, created);", true},
		}

		for _, index := range indexes {
			b.Run(index.query, func(b *testing.B) {
				if index.skip {
					b.SkipNow()
				}

				db := newDB(b, "bench.db")
				_, err := db.Exec(index.query)
				is.NotError(b, err)

				var queues []*goqite.Queue
				for i := 0; i < 10; i++ {
					queues = append(queues, newQ(b, goqite.NewOpts{Name: fmt.Sprintf("q%v", i)}, "bench.db"))
				}

				for i := 0; i < 100_000; i++ {
					q := queues[rand.Intn(len(queues))]
					err := q.Send(context.Background(), goqite.Message{
						Body: []byte("yo"),
					})
					is.NotError(b, err)
				}

				b.ResetTimer()

				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						q := queues[rand.Intn(len(queues))]

						m, err := q.Receive(context.Background())
						is.NotError(b, err)

						err = q.Delete(context.Background(), m.ID)
						is.NotError(b, err)
					}
				})
			})
		}
	})
}

func newDB(t testing.TB, path string) *sql.DB {
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

func newQ(t testing.TB, opts goqite.NewOpts, path string) *goqite.Queue {
	t.Helper()

	opts.DB = newDB(t, path)

	if opts.Name == "" {
		opts.Name = "test"
	}

	return goqite.New(opts)
}
