package goqite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

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
	if m.Delay < 0 {
		panic("delay cannot be negative")
	}

	timeout := time.Now().Add(m.Delay).Format(rfc3339Milli)

	_, err := q.db.ExecContext(ctx, `insert into goqite (queue, body, timeout) values (?, ?, ?)`, q.name, m.Body, timeout)
	if err != nil {
		return err
	}
	return nil
}

// Receive a Message from the queue, or nil if there is none.
func (q *Queue) Receive(ctx context.Context) (*Message, error) {
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
	if err := q.db.QueryRowContext(ctx, query, timeoutFormatted, q.name, nowFormatted, q.maxReceive).Scan(&m.ID, &m.Body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

// Extend a message timeout by the given delay from now.
func (q *Queue) Extend(ctx context.Context, id ID, delay time.Duration) error {
	if delay < 0 {
		panic("delay cannot be negative")
	}

	timeout := time.Now().Add(delay).Format(rfc3339Milli)

	_, err := q.db.ExecContext(ctx, `update goqite set timeout = ? where queue = ? and id = ?`, timeout, q.name, id)
	return err
}

// Delete a Message from the queue by id.
func (q *Queue) Delete(ctx context.Context, id ID) error {
	_, err := q.db.ExecContext(ctx, `delete from goqite where queue = ? and id = ?`, q.name, id)
	return err
}

type discardLogger struct{}

func (l *discardLogger) Println(v ...any) {}
