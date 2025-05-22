package goqite_test

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"maragu.dev/is"

	"maragu.dev/goqite"
	internaltesting "maragu.dev/goqite/internal/testing"
)

func TestQueue(t *testing.T) {
	internaltesting.Run(t, "can send and receive and delete a message", time.Millisecond, func(t *testing.T, db *sql.DB, q *goqite.Queue) {
		m, err := q.Receive(t.Context())
		is.NotError(t, err)
		is.Nil(t, m)

		m = &goqite.Message{
			Body: []byte("yo"),
		}

		err = q.Send(t.Context(), *m)
		is.NotError(t, err)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))

		err = q.Delete(t.Context(), m.ID)
		is.NotError(t, err)

		time.Sleep(time.Millisecond)

		m, err = q.Receive(t.Context())
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

	t.Run("panics if SQL flavor is too high", func(t *testing.T) {
		defer func() {
			r := recover()
			is.Equal(t, "unsupported SQL flavor 2", r)
		}()

		// Using 2 as an invalid SQLFlavor value (should be 0 or 1)
		goqite.New(goqite.NewOpts{DB: &sql.DB{}, Name: "test", SQLFlavor: 2})
	})
	
	t.Run("panics if SQL flavor is negative", func(t *testing.T) {
		defer func() {
			r := recover()
			is.Equal(t, "unsupported SQL flavor -1", r)
		}()

		goqite.New(goqite.NewOpts{DB: &sql.DB{}, Name: "test", SQLFlavor: -1})
	})
}

func TestQueue_Send(t *testing.T) {
	internaltesting.Run(t, "panics if delay is negative", 0, func(t *testing.T, db *sql.DB, q *goqite.Queue) {
		var err error
		defer func() {
			is.NotError(t, err)
			r := recover()
			is.Equal(t, "delay cannot be negative", r)
		}()

		err = q.Send(t.Context(), goqite.Message{Delay: -1})
	})
}

func TestQueue_Receive(t *testing.T) {
	internaltesting.Run(t, "does not receive a delayed message immediately", 0, func(t *testing.T, db *sql.DB, q *goqite.Queue) {
		m := &goqite.Message{
			Body:  []byte("yo"),
			Delay: 100 * time.Millisecond,
		}

		err := q.Send(t.Context(), *m)
		is.NotError(t, err)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.Nil(t, m)

		time.Sleep(100 * time.Millisecond)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))
	})

	internaltesting.Run(t, "does not receive a message twice in a row", 0, func(t *testing.T, db *sql.DB, q *goqite.Queue) {
		m := &goqite.Message{
			Body: []byte("yo"),
		}

		err := q.Send(t.Context(), *m)
		is.NotError(t, err)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.Nil(t, m)
	})

	internaltesting.Run(t, "does receive a message up to three times if set and timeout has passed", time.Millisecond, func(t *testing.T, db *sql.DB, q *goqite.Queue) {
		m := &goqite.Message{
			Body: []byte("yo"),
		}

		err := q.Send(t.Context(), *m)
		is.NotError(t, err)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))

		time.Sleep(time.Millisecond)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))

		time.Sleep(time.Millisecond)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))

		time.Sleep(time.Millisecond)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.Nil(t, m)
	})

	t.Run("does not receive a message from a different queue", func(t *testing.T) {
		tests := []struct {
			name   string
			flavor goqite.SQLFlavor
			db     *sql.DB
		}{
			{"sqlite", goqite.SQLFlavorSQLite, internaltesting.NewSQLiteDB(t)},
			{"postgresql", goqite.SQLFlavorPostgreSQL, internaltesting.NewPostgreSQLDB(t)},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				q1 := internaltesting.NewQ(t, goqite.NewOpts{DB: test.db, Name: "q1", SQLFlavor: test.flavor})
				q2 := internaltesting.NewQ(t, goqite.NewOpts{DB: test.db, Name: "q2", SQLFlavor: test.flavor})

				err := q1.Send(t.Context(), goqite.Message{Body: []byte("yo")})
				is.NotError(t, err)

				m, err := q2.Receive(t.Context())
				is.NotError(t, err)
				is.Nil(t, m)
			})
		}
	})
}

func TestQueue_SendAndGetID(t *testing.T) {
	internaltesting.Run(t, "returns the message ID", 0, func(t *testing.T, db *sql.DB, q *goqite.Queue) {
		m := goqite.Message{
			Body: []byte("yo"),
		}

		id, err := q.SendAndGetID(t.Context(), m)
		is.NotError(t, err)
		is.Equal(t, 34, len(id))

		err = q.Delete(t.Context(), id)
		is.NotError(t, err)
	})
}

func TestQueue_Extend(t *testing.T) {
	internaltesting.Run(t, "does not receive a message that has had the timeout extended", 0, func(t *testing.T, db *sql.DB, q *goqite.Queue) {
		m := &goqite.Message{
			Body: []byte("yo"),
		}

		err := q.Send(t.Context(), *m)
		is.NotError(t, err)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.NotNil(t, m)

		err = q.Extend(t.Context(), m.ID, time.Second)
		is.NotError(t, err)

		time.Sleep(time.Millisecond)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.Nil(t, m)
	})

	internaltesting.Run(t, "panics if delay is negative", 0, func(t *testing.T, db *sql.DB, q *goqite.Queue) {
		var err error
		defer func() {
			is.NotError(t, err)
			r := recover()
			is.Equal(t, "delay cannot be negative", r)
		}()

		m := &goqite.Message{
			Body: []byte("yo"),
		}

		err = q.Send(t.Context(), *m)
		is.NotError(t, err)

		m, err = q.Receive(t.Context())
		is.NotError(t, err)
		is.NotNil(t, m)

		err = q.Extend(t.Context(), m.ID, -1)
	})
}

func TestQueue_ReceiveAndWait(t *testing.T) {
	internaltesting.Run(t, "waits for a message until the context is cancelled", 0, func(t *testing.T, db *sql.DB, q *goqite.Queue) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
		defer cancel()

		m, err := q.ReceiveAndWait(ctx, time.Millisecond)
		is.Error(t, context.DeadlineExceeded, err)
		is.Nil(t, m)
	})

	internaltesting.Run(t, "gets a message immediately if there is one", 0, func(t *testing.T, db *sql.DB, q *goqite.Queue) {
		err := q.Send(t.Context(), goqite.Message{Body: []byte("yo")})
		is.NotError(t, err)

		m, err := q.ReceiveAndWait(t.Context(), time.Millisecond)
		is.NotError(t, err)
		is.NotNil(t, m)
		is.Equal(t, "yo", string(m.Body))
	})
}

func BenchmarkQueue(b *testing.B) {
	b.Run("send, receive, delete", func(b *testing.B) {
		q := internaltesting.NewQ(b, goqite.NewOpts{})

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

				db := internaltesting.NewSQLiteDB(b)
				_, err := db.Exec(index.query)
				is.NotError(b, err)

				var queues []*goqite.Queue
				for i := range 10 {
					queues = append(queues, internaltesting.NewQ(b, goqite.NewOpts{Name: fmt.Sprintf("q%v", i)}))
				}

				for range 100_000 {
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
