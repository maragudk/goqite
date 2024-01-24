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
	MaxReceive int           // Max receive count for messages before they cannot be received anymore.
	Timeout    time.Duration // Default timeout for messages before they can be re-received.
}

// New Queue with the given options.
// Defaults if not given:
// - Logs are discarded.
// - Max receive count is 3.
// - Timeout is a second.
func New(opts NewOpts) *Queue {
	if opts.DB == nil {
		panic("DB cannot be nil")
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
		opts.Timeout = time.Second
	}

	return &Queue{
		db:         opts.DB,
		log:        opts.Log,
		maxReceive: opts.MaxReceive,
		timeout:    opts.Timeout,
	}
}

type Queue struct {
	db         *sql.DB
	log        logger
	maxReceive int
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
		return errors.New("delay cannot be negative")
	}

	timeout := time.Now().Add(m.Delay).Format(rfc3339Milli)

	_, err := q.db.ExecContext(ctx, `insert into queue (body, timeout) values (?, ?)`, m.Body, timeout)
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
		update queue
		set
			timeout = ?,
			received = received + 1
		where id = (
			select id from queue
			where
				? >= timeout and
				received < ?
			order by created
			limit 1
		)
		returning id, body`

	var m Message
	if err := q.db.QueryRowContext(ctx, query, timeoutFormatted, nowFormatted, q.maxReceive).Scan(&m.ID, &m.Body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

// Delete a Message from the queue by id.
func (q *Queue) Delete(ctx context.Context, id ID) error {
	_, err := q.db.ExecContext(ctx, `delete from queue where id = ?`, id)
	return err
}

type discardLogger struct{}

func (l *discardLogger) Println(v ...any) {}
