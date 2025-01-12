package models

import "time"

type Message struct {
	Role      string
	Content   string
	Timestamp time.Time
}
