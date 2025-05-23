package jobs_test

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"maragu.dev/is"

	"maragu.dev/goqite"
	internalsql "maragu.dev/goqite/internal/sql"
	internaltesting "maragu.dev/goqite/internal/testing"
	"maragu.dev/goqite/jobs"
)

func TestRunner_Register(t *testing.T) {
	t.Run("can register a new job", func(t *testing.T) {
		r := jobs.NewRunner(jobs.NewRunnerOpts{})
		r.Register("test", func(ctx context.Context, m []byte) error {
			return nil
		})
	})

	t.Run("panics if the same job is registered twice", func(t *testing.T) {
		r := jobs.NewRunner(jobs.NewRunnerOpts{})
		r.Register("test", func(ctx context.Context, m []byte) error {
			return nil
		})
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("did not panic")
			}
			is.Equal(t, `job "test" already registered`, r)
		}()
		r.Register("test", func(ctx context.Context, m []byte) error {
			return nil
		})
	})
}

func TestRunner_Start(t *testing.T) {
	t.Run("can run a named job", func(t *testing.T) {
		q, r := newRunner(t)

		var ran bool
		ctx, cancel := context.WithCancel(t.Context())
		r.Register("test", func(ctx context.Context, m []byte) error {
			ran = true
			is.Equal(t, "yo", string(m))
			cancel()
			return nil
		})

		err := jobs.Create(ctx, q, "test", goqite.Message{Body: []byte("yo")})
		is.NotError(t, err)

		r.Start(ctx)
		is.True(t, ran)
	})

	t.Run("doesn't run a different job", func(t *testing.T) {
		q, r := newRunner(t)

		var ranTest, ranDifferentTest bool
		ctx, cancel := context.WithCancel(t.Context())
		r.Register("test", func(ctx context.Context, m []byte) error {
			ranTest = true
			return nil
		})
		r.Register("different-test", func(ctx context.Context, m []byte) error {
			ranDifferentTest = true
			cancel()
			return nil
		})

		err := jobs.Create(ctx, q, "different-test", goqite.Message{Body: []byte("yo")})
		is.NotError(t, err)

		r.Start(ctx)
		is.True(t, !ranTest)
		is.True(t, ranDifferentTest)
	})

	t.Run("panics if the job is not registered", func(t *testing.T) {
		q, r := newRunner(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		err := jobs.Create(ctx, q, "test", goqite.Message{Body: []byte("yo")})
		is.NotError(t, err)

		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("did not panic")
			}
			is.Equal(t, `job "test" not registered`, r)
		}()
		r.Start(ctx)
	})

	t.Run("does not panic if job panics", func(t *testing.T) {
		q, r := newRunner(t)

		ctx, cancel := context.WithCancel(t.Context())

		r.Register("test", func(ctx context.Context, m []byte) error {
			cancel()
			panic("test panic")
		})

		err := jobs.Create(ctx, q, "test", goqite.Message{Body: []byte("yo")})
		is.NotError(t, err)

		r.Start(ctx)
	})

	t.Run("extends a job's timeout if it takes longer than the default timeout", func(t *testing.T) {
		q, r := newRunner(t)

		var runCount int
		ctx, cancel := context.WithCancel(t.Context())
		r.Register("test", func(ctx context.Context, m []byte) error {
			runCount++
			// This is more than the default timeout, so it should extend
			time.Sleep(150 * time.Millisecond)
			cancel()
			return nil
		})

		err := jobs.Create(ctx, q, "test", goqite.Message{Body: []byte("yo")})
		is.NotError(t, err)

		r.Start(ctx)
		is.Equal(t, 1, runCount)
	})

	t.Run("processes jobs with higher priority first", func(t *testing.T) {
		q, r := newRunner(t)

		var order []string
		ctx, cancel := context.WithCancel(t.Context())
		r.Register("test", func(ctx context.Context, m []byte) error {
			order = append(order, string(m))
			if len(order) == 3 {
				cancel()
			}
			return nil
		})

		// Create jobs with different priorities
		err := jobs.Create(ctx, q, "test", goqite.Message{Body: []byte("low"), Priority: 0})
		is.NotError(t, err)
		err = jobs.Create(ctx, q, "test", goqite.Message{Body: []byte("high"), Priority: 10})
		is.NotError(t, err)
		err = jobs.Create(ctx, q, "test", goqite.Message{Body: []byte("medium"), Priority: 5})
		is.NotError(t, err)

		r.Start(ctx)
		
		// Jobs should be processed in priority order: high (10), medium (5), low (0)
		is.Equal(t, 3, len(order))
		is.Equal(t, "high", order[0])
		is.Equal(t, "medium", order[1])
		is.Equal(t, "low", order[2])
	})
}

func TestCreateTx(t *testing.T) {
	t.Run("can create a job inside a transaction", func(t *testing.T) {
		db := internaltesting.NewSQLiteDB(t)
		q := internaltesting.NewQ(t, goqite.NewOpts{DB: db})
		r := jobs.NewRunner(jobs.NewRunnerOpts{Log: internaltesting.NewLogger(t), Queue: q})

		var ran bool
		ctx, cancel := context.WithCancel(t.Context())
		r.Register("test", func(ctx context.Context, m []byte) error {
			ran = true
			is.Equal(t, "yo", string(m))
			cancel()
			return nil
		})

		err := internalsql.InTx(ctx, db, func(tx *sql.Tx) error {
			return jobs.CreateTx(ctx, tx, q, "test", goqite.Message{Body: []byte("yo")})
		})
		is.NotError(t, err)

		r.Start(ctx)
		is.True(t, ran)
	})
}

func ExampleRunner_Start() {
	log := slog.Default()

	// Setup the db
	db, err := sql.Open("sqlite3", ":memory:?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		log.Info("Error opening db", "error", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Setup the schema
	schema, err := os.ReadFile("schema_sqlite.sql")
	if err != nil {
		log.Info("Error reading schema:", "error", err)
		return
	}

	if _, err := db.Exec(string(schema)); err != nil {
		log.Info("Error executing schema:", "error", err)
		return
	}

	// Make a new queue for the jobs. You can have as many of these as you like, just name them differently.
	q := goqite.New(goqite.NewOpts{
		DB:   db,
		Name: "jobs",
	})

	// Make a job runner with a job limit of 1 and a short message poll interval.
	r := jobs.NewRunner(jobs.NewRunnerOpts{
		Limit:        1,
		Log:          log,
		PollInterval: 10 * time.Millisecond,
		Queue:        q,
	})

	// Register our "print" job.
	r.Register("print", func(ctx context.Context, m []byte) error {
		fmt.Println(string(m))
		return nil
	})

	// Create a "print" job with a message.
	if err := jobs.Create(context.Background(), q, "print", goqite.Message{Body: []byte("Yo")}); err != nil {
		log.Info("Error creating job", "error", err)
	}

	// Stop the job runner after a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// Start the job runner and see the job run.
	r.Start(ctx)

	// Output: Yo
}

func newRunner(t *testing.T) (*goqite.Queue, *jobs.Runner) {
	t.Helper()

	q := internaltesting.NewQ(t, goqite.NewOpts{DB: internaltesting.NewSQLiteDB(t), Timeout: 100 * time.Millisecond})
	r := jobs.NewRunner(jobs.NewRunnerOpts{Limit: 10, Log: internaltesting.NewLogger(t), Queue: q, Extend: 100 * time.Millisecond})
	return q, r
}
