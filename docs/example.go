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

create index goqite_queue_created_idx on goqite (queue, created);
`

func main() {
	// Bring your own database connection, since you probably already have it,
	// as well as some sort of schema migration system.
	db, err := sql.Open("sqlite3", ":memory:?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		log.Fatalln(err)
	}
	if _, err := db.Exec(schema); err != nil {
		log.Fatalln(err)
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
		log.Fatalln(err)
	}

	// Receive a message from the queue, during which time it's not available to
	// other consumers (until the message timeout has passed).
	m, err := q.Receive(context.Background())
	if err != nil {
		log.Fatalln(err)
	}

	fmt.Println(string(m.Body))

	// If you need more time for processing the message, you can extend
	// the message timeout as many times as you want.
	if err := q.Extend(context.Background(), m.ID, time.Second); err != nil {
		log.Fatalln(err)
	}

	// Make sure to delete the message, so it doesn't get redelivered.
	if err := q.Delete(context.Background(), m.ID); err != nil {
		log.Fatalln(err)
	}
}
