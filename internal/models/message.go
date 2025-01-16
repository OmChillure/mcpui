package models

import "time"

type Message struct {
	ID        string
	Role      string
	Content   string
	Timestamp time.Time

	StreamingState string
}

const (
	StreamingStateLoading   = "loading"
	StreamingStateStreaming = "streaming"
	StreamingStateEnded     = "ended"
)
