package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maragudk/is"
	_ "github.com/mattn/go-sqlite3"

	"maragu.dev/goqite"
	qhttp "maragu.dev/goqite/http"
	internaltesting "maragu.dev/goqite/internal/testing"
)

type wrapper struct {
	Message goqite.Message
}

type queueMock struct {
	err error
}

func (q *queueMock) Send(ctx context.Context, m goqite.Message) error {
	return q.err
}

func (q *queueMock) Receive(ctx context.Context) (*goqite.Message, error) {
	return nil, q.err
}

func (q *queueMock) ReceiveAndWait(ctx context.Context, interval time.Duration) (*goqite.Message, error) {
	return nil, q.err
}

func (q *queueMock) Extend(ctx context.Context, id goqite.ID, delay time.Duration) error {
	return q.err
}

func (q *queueMock) Delete(ctx context.Context, id goqite.ID) error {
	return q.err
}

func TestNewHandler(t *testing.T) {
	t.Run("errors if cannot decode request", func(t *testing.T) {
		q := &queueMock{}
		h := qhttp.NewHandler(q)

		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			t.Run(method, func(t *testing.T) {
				r := httptest.NewRequest(method, "/", strings.NewReader(`{`))
				w := httptest.NewRecorder()
				h(w, r)

				is.Equal(t, http.StatusBadRequest, w.Code)
			})
		}
	})
}

func TestNewHandler_Get(t *testing.T) {
	t.Run("receives nothing if there is no message", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		code, body, _ := newRequest(t, h, http.MethodGet, nil)
		is.Equal(t, http.StatusNoContent, code)
		is.Equal(t, "", body)
	})

	t.Run("errors if cannot receive from queue", func(t *testing.T) {
		q := &queueMock{err: errors.New("oh no")}
		h := qhttp.NewHandler(q)

		code, _, _ := newRequest(t, h, http.MethodGet, nil)
		is.Equal(t, http.StatusInternalServerError, code)
	})

	t.Run("can wait for a message", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		r := httptest.NewRequest(http.MethodGet, "/?timeout=300ms", nil)
		w := httptest.NewRecorder()
		h(w, r)

		is.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("errors if timeout is invalid", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		for _, timeout := range []string{"notaduration", "0s", "20s1ns"} {
			t.Run(timeout, func(t *testing.T) {
				r := httptest.NewRequest(http.MethodGet, "/?timeout="+timeout, nil)
				w := httptest.NewRecorder()
				h(w, r)

				is.Equal(t, http.StatusBadRequest, w.Code)
			})
		}
	})

	t.Run("errors if interval is invalid", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		for _, interval := range []string{"notaduration", "0s", "100ms1ns"} {
			t.Run(interval, func(t *testing.T) {
				r := httptest.NewRequest(http.MethodGet, "/?timeout=100ms&interval="+interval, nil)
				w := httptest.NewRecorder()
				h(w, r)

				is.Equal(t, http.StatusBadRequest, w.Code)
			})
		}
	})
}

func TestNewHandler_Post(t *testing.T) {
	t.Run("posts and receives a message", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		code, body, _ := newRequest(t, h, http.MethodPost, &goqite.Message{
			Body: []byte("yo"),
		})
		is.Equal(t, http.StatusOK, code)
		is.Equal(t, "", body)

		code, _, res := newRequest(t, h, http.MethodGet, nil)
		is.Equal(t, http.StatusOK, code)
		is.Equal(t, "yo", string(res.Message.Body))
	})

	t.Run("errors if delay is negative", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		code, body, _ := newRequest(t, h, http.MethodPost, &goqite.Message{
			Delay: -1,
		})
		is.Equal(t, http.StatusBadRequest, code)
		is.Equal(t, "delay cannot be negative", body)
	})

	t.Run("errors if cannot send to queue", func(t *testing.T) {
		q := &queueMock{err: errors.New("oh no")}
		h := qhttp.NewHandler(q)

		code, _, _ := newRequest(t, h, http.MethodPost, &goqite.Message{
			Body: []byte("yo"),
		})
		is.Equal(t, http.StatusInternalServerError, code)
	})
}

