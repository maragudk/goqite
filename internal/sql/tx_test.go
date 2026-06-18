package sql

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"maragu.dev/is"
)

func TestIsSerializationError(t *testing.T) {
	t.Run("matches postgres serialization errors", func(t *testing.T) {
		err := &pgconn.PgError{Code: "40001"}
		is.True(t, isSerializationError(err))
	})

	t.Run("matches wrapped postgres serialization errors", func(t *testing.T) {
		err := fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "40001"})
		is.True(t, isSerializationError(err))
	})

	t.Run("does not match other errors", func(t *testing.T) {
		err := &pgconn.PgError{Code: "23505"}
		is.True(t, !isSerializationError(err))
	})
}
