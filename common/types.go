package common

import "time"

type ID string

type Message struct {
	ID    ID
	Delay time.Duration
	Body  []byte
}
