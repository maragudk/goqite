// Package goqite provides the named Queue.
// It is backed by a SQLite table where the messages are stored.
package goqite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"
)

//go:embed schema.sql
var schema string

// rfc3339Milli is like time.RFC3339Nano, but with millisecond precision, and fractional seconds do not have trailing
// zeros removed.
const rfc3339Milli = "2006-01-02T15:04:05.000Z07:00"

type logger interface {
	Println(v ...any)
}

type NewOpts struct {
	DB         *sql.DB
	Log        logger
	MaxReceive int // Max receive count for messages before they cannot be received anymore.
	Name       string
	Timeout    time.Duration // Default timeout for messages before they can be re-received.
}

// New Queue with the given options.
// Defaults if not given:
// - Logs are discarded.
// - Max receive count is 3.
// - Timeout is five seconds.
func New(opts NewOpts) *Queue {
	if opts.DB == nil {
		panic("db cannot be nil")
	}

	if opts.Name == "" {
		panic("name cannot be empty")
	}

	if opts.Log == nil {
		opts.Log = &discardLogger{}
	}

	if opts.MaxReceive < 0 {
		panic("max receive cannot be negative")
	}

	if opts.MaxReceive == 0 {
		opts.MaxReceive = 3
	}

	if opts.Timeout < 0 {
		panic("timeout cannot be negative")
	}

	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}

	return &Queue{
		db:         opts.DB,
		name:       opts.Name,
		log:        opts.Log,
		maxReceive: opts.MaxReceive,
		timeout:    opts.Timeout,
	}
}

type Queue struct {
	db         *sql.DB
	log        logger
	maxReceive int
	name       string
	timeout    time.Duration
}

type ID string

type Message struct {
	ID    ID
	Delay time.Duration
	Body  []byte
}

// Send a Message to the queue with an optional delay.
func (q *Queue) Send(ctx context.Context, m Message) error {
	return q.inTx(ctx, func(tx *sql.Tx) error {
		return q.SendTx(ctx, tx, m)
	})
}

// SendTx is like Send, but within an existing transaction.
func (q *Queue) SendTx(ctx context.Context, tx *sql.Tx, m Message) error {
	if m.Delay < 0 {
		panic("delay cannot be negative")
	}

	timeout := time.Now().Add(m.Delay).Format(rfc3339Milli)

	_, err := tx.ExecContext(ctx, `insert into goqite (queue, body, timeout) values (?, ?, ?)`, q.name, m.Body, timeout)
	if err != nil {
		return err
	}
	return nil
}

// Receive a Message from the queue, or nil if there is none.
func (q *Queue) Receive(ctx context.Context) (*Message, error) {
	var m *Message
	err := q.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		m, err = q.ReceiveTx(ctx, tx)
		return err
	})
	return m, err
}

// ReceiveTx is like Receive, but within an existing transaction.
func (q *Queue) ReceiveTx(ctx context.Context, tx *sql.Tx) (*Message, error) {
	now := time.Now()
	nowFormatted := now.Format(rfc3339Milli)
	timeoutFormatted := now.Add(q.timeout).Format(rfc3339Milli)

	query := `
		update goqite
		set
			timeout = ?,
			received = received + 1
		where id = (
			select id from goqite
			where
				queue = ? and
				? >= timeout and
				received < ?
			order by created
			limit 1
		)
		returning id, body`

	var m Message
	if err := tx.QueryRowContext(ctx, query, timeoutFormatted, q.name, nowFormatted, q.maxReceive).Scan(&m.ID, &m.Body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

// ReceiveAndWait for a Message from the queue or the context is cancelled.
// If the context is cancelled, the error will be non-nil. See context.Context.Err.
func (q *Queue) ReceiveAndWait(ctx context.Context, interval time.Duration) (*Message, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			m, err := q.Receive(ctx)
			if err != nil {
				return nil, err
			}
			if m != nil {
				return m, nil
			}
		}
	}
}

// Extend a Message timeout by the given delay from now.
func (q *Queue) Extend(ctx context.Context, id ID, delay time.Duration) error {
	return q.inTx(ctx, func(tx *sql.Tx) error {
		return q.ExtendTx(ctx, tx, id, delay)
	})
}

// ExtendTx is like Extend, but within an existing transaction.
func (q *Queue) ExtendTx(ctx context.Context, tx *sql.Tx, id ID, delay time.Duration) error {
	if delay < 0 {
		panic("delay cannot be negative")
	}

	timeout := time.Now().Add(delay).Format(rfc3339Milli)

	_, err := tx.ExecContext(ctx, `update goqite set timeout = ? where queue = ? and id = ?`, timeout, q.name, id)
	return err
}

// Delete a Message from the queue by id.
func (q *Queue) Delete(ctx context.Context, id ID) error {
	return q.inTx(ctx, func(tx *sql.Tx) error {
		return q.DeleteTx(ctx, tx, id)
	})
}

// DeleteTx is like Delete, but within an existing transaction.
func (q *Queue) DeleteTx(ctx context.Context, tx *sql.Tx, id ID) error {
	_, err := tx.ExecContext(ctx, `delete from goqite where queue = ? and id = ?`, q.name, id)
	return err
}

func (q *Queue) inTx(ctx context.Context, cb func(*sql.Tx) error) (err error) {
	tx, txErr := q.db.Begin()
	if txErr != nil {
		return fmt.Errorf("cannot start tx: %w", txErr)
	}

	defer func() {
		if rec := recover(); rec != nil {
			err = rollback(tx, nil)
			panic(rec)
		}
	}()

	if err := cb(tx); err != nil {
		return rollback(tx, err)
	}

	if txErr := tx.Commit(); txErr != nil {
		return fmt.Errorf("cannot commit tx: %w", txErr)
	}

	return nil
}

func rollback(tx *sql.Tx, err error) error {
	if txErr := tx.Rollback(); txErr != nil {
		return fmt.Errorf("cannot roll back tx after error (tx error: %v), original error: %w", txErr, err)
	}
	return err
}

type discardLogger struct{}

func (l *discardLogger) Println(v ...any) {}

// Initialize the queue in the database.
func Initialize(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schema)
	return err
}
