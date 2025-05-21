# goqite

<img src="docs/logo.png" alt="Logo" width="300" align="right">

[![GoDoc](https://pkg.go.dev/badge/maragu.dev/goqite)](https://pkg.go.dev/maragu.dev/goqite)
[![CI](https://github.com/maragudk/goqite/actions/workflows/ci.yml/badge.svg)](https://github.com/maragudk/goqite/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/maragudk/goqite/graph/badge.svg?token=DxGkk2lLHF)](https://codecov.io/gh/maragudk/goqite)

goqite (pronounced Go-queue-ite) is a persistent message queue Go library built on SQLite and inspired by AWS SQS (but much simpler).

Also supports Postgres!

```shell
go get maragu.dev/goqite
```

Made with ✨sparkles✨ by [maragu](https://www.maragu.dev/): independent software consulting for cloud-native Go apps & AI engineering.

[Contact me at markus@maragu.dk](mailto:markus@maragu.dk) for consulting work, or perhaps an invoice to support this project?

## Features

- Messages are persisted in a single database table.
- Messages are sent to and received from the queue, and are guaranteed to not be redelivered before a timeout occurs.
- Support for multiple queues in one table.
- Message timeouts can be extended, to support e.g. long-running tasks.
- A job runner abstraction is provided on top of the queue, for your background tasks.
- A simple HTTP handler is provided for your convenience.
- No non-test dependencies. Bring your own SQL driver.

## Examples

### Queue

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"maragu.dev/goqite"
)

func main() {
	log := slog.Default()

	// Setup the db
	db, err := sql.Open("sqlite3", ":memory:?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		log.Info("Error opening db", "error", err)
		return
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

	// Create a new queue named "jobs".
	// You can also customize the message redelivery timeout and maximum receive count,
	// but here, we use the defaults.
	q := goqite.New(goqite.NewOpts{
		DB:   db,
		Name: "jobs",
	})

	// Send a message to the queue.
	// Note that the body is an arbitrary byte slice, so you can decide
	// what kind of payload you have. You can also set a message delay.
	err = q.Send(context.Background(), goqite.Message{
		Body: []byte("yo"),
	})
	if err != nil {
		log.Info("Error sending message", "error", err)
		return
	}

	// Receive a message from the queue, during which time it's not available to
	// other consumers (until the message timeout has passed).
	m, err := q.Receive(context.Background())
	if err != nil {
		log.Info("Error receiving message", "error", err)
		return
	}

	fmt.Println(string(m.Body))

	// If you need more time for processing the message, you can extend
	// the message timeout as many times as you want.
	if err := q.Extend(context.Background(), m.ID, time.Second); err != nil {
		log.Info("Error extending message timeout", "error", err)
		return
	}

	// Make sure to delete the message, so it doesn't get redelivered.
	if err := q.Delete(context.Background(), m.ID); err != nil {
		log.Info("Error deleting message", "error", err)
		return
	}
}
```

### Jobs

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"maragu.dev/goqite"
	"maragu.dev/goqite/jobs"
)

func main() {
	log := slog.Default()

	// Setup the db
	db, err := sql.Open("sqlite3", ":memory:?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		log.Info("Error opening db", "error", err)
		return
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
	if err := jobs.Create(context.Background(), q, "print", []byte("Yo")); err != nil {
		log.Info("Error creating job", "error", err)
	}

	// Stop the job runner after a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// Start the job runner and see the job run.
	r.Start(ctx)
}
```

## Benchmarks

Just for fun, some benchmarks. 🤓

On a MacBook Pro with M3 Ultra chip and SSD, sequentially sending, receiving, and deleting a message:

```shell
$ make benchmark
go test -cpu 1,2,4,8,16 -bench=.
goos: darwin
goarch: arm64
pkg: github.com/maragudk/goqite
BenchmarkQueue/send,_receive,_delete            	   21444	     54262 ns/op
BenchmarkQueue/send,_receive,_delete-2          	   17278	     68615 ns/op
BenchmarkQueue/send,_receive,_delete-4          	   16092	     73888 ns/op
BenchmarkQueue/send,_receive,_delete-8          	   15346	     78255 ns/op
BenchmarkQueue/send,_receive,_delete-16         	   15106	     79517 ns/op
```

Note that the slowest result above is around 12,500 messages / second with 16 parallel producers/consumers.
The fastest result is around 18,500 messages / second with just one producer/consumer.
(SQLite only allows one writer at a time, so the parallelism just creates write contention.)
