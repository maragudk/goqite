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
