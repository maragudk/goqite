package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/maragudk/is"

	"github.com/maragudk/goqite"
	"github.com/maragudk/goqite/internal"
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

func newRunner(t *testing.T) (*goqite.Queue, *jobs.Runner) {
	t.Helper()

	q := internal.NewQ(t, goqite.NewOpts{}, ":memory:")
	r := jobs.NewRunner(jobs.NewRunnerOpts{Log: internal.NewLogger(t), Queue: q})
	return q, r
}
