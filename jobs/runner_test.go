package jobs_test

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/maragudk/is"
	_ "github.com/mattn/go-sqlite3"

	"github.com/maragudk/goqite"
	internalsql "github.com/maragudk/goqite/internal/sql"
	internaltesting "github.com/maragudk/goqite/internal/testing"
	"github.com/maragudk/goqite/jobs"
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
		ctx, cancel := context.WithCancel(context.Background())
		r.Register("test", func(ctx context.Context, m []byte) error {
			ran = true
			is.Equal(t, "yo", string(m))
			cancel()
			return nil
		})

		err := jobs.Create(ctx, q, "test", []byte("yo"))
		is.NotError(t, err)

		r.Start(ctx)
		is.True(t, ran)
	})

	t.Run("doesn't run a different job", func(t *testing.T) {
		q, r := newRunner(t)

		var ranTest, ranDifferentTest bool
		ctx, cancel := context.WithCancel(context.Background())
		r.Register("test", func(ctx context.Context, m []byte) error {
			ranTest = true
			return nil
		})
		r.Register("different-test", func(ctx context.Context, m []byte) error {
			ranDifferentTest = true
			cancel()
			return nil
		})

		err := jobs.Create(ctx, q, "different-test", []byte("yo"))
		is.NotError(t, err)

		r.Start(ctx)
		is.True(t, !ranTest)
		is.True(t, ranDifferentTest)
	})

	t.Run("panics if the job is not registered", func(t *testing.T) {
		q, r := newRunner(t)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err := jobs.Create(ctx, q, "test", []byte("yo"))
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

		ctx, cancel := context.WithCancel(context.Background())

		r.Register("test", func(ctx context.Context, m []byte) error {
			cancel()
			panic("test panic")
		})

		err := jobs.Create(ctx, q, "test", []byte("yo"))
		is.NotError(t, err)

		r.Start(ctx)
	})
}

func TestCreateTx(t *testing.T) {
	t.Run("can create a job inside a transaction", func(t *testing.T) {
		db := internaltesting.NewDB(t, ":memory:")
		q := internaltesting.NewQ(t, goqite.NewOpts{DB: db}, ":memory:")
		r := jobs.NewRunner(jobs.NewRunnerOpts{Log: internaltesting.NewLogger(t), Queue: q})

		var ran bool
		ctx, cancel := context.WithCancel(context.Background())
		r.Register("test", func(ctx context.Context, m []byte) error {
			ran = true
			is.Equal(t, "yo", string(m))
			cancel()
			return nil
		})

		err := internalsql.InTx(db, func(tx *sql.Tx) error {
			return jobs.CreateTx(ctx, tx, q, "test", []byte("yo"))
		})
		is.NotError(t, err)

		r.Start(ctx)
		is.True(t, ran)
	})
}

func ExampleRunner_Start() {
	log := slog.Default()

	// Setup the db and goqite schema.
	db, err := sql.Open("sqlite3", ":memory:?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		log.Info("Error opening db", "error", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := goqite.Setup(context.Background(), db); err != nil {
		log.Info("Error in setup", "error", err)
	}

	// Make a new queue for the jobs. You can have as many of these as you like, just name them differently.
	q := goqite.New(goqite.NewOpts{
		DB:   db,
		Name: "jobs",
	})

	// Make a job runner with a job limit of 1 and a short message poll interval.
	r := jobs.NewRunner(jobs.NewRunnerOpts{
		Limit:        1,
		Log:          slog.Default(),
		PollInterval: 10 * time.Millisecond,
		Queue:        q,
	})

	// Register our "print" job.
	r.Register("print", func(ctx context.Context, m []byte) error {
		fmt.Println(string(m))
		return nil
	})

	// Create a "print" job with a message.
	if err := jobs.Create(context.Background(), q, "print", []byte("Yo")); err != nil {
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

	q := internaltesting.NewQ(t, goqite.NewOpts{}, ":memory:")
	r := jobs.NewRunner(jobs.NewRunnerOpts{Log: internaltesting.NewLogger(t), Queue: q})
	return q, r
}