func TestNewHandler_Put(t *testing.T) {
	t.Run("can extend a message timeout", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{Timeout: time.Millisecond})

		code, _, _ := newRequest(t, h, http.MethodPost, &goqite.Message{
			Body: []byte("yo"),
		})
		is.Equal(t, http.StatusOK, code)

		code, _, res := newRequest(t, h, http.MethodGet, nil)
		is.Equal(t, http.StatusOK, code)
		is.Equal(t, "yo", string(res.Message.Body))

		code, _, _ = newRequest(t, h, http.MethodPut, &goqite.Message{
			ID:    res.Message.ID,
			Delay: time.Second,
		})
		is.Equal(t, http.StatusOK, code)

		time.Sleep(time.Millisecond)

		code, _, _ = newRequest(t, h, http.MethodGet, nil)
		is.Equal(t, http.StatusNoContent, code)
	})

	t.Run("errors if delay is zero", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		code, body, _ := newRequest(t, h, http.MethodPut, &goqite.Message{
			ID:    "1",
			Delay: 0,
		})
		is.Equal(t, http.StatusBadRequest, code)
		is.Equal(t, "delay must larger than zero", body)
	})

	t.Run("errors if id is empty", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		code, body, _ := newRequest(t, h, http.MethodPut, &goqite.Message{
			Delay: 1,
		})
		is.Equal(t, http.StatusBadRequest, code)
		is.Equal(t, "ID cannot be empty", body)
	})

	t.Run("errors if cannot extend in queue", func(t *testing.T) {
		q := &queueMock{err: errors.New("oh no")}
		h := qhttp.NewHandler(q)

		code, _, _ := newRequest(t, h, http.MethodPut, &goqite.Message{
			ID:    "1",
			Delay: 1,
		})
		is.Equal(t, http.StatusInternalServerError, code)
	})
}

func TestNewHandler_Delete(t *testing.T) {
	t.Run("deletes a message", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		code, _, _ := newRequest(t, h, http.MethodPost, &goqite.Message{
			Body: []byte("yo"),
		})
		is.Equal(t, http.StatusOK, code)

		code, _, res := newRequest(t, h, http.MethodGet, nil)
		is.Equal(t, http.StatusOK, code)

		code, body, _ := newRequest(t, h, http.MethodDelete, &res.Message)
		is.Equal(t, http.StatusOK, code)
		is.Equal(t, "", body)

		code, _, _ = newRequest(t, h, http.MethodGet, nil)
		is.Equal(t, http.StatusNoContent, code)
	})

	t.Run("errors if id is empty", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		code, body, _ := newRequest(t, h, http.MethodDelete, &goqite.Message{})
		is.Equal(t, http.StatusBadRequest, code)
		is.Equal(t, "ID cannot be empty", body)
	})

	t.Run("errors if cannot delete from queue", func(t *testing.T) {
		q := &queueMock{err: errors.New("oh no")}
		h := qhttp.NewHandler(q)

		code, _, _ := newRequest(t, h, http.MethodDelete, &goqite.Message{
			ID: "1",
		})
		is.Equal(t, http.StatusInternalServerError, code)
	})
}

func newRequest(t testing.TB, h http.HandlerFunc, method string, m *goqite.Message) (int, string, *wrapper) {
	t.Helper()

	var body io.Reader
	if m != nil {
		b, err := json.Marshal(wrapper{*m})
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}

	r := httptest.NewRequest(method, "/", body)
	w := httptest.NewRecorder()
	h(w, r)

	var res wrapper
	_ = json.Unmarshal(w.Body.Bytes(), &res)

	return w.Code, strings.TrimSpace(w.Body.String()), &res
}

func newH(t testing.TB, opts goqite.NewOpts) http.HandlerFunc {
	t.Helper()

	q := internaltesting.NewQ(t, opts, ":memory:")
	return qhttp.NewHandler(q)
}
