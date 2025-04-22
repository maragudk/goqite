package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"maragu.dev/goqite"
	"maragu.dev/goqite/jobs"
)

func main() {
	log := slog.Default()

	// Setup the db and goqite schema.
	db, err := sql.Open("sqlite3", ":memory:?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		log.Info("Error opening db", "error", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := goqite.Setup(context.Background(), db); err != nil {
		log.Info("Error in setup", "error", err)
	}

	// Make a new queue for the jobs. You can have as many of these as you like, just name them differently.
	q := goqite.New(goqite.NewOpts{
		DB:   db,
		Name: "jobs",
	})

	// Make a job runner with a job limit of 1 and a short message poll interval.
	r := jobs.NewRunner(jobs.NewRunnerOpts{
		Limit:        1,
		Log:          slog.Default(),
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
