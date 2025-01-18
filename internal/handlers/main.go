package handlers

import (
	"context"
	"fmt"
	"html/template"
	"iter"
	"time"

	mcpwebui "github.com/MegaGrindStone/mcp-web-ui"
	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
	"github.com/tmaxmax/go-sse"
)

// LLM represents a large language model interface that provides chat functionality. It accepts a context
// and a sequence of messages, returning an iterator that yields response chunks and potential errors.
type LLM interface {
	Chat(ctx context.Context, messages []models.Message) iter.Seq2[string, error]
}

// Store defines the interface for managing chat and message persistence. It provides methods for
// creating, reading, and updating chats and their associated messages. The interface supports both
// atomic operations and bulk retrieval of chats and messages.
type Store interface {
	Chats(ctx context.Context) ([]models.Chat, error)
	AddChat(ctx context.Context, chat models.Chat) (string, error)
	UpdateChat(ctx context.Context, chat models.Chat) error

	Messages(ctx context.Context, chatID string) ([]models.Message, error)
	AddMessage(ctx context.Context, chatID string, message models.Message) (string, error)
	UpdateMessage(ctx context.Context, chatID string, message models.Message) error
}

// Main handles the core functionality of the chat application, managing server-sent events,
// HTML templates, and interactions between the LLM and Store components.
type Main struct {
	sseSrv    *sse.Server
	templates *template.Template

	llm   LLM
	store Store
}

const chatsSSETopic = "chats"

// NewMain creates a new Main instance with the provided LLM and Store implementations. It initializes
// the SSE server with default configurations and parses the required HTML templates from the embedded
// filesystem. The SSE server is configured to handle both default events and chat-specific topics.
func NewMain(llm LLM, store Store) (Main, error) {
	// We parse templates from three distinct directories to separate layout, pages, and partial views
	tmpl, err := template.ParseFS(
		mcpwebui.TemplateFS,
		"templates/layout/*.html",
		"templates/pages/*.html",
		"templates/partials/*.html",
	)
	if err != nil {
		return Main{}, err
	}

	return Main{
		sseSrv: &sse.Server{
			OnSession: func(s *sse.Session) (sse.Subscription, bool) {
				// We start with default topics that all clients should subscribe to
				topics := []string{sse.DefaultTopic, chatsSSETopic}

				// We create a message-specific topic if the client requests updates for a particular message
				messageID := s.Req.URL.Query().Get("message_id")
				if messageID != "" {
					topics = append(topics, messageIDTopic(messageID))
				}

				return sse.Subscription{
					Client:      s,
					LastEventID: s.LastEventID,
					Topics:      topics,
				}, true
			},
		},
		templates: tmpl,
		llm:       llm,
		store:     store,
	}, nil
}

func messageIDTopic(messageID string) string {
	return fmt.Sprintf("message-%s", messageID)
}

// Shutdown gracefully terminates the Main instance's SSE server. It broadcasts a close message to all
// connected clients and waits up to 5 seconds for connections to terminate. After the timeout, any
// remaining connections are forcefully closed.
func (m Main) Shutdown(ctx context.Context) error {
	e := &sse.Message{Type: sse.Type("closeChat")}
	// We create a close event that complies with SSE spec requiring data
	e.AppendData("bye")

	// We ignore the error here since we're shutting down anyway
	_ = m.sseSrv.Publish(e)

	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	return m.sseSrv.Shutdown(ctx)
}
