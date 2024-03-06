package http_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maragudk/is"
	_ "github.com/mattn/go-sqlite3"

	"github.com/maragudk/goqite"
	qhttp "github.com/maragudk/goqite/http"
)

type wrapper struct {
	Message goqite.Message
}

func TestGoqiteHandler_Get(t *testing.T) {
	t.Run("receives nothing if there is no message", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{})

		code, body, _ := newRequest(t, h, http.MethodGet, nil)
		is.Equal(t, http.StatusNoContent, code)
		is.Equal(t, "", body)
	})
}

func TestGoqiteHandler_Post(t *testing.T) {
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
}

func TestGoqiteHandler_Put(t *testing.T) {
	t.Run("can extend a message timeout", func(t *testing.T) {
		h := newH(t, goqite.NewOpts{Timeout: time.Millisecond})

		code, _, _ := newRequest(t, h, http.MethodPost, &goqite.Message{
			Body: []byte("yo"),
		})
		is.Equal(t, http.StatusOK, code)

		code, _, res := newRequest(t, h, http.MethodGet, nil)
		is.Equal(t, http.StatusOK, code)
		is.Equal(t, "yo", string(res.Message.Body))

		code, _, res = newRequest(t, h, http.MethodPut, &goqite.Message{
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
}

func TestGoqiteHandler_Delete(t *testing.T) {
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

	q := newQ(t, opts)
	return qhttp.GoqiteHandler(q)
}

func newQ(t testing.TB, opts goqite.NewOpts) *goqite.Queue {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	schema, err := os.ReadFile("../schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(string(schema))
	if err != nil {
		t.Fatal(err)
	}

	opts.DB = db

	if opts.Name == "" {
		opts.Name = "test"
	}

	return goqite.New(opts)
}
