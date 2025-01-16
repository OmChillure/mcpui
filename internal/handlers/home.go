package handlers

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	mcpwebui "github.com/MegaGrindStone/mcp-web-ui"
	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
	"github.com/google/uuid"
	"github.com/tmaxmax/go-sse"
)

type Home struct {
	sseSrv    *sse.Server
	templates *template.Template
}

type homePageData struct {
	Chats         []models.Chat
	Messages      []models.Message
	CurrentChatID string
}

const chatsTopic = "chats"

var (
	chatsSSEType    = sse.Type("chats")
	messagesSSEType = sse.Type("messages")
)

var chats = []models.Chat{}

func NewHome() (Home, error) {
	tmpl, err := template.ParseFS(
		mcpwebui.TemplateFS,
		"templates/layout/*.html",
		"templates/pages/*.html",
		"templates/partials/*.html",
	)
	if err != nil {
		return Home{}, err
	}

	return Home{
		sseSrv: &sse.Server{
			OnSession: func(s *sse.Session) (sse.Subscription, bool) {
				topics := []string{sse.DefaultTopic, chatsTopic}

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
	}, nil
}

func messageIDTopic(messageID string) string {
	return fmt.Sprintf("message-%s", messageID)
}

func (h Home) HandleHome(w http.ResponseWriter, r *http.Request) {
	for i := range chats {
		chats[i].Active = false
	}

	currentChatID := ""
	var messages []models.Message
	if r.URL.Query().Get("chat_id") != "" {
		currentChatID = r.URL.Query().Get("chat_id")
		idx := slices.IndexFunc(chats, func(c models.Chat) bool {
			return c.ID == currentChatID
		})
		if idx >= 0 {
			messages = chats[idx].Messages
			chats[idx].Active = true
		}
	}
	data := homePageData{
		Chats:         chats,
		Messages:      messages,
		CurrentChatID: currentChatID,
	}

	err := h.templates.ExecuteTemplate(w, "home.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h Home) HandleChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	message := r.FormValue("message")
	if message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	var err error

	chatID := r.FormValue("chat_id")
	isNewChat := false
	if chatID == "" {
		chatID, err = h.newChat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		isNewChat = true
	}

	userMsg := models.Message{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	}

	aiMsg := models.Message{
		ID:             uuid.New().String(),
		Role:           "ai",
		Timestamp:      time.Now(),
		StreamingState: models.StreamingStateLoading,
	}

	messages := []models.Message{userMsg, aiMsg}

	idx := slices.IndexFunc(chats, func(c models.Chat) bool {
		return c.ID == chatID
	})
	if idx < 0 {
		http.Error(w, "Chat not found", http.StatusNotFound)
		return
	}

	chats[idx].Messages = append(chats[idx].Messages, messages...)

	go func() {
		chunks := []string{
			"Hello! ",
			"I'm your ",
			"AI assistant. ",
			"How can I ",
			"help you today?",
		}

		msgIdx := len(chats[idx].Messages) - 1

		time.Sleep(time.Second * 2)

		for _, chunk := range chunks {
			time.Sleep(time.Second * 1)

			chats[idx].Messages[msgIdx].Content += chunk
			content := chats[idx].Messages[msgIdx].Content
			chats[idx].Messages[msgIdx].StreamingState = models.StreamingStateStreaming

			msg := sse.Message{
				Type: messagesSSEType,
			}
			msg.AppendData(fmt.Sprintf("<p class='mb-0'>%s</p>", content))
			msgID := chats[idx].Messages[msgIdx].ID

			if err := h.sseSrv.Publish(&msg, messageIDTopic(msgID)); err != nil {
				log.Printf("Failed to publish message: %v", err)
				return
			}
		}

		e := &sse.Message{Type: sse.Type("closeMessage")}
		e.AppendData("bye")
		_ = h.sseSrv.Publish(e)
	}()

	if isNewChat {
		go h.generateChatTitle(chats[idx])

		data := homePageData{
			CurrentChatID: chatID,
			Messages:      chats[idx].Messages,
		}
		err = h.templates.ExecuteTemplate(w, "chatbox", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	err = h.templates.ExecuteTemplate(w, "user_message", userMsg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = h.templates.ExecuteTemplate(w, "ai_message", aiMsg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h Home) HandleSSE(w http.ResponseWriter, r *http.Request) {
	h.sseSrv.ServeHTTP(w, r)
}

func (h Home) Shutdown(ctx context.Context) error {
	e := &sse.Message{Type: sse.Type("closeChat")}
	// Adding data is necessary because spec-compliant clients
	// do not dispatch events without data.
	e.AppendData("bye")
	// Broadcast a close message so clients can gracefully disconnect.
	_ = h.sseSrv.Publish(e)

	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	// We use a context with a timeout so the program doesn't wait indefinitely
	// for connections to terminate. There may be misbehaving connections
	// which may hang for an unknown timespan, so we just stop waiting on Shutdown
	// after a certain duration.
	return h.sseSrv.Shutdown(ctx)
}

func (h Home) newChat() (string, error) {
	for i := range chats {
		chats[i].Active = false
	}
	newChat := models.Chat{
		ID:     uuid.New().String(),
		Active: true,
	}
	chats = append(chats, newChat)

	divs, err := h.chatDivs()
	if err != nil {
		return "", fmt.Errorf("failed to create chat divs: %w", err)
	}

	msg := sse.Message{
		Type: chatsSSEType,
	}
	msg.AppendData(divs)

	if err := h.sseSrv.Publish(&msg, chatsTopic); err != nil {
		return "", fmt.Errorf("failed to publish chats: %w", err)
	}

	return newChat.ID, nil
}

func (h Home) generateChatTitle(chat models.Chat) {
	time.Sleep(time.Second * 2)

	title := fmt.Sprintf("Chat %s", chat.ID)

	idx := slices.IndexFunc(chats, func(c models.Chat) bool {
		return c.ID == chat.ID
	})

	chats[idx].Title = title

	divs, err := h.chatDivs()
	if err != nil {
		log.Printf("Failed to generate chat title: %v", err)
		return
	}

	msg := sse.Message{
		Type: chatsSSEType,
	}
	msg.AppendData(divs)
	if err := h.sseSrv.Publish(&msg, chatsTopic); err != nil {
		log.Printf("Failed to publish chats: %v", err)
	}
}

func (h Home) chatDivs() (string, error) {
	var sb strings.Builder
	for _, chat := range chats {
		err := h.templates.ExecuteTemplate(&sb, "chat_title", chat)
		if err != nil {
			return "", fmt.Errorf("failed to execute chat_title template: %w", err)
		}
	}
	return sb.String(), nil
}
