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

	internalsql "maragu.dev/goqite/internal/sql"
)

type SQLFlavor int

const (
	SQLFlavorSQLite SQLFlavor = iota
	SQLFlavorPostgreSQL
)

// rfc3339Milli is like time.RFC3339Nano, but with millisecond precision, and fractional seconds do not have trailing
// zeros removed.
const rfc3339Milli = "2006-01-02T15:04:05.000Z07:00"

type NewOpts struct {
	DB         *sql.DB
	MaxReceive int // Max receive count for messages before they cannot be received anymore.
	Name       string
	SQLFlavor  SQLFlavor
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

	if opts.SQLFlavor < SQLFlavorSQLite || opts.SQLFlavor > SQLFlavorPostgreSQL {
		panic("unsupported SQL flavor " + fmt.Sprint(opts.SQLFlavor))
	}

	return &Queue{
		db:         opts.DB,
		flavor:     opts.SQLFlavor,
		name:       opts.Name,
		maxReceive: opts.MaxReceive,
		timeout:    opts.Timeout,
	}
}

type Queue struct {
	db         *sql.DB
	flavor     SQLFlavor
	maxReceive int
	name       string
	timeout    time.Duration
}

type ID string

type Message struct {
	ID       ID
	Body     []byte
	Delay    time.Duration
	Priority int // Higher priority messages are received first
}

// Send a Message to the queue with an optional delay.
func (q *Queue) Send(ctx context.Context, m Message) error {
	return internalsql.InTx(ctx, q.db, func(tx *sql.Tx) error {
		return q.SendTx(ctx, tx, m)
	})
}

// SendTx is like Send, but within an existing transaction.
func (q *Queue) SendTx(ctx context.Context, tx *sql.Tx, m Message) error {
	_, err := q.SendAndGetIDTx(ctx, tx, m)
	return err
}

// SendAndGetID is like Send, but also returns the message ID, which can be used
// to interact with the message without receiving it first.
func (q *Queue) SendAndGetID(ctx context.Context, m Message) (ID, error) {
	var id ID
	err := internalsql.InTx(ctx, q.db, func(tx *sql.Tx) error {
		var err error
		id, err = q.SendAndGetIDTx(ctx, tx, m)
		return err
	})
	return id, err
}

// SendAndGetIDTx is like SendAndGetID, but within an existing transaction.
func (q *Queue) SendAndGetIDTx(ctx context.Context, tx *sql.Tx, m Message) (ID, error) {
	if m.Delay < 0 {
		panic("delay cannot be negative")
	}

	timeout := time.Now().UTC().Add(m.Delay)

	var id ID
	switch q.flavor {
	case SQLFlavorSQLite:
		query := `insert into goqite (queue, body, timeout, priority) values (?, ?, ?, ?) returning id`
		if err := tx.QueryRowContext(ctx, query, q.name, m.Body, timeout.Format(rfc3339Milli), m.Priority).Scan(&id); err != nil {
			return "", err
		}

	case SQLFlavorPostgreSQL:
		query := `insert into goqite (queue, body, timeout, priority) values ($1, $2, $3, $4) returning id`
		if err := tx.QueryRowContext(ctx, query, q.name, m.Body, timeout, m.Priority).Scan(&id); err != nil {
			return "", err
		}
	}

	return id, nil
}

// Receive a Message from the queue, or nil if there is none.
func (q *Queue) Receive(ctx context.Context) (*Message, error) {
	var m *Message
	err := internalsql.InTx(ctx, q.db, func(tx *sql.Tx) error {
		var err error
		m, err = q.ReceiveTx(ctx, tx)
		return err
	})
	return m, err
}

// ReceiveTx is like Receive, but within an existing transaction.
func (q *Queue) ReceiveTx(ctx context.Context, tx *sql.Tx) (*Message, error) {
	now := time.Now().UTC()
	timeout := now.Add(q.timeout)

	var m Message

	switch q.flavor {
	case SQLFlavorSQLite:
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
				order by priority desc, created
				limit 1
			)
			returning id, body`

		if err := tx.QueryRowContext(ctx, query, timeout.Format(rfc3339Milli), q.name, now.Format(rfc3339Milli), q.maxReceive).Scan(&m.ID, &m.Body); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}

	case SQLFlavorPostgreSQL:
		query := `
			update goqite
			set
				timeout = $1,
				received = received + 1
			where id = (
				select id from goqite
				where
					queue = $2 and
					$3 >= timeout and
					received < $4
				order by priority desc, created
				limit 1
			)
			returning id, body`

		if err := tx.QueryRowContext(ctx, query, timeout, q.name, now, q.maxReceive).Scan(&m.ID, &m.Body); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
	}

	return &m, nil
}

// ReceiveAndWait for a Message from the queue, polling at the given interval, until the context is cancelled.
// If the context is cancelled, the error will be non-nil. See [context.Context.Err].
func (q *Queue) ReceiveAndWait(ctx context.Context, interval time.Duration) (*Message, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			m, err := q.Receive(context.WithoutCancel(ctx))
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
	return internalsql.InTx(ctx, q.db, func(tx *sql.Tx) error {
		return q.ExtendTx(ctx, tx, id, delay)
	})
}

// ExtendTx is like Extend, but within an existing transaction.
func (q *Queue) ExtendTx(ctx context.Context, tx *sql.Tx, id ID, delay time.Duration) error {
	if delay < 0 {
		panic("delay cannot be negative")
	}

	timeout := time.Now().UTC().Add(delay)

	var err error
	switch q.flavor {
	case SQLFlavorSQLite:
		_, err = tx.ExecContext(ctx, `update goqite set timeout = ? where queue = ? and id = ?`, timeout.Format(rfc3339Milli), q.name, id)

	case SQLFlavorPostgreSQL:
		_, err = tx.ExecContext(ctx, `update goqite set timeout = $1 where queue = $2 and id = $3`, timeout, q.name, id)
	}

	return err
}

// Delete a Message from the queue by id.
func (q *Queue) Delete(ctx context.Context, id ID) error {
	return internalsql.InTx(ctx, q.db, func(tx *sql.Tx) error {
		return q.DeleteTx(ctx, tx, id)
	})
}

// DeleteTx is like Delete, but within an existing transaction.
func (q *Queue) DeleteTx(ctx context.Context, tx *sql.Tx, id ID) error {
	var err error
	switch q.flavor {
	case SQLFlavorSQLite:
		_, err = tx.ExecContext(ctx, `delete from goqite where queue = ? and id = ?`, q.name, id)

	case SQLFlavorPostgreSQL:
		_, err = tx.ExecContext(ctx, `delete from goqite where queue = $1 and id = $2`, q.name, id)
	}

	return err
}
