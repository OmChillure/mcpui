package models

import "time"

// Chat represents a conversation container in the chat system. It provides basic identification and
// labeling capabilities for organizing message threads.
type Chat struct {
	ID    string
	Title string
}

// Message represents an individual communication entry within a chat. It contains the core components
// of a chat message including its unique identifier, the participant's role, the actual content, and
// the precise time when the message was created.
type Message struct {
	ID        string
	Role      string
	Content   string
	Timestamp time.Time
}
