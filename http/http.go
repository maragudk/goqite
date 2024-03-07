// Package http provides an HTTP handler for a goqite.Queue.
// GET receives a message from the queue, if any. If there is no message, it returns a 204 No Content.
// POST sends a message to the queue.
// PUT extends a message's timeout.
// DELETE deletes a message from the queue.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/maragudk/goqite"
)

type queue interface {
	Send(ctx context.Context, m goqite.Message) error
	Receive(ctx context.Context) (*goqite.Message, error)
	ReceiveAndWait(ctx context.Context, interval time.Duration) (*goqite.Message, error)
	Extend(ctx context.Context, id goqite.ID, delay time.Duration) error
	Delete(ctx context.Context, id goqite.ID) error
}

type request struct {
	Message goqite.Message
}

type response struct {
	Message *goqite.Message
}

func Handler(q queue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			var m *goqite.Message
			var err error

			if r.URL.Query().Get("timeout") == "" {
				m, err = q.Receive(r.Context())
			} else {
				var timeout time.Duration
				timeout, err = time.ParseDuration(r.URL.Query().Get("timeout"))
				if err != nil {
					http.Error(w, "error parsing timeout parameter: "+err.Error(), http.StatusBadRequest)
					return
				}

				if timeout <= 0 || timeout > 20*time.Second {
					http.Error(w, "timeout must be between 0 (exclusive) and 20 (inclusive) seconds", http.StatusBadRequest)
					return
				}

				interval := min(timeout, 100*time.Millisecond)
				if r.URL.Query().Get("interval") != "" {
					interval, err = time.ParseDuration(r.URL.Query().Get("interval"))
					if err != nil {
						http.Error(w, "error parsing interval parameter: "+err.Error(), http.StatusBadRequest)
						return
					}

					if interval <= 0 || interval > timeout {
						http.Error(w, "interval must be between 0 (exclusive) and timeout (inclusive)", http.StatusBadRequest)
						return
					}
				}

				ctx, cancel := context.WithTimeout(r.Context(), timeout)
				defer cancel()

				m, err = q.ReceiveAndWait(ctx, interval)
				if err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}

			if err != nil {
				http.Error(w, "error receiving message: "+err.Error(), http.StatusInternalServerError)
				return
			}

			if m == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if err := json.NewEncoder(w).Encode(response{Message: m}); err != nil {
				http.Error(w, "error encoding message: "+err.Error(), http.StatusInternalServerError)
				return
			}

		case http.MethodPost:
			req, ok := fromJson(w, r)
			if !ok {
				return
			}

			if req.Message.Delay < 0 {
				http.Error(w, "delay cannot be negative", http.StatusBadRequest)
				return
			}

			if err := q.Send(r.Context(), req.Message); err != nil {
				http.Error(w, "error sending message: "+err.Error(), http.StatusInternalServerError)
				return
			}

		case http.MethodPut:
			req, ok := fromJson(w, r)
			if !ok {
				return
			}

			if req.Message.ID == "" {
				http.Error(w, "ID cannot be empty", http.StatusBadRequest)
				return
			}
			if req.Message.Delay <= 0 {
				http.Error(w, "delay must larger than zero", http.StatusBadRequest)
				return
			}

			err := q.Extend(r.Context(), req.Message.ID, req.Message.Delay)
			if err != nil {
				http.Error(w, "error extending message: "+err.Error(), http.StatusInternalServerError)
				return
			}

		case http.MethodDelete:
			req, ok := fromJson(w, r)
			if !ok {
				return
			}

			if req.Message.ID == "" {
				http.Error(w, "ID cannot be empty", http.StatusBadRequest)
				return
			}

			if err := q.Delete(r.Context(), req.Message.ID); err != nil {
				http.Error(w, "error deleting message: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
}

func fromJson(w http.ResponseWriter, r *http.Request) (request, bool) {
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "error decoding request: "+err.Error(), http.StatusBadRequest)
		return req, false
	}
	return req, true
}
