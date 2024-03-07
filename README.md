# goqite

<img src="docs/logo.png" alt="Logo" width="300" align="right">

[![GoDoc](https://pkg.go.dev/badge/github.com/maragudk/goqite)](https://pkg.go.dev/github.com/maragudk/goqite)
[![Go](https://github.com/maragudk/goqite/actions/workflows/ci.yml/badge.svg)](https://github.com/maragudk/goqite/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/maragudk/goqite/graph/badge.svg?token=DxGkk2lLHF)](https://codecov.io/gh/maragudk/goqite)

goqite (pronounced Go-queue-ite) is a persistent message queue Go library built on SQLite and inspired by AWS SQS (but much simpler).

## Features

- Messages are persisted in a SQLite table.
- Messages are sent to and received from the queue, and are guaranteed to not be redelivered before a timeout occurs.
- Support for multiple queues in one table.
- Message timeouts can be extended, to support e.g. long-running tasks.
- A simple HTTP handler is provided for your convenience.
- No non-test dependencies. Bring your own SQLite driver.

## Example

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/maragudk/goqite"
)

const schema = `
create table goqite (
  id text primary key default ('m_' || lower(hex(randomblob(16)))),
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  updated text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  queue text not null,
  body blob not null,
  timeout text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  received integer not null default 0
) strict;

create trigger goqite_updated_timestamp after update on goqite begin
  update goqite set updated = strftime('%Y-%m-%dT%H:%M:%fZ') where id = old.id;
end;
`

func main() {
	db, err := sql.Open("sqlite3", ":memory:?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		log.Fatalln(err)
	}
	if _, err := db.Exec(schema); err != nil {
		log.Fatalln(err)
	}

	// Create a new queue named "jobs".
	// You can also customize the message redelivery timeout and maximum receive count, but here, we use the defaults.
	q := goqite.New(goqite.NewOpts{
		DB:   db,
		Name: "jobs",
	})

	// Send a message to the queue.
	// Note that the body is an arbitrary byte slice, so you can decide what kind of payload you have.
	// You can also set a message delay.
	err = q.Send(context.Background(), goqite.Message{
		Body: []byte("yo"),
	})
	if err != nil {
		log.Fatalln(err)
	}

	// Receive a message from the queue, during which time it's not available to other consumers
	// (until the message timeout has passed).
	m, err := q.Receive(context.Background())
	if err != nil {
		log.Fatalln(err)
	}

	fmt.Println(string(m.Body))

	// If you need more time for processing the message, you can extend the message timeout as many times as you want.
	if err := q.Extend(context.Background(), m.ID, time.Second); err != nil {
		log.Fatalln(err)
	}

	// Make sure to delete the message, so it doesn't get redelivered.
	if err := q.Delete(context.Background(), m.ID); err != nil {
		log.Fatalln(err)
	}
}
```

## Benchmarks

Just for fun, some benchmarks. 🤓

On a MacBook Air with M1 chip and SSD, sequentially sending, receiving, and deleting a message:

```shell
$ make benchmark
go test -cpu 1,2,4,8 -bench=.
goos: darwin
goarch: arm64
pkg: github.com/maragudk/goqite
BenchmarkQueue/send_and_receive_messages           	   15885	     76166 ns/op
BenchmarkQueue/send_and_receive_messages-2         	   17215	     66464 ns/op
BenchmarkQueue/send_and_receive_messages-4         	   18106	     63733 ns/op
BenchmarkQueue/send_and_receive_messages-8         	   19184	     62530 ns/op
PASS
ok  	github.com/maragudk/goqite	7.895s
```

Note that the slowest result above is around 13,000 messages / second.

Made in 🇩🇰 by [maragu](https://www.maragu.dk/), maker of [online Go courses](https://www.golang.dk/).
