package models

type Chat struct {
	ID       string
	Title    string
	Messages []Message
	Active   bool
}
